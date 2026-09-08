// Copyright (c) 2026 Tarek Wasfy
package main

import "strings"

func isSemanticTextPath(path string) bool {
	l := strings.ToLower(path)
	return strings.HasSuffix(l, ".se") || strings.HasSuffix(l, ".sp")
}

func isSemanticCompressedPath(path string) bool {
	l := strings.ToLower(path)
	return strings.HasSuffix(l, ".spz")
}

func isSemanticPath(path string) bool {
	return isSemanticTextPath(path) || isSemanticCompressedPath(path)
}

func isSemanticTextFormat(format string) bool {
	f := strings.ToLower(strings.TrimPrefix(format, "."))
	return f == "se" || f == "sp"
}

func isSemanticCompressedFormat(format string) bool {
	f := strings.ToLower(strings.TrimPrefix(format, "."))
	return f == "spz"
}
