// Copyright (c) 2026 Tarek Wasfy
// selfhost-audit records the current compiler/module closure. It reports
// evidence only; it does not invent reachability or alter the compiler.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type item struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Kind        string `json:"kind"`
	Size        int64  `json:"bytes"`
	CompileTime bool   `json:"compile_time"`
	Runtime     bool   `json:"runtime"`
	Embedding   string `json:"embedding_strategy"`
	Status      string `json:"status"`
}

func main() {
	root := flag.String("root", ".", "project root")
	out := flag.String("out", "compiler-migration/semantic-selfhost", "audit output")
	witness := flag.String("csharp-witness", "", "optional native C# witness executable to verify")
	flag.Parse()
	var items []item
	_ = filepath.WalkDir(*root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			n := strings.ToLower(d.Name())
			if n == ".git" || n == ".cache" || n == "node_modules" || n == "obj" || n == "bin" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		kind, compile, runtime := "source", true, false
		switch ext {
		case ".go":
			kind = "go_source"
		case ".cs":
			kind = "csharp_source"
		case ".json", ".csv", ".toml", ".yaml", ".yml", ".se", ".sp", ".spz":
			kind, compile, runtime = "static_resource", false, true
		case ".dll", ".exe":
			kind, compile, runtime = "external_binary", false, true
		default:
			return nil
		}
		st, e := d.Info()
		if e != nil {
			return nil
		}
		rel, _ := filepath.Rel(*root, path)
		strategy := "embedded_or_compiled"
		if kind == "external_binary" {
			strategy = "external_dependency_requires_explicit_boundary"
		}
		items = append(items, item{Name: filepath.Base(path), Path: filepath.ToSlash(rel), Kind: kind, Size: st.Size(), CompileTime: compile, Runtime: runtime, Embedding: strategy, Status: "INVENTORIED"})
		return nil
	})
	sort.Slice(items, func(i, j int) bool { return items[i].Path < items[j].Path })
	if e := os.MkdirAll(*out, 0755); e != nil {
		panic(e)
	}
	write := func(name string, value any) {
		b, _ := json.MarshalIndent(value, "", "  ")
		_ = os.WriteFile(filepath.Join(*out, name), append(b, '\n'), 0644)
	}
	write("module-closure.json", map[string]any{"schema_version": "semantic-module-closure-v1", "source_root": *root, "items": items})
	write("closure.json", map[string]any{"schema_version": "semantic-compiler-closure-v1", "source_root": *root, "selection": "existing productive packages and generated migration artifacts", "items": items})
	status := "INVENTORIED_NOT_SELFHOSTED"
	blocker := map[string]any{"kind": "GO_COMPILER_SELFHOST", "status": "OPEN", "evidence": "full compiler self-host generation has not been run"}
	if *witness != "" {
		if _, err := os.Stat(*witness); err == nil {
			cmd := exec.Command(*witness)
			err := cmd.Run()
			code := 0
			if err != nil {
				if ee, ok := err.(*exec.ExitError); ok {
					code = ee.ExitCode()
				} else {
					code = -1
				}
			}
			if code == 42 {
				status = "WITNESS_VALIDATED"
				blocker = map[string]any{"kind": "ENTRY_RETURN_CONTRACT", "status": "CLOSED", "evidence": "CSharp Add/Main witness exited 42"}
			} else {
				blocker = map[string]any{"kind": "ENTRY_RETURN_CONTRACT", "status": "OPEN", "evidence": fmt.Sprintf("CSharp witness exited %d", code)}
			}
		}
	}
	write("generation-status.json", map[string]any{"schema_version": "semantic-selfhost-generation-v1", "status": status, "compiler_core_embedded": false, "external_bridges": []string{"roslyn_bootstrap"}})
	b, _ := json.Marshal(blocker)
	_ = os.WriteFile(filepath.Join(*out, "blockers.jsonl"), append(b, '\n'), 0644)
}
