// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnosticContextStrictAndSaturatingModes(t *testing.T) {
	f := SemanticFailure{Stage: "SOURCE_TO_UAST", Category: "EXPRESSION", NodeKind: "CallExpr", SemanticRole: "callee", RecoveryKind: RecoveryLocal, Diagnostic: "unsupported"}
	strict := NewDiagnosticContext(DiagnosticStrict)
	if err := strict.Record(f); err == nil {
		t.Fatal("strict mode must return an error")
	}
	if len(strict.Failures) != 0 {
		t.Fatal("strict mode must not retain a diagnostic hole")
	}
	saturating := NewDiagnosticContext(DiagnosticSaturate)
	if err := saturating.Record(f); err != nil {
		t.Fatal(err)
	}
	if len(saturating.Failures) != 1 || saturating.Failures[0].NormalizedSignature == "" {
		t.Fatalf("saturating failure was not normalized: %+v", saturating.Failures)
	}
	if len(saturating.Holes) != 1 || saturating.Holes[0].FailureID == "" {
		t.Fatalf("saturating failure did not create a transient hole: %+v", saturating.Holes)
	}
}

func TestDiagnosticHolesNeverEnterSemanticArtifacts(t *testing.T) {
	// An invalid structured fact is deliberately recoverable only in the
	// diagnostic plane.  The canonical builder and every downstream artifact
	// must see either a valid node or no node at all.
	facts := FrontendSemanticFacts{Nodes: []UniversalASTNode{{ID: 7, StructuralKind: "", FieldMask: nil}}}
	saturating := NewDiagnosticContext(DiagnosticSaturate)
	if successful, holes := SaturateFrontendFacts(facts, saturating); successful != 0 || holes != 1 {
		t.Fatalf("unexpected saturation result: successful=%d holes=%d", successful, holes)
	}
	if len(saturating.Holes) != 1 || saturating.Holes[0].FailureID == "" {
		t.Fatalf("missing transient diagnostic hole: %+v", saturating.Holes)
	}
	if _, err := ValidateFrontendFacts(facts); err == nil {
		t.Fatal("strict frontend validation accepted a diagnostic hole")
	}

	// A valid canonical program is built independently; the transient failure
	// context is never passed to it.  Check all serialized semantic planes for
	// hole identifiers and diagnostic payloads.
	p := NewSemanticProgram(&BlockStmt{}, "eager_left_to_right")
	semanticJSON, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	uastJSON, err := p.MarshalUniversalASTJSON()
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{"semantic": semanticJSON, "uast": uastJSON} {
		if bytes.Contains(data, []byte(saturating.Holes[0].FailureID)) || bytes.Contains(data, []byte("DiagnosticHole")) {
			t.Fatalf("%s artifact leaked a diagnostic hole: %s", name, data)
		}
		var decoded any
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("%s artifact is not valid JSON: %v", name, err)
		}
	}
	if _, _, err := ApplyPrimitiveClosure(p.UniversalAST, "native-x86_64-windows"); err != nil {
		t.Fatalf("primitive closure rejected valid hole-free program: %v", err)
	}
}

func TestFailureReducerPreservesNormalizedSignature(t *testing.T) {
	root := &ReductionNode{Kind: "program", Children: []*ReductionNode{{Kind: "bad"}, {Kind: "independent"}}}
	result, err := ReduceFailure(root, "EXPRESSION|BAD", func(candidate *ReductionNode) (string, error) {
		for _, child := range candidate.Children {
			if child.Kind == "bad" {
				return "EXPRESSION|BAD", nil
			}
		}
		return "OTHER", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Root.Children) != 1 || result.Root.Children[0].Kind != "bad" {
		t.Fatalf("reducer did not remove irrelevant structure: %+v", result.Root)
	}
	dir := t.TempDir()
	if err := WriteReductionArtifacts(dir, map[string]ReductionResult{"EXPRESSION|BAD": result}, map[string]SemanticFailure{"EXPRESSION|BAD": {FailureFamily: "EXPRESSION|BAD"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "reduced", "expression_bad", "minimal_failure.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRewriteClosureBoundsCyclesAndPreservesRuleContract(t *testing.T) {
	rules := []SemanticRewriteRule{
		{ID: "a-to-b", From: "A", To: []RewriteState{{Operation: "B"}}, Preserve: func(RewriteState, []RewriteState) bool { return true }},
		{ID: "b-to-c", From: "B", To: []RewriteState{{Operation: "C"}}, Preserve: func(RewriteState, []RewriteState) bool { return true }},
		{ID: "c-to-a", From: "C", To: []RewriteState{{Operation: "A"}}, Preserve: func(RewriteState, []RewriteState) bool { return true }},
	}
	proof, ok := FindSemanticRewriteClosure(RewriteState{Operation: "A"}, rules, func(s RewriteState) bool { return s.Operation == "C" }, 3, 16)
	if !ok || !proof.Valid || proof.Depth != 2 {
		t.Fatalf("unexpected closure proof: %+v, ok=%v", proof, ok)
	}
	if _, ok := FindSemanticRewriteClosure(RewriteState{Operation: "A"}, rules, func(s RewriteState) bool { return s.Operation == "Z" }, 2, 16); ok {
		t.Fatal("bounded search found an impossible closure")
	}
}

func TestFailureSaturationReportContainsStructuredMatrices(t *testing.T) {
	dir := t.TempDir()
	ctx := NewDiagnosticContext(DiagnosticSaturate)
	if err := ctx.Record(SemanticFailure{Stage: "SOURCE_TO_UAST", Category: "EXPRESSION", SourceFile: "pkg/a.go", SourceStart: 4, FailureFamily: "missing_rhs", RecoveryKind: RecoveryLocal}); err != nil {
		t.Fatal(err)
	}
	if err := ctx.Record(SemanticFailure{Stage: "TARGET_LEGALIZATION", Category: "TARGET", SourceFile: "pkg/a.go", SourceStart: 8, FailureFamily: "missing_renderer", RecoveryKind: RecoveryStatement}); err != nil {
		t.Fatal(err)
	}
	if err := WriteFailureSaturationReport(dir, ctx); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"00_pipeline_abort_points.csv", "01_failure_reduction_matrix.csv", "02_failure_root_cause_matrix.csv", "03_latent_failure_matrix.csv", "04_failure_dependency_graph.csv", "07_semantic_frontier.csv", "08_semantic_coverage_by_file.csv", "09_semantic_coverage_by_package.csv", "10_semantic_coverage_by_stage.csv", "11_failure_instances.csv", "12_failure_families.csv", "21_non_derivability_matrix.csv", "summary.json", "summary.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing report %s: %v", name, err)
		}
	}
}
