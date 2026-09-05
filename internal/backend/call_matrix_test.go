package backend

import "testing"

func TestArgumentBindingPermutationAndDefaults(t *testing.T) {
	ast, err := parse("f <- function(x,y=2) { return(x+y) }")
	if err != nil {
		t.Fatal(err)
	}
	fn := ast.List[0].(*AssignStmt).Value.(*FunctionExpr)
	b, err := argumentBindingMatrix(fn, []Arg{{Name: "y", Value: &LiteralExpr{Kind: "number", Text: "3"}}, {Name: "x", Value: &LiteralExpr{Kind: "number", Text: "1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if b.At(0, 1) != 1 || b.At(1, 0) != 1 || b.At(0, 0) != 0 || b.At(1, 1) != 0 {
		t.Fatalf("invalid permutation: %+v", b)
	}
	for _, target := range Languages {
		for _, code := range []string{"f <- function(x,y=2) { return(x+y) }; print(f(3))", "f <- function(x,y) { return(x-y) }; print(f(y=2,x=7))", "f <- function(x) { return(x+x) }; print(f(print(3)))"} {
			if _, err := TranspileFrom("python", target.ID, code); err != nil {
				t.Fatalf("%s: %v", target.ID, err)
			}
		}
	}
}

func TestDemandMatrixCountsRepeatedAndUnusedParameters(t *testing.T) {
	ast, err := parse("f <- function(x,y) { return(x+x) }")
	if err != nil {
		t.Fatal(err)
	}
	fn := ast.List[0].(*AssignStmt).Value.(*FunctionExpr)
	demand := parameterDemand(fn)
	risk, err := demand.Multiply(columnVector([]float64{1, 0}))
	if err != nil {
		t.Fatal(err)
	}
	total := 0.0
	for _, v := range risk.Data {
		total += v
	}
	if total != 2 {
		t.Fatalf("effectful x has %v uses, want 2", total)
	}
	risk, err = demand.Multiply(columnVector([]float64{0, 1}))
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range risk.Data {
		if v != 0 {
			t.Fatal("unused y must have zero demand")
		}
	}
}
