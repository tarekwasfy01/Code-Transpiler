package backend

import (
	"strings"
	"testing"
)

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
	if len(a.Rows) != 3 || len(a.Classes) != 2 || len(a.Primitives) != 2 {
		t.Fatalf("rows=%d classes=%d primitives=%d", len(a.Rows), len(a.Classes), len(a.Primitives))
	}
	for _, row := range a.Rows {
		if len(row.Missing) != 1 || !strings.HasPrefix(row.Missing[0], "native_form:"+row.Target+":") {
			t.Fatalf("%s missing=%v", row.CandidateID, row.Missing)
		}
		for _, missing := range row.Missing {
			found := false
			for _, required := range row.Required {
				if required == missing {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("%s lost exact residual edge %q", row.CandidateID, missing)
			}
		}
	}
}

func TestUnprovenDirectLoweringRegistryCannotPromote(t *testing.T) {
	if _, ok := DirectLoweringContractFor("go", "PROJ_030"); ok {
		t.Fatal("unproven generated registry must not expose a direct contract")
	}
}
