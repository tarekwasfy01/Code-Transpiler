// Copyright (c) 2026 Tarek Wasfy
//
// csc-selfhost performs the first productive C# compiler migration stage:
// source units are bound through the existing Roslyn bridge and serialized as
// the existing Canonical UAST/SemanticProgram. It is a bounded coordinator,
// not a second semantic representation or native backend.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

type unitResult struct {
	File       string `json:"file"`
	Status     string `json:"status"`
	Output     string `json:"output,omitempty"`
	Diagnostic string `json:"diagnostic,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

type report struct {
	SchemaVersion string       `json:"schema_version"`
	SourceRoot    string       `json:"source_root"`
	OutputRoot    string       `json:"output_root"`
	Total         int          `json:"total"`
	Pass          int          `json:"pass"`
	Fail          int          `json:"fail"`
	Units         []unitResult `json:"units"`
}

func main() {
	root := flag.String("root", ".", "C# compiler source root")
	out := flag.String("out", "compiler-migration/csc/selfhost/semantic", "output directory")
	workers := flag.Int("workers", 8, "parallel Roslyn binding workers")
	format := flag.String("format", "json", "output format: json or se")
	wholeProgram := flag.Bool("whole-program", true, "bind the discovered C# files as one compiler closure")
	flag.Parse()
	if *format != "json" && *format != "se" {
		fatal("format must be json or se")
	}
	if *workers < 1 {
		*workers = 1
	}
	var files []string
	if err := filepath.WalkDir(*root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := strings.ToLower(d.Name())
			if name == "bin" || name == "obj" || name == ".git" || name == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".cs") && !strings.HasSuffix(path, ".g.cs") {
			files = append(files, path)
		}
		return nil
	}); err != nil {
		fatal(err.Error())
	}
	sort.Strings(files)
	// CompilerSubset.cs is the historical single-file fixture.  Once the
	// real Program/Compiler/HostBoundary closure is present it would add a
	// second Main method and make Roslyn correctly reject the compilation as
	// ambiguous.  Keep it usable when it is the only source, but never merge
	// it into the real compiler closure.
	if len(files) > 1 {
		filtered := files[:0]
		for _, file := range files {
			if strings.EqualFold(filepath.Base(file), "CompilerSubset.cs") {
				continue
			}
			filtered = append(filtered, file)
		}
		files = filtered
	}
	if len(files) == 0 {
		fatal("no C# source files discovered")
	}
	if err := os.MkdirAll(*out, 0755); err != nil {
		fatal(err.Error())
	}
	if *wholeProgram {
		runWholeProgram(files, *root, *out, *format)
		return
	}
	results := make([]unitResult, len(files))
	jobs := make(chan int)
	var wg sync.WaitGroup
	var pass atomic.Int64
	for n := 0; n < *workers; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				file := files[i]
				start := time.Now()
				data, err := os.ReadFile(file)
				r := unitResult{File: filepath.ToSlash(file)}
				if err == nil {
					var p *backend.SemanticProgram
					p, err = backend.RoslynBoundToSemantic(file, string(data))
					if err == nil {
						err = backend.CompleteCanonicalUASTContracts(p)
					}
					if err == nil {
						rel, _ := filepath.Rel(*root, file)
						name := strings.TrimSuffix(rel, filepath.Ext(rel)) + "." + *format
						path := filepath.Join(*out, name)
						var wire []byte
						if *format == "se" {
							wire, err = p.MarshalSemanticSEWithSource()
						} else {
							wire, err = p.MarshalUniversalASTJSON()
						}
						if err == nil {
							err = os.MkdirAll(filepath.Dir(path), 0755)
						}
						if err == nil {
							err = os.WriteFile(path, wire, 0644)
							r.Output = path
						}
					}
				}
				r.DurationMS = time.Since(start).Milliseconds()
				if err != nil {
					r.Status = "FAIL"
					r.Diagnostic = err.Error()
				} else {
					r.Status = "PASS"
					pass.Add(1)
				}
				results[i] = r
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	r := report{SchemaVersion: "csc-selfhost-semantic-v1", SourceRoot: *root, OutputRoot: *out, Total: len(results), Pass: int(pass.Load()), Units: results}
	r.Fail = r.Total - r.Pass
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fatal(err.Error())
	}
	path := filepath.Join(*out, "report.json")
	if err = os.WriteFile(path, append(b, '\n'), 0644); err != nil {
		fatal(err.Error())
	}
	fmt.Printf("CSC_SEMANTIC_UNITS=%d PASS=%d FAIL=%d REPORT=%s\n", r.Total, r.Pass, r.Fail, path)
	if r.Fail != 0 {
		os.Exit(1)
	}
}

// runWholeProgram is the self-hosting route.  A compiler is not a bag of
// independently bindable source files: Program, Compiler and HostBoundary
// resolve each other's symbols.  Concatenating the discovered compilation
// units gives Roslyn one normal C# compilation while retaining every original
// source file verbatim in the report.  This creates one canonical UAST rather
// than an ad-hoc C# IR or a per-file fallback.
func runWholeProgram(files []string, root, out, format string) {
	parts := make([]string, 0, len(files))
	usingSet := map[string]bool{}
	usingLines := make([]string, 0)
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			writeWholeProgramReport(root, out, []unitResult{{File: filepath.ToSlash(file), Status: "FAIL", Diagnostic: err.Error()}})
			fatal(err.Error())
		}
		body := make([]string, 0)
		for _, line := range strings.Split(string(data), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "using ") && strings.HasSuffix(trimmed, ";") {
				if !usingSet[trimmed] {
					usingSet[trimmed] = true
					usingLines = append(usingLines, trimmed)
				}
				continue
			}
			body = append(body, line)
		}
		// Preserve line boundaries between source units.  Roslyn's spans then
		// still point into a deterministic, auditable compiler closure.
		parts = append(parts, "// semantic-csc-unit: "+filepath.ToSlash(file)+"\n"+strings.Join(body, "\n"))
	}
	start := time.Now()
	program, err := backend.RoslynBoundToSemantic("semantic-csc-selfhost.cs", strings.Join(usingLines, "\n")+"\n\n"+strings.Join(parts, "\n\n"))
	result := unitResult{File: "<whole-program>", DurationMS: time.Since(start).Milliseconds()}
	if err == nil {
		err = backend.CompleteCanonicalUASTContracts(program)
	}
	if err == nil {
		var wire []byte
		if format == "se" {
			wire, err = program.MarshalSemanticSEWithSource()
		} else {
			wire, err = program.MarshalUniversalASTJSON()
		}
		if err == nil {
			result.Output = filepath.Join(out, "semantic-csc-selfhost."+format)
			err = os.WriteFile(result.Output, wire, 0644)
		}
	}
	if err != nil {
		result.Status, result.Diagnostic = "FAIL", err.Error()
		writeWholeProgramReport(root, out, []unitResult{result})
		fmt.Fprintf(os.Stderr, "CSC_SELFHOST_WHOLE_PROGRAM=FAIL diagnostic=%s\n", result.Diagnostic)
		os.Exit(1)
	}
	result.Status = "PASS"
	writeWholeProgramReport(root, out, []unitResult{result})
	fmt.Printf("CSC_SELFHOST_WHOLE_PROGRAM=PASS sources=%d output=%s\n", len(files), result.Output)
}

func writeWholeProgramReport(root, out string, units []unitResult) {
	pass := 0
	for _, unit := range units {
		if unit.Status == "PASS" {
			pass++
		}
	}
	r := report{SchemaVersion: "csc-selfhost-semantic-v2", SourceRoot: root, OutputRoot: out, Total: len(units), Pass: pass, Fail: len(units) - pass, Units: units}
	b, err := json.MarshalIndent(r, "", "  ")
	if err == nil {
		_ = os.WriteFile(filepath.Join(out, "report.json"), append(b, '\n'), 0644)
	}
}

func fatal(s string) { fmt.Fprintln(os.Stderr, "csc-selfhost:", s); os.Exit(2) }
