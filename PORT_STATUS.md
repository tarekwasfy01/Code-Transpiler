# Managed port status

## Packaging target

- Package: `CodeTranspiler.Managed`
- Version: `0.4.1`
- Framework: `net8.0`
- External NuGet dependencies: **0**
- Foreign runtimes/compilers shipped: **0**
- The same assembly exposes both the library API and CLI/GUI entry point.

## Managed functionality

- Language registry: R, Go, Python, Rust, C, C++, Zig, Julia, Nim, C#, Java, Kotlin, Swift.
- 156 directed non-identity routes.
- Lexer/expression lowering.
- Function parsing and emission.
- Common type/class/struct/interface/enum parsing and emission for all targets.
- Variables, assignment, return, throw, if, while, C-style for, foreach/range-style loops.
- Recursive indentation blocks for Python/Nim including if/elif/else, while, for, try/except/finally.
- Arrays, maps, indexes, slices, calls, members, lambdas and common scalar expressions.
- Index-base canonicalization (1-based sources such as Julia/R to canonical zero base, then target projection).
- Fragment fallback and route scoring.
- Semantic JSON serialize/deserialize/merge.
- Batch API.
- Directory/project translation.
- CLI.
- Loopback-only browser GUI with code, diagnostics, route attempts and semantic JSON panes.
- Import-free C# target prelude (fully-qualified BCL collection/runtime references where needed).

## Deliberately not shipped

The original repository contains native compiler, external toolchain, LLVM, machine-code/decompiler and foreign-runtime integration paths. They are not part of this NuGet package because they would violate the dependency-free/managed-only packaging goal. The managed package focuses on source-to-source semantic transpilation.

## Validation performed in the bootstrap environment

- Every managed `.cs` source file was accepted by the Go implementation's C# frontend and projected onward to Go.
- The independent Julia implementation accepted the managed CLI C# source and projected it through its Julia pivot.
- No `<PackageReference>` exists under `src/`.
- The environment used for bootstrap did not contain a .NET SDK/C# compiler, so the final authoritative compile occurs when `dotnet pack` is invoked. The GitHub workflow performs no source generation or transpilation; packaging consumes only the checked-in managed C# source.
