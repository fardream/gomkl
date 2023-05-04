package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"log"
	"os"
	"path"
	"sort"
	"strings"
	"text/template"

	"github.com/go-clang/clang-v15/clang"
	"github.com/spf13/cobra"
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

func run(cmd *cobra.Command, args []string) {
	if mklPath == "" {
		mklRoot := os.Getenv("MKLROOT")
		if mklRoot == "" {
			mklRoot = "/opt/intel/oneapi/mkl/latest"
		}
		mklPath = path.Join(mklRoot, "include", "mkl.h")
	}

	includePath := path.Dir(mklPath)

	idx := clang.NewIndex(0, 1)
	defer idx.Dispose()

	tu := idx.ParseTranslationUnit(mklPath, []string{fmt.Sprintf("-I%s", includePath)}, nil, 0)
	defer tu.Dispose()

	diagnostics := tu.Diagnostics()
	for _, d := range diagnostics {
		log.Println("PROBLEM: ", d.Spelling())
	}

	flist := readFuncList(inputFuncsPath)

	cursor := tu.TranslationUnitCursor()

	funcs := make([]funcDef, 0)

	cursor.Visit(func(cursor, parent clang.Cursor) (status clang.ChildVisitResult) {
		if cursor.IsNull() {
			return clang.ChildVisit_Continue
		}

		if cursor.Kind() != clang.Cursor_FunctionDecl {
			return clang.ChildVisit_Continue
		}

		name := cursor.Spelling()

		is32, is64, betterName := flist.findFunc(name)

		if !is32 && !is64 {
			return clang.ChildVisit_Continue
		}

		fdef := funcDef{RawName: name, is32: is32, BetterName: betterName}
		fdef.returnType = cursor.ResultType().Spelling()
		for i := uint32(0); i < uint32(cursor.NumArguments()); i++ {
			arg := cursor.Argument(i)
			paramName := arg.Spelling()
			if paramName == "" {
				paramName = fmt.Sprintf("p%d", i)
			}
			typeName := arg.Type().Spelling()
			rustname, dontUse := getParamType(typeName)
			fdef.args = append(fdef.args, funcArg{
				name:     paramName,
				typeName: typeName,
				rustName: rustname,
				dontUse:  dontUse,
			})
		}

		funcs = append(funcs, fdef)

		return clang.ChildVisit_Continue
	})

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
