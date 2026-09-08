# SemanticProgram v1

## Canonical invariant

`SemanticProgram` and the Universal AST are one canonical IR. Once a frontend
has performed the one-time legacy projection, `UniversalASTDocument` is the
only semantic truth owned by a `SemanticProgram`. `Body`, `SemanticDocument`
and the old statement/expression values are detached views regenerated from
that UAST. Mutating such a view cannot overwrite the UAST.

A legacy `SemanticDocument` without UAST may still be imported once. A valid
but richer UAST can be serialized and validated directly; execution is refused
unless the active backend proves it preserves the complete payload. The
deterministic reverse-and-reproject check rejects extra facets, fields or
relations that the temporary legacy backend adapter would otherwise lose.

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

`RunSemantic` validates and executes the canonical UAST graph directly. It
uses UAST fields plus `syntax.child`, branch, call and operand relations; it no
longer reconstructs a `SemanticDocument` or reads a legacy `BlockStmt` as
semantic input. After successful execution it rematerializes the public
`Body` compatibility view so external legacy mutations cannot become a second
semantic truth. Round-trip tests compare deterministic observations without
regenerating R or another source language.

R output, signature checks, call resolution and typed-operation validation are
also direct UAST consumers. Generic target emission and function-flow analysis
currently use small, transient syntax-subtree views derived from UAST nodes;
they do not create a full legacy document or write data back into the UAST.

The facet execution gate multiplies demanded structure, facet and relation
vectors by the target capability planes. Each matrix cell is `direct`,
`lowerable`, `runtime-required`, `unsupported` or `unknown`; unsupported and
unknown semantic demand is rejected explicitly.

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

Source spans and per-node type fields are present. Structural type, occurrence,
incidence and nominal-reference matrices are derived and validated on import.
These representations do not establish complete static type checking or
declaration-timing proof. The v1 binding matrix is lexical-candidate evidence; it does not
claim exact dynamic-environment or shadowing semantics.

## Next expansion

The next compatible schema version should add first-class static integer widths,
aggregate/layout types, pointer provenance, ownership/lifetime and ABI calls.
Those additions need independent source and target evidence; they must not alter
the meaning of v1 documents.
# Embedded eight-language semantic feature space

SemanticProgram carries a calculated feature model for Go, Python, R, Rust,
C/C++, Kotlin, Java and C#. It contains 98 shared features, 434 namespaced
language-specific features, 82 universal node kinds and 23 relation kinds. The
active language is a one-hot vector. Generic, dialect, node and relation demand
vectors are matrix products and are recomputed during JSON validation.

## Universal AST schema matrix v1

`SemanticProgram.UniversalAST` is the canonical matrix-derived schema. Each
node has exactly one of 109 structural kinds, a sparse subset of 334 exact
semantic quotient facets and a field mask derived from the structural/facet
applicability matrices. Relations use the closed catalog of 55 concrete kinds.
The embedded basis also preserves 44 semantic axes, 23 relation axes, 57 fields,
17 layers and the SemanticProgram coverage lower/upper interval matrices.

JSON import checks the 553-to-334 one-hot quotient, exact signature classes,
dimensions, coverage intervals, language-facet vector, node field masks,
relation applicability and references. Custom UAST payloads remain representable
but executable backends reject them until a lowering proves preservation.

The generated coverage report is written to
`outputs/uast-coverage/coverage.json` and `coverage.csv`. It distinguishes
schema representability, compatibility projection and direct execution; none
of these counts is presented as full language-semantic parity.
