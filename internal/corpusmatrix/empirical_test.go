package corpusmatrix

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
)

func TestEmpiricalProofBooleanPromotionAndContradiction(t *testing.T) {
	out := t.TempDir()
	base := func(hash, repo string, caps ...string) FileResult {
		return FileResult{State: ResultFull, UsedFeatures: []string{"feature.assignment"}, Capabilities: caps,
			Record: CorpusRecord{LanguageID: "go", NormalizedSourceHash: hash, PackageOrRepo: repo, CorpusSource: "MLCPD"}}
	}
	cfg := Config{MinDistinctHashes: 2, MinDistinctRepositories: 0, MinDistinctCorpusSources: 1}
	proofs, contradictions, err := writeEmpiricalProof(out, []FileResult{
		base("h1", "repo-a", "UASF_0001"), base("h2", "repo-b", "UASF_0001"),
	}, cfg)
	if err != nil || proofs != 1 || contradictions != 0 {
		t.Fatalf("stable observations: proofs=%d contradictions=%d err=%v", proofs, contradictions, err)
	}
	f, err := os.Open(filepath.Join(out, "empirical_proof_matrix.csv"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(f).ReadAll()
	_ = f.Close()
	if err != nil || len(rows) != 2 || rows[1][10] != "NEW_EMPIRICAL_PROOF" || rows[1][9] != "true" {
		t.Fatalf("unexpected proof row: rows=%v err=%v", rows, err)
	}

	out = t.TempDir()
	proofs, contradictions, err = writeEmpiricalProof(out, []FileResult{
		base("h1", "repo-a", "UASF_0001"), base("h2", "repo-b"),
	}, cfg)
	if err != nil || proofs != 0 || contradictions != 1 {
		t.Fatalf("contradicting observations: proofs=%d contradictions=%d err=%v", proofs, contradictions, err)
	}
	f, err = os.Open(filepath.Join(out, "empirical_proof_matrix.csv"))
	if err != nil {
		t.Fatal(err)
	}
	rows, err = csv.NewReader(f).ReadAll()
	_ = f.Close()
	if err != nil || len(rows) != 2 || rows[1][10] != "EMPIRICAL_CONTRADICTION" || rows[1][9] != "false" {
		t.Fatalf("unexpected contradiction row: rows=%v err=%v", rows, err)
	}
}
