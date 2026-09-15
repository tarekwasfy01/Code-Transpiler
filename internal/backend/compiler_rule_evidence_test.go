package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeCompilerEvidenceHasSourceProvenanceAndNoNameEquivalence(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	report, err := ExtractNativeCompilerEvidence(filepath.Clean(filepath.Join(wd, "..", "..")))
	if err != nil {
		t.Fatal(err)
	}
	if report.Units == 0 || report.Functions == 0 || len(report.Rules) == 0 || len(report.CallEdges) == 0 || len(report.EncodableOps) == 0 {
		t.Fatalf("incomplete source evidence: units=%d functions=%d rules=%d edges=%d encodable=%d", report.Units, report.Functions, len(report.Rules), len(report.CallEdges), len(report.EncodableOps))
	}
	if len(report.Equivalences) != 0 {
		t.Fatalf("source names alone must not be promoted to semantic equivalence: %d", len(report.Equivalences))
	}
	seenSelection := false
	for _, rule := range report.Rules {
		if rule.Relation == "" || rule.Confidence == "" || rule.SemanticIdentity == "" {
			t.Fatalf("rule lacks evidence classification: %+v", rule)
		}
		for _, loc := range rule.Locations {
			if loc.Repository == "" || loc.File == "" || loc.Symbol == "" || loc.StartLine < 1 || loc.EndLine < loc.StartLine || len(loc.SHA256) != 64 {
				t.Fatalf("incomplete provenance on %s: %+v", rule.ID, loc)
			}
			if strings.HasPrefix(rule.SemanticIdentity, "uast.kind:") && strings.Contains(loc.Symbol, "expression") {
				seenSelection = true
			}
		}
	}
	if !seenSelection {
		t.Fatal("expected a UAST-kind to native-selection evidence chain")
	}
}

func TestNativeCompilerEvidenceWriterEmitsStableJsonLines(t *testing.T) {
	report := &NativeCompilerEvidenceReport{SchemaVersion: CompilerRuleEvidenceSchema, Compiler: "fixture", Version: "local", Rules: []CompilerEvidence{{ID: "r1", Compiler: "fixture", Version: "local", Stage: CompilerStageInstructionSelect, InputPattern: "uast.kind:literal", Relation: "lowering_component", Confidence: EvidenceDirect}}}
	dir := t.TempDir()
	if err := WriteNativeCompilerEvidence(report, dir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "translation-rules.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("got %d jsonl records", len(lines))
	}
	var got CompilerEvidence
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != "r1" {
		t.Fatalf("unexpected record %#v", got)
	}
	for _, name := range []string{"compiler-evidence-index.json", "semantic-equivalences.jsonl", "semantic-gaps.jsonl", "compiler-graph.jsonl", "evidence-summary.json", "extraction-report.md"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("missing %s: %v", name, err)
		}
	}
}
