package backend

import "testing"

// R function definitions are a structured closure/binding family shared by
// the MatrixIR frontend. Keep a minimal real R witness so corpus replay never
// regresses to passing a Tree-sitter expected-tree payload as source.
func TestRFunctionWitness(t *testing.T) {
	p, err := LowerSource("r", "", "function() 1")
	if err != nil {
		t.Fatalf("lower R function witness: %v", err)
	}
	if _, err := EmitSemanticDirect("c", p); err != nil {
		t.Fatalf("emit R function witness directly: %v", err)
	}
	if _, err := EmitSemanticDirect("r", p); err != nil {
		t.Fatalf("emit R function witness to R directly: %v", err)
	}
}

func TestRFunctionCorpusWitness(t *testing.T) {
	const source = `function() 1
function() {}
function(arg1, arg2) { arg2 }
function(arg1, arg2 = 2) {}
function(x, y, z = 3) {}
function() 1
function() 1 + 1
function() function() {}
function(x = function() {}) {}
function() for(i in 1:5) i`
	p, err := LowerSource("r", "", source)
	if err != nil {
		t.Fatalf("lower R function corpus witness: %v", err)
	}
	if _, err := EmitSemanticDirect("r", p); err != nil {
		t.Fatalf("emit R function corpus witness to R directly: %v", err)
	}
}
