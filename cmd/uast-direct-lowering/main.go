package main

import (
	"flag"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"path/filepath"
)

func main() {
	out := flag.String("out", "outputs/direct-lowering", "output directory")
	candidates := flag.String("candidates", "outputs/multilanguage-corpus-fixpoint/promotion-batch-3/promotion_candidates.csv", "runtime promotion candidates")
	flag.Parse()
	a, err := backend.WriteDirectLoweringAnalysis(filepath.Clean(*out), filepath.Clean(*candidates))
	if err != nil {
		panic(err)
	}
	exec, err := backend.WriteExecutionPrimitiveMatrixAnalysis(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	fmt.Printf("Direct lowering matrix: classes=%d atomic-obligations=%d missing-vectors=%d components=%d exact-primitives=%d data-only=%d new-handler-classes=%d execution-primitives=%d current-support=%d/%d derived-support=%d/%d closed-support=%d/%d unknown=%d residual-classes=%d proof-classes=%d proven=%d impossible=%d conflicts=%d insufficient-proof=%d missing=%d semantic-missing=%d existing-direct=%d reconstructed-direct=%d contradictions=%d direct-classes=%d/%d\n", len(a.Rows), len(a.AtomicObligations), len(a.Classes), a.ConnectedComponents, len(a.Primitives), a.DataOnlyPrimitives, a.NewHandlerClasses, len(exec.Primitives), exec.NativeSupportCells, len(exec.Targets)*len(exec.Primitives), exec.DerivedSupportCells, len(exec.Targets)*len(exec.Primitives), exec.ClosedSupportCells, len(exec.Targets)*len(exec.Primitives), exec.UnknownSupportCells, exec.ResidualClasses, exec.SolveObligations, exec.NativeProven, exec.NativeImpossible, exec.Conflicts, exec.InsufficientProof, exec.MissingCells, exec.SemanticMissingCells, exec.ExistingDirectCells, exec.ReconstructedDirectCells, len(exec.Contradictions), exec.DirectCells, len(exec.Targets)*len(exec.ProjectionClasses))
}
