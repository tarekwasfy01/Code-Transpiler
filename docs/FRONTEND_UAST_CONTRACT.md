# Frontend-to-UAST contract

Every language frontend emits short-lived `FrontendSemanticFacts`. They are a
frontend result, not a second intermediate representation. The only canonical
representation remains `SemanticProgram` / `UniversalASTDocument`.

```text
Language parser or HIR
→ FrontendSemanticFacts
→ raw UniversalASTDocument
→ AnalyzeUniversalEvidence
→ NormalizeUniversalAST
→ SemanticProgram
```

## Required facts

A frontend must provide stable node IDs, UAST structure kinds, allowed
universal field values, source spans, and all proven relations. It must also
provide every proven type, symbol and binding, plus its proven frontend
evidence. Relation endpoints use stable node, symbol, binding, or other schema
domains and include role and ordinal data whenever the schema relation carries
them.

The shared builder validates nodes, field masks, relation endpoints and target
schema/matrix constraints. `AnalyzeUniversalEvidence` is the one shared
evidence pass; a frontend must never guess facets or manufacture evidence.

## Optional language facts

A frontend may retain language-local proved facts in `LanguageFacts` while it
is building the UAST. Such facts become output only when they map to an
existing, allowed UAST field, relation, facet, type, or projection. They do not
create language-specific semantic truth alongside the UAST.

## Rule

**Not proven → not emitted.**

Missing source, type, symbol, binding, relation, evidence, or language detail
stays unset. A new frontend must not repair missing facts by names, rankings,
or target-specific heuristics.
