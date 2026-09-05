package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

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
