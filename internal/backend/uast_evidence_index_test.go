// Copyright (c) 2026 Tarek Wasfy
package backend

import "testing"

// The indexed resolver must preserve the previous ordered lexical lookup:
// among all bindings whose scopes are ancestors, the lowest binding ID wins.
func TestAnalyzeUniversalEvidenceIndexedBindingsMatchesOrderedReference(t *testing.T) {
	p := universalExecutableRoundtripProgram(t)
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	u, err := ProjectSemanticDocumentToUniversal(doc)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := AnalyzeUniversalEvidence(u)
	if err != nil {
		t.Fatal(err)
	}
	wantNonzeros := 0
	for row, node := range evidence.Nodes {
		if node.Kind != "identifier" {
			continue
		}
		want := -1
		for _, binding := range evidence.Bindings {
			for scope := node.Scope; scope >= 0; scope = evidence.Scopes[scope].Parent {
				if binding.Name == node.Symbol && binding.Scope == scope {
					want = binding.ID
					break
				}
			}
			if want >= 0 {
				break
			}
		}
		if want >= 0 {
			wantNonzeros++
			if got := evidence.Binding.At(row, want); got != 1 {
				t.Errorf("identifier row %d resolved binding %d to %v", row, want, got)
			}
			if got := evidence.Data.At(row, evidence.Bindings[want].Definition); got != 1 {
				t.Errorf("identifier row %d lacks definition edge for binding %d", row, want)
			}
		}
	}
	if got := evidence.Binding.NonZeros(); got != wantNonzeros {
		t.Fatalf("binding matrix has %d nonzeros, want %d from ordered reference", got, wantNonzeros)
	}
}
