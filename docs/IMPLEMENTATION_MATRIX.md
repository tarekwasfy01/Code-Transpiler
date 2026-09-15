# Universal implementation matrix

The matrix is used by the compiler, not only by this document. `implementation-matrix`
and `ImplementationMatrixJSON()` expose the same operation registry and availability
planes that gate typed operation emission. Operations remain independent of source
language. Each frontend maps native semantics into them; each backend implements
their specified behavior.

## Current executable boundary

| Component | Implemented | Still unsupported |
|---|---|---|
| Core values | Signed/unsigned 8, 16, 32, 64-bit integers; existing legacy values | Arbitrary precision source integers, native floating-point contracts, pointers/ownership |
| Typed operations | 19 operations: constants, loads, format, conversion, negate/complement, add/subtract/multiply, bit operations, comparisons | Division, remainder, shifts and additional overflow policies |
| Native input adapters | Bounded Go scalar/functions/control subset | Native C, C++, Rust, Python, R, Zig, Julia, Nim, C#, Java, Kotlin, Swift extraction |
| Exact integer output adapters | Go, Python, Rust, C, C++, Java, C# | R, Zig, Julia, Nim, Kotlin, Swift |
| JSON | Typed core operations, integer parameter types, local operation contracts, source spans, evidence validation | General arbitrary node annotations/signatures outside modeled forms |
| High-level features | Existing bounded functions and control flow | Classes, exceptions, ownership, generics, FFI, concurrency and GPU |

Default compatibility translation still exposes all 13 language routes. It is
separate from the strict native adapter matrix and does not establish full native
language equivalence. In particular, generating Rust/C does not implement native
Rust/C source extraction.

## Matrices and calculation

- Operation matrix: 19 rows by 28 stages: JSON, runtime, 13 frontends, 13 backends.
- Feature matrix: 27 rows by 13 target languages.
- Implementation planes are mutually exclusive: implemented or unsupported.
- `required_operations * unsupported` counts missing implementations per stage.
- Source-target availability is the outer product of the source and target
  acceptance vectors for the required operation set. The emitted matrix uses
  all 19 operations. It is not proof of a successful multi-hop roundtrip.
- Scope, operand order, call graph, dataflow and binding relations remain separate
  semantic matrices. Integer arithmetic is not replaced by a floating-point matrix
  approximation: exact values use integer bit patterns and specified modular rules.

The requirements used for gating are extracted from executable operation nodes
and typed parameters. Deleting optional producer capability strings does not enable
an unsupported backend. Type mismatches and integer values fed to legacy floating
operators are rejected. Native unsupported syntax has no silent compatibility fallback.

## Reproducible validation

`TestNativeIntegerDifferentialMatrix` exercises 1,472 operation/conversion cases,
including boundaries and deterministic random values. Its eight rows correspond to
the integer domains; each row is compiled independently to keep memory bounded.
An independent `math/big` oracle checks the semantic runtime after JSON roundtrip.
With `CODETRANSPILER_NATIVE_E2E=1`, original Go and generated Go, Python, Rust, C
and C++ are compiled/run and stdout/stderr compared. C/C++ use optimization.

`TestNativeExactIntegerPipeline` additionally exercises helper calls, parameter
types, loops, zero initialization, values above 2^53 and 64-bit wrap boundaries.
`TestIntegerImplementationMatrixValidation` checks rejection and matrix projection.
Original arithmetic and function/side-effect differential tests remain separate.

Required tools for the complete native test run: Go, Python, rustc, gcc, g++ on PATH.
Without the environment flag, external execution is not performed. Matrix support
declarations must not be described as execution evidence.

No complete adapter implementation is claimed for unsupported cells. The next work
is to fill those cells through implementation and independent semantic validation,
not by adding aliases or permissive fallback routes.

## UB01: structural type projection (2026-08-31)

This is partial UB01 implementation, not completion of the 55-feature bundle.
The shared HIR type schema projects structural types into a sorted document-local
table and a binary parent/child adjacency matrix. Type IDs may change when types
are added; this is not yet a cross-document identity or type-equivalence system.

| Type family / invariant | Structured schema | Native Go capture | Derived type matrix | Executable cross-target semantics |
| --- | --- | --- | --- | --- |
| Generic parameters and arguments | Yes | Yes, named/alias/function types | Yes | Not established |
| Type parameter constraints | Yes | Yes | Yes | Not established |
| Interface methods and embedded types | Yes | Yes | Yes | Not established |
| Union terms and underlying-type sets | Yes | Yes | Yes | Not established |
| Default argument and operation-domain types | Yes | Existing producer dependent | Yes | Existing operation subset only |
| Cyclic in-memory type pointers | Rejected before projection | Finite named references instead | Rejected | Not applicable |
| Graph supplied without matching table | Legacy absence of both allowed | Not applicable | Rejected | Not applicable |

Projection tests check native Go generic/recursive structures, constraint terms,
interface methods, deterministic JSON roundtrip, omitted occurrence classes and
invalid graph input. The compatibility graph remains binary. New incidence
matrices preserve field/parameter roles, order and repeated children (see below).
Named-reference resolution, ABI/layout semantics and executable generic lowering
remain open. No archive completion score is increased
solely because these fields exist.

### UB01 relation projection completed

Every newly generated semantic document includes a derived `type_relations`
object. Older v1 documents without that object remain readable. Supplied relation
data must exactly match the HIR-derived projection; deleting just the type table
does not bypass validation. Removing all optional projections is allowed for
legacy compatibility and does not establish trust in producer claims.

| Matrix / vector | Dimensions | Meaning |
| --- | --- | --- |
| U (`uses`) | type occurrences x structural types | One type per nonempty typed HIR slot |
| P (`parents`) | structural edges x structural types | Parent type of each labelled edge |
| C (`children`) | structural edges x structural types | Child type of each labelled edge |
| U transpose * ones (`usage_counts`) | structural types | Number of direct HIR uses, not recursive reachability |
| P transpose * C | types x types | Counts repeated direct structural connections |

Occurrence rows use deterministic JSON paths and do not depend on potentially
repeated node IDs. Attributes/Extensions maps are excluded. Edge rows retain role,
index, member name and underlying-type-set flag. The shared child vocabulary
covers element, key, value, result, constraint, parameter, type_parameter,
type_argument, embedded, field, method and term. All current producers use the
same projection; this adds no source-language parser or target-runtime support.

Tests verify exact expected occurrence paths and usage counts, repeated parameter
edges, incidence multiplication, serialization and rejection of mutations to all
relation planes. These tests establish representation integrity, not type checking
or cross-language semantic equivalence.

### UB01 nominal references and native analysis

Types can now carry explicit `identity` and `reference` fields. Go capture uses
package path, declaration position, name and instantiated type spelling for a
source-local identity. It distinguishes shadowed declarations and generic
instantiations; it is not stable linkage across file sets or source edits.
Recursive types end in finite identity references. Anonymous pointer/slice
structure is retained until a named declaration closes the cycle.

The nominal projection groups definition views and references by sorted identity
columns. With D = definition-to-identity and R = reference-to-identity, resolution
is R * D transpose. A reference can connect to multiple captured views of the
same definition; those views are not asserted to be structurally interchangeable.
A binary unresolved vector flags references with no captured definition. Missing
identities and references containing definition children are invalid. Dangling
references remain visible as analysis gaps, not invented definitions.

Native Go analysis now emits the same structural table, graph and relation
matrices as HIR projection, with occurrence paths pointing to its event types.
Analysis remains non-executable. Existing v1 documents without the nominal plane
remain readable. No backend capability cells are enabled by this analysis work.
Cross-module resolution, alias expansion, assignment compatibility and ABI/layout
remain open; the full UB01 bundle is not complete.

## Python handoff: CL01 in progress

`run-python-handoff.ps1` recalculates the supplied feature/cluster/axis products
locally, rescans CPython through Python's AST, generates declaration incidence
matrices and runs signature differential verification plus project tests.
The top cluster remains CL01. No imported coverage cells are manually updated.

The shared `BindSignature` primitive derives parameter x argument incidence and
uses its row-sum vector to validate cardinalities and select defaults. All five
parameter modes are supported: positional-only, positional-or-keyword,
variadic-positional, keyword-only and variadic-keyword. Source argument order is
retained in columns. Unexpanded spreads are explicitly rejected. Generated
differential verification compares 640 calls with Python inspect.Signature.bind.

This is an implemented binding primitive and a declaration syntax collector,
not completion of CL01. Existing executable HIR call paths have not yet adopted
the richer signature contract. Module/class execution, annotation evaluation,
generic instantiation and target parity remain open. The original mixed-source
collector was not supplied, so its exported propagation operator is reproduced
separately from fresh AST-based evidence. No global completion increase is claimed.

## Joint handoff execution

`run-all-handoffs.ps1` runs the joint calculator, differential tests, CPython
scan and live compiler workbench. The joint calculator imports every cluster
instruction from Go, Python, R, Rust, Clang/C++, Kotlin, Java and C# (218
entries), reproduces the supplied node/gap products, aligns the 98 shared
feature names, normalizes each language's evidence vector and projects through
the 82-node/23-relation basis. Language-specific features retain separate
dialect-to-axis projections; missing universal mappings are explicitly exported
instead of being filled with zero.

The shared projection is from the Go handoff and its cross-language reuse is a
planning hypothesis. Imported scanner counts are demand evidence, not a fresh
Go/R scan or proof of support. Cluster records remain not_verified_complete.

The type-domain block now includes a derived equivalence matrix in HIR and native
analysis. Alias/reference normalization is followed by greatest-fixed-point
structural refinement. An unknown vector propagates by dependency-matrix
multiplication. Numeric widths, signedness, child roles and nominal identities
remain distinct; display names and source provenance do not define structural
equality. Unknown kinds, ambiguous nominal views and unresolved alias cycles do
not establish equality. JSON import recomputes and checks the new plane while
allowing legacy documents that omit it.

This proves bounded type-domain equivalence, not general assignability, numeric
coercion, inheritance, ABI compatibility or full language type checking. No
backend capability is enabled by this projection. Executable declaration/module
semantics and the remaining cluster acceptance criteria are still open.

## Eight-language handoff, exact signatures and call resolution

The local calculation now includes Go, Python, R, Rust, Clang/C++, Kotlin, Java
and C#. It aligns eight normalized 98-feature vectors with the supplied
82-node/23-relation projection. Language-specific features remain in separate
axis projections. All 218 imported cluster records remain
`not_verified_complete`. Ten handoff archives contribute 180 selected files to
`matrices/handoffs/`; the generated
manifest records archive and file hashes.

Semantic functions may opt into `binding=exact_v1`, explicit parameter modes and
`default_evaluation=definition|call`. The direct runtime uses the parameter x
argument matrix and its count/default vectors. It preserves callee and argument
evaluation order, binds positional-only, positional-or-keyword, keyword-only and
both variadic modes, and rejects conflicts. Definition defaults are evaluated
once at closure creation; call defaults use the callee environment and parameter
order.

This contract is represented, roundtripped and directly executable. Existing
frontends retain legacy signatures until they emit it explicitly. Target source
generators and R text serialization reject the contract because parity is not
implemented. This advances shared Python declaration, C++ overload/calls and
Kotlin call-resolution work without claiming implicit receivers, templates or
backend support.

Calls may now carry `call.resolution.exact.v1`: a candidate x obligation required
plane, a matching satisfied plane, a candidate x argument conversion-cost
matrix and a priority vector. The validator computes missing obligations and
conversion totals by matrix multiplication, requires one lexicographic minimum
and verifies the serialized selected index. Candidate result/signature types are
included in the derived type table. The direct runtime invokes the selected
declaration. Ambiguous minima, forged selections and malformed planes fail
closed; target generators reject the contract until they implement equivalent
dispatch.

The calculated feature basis is embedded directly in SemanticProgram. It holds
the complete supplied 8 x 98 generic language matrix, the 8 x 434 namespaced
dialect-feature matrix, the 98 x 82 feature-to-node matrix and the 98 x 23
feature-to-relation matrix. A one-hot language vector produces the program's
generic and dialect demand; subsequent products produce node and relation
vectors. JSON import verifies the embedded basis, profile selection and every
derived product. This is a complete representation of the supplied semantic
feature space, while the 218 executable implementation clusters retain their
separate verification status.

The Universal AST Schema Matrix v1 handoff is copied under
`matrices/handoffs/uast_schema/` and recalculated by
`tools/matrix-audit/build_uast_schema.py` without a ranking operator. The
embedded sparse basis retains 553 source features, 334 exact quotient facets,
109 structural kinds, 44 semantic axes, 23 relation axes, 55 concrete
relations, 57 fields and 17 layers. SemanticProgram now transports sparse
facet vectors, derived field masks, typed field JSON, source spans and concrete
relation instances. Import validation rejects altered masks, unknown or
inapplicable relations, broken references, invalid coverage intervals and a
modified basis hash. Represented UAST nodes are not silently executed by a
backend without a registered lowering.
