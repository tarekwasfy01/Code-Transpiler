// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/tarekwasfy01/Code-Transpiler/v2/internal/backend"
)

// semanticCSC exposes the Roslyn-bound adapter through the main CLI.  It
// emits the existing canonical UniversalAST/SemanticProgram JSON; Roslyn is
// used only for parsing and binding evidence and never becomes a second IR.
func semanticCSC(args []string) error {
	fs := flag.NewFlagSet("semantic-csc", flag.ContinueOnError)
	out := fs.String("o", "", "output SemanticProgram JSON")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-o": true})); err != nil {
		return err
	}
	if fs.NArg() != 1 || *out == "" {
		return fmt.Errorf("usage: semantic-csc input.cs -o output.json")
	}
	input := fs.Arg(0)
	source, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	program, err := backend.RoslynBoundToSemantic(input, string(source))
	if err != nil {
		return err
	}
	doc, err := program.MarshalUniversalASTJSON()
	if err != nil {
		return err
	}
	var pretty any
	if json.Unmarshal(doc, &pretty) == nil {
		doc, _ = json.MarshalIndent(pretty, "", "  ")
		doc = append(doc, '\n')
	}
	if err := os.WriteFile(*out, doc, 0644); err != nil {
		return err
	}
	fmt.Printf("SEMANTIC_JSON=%s\n", *out)
	return nil
}
