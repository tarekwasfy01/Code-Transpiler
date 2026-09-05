package backend

import "testing"

func TestCanonicalHarvestConstructUsesSharedContracts(t *testing.T) {
	for _, tc := range []struct {
		operation string
		want      string
	}{
		{"ASSIGNMENT", "assign"}, {"LOAD", "identifier"},
		{"LITERAL", "literal"}, {"EQ", "binary"},
		{"NOT", "unary"}, {"ITERATION", "iteration"},
	} {
		if got := canonicalHarvestConstruct(tc.operation); got != tc.want {
			t.Fatalf("%s: got %q, want %q", tc.operation, got, tc.want)
		}
		if _, _, ok := matrixUASTKind(tc.operation); !ok {
			t.Fatalf("%s did not resolve to a canonical UAST contract", tc.operation)
		}
	}
}
