# Verified build — v7

Verified locally with Rust 1.98.1 x86_64.

## Rust quality gates

- `cargo fmt --check` — passed
- `cargo test --all-targets` — 29/29 tests passed
- `cargo clippy --all-targets --all-features -- -D warnings` — passed with zero warnings
- `cargo package` — package created and verified by Cargo

## Native frontends

The crate now has three real native source frontends:

- Go
- Julia
- typed Python subset

Semantic Exchange (`SE/1`) is both a source and a target.

## End-to-end execution

- Julia -> Rust -> `rustc` -> executable output: `13`
- typed Python -> Rust -> `rustc` -> executable output: `10`
- Go -> SE/1 -> Rust -> `rustc` -> executable output: `5`

No Go executable, DLL, external transpiler process, or runtime fallback is used.
