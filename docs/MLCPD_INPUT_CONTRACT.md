# MLCPD streaming input contract

The runner output is consumed as a corpus source, never as a UAST source. A
results directory may contain `records/*.jsonl.gz` shards; each JSON object
must provide `language` and `code` (aliases `language_id` and `source_code` are
accepted), with optional `id`, package/path, `lang_specific_parse`,
`universal_schema`, and `num_errors` fields. The pipeline streams gzip JSONL
records and also accepts CSV fixtures. Parquet shards are left untouched for a
reader supplied by the streaming runner and are never misinterpreted as CSV.

`lang_specific_parse`, `universal_schema`, and the runner's structural matrices
are stored as `MLCPD_STRUCTURAL` evidence only. For every record containing
source code, the productive language frontend is the sole producer of the
UAST:

```text
source -> FrontendSemanticFacts -> Raw UAST -> EnrichUniversalAST
       -> AnalyzeUniversalEvidence -> NormalizeUniversalAST
```

The result is either `UAST_FULL` or `UAST_REJECTED_GAP` (with optional
technical input/parser states). No MLCPD node or schema name is promoted to a
Universal AST node, facet, relation, field, or capability.
