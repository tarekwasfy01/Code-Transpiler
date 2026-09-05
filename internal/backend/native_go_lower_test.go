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

func TestNativeGoExecutableRejectsUnsupported(t *testing.T) {
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
			if _, err := LowerNativeGo("unsupported.go", source); err == nil {
				t.Fatal("unsupported source silently accepted")
			}
		})
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
