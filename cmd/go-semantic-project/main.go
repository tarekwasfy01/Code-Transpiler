// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

type result struct {
	File, Hash, Status, Phase, Diagnostic, Output string
	Bytes                                         int64
	DurationMS                                    int64
}

func main() {
	root := flag.String("root", ".", "project root")
	out := flag.String("out", "outputs/go-semantic-project-current", "output directory")
	workers := flag.Int("workers", 6, "parallel frontend workers")
	timeoutSeconds := flag.Int("timeout", 20, "per-file frontend timeout in seconds")
	flag.Parse()
	if err := os.MkdirAll(filepath.Join(*out, "semantic-json"), 0755); err != nil {
		panic(err)
	}
	var files []string
	err := filepath.WalkDir(*root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			base := strings.ToLower(filepath.Base(path))
			if strings.Contains(path, ".cache") || strings.Contains(path, "outputs") || strings.HasPrefix(base, "github-release") || strings.HasPrefix(base, "github-push-staging") || base == "matrices" || base == "vendor" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".go") && !strings.HasSuffix(path, "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	sort.Strings(files)
	results := make([]result, len(files))
	if *workers < 1 {
		*workers = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < *workers; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				file := files[i]
				data, e := os.ReadFile(file)
				if e != nil {
					results[i] = result{File: file, Status: "FAIL", Phase: "READ", Diagnostic: e.Error()}
					continue
				}
				sum := sha256.Sum256(data)
				rel, _ := filepath.Rel(*root, file)
				start := time.Now()
				p, e := safeLower(file, string(data), time.Duration(*timeoutSeconds)*time.Second)
				r := result{File: rel, Hash: fmt.Sprintf("%x", sum[:]), Bytes: int64(len(data)), DurationMS: time.Since(start).Milliseconds()}
				if e != nil {
					r.Status = "FAIL"
					r.Phase = "SOURCE_TO_SEMANTIC"
					if e == context.DeadlineExceeded {
						r.Status = "TIMEOUT"
						r.Phase = "TIMEOUT"
					}
					r.Diagnostic = compact(e.Error())
				} else {
					wire, x := p.MarshalSemanticJSON()
					if x != nil {
						r.Status = "FAIL"
						r.Phase = "SEMANTIC_SERIALIZE"
						r.Diagnostic = compact(x.Error())
					} else {
						r.Status = "PASS"
						r.Phase = "SOURCE_TO_SEMANTIC"
						name := strings.ReplaceAll(rel, "\\", "__")
						r.Output = filepath.Join("semantic-json", name+".semantic.json")
						if x = os.WriteFile(filepath.Join(*out, r.Output), wire, 0644); x != nil {
							r.Status = "FAIL"
							r.Phase = "SEMANTIC_WRITE"
							r.Diagnostic = compact(x.Error())
						}
					}
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
	families := map[string]map[string]int{}
	for _, r := range results {
		if r.Status == "FAIL" {
			fam, prim := classify(r.Diagnostic)
			if families[fam] == nil {
				families[fam] = map[string]int{}
			}
			families[fam][prim]++
		}
	}
	writeResults(*out, results, families)
	pass, fail := 0, 0
	for _, r := range results {
		if r.Status == "PASS" {
			pass++
		} else {
			fail++
		}
	}
	summary := map[string]any{"total": len(results), "pass": pass, "fail": fail, "root": *root, "output": *out}
	b, _ := json.MarshalIndent(summary, "", "  ")
	_ = os.WriteFile(filepath.Join(*out, "summary.json"), b, 0644)
	fmt.Printf("GO_FILES=%d PASS=%d FAIL=%d OUTPUT=%s\n", len(results), pass, fail, *out)
}

func safeLower(file, source string, limit time.Duration) (*backend.SemanticProgram, error) {
	ch := make(chan struct {
		p   *backend.SemanticProgram
		err error
	}, 1)
	go func() {
		var p *backend.SemanticProgram
		var err error
		defer func() {
			if x := recover(); x != nil {
				err = fmt.Errorf("frontend panic: %v", x)
			}
			ch <- struct {
				p   *backend.SemanticProgram
				err error
			}{p, err}
		}()
		p, err = backend.LowerSource("go", file, source)
	}()
	select {
	case r := <-ch:
		return r.p, r.err
	case <-time.After(limit):
		return nil, context.DeadlineExceeded
	}
}

func compact(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > 500 {
		return s[:500]
	}
	return s
}
func classify(s string) (string, string) {
	l := strings.ToLower(s)
	switch {
	case strings.Contains(l, "import") || strings.Contains(l, "package"):
		return "PACKAGE_RESOLUTION", "MODULE_BINDING"
	case strings.Contains(l, "type") || strings.Contains(l, "declared"):
		return "TYPE_DECLARATION", "TYPE_CONTRACT"
	case strings.Contains(l, "range") || strings.Contains(l, "loop") || strings.Contains(l, "for"):
		return "CONTROL_FLOW", "ITERATION_CONTROL"
	case strings.Contains(l, "slice") || strings.Contains(l, "index") || strings.Contains(l, "array") || strings.Contains(l, "map"):
		return "AGGREGATE", "AGGREGATE_ACCESS"
	case strings.Contains(l, "call") || strings.Contains(l, "function") || strings.Contains(l, "method"):
		return "CALL_BINDING", "CALL_SIGNATURE"
	default:
		return "SEMANTIC_CONTRACT", "STRUCTURED_FRONTEND"
	}
}
func writeResults(out string, rs []result, fam map[string]map[string]int) {
	f, _ := os.Create(filepath.Join(out, "results.csv"))
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"file", "source_hash", "status", "phase", "bytes", "duration_ms", "output", "diagnostic"})
	for _, r := range rs {
		_ = w.Write([]string{r.File, r.Hash, r.Status, r.Phase, fmt.Sprint(r.Bytes), fmt.Sprint(r.DurationMS), r.Output, r.Diagnostic})
	}
	w.Flush()
	f, _ = os.Create(filepath.Join(out, "failure_matrix.csv"))
	defer f.Close()
	w = csv.NewWriter(f)
	_ = w.Write([]string{"failure_family", "primitive", "count"})
	for a, m := range fam {
		for p, n := range m {
			_ = w.Write([]string{a, p, fmt.Sprint(n)})
		}
	}
	w.Flush()
}
