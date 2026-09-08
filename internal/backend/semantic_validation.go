// Copyright (c) 2026 Tarek Wasfy
package backend

import "fmt"

// ValidateSemanticProgram checks the live tree and recomputes its evidence.
// Mutating Body without updating its relations cannot bypass JSON validation.
func ValidateSemanticProgram(p *SemanticProgram) error {
	if p == nil {
		return fmt.Errorf("missing semantic program")
	}
	if p.UniversalAST != nil {
		// Complete the canonical structural closure before validating the
		// executable graph. Compatibility-imported UAST documents may carry only
		// syntax/evidence relations at ingress; canonicalUniversalAST derives the
		// same binding/scope relations used by the direct runtime and emitters.
		u, err := canonicalUniversalAST(p)
		if err != nil {
			return err
		}
		if err := validateUniversalASTDocument(u); err != nil {
			return err
		}
		if u.Projection != "semantic_document.v1" {
			return nil
		}
		_, err = newUASTExecutionGraph(u)
		return err
	}
	doc, err := p.Document()
	if err != nil {
		return err
	}
	_, err = ParseSemanticDocument(doc)
	return err
}

func validateExecutableDialects(p *SemanticProgram) error {
	// No dialect operation lowerer is registered yet. Requiring only "core"
	// must not make an arbitrary GPU/Rust operation disappear during emission.
	dialects := p.Dialects
	if p != nil && p.UniversalAST != nil {
		dialects = p.UniversalAST.Dialects
	}
	for _, dialect := range dialects {
		if dialect.Name != "core" || len(dialect.Operations) > 0 {
			return fmt.Errorf("unsupported executable dialect %q: no operation lowerer registered", dialect.Name)
		}
	}
	return nil
}
