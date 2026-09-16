// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/v2/internal/backend"
)

// semanticLinkCommand materializes selected Semantic module dependencies in a
// portable semantic document. It is intentionally separate from compile: the
// resulting file is inspectable and can be validated, formatted, merged, or
// compiled later. Without --embed-all it honors semantic_module_link_roots.
func semanticLinkCommand(args []string) error {
	fs := flag.NewFlagSet("semantic-link", flag.ContinueOnError)
	out := fs.String("o", "", "linked .se, .sp, .spz, or .json output")
	embedAll := fs.Bool("embed-all", false, "link every declared module instead of selected roots")
	moduleRoot := fs.String("module-root", "", "semantic module store root")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{
		"-o": true, "-embed-all": false, "--embed-all": false, "-module-root": true,
	})); err != nil {
		return err
	}
	if fs.NArg() != 1 || *out == "" {
		return fmt.Errorf("usage: semantic-link [--embed-all] [-module-root DIR] input.se|input.sp|input.spz|input.json -o linked.se")
	}
	input := fs.Arg(0)
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	var p *backend.SemanticProgram
	switch {
	case strings.HasPrefix(string(data), "SPZ2"):
		p, err = backend.ParseSemanticSPZ(data)
	case strings.HasSuffix(strings.ToLower(input), ".json") || strings.HasPrefix(strings.TrimSpace(string(data)), "{"):
		p, err = backend.ParseSemanticJSON(data)
	default:
		p, err = backend.ParseSemanticSE(data)
	}
	if err != nil {
		return err
	}
	resolver, err := backend.NewUniversalModuleResolver()
	if err != nil {
		return err
	}
	if *moduleRoot != "" {
		resolver.Store.Root = *moduleRoot
		if err := resolver.Store.Ensure(); err != nil {
			return err
		}
	}
	if err := resolver.LinkSemanticDependenciesWithOptions(p, backend.SemanticModuleLinkOptions{
		BaseDir: filepath.Dir(input), EmbedAll: *embedAll,
	}); err != nil {
		return err
	}
	var encoded []byte
	switch strings.ToLower(filepath.Ext(*out)) {
	case ".se":
		encoded, err = p.MarshalSemanticSEReadable()
	case ".sp":
		encoded, err = p.MarshalSemanticSP()
	case ".spz":
		encoded, err = p.MarshalSemanticSPZ()
	case ".json":
		encoded, err = p.MarshalSemanticJSON()
	default:
		return fmt.Errorf("unsupported semantic-link output %q", *out)
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, encoded, 0644); err != nil {
		return err
	}
	nodes := 0
	if p.UniversalAST != nil {
		nodes = len(p.UniversalAST.Nodes)
	}
	mode := "selected-roots"
	if *embedAll {
		mode = "embed-all"
	}
	fmt.Printf("Semantic link output=%s nodes=%d mode=%s\n", *out, nodes, mode)
	return nil
}
