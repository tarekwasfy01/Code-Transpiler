// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// semantic-csc-bootstrap is intentionally a small host. It only performs the
// bootstrap bridge (C# parse/bind) and canonical SemanticProgram emission;
// GUI, CLI frameworks, and unrelated frontends are not part of this path.
func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: semantic-csc-bootstrap.exe input.cs output.semantic.json")
		os.Exit(2)
	}
	input, output := os.Args[1], os.Args[2]
	host, err := resolveBootstrapHost()
	if err != nil {
		fail("resolve explicit semantic-csc host", err)
	}
	cmd := exec.Command(host, "semantic-csc", input, "-o", output)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fail("run semantic-csc host", err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		fail("read host SemanticProgram output", err)
	}
	var document any
	if err := json.Unmarshal(data, &document); err != nil {
		fail("decode host SemanticProgram output", err)
	}
	if !containsNonEmptyNodes(document) {
		fail("validate host SemanticProgram output", fmt.Errorf("CSC_SELFHOST_EMPTY_UAST: host produced no executable UAST nodes"))
	}
	fmt.Printf("SEMANTIC_BOOTSTRAP_INPUT=%s\nSEMANTIC_BOOTSTRAP_OUTPUT=%s\n", input, output)
}

func containsNonEmptyNodes(value any) bool {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			if key == "nodes" {
				if list, ok := child.([]any); ok && len(list) > 0 {
					for _, node := range list {
						if executableNode(node) {
							return true
						}
					}
				}
			}
			if containsNonEmptyNodes(child) {
				return true
			}
		}
	case []any:
		for _, child := range current {
			if containsNonEmptyNodes(child) {
				return true
			}
		}
	}
	return false
}

func executableNode(value any) bool {
	node, ok := value.(map[string]any)
	if !ok {
		return false
	}
	kind, _ := node["structural_kind"].(string)
	switch kind {
	case "", "Scope", "ModuleDecl", "SymbolRef", "ImportDecl", "TypeDecl":
		return false
	default:
		return true
	}
}

func resolveBootstrapHost() (string, error) {
	if configured := os.Getenv("CODETRANSPILER_BOOTSTRAP_HOST"); configured != "" {
		if _, err := os.Stat(configured); err != nil {
			return "", fmt.Errorf("configured host %q: %w", configured, err)
		}
		return configured, nil
	}
	if path, err := exec.LookPath("CodeTranspiler.exe"); err == nil {
		return path, nil
	}
	if runtime.GOOS == "windows" {
		candidates := []string{
			filepath.Join("outputs", "virtualdisplays-semantic", "build-loop-20260913", "r2many-current.exe"),
			filepath.Join("outputs", "CodeTranspiler-current.exe"),
		}
		for _, candidate := range candidates {
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
		}
	}
	return "", fmt.Errorf("set CODETRANSPILER_BOOTSTRAP_HOST to a semantic-csc host executable")
}

func fail(operation string, err error) {
	fmt.Fprintf(os.Stderr, "SEMANTIC_CSC_BOOTSTRAP_FAILED operation=%s error=%v\n", operation, err)
	os.Exit(1)
}
