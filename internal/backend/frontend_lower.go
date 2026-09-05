package backend

import (
	"encoding/json"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"sort"
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
	// CanonicalSemanticEvent is the modern frontend boundary.  Materialize it
	// directly for every event, including ordinary statements and expressions.
	// The previous branch reparsed CanonicalEvent.Text through
	// parseFrontendFacts whenever no structured family was present.  That made
	// the productive path depend on a second text parser and could turn a valid
	// frontend result into an unrelated "expected ..." diagnostic.  The
	// compatibility text bridge remains available through its explicit API, but
	// is never reached from the canonical source-to-UAST path.
	if err = materializeStructuredMatrixFacts(language, canonical.SemanticEvents, builder); err != nil {
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
	// Preserve exact source bytes in the canonical surface plane. The matrix
	// facts remain the only semantic input; this payload is used solely by an
	// explicit same-language preservation emission.
	u.Surface = NewUniversalASTSurface(language, source)
	// The frontend facts and its temporary compatibility projection are now
	// discarded. SemanticProgram owns only the canonical UAST.
	return &SemanticProgram{
		CompatibilityR:   canonical.R,
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

// incompleteUniversalShape returns the missing structural contract dimension
// for a known semantic construct.  It only inspects parser-produced roles; it
// never infers operands from source spelling.  An empty result means that the
// event already has a shape that can be represented directly (or that its
// shape has no mandatory child contract in the canonical schema).
func incompleteUniversalShape(kind string, roles []matrixir.CanonicalRoleFact, fields map[string]string) string {
	has := func(names ...string) bool {
		for _, role := range roles {
			canonical := universalRelationRoleAt(kind, role.Role, role.Ordinal)
			for _, name := range names {
				if canonical == name {
					return true
				}
			}
		}
		return false
	}
	switch strings.ToLower(kind) {
	case "if":
		if !has("condition") {
			return "condition"
		}
	case "while":
		if !has("condition") {
			return "condition"
		}
	case "for", "foreach", "loop", "iteration":
		if !has("sequence", "iterable", "source", "value") {
			return "iterable"
		}
	case "function", "closure", "lambda":
		// MatrixIR carries block ownership separately as ParentID. The materializer
		// creates the canonical Scope/body relation from that fact, so an absent
		// body role here is not evidence that the function value is unsupported.
	case "call":
		if !has("value", "callee", "function", "target") {
			return "callee"
		}
	case "index", "slice":
		if !has("value", "base") {
			return "base"
		}
	case "binary":
		if !has("left") {
			return "left"
		}
		if !has("right") {
			return "right"
		}
	case "unary":
		if !has("value", "operand") {
			return "operand"
		}
	case "assign":
		// A declaration/binding target can be carried as the already-proven
		// canonical name field. The target emitter consumes that field directly;
		// requiring a duplicate target edge would turn a complete structured
		// binding into an artificial unsupported marker.
		if !has("target") && strings.TrimSpace(fields["name"]) == "" {
			return "target"
		}
		if !has("value", "expression") {
			return "value"
		}
	case "expression":
		// A leaf expression is valid when it carries a literal/name/operator
		// field.  An entirely empty event is the one case that needs a marker.
		if len(roles) == 0 && strings.TrimSpace(fields["name"]) == "" && strings.TrimSpace(fields["value"]) == "" && strings.TrimSpace(fields["operator"]) == "" && strings.TrimSpace(fields["literal_kind"]) == "" {
			return "operand"
		}
	}
	return ""
}

// universalRelationRole is the single schema join between MatrixIR's
// language-neutral role vocabulary and the canonical UAST role vocabulary.
// The conversion is structural (base -> value, iterable -> sequence, ...),
// so no target syntax or source-text interpretation is involved.
func universalRelationRole(kind, role string) string {
	return universalRelationRoleAt(kind, role, 0)
}

// universalRelationRoleAt normalizes the finite MatrixIR role alphabet into
// the canonical child schema. Ordinal is structural parser evidence, used
// only where one neutral repeated operand needs its existing left/right slot;
// no source spelling or language-specific syntax is inspected.
func universalRelationRoleAt(kind, role string, ordinal int) string {
	role = strings.ToLower(strings.TrimSpace(role))
	switch strings.ToLower(kind) {
	case "expression":
		// Generic grammar events often retain a neutral value/operand role. For
		// a one-value expression statement these are schema aliases of the
		// canonical expression child, not language-specific syntax.
		if role == "value" || role == "operand" || role == "base" || role == "receiver" || role == "object" || role == "argument" || role == "child" || role == "item" {
			return "expression"
		}
	case "assign":
		if role == "left" || role == "lhs" || role == "binding" || role == "identifier" || role == "name" || role == "variable" {
			return "target"
		}
		if role == "right" || role == "rhs" || role == "initializer" {
			return "value"
		}
	case "binary":
		if role == "left" || role == "lhs" || role == "first" {
			return "left"
		}
		if role == "right" || role == "rhs" || role == "second" {
			return "right"
		}
		if role == "operand" || role == "value" || role == "argument" || role == "child" {
			if ordinal == 0 {
				return "left"
			}
			return "right"
		}
	case "if":
		if role == "value" || role == "expression" || role == "test" || role == "predicate" || role == "guard" {
			return "condition"
		}
		if role == "body" || role == "block" {
			return "then"
		}
	case "while":
		if role == "value" || role == "expression" || role == "test" || role == "predicate" || role == "guard" {
			return "condition"
		}
		if role == "block" {
			return "body"
		}
	case "unary":
		if role == "operand" || role == "argument" || role == "expression" || role == "child" {
			return "value"
		}
	case "index", "slice":
		if role == "base" {
			return "value"
		}
		if role == "index" || role == "start" || role == "end" || role == "step" {
			return "argument"
		}
	case "return":
		if role == "value" {
			return "expression"
		}
	case "for", "foreach", "loop", "iteration":
		if role == "iterable" || role == "source" || role == "value" || role == "collection" || role == "range" {
			return "sequence"
		}
		if role == "item" || role == "element" || role == "variable" || role == "target" {
			return "binding"
		}
		// Keep the binding edge as a structural relation. The executable
		// ForEach contract consumes the proven binding name from the node's
		// `name` field; the edge itself remains available for graph reachability
		// and tuple/destructuring metadata.
	case "call":
		if role == "function" || role == "target" || role == "callee" || role == "receiver" {
			return "value"
		}
		if role == "args" || role == "arguments" {
			return "argument"
		}
	case "aggregate", "comprehension", "tuple":
		if role == "elements" || role == "items" || role == "element" || role == "value" {
			return "argument"
		}
	case "function", "closure", "lambda":
		if role == "params" || role == "parameters" {
			return "parameter"
		}
		if role == "return" || role == "value" {
			return "body"
		}
	}
	return role
}

// executableSyntaxRole reports whether a relation is part of the executable
// child shape of a canonical node. MatrixIR retains every grammar relation as
// structured evidence, including relations that belong to a parent grammar
// production rather than to the event which happened to carry it. Such a
// relation must stay observable, but it may not be installed as syntax.child
// on an unrelated canonical node: doing so makes an otherwise valid UAST
// impossible to execute (for example an `else` edge on a closure).
//
// This is a schema-level gate, not a language rule. The rejected relation is
// materialized below as semantic.child_evidence with its original role.
func executableSyntaxRole(kind, role string) bool {
	switch strings.ToLower(kind) {
	case "block":
		return role == "statement"
	case "expression":
		return role == "expression"
	case "assign":
		return role == "expression" || role == "value" || role == "target"
	case "if":
		return role == "condition" || role == "then" || role == "else"
	case "while":
		return role == "condition" || role == "body"
	case "for":
		return role == "sequence" || role == "body" || role == "binding"
	case "return":
		return role == "expression"
	case "unary", "iteration":
		return role == "value" || role == "operand"
	case "binary":
		return role == "left" || role == "right"
	case "call":
		return role == "value" || role == "callee" || role == "argument"
	case "index":
		return role == "value" || role == "argument"
	case "function":
		return role == "parameter" || role == "body"
	case "parameter":
		return role == "default"
	}
	// Structural kinds without a restrictive executable child contract keep
	// their typed grammar relations. Validation of those kinds remains owned by
	// the canonical UAST basis.
	return true
}

func executableKindForEvent(kind string) string {
	switch strings.ToLower(kind) {
	case "closure", "lambda":
		return "function"
	case "foreach", "loop", "iteration":
		return "for"
	case "slice":
		return "index"
	}
	return strings.ToLower(kind)
}

// materializeStructuredMatrixFacts consumes only MatrixIR roles and operands.
// It intentionally never reads Event.Text: a family without proven child data
// is rejected rather than being reparsed as executable source text.
func materializeStructuredMatrixFacts(language string, events []matrixir.CanonicalSemanticEvent, builder *FrontendFactsBuilder) error {
	header, err := NewUniversalASTDocument(language)
	if err != nil {
		return err
	}
	// Only executable syntax.child edges participate in root ownership. Raw
	// grammar roles are broader than the canonical shape and must not make a
	// node appear attached before the role has passed the schema gate below.
	children := map[int]bool{}
	bodyOwnerIDs := map[int]bool{}
	for _, e := range events {
		if _, ok := matrixir.ProducerClassForEvent(e); !ok {
			if e.FactFamily != "" {
				return fmt.Errorf("MISSING_PRODUCER_CLASS: %s", e.FactFamily)
			}
		}
		switch strings.ToLower(e.StructureKind) {
		case "if", "while", "repeat", "for", "foreach", "loop", "iteration", "function", "closure", "lambda":
			bodyOwnerIDs[e.ID] = true
		}
	}
	// Compute ownership over the complete typed event graph before attaching
	// root statements. Event IDs are source ordered, so a child can otherwise
	// appear before its parent and be transiently misclassified as a root
	// statement (for example the `x` in `x = 2`). This is a single generic
	// child-recursion pass over MatrixIR roles; it never uses source text.
	for _, e := range events {
		executableKind := executableKindForEvent(e.StructureKind)
		for _, r := range e.Roles {
			canonicalRole := universalRelationRoleAt(e.StructureKind, r.Role, r.Ordinal)
			if executableSyntaxRole(executableKind, canonicalRole) {
				children[r.ChildNodeID] = true
			}
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
			// Tuple/destructuring iteration bindings remain structured metadata on
			// the loop node.  The target emitter consumes this attribute when a
			// native target has a distinct index/value binding form.
			if indexBinding := event.Fields["index_binding"]; indexBinding != "" {
				if n.Attributes == nil {
					n.Attributes = map[string]json.RawMessage{}
				}
				raw, _ := json.Marshal(indexBinding)
				n.Attributes["iteration.index_binding"] = raw
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
		// Generated frontends may already provide the canonical UAST structural
		// name (for example SwitchMatchStmt or AwaitExpr). Accept that exact
		// schema identity without introducing a language-specific handler. The
		// field mask and validation remain owned by the UAST basis matrices.
		if err := loadUniversalASTBasis(); err == nil {
			for _, structural := range uastEmbedded.Basis.StructuralKinds {
				if strings.EqualFold(structural, k) {
					return structural, strings.ToLower(structural), true
				}
			}
		}
		return "", "", false
	}
	eventByID := make(map[int]matrixir.CanonicalSemanticEvent, len(events))
	// The MatrixIR construct label can be broader than the concrete canonical
	// semantic kind selected by mapKind (or can be rewritten to an explicit
	// unsupported marker when mandatory structure is absent). Keep the latter
	// for the relation pass so it validates the shape that was actually emitted.
	executableKindByEvent := make(map[int]string, len(events))
	for _, event := range events {
		eventByID[event.ID] = event
	}
	// MatrixIR may retain module/header and skip actions so grammar/source
	// provenance stays observable.  They do not denote executable UAST nodes
	// when they carry no structured payload; emitting them as OperationExpr
	// creates a phantom statement with a missing `expression` child.
	isScaffold := func(e matrixir.CanonicalSemanticEvent) bool {
		if e.Action == "skip" {
			return true
		}
		return e.Action == "module" && len(e.Roles) == 0 && len(e.Operands) == 0 && len(e.Bindings) == 0 && len(e.Symbols) == 0 && len(e.Fields) == 0
	}
	isBranchEvent := func(e matrixir.CanonicalSemanticEvent) bool {
		if !strings.EqualFold(e.StructureKind, "if") {
			return false
		}
		branch := strings.ToLower(strings.TrimSpace(e.Fields["branch"]))
		return branch == "else" || branch == "else-if"
	}
	for _, e := range events {
		if isScaffold(e) {
			// Keep the event's stable numeric identity so evidence/binding
			// matrices remain aligned, but represent a grammar-only scaffold as a
			// non-executable module node.  It must never become an OperationExpr.
			if err := emit(e.ID+1, "ModuleDecl", "module", &e); err != nil {
				return err
			}
			executableKindByEvent[e.ID] = "module"
			continue
		}
		if isBranchEvent(e) && len(e.Roles) == 0 {
			// Preserve the event's stable ID while representing a plain else
			// header as the branch Scope itself. Its statements are attached in
			// the block materialization pass below.
			if err := emit(e.ID+1, "Scope", "block", nil); err != nil {
				return err
			}
			executableKindByEvent[e.ID] = "block"
			continue
		}
		input, structured := MatrixStructuredAdapter(e)
		// Keep the parser's original semantic kind for relation conversion even
		// when an incomplete shape is represented by an explicit unsupported
		// marker below.  Otherwise a structured index/loop role would be
		// reclassified as a generic expression during validation.
		semanticKind := e.StructureKind
		structural, kind, ok := mapKind(semanticKind)
		if !ok {
			if e.FactFamily != "" {
				return fmt.Errorf("MISSING_STRUCTURED_FACT_DATA: %s/%s", language, e.FactFamily)
			}
			// Preserve an otherwise unknown parser construct as an explicit,
			// source-free operation marker.  Dropping the event would detach its
			// semantic facts and make the UAST incomplete.  The marker remains
			// eligible for the normal runtime last resort, while direct emitters
			// continue to reject it as unavailable native semantics.
			original := strings.TrimSpace(e.StructureKind)
			if original == "" {
				original = "unknown"
			}
			fields := make(map[string]string, len(e.Fields)+1)
			for k, v := range e.Fields {
				fields[k] = v
			}
			fields["operator"] = "unsupported." + original + ".mapping"
			e.Fields = fields
			e.Action = "expression"
			e.StructureKind = "expression"
			structural, kind, ok = "OperationExpr", "expression", true
		}
		if structured {
			e.Fields = input.Fields
			e.Roles = input.Roles
			e.Operands = input.Operands
			e.Bindings = input.Bindings
			e.Symbols = input.Types
			e.SourceOffset = input.SourceOffset
		}
		// Assignment bindings are represented structurally by the target child.
		// Carry the proven identifier into the canonical assignment field so all
		// target emitters see the same binding contract; never infer it from the
		// original event text.
		if strings.EqualFold(e.StructureKind, "assign") && e.Fields["name"] == "" {
			for _, role := range e.Roles {
				if role.Role != "target" {
					continue
				}
				if target, ok := eventByID[role.ChildNodeID]; ok && target.Fields["name"] != "" {
					if e.Fields == nil {
						e.Fields = map[string]string{}
					}
					e.Fields["name"] = target.Fields["name"]
				}
				break
			}
		}
		// A loop binding is a structured child fact.  Materialize its proven
		// name into the parent field consumed by the canonical ForEachStmt
		// shape; this is not source reparsing.
		if strings.EqualFold(e.StructureKind, "iteration") || strings.EqualFold(e.StructureKind, "for") {
			if e.Fields["name"] == "" {
				for _, role := range e.Roles {
					if role.Role != "binding" {
						continue
					}
					child, ok := eventByID[role.ChildNodeID]
					if !ok {
						continue
					}
					if child.Fields["name"] != "" {
						if e.Fields == nil {
							e.Fields = map[string]string{}
						}
						e.Fields["name"] = child.Fields["name"]
						break
					}
					// Tuple bindings are represented by one neutral binding
					// pattern. Preserve both proven names as data so the target
					// renderer can select its native index/value form.
					if e.Fields == nil {
						e.Fields = map[string]string{}
					}
					var names []string
					for _, bindingRole := range child.Roles {
						if binding, exists := eventByID[bindingRole.ChildNodeID]; exists && binding.Fields["name"] != "" {
							names = append(names, binding.Fields["name"])
						}
					}
					if len(names) > 0 {
						e.Fields["name"] = names[len(names)-1]
					}
					if len(names) > 1 {
						e.Fields["index_binding"] = names[0]
					}
					break
				}
			}
		}
		// MatrixIR can prove that a construct exists while the current
		// frontend has not yet supplied every child required by its concrete
		// UAST shape (for example an ``if`` whose condition is still opaque).
		// Keep that construct in the canonical graph as an explicit operation
		// marker.  This is deliberately structural: no Event.Text is parsed or
		// copied into the target program.  The execution validator recognizes
		// the marker and the normal compatibility/runtime path can handle it as
		// an unsupported operation instead of rejecting the whole UAST.
		if original := e.StructureKind; original != "" {
			if missing := incompleteUniversalShape(original, e.Roles, e.Fields); missing != "" {
				fields := make(map[string]string, len(e.Fields)+1)
				for k, v := range e.Fields {
					fields[k] = v
				}
				fields["operator"] = "unsupported." + original + "." + missing
				e.Fields = fields
				e.Action = "expression"
				e.StructureKind = "expression"
				structural, kind = "OperationExpr", "expression"
			}
		}
		if err := emit(e.ID+1, structural, kind, &e); err != nil {
			return err
		}
		executableKindByEvent[e.ID] = kind
	}
	for _, e := range events {
		if isScaffold(e) {
			// Grammar-only scaffolds stay attached to the document root as
			// non-executable module statements.
			attrs := map[string]json.RawMessage{}
			role, _ := json.Marshal("statement")
			ordinal, _ := json.Marshal(e.ID)
			attrs["role"], attrs["ordinal"] = role, ordinal
			builder.AddRelation(FrontendRelationFact{Kind: "syntax.child", From: 0, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(e.ID + 1)}, Role: "statement", Ordinal: e.ID, Attributes: attrs})
			continue
		}
		if isBranchEvent(e) && len(e.Roles) == 0 {
			// The branch header was materialized as its own Scope node so the
			// canonical event IDs remain contiguous. Attach that Scope directly
			// to its owning IfStmt as the false branch.
			if e.ParentID >= 0 {
				attrs := map[string]json.RawMessage{}
				role, _ := json.Marshal("else")
				ordinal, _ := json.Marshal(0)
				attrs["role"], attrs["ordinal"] = role, ordinal
				if executableKindByEvent[e.ParentID] == "if" {
					builder.AddRelation(FrontendRelationFact{Kind: "syntax.child", From: e.ParentID + 1, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(e.ID + 1)}, Role: "else", Ordinal: 0, Attributes: attrs})
					children[e.ID] = true
				}
			}
			continue
		}
		if isBranchEvent(e) && e.ParentID >= 0 {
			attrs := map[string]json.RawMessage{}
			role, _ := json.Marshal("else")
			ordinal, _ := json.Marshal(0)
			attrs["role"], attrs["ordinal"] = role, ordinal
			if executableKindByEvent[e.ParentID] == "if" {
				builder.AddRelation(FrontendRelationFact{Kind: "syntax.child", From: e.ParentID + 1, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(e.ID + 1)}, Role: "else", Ordinal: 0, Attributes: attrs})
				children[e.ID] = true
			}
		}
		for _, r := range e.Roles {
			attrs := map[string]json.RawMessage{}
			canonicalRole := universalRelationRoleAt(e.StructureKind, r.Role, r.Ordinal)
			role, _ := json.Marshal(canonicalRole)
			ordinal, _ := json.Marshal(r.Ordinal)
			attrs["role"], attrs["ordinal"] = role, ordinal
			executableKind := executableKindByEvent[e.ID]
			if executableKind == "" {
				executableKind = executableKindForEvent(e.StructureKind)
			}
			if !executableSyntaxRole(executableKind, canonicalRole) {
				// This relation is a grammar ownership edge, not an executable
				// child of the canonical node that carried it. Surface provenance
				// remains on the UAST document; do not manufacture a relation kind
				// outside the fixed UAST relation basis.
				continue
			}
			builder.AddRelation(FrontendRelationFact{Kind: "syntax.child", From: e.ID + 1, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(r.ChildNodeID + 1)}, Role: canonicalRole, Ordinal: r.Ordinal, Attributes: attrs})
			children[r.ChildNodeID] = true
		}
		// A child of a control/function body is attached in the body-scope
		// pass. Other unowned events become root statements rather than detached
		// graph nodes, preserving an executable canonical document.
		if !children[e.ID] && (e.ParentID < 0 || !bodyOwnerIDs[e.ParentID]) {
			attrs := map[string]json.RawMessage{}
			role, _ := json.Marshal("statement")
			ordinal, _ := json.Marshal(e.ID)
			attrs["role"], attrs["ordinal"] = role, ordinal
			builder.AddRelation(FrontendRelationFact{Kind: "syntax.child", From: 0, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(e.ID + 1)}, Role: "statement", Ordinal: e.ID, Attributes: attrs})
		}
	}
	// Block ownership is proved by the MatrixIR grammar/block stack and is
	// carried on CanonicalSemanticEvent.ParentID.  Materialize one ordinary
	// Scope node for each executable parent so loop/function bodies retain the
	// canonical UAST contract (one body child) even when a body contains several
	// statements.  The scope is structural bookkeeping; it introduces no
	// language syntax or new semantic primitive.
	bodyByParent := map[int][]int{}
	for _, e := range events {
		if e.ParentID < 0 || isBranchEvent(e) {
			continue
		}
		parent, ok := eventByID[e.ParentID]
		if !ok || !bodyOwnerIDs[parent.ID] {
			continue
		}
		bodyByParent[e.ParentID] = append(bodyByParent[e.ParentID], e.ID)
	}
	// Keep body-capable parents with no currently attached statements as well.
	// This occurs for an empty closure/function body and is represented by an
	// empty canonical Scope rather than by an unsupported text marker.
	for _, e := range events {
		if bodyOwnerIDs[e.ID] {
			if _, exists := bodyByParent[e.ID]; !exists {
				bodyByParent[e.ID] = nil
			}
		}
	}
	parents := make([]int, 0, len(bodyByParent))
	for parent := range bodyByParent {
		parents = append(parents, parent)
	}
	sort.Ints(parents)
	nextNodeID := len(events) + 1
	for _, parentID := range parents {
		childrenIDs := bodyByParent[parentID]
		parentEvent, parentExists := eventByID[parentID]
		if !parentExists {
			continue
		}
		hasExplicitBody := false
		for _, role := range parentEvent.Roles {
			if universalRelationRoleAt(parentEvent.StructureKind, role.Role, role.Ordinal) == "body" {
				hasExplicitBody = true
				break
			}
		}
		if hasExplicitBody && len(childrenIDs) == 0 {
			continue
		}
		seenChild := map[int]bool{}
		ordered := make([]int, 0, len(childrenIDs))
		for _, childID := range childrenIDs {
			if !seenChild[childID] {
				seenChild[childID] = true
				ordered = append(ordered, childID)
			}
		}
		scopeID := nextNodeID
		if isBranchEvent(parentEvent) && len(parentEvent.Roles) == 0 {
			// Plain else headers already own their Scope node. Reuse that stable
			// event ID rather than creating a detached duplicate.
			scopeID = parentID + 1
		} else {
			nextNodeID++
			if err := emit(scopeID, "Scope", "block", nil); err != nil {
				return err
			}
		}
		if !(isBranchEvent(parentEvent) && len(parentEvent.Roles) == 0) {
			attrs := map[string]json.RawMessage{}
			fromID, roleName := parentID+1, "body"
			if strings.EqualFold(parentEvent.StructureKind, "if") {
				if isBranchEvent(parentEvent) && parentEvent.ParentID >= 0 {
					if len(parentEvent.Roles) > 0 {
						// An else-if is itself an IfStmt. Its body is the
						// structured `then` child of that nested branch; the
						// branch relation to the outer IfStmt is added above.
						fromID, roleName = parentID+1, "then"
					} else {
						fromID, roleName = parentEvent.ParentID+1, "else"
					}
				} else {
					roleName = "then"
				}
			}
			role, _ := json.Marshal(roleName)
			attrs["role"] = role
			builder.AddRelation(FrontendRelationFact{Kind: "syntax.child", From: fromID, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(scopeID)}, Role: roleName, Ordinal: 0, Attributes: attrs})
		}
		for ordinal, childID := range ordered {
			attrs := map[string]json.RawMessage{}
			role, _ := json.Marshal("statement")
			ord, _ := json.Marshal(ordinal)
			attrs["role"], attrs["ordinal"] = role, ord
			builder.AddRelation(FrontendRelationFact{Kind: "syntax.child", From: scopeID, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(childID + 1)}, Role: "statement", Ordinal: ordinal, Attributes: attrs})
			children[childID] = true
		}
	}
	// MatrixIR grammar ownership can contain non-executable carrier edges (for
	// example an else marker temporarily associated with a function production).
	// After the canonical roles and body scopes have been materialized, attach
	// any still-unowned node to the document scope. This is a structural safety
	// net: it never invents an operand or source semantic, and it prevents an
	// accepted source from producing a detached/multiple-root UAST.
	owned := map[int]bool{}
	for _, relation := range builder.Facts.Relations {
		if relation.Kind != "syntax.child" || relation.To.Domain != "node" {
			continue
		}
		if id, err := strconv.Atoi(relation.To.ID); err == nil {
			owned[id] = true
		}
	}
	for _, node := range builder.Facts.Nodes {
		if node.ID == 0 || owned[node.ID] {
			continue
		}
		attrs := map[string]json.RawMessage{}
		role, _ := json.Marshal("statement")
		ordinal, _ := json.Marshal(node.ID)
		attrs["role"], attrs["ordinal"] = role, ordinal
		builder.AddRelation(FrontendRelationFact{Kind: "syntax.child", From: 0, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(node.ID)}, Role: "statement", Ordinal: node.ID, Attributes: attrs})
		owned[node.ID] = true
	}
	builder.Facts.SchemaVersion = header.SchemaVersion
	builder.Facts.BasisSHA256 = header.BasisSHA256
	builder.Facts.LanguageProfile = header.LanguageProfile
	builder.Facts.LanguageFacet = append([]float64(nil), header.LanguageFacet...)
	// The transport is structured facts; use the established canonical facts
	// projection rather than creating a family-specific representation.
	// Keep the transient facts projection explicit.  The document adapter
	// accepts this canonical UAST as a read-only compatibility view without
	// requiring a synthetic semantic-document digest.
	builder.Facts.Projection = "frontend_facts.v1"
	builder.Facts.Evaluation = "eager_left_to_right"
	builder.Facts.ValueModel = "tagged_dynamic_binary64"
	builder.Facts.IndexBase = 1
	builder.Facts.Types = defaultSemanticTypeContract()
	builder.Facts.Origin = SemanticOrigin{SourceLanguage: language, EntryPoint: "main"}
	builder.Facts.Metadata = map[string]string{"frontend": "matrix-structured-facts-v1"}
	return nil
}

// LowerMatrixEventsWithFactSink is an explicit compatibility bridge for
// callers that still provide event text. It is never called by LowerSource or
// the product TranspileCore path. New frontends must use
// materializeStructuredMatrixFacts through LowerMatrixLanguage.
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

// LowerMatrixActions is an explicit compatibility bridge. MatrixIR has
// already recognized the source grammar and selected normalized actions; this
// function consumes those actions rather than CanonicalProgram.R. The action
// payload is deliberately kept local to the frontend and never becomes a
// transport representation of Program.
//
// It is retained for old action-stream callers and is not a productive source
// entry point. Source-specific modern frontends must produce structured facts
// and call LowerMatrixLanguage instead.
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
	return ParseSemanticCompatibility(source, strings.Join(parts, "\n"))
}

// LowerMatrixEvents is an explicit compatibility adapter for legacy callers;
// the productive source path consumes CanonicalSemanticEvents directly in
// LowerMatrixLanguage and never reparses Event.Text.
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
	return ParseSemanticCompatibility(source, strings.Join(parts, "\n"))
}
