// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

// failure-saturation-report consumes structured JSONL failures emitted by a
// frontend diagnostic run and writes the deterministic saturation matrices.
// It never parses diagnostics or source text.
func main() {
	in := flag.String("input", "", "JSONL SemanticFailure input")
	factsPath := flag.String("facts", "", "JSON FrontendSemanticFacts input")
	out := flag.String("out", "outputs/failure-saturation", "output directory")
	mode := flag.String("mode", "SATURATE", "STRICT or SATURATE")
	flag.Parse()
	ctx := backend.NewDiagnosticContext(backend.DiagnosticMode(*mode))
	if *in != "" {
		f, err := os.Open(*in)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		s := bufio.NewScanner(f)
		s.Buffer(make([]byte, 64*1024), 4*1024*1024)
		for s.Scan() {
			var failure backend.SemanticFailure
			if err := json.Unmarshal(s.Bytes(), &failure); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			if err := ctx.Record(failure); err != nil && ctx.Mode == backend.DiagnosticStrict {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
		}
		if err := s.Err(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
	if *factsPath != "" {
		data, err := os.ReadFile(*factsPath)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		var facts backend.FrontendSemanticFacts
		if err := json.Unmarshal(data, &facts); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		successful, holes := backend.SaturateFrontendFacts(facts, ctx)
		fmt.Printf("STRUCTURED_NODES_SUCCESS=%d STRUCTURED_NODES_HOLES=%d\n", successful, holes)
	}
	if err := backend.WriteFailureSaturationReport(*out, ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("FAILURE_INSTANCES=%d FAILURE_FAMILIES=%d OUT=%s\n", len(ctx.Failures), ctx.Summary().Families, *out)
}
