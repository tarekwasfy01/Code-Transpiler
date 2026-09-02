// uast-matrix-adapter writes the canonical input contract for the external
// uast-matrix-engine. It only serializes existing project facts; all matrix
// algebra is intentionally delegated to the engine executable.
package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

func main() {
	out := flag.String("out", filepath.FromSlash("matrices/uast_engine"), "external engine input directory")
	flag.Parse()
	if err := backend.WriteUASTMatrixEngineInputs(*out); err != nil {
		panic(err)
	}
	fmt.Printf("UAST_MATRIX_ENGINE_INPUTS=%s\n", *out)
}
