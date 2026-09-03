# Language source evidence scans

This directory contains outputs from the existing Language Semantic Matrix
Builder over the supplied Julia, Nim, Swift, and Zig compiler/source archives.
The scans preserve source provenance and matrix output, but they are not a
canonical UASF mapping. The generic feature projection is currently identical
across the four outputs, so it is not language-specific proof. The scans may be
joined to `matrices/uast_engine` only when an exact compiler-level semantic
contract is available.

Current scans:

- Julia: 1,696 files, 98 source feature columns
- Nim: 3,966 files, 98 source feature columns
- Swift: 23,207 files, 98 source feature columns
- Zig: 19,649 files, 98 source feature columns

Corpus observations and package counts remain empirical evidence. They never
set canonical, direct, tested, or preservation truth by themselves.

Run `go run ./cmd/uast-source-verify --project .` to regenerate the explicit
56-row source-contract check in
`matrices/uast_handoff/source_contract_verification.csv`. The checker records
source-scan/anchor presence and exact crosswalk status without promoting an
observation into the canonical UAST matrices.
The companion `matrices/uast_handoff/source_contract_manifest.csv` records the
checked source anchors with SHA-256 hashes where a file was present.
Six Julia capabilities are additionally backed by the checked-in
SemanticAST.jl crosswalk; see
`matrices/uast_handoff/julia_semanticast_crosswalk.csv`.
