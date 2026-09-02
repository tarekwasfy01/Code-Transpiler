package backend

import "testing"

func TestTypeEquivalenceAliasAndUnknownPropagation(t *testing.T) {
	signed := true
	i32 := SemanticType{Kind: "integer", Bits: 32, Signed: &signed, Name: "int32"}
	other := i32
	other.Name = "integer32"
	other.TypeOrigin = "explicit"
	i64 := i32
	i64.Bits = 64
	types := []SemanticType{i32, other, i64, {Kind: "alias", Identity: "A", Element: &i32}, {Kind: "slice", Element: &SemanticType{Kind: "unknown"}}}
	root := &SemanticStatement{}
	for _, typ := range types {
		root.Statements = append(root.Statements, SemanticStatement{Type: typ})
	}
	table, _, err := deriveTypeTable(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := deriveTypeRelations(root, table)
	if err != nil {
		t.Fatal(err)
	}
	var group []int
	wide := -1
	for i, e := range table {
		if e.Type.Bits == 32 || e.Type.Kind == "alias" {
			group = append(group, i)
		}
		if e.Type.Bits == 64 {
			wide = i
		}
		if (e.Type.Kind == "unknown" || e.Type.Kind == "slice") && r.Equivalence.Unknown[i] != 1 {
			t.Fatal("unknown did not propagate")
		}
	}
	for _, i := range group {
		for _, j := range group {
			if r.Equivalence.Equivalent.At(i, j) != 1 {
				t.Fatal("equivalent domains failed")
			}
		}
		if r.Equivalence.Equivalent.At(i, wide) != 0 {
			t.Fatal("widths conflated")
		}
	}
}

func TestTypeEquivalenceRecursiveNominalDomains(t *testing.T) {
	ref := SemanticType{Kind: "named", Identity: "Node", Reference: true}
	root := &SemanticStatement{Type: SemanticType{Kind: "named", Identity: "Node", Element: &SemanticType{Kind: "struct", Fields: []SemanticField{{Name: "next", Type: SemanticType{Kind: "pointer", Element: &ref}}}}}}
	table, _, err := deriveTypeTable(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := deriveTypeRelations(root, table)
	if err != nil {
		t.Fatal(err)
	}
	def, reference := -1, -1
	for i, e := range table {
		if e.Type.Identity == "Node" {
			if e.Type.Reference {
				reference = i
			} else {
				def = i
			}
		}
	}
	if r.Equivalence.Equivalent.At(def, reference) != 1 || r.Equivalence.Unknown[reference] != 0 {
		t.Fatal("finite reference did not resolve coinductively")
	}
}

func TestTypeEquivalencePropagatesStructuralMismatch(t *testing.T) {
	signed := true
	makeType := func(bits int) SemanticType {
		return SemanticType{Kind: "slice", Element: &SemanticType{Kind: "tuple", Parameters: []SemanticType{{Kind: "integer", Bits: bits, Signed: &signed}}}}
	}
	root := &SemanticStatement{Type: makeType(32), Statements: []SemanticStatement{{Type: makeType(64)}}}
	table, _, err := deriveTypeTable(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := deriveTypeEquivalence(table)
	if err != nil {
		t.Fatal(err)
	}
	var slices []int
	for i, e := range table {
		if e.Type.Kind == "slice" {
			slices = append(slices, i)
		}
	}
	if r.Equivalent.At(slices[0], slices[1]) != 0 || r.Rounds < 2 {
		t.Fatal("nested width mismatch not refined")
	}
}

func TestTypeEquivalenceDoesNotEraseAliasQualifiers(t *testing.T) {
	signed := true
	base := SemanticType{Kind: "integer", Bits: 32, Signed: &signed}
	root := &SemanticStatement{Type: SemanticType{Kind: "alias", Identity: "Nullable", Nullable: "nullable", Element: &base}}
	table, _, err := deriveTypeTable(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := deriveTypeEquivalence(table)
	if err != nil {
		t.Fatal(err)
	}
	for i, e := range table {
		if e.Type.Kind == "alias" && (r.Unknown[i] != 1 || r.Equivalent.At(i, i) != 0) {
			t.Fatal("qualified alias treated as transparent")
		}
	}
}
