package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadEmpiricalProofTreatsSufficientEvidenceAsProof(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empirical_proven_uast_matrix.csv")
	data := "language_id,canonical_semantic_id,empirical_proven,empirical_conflict,rule\n" +
		"python,UASF_0001,true,false,independent\n" +
		"python,UASF_0002,false,true,contradiction\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	proofs, err := readEmpiricalProof(path)
	if err != nil {
		t.Fatal(err)
	}
	if !proofs["python\x00UASF_0001"].Proven || proofs["python\x00UASF_0001"].Conflict {
		t.Fatalf("expected positive empirical proof, got %#v", proofs["python\x00UASF_0001"])
	}
	if proofs["python\x00UASF_0002"].Proven || !proofs["python\x00UASF_0002"].Conflict {
		t.Fatalf("expected empirical conflict, got %#v", proofs["python\x00UASF_0002"])
	}
}
