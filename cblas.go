package gomkl

import "fmt"

// #include <mkl.h>
import "C"

func CblasDaxpy(n int, a float64, x []float64, incx int, y []float64, incy int) error {
	if len(x) < (n*incx - incx + 1) {
		return fmt.Errorf("length of x is too short: %d", len(x))
	}
	if len(y) < (n*incy - incy + 1) {
		return fmt.Errorf("length of y is too short: %d", len(y))
	}

	C.cblas_daxpy((C.int)(n), (C.double)(a), (*C.double)(&x[0]), (C.int)(incx), (*C.double)(&y[0]), (C.int)(incy))

	return nil
}
