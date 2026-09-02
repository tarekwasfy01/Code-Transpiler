# 13-language external semantic matrix

Languages: C, C++, C#, Go, Java, Julia, Kotlin, Nim, Python, R, Rust, Swift, Zig.

## Purpose
This package summarizes semantics from official language references/specifications and selected formal/external semantic models into a matrix-friendly form.

It is intentionally **not** a replacement for the project's canonical 334 UASF capabilities.

## Proof-safe matrix convention
Use both matrices together:

- `semantic_atom_value.csv`
- `semantic_atom_known.csv`

A cell is meaningful only when `KNOWN=1`.

- `KNOWN=1, VALUE=1` => PRESENT
- `KNOWN=1, VALUE=0` => ABSENT
- `KNOWN=0` => UNRESOLVED; VALUE must be ignored

This avoids turning missing evidence into negative evidence.

## Recommended use with the Universal AST project
1. Treat `EXTSEM_*` as an independent external semantic basis.
2. Build `M_EXTSEM_UASF` (EXTSEM atom × canonical UASF capability).
3. Multiply language evidence by that crosswalk.
4. XOR/compare the result against the existing Language×UASF matrix.
5. Investigate only residual mismatches and UNRESOLVED cells.
6. Keep MLCPD and ecosystem-miner evidence separate as empirical evidence.

## Important limitations
- This is a compact semantic profile, not a line-by-line formalization of every language rule.
- Several languages have platform-dependent behavior (notably Kotlin, C#, Python implementation memory behavior, and concurrency libraries).
- Some external formal models are historical or incomplete; those are labeled accordingly.
- C/C++ standard drafts and compiler documents are much more precise for low-level sequencing/UB than generic summaries.
- Do not auto-promote an EXTSEM-to-UASF mapping without an exact semantic contract match.
