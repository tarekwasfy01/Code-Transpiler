# Universal AST direct execution status

Snapshot: 2026-09-01

## Canonical invariant

`SemanticProgram = UniversalASTDocument` is enforced at the program boundary.
A frontend may still construct the historical executable tree during analysis,
but the first document or backend operation projects it once. From that point
on the UAST is the only semantic truth. `Body`, `SemanticDocument.Root` and
historical statement/expression objects are derived compatibility views and
cannot write semantics back into the UAST.

Schema-valid UAST semantics that an executor cannot preserve are rejected
explicitly. No backend silently drops nodes, fields, facets or relations.

## Direct execution paths

The following paths now read the canonical UAST directly:

- graph and losslessness validation (`newUASTExecutionGraph`);
- embedded semantic runtime (`runState.uastBlock`);
- R output (`universalRSource`);
- signature validation (`validateDirectSignatureContracts`);
- call resolution (`validateDirectCallResolutions`);
- typed-operation requirements (`directTypedRequirements`).

Generic target generation and function-flow/inline-function processing now
emit and analyze directly from UAST nodes, IDs, fields and relations. No
legacy function or call tree is reconstructed. After direct runtime execution,
the public legacy `Body` view is refreshed once so external API mutations
cannot appear to change the canonical program.

## Measured coverage

All figures below are generated from schema, code and sparse matrices. The
machine-readable sources are `outputs/uast-direct-execution/coverage.json`,
`direct-execution-matrix.{json,csv}` and `outputs/uast-coverage/coverage.json`.

- Structures: 109 total; 17 compatibility-projected; 16 directly executable;
  0 currently classified lowerable; 93 representable but not directly
  executable. The projected-only `AggregateExpr` is a fallback storage class
  and is rejected without a proved executable semantic kind.
- Direct structures: `AssignStmt`, `BreakStmt`, `CallExpr`, `ClosureExpr`,
  `ContinueStmt`, `ForEachStmt`, `IfStmt`, `IndexExpr`, `LiteralExpr`,
  `LoopStmt`, `NilLiteral`, `OperationExpr`, `ParameterDecl`, `ReturnStmt`,
  `Scope`, `SymbolRef`.
- Facets: 334 total; 14 projected and directly interpreted by the facet-vector
  capability gate; 0 lowerable; 320 not executable. Unsupported and unknown
  counts are target-dependent and recorded exactly for all 13 targets in the
  capability matrix.
- Relations: 55 total; 16 projected; 5 directly consumed; 11 retained and
  validated as stored/lowerable evidence; 39 not projected.
- Direct relations: `syntax.child`, `control.true`, `control.false`,
  `call.calls`, `data.operand`.
- Stored relations: `binding.declares`, `binding.refers`, `control.next`,
  `data.def_use`, `effect.has`, `evaluation.before`, `name.resolves`,
  `operation.kind`, `scope.parent`, `type.has`, `type.origin`.
- Fields: 57 total; 28 direct UAST channels; 29 not directly connected.
- Direct fields/records: `id`, `kind`, `scope_id`, `type_ref`, `type_origin`,
  `operation`, `effects`, `binding_refs`, `name`, `attributes`, `extensions`,
  `operands`, `condition`, `branches`, `body`, `members`, `arguments`,
  `parameters`, `value`, `callee`, `ownership`, `lifetime`,
  `evaluation_order`, `dispatch`, `exception_model`, `candidates`,
  `source_span`, `semantic_facets`.
- Crosswalk: 57 rows, 30 unique target fields, 0 invalid mappings.

The target matrix covers 109 structures, 334 facets and 55 relations across 13
registered backends. Every cell is exactly one of `direct`, `lowerable`,
`runtime-required`, `unsupported` or `unknown`; one-hot validation is tested.
Emission multiplies the demanded structure, facet and relation vectors by
these status planes and rejects unsupported or unknown demand.

## Remaining adapters

The generated scanner currently finds 9 conversion boundaries and 36 total
compatibility-call observations. The nine boundaries are:

1. `LowerNativeGo -> documentStatementAST`
2. `Document -> installLegacyProgramView`
3. `documentFromCanonicalUniversalAST -> SemanticDocumentFromUniversalAST`
4. `documentFromCanonicalUniversalAST -> installLegacyProgramView`
5. `ParseUniversalASTJSON -> installLegacyProgramView`
6. `ParseSemanticDocument -> documentStatementAST`
7. `validateUniversalASTCompatibility -> SemanticDocumentFromUniversalAST`
8. `validateExecutableUniversalProjection -> SemanticDocumentFromUniversalAST`
9. `refreshLegacyExecutableBodyView -> legacyExecutableBodyFromUniversal`

Exact files, lines, callers and classifications are in
`outputs/uast-direct-execution/legacy-adapters.{json,csv}`. Generic target
generation and function flow are now both marked as `direct-uast` paths.

## Remaining non-executable semantics

The 93 structure kinds and 320 facets outside the direct executor remain valid
UAST data but cannot yet be executed. They cover classes/interfaces,
generics/templates, exceptions/unwinding, concurrency/coroutines, FFI/ABI,
ownership/borrowing/lifetimes, layout/linkage, compile-time semantics,
module/import execution, advanced type structures and language dialect
semantics. The direct-execution matrix enumerates every exact item and every
target state, so this list is not inferred from a priority ranking.

## Verification

- Go suite: 425 tests/subtests passed, 0 failed, 1 skipped.
- Packages: 7 passed, 0 failed; 8 contained no tests.
- Handoff matrix suite: 7 passed, including 640 signature differential cases.
- Native matrix probes: 3 passed, 0 failed.
- Schema algebra: 109 structures, 334 facets, 55 relations, 57 fields.

Detailed Go events and the compact result are stored in
`outputs/uast-direct-execution/go-test-events.jsonl` and
`go-test-summary.json`.
