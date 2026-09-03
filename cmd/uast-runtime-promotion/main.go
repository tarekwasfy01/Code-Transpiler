package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

func main() {
	out := flag.String("out", "outputs/runtime-direct-promotion", "promotion evidence output directory")
	ecosystem := flag.String("ecosystem-provenance", "", "ecosystem semantic_evidence_provenance.csv")
	artifacts := flag.String("ecosystem-artifacts", "", "ecosystem artifact_summary.csv")
	flag.Parse()
	cleanOptional := func(path string) string {
		if path == "" {
			return ""
		}
		return filepath.Clean(path)
	}
	summary, err := backend.WriteRuntimePromotionEvidence(filepath.Clean(*out), cleanOptional(*ecosystem), cleanOptional(*artifacts))
	if err != nil {
		panic(err)
	}
	fmt.Printf("Runtime direct promotion: records=%d relevant=%d candidates=%d ecosystem-packages=%d empirical-semantic-proven=%d direct-proven=%d promoted=%d direct=%d runtime=%d conflicts=%d\n", summary.SourceRecordsRead, summary.RelevantRuntimeMatches, summary.DirectCandidates, summary.EcosystemPackages, summary.EmpiricalSemanticProven, summary.NewProvenDirectContracts, summary.RuntimeCellsPromoted, summary.DirectAfter, summary.RuntimeAfter, summary.Conflicts)
}
