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

func TestRepairSemantic174UnitsFromSource(t *testing.T) {
	inputRoot := filepath.Join("..", "..", "outputs", "pipeline-validation-2026-09-10", "input174")
	sourceRoot := filepath.Join("..", "..", "outputs", "gui-go-se-closure", "go-source")
	if _, err := os.Stat(inputRoot); err != nil {
		t.Skip(err)
	}
	if _, err := os.Stat(sourceRoot); err != nil {
		t.Skip(err)
	}
	outRoot := filepath.Join("..", "..", "outputs", "pipeline-validation-2026-09-10", "input174-repaired")
	if err := os.MkdirAll(outRoot, 0755); err != nil {
		t.Fatal(err)
	}
	type row struct{ unit, source, status, detail string }
	var rows []row
	start := time.Now()
	err := filepath.WalkDir(inputRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.ToLower(filepath.Ext(path)) != ".se" {
			return nil
		}
		rel, err := filepath.Rel(inputRoot, path)
		if err != nil {
			return err
		}
		sourcePath := filepath.Join(sourceRoot, strings.TrimSuffix(rel, filepath.Ext(rel))+".go")
		if _, err := os.Stat(sourcePath); err != nil {
			rows = append(rows, row{rel, sourcePath, "MISSING_SOURCE", err.Error()})
			return nil
		}
		source, err := os.ReadFile(sourcePath)
		if err != nil {
			rows = append(rows, row{rel, sourcePath, "READ_FAIL", err.Error()})
			return nil
		}
		program, err := LowerNativeGo(sourcePath, string(source))
		if err != nil {
			rows = append(rows, row{rel, sourcePath, "LOWER_FAIL", err.Error()})
			return nil
		}
		encoded, err := program.MarshalSemanticSESemanticOnly()
		if err != nil {
			rows = append(rows, row{rel, sourcePath, "EXPORT_FAIL", err.Error()})
			return nil
		}
		destination := filepath.Join(outRoot, rel)
		if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(destination, encoded, 0644); err != nil {
			return err
		}
		rows = append(rows, row{rel, sourcePath, "PASS", fmt.Sprintf("bytes=%d", len(encoded))})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	journal := map[string]any{"input_root": inputRoot, "source_root": sourceRoot, "output_root": outRoot, "units": len(rows), "passed": 0, "failed": 0, "elapsed_micros": time.Since(start).Microseconds()}
	for _, item := range rows {
		if item.status == "PASS" {
			journal["passed"] = journal["passed"].(int) + 1
		} else {
			journal["failed"] = journal["failed"].(int) + 1
		}
	}
	data, _ := json.MarshalIndent(journal, "", "  ")
	_ = os.WriteFile(filepath.Join(outRoot, "repair-journal.json"), append(data, '\n'), 0644)
	file, err := os.Create(filepath.Join(outRoot, "repair-matrix.csv"))
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(file)
	_ = w.Write([]string{"unit", "source", "status", "detail"})
	for _, item := range rows {
		_ = w.Write([]string{item.unit, item.source, item.status, item.detail})
	}
	w.Flush()
	_ = file.Close()
	t.Logf("REPAIR_174 units=%d passed=%d failed=%d output=%s", len(rows), journal["passed"], journal["failed"], outRoot)
}
