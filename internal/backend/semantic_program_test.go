package backend

import (
	"strings"
	"testing"
)

func TestEagerRBoundaryPreservesActualCallSemantics(t *testing.T) {
	src := "twice <- function(x) { return(7) }\nprobe <- function(x) { print(99);return(x) }\nprint(twice(probe(3)))\n"
	original, err := ParseSemantic("python", src)
	if err != nil {
		t.Fatal(err)
	}
	r, err := EmitSemantic("r", original)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r, "force(r2m_value_0)") {
		t.Fatal("R boundary does not actually force unused argument")
	}
	decoded, err := ParseSemantic("r", r)
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range decoded.Evidence.Nodes {
		if n.Kind == "call" && decoded.Evidence.CallModes.At(i, 1) != 1 {
			t.Fatal("decoded eager call missing from call-mode matrix")
		}
	}
	for _, l := range Languages {
		a, e := EmitSemantic(l.ID, original)
		if e != nil {
			t.Fatal(e)
		}
		b, e := EmitSemantic(l.ID, decoded)
		if e != nil || a != b {
			t.Fatalf("%s R boundary differs: %v", l.ID, e)
		}
	}
	// A changed helper must not retain the eager interpretation.
	modified := strings.Replace(r, "force(r2m_value_0)", "print(123)", 1)
	p, e := ParseSemantic("r", modified)
	if e == nil {
		code, e := EmitSemantic("python", p)
		old, _ := EmitSemantic("python", original)
		if e == nil && code == old {
			t.Fatal("modified helper was discarded")
		}
	}
}
func TestSemanticMatricesDistinguishScopeAndUnknowns(t *testing.T) {
	p, e := ParseSemantic("python", "x <- 1\nf <- function(x) { y <- x + 2; return(y) }\nprint(f(x))")
	if e != nil {
		t.Fatal(e)
	}
	if p.Evidence.Scope.Cols != 2 || p.Evidence.Contract[4] != 0 {
		t.Fatal("scope or unknown source equivalence lost")
	}
	for i := range p.Evidence.Nodes {
		var sum float64
		for c := 0; c < p.Evidence.Types.Cols; c++ {
			sum += p.Evidence.Types.At(i, c)
		}
		if sum != 1 {
			t.Fatal("type vector not one-hot including unknown")
		}
	}
	p.Evaluation = "unknown"
	if _, e := EmitSemantic("go", p); e == nil {
		t.Fatal("unknown contract silently chosen")
	}
}
func TestEagerRForceShadowingRejects(t *testing.T) {
	p, _ := ParseSemantic("python", "force <- function(x) {return(0)}\nprint(2)")
	if _, e := EmitSemantic("r", p); e == nil {
		t.Fatal("shadowed force could change generated wrapper semantics")
	}
}
