package codetranspiler

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

func TestPublicNativeSemanticPipeline(t *testing.T) {
	doc, err := NativeSemanticJSON("go", "native.go", `package main; import "fmt"; func main(){text:="native"; fmt.Println(text)}`)
	if err != nil {
		t.Fatal(err)
	}
	code, err := TranspileSemanticJSON("go", doc)
	if err != nil || code == "" {
		t.Fatalf("native public pipeline: %v", err)
	}
	if _, err := TranspileSemanticJSON("r", doc); err == nil {
		t.Fatal("unverified native target accepted")
	}
	if _, err := NativeSemanticJSON("go", "integer.go", `package main;func main(){x:=1;_=x}`); err == nil {
		t.Fatal("native frontend fell back to legacy")
	}
}
