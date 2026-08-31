package backend

import (
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"reflect"
	"strings"
	"testing"
)

func TestIterationMatrixStateAndHygiene(t *testing.T) {
	for length := 0; length < 19; length++ {
		state, _ := matrixir.MatrixFromRows([][]float64{{1}, {float64(length)}, {1}})
		for position := 1; position <= length+2; position++ {
			if state.At(0, 0) != float64(position) || state.At(1, 0) != float64(length) || state.At(2, 0) != 1 {
				t.Fatal("cursor transition corrupted state", state)
			}
			state, _ = iterationAdvanceMatrix().Multiply(state)
		}
	}
	ast, err := parse("f <- function(r2miterseq1) { r2miterpos2 <- 3; total <- 0; for(i in r2miterseq1) { for(j in c(1,2)) { if(j==2) { next }; total <- total+i }; if(i==2) { break } }; return(total) }")
	if err != nil {
		t.Fatal(err)
	}
	fn := ast.List[0].(*AssignStmt).Value.(*FunctionExpr)
	first, err := buildFunctionFlow(fn)
	if err != nil {
		t.Fatal(err)
	}
	second, err := buildFunctionFlow(fn)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("lowering mutated parsed function")
	}
	if len(first.iterations) != 2 || !first.stateMachine {
		t.Fatal("missing iteration state")
	}
	for _, it := range first.iterations {
		for _, name := range []string{it.Sequence, it.Position, it.Length} {
			if !strings.HasPrefix(name, "\x00") {
				t.Fatal("private slot has source-visible identity")
			}
			if _, err := lex(name); err == nil {
				t.Fatal("source lexer accepts private slot identity")
			}
		}
		if it.Sequence == "r2miterseq1" || it.Position == "r2miterpos2" {
			t.Fatal("captured source identifier")
		}
	}
	demand := parameterDemand(fn)
	found := false
	for _, v := range demand.Data {
		found = found || v != 0
	}
	if !found {
		t.Fatal("iterable parameter demand missing")
	}
}

func TestIterationGuardsAcrossTargets(t *testing.T) {
	for _, target := range Languages {
		for _, code := range []string{
			"f <- function(x) { total <- 0; for(i in x) { total <- total+i }; return(total) }; print(f(print(3)))",
			"f <- function(x) { total <- 0; for(i in c(1,2)) { total <- total+x }; return(total) }; print(f(print(3)))",
		} {
			_, err := Transpile(target.ID, code)
			if err == nil || !strings.Contains(err.Error(), "promise") {
				t.Fatalf("%s: lost promise guard: %v", target.ID, err)
			}
		}
		_, err := Transpile(target.ID, "f <- function() { for(i in c()) { print(i) }; return(i) }; print(f())")
		if err == nil || !strings.Contains(err.Error(), "before definite assignment") {
			t.Fatalf("%s: lost initialization guard: %v", target.ID, err)
		}
	}
}
