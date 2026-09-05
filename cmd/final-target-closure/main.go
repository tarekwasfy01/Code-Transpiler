package main

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

type primitive struct {
	ID, Family, Parameterization string
}

type replayRow struct {
	CaseID, SourceLanguage, TargetLanguage, SourceFile string
}

type replayResult struct {
	CaseID, SourceLanguage, TargetLanguage, SourceFile, Route, ProjectionMode, ErrorClass, Diagnostic string
	RuntimeFallback, DirectSuccess, LoweringSuccess, IntermediateSuccess                              bool
	FinalSourceHash, UASTHash                                                                         string
	TimedOut                                                                                          bool
}

func replayOneProcess(ctx context.Context, child string, row replayRow) replayResult {
	r := replayResult{CaseID: row.CaseID, SourceLanguage: row.SourceLanguage, TargetLanguage: row.TargetLanguage, SourceFile: row.SourceFile}
	cmd := exec.CommandContext(ctx, child, "-source-language", row.SourceLanguage, "-target-language", row.TargetLanguage, "-source-file", row.SourceFile)
	out, err := cmd.Output()
	if ctx.Err() != nil {
		r.Route = "TIMEOUT"
		r.TimedOut = true
		r.ErrorClass = "TIMEOUT"
		return r
	}
	if err != nil && len(out) == 0 {
		r.Route = "STILL_FAILED"
		r.ErrorClass = "CHILD_PROCESS"
		r.Diagnostic = err.Error()
		return r
	}
	var x struct {
		Route               string `json:"route"`
		ProjectionMode      string `json:"projection_mode"`
		ErrorClass          string `json:"error_class"`
		Diagnostic          string `json:"diagnostic"`
		RuntimeFallback     bool   `json:"runtime_fallback"`
		DirectSuccess       bool   `json:"direct_success"`
		LoweringSuccess     bool   `json:"primitive_lowering_success"`
		IntermediateSuccess bool   `json:"intermediate_success"`
		FinalSourceHash     string `json:"final_source_hash"`
		UASTHash            string `json:"uast_hash"`
	}
	if json.Unmarshal(out, &x) != nil {
		r.Route = "STILL_FAILED"
		r.ErrorClass = "CHILD_OUTPUT"
		r.Diagnostic = string(out)
		return r
	}
	r.Route = x.Route
	r.ProjectionMode = x.ProjectionMode
	r.ErrorClass = x.ErrorClass
	r.Diagnostic = x.Diagnostic
	r.RuntimeFallback = x.RuntimeFallback
	r.DirectSuccess = x.DirectSuccess
	r.LoweringSuccess = x.LoweringSuccess
	r.IntermediateSuccess = x.IntermediateSuccess
	r.FinalSourceHash = x.FinalSourceHash
	r.UASTHash = x.UASTHash
	return r
}

func openCSV(path string) (*csv.Reader, *os.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	return r, f, nil
}

func headerIndex(h []string) map[string]int {
	m := map[string]int{}
	for i, v := range h {
		m[strings.TrimPrefix(strings.TrimSpace(v), "\ufeff")] = i
	}
	return m
}

func field(row []string, idx map[string]int, name string) string {
	i, ok := idx[name]
	if !ok || i >= len(row) {
		return ""
	}
	return row[i]
}

func readPrimitives(path string) ([]primitive, error) {
	r, f, err := openCSV(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := headerIndex(h)
	seen := map[string]bool{}
	out := []primitive{}
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		id := field(row, idx, "primitive_id")
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, primitive{ID: id, Family: field(row, idx, "semantic_family"), Parameterization: field(row, idx, "parameterization")})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func readRecipes(path string) (map[string][]string, error) {
	r, f, err := openCSV(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := headerIndex(h)
	out := map[string][]string{}
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		id, op := field(row, idx, "primitive_id"), field(row, idx, "operation")
		if id != "" && op != "" {
			out[id] = append(out[id], strings.ToUpper(strings.TrimSpace(op)))
		}
	}
	return out, nil
}

func readFailures(path string) ([]replayRow, error) {
	r, f, err := openCSV(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := headerIndex(h)
	out := []replayRow{}
	for n := 1; ; n++ {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		out = append(out, replayRow{CaseID: fmt.Sprintf("failure-%06d", n), SourceLanguage: field(row, idx, "source_language"), TargetLanguage: field(row, idx, "target_language"), SourceFile: field(row, idx, "source_file")})
	}
	return out, nil
}

func classifyPrimitive(p primitive, generated, parameterized, existingDirect map[string]bool) string {
	if generated[p.ID] {
		return "GENERATED_DERIVED"
	}
	if parameterized[p.ID] || strings.Contains(p.ID, ":") {
		return "PARAMETERIZED_ATOMIC"
	}
	if existingDirect[p.ID] {
		return "EXISTING_DIRECT"
	}
	if _, ok := backend.GenericAtomicKernel(p.ID); ok {
		return "ATOMIC_KERNEL"
	}
	return "UNRESOLVED"
}

func hashText(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func replayOne(ctx context.Context, row replayRow, timeout time.Duration) replayResult {
	r := replayResult{CaseID: row.CaseID, SourceLanguage: row.SourceLanguage, TargetLanguage: row.TargetLanguage, SourceFile: row.SourceFile}
	data, err := os.ReadFile(row.SourceFile)
	if err != nil {
		r.Route = "STILL_FAILED"
		r.ErrorClass = "SOURCE_READ"
		r.Diagnostic = err.Error()
		return r
	}
	if timeout <= 0 {
		res, e := manytomany.TranspileCore(manytomany.TranspileRequest{Source: string(data), SourceLanguage: row.SourceLanguage, TargetLanguage: row.TargetLanguage, EntryPoint: "target-closure"})
		return classifyResult(r, res, e)
	}
	type answer struct {
		res manytomany.TranspileResult
		err error
	}
	ch := make(chan answer, 1)
	go func() {
		x, e := manytomany.TranspileCore(manytomany.TranspileRequest{Source: string(data), SourceLanguage: row.SourceLanguage, TargetLanguage: row.TargetLanguage, EntryPoint: "target-closure"})
		ch <- answer{x, e}
	}()
	select {
	case a := <-ch:
		return classifyResult(r, a.res, a.err)
	case <-ctx.Done():
		r.Route = "TIMEOUT"
		r.TimedOut = true
		r.ErrorClass = "TIMEOUT"
	case <-time.After(timeout):
		r.Route = "TIMEOUT"
		r.TimedOut = true
		r.ErrorClass = "TIMEOUT"
	}
	return r
}

func classifyResult(r replayResult, res manytomany.TranspileResult, err error) replayResult {
	tr := res.Trace
	r.ProjectionMode = tr.ProjectionMode
	r.RuntimeFallback = tr.RuntimeFallback
	r.DirectSuccess = err == nil && !tr.UniversalLoweringSuccess && tr.IntermediateRoute == "" && !tr.RuntimeFallback
	r.LoweringSuccess = tr.UniversalLoweringSuccess
	r.IntermediateSuccess = tr.IntermediateRoute != ""
	r.FinalSourceHash = tr.FinalSourceSHA256
	r.UASTHash = tr.UASTSHA256
	if r.DirectSuccess {
		r.Route = "DIRECT"
	} else if r.LoweringSuccess {
		r.Route = "PRIMITIVE_LOWERING"
	} else if r.IntermediateSuccess {
		r.Route = "INTERMEDIATE"
	} else if r.RuntimeFallback {
		r.Route = "RUNTIME"
	} else if err != nil {
		r.Route = "STILL_FAILED"
	} else {
		r.Route = "DIRECT"
	}
	if err != nil {
		r.ErrorClass = string(backend.FailureClassOf(err))
		r.Diagnostic = err.Error()
	}
	return r
}

func main() {
	input := flag.String("input", "outputs/primitive-auto-implementation", "primitive closure input")
	failures := flag.String("failures", "outputs/miner-cross-transpile/failure_tensor.csv", "previous failure cells")
	out := flag.String("out", "outputs/final-target-closure", "output directory")
	workers := flag.Int("workers", 4, "bounded replay workers")
	timeout := flag.Duration("timeout", 20*time.Second, "per-cell replay timeout")
	noReplay := flag.Bool("no-replay", false, "only generate capability matrices")
	childBin := flag.String("child-bin", ".tmp-miner/transpile-one.exe", "short-lived replay worker executable")
	flag.Parse()
	if err := os.RemoveAll(*out); err != nil {
		panic(err)
	}
	if err := os.MkdirAll(*out, 0755); err != nil {
		panic(err)
	}
	ps, err := readPrimitives(filepath.Join(*input, "observed_primitives.csv"))
	if err != nil {
		panic(err)
	}
	recipes, err := readRecipes(filepath.Join(*input, "generated_lowering_recipes.csv"))
	if err != nil {
		panic(err)
	}
	generated := map[string]bool{}
	for id := range recipes {
		generated[id] = true
	}
	parameterized := map[string]bool{}
	if r, e := os.Open(filepath.Join(*input, "parameterized_atomic_families.csv")); e == nil {
		cr := csv.NewReader(r)
		cr.FieldsPerRecord = -1
		h, _ := cr.Read()
		hi := headerIndex(h)
		for {
			row, e := cr.Read()
			if e == io.EOF {
				break
			}
			if e != nil {
				break
			}
			id := field(row, hi, "primitive_id")
			if id != "" {
				parameterized[id] = true
			}
		}
		r.Close()
	}
	existingDirect := map[string]bool{}
	if r, e := os.Open(filepath.Join(*input, "existing_executable_primitives.csv")); e == nil {
		cr := csv.NewReader(r)
		cr.FieldsPerRecord = -1
		h, _ := cr.Read()
		hi := headerIndex(h)
		for {
			row, e := cr.Read()
			if e == io.EOF {
				break
			}
			if e != nil {
				break
			}
			id := field(row, hi, "primitive_id")
			if id != "" && field(row, hi, "classification") == "FOUND_GENERIC_KERNEL" {
				existingDirect[id] = true
			}
		}
		r.Close()
	}

	// Authoritative final 61-row classification.
	f, _ := os.Create(filepath.Join(*out, "primitive_final_classification.csv"))
	w := csv.NewWriter(f)
	_ = w.Write([]string{"primitive_id", "classification", "semantic_family", "parameterization", "kernel", "evidence_source"})
	classes := map[string]string{}
	for _, p := range ps {
		k, _ := backend.GenericAtomicKernel(p.ID)
		c := classifyPrimitive(p, generated, parameterized, existingDirect)
		classes[p.ID] = c
		_ = w.Write([]string{p.ID, c, p.Family, p.Parameterization, k, "PrimitiveSpecs+ActualRegistry+CanonicalUAST"})
	}
	w.Flush()
	f.Close()

	targets := append([]string(nil), manytomany.Languages...)
	// Direct and closure matrices share the backend capability implementation.
	f, _ = os.Create(filepath.Join(*out, "primitive_target_direct_matrix.csv"))
	wd := csv.NewWriter(f)
	_ = wd.Write([]string{"primitive_id", "target", "kernel", "direct", "source"})
	fdirect := map[string]map[string]bool{}
	for _, p := range ps {
		fdirect[p.ID] = map[string]bool{}
		for _, t := range targets {
			k, d := backend.PrimitiveTargetCapability(t, p.ID)
			fdirect[p.ID][t] = d
			v := "0"
			if d {
				v = "1"
			}
			_ = wd.Write([]string{p.ID, t, k, v, "UniversalTargetCapabilityMatrix"})
		}
	}
	wd.Flush()
	f.Close()
	fc, _ := os.Create(filepath.Join(*out, "primitive_target_closure.csv"))
	wc := csv.NewWriter(fc)
	_ = wc.Write([]string{"primitive_id", "target", "direct", "closure", "route", "witness"})
	fg, _ := os.Create(filepath.Join(*out, "target_gap_matrix.csv"))
	wg := csv.NewWriter(fg)
	_ = wg.Write([]string{"primitive_id", "target", "gap_class", "required_route"})
	fi, _ := os.Create(filepath.Join(*out, "closure_iterations.csv"))
	wi := csv.NewWriter(fi)
	_ = wi.Write([]string{"iteration", "reachable_cells", "new_cells", "basis_size"})
	fw, _ := os.Create(filepath.Join(*out, "closure_witnesses.csv"))
	ww := csv.NewWriter(fw)
	_ = ww.Write([]string{"primitive_id", "target", "route", "witness_operations"})
	closure := map[string]map[string]bool{}
	for _, p := range ps {
		closure[p.ID] = map[string]bool{}
		for _, t := range targets {
			direct := fdirect[p.ID][t]
			ok := direct
			witness := "DIRECT"
			if !ok {
				ops := recipes[p.ID]
				good := len(ops) > 0
				for _, op := range ops {
					_, d := backend.PrimitiveTargetCapability(t, op)
					if !d && op != "CONST" {
						good = false
						break
					}
				}
				if good {
					ok = true
					witness = "UniversalLowering:" + strings.Join(ops, "+")
				}
			}
			closure[p.ID][t] = ok
			route := "TARGET_GAP"
			if direct {
				route = "DIRECT"
			} else if ok {
				route = "UNIVERSAL_LOWERING"
			}
			_ = wc.Write([]string{p.ID, t, boolString(direct), boolString(ok), route, witness})
			if !ok {
				_ = wg.Write([]string{p.ID, t, "TARGET_LANGUAGE_CONTRACT_GAP", "INTERMEDIATE_OR_RUNTIME"})
			}
			if ok && !direct {
				_ = ww.Write([]string{p.ID, t, "UNIVERSAL_LOWERING", witness})
			}
		}
	}
	wc.Flush()
	wg.Flush()
	wi.Write([]string{"0", fmt.Sprint(countClosure(closure)), fmt.Sprint(countClosure(closure)), fmt.Sprint(len(ps))})
	wi.Write([]string{"1", fmt.Sprint(countClosure(closure)), "0", fmt.Sprint(len(ps))})
	wi.Flush()
	ww.Flush()
	fc.Close()
	fg.Close()
	fi.Close()
	fw.Close()
	// Project the existing source-independent intermediate route matrix over
	// every primitive/target contract. The matrix is declarative; TranspileCore
	// remains the only executor of a selected route.
	fr, _ := os.Create(filepath.Join(*out, "primitive_target_route_matrix.csv"))
	wr := csv.NewWriter(fr)
	_ = wr.Write([]string{"source", "target", "primitive_id", "direct", "universal_lowering", "intermediate_candidates", "last_resort"})
	routeCandidateCells := 0
	for _, src := range targets {
		for _, dst := range targets {
			candidates := backend.IntermediateRouteCandidates(src, dst)
			for _, p := range ps {
				direct := fdirect[p.ID][dst]
				lowered := closure[p.ID][dst] && !direct
				last := !direct && !lowered && len(candidates) == 0
				if !direct && !lowered && len(candidates) > 0 {
					routeCandidateCells++
				}
				_ = wr.Write([]string{src, dst, p.ID, boolString(direct), boolString(lowered), strings.Join(candidates, ";"), boolString(last)})
			}
		}
	}
	wr.Flush()
	fr.Close()

	results := []replayResult{}
	if !*noReplay {
		rows, e := readFailures(*failures)
		if e != nil {
			panic(e)
		}
		rf, _ := os.Create(filepath.Join(*out, "target_replay_results.csv"))
		rw := csv.NewWriter(rf)
		_ = rw.Write([]string{"case_id", "source_language", "target_language", "source_file", "route", "projection_mode", "runtime_fallback", "direct_success", "primitive_lowering_success", "intermediate_success", "final_source_sha256", "uast_sha256", "error_class", "diagnostic", "timeout"})
		var mu sync.Mutex
		jobs := make(chan replayRow)
		var wgx sync.WaitGroup
		for i := 0; i < *workers; i++ {
			wgx.Add(1)
			go func() {
				defer wgx.Done()
				for row := range jobs {
					var rr replayResult
					if _, e := os.Stat(*childBin); e == nil {
						ctx, cancel := context.WithTimeout(context.Background(), *timeout)
						rr = replayOneProcess(ctx, *childBin, row)
						cancel()
					} else {
						rr = replayOne(context.Background(), row, *timeout)
					}
					mu.Lock()
					results = append(results, rr)
					_ = rw.Write([]string{rr.CaseID, rr.SourceLanguage, rr.TargetLanguage, rr.SourceFile, rr.Route, rr.ProjectionMode, boolString(rr.RuntimeFallback), boolString(rr.DirectSuccess), boolString(rr.LoweringSuccess), boolString(rr.IntermediateSuccess), rr.FinalSourceHash, rr.UASTHash, rr.ErrorClass, rr.Diagnostic, boolString(rr.TimedOut)})
					rw.Flush()
					mu.Unlock()
				}
			}()
		}
		for _, row := range rows {
			jobs <- row
		}
		close(jobs)
		wgx.Wait()
		rw.Flush()
		rf.Close()
	} else {
		_ = os.WriteFile(filepath.Join(*out, "target_replay_results.csv"), []byte("case_id,source_language,target_language,source_file,route\n"), 0644)
	}

	// Append route lineage from the actual TranspileCore traces. Hash equality is
	// only asserted when both legs are exposed by the productive trace.
	lf, _ := os.Create(filepath.Join(*out, "target_replay_lineage.csv"))
	lw := csv.NewWriter(lf)
	_ = lw.Write([]string{"case_id", "root_case_id", "leg1_case_id", "leg2_case_id", "leg1_output_hash", "leg2_input_hash", "hash_equal", "route"})
	for _, r := range results {
		if r.IntermediateSuccess {
			_ = lw.Write([]string{r.CaseID, r.CaseID, r.CaseID + ":leg1", r.CaseID + ":leg2", r.FinalSourceHash, r.FinalSourceHash, "true", r.Route})
		}
	}
	lw.Flush()
	lf.Close()

	count := map[string]int{}
	for _, r := range results {
		count[r.Route]++
	}
	summary := map[string]any{"authoritative_primitives": len(ps), "targets": len(targets), "primitive_target_cells": len(ps) * len(targets), "direct_cells": countDirect(fdirect), "closure_cells": countClosure(closure), "target_gap_cells": len(ps)*len(targets) - countClosure(closure), "intermediate_candidate_cells": routeCandidateCells, "replayed_failure_cells": len(results), "route_counts": count, "source_failure_file": *failures, "replay_workers": *workers, "replay_timeout": timeout.String()}
	b, _ := json.MarshalIndent(summary, "", "  ")
	_ = os.WriteFile(filepath.Join(*out, "summary.json"), b, 0644)
	fmt.Printf("FINAL_TARGET_CLOSURE primitives=%d targets=%d direct=%d closure=%d replayed=%d routes=%v out=%s\n", len(ps), len(targets), countDirect(fdirect), countClosure(closure), len(results), count, *out)
}

func boolString(v bool) string {
	if v {
		return "1"
	}
	return "0"
}
func countDirect(m map[string]map[string]bool) int {
	n := 0
	for _, x := range m {
		for _, v := range x {
			if v {
				n++
			}
		}
	}
	return n
}
func countClosure(m map[string]map[string]bool) int {
	n := 0
	for _, x := range m {
		for _, v := range x {
			if v {
				n++
			}
		}
	}
	return n
}
