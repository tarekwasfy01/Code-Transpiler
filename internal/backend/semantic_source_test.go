package backend

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSourceSpansSurviveWithoutChangingExecution(t *testing.T) {
	p, _ := ParseSemantic("r", "x <- 3; print(x)")
	d, _ := p.Document()
	// This explicitly exercises the one-time legacy import boundary. Once the
	// document is projected again, UAST is authoritative.
	d.UniversalAST = nil
	span := &SemanticSourceSpan{File: "original.go", StartOffset: 4, EndOffset: 12, StartLine: 2, StartColumn: 1, EndLine: 2, EndColumn: 9}
	d.Root.Statements[0].Source = span
	d.Root.Statements[0].Expression.Source = span
	q, err := ParseSemanticDocument(d)
	if err != nil {
		t.Fatal(err)
	}
	if d.Root.Statements[0].Source == nil {
		t.Fatal("import mutated caller")
	}
	beforeCanonical, err := q.MarshalUniversalASTJSON()
	if err != nil {
		t.Fatal(err)
	}
	after, err := q.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeCanonical, after) {
		t.Fatal("source mapping lost")
	}
	if !EquivalentSemanticObservations(ObserveSemantic(p), ObserveSemantic(q)) {
		t.Fatal("source metadata changed behavior")
	}
	q.Body.List = append(q.Body.List, &ExprStmt{X: &LiteralExpr{Kind: "number", Text: "4"}})
	if _, err = q.Document(); err != nil {
		t.Fatal(err)
	}
	if len(q.Body.List) != 2 {
		t.Fatal("legacy tree mutation overwrote canonical UAST")
	}
}

func TestExtensionNumbersRemainExact(t *testing.T) {
	p, _ := ParseSemantic("r", "print(1)")
	p.Extensions = map[string]any{"exact_integer": json.Number("9007199254740993")}
	first, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseSemanticJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := q.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("extension number rounded through binary64")
	}
}
