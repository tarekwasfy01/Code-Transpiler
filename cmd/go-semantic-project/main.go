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

	"github.com/tarekwasfy01/Code-Transpiler/v2/internal/backend"
)

type result struct {
	File, Hash, Status, Phase, Diagnostic, Output string
	Modules                                       []string
	Embeddings                                    []backend.SemanticEmbeddedModule
	Bytes                                         int64
	DurationMS                                    int64
}

type streamingFailureLog struct {
	mu sync.Mutex
	f  *os.File
}

func (l *streamingFailureLog) record(index int, r result) {
	if l == nil || l.f == nil || r.Status == "PASS" {
		return
	}
	entry := struct {
		Index int `json:"index"`
		result
	}{Index: index, result: r}
	b, err := json.Marshal(entry)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, _ = l.f.Write(append(b, '\n'))
	_ = l.f.Sync()
}

func main() {
	root := flag.String("root", ".", "project root")
	singleFile := flag.String("file", "", "transpile only this Go file while retaining root module resolution")
	out := flag.String("out", "outputs/go-semantic-project-current", "output directory")
	format := flag.String("format", "se", "output format: se or json")
	moduleRoot := flag.String("module-root", "", "shared Semantic module store root")
	embedModules := flag.Bool("embed-modules", false, "embed resolved module bodies once in .se files and link later users to the owner")
	embedMode := flag.String("embed-mode", "full", "module embedding mode: full or needed")
	license := flag.Bool("license", false, "copy imported module licenses once")
	resume := flag.Bool("resume", false, "skip source files whose semantic output already exists")
	workers := flag.Int("workers", 6, "parallel frontend workers")
	// Go package type resolution can legitimately traverse a module import
	// graph. Twenty seconds classified valid large files as TIMEOUT before the
	// frontend had a chance to finish; keep the limit bounded but give one
	// package-resolution pass enough time to complete.
	timeoutSeconds := flag.Int("timeout", 600, "per-file frontend timeout in seconds")
	flag.Parse()
	if *embedMode != "full" && *embedMode != "needed" {
		panic("embed-mode must be full or needed")
	}
	if *format != "se" && *format != "json" {
		panic("format must be se or json")
	}
	if err := os.MkdirAll(filepath.Join(*out, "semantic-"+*format), 0755); err != nil {
		panic(err)
	}
	embedStoreRoot := *moduleRoot
	if *embedModules && strings.TrimSpace(embedStoreRoot) == "" {
		var embedErr error
		embedStoreRoot, embedErr = backend.ModuleStoreRoot()
		if embedErr != nil {
			panic(embedErr)
		}
	}
	embedRegistry := backend.NewSemanticModuleEmbeddingRegistry(filepath.Join(*out, "semantic-"+*format))
	var files []string
	err := filepath.WalkDir(*root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			base := strings.ToLower(filepath.Base(path))
			if (path != *root && strings.HasPrefix(base, ".")) || strings.Contains(path, ".cache") || strings.Contains(path, ".codex-modcache") || strings.Contains(path, ".gomodcache") || strings.Contains(path, ".full-push") || strings.Contains(path, ".tmp-") || strings.Contains(path, "outputs") || strings.HasPrefix(base, "github-release") || strings.HasPrefix(base, "github-push-staging") || base == "matrices" || base == "vendor" || base == "testdata" {
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
	if strings.TrimSpace(*singleFile) != "" {
		candidate := *singleFile
		if !filepath.IsAbs(candidate) {
			candidate = filepath.Join(*root, candidate)
		}
		candidate, err = filepath.Abs(candidate)
		if err != nil {
			panic(err)
		}
		if st, statErr := os.Stat(candidate); statErr != nil || st.IsDir() {
			panic(fmt.Sprintf("-file is not a Go file: %s", candidate))
		}
		files = []string{candidate}
	}
	if *resume {
		pending := files[:0]
		for _, file := range files {
			rel, _ := filepath.Rel(*root, file)
			name := strings.TrimSuffix(rel, filepath.Ext(rel)) + "." + *format
			if _, statErr := os.Stat(filepath.Join(*out, "semantic-"+*format, name)); statErr == nil {
				continue
			}
			pending = append(pending, file)
		}
		files = pending
	}
	results := make([]result, len(files))
	failureFile, err := os.OpenFile(filepath.Join(*out, "failure_matrix.ndjson"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		panic(err)
	}
	failureLog := &streamingFailureLog{f: failureFile}
	defer failureFile.Close()
	if *workers < 1 {
		*workers = 1
	}
	type packageJob []int
	byDir := make(map[string][]int)
	var dirs []string
	for i, file := range files {
		dir := filepath.Dir(file)
		if _, exists := byDir[dir]; !exists {
			dirs = append(dirs, dir)
		}
		byDir[dir] = append(byDir[dir], i)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if len(byDir[dirs[i]]) != len(byDir[dirs[j]]) {
			return len(byDir[dirs[i]]) > len(byDir[dirs[j]])
		}
		return dirs[i] < dirs[j]
	})
	jobs := make(chan packageJob)
	process := func(i int, p *backend.SemanticProgram, lowerErr error, data []byte, started time.Time) {
		file := files[i]
		rel, _ := filepath.Rel(*root, file)
		sum := sha256.Sum256(data)
		r := result{File: rel, Hash: fmt.Sprintf("%x", sum[:]), Bytes: int64(len(data)), DurationMS: time.Since(started).Milliseconds()}
		if lowerErr != nil {
			r.Status = "FAIL"
			r.Phase = "SOURCE_TO_SEMANTIC"
			if lowerErr == context.DeadlineExceeded {
				r.Status, r.Phase = "TIMEOUT", "TIMEOUT"
			}
			r.Diagnostic = compact(lowerErr.Error())
		} else if x := backend.CompleteCanonicalUASTContracts(p); x != nil {
			r.Status, r.Phase, r.Diagnostic = "FAIL", "UAST_CONTRACT_COMPLETION", compact(x.Error())
		} else {
			r.Modules = append([]string(nil), p.Origin.Modules...)
			name := strings.TrimSuffix(rel, filepath.Ext(rel)) + "." + *format
			r.Output = filepath.Join("semantic-"+*format, name)
			var x error
			if *embedModules {
				r.Embeddings, x = backend.EmbedSemanticModules(p, backend.SemanticModuleEmbeddingOptions{
					BaseDir: *root, StoreRoot: embedStoreRoot, UnitPath: filepath.Join(*out, r.Output), Language: p.Origin.SourceLanguage, NeededOnly: *embedMode == "needed", Registry: embedRegistry,
				})
			}
			var wire []byte
			if x == nil && *format == "se" {
				wire, x = p.MarshalSemanticSEWithSource()
			} else if x == nil {
				wire, x = p.MarshalSemanticJSON()
			}
			if x != nil {
				r.Status, r.Phase, r.Diagnostic = "FAIL", "SEMANTIC_SERIALIZE_OR_EMBED", compact(x.Error())
			} else {
				r.Status, r.Phase = "PASS", "SOURCE_TO_SEMANTIC"
				outputPath := filepath.Join(*out, r.Output)
				if x = os.MkdirAll(filepath.Dir(outputPath), 0755); x == nil {
					x = os.WriteFile(outputPath, wire, 0644)
				}
				if x == nil {
					unitID := filepath.ToSlash(rel)
					var summary backend.SemanticUnitSummary
					summary, x = backend.BuildSemanticUnitSummary(outputPath, unitID)
					if x == nil {
						var summaryBytes []byte
						summaryBytes, x = json.Marshal(summary)
						if x == nil {
							x = os.WriteFile(outputPath+".summary.json", append(summaryBytes, '\n'), 0644)
						}
					}
				}
				if x != nil {
					r.Status, r.Phase, r.Diagnostic = "FAIL", "SEMANTIC_WRITE_OR_SUMMARY", compact(x.Error())
				}
			}
		}
		results[i] = r
		failureLog.record(i, r)
	}
	var wg sync.WaitGroup
	// Package checking is intentionally single-flight. Each checked package
	// then fans out to the configured unit workers, so the requested CPU
	// parallelism is not multiplied by the number of simultaneously checked
	// packages.
	for n := 0; n < 1; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range jobs {
				started := time.Now()
				batch := make([]backend.NativeGoSourceFile, 0, len(job))
				contents := make(map[int][]byte, len(job))
				indexByPath := make(map[string]int, len(job))
				for _, i := range job {
					data, readErr := os.ReadFile(files[i])
					if readErr != nil {
						process(i, nil, readErr, nil, started)
						continue
					}
					contents[i] = data
					batch = append(batch, backend.NativeGoSourceFile{Filename: files[i], Source: string(data)})
					indexByPath[files[i]] = i
				}
				if len(batch) == 0 {
					continue
				}
				if *singleFile != "" && len(batch) == 1 {
					p, lowerErr := lowerWithRetry(batch[0].Filename, batch[0].Source, time.Duration(*timeoutSeconds)*time.Second)
					i := indexByPath[batch[0].Filename]
					process(i, p, lowerErr, contents[i], started)
					continue
				}
				err := backend.LowerNativeGoSourcePackageWithWorkers(batch, *workers, func(unit backend.NativeGoSourceFile, p *backend.SemanticProgram, lowerErr error) {
					i := indexByPath[unit.Filename]
					process(i, p, lowerErr, contents[i], started)
				})
				if err != nil {
					// A directory can contain multiple Go package identities or
					// otherwise be unsuitable for a shared check. Preserve the
					// established per-file semantics for that exceptional group.
					for _, unit := range batch {
						i := indexByPath[unit.Filename]
						p, lowerErr := lowerWithRetry(unit.Filename, unit.Source, time.Duration(*timeoutSeconds)*time.Second)
						process(i, p, lowerErr, contents[i], started)
					}
				}
			}
		}()
	}
	for _, dir := range dirs {
		jobs <- packageJob(byDir[dir])
	}
	close(jobs)
	wg.Wait()
	// Keep module imports in one project-level manifest.  Per-unit output must
	// not embed the same dependency bodies repeatedly; the project compiler can
	// resolve this manifest through its GlobalSemanticIndex/link plan.
	links := make([]map[string]any, 0, len(results))
	for _, r := range results {
		if r.Status != "PASS" || len(r.Modules) == 0 {
			continue
		}
		links = append(links, map[string]any{
			"unit":    r.File,
			"output":  r.Output,
			"imports": r.Modules,
		})
	}
	if b, e := json.MarshalIndent(map[string]any{
		"schema": "semantic-project-module-links.v1",
		"units":  links,
	}, "", "  "); e == nil {
		if e = os.WriteFile(filepath.Join(*out, "module-links.json"), append(b, '\n'), 0644); e != nil {
			panic(e)
		}
	}
	if false && *embedModules {
		storeRoot := *moduleRoot
		if strings.TrimSpace(storeRoot) == "" {
			storeRoot, err = backend.ModuleStoreRoot()
			if err != nil {
				panic(err)
			}
		}
		semanticDir := filepath.Join(*out, "semantic-se")
		registry := backend.NewSemanticModuleEmbeddingRegistry(semanticDir)
		type embeddingReport struct {
			Unit    string                           `json:"unit"`
			Entries []backend.SemanticEmbeddedModule `json:"entries"`
			Error   string                           `json:"error,omitempty"`
		}
		reports := make([]embeddingReport, 0)
		for i := range results {
			if results[i].Status != "PASS" || results[i].Output == "" {
				continue
			}
			path := filepath.Join(*out, results[i].Output)
			data, readErr := os.ReadFile(path)
			report := embeddingReport{Unit: results[i].Output}
			if readErr == nil {
				program, parseErr := backend.ParseSemanticSE(data)
				if parseErr == nil {
					// The frontend keeps import facts on the SemanticProgram boundary;
					// older SE graph serialization could leave the mirrored UAST origin
					// empty. Restore the already measured per-file import facts before
					// embedding, never by rescanning or retranspiling the Go source.
					program.Origin.Modules = append([]string(nil), results[i].Modules...)
					if program.UniversalAST != nil {
						program.UniversalAST.Origin.Modules = append([]string(nil), results[i].Modules...)
					}
					report.Entries, parseErr = backend.EmbedSemanticModules(program, backend.SemanticModuleEmbeddingOptions{
						BaseDir: *root, StoreRoot: storeRoot, UnitPath: path, Registry: registry,
					})
					if parseErr == nil {
						data, parseErr = program.MarshalSemanticSEWithSource()
						if parseErr == nil {
							parseErr = os.WriteFile(path, data, 0644)
						}
					}
				}
				if parseErr != nil {
					report.Error = parseErr.Error()
				}
			} else {
				report.Error = readErr.Error()
			}
			reports = append(reports, report)
		}
		b, e := json.MarshalIndent(map[string]any{"schema": "semantic-module-embedding.v1", "units": reports}, "", "  ")
		if e != nil {
			panic(e)
		}
		if e = os.WriteFile(filepath.Join(*out, "module-embedding.json"), append(b, '\n'), 0644); e != nil {
			panic(e)
		}
	}
	if *embedModules {
		type embeddingReport struct {
			Unit    string                           `json:"unit"`
			Entries []backend.SemanticEmbeddedModule `json:"entries"`
		}
		reports := make([]embeddingReport, 0)
		for _, r := range results {
			if r.Status == "PASS" && len(r.Embeddings) > 0 {
				reports = append(reports, embeddingReport{Unit: r.Output, Entries: r.Embeddings})
			}
		}
		b, e := json.MarshalIndent(map[string]any{"schema": "semantic-module-embedding.v1", "phase": "pre-serialization", "units": reports}, "", "  ")
		if e != nil {
			panic(e)
		}
		if e = os.WriteFile(filepath.Join(*out, "module-embedding.json"), append(b, '\n'), 0644); e != nil {
			panic(e)
		}
	}
	if *license && strings.TrimSpace(*moduleRoot) != "" {
		if _, e := backend.CopyImportedPackageLicenses(*moduleRoot, *out); e != nil {
			panic(e)
		}
	}
	if *license {
		// Preserve the repository license alongside the generated project. This
		// is independent of imported-package notices and remains useful when the
		// shared module store is empty.
		if data, e := os.ReadFile(filepath.Join(*root, "LICENSE")); e == nil {
			licenseDir := filepath.Join(*out, "licenses")
			if e = os.MkdirAll(licenseDir, 0755); e != nil {
				panic(e)
			}
			if e = os.WriteFile(filepath.Join(licenseDir, "PROJECT-LICENSE"), data, 0644); e != nil {
				panic(e)
			}
		}
	}
	families := map[string]map[string]int{}
	for _, r := range results {
		if r.Status != "PASS" {
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
	if limit <= 0 {
		r := <-ch
		return r.p, r.err
	}
	select {
	case r := <-ch:
		return r.p, r.err
	case <-time.After(limit):
		// Keep the per-file bound hard so one pathological frontend analysis
		// cannot stall the complete worker pool. The lowering goroutine is
		// isolated and its result is discarded after the deadline.
		return nil, context.DeadlineExceeded
	}
}

// lowerWithRetry gives complex but finite compiler units a bounded second
// chance. The retry remains isolated per file and is still capped, so a
// pathological unit cannot stall the complete project run.
func lowerWithRetry(file, source string, limit time.Duration) (*backend.SemanticProgram, error) {
	p, err := safeLower(file, source, limit)
	if err != context.DeadlineExceeded {
		return p, err
	}
	if limit <= 0 {
		return p, err
	}
	if limit > 10*time.Minute {
		limit = 10 * time.Minute
	} else {
		limit *= 4
	}
	return safeLower(file, source, limit)
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
	case strings.Contains(l, "timeout") || strings.Contains(l, "deadline exceeded"):
		return "FRONTEND_ANALYSIS_TIMEOUT", "ANALYSIS_BUDGET"
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
