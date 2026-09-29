# code-transpiler

A pure-Rust, dependency-light incremental port of Code-Transpiler. It contains
no Go fallback, DLL, bundled external compiler, or `r2many` process.

## What works now

Native source frontends:

- Go
- Julia
- typed Python subset
- Semantic Exchange (`SE/1`) as an interchange input

Native output targets currently exposed by the route matrix:

- Rust
- Python
- Julia
- C
- C++
- Semantic Exchange (`SE/1`)

Not every source supports every target yet. The GUI only enables routes that are
actually implemented.

## Semantic Exchange

Select **Semantic Exchange** (alias `se`) as the target to get a deterministic
`SE/1` document containing semantic JSON and a canonical Julia pivot. SE can be
fed back into the transpiler and emitted to Rust/Python/Julia/C/C++.

Example:

```text
code-transpiler transpile go se input.go program.se
code-transpiler transpile se rust program.se output.rs
```

## GUI

```text
cargo run -- gui 8765
```

Open `http://127.0.0.1:8765/`.

The GUI is fully English. All known source languages remain visible, but the
target selector only enables native routes for the selected source. Go, Julia,
Python and Semantic Exchange currently have outbound native routes.

## Verification

v7 was verified with Rust 1.98.1 x86_64:

- 29/29 tests passed
- `cargo clippy --all-targets --all-features -- -D warnings` passed
- `cargo fmt --check` passed
- end-to-end generated Rust executables were run for Julia, Python and SE input
- `cargo package` verified the publishable crate

See `VERIFIED_BUILD.md` and `PORTING_STATUS.md`.
