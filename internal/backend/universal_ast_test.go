package backend

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSemanticDocumentUniversalASTSemanticDocumentRoundTrip(t *testing.T) {
	p, err := ParseSemantic("go", `f <- function(a, b = 2) { if (a > b) { return(a) } else { return(b) } }
x <- f(a = 3)
for (i in 1:3) { x <- x + i }
print(x)`)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	u, err := ProjectSemanticDocumentToUniversal(doc)
	if err != nil {
		t.Fatal(err)
	}
	back, err := SemanticDocumentFromUniversalAST(u)
	if err != nil {
		t.Fatal(err)
	}
	left, right := doc, back
	left.UniversalAST = nil
	right.UniversalAST = nil
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	if !bytes.Equal(a, b) {
		at := 0
		for at < len(a) && at < len(b) && a[at] == b[at] {
			at++
		}
		lo := at - 100
		if lo < 0 {
			lo = 0
		}
		hi := at + 300
		if hi > len(a) {
			hi = len(a)
		}
		hj := at + 300
		if hj > len(b) {
			hj = len(b)
		}
		t.Fatalf("semantic roundtrip differs at byte %d\nwant=%s\ngot=%s", at, a[lo:hi], b[lo:hj])
	}
	if _, err = ParseSemanticDocument(back); err != nil {
		t.Fatal(err)
	}
}

func testUniversalDocument(t *testing.T) *UniversalASTDocument {
	t.Helper()
	d, err := NewUniversalASTDocument("cpp")
	if err != nil {
		t.Fatal(err)
	}
	kind, facet := uastEmbedded.Basis.StructuralKinds[0], uastEmbedded.Basis.Facets[0]
	first, err := d.AddNode(kind, []string{facet}, map[string]json.RawMessage{"id": json.RawMessage("0")})
	if err != nil {
		t.Fatal(err)
	}
	second, err := d.AddNode(kind, []string{facet}, map[string]json.RawMessage{"id": json.RawMessage("1")})
	if err != nil {
		t.Fatal(err)
	}
	ki, fi := indexOf(uastEmbedded.Basis.StructuralKinds, kind), indexOf(uastEmbedded.Basis.Facets, facet)
	relation := ""
	for col, name := range uastEmbedded.Basis.ConcreteRelations {
		if uastEmbedded.Basis.StructuralConcreteRelation.At(ki, col) != 0 || uastEmbedded.Basis.FacetConcreteRelation.At(fi, col) != 0 {
			relation = name
			break
		}
	}
	if relation == "" {
		t.Fatal("test node has no applicable relation")
	}
	if err := d.AddRelation(relation, first, UniversalASTReference{Domain: "node", ID: "1"}, map[string]json.RawMessage{"evidence": json.RawMessage(`{"source":"matrix"}`)}); err != nil {
		t.Fatal(err)
	}
	if second != 1 {
		t.Fatal("unstable node IDs")
	}
	return d
}

func TestUniversalASTSchemaDimensionsAndAlgebra(t *testing.T) {
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	b := uastEmbedded.Basis
	if len(b.Features) != 553 || len(b.Facets) != 334 || len(b.StructuralKinds) != 109 || len(b.SemanticAxes) != 44 || len(b.RelationAxes) != 23 || len(b.ConcreteRelations) != 55 || len(b.Fields) != 57 || len(b.Layers) != 17 {
		t.Fatal("universal schema dimensions changed")
	}
}

func TestUniversalASTSemanticProgramRoundTrip(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{}, "eager_left_to_right")
	p.Origin.SourceLanguage = "cpp"
	p.UniversalAST = testUniversalDocument(t)
	data, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseSemanticJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if q.UniversalAST == nil || len(q.UniversalAST.Nodes) != 2 || len(q.UniversalAST.Relations) != 1 {
		t.Fatal("universal AST payload lost")
	}
	if _, err := RunSemantic(q); err == nil || !strings.Contains(err.Error(), "no executable lowering") {
		t.Fatalf("universal semantics silently dropped: %v", err)
	}
}

func TestUniversalASTRejectsForgedFieldMaskAndRelation(t *testing.T) {
	d := testUniversalDocument(t)
	d.Nodes[0].FieldMask = d.Nodes[0].FieldMask[:1]
	if err := validateUniversalASTDocument(d); err == nil || !strings.Contains(err.Error(), "field mask") {
		t.Fatalf("forged field mask accepted: %v", err)
	}
	d = testUniversalDocument(t)
	d.Relations[0].Kind = "not.a.relation"
	if err := validateUniversalASTDocument(d); err == nil || !strings.Contains(err.Error(), "unknown concrete relation") {
		t.Fatalf("unknown relation accepted: %v", err)
	}
}
