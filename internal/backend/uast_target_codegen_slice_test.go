// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"strings"
	"testing"
)

func sliceProjectionGraph(withStep bool) *uastExecutionGraph {
	nodes := map[int]*UniversalASTNode{
		1: {ID: 1, StructuralKind: "SliceExpr"},
		2: {ID: 2, StructuralKind: "SymbolRef"},
		3: {ID: 3, StructuralKind: "LiteralExpr"},
		4: {ID: 4, StructuralKind: "LiteralExpr"},
	}
	common := map[int]universalDecodedCommon{
		1: {ID: 1, Kind: "slice"},
		2: {ID: 2, Kind: "identifier", Name: "values"},
		3: {ID: 3, Kind: "literal", Operation: universalOperationRecord{LiteralKind: "number", Text: "1"}},
		4: {ID: 4, Kind: "literal", Operation: universalOperationRecord{LiteralKind: "number", Text: "5"}},
	}
	children := map[int]map[string][]universalChild{
		1: {"value": {{ID: 2}}, "argument": {{ID: 3, Meta: universalChildRecord{Role: "argument", Ordinal: 0}}, {ID: 4, Meta: universalChildRecord{Role: "argument", Ordinal: 1}}}},
	}
	if withStep {
		nodes[5] = &UniversalASTNode{ID: 5, StructuralKind: "LiteralExpr"}
		common[5] = universalDecodedCommon{ID: 5, Kind: "literal", Operation: universalOperationRecord{LiteralKind: "number", Text: "2"}}
		children[1]["step"] = []universalChild{{ID: 5, Meta: universalChildRecord{Role: "step"}}}
	}
	return &uastExecutionGraph{nodes: nodes, common: common, children: children}
}

func TestUASTSliceProjectionUsesCSharpSliceAdapterWithoutInventingStep(t *testing.T) {
	g := &targetGen{target: "csharp", nativeDirect: true, usedNames: map[string]bool{}, helperSources: map[string]string{}, cValues: map[string]bool{}}
	got, err := g.uastSliceExpression(sliceProjectionGraph(false), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "R2.Slice(values, 1, 5, true, true, null)" {
		t.Fatalf("ordinary slice adapter did not preserve bounds: got %q", got)
	}
	if strings.Contains(got, "stepped-slice") {
		t.Fatalf("ordinary slice incorrectly acquired an implicit step: %q", got)
	}
}

func TestUASTSliceProjectionPreservesExplicitStep(t *testing.T) {
	g := &targetGen{target: "python", nativeDirect: true, usedNames: map[string]bool{}, helperSources: map[string]string{}, cValues: map[string]bool{}}
	got, err := g.uastSliceExpression(sliceProjectionGraph(true), 1)
	if err != nil {
		t.Fatal(err)
	}
	if got != "values[1:5:2]" {
		t.Fatalf("explicit slice roles were not preserved: got %q", got)
	}
}

func TestUASTPointerProjectionUsesCSharpReferenceCell(t *testing.T) {
	graph := &uastExecutionGraph{
		nodes: map[int]*UniversalASTNode{
			1: {ID: 1, StructuralKind: "AddressOf"},
			2: {ID: 2, StructuralKind: "SymbolRef"},
			3: {ID: 3, StructuralKind: "Deref"},
		},
		common: map[int]universalDecodedCommon{
			1: {ID: 1, Kind: "address"},
			2: {ID: 2, Kind: "identifier", Name: "x"},
			3: {ID: 3, Kind: "deref"},
		},
		children: map[int]map[string][]universalChild{
			1: {"value": {{ID: 2}}},
			3: {"value": {{ID: 1}}},
		},
	}
	g := &targetGen{target: "csharp", nativeDirect: true, usedNames: map[string]bool{}, helperSources: map[string]string{}, cValues: map[string]bool{}}
	address, err := g.uastPointerExpression(graph, 1, true)
	if err != nil || address != "R2.Address(x)" {
		t.Fatalf("C# address projection = %q, %v", address, err)
	}
	deref, err := g.uastPointerExpression(graph, 3, false)
	if err != nil || deref != "R2.Deref(R2.Address(x))" {
		t.Fatalf("C# dereference projection = %q, %v", deref, err)
	}
}

func TestCSharpProjectionEmitsTypedUASTClosureAsTargetLambda(t *testing.T) {
	program, err := LowerNativeGo("sort-lambda.go", `package main
import "sort"
func main() {
	values := []int{2, 1}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
}`)
	if err != nil {
		t.Fatal(err)
	}
	source, err := (UniversalTargetProjector{}).EmitDirect(program.UniversalAST, "csharp")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(source, "(i, j) => {") {
		t.Fatalf("C# projection did not emit the anonymous function as a lambda:\n%s", source)
	}
}
