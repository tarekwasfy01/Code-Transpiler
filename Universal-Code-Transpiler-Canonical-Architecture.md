# Universal Code Transpiler – Canonical Architecture

## Status

This document is the authoritative architecture specification for the project.

If older prompts, handoffs, comments, reports, implementation notes, or chat transcripts conflict with this document, THIS DOCUMENT WINS.

Do not infer architecture from historical implementation details.

---

# 1. Fundamental Definition

`SemanticProgram` IS the Universal AST.

There are not two permanent intermediate representations.

The intended identity is:

```text
SemanticProgram = Universal Semantic AST = canonical IR
```

`UniversalASTDocument` may currently exist as a field, structure, migration representation, or implementation detail inside `SemanticProgram`, but it must not become a second permanent IR.

The final architecture must contain exactly one canonical language-independent semantic representation: `SemanticProgram`.

---

# 2. Final Pipeline

```text
Source Language
      ↓
Language Frontend
      ↓
Semantic Extraction
      ↓
SemanticProgram / UAST
      ↓
Language-Independent Transformations
      ↓
Target-Language Projection
      ↓
Target Backend
      ↓
Target Source
```

There must ultimately be no mandatory intermediate conversion:

```text
SemanticProgram → another IR → Backend
```

because `SemanticProgram` itself is the universal IR.

---

# 3. Purpose of SemanticProgram

`SemanticProgram` must represent program meaning independently of source-language syntax.

It must be capable of representing at least:

- structural nodes
- semantic facets
- typed universal fields
- source positions
- symbols
- declarations
- references
- scopes
- types
- operators
- literals
- calls
- parameters
- returns
- modules/imports
- control flow
- data flow
- effects
- exceptions
- ownership/borrowing where applicable
- memory semantics where applicable
- concurrency semantics
- generics/templates
- traits/interfaces/protocols
- language-specific semantic facets where universalization would otherwise lose meaning
- coverage information
- source-language semantic projection information

Language-specific information may exist inside the universal model as explicit semantic facets or fields.

It must NOT require a separate language-specific AST to preserve meaning after frontend lowering.

---

# 4. Matrix-Driven Semantic Model

The Universal AST schema is derived from the collected language semantic matrices.

Matrices are not merely documentation.

They are part of the architecture.

Conceptually:

```text
Language Features
      ↓
Language → Semantic Matrix
      ↓
Semantic Facets
      ↓
Semantic → Structure Matrix
      ↓
Universal Structure Nodes
      ↓
Structure → Field Matrix
      ↓
Valid Universal Fields
```

Relations are similarly derived:

```text
Semantic Facets
      ↓
Semantic → Relation Matrix
      ↓
Allowed Relations
```

Target-language projection must also become matrix-driven where practical.

---

# 5. No Rankings or Priority Heuristics

Do not introduce language rankings, semantic priority lists, preferred-language ordering, or arbitrary rule precedence as a replacement for the matrix model.

Do not solve ambiguity by saying one language is more representative than another.

Universal semantic classes must arise from equivalence, compatibility, crosswalks, and matrix algebra.

---

# 6. No Invented Semantics

If a frontend cannot prove a semantic fact, do not set it.

Rule:

```text
Not proven ≠ true
```

Unknown semantics remain unknown.

Do not guess semantic facets merely because a syntactic construct usually means something.

---

# 7. No Silent Semantic Loss

If a construct can be represented in `SemanticProgram`, but a target backend cannot express it, the backend must explicitly report that incompatibility.

Never silently:

- drop a node
- remove a relation
- erase an effect
- discard ownership semantics
- flatten unsupported control flow
- replace semantics with an approximation without recording that transformation

Rule:

```text
Unsupported ≠ removable
```

---

# 8. Current Migration State

The project currently contains an older executable `SemanticProgram` representation and a newer universal representation named `UniversalASTDocument`.

This duplication is TEMPORARY.

The current compatibility bridge exists only to prove that the universal representation is expressive enough.

Temporary migration path:

```text
Old SemanticProgram representation
        ↓
Universal representation
        ↓
Old representation
```

The purpose of this roundtrip is validation.

It is NOT the final architecture.

---

# 9. Required Migration

Complete:

```text
Old SemanticProgram
→ Universal representation
→ Old SemanticProgram
```

and validate semantic equivalence.

After equivalence is established:

1. Promote the universal structures into the canonical `SemanticProgram`.
2. Remove or demote duplicated old Statement/Expression/etc. structures.
3. Convert old representations into compatibility views/adapters if temporarily required.
4. Backends migrate to consuming canonical `SemanticProgram`.
5. Frontends migrate to producing canonical `SemanticProgram`.
6. Delete obsolete compatibility layers once no longer needed.

Final state:

```text
Frontend
   ↓
SemanticProgram / UAST
   ↓
Backend
```

---

# 10. Avoid Parallel ASTs

Do not permanently keep:

```text
SemanticProgram
├── Old Statements
├── Old Expressions
└── UniversalASTDocument
```

as two independently authoritative representations.

That creates synchronization problems and defeats the purpose of the universal AST.

Instead:

```text
SemanticProgram
├── Universal Nodes
├── Semantic Facets
├── Universal Fields
├── Relations
├── Types
├── Symbols
├── Control Flow
├── Data Flow
├── Coverage
└── Language Projection
```

Any legacy Statement/Expression API should eventually become:

- a view,
- an adapter,
- a helper API,

or be removed.

It must not remain an independent semantic truth source.

---

# 11. Universal Node Principle

A node should represent universal semantic structure rather than source-language syntax.

Example:

An `if` construct is not fundamentally a Python-if, Go-if, Rust-if, etc.

It represents a universal conditional semantic structure with relations such as:

```text
conditional
├── condition
├── true branch
└── false branch
```

Language-specific properties may be attached as facets.

The same principle applies to:

- loops
- calls
- assignments
- declarations
- pattern matching
- exceptions
- async operations
- ownership
- generators
- macros where semantically representable

---

# 12. Relations Are First-Class

Tree structure alone is insufficient.

The UAST must support graph relations including, as available:

```text
syntax.child
symbol.declares
symbol.references
type.hasType
call.targets
control.next
control.branch
control.loop
data.read
data.write
data.dependsOn
```

Additional relations should be generated from the schema/matrices rather than introduced as backend-specific hacks.

---

# 13. Schema Evolution

If implementation discovers missing semantics:

DO NOT work around the missing schema using ad-hoc Go structures.

Instead:

```text
Observed semantic gap
      ↓
Check language matrices / handoffs
      ↓
Extend semantic facet model
      ↓
Recompute algebra/schema
      ↓
Regenerate universal_ast_schema.json
      ↓
Update validation
      ↓
Update implementation
```

The generated schema remains the source of structural validity.

---

# 14. Language Scaling Goal

The architecture must avoid pairwise translators.

Wrong:

```text
Python → Go
Python → Rust
Go → Python
Go → Rust
Rust → Python
...
```

Correct:

```text
Python ─┐
Go ─────┤
Rust ───┤
R ──────┼→ SemanticProgram/UAST → target projection
Swift ──┤
C ──────┤
C++ ────┤
C# ─────┘
```

For N languages, the conceptual implementation should approach:

```text
N frontends + N backends
```

rather than:

```text
N × (N - 1) translators
```

---

# 15. Frontend Responsibility

A frontend must extract provable semantic facts from its language.

Eventually it should write directly into the universal `SemanticProgram`.

Frontend-specific parsing infrastructure may remain language-specific.

But after semantic lowering:

```text
Language-specific AST
        ↓
SemanticProgram
```

the backend must not depend on the original parser AST.

---

# 16. Backend Responsibility

A backend consumes semantic meaning, not source-language syntax.

The backend should answer:

```text
How can this universal semantic construct be represented in target language X?
```

not:

```text
How do I translate Python node Y into Rust node Z?
```

Backends must use explicit compatibility/projection logic.

---

# 17. Target Projection

Target-language projection should eventually use the same semantic matrix system.

Conceptually:

```text
Universal Semantic Vector
        ↓
Target Capability Matrix
        ↓
Representable Semantics
        +
Unsupported Semantics
        +
Required Lowerings
```

This should allow the system to distinguish:

- directly representable
- representable after valid lowering
- representable with runtime support
- impossible without semantic loss

---

# 18. Roundtrip Invariant

During migration:

```text
Old SemanticProgram
      ↓
Universal representation
      ↓
Old SemanticProgram'
```

must preserve semantic equivalence.

Test at least:

- structural meaning
- literals
- operators
- declarations
- symbols
- references
- types
- parameters
- calls
- imports
- control flow
- data flow
- source mapping
- supported semantic metadata

Byte-for-byte equality is not required.

Semantic equivalence is required.

---

# 19. Validation Invariants

Every imported or constructed SemanticProgram/UAST must validate:

- structure-node validity
- semantic facet validity
- node-field mask compatibility
- relation validity
- relation endpoint validity
- reference integrity
- source-position validity
- language projection consistency
- coverage lower bound
- coverage upper bound
- schema version
- matrix-derived invariants

Invalid semantic documents must fail explicitly.

---

# 20. Coverage

Coverage must distinguish at least:

```text
definitely represented semantics
```

from:

```text
possibly represented / unresolved semantics
```

using lower and upper coverage where applicable.

Coverage is not merely a percentage.

It is part of semantic correctness.

---

# 21. Current Priority

Do not expand randomly into additional languages yet.

Current sequence:

```text
1. Complete old SemanticProgram → universal representation
2. Complete universal representation → old SemanticProgram
3. Prove roundtrip preservation
4. Fill schema gaps algebraically
5. Consolidate universal representation into SemanticProgram
6. Make SemanticProgram the only canonical IR
7. Migrate backends to canonical SemanticProgram
8. Migrate frontends to canonical SemanticProgram
9. Remove obsolete compatibility representations
10. Expand language coverage
11. Expand control/data/type/effect semantics
12. Build target projection matrices
```

Do not skip consolidation and build another permanent layer.

---

# 22. Non-Goals

Do NOT:

- create another IR beside SemanticProgram
- introduce a new universal AST with another name
- keep two canonical ASTs
- solve compatibility through thousands of pairwise source-target rules
- guess missing semantic facts
- silently delete unsupported semantics
- replace matrix algebra with rankings
- add language-specific backend hacks when the universal schema is missing a concept
- optimize code architecture at the expense of semantic fidelity

---

# 23. Core Invariant

The entire project follows this rule:

```text
One canonical semantic program.
Many language projections.
```

Or equivalently:

```text
SemanticProgram = Universal AST
```

This invariant must remain true in all future architectural changes.

---

# 24. Decision Rule for Codex

Before implementing any architectural change, ask internally:

```text
Does this make SemanticProgram more universal,
or does it accidentally create another representation?
```

If it creates another independent representation, do not implement it.

If existing code forces temporary duplication, mark that duplication explicitly as migration-only and provide a path for its removal.

---

# 25. Definition of Success

The Universal AST is complete enough when a source program can be:

```text
Source
  ↓
Frontend
  ↓
SemanticProgram
  ↓
Target Backend
  ↓
Target
```

without requiring the source AST again and without losing any semantics supported by the universal schema.

The final measure of success is not how many AST node names exist.

It is whether program semantics survive language-independent representation and target projection.