# Semantic frontend migration

The executable SemanticProgram, not a source language, is the semantic boundary.
Migration order: integrity checks, typed frontend input, native Go extraction,
C and Rust extraction, then differential execution across supported backends.

## Integrity baseline

The importer now rejects unknown JSON fields outside declared extension maps,
multiple JSON documents, and executable trees whose annotations cannot survive
reconstruction. It still recomputes and compares the evidence matrices.
Required program capabilities are checked against every target before emission.

This deliberately rejects explicit types and node extensions
that the present runtime tree cannot preserve. Source spans now survive the
roundtrip; stale mappings after tree changes are rejected. Schema expressiveness is not
implementation support. Document-level extension maps remain supported.
Consumers must use the generated canonical IDs and annotations for schema v1;
arbitrary renumbering is currently rejected rather than normalized silently.

## Next implementation boundary

Native frontends should produce structured executable nodes with source spans,
types and lexical binding identities directly, never reconstructed R text.
First extend the runtime tree and roundtrip codec to retain those annotations.
An independent frontend interface and a Go parser/type-checker implementation
now exist. They extract analysis events without calling the legacy parser.
Direct executable lowering now exists for a deliberately bounded Go scalar
subset, using the native AST rather than the analysis artifact as its input.
Unsupported input must be explicit;
do not silently retry the legacy text normalizer after a semantic failure.

Exact integer types require exact execution and lowering or explicit backend
rejection. Adding a uint64 annotation to the existing binary64 runtime does not
provide uint64 semantics. Apply the same rule to pointers, borrowing and casts.

## Verification matrix

The current integrity tests vary IDs, scope, binding, type, operation, effects,
source span and node extension data. Capability tests cross unsupported and core
requirements with every registered backend. Existing deterministic JSON tests
continue to compare emission across all targets.

The randomized differential test now covers 1,024 bounded arithmetic cases.
It compares a reference Go program, SemanticProgram execution, JSON roundtrip,
and generated Go execution. Enable external execution with
`CODETRANSPILER_NATIVE_E2E=1`. It tests runtime/backend behavior, not the new
native frontend. Signed zero distinguishes integer and binary64 arithmetic;
the equivalence subset excludes multiplication by zero for that reason.
Neither a valid roundtrip nor successful emission proves native equivalence.

## Native analysis API and CLI

`NativeAnalysisJSON("go", "input.go", source)` uses Go's native parser and type
checker. It retains exact literal spelling, integer widths and signedness,
pointer and collection types, lexical scope membership, and symbol identity.
Imports require a module resolver and are currently rejected. Type checking
uses gc/amd64 explicitly; platform-dependent integer widths remain unknown in
the projected type schema. This does not claim architecture independence.

`native-analysis -source go input.go -o analysis.json` produces the same artifact.
It is explicitly non-executable and cannot be passed to semantic-transpile.
Its binding-incidence matrix B produces same-symbol relations via B * B^T.
This relation is not dataflow, execution order or a proof of purity.

`CapabilityMatrixJSON(features)` and `capability-matrix` expose 27 standard
feature axes across 13 targets (351 cells), plus any caller-supplied features.
Native/lowering/emulated/unsupported status planes are mutually exclusive.
Emission checks the requirement vector against the unsupported plane by matrix
multiplication. Unsupported entries remain unsupported until a lowerer is
implemented and tested; target syntax alone never establishes support.

## Remaining implementation work

- Extend typed executable lowering beyond the current scalar subset.
- Native C and Rust frontends; module/import resolution for Go.
- Extend exact integer operations beyond the implemented wrap/conversion subset; add other numeric models.
- High-level exceptions, ownership, classes, generics, concurrency and FFI.
- Broader differential testing, including native frontend paths and file effects.
- Operation registry/lowering library, GPU adapters, and later MIR/LIR layers.

These are not implemented by the new analysis layer or capability matrix.
See SEMANTIC_DEVELOPMENT.md for the complete user-supplied architecture rules.

## Direct executable Go frontend

`NativeSemanticJSON("go", filename, source)` and
`semantic-export -native -source go input.go -o program.json` now produce an
executable SemanticDocument without normalizing or reparsing R text. JSON import
also no longer rebuilds CanonicalR or a lexical R graph. Evidence matrices are
the imported program's relation representation.

Supported scalar subset: package main with a main function, bool and printable ASCII
string values, scalar local assignments/declarations (including zero values),
block shadowing, if/else, condition-only for, unlabeled break/continue, and
fmt.Println with one string argument. String escapes, quotes, backslashes and
Unicode are rejected until their target codecs are verified. Variable string
concatenation, unimplemented numeric operations, global declarations, parallel
assignment, concurrency and additional imports are rejected. There is no
fallback to the legacy frontend, including in unsupported dead code.

Go object identities are lowered to unique internal names, preserving block
shadowing even though the legacy runtime has function-scoped storage. Original
binding types are retained in the document extension native_binding_types;
this is not yet a general native type system in the executable runtime.
Source spans survive JSON. Runtime resource limits still apply to execution.

The requirement native.go.scalar gates execution/emission. It is supported by
the embedded runtime and Go, Python, Rust and C targets. Other native-source target routes
remain disabled until their observable behavior has been validated. Existing
legacy routes remain unchanged. The strict path is opt-in through the API/flag.

The native differential test includes 32 scoped control-flow cases, compares
original Go execution, SemanticProgram execution, JSON-restored execution and
generated Go/Python/Rust/C execution, and checks stdout/stderr/errors. This is separate from
the 1,024 arithmetic IR/runtime cases. Both external tests were run successfully
with CODETRANSPILER_NATIVE_E2E=1. The CLI smoke yields inner/outer/done/different.

## CLI routing and expanded target validation

The transpile command accepts every registered source/target pair in default
compatibility mode. It now supports extension-based source detection, from/to
aliases and target=all with a per-target JSON report. Identity routes copy the
source to a different path. Native mode validates even identity inputs.
169 generated-fixture CLI routes (13 x 13) passed; these measure routing, not
complete source-language equivalence. Additional C/Python/Go source smokes pass.

Native backend comparisons exposed string equality errors in C and Rust plus
Windows CRLF output in C/Python. Equality and inequality now compare string
contents; their existing numeric branches remain unchanged. C/Python generated
runtimes now use LF stdout, matching the semantic runtime. Candidate execution
tests compare bytes without newline normalization and pass across 32 scoped
cases with both equal and unequal strings. Native source features outside the
declared scalar subset remain unsupported.

## Native helper functions

The strict Go frontend now lowers nonrecursive helper declarations, named
bool/string/fixed-width integer parameters, zero or one unnamed scalar result, direct eager calls,
and conditional returns into structured SemanticProgram function nodes.
The sparse `native_call_graph` extension records caller-to-callee edges;
`native_call_graph_axes` identifies their internal function names. Dependency
ordering supports functions declared before their callees. Cycles are rejected.
The matrix is frontend analysis metadata, not trusted executable authority.

The additional `native.go.functions` requirement is enabled for the embedded
runtime and Go/Python/Rust/C/C++ targets. Variadics, multiple/named results,
indirect calls, recursion and architecture-sized numeric parameters remain explicit errors. The capability matrix now has 27 base
features by 13 targets (351 cells).

Execution tests compare original Go, JSON-restored SemanticProgram, generated
Go, Python, Rust, C and C++ (with optimization). Cases cover parameter mutation without changing caller bindings,
declaration ordering, nested calls, left-to-right exactly-once evaluation of
arguments including unused parameters, conditional returns, and skipped or
executed side effects under boolean short-circuit operators. Runtime boolean
short-circuit handling was corrected; non-boolean coercion retains its previous
behavior. JSON roundtrips remain deterministic. This is a bounded semantic
extension, not completion of native Go or of the universal type system.

Native void fallthrough is now lowered to an explicit return. This permits
the same control-flow matrices to handle empty functions and functions ending
after a side effect on every enabled target. Go expression statements explicitly
discard boxed results, including nil; the generated-code decoder recognizes
that discard rather than inventing a variable binding. Binary operands with
effects use ordered local value bindings, without moving effects outside an
enclosing branch or short circuit. These are shared lowering rules.

The reproducible source fixture is `internal/backend/testdata/native_functions.go`;
`matrices/native-functions.semantic.json` contains its executable document and
call graph. Run `go test ./internal/backend -run TestNativeFunction -count=1 -v`
with `CODETRANSPILER_NATIVE_E2E=1` to include original and generated execution.
The test requires Go, Python, rustc, gcc and g++ on PATH; without that environment
flag it checks only the semantic runtime, serialization and capability gates.

## Universal exact integer operations and adapter matrix

The executable tree now supports typed core OperationExpr nodes. Each carries
an integer domain, semantic operation and ordered operands. Integer values use
bit patterns rather than float64, with explicit modulo-2^width arithmetic and
explicit narrowing/widening conversions. The additive tagged_exact_scalars_v1
value contract distinguishes these programs from the legacy binary64 contract.
Source parameter types are preserved in the typed core; declaration origins also
remain in native binding metadata. General annotations are not all modeled yet.

The implementation registry has 19 operation rows and 28 stage columns covering
JSON, runtime and every registered source/target adapter. The feature matrix has
27 rows by 13 targets. Code emission checks the actual operation requirement
vector against the unsupported plane, independently of optional capability
strings. Type checks reject invalid widths/literals, incompatible operands and
integer values entering legacy float operations. Exact target implementations
exist for Go, Python, Rust, C and C++; native executable input remains bounded Go.

See IMPLEMENTATION_MATRIX.md for the authoritative scope and test commands.
The additional differential matrix exercises 1,472 cases over eight integer
domains; this is distinct from the older 1,024 binary64 arithmetic cases.
