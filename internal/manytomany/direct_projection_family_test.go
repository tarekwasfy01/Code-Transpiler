package manytomany

import (
	"strings"
	"testing"
)

// Module headers are structured UAST containers. They must not turn a
// source-independent direct projection into an unsupported expression.
func TestDirectProjectionEmitsModuleContainer(t *testing.T) {
	p, err := Parse("go", "package main\nfunc main() { println(1) }\n")
	if err != nil {
		t.Fatal(err)
	}
	code, err := EmitDirect("python", p)
	if err != nil {
		t.Fatalf("direct module projection: %v", err)
	}
	if strings.TrimSpace(code) == "" {
		t.Fatal("direct module projection emitted no source")
	}
}
