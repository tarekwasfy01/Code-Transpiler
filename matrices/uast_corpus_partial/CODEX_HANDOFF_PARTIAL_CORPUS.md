# CODEX HANDOFF — PARTIAL ECOSYSTEM CORPUS EVIDENCE

## Purpose

Continue directly from the existing UAST semantic-expansion handoff in `base/handoff/` and incorporate the attached *partial empirical corpus evidence* as an audit/input layer.

This corpus run was intentionally aborted by the user. It is useful but NOT complete. Do not infer absence from anything not observed.

## Frozen architecture — DO NOT CHANGE

- `SemanticProgram = UniversalASTDocument = canonical semantic IR`.
- No second semantic IR.
- Frontend path remains `FrontendSemanticFacts -> Raw UAST -> Enrich -> AnalyzeUniversalEvidence -> Normalize`.
- Target path remains `UAST -> UniversalTargetEngine/Projector + TargetSpec + PreservationRegistry + RequirementRegistry -> Doc -> Formatter -> Source`.
- No new LowererIR/BackendIR/RuntimeIR/TypedOperation IR/HelperIR.
- Preservation order remains `DIRECT -> REWRITE -> HELPER -> EMULATE -> RUNTIME -> ERROR`.
- `not proven -> not emitted`.
- Infrastructure is frozen. This handoff is semantic evidence expansion, not another architecture migration.

## Existing canonical baseline

Use `base/handoff/` as the authoritative prior handoff.

Its previously calculated canonical block remains:

- 334 canonical capabilities total
- 320 new canonical-ready capabilities
- Schema delta: 86 Structures / 39 Relations / 320 Facets / 27 Fields
- Canonical UAST readiness is independent of target preservation.

Do NOT reintroduce the incorrect rule that missing target preservation blocks canonical UAST representation.

## Corpus run scope

Run state: `ABORTED_BY_USER_AFTER_PARTIAL_PROGRESS`.

Observed successful artifacts: 671

- R / CRAN: 648
- Julia / GitHub seeds: 6
- Nim / GitHub seeds: 6
- Swift / GitHub seeds: 6
- Zig / GitHub seeds: 5

Failed/skipped: 4.

The run had NOT yet reached PyPI, crates.io, or the Go registry. CRAN was only partially traversed. Therefore:

- No absence claim is allowed for Python, Rust, Go, or the unprocessed CRAN remainder.
- No completeness claim is allowed for any language.
- This is positive empirical evidence only.

See `corpus_derived/partial_corpus_summary.json` and the raw corpus outputs under `corpus_partial/`.

## Critical evidence distinction

Corpus observations are NOT compiler-level semantic proof.

Use these layers separately:

```text
COMPILER_PROVEN[l,c]       semantic authority from compiler/language matrices
CORPUS_OBSERVED[l,c]       empirical real-world source occurrence
BASELINE_PRESENT[l,c]      prior language/capability matrix
NEW_EMPIRICAL[l,c]         CORPUS_OBSERVED && !BASELINE_PRESENT
```

Never set `CANONICAL`, `DIRECT`, `TESTED`, or preservation truth solely because corpus evidence exists.

Corpus evidence may:

1. confirm an existing language->capability edge,
2. flag a likely missing frontend crosswalk,
3. identify source constructs that require compiler-source verification,
4. provide real-world examples for tests after semantic proof is established.

## New empirical language x semantic observations

`corpus_derived/new_empirical_language_semantic_evidence.csv` contains 56 positive observations not present in the prior baseline:

- Julia: 13
- Nim: 13
- Swift: 16
- Zig: 14

These are especially important because the prior canonical adapter lacked evidence rows for these four languages.

Treat every such cell as:

```text
EMPIRICAL_CANDIDATE[l,c] = 1
```

NOT as:

```text
COMPILER_PROVEN[l,c] = 1
```

For each of the 56 cells:

1. Find the corresponding evidence in the supplied Julia/Nim/Swift/Zig compiler-source/language matrices already available in the project/handoff context.
2. If exact semantic contract matches the existing UASF capability, add/fix the frontend language-feature crosswalk.
3. If only syntax similarity exists, leave it unproven.
4. If the construct expresses semantics not represented by any existing capability, add it to a semantic-gap report; do not invent a UASF mapping ad hoc.

## Baseline confirmations

`corpus_derived/baseline_confirmation_language_semantic_evidence.csv` contains 21 R language/capability cells observed in real CRAN packages that were already present in the baseline.

Use these as validation evidence only. They do not expand canonical semantics.

## Candidate signatures

Raw unknown normalized signatures: 35,614.

Do NOT process them manually one-by-one and do NOT rank them.

Use matrix/group operations:

- `corpus_derived/candidate_signatures_multi_package.csv`: 4,008 signatures observed in >=2 packages.
- `corpus_derived/candidate_signatures_3plus_packages.csv`: 1,833 signatures observed in >=3 packages.

Counts are evidence multiplicity, not semantic importance.

Recommended audit transform:

```text
SIG[l,s] = observed normalized signature
PKG[l,s] = package support set
KNOWN[s,c] = exact existing extractor/crosswalk match only
UNMAPPED[l,s] = SIG[l,s] && !OR_c KNOWN[s,c]
```

Then cluster only by exact normalized structural contract + language context. Do not use fuzzy/nearest-semantic matching to create UASF truth.

For a candidate signature to become a semantic-program mapping, require an exact compiler/source-language semantic proof.

## Files to use

### Prior authoritative handoff
`base/handoff/`

### Raw partial corpus results
`corpus_partial/`

Important raw matrices:

- `language_feature_matrix.csv`
- `language_semantic_matrix.csv`
- `language_uast_matrix.csv`
- `package_feature_matrix.csv`
- `package_semantic_matrix.csv`
- `semantic_uast_matrix.csv`
- `semantic_observation_delta.csv`
- `semantic_evidence_provenance.csv`
- `candidate_signature_matrix.csv`
- `feature_cooccurrence_matrix.csv`
- `semantic_cooccurrence_matrix.csv`
- `artifact_summary.csv`
- `artifact_records.jsonl`

### Derived audit files
`corpus_derived/`

- `partial_corpus_summary.json`
- `new_empirical_language_semantic_evidence.csv`
- `baseline_confirmation_language_semantic_evidence.csv`
- `candidate_signatures_multi_package.csv`
- `candidate_signatures_3plus_packages.csv`

## Work to perform now

### A. Integrate the four missing-language evidence rows

Use the 56 `NEW_EMPIRICAL` cells as an audit list against the already gathered compiler/language matrices for Julia, Nim, Swift and Zig.

For every exact compiler-proven match:

- update the canonical `language_features.csv` / relevant adapter source,
- retain exact provenance,
- regenerate language->capability matrices,
- rerun the local matrix engine/handoff analyzer,
- report the change in target native/direct candidates.

Do this matrix-wise, not feature-by-feature manually.

### B. Validate R baseline with CRAN corpus

Use the 21 baseline-confirmation cells and provenance to add regression/test corpus references where practical. Do not alter semantics merely because occurrence counts are high.

### C. Audit unknown signatures

Process the >=3-package matrix first as a *set operation*, then >=2-package residuals. This is not priority ranking; it reduces redundant signatures deterministically.

Classify each equivalence class into exactly one of:

```text
EXACT_EXISTING_SEMANTIC
MISSING_FRONTEND_CROSSWALK
COMPILER_VERIFICATION_REQUIRED
NEW_SEMANTIC_GAP
SYNTAX_ONLY_OR_NOISE
```

Never silently map `COMPILER_VERIFICATION_REQUIRED` or `NEW_SEMANTIC_GAP` into an existing capability.

### D. Keep canonical and target readiness separate

For any newly compiler-proven language/capability cell:

```text
CANONICAL_READY[c]
= EVIDENCE[c] && CANONICAL[c] && !CONFLICT[c] && DEPENDENCIES_READY[c]
```

Target support remains separate:

```text
E2E_READY[t,c]
= PRODUCED[c] && PRESERVABLE[t,c] && TESTED[t,c]
```

Same-language corpus evidence may create a `DIRECT_CANDIDATE`, but it is not `DIRECT_PROVEN` until codegen preservation tests pass.

## Do NOT do

- Do not redesign the architecture.
- Do not add a second semantic IR.
- Do not block canonical UAST representation because target preservation is absent.
- Do not automatically canonicalize unknown corpus signatures.
- Do not infer language absence from this aborted corpus run.
- Do not use package counts as scores/priorities.
- Do not rewrite working target renderers.
- Do not execute downloaded third-party code.

## Testing

Use targeted tests while integrating exact matrix-derived changes. At the end run the existing synchronous backend suite:

```powershell
$env:GOCACHE=(Resolve-Path '.gocache').Path
go test ./internal/backend -count=1
```

## Required final report

Report:

1. exact number of the 56 empirical cells confirmed by compiler-source evidence,
2. exact number rejected/unproven,
3. updated per-language semantic evidence counts for Julia/Nim/Swift/Zig,
4. updated native/direct candidate counts across all 13 targets,
5. candidate-signature classification counts,
6. any genuinely new semantic gaps,
7. schema delta only if compiler-proven gaps require it,
8. backend test result,
9. `SEMANTIC EXPANSION COMPLETE: YES/NO`,
10. blockers, if any.

A later, larger ecosystem corpus run may be supplied. Design this integration so new corpus matrices can be unioned monotonically without changing architecture.
