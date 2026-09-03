package backend

import (
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"testing"
)

func TestLowerMatrixActionsDoesNotReadCanonicalTextField(t *testing.T) {
	c, err := matrixir.Canonicalize("python", "x = 2\nprint(x + 3)\n")
	if err != nil {
		t.Fatal(err)
	}
	c.R = "this is deliberately not parser input"
	p, err := LowerMatrixEvents("python", c.Events)
	if err != nil {
		t.Fatal(err)
	}
	out, err := EmitSemantic("go", p)
	if err != nil || out == "" {
		t.Fatalf("action lowering output=%q err=%v", out, err)
	}
}
