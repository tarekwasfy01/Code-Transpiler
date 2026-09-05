package backend

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
)

type universalOperationRecord struct {
	Semantics         SemanticSemantics       `json:"semantics,omitempty"`
	Typed             *SemanticOperation      `json:"typed,omitempty"`
	Operator          string                  `json:"operator,omitempty"`
	LiteralKind       string                  `json:"literal_kind,omitempty"`
	Text              string                  `json:"text,omitempty"`
	AssignOp          string                  `json:"assign_op,omitempty"`
	DoubleIndex       bool                    `json:"double_index,omitempty"`
	FunctionBinding   string                  `json:"function_binding,omitempty"`
	DefaultEvaluation string                  `json:"default_evaluation,omitempty"`
	ParameterMode     string                  `json:"parameter_mode,omitempty"`
	ParameterPassing  string                  `json:"parameter_passing,omitempty"`
	CallResolution    *SemanticCallResolution `json:"call_resolution,omitempty"`
}

type universalChildRecord struct {
	Role    string `json:"role"`
	Ordinal int    `json:"ordinal,omitempty"`
	Name    string `json:"name,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

type universalReferenceField struct {
	Role      string                `json:"role,omitempty"`
	Reference UniversalASTReference `json:"reference"`
	Name      string                `json:"name,omitempty"`
	Missing   bool                  `json:"missing,omitempty"`
}

type universalValueField struct {
	LiteralKind string                 `json:"literal_kind,omitempty"`
	Text        string                 `json:"text,omitempty"`
	Reference   *UniversalASTReference `json:"reference,omitempty"`
}

type universalOwnershipField struct {
	ParameterPassing string `json:"parameter_passing,omitempty"`
	TypeOwnership    string `json:"type_ownership,omitempty"`
}

type universalEvaluationField struct {
	OperationEvaluation string `json:"operation_evaluation,omitempty"`
	DefaultEvaluation   string `json:"default_evaluation,omitempty"`
}

type universalDispatchField struct {
	OperationDispatch string `json:"operation_dispatch,omitempty"`
	FunctionBinding   string `json:"function_binding,omitempty"`
}

func defaultUniversalFacets(kind string) []string {
	row := indexOf(uastEmbedded.Basis.StructuralKinds, kind)
	if row < 0 {
		return nil
	}
	out := []string{}
	for col, name := range uastEmbedded.Basis.Facets {
		if uastEmbedded.Basis.StructuralFacetSeed.At(row, col) != 0 {
			out = append(out, name)
		}
	}
	return out
}

func semanticStatementStructuralKind(kind string) string {
	switch kind {
	case "assign":
		return "AssignStmt"
	case "if":
		return "IfStmt"
	case "while", "repeat":
		return "LoopStmt"
	case "for":
		return "ForEachStmt"
	case "return":
		return "ReturnStmt"
	case "break":
		return "BreakStmt"
	case "continue":
		return "ContinueStmt"
	case "block":
		return "Scope"
	default:
		return "OperationExpr"
	}
}
func semanticExpressionStructuralKind(e *SemanticExpression) string {
	switch e.Kind {
	case "identifier":
		return "SymbolRef"
	case "literal":
		if e.LiteralKind == "null" || e.LiteralKind == "na" {
			return "NilLiteral"
		}
		return "LiteralExpr"
	case "call":
		return "CallExpr"
	case "index":
		return "IndexExpr"
	case "function":
		return "ClosureExpr"
	case "typed_operation", "binary", "unary", "iteration":
		return "OperationExpr"
	default:
		return "AggregateExpr"
	}
}

// ProjectSemanticDocumentToUniversal is the compatibility crosswalk. Facets
// come only from supplied structural seed rows; no unseeded facet is guessed.
func ProjectSemanticDocumentToUniversal(doc SemanticDocument) (*UniversalASTDocument, error) {
	// The compatibility document is an import value.  Once projected, none of
	// its maps, slices or source-position pointers may remain shared with the
	// canonical UAST; otherwise mutating the legacy view would mutate the
	// canonical representation through aliasing.
	var err error
	doc, err = cloneSemanticDocumentValue(doc, false)
	if err != nil {
		return nil, err
	}
	u, err := NewUniversalASTDocument(doc.Origin.SourceLanguage)
	if err != nil {
		return nil, err
	}
	u.Projection, u.Evaluation, u.ValueModel, u.IndexBase, u.Types, u.Origin = "semantic_document.v1", doc.Evaluation, doc.ValueModel, doc.IndexBase, doc.Types, doc.Origin
	u.Metadata, u.Extensions, u.Contracts, u.Dialects, u.SemanticFeatures = doc.Metadata, doc.Extensions, doc.Contracts, doc.Dialects, doc.SemanticFeatures
	u.TypeTable, u.TypeGraph, u.TypeRelations, u.Evidence = doc.TypeTable, doc.TypeGraph, doc.TypeRelations, doc.Evidence
	semanticIDs := map[int]int{}
	var statement func(*SemanticStatement) (int, error)
	var expression func(*SemanticExpression) (int, error)
	add := func(structural, semanticKind string, semanticID, scope int, typ SemanticType, typeOrigin string, semantics SemanticSemantics, effects []string, binding *int, source *SemanticSourceSpan, attributes, extensions map[string]any, name string, operation universalOperationRecord) (int, error) {
		id, err := u.AddNode(structural, defaultUniversalFacets(structural), nil)
		if err != nil {
			return 0, err
		}
		n := &u.Nodes[id]
		n.Source = source
		n.Attributes = rawMessageMap(attributes)
		put := func(field string, value any) {
			if containsString(n.FieldMask, field) {
				if data, e := json.Marshal(value); e == nil && string(data) != "null" {
					if n.Fields == nil {
						n.Fields = map[string]json.RawMessage{}
					}
					n.Fields[field] = data
				}
			}
		}
		put("id", semanticID)
		put("kind", semanticKind)
		put("scope_id", scope)
		if typ.Kind != "" {
			put("type_ref", typ)
		}
		if typeOrigin != "" {
			put("type_origin", typeOrigin)
		}
		if !reflect.DeepEqual(operation, universalOperationRecord{}) || !reflect.DeepEqual(semantics, SemanticSemantics{}) {
			operation.Semantics = semantics
			put("operation", operation)
		}
		if len(effects) > 0 {
			put("effects", effects)
		}
		if binding != nil {
			put("binding_refs", []int{*binding})
		}
		if name != "" {
			put("name", name)
		}
		if len(attributes) > 0 {
			put("attributes", attributes)
		}
		if len(extensions) > 0 {
			put("extensions", extensions)
		}
		if semanticID >= 0 {
			semanticIDs[semanticID] = id
		}
		return id, nil
	}
	link := func(parent, child int, meta universalChildRecord) {
		attrs := map[string]json.RawMessage{}
		for key, value := range map[string]any{"role": meta.Role, "ordinal": meta.Ordinal, "name": meta.Name, "missing": meta.Missing} {
			data, _ := json.Marshal(value)
			attrs[key] = data
		}
		u.Relations = append(u.Relations, UniversalASTRelation{Kind: "syntax.child", From: parent, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(child)}, Attributes: attrs})
	}
	expression = func(e *SemanticExpression) (int, error) {
		if e == nil {
			return -1, nil
		}
		op := universalOperationRecord{Typed: e.Operation, Operator: e.Operator, LiteralKind: e.LiteralKind, Text: e.Text, DoubleIndex: e.DoubleIndex, CallResolution: e.Resolution}
		if e.Function != nil {
			op.FunctionBinding = e.Function.Binding
			op.DefaultEvaluation = e.Function.DefaultEvaluation
		}
		id, err := add(semanticExpressionStructuralKind(e), e.Kind, e.ID, e.Scope, e.Type, e.TypeOrigin, e.Semantics, e.Effects, e.Binding, e.Source, e.Attributes, e.Extensions, e.Name, op)
		if err != nil {
			return 0, err
		}
		childExpr := func(role string, ordinal int, c *SemanticExpression, argument SemanticArgument) error {
			if c == nil {
				return nil
			}
			q, er := expression(c)
			if er == nil {
				link(id, q, universalChildRecord{Role: role, Ordinal: ordinal, Name: argument.Name, Missing: argument.Missing})
			}
			return er
		}
		if err = childExpr("left", 0, e.Left, SemanticArgument{}); err != nil {
			return 0, err
		}
		if err = childExpr("right", 0, e.Right, SemanticArgument{}); err != nil {
			return 0, err
		}
		if err = childExpr("value", 0, e.Value, SemanticArgument{}); err != nil {
			return 0, err
		}
		for i, a := range e.Arguments {
			if a.Value != nil {
				if err = childExpr("argument", i, a.Value, a); err != nil {
					return 0, err
				}
			} else if a.Missing {
				mid, er := add("LiteralExpr", "missing_argument", -1, e.Scope, SemanticType{}, "", SemanticSemantics{}, nil, nil, nil, nil, nil, "", universalOperationRecord{LiteralKind: "missing"})
				if er != nil {
					return 0, er
				}
				link(id, mid, universalChildRecord{Role: "argument", Ordinal: i, Name: a.Name, Missing: true})
			}
		}
		if e.Function != nil {
			for i, p := range e.Function.Parameters {
				pid, er := add("ParameterDecl", "parameter", p.ID, e.Scope, p.Type, p.Type.TypeOrigin, SemanticSemantics{}, nil, nil, nil, nil, nil, p.Name, universalOperationRecord{ParameterMode: p.Mode, ParameterPassing: p.Passing})
				if er != nil {
					return 0, er
				}
				if p.Default != nil {
					did, er := expression(p.Default)
					if er != nil {
						return 0, er
					}
					link(pid, did, universalChildRecord{Role: "default"})
				}
				link(id, pid, universalChildRecord{Role: "parameter", Ordinal: i})
			}
			bid, er := statement(&e.Function.Body)
			if er != nil {
				return 0, er
			}
			link(id, bid, universalChildRecord{Role: "body"})
		}
		return id, nil
	}
	statement = func(s *SemanticStatement) (int, error) {
		if s == nil {
			return -1, nil
		}
		id, err := add(semanticStatementStructuralKind(s.Kind), s.Kind, s.ID, s.Scope, s.Type, s.TypeOrigin, s.Semantics, s.Effects, nil, s.Source, s.Attributes, s.Extensions, s.Name, universalOperationRecord{AssignOp: s.AssignOp})
		if err != nil {
			return 0, err
		}
		addExpr := func(role string, c *SemanticExpression) error {
			if c == nil {
				return nil
			}
			q, e := expression(c)
			if e == nil {
				link(id, q, universalChildRecord{Role: role})
			}
			return e
		}
		addStmt := func(role string, ordinal int, c *SemanticStatement) error {
			if c == nil {
				return nil
			}
			q, e := statement(c)
			if e == nil {
				link(id, q, universalChildRecord{Role: role, Ordinal: ordinal})
			}
			return e
		}
		if err = addExpr("expression", s.Expression); err != nil {
			return 0, err
		}
		if err = addExpr("condition", s.Condition); err != nil {
			return 0, err
		}
		if err = addExpr("sequence", s.Sequence); err != nil {
			return 0, err
		}
		if err = addStmt("then", 0, s.Then); err != nil {
			return 0, err
		}
		if err = addStmt("else", 0, s.Else); err != nil {
			return 0, err
		}
		if err = addStmt("body", 0, s.Body); err != nil {
			return 0, err
		}
		for i := range s.Statements {
			if err = addStmt("statement", i, &s.Statements[i]); err != nil {
				return 0, err
			}
		}
		return id, nil
	}
	if _, err = statement(&doc.Root); err != nil {
		return nil, err
	}
	if err = materializeUniversalCrosswalkFields(u); err != nil {
		return nil, err
	}
	appendUniversalEvidenceRelations(u, semanticIDs, doc.Evidence)
	u.SemanticDocumentSHA256 = semanticDocumentDigest(doc)
	if err = validateUniversalASTDocument(u); err != nil {
		return nil, err
	}
	return u, nil
}

// materializeUniversalCrosswalkFields evaluates the checked-in field
// crosswalk over the syntax relation graph.  It adds only mappings explicitly
// present in the crosswalk (children, operation facets, type ownership and
// lifetime); no field is inferred from a spelling heuristic.
func materializeUniversalCrosswalkFields(u *UniversalASTDocument) error {
	children, err := universalChildrenByRole(u)
	if err != nil {
		return err
	}
	ref := func(id int) UniversalASTReference { return UniversalASTReference{Domain: "node", ID: strconv.Itoa(id)} }
	put := func(n *UniversalASTNode, field string, value any) error {
		if !containsString(n.FieldMask, field) {
			return nil
		}
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if string(data) == "null" {
			return nil
		}
		if n.Fields == nil {
			n.Fields = map[string]json.RawMessage{}
		}
		n.Fields[field] = data
		return nil
	}
	for i := range u.Nodes {
		n := &u.Nodes[i]
		c, err := decodeUniversalCommon(n)
		if err != nil {
			return fmt.Errorf("crosswalk node %d: %w", n.ID, err)
		}
		roles := children[n.ID]
		operandRoles := []string{}
		switch c.Kind {
		case "assign", "expression":
			operandRoles = []string{"expression"}
		case "for":
			operandRoles = []string{"sequence"}
		case "binary":
			operandRoles = []string{"left", "right"}
		}
		var operands []universalReferenceField
		for _, role := range operandRoles {
			for _, item := range roles[role] {
				operands = append(operands, universalReferenceField{Role: role, Reference: ref(item.ID)})
			}
		}
		if len(operands) > 0 {
			if err := put(n, "operands", operands); err != nil {
				return err
			}
		}
		if items := roles["condition"]; len(items) == 1 {
			if err := put(n, "condition", ref(items[0].ID)); err != nil {
				return err
			}
		}
		var branches []universalReferenceField
		for _, role := range []string{"then", "else"} {
			for _, item := range roles[role] {
				branches = append(branches, universalReferenceField{Role: role, Reference: ref(item.ID)})
			}
		}
		if len(branches) > 0 {
			if err := put(n, "branches", branches); err != nil {
				return err
			}
		}
		if items := roles["body"]; len(items) == 1 {
			if err := put(n, "body", ref(items[0].ID)); err != nil {
				return err
			}
		}
		if items := roles["statement"]; len(items) > 0 {
			members := make([]UniversalASTReference, len(items))
			for j, item := range items {
				members[j] = ref(item.ID)
			}
			if err := put(n, "members", members); err != nil {
				return err
			}
		}
		if items := roles["argument"]; len(items) > 0 {
			arguments := make([]universalReferenceField, len(items))
			for j, item := range items {
				arguments[j] = universalReferenceField{Role: "argument", Reference: ref(item.ID), Name: item.Meta.Name, Missing: item.Meta.Missing}
			}
			if err := put(n, "arguments", arguments); err != nil {
				return err
			}
		}
		if c.Kind == "call" {
			if items := roles["value"]; len(items) == 1 {
				if err := put(n, "callee", ref(items[0].ID)); err != nil {
					return err
				}
			}
		}
		if items := roles["parameter"]; len(items) > 0 {
			parameters := make([]UniversalASTReference, len(items))
			for j, item := range items {
				parameters[j] = ref(item.ID)
			}
			if err := put(n, "parameters", parameters); err != nil {
				return err
			}
		}
		value := universalValueField{LiteralKind: c.Operation.LiteralKind, Text: c.Operation.Text}
		if items := roles["value"]; len(items) == 1 {
			q := ref(items[0].ID)
			value.Reference = &q
		}
		if items := roles["default"]; len(items) == 1 {
			q := ref(items[0].ID)
			value.Reference = &q
		}
		if value.LiteralKind != "" || value.Text != "" || value.Reference != nil {
			if err := put(n, "value", value); err != nil {
				return err
			}
		}
		ownership := universalOwnershipField{ParameterPassing: c.Operation.ParameterPassing, TypeOwnership: c.Type.Ownership}
		if ownership.ParameterPassing != "" || ownership.TypeOwnership != "" {
			if err := put(n, "ownership", ownership); err != nil {
				return err
			}
		}
		if c.Type.Lifetime != "" {
			if err := put(n, "lifetime", c.Type.Lifetime); err != nil {
				return err
			}
		}
		evaluation := universalEvaluationField{OperationEvaluation: c.Semantics.EvaluationOrder, DefaultEvaluation: c.Operation.DefaultEvaluation}
		if evaluation.OperationEvaluation != "" || evaluation.DefaultEvaluation != "" {
			if err := put(n, "evaluation_order", evaluation); err != nil {
				return err
			}
		}
		dispatch := universalDispatchField{OperationDispatch: c.Semantics.Dispatch, FunctionBinding: c.Operation.FunctionBinding}
		if dispatch.OperationDispatch != "" || dispatch.FunctionBinding != "" {
			if err := put(n, "dispatch", dispatch); err != nil {
				return err
			}
		}
		if c.Semantics.ErrorModel != "" {
			if err := put(n, "exception_model", c.Semantics.ErrorModel); err != nil {
				return err
			}
		}
		if c.Operation.CallResolution != nil {
			if err := put(n, "candidates", c.Operation.CallResolution); err != nil {
				return err
			}
		}
	}
	return nil
}

func universalRelationAllowed(n *UniversalASTNode, kind string) bool {
	relation := indexOf(uastEmbedded.Basis.ConcreteRelations, kind)
	if relation < 0 {
		return false
	}
	if indexOf(uastEmbedded.Basis.GlobalRelations, kind) >= 0 {
		return true
	}
	row := indexOf(uastEmbedded.Basis.StructuralKinds, n.StructuralKind)
	if uastEmbedded.Basis.StructuralConcreteRelation.At(row, relation) != 0 {
		return true
	}
	for _, f := range n.SemanticFacets {
		if uastEmbedded.Basis.FacetConcreteRelation.At(indexOf(uastEmbedded.Basis.Facets, f), relation) != 0 {
			return true
		}
	}
	return false
}
func appendUniversalEvidenceRelations(u *UniversalASTDocument, ids map[int]int, e SemanticEvidence) {
	// UAST node identifiers are stable document identifiers, not slice offsets.
	// Projection can preserve sparse IDs, so relation authorization must resolve
	// through this index instead of addressing u.Nodes with an ID directly.
	nodesByID := make(map[int]*UniversalASTNode, len(u.Nodes))
	for i := range u.Nodes {
		nodesByID[u.Nodes[i].ID] = &u.Nodes[i]
	}
	seen := map[string]bool{}
	for _, relation := range u.Relations {
		seen[relation.Kind+"\x00"+strconv.Itoa(relation.From)+"\x00"+relation.To.Domain+"\x00"+relation.To.ID] = true
	}
	addRef := func(kind string, from int, to UniversalASTReference, attributes map[string]json.RawMessage) {
		node := nodesByID[from]
		if node == nil || !universalRelationAllowed(node, kind) {
			return
		}
		key := kind + "\x00" + strconv.Itoa(from) + "\x00" + to.Domain + "\x00" + to.ID
		if seen[key] {
			return
		}
		seen[key] = true
		u.Relations = append(u.Relations, UniversalASTRelation{Kind: kind, From: from, To: to, Attributes: attributes})
	}
	add := func(kind string, from, to int) {
		a, ok := ids[from]
		b, ok2 := ids[to]
		if ok && ok2 {
			addRef(kind, a, UniversalASTReference{Domain: "node", ID: strconv.Itoa(b)}, nil)
		}
	}
	e.Control.Each(func(r, c int, _ float64) { add("control.next", r, c) })
	e.Data.Each(func(r, c int, _ float64) { add("data.def_use", r, c) })
	e.Order.Each(func(r, c int, _ float64) { add("evaluation.before", r, c) })
	e.Binding.Each(func(r, c int, _ float64) {
		if a, ok := ids[r]; ok {
			bindingID := c
			if c >= 0 && c < len(e.Bindings) {
				bindingID = e.Bindings[c].ID
			}
			to := UniversalASTReference{Domain: "binding", ID: strconv.Itoa(bindingID)}
			addRef("binding.refers", a, to, nil)
			addRef("name.resolves", a, to, nil)
		}
	})
	for _, binding := range e.Bindings {
		if definition, ok := ids[binding.Definition]; ok {
			addRef("binding.declares", definition, UniversalASTReference{Domain: "binding", ID: strconv.Itoa(binding.ID)}, nil)
		}
	}
	e.Effects.Each(func(r, c int, _ float64) {
		if from, ok := ids[r]; ok && c >= 0 && c < len(e.EffectAxes) {
			addRef("effect.has", from, UniversalASTReference{Domain: "effect", ID: e.EffectAxes[c]}, nil)
		}
	})
	e.Types.Each(func(r, c int, _ float64) {
		if from, ok := ids[r]; ok && c >= 0 && c < len(e.TypeAxes) {
			addRef("type.has", from, UniversalASTReference{Domain: "type_axis", ID: e.TypeAxes[c]}, nil)
		}
	})
	children, _ := universalChildrenByRole(u)
	for i := range u.Nodes {
		n := &u.Nodes[i]
		c, err := decodeUniversalCommon(n)
		if err != nil {
			continue
		}
		operation := c.Semantics.Operation
		if c.Operation.Typed != nil {
			operation = c.Operation.Typed.Name
		}
		if operation != "" {
			addRef("operation.kind", n.ID, UniversalASTReference{Domain: "operation", ID: operation}, nil)
		}
		origin := c.TypeOrigin
		if origin == "" {
			origin = c.Type.TypeOrigin
		}
		if origin != "" {
			addRef("type.origin", n.ID, UniversalASTReference{Domain: "type_origin", ID: origin}, nil)
		}
		var operands []universalReferenceField
		if decodeUniversalField(n, "operands", &operands) == nil {
			for _, operand := range operands {
				addRef("data.operand", n.ID, operand.Reference, nil)
			}
		}
		if c.Kind == "if" {
			for _, branch := range children[n.ID]["then"] {
				addRef("control.true", n.ID, UniversalASTReference{Domain: "node", ID: strconv.Itoa(branch.ID)}, nil)
			}
			for _, branch := range children[n.ID]["else"] {
				addRef("control.false", n.ID, UniversalASTReference{Domain: "node", ID: strconv.Itoa(branch.ID)}, nil)
			}
		}
		if c.Kind == "call" {
			var callee UniversalASTReference
			if decodeUniversalField(n, "callee", &callee) == nil && callee.Domain != "" {
				addRef("call.calls", n.ID, callee, nil)
			}
		}
		if c.Kind == "block" && c.Scope >= 0 && c.Scope < len(e.Scopes) && e.Scopes[c.Scope].Parent >= 0 {
			addRef("scope.parent", n.ID, UniversalASTReference{Domain: "scope", ID: strconv.Itoa(e.Scopes[c.Scope].Parent)}, nil)
		}
	}
}
func rawMessageMap(values map[string]any) map[string]json.RawMessage {
	if len(values) == 0 {
		return nil
	}
	out := map[string]json.RawMessage{}
	for k, v := range values {
		if data, err := json.Marshal(v); err == nil {
			out[k] = data
		}
	}
	return out
}
func containsString(values []string, want string) bool { return indexOf(values, want) >= 0 }

type universalChild struct {
	ID   int
	Meta universalChildRecord
}

func universalChildrenByRole(u *UniversalASTDocument) (map[int]map[string][]universalChild, error) {
	out := map[int]map[string][]universalChild{}
	for _, r := range u.Relations {
		if r.Kind != "syntax.child" {
			continue
		}
		if r.To.Domain != "node" {
			return nil, fmt.Errorf("syntax child target is not a node")
		}
		id, err := strconv.Atoi(r.To.ID)
		if err != nil {
			return nil, err
		}
		meta := universalChildRecord{}
		if err = json.Unmarshal(r.Attributes["role"], &meta.Role); err != nil {
			return nil, err
		}
		json.Unmarshal(r.Attributes["ordinal"], &meta.Ordinal)
		json.Unmarshal(r.Attributes["name"], &meta.Name)
		json.Unmarshal(r.Attributes["missing"], &meta.Missing)
		if out[r.From] == nil {
			out[r.From] = map[string][]universalChild{}
		}
		out[r.From][meta.Role] = append(out[r.From][meta.Role], universalChild{id, meta})
	}
	for _, roles := range out {
		for role := range roles {
			sort.Slice(roles[role], func(i, j int) bool { return roles[role][i].Meta.Ordinal < roles[role][j].Meta.Ordinal })
		}
	}
	return out, nil
}

func decodeUniversalField[T any](n *UniversalASTNode, name string, out *T) error {
	data := n.Fields[name]
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}

type universalDecodedCommon struct {
	Kind                   string
	ID, Scope              int
	Type                   SemanticType
	TypeOrigin             string
	Semantics              SemanticSemantics
	Effects                []string
	Binding                *int
	Name                   string
	Attributes, Extensions map[string]any
	Operation              universalOperationRecord
}

func decodeUniversalCommon(n *UniversalASTNode) (universalDecodedCommon, error) {
	c := universalDecodedCommon{}
	if err := decodeUniversalField(n, "kind", &c.Kind); err != nil {
		return c, err
	}
	if c.Kind == "" {
		return c, fmt.Errorf("universal node %d lacks semantic kind crosswalk", n.ID)
	}
	decodeUniversalField(n, "id", &c.ID)
	decodeUniversalField(n, "scope_id", &c.Scope)
	decodeUniversalField(n, "type_ref", &c.Type)
	decodeUniversalField(n, "type_origin", &c.TypeOrigin)
	decodeUniversalField(n, "effects", &c.Effects)
	decodeUniversalField(n, "name", &c.Name)
	decodeUniversalField(n, "operation", &c.Operation)
	c.Semantics = c.Operation.Semantics
	var refs []int
	if err := decodeUniversalField(n, "binding_refs", &refs); err != nil {
		return c, err
	}
	if len(refs) > 1 {
		return c, fmt.Errorf("node %d has multiple executable binding references", n.ID)
	}
	if len(refs) == 1 {
		c.Binding = &refs[0]
	}
	if data := n.Fields["attributes"]; len(data) > 0 {
		if err := json.Unmarshal(data, &c.Attributes); err != nil {
			return c, err
		}
	}
	if data := n.Fields["extensions"]; len(data) > 0 {
		if err := json.Unmarshal(data, &c.Extensions); err != nil {
			return c, err
		}
	}
	return c, nil
}

// SemanticDocumentFromUniversalAST is lossless for the currently executable
// SemanticDocument projection. Pure UAST nodes outside that projection remain
// representable but are rejected instead of being approximated.
func SemanticDocumentFromUniversalAST(u *UniversalASTDocument) (SemanticDocument, error) {
	if err := validateUniversalASTDocument(u); err != nil {
		return SemanticDocument{}, err
	}
	if u.Projection != "semantic_document.v1" {
		return SemanticDocument{}, fmt.Errorf("universal AST has no lossless SemanticDocument projection")
	}
	if len(u.Nodes) == 0 {
		return SemanticDocument{}, fmt.Errorf("universal AST projection has no root")
	}
	nodes := map[int]*UniversalASTNode{}
	for i := range u.Nodes {
		nodes[u.Nodes[i].ID] = &u.Nodes[i]
	}
	children, err := universalChildrenByRole(u)
	if err != nil {
		return SemanticDocument{}, err
	}
	visiting := map[int]bool{}
	var statement func(int) (SemanticStatement, error)
	var expression func(int) (*SemanticExpression, error)
	var parameter func(int) (SemanticParameter, error)
	one := func(id int, role string) (int, bool, error) {
		items := children[id][role]
		if len(items) == 0 {
			return 0, false, nil
		}
		if len(items) != 1 {
			return 0, false, fmt.Errorf("node %d role %q is not singular", id, role)
		}
		return items[0].ID, true, nil
	}
	expression = func(id int) (*SemanticExpression, error) {
		n := nodes[id]
		if n == nil {
			return nil, fmt.Errorf("universal expression node %d missing", id)
		}
		if visiting[id] {
			return nil, fmt.Errorf("universal syntax cycle at node %d", id)
		}
		visiting[id] = true
		defer delete(visiting, id)
		c, e := decodeUniversalCommon(n)
		if e != nil {
			return nil, e
		}
		if c.Kind == "missing_argument" {
			return nil, nil
		}
		x := &SemanticExpression{ID: c.ID, Kind: c.Kind, Scope: c.Scope, Type: c.Type, TypeOrigin: c.TypeOrigin, Semantics: c.Semantics, Effects: c.Effects, Binding: c.Binding, Source: n.Source, Attributes: c.Attributes, Extensions: c.Extensions, Name: c.Name, Operation: c.Operation.Typed, Operator: c.Operation.Operator, LiteralKind: c.Operation.LiteralKind, Text: c.Operation.Text, DoubleIndex: c.Operation.DoubleIndex, Resolution: c.Operation.CallResolution}
		get := func(role string) (*SemanticExpression, error) {
			q, ok, er := one(id, role)
			if er != nil || !ok {
				return nil, er
			}
			return expression(q)
		}
		if x.Left, e = get("left"); e != nil {
			return nil, e
		}
		if x.Right, e = get("right"); e != nil {
			return nil, e
		}
		if x.Value, e = get("value"); e != nil {
			return nil, e
		}
		for _, item := range children[id]["argument"] {
			if item.Meta.Missing {
				x.Arguments = append(x.Arguments, SemanticArgument{Name: item.Meta.Name, Missing: true})
				continue
			}
			value, er := expression(item.ID)
			if er != nil {
				return nil, er
			}
			x.Arguments = append(x.Arguments, SemanticArgument{Name: item.Meta.Name, Value: value})
		}
		if c.Kind == "function" {
			fn := &SemanticFunction{Binding: c.Operation.FunctionBinding, DefaultEvaluation: c.Operation.DefaultEvaluation}
			for _, item := range children[id]["parameter"] {
				p, er := parameter(item.ID)
				if er != nil {
					return nil, er
				}
				fn.Parameters = append(fn.Parameters, p)
			}
			bodyID, ok, er := one(id, "body")
			if er != nil || !ok {
				if er == nil {
					er = fmt.Errorf("function node %d lacks body", id)
				}
				return nil, er
			}
			body, er := statement(bodyID)
			if er != nil {
				return nil, er
			}
			fn.Body = body
			x.Function = fn
		}
		return x, nil
	}
	parameter = func(id int) (SemanticParameter, error) {
		n := nodes[id]
		if n == nil {
			return SemanticParameter{}, fmt.Errorf("parameter node %d missing", id)
		}
		c, e := decodeUniversalCommon(n)
		if e != nil {
			return SemanticParameter{}, e
		}
		if c.Kind != "parameter" {
			return SemanticParameter{}, fmt.Errorf("node %d is not a parameter", id)
		}
		p := SemanticParameter{ID: c.ID, Name: c.Name, Type: c.Type, Passing: c.Operation.ParameterPassing, Mode: c.Operation.ParameterMode}
		if q, ok, er := one(id, "default"); er != nil {
			return p, er
		} else if ok {
			p.Default, er = expression(q)
			if er != nil {
				return p, er
			}
		}
		return p, nil
	}
	statement = func(id int) (SemanticStatement, error) {
		n := nodes[id]
		if n == nil {
			return SemanticStatement{}, fmt.Errorf("universal statement node %d missing", id)
		}
		if visiting[id] {
			return SemanticStatement{}, fmt.Errorf("universal syntax cycle at node %d", id)
		}
		visiting[id] = true
		defer delete(visiting, id)
		c, e := decodeUniversalCommon(n)
		if e != nil {
			return SemanticStatement{}, e
		}
		s := SemanticStatement{ID: c.ID, Kind: c.Kind, Scope: c.Scope, Type: c.Type, TypeOrigin: c.TypeOrigin, Semantics: c.Semantics, Effects: c.Effects, Source: n.Source, Attributes: c.Attributes, Extensions: c.Extensions, Name: c.Name, AssignOp: c.Operation.AssignOp}
		getExpr := func(role string) (*SemanticExpression, error) {
			q, ok, er := one(id, role)
			if er != nil || !ok {
				return nil, er
			}
			return expression(q)
		}
		getStmt := func(role string) (*SemanticStatement, error) {
			q, ok, er := one(id, role)
			if er != nil || !ok {
				return nil, er
			}
			v, er := statement(q)
			return &v, er
		}
		// Canonical frontend facts use the structured `value` role for an
		// assignment.  The older SemanticDocument view called the same slot
		// `expression`; accept both spellings at this boundary so the runtime
		// compatibility executor can consume the canonical UAST without asking
		// the frontend to synthesize a second relation.
		expressionRole := "expression"
		if c.Kind == "assign" {
			if _, ok, roleErr := one(id, expressionRole); roleErr != nil {
				return s, roleErr
			} else if !ok {
				expressionRole = "value"
			}
		}
		if s.Expression, e = getExpr(expressionRole); e != nil {
			return s, e
		}
		if s.Condition, e = getExpr("condition"); e != nil {
			return s, e
		}
		if s.Sequence, e = getExpr("sequence"); e != nil {
			return s, e
		}
		if s.Then, e = getStmt("then"); e != nil {
			return s, e
		}
		if s.Else, e = getStmt("else"); e != nil {
			return s, e
		}
		if s.Body, e = getStmt("body"); e != nil {
			return s, e
		}
		for _, item := range children[id]["statement"] {
			v, er := statement(item.ID)
			if er != nil {
				return s, er
			}
			s.Statements = append(s.Statements, v)
		}
		return s, nil
	}
	root, err := statement(0)
	if err != nil {
		return SemanticDocument{}, err
	}
	doc := SemanticDocument{SchemaVersion: SemanticDocumentVersion, Schema: SemanticDocumentSchema, Evaluation: u.Evaluation, ValueModel: u.ValueModel, IndexBase: u.IndexBase, Types: u.Types, Origin: u.Origin, Metadata: u.Metadata, Extensions: u.Extensions, Contracts: u.Contracts, Dialects: u.Dialects, SemanticFeatures: u.SemanticFeatures, TypeTable: u.TypeTable, TypeGraph: u.TypeGraph, TypeRelations: u.TypeRelations, Root: root, Evidence: u.Evidence}
	// Return a detached compatibility view.  The UAST pointer itself is
	// intentionally shared and is the only canonical handle in the result.
	doc, err = cloneSemanticDocumentValue(doc, false)
	if err != nil {
		return SemanticDocument{}, err
	}
	doc.UniversalAST = u
	return doc, nil
}

// cloneSemanticDocumentValue preserves json.Number while detaching every
// reference-bearing legacy value.  includeUniversal is used only by tests and
// diagnostics; canonical projection normally clones without the UAST handle.
func cloneSemanticDocumentValue(doc SemanticDocument, includeUniversal bool) (SemanticDocument, error) {
	if !includeUniversal {
		doc.UniversalAST = nil
	}
	data, err := json.Marshal(semanticDocumentWire(doc))
	if err != nil {
		return SemanticDocument{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var wire semanticDocumentWire
	if err = decoder.Decode(&wire); err != nil {
		return SemanticDocument{}, err
	}
	return SemanticDocument(wire), nil
}

func validateUniversalASTCompatibility(doc SemanticDocument) error {
	if doc.UniversalAST == nil {
		return fmt.Errorf("semantic compatibility document has no universal AST")
	}
	back, err := SemanticDocumentFromUniversalAST(doc.UniversalAST)
	if err != nil {
		return err
	}
	left, right := doc, back
	left.UniversalAST, right.UniversalAST = nil, nil
	a, _ := json.Marshal(semanticDocumentWire(left))
	b, _ := json.Marshal(semanticDocumentWire(right))
	if !reflect.DeepEqual(a, b) {
		at := 0
		for at < len(a) && at < len(b) && a[at] == b[at] {
			at++
		}
		lo := at - 80
		if lo < 0 {
			lo = 0
		}
		ha := at + 160
		if ha > len(a) {
			ha = len(a)
		}
		hb := at + 160
		if hb > len(b) {
			hb = len(b)
		}
		return fmt.Errorf("universal AST compatibility projection differs from SemanticDocument at byte %d: %s != %s", at, a[lo:ha], b[lo:hb])
	}
	if doc.UniversalAST.SemanticDocumentSHA256 != semanticDocumentDigest(doc) {
		return fmt.Errorf("universal AST semantic document digest differs from compatibility document")
	}
	if err := validateExecutableUniversalProjection(doc.UniversalAST); err != nil {
		return err
	}
	return nil
}

// validateExecutableUniversalProjection proves that the temporary legacy
// backend adapter can preserve the complete UAST payload.  A valid but richer
// UAST is representable, but is rejected for execution until a backend lowers
// those extra facets, fields or relations directly.
func validateExecutableUniversalProjection(u *UniversalASTDocument) error {
	if u == nil || u.Projection != "semantic_document.v1" {
		return fmt.Errorf("universal AST payload has no executable compatibility projection")
	}
	doc, err := SemanticDocumentFromUniversalAST(u)
	if err != nil {
		return err
	}
	doc.UniversalAST = nil
	expected, err := ProjectSemanticDocumentToUniversal(doc)
	if err != nil {
		return err
	}
	// The lossless source surface is deliberately outside the temporary
	// executable SemanticDocument view. Preserve it on the comparison copy so
	// compatibility validation checks semantic fidelity rather than discarding
	// verified original bytes.
	expected.Surface = cloneUniversalASTSurface(u.Surface)
	actualJSON, err := json.Marshal(u)
	if err != nil {
		return err
	}
	expectedJSON, err := json.Marshal(expected)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actualJSON, expectedJSON) {
		return fmt.Errorf("universal AST contains semantics not preserved by the temporary legacy backend adapter")
	}
	return nil
}

func semanticDocumentDigest(doc SemanticDocument) string {
	doc.UniversalAST = nil
	data, _ := json.Marshal(semanticDocumentWire(doc))
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func reconcileUniversalAST(doc *SemanticDocument) error {
	if doc.UniversalAST == nil {
		u, err := ProjectSemanticDocumentToUniversal(*doc)
		if err != nil {
			return err
		}
		doc.UniversalAST = u
		return nil
	}
	if err := validateUniversalASTDocument(doc.UniversalAST); err != nil {
		return err
	}
	if doc.UniversalAST.Projection != "semantic_document.v1" {
		return fmt.Errorf("SemanticDocument cannot carry a non-compatibility universal AST; serialize the canonical UAST directly")
	}
	// Projection is deliberately one-way.  A document without UAST may be
	// imported once above; after that the legacy tree is only a derived view and
	// may never overwrite the canonical UAST.
	return validateUniversalASTCompatibility(*doc)
}

// legacyExecutableBodyFromUniversal is a read-only public compatibility view.
// Canonical facets are allowed to be richer than the historical statement
// shape because the view is never used as semantic input.
func legacyExecutableBodyFromUniversal(u *UniversalASTDocument) (*BlockStmt, error) {
	if err := validateUniversalASTDocument(u); err != nil {
		return nil, err
	}
	if err := validateUniversalExecutionContracts(u); err != nil {
		return nil, err
	}
	doc, err := SemanticDocumentFromUniversalAST(u)
	if err != nil {
		return nil, err
	}
	root, err := documentStatementAST(doc.Root)
	if err != nil {
		return nil, err
	}
	body, ok := root.(*BlockStmt)
	if !ok {
		return nil, fmt.Errorf("universal AST root is not an executable block")
	}
	return body, nil
}

// refreshLegacyExecutableBodyView updates only the public compatibility view.
// No executor or validator may read the result as semantic input.
func refreshLegacyExecutableBodyView(p *SemanticProgram, u *UniversalASTDocument) error {
	body, err := legacyExecutableBodyFromUniversal(u)
	if err != nil {
		return err
	}
	p.Body = body
	return nil
}

// semanticBodyFromUniversal remains for source compatibility inside the
// package.  New backend/runtime code should name the legacy boundary above.
func semanticBodyFromUniversal(u *UniversalASTDocument) (*BlockStmt, error) {
	return legacyExecutableBodyFromUniversal(u)
}
