package backend

import "testing"

func TestSyntaxFailureSignature(t *testing.T) {
	for diagnostic, want := range map[string]string{
		"error: expected expression before ')' token": "expected expression",
		"unexpected token `}`":                        "unexpected token",
		"invalid assignment target":                   "invalid assignment target",
	} {
		if got := SyntaxFailureSignature(diagnostic); got != want {
			t.Fatalf("%q -> %q, want %q", diagnostic, got, want)
		}
	}
}
