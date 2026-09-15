//go:build integration

// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLLVMProject174Measurement(t *testing.T) {
	root := filepath.Join("..", "..", "outputs", "pipeline-validation-2026-09-10", "input174")
	repairedRoot := filepath.Join("..", "..", "outputs", "pipeline-validation-2026-09-10", "input174-repaired")
	if _, err := os.Stat(filepath.Join(repairedRoot, "repair-journal.json")); err == nil {
		root = repairedRoot
	}
	if _, err := os.Stat(root); err != nil {
		t.Skip("174-unit fixture set unavailable")
	}
	project, err := LoadSemanticProject(root, "main")
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	out := filepath.Join("..", "..", "outputs", "llvm-project-174-2026-09-10")
	if err := os.MkdirAll(out, 0755); err != nil {
		t.Fatal(err)
	}
	finalMatrixPath := filepath.Join(out, "error-matrix.csv")
	partialMatrixPath := filepath.Join(out, "error-matrix.partial.csv")
	_ = os.Remove(partialMatrixPath)
	matrixFile, err := os.Create(partialMatrixPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = matrixFile.Close()
	}()
	matrixWriter := csv.NewWriter(matrixFile)
	_ = matrixWriter.Write([]string{"unit", "stage", "error_code", "family", "primitive", "contract", "detail"})
	writeRow := func(row [7]string) {
		_ = matrixWriter.Write(row[:])
		matrixWriter.Flush()
	}
	if err := buildSemanticProjectSummaries(project, true); err != nil {
		writeRow([7]string{"<project>", "summary", "LLVM_PROJECT_SUMMARY", "PROJECT_INDEX", "summary-prepass", "global-index", err.Error()})
		_ = matrixFile.Close()
		t.Fatal(err)
	}
	result := LLVMProjectResult{}
	type matrixRow struct{ unit, stage, code, family, primitive, contract, detail string }
	var rows []matrixRow
	for _, unit := range project.Units {
		data, readErr := os.ReadFile(unit.Path)
		if readErr != nil {
			row := matrixRow{unit.ID, "load", "LLVM_UNIT_READ", "UNIT_IO", "read", "unit-input", readErr.Error()}
			rows = append(rows, row)
			writeRow([7]string{row.unit, row.stage, row.code, row.family, row.primitive, row.contract, row.detail})
			continue
		}
		program, loadErr := loadSemanticUnitBytes(unit.Path, data)
		if loadErr != nil {
			row := matrixRow{unit.ID, "parse", "LLVM_UNIT_PARSE", "UNIT_PARSE", "semantic-transport", "canonical-unit", loadErr.Error()}
			rows = append(rows, row)
			writeRow([7]string{row.unit, row.stage, row.code, row.family, row.primitive, row.contract, row.detail})
			continue
		}
		unitOpts := LLVMCompileOptions{LLVMPath: `C:\Program Files\clang+llvm-23.1.1-x86_64-pc-windows-msvc\bin`, TargetTriple: "x86_64-pc-windows-msvc", OutputKind: LLVMObject, ProjectIndex: &project.Index, UnitID: unit.ID, ProjectBindings: semanticFunctionBindings(program)}
		for _, summary := range project.Index.Summaries {
			for _, fn := range summary.Functions {
				if fn.Name != "" {
					unitOpts.ProjectBindings["native_var_"+fn.Name] = fn.Name
					unitOpts.ProjectBindings["native_symbol_"+fn.Name] = fn.Name
				}
			}
		}
		for i, fn := range project.Index.Summaries[unit.ID].Functions {
			if fn.Name != "" && unitOpts.ProjectBindings[fmt.Sprintf("native_function_%d", i)] == "" {
				unitOpts.ProjectBindings[fmt.Sprintf("native_function_%d", i)] = fn.Name
			}
		}
		compiled, compileErr := CompileLLVM(program, unitOpts)
		program = nil
		if compileErr != nil {
			code, family, primitive, contract := classifyLLVMProjectError(compileErr.Error())
			row := matrixRow{unit.ID, "lower", code, family, primitive, contract, compileErr.Error()}
			rows = append(rows, row)
			writeRow([7]string{row.unit, row.stage, row.code, row.family, row.primitive, row.contract, row.detail})
			continue
		}
		result.ObjectCount++
		result.Fragments = append(result.Fragments, LLVMObjectFragment{UnitID: unit.ID, SemanticRoot: unit.SemanticRoot, Object: compiled.Bytes})
	}
	journal := map[string]any{"units": len(project.Units), "object_count": result.ObjectCount, "cache_hits": result.CacheHits, "cache_misses": result.CacheMisses, "total_micros": time.Since(start).Microseconds(), "status": "PASS", "error_rows": len(rows)}
	if len(rows) > 0 {
		journal["status"] = "FAIL"
	}
	data, _ := json.MarshalIndent(journal, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "build-journal.json"), append(data, '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	matrixWriter.Flush()
	if err := matrixWriter.Error(); err != nil {
		_ = matrixFile.Close()
		t.Fatal(err)
	}
	if err := matrixFile.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(finalMatrixPath); err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	if err := os.Rename(partialMatrixPath, finalMatrixPath); err != nil {
		t.Fatal(err)
	}
	if len(rows) > 0 {
		t.Logf("LLVM_174_STATUS=FAIL units=%d objects=%d error_rows=%d matrix=%s partial=%s", len(project.Units), result.ObjectCount, len(rows), finalMatrixPath, partialMatrixPath)
		return
	}
	if result.ObjectCount != len(project.Units) {
		t.Fatalf("objects=%d units=%d", result.ObjectCount, len(project.Units))
	}
}

func classifyLLVMProjectError(message string) (code, family, primitive, contract string) {
	upper := strings.ToUpper(message)
	switch {
	case strings.Contains(upper, "LLVM_CALL_ABI_CONTRACT_MISMATCH"):
		return "LLVM_CALL_ABI_CONTRACT_MISMATCH", "CALL_ABI", "arity-check", "callable-signature"
	case strings.Contains(upper, "LLVM_CALL_ABI_CONTRACT_MISSING"):
		return "LLVM_CALL_ABI_CONTRACT_MISSING", "CALL_ABI", "contract-presence", "callable-signature"
	case strings.Contains(upper, "LLVM_PROJECTION_GAP"):
		return "LLVM_PROJECTION_GAP", "UAST_PROJECTION", "matrix-cell", "uast-to-llvm"
	case strings.Contains(upper, "LLVM_EXTERNAL"):
		return "LLVM_EXTERNAL_CONTRACT", "EXTERNAL_ABI", "external-resolution", "external-symbol"
	default:
		return "LLVM_UNIT_COMPILE", "UNIT_COMPILE", "compile-error", "unit-contract"
	}
}
