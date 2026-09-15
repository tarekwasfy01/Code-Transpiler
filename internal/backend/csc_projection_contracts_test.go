// Copyright (c) 2026 Tarek Wasfy
package backend

import "testing"

func TestCSharpProjectionPrimitivesCoverCSCErrorFamilies(t *testing.T) {
	primitives := CSharpProjectionPrimitives()
	if len(primitives) != 5 {
		t.Fatalf("primitive count=%d, want 5", len(primitives))
	}
	seen := map[string]bool{}
	for _, primitive := range primitives {
		if primitive.ID == "" || primitive.InputFamily == "" || len(primitive.RequiredFacts) == 0 || len(primitive.EmittedSurface) == 0 || primitive.FailureIfUnset == "" {
			t.Fatalf("incomplete primitive: %+v", primitive)
		}
		seen[primitive.InputFamily] = true
	}
	for _, family := range []string{"syntax_boundary", "namespace_member_model", "keyword_identifier_collision", "member_declaration", "other_csc"} {
		if !seen[family] {
			t.Fatalf("missing CSC family primitive %q", family)
		}
	}
}
