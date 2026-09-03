# Offline Grammar Tables

These tables are generated from the checked-in Tree-sitter grammar exports and
are consumed as neutral frontend data. They are not a Tree-sitter runtime and
must not be imported by production code as a parser library.

* `grammar_production_specs.csv` – flattened grammar operations
* `parse_states.csv` – deterministic rule/operator state index
* `parse_transitions.csv` – rule-to-symbol transitions
* `grammar_symbol_inventory.csv` – terminal/nonterminal inventory
* `rule_dependencies.csv` – rule dependency edges

Regenerate with:

```text
go run ./cmd/tree-sitter-frontend-closure -in matrices/tree_sitter_full -out matrices/frontend_closure -upi .cache/handoff-upi-complete
```

The tables are evidence and parse-structure inputs. Semantic support still
requires a registered producer and a valid `FrontendSemanticFacts` projection.
