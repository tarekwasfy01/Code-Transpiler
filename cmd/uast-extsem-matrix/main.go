package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/tarekwasfy01/Code-Transpiler/internal/extsemmatrix"
)

func main() {
	project := flag.String("project", ".", "project root")
	input := flag.String("input", "", "semantic-language-matrix-13.zip or extracted directory")
	proof := flag.String("proof", "", "optional uast-semantic-proof-compression-v1.zip or extracted directory")
	out := flag.String("out", "", "output directory (defaults to matrices/uast_handoff/semantic_language_matrix_13)")
	flag.Parse()
	if *input == "" {
		fmt.Fprintln(os.Stderr, "-input is required")
		os.Exit(2)
	}
	if *out == "" {
		*out = filepath.Join(*project, "matrices", "uast_handoff", "semantic_language_matrix_13")
	}
	s, err := extsemmatrix.Import(extsemmatrix.Options{Project: *project, Input: *input, Out: *out})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if *proof != "" {
		p, err := extsemmatrix.ImportProof(extsemmatrix.Options{Project: *project, Input: *input, Out: *out}, *proof)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Printf("PROOF_COMPRESSION=PASS basis=%d uasf=%d exact_rows=%d present_cells=%d direct_basis_cells=%d corroborating_basis_cells=%d sha256_verified=%t\n", p.BasisDimensions, p.UASFCapabilities, p.ExactCrosswalkRows, p.PresentLanguageCells, p.DirectBasisCells, p.BasisSupportedCells, p.SHA256Verified)
	}
	fmt.Printf("EXTSEM_IMPORT=PASS languages=%d atoms=%d canonical=%d confirmed_crosswalk=%d unresolved_crosswalk=%d sha256_verified=%t out=%s\n", s.Languages, s.Atoms, s.CanonicalCapabilities, s.ConfirmedCrosswalkCells, s.UnresolvedCrosswalkCells, s.SHA256Verified, *out)
}
