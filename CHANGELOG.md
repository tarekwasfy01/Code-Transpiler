# Changelog

## 0.4.1

- Added **SE (Semantic Exchange)** as a first-class input and output language.
- Added `SE/1` round-trip format containing semantic JSON and the canonical Julia pivot.
- Added SE aliases (`semantic`, `semantics`, `semantic-exchange`) and `.se` file inference.
- GUI now exposes SE in both source and target selectors.
- Added Python → SE → Julia execution and SE → Go regression tests.

## 0.4.0

- Added public semantic analysis output (`semantic_program`, `semantic_output`, `transpile_with_semantics`).
- Added semantic text and deterministic JSON rendering to the GUI/API.
- Added explicit source-index-base vs canonical one-based pivot reporting.
- Fixed Python indentation / `if` / `elif` / `else`, `range`, `//`, `**`, and known-list indexing regressions.
- Fixed double index shifting during Julia-pivot -> zero-based target projection.
- Added target-specific index casts for Rust, Java, C#, Kotlin, Swift, Zig, and Nim.
- Added safer vector/list projection and mutable collection lowering for C++, Go, Python, Rust, Java, C#, Kotlin, Swift, Nim, and R.
- Dynamic vector append for C/Zig now fails explicitly instead of emitting known-wrong code.
- Added compiler checks for collection/index output where toolchains are available.
- 142 package tests pass under Julia 1.13.1.
