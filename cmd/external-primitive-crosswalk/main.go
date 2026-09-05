package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

func main() {
	out := flag.String("out", "outputs/external-primitive-crosswalk", "directory for the external evidence crosswalk")
	flag.Parse()
	report, err := backend.WriteExternalPrimitiveCrosswalkReport(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "external primitive crosswalk:", err)
		os.Exit(1)
	}
	fmt.Printf("EXTERNAL_ROWS=%d GAP_MATCHES=%d PROMOTED_EXACT=%d REMAINING=%d RATE=%.4f OUT=%s\n", report.ExternalEvidenceRows, report.GapRowsWithExternalMatch, report.GapRowsPromotedExact, report.RemainingInsufficient, report.ExternalGapClosureRate, *out)
}
