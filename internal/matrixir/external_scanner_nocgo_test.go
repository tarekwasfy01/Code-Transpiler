// Copyright (c) 2026 Tarek Wasfy
//go:build !cgo

package matrixir

import (
	"strings"
	"testing"
)

func TestExternalScannerFactoryWithoutCGO(t *testing.T) {
	scanner, err := newExternalScannerRuntime("test-language")
	if scanner != nil {
		t.Fatalf("pure-Go factory returned a scanner: %#v", scanner)
	}
	if err == nil || !strings.Contains(err.Error(), "cgo disabled") {
		t.Fatalf("pure-Go factory error = %v, want explicit cgo-disabled capability error", err)
	}
}
