// Copyright (c) 2026 Tarek Wasfy
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

func TestLowerMatrixTypePropagationAcrossBindings(t *testing.T) {
	p, err := LowerMatrixLanguage("python", "x = 7\ny = x + 5\n")
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]SemanticType{}
	for i := range p.UniversalAST.Nodes {
		common, decodeErr := decodeUniversalCommon(&p.UniversalAST.Nodes[i])
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		if common.Kind == "assign" && common.Name != "" {
			byName[common.Name] = common.Type
		}
	}
	if got := byName["x"]; got.Kind != "float" || got.Bits != 64 {
		t.Fatalf("binding x lost literal type: %+v", got)
	}
	if got := byName["y"]; got.Kind != "float" || got.Bits != 64 {
		t.Fatalf("binding y lost transitive expression type: %+v", got)
	}
}
