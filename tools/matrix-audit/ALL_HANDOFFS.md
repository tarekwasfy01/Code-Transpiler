# Joint Go, Python, R, Rust, C/C++, Kotlin, Java and C# workflow

Run from PowerShell:

```powershell
.\run-all-handoffs.ps1
```

All archive paths and the CPython source path can be overridden with
`-GoHandoff`, `-PythonHandoff`, `-RHandoff`, `-RustHandoff`,
`-ClangCppHandoff`, `-KotlinHandoff`, `-JavaHandoff`, `-CSharpHandoff` and
`-PythonSource`. Use
`-SkipPythonScan` only when the local CPython scan has already been recorded.
The default run includes it. All paths point to local files; no source is
uploaded or executed as part of scanning.

The workflow regenerates `matrices/handoffs/` from all ten archives, runs
calculation tests and 640 signature differential cases,
reproduces the supplied products, computes shared demands, collects CPython
declarations, regenerates the SemanticProgram feature space, then executes the
live compiler workbench and project tests.
Fresh report directories prevent overwriting earlier evidence.

Joint reports under `outputs/all-handoffs/` contain:

- `language-feature.csv`: aligned, normalized 8 x 98 generic evidence matrix.
- `language-nodes.csv` and `language-relations.csv`: products with the supplied
  98 x 82 node and 98 x 23 relation projections.
- `joint-*-demand.csv`: summed demand, with each language equally weighted.
- separate dialect-axis demand files for R, Rust, C/C++, Kotlin, Java and C#.
- `unmapped-dialect-uast.csv`: requirements without a supplied universal-node map.
- `r-clusters.csv`, `r-feature-gaps.csv` and `python/`: reproduced baseline gaps.
- `report.json`: archive hashes, reproduction errors, all 218 imported task
  entries, basis dimensions and explicit evidence limits.

These reports do not say that all tasks are implemented. They align the eight
work orders and make their calculations reproducible. In particular, the
Go-origin shared projection remains a planning hypothesis for other languages, the
imported support estimates may be stale, and demand is not missing-implementation
coverage. The original Go/R/Rust/C++/Kotlin/Java/C# collectors were not provided and have not been
identically rerun. Raw generic scanner counts are not a semantic oracle.

The current shared compiler addition is type-domain equivalence: alias and
nominal-reference resolution, a fixed-point structural relation, and an unknown
vector propagated through type dependencies. The result is embedded in native
analysis and SemanticDocument type relations and verified on JSON import.
It does not implement assignment conversion, subtyping, ABI compatibility,
complete class/module execution or the remaining source-language dialects.

The executable SemanticProgram also has an opt-in exact function-signature
contract. It serializes parameter modes plus default evaluation at definition or
call time. The runtime derives parameter/argument incidence and default vectors,
evaluates each source argument once in column order and rejects duplicate,
missing or inadmissible arguments. Backends currently capability-reject this
contract rather than emitting code with different semantics.

Exact call resolution is represented separately from signature binding. Its
candidate x obligation and candidate x argument-cost matrices plus priority
vector must yield one unique minimum matching the selected declaration. The
direct SemanticProgram runtime executes that declaration; source backends reject
the capability until their overload and conversion rules preserve it.

`build_semantic_space.py` embeds all calculated generic, dialect, node and
relation matrices into `SemanticProgram`. A one-hot source-language vector is
multiplied through these planes. Serialized programs carry the basis and derived
vectors; imports recalculate and verify them instead of trusting stored cells.
