// Copyright (c) 2026 Tarek Wasfy
package backend

import "testing"

func TestSyntaxResolutionClassificationKeepsGrammarSeparate(t *testing.T) {
	for _, diagnostic := range []string{
		"error[E0425]: cannot find value `a` in this scope",
		"program.c:1: error: ‘a’ undeclared (first use in this function)",
		"Main.java:1: error: cannot find symbol",
	} {
		if !syntaxIsBlockedOnlyBySemanticResolution(diagnostic) {
			t.Fatalf("resolution diagnostic classified as syntax: %q", diagnostic)
		}
	}
	for _, diagnostic := range []string{
		"error: expected expression, found `;`",
		"program.c:1: error: expected ';' before '}' token",
	} {
		if syntaxIsBlockedOnlyBySemanticResolution(diagnostic) {
			t.Fatalf("syntax diagnostic classified as resolution: %q", diagnostic)
		}
	}
}
