package backend

import (
	"encoding/json"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"strconv"
	"strings"
)

// MatrixFrontendLanguages exposes every language currently backed by the
// matrix grammar extractor. Each uses the same typed-event contract.
func MatrixFrontendLanguages() []string {
	return append([]string(nil), matrixir.Languages[:]...)
}

// LowerMatrixLanguage is the shared frontend path for every currently
// matrix-recognised source language. Parser-specific facts remain transient;
// the returned SemanticProgram owns only the canonical UAST.
func LowerMatrixLanguage(language, source string) (*SemanticProgram, error) {
	canonical, err := matrixir.Canonicalize(language, source)
	if err != nil {
		return nil, err
	}
	builder := &FrontendFactsBuilder{}
	if _, err = LowerMatrixEventsWithFactSink(language, canonical.Events, builder); err != nil {
		return nil, err
	}
	offsets := make([]int, 0, len(canonical.Events))
	for _, event := range canonical.Events {
		offsets = append(offsets, event.Source)
	}
	offsetFacts, err := json.Marshal(offsets)
	if err != nil {
		return nil, err
	}
	facts := builder.Facts
	facts.LanguageFacts = map[string]json.RawMessage{language + ".matrix_event_offsets": offsetFacts, language + ".typed_event_count": json.RawMessage(strconv.Itoa(len(canonical.SemanticEvents)))}
	u, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		return nil, err
	}
	// The frontend facts and its temporary compatibility projection are now
	// discarded. SemanticProgram owns only the canonical UAST.
	return &SemanticProgram{
		Evaluation:       u.Evaluation,
		ValueModel:       u.ValueModel,
		IndexBase:        u.IndexBase,
		Types:            u.Types,
		Origin:           u.Origin,
		Metadata:         u.Metadata,
		Extensions:       u.Extensions,
		Contracts:        u.Contracts,
		Dialects:         u.Dialects,
		SemanticFeatures: u.SemanticFeatures,
		UniversalAST:     u,
		Evidence:         u.Evidence,
	}, nil
}

// LowerMatrixEventsWithFactSink is the productive MatrixIR parser boundary.
// Canonical event text is parsed once into short-lived ParsedNode handles and
// facts; no SemanticDocument or legacy AST is built on this route.
func LowerMatrixEventsWithFactSink(language string, events []matrixir.CanonicalEvent, sink *FrontendFactsBuilder) (*SemanticProgram, error) {
	parts := make([]string, 0, len(events))
	for _, event := range events {
		if strings.TrimSpace(event.Text) != "" {
			parts = append(parts, event.Text)
		}
	}
	facts, err := parseFrontendFacts(language, strings.Join(parts, "\n"), sink)
	if err != nil {
		return nil, err
	}
	u, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		return nil, err
	}
	return &SemanticProgram{Evaluation: u.Evaluation, ValueModel: u.ValueModel, IndexBase: u.IndexBase, Types: u.Types, Origin: u.Origin, Metadata: u.Metadata, Extensions: u.Extensions, Contracts: u.Contracts, Dialects: u.Dialects, SemanticFeatures: u.SemanticFeatures, UniversalAST: u, Evidence: u.Evidence}, nil
}

// LowerPython keeps the concrete frontend entry point while delegating to the
// language-neutral matrix extractor.
func LowerPython(source string) (*SemanticProgram, error) {
	return LowerMatrixLanguage("python", source)
}

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
