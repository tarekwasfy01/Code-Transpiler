package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/tarekwasfy01/Code-Transpiler/internal/corpusmatrix"
)

func main() {
	fs := flag.NewFlagSet("uast-corpus-matrix", flag.ExitOnError)
	project := fs.String("project", ".", "project root")
	out := fs.String("out", "outputs/uast-corpus-matrix", "matrix output directory")
	miner := fs.String("miner-zip", "", "verified ecosystem-miner result ZIP or extracted run directory")
	mlcpd := fs.String("mlcpd", "", "local MLCPD CSV/JSONL file or directory")
	folder := fs.String("folder", "", "source folder to scan")
	language := fs.String("language", "", "language for --folder; empty infers by extension")
	min := fs.Int("min-occurrences", 1, "empirical acceptance threshold")
	minHashes := fs.Int("min-distinct-hashes", 0, "minimum deduplicated source hashes for empirical proof (defaults to --min-occurrences)")
	minRepos := fs.Int("min-distinct-repositories", 0, "minimum repository identities; 0 disables when unavailable")
	minSources := fs.Int("min-distinct-corpus-sources", 1, "minimum independent corpus sources")
	workers := fs.Int("workers", 0, "bounded translation workers (default: CPU count, max 8)")
	iteration := fs.Int("iteration", 1, "fixpoint iteration number")
	checkpoint := fs.String("checkpoint", "", "checkpoint JSON for resumable translation")
	maxRecords := fs.Int("max-records", 0, "maximum deterministically selected deduplicated records; 0 means no limit")
	fs.Parse(os.Args[1:])

	var records []corpusmatrix.CorpusRecord
	add := func(rs []corpusmatrix.CorpusRecord, err error) {
		if err != nil {
			panic(err)
		}
		records = append(records, rs...)
	}
	if *miner != "" {
		add(corpusmatrix.LoadMiner(*miner))
	}
	if *mlcpd != "" {
		add(corpusmatrix.LoadMLCPD(*mlcpd))
	}
	if *folder != "" {
		add(corpusmatrix.LoadFolder(*folder, *language))
	}
	if len(records) == 0 {
		panic("no corpus input; provide --miner-zip, --mlcpd or --folder")
	}
	// Corpus smoke runs must remain bounded. Select after deduplication by the
	// stable source identity so changing loader traversal order cannot change
	// the evidence sample. This affects only ephemeral corpus input, never UAST.
	if *maxRecords > 0 {
		records, _ = corpusmatrix.Dedupe(records)
		sort.Slice(records, func(i, j int) bool {
			if records[i].LanguageID != records[j].LanguageID {
				return records[i].LanguageID < records[j].LanguageID
			}
			if records[i].NormalizedSourceHash != records[j].NormalizedSourceHash {
				return records[i].NormalizedSourceHash < records[j].NormalizedSourceHash
			}
			return records[i].CorpusRecordID < records[j].CorpusRecordID
		})
		if len(records) > *maxRecords {
			records = records[:*maxRecords]
		}
	}
	cfg := corpusmatrix.Config{Project: *project, Out: filepath.Clean(*out), MinOccurrences: *min, MinDistinctHashes: *minHashes, MinDistinctRepositories: *minRepos, MinDistinctCorpusSources: *minSources, Workers: *workers, Iteration: *iteration, Checkpoint: *checkpoint}
	sum, err := corpusmatrix.Run(cfg, records)
	if err != nil {
		panic(err)
	}
	fmt.Printf("UAST corpus matrix: files=%d full=%d rejected=%d gap_classes=%d capabilities=%d validated=%d output=%s\n", sum.FilesTotal, sum.FilesUASTFull, sum.FilesRejectedGap, sum.UniqueGapClasses, sum.CapabilitiesUsed, sum.CapabilitiesCorpusValidated, cfg.Out)
}
