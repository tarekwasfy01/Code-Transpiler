// Copyright (c) 2026 Tarek Wasfy
package matrixir

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ExecutionReadyBundle is the immutable authority selected for a frontend
// parse.  The hash covers the table files that determine machine behaviour;
// callers can retain it in diagnostics and semantic provenance.
type ExecutionReadyBundle struct {
	Dir       string   `json:"dir"`
	Hash      string   `json:"hash"`
	Languages []string `json:"languages"`
}

var executionReadyCache = struct {
	sync.RWMutex
	items map[string]ExecutionReadyBundle
}{items: make(map[string]ExecutionReadyBundle)}

// ResolveExecutionReadyBundle deterministically selects one complete bundle.
// An explicit directory wins. Otherwise the first complete candidate in the
// supplied order is used. No language-specific selection is performed.
func ResolveExecutionReadyBundle(explicit string, candidates ...string) (ExecutionReadyBundle, error) {
	paths := append([]string(nil), candidates...)
	if strings.TrimSpace(explicit) != "" {
		paths = append([]string{explicit}, paths...)
	}
	seen := map[string]bool{}
	for _, raw := range paths {
		dir, err := filepath.Abs(raw)
		if err != nil || seen[dir] {
			continue
		}
		seen[dir] = true
		if !executionReadyComplete(dir) {
			continue
		}
		executionReadyCache.RLock()
		cached, found := executionReadyCache.items[dir]
		executionReadyCache.RUnlock()
		if found {
			return cached, nil
		}
		bundle, err := inspectExecutionReadyBundle(dir)
		if err == nil {
			executionReadyCache.Lock()
			executionReadyCache.items[dir] = bundle
			executionReadyCache.Unlock()
			return bundle, nil
		}
	}
	return ExecutionReadyBundle{}, fmt.Errorf("no complete execution-ready frontend bundle")
}

func executionReadyComplete(dir string) bool {
	for _, name := range []string{"lex_modes.csv", "lex_dispatch.csv", "parse_dispatch.csv", "parse_action_lists.csv"} {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || info.IsDir() {
			return false
		}
	}
	return true
}

func inspectExecutionReadyBundle(dir string) (ExecutionReadyBundle, error) {
	names := []string{"lex_modes.csv", "lex_dispatch.csv", "lex_accepts.csv", "parse_dispatch.csv", "parse_action_lists.csv", "symbols.csv", "productions.csv", "field_map.csv"}
	h := sha256.New()
	for _, name := range names {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return ExecutionReadyBundle{}, err
		}
		_, _ = h.Write([]byte(name))
		_, _ = h.Write(data)
	}
	languages := []string{}
	if data, err := os.ReadFile(filepath.Join(dir, "source_coverage.csv")); err == nil {
		for _, line := range strings.Split(string(data), "\n")[1:] {
			fields := strings.Split(line, ",")
			if len(fields) > 0 && strings.TrimSpace(fields[0]) != "" {
				languages = append(languages, strings.TrimSpace(fields[0]))
			}
		}
	}
	sort.Strings(languages)
	return ExecutionReadyBundle{Dir: dir, Hash: hex.EncodeToString(h.Sum(nil)), Languages: uniqueStrings(languages)}, nil
}

func (b ExecutionReadyBundle) MarshalJSON() ([]byte, error) {
	type alias ExecutionReadyBundle
	return json.Marshal(alias(b))
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}
