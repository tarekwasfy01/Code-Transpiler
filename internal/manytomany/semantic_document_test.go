package manytomany

import (
	"bytes"
	"testing"
)

func TestSemanticDocumentIsTheTransportBetweenLanguageAdapters(t *testing.T) {
	p, err := Parse("c", `#include <stdio.h>
int main(void) { int x = 2; while (x < 5) { x = x + 1; } printf("%g\\n", (double)x); return 0; }`)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Semantic.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(doc, []byte("CanonicalR")) {
		t.Fatal("canonical R leaked into semantic transport")
	}
	q, err := ParseDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	if q.CanonicalR != "" || q.Graph != nil {
		t.Fatal("document import rebuilt legacy R diagnostics")
	}
	for _, target := range []string{"go", "rust", "python", "c"} {
		a, err := Emit(target, p)
		if err != nil {
			t.Fatalf("direct %s: %v", target, err)
		}
		b, err := Emit(target, q)
		if err != nil || a != b {
			t.Fatalf("document %s differs: %v", target, err)
		}
	}
}
