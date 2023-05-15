package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"text/template"

	"github.com/spf13/cobra"
	"modernc.org/cc/v4"
)

//go:embed rs.tmpl
var rsTmplText string

var (
	mklPath          = ""
	inputFuncsPath   = ""
	outputFile       = ""
	mklProviderCrate = "crate"
	traitName        = "MKLRoutines"
)

type funcArg struct {
	name     string
	typeName string
	rustName string
	dontUse  bool
}
type funcDef struct {
	RawName    string
	is32       bool
	returnType string
	args       []funcArg
	BetterName string
}

type tmpInput struct {
	funcDefs        []funcDef
	providerCrate   string
	DesiredFuncList []string
}

func (*tmpInput) TraitName() string {
	return traitName
}

func (i *tmpInput) UseLine() string {
	uses := make([]string, 0, len(i.funcDefs)+3)

	blastypes := make(map[string]struct{})

	for _, f := range i.funcDefs {
		uses = append(uses, f.RawName)
		for _, arg := range f.args {
			if !arg.dontUse {
				blastypes[arg.rustName] = struct{}{}
			}
		}
	}

	for k := range blastypes {
		uses = append(uses, k)
	}

	sort.Strings(uses)

	return fmt.Sprintf("%s::{%s}", i.providerCrate, strings.Join(uses, ", "))
}

func (i *tmpInput) getfuncs(is32 bool) []*funcDef {
	r := []*funcDef{}
	for _, f := range i.funcDefs {
		f := f
		if f.is32 == is32 {
			r = append(r, &f)
		}
	}

	return r
}

func (i *tmpInput) F64Funcs() []*funcDef {
	return i.getfuncs(false)
}

func (i *tmpInput) F32Funcs() []*funcDef {
	return i.getfuncs(true)
}

func (i *tmpInput) TraitFuncs() []*funcDef {
	return i.getfuncs(true)
}

func (f *funcDef) ReturnDeclare() string {
	switch f.returnType {
	case "void":
		return ""
	case "int32_t", "int":
		return "-> i32"
	case "float", "double":
		return "-> Self"
	case "size_t":
		return "-> usize"
	default:
		return f.returnType
	}
}

func getParamType(t string) (string, bool) {
	switch t {
	case "size_t":
		return "usize", true
	case "int32_t", "int", "const int", "const int32_t":
		return "i32", true
	case "int64_t":
		return "i64", true
	case "const double *", "const float *", "const float[]", "const double[]":
		return "*const Self", true
	case "double *", "float *", "float[]", "double[]":
		return "*mut Self", true
	case "double", "float", "const double", "const float":
		return "Self", true
	case "char":
		return "i8", true
	case "int *":
		return "*mut i32", true
	}

	if strings.HasPrefix(t, "const ") {
		return strings.TrimPrefix(t, "const "), false
	}

	return t, false
}

func (f *funcDef) Params() []string {
	r := []string{}

	for _, p := range f.args {
		t, _ := getParamType(p.typeName)
		r = append(r, fmt.Sprintf("%s: %s", p.name, t))
	}

	return r
}

func (f *funcDef) CallParams() []string {
	r := []string{}

	for _, p := range f.args {
		r = append(r, p.name)
	}

	return r
}

func retrieveType(r *cc.DeclarationSpecifiers) string {
	switch r.Case {
	case cc.DeclarationSpecifiersTypeQual:
		return r.TypeQualifier.Token.SrcStr() + " " + retrieveType(r.DeclarationSpecifiers)
	case cc.DeclarationSpecifiersTypeSpec:
		return r.TypeSpecifier.Token.SrcStr()
	case cc.DeclarationSpecifiersAlignSpec:
		fallthrough
	case cc.DeclarationSpecifiersFunc:
		fallthrough
	case cc.DeclarationSpecifiersStorage:
		fallthrough
	case cc.DeclarationSpecifiersAttr:
		fallthrough
	default:
		return retrieveType(r.DeclarationSpecifiers)
	}
}

func retrieveParams(r *cc.ParameterList) []funcArg {
	if r == nil {
		return nil
	}

	if r.ParameterDeclaration != nil {
		// typename
		typeName := retrieveType(r.ParameterDeclaration.DeclarationSpecifiers)
		decl := r.ParameterDeclaration.Declarator
		paramName := decl.DirectDeclarator.Token.SrcStr()
		if decl.Pointer != nil && decl.Pointer.Case == cc.PointerTypeQual {
			typeName = typeName + " *"
		}
		if decl.DirectDeclarator.Case == cc.DirectDeclaratorArr {
			typeName = typeName + "[]"
			paramName = decl.DirectDeclarator.DirectDeclarator.Token.SrcStr()
		}

		rustname, dontUse := getParamType(typeName)
		return append([]funcArg{{
			name:     paramName,
			typeName: typeName,
			rustName: rustname,
			dontUse:  dontUse,
		}}, retrieveParams(r.ParameterList)...)
	}

	return retrieveParams(r.ParameterList)
}

func (flist *funcListInput) retrieveFuncDef(d *cc.ExternalDeclaration) *funcDef {
	if d == nil {
		return nil
	}

	if d.Declaration == nil {
		return nil
	}

	// DeclarationSpecifiers InitDeclaratorList AttributeSpecifierList ';'  // Case DeclarationDecl
	if d.Declaration.Case != cc.DeclarationDecl {
		return nil
	}

	if d.Declaration.InitDeclaratorList == nil {
		return nil
	}

	if d.Declaration.InitDeclaratorList.InitDeclarator == nil {
		return nil
	}

	//	InitDeclarator:
	//	        Declarator Asm                  // Case InitDeclaratorDecl
	//	|       Declarator Asm '=' Initializer  // Case InitDeclaratorInit
	if d.Declaration.InitDeclaratorList.InitDeclarator.Case != cc.InitDeclaratorDecl {
		return nil
	}

	decl := d.Declaration.InitDeclaratorList.InitDeclarator.Declarator.DirectDeclarator

	if decl == nil {
		return nil
	}

	// function name
	if decl.DirectDeclarator == nil || decl.DirectDeclarator.Case != cc.DirectDeclaratorIdent {
		return nil
	}

	name := decl.DirectDeclarator.Token.SrcStr()

	is32, is64, betterName := flist.findFunc(name)

	if !is32 && !is64 {
		return nil
	}

	returnType := retrieveType(d.Declaration.DeclarationSpecifiers)

	// retrieve arguments
	fdef := funcDef{
		RawName:    name,
		returnType: returnType,
		BetterName: betterName,
		args:       retrieveParams(decl.ParameterTypeList.ParameterList),
		is32:       is32,
	}

	return &fdef
}

func run(cmd *cobra.Command, args []string) {
	if mklPath == "" {
		mklRoot := os.Getenv("MKLROOT")
		if mklRoot == "" {
			mklRoot = "/opt/intel/oneapi/mkl/latest"
		}
		mklPath = path.Join(mklRoot, "include", "mkl.h")
	}

	includePath := path.Dir(mklPath)

	compiler := getOrPanic(cc.NewConfig("", ""))
	compiler.IncludePaths = append(compiler.IncludePaths, includePath)

	ccast := getOrPanic(cc.Translate(compiler, []cc.Source{
		{Name: "<predefined>", Value: compiler.Predefined},
		{Name: "<builtin>", Value: cc.Builtin},
		{Name: mklPath},
	}))

	flist := readFuncList(inputFuncsPath)

	funcs := make([]funcDef, 0)

	cctu := ccast.TranslationUnit

	for thistu := cctu; thistu != nil; thistu = thistu.TranslationUnit {
		f := flist.retrieveFuncDef(thistu.ExternalDeclaration)
		if f != nil {
			funcs = append(funcs, *f)
		}
	}

	rsTmpl := getOrPanic(template.New("rs-tmpl").Parse(rsTmplText))

	var b bytes.Buffer

	orPanic(rsTmpl.Execute(&b, &tmpInput{
		funcDefs:        funcs,
		providerCrate:   mklProviderCrate,
		DesiredFuncList: flist.desiredFuncList,
	}))

	orPanic(os.WriteFile(outputFile, b.Bytes(), 0o666))
}

func main() {
	cmd := &cobra.Command{
		Short: "generate select bindings for rust",
		Use:   "gen-mkl-rs",
		Args:  cobra.NoArgs,
	}

	cmd.Flags().StringVarP(&inputFuncsPath, "input", "i", inputFuncsPath, "list of functions to generate. use * for s/d")
	cmd.MarkFlagFilename("input")
	cmd.MarkFlagRequired("input")

	cmd.Flags().StringVarP(&outputFile, "output", "o", outputFile, "output file")
	cmd.MarkFlagFilename("output", "rs")
	cmd.MarkFlagRequired("output")

	cmd.Flags().StringVarP(&mklPath, "mkl-header", "m", mklPath, "path to mkl.h file")
	cmd.MarkFlagFilename("mkl-header", ".h")

	cmd.Flags().StringVarP(&mklProviderCrate, "mkl-provider-crate", "c", mklProviderCrate, "mkl provider crate")
	cmd.Flags().StringVarP(&traitName, "trait-name", "t", traitName, "trait name")
	cmd.Run = run
	cmd.Execute()
}
