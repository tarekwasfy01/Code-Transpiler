# CODEX — USE THESE MATRICES DIRECTLY

Do not re-parse `action_args` or lexer guards. They are already decoded.

Implement only the generic interpreters:

```text
Source
 -> GenericLexerEngine(lex_modes + lex_dispatch + predicate_bytecode + character_sets + lex_accepts)
 -> GenericLR/GLRParser(parse_dispatch + parse_action_lists)
 -> neutral ParseNode(symbol, production_id, children, fields, aliases)
 -> existing 33 ProducerClasses
 -> CanonicalSemanticEvents
 -> FrontendSemanticFacts
 -> UAST
```

Important:
- `dispatch_kind=STATE` is GOTO; do not build a separate GOTO grammar.
- REDUCE already has `lhs_symbol_id`, `child_count`, `dynamic_precedence`, and `production_id`.
- Large/small parse tables are already unified in `parse_dispatch.csv`.
- No productive call to Legacy/Canonicalize/TraceCanonicalize. Legacy may only be used as an oracle.
- External scanner rows are explicitly separate; do not fake them as the internal lexer.

Acceptance target for available parser tables:
`GENERIC_PATH_CALLS_LEGACY=false` and at least one source performs SHIFT+REDUCE+GOTO+ACCEPT using these CSVs.
