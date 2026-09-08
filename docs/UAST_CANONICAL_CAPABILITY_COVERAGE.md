# Canonical UASF coverage

The Universal AST schema contains all 334 canonical semantic facets
(`UASF_0001` … `UASF_0334`) and the complete structural catalog (109 kinds,
55 concrete relations, and 57 universal fields).  Every canonical facet is
therefore representable in `UniversalASTDocument` even when a frontend has not
yet produced a proving observation for a particular source feature.

Evidence controls the source-feature crosswalk, not structural existence:

```text
canonical UASF -> representable in UAST
source feature -> UASF -> requires formal/spec/compiler or empirical proof
CONFLICT -> blocks that mapping
UNRESOLVED -> remains unpromoted
```

The matrix engine reads only `matrices/uast_engine` for canonical dimensions.
External proof, MLCPD, miner, and corpus files are evidence inputs and are
never auto-discovered as new dimensions.  The current canonical run reports
13 languages, 385 source-feature IDs, and 334 UASF with zero accidental
external dimensions.
