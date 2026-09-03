package backend

import (
	"encoding/json"
	"testing"
)

// universalBackendFixture is deliberately constructed before these tests enter
// the backend. Every assertion below starts with a canonical UAST document.
func universalBackendFixture(t *testing.T) *UniversalASTDocument {
	t.Helper()
	p, err := LowerMatrixLanguage("r", "x <- 1\ny <- x + 2\nif (y > 1) { print(y) } else { print(0) }\n")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(p.UniversalAST)
	if err != nil {
		t.Fatal(err)
	}
	var u UniversalASTDocument
	if err = json.Unmarshal(data, &u); err != nil {
		t.Fatal(err)
	}
	return &u
}

func emitCanonicalUAST(target string, u *UniversalASTDocument) (string, error) {
	if target == "r" {
		return universalRSource(u, true)
	}
	graph, err := newUASTExecutionGraph(u)
	if err != nil {
		return "", err
	}
	if err = validateUASTTargetCapabilities(u, target); err != nil {
		return "", err
	}
	return generateTargetFromUniversal(u.Evaluation, target, graph)
}

func TestUniversalBackendUASTOnlyTargetMatrix(t *testing.T) {
	u := universalBackendFixture(t)
	for _, backend := range Backends() {
		backend := backend
		t.Run(backend.ID, func(t *testing.T) {
			out, err := emitCanonicalUAST(backend.ID, u)
			if err != nil {
				t.Fatal(err)
			}
			if out == "" {
				t.Fatal("UAST target emitter returned empty source")
			}
		})
	}
}

func TestUniversalBackendIgnoresSourceProvenance(t *testing.T) {
	u := universalBackendFixture(t)
	clone := func(source string) *UniversalASTDocument {
		data, err := json.Marshal(u)
		if err != nil {
			t.Fatal(err)
		}
		var out UniversalASTDocument
		if err = json.Unmarshal(data, &out); err != nil {
			t.Fatal(err)
		}
		out.Origin.SourceLanguage = source
		if out.Metadata == nil {
			out.Metadata = map[string]string{}
		}
		out.Metadata["source_provenance"] = source
		return &out
	}
	for _, target := range []string{"r", "go", "python", "rust"} {
		fromR, err := emitCanonicalUAST(target, clone("r"))
		if err != nil {
			t.Fatal(err)
		}
		fromSwift, err := emitCanonicalUAST(target, clone("swift"))
		if err != nil {
			t.Fatal(err)
		}
		if fromR != fromSwift {
			t.Fatalf("target %s changed output for source provenance only", target)
		}
	}
}

func TestUniversalBackendCrossLanguageTargetMatrix(t *testing.T) {
	const corpus = "x = 1\ny = x + 2\nprint(y)\n"
	sources, targets := Frontends(), Backends()
	passed := 0
	for _, source := range sources {
		p, err := LowerMatrixLanguage(source.ID, corpus)
		if err != nil {
			t.Fatalf("frontend %s: %v", source.ID, err)
		}
		for _, target := range targets {
			out, err := emitCanonicalUAST(target.ID, p.UniversalAST)
			if err != nil {
				t.Fatalf("%s -> %s: %v", source.ID, target.ID, err)
			}
			if out == "" {
				t.Fatalf("%s -> %s emitted empty source", source.ID, target.ID)
			}
			passed++
		}
	}
	if want := len(sources) * len(targets); passed != want {
		t.Fatalf("cross-language matrix passed=%d want=%d", passed, want)
	}
}

func TestUniversalBackendAuditMatrix(t *testing.T) {
	report, err := UniversalBackendAuditReport()
	if err != nil {
		t.Fatal(err)
	}
	if !report.UniversalCoreUASTOnly || report.ProductiveLegacyASTDependency || report.SourceLanguageSemanticBranches != 0 || report.BackendSemanticBoundaries != 0 {
		t.Fatalf("unexpected backend audit state: %+v", report)
	}
	if len(report.Targets) != len(Backends()) || len(report.Compatibility.Sources) != len(Frontends()) || len(report.Compatibility.Targets) != len(Backends()) {
		t.Fatal("backend audit registry dimensions differ from registered languages")
	}
	for row := range report.Compatibility.Sources {
		for col := range report.Compatibility.Targets {
			if report.Compatibility.Full.At(row, col) == 0 || report.Compatibility.Partial.At(row, col) != 0 || report.Compatibility.Unsupported.At(row, col) != 0 {
				t.Fatalf("common baseline compatibility %s -> %s is not full", report.Compatibility.Sources[row], report.Compatibility.Targets[col])
			}
		}
	}
}
