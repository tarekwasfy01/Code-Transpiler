// Copyright (c) 2026 Tarek Wasfy
package main

import "strings"

import "github.com/tarekwasfy01/Code-Transpiler/v2/internal/backend"

func isSemanticTextPath(path string) bool {
	l := strings.ToLower(path)
	return strings.HasSuffix(l, ".se") || strings.HasSuffix(l, ".sp") || strings.HasSuffix(l, ".json")
}

func isSemanticCompressedPath(path string) bool {
	l := strings.ToLower(path)
	return strings.HasSuffix(l, ".spz")
}

func isSemanticPath(path string) bool {
	l := strings.ToLower(path)
	return isSemanticTextPath(path) || isSemanticCompressedPath(path) || strings.HasSuffix(l, ".smod")
}

func isSemanticTextFormat(format string) bool {
	f := strings.ToLower(strings.TrimPrefix(format, "."))
	return f == "se" || f == "sp"
}

func isSemanticCompressedFormat(format string) bool {
	f := strings.ToLower(strings.TrimPrefix(format, "."))
	return f == "spz"
}

// parseSemanticInput is the single extension-aware reader used by every
// Semantic CLI action.  Keeping .se explicit prevents it from being treated
// as a legacy .sp payload when commands are invoked through the sp alias.
func parseSemanticInput(path string, data []byte) (*backend.SemanticProgram, error) {
	if strings.HasSuffix(strings.ToLower(path), ".smod") {
		return backend.MergeSemanticFiles([]string{path})
	}
	if isSemanticCompressedPath(path) {
		return backend.ParseSemanticSPZ(data)
	}
	if strings.HasSuffix(strings.ToLower(path), ".se") {
		return backend.ParseSemanticSE(data)
	}
	if strings.HasSuffix(strings.ToLower(path), ".json") || strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		return backend.ParseSemanticJSON(data)
	}
	return backend.ParseSemanticSP(data)
}
