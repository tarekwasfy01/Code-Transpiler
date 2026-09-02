package backend

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A bounded integer subset that is also exact in binary64. This tests the
// runtime/backend, NOT arbitrary native integer or source frontend equivalence.
func TestRandomizedSemanticDifferential(t *testing.T) {
	random := rand.New(rand.NewSource(20260831))
	var semantic, original, want strings.Builder
	original.WriteString("package main\nimport \"fmt\"\nfunc main(){\n")
	for i := 0; i < 1024; i++ {
		a, b, c := random.Intn(200)-100, random.Intn(200)-100, 1+random.Intn(20)
		expression := fmt.Sprintf("((%d + %d) * %d)", a, b, c)
		fmt.Fprintf(&semantic, "print(%s)\n", expression)
		fmt.Fprintf(&original, "fmt.Println(%s)\n", expression)
		fmt.Fprintln(&want, (a+b)*c)
	}
	original.WriteString("}\n")
	program, err := ParseSemantic("python", semantic.String())
	if err != nil {
		t.Fatal(err)
	}
	observed := ObserveSemantic(program)
	if observed.Error != "" || observed.Stdout != want.String() {
		t.Fatalf("semantic arithmetic differs: %s", observed.Error)
	}
	data, err := program.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := ParseSemanticJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if !EquivalentSemanticObservations(observed, ObserveSemantic(restored)) {
		t.Fatal("roundtrip changed output/error/effects")
	}
	if os.Getenv("CODETRANSPILER_NATIVE_E2E") != "1" {
		t.Log("1024 semantic cases PASS; external Go executions NOT RUN (set CODETRANSPILER_NATIVE_E2E=1)")
		return
	}
	generated, err := EmitSemantic("go", restored)
	if err != nil {
		t.Fatal(err)
	}
	for name, source := range map[string]string{"original": original.String(), "generated": generated} {
		t.Run(name, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "main.go")
			if err := os.WriteFile(file, []byte(source), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			cmd := exec.CommandContext(ctx, "go", "run", file)
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			output, err := cmd.Output()
			if err != nil {
				t.Fatalf("execution failed: %v %s", err, stderr.String())
			}
			if stderr.Len() != 0 || string(output) != want.String() {
				t.Fatalf("observable behavior differs: stderr=%s stdout length=%d", stderr.String(), len(output))
			}
		})
	}
}

func TestExecutionRejectsUndeclaredDialectAndStaleEvidence(t *testing.T) {
	p, _ := ParseSemantic("r", "print(1)")
	p.UniversalAST.Dialects = []SemanticDialect{{Name: "gpu", Operations: []SemanticDialectOperation{{ID: "k", Kind: "compute"}}}}
	if _, err := RunSemantic(p); err == nil {
		t.Fatal("unregistered dialect executed")
	}
	for _, target := range Languages {
		if _, err := EmitSemantic(target.ID, p); err == nil {
			t.Fatalf("%s ignored dialect", target.ID)
		}
	}
	p.UniversalAST.Dialects = nil
	p.Body.List = append(p.Body.List, &ExprStmt{X: &LiteralExpr{Kind: "number", Text: "2"}})
	if _, err := RunSemantic(p); err != nil {
		t.Fatalf("detached legacy body changed canonical execution: %v", err)
	}
}
