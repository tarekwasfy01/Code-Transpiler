// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
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
	if len(a.Rows) != 0 || len(a.Classes) != 0 || len(a.Primitives) != 0 {
		t.Fatalf("rows=%d classes=%d primitives=%d", len(a.Rows), len(a.Classes), len(a.Primitives))
	}
}

func TestUnprovenDirectLoweringRegistryCannotPromote(t *testing.T) {
	if _, ok := DirectLoweringContractFor("go", "PROJ_030"); ok {
		t.Fatal("unproven generated registry must not expose a direct contract")
	}
}

func TestDirectCrosswalkRepairsDerivedFieldForAnyFrontend(t *testing.T) {
	program, err := LowerNativeGo("crosswalk.go", "package main\nfunc main() {}\n")
	if err != nil {
		t.Fatal(err)
	}
	u, err := canonicalUniversalAST(program)
	if err != nil {
		t.Fatal(err)
	}
	u.Metadata = map[string]string{"frontend": "fixture-without-trust-marker"}
	if len(u.Nodes) == 0 {
		t.Fatal("lowered UAST has no root node")
	}
	if u.Nodes[0].Fields == nil {
		u.Nodes[0].Fields = map[string]json.RawMessage{}
	}
	u.Nodes[0].Fields["members"] = json.RawMessage(`"stale-derived-value"`)
	if err := validateDirectCrosswalkFields(u); err != nil {
		t.Fatal(err)
	}
	if u.Metadata["crosswalk.repaired"] != "derived-fields-from-relations" {
		t.Fatalf("derived crosswalk repair was not recorded: %#v", u.Metadata)
	}
	if err := validateDirectCrosswalkFields(u); err != nil {
		t.Fatalf("repaired crosswalk did not validate on the next pass: %v", err)
	}
}
