package backend

import "testing"

func TestDirectLoweringAnalysisFactorsTargetNativeFormsExactly(t *testing.T) {
	rows := []map[string]string{
		{"candidate_id": "PROM_001", "target": "go", "projection_class": "PROJ_030", "uasf_set": "UASF_0054"},
		{"candidate_id": "PROM_002", "target": "go", "projection_class": "PROJ_030", "uasf_set": "UASF_0054"},
		{"candidate_id": "PROM_003", "target": "python", "projection_class": "PROJ_030", "uasf_set": "UASF_0054"},
	}
	a, err := directLoweringRequirements(rows)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Rows) != 0 || len(a.Classes) != 0 || len(a.Primitives) != 0 {
		t.Fatalf("rows=%d classes=%d primitives=%d", len(a.Rows), len(a.Classes), len(a.Primitives))
	}
}

func TestUnprovenDirectLoweringRegistryCannotPromote(t *testing.T) {
	if _, ok := DirectLoweringContractFor("go", "PROJ_030"); ok {
		t.Fatal("unproven generated registry must not expose a direct contract")
	}
}
