package backend

import "testing"

// The modern MatrixIR source path must consume CanonicalSemanticEvents
// directly.  A change that routes it through parseFrontendFacts would make a
// second text parser part of production and fail this migration gate.
func TestNoPostFrontendSourceReparse(t *testing.T) {
	before := textSemanticParseCalls.Load()
	if _, err := LowerMatrixLanguage("python", "x = 1\nprint(x)\n"); err != nil {
		t.Fatal(err)
	}
	if after := textSemanticParseCalls.Load(); after != before {
		t.Fatalf("post-frontend text parser reached from canonical path: before=%d after=%d", before, after)
	}
}
