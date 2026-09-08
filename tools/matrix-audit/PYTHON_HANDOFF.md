# CPython matrix handoff execution

Run `./run-python-handoff.ps1` from PowerShell. Optional `-Handoff` and `-Source`
override the local archive and CPython checkout. `-SkipProjectTests` still runs
the signature differential tests, but skips the broad Go project test suite.
Requires local Go and Python 3.12+ (the current CPython corpus may need a newer
Python parser). No scanned source is executed and no packages are installed.

The pipeline generates, rather than manually fills, matrix cells:

1. Read archive CSV bases by feature name; verify dimensions and finite values.
2. Calculate base gaps, propagated gaps, cluster scores, semantic axis gaps and
   mapped canonical ASDL/grammar gaps. Check reproduced exported scores with a
   tolerance for rounded coefficients. Never infer current implementation from
   old support estimates.
3. Hash the local CPython tree. Parse all Python source files using the local
   Python AST. Generate file/feature counts and P-transpose times P cooccurrence.
   Preserve parse failures explicitly. Read constructors only from canonical
   Python.asdl and PEG headers only from canonical python.gram.
4. Capture CL01 declaration syntax: modules/imports/classes, signatures,
   annotation trees, type parameters, bases and decorators. Produce ownership
   and declaration-kind incidence matrices and count vectors.
5. Compare the shared Go signature binder with Python inspect.Signature.bind
   over 640 automatically generated calls. Verify default-selection and argument
   cardinality vectors as well as acceptance/rejection.
6. Recalculate the live compiler matrices and run project tests.

## Evidence boundaries

The archive's source scanner and complete coefficient-generation implementation
were not supplied. Reproduction uses its exported propagation operator and
support vector; the fresh AST scan is a separate collector, not an identical
rerun of the original mixed C/docs/grammar scanner. Feature extraction rules are
syntax signals, not proof of language support. Unmapped canonical entries remain
visible. Files with parse errors contribute no AST counts.

CL01 is **partial**, not complete. `BindSignature` implements the neutral
argument-binding primitive, including positional-only, positional-or-keyword,
keyword-only and variadic modes. It rejects unresolved spreads, duplicate
arguments, missing required parameters and malformed signatures. Matrix columns
preserve input order. It does not evaluate defaults, decorators or annotations.

The declaration capture format is explicitly non-executable Python syntax
evidence, not SemanticDocument. Integration into the executable HIR, module
resolution, class execution, structured resolved annotations and generics remains
open. The binder has not replaced existing R-compatible function call semantics.
No backend capability is enabled and no completion score is raised by these
analysis results. CL02 through CL10 remain to be implemented.
