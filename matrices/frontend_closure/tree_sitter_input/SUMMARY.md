# Tree-sitter Frontend Matrix

Languages: 13

This package is designed as an evidence matrix for:
Source syntax -> Semantic / UniversalASTDocument.

## Lossless evidence
- 01_grammar_flat.csv: every scalar leaf from every grammar.json
- 07_node_types_flat.csv: every scalar leaf from every node-types.json
- raw/: original grammar/query/scanner/license files (unless --no-raw)

## Normalized semantic/parser tables
- 02_grammar_atoms.csv
- 03_rules.csv
- 04_rule_symbol_edges.csv
- 05_fields.csv
- 06_specials.csv
- 08_node_types.csv
- 09_node_fields.csv
- 10_node_children.csv
- 11_subtypes.csv
- 12_queries.csv
- 13_query_captures.csv
- 14_external_scanners.csv
- 15_corpus_cases.csv

## Algebra-ready Boolean matrices
- 16_language_operator_matrix.csv
- 17_language_rule_matrix.csv
- 18_language_field_matrix.csv
- 19_language_node_type_matrix.csv
- 20_rule_operator_matrix.csv

## UAST bridge
- 21_uast_join_template.csv

The flattened tables retain the original JSON information, so the normalized
matrices can be regenerated without relying on hand-written classifications.
