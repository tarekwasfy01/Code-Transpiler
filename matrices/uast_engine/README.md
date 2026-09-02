# UAST Matrix Engine canonical input contract

The engine reads only this folder. Files elsewhere in the project are evidence
or reports and cannot expand canonical dimensions. Proof-compression,
external-semantic, MLCPD, miner, corpus, and output files are deliberately
excluded from canonical discovery.

Input classes are explicit: the schema/status files define canonical
dimensions; language/feature/dependency/target files are canonical crosswalks.
Evidence classes may reference canonical IDs but never add new axes.

## Files
- language_features.csv: language_id, source_feature_id
- feature_capabilities.csv: source_feature_id, canonical_semantic_id
- capability_schema.csv: canonical_semantic_id, element_type, element_id
  - element_type: structure | relation | facet | field
- capability_dependencies.csv: canonical_semantic_id, depends_on_semantic_id
- capability_status.csv: canonical_semantic_id, canonical, conflict, already_uast
- current_uast_elements.csv: element_type, element_id, implemented
- target_preservation.csv: target_id, canonical_semantic_id, direct, rewrite, helper, emulate, runtime, tested

Boolean values accepted: 1/0, true/false, yes/no, y/n.

The engine never guesses missing semantics. Missing evidence remains blocked/unproven.
