package backend

import "testing"

func TestSemanticClosureMatricesAreProductive(t *testing.T) {
	m, err := SemanticClosureMatrices()
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Languages) != 13 || len(m.CanonicalForms) == 0 || len(m.Features) == 0 {
		t.Fatalf("unexpected closure axes: languages=%d forms=%d features=%d", len(m.Languages), len(m.CanonicalForms), len(m.Features))
	}
	if m.MFrontend.Rows != 13 || m.MFrontend.Cols != len(m.CanonicalForms) || m.MRel.Rows != len(m.RelationPatterns) {
		t.Fatalf("closure matrix dimensions are not derived from axes")
	}
	if len(m.Composition) == 0 || m.MCompose.Rows != len(m.Composition) {
		t.Fatal("missing evidence-backed composition recipes")
	}
	if got := len(m.CompilerEvidence); got != 55 {
		t.Fatalf("compiler evidence was not embedded: got %d rows", got)
	}
	if got := len(m.CompilerSources); got != 47 {
		t.Fatalf("compiler-source provenance was not embedded: got %d rows", got)
	}
	if got := len(m.FrontendFeatures); got != 216 || len(m.FrontendPhases) != 55 || len(m.FrontendRelations) != 21 {
		t.Fatalf("unexpected compiler matrix pack dimensions: features=%d phases=%d relations=%d", got, len(m.FrontendPhases), len(m.FrontendRelations))
	}
	if len(m.Relations) <= len(projectedUASTRelations) {
		t.Fatal("compiler relation evidence did not enter the closure axis")
	}
}

func TestSemanticClosureIsUsedByCanonicalPath(t *testing.T) {
	p, err := LowerMatrixLanguage("python", "x = 1\nprint(x)\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.UniversalAST == nil || p.UniversalAST.Extensions == nil {
		t.Fatal("canonical path did not carry semantic closure metadata")
	}
	if _, ok := p.UniversalAST.Extensions["semantic_closure"]; !ok {
		t.Fatal("semantic closure was not applied by the canonical path")
	}
	evidence, ok := p.UniversalAST.Extensions["frontend_compiler_evidence"].(map[string]any)
	if !ok || len(evidence) == 0 {
		t.Fatal("canonical path did not retain compiler evidence provenance")
	}
	trace := BuildSemanticTrace(true, p.UniversalAST, SemanticTraceRoute{RouteType: "DIRECT"})
	if len(trace.FrontendEvidence) == 0 {
		t.Fatal("semantic trace did not preserve canonical frontend evidence")
	}
}
