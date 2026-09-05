package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

func main() {
	out := flag.String("out", "outputs/primitive-compiler", "directory for generated primitive compiler artifacts")
	flag.Parse()
	r, err := backend.WritePrimitiveCompilerReport(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "primitive compiler:", err)
		os.Exit(1)
	}
	fmt.Printf("GENERATED=%d RECIPES=%d ATOMIC=%d BASIS=%s OUT=%s\n", len(r.Specs), len(r.Recipes), len(r.AtomicPrimitives), r.BasisHash, *out)
}
