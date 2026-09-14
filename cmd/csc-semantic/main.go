// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"flag"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/v2/v2/internal/backend"
	"os"
)

func main() {
	out := flag.String("o", "", "output SemanticProgram JSON")
	flag.Parse()
	if flag.NArg() != 1 || *out == "" {
		fmt.Fprintln(os.Stderr, "usage: csc-semantic -o output.json input.cs")
		os.Exit(2)
	}
	name := flag.Arg(0)
	src, e := os.ReadFile(name)
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	p, e := backend.RoslynBoundToSemantic(name, string(src))
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	data, e := p.MarshalUniversalASTJSON()
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	if e = os.WriteFile(*out, data, 0644); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
	fmt.Printf("SEMANTIC_JSON=%s\n", *out)
}
