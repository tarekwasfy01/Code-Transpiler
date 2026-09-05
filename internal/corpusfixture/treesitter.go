// Package corpusfixture decodes Tree-sitter corpus fixture containers.
// It deliberately knows nothing about any source language; it only separates
// the executable source field from a following expected S-expression.
package corpusfixture

import "strings"

// SplitTreeSitterFixture returns executable source and its expected tree.
// Some corpus exporters store both fields in source_text. A separator is a
// line of three or more hyphens whose next nonblank line begins an S-expression.
// That final condition prevents ordinary minus operators and hyphen comments
// from being treated as a fixture boundary. An explicit expected tree wins,
// while an embedded tree is always removed from source to prevent leakage.
func SplitTreeSitterFixture(source, expected string) (string, string) {
	source = strings.ReplaceAll(strings.ReplaceAll(source, "\r\n", "\n"), "\r", "\n")
	lines := strings.Split(source, "\n")
	for i, line := range lines {
		if !hyphenSeparator(line) {
			continue
		}
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}
		if j >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[j]), "(") {
			continue
		}
		embedded := strings.TrimSpace(strings.Join(lines[j:], "\n"))
		if strings.TrimSpace(expected) == "" {
			expected = embedded
		}
		return strings.TrimRight(strings.Join(lines[:i], "\n"), "\n"), expected
	}
	return source, expected
}

func hyphenSeparator(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 3 {
		return false
	}
	for _, r := range t {
		if r != '-' {
			return false
		}
	}
	return true
}
