// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type roslynBoundWire struct {
	Schema       string           `json:"schema"`
	ParseSuccess bool             `json:"parse_success"`
	BindSuccess  bool             `json:"bind_success"`
	SourceHash   string           `json:"source_sha256"`
	Nodes        []map[string]any `json:"nodes"`
	Diagnostics  []map[string]any `json:"diagnostics"`
}

// RoslynBoundToSemantic runs the external Roslyn bridge and enriches the
// existing canonical C# frontend result with bound facts. The bridge is an
// adapter only; UniversalAST/SemanticProgram remains the sole semantic carrier.
func RoslynBoundToSemantic(filename, source string) (*SemanticProgram, error) {
	wire, err := runRoslynBoundAdapter(filename, source)
	if err != nil {
		return nil, err
	}
	if !wire.ParseSuccess || !wire.BindSuccess {
		return nil, fmt.Errorf("CSC_SEMANTIC_BIND_UNRESOLVED: diagnostics=%v", wire.Diagnostics)
	}
	// Prefer the structured Roslyn projection when bound records are
	// available.  The generic matrix lexer intentionally has no C# grammar
	// knowledge and can therefore retain declarations as metadata while
	// dropping executable bodies/calls.  projectRoslynExecutableFacts converts
	// the already-bound facts directly into the existing Canonical UAST; it is
	// not a second IR or a diagnostic/text fallback.
	projected, projectionErr := projectRoslynExecutableFacts(wire.Nodes)
	if projectionErr != nil || projected == nil {
		if projectionErr == nil {
			projectionErr = fmt.Errorf("canonical UAST missing")
		}
		return nil, fmt.Errorf("CSC_TO_SEMANTIC_UNSUPPORTED: %w", projectionErr)
	}
	// A bound C# source has exactly one canonical projection route.  The
	// generic matrix lexer is intentionally not a fallback here: accepting its
	// structural-only result would silently discard executable semantics.
	p := &SemanticProgram{UniversalAST: projected}
	if p.UniversalAST.Metadata == nil {
		p.UniversalAST.Metadata = map[string]string{}
	}
	p.UniversalAST.Metadata["roslyn_bound"] = "true"
	p.UniversalAST.Metadata["roslyn_bridge_schema"] = wire.Schema
	p.UniversalAST.Metadata["roslyn_source_sha256"] = wire.SourceHash
	if p.UniversalAST.Extensions == nil {
		p.UniversalAST.Extensions = map[string]any{}
	}
	p.UniversalAST.Extensions["roslyn_bound_nodes"] = wire.Nodes
	p.UniversalAST.Extensions["roslyn_diagnostics"] = wire.Diagnostics
	// Promote bound facts onto the canonical nodes by source span.  The
	// Roslyn bridge remains an adapter, but these facts are now consumed from
	// the same UAST node that emitters and legality checks already inspect;
	// they are not a parallel semantic tree.  A span that cannot be matched is
	// retained in the evidence extension and never guessed onto another node.
	attachRoslynFactsToCanonicalNodes(p.UniversalAST, wire.Nodes)
	return p, nil
}

func attachRoslynFactsToCanonicalNodes(u *UniversalASTDocument, bound []map[string]any) {
	if u == nil {
		return
	}
	for _, b := range bound {
		start, okStart := jsonInt(b["source_start"])
		end, okEnd := jsonInt(b["source_end"])
		if !okStart || !okEnd || end < start {
			continue
		}
		for i := range u.Nodes {
			n := &u.Nodes[i]
			if n.Source == nil || n.Source.StartOffset != start || n.Source.EndOffset != end {
				continue
			}
			if n.Attributes == nil {
				n.Attributes = map[string]json.RawMessage{}
			}
			for _, key := range []string{"node_kind", "semantic_operation", "symbol_identity", "resolved_type", "extra"} {
				if value, exists := b[key]; exists {
					if raw, err := json.Marshal(value); err == nil {
						n.Attributes["roslyn."+key] = raw
					}
				}
			}
			break
		}
	}
}

func jsonInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), n == float64(int(n))
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func runRoslynBoundAdapter(filename, source string) (*roslynBoundWire, error) {
	dir, err := os.MkdirTemp("", "codetranspiler-roslyn-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	in := filepath.Join(dir, "input.cs")
	out := filepath.Join(dir, "bound.json")
	if err := os.WriteFile(in, []byte(source), 0600); err != nil {
		return nil, err
	}
	adapter := os.Getenv("CODETRANSPILER_ROSLYN_ADAPTER")
	if adapter == "" {
		adapter = filepath.Join(csharpRepoRoot(), "compiler-migration", "csc", "adapter", "bin", "Release", "net10.0", "roslyn-bound-adapter.dll")
	} else if !filepath.IsAbs(adapter) {
		adapter = filepath.Join(csharpRepoRoot(), adapter)
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// The release adapter is published as a Windows apphost next to the
		// DLL.  Prefer that executable so C# semantic extraction does not
		// require a separate `dotnet` command on PATH.  Keep the DLL+dotnet
		// invocation as a compatibility fallback for development layouts.
		adapterExe := strings.TrimSuffix(adapter, filepath.Ext(adapter)) + ".exe"
		if _, statErr := os.Stat(adapterExe); statErr == nil {
			cmd = exec.Command(adapterExe, in, out)
		} else {
			cmd = exec.Command("dotnet", adapter, in, out)
		}
	} else {
		cmd = exec.Command("dotnet", adapter, in, out)
	}
	// Keep Roslyn's first-use and package state inside this project.  The
	// semantic bridge must not depend on a writable global user profile (and a
	// locked global sentinel must not prevent UAST projection).
	if _, cwdErr := os.Getwd(); cwdErr == nil {
		dotnetHome := filepath.Join(csharpRepoRoot(), ".cache", "dotnet-selfhost")
		if mkdirErr := os.MkdirAll(dotnetHome, 0700); mkdirErr != nil {
			return nil, fmt.Errorf("CSC_ROSLYN_LOCAL_DOTNET_HOME: %w", mkdirErr)
		}
		cmd.Env = append(os.Environ(),
			"DOTNET_CLI_HOME="+dotnetHome,
			"DOTNET_SKIP_FIRST_TIME_EXPERIENCE=1",
			"DOTNET_NOLOGO=1",
		)
	}
	if data, e := cmd.CombinedOutput(); e != nil {
		return nil, fmt.Errorf("CSC_ROSLYN_ADAPTER_FAILED: %w: %s", e, data)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		return nil, err
	}
	var wire roslynBoundWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return nil, fmt.Errorf("CSC_ROSLYN_WIRE_INVALID: %w", err)
	}
	return &wire, nil
}

func csharpRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return "."
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dir
		}
		dir = parent
	}
}
