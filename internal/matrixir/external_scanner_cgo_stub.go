// Copyright (c) 2026 Tarek Wasfy
//go:build !cgo

package matrixir

import "fmt"

// The table-driven parser remains available in pure-Go builds. Only the
// optional external-scanner backend is unavailable.
func newPlatformExternalScanner(language string) (externalScannerRuntime, error) {
	return nil, fmt.Errorf("external scanner unavailable for %s: cgo disabled", language)
}
