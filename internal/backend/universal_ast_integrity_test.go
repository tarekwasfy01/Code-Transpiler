package backend

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
)

func cloneUniversalDocument(t *testing.T, in *UniversalASTDocument) *UniversalASTDocument {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out UniversalASTDocument
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

func cloneUniversalBasis(t *testing.T) UniversalASTBasis {
	t.Helper()
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(uastEmbedded.Basis)
	if err != nil {
		t.Fatal(err)
	}
	var out UniversalASTBasis
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func requireUASTError(t *testing.T, d *UniversalASTDocument, contains string) {
	t.Helper()
	err := validateUniversalASTDocument(d)
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("invalid UAST accepted or wrong error: want %q, got %v", contains, err)
	}
}

func requireUASTBasisError(t *testing.T, b *UniversalASTBasis, contains string) {
	t.Helper()
	err := validateUniversalASTBasis(b)
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Fatalf("invalid UAST basis accepted or wrong error: want %q, got %v", contains, err)
	}
}

func TestUniversalASTRejectsInvalidFacetsAndNodes(t *testing.T) {
	base := testUniversalDocument(t)
	tests := []struct {
		name string
		want string
		edit func(*UniversalASTDocument)
	}{
		{"unknown facet", "unknown semantic facet", func(d *UniversalASTDocument) { d.Nodes[0].SemanticFacets = []string{"UASF_missing"} }},
		{"duplicate facet", "duplicate semantic facet", func(d *UniversalASTDocument) {
			d.Nodes[0].SemanticFacets = append(d.Nodes[0].SemanticFacets, d.Nodes[0].SemanticFacets[0])
		}},
		{"unknown structural kind", "unknown structural kind", func(d *UniversalASTDocument) { d.Nodes[0].StructuralKind = "UnknownNode" }},
		{"negative node ID", "unique and nonnegative", func(d *UniversalASTDocument) { d.Nodes[0].ID = -1 }},
		{"duplicate node ID", "unique and nonnegative", func(d *UniversalASTDocument) { d.Nodes[1].ID = d.Nodes[0].ID }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := cloneUniversalDocument(t, base)
			test.edit(d)
			requireUASTError(t, d, test.want)
		})
	}
}

func TestUniversalASTRejectsInvalidFieldsAndMasks(t *testing.T) {
	base := testUniversalDocument(t)
	tests := []struct {
		name string
		want string
		edit func(*UniversalASTDocument)
	}{
		{"truncated field mask", "field mask", func(d *UniversalASTDocument) {
			d.Nodes[0].FieldMask = d.Nodes[0].FieldMask[:len(d.Nodes[0].FieldMask)-1]
		}},
		{"reordered field mask", "field mask", func(d *UniversalASTDocument) {
			d.Nodes[0].FieldMask[0], d.Nodes[0].FieldMask[1] = d.Nodes[0].FieldMask[1], d.Nodes[0].FieldMask[0]
		}},
		{"unknown field", "is not applicable", func(d *UniversalASTDocument) { d.Nodes[0].Fields["not_a_universal_field"] = json.RawMessage(`true`) }},
		{"invalid field JSON", "not valid JSON", func(d *UniversalASTDocument) { d.Nodes[0].Fields[d.Nodes[0].FieldMask[0]] = json.RawMessage(`{`) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := cloneUniversalDocument(t, base)
			test.edit(d)
			requireUASTError(t, d, test.want)
		})
	}
}

func TestUniversalASTRejectsInvalidRelationsAndReferences(t *testing.T) {
	base := testUniversalDocument(t)
	inapplicable := ""
	n := base.Nodes[0]
	ki := indexOf(uastEmbedded.Basis.StructuralKinds, n.StructuralKind)
	for col, relation := range uastEmbedded.Basis.ConcreteRelations {
		if indexOf(uastEmbedded.Basis.GlobalRelations, relation) >= 0 || uastEmbedded.Basis.StructuralConcreteRelation.At(ki, col) != 0 {
			continue
		}
		allowed := false
		for _, facet := range n.SemanticFacets {
			if uastEmbedded.Basis.FacetConcreteRelation.At(indexOf(uastEmbedded.Basis.Facets, facet), col) != 0 {
				allowed = true
				break
			}
		}
		if !allowed {
			inapplicable = relation
			break
		}
	}
	if inapplicable == "" {
		t.Fatal("test fixture unexpectedly permits every concrete relation")
	}
	tests := []struct {
		name string
		want string
		edit func(*UniversalASTDocument)
	}{
		{"unknown relation", "unknown concrete relation", func(d *UniversalASTDocument) { d.Relations[0].Kind = "relation.unknown" }},
		{"inapplicable relation", "not applicable", func(d *UniversalASTDocument) { d.Relations[0].Kind = inapplicable }},
		{"missing source node", "source node", func(d *UniversalASTDocument) { d.Relations[0].From = 999 }},
		{"missing target node", "target node", func(d *UniversalASTDocument) { d.Relations[0].To = UniversalASTReference{Domain: "node", ID: "999"} }},
		{"nonnumeric target node", "target node", func(d *UniversalASTDocument) { d.Relations[0].To = UniversalASTReference{Domain: "node", ID: "x"} }},
		{"empty reference domain", "reference incomplete", func(d *UniversalASTDocument) { d.Relations[0].To = UniversalASTReference{ID: "1"} }},
		{"empty external reference ID", "reference incomplete", func(d *UniversalASTDocument) { d.Relations[0].To = UniversalASTReference{Domain: "type"} }},
		{"invalid relation attribute", "invalid relation attribute", func(d *UniversalASTDocument) { d.Relations[0].Attributes["evidence"] = json.RawMessage(`{`) }},
		{"empty relation attribute name", "invalid relation attribute", func(d *UniversalASTDocument) { d.Relations[0].Attributes[""] = json.RawMessage(`true`) }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := cloneUniversalDocument(t, base)
			test.edit(d)
			requireUASTError(t, d, test.want)
		})
	}
}

func TestUniversalASTRejectsInvalidLanguageProjection(t *testing.T) {
	base := testUniversalDocument(t)
	tests := []struct {
		name string
		want string
		edit func(*UniversalASTDocument)
	}{
		{"unknown language", "invalid universal AST language projection", func(d *UniversalASTDocument) { d.LanguageProfile = "unknown" }},
		{"wrong vector dimension", "invalid universal AST language projection", func(d *UniversalASTDocument) { d.LanguageFacet = d.LanguageFacet[:len(d.LanguageFacet)-1] }},
		{"forged vector value", "differs from matrix product", func(d *UniversalASTDocument) { d.LanguageFacet[0] += 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			d := cloneUniversalDocument(t, base)
			test.edit(d)
			requireUASTError(t, d, test.want)
		})
	}
}

func TestUniversalASTBasisRejectsInvalidCoverageAndCrosswalk(t *testing.T) {
	tests := []struct {
		name string
		want string
		edit func(*UniversalASTBasis)
	}{
		{"negative lower bound", "coverage interval", func(b *UniversalASTBasis) { b.CoverageLower.Set(0, 0, -0.01) }},
		{"upper bound above one", "coverage interval", func(b *UniversalASTBasis) { b.CoverageUpper.Set(0, 0, 1.01) }},
		{"reversed interval", "coverage interval", func(b *UniversalASTBasis) { b.CoverageLower.Set(0, 0, 0.9); b.CoverageUpper.Set(0, 0, 0.1) }},
		{"NaN lower bound", "coverage interval", func(b *UniversalASTBasis) { b.CoverageLower.Set(0, 0, math.NaN()) }},
		{"unknown crosswalk field", "absent from universal field catalog", func(b *UniversalASTBasis) { b.CrosswalkFields = append(b.CrosswalkFields, "unknown_crosswalk_field") }},
		{"duplicate crosswalk field", "labels are not unique", func(b *UniversalASTBasis) { b.CrosswalkFields = append(b.CrosswalkFields, b.CrosswalkFields[0]) }},
		{"unknown global relation", "absent from concrete relation catalog", func(b *UniversalASTBasis) { b.GlobalRelations = append(b.GlobalRelations, "relation.unknown") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			b := cloneUniversalBasis(t)
			test.edit(&b)
			requireUASTBasisError(t, &b, test.want)
		})
	}
}

func TestUniversalASTBasisCoverageAndCrosswalkAreExhaustivelyValid(t *testing.T) {
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	for row, language := range uastEmbedded.Basis.Languages {
		for col, facet := range uastEmbedded.Basis.Facets {
			lo := uastEmbedded.Basis.CoverageLower.At(row, col)
			hi := uastEmbedded.Basis.CoverageUpper.At(row, col)
			if math.IsNaN(lo) || math.IsNaN(hi) || math.IsInf(lo, 0) || math.IsInf(hi, 0) || lo < 0 || lo > hi || hi > 1 {
				t.Fatalf("invalid coverage interval %s/%s: [%v,%v]", language, facet, lo, hi)
			}
		}
	}
	for _, field := range uastEmbedded.Basis.CrosswalkFields {
		if indexOf(uastEmbedded.Basis.Fields, field) < 0 {
			t.Fatalf("crosswalk field %q is missing", field)
		}
	}
}
