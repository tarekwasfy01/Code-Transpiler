package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"crypto/sha256"
	"encoding/hex"
)

// capture_transpile is the miner hook wrapper. It preserves the exact
// transpile invocation while exporting the canonical SemanticProgram beside
// the miner's result, so frontend-fact/UAST capability matrices can be
// computed without retaining third-party source.
func main() {
	if len(os.Args) < 2 { fmt.Fprintln(os.Stderr, "usage: capture_transpile <transpiler args>"); os.Exit(2) }
	args := append([]string(nil), os.Args[1:]...)
	var source, input string
	for i, a := range args {
		if (a == "-source" || a == "-from") && i+1 < len(args) { source = args[i+1] }
		if !strings.HasPrefix(a, "-") && i > 0 {
			if _, err := os.Stat(a); err == nil {
				// The only existing positional file is the source; output is created later.
				input = a
			}
		}
	}
	if source == "" { source = "auto" }
	if input == "" { input = args[len(args)-1] }
	root := os.Getenv("UAST_FACT_CAPTURE_DIR")
	if root != "" {
		_ = os.MkdirAll(root, 0755)
		id := "capture"
		if b, err := os.ReadFile(input); err == nil { sum := sha256.Sum256(b); id = hex.EncodeToString(sum[:]) }
		out := filepath.Join(root, id+".semantic.json")
		transpiler := os.Getenv("CODETRANSPILER_EXE")
		if transpiler == "" { transpiler = "CodeTranspiler.exe" }
		if source != "auto" {
			cmd := exec.Command(transpiler, append([]string{"semantic-export", "-source", source, input, "-o", out}, nil...)...)
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			_ = cmd.Run()
		}
	}
	transpiler := os.Getenv("CODETRANSPILER_EXE")
	if transpiler == "" { transpiler = "CodeTranspiler.exe" }
	cmd := exec.Command(transpiler, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		if x, ok := err.(*exec.ExitError); ok { os.Exit(x.ExitCode()) }
		fmt.Fprintln(os.Stderr, err); os.Exit(1)
	}
}
