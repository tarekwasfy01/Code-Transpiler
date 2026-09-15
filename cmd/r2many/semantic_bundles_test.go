// Copyright (c) 2026 Tarek Wasfy
package main

import "testing"

func TestEmbeddedSemanticBundles(t *testing.T) {
	info, err := verifyEmbeddedSemanticBundles()
	if err != nil {
		t.Fatal(err)
	}
	if len(info) != 3 {
		t.Fatalf("got %d bundles, want 3", len(info))
	}
	entries := 0
	for _, item := range info {
		entries += item.Members
	}
	if entries != 176 {
		t.Fatalf("got %d member entries, want 176", entries)
	}
}
