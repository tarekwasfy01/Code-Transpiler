# Execution-ready Tree-sitter matrix

This package normalizes the uploaded REAL_TS_MATRIX into directly executable neutral tables.

## Runtime inputs

- `lex_modes.csv` — parse-state to internal/external lexer state
- `lex_dispatch.csv` — ordered lexer transitions; evaluate `predicate_id`
- `lex_predicate_bytecode.csv` — postfix predicate bytecode
- `lex_character_sets.csv` — ranges used by `SET_CONTAINS`
- `lex_accepts.csv` — accepted token symbol per lexer state
- `parse_dispatch.csv` — canonical `(parse_state, symbol) -> ACTION or STATE(GOTO)`
- `parse_action_lists.csv` — decoded SHIFT/SHIFT_REPEAT/SHIFT_EXTRA/REDUCE/ACCEPT_INPUT/RECOVER entries
- `symbols.csv` — symbol ids, display names, metadata
- `productions.csv`, `field_map.csv`, `alias_sequences.csv` — reduction node metadata
- `external_scanner_kernels.csv` — external-scanner evidence (scanner code remains separate)

## Generic parser mechanics

`ACTION`: lookup `(top_state, lookahead_symbol)` in `parse_dispatch.csv`, then execute its `action_list_id`.

`REDUCE`: pop `child_count`; build lhs node; lookup `(state_after_pop, lhs_symbol_id)` in `parse_dispatch.csv`. A `STATE` entry is the GOTO.

## Lexer predicate bytecode

Opcodes are: `PUSH_LOOKAHEAD`, `PUSH_EOF`, `PUSH_INT`, `PUSH_BOOL`, `SET_CONTAINS`, `CMP_EQ`, `CMP_NE`, `CMP_LT`, `CMP_LE`, `CMP_GT`, `CMP_GE`, `NOT`, `AND`, `OR`. Evaluate with a boolean/integer stack. Transitions are ordered by `transition_ordinal`; first matching transition wins.

## Source coverage

Available parser.c: 12/13. Missing: swift.

See `MATRIX_DIMENSIONS.json` for validation counts.
