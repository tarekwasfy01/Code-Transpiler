// Copyright (c) 2026 Tarek Wasfy
package backend

import "testing"

func TestPrimitiveCompilerIncludesCanonicalAtomicAuthority(t *testing.T) {
	report, err := CompileUniversalPrimitiveSpecs()
	if err != nil {
		t.Fatal(err)
	}
	recipes := map[string]GeneratedLoweringRecipe{}
	for _, recipe := range report.Recipes {
		recipes[recipe.Primitive] = recipe
	}
	for _, id := range []string{"ASSIGNMENT", "LOAD", "LITERAL", "CALL", "NOT", "ITERATION", "ADD", "EQ"} {
		kernel, ok := GenericAtomicKernel(id)
		if !ok || kernel == "" {
			t.Fatalf("canonical primitive %s has no kernel", id)
		}
		recipe, ok := recipes[id]
		if !ok || recipe.ProofState != "CANONICAL_UAST_TERMINAL" {
			t.Fatalf("canonical primitive %s is absent from compiler authority", id)
		}
	}
}

func TestPrimitiveCompilerReportsOnlyVerifiedStructuralExecutors(t *testing.T) {
	report, err := CompileUniversalPrimitiveSpecs()
	if err != nil {
		t.Fatal(err)
	}
	if report.GeneratedExecutorReachable != 5 {
		t.Fatalf("expected the five verified scalar/aggregate graph handlers to be executable, got %d", report.GeneratedExecutorReachable)
	}
	if len(report.ContractGaps) == 0 {
		t.Fatal("generated recipes without graph handlers must remain visible as contract gaps")
	}
	for _, id := range report.ContractGaps {
		if id == "DOUBLE" || id == "AVERAGE2" || id == "MEAN" || id == "ALL" || id == "RMS" {
			t.Fatalf("%s has an exact structural graph handler", id)
		}
	}
}
