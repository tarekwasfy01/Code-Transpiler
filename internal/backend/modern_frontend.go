// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"fmt"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// modernFrontend is the only contract a source frontend exposes to the
// product.  It returns a SemanticProgram whose UniversalAST is already
// built; target code generation never receives source text from this layer.
type modernFrontend func(filename, source string) (*SemanticProgram, error)

func firstModernFrontend(candidates ...modernFrontend) modernFrontend {
	return func(filename, source string) (*SemanticProgram, error) {
		var last error
		for index, candidate := range candidates {
			if candidate == nil {
				continue
			}
			program, err := candidate(filename, source)
			if err == nil {
				if index > 0 && program != nil && program.UniversalAST != nil {
					if program.UniversalAST.Metadata == nil {
						program.UniversalAST.Metadata = map[string]string{}
					}
					program.UniversalAST.Metadata["frontend_route"] = "NATIVE_ORACLE_RESCUE"
				}
				return program, nil
			}
			last = err
		}
		if last == nil {
			last = fmt.Errorf("no modern frontend registered")
		}
		return nil, last
	}
}

// modernFrontends is deliberately a table rather than a language switch.
// The table is the sole place where a concrete parser is selected.  Languages
// without a dedicated native AST frontend use the same MatrixIR structured
// frontend, while Go uses its structured go/ast producer.
var modernFrontends = func() map[string]modernFrontend {
	frontends := make(map[string]modernFrontend, len(matrixir.Languages))
	for _, language := range matrixir.Languages {
		language := language
		frontends[language] = func(_ string, source string) (*SemanticProgram, error) {
			return LowerMatrixLanguage(language, source)
		}
	}
	// Go uses the same universal matrix frontend by default.  The existing
	// structured go/ast producer remains a fail-closed oracle/bootstrap
	// fallback for source-only deployments or incomplete table bundles.
	frontends["go"] = firstModernFrontend(
		frontends["go"],
		func(filename, source string) (*SemanticProgram, error) {
			return LowerNativeGo(filename, source)
		},
	)
	return frontends
}()

// LowerSource is the sole source-to-UAST frontend selector used by the
// product. Frontends may use different parser implementations, but every
// successful implementation returns the same canonical UniversalASTDocument.
// A native structured frontend is attempted where one is registered; the
// matrix frontend is the language-neutral fallback. Neither branch invokes a
// text/regex semantic parser or a legacy AST adapter.
func LowerSource(language, filename, source string) (*SemanticProgram, error) {
	language = NormalizeLanguage(language)
	frontend, ok := modernFrontends[language]
	if !ok {
		return nil, fmt.Errorf("unsupported source language %q", language)
	}
	return frontend(filename, source)
}

// LowerSourceWithDiagnostics runs the same productive frontend selection as
// LowerSource while exposing a separate, transient diagnostic context. In
// strict mode it is equivalent to LowerSource. In saturating mode a failed
// frontend call is recorded as structured diagnostic data, but no placeholder
// is returned as a SemanticProgram and no recovery value is accepted by the
// production path.
func LowerSourceWithDiagnostics(language, filename, source string, mode DiagnosticMode) (*SemanticProgram, *DiagnosticContext, error) {
	ctx := NewDiagnosticContext(mode)
	language = NormalizeLanguage(language)
	var program *SemanticProgram
	var err error
	program, err = LowerSource(language, filename, source)
	if err != nil && mode == DiagnosticSaturate && language == "go" {
		// First preserve the exact productive frontend selection. Only when that
		// path fails do we replay the structured Go AST producer with a separate
		// diagnostic sink to expose node-local contracts. No partial program or
		// transient hole enters canonical UAST.
		var detailErr error
		program, detailErr = LowerNativeGoWithDiagnostics(filename, source, ctx)
		if detailErr != nil {
			err = detailErr
		}
	}
	if err == nil {
		return program, ctx, nil
	}
	failure := SemanticFailure{
		Stage:             "SOURCE_TO_UAST",
		Category:          "FRONTEND",
		SourceLanguage:    language,
		SourceFile:        filename,
		FailureFamily:     "frontend_lowering",
		ObservedStructure: "structured frontend returned error",
		Diagnostic:        err.Error(),
		RecoveryKind:      RecoveryFatal,
		RecoverySafety:    "no-canonical-recovery",
	}
	// Detailed structured failures may already have been recorded by the Go
	// producer. Keep one coarse boundary row only when no detail exists.
	if len(ctx.Failures) == 0 {
		if recordErr := ctx.Record(failure); recordErr != nil {
			return nil, ctx, recordErr
		}
	}
	if mode == DiagnosticSaturate {
		return nil, ctx, err
	}
	if recordErr := ctx.Record(failure); recordErr != nil {
		return nil, ctx, recordErr
	}
	return nil, ctx, err
}
