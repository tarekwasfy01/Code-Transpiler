package backend

import (
	"strings"
	"testing"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

func resolutionMatrix(t *testing.T, rows [][]float64) matrixir.Matrix {
	t.Helper()
	m, err := matrixir.MatrixFromRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func resolvedCallProgram(t *testing.T) *SemanticProgram {
	t.Helper()
	selected := 1
	fn := func(value string) *FunctionExpr {
		return &FunctionExpr{Body: &BlockStmt{List: []Stmt{&ReturnStmt{X: &LiteralExpr{Kind: "number", Text: value}}}}}
	}
	resolved := &CallExpr{Fun: &IdentExpr{Name: "overload"}, Args: []Arg{{Value: &LiteralExpr{Kind: "number", Text: "7"}}}, Resolution: &SemanticCallResolution{
		Candidates: []SemanticCallCandidate{
			{Name: "overload(integer)", Declaration: "overload_integer", Type: SemanticType{Kind: "function", Result: &SemanticType{Kind: "integer", Bits: 32}}},
			{Name: "overload(float)", Declaration: "overload_float", Type: SemanticType{Kind: "function", Result: &SemanticType{Kind: "float", Bits: 64, IEEE754: true}}},
		},
		Obligations:    []string{"arity", "visible"},
		Required:       resolutionMatrix(t, [][]float64{{1, 1}, {1, 1}}),
		Satisfied:      resolutionMatrix(t, [][]float64{{1, 1}, {1, 1}}),
		ConversionCost: resolutionMatrix(t, [][]float64{{2}, {0}}),
		Priority:       []float64{0, 0}, Selected: &selected,
	}}
	body := &BlockStmt{List: []Stmt{
		&AssignStmt{Name: "overload_integer", Op: "<-", Value: fn("1")},
		&AssignStmt{Name: "overload_float", Op: "<-", Value: fn("2")},
		&ExprStmt{X: &CallExpr{Fun: &IdentExpr{Name: "print"}, Args: []Arg{{Value: resolved}}}},
	}}
	return NewSemanticProgram(body, "eager_left_to_right")
}

func TestSemanticCallResolutionMatrixRoundTripAndExecution(t *testing.T) {
	p := resolvedCallProgram(t)
	data, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseSemanticJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	out, err := RunSemantic(q)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "2") {
		t.Fatalf("selected declaration did not execute: %q", out)
	}
	for _, target := range Backends() {
		if _, err := EmitSemantic(target.ID, q); err == nil || !strings.Contains(err.Error(), ExactCallResolutionCapability) {
			t.Fatalf("%s failed to capability-reject call resolution: %v", target.ID, err)
		}
	}
}

func TestSemanticCallResolutionRejectsWrongSelectionAndAmbiguity(t *testing.T) {
	p := resolvedCallProgram(t)
	call := p.Body.List[2].(*ExprStmt).X.(*CallExpr).Args[0].Value.(*CallExpr)
	wrong := 0
	call.Resolution.Selected = &wrong
	if _, err := p.Document(); err == nil || !strings.Contains(err.Error(), "does not match matrix result") {
		t.Fatalf("wrong selected candidate accepted: %v", err)
	}
	p = resolvedCallProgram(t)
	call = p.Body.List[2].(*ExprStmt).X.(*CallExpr).Args[0].Value.(*CallExpr)
	call.Resolution.ConversionCost = resolutionMatrix(t, [][]float64{{0}, {0}})
	if _, err := p.Document(); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous minimum accepted: %v", err)
	}
}

func TestSemanticCallResolutionRejectsMalformedPlanes(t *testing.T) {
	p := resolvedCallProgram(t)
	call := p.Body.List[2].(*ExprStmt).X.(*CallExpr).Args[0].Value.(*CallExpr)
	call.Resolution.Required = resolutionMatrix(t, [][]float64{{1}, {1}})
	if _, err := p.Document(); err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Fatalf("malformed resolution matrix accepted: %v", err)
	}
	p = resolvedCallProgram(t)
	call = p.Body.List[2].(*ExprStmt).X.(*CallExpr).Args[0].Value.(*CallExpr)
	call.Resolution.Required.Data = call.Resolution.Required.Data[:1]
	if _, err := p.Document(); err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Fatalf("truncated matrix storage accepted: %v", err)
	}
}
