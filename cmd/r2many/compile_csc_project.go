// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

// compileCSCProject links independently projected C# compilation units.  The
// Semantic units remain separate: only the target prelude and their generated
// Main bodies are combined for csc.exe.  A unit body is scoped in its own
// block, so generated local names cannot collide.  This is only the target
// link stage; cross-unit semantic symbol resolution remains a prerequisite
// and is deliberately not inferred from a successful csc run.
func compileCSCProject(args []string) error {
	fs := flag.NewFlagSet("compile-csc-project", flag.ContinueOnError)
	out := fs.String("o", "", "output executable")
	keep := fs.Bool("keep-cs", false, "keep the linked C# beside the executable")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-o": true, "-keep-cs": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 || *out == "" {
		return fmt.Errorf("usage: compile-csc-project semantic-unit-directory -o output.exe [-keep-cs]")
	}
	root, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}
	*out, err = filepath.Abs(*out)
	if err != nil {
		return err
	}
	info, err := os.Stat(root)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("compile-csc-project input is not a directory: %s", root)
	}
	var paths []string
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		if isSemanticProjectUnit(path) {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(paths) == 0 {
		return fmt.Errorf("compile-csc-project found no semantic units in %s", root)
	}
	sort.Strings(paths)
	var linked strings.Builder
	var marker string
	var suffix string
	for _, path := range paths {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		var source string
		if strings.HasSuffix(strings.ToLower(path), ".se") || strings.HasSuffix(strings.ToLower(path), ".sp") || strings.HasSuffix(strings.ToLower(path), ".spz") {
			source, err = manytomany.TranspileSemanticSPWithOptions("csharp", data, manytomany.TranspileRequest{ModuleBaseDir: filepath.Dir(path), ModuleEmbeddingMode: "all", EmbedAllModules: true})
		} else {
			var program manytomany.Program
			program, err = manytomany.ParseDocument(data)
			if err == nil {
				source, err = manytomany.Emit("csharp", program)
			}
		}
		if err != nil {
			return fmt.Errorf("semantic-to-csharp projection failed for %s: %w", path, err)
		}
		unitMarker, body, unitSuffix, splitErr := splitCSharpMain(source)
		if splitErr != nil {
			return fmt.Errorf("C# unit %s is not linkable: %w", path, splitErr)
		}
		if marker == "" {
			marker, suffix = unitMarker, unitSuffix
			linked.WriteString(marker)
		} else if marker != unitMarker || suffix != unitSuffix {
			return fmt.Errorf("C# unit %s has a different runtime/program contract", path)
		}
		linked.WriteString("\n    {\n")
		linked.WriteString(body)
		linked.WriteString("\n    }\n")
	}
	linked.WriteString(suffix)
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(*out), ".compile-csc-project-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	csPath := filepath.Join(tmp, "project.cs")
	if err := os.WriteFile(csPath, []byte(linked.String()), 0o644); err != nil {
		return err
	}
	if *keep {
		kept := strings.TrimSuffix(*out, filepath.Ext(*out)) + ".cs"
		if err := os.WriteFile(kept, []byte(linked.String()), 0o644); err != nil {
			return err
		}
	}
	csc, err := resolveCSC()
	if err != nil {
		return err
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
	fmt.Printf("CSC_PROJECT_BUILD=PASS\nUNITS=%d\nEXE=%s\n", len(paths), *out)
	return nil
}

// isSemanticProjectUnit deliberately accepts only semantic document names.
// Export sidecars such as *.se.summary.json are metadata, not compilation
// units; treating every JSON file as a SemanticProgram makes project builds
// fail before the first source unit is projected.
func isSemanticProjectUnit(path string) bool {
	lower := strings.ToLower(filepath.Base(path))
	if strings.HasSuffix(lower, ".se") || strings.HasSuffix(lower, ".sp") || strings.HasSuffix(lower, ".spz") {
		return true
	}
	return strings.HasSuffix(lower, ".semantic.json")
}

func splitCSharpMain(source string) (marker, body, suffix string, err error) {
	const start = "class Program { static void Main() {"
	index := strings.Index(source, start)
	if index < 0 {
		return "", "", "", fmt.Errorf("missing generated Program.Main marker")
	}
	bodyStart := index + len(start)
	end := strings.LastIndex(source, "\n} }")
	if end < bodyStart {
		end = strings.LastIndex(source, "\r\n} }")
	}
	if end < bodyStart {
		return "", "", "", fmt.Errorf("missing generated Program.Main terminator")
	}
	return source[:index] + start, source[bodyStart:end], source[end:], nil
}
