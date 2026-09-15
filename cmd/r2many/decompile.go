// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	codetranspiler "github.com/tarekwasfy01/Code-Transpiler"
)

// decompileSemantic exposes the structured machine frontend directly. The
// result is canonical SemanticProgram JSON, suitable for semantic-transpile
// or inspection; no legacy/source parser is involved.
func decompileSemantic(args []string) error {
	fs := flag.NewFlagSet("decompile", flag.ContinueOnError)
	inputKind := fs.String("input", "executable", "assembly|machine|object|executable")
	out := fs.String("o", "", "SemanticProgram JSON output path (default stdout)")
	arch := fs.String("arch", "x86_64", "binary architecture")
	base := fs.Uint64("base-address", 0, "optional binary image base address")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{
		"-input": true, "-o": true, "-arch": true, "-base-address": true,
	})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: decompile -input assembly|machine|object|executable input [-o output.json]")
	}
	kind := map[string]codetranspiler.CompileInputKind{
		"assembly":     codetranspiler.InputAssembly,
		"machine":      codetranspiler.InputMachine,
		"machine_code": codetranspiler.InputMachine,
		"object":       codetranspiler.InputObject,
		"executable":   codetranspiler.InputExecutable,
	}[*inputKind]
	if kind == "" {
		return fmt.Errorf("unsupported input kind %q", *inputKind)
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	encoded, err := codetranspiler.DecompileSemanticJSON(data, codetranspiler.CompileOptions{
		InputKind: kind, SourceArch: *arch, TargetArch: *arch,
		TargetOS: "windows", ABI: "win64", BaseAddress: *base,
	})
	if err != nil {
		return err
	}
	if *out == "" {
		var pretty json.RawMessage = encoded
		formatted, e := json.MarshalIndent(pretty, "", "  ")
		if e == nil {
			encoded = formatted
		}
		_, err = os.Stdout.Write(append(encoded, '\n'))
		return err
	}
	return os.WriteFile(*out, append(encoded, '\n'), 0644)
}
