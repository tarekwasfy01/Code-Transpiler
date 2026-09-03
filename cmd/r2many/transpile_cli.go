package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

type translationResult struct {
	Target string `json:"target"`
	Path   string `json:"path,omitempty"`
	Error  string `json:"error,omitempty"`
}

func sourceLanguage(source, filename string) (string, error) {
	if source != "auto" {
		source = backend.NormalizeLanguage(source)
		if !backend.HasFrontend(source) {
			return "", fmt.Errorf("unknown source %q", source)
		}
		return source, nil
	}
	extension := filepath.Ext(filename)
	for _, spec := range backend.Frontends() {
		for _, ext := range spec.Extensions {
			if strings.EqualFold(ext, extension) {
				return spec.ID, nil
			}
		}
	}
	return "", fmt.Errorf("cannot infer language from %q; specify -source", filename)
}

func targetExtension(target string) string {
	for _, spec := range backend.Frontends() {
		if spec.ID == target {
			return spec.Extensions[0]
		}
	}
	return ""
}

func transpile(args []string) error {
	fs := flag.NewFlagSet("transpile", flag.ContinueOnError)
	var source, target, out string
	fs.StringVar(&source, "source", "auto", "source language or auto")
	fs.StringVar(&source, "from", "auto", "alias of -source")
	fs.StringVar(&target, "target", "go", "target language or all")
	fs.StringVar(&target, "to", "go", "alias of -target")
	fs.StringVar(&out, "o", "", "output file, or directory for -target all")
	native := fs.Bool("native", false, "strict native frontend; no legacy fallback")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-source": true, "-from": true, "-target": true, "-to": true, "-o": true, "-native": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: transpile -source <language|auto> -target <language|all> input [-o output] [-native]")
	}
	input := fs.Arg(0)
	source, err := sourceLanguage(source, input)
	if err != nil {
		return err
	}
	target = backend.NormalizeLanguage(target)
	if target != "all" && !backend.HasBackend(target) {
		return fmt.Errorf("unknown target %q", target)
	}
	if target == "all" && out == "" {
		return fmt.Errorf("-target all requires -o <output-directory>")
	}
	if target == "all" && samePath(input, filepath.Join(out, "translation-report.json")) {
		return fmt.Errorf("report path would overwrite source file")
	}
	data, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	var program manytomany.Program
	// Native mode validates even identity routes; legacy identity is a copy.
	if *native {
		if source != "go" {
			return fmt.Errorf("strict native frontend for %q is not implemented", source)
		}
		p, e := backend.LowerNativeGo(input, string(data))
		if e != nil {
			return e
		}
		program = manytomany.Program{Source: source, Semantic: p}
	} else if target == "all" || target != source {
		program, err = manytomany.Parse(source, string(data))
		if err != nil {
			return err
		}
	}
	targets := []string{target}
	if target == "all" {
		targets = nil
		for _, spec := range backend.Backends() {
			targets = append(targets, spec.ID)
		}
		if err = os.MkdirAll(out, 0755); err != nil {
			return err
		}
	}
	results := make([]translationResult, 0, len(targets))
	failures := 0
	for _, destination := range targets {
		result := translationResult{Target: destination}
		output := out
		if target == "all" {
			output = filepath.Join(out, strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))+targetExtension(destination))
		}
		if output == "" {
			output = strings.TrimSuffix(input, filepath.Ext(input)) + targetExtension(destination)
			if samePath(input, output) {
				output = strings.TrimSuffix(input, filepath.Ext(input)) + ".transpiled" + targetExtension(destination)
			}
		}
		var code string
		if samePath(input, output) {
			err = fmt.Errorf("refusing to overwrite source file %q", input)
		} else if !*native {
			// CLI and GUI share the same immutable request boundary. The CLI may
			// still fan out targets, but every individual output crosses exactly
			// the same Frontend -> UAST -> Native/Runtime core as GUI Convert.
			result, coreErr := manytomany.TranspileCore(manytomany.TranspileRequest{Source: string(data), SourceLanguage: source, TargetLanguage: destination, EntryPoint: "cli"})
			code, err = result.Code, coreErr
		} else if program.Semantic == nil && destination == source {
			// This is the explicit legacy ingress compatibility case: parsing did
			// not produce a canonical UAST, so source preservation is the only
			// non-projecting operation permitted here.
			code = string(data)
			err = nil
		} else {
			// A successfully parsed program is canonical UAST even on an identity
			// route.  Keeping the source-text copy shortcut here would bypass the
			// NativeEmitter and make route mode observable again.
			code, err = manytomany.Emit(destination, program)
		}
		if err == nil {
			err = os.WriteFile(output, []byte(code), 0644)
		}
		if err != nil {
			result.Error = err.Error()
			failures++
		} else {
			result.Path = output
		}
		results = append(results, result)
	}
	if target == "all" {
		encoded, e := json.MarshalIndent(results, "", "  ")
		if e != nil {
			return e
		}
		if e = os.WriteFile(filepath.Join(out, "translation-report.json"), encoded, 0644); e != nil {
			return e
		}
	}
	if failures > 0 {
		return fmt.Errorf("%d/%d translations failed: %v", failures, len(results), results)
	}
	return nil
}

func samePath(a, b string) bool {
	x, e := filepath.Abs(a)
	if e != nil {
		return false
	}
	y, e := filepath.Abs(b)
	if e != nil {
		return false
	}
	if strings.EqualFold(x, y) {
		return true
	}
	ax, ae := os.Stat(x)
	by, be := os.Stat(y)
	return ae == nil && be == nil && os.SameFile(ax, by)
}
