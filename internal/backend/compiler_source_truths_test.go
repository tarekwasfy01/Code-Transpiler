// Copyright (c) 2026 Tarek Wasfy
package backend

import "testing"

func TestCompilerSourceTruthsUseCanonicalContractFamilies(t *testing.T) {
	truths := CompilerSourceTruths()
	if len(truths) != 7 {
		t.Fatalf("truth family count=%d, want 7", len(truths))
	}
	for _, truth := range truths {
		if truth.ID == "" || truth.Layer == "" || len(truth.SemanticAxes) == 0 || len(truth.RequiredFacts) == 0 || len(truth.BackendDemand) == 0 || truth.FailureIfUnset == "" {
			t.Fatalf("incomplete truth family: %+v", truth)
		}
	}
}

func TestCompilerSourceTruthsAreDefensive(t *testing.T) {
	first := CompilerSourceTruths()
	first[0].SemanticAxes[0] = "mutated"
	second := CompilerSourceTruths()
	if second[0].SemanticAxes[0] == "mutated" {
		t.Fatal("truth registry returned mutable shared state")
	}
}
