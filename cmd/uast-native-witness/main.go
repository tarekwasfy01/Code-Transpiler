// Copyright (c) 2026 Tarek Wasfy
// uast-native-witness builds a canonical semantic program directly and emits
// the PE without a source frontend or an external compiler.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

func main() {
	out := flag.String("o", "uast-witness.exe", "output PE path")
	flag.Parse()
	program := backend.NewSemanticProgram(&backend.BlockStmt{List: []backend.Stmt{
		&backend.ReturnStmt{X: &backend.LiteralExpr{Kind: "integer", Text: "42"}},
	}}, "eager_left_to_right")
	result, err := backend.EmitNativeExecutable(program, "native-x86_64-windows", "")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(*out, result.Bytes, 0755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("UAST_NATIVE_EXE=%s\nINSTRUCTIONS=%d\nBYTES=%d\n", *out, result.InstructionCount, len(result.Bytes))
}
