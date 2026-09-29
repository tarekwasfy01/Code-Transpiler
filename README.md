# CodeTranspiler.jl

A **binary-free, native Julia transpiler** with a local browser GUI. Version 0.4 uses Julia as a semantic pivot: each supported frontend lowers source code to Julia, and each target emitter projects the supported Julia subset to the requested language. No EXE, DLL, JLL, Go compiler, or external backend is bundled or invoked by the package.

## Languages

The package now has native frontend/target paths for **14 languages**:

- R
- Go
- Rust
- C++
- C
- Python
- Zig
- Julia
- Nim
- C#
- Java
- Kotlin
- Swift

For syntax inside the common supported subset, any source can be routed through Julia to any target. `routes()` therefore exposes the 182 non-identity language pairs. Identity routes are returned unchanged.

This does **not** mean every feature of all 14 languages is implemented. The translator is deliberately conservative: specialized constructs that cannot yet be lowered safely should fail rather than silently change semantics.

## Common native subset

The multi-language pivot currently covers scalar functions, typed parameters and return values where the source supplies them, local assignments, returns, basic arithmetic and boolean expressions, `if`/`else`, `while`, common counted/range loops, and common print operations. The mature Go frontend additionally handles structs/methods, slices, arrays, maps, `make`, `append`, Go zero values, grouped parameters, integer division, string concatenation, and several standard-library mappings.

### C / C++

C and C++ received additional semantic handling for ordinary typed functions, scalar declarations, classic counted `for` loops, simple fixed/local array literals, zero-based array indexing translated to Julia's one-based indexing, `printf` with one common scalar conversion, `puts`, `std::cout`, and increment/decrement statements. More complex pointer arithmetic, macros, unions, templates, aliasing, and ABI-sensitive constructs are not yet complete.

## Quick start

```julia
using CodeTranspiler

code = transpile("python", "rust", """
def add(a: int, b: int) -> int:
    return a + b
""")
println(code)
```

Go to Julia remains available directly:

```julia
julia_code = transpile("go", "julia", """
package main
func add(a int, b int) int { return a + b }
""")
```

## Semantic output

The package exposes semantic analysis separately from generated code:

```julia
p = semantic_program("python", source)
println(semantic_output("python", source; target="rust"))
println(semantic_output("python", source; target="rust", format=:json))

result = transpile_with_semantics("python", "rust", source)
println(result.code)
println(result.semantics)
```

The report includes source evaluation/value model, source and canonical index bases, discovered functions/bindings, normalized operations, control flow, effects/mutations, features, and target-specific semantic warnings. The browser GUI shows this report in its third pane.

Collection projection in v0.4 also preserves common vector/list behavior more carefully: index-base conversion is protected against double shifting; Java/C#/Kotlin use resizable collections where Julia `Vector` mutation is required; and length/index integer types are normalized for Go, Rust, Swift, Java, Kotlin and related targets. C and Zig dynamic append currently fail explicitly rather than emitting known-wrong allocation semantics.

### SE (Semantic Exchange) as input and output

`SE` is a first-class language in the registry and GUI. A `.se` document contains deterministic semantic JSON plus the canonical Julia pivot. This makes semantic output reusable as input:

```julia
se = transpile("python", "se", python_code)
rust = transpile("se", "rust", se)
```

The format starts with `SE/1` and rejects malformed or pivot-less input instead of guessing.

## GUI

```julia
using CodeTranspiler
CodeTranspiler.gui()
```

Open `http://127.0.0.1:8765/`. Both dropdowns expose all 14 languages. Stop the server with `CodeTranspiler.stop_gui()`.

## Verification

The package is tested with the supplied **Julia 1.13.1** binary. The native test suite currently contains 142 passing checks. It executes generated Julia from every frontend, checks every target emitter, cross-language pivot routes, semantic text/JSON output, Go semantics, C zero-based indexing/loop semantics, and collection/index projection regressions.

Generated common-subset targets were additionally accepted by the compilers available in the build environment: GCC (C11), G++ (C++17), Go, Python, Java (`javac`), Kotlin (`kotlinc`), and Swift (`swiftc`). Compilers for Rust, Zig, Nim, C#, and R were not available in this environment, so those target emitters have Julia-side tests but not an external compiler check here.

## Safety of translation

Code transpilation is semantic work, not just syntax replacement. Unsupported or ambiguous language-specific features should be treated as incomplete until a dedicated lowering exists. In particular, pointers/unsafe code, concurrency primitives, reflection, complex generics/templates, preprocessor behavior, ownership/borrowing semantics, exceptions across all languages, and advanced collection/index semantics still need deeper per-language implementations.

## License

MIT, matching the upstream Code-Transpiler project.
