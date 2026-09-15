package backend

import (
	"strings"
	"testing"
)

func TestUASTPythonFunctionFallthroughReturnsNone(t *testing.T) {
	makeGraph := func(language string) *uastExecutionGraph {
		return &uastExecutionGraph{
			document: &UniversalASTDocument{LanguageProfile: language},
			nodes: map[int]*UniversalASTNode{
				1: {ID: 1, StructuralKind: "ClosureExpr"},
				2: {ID: 2, StructuralKind: "Scope"},
				3: {ID: 3, StructuralKind: "IfStmt"},
				4: {ID: 4, StructuralKind: "Scope"},
				5: {ID: 5, StructuralKind: "ReturnStmt"},
				6: {ID: 6, StructuralKind: "AssignStmt"},
				7: {ID: 7, StructuralKind: "LiteralExpr"},
			},
			common: map[int]universalDecodedCommon{
				1: {ID: 1, Kind: "function"},
				2: {ID: 2, Kind: "block"},
				3: {ID: 3, Kind: "if"},
				4: {ID: 4, Kind: "block"},
				5: {ID: 5, Kind: "return"},
				6: {ID: 6, Kind: "assign"},
				7: {ID: 7, Kind: "literal"},
			},
			children: map[int]map[string][]universalChild{
				1: {"body": {{ID: 2}}},
				2: {"statement": {{ID: 6}, {ID: 3}}},
				4: {"statement": {{ID: 5}}},
				6: {"value": {{ID: 7}}},
			},
			relations: map[int]map[string][]UniversalASTReference{
				3: {"control.true": {{Domain: "node", ID: "4"}}},
			},
		}
	}

	pythonFlow, err := buildUASTFunctionFlow(makeGraph("python"), 1)
	if err != nil {
		t.Fatalf("Python fallthrough should be represented as implicit None: %v", err)
	}
	if !pythonFlow.implicitVoidReturn {
		t.Fatal("Python function flow did not retain implicit-None semantics")
	}

	csFlow, err := buildUASTFunctionFlow(makeGraph("csharp"), 1)
	if err == nil || !strings.Contains(err.Error(), "path without explicit return") {
		t.Fatalf("C# non-void fallthrough must remain rejected, flow=%v err=%v", csFlow, err)
	}
}
