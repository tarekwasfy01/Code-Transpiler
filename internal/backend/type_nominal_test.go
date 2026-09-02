package backend

import "testing"

func TestNominalResolutionAndGaps(t *testing.T) {
	root := &SemanticStatement{Statements: []SemanticStatement{
		{Type: SemanticType{Kind: "named", Identity: "scope1:T", Element: &SemanticType{Kind: "integer"}}},
		{Type: SemanticType{Kind: "named", Identity: "scope1:T", Reference: true}},
		{Type: SemanticType{Kind: "named", Identity: "scope2:T", Reference: true}},
	}}
	table, _, err := deriveTypeTable(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := deriveTypeRelations(root, table)
	if err != nil {
		t.Fatal(err)
	}
	if r.Nominal.Resolution.NonZeros() != 1 {
		t.Fatal("nominal resolution conflates identities")
	}
	for i, e := range table {
		want := 0
		if e.Type.Identity == "scope2:T" {
			want = 1
		}
		if r.Nominal.Unresolved[i] != want {
			t.Fatalf("wrong gap for %+v", e.Type)
		}
	}
	root.Type = SemanticType{Kind: "named", Reference: true}
	table, _, err = deriveTypeTable(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = deriveTypeRelations(root, table); err == nil {
		t.Fatal("anonymous reference accepted")
	}
}

func TestNativeRecursiveNominalMatrix(t *testing.T) {
	a, err := (GoNativeFrontend{}).Analyze("recursive.go", `package sample
 type Node[T any] struct { Value T; Next *Node[T] }
 var A Node[int32]
 var B Node[string]
 func f() { type Local int32; var x Local; _ = x }
 func g() { type Local string; var x Local; _ = x }
 `)
	if err != nil {
		t.Fatal(err)
	}
	n := a.TypeRelations.Nominal
	if n.Resolution.NonZeros() == 0 {
		t.Fatal("recursive types have no resolution edges")
	}
	for i, gap := range n.Unresolved {
		if gap != 0 {
			t.Fatalf("unresolved: %+v", a.TypeTable[i])
		}
	}
	locals := map[string]bool{}
	for _, entry := range a.TypeTable {
		if entry.Type.Name == "sample.Local" {
			locals[entry.Type.Identity] = true
		}
	}
	if len(locals) != 2 {
		t.Fatalf("local declarations conflated: %v", locals)
	}
	if len(a.TypeRelations.Occurrences) != len(a.Events) {
		t.Fatal("missing event types")
	}
	if a.Executable {
		t.Fatal("analysis must not claim executable lowering")
	}
}
