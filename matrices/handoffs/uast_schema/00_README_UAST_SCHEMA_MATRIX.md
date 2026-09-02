# Universal AST Schema Matrix v1 — algebraic construction

This package is derived from the 8-language Universal Semantic Matrix (Python, R, Rust, C/C++, Kotlin, Java, C#, Go).
It contains no priority/ranking calculation.

## Exact construction

Let:
- `P` = Language × Feature presence, shape `8 × 553`
- `S` = Feature × SemanticAxis, shape `553 × 44`
- `R` = Feature × RelationAxis, shape `553 × 23`
- `U` = seeded Feature × StructuralNode projection, shape `553 × 109`

Define the schema signature matrix:

`Z = [S | R]`

Features are quotient-equivalent iff their rows of `Z` are exactly equal. The quotient projection `Q` is Feature × SemanticFacet.
This produces exactly **334 semantic facet classes** (`UASF_0001...`) from 553 features.

Then:
- `FacetAxis = bool(Q.T @ S)`
- `FacetRelationAxis = bool(Q.T @ R)`
- `LanguageFacet = bool(P @ Q)`
- `StructuralNodeFacetSeed = U.T @ Q`
- `FacetLayer = bool(FacetAxis @ AxisLayer)`
- `FacetConcreteRelation = bool(FacetRelationAxis @ RelationAxisConcreteRelation + FacetAxis @ SemanticAxisConcreteRelation)`
- `FacetField = bool(FacetAxis @ SemanticAxisField + FacetConcreteRelation @ ConcreteRelationField)`

No nearest-neighbor assignment is used. No language-specific feature is guessed onto an unrelated structural node.

## Concrete schema model

The universal AST is hybrid:
1. **Structural kind space:** 109 existing structural node kinds.
2. **Semantic facet space:** 334 exact quotient classes derived from language semantics.
3. **Semantic axis space:** 44 axes.
4. **Concrete relation space:** 55 relation kinds.
5. **Field space:** 57 schema fields.
6. **Layer space:** 17 orthogonal schema layers.

A node instance therefore has one structural kind and a sparse semantic-facet vector instead of requiring every source-language semantic distinction to become a different syntax-node struct.

## Why this matters

Only 119 of 553 features had direct structural-node seed mappings. The quotient/facet construction keeps all 553 features without inventing mappings for the remaining 434.

## Codex rule

Do not iterate feature rows as a TODO list. Implement the matrix schema and transformations. SemanticProgram remains the executable semantic representation; this package specifies how to generalize it into a universal structural-node + semantic-facet + relation model.
