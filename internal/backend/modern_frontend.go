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
		for _, candidate := range candidates {
			if candidate == nil {
				continue
			}
			program, err := candidate(filename, source)
			if err == nil {
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
	frontends["go"] = firstModernFrontend(
		func(filename, source string) (*SemanticProgram, error) {
			return LowerNativeGo(filename, source)
		},
		frontends["go"],
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
