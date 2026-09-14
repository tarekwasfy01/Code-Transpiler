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

	"github.com/tarekwasfy01/Code-Transpiler/v2/v2/internal/backend"
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
	if err := os.MkdirAll(*out, 0755); err != nil {
		fatal(err.Error())
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

func fatal(s string) { fmt.Fprintln(os.Stderr, "csc-selfhost:", s); os.Exit(2) }
