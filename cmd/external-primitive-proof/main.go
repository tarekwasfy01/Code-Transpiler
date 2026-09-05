package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

func main() {
	out := flag.String("out", "outputs/external-primitive-crosswalk", "directory for proof output")
	flag.Parse()
	n, err := backend.WriteExternalPrimitiveProofReport(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "external primitive proof:", err)
		os.Exit(1)
	}
	fmt.Printf("PROVEN_SEMANTIC_SHAPES=%d OUT=%s\n", n, *out)
}
