// Copyright (c) 2026 Tarek Wasfy
package codetranspiler

import (
	"testing"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

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
	for _, target := range Languages() {
		out, err := TranspileSemanticJSON(target.ID, doc)
		if err != nil || out == "" {
			t.Fatalf("native public pipeline %s: %v", target.ID, err)
		}
		// The public API accepts the explicit compatibility-runtime result when
		// this program has no pure native rendering for a target. Pure-native
		// eligibility is enforced at the UAST projector boundary by final-source
		// taint and syntax gates; this smoke test proves the documented
		// Native -> Runtime -> Error fallback remains available for all targets.
	}
	if _, err := NativeSemanticJSON("go", "integer.go", `package main;func main(){x:=1;_=x}`); err == nil {
		t.Fatal("native frontend fell back to legacy")
	}
}

func TestPublicCompileUsesNativeGoFrontend(t *testing.T) {
	result, err := Compile(`package main
func main() { x := int32(2); if x > 1 { x = x + 1 } }`, CompileOptions{
		SourceLanguage: "go",
		TargetArch:     "x86_64",
		TargetOS:       "windows",
		ABI:            "win64",
		OutputKind:     Assembly,
	})
	if err != nil {
		t.Fatalf("native Go compile: %v", err)
	}
	if result.Text == "" || result.InstructionCount == 0 {
		t.Fatalf("native Go compile produced no assembly: %+v", result)
	}
}

func TestPublicCompileAcceptsSemanticJSONFrontend(t *testing.T) {
	p := backend.NewSemanticProgram(&backend.BlockStmt{List: []backend.Stmt{
		&backend.ReturnStmt{X: &backend.LiteralExpr{Kind: "integer", Text: "34"}},
	}}, "eager_left_to_right")
	wire, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Compile(string(wire), CompileOptions{SourceLanguage: "semantic", InputKind: InputSource, OutputKind: Assembly, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	if err != nil {
		t.Fatalf("semantic JSON compile: %v", err)
	}
	if got.Text == "" || got.InstructionCount == 0 {
		t.Fatalf("semantic JSON compile produced no assembly: %#v", got)
	}
}
