package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompilerRepositoryEvidenceKeepsCompilerNeutralProvenance(t *testing.T) {
	root := t.TempDir()
	roslyn := filepath.Join(root, "src", "Compilers", "CSharp", "Portable", "Lowering", "Rewrite.cs")
	if err := os.MkdirAll(filepath.Dir(roslyn), 0755); err != nil {
		t.Fatal(err)
	}
	cs := `class Rewriter {
  object Rewrite(BoundNode n) {
    // case BoundKind.Fake: return MustNotAppear();
    switch (n.Kind) {
      case BoundKind.BinaryOperator: return MakeBinary(n);
      case BoundKind.Literal: return MakeLiteral(n);
      default: return n;
    }
  }
}`
	if err := os.WriteFile(roslyn, []byte(cs), 0644); err != nil {
		t.Fatal(err)
	}
	helper := filepath.Join(root, "src", "Compilers", "CSharp", "Portable", "Lowering", "Helpers.cs")
	if err := os.WriteFile(helper, []byte(`class Helpers {
  object MakeBinary(BoundNode n) { return new BoundBinaryOperator(); }
  object MakeLiteral(BoundNode n) { return new BoundLiteral(); }
}`), 0644); err != nil {
		t.Fatal(err)
	}
	td := filepath.Join(root, "llvm", "lib", "Target", "X86", "X86Patterns.td")
	if err := os.MkdirAll(filepath.Dir(td), 0755); err != nil {
		t.Fatal(err)
	}
	tablegen := `def ADD32rr : Instruction { let Pattern = (add GR32:$lhs, GR32:$rhs); }
def : Pat<(add GR32:$a, GR32:$b), (ADD32rr GR32:$a, GR32:$b)>;`
	if err := os.WriteFile(td, []byte(tablegen), 0644); err != nil {
		t.Fatal(err)
	}
	commit := "0123456789abcdef"
	r, err := ExtractCompilerRepositoryEvidence(CompilerRepositorySpec{Compiler: "fixture Roslyn", Repository: "https://example.invalid/roslyn", Root: root, Commit: commit, License: "MIT", Include: []string{"src/Compilers/CSharp/Portable/Lowering"}, Extensions: []string{".cs"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Units) != 2 || len(r.Rules) != 2 {
		t.Fatalf("units=%d rules=%d", len(r.Units), len(r.Rules))
	}
	if len(r.Equivalences) != 0 {
		t.Fatalf("name matching produced semantic equivalence: %d", len(r.Equivalences))
	}
	for _, rule := range r.Rules {
		if len(rule.Locations) == 0 || rule.Locations[0].SHA256 == "" || rule.Locations[0].Commit != commit || rule.Locations[0].StartLine < 1 {
			t.Fatalf("missing source provenance: %+v", rule)
		}
	}
	found := false
	for _, rule := range r.Rules {
		if strings.Contains(rule.SemanticIdentity, "BoundKind.BinaryOperator") && strings.Contains(strings.Join(rule.OutputPattern, " "), "MakeBinary") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected dispatch-to-call evidence, got %+v", r.Rules)
	}
	crossFileCall := false
	for _, edge := range r.Edges {
		if edge.Relation == "calls" && strings.Contains(strings.ToLower(edge.To), "helpers.cs") {
			crossFileCall = true
		}
	}
	if !crossFileCall {
		t.Fatalf("expected repository-wide call edge to helper node, got %+v", r.Edges)
	}

	llvm, err := ExtractCompilerRepositoryEvidence(CompilerRepositorySpec{Compiler: "fixture LLVM", Repository: "https://example.invalid/llvm", Root: root, Commit: commit, License: "Apache-2.0 WITH LLVM-exception", TargetArchitecture: "x86_64", Include: []string{"llvm/lib/Target/X86/X86Patterns.td"}, Extensions: []string{".td"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(llvm.Rules) != 1 || len(llvm.Nodes) == 0 {
		t.Fatalf("TableGen extraction failed: nodes=%d rules=%d", len(llvm.Nodes), len(llvm.Rules))
	}
	if !strings.Contains(strings.Join(llvm.Rules[0].OutputPattern, " "), "ADD32rr") {
		t.Fatalf("TableGen pattern source was not retained: %+v", llvm.Rules[0])
	}
}

func TestCompilerRepositoryWriterEmitsGraphAndRuleArtifacts(t *testing.T) {
	dir := t.TempDir()
	r := &CompilerRepositoryReport{SchemaVersion: RepositoryEvidenceSchema, Compiler: "fixture", Repository: "repo", Commit: "deadbeef", Version: "deadbeef", License: "MIT", Rules: []CompilerEvidence{{ID: "r", Compiler: "fixture", Version: "deadbeef", Relation: "dispatch_to_implementation_component", Confidence: EvidenceDirect}}, Summary: map[string]int{}}
	if err := WriteCompilerRepositoryEvidence(r, dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"compiler-evidence-index.json", "translation-rules.jsonl", "semantic-equivalences.jsonl", "semantic-gaps.jsonl", "compiler-graph.jsonl", "source-nodes.jsonl", "evidence-summary.json", "extraction-report.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}

func TestCompilerMatrixKeepsCompilerNamespacesSeparateUntilExplicitCrosswalk(t *testing.T) {
	locA := CompilerEvidenceLocation{Repository: "roslyn", Commit: "aa", File: "a.cs", SHA256: "sha-a", Symbol: "Rewrite", StartLine: 2, EndLine: 8}
	locB := CompilerEvidenceLocation{Repository: "llvm", Commit: "bb", File: "b.td", SHA256: "sha-b", Symbol: "ADD32rr", StartLine: 4, EndLine: 4}
	a := &CompilerRepositoryReport{Compiler: "Roslyn", Repository: "roslyn", Commit: "aa", Rules: []CompilerEvidence{{ID: "a", Compiler: "Roslyn", SemanticIdentity: "source.dispatch:Add", Relation: "dispatch_to_implementation_component", Confidence: EvidenceDirect, Locations: []CompilerEvidenceLocation{locA}}}}
	b := &CompilerRepositoryReport{Compiler: "LLVM", Repository: "llvm", Commit: "bb", Rules: []CompilerEvidence{{ID: "b", Compiler: "LLVM", SemanticIdentity: "tablegen.declaration:ADD32rr", Relation: "declarative_target_pattern", Confidence: EvidenceDirect, Locations: []CompilerEvidenceLocation{locB}}}}
	m := MergeCompilerEvidenceMatrix([]*CompilerRepositoryReport{a, b}, nil)
	if len(m.Equivalences) != 0 {
		t.Fatalf("no explicit crosswalk was provided, got %d equivalences", len(m.Equivalences))
	}
	if len(m.PrimitiveCandidates) != 2 {
		t.Fatalf("expected separate unpromoted candidates, got %d", len(m.PrimitiveCandidates))
	}
	for _, c := range m.PrimitiveCandidates {
		if c.CanonicalIdentity != "" || c.Status != "candidate" {
			t.Fatalf("candidate was promoted: %+v", c)
		}
	}
}

func TestCompilerCrosswalkRequiresEquivalentNormalizedPreconditions(t *testing.T) {
	locA := CompilerEvidenceLocation{Repository: "roslyn", Commit: "aa", File: "a.cs", SHA256: "sha-a", Symbol: "Add", StartLine: 2, EndLine: 8}
	locB := CompilerEvidenceLocation{Repository: "llvm", Commit: "bb", File: "b.cpp", SHA256: "sha-b", Symbol: "lowerAdd", StartLine: 4, EndLine: 9}
	a := CompilerEvidence{ID: "roslyn.add", Compiler: "Roslyn", Conditions: []string{"bound_type=i32", "checked=false"}, Locations: []CompilerEvidenceLocation{locA}}
	b := CompilerEvidence{ID: "llvm.add", Compiler: "LLVM", Conditions: []string{"i32", "nsw=false"}, Locations: []CompilerEvidenceLocation{locB}}
	m := &CompilerEvidenceMatrix{Rules: []CompilerEvidence{a, b}}
	bad := CompilerSemanticCrosswalk{CanonicalIdentity: "integer.add.s32", Relation: "exact_equivalent", EvidenceIDs: []string{a.ID, b.ID}, EvidenceContracts: map[string][]string{a.ID: {"width=32", "overflow=wrap"}, b.ID: {"width=32", "overflow=poison"}}, Confidence: EvidenceDerived}
	if err := ApplyExplicitCompilerCrosswalks(m, []CompilerSemanticCrosswalk{bad}); err == nil {
		t.Fatal("crosswalk with conflicting overflow semantics was accepted")
	}
	good := bad
	good.EvidenceContracts = map[string][]string{a.ID: {"width=32", "overflow=wrap"}, b.ID: {"overflow=wrap", "width=32"}}
	if err := ApplyExplicitCompilerCrosswalks(m, []CompilerSemanticCrosswalk{good}); err != nil {
		t.Fatalf("compatible explicit crosswalk rejected: %v", err)
	}
	if len(m.Equivalences) != 1 || m.Equivalences[0].Concept != "integer.add.s32" {
		t.Fatalf("expected one explicit equivalence, got %+v", m.Equivalences)
	}
}

func TestNativeCoverageAuditDoesNotInferCanonicalIdentity(t *testing.T) {
	m := &CompilerEvidenceMatrix{PrimitiveCandidates: []CompilerPrimitiveCandidate{{ID: "candidate:llvm:add", Reason: "source evidence exists"}}}
	a := BuildNativeCoverageAudit(m)
	if len(a.Rows) != 1 || a.Rows[0].Status != "insufficient_evidence" {
		t.Fatalf("audit=%#v", a)
	}
}
