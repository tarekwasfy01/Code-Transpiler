// Copyright (c) 2026 Tarek Wasfy
//
// go-selfhost orchestrates the existing Go compiler migration stages.  It is
// intentionally only a coordinator: closure extraction and Go-to-Semantic
// lowering remain the existing productive implementations and no second IR is
// introduced here.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type stage struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	ExitCode   int    `json:"exit_code"`
	DurationMS int64  `json:"duration_ms"`
	Output     string `json:"output"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

type manifest struct {
	SchemaVersion string  `json:"schema_version"`
	SourceRoot    string  `json:"source_root"`
	OutputRoot    string  `json:"output_root"`
	Stages        []stage `json:"stages"`
	Status        string  `json:"status"`
}

func main() {
	root := flag.String("root", `C:\\PROGRA~1\\Go\\src\\cmd\\compile`, "Go compiler source root")
	out := flag.String("out", "compiler-migration/go/selfhost", "migration output directory")
	workers := flag.Int("workers", 8, "parallel frontend workers")
	timeout := flag.Int("timeout", 600, "per-file frontend timeout in seconds")
	format := flag.String("format", "se", "semantic output format: se or json")
	flag.Parse()
	if *format != "se" && *format != "json" {
		fatal("format must be se or json")
	}
	if err := os.MkdirAll(*out, 0755); err != nil {
		fatal(err.Error())
	}
	exe, err := os.Executable()
	if err != nil {
		fatal(err.Error())
	}
	binDir := filepath.Dir(exe)
	stages := make([]stage, 0, 2)
	closurePath := filepath.Join(*out, "index", "go-compiler-resolved-callgraph.jsonl")
	reportPath := filepath.Join(*out, "closure", "go-compiler-executable-closure.json")
	stages = append(stages, runStage("closure", filepath.Join(binDir, exeName("go-compiler-closure")), []string{
		"-root", *root, "-out", closurePath, "-closure-out", reportPath,
	}))
	semanticOut := filepath.Join(*out, "semantic")
	stages = append(stages, runStage("semantic", filepath.Join(binDir, exeName("go-semantic-project")), []string{
		"-root", *root, "-out", semanticOut, "-format", *format,
		"-workers", fmt.Sprint(*workers), "-timeout", fmt.Sprint(*timeout),
	}))
	status := "PASS"
	for _, s := range stages {
		if s.Status != "PASS" {
			status = "FAIL"
		}
	}
	m := manifest{SchemaVersion: "go-selfhost-migration-v1", SourceRoot: *root, OutputRoot: *out, Stages: stages, Status: status}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	manifestPath := filepath.Join(*out, "selfhost-manifest.json")
	if err := os.WriteFile(manifestPath, append(b, '\n'), 0644); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("SELF_HOST_STATUS=%s OUTPUT=%s\n", status, manifestPath)
	if status != "PASS" {
		os.Exit(1)
	}
}

func exeName(name string) string {
	if strings.EqualFold(os.Getenv("OS"), "Windows_NT") || filepath.Ext(os.Args[0]) == ".exe" {
		return name + ".exe"
	}
	return name
}

func runStage(name, executable string, args []string) stage {
	start := time.Now()
	s := stage{Name: name, Output: executable}
	cmd := exec.Command(executable, args...)
	// Compiler-source export is declaration-focused: the productive AST
	// lowerer still emits function bodies, while go/types body checking is an
	// optional cost that can otherwise dominate bootstrap on mutually
	// recursive compiler packages.
	if name == "semantic" {
		cmd.Env = append(os.Environ(), "UAST_GO_IGNORE_FUNC_BODIES=1", "UAST_GO_SKIP_TYPECHECK=1")
	}
	out, err := cmd.CombinedOutput()
	s.DurationMS = time.Since(start).Milliseconds()
	if err == nil {
		s.Status = "PASS"
		s.ExitCode = 0
		return s
	}
	s.Status = "FAIL"
	s.ExitCode = 1
	if exitErr, ok := err.(*exec.ExitError); ok {
		s.ExitCode = exitErr.ExitCode()
	}
	s.Diagnostic = strings.TrimSpace(string(out))
	if s.Diagnostic == "" {
		s.Diagnostic = err.Error()
	}
	return s
}

func fatal(message string) {
	fmt.Fprintln(os.Stderr, "go-selfhost:", message)
	os.Exit(2)
}
