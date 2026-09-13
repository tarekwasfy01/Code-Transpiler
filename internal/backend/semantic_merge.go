// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MergeSemanticFiles combines semantic transports and .smod packages into a
// single canonical SemanticProgram. Input order is preserved for deterministic
// declaration and evaluation ordering.
func MergeSemanticFiles(paths []string) (*SemanticProgram, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("no semantic inputs")
	}
	var out *SemanticProgram
	for _, path := range paths {
		p, err := readSemanticInput(path)
		if err != nil {
			return nil, err
		}
		if out == nil {
			out = p
		} else {
			mergeSemanticPrograms(out, p)
		}
	}
	return out, nil
}

func readSemanticInput(path string) (*SemanticProgram, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".smod" {
		var m SemanticPackageManifest
		if err = json.Unmarshal(b, &m); err != nil {
			return nil, err
		}
		var parts []string
		for _, f := range m.Files {
			if f.Status == "SEMANTIC_READY" && f.SemanticPath != "" {
				parts = append(parts, filepath.Join(filepath.Dir(path), filepath.FromSlash(f.SemanticPath)))
			}
		}
		return MergeSemanticFiles(parts)
	}
	switch ext {
	case ".spz":
		return ParseSemanticSPZ(b)
	case ".sp":
		return ParseSemanticSP(b)
	case ".se":
		return ParseSemanticSE(b)
	case ".json":
		return ParseSemanticJSON(b)
	default:
		return nil, fmt.Errorf("unsupported semantic input %q", path)
	}
}
