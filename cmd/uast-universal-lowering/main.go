// Command uast-universal-lowering emits the matrix/report plane for the
// universal UAST lowering stage.  It never changes the productive registries.
package main

import (
	"flag"
	"fmt"
	"log"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

func main() {
	out := flag.String("out", "outputs/universal-lowering", "report directory")
	flag.Parse()
	analysis, err := backend.WriteUniversalLoweringAnalysis(*out)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("schema=%s rules=%d direct=%d lowering_reachable=%d residual=%d runtime_only=%d\n", analysis.Schema, len(analysis.Rules), analysis.DirectTargetCells, analysis.LoweringReachableCells, analysis.ResidualCells, analysis.RuntimeOnlyCells)
}
