package backend

import (
	"strings"
	"testing"
)

func TestFunctionFlowPathProjection(t *testing.T) {
	code := "f <- function(x) { if (x > 0) { if (x > 2) { return(9) } else { return(8) } }; return(7) }"
	ast, err := parse(code)
	if err != nil {
		t.Fatal(err)
	}
	f, err := buildFunctionFlow(ast.List[0].(*AssignStmt).Value.(*FunctionExpr))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		choices []bool
		want    string
	}{{[]bool{true, true}, "9"}, {[]bool{true, false}, "8"}, {[]bool{false}, "7"}} {
		node, index := f.entry, 0
		for {
			if r, ok := f.nodes[node].(*ReturnStmt); ok {
				if r.X.(*LiteralExpr).Text != tc.want {
					t.Fatalf("%v returns %v, want %s", tc.choices, r.X, tc.want)
				}
				if index != len(tc.choices) {
					t.Fatal("unvisited condition")
				}
				break
			}
			if index >= len(tc.choices) {
				t.Fatal("unexpected condition")
			}
			m := f.F
			if tc.choices[index] {
				m = f.T
			}
			index++
			node, err = flowSuccessor(m, node)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if f.reachable[0] != 0 {
		t.Fatal("fallthrough must not be reachable")
	}
}

func TestFunctionFlowRejectsUnmodeledPaths(t *testing.T) {
	for _, code := range []string{
		"f <- function(x) { if (x > 0) { return(1) } }",
		"f <- function(x) { break; return(x) }",
		"f <- function(x) { next; return(x) }",
		"f <- function(x) { while(x > 0) { y <- 1; x <- x-1 }; return(y) }",
		"f <- function(x) { y <<- x; return(y) }",
	} {
		ast, err := parse(code)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = buildFunctionFlow(ast.List[0].(*AssignStmt).Value.(*FunctionExpr)); err == nil {
			t.Fatalf("accepted %s", code)
		}
	}
}

func TestFunctionFlowPreservesPromiseAndRecursionGuards(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{"f <- function(x) { if (FALSE) { return(x) } else { return(7) } }; print(f(print(3)))", "promise"},
		{"f <- function(x) { while (x > 0) { x <- x-1 }; return(x) }; print(f(print(3)))", "promise"},
		{"f <- function(x) { if (x > 0) { return(f(x-1)) }; return(0) }; print(f(3))", "recursive"},
	} {
		for _, target := range Languages {
			_, err := TranspileFrom("r", target.ID, tc.code)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("%s: %v, want %s", target.ID, err, tc.want)
			}
		}
	}
}

func TestLoopFlowRoutesBreakAndContinue(t *testing.T) {
	ast, err := parse("f <- function(x) { while (x > 0) { x <- x-1; if (x == 2) { break }; next }; return(x) }")
	if err != nil {
		t.Fatal(err)
	}
	f, err := buildFunctionFlow(ast.List[0].(*AssignStmt).Value.(*FunctionExpr))
	if err != nil {
		t.Fatal(err)
	}
	loop, exit := -1, -1
	for i, s := range f.nodes {
		switch s.(type) {
		case *WhileStmt:
			loop = i
		case *ReturnStmt:
			exit = i
		}
	}
	if !f.stateMachine || loop < 0 || exit < 0 || f.cycles[loop] != 1 {
		t.Fatal("missing loop state")
	}
	for i, s := range f.nodes {
		switch s.(type) {
		case *BreakStmt:
			next, err := flowSuccessor(f.A, i)
			if err != nil || next != exit {
				t.Fatalf("break: %d, %v", next, err)
			}
		case *NextStmt:
			next, err := flowSuccessor(f.A, i)
			if err != nil || next != loop {
				t.Fatalf("continue: %d, %v", next, err)
			}
		}
	}
}

func TestLoopStateRejectsConditionalInitialization(t *testing.T) {
	for _, body := range []string{
		"while(x > 0) { y <- 7; x <- x-1 }; return(y)",
		"while(x > 0) { if(x > 1) { y <- 7 }; x <- y }; return(x)",
	} {
		ast, err := parse("f <- function(x) { " + body + " }")
		if err != nil {
			t.Fatal(err)
		}
		_, err = buildFunctionFlow(ast.List[0].(*AssignStmt).Value.(*FunctionExpr))
		if err == nil || !strings.Contains(err.Error(), "before definite assignment") {
			t.Fatalf("got %v", err)
		}
	}
}
