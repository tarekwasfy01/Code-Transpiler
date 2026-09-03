# UAST Semantic Proof Compression v1

This package performs the shortcut discussed in-chat: use the existing compiler/source-derived matrices as a mathematical semantic quotient before doing corpus-scale empirical validation.

## Result

- 808 source features are represented.
- They reduce by exact 67-bit semantic signature equality to 334 unique UASF classes.
- All 808 feature→UASF crosswalk rows are exact signature matches inside the current matrix ontology.
- The 67-bit basis consists of 44 semantic axes + 23 relation axes.
- All 334 UASF signatures are unique.
- 119 UASF classes compress more than one source feature.
- Largest equivalence class: UASF_0208 with 35 source features.
- The 13-language presence matrix contains 1017 Language×UASF present cells and its union covers all 334 UASF capabilities.
- The uploaded K-Java paper contributes 14 independent formal runtime-semantic rules for Java.

## What this buys us

We no longer need to empirically rediscover the internal 808→334 quotient feature by feature.

The remaining empirical work should focus on:
1. basis dimensions without independent specification/formal corroboration,
2. semantics outside the current 67-dimensional ontology,
3. conflicts,
4. runtime/behavioral distinctions that a static signature can miss.

## Important limitation

`EXACT_SIGNATURE_EQUIVALENCE` means exact equality in the project's present semantic matrix representation. It is a strong consistency proof, but not a theorem that the 67-dimensional representation is complete for every subtle language behavior.

See `FORMULAS.md`.
