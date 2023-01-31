package gomkl_test

import (
	"testing"

	"github.com/fardream/gomkl"
)

func TestCblasDaxpy(t *testing.T) {
	expected := []float64{3, 6, 9, 12, 1}
	y := []float64{0, 0, 0, 0, 1}
	err := gomkl.CblasDaxpy(4, 3, []float64{1, 2, 3, 4, 5}, 1, y, 1)
	if err != nil {
		t.Fatalf("failed to daxpy: %v", err)
	}

	for i := 0; i < len(expected); i++ {
		if expected[i] != y[i] {
			t.Errorf("error at %d: expected: %f, got %f", i, expected[i], y[i])
		}
	}
}
