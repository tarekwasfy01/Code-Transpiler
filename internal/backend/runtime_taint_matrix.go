package backend

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RuntimeTaintFixture is one real, minimal program context.  It is deliberately
// limited to syntax-emitting primitives; metadata, proof and validation-only
// primitives never receive invented source fixtures.
type RuntimeTaintFixture struct {
	ID, Source string
	Primitives []string
}

// RuntimeTaintCell records the real two-stage result for one target and one
// emitted fixture: native first, then the private compatibility fallback.
type RuntimeTaintCell struct {
	Target, Fixture, Primitive, NativeError, RuntimeError, SyntaxFailure   string
	NativeReached, PureNative, RuntimeFallback, SyntaxChecked, SyntaxValid bool
	TaintArtifacts                                                         []string
}

// DefaultRuntimeTaintFixtures covers the executable emission basis rather
// than manufacturing 28 language snippets.  The primitive labels come from
// the existing syntax structure contracts.
func DefaultRuntimeTaintFixtures() []RuntimeTaintFixture {
	return []RuntimeTaintFixture{
		{ID: "literal-expression", Source: "print(1 + 2)", Primitives: []string{"expression", "syntax"}},
		{ID: "binding-declaration", Source: "x <- 1\nprint(x)", Primitives: []string{"binding", "declaration", "data", "expression", "syntax"}},
		{ID: "call-arguments", Source: "print(sum(1, 2))", Primitives: []string{"call", "expression", "syntax"}},
		{ID: "control-branch", Source: "if (TRUE) { print(1) } else { print(0) }", Primitives: []string{"control", "expression", "syntax"}},
		{ID: "aggregate-index", Source: "x <- c(1, 2)\nprint(x[1])", Primitives: []string{"binding", "data", "expression", "syntax"}},
		{ID: "function-return", Source: "f <- function(x) { return(x + 1) }\nprint(f(2))", Primitives: []string{"binding", "call", "control", "declaration", "expression", "syntax"}},
	}
}

// AnalyzeRuntimeTaintMatrix obtains its observations from final emitted target
// source. It never turns an unchecked toolchain, a registry label, or an
// unavailable native route into a positive direct claim.
func AnalyzeRuntimeTaintMatrix(fixtures []RuntimeTaintFixture) ([]RuntimeTaintCell, error) {
	if len(fixtures) == 0 {
		fixtures = DefaultRuntimeTaintFixtures()
	}
	targets := Backends()
	sort.Slice(targets, func(i, j int) bool { return targets[i].ID < targets[j].ID })
	cells := make([]RuntimeTaintCell, 0, len(targets)*len(fixtures)*6)
	for _, fixture := range fixtures {
		program, err := ParseSemantic("r", fixture.Source)
		if err != nil {
			return nil, fmt.Errorf("runtime-taint fixture %s: %w", fixture.ID, err)
		}
		u, err := canonicalUniversalAST(program)
		if err != nil {
			return nil, err
		}
		graph, err := newUASTExecutionGraph(u)
		if err != nil {
			return nil, err
		}
		for _, target := range targets {
			native, nativeErr := generateTargetFromUniversalExisting(u.Evaluation, target.ID, graph)
			taint := AnalyzeRuntimeTaint(native, nil)
			syntax := TargetSyntaxCheck{}
			pure := nativeErr == nil && !taint.Tainted()
			if pure {
				syntax = CheckTargetSyntax(target.ID, native)
				pure = !syntax.Checked || syntax.Valid
			}
			runtimeOK := false
			var runtimeErr error
			if !pure {
				_, runtimeErr = generateTargetFromUniversalCompatibility(u.Evaluation, target.ID, graph)
				runtimeOK = runtimeErr == nil
			}
			for _, primitive := range fixture.Primitives {
				cell := RuntimeTaintCell{Target: target.ID, Fixture: fixture.ID, Primitive: primitive, NativeReached: nativeErr == nil, PureNative: pure, RuntimeFallback: runtimeOK, SyntaxChecked: syntax.Checked, SyntaxValid: syntax.Valid, TaintArtifacts: append([]string(nil), taint.Artifacts...)}
				if nativeErr != nil {
					cell.NativeError = nativeErr.Error()
				} else if syntax.Checked && !syntax.Valid {
					// Preserve the raw parser/compiler observation; SyntaxFailure is
					// only its exact diagnostic quotient.
					cell.NativeError = syntax.Failure
				}
				if runtimeErr != nil {
					cell.RuntimeError = runtimeErr.Error()
				}
				if syntax.Checked && !syntax.Valid {
					cell.SyntaxFailure = SyntaxFailureSignature(syntax.Failure)
				}
				cells = append(cells, cell)
			}
		}
	}
	sort.Slice(cells, func(i, j int) bool {
		return strings.Join([]string{cells[i].Target, cells[i].Primitive, cells[i].Fixture}, "|") < strings.Join([]string{cells[j].Target, cells[j].Primitive, cells[j].Fixture}, "|")
	})
	return cells, nil
}

// WriteRuntimeTaintMatrix writes observations, not capability declarations.
func WriteRuntimeTaintMatrix(out string) ([]RuntimeTaintCell, error) {
	cells, err := AnalyzeRuntimeTaintMatrix(nil)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return nil, err
	}
	f, err := os.Create(filepath.Join(out, "runtime_taint_matrix.csv"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write([]string{"target", "execution_primitive", "fixture", "native_emitter_reached", "final_source_runtime_taint", "target_syntax_checked", "target_syntax_valid", "pure_native", "runtime_fallback", "error", "syntax_failure", "runtime_artifacts"}); err != nil {
		return nil, err
	}
	for _, c := range cells {
		errText := c.NativeError
		if errText == "" {
			errText = c.RuntimeError
		}
		if err := w.Write([]string{c.Target, c.Primitive, c.Fixture, fmt.Sprintf("%t", c.NativeReached), fmt.Sprintf("%t", len(c.TaintArtifacts) != 0), fmt.Sprintf("%t", c.SyntaxChecked), fmt.Sprintf("%t", c.SyntaxValid), fmt.Sprintf("%t", c.PureNative), fmt.Sprintf("%t", c.RuntimeFallback), errText, c.SyntaxFailure, strings.Join(c.TaintArtifacts, ";")}); err != nil {
			return nil, err
		}
	}
	return cells, w.Error()
}
