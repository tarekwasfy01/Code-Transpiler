# Copyright (c) 2026 Tarek Wasfy

# SFGC: Semantic Fixed-Point Grammar Compression

The complete transport-level SFPC specification is in
[`SFPC.md`](SFPC.md).

SFGC is the compression and interchange model used by the readable `.se`
format. It reduces a canonical `SemanticProgram` to explicit semantic facts,
stable references, schema defaults, and deterministic derivations. It does not
introduce a second persistent IR: the canonical UAST remains authoritative.

## Model

A canonical program is treated as a typed attributed graph:

```text
P = (V, E, T, A, C)
```

where `V` are UAST nodes, `E` relations, `T` type information, `A` semantic
attributes, and `C` contracts and execution constraints.

The two normative operations are:

```text
R(P)  = canonical .se reduction
X(SE) = expansion into SemanticProgram
```

The required contracts are:

```text
X(R(P)) ≡ N(P)
R(X(SE)) = SE
```

The second equation is byte exact for canonical `.se` documents.

## Field classes

Every serialized field belongs to one representation class:

| Class | Meaning |
|---|---|
| `EXPLICIT` | Irreducible information that must be stored. |
| `REFERENCED` | Stored once and addressed by a stable ID or pool. |
| `DEFAULT` | Reconstructed from the versioned schema or profile. |
| `DERIVED` | Recomputed deterministically from stored facts. |
| `DEBUG_ONLY` | Provenance or diagnostic information kept in the lossless form. |

The writer may omit only values whose reconstruction is unambiguous. Unknown
extension fields remain lossless through their explicit field spelling.

## Bounded expansion

SFGC derivations are deliberately non-Turing-complete. The permitted families
are references, pools, ranges, sparse sets/matrices, DAG sharing, containment,
sequence, projection, and deterministic derivation. No shell, network, dynamic
imports, evaluation, or arbitrary recursion is allowed.

For a grammar rule `G_i`:

```text
Size(G_i) = local(G_i) + Σ multiplicity(i,j) * Size(G_j)
```

Bounds are checked before materialization. Inputs that exceed policy limits,
contain cycles, invalid references, overflowing ranges, or malformed deltas
are rejected closed without unbounded allocation.

## Canonical fixed point and root

The semantic root is computed as:

```text
H(P) = SHA256(CanonicalSemanticSerialization(N(P)))
```

The `.se` header carries the root and the parser verifies it after expansion.
The fixed-point serializer is deterministic: field order, IDs, references, and
rewrite tie-breaking are canonical.

Compression is applied only when the exact UTF-8 byte cost decreases:

```text
gain(rule, S) = |UTF8(S)| - |UTF8(rule(S))|
```

Only `gain > 0` rewrites are legal. Therefore every applied rewrite strictly
reduces the finite byte length and the optimization phase terminates.

There is no universal claim that every `.se` is smaller than every JSON form;
the engineering target is measured against compact JSON, FlatSE, SP, and SPZ.

Two serialization modes are available. `MarshalSemanticSESemanticOnly` omits
the `DEBUG_ONLY` source surface and emits only the semantic core; ordinary
`MarshalSemanticSE` retains the source surface for exact source-preserving
roundtrips. Both modes use the same canonical UAST and are verified by the
same parser.

## Compatibility

`.sp` remains unchanged. `.spz` continues to carry either `sp <version>` or
`se <version>` text and selects the parser from the decompressed header.
`.semantic.json` remains the debug/interchange oracle, while productive `.se`
parsing uses the structured SE value parser rather than an embedded canonical
JSON payload.

## SFPC verification kernel

The phase-two SFPC verifier is intentionally independent of lowering. Its
entry point is `backend.VerifySemanticSE(data, maxBytes)`. It checks UTF-8,
the `se` header, schema validity, canonical UAST validation, duplicate node
IDs, dangling node references, and the configured input bound. A document is
accepted only when every check succeeds; malformed input returns a structured
error result and never panics.

The verifier computes the semantic root from the canonical serialization:

```text
root = SHA256(CanonicalSemanticJSON(UniversalAST))
```

The result exposes the root, node/relation counts, and the expansion bound so
callers can enforce policy before materialization. It does not infer missing
semantics, run a compiler, or treat diagnostics as evidence.

`OpenSFPCQuery` provides direct semantic accessors (`GetNode`, `GetType`,
`GetRelations`, `GetFunction`, `GetBinding`, and `GetEffects`). `MerkleRoots`
returns domain-separated program, node, type, relation, and contract roots.
These are deterministic UAST queries and do not introduce a second stored
representation.

## Semantic-only `.se`

The CLI emits `.se` without the optional source `surface` by default. This is
the compact fixed-point form; provenance can be requested explicitly:

```text
CodeTranspiler.exe semantic-export -source go input.go -format se -o input.se
CodeTranspiler.exe semantic-export -source go input.go -format se -preserve-source -o input.se
CodeTranspiler.exe semantic-convert input.json -o input.se
```

The public API exposes the same choice through
`MarshalSemanticSESemanticOnly` and `MarshalSemanticSEWithSource`.

The phase-two artifacts under `outputs/sfpc-phase2/` distinguish machine
checked properties from proof sketches and empirical measurements. A passing
verification run is therefore reported as `MACHINE-CHECKED`, never as a
mathematical proof.

## Status vocabulary

Reports distinguish `PROVED`, `PROOF_SKETCH`, `EMPIRICALLY_VALIDATED`,
`CONJECTURE`, and `ENGINEERING_TARGET`. Passing a test is reported as empirical
validation, not as a mathematical proof.
