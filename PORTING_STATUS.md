# Porting status

## Native / binary-free

The package no longer contains or searches for an external transpiler executable. Version 0.4 has native Julia frontends and target emitters for all 14 languages in the registry, including SE as a round-trippable semantic exchange format, connected through a Julia semantic pivot.

### Verified frontends

For R, Go, Rust, C++, C, Python, Zig, Julia, Nim, C#, Java, Kotlin, and Swift, a representative scalar `add` function is lowered to Julia, loaded by Julia 1.13.1, and executed with the expected result.

### Verified targets

All 13 Julia target emitters are covered by tests. Basic generated code was also compiler-checked where a compiler exists in this environment: C, C++, Go, Python, Java, Kotlin, and Swift.

### Go frontend

Most developed frontend. Includes functions, methods, structs, grouped params, zero values, slices, fixed arrays, maps, make/append, range, known-container index translation, integer division, string concatenation, and selected fmt/strings/math/errors mappings.

### C / C++ frontend

Includes typed scalar functions/declarations, return/if/else/while, common counted loops, simple local array literals, known-array zero-based index correction, printf/puts, C++ cout, and ++/--.

### Semantic output

`semantic_program`, `semantic_output`, and `transpile_with_semantics` expose the normalized semantic pivot. Reports include source/canonical index bases, bindings/types, operations, control flow, effects, features, and target warnings. The GUI renders the semantic report alongside generated code.

### Collection projection

Common vector/list literals, indexing, `length`, and `push!/append` are projected to target-native forms. Java uses `ArrayList`, C# uses `List<T>`, Kotlin uses `MutableList<T>`, C++ uses `std::vector`, Rust uses `Vec`, Go uses slices, Swift uses arrays, Nim uses sequences, Python uses lists, and R uses vectors. C and Zig dynamic append are deliberately rejected until allocator/capacity semantics are modeled. Index conversion now guards against accidental double 0/1-base shifts.

### Shared multi-language subset

Functions, parameters/returns, assignments, basic expressions, branching, while loops, common range/count loops, and printing. Python/Nim indentation and brace-based frontends are handled separately. R function syntax has its own lowering.

## Still incomplete

- Full pointer/reference/aliasing semantics
- C/C++ preprocessor, macros, unions, templates and ABI details
- Rust ownership, borrowing, lifetimes, traits, macros and async
- Go goroutines/channels/select/defer/interfaces/generics/unsafe/reflection
- Python descriptors, generators, decorators, async, metaclasses and dynamic monkey-patching
- JVM/.NET reflection, annotations/attributes, exceptions and advanced generics
- Zig comptime and advanced pointer/slice semantics
- Nim macros/templates and advanced metaprogramming
- Swift protocols, optionals/error model and concurrency
- R vector recycling, NA semantics, lazy evaluation and NSE beyond the existing runtime groundwork
- Exact collection indexing semantics for every container type in every language

The intended policy remains: reject or clearly expose unsupported semantics rather than intentionally emit known-wrong code.
