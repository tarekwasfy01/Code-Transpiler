# Partial corpus evidence audit

This directory is an evidence layer only. The corpus run was aborted before
the PyPI, crates.io, and Go registry phases and does not change compiler
provenance or target preservation state.

## Language semantic observations

The corpus contains 56 new empirical language/capability observations:

- Julia: 13
- Nim: 13
- Swift: 16
- Zig: 14

The handoff itself does not contain the previously claimed UASF-specific
compiler matrices. The complete Julia, Nim, Swift, and Zig source archives
were nevertheless scanned with the existing Language Semantic Matrix Builder
and are stored under `matrices/language_source_scans/`. Those scans are source
evidence inventories; they do not contain an exact feature-to-UASF crosswalk.
Consequently:

- compiler-confirmed cells: 0
- empirical candidates requiring exact compiler-contract verification: 56
- canonical language-matrix updates: 0

The source scans are positive evidence and must not be interpreted as absence
for any of the four languages. An exact UASF update requires the missing
language-specific crosswalk or a manually auditable compiler contract.

The 21 R observations are baseline confirmations and remain validation data;
they do not expand the canonical capability matrix.

## Unknown normalized signatures

The corpus reports 35,614 normalized signatures, of which 4,008 occur in at
least two packages and 1,833 occur in at least three packages. The source
catalog marks every one as `known_pattern_match=0`. With no compiler-level
contract supplied for these signatures, the conservative set classification is:

| Classification | Count |
| --- | ---: |
| EXACT_EXISTING_SEMANTIC | 0 |
| MISSING_FRONTEND_CROSSWALK | 0 |
| COMPILER_VERIFICATION_REQUIRED | 35,614 |
| NEW_SEMANTIC_GAP | 0 |
| SYNTAX_ONLY_OR_NOISE | 0 |

This is a verification status, not a semantic mapping. No corpus observation
sets `canonical`, `direct`, `tested`, or preservation truth.

## Matrix result

The supplied handoff analyzer was rerun after importing this corpus. The
canonical result remains 334 capabilities, 320 new canonical-ready
capabilities, and schema delta 86 structures / 39 relations / 320 facets / 27
fields. Direct candidates remain 764.

The source archives are now checked by the reproducible
`cmd/uast-source-verify` utility. Its 56-row result is
`matrices/uast_handoff/source_contract_verification.csv`. All four languages
have a non-empty source scan and Julia, Nim, and Zig have all declared source
anchors; Swift is missing the exact `docs/Compiler.md` path in the supplied
archive. The builder's feature projection is byte-identical for all four
languages, so it is a generic template rather than language-specific compiler
evidence. Six Julia rows now have an explicit, auditable SemanticAST.jl
crosswalk (`UASF_0119`, `UASF_0218`, `UASF_0302`, `UASF_0305`, `UASF_0306`,
`UASF_0308`) and are registered in the canonical language-feature input. The
remaining 50 rows have no exact crosswalk and are not promoted. This
distinguishes source archive presence from a verified UAST projection and
avoids treating missing crosswalk data as negative evidence. The checked
anchor paths and hashes are recorded in
`matrices/uast_handoff/source_contract_manifest.csv`.
