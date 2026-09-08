# SemanticProgram – Universal Development Instructions

Work on the current `Code-Transpiler` repository and continue developing `SemanticProgram` as a language-neutral, serializable semantic program representation.

The primary goal is NOT to maximize the number of syntax translations.

The primary goal is:

> Preserve program meaning as completely, explicitly and language-independently as possible.

`SemanticProgram` must become capable of representing a program without depending on the original source language or original source text.

## Core architectural rule

Use this architecture:

Source Language
→ Native Frontend
→ SemanticProgram
→ Semantic Validation / Analysis
→ Lowering
→ Target Backend

Do not use another programming language as the semantic bridge.

In particular, Canonical R may remain temporarily for compatibility, diagnostics or legacy tests, but it must never be required to preserve program meaning or to emit another target.

The long-term invariant is:

Source code can be deleted after conversion to SemanticProgram without losing the information required to analyze, execute or translate the supported semantics.

## SemanticProgram is the canonical semantic representation

Treat `SemanticProgram` / `SemanticDocument` as a real program representation, not as metadata attached to another AST.

The serialized JSON must contain everything necessary to reconstruct the supported executable program.

Maintain deterministic:

SemanticProgram
→ JSON
→ SemanticProgram
→ JSON

round trips.

Equivalent documents must preserve:

* executable tree
* node IDs
* types
* bindings
* scopes
* control flow
* data flow
* effects
* evaluation order
* operation semantics
* contracts
* dialect information

Never silently discard unknown semantic fields.

Unknown semantics must remain explicitly `unknown`, be preserved as an extension/dialect value, or cause a clear unsupported-capability error.

Never guess semantics merely because a target language has convenient syntax.

## Prefer semantic operations over syntax

Do not represent meaning only with source operators such as:

`+`, `-`, `[]`, `==`

Whenever meaning is known, normalize it to semantic operations such as:

* numeric.add
* integer.add
* floating.add
* vector.add
* matrix.multiply
* string.concat
* logical.and
* comparison.equal
* collection.index
* function.call

Source spelling may be retained as metadata, but backends should primarily consume semantic operations.

Every operation should be able to carry relevant semantics including:

* dispatch model
* evaluation order
* short-circuit behavior
* overflow behavior
* coercion behavior
* broadcasting/vectorization
* index semantics
* error behavior
* semantic confidence

## Replace textual intermediate events

The highest-priority frontend improvement is to remove text reconstruction between source analysis and SemanticProgram.

Do not do:

Source
→ normalized text fragments
→ shared text parser
→ SemanticProgram

Move toward:

Source
→ typed semantic events / native AST
→ SemanticProgram

Semantic frontend events should eventually carry structured data such as:

* NodeID
* kind
* source span
* scope
* binding
* semantic type
* type origin
* operation
* operands
* effects
* evaluation semantics
* attributes
* extensions

Do not stringify structured information only to parse it again later.

## Native frontends

Migrate languages incrementally.

Do not attempt every language simultaneously.

Build the frontend interface so any language can eventually implement it independently.

Prefer starting with languages that expose important semantic differences, especially:

* Go
* C
* Rust
* Python
* R

Each frontend should preserve source semantics instead of forcing them into a common source-language model.

Examples:

Go:

* concrete integer widths
* signedness
* pointers
* slices
* arrays
* maps
* interfaces
* goroutines/channels later

C:

* integer widths
* signedness
* pointer operations
* arrays
* casts
* storage
* ABI-relevant facts
* undefined/implementation-defined behavior where relevant

Rust:

* integer widths
* references
* ownership
* borrowing
* moves
* mutability
* lifetimes where semantically relevant
* Result/Option
* traits/generics later

Python:

* dynamic dispatch
* object semantics
* arbitrary precision integers
* exceptions
* iterators
* generators
* truth semantics

R:

* lazy arguments
* NA
* NULL
* NaN
* recycling/vectorization
* one-based indexing
* named/missing arguments
* dynamic environments

Do not claim semantic equivalence for unsupported features.

## Structured type system

Continue making `SemanticType` capable of expressing general programming-language types.

The schema should support at least:

* integer(bits, signed)
* floating-point(bits, IEEE semantics)
* boolean
* string/text
* null
* missing
* NA
* NaN
* arbitrary precision integer
* decimal
* complex
* dynamic/any
* pointer
* reference
* optional
* array
* vector
* matrix
* tensor
* tuple
* set
* list
* map
* function
* struct
* class/object
* interface/protocol/trait
* enum
* generic/type parameter

Preserve `type_origin`, e.g.:

* explicit
* inferred
* coerced
* dynamic
* unknown

Do not infer precision that the frontend cannot prove.

A source `uint32` must never become generic binary64 merely because an old runtime used doubles.

## Bindings and scopes

Identifiers must not be identified only by their textual name.

Use stable IDs for:

* nodes
* bindings/symbols
* scopes
* functions
* types where useful

Preserve lexical and later dynamic binding semantics explicitly.

Support:

* declaration
* reference
* mutation
* shadowing
* capture
* parameter binding
* global/local distinction

The binding graph must distinguish two identically named variables in different scopes.

If exact binding cannot be proven, label the relationship conservatively rather than pretending it is exact.

## Semantic relations

Treat semantic graphs/matrices as first-class program information.

Maintain and expand relations for:

* syntax
* control flow
* data flow
* binding
* effects
* scope
* evaluation order
* call modes

Do not duplicate contradictory graph state.

If serialized graphs are included as evidence, recompute and validate them when reading the document.

Reject inconsistent documents.

Design graph storage so additional relations can be added without changing the basic architecture.

## Effects

Use a conservative effect system.

Support categories such as:

* local.read
* local.write
* global.read
* global.write
* memory.read
* memory.write
* memory.allocate
* io.read
* io.write
* filesystem.read
* filesystem.write
* network
* exception.throw
* process
* thread.spawn
* synchronization
* atomic
* ffi
* time
* random
* reflection
* unknown.call

An operation/function can only be classified as pure when all relevant effects are proven absent.

Unknown calls must remain conservative.

The effect model should later support optimization and parallelization, but correctness comes first.

## Evaluation semantics

Do not keep evaluation behavior only as a global program property.

Allow semantics to become progressively more local.

Support distinctions such as:

* eager
* lazy
* call-by-value
* call-by-reference
* call-by-need
* left-to-right
* unspecified order
* short-circuit
* conditional evaluation

A program may contain operations with different evaluation contracts.

Do not force everything into a single global evaluation model.

## Indexing and collections

Do not rely only on global `IndexBase`.

Index operations should eventually contain their own semantics.

Represent where relevant:

* zero-based / one-based
* negative-index behavior
* slicing bounds
* inclusive/exclusive end
* out-of-bounds behavior
* missing index
* multidimensional indexing
* vectorized indexing
* R exclusion indexing
* Python negative indexing

Distinguish collection types instead of flattening all of them into one generic vector/list.

## Special values

Never collapse these into one representation:

* null
* missing
* NA
* NaN
* undefined

They have different semantics in different languages.

Preserve distinctions through JSON, execution and target lowering.

## Explicit conversions

Represent conversions explicitly.

Support conversion modes such as:

* implicit
* explicit
* checked
* unchecked
* saturating
* truncating
* widening
* narrowing
* reinterpret

Do not hide meaningful coercions inside backend code generation.

## Capability model

Expand backend capability contracts.

A backend should be able to classify every relevant semantic feature as:

* native
* lowering
* emulated
* unsupported

Include a reason when useful.

Before emitting code, check required capabilities.

Do not silently emit a semantically different fallback.

Capability information should eventually cover areas such as:

* integer widths
* pointer semantics
* ownership
* exceptions
* classes
* generics
* concurrency
* FFI
* reflection
* GPU operations
* numeric models
* indexing semantics

## Dialects

Keep specialized semantics outside the minimal universal core.

Use versioned dialects/extensions for domains or language-specific constructs that cannot yet be represented faithfully in the core.

Examples:

* r
* rust
* cpp
* python
* gpu
* wasm
* simd
* database
* scientific

A dialect must declare required capabilities.

Unknown dialects must never be silently ignored by a target backend.

Preserve or reject them explicitly.

## GPU architecture

Do not turn the universal core into a shader language.

Keep GPU semantics in a separate `gpu` dialect.

Potential GPU concepts include:

* kernel
* vertex
* fragment
* compute
* thread/work item
* workgroup
* subgroup
* barrier
* buffer
* texture
* sampler
* atomic
* wave/subgroup operations
* vector
* matrix
* cooperative matrix

CrossGL/CrossTL may later be used as an external GPU adapter.

Do not copy CrossTL source into the core unless there is a concrete technical reason and licensing/provenance is handled separately.

Prefer:

SemanticProgram GPU dialect
→ CrossGL adapter
→ CrossTL
→ CUDA / HIP / Metal / HLSL / GLSL / WGSL / SPIR-V

## Preserve high-level semantics

Do not lower high-level constructs too early.

Keep semantic constructs such as:

* functions
* loops
* match
* exceptions
* classes
* methods
* closures
* async tasks
* channels
* matrix operations

at a high semantic level until lowering is necessary.

Do not convert everything immediately into labels/gotos or runtime calls.

Later use multiple IR levels:

SemanticProgram / HIR
→ MIR
→ LIR / Machine IR

SemanticProgram should remain the representation closest to program meaning.

## Source mapping

Propagate `SourceSpan` from native frontends into SemanticProgram.

Preserve:

* file
* start/end offset
* start/end line
* start/end column

Generated diagnostics should eventually point back to the original source construct.

Source locations are metadata and must not define semantics, but they must survive transformations whenever possible.

## Extensibility

Do not require a schema-breaking redesign whenever a new programming-language concept appears.

Maintain controlled extension points such as:

* attributes
* annotations
* extensions
* dialects
* capability declarations

However, do not use generic maps as a substitute for modeling important core semantics.

Once a concept becomes common and stable, promote it from an extension into the typed core schema.

## Semantic confidence

Where useful, attach confidence/status information:

* exact
* inferred
* conservative
* approximate
* fallback
* unknown

Never label approximate or heuristic lowering as exact.

This information should be consumable by diagnostics and capability reporting.

## Validation

Every SemanticDocument must be validated before execution or emission.

Validate at least:

* schema/version
* unique IDs
* valid references
* valid scope hierarchy
* binding references
* node shapes
* type structures
* operation semantics
* dialect requirements
* capability requirements
* graph dimensions
* graph/evidence consistency

Malformed documents must fail explicitly.

## Semantic equivalence testing

Build tests around meaning, not only generated source strings.

Maintain JSON determinism tests.

Add differential testing:

Original Source
→ execute

Original Source
→ SemanticProgram
→ execute

Original Source
→ SemanticProgram
→ Target
→ compile/run

Compare observable behavior where deterministic:

* return value
* stdout
* stderr
* exceptions/errors
* mutations
* file effects where safely testable
* semantic effect summary

Generate many small randomized programs from the supported semantic subset and run them across multiple languages.

Prefer thousands of automated semantic cases over a few large hand-written examples.

## Backend generation

Backends should increasingly consume SemanticProgram information directly.

Do not make backends re-parse generated text or rediscover information already present in SemanticProgram.

A backend should perform:

SemanticProgram
→ capability check
→ target-specific lowering
→ code generation

Keep lowering separate from pretty-printing.

## Universal semantic library

Prepare the architecture for standardized semantic operations that can be lowered differently per target.

Possible namespaces:

* math
* vector
* matrix
* tensor
* statistics
* collections
* string
* filesystem
* network
* concurrency
* signal
* image

For example, represent matrix multiplication as a semantic operation rather than manually expanded loops whenever the source meaning is known.

A target may lower the same operation to:

* native syntax
* standard library
* optimized library
* SIMD
* BLAS
* GPU
* fallback implementation

without changing program meaning.

Do not prematurely implement a massive standard library; define the mechanism first.

## Future authoring language

Do not prioritize a custom text language until SemanticProgram JSON and semantics are stable.

Eventually a compact SemanticProgram DSL may be added as an authoring/view layer.

The DSL must parse into the same canonical SemanticProgram and must not become a second competing semantic model.

JSON remains the stable interchange representation.

## Compatibility policy

Backward compatibility matters once a SemanticProgram schema version is published.

Do not silently change existing field semantics.

For incompatible changes:

* increment schema version
* provide migration when practical
* reject unsupported versions explicitly

Prefer additive evolution when semantics remain compatible.

## Development priorities

When deciding what to implement next, use this order:

1. Semantic correctness.
2. Information preservation.
3. Validation.
4. Native source semantic extraction.
5. Type/binding/control/effect accuracy.
6. Semantic equivalence tests.
7. Backend capability correctness.
8. Additional language features.
9. Additional target languages.
10. Performance optimization.

Do not add a new target language merely to increase the supported-language count if existing frontends still lose important semantics.

## Immediate high-value work

Prioritize:

1. Replace text-based matrix events with typed semantic events.
2. Implement progressively more native frontend parsing/lowering.
3. Start with strong semantic preservation for Go, C and Rust.
4. Populate real structured types from source declarations and inference.
5. Populate real SourceSpan information.
6. Improve binding/shadowing/capture correctness.
7. Make operation semantics more specific than generic `add`, `call`, etc.
8. Expand capability matrices.
9. Add cross-runtime semantic equivalence tests.
10. Keep CanonicalR only as compatibility/debug output and continue removing dependencies on it.

## General implementation rule

Whenever choosing between:

A) a shortcut that produces more translated code

and

B) a representation that preserves more semantics

choose B.

Never hide unsupported behavior behind a best-effort translation unless it is explicitly marked as emulation/approximation.

A smaller exact semantic subset is more valuable than a large subset with silent semantic corruption.

The long-term goal is:

Any supported language
→ SemanticProgram
→ any supported language

while preserving program meaning as far as the declared semantic and capability contracts permit.
