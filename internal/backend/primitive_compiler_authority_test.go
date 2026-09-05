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
