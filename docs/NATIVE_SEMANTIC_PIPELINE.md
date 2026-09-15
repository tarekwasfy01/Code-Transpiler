# Semantic/UAST to native PE

The productive native entry point is `internal/backend.EmitNativeExecutable`.
It validates the canonical UAST, resolves compile-time contracts on that same
document, collects requirements from actual nodes, solves the exact
direct/recipe closure, rejects unlinked runtime effects, then calls the
existing x86-64 selector, register allocator, encoder and PE writer.
The program representation remains `UniversalASTDocument`; closure and
witness structs are analysis results only.

The direct construction witness is:

```powershell
$env:GOCACHE = (Join-Path (Get-Location) '.gocache-native')
go run ./cmd/uast-native-witness -o .\uast-witness-native.exe
```

This path does not invoke Go, GCC, Clang or an assembler. The optional
`CompileOptions.ViaAssembly` path remains an explicit cross-check only.

## Fail-closed boundary

Generated recipes with non-empty steps are not treated as implemented by
appending disconnected nodes. They require a verified UAST graph rewrite that
preserves operand binding, result uses, control edges, effects and termination.
Identity recipes of the exact form `RESULT($0)` are supported as verified
graph-preserving rewrites. The proven leaf form `DOUBLE = ADD($0,$0)` now
replaces the matched operation and clones its operand node with consistent
syntax/data/evaluation edges. Structural recipes with control/effect changes
still return a concrete error until those replacements are implemented.
Static string data and constant numeric aggregate cells are emitted. Dynamic
numeric aggregates are materialized in the current native frame, and canonical
one-based indexing reads the length word and traps on invalid bounds. Aggregate
values that escape the current native frame, closures, string formatting,
external ABI, and non-empty runtime effects remain explicit gaps in the native
x64 contract rather than silent fallbacks.

Current direct witness evidence (rebuilt 2026-09-07):

* `uast-witness-native.exe`: 16996882 bytes, process exit `0`
* SHA-256: `E7C7A92CD6E7ED3005B8449AC5DE4F5F9DA2ABB2676854DF67125CDFFC259055`
