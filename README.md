# Code Transpiler

Go module: `github.com/tarekwasfy01/Code-Transpiler`

```go
import transpiler "github.com/tarekwasfy01/Code-Transpiler"

goSource, err := transpiler.Transpile("c", "go", cSource)
semanticJSON, err := transpiler.SemanticJSON("python", pythonSource)
rustSource, err := transpiler.TranspileSemanticJSON("rust", semanticJSON)
```

The public package exposes all 13 registered languages, all 156 directed
cross-language routes, SemanticProgram JSON and backend capability contracts.

Code Transpiler (UCT) is an experimental matrix-driven many-to-many source-code transpiler. It uses a shared CIR and target emitters so supported languages can translate through one architecture instead of maintaining a separate transpiler for every language pair.

Supported languages: **R, Go, Rust, C++, C, Python, Zig, Julia, Nim, C#, Java, Kotlin, and Swift**.

The architecture exposes **156 directed cross-language routes** (13 × 12). Route availability does not imply exact full-language semantic compatibility; complex language-specific constructs are still being expanded.

Code Transpiler combines R transpilation to **Go, Rust, C++, C, Python, Zig, Julia, Nim, C#, Java, Kotlin and Swift**.

## Matrix resolution

The common matrix has **702 R primitive entries × 12 targets = 8,424 target routes**.

All 12 targets now use the same R lexer/parser instead of the former line-by-line fallback parser. The shared frontend parses:

- assignments (`<-`, `<<-`, `=`)
- calls and named arguments
- unary and binary operators
- vector/index expressions
- `if / else`
- `while`
- `for`
- `repeat`
- function definitions
- `return`, `break`, `next`

The target generators lower operators and primitive calls through the target matrix/runtime dispatch.

## Compatibility policy

A matrix route is not the same thing as exact Base-R semantics. Common primitives have executable native handlers. A rare primitive whose exact semantics are not implemented produces an explicit target-runtime error. Code Transpiler does **not** silently substitute an incorrect implementation.

This is important for environments, promises, active bindings, S3/S4 dispatch, serialization wire formats, native `.Call/.C`, connection internals and other stateful Base-R behavior.

## Build

Run `powershell -ExecutionPolicy Bypass -File .\build-onefile.ps1`.

Output: `dist\CodeTranspiler.exe`

## CLI

`Code Transpiler.exe targets`

`CodeTranspiler.exe routes`

`CodeTranspiler.exe transpile -source c -target go input.c -o output.go`

`CodeTranspiler.exe semantic-export -source python input.py -o program.semantic.json`

`CodeTranspiler.exe semantic-transpile -target rust program.semantic.json -o output.rs`

`Code Transpiler.exe transpile -target cpp input.R -o output.cpp`

`Code Transpiler.exe transpile -target rust input.R -o output.rs`

`Code Transpiler.exe transpile -target python input.R -o output.py`


## Kernel resolution v3

Every generated primitive call now carries both its matrix kernel and primitive name into the target runtime. Go and Python received broad executable kernel implementations for arithmetic/vector recycling, reductions, numeric functions, predicates/coercion, ordering, character functions, random generation, filesystem/environment and time functions. Other target runtimes use the same kernel-aware call ABI and are being filled against the same matrix.


## v5 — all matrix routes executable

All 702 primitive names are now routed in every one of the 12 target languages (8,424 routes total). The former explicit `not implemented` terminal path was replaced by 34 executable kernel fallbacks covering runtime, IO, environment, language, matrix, character, numeric, random, predicate, system, replacement, attribute, serialization, reduction, bitwise, matching, ordering, datetime, subset, cumulative, logical, missingness, iteration and coercion categories. Exact Base-R behavior for runtime-specific internals remains an emulation rather than the original R runtime.


## GUI redesign / embedded R runtime

The GUI now follows the R2Go editor layout and styling: high-contrast syntax highlighting, R editor left, target editor right, centered Convert button, target dropdown in the upper row, and Copy / Save As in the lower-left action row. A Run R button executes the left editor with the Pure-Go compatibility runtime embedded in Code Transpiler.exe. Generated target source continues to embed its target-specific compatibility runtime.


## CLI target execution

`Code Transpiler.exe run input.R` uses the embedded Code Transpiler R runtime. `Code Transpiler.exe run -target go input.R`, `-target rust`, `-target python`, and the other target IDs transpile to a temporary target source and execute it with the matching installed toolchain/interpreter. Temporary files are removed after execution. Open CMD now opens a persistent terminal in the EXE directory.


## Embedded target runtimes

All 12 target compatibility runtimes are now stored under `internal/runtimeassets/data` and compiled into the one-file executable with Go `embed.FS`. `run -target <language>` materializes the matching bundle in its temporary work directory. The bundle also carries the existing full R2Rust `rust_runtime` tree and the available R2Go runtime provenance/source material.
