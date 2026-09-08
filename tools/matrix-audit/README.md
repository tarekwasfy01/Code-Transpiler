# Reproducible architecture audit

The sections below record successive audit versions. Historical descriptions
of missing runtime packages and rejected function features describe those
versions, not the current implementation. For the latest run use V16 below.

These tools measure the existing transpiler. They do not change its parser,
generators, runtimes, application, or existing release executable.

Run in the repository root with PowerShell:

```powershell
$env:GOCACHE = Join-Path (Get-Location) '.audit-cache\go-build'
go build -o .audit-cache\matrix-audit.exe ./tools/matrix-audit
$env:AUDIT_PYTHON = '<absolute path to Python>'
$env:AUDIT_JAVA_HOME = '<optional JDK directory containing bin/javac.exe and bin/java.exe>'
node tools/matrix-audit/audit.mjs
```

The adapter invokes `manytomany.Transpile`, the same function used by the GUI
and CLI. It can be built independently of the GUI. Go, Rust, C, C++, Python and optionally
Java are used for native execution. Other target emitters are still exercised.
All child processes are hidden. Generated programs and build caches stay in
`.audit-cache`. The audit writes evidence to
`outputs/transpiler-audit-2026-08-30`.

The 208 source fixtures cover 16 categories for each of 13 languages. Available
native source executions must compile, run and match their contract before
target failures are counted. Remaining sources are marked as having only a
specified fixture oracle. This run validated 96 native source fixtures. An
unavailable toolchain is UNKNOWN, never PASS or FAIL. A timeout is UNKNOWN.

The output comparison is scalar, not byte-exact: whitespace at the edges, R's
`[1]` display prefix and simple string quotation are normalized. Boolean
representations true/TRUE/True/1 are accepted; numeric tolerance is 1e-10.
Debug renderings such as `Num(14.0)` are not treated as normal scalar output.
Therefore output mismatches include display incompatibilities, not only wrong
numeric values. Side effects, memory behavior and full language semantics are
outside these first cases.

`workbook.mjs` creates the XLSX and numeric exports from measurements.json.
It requires the bundled `@oai/artifact-tool` and the bundled Node runtime.
Use Codex's workspace dependency loader for their paths. The working
`node_modules` junction is a local convenience and is not part of the deliverable.
It produces formula-based masks and summary sheets and renders verification
previews. Data processing uses only Node built-ins; no workbook fallback
library is used.

The numeric export has:

- `F`: 156 × 32, values 0/1/null.
- `B`: 156 × 32, unknowns represented by zero for multiplication.
- `K`: 156 × 32, observation mask; 1 means known.
- `stages`: B/K matrices for each of emit, compile, run and output.
- Explicit ordered lists of language pairs and feature IDs.

Never use B without K. An unweighted failure rate is
`(B @ ones) / (K @ ones)` elementwise, leaving zero denominators undefined.
This is a fixture failure rate, not a probability of translation correctness.
No priority weighting is included.

## Semantic matrix sequence

The matrix sequence is cumulative and reproducible:

- V2 projects the old CIR graph into 48 semantic dimensions and five relation matrices.
- V3 adds a quote-aware lexical graph for all 208 fixtures.
- V4 adds the lexical rule matrix and 30 context axes.
- V5 projects the 32-dimensional behavior contract into the semantic space and builds a 2,496 by 227 design matrix.
- V6 adds row-wise Kronecker products for source-language by lexical-axis and target-language by semantic-axis interactions. Its design matrix is 2,496 by 1,241.
- V7 subtracts the exact feature mean vector and adds 390 source-language residual dimensions. The resulting virtual design is 2,496 by 1,631.

Run the stages in order after `audit.mjs`:

```powershell
node tools/matrix-audit/semantic-matrix-v5.mjs
node tools/matrix-audit/semantic-matrix-v6.mjs
node tools/matrix-audit/semantic-matrix-v7.mjs
```

V6 and V7 store the exact Gram matrix in blocks instead of repeating the full
dense square. The blocks reconstruct `X transpose X` without approximation.
The calculations use no route weights and no manually assigned priorities.

## V11: expanded, masked evidence

```powershell
node tools/matrix-audit/audit.mjs --extended
node tools/matrix-audit/evidence-matrix.mjs outputs/transpiler-audit-v11 outputs/transpiler-audit-v11-before
```

This adds nine independent case classes to the sixteen original fixtures:
literal preservation, multiple and computed indices, zero returns, nested
calls, symbolic bounds, same-line statement sequences and unused effectful
arguments. There are 325 source fixtures and 3,900 directed route fixtures.
Sixteen further semantic categories remain explicitly UNKNOWN. Six native
toolchains validate 150 source fixtures before translation is tested.

The evidence tensor has 156 pairs × 41 classes × 4 stages = 25,584 cells.
The real sparse design X is 6,396 × 1,133, with exactly 31,980 nonzero entries.
Its features are source, target, case and source/case plus target/case
interactions. Outcomes are never included as design columns. The artifact
stores X, observation/failure/unknown masks, XᵀX, XᵀE and XᵀK; every column
and row has an explicit identifier. All weights equal one.

The historical V8–V10 projections contain manually specified capability goals
and partially virtual dimensions. They must not be used as measured semantic
coverage or as a release gate. V11 does not claim that more matrix dimensions
imply a more capable translator. Matrix projections describe observed failure
classes; syntax and semantic rules still require implementation and validation.

The current frontend uses token-class projections and delimiter incidence for
statement segmentation and expression protection. It still lowers to the old
R-shaped expression parser and retains restricted header recognizers. The
call matrix prevents unsafe substitution of effectful used arguments and
supports straight-line bodies with a final return. General recursion, closures,
defaults and named arguments are not thereby implemented.

Range lowering preserves symbolic endpoints. A 3 × 3 affine matrix adjusts
exclusive endpoints, which are snapshotted once; an explicit guard preserves
empty ascending ranges. Counting loops retain condition reevaluation and
their increment on continue. Nonunit range steps are rejected instead of
silently changed. Focused tests additionally cover negative starts, changing
bounds, empty ranges and continue behavior. The obsolete physical-line splitter,
number-extraction range fallback and single-index rewriter have been removed.

`pipeline-check.mjs` is a quick Python-target diagnostic over the stored fixtures.
Its output is not an additional 325 independent cross-language compatibility
proofs: it includes Python identity routes, and shares the full audit's corpus.
Only the full audit contributes to the published evidence matrix.

The audit records a sorted path/hash manifest for internal and command sources,
the audit harness and Go module files, plus the exact adapter binary hash.
It rejects a run if those files change during execution. Source changes in
matrixir were absent from the historical six-file hash; do not compare that old
hash as a complete code identity. UTF-8 output is set explicitly for Python and
Java, and stream decoding retains partial multibyte characters.

PASS means only that the recorded fixture completed all four stages against its
specified oracle. See source_validated for native source evidence. Unsupported
toolchains, untested semantics and timeouts remain UNKNOWN. FAIL → UNKNOWN is
never counted as a repair. Do not build a release from a partially green matrix.

## V12 and the requested experimental EXE

The user subsequently requested a new EXE after the next combined matrix pass.
This authorizes an explicitly experimental build with documented unknown cells;
it does not convert UNKNOWN into supported language semantics.

```powershell
node tools/matrix-audit/audit.mjs --v12
node tools/matrix-audit/evidence-matrix.mjs outputs/transpiler-audit-v12 outputs/transpiler-audit-v12-before
./build-onefile.ps1
node tools/matrix-audit/verify-release.mjs dist/CodeTranspiler.exe
```

V12 has 429 source fixtures, 5,148 route fixtures and 49 case/category columns:
33 executable case classes and 16 categories still reserved. The full stage
tensor has 30,576 cells. X is 7,644 × 1,349 with 38,220 nonzero values.
New cases combine unary operators, logical conditions, short-circuit effects,
used/repeated effectful arguments and local function variables.

The argument/parameter mapping is a partial permutation matrix B. D × B maps
parameter occurrences to actual arguments, and D × B × e identifies effectful
uses. Actual values are bound once in their original evaluation context.
Standard C uses static helper functions with explicit value captures; it does
not require GNU statement expressions. C vectors copy their storage instead of
returning pointers to temporary compound-literal argument arrays. Complete
ownership/garbage-collection semantics remain outside this fixture coverage.

The operator matrix selects numeric sign, complement and short-circuit gate
polarity. Logical right-hand operands remain conditional. Local scalar
bindings, simple defaults and exact named-argument mappings are implemented;
recursion, general closures, conditional R promises and other unmodeled language
features remain limited or explicitly rejected. Unit tests for default/named
binding are separate from the 33-class native corpus.

Runtime source is compiled into the application, then exposed by runtimeassets.
External target compilers are not bundled. Python and Java scalar output now
print small finite integral numbers without an artificial decimal suffix;
the output oracle has not been relaxed. Zig iterable emission/runtime support
was extended but is not natively certified by this six-compiler run.

## V13: function control-flow matrices

```powershell
node tools/matrix-audit/audit.mjs --v13
node tools/matrix-audit/evidence-matrix.mjs outputs/transpiler-audit-v13 outputs/transpiler-audit-v13-before
node tools/matrix-audit/verify-matrices.mjs outputs/transpiler-audit-v13
./build-onefile.ps1
node tools/matrix-audit/verify-release.mjs dist/CodeTranspiler.exe outputs/transpiler-audit-v13
```

Set `AUDIT_OUTPUT` to an alternative evidence directory when capturing a
baseline. Do not overwrite a historical measurement or change sources during
an audit. The baseline V13 run contains exactly the same fixture IDs and
contracts as the final V13 run, so their transitions are directly comparable.

V13 adds eight classes: positive/negative conditional returns, early returns
through both paths, branch-local bindings, conditional effects, local-value
joins and nested returns. There are 533 source fixtures, 6,396 route fixtures,
57 feature/category columns (41 executable, 16 reserved) and 35,568 stage
cells. The evidence design X is 8,892 × 1,565 with 44,460 nonzero entries.

`internal/backend/flow_matrix.go` builds A, T and F transition matrices from
function AST nodes. A holds unconditional edges; T and F hold the true/false
edges. Return rows are terminal. Boolean closure and the entry basis vector
establish reachable nodes and reject reachable fallthrough or cycles. The
generator selects successors through basis-vector matrix products and carries
local bindings separately along each path. Native conditional expressions
execute only the selected branch. They do not eagerly compute both branches
and multiply values by a zero/one selector, which would break side effects.

`function_flow_matrices.json` contains these actual generator matrices and
their entry/reachability vectors for every function in the fixture corpus.
The independent verifier traverses each graph to check the matrix closure,
validates row degrees, and separately recomputes every entry of XᵀX, XᵀE and
XᵀK. These are descriptive and structural calculations, not learned language
semantics or evidence that arbitrary languages can be translated linearly.

General loops inside this function representation, closures, recursion and
effectful conditional R promises remain unsupported. The graph is bounded to
255 statements and generated path expansion to 4,096 nodes; larger functions
need a different lowering strategy. The existing R expression parser remains
in use. Function-local values use distinct immutable generated names across
branches; this is not a complete model of every source language's scoping.

The common indentation frontend now closes deeper frames before matching an
outer else. Its regression test covers both Python and Nim profiles. The
Python-only pipeline diagnostic is useful before native testing but is not
added to the cross-language evidence counts.

The pinned, noninteractive builder runs all package tests, embeds the icon,
builds a candidate x64 GUI PE, preserves the previous EXE and replaces the main
artifact only after structural checks. `verify-release.mjs` invokes the actual
EXE, checks version/runtime listing/embedded execution/error handling, and
compares every emitted route byte for byte with the native audit's source code.
GUI startup and PE imports are recorded separately. None of these checks is a
claim of complete GUI workflow or universal language compatibility.

## V14: cyclic flow and mutable state vectors

```powershell
node tools/matrix-audit/audit.mjs --v14
node tools/matrix-audit/evidence-matrix.mjs outputs/transpiler-audit-v14 outputs/transpiler-audit-v14-before
node tools/matrix-audit/verify-matrices.mjs outputs/transpiler-audit-v14
./build-onefile.ps1
node tools/matrix-audit/verify-release.mjs dist/CodeTranspiler.exe outputs/transpiler-audit-v14
```

V14 has 637 source fixtures, 7,644 route fixtures and 65 feature/category
columns: 49 executable classes and 16 reserved. X is 10,140 × 1,781 with
50,700 nonzero entries; the four-stage tensor has 40,560 cells.

Eight new classes cover while loops in functions, zero iterations, break,
continue, early return, nested loops, effects per iteration and conditional
updates. Native source contracts are validated before target outcomes.

The same A/T/F control matrices now contain loop backedges. While has true
and false edges; break targets the innermost loop exit and continue its
condition. Return remains terminal. The closure diagonal records cycles;
reachability is not a proof that a loop terminates.

`state_matrix.go` adds node-by-slot read and write matrices and a definite
assignment fixed point over predecessor intersections. Parameters form the
initialization vector. Reachable reads must be included in the definite
assignment mask. The verifier independently propagates sets along graph paths
to check this mask and computes cycle membership by graph traversal.

Functions containing while loops lower to a native state machine with a
program counter and a mutable value vector. Matrix successor projections
determine state transitions; source expressions update vector slots at runtime.
There is no fixed iteration count or recursive unrolling. Arbitrary source
expressions are not thereby linear functions. The 255-statement graph bound
remains; the previous 4,096-node path-expansion bound applies only to acyclic
expression lowering, not to loop iterations.

Python and standard C use lifted statement helpers; other supported targets
use native expression closures. C releases its private slot array when the
function returns, after copying the return value. This is not complete garbage
collection for user vectors. Missing typed state-vector support in Nim returns
an explicit translation error rather than emitting a fabricated runtime type.

The R expression parser remains. Native iterable for loops inside this function
graph, closures, recursion, full R promises and full runtime/type semantics
remain outside this block. Existing frontend counting-loop normalization can
produce while nodes, but this audit does not certify every for-loop form.

## V15: native Nim evidence and a typed value runtime

Set `AUDIT_NIM` to the local compiler's absolute path in addition to
`AUDIT_PYTHON` and `AUDIT_JAVA_HOME`. V15 uses the pinned Nim 2.2.10 Windows x64
distribution in `.audit-cache/toolchains`, with the existing GCC toolchain.
The ZIP digest is checked against the official GitHub release asset metadata;
the compiler and its MIT notice remain separate from the delivered EXE.

```powershell
node tools/matrix-audit/audit.mjs --v15
node tools/matrix-audit/evidence-matrix.mjs outputs/transpiler-audit-v15 outputs/transpiler-audit-v15-before
node tools/matrix-audit/verify-matrices.mjs outputs/transpiler-audit-v15
./build-onefile.ps1
node tools/matrix-audit/verify-release.mjs dist/CodeTranspiler.exe outputs/transpiler-audit-v15
```

Four additional comparison classes cover equality, inequality, less-or-equal
and greater-or-equal. There are 689 fixtures, 8,268 directed routes, 69
feature/category columns (53 executable and 16 reserved), and 43,056 stage
cells. X is 10,764 × 1,889 with 53,820 nonzero entries. Seven native source
toolchains validate 371 source examples. Missing toolchains remain UNKNOWN.

`nim_runtime.go` replaces the placeholder runtime with a tagged RValue shared
by nulls, numbers, booleans, strings and vectors. A 5 × 3 capability matrix
controls numeric, scalar truth and iteration projections. Arithmetic,
comparison, print, vectors, positive scalar indexing and length operate on
this representation. Unsupported operations raise explicit errors; primitive
routing coverage still does not imply full semantic implementation.

Nim uses the same A/T/F flow matrices and read/write/initialization masks as
the other backends. Its state vector is carried in the same RValue type and
its state-machine helpers have typed value parameters. Nested procedure
expressions bind actual values once, in the selected evaluation context.

Source identifiers are encoded injectively into Nim-safe identifiers to avoid
keyword, underscore and style-insensitive name collisions. Generated temporary
names use a disjoint prefix. Strings and primitive names are not rewritten.
These changes apply to all generated source bindings, not a fixture-name list.

V15's release verifier now records separate route, emitted-code and expected-
error counts. Historical V14 files used the translation-match field for the
route total; their separate scope report explains that historical counter.

This block does not replace the R expression parser or implement arbitrary R
coercion, vector recycling, promises, closures, recursion or every Nim feature.
The native corpus and separate additional probes define the verified scope.

## V16: private iteration vectors in function flow

Use the same seven native toolchains and environment variables as V15:

```powershell
node tools/matrix-audit/audit.mjs --v16
node tools/matrix-audit/evidence-matrix.mjs outputs/transpiler-audit-v16 outputs/transpiler-audit-v16-before
node tools/matrix-audit/verify-matrices.mjs outputs/transpiler-audit-v16
./build-onefile.ps1
node tools/matrix-audit/verify-release.mjs dist/CodeTranspiler.exe outputs/transpiler-audit-v16
node outputs/transpiler-audit-v16/verify-iteration-exe.mjs
```

Six new classes exercise `for` inside functions: sum, break, continue, return,
nested loops and effects. The corpus contains 767 source fixtures, 9,204
directed routes, 75 feature/category columns (59 executable, 16 reserved),
and 46,800 stage cells. X has shape 11,700 × 2,051 with 58,500 nonzeros.

The common function graph lowers iterable loops to private sequence, length
and position slots, followed by A/T/F transitions. The iterable expression is
evaluated once. Its outer vector is copied, preserving typed elements and
nested vectors: null becomes empty, a scalar becomes a singleton, and a vector
keeps its elements. The source iterator is assigned only when an element exists.
The cursor advances before the body, so continue reaches the next element;
break and return retain the original graph exit rules.

For state v = [position, length, 1], the cursor update is computed from
M = [[1,0,1],[0,1,0],[0,0,1]]. The exported matrix is independently checked on
integer states. Sequence gathering and conditional execution are not linear
operations; this is not a claim that arbitrary program semantics are linear.

Internal iteration slots have identities rejected by the source lexer and
resolve only to state-vector positions. User functions cannot override the
internal size operation. Parameter-demand matrices now include iterable and
body expressions. Generated binding birth positions prevent inner lambda/proc
parameters from being captured by an enclosing flow helper. Definite-assignment
failures return a typed safety error instead of falling back to an unchecked
closure emitter. Other unsupported graph shapes can still use older paths;
this does not certify every emitted function.

Separate exact-EXE probes cover null/empty/scalar/nested/mixed values, effects,
rebinding, nested transfers, private-name hygiene and explicit rejections.
They are not added to the main matrix totals. C zero-argument dispatch uses
NULL with zero count and is compiled under strict C11 in a regression test.

Native toolchains remain external. Full lexical environments, promises,
recursion, all source syntaxes, and parser replacement remain open. C snapshot
payloads follow the existing runtime allocation lifetime and can remain
allocated until process exit; full vector ownership is not implemented. The
255-statement graph bound includes synthesized iteration nodes.
