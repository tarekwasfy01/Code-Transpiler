package backend

import "testing"

func TestCanonicalPrimitiveWitnessCoversAuthority(t *testing.T) {
	report, err := CompileUniversalPrimitiveSpecs()
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range report.Specs {
		witness, err := BuildCanonicalPrimitiveWitness(spec.ID)
		if err != nil {
			t.Fatalf("%s: %v", spec.ID, err)
		}
		if witness.Program == nil || witness.Program.UniversalAST == nil {
			t.Fatalf("%s has no canonical UAST witness", spec.ID)
		}
		if err := validateUniversalASTDocument(witness.Program.UniversalAST); err != nil {
			t.Fatalf("%s witness UAST: %v", spec.ID, err)
		}
	}
}
