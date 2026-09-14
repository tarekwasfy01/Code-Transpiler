// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

// compileCSC is the explicit SemanticProgram -> C# -> csc.exe route.  It
// keeps semantic loading in the normal UAST pipeline and never treats a C#
// diagnostic as a successful native build.
func compileCSC(args []string) error {
	fs := flag.NewFlagSet("compile-csc", flag.ContinueOnError)
	out := fs.String("o", "", "output executable")
	keep := fs.Bool("keep-cs", false, "keep the generated C# beside the executable")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-o": true, "-keep-cs": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 || *out == "" {
		return fmt.Errorf("usage: compile-csc input.se|input.json -o output.exe [-keep-cs]")
	}
	var err error
	input := fs.Arg(0)
	input, err = filepath.Abs(input)
	if err != nil {
		return err
	}
	*out, err = filepath.Abs(*out)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	var source string
	if strings.HasSuffix(strings.ToLower(input), ".se") || strings.HasSuffix(strings.ToLower(input), ".sp") || strings.HasSuffix(strings.ToLower(input), ".spz") {
		source, err = manytomany.TranspileSemanticSP("csharp", data)
	} else {
		program, parseErr := manytomany.ParseDocument(data)
		if parseErr != nil {
			return parseErr
		}
		source, err = manytomany.Emit("csharp", program)
	}
	if err != nil {
		return fmt.Errorf("semantic-to-csharp projection failed: %w", err)
	}
	csc, err := resolveCSC()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(*out), ".compile-csc-")
	if err != nil {
		return err
	}
	csPath := filepath.Join(tmp, "program.cs")
	if err := os.WriteFile(csPath, []byte(source), 0o644); err != nil {
		return err
	}
	if *keep {
		kept := strings.TrimSuffix(*out, filepath.Ext(*out)) + ".cs"
		if err := os.WriteFile(kept, []byte(source), 0o644); err != nil {
			return err
		}
	}
	cmd := exec.Command(csc, "/nologo", "/target:exe", "/r:System.Numerics.dll", "/out:"+*out, csPath)
	cmd.Dir = tmp
	combined, runErr := cmd.CombinedOutput()
	if runErr != nil {
		logPath := strings.TrimSuffix(*out, filepath.Ext(*out)) + ".csc.log"
		_ = os.WriteFile(logPath, combined, 0o644)
		return fmt.Errorf("csc.exe failed: %w: %s", runErr, strings.TrimSpace(string(combined)))
	}
	if _, err := os.Stat(*out); err != nil {
		return fmt.Errorf("csc.exe reported success but produced no executable: %w", err)
	}
	_ = os.RemoveAll(tmp)
	fmt.Printf("CSC_BUILD=PASS\nEXE=%s\n", *out)
	return nil
}

func resolveCSC() (string, error) {
	if path, err := exec.LookPath("csc.exe"); err == nil {
		return path, nil
	}
	for _, path := range []string{
		`C:\Windows\Microsoft.NET\Framework64\v4.0.30319\csc.exe`,
		`C:\Windows\Microsoft.NET\Framework\v4.0.30319\csc.exe`,
	} {
		if _, err := os.Stat(path); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("csc.exe not found")
}
