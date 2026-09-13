// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"os"
	"testing"
)

func TestRoslynExecutableProjectionMinimalAdd(t *testing.T) {
	wire, err := runRoslynBoundAdapter("minimal.cs", `class Add { static int AddTwo(int a, int b) { return a + b; } static int Main() { return AddTwo(20, 22); } }`)
	if err != nil {
		t.Fatal(err)
	}
	uast, err := projectRoslynExecutableFacts(wire.Nodes)
	if err != nil {
		t.Fatalf("projection: %v", err)
	}
	counts := map[string]int{}
	for _, n := range uast.Nodes {
		counts[n.StructuralKind]++
	}
	if counts["ClosureExpr"] != 2 || counts["ParameterDecl"] != 2 || counts["SymbolRef"] < 2 || counts["CallExpr"] != 1 || counts["ReturnStmt"] != 2 || counts["LiteralExpr"] < 2 || counts["OperationExpr"] < 1 {
		t.Fatalf("unexpected canonical projection counts: %#v", counts)
	}
	for _, n := range uast.Nodes {
		if n.StructuralKind != "ParameterDecl" {
			continue
		}
		var name string
		if err := json.Unmarshal(n.Fields["name"], &name); err != nil || (name != "a" && name != "b") {
			t.Fatalf("parameter declaration lost Roslyn binding: node=%#v name=%q err=%v", n, name, err)
		}
	}
}

func TestRoslynBoundToSemanticUsesExecutableProjection(t *testing.T) {
	p, err := RoslynBoundToSemantic("minimal.cs", `class Add { static int AddTwo(int a, int b) { return a + b; } static int Main() { return AddTwo(20, 22); } }`)
	if err != nil {
		t.Fatal(err)
	}
	if p.UniversalAST == nil || len(p.UniversalAST.Nodes) < 10 {
		t.Fatalf("bound adapter retained non-executable UAST: nodes=%d", len(p.UniversalAST.Nodes))
	}
	if rows, ok := p.UniversalAST.Extensions["external_evidence.go2cs.v1"].([]map[string]any); !ok || len(rows) < 10 {
		t.Fatalf("go2cs contract evidence was not attached to canonical UAST: %#v", p.UniversalAST.Extensions["external_evidence.go2cs.v1"])
	}
}

func TestRoslynBoundToSemanticSubsetSource(t *testing.T) {
	source, err := os.ReadFile(csharpRepoRoot() + "/compiler-migration/csc/selfhost/subset/CompilerSubset.cs")
	if err != nil {
		t.Fatal(err)
	}
	p, err := RoslynBoundToSemantic("CompilerSubset.cs", string(source))
	if err != nil {
		t.Fatal(err)
	}
	if p.UniversalAST == nil || len(p.UniversalAST.Nodes) <= 4 {
		t.Fatalf("subset projection is structural-only: nodes=%d", len(p.UniversalAST.Nodes))
	}
}
