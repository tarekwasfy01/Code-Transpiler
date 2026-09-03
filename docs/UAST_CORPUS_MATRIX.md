# UAST corpus matrix pipeline

`cmd/uast-corpus-matrix` is the shared empirical validation runner. It accepts
the verified ecosystem-miner archive or extracted run directory, a local MLCPD
CSV/JSONL fixture or streaming-results directory, and/or a source folder. Every
input is normalized to a short-lived `CorpusRecord`, then
deduplicated by normalized source hash before translation.

Source files use the productive matrix frontend:

```text
source -> FrontendSemanticFacts -> Raw UAST -> EnrichUniversalAST
       -> AnalyzeUniversalEvidence -> NormalizeUniversalAST -> SemanticProgram
```

Successful files are written as `UAST_FULL`. Parser or semantic failures are
`UAST_REJECTED_GAP`; unavailable miner source payloads remain explicit
`INPUT_ERROR` rows. Multiple gap categories are retained per file. Failure
classes are exact equivalence classes of the sorted gap vector, identified by a
stable SHA-256 hash.

The miner archive contains provenance and feature matrices but not source text.
Those rows are therefore preserved as structural empirical evidence and are
never treated as successful UAST translations. MLCPD's `lang_specific_parse`
and `universal_schema` fields are stored as structural observations only; they
are not interpreted as canonical UAST semantics.

Example:

```powershell
go run ./cmd/uast-corpus-matrix `
  --miner-zip C:\path\run.zip `
  --folder C:\path\sample `
  --out outputs/uast-corpus-matrix `
  --min-occurrences 2 `
  --workers 4 `
  --checkpoint outputs/uast-corpus-matrix/checkpoint.json
```

For a continuously growing miner/MLCPD run, use
`scripts/run-uast-corpus-live.ps1`. It reuses the checkpoint and writes a new
deterministic snapshot on each invocation, so newly arrived shards are added
without reprocessing completed records.

The output directory contains sparse CSV matrices for source provenance,
features, capabilities, translation results, multi-gap failures, exact gap
classes, MLCPD node observations, fixpoint iterations, empirical proof gates,
and SHA-256 checksums. `empirical_proof_matrix.csv` applies boolean gates to
deduplicated `UAST_FULL` observations: distinct hashes and corpus sources are
required, and repository identities are counted only when supplied. A cell is
`NEW_EMPIRICAL_PROOF` only with zero observed contradictions and a stable UAST
result; conflicting observations remain `EMPIRICAL_CONTRADICTION`. Configure
the gates with `--min-distinct-hashes`, `--min-distinct-repositories`, and
`--min-distinct-corpus-sources`.
