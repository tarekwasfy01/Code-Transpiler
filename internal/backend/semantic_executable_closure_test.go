// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildExecutableClosureResolvesOnlyReachableSemanticUnits(t *testing.T) {
	entryID := projectFunctionLabel("app.entry")
	calleeID := projectFunctionLabel("lib.value")
	result := SemanticType{Kind: "integer", Bits: 32}
	calleeType := SemanticType{Kind: "function", Parameters: []SemanticType{}, Result: &result}
	index := SemanticProjectIndex{Summaries: map[string]SemanticUnitSummary{
		"app.se": {
			Schema: semanticUnitSummarySchema, UnitID: "app.se", Package: "app",
			Functions: []ProjectFunctionSummary{{ID: entryID, Name: "entry", HasBody: true, Type: calleeType}},
			Calls:     []ProjectCallSummary{{CallerID: entryID, NodeID: 8, TargetID: calleeID, TargetName: "value"}},
		},
		"lib.se": {
			Schema: semanticUnitSummarySchema, UnitID: "lib.se", Package: "lib",
			Functions: []ProjectFunctionSummary{{ID: calleeID, Name: "value", HasBody: true, Type: calleeType}},
		},
		"unused.se": {
			Schema: semanticUnitSummarySchema, UnitID: "unused.se", Package: "unused",
			Functions: []ProjectFunctionSummary{{ID: projectFunctionLabel("unused.dead"), Name: "dead", HasBody: true, Type: calleeType}},
		},
	}}

	closure := BuildExecutableClosure(&index, []string{"entry"})
	if len(closure.Unresolved) != 0 {
		t.Fatalf("unexpected unresolved closure facts: %#v", closure.Unresolved)
	}
	if len(closure.ReachableUnits) != 2 || closure.ReachableUnits[0] != "app.se" || closure.ReachableUnits[1] != "lib.se" {
		t.Fatalf("reachable units=%v, want only app.se and lib.se", closure.ReachableUnits)
	}
	if closure.ReachableFunctionCount != 2 {
		t.Fatalf("reachable functions=%d, want 2", closure.ReachableFunctionCount)
	}
}

func TestSemanticProjectEmbeddedModuleBecomesSeparateUnit(t *testing.T) {
	dir := t.TempDir()
	importer, err := LowerNativeGo(filepath.Join(dir, "main.go"), "package main\nfunc entry() int { return 42 }\n")
	if err != nil {
		t.Fatal(err)
	}
	module, err := LowerNativeGo(filepath.Join(dir, "lib.go"), "package lib\nfunc value() int { return 7 }\n")
	if err != nil {
		t.Fatal(err)
	}
	moduleBytes, err := module.MarshalSemanticSESemanticOnly()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := json.Marshal([]SemanticEmbeddedModule{{
		Identity: "lib", SemanticRoot: stableBytesHash(moduleBytes), Mode: "inline",
		Payload: base64.StdEncoding.EncodeToString(moduleBytes),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if importer.Metadata == nil {
		importer.Metadata = map[string]string{}
	}
	importer.Metadata["semantic_module_embeddings"] = string(entries)
	if importer.UniversalAST.Metadata == nil {
		importer.UniversalAST.Metadata = map[string]string{}
	}
	importer.UniversalAST.Metadata["semantic_module_embeddings"] = string(entries)
	importerBytes, err := importer.MarshalSemanticSESemanticOnly()
	if err != nil {
		t.Fatal(err)
	}
	importerPath := filepath.Join(dir, "main.se")
	if err := os.WriteFile(importerPath, importerBytes, 0600); err != nil {
		t.Fatal(err)
	}
	parsedImporter, err := loadSemanticUnitBytes(importerPath, importerBytes)
	if err != nil {
		t.Fatal(err)
	}
	if parsedImporter.Metadata["semantic_module_embeddings"] == "" {
		t.Fatalf("semantic module embedding metadata did not survive .se serialization: %s", importerBytes[:minInt(len(importerBytes), 512)])
	}
	project := &SemanticProject{Units: []*SemanticCompilationUnit{{ID: "main.se", Path: importerPath}}}
	if err := expandSemanticProjectModules(project, dir, ""); err != nil {
		t.Fatal(err)
	}
	if len(project.Units) != 2 {
		t.Fatalf("expanded semantic units=%d, want importer plus one module unit: %#v", len(project.Units), project.Units)
	}
	if filepath.Base(filepath.Dir(project.Units[1].Path)) != "embedded-units" {
		t.Fatalf("inline module was not materialized in its separate unit cache: %s", project.Units[1].Path)
	}
	project.Index = SemanticProjectIndex{Units: map[string]string{}, Symbols: map[string]ProjectSymbol{}, Dependencies: map[string][]string{}, Summaries: map[string]SemanticUnitSummary{}}
	if err := buildSemanticProjectSummaries(project, false); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, unit := range project.Units {
		for _, function := range project.Index.Summaries[unit.ID].Functions {
			if function.Name == "value" {
				found = true
			}
		}
	}
	if !found {
		t.Fatal("embedded module function was not indexed as an independent project unit")
	}
}

func TestBuildExecutableClosureFailsClosedForUnresolvedCallAndPersistsReport(t *testing.T) {
	entryID := projectFunctionLabel("app.entry")
	result := SemanticType{Kind: "integer", Bits: 32}
	entryType := SemanticType{Kind: "function", Parameters: []SemanticType{}, Result: &result}
	index := SemanticProjectIndex{Summaries: map[string]SemanticUnitSummary{
		"app.se": {
			Schema: semanticUnitSummarySchema, UnitID: "app.se", Package: "app",
			Functions: []ProjectFunctionSummary{{ID: entryID, Name: "entry", HasBody: true, Type: entryType}},
			Calls:     []ProjectCallSummary{{CallerID: entryID, NodeID: 23, TargetName: "missing"}},
		},
	}}
	project := &SemanticProject{Units: []*SemanticCompilationUnit{{ID: "app.se", Path: filepath.Join(t.TempDir(), "app.se")}}, Index: index}
	reportPath := filepath.Join(t.TempDir(), "executable-closure.json")
	closure, err := projectExecutableClosure(project, []string{"entry"}, reportPath)
	if err == nil || !strings.Contains(err.Error(), "EXECUTABLE_CLOSURE_UNRESOLVED") {
		t.Fatalf("expected fail-closed executable closure error, got %v", err)
	}
	if len(closure.Unresolved) != 1 || closure.Unresolved[0].NodeID != 23 {
		t.Fatalf("unresolved report facts=%#v", closure.Unresolved)
	}
	if _, statErr := os.Stat(reportPath); statErr != nil {
		t.Fatalf("closure report was not persisted before failure: %v", statErr)
	}
}

func TestBuildExecutableClosureAcceptsOnlyCompleteExternalContract(t *testing.T) {
	entryID := projectFunctionLabel("app.entry")
	result := SemanticType{Kind: "integer", Bits: 32}
	entryType := SemanticType{Kind: "function", Parameters: []SemanticType{}, Result: &result}
	externalResult := SemanticType{Kind: "integer", Bits: 32}
	external := &ProjectExternalImport{
		Library: "kernel32.dll", Symbol: "GetTickCount", CallingConvention: "win64",
		Parameters: []SemanticType{}, Result: externalResult,
	}
	index := SemanticProjectIndex{Summaries: map[string]SemanticUnitSummary{
		"app.se": {
			Schema: semanticUnitSummarySchema, UnitID: "app.se", Package: "app",
			Functions: []ProjectFunctionSummary{{ID: entryID, Name: "entry", HasBody: true, Type: entryType}},
			Calls:     []ProjectCallSummary{{CallerID: entryID, NodeID: 31, TargetName: "GetTickCount", ExternalImport: external}},
		},
	}}
	closure := BuildExecutableClosure(&index, []string{"entry"})
	if len(closure.Unresolved) != 0 || len(closure.RequiredExternalImports) != 1 {
		t.Fatalf("complete external ABI contract was not retained: unresolved=%#v imports=%#v", closure.Unresolved, closure.RequiredExternalImports)
	}
	external.Parameters = nil
	external.CallingConvention = ""
	closure = BuildExecutableClosure(&index, []string{"entry"})
	if len(closure.Unresolved) != 1 {
		t.Fatalf("incomplete external ABI contract was not rejected: %#v", closure.Unresolved)
	}
}
