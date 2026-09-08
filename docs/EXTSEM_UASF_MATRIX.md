# External semantic matrix projection

`semantic-language-matrix-13.zip` is imported as independent, source-catalogued
evidence.  It is not a replacement for the canonical 334 UASF capabilities and
it never changes `universal_ast_schema.json` by itself.

The input classes are kept separate: `CANONICAL_DIMENSION` and
`CANONICAL_CROSSWALK` are read only from `matrices/uast_engine`; the imported
package and proof compression are `EXTERNAL_EVIDENCE`/`PROOF_EVIDENCE`.
Corpus and MLCPD observations are `CORPUS_OBSERVATION` and can promote an
existing mapping only through the boolean empirical-proof gates.  They never
create a language, feature, facet, relation, or field dimension.

The importer writes three deterministic matrices:

* `M_LANGUAGE_EXTSEM.csv` — 13 languages × 51 external atoms.  A cell is
  meaningful only when `known=1`; `UNRESOLVED` is preserved.
* `M_EXTSEM_UASF.csv` — 51 atoms × 334 canonical capabilities.  A `1` is
  emitted only for an exact anchor already present in the project’s canonical
  feature/crosswalk data.  All other cells remain `UNRESOLVED`.
* `M_LANGUAGE_UASF_EXTERNAL.csv` — the boolean product
  `M_LANGUAGE_EXTSEM × M_EXTSEM_UASF`.  Presence is propagated only from
  explicitly `PRESENT` external cells; explicit `ABSENT` is retained when no
  contradictory present atom exists.

`M_LANGUAGE_UASF_DIFF.csv` compares that product with the existing language
matrix and classifies cells as `CONFIRMED`, `CONFLICT`, `UNRESOLVED`, or
`NEW_EXTERNAL_EVIDENCE`.  Corpus and MLCPD observations remain separate.

The package’s `SHA256SUMS.csv` is checked before import.  Source catalog,
evidence rows, profile, atom dictionary, and workbook are copied unchanged to
`matrices/uast_handoff/semantic_language_matrix_13/` so every promoted cell can
be traced back to the official source set.

Run from the project root:

```powershell
$env:GOCACHE = Join-Path (Get-Location) '.gocache-extsem'
go run ./cmd/uast-extsem-matrix --project . --input F:\download\semantic-language-matrix-13.zip
```

The generated `import_summary.json` records the package hash, checksum result,
matrix dimensions, confirmed/unresolved crosswalk cells, and the diff counts.

An optional `--proof` input imports `uast-semantic-proof-compression-v1.zip`.
Its 67-dimensional basis and 808 exact source-feature quotient are copied to
`proof_compression_v1/`; `M_EXTSEM_UASF_PROOF.csv` is computed as the matrix
product `M_EXTSEM_B × M_UASF_Bᵀ`. Direct basis matches and corroborating basis
overlaps remain explicitly labelled, so this independent package can validate
the project matrices without silently changing the canonical schema.

Empirical promotion is likewise boolean: distinct normalized source hashes,
optional repository identities, and corpus sources are deduplicated before a
cell is marked `NEW_EMPIRICAL_PROOF`.  A single reproducible contradiction is
kept as `EMPIRICAL_CONTRADICTION` and blocks promotion; missing observations
remain `INSUFFICIENT_EVIDENCE`.
