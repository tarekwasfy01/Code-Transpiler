# Local matrix workbench

From PowerShell, run the root `run-matrix-workbench.ps1` script. It changes to the
repository root, uses the workspace Go cache and runs the current Go tool:

```powershell
.\run-matrix-workbench.ps1
```

For calculation and native analysis without the project test suite:

```powershell
.\run-matrix-workbench.ps1 -SkipTests
```

No Python, NumPy, external AI service or additional installation is needed. Go
must be available. The tool writes a fresh timestamped directory under
`outputs/matrix-workbench/`; it never overwrites an existing report directory or
changes compiler implementation cells.

Direct invocation and options:

```powershell
go run ./cmd/matrix-workbench -test -timeout 10m
go run ./cmd/matrix-workbench -sources tests/matrix-workbench -out outputs/my-new-report
```

`-sources` reads every `*.go.txt` file as a native Go analysis probe. The current
frontend requires self-contained sources without imports. Failed probes produce
FAIL evidence and a nonzero exit code; they are not silently omitted. Analysis
success does not mean the construct can execute in a target language.

The program loads the current compiled registries rather than old CSV snapshots:

* Required operations vector times unsupported stage matrix gives stage gaps.
* Required features vector times unsupported target matrix gives target gaps.
* Unsupported times its transpose counts common missing targets between features.
* Directed closure computes candidate intermediate-language routes for the exact
  operation basis. It does not establish translation fidelity along those routes.
* Native probes produce type-use, structural-edge and nominal-resolution matrices.

Reports contain:

* `report.json`: input source hashes, live matrices, probe results, test totals and
  explicit evidence limits.
* `gaps.csv`: missing operation/stage and feature/target cells.
* `feature-worklist.csv`: features sorted by number of missing targets, with equal
  weight. This is a worklist, not a universal semantic completion percentage.
* `SUMMARY.md`: human-readable totals.
* `tests.jsonl` and `tests.stderr.txt`: raw test evidence when `-test` is used.

Project tests cover the root, commands and internal packages. Counts include
subtests. External compiler execution remains subject to the existing test opt-in
environment variables; skipped tests are reported separately. A matrix declaration
does not become verified just because unrelated tests passed.

The archive's broader 310-feature handoff model is not automatically re-scored:
its feature-to-test evidence mapping still needs to be connected. The tool does
not synthesize missing compiler functions or claim unsupported languages work.
