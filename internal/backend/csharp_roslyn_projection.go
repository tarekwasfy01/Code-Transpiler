// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type roslynProjectedNode struct {
	id                     int
	start, end             int
	kind                   string
	sourceKind             string
	parentStart, parentEnd int
}

// projectRoslynExecutableFacts is the canonical C# subset projection. Roslyn
// records are frontend evidence only; the result is immediately the existing
// UAST facts contract, with no second C# IR.
func projectRoslynExecutableFacts(records []map[string]any) (*UniversalASTDocument, error) {
	header, err := NewUniversalASTDocument("csharp")
	if err != nil {
		return nil, fmt.Errorf("CSC_SELFHOST_UAST_HEADER: %w", err)
	}
	facts := FrontendSemanticFacts{
		SchemaVersion: header.SchemaVersion, BasisSHA256: header.BasisSHA256,
		LanguageProfile: header.LanguageProfile, LanguageFacet: append([]float64(nil), header.LanguageFacet...), Projection: "frontend_facts.v1",
		Evaluation: "eager_left_to_right", ValueModel: "tagged_dynamic_binary64", IndexBase: 1,
		Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "csharp", EntryPoint: "main"},
	}
	facts.Extensions = map[string]any{"function_entry_bindings": map[string]string{}}
	attachCSharpGo2CSEvidence(&facts)
	root := roslynProjectionNode(&facts, 0, "Scope", "block", "", "", "")
	facts.Nodes = append(facts.Nodes, root)
	ordered := append([]map[string]any(nil), records...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return jsonNumber(ordered[i]["source_start"]) < jsonNumber(ordered[j]["source_start"])
	})
	if err := validateRoslynExecutableCoverage(ordered); err != nil {
		return nil, err
	}
	var nodes []roslynProjectedNode
	for _, record := range ordered {
		kind, ok := roslynProjectionKind(stringValue(record["node_kind"]), stringValue(record["semantic_operation"]))
		if !ok {
			continue
		}
		start, end := jsonNumber(record["source_start"]), jsonNumber(record["source_end"])
		if end <= start {
			continue
		}
		id := len(facts.Nodes)
		name := roslynProjectionName(record, kind)
		if kind == "AssignStmt" && name == "" {
			// Roslyn stores the declared local on the nested
			// VariableDeclarator record, while the enclosing declaration group
			// carries no symbol identity.  Preserve that binding structurally.
			for _, childRecord := range ordered {
				if stringValue(childRecord["node_kind"]) != "VariableDeclarator" {
					continue
				}
				cs, ce := jsonNumber(childRecord["source_start"]), jsonNumber(childRecord["source_end"])
				if cs >= start && ce <= end {
					name = roslynSymbolShortName(stringValue(childRecord["symbol_identity"]))
					if name == "" {
						name = strings.TrimSpace(stringValue(childRecord["text"]))
					}
					break
				}
			}
		}
		typ := roslynProjectionType(stringValue(record["resolved_type"]), stringValue(record["text"]))
		n := roslynProjectionNode(&facts, id, kind, roslynProjectionSemanticKind(kind, stringValue(record["semantic_operation"])), name, stringValue(record["semantic_operation"]), stringValue(record["text"]))
		if typ.Kind != "" {
			roslynSetField(&n, "type_ref", typ)
			roslynSetField(&n, "type_origin", typ.TypeOrigin)
		}
		n.Source = &SemanticSourceSpan{StartOffset: start, EndOffset: end}
		facts.Nodes = append(facts.Nodes, n)
		if kind == "ClosureExpr" {
			if bindings, ok := facts.Extensions["function_entry_bindings"].(map[string]string); ok {
				short := roslynSymbolShortName(stringValue(record["symbol_identity"]))
				if short != "" {
					if strings.EqualFold(short, "Main") {
						bindings[short] = "main"
					} else {
						bindings[short] = short
					}
				}
			}
		}
		parentStart, parentEnd := jsonNumber(record["parent_start"]), jsonNumber(record["parent_end"])
		nodes = append(nodes, roslynProjectedNode{id: id, start: start, end: end, kind: kind, sourceKind: stringValue(record["node_kind"]), parentStart: parentStart, parentEnd: parentEnd})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("CSC_SELFHOST_EXECUTABLE_PROJECTION_EMPTY")
	}
	for _, child := range nodes {
		parent := 0
		bestSize := int(^uint(0) >> 1)
		for _, candidate := range nodes {
			if candidate.id == child.id || candidate.start > child.start || candidate.end < child.end {
				continue
			}
			size := candidate.end - candidate.start
			if size < bestSize {
				parent, bestSize = candidate.id, size
			}
		}
		role := roslynProjectionRole(child.kind, parent, nodes, child.id)
		if parent >= 0 && parent < len(facts.Nodes) {
			// Operation/call nodes are operands only when they are children of
			// another operation or call.  Statement expressions (return,
			// assignment, expression statement) use the canonical expression role.
			parentKind := facts.Nodes[parent].StructuralKind
			if parentKind == "ReturnStmt" || parentKind == "AssignStmt" || parentKind == "ExpressionStmt" {
				role = "expression"
			}
			if parentKind == "IfStmt" {
				if child.kind == "OperationExpr" || child.kind == "CallExpr" || child.kind == "SymbolRef" || child.kind == "LiteralExpr" {
					role = "condition"
				} else {
					role = "then"
				}
			}
			if parent == 0 && child.kind != "ClosureExpr" {
				// Non-executable top-level Roslyn records (for example using
				// directives) are retained as harmless statements in the root
				// scope so they cannot introduce an invalid value edge.
				role = "statement"
			}
			if parentKind == "Scope" {
				role = "statement"
			}
			if parentKind == "CallExpr" {
				callStart := -1
				if facts.Nodes[parent].Source != nil {
					callStart = facts.Nodes[parent].Source.StartOffset
				}
				if child.start == callStart {
					role = "callee"
				} else {
					role = "argument"
				}
			}
		}
		if parentNode := projectedNodeByID(nodes, parent); parentNode != nil {
			if parentNode.kind == "OperationExpr" {
				siblings := projectedChildrenForSpan(nodes, parentNode.start, parentNode.end)
				for i, sibling := range siblings {
					if sibling.id == child.id {
						if len(siblings) == 2 && i == 0 {
							role = "left"
						} else if len(siblings) == 2 && i == 1 {
							role = "right"
						}
						break
					}
				}
			}
		}
		if parent >= 0 && parent < len(facts.Nodes) && child.kind == "SymbolRef" && facts.Nodes[parent].StructuralKind == "CallExpr" {
			// The innermost identifier at the beginning of an invocation is the
			// callable; subsequent nested identifiers are argument expressions.
			callStart, callEnd := 0, 0
			for _, candidate := range nodes {
				if candidate.id == parent {
					callStart, callEnd = candidate.start, candidate.end
					break
				}
			}
			_ = callEnd
			if child.start == callStart {
				role = "callee"
			} else {
				role = "argument"
			}
		}
		if child.kind == "ClosureExpr" {
			parent = 0
			role = "statement"
		}
		if child.sourceKind == "Parameter" {
			role = "parameter"
		}
		if child.kind == "ReturnStmt" || child.kind == "IfStmt" || child.kind == "LoopStmt" || child.kind == "LocalDecl" {
			role = "statement"
		}
		if parent >= 0 && parent < len(facts.Nodes) && facts.Nodes[parent].StructuralKind == "IfStmt" && (child.kind == "ReturnStmt" || child.kind == "Scope") {
			role = "then"
		}
		roleRaw, _ := json.Marshal(role)
		ordinalRaw, _ := json.Marshal(child.start)
		facts.Relations = append(facts.Relations, FrontendRelationFact{Kind: "syntax.child", From: parent, To: UniversalASTReference{Domain: "node", ID: fmt.Sprint(child.id)}, Role: role, Ordinal: child.start, Attributes: map[string]json.RawMessage{"role": roleRaw, "ordinal": ordinalRaw}})
	}
	for i := range facts.Nodes {
		if _, err := decodeUniversalCommon(&facts.Nodes[i]); err != nil {
			return nil, fmt.Errorf("CSC_SELFHOST_EXECUTABLE_PROJECTION_NODE_%d: %w", facts.Nodes[i].ID, err)
		}
	}
	uast, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		return nil, fmt.Errorf("CSC_SELFHOST_EXECUTABLE_PROJECTION_CANONICAL: %w", err)
	}
	return uast, nil
}

func roslynProjectionKind(nodeKind, operation string) (string, bool) {
	switch nodeKind {
	case "MethodDeclaration":
		return "ClosureExpr", true
	case "Parameter":
		// A Roslyn Parameter is a declaration, not a use.  Treating it as a
		// SymbolRef loses the function ABI: the native lowering then has no
		// formal slots into which a resolved call can place its arguments.
		// ParameterDecl is already the canonical UAST representation used by
		// all frontends, so this is a projection correction rather than a
		// C#-specific secondary IR.
		return "ParameterDecl", true
	case "Block":
		return "Scope", true
	case "ReturnStatement":
		return "ReturnStmt", true
	case "IfStatement":
		return "IfStmt", true
	case "WhileStatement", "ForStatement":
		return "LoopStmt", true
	case "LocalDeclarationStatement":
		return "AssignStmt", true
	case "LiteralExpression", "NumericLiteralExpression", "StringLiteralExpression", "CharacterLiteralExpression":
		return "LiteralExpr", true
	case "InvocationExpression":
		return "CallExpr", true
	case "AssignmentExpression":
		return "AssignStmt", true
	case "BinaryExpression", "AddExpression", "SubtractExpression", "MultiplyExpression", "DivideExpression", "ModuloExpression", "EqualsExpression", "NotEqualsExpression", "LessThanExpression", "LessThanOrEqualExpression", "GreaterThanExpression", "GreaterThanOrEqualExpression":
		return "OperationExpr", true
	case "PrefixUnaryExpression":
		return "OperationExpr", true
	case "IdentifierName":
		return "SymbolRef", true
	case "ExpressionStatement":
		return "OperationExpr", true
	case "SimpleMemberAccessExpression":
		return "MemberAccessExpr", true
	case "ElementAccessExpression":
		return "IndexExpr", true
	case "ThrowStatement":
		return "RaisePanicStmt", true
	default:
		return "", false
	}
}

// validateRoslynExecutableCoverage prevents a partial UAST from being
// accepted as a successful binding. Roslyn emits many declaration/scaffolding
// records that are intentionally not executable nodes; every other record
// inside a method body must have an explicit canonical projection.
func validateRoslynExecutableCoverage(records []map[string]any) error {
	type span struct{ start, end int }
	methods := make([]span, 0)
	for _, record := range records {
		if stringValue(record["node_kind"]) == "MethodDeclaration" {
			methods = append(methods, span{jsonNumber(record["source_start"]), jsonNumber(record["source_end"])})
		}
	}
	scaffold := map[string]bool{
		"MethodDeclaration": true, "Parameter": true, "Block": true,
		"ParameterList": true, "PredefinedType": true, "VariableDeclaration": true,
		"VariableDeclarator": true, "EqualsValueClause": true, "Argument": true,
		"ArgumentList": true, "TypeArgumentList": true,
	}
	for _, record := range records {
		kind := stringValue(record["node_kind"])
		start, end := jsonNumber(record["source_start"]), jsonNumber(record["source_end"])
		insideMethod := false
		for _, method := range methods {
			if start >= method.start && end <= method.end {
				insideMethod = true
				break
			}
		}
		if !insideMethod || scaffold[kind] {
			continue
		}
		if _, ok := roslynProjectionKind(kind, stringValue(record["semantic_operation"])); !ok && !roslynThrowPayload(kind, start, end, records) {
			return fmt.Errorf("CSC_UAST_UNSUPPORTED_NODE: node_kind=%s source_start=%d source_end=%d operation=%s", kind, start, end, stringValue(record["semantic_operation"]))
		}
	}
	return nil
}

// Roslyn represents `throw new E(...)` as a ThrowStatement containing object
// construction and argument records.  The canonical panic node already carries
// the executable control effect.  Until exception objects gain a value/runtime
// representation, its payload is deliberately omitted rather than being
// misprojected as a normal allocation.  This narrow exception applies only
// inside a concrete throw span; ordinary object creation remains fail-closed.
func roslynThrowPayload(kind string, start, end int, records []map[string]any) bool {
	if kind != "ObjectCreationExpression" && kind != "Argument" && kind != "ArgumentList" {
		return false
	}
	for _, candidate := range records {
		if stringValue(candidate["node_kind"]) != "ThrowStatement" {
			continue
		}
		throwStart, throwEnd := jsonNumber(candidate["source_start"]), jsonNumber(candidate["source_end"])
		if start >= throwStart && end <= throwEnd {
			return true
		}
	}
	return false
}

func roslynProjectionSemanticKind(structural, operation string) string {
	switch structural {
	case "FunctionDecl", "MethodDecl", "ClosureExpr":
		return "function"
	case "ParameterDecl":
		return "parameter"
	case "SymbolRef":
		return "identifier"
	case "Scope":
		return "block"
	case "ReturnStmt":
		return "return"
	case "IfStmt":
		return "if"
	case "LoopStmt":
		return "loop"
	case "AssignStmt", "VariableDecl":
		return "assign"
	case "LiteralExpr":
		return "literal"
	case "CallExpr":
		return "call"
	case "MemberAccessExpr":
		return "member"
	case "IndexExpr":
		return "index"
	case "RaisePanicStmt":
		return "panic"
	case "OperationExpr":
		if strings.HasPrefix(operation, "Unary.") || operation == "PrefixUnaryExpression" {
			return "unary"
		}
		return "binary"
	default:
		return "expression"
	}
}

func roslynProjectionNode(f *FrontendSemanticFacts, id int, structural, kind, name, operation, text string) UniversalASTNode {
	n := UniversalASTNode{ID: id, StructuralKind: structural, SemanticFacets: defaultUniversalFacets(structural), Fields: map[string]json.RawMessage{}}
	mask, _ := universalFieldMask(&n)
	n.FieldMask = mask
	roslynSetField(&n, "id", id)
	roslynSetField(&n, "kind", kind)
	if name != "" {
		roslynSetField(&n, "name", name)
	}
	op := roslynOperation(operation, text)
	if len(op) > 0 {
		roslynSetField(&n, "operation", op)
	}
	return n
}

func roslynSetField(n *UniversalASTNode, key string, value any) {
	if !containsString(n.FieldMask, key) {
		return
	}
	raw, err := json.Marshal(value)
	if err == nil {
		n.Fields[key] = raw
	}
}
func roslynProjectionName(r map[string]any, kind string) string {
	if s := stringValue(r["symbol_identity"]); s != "" && (kind == "ClosureExpr" || kind == "SymbolRef" || kind == "ParameterDecl") {
		short := roslynSymbolShortName(s)
		if kind == "ClosureExpr" && strings.EqualFold(short, "Main") {
			return "main"
		}
		return short
	}
	text := strings.TrimSpace(stringValue(r["text"]))
	if kind == "ParameterDecl" {
		p := strings.Fields(text)
		if len(p) > 0 {
			return p[len(p)-1]
		}
	}
	return ""
}

func roslynSymbolShortName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '('); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	parts := strings.Fields(strings.TrimSpace(s))
	if len(parts) > 1 {
		return parts[len(parts)-1]
	}
	return strings.TrimSpace(s)
}
func roslynOperation(operation, text string) map[string]any {
	op := map[string]any{}
	switch operation {
	case "Binary.ADD":
		op["operator"] = "+"
	case "Binary.SUB":
		op["operator"] = "-"
	case "Binary.MUL":
		op["operator"] = "*"
	case "Binary.DIV":
		op["operator"] = "/"
	case "Binary.REM":
		op["operator"] = "%"
	case "Compare.EQ":
		op["operator"] = "=="
	case "Compare.NE":
		op["operator"] = "!="
	case "Compare.LT":
		op["operator"] = "<"
	case "Compare.LE":
		op["operator"] = "<="
	case "Compare.GT":
		op["operator"] = ">"
	case "Compare.GE":
		op["operator"] = ">="
	case "Logical.AND":
		op["operator"] = "logical_and"
	case "Logical.OR":
		op["operator"] = "logical_or"
	case "Logical.NOT":
		op["operator"] = "logical_not"
	case "Unary.NEG":
		op["operator"] = "negate"
	case "Unary.POS":
		op["operator"] = "positive"
	case "Literal":
		op["literal_kind"] = roslynLiteralKind(text)
		op["text"] = strings.TrimSpace(text)
	case "Call":
		op["operator"] = "call"
	case "Return":
		op["operator"] = "return"
	default:
		return nil
	}
	op["semantics"] = map[string]any{"confidence": "exact", "evaluation_order": "left_to_right"}
	return op
}
func roslynLiteralKind(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "\"") {
		return "string"
	}
	if text == "true" || text == "false" {
		return "boolean"
	}
	// Roslyn's NumericLiteralExpression text preserves the source spelling.
	// Integral C# literals must remain integer semantic values; classifying all
	// numerics as binary64 makes the native selector use the floating ABI and
	// loses an integer Main return at the PE boundary.
	if !strings.ContainsAny(text, ".eE") {
		return "integer"
	}
	return "number"
}
func roslynProjectionType(typ, text string) SemanticType {
	typ = strings.TrimSpace(typ)
	if strings.Contains(text, "string") {
		typ = "string"
	}
	switch typ {
	case "bool":
		return SemanticType{Kind: "boolean", TypeOrigin: "explicit"}
	case "string":
		return SemanticType{Kind: "string", TypeOrigin: "explicit"}
	case "void":
		return SemanticType{Kind: "void", TypeOrigin: "explicit"}
	case "int", "":
		signed := true
		return SemanticType{Kind: "integer", Bits: 32, Signed: &signed, TypeOrigin: "inferred"}
	default:
		return SemanticType{Kind: "integer", Bits: 32, TypeOrigin: "inferred"}
	}
}
func roslynProjectionRole(kind string, parent int, nodes []roslynProjectedNode, child int) string {
	if kind == "Scope" {
		return "body"
	}
	if kind == "OperationExpr" {
		return "operand"
	}
	if kind == "CallExpr" {
		// A call nested in a return/assignment is itself an expression.  The
		// argument role is reserved for edges originating at the CallExpr.
		return "expression"
	}
	if kind == "SymbolRef" {
		return "value"
	}
	return "expression"
}

func projectedNodeByID(nodes []roslynProjectedNode, id int) *roslynProjectedNode {
	for i := range nodes {
		if nodes[i].id == id {
			return &nodes[i]
		}
	}
	return nil
}

func projectedChildrenForSpan(nodes []roslynProjectedNode, start, end int) []roslynProjectedNode {
	children := make([]roslynProjectedNode, 0, 2)
	for _, node := range nodes {
		if node.parentStart == start && node.parentEnd == end {
			children = append(children, node)
		}
	}
	sort.SliceStable(children, func(i, j int) bool { return children[i].start < children[j].start })
	return children
}
func stringValue(v any) string { s, _ := v.(string); return s }
func jsonNumber(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	}
	return -1
}
