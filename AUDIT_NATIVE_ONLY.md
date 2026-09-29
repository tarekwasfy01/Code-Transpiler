# Native-only audit — v7

The publishable source contains no Go executable, Windows DLL, `.go` runtime,
`r2many`, or external transpiler fallback. Runtime transpilation is performed by
Rust source code in this crate.

Semantic Exchange and the Julia/Python frontends are also native Rust code.
