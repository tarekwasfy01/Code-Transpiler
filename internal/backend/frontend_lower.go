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
	canonical, err := matrixir.NewGenericLexerLREngine(language).Parse(source)
	if err != nil {
		return nil, err
	}
	builder := &FrontendFactsBuilder{}
	if hasStructuredFamilies(canonical.SemanticEvents) {
		if err = materializeStructuredMatrixFacts(language, canonical.SemanticEvents, builder); err != nil {
			return nil, err
		}
	} else if _, err = LowerMatrixEventsWithFactSink(language, canonical.Events, builder); err != nil {
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
		return nil, fmt.Errorf("matrix structured facts to UAST: %w", err)
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

func hasStructuredFamilies(events []matrixir.CanonicalSemanticEvent) bool {
	for _, event := range events {
		if event.FactFamily != "" {
			return true
		}
	}
	return false
}

// materializeStructuredMatrixFacts consumes only MatrixIR roles and operands.
// It intentionally never reads Event.Text: a family without proven child data
// is rejected rather than being reparsed as executable source text.
func materializeStructuredMatrixFacts(language string, events []matrixir.CanonicalSemanticEvent, builder *FrontendFactsBuilder) error {
	header, err := NewUniversalASTDocument(language)
	if err != nil {
		return err
	}
	children := map[int]bool{}
	for _, e := range events {
		if e.FactFamily == "" {
			continue
		}
		if _, ok := matrixir.ProducerClassForEvent(e); !ok {
			return fmt.Errorf("MISSING_PRODUCER_CLASS: %s", e.FactFamily)
		}
		for _, r := range e.Roles {
			children[r.ChildNodeID] = true
		}
	}
	emit := func(id int, structural, kind string, event *matrixir.CanonicalSemanticEvent) error {
		n := UniversalASTNode{ID: id, StructuralKind: structural, SemanticFacets: defaultUniversalFacets(structural), Fields: map[string]json.RawMessage{}}
		mask, e := universalFieldMask(&n)
		if e != nil {
			return e
		}
		n.FieldMask = mask
		for _, pair := range []struct {
			k string
			v any
		}{{"id", id}, {"kind", kind}} {
			if containsString(mask, pair.k) {
				raw, _ := json.Marshal(pair.v)
				n.Fields[pair.k] = raw
			}
		}
		if event != nil {
			if name := event.Fields["name"]; name != "" && containsString(mask, "name") {
				raw, _ := json.Marshal(name)
				n.Fields["name"] = raw
			}
			op := universalOperationRecord{Operator: event.Fields["operator"], LiteralKind: event.Fields["literal_kind"], Text: event.Fields["value"]}
			if op.Operator != "" || op.LiteralKind != "" || op.Text != "" {
				if raw, err := json.Marshal(op); err == nil && containsString(mask, "operation") {
					n.Fields["operation"] = raw
				}
			}
		}
		builder.AddNode(n)
		if event != nil && event.SourceOffset >= 0 {
			builder.AddSource(FrontendSourceFact{NodeID: id, Span: SemanticSourceSpan{File: language, StartOffset: event.SourceOffset, EndOffset: event.SourceOffset}})
		}
		return nil
	}
	if err := emit(0, "Scope", "block", nil); err != nil {
		return err
	}
	mapKind := func(k string) (string, string, bool) {
		if structural, kind, ok := matrixUASTKind(k); ok {
			return structural, kind, true
		}
		switch k {
		case "literal":
			return "LiteralExpr", "literal", true
		case "identifier":
			return "SymbolRef", "identifier", true
		case "binary", "unary":
			return "OperationExpr", k, true
		case "deref":
			return "Deref", k, true
		case "address":
			return "AddressOf", k, true
		case "member":
			return "MemberAccessExpr", k, true
		case "call":
			return "CallExpr", "call", true
		case "index", "slice":
			if k == "slice" {
				return "SliceExpr", k, true
			}
			return "IndexExpr", k, true
		case "assign":
			return "AssignStmt", "assign", true
		case "return":
			return "ReturnStmt", "return", true
		case "function", "closure", "lambda":
			return "ClosureExpr", "function", true
		case "for", "foreach", "loop", "iteration":
			// Every matrix front end uses the existing `for` semantic contract for
			// ForEachStmt.  `iteration` is a parsed-family label, not a semantic
			// kind accepted by the UAST schema.
			return "ForEachStmt", "for", true
		case "binding":
			return "BindingPattern", "binding", true
		case "aggregate", "comprehension":
			return "AggregateExpr", "aggregate", true
		case "tuple":
			return "TupleExpr", "tuple", true
		case "block":
			return "Scope", "block", true
		case "if":
			return "IfStmt", "if", true
		case "while":
			return "LoopStmt", "while", true
		case "module":
			return "ModuleDecl", "module", true
		case "exception":
			return "TryStmt", "exception", true
		case "concurrency":
			return "ConcurrencyOp", "concurrency", true
		case "reflection":
			return "ReflectionOp", "reflection", true
		case "expression", "unknown":
			return "OperationExpr", "expression", true
		}
		return "", "", false
	}
	for _, e := range events {
		input, structured := MatrixStructuredAdapter(e)
		structural, kind, ok := mapKind(e.StructureKind)
		if !ok {
			if e.FactFamily != "" {
				return fmt.Errorf("MISSING_STRUCTURED_FACT_DATA: %s/%s", language, e.FactFamily)
			}
			continue
		}
		if structured {
			e.Fields = input.Fields
			e.Roles = input.Roles
			e.Operands = input.Operands
			e.Bindings = input.Bindings
			e.Symbols = input.Types
			e.SourceOffset = input.SourceOffset
		}
		if err := emit(e.ID+1, structural, kind, &e); err != nil {
			return err
		}
	}
	for _, e := range events {
		for _, r := range e.Roles {
			attrs := map[string]json.RawMessage{}
			role, _ := json.Marshal(r.Role)
			ordinal, _ := json.Marshal(r.Ordinal)
			attrs["role"], attrs["ordinal"] = role, ordinal
			builder.AddRelation(FrontendRelationFact{Kind: "syntax.child", From: e.ID + 1, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(r.ChildNodeID + 1)}, Role: r.Role, Ordinal: r.Ordinal, Attributes: attrs})
		}
		if !children[e.ID] {
			attrs := map[string]json.RawMessage{}
			role, _ := json.Marshal("statement")
			ordinal, _ := json.Marshal(e.ID)
			attrs["role"], attrs["ordinal"] = role, ordinal
			builder.AddRelation(FrontendRelationFact{Kind: "syntax.child", From: 0, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(e.ID + 1)}, Role: "statement", Ordinal: e.ID, Attributes: attrs})
		}
	}
	builder.Facts.SchemaVersion = header.SchemaVersion
	builder.Facts.BasisSHA256 = header.BasisSHA256
	builder.Facts.LanguageProfile = header.LanguageProfile
	builder.Facts.LanguageFacet = append([]float64(nil), header.LanguageFacet...)
	// The transport is structured facts; use the established canonical facts
	// projection rather than creating a family-specific representation.
	builder.Facts.Projection = "frontend_facts.v1"
	builder.Facts.Evaluation = "eager_left_to_right"
	builder.Facts.ValueModel = "tagged_dynamic_binary64"
	builder.Facts.IndexBase = 1
	builder.Facts.Types = defaultSemanticTypeContract()
	builder.Facts.Origin = SemanticOrigin{SourceLanguage: language, EntryPoint: "main"}
	builder.Facts.Metadata = map[string]string{"frontend": "matrix-structured-facts-v1"}
	return nil
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
