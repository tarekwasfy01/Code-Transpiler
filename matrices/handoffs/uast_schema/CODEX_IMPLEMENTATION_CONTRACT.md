# Codex implementation contract — Universal AST Schema Matrix v1

Implement the schema algebra, not a priority list.

## Required representation

Preserve these independent dimensions in code/data:
- StructuralNodeKind: 109 labels from `36_structural_node_kind_catalog.csv`.
- SemanticFacet: 334 quotient classes from `01_semantic_facet_catalog.csv`.
- SemanticAxis: 44 axes.
- ConcreteRelationKind: 55 relations.
- UniversalField: 57 fields.
- SchemaLayer: 17 layers.

Do **not** create one Go struct per language feature. A node should carry a structural kind plus sparse facets/relations/evidence.

## Matrix invariants

1. `Q.sum(axis=1) == 1` for Feature→SemanticFacet.
2. A semantic facet is exactly one equivalence class of identical `[SemanticAxis | RelationAxis]` rows.
3. Existing structural node mappings are seeds only (`11`/`12` matrices); do not infer missing source-language nodes by nearest-neighbor guesses.
4. Field applicability comes from `25_semantic_facet_field_matrix.csv` and `26_structural_node_field_matrix.csv`.
5. Relation applicability comes from `20_semantic_facet_concrete_relation_matrix.csv` and `21_structural_node_concrete_relation_matrix.csv`.
6. Preserve SemanticProgram coverage as lower/upper interval matrices; do not replace unknown/partial with a fabricated scalar.
7. Do not use priorities, weights, rankings, or sorted implementation order.

## Suggested repository adaptation

Generalize current SemanticDocument/SemanticEvidence so the serialized program can carry:
- structural kind id
- sparse semantic facet ids
- sparse relation instances
- field mask / typed properties
- source spans
- existing type/effect/control/data/binding/evaluation evidence
- dialect/extensions without silently dropping unsupported semantics

`33_current_semanticprogram_to_uast_field_crosswalk.csv` gives the compatibility bridge from current SemanticProgram fields.

Acceptance is algebraic: serialization round-trip must preserve structural kind, semantic-facet vector, concrete relations, and applicable field data exactly.
