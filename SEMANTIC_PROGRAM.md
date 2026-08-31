# SemanticProgram v1

## As authoring format

`SemanticProgram` is a complete JSON transport for the executable core, not a
matrix side-car. Every serialized statement, expression and parameter has a
stable integer `id`, lexical `scope`, structured `type` with `type_origin`,
local `semantics`, effects and optional attributes or extensions. Literal
values stay textual, so JSON numeric precision cannot change a program.
`NULL`, `NA`, `NaN` and a missing call argument are distinct.

The header also contains origin, metadata, extensions and semantic contracts
(`requires`, `ensures`, `invariants`). Extensions are producer data: a backend
must reject or preserve unknown extensions deliberately, never claim that it
understood them. COO evidence continues to store syntax, control, data,
binding, effect, evaluation-order and scope relations.

Node-local semantics record builtin operation identity, dispatch,
short-circuit behaviour, evaluation mode and per-index base, negative-index,
out-of-bounds and slicing contracts. Facts that a frontend cannot prove are
encoded as `unknown`, not upgraded to `exact` for a convenient target syntax.

The executable v1 core is blocks, assignment, expressions, functions,
named/default/missing arguments, calls, indexing, conditionals, loops,
return/break/continue, unary/binary operations and iteration intrinsics.
Classes, modules, exceptions, casts, generics, FFI and concurrency are not
accepted as executable nodes yet: they require real analysis and lowering in
every relevant backend.

## Structured types and capability contracts

`SemanticType` can represent signed/unsigned integer and IEEE floating-point
widths, pointer/reference/optional ownership, arrays/vectors/matrices, maps,
functions with parameter/result types, structures/classes/enums and generic
constraints. A frontend sets `type_origin` to `explicit`, `inferred`,
`coerced`, `dynamic` or `unknown`. The current common frontends retain their
honest binary64/dynamic contract until they prove a narrower native type.

`BackendCapability(feature, backend)` returns `native`, `lowering`,
`emulated` or `unsupported`, with a reason. Emission must not reinterpret an
unsupported feature as a fallback. This is the contract basis for future
ownership, exceptions, FFI, classes, generics, concurrency and GPU lowering.

## Effects and semantic equivalence

The effect matrix distinguishes local/global and memory access, I/O, file
access, network, allocation, exceptions, thread spawning, synchronization,
FFI, time, randomness, unknown calls and control flow. `SummarizeEffects`
derives a conservative purity result from that matrix.

`RunSemantic` executes the deserialized tree directly. The round-trip test now
compares deterministic observations (stdout, error and effect summary) for the
original SemanticProgram and its JSON-decoded copy, without regenerating R or
another source language.

## Registries and GPU dialect boundary

The core now exposes declarative frontend/backend registries and a deterministic
SemanticDocument visitor. This lets a frontend or backend declare aliases,
extensions, supported capabilities and dialects before it is selected. The
registry is the capability gate for future exception, FFI, generic, class and
concurrency lowerers.

GPU work belongs in a separate `gpu` dialect, with explicit stages, resources,
layouts and capability checks. [CrossTL design boundary](CROSSTL_DESIGN.md)
records why CrossTL is a potential external shader adapter, not bundled core
code.

`SemanticProgram` is now the transport format between a language adapter and a
target emitter. Its JSON document has schema `r2many.semantic-program`, version
1. It carries a recursive executable tree, evaluation contract, value contract
and index base. It does not carry Canonical R or an original-source copy.

The current contract is deliberately narrow:

- dynamic vectors, explicit null, UTF-8 text and R-compatible truth;
- binary64 numbers;
- one-based indexing;
- lazy-demand or eager-left-to-right calls, represented explicitly;
- unknown integer width, pointer, ownership and ABI.

`unknown` is a contract value, not permission for an emitter to guess. The
current generator rejects documents that claim a pointer, ownership, ABI or
integer-width model it does not implement.

## Boundaries

```
source language -> source adapter -> SemanticProgram JSON -> target emitter
generated target -> verified target decoder -> SemanticProgram JSON -> target emitter
R source <-> R adapter
```

The existing source adapters for C, Go, Python, Rust and the other languages
now hand an ordered matrix-action/event stream, including structural block
closures, to the SemanticProgram lowerer. `CanonicalProgram.R` is no longer
consumed on the normal parse path. The common-subset expression decoder still
uses the R-shaped action grammar internally; replacing it with native parsers
per source language is the remaining frontend migration.

The field `Program.CanonicalR` remains temporarily for compatibility with older
diagnostic and embedded-R tests. It is regenerated from `SemanticProgram`; it
is never used by `Emit`, semantic fanout or semantic function-flow analysis.

## Adapter modes

The matrix adapter exposes:

- `semantic-document`: source code to a JSON semantic document;
- `from-semantic-document`: JSON semantic document to a requested target;
- `semantic`: the semantic evidence view;
- `function-flow`: flow matrices from the in-memory semantic program.

These modes are test/audit interfaces and do not build a release executable.

## What v1 now serializes

The document contains the executable body plus checked semantic evidence:
explicit node IDs, source origin, scopes, candidate lexical bindings and sparse
syntax, control, data, binding, effect, evaluation-order, call-mode and scope
matrices. JSON import recomputes evidence from the executable body and rejects a
document whose graph, axes or contract has changed. COO matrix input rejects
duplicate and out-of-range entries.

`SourceSpan` fields, per-node static types and declaration-timing proof are not
yet present. The v1 binding matrix is lexical-candidate evidence; it does not
claim exact dynamic-environment or shadowing semantics.

## Next expansion

The next compatible schema version should add first-class static integer widths,
aggregate/layout types, pointer provenance, ownership/lifetime and ABI calls.
Those additions need independent source and target evidence; they must not alter
the meaning of v1 documents.
