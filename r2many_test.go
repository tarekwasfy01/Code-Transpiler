package r2many

import "testing"

func TestPublicPackageManyToManyAndSemanticJSON(t *testing.T) {
	if len(Languages()) != 13 {
		t.Fatalf("languages=%d", len(Languages()))
	}
	goCode, err := Transpile("c", "go", "int main() { int x = 2; printf(\"%d\\n\", x); return 0; }")
	if err != nil || goCode == "" {
		t.Fatalf("C to Go: %v", err)
	}
	doc, err := SemanticJSON("python", "x = 2\nprint(x)\n")
	if err != nil {
		t.Fatal(err)
	}
	rustCode, err := TranspileSemanticJSON("rust", doc)
	if err != nil || rustCode == "" {
		t.Fatalf("semantic JSON to Rust: %v", err)
	}
}
