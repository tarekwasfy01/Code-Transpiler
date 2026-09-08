// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"os"
	"path/filepath"
	"testing"
)

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
