// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func nativeScalarCorpus() (string, string) {
	var source, want strings.Builder
	source.WriteString("package main\nimport \"fmt\"\nfunc main(){\n")
	for i := 0; i < 32; i++ {
		fmt.Fprintf(&source, `{ var active bool = true; text := "case%d"
{ text := "shadow"; fmt.Println(text) }
for active { fmt.Println(text); active = false }
if !active && text == "case%d" { fmt.Println("done") } else { fmt.Println("wrong") }
if text == "different" {fmt.Println("wrong equality")}
if text != "different" {fmt.Println("different")}
}
`, i, i)
		fmt.Fprintf(&want, "shadow\ncase%d\ndone\ndifferent\n", i)
	}
	source.WriteString("}\n")
	return source.String(), want.String()
}

func TestNativeGoPackageAndImportFacts(t *testing.T) {
	source := `package library
import "fmt"
func Emit() { fmt.Println(1) }
`
	p, err := LowerNativeGo("library.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Metadata["package"]; got != "library" {
		t.Fatalf("package fact lost: %q", got)
	}
	if len(p.Origin.Modules) != 1 || p.Origin.Modules[0] != "fmt" {
		t.Fatalf("import facts lost: %#v", p.Origin.Modules)
	}
	entries, ok := p.Extensions["function_entry_bindings"].(map[string]string)
	if !ok || entries["Emit"] == "" {
		t.Fatalf("library function facts were not lowered: %#v", p.Extensions["function_entry_bindings"])
	}
}

func TestNativeGoIntegerFieldSelectorDoesNotBecomeZero(t *testing.T) {
	program, err := LowerNativeGo("integer-field.go", `package main
type record struct { value int64 }
func main() {
	r := record{value: 37}
	_ = r.value
}
`)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := program.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(encoded, []byte(`"name":"value"`)) {
		t.Fatal("integer field selector was lost from semantic output")
	}
	if !bytes.Contains(encoded, []byte(`"text":"37"`)) {
		t.Fatal("integer field initializer was lost from semantic output")
	}
	if bytes.Contains(encoded, []byte(`"text":"0"`)) {
		t.Fatal("unsupported integer expression was silently replaced by zero")
	}
}

func TestNativeGoCompilerIROpCrosswalkUsesExistingIntegerContracts(t *testing.T) {
	for _, tc := range []struct {
		token, ir, semantic string
	}{
		{"+", "OADD", "integer.add"}, {"-", "OSUB", "integer.subtract"},
		{"*", "OMUL", "integer.multiply"}, {"/", "ODIV", "integer.divide"},
		{"%", "OMOD", "integer.remainder"}, {"<<", "OLSH", "integer.shift_left"},
		{">>", "ORSH", "integer.shift_right"}, {"&", "OAND", "integer.and"},
		{"|", "OOR", "integer.or"}, {"^", "OXOR", "integer.xor"},
		{"&^", "OANDNOT", "integer.and_not"},
	} {
		if got := nativeGoIRIntegerOperation(tc.token, false); got != tc.semantic {
			t.Errorf("token %s: got %q, want %q", tc.token, got, tc.semantic)
		}
		if got := nativeGoIRIntegerOperation(tc.ir, false); got != tc.semantic {
			t.Errorf("IR opcode %s: got %q, want %q", tc.ir, got, tc.semantic)
		}
	}
	for _, tc := range []struct{ token, ir, semantic string }{
		{"==", "OEQ", "integer.equal"}, {"!=", "ONE", "integer.not_equal"},
		{"<", "OLT", "integer.less"}, {"<=", "OLE", "integer.less_equal"},
		{">", "OGT", "integer.greater"}, {">=", "OGE", "integer.greater_equal"},
	} {
		if a, b := nativeGoIRIntegerComparison(tc.token), nativeGoIRIntegerComparison(tc.ir); a != tc.semantic || b != tc.semantic {
			t.Errorf("comparison %s/%s mapped to %q/%q, want %q", tc.token, tc.ir, a, b, tc.semantic)
		}
	}
	for _, tc := range []struct{ token, ir, semantic string }{
		{"+", "OPLUS", "integer.value"}, {"-", "ONEG", "integer.negate"}, {"^", "OBITNOT", "integer.complement"},
	} {
		if a, b := nativeGoIRIntegerOperation(tc.token, true), nativeGoIRIntegerOperation(tc.ir, true); a != tc.semantic || b != tc.semantic {
			t.Errorf("unary %s/%s mapped to %q/%q, want %q", tc.token, tc.ir, a, b, tc.semantic)
		}
	}
	if got := nativeGoIRIntegerOperation("OADDSTR", false); got != "" {
		t.Fatalf("string addition was incorrectly classified as integer operation %q", got)
	}
}

func TestSemanticOperationFromGoIRFailsClosedForCompilerOnlyOps(t *testing.T) {
	typ := integerType(64, true)
	op, ok := SemanticOperationFromGoIR("OADD", typ, false)
	if !ok || op.Name != "integer.add" || op.Type.Kind != typ.Kind || op.Type.Bits != typ.Bits || op.Type.Signed != typ.Signed {
		t.Fatalf("OADD adapter result = %#v, %v", op, ok)
	}
	if _, ok := SemanticOperationFromGoIR("OINLCALL", typ, false); ok {
		t.Fatal("compiler-only inlining opcode was promoted to executable semantic operation")
	}
	if _, ok := SemanticOperationFromGoIR("OADDSTR", typ, false); ok {
		t.Fatal("string addition was incorrectly accepted by integer adapter")
	}
}

func TestGoIRNodeContractUsesCanonicalStructures(t *testing.T) {
	for _, tc := range []struct{ opcode, structural, semantic string }{
		{"OCALL", "CallExpr", "call"}, {"OAS", "AssignStmt", "assignment"},
		{"OIF", "IfStmt", "if"}, {"OFOR", "LoopStmt", "for"},
		{"ORANGE", "ForEachStmt", "range"}, {"OINDEX", "IndexExpr", "index"},
		{"ORETURN", "ReturnStmt", "return"}, {"OBLOCK", "Scope", "block"},
	} {
		got, ok := GoIRNodeContractFor(tc.opcode)
		if !ok || got.StructuralKind != tc.structural || got.SemanticKind != tc.semantic {
			t.Errorf("%s: got %#v, %v", tc.opcode, got, ok)
		}
	}
	for _, opcode := range []string{"OINLCALL", "ORESULT", "OJUMPTABLE", "OTAILCALL"} {
		if _, ok := GoIRNodeContractFor(opcode); ok {
			t.Errorf("compiler-only opcode %s was promoted", opcode)
		}
	}
}

func TestNativeGoPackageContextResolvesButDoesNotDuplicateSiblingImplementations(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.test\n\ngo 1.23\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.go"), []byte("package main\n\nvar Shared int64 = 7\nfunc helper(v int64) int64 { return v + Shared }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.go")
	source := "package main\nfunc main() { helper(1) }\n"
	if err := os.WriteFile(mainPath, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	p, err := LowerNativeGo(mainPath, source)
	if err != nil {
		t.Fatal(err)
	}
	entries, ok := p.Extensions["function_entry_bindings"].(map[string]string)
	if !ok || entries["main"] == "" {
		t.Fatalf("requested file function was not lowered: %#v", p.Extensions["function_entry_bindings"])
	}
	if entries["helper"] != "" {
		t.Fatalf("sibling implementation was duplicated into the requested compilation unit: %#v", entries)
	}
	if p.Extensions["native_type_table"] == nil {
		t.Fatal("package type declarations were not retained as structured facts")
	}
	context, _ := p.Extensions["native_package_context"].(map[string]any)
	files, _ := context["files"].([]string)
	if len(files) != 2 {
		t.Fatalf("sibling resolver context was not retained: %#v", context)
	}
}

func TestLowerNativeGoPackageSharesTypecheckAndKeepsPerFileBodies(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helper.go"), []byte("package sample\nfunc helper(x int64) int64 { return x + 1 }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	mainPath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(mainPath, []byte("package sample\nfunc main() { _ = helper(41) }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	programs, err := LowerNativeGoPackage([]string{mainPath, filepath.Join(dir, "helper.go")})
	if err != nil {
		t.Fatal(err)
	}
	if len(programs) != 2 {
		t.Fatalf("got %d per-file programs; want 2", len(programs))
	}
	for path, program := range programs {
		if program == nil || program.Metadata["package"] != "sample" {
			t.Fatalf("invalid package semantic for %s: %#v", path, program)
		}
	}
	main := programs[mainPath]
	if main == nil {
		t.Fatalf("no independent unit for %s", mainPath)
	}
	entries, _ := main.Extensions["function_entry_bindings"].(map[string]string)
	if entries["main"] == "" || entries["helper"] != "" {
		t.Fatalf("unit bodies were merged or main body lost: %#v", entries)
	}
}

func TestNativeGoExecutableRoundtrip(t *testing.T) {
	source, want := nativeScalarCorpus()
	p, err := LowerNativeGo("native.go", source)
	if err != nil {
		t.Fatal(err)
	}
	observed := ObserveSemantic(p)
	if observed.Error != "" || observed.Stdout != want {
		t.Fatalf("direct native execution mismatch: %s\n%s", observed.Error, observed.Stdout)
	}
	data, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"source"`)) {
		t.Fatal("missing source mapping")
	}
	q, err := ParseSemanticJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	again, err := q.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, again) {
		t.Fatal("native roundtrip changed document")
	}
	if !EquivalentSemanticObservations(observed, ObserveSemantic(q)) {
		t.Fatal("roundtrip execution/effects changed")
	}
	// Only validated target contracts are enabled for native source semantics.
	for _, target := range Backends() {
		_, err := EmitSemantic(target.ID, q)
		if BackendCapability("native.go.scalar", target.ID).Status != CapabilityUnsupported && err != nil {
			t.Fatalf("go: %v", err)
		}
		if BackendCapability("native.go.scalar", target.ID).Status == CapabilityUnsupported && err == nil {
			t.Fatalf("unverified native target %s accepted", target.ID)
		}
	}
	if os.Getenv("CODETRANSPILER_NATIVE_E2E") != "1" {
		t.Log("external native comparison NOT RUN; set CODETRANSPILER_NATIVE_E2E=1")
		return
	}
	generated, err := EmitSemantic("go", q)
	if err != nil {
		t.Fatal(err)
	}
	for name, code := range map[string]string{"original": source, "generated": generated} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "main.go")
			if err := os.WriteFile(file, []byte(code), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "run", file)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("%v %s", err, stderr.String())
			}
			if string(output) != want || stderr.Len() != 0 {
				t.Fatalf("native observable mismatch stdout=%q stderr=%q", output, stderr.String())
			}
		})
	}
}

func TestNativeGoExecutableStructuralAcceptance(t *testing.T) {
	cases := []string{
		`package main; func main(){x:=uint64(9007199254740993);_ = x}`,
		`package main; import "fmt"; func main(){fmt.Println("unicode: ä")}`,
		`package main; func other(){defer other()};func main(){other()}`,
		`package main; func main(){if false {x:=1;_=x}}`,
		`package main; func main(){go func(){}()}`,
		`package main; func main(){x,y:=true,false;_,_=x,y}`,
	}
	for i, source := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			p, err := LowerNativeGo("structural.go", source)
			if err != nil {
				t.Fatalf("structurally valid source rejected: %v", err)
			}
			if p.UniversalAST == nil || len(p.UniversalAST.Nodes) == 0 {
				t.Fatal("structurally valid source produced no canonical UAST")
			}
		})
	}
}

func TestNativeGoAsyncStatementRunsThroughSemanticTaskContract(t *testing.T) {
	p, err := LowerNativeGo("async.go", `package main
var state int64
func worker() { state = 1 }
func main() { go worker() }
`)
	if err != nil {
		t.Fatal(err)
	}
	if p.UniversalAST == nil || len(p.UniversalAST.Nodes) == 0 {
		t.Fatal("async source produced no canonical UAST")
	}
	if _, err := RunSemantic(p); err != nil {
		t.Fatalf("async semantic task contract failed: %v", err)
	}
}

// This is the container/iteration/closure shape used by the desktop client.
// It must cross the Go AST -> FrontendSemanticFacts -> canonical UAST boundary
// as structure, never as an embedded Go expression string.
func TestNativeGoStructuredContainerIterationClosure(t *testing.T) {
	source := `package main
import "fmt"
func main() {
    var x []float64 = []float64{1, 2, 3}
    fmt.Println(func() []float64 { out := make([]float64, len(x)); for i, v := range x { out[i] = v * 2 }; return out }())
}`
	p, err := LowerNativeGo("input.go", source)
	if err != nil {
		t.Fatal(err)
	}
	if p.UniversalAST == nil || len(p.UniversalAST.Nodes) == 0 {
		t.Fatal("structured Go input did not produce a canonical UAST")
	}
	for _, n := range p.UniversalAST.Nodes {
		if n.StructuralKind == "" {
			t.Fatal("UAST contains an untyped structural node")
		}
	}
	graph, err := newUASTExecutionGraph(p.UniversalAST)
	if err != nil {
		t.Fatal(err)
	}
	cpp, err := generateTargetFromUniversalExisting(p.UniversalAST.Evaluation, "cpp", graph)
	if err != nil {
		t.Fatalf("structured Go UAST did not reach the native C++ backend: %v", err)
	}
	if taint := AnalyzeRuntimeTaint(cpp, nil); taint.Tainted() {
		t.Fatalf("native C++ output contains runtime artifacts: %v\n%s", taint.Artifacts, cpp)
	}
	// The public projector must select this same runtime-free native path; it
	// may not silently exchange a structurally projectable UAST for the
	// compatibility backend.
	routed, err := EmitSemantic("cpp", p)
	if err != nil {
		t.Fatalf("public C++ projection failed: %v", err)
	}
	if taint := AnalyzeRuntimeTaint(routed, nil); taint.Tainted() {
		t.Fatalf("public C++ projection fell back to runtime artifacts: %v\n%s", taint.Artifacts, routed)
	}
	if _, err := exec.LookPath("g++"); err == nil {
		file := filepath.Join(t.TempDir(), "main.cpp")
		if err := os.WriteFile(file, []byte(routed), 0600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command("g++", "-std=c++17", "-fsyntax-only", file).CombinedOutput(); err != nil {
			t.Fatalf("C++ native closure output is not syntactically valid: %v\n%s\n%s", err, output, cpp)
		}
	}
}

// The failure primitive matrix identifies unary, relational binary and counted
// loop lowering as one shared frontend contract. Keep them together so future
// changes cannot reintroduce a scalar-only Go AST subset.
func TestNativeGoStructuredOperatorsAndCountedLoop(t *testing.T) {
	source := `package main
import "fmt"
func main() {
    for i := 0.0; i < 3.0; i++ {
        if -i <= 0.0 && i >= 0.0 { fmt.Println(i) }
    }
}`
	p, err := LowerNativeGo("operators.go", source)
	if err != nil {
		t.Fatal(err)
	}
	cpp, err := EmitSemantic("cpp", p)
	if err != nil {
		t.Fatal(err)
	}
	if taint := AnalyzeRuntimeTaint(cpp, nil); taint.Tainted() {
		t.Fatalf("counted-loop projection contains runtime artifacts: %v\n%s", taint.Artifacts, cpp)
	}
	if _, err := exec.LookPath("g++"); err == nil {
		file := filepath.Join(t.TempDir(), "main.cpp")
		if err := os.WriteFile(file, []byte(cpp), 0600); err != nil {
			t.Fatal(err)
		}
		if output, err := exec.Command("g++", "-std=c++17", "-fsyntax-only", file).CombinedOutput(); err != nil {
			t.Fatalf("C++ native counted-loop output is invalid: %v\n%s\n%s", err, output, cpp)
		}
	}
}
