// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDependencyBelongsToGoModuleUsesDeclaredBoundary(t *testing.T) {
	modulePath := "github.com/example/tool/v3"
	tests := []struct {
		dependency string
		want       bool
	}{
		{dependency: modulePath, want: true},
		{dependency: modulePath + "/formatters/html", want: true},
		{dependency: modulePath + "-extra/formatters", want: false},
		{dependency: "github.com/example/other/v3", want: false},
		{dependency: "fmt", want: false},
	}
	for _, tt := range tests {
		if got := dependencyBelongsToGoModule(tt.dependency, modulePath); got != tt.want {
			t.Errorf("dependencyBelongsToGoModule(%q, %q) = %t, want %t", tt.dependency, modulePath, got, tt.want)
		}
	}
}

func TestGoModulePathInTreeFindsArchiveWrapper(t *testing.T) {
	root := t.TempDir()
	moduleDir := filepath.Join(root, "github.com", "example", "tool@v1.2.3")
	if err := os.MkdirAll(moduleDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte("module github.com/example/tool\n\ngo 1.23\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if got := goModulePathInTree(root); got != "github.com/example/tool" {
		t.Fatalf("goModulePathInTree() = %q, want %q", got, "github.com/example/tool")
	}
}

func TestGoDependencyUsesRequiredModuleVersion(t *testing.T) {
	root := t.TempDir()
	goMod := filepath.Join(root, "go.mod")
	contents := "module github.com/example/app\n\ngo 1.23\n\nrequire (\n\tgithub.com/acme/lib/v2 v2.7.1\n\tgithub.com/acme/other v1.4.0 // indirect\n)\n"
	if err := os.WriteFile(goMod, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	requirements := readGoModuleRequirementVersions(goMod)
	modulePath, version, ok := goModuleRequirementForImport("github.com/acme/lib/v2/subpkg", requirements)
	if !ok || modulePath != "github.com/acme/lib/v2" || version != "v2.7.1" {
		t.Fatalf("resolved requirement = (%q, %q, %t), want (%q, %q, true)", modulePath, version, ok, "github.com/acme/lib/v2", "v2.7.1")
	}
	if got := goModuleProxyZipURL(modulePath, version); got != "https://proxy.golang.org/github.com%2Facme%2Flib%2Fv2/@v/v2.7.1.zip" {
		t.Fatalf("proxy archive URL = %q", got)
	}
	if gotModule, gotVersion := splitGoModuleVersion("github.com/acme/lib/v2@v2.7.1"); gotModule != modulePath || gotVersion != version {
		t.Fatalf("split explicit version = (%q, %q)", gotModule, gotVersion)
	}
}

func TestSemanticModuleStoreRoundTrip(t *testing.T) {
	p, err := LowerMatrixLanguage("r", "x <- 1")
	if err != nil {
		t.Fatal(err)
	}
	m, err := NewSemanticModule("fixture", "r", "x <- 1", p)
	if err != nil {
		t.Fatal(err)
	}
	store := SemanticModuleStore{Root: t.TempDir()}
	if err = store.SaveModule(m); err != nil {
		t.Fatal(err)
	}
	opened, err := store.OpenModule(m.CacheKey)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.VerifyModule(m.CacheKey); err != nil {
		t.Fatal(err)
	}
	if opened.Identity != m.Identity || opened.SemanticRoot != m.SemanticRoot {
		t.Fatalf("round trip identity/root mismatch: %#v %#v", opened, m)
	}
	found, err := store.FindModule("fixture")
	if err != nil || found.CacheKey != m.CacheKey {
		t.Fatalf("find failed: %v", err)
	}
	if err = store.RemoveModule(m.CacheKey); err != nil {
		t.Fatal(err)
	}
}

func TestMergeCanonicalUniversalASTRawRemapsAndInternsContracts(t *testing.T) {
	makeContract := func(id int, identity string) SemanticContract {
		payload, err := contractPayload(SemanticSymbolContractKind, SemanticSymbolContract{
			Identity: identity, Linkage: "external", Visibility: "public", ImportExport: "import",
		})
		if err != nil {
			t.Fatal(err)
		}
		return SemanticContract{ID: id, Kind: SemanticSymbolContractKind, Hash: semanticContractHash(SemanticSymbolContractKind, payload), Payload: payload}
	}

	dst := &UniversalASTDocument{
		ContractSchema: semanticContractSchema,
		Nodes:          []UniversalASTNode{{ID: 0, StructuralKind: "Block", Fields: map[string]json.RawMessage{}}},
		ContractTable:  []SemanticContract{makeContract(0, "existing.symbol")},
	}
	src := &UniversalASTDocument{
		ContractSchema: semanticContractSchema,
		Nodes: []UniversalASTNode{
			{ID: 0, StructuralKind: "Module", Fields: map[string]json.RawMessage{}},
			{ID: 1, StructuralKind: "SymbolRef", Fields: map[string]json.RawMessage{}},
		},
		ContractTable: []SemanticContract{
			makeContract(0, "existing.symbol"), // must reuse the destination entry
			makeContract(1, "tkinter.Tk"),      // must be appended and rebased
		},
		ContractRefs: []SemanticContractReference{
			{NodeID: 1, ContractID: 0, Role: "existing"},
			{NodeID: 1, ContractID: 1, Role: "symbol"},
		},
	}

	if err := mergeCanonicalUniversalASTRaw(dst, src); err != nil {
		t.Fatalf("mergeCanonicalUniversalASTRaw: %v", err)
	}
	if got, want := len(dst.ContractTable), 2; got != want {
		t.Fatalf("contract table length = %d, want %d", got, want)
	}
	if got, want := len(dst.ContractRefs), 2; got != want {
		t.Fatalf("contract reference count = %d, want %d", got, want)
	}
	if got, want := dst.ContractRefs[0].NodeID, 2; got != want {
		t.Errorf("first contract node = %d, want rebased node %d", got, want)
	}
	if got, want := dst.ContractRefs[0].ContractID, 0; got != want {
		t.Errorf("deduplicated contract id = %d, want %d", got, want)
	}
	if got, want := dst.ContractRefs[1].NodeID, 2; got != want {
		t.Errorf("second contract node = %d, want rebased node %d", got, want)
	}
	if got, want := dst.ContractRefs[1].ContractID, 1; got != want {
		t.Errorf("new contract id = %d, want %d", got, want)
	}
	if got, want := dst.ContractTable[1].ID, 1; got != want {
		t.Errorf("appended contract table id = %d, want %d", got, want)
	}
}

func TestSemanticModuleDetectionAndExplicitOverride(t *testing.T) {
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "go.mod"), []byte("module fixture\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "main.go"), []byte("package main\n"), 0644); err != nil {
		t.Fatal(err)
	}
	det := DetectArtifact(d)
	if det.Confidence != "DETECTED" || det.Language != "go" {
		t.Fatalf("unexpected detection: %#v", det)
	}
	if err := os.WriteFile(filepath.Join(d, "helper.c"), []byte("int x;\n"), 0644); err != nil {
		t.Fatal(err)
	}
	det = DetectArtifact(d)
	if det.Confidence != "AMBIGUOUS" || !det.Mixed {
		t.Fatalf("mixed directory was guessed: %#v", det)
	}
}
