package backend

import (
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"strings"
)

// LowerMatrixActions is the common-subset frontend boundary. MatrixIR has
// already recognized the source grammar and selected normalized actions; this
// function consumes those actions rather than CanonicalProgram.R. The action
// payload is deliberately kept local to the frontend and never becomes a
// transport representation of Program.
//
// This is a migration boundary, not a claim that every language already has a
// complete native parser. Source-specific lowerers can replace this function
// one language at a time without changing SemanticProgram or emitters.
func LowerMatrixActions(source string, nodes []matrixir.CanonicalNode) (*SemanticProgram, error) {
	if len(nodes) == 0 {
		return nil, fmt.Errorf("%s frontend produced no semantic actions", source)
	}
	parts := make([]string, 0, len(nodes))
	for _, node := range nodes {
		if strings.TrimSpace(node.Text) != "" {
			parts = append(parts, node.Text)
		}
		if strings.TrimSpace(node.Post) != "" {
			parts = append(parts, node.Post)
		}
		for i := 0; i < node.Close; i++ {
			parts = append(parts, "}")
		}
	}
	if len(parts) == 0 {
		return NewSemanticProgram(&BlockStmt{}, "eager_left_to_right"), nil
	}
	// The core parser here decodes the action payload, not user source and not a
	// cross-stage CanonicalR field. It remains temporary until actions carry
	// typed operands directly.
	return ParseSemantic(source, strings.Join(parts, "\n"))
}

// LowerMatrixEvents consumes the ordered structural stream emitted by the
// source matrix adapter. It replaces the old CanonicalProgram.R handoff.
func LowerMatrixEvents(source string, events []matrixir.CanonicalEvent) (*SemanticProgram, error) {
	parts := make([]string, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.Text) != "" {
			parts = append(parts, event.Text)
		}
	}
	if len(parts) == 0 {
		return NewSemanticProgram(&BlockStmt{}, "eager_left_to_right"), nil
	}
	return ParseSemantic(source, strings.Join(parts, "\n"))
}
