package main

import (
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"testing"
)

func TestRouteClosureDistinguishesCandidateReachability(t *testing.T) {
	m := matrixir.NewSparseMatrix(4, 4)
	m.Set(0, 1, 1)
	m.Set(1, 2, 1)
	r, err := routeClosure(m)
	if err != nil {
		t.Fatal(err)
	}
	if r.NonZeros() != 3 || r.At(0, 2) != 1 || r.At(2, 0) != 0 || r.At(3, 3) != 0 {
		t.Fatal("incorrect directed reachability")
	}
	if m.At(0, 2) != 0 {
		t.Fatal("closure mutated direct declarations")
	}
}
