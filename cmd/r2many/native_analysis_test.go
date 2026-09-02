package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeAnalysisCLI(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "input.go")
	output := filepath.Join(dir, "analysis.json")
	if err := os.WriteFile(source, []byte("package demo\nvar x uint32 = 7\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := nativeAnalysis([]string{source, "-o", output, "-source", "go"}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var document struct {
		Schema     string `json:"schema"`
		Executable bool   `json:"executable"`
	}
	if err = json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.Schema != "code-transpiler.native-analysis.v1" || document.Executable {
		t.Fatal("analysis mislabeled")
	}
	if err = nativeAnalysis([]string{"-source", "rust", source}); err == nil {
		t.Fatal("unsupported frontend accepted")
	}
}

func TestNativeSemanticExportCLI(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "native.go")
	output := filepath.Join(dir, "program.json")
	if err := os.WriteFile(source, []byte(`package main; import "fmt"; func main(){fmt.Println("native")}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := semanticExport([]string{source, "-native", "-source", "go", "-o", output}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(source, []byte(`package main;func main(){x:=1;_=x}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := semanticExport([]string{"-native", "-source", "go", source, "-o", output}); err == nil {
		t.Fatal("CLI silently fell back to legacy frontend")
	}
}
