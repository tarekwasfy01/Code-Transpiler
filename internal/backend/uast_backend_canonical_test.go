package backend

import (
	"strings"
	"testing"
)

func TestCanonicalUniversalASTWinsOverLegacyProgramView(t *testing.T) {
	p, err := ParseSemantic("r", "print(1)")
	if err != nil {
		t.Fatal(err)
	}
	before, err := RunSemantic(p)
	if err != nil {
		t.Fatal(err)
	}
	if p.UniversalAST == nil {
		t.Fatal("execution did not establish canonical UAST")
	}

	// Body is retained for API compatibility, but is a derived view after the
	// first projection. It cannot become a second semantic truth.
	p.Body.List = append(p.Body.List, &ExprStmt{X: &CallExpr{Fun: &IdentExpr{Name: "print"}, Args: []Arg{{Value: &LiteralExpr{Kind: "number", Text: "2"}}}}})
	after, err := RunSemantic(p)
	if err != nil {
		t.Fatal(err)
	}
	if after != before || len(p.Body.List) != 1 {
		t.Fatalf("legacy mutation changed canonical behavior: before=%q after=%q nodes=%d", before, after, len(p.Body.List))
	}
	if err = p.AttachSemanticFeatureProfile("go"); err == nil {
		t.Fatal("legacy semantic-feature mutation accepted after canonical projection")
	}
}

func TestExecutableBackendConsumesAdditionalCanonicalFacetThroughRegistry(t *testing.T) {
	p, err := ParseSemantic("r", "print(1)")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	u := doc.UniversalAST
	n := &u.Nodes[0]
	added := ""
	for _, facet := range uastEmbedded.Basis.Facets {
		if !containsString(n.SemanticFacets, facet) {
			n.SemanticFacets = append(n.SemanticFacets, facet)
			added = facet
			break
		}
	}
	if added == "" {
		t.Fatal("test could not find an additional schema facet")
	}
	n.FieldMask, err = universalFieldMask(n)
	if err != nil {
		t.Fatal(err)
	}
	if err = validateUniversalASTDocument(u); err != nil {
		t.Fatalf("test mutation must remain schema-valid: %v", err)
	}
	output, err := RunSemantic(p)
	if err != nil {
		t.Fatalf("execution registry rejected canonical facet %q: %v", added, err)
	}
	if output != "1\n" {
		t.Fatalf("canonical facet changed the existing runtime value contract: %q", output)
	}
}

func TestLegacyDocumentCannotCarryIndependentCustomUniversalAST(t *testing.T) {
	p, err := ParseSemantic("r", "print(1)")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	doc.UniversalAST = testUniversalDocument(t)
	if _, err = ParseSemanticDocument(doc); err == nil || !strings.Contains(err.Error(), "serialize the canonical UAST directly") {
		t.Fatalf("custom UAST and independent legacy tree were accepted together: %v", err)
	}
}
