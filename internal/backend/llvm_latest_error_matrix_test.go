//go:build integration

// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestLLVMLatestErrorMatrix deliberately compiles every available unit
// independently. It is a diagnostic pass: one bad unit must not hide later
// unit failures, and no failed unit is converted into an object or stub.
func TestLLVMLatestErrorMatrix(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	root := strings.TrimSpace(os.Getenv("SEMANTIC_LLVM_INPUT"))
	if root == "" {
		root = filepath.Join(repoRoot, "outputs", "go-to-se-latest-2026-09-11", "semantic-se")
	}
	if _, err := os.Stat(root); err != nil {
		t.Skipf("semantic export unavailable: %s", root)
	}
	out := strings.TrimSpace(os.Getenv("SEMANTIC_LLVM_MATRIX_OUT"))
	if out == "" {
		out = filepath.Join(repoRoot, "outputs", "llvm-latest-error-matrix-2026-09-11")
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	project, err := LoadSemanticProject(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	partial := filepath.Join(out, "error-matrix.partial.csv")
	final := filepath.Join(out, "error-matrix.csv")
	_ = os.Remove(partial)
	f, err := os.Create(partial)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"unit", "stage", "error_code", "family", "primitive", "contract", "detail"})
	// Persist the schema before any potentially expensive prepass. A killed or
	// timed-out diagnostic process must still leave a readable partial matrix.
	w.Flush()
	type row struct{ Unit, Stage, Code, Family, Primitive, Contract, Detail string }
	var rows []row
	pass := 0
	started := time.Now()
	// Measure the actual production LLVM route: the already lowered canonical
	// UAST is the only semantic input. Source recovery is intentionally not
	// enabled here, so missing contracts remain visible instead of masking the
	// production projection gap with a second frontend run.
	summaryErr := buildSemanticProjectSummaries(project, false)
	if summaryErr != nil {
		// The partial matrix is already open, so a prepass failure survives a
		// timeout or process interruption and cannot hide later unit failures.
		_ = os.WriteFile(filepath.Join(out, "summary-prepass-error.txt"), []byte(summaryErr.Error()+"\n"), 0644)
		t.Logf("LLVM_LATEST_SUMMARY_PREPASS_ERROR=%v", summaryErr)
	}
	if summaryErr != nil {
		r := row{"<project>", "summary", "LLVM_PROJECT_SUMMARY", "PROJECT_INDEX", "summary-prepass", "global-index", summaryErr.Error()}
		rows = append(rows, r)
		_ = w.Write([]string{r.Unit, r.Stage, r.Code, r.Family, r.Primitive, r.Contract, r.Detail})
		w.Flush()
	}
	for _, unit := range project.Units {
		data, readErr := os.ReadFile(unit.Path)
		if readErr != nil {
			r := row{unit.ID, "load", "LLVM_UNIT_READ", "UNIT_IO", "read", "unit-input", readErr.Error()}
			rows = append(rows, r)
			_ = w.Write([]string{r.Unit, r.Stage, r.Code, r.Family, r.Primitive, r.Contract, r.Detail})
			w.Flush()
			continue
		}
		program, loadErr := loadSemanticUnitBytes(unit.Path, data)
		if loadErr != nil {
			r := row{unit.ID, "parse", "LLVM_UNIT_PARSE", "UNIT_PARSE", "semantic-transport", "canonical-unit", loadErr.Error()}
			rows = append(rows, r)
			_ = w.Write([]string{r.Unit, r.Stage, r.Code, r.Family, r.Primitive, r.Contract, r.Detail})
			w.Flush()
			continue
		}
		unitSummary := project.Index.Summaries[unit.ID]
		unitUAST, canonicalErr := canonicalUniversalAST(program)
		if canonicalErr != nil {
			r := row{unit.ID, "canonicalize", "LLVM_UNIT_CANONICAL_UAST", "UAST_CANONICALIZATION", "canonical-uast", "unit-uast", canonicalErr.Error()}
			rows = append(rows, r)
			_ = w.Write([]string{r.Unit, r.Stage, r.Code, r.Family, r.Primitive, r.Contract, r.Detail})
			w.Flush()
			continue
		}
		if projectErr := applyProjectCallableContractsToUAST(unitUAST, unitSummary); projectErr != nil {
			r := row{unit.ID, "uast-contract", "LLVM_UNIT_CONTRACT_PROJECTION", "UAST_CONTRACT", "callable-contract", "project-summary", projectErr.Error()}
			rows = append(rows, r)
			_ = w.Write([]string{r.Unit, r.Stage, r.Code, r.Family, r.Primitive, r.Contract, r.Detail})
			w.Flush()
			continue
		}
		program.UniversalAST = unitUAST
		bindings := semanticFunctionBindings(program)
		for _, summary := range project.Index.Summaries {
			for _, fn := range summary.Functions {
				if fn.Name != "" {
					bindings["native_var_"+fn.Name] = fn.Name
					bindings["native_symbol_"+fn.Name] = fn.Name
				}
			}
		}
		for i, fn := range project.Index.Summaries[unit.ID].Functions {
			if fn.Name != "" && bindings[fmt.Sprintf("native_function_%d", i)] == "" {
				bindings[fmt.Sprintf("native_function_%d", i)] = fn.Name
			}
		}
		compiled, compileErr := CompileLLVM(program, LLVMCompileOptions{
			LLVMPath:     `C:\Program Files\clang+llvm-23.1.1-x86_64-pc-windows-msvc\bin`,
			TargetTriple: "x86_64-pc-windows-msvc", OutputKind: LLVMObject,
			ProjectIndex: &project.Index, UnitID: unit.ID, ProjectBindings: bindings,
		})
		program = nil
		if compileErr != nil {
			code, family, primitive, contract := classifyLLVMProjectError(compileErr.Error())
			r := row{unit.ID, "lower", code, family, primitive, contract, compileErr.Error()}
			rows = append(rows, r)
			_ = w.Write([]string{r.Unit, r.Stage, r.Code, r.Family, r.Primitive, r.Contract, r.Detail})
			w.Flush()
			continue
		}
		_ = compiled
		pass++
	}
	w.Flush()
	if err := w.Error(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(final)
	if err := os.Rename(partial, final); err != nil {
		t.Fatal(err)
	}
	families := map[string]int{}
	for _, r := range rows {
		families[r.Family+"/"+r.Primitive+"/"+r.Contract]++
	}
	keys := make([]string, 0, len(families))
	for k := range families {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ff, err := os.Create(filepath.Join(out, "error-families.csv"))
	if err != nil {
		t.Fatal(err)
	}
	fw := csv.NewWriter(ff)
	_ = fw.Write([]string{"family", "primitive", "contract", "count"})
	for _, k := range keys {
		parts := strings.SplitN(k, "/", 3)
		_ = fw.Write([]string{parts[0], parts[1], parts[2], fmt.Sprint(families[k])})
	}
	fw.Flush()
	_ = ff.Close()
	j := map[string]any{"schema": "llvm-error-matrix.v1", "input_root": root, "units_total": len(project.Units), "objects_success": pass, "objects_failed": len(rows), "error_rows": len(rows), "elapsed_micros": time.Since(started).Microseconds(), "matrix": final, "families": filepath.Join(out, "error-families.csv"), "status": map[bool]string{true: "PASS", false: "FAIL"}[len(rows) == 0]}
	b, _ := json.MarshalIndent(j, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "build-journal.json"), append(b, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	t.Logf("LLVM_LATEST_MATRIX units=%d objects=%d failures=%d matrix=%s", len(project.Units), pass, len(rows), final)
}
