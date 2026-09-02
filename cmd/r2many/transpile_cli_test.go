package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

// Generated fixtures exercise the complete routing matrix, not full native
// language compatibility. Native source smoke cases are tested separately.
func TestCLIAllLanguageRoutes(t *testing.T) {
	p, err := backend.ParseSemantic("r", "print(2)")
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range backend.Frontends() {
		t.Run(source.ID, func(t *testing.T) {
			code, err := backend.EmitSemantic(source.ID, p)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			input := filepath.Join(dir, "input"+source.Extensions[0])
			out := filepath.Join(dir, "targets")
			if err = os.WriteFile(input, []byte(code), 0600); err != nil {
				t.Fatal(err)
			}
			if err = transpile([]string{input, "-to", "all", "-o", out}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(out, "translation-report.json"))
			if err != nil {
				t.Fatal(err)
			}
			var results []translationResult
			if err = json.Unmarshal(data, &results); err != nil {
				t.Fatal(err)
			}
			if len(results) != 13 {
				t.Fatalf("got %d routes", len(results))
			}
			for _, r := range results {
				if r.Error != "" || r.Path == "" {
					t.Fatalf("failed route: %+v", r)
				}
			}
		})
	}
}

func TestCLINativeSourcesAndAliases(t *testing.T) {
	cases := []struct{ from, to, code string }{{"c", "py", `int main(){int x=2;printf("%d\n",x);return 0;}`}, {"py", "c++", "x = 2\nprint(x)\n"}, {"go", "rust", `func main(){x:=2;fmt.Println(x)}`}}
	for _, c := range cases {
		t.Run(c.from, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "input.txt")
			out := filepath.Join(dir, "result.txt")
			if err := os.WriteFile(input, []byte(c.code), 0600); err != nil {
				t.Fatal(err)
			}
			if err := transpile([]string{input, "-from", c.from, "-to", c.to, "-o", out}); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(out)
			if err != nil || len(data) == 0 {
				t.Fatalf("empty output: %v", err)
			}
		})
	}
}

func TestCLIPreservesInputAndReportsNativeFailures(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.go")
	source := []byte(`package main;import "fmt";func main(){fmt.Println("native")}`)
	if err := os.WriteFile(input, source, 0600); err != nil {
		t.Fatal(err)
	}
	if err := transpile([]string{input, "-target", "go", "-o", input}); err == nil {
		t.Fatal("overwrote source")
	}
	if err := transpile([]string{input, "-target", "go"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "input.transpiled.go")); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "native")
	if err := transpile([]string{input, "-native", "-to", "all", "-o", out}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "translation-report.json"))
	if err != nil {
		t.Fatal(err)
	}
	var results []translationResult
	if err = json.Unmarshal(data, &results); err != nil {
		t.Fatal(err)
	}
	failures := 0
	for _, r := range results {
		if r.Error != "" {
			failures++
		}
	}
	if failures != 0 {
		t.Fatalf("expected diagnostics instead of route rejections, got %d", failures)
	}
}
