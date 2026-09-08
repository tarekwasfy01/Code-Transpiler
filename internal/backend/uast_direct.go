// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// uastExecutionGraph is a read-only index over the canonical UAST.  It is a
// derived view: it stores only node addresses and decoded copies of fields and
// never owns or mutates semantic state.
type uastExecutionGraph struct {
	document  *UniversalASTDocument
	nodes     map[int]*UniversalASTNode
	common    map[int]universalDecodedCommon
	children  map[int]map[string][]universalChild
	relations map[int]map[string][]UniversalASTReference
	root      int
}

// expressionOwnedByStructuredParent reports whether a node is already
// consumed as an operand/condition/value of another canonical node.  Shared
// UAST graphs may retain the node as a root-level reachability attachment;
// emitting it again as a statement would duplicate semantics.
func expressionOwnedByStructuredParent(g *uastExecutionGraph, id int) bool {
	if g == nil {
		return false
	}
	for _, roles := range g.children {
		for role, children := range roles {
			for _, child := range children {
				if child.ID != id {
					continue
				}
				switch role {
				case "statement", "body", "then", "else", "cleanup", "handler":
					continue
				default:
					return true
				}
			}
		}
	}
	return false
}

var directSemanticStructure = map[string]string{
	"block":            "Scope",
	"expression":       "OperationExpr",
	"assign":           "AssignStmt",
	"if":               "IfStmt",
	"while":            "LoopStmt",
	"repeat":           "LoopStmt",
	"for":              "ForEachStmt",
	"return":           "ReturnStmt",
	"break":            "BreakStmt",
	"continue":         "ContinueStmt",
	"identifier":       "SymbolRef",
	"literal":          "LiteralExpr", // null/NA are checked separately below.
	"call":             "CallExpr",
	"index":            "IndexExpr",
	"function":         "ClosureExpr",
	"typed_operation":  "OperationExpr",
	"binary":           "OperationExpr",
	"unary":            "OperationExpr",
	"iteration":        "OperationExpr",
	"missing_argument": "LiteralExpr",
	"parameter":        "ParameterDecl",
}

// These are the node channels read by the direct execution layer.  Source and
// facets live in dedicated node members and are therefore included separately
// by the generated coverage report.
var directUASTFields = map[string]bool{
	"id": true, "kind": true, "scope_id": true, "type_ref": true,
	"type_origin": true, "operation": true, "effects": true,
	"binding_refs": true, "name": true, "attributes": true,
	"extensions": true,
	"operands":   true, "condition": true, "branches": true, "body": true,
	"members": true, "arguments": true, "parameters": true, "value": true,
	"callee":    true,
	"ownership": true, "lifetime": true, "evaluation_order": true,
	"dispatch": true, "exception_model": true, "candidates": true,
}

var derivedDirectUASTFields = map[string]bool{
	"operands": true, "condition": true, "branches": true, "body": true,
	"members": true, "arguments": true, "parameters": true, "value": true,
	"callee":    true,
	"ownership": true, "lifetime": true, "evaluation_order": true,
	"dispatch": true, "exception_model": true, "candidates": true,
}

func canonicalUniversalAST(p *SemanticProgram) (*UniversalASTDocument, error) {
	if p == nil {
		return nil, fmt.Errorf("missing semantic program")
	}
	if p.UniversalAST == nil {
		return nil, fmt.Errorf("semantic program has no canonical UniversalASTDocument")
	}
	// Rich/non-executable canonical documents are validated by their own
	// schema path. Do not run executable binding/closure derivation on them:
	// doing so would turn a deliberate "no executable lowering" result into a
	// misleading missing-semantic-kind error.
	if p.UniversalAST.Projection != "semantic_document.v1" && p.UniversalAST.Projection != "frontend_facts.v1" {
		return p.UniversalAST, nil
	}
	// Compatibility-imported canonical documents may not have gone through the
	// matrix frontend's final structural pass. Complete the same binding/scope
	// closure from their existing syntax.child graph before validating and
	// executing, so both ingress paths expose one identical symbol graph.
	if err := appendFrontendStructuralClosure(p.UniversalAST); err != nil {
		return nil, err
	}
	if err := removeDuplicateFunctionRootFragments(p.UniversalAST); err != nil {
		return nil, err
	}
	if err := ApplySemanticClosure(p.UniversalAST); err != nil {
		return nil, err
	}
	return p.UniversalAST, nil
}

// removeDuplicateFunctionRootFragments repairs a structural-closure artifact
// without interpreting source spelling: some frontend event streams retain a
// function body's block once below the function and once as a document-root
// statement.  Executing both copies violates ownership and evaluation order.
// Remove a root block only when its complete canonical subtree fingerprint is
// already present below a function body.  This is a graph rewrite over typed
// UAST edges, not a language-specific statement filter.
func removeDuplicateFunctionRootFragments(u *UniversalASTDocument) error {
	if u == nil {
		return nil
	}
	nodes := map[int]*UniversalASTNode{}
	children := map[int][]UniversalASTRelation{}
	for i := range u.Nodes {
		nodes[u.Nodes[i].ID] = &u.Nodes[i]
	}
	for _, r := range u.Relations {
		if r.Kind == "syntax.child" && r.To.Domain == "node" {
			children[r.From] = append(children[r.From], r)
		}
	}
	// Native Go has no executable document-level statements.  If its
	// structural closure produced a function declaration, every additional
	// document-root Scope is therefore an orphaned body fragment.  This is the
	// same ownership proof in a cheaper form and avoids relying on source spans
	// or generated binding names.
	if u.Metadata != nil && u.Metadata["frontend"] == "native-go-uast-v1" {
		hasFunction := false
		for _, n := range u.Nodes {
			var kind string
			_ = json.Unmarshal(n.Fields["kind"], &kind)
			if kind == "function" || strings.EqualFold(n.StructuralKind, "ClosureExpr") {
				hasFunction = true
				break
			}
		}
		if hasFunction {
			removed := map[int]bool{}
			var mark func(int)
			mark = func(id int) {
				if removed[id] { return }
				removed[id] = true
				for _, child := range children[id] {
					if childID, err := strconv.Atoi(child.To.ID); err == nil { mark(childID) }
				}
			}
			for _, r := range u.Relations {
				if r.Kind != "syntax.child" || r.From != 0 || r.To.Domain != "node" { continue }
				id, err := strconv.Atoi(r.To.ID)
				if err == nil && nodes[id] != nil && strings.EqualFold(nodes[id].StructuralKind, "Scope") { mark(id) }
			}
			if len(removed) == 0 { return nil }
			keptNodes := make([]UniversalASTNode, 0, len(u.Nodes)-len(removed))
			for _, n := range u.Nodes { if !removed[n.ID] { keptNodes = append(keptNodes, n) } }
			u.Nodes = keptNodes
			filtered := make([]UniversalASTRelation, 0, len(u.Relations))
			for _, r := range u.Relations {
				remove := removed[r.From]
				if !remove && r.To.Domain == "node" {
					if id, err := strconv.Atoi(r.To.ID); err == nil { remove = removed[id] }
				}
				if !remove {
					filtered = append(filtered, r)
				}
			}
			u.Relations = filtered
			return nil
		}
	}
	var fingerprint func(int, map[int]bool) (string, error)
	fingerprint = func(id int, active map[int]bool) (string, error) {
		if active[id] {
			return "cycle", nil
		}
		n := nodes[id]
		if n == nil {
			return "missing", fmt.Errorf("syntax child node %d missing", id)
		}
		active[id] = true
		defer delete(active, id)
		var b strings.Builder
		b.WriteString(n.StructuralKind)
		keys := make([]string, 0, len(n.Fields))
		for key := range n.Fields {
			// These are identity/ownership facts, not subtree semantics.
			if key == "id" || key == "scope_id" || key == "binding_refs" ||
				key == "source" || key == "source_span" || key == "provenance" || key == "evidence" {
				continue
			}
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			b.WriteByte('|')
			b.WriteString(key)
			b.WriteByte('=')
			b.Write(n.Fields[key])
		}
		ordered := append([]UniversalASTRelation(nil), children[id]...)
		sort.SliceStable(ordered, func(i, j int) bool {
			li, _ := json.Marshal(ordered[i].Attributes["ordinal"])
			lj, _ := json.Marshal(ordered[j].Attributes["ordinal"])
			return string(li) < string(lj)
		})
		for _, r := range ordered {
			childID, err := strconv.Atoi(r.To.ID)
			if err != nil {
				return "", err
			}
			child, err := fingerprint(childID, active)
			if err != nil {
				return "", err
			}
			b.WriteString("|")
			b.WriteString(string(r.Attributes["role"]))
			b.WriteByte(':')
			b.WriteString(child)
		}
		return b.String(), nil
	}
	functionBodyFragments := map[string]bool{}
	var collectFunctionBodies func(int, map[int]bool)
	collectFunctionBodies = func(id int, seen map[int]bool) {
		if seen[id] {
			return
		}
		seen[id] = true
		for _, r := range children[id] {
			childID, err := strconv.Atoi(r.To.ID)
			if err != nil {
				continue
			}
			if nodes[childID] != nil && strings.EqualFold(nodes[childID].StructuralKind, "Scope") {
				if fp, err := fingerprint(childID, map[int]bool{}); err == nil {
					functionBodyFragments[fp] = true
				}
			}
			collectFunctionBodies(childID, seen)
		}
	}
	for _, n := range u.Nodes {
		kind := ""
		if raw := n.Fields["kind"]; len(raw) > 0 {
			_ = json.Unmarshal(raw, &kind)
		}
		if kind == "function" || strings.EqualFold(n.StructuralKind, "ClosureExpr") {
			collectFunctionBodies(n.ID, map[int]bool{})
		}
	}
	if len(functionBodyFragments) == 0 {
		return nil
	}
	filtered := make([]UniversalASTRelation, 0, len(u.Relations))
	for _, r := range u.Relations {
		remove := false
		if r.Kind == "syntax.child" && r.From == 0 && r.To.Domain == "node" {
			id, err := strconv.Atoi(r.To.ID)
			if err == nil && nodes[id] != nil && strings.EqualFold(nodes[id].StructuralKind, "Scope") {
				if fp, fpErr := fingerprint(id, map[int]bool{}); fpErr == nil && functionBodyFragments[fp] {
					remove = true
				}
			}
		}
		if !remove {
			filtered = append(filtered, r)
		}
	}
	u.Relations = filtered
	return nil
}

func newUASTExecutionGraph(u *UniversalASTDocument) (*uastExecutionGraph, error) {
	if err := validateUniversalASTDocument(u); err != nil {
		return nil, err
	}
	if err := validateUniversalExecutionContracts(u); err != nil {
		return nil, err
	}
	if u == nil || len(u.Nodes) == 0 {
		return nil, fmt.Errorf("universal AST has no executable root")
	}
	if u.Projection != "semantic_document.v1" && u.Projection != "frontend_facts.v1" {
		return nil, fmt.Errorf("universal AST payload is represented but has no executable lowering in the direct UAST runtime")
	}
	if err := validateDirectCrosswalkFields(u); err != nil {
		return nil, err
	}
	if err := validateDirectProjectedRelations(u); err != nil {
		return nil, err
	}
	if err := mergeCanonicalizeOnlySyntaxRoots(u); err != nil {
		return nil, err
	}
	children, err := universalChildrenByRole(u)
	if err != nil {
		return nil, err
	}
	g := &uastExecutionGraph{document: u, nodes: map[int]*UniversalASTNode{}, common: map[int]universalDecodedCommon{}, children: children, relations: map[int]map[string][]UniversalASTReference{}, root: -1}
	for _, relation := range u.Relations {
		if g.relations[relation.From] == nil {
			g.relations[relation.From] = map[string][]UniversalASTReference{}
		}
		g.relations[relation.From][relation.Kind] = append(g.relations[relation.From][relation.Kind], relation.To)
	}
	parents := map[int]int{}
	for i := range u.Nodes {
		n := &u.Nodes[i]
		g.nodes[n.ID] = n
		c, err := decodeUniversalCommon(n)
		if err != nil {
			return nil, err
		}
		want, ok := directSemanticStructure[c.Kind]
		if ok {
			if c.Kind == "literal" && (c.Operation.LiteralKind == "null" || c.Operation.LiteralKind == "na") {
				want = "NilLiteral"
			}
			if n.StructuralKind != want {
				return nil, fmt.Errorf("universal node %d structural kind %q disagrees with semantic kind %q", n.ID, n.StructuralKind, c.Kind)
			}
		} else if !universalExecutionStructureImplemented(n.StructuralKind) {
			return nil, fmt.Errorf("universal node %d semantic kind %q has no execution primitive composition", n.ID, c.Kind)
		}
		// Additional canonical facets are consumed through the execution
		// primitive registry above. They no longer require a legacy facet view.
		for field := range n.Fields {
			if !universalExecutionFieldImplemented(field) {
				return nil, fmt.Errorf("universal field %q on node %d has no execution primitive composition", field, n.ID)
			}
		}
		g.common[n.ID] = c
	}
	for parent, roles := range children {
		if g.nodes[parent] == nil {
			return nil, fmt.Errorf("syntax parent node %d missing", parent)
		}
		for _, items := range roles {
			for _, item := range items {
				if g.nodes[item.ID] == nil {
					return nil, fmt.Errorf("syntax child node %d missing", item.ID)
				}
				// Canonical UAST syntax relations may share a semantic node (for
				// example a symbol reference can be both a statement value and an
				// assignment target).  Keep the graph as a DAG; uniqueness of
				// parents is not a validity requirement.
				parents[item.ID]++
			}
		}
	}
	for id := range g.nodes {
		if parents[id] == 0 {
			if g.root >= 0 {
				return nil, fmt.Errorf("universal AST has multiple syntax roots")
			}
			g.root = id
		}
	}
	if g.root < 0 {
		return nil, fmt.Errorf("universal AST has no syntax root")
	}
	seen := map[int]bool{}
	var visit func(int) error
	visit = func(id int) error {
		if seen[id] {
			return nil
		}
		seen[id] = true
		for _, roles := range g.children[id] {
			for _, child := range roles {
				if err := visit(child.ID); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := visit(g.root); err != nil {
		return nil, err
	}
	if len(seen) != len(g.nodes) {
		return nil, fmt.Errorf("universal AST contains detached semantics without a direct execution path")
	}
	if err := g.validateShapes(); err != nil {
		return nil, err
	}
	return g, nil
}

// mergeCanonicalizeOnlySyntaxRoots joins independently serialized canonical
// fragments under the deterministic block root. The SP self-host artifacts
// are explicitly marked CANONICALIZE_ONLY and may contain several source
// modules whose local syntax roots were preserved during export. This repair
// changes only syntax.child ownership; projected semantic relations remain
// derived from the same evidence planes.
func mergeCanonicalizeOnlySyntaxRoots(u *UniversalASTDocument) error {
	if u == nil || u.Metadata == nil || u.Metadata["frontend_route"] != "CANONICALIZE_ONLY" {
		return nil
	}
	parents := map[int]bool{}
	for _, relation := range u.Relations {
		if relation.Kind == "syntax.child" && relation.To.Domain == "node" {
			if id, err := strconv.Atoi(relation.To.ID); err == nil {
				parents[id] = true
			}
		}
	}
	roots := make([]int, 0)
	for _, node := range u.Nodes {
		if !parents[node.ID] {
			roots = append(roots, node.ID)
		}
	}
	if len(roots) <= 1 {
		return nil
	}
	sort.Ints(roots)
	primary := roots[0]
	for _, node := range u.Nodes {
		if node.ID == 0 && strings.EqualFold(node.StructuralKind, "Scope") {
			primary = node.ID
			break
		}
	}
	ordinal := 0
	for _, relation := range u.Relations {
		if relation.Kind == "syntax.child" && relation.From == primary {
			ordinal++
		}
	}
	for _, root := range roots {
		if root == primary {
			continue
		}
		attrs := map[string]json.RawMessage{}
		role, _ := json.Marshal("statement")
		ord, _ := json.Marshal(ordinal)
		attrs["role"], attrs["ordinal"] = role, ord
		u.Relations = append(u.Relations, UniversalASTRelation{Kind: "syntax.child", From: primary, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(root)}, Attributes: attrs})
		ordinal++
	}
	u.Metadata["syntax.roots.merged"] = strconv.Itoa(len(roots))
	return nil
}

func validateDirectProjectedRelations(u *UniversalASTDocument) error {
	copyDocument := *u
	copyDocument.Relations = nil
	semanticIDs := map[int]int{}
	for i := range u.Nodes {
		n := &u.Nodes[i]
		c, err := decodeUniversalCommon(n)
		if err != nil {
			return err
		}
		if c.ID >= 0 {
			semanticIDs[c.ID] = n.ID
		}
	}
	for _, relation := range u.Relations {
		if relation.Kind == "syntax.child" {
			copyDocument.Relations = append(copyDocument.Relations, relation)
		}
	}
	appendUniversalEvidenceRelations(&copyDocument, semanticIDs, u.Evidence)
	// canonicalUniversalAST always completes the same structural closure,
	// including for compatibility-imported documents. Reproduce that complete
	// deterministic pass on the validation copy before applying semantic
	// implications so evidence and executable relations are compared equally.
	if err := appendFrontendStructuralClosure(&copyDocument); err != nil {
		return err
	}
	// canonicalUniversalAST applies the semantic closure after rebuilding the
	// structural graph. Reproduce that deterministic matrix closure on the
	// validation copy as well; otherwise relations such as scope.parent that
	// are materialized by the closure appear as false projection mismatches.
	if err := ApplySemanticClosure(&copyDocument); err != nil {
		return err
	}
	key := func(relation UniversalASTRelation) (string, error) {
		data, err := json.Marshal(relation)
		return string(data), err
	}
	actual, expected := map[string]int{}, map[string]int{}
	for _, relation := range u.Relations {
		if relation.Kind == "syntax.child" || !projectedUASTRelations[relation.Kind] {
			continue
		}
		value, err := key(relation)
		if err != nil {
			return err
		}
		actual[value]++
	}
	for _, relation := range copyDocument.Relations {
		if relation.Kind == "syntax.child" || !projectedUASTRelations[relation.Kind] {
			continue
		}
		value, err := key(relation)
		if err != nil {
			return err
		}
		expected[value]++
	}
	if !reflect.DeepEqual(actual, expected) {
		if u.Metadata != nil && u.Metadata["frontend_route"] == "CANONICALIZE_ONLY" {
			// As with derived fields, canonicalize-only artifacts may carry a
			// projected relation plane from an older graph pass. Preserve syntax
			// and non-projected facts, then replace only the projected relation
			// plane with the matrix/evidence-derived result.
			repaired := make([]UniversalASTRelation, 0, len(u.Relations)+len(copyDocument.Relations))
			for _, relation := range u.Relations {
				if relation.Kind == "syntax.child" || !projectedUASTRelations[relation.Kind] {
					repaired = append(repaired, relation)
				}
			}
			for _, relation := range copyDocument.Relations {
				if relation.Kind != "syntax.child" && projectedUASTRelations[relation.Kind] {
					repaired = append(repaired, relation)
				}
			}
			u.Relations = repaired
			if u.Metadata["crosswalk.repaired"] == "" {
				u.Metadata["crosswalk.repaired"] = "relations-from-evidence"
			} else {
				u.Metadata["crosswalk.repaired"] += ";relations-from-evidence"
			}
			return nil
		}
		keys := make([]string, 0, len(actual)+len(expected))
		seen := map[string]bool{}
		for key := range actual {
			seen[key] = true
			keys = append(keys, key)
		}
		for key := range expected {
			if !seen[key] {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			if actual[key] != expected[key] {
				return fmt.Errorf("universal relation graph differs from matrix/evidence projection: relation=%s actual=%d expected=%d", key, actual[key], expected[key])
			}
		}
		return fmt.Errorf("universal relation graph differs from matrix/evidence projection")
	}
	return nil
}

// validateDirectCrosswalkFields recomputes the field projection from the
// checked-in crosswalk and syntax matrix.  Duplicate field/relationship views
// must agree byte-for-byte, so neither can become an independent truth.
func validateDirectCrosswalkFields(u *UniversalASTDocument) error {
	copyDocument := *u
	copyDocument.Nodes = make([]UniversalASTNode, len(u.Nodes))
	for i := range u.Nodes {
		copyDocument.Nodes[i] = u.Nodes[i]
		copyDocument.Nodes[i].Fields = map[string]json.RawMessage{}
		for name, value := range u.Nodes[i].Fields {
			if !derivedDirectUASTFields[name] {
				copyDocument.Nodes[i].Fields[name] = append(json.RawMessage(nil), value...)
			}
		}
	}
	if err := materializeUniversalCrosswalkFields(&copyDocument); err != nil {
		return err
	}
	for i := range u.Nodes {
		actual, expected := u.Nodes[i].Fields, copyDocument.Nodes[i].Fields
		for field := range derivedDirectUASTFields {
			a, aok := actual[field]
			b, bok := expected[field]
			if aok != bok || (aok && !bytes.Equal(a, b)) {
				if u.Metadata != nil && (u.Metadata["frontend_route"] == "CANONICALIZE_ONLY" || u.Metadata["frontend"] == "native-go-uast-v1") {
					// Canonicalize-only interchange artifacts may contain stale
					// derived field planes from a prior node numbering pass. The
					// syntax relation graph is authoritative; refresh only the
					// derived field and retain the repair as auditable metadata.
					if actual == nil {
						actual = map[string]json.RawMessage{}
						u.Nodes[i].Fields = actual
					}
					if bok {
						actual[field] = append(json.RawMessage(nil), b...)
					} else {
						delete(actual, field)
					}
					if u.Metadata["crosswalk.repaired"] == "" {
						u.Metadata["crosswalk.repaired"] = "derived-fields-from-relations"
					}
					continue
				}
				return fmt.Errorf("universal field %q on node %d differs from crosswalk matrix projection", field, u.Nodes[i].ID)
			}
		}
	}
	return nil
}

func (g *uastExecutionGraph) one(id int, role string, required bool) (int, bool, error) {
	items := g.children[id][role]
	if len(items) == 0 {
		if required {
			return 0, false, fmt.Errorf("universal node %d lacks required %q child", id, role)
		}
		return 0, false, nil
	}
	if len(items) != 1 {
		if role == "argument" {
			// Some canonical projections use the generic argument role for a
			// positional place (for example index/address operands) while the
			// complete call contract retains multiple argument edges. Preserve
			// deterministic first-position extraction here; calls use many().
			return items[0].ID, true, nil
		}
		return 0, false, fmt.Errorf("universal node %d role %q must be singular", id, role)
	}
	return items[0].ID, true, nil
}

func (g *uastExecutionGraph) many(id int, role string) []universalChild {
	return g.children[id][role]
}

func (g *uastExecutionGraph) relationNodes(id int, kind string) ([]int, error) {
	refs := g.relations[id][kind]
	out := make([]int, len(refs))
	for i, ref := range refs {
		if ref.Domain != "node" {
			return nil, fmt.Errorf("relation %q from node %d does not target a node", kind, id)
		}
		value, err := strconv.Atoi(ref.ID)
		if err != nil || g.nodes[value] == nil {
			return nil, fmt.Errorf("relation %q from node %d has missing target", kind, id)
		}
		out[i] = value
	}
	return out, nil
}

func (g *uastExecutionGraph) oneRelationNode(id int, kind string, required bool) (int, bool, error) {
	items, err := g.relationNodes(id, kind)
	if err != nil {
		return 0, false, err
	}
	// Frontend facts may carry the same semantic edge as a structured syntax
	// child (for example a call's `callee`) without duplicating a relation.
	// Resolve that canonical child role before declaring the relation absent.
	if len(items) == 0 {
		if kind == "call.calls" {
			if child, ok, childErr := g.one(id, "callee", false); childErr != nil {
				return 0, false, childErr
			} else if ok {
				items = []int{child}
			}
		}
	}
	if len(items) == 0 {
		if required {
			return 0, false, fmt.Errorf("universal node %d lacks required relation %q", id, kind)
		}
		return 0, false, nil
	}
	if len(items) != 1 {
		return 0, false, fmt.Errorf("universal node %d relation %q must be singular", id, kind)
	}
	return items[0], true, nil
}

// callTarget resolves the executable callee plane from either the structured
// value/callee child or the canonical call.calls relation.  Frontends may keep
// a transparent expression wrapper in the syntax plane while the binding
// resolver has already proved the direct target in the relation plane; both
// are one contract and must feed the same native selector.
func (g *uastExecutionGraph) callTarget(id int) (int, bool, error) {
	first, ok, err := g.one(id, "value", false)
	if err != nil {
		return 0, false, err
	}
	if !ok {
		first, ok, err = g.one(id, "callee", false)
		if err != nil {
			return 0, false, err
		}
	}
	acceptable := func(target int) bool {
		c, exists := g.common[target]
		if !exists {
			return false
		}
		return c.Kind == "identifier" || c.Kind == "function" || c.Type.Reference || c.Kind == "deref" || c.Kind == "index" || c.Kind == "aggregate" ||
			g.nodes[target].StructuralKind == "SymbolRef" || g.nodes[target].StructuralKind == "ClosureExpr"
	}
	if ok && acceptable(first) {
		return first, true, nil
	}
	refs, relErr := g.relationNodes(id, "call.calls")
	if relErr != nil {
		return 0, false, relErr
	}
	for _, target := range refs {
		if acceptable(target) {
			return target, true, nil
		}
	}
	return first, ok, nil
}

func (g *uastExecutionGraph) rejectOtherRoles(id int, allowed ...string) error {
	ok := map[string]bool{}
	for _, role := range allowed {
		ok[role] = true
	}
	for role := range g.children[id] {
		if !ok[role] {
			return fmt.Errorf("universal node %d has unsupported syntax role %q", id, role)
		}
	}
	return nil
}

func (g *uastExecutionGraph) validateShapes() error {
	for id, c := range g.common {
		one := func(role string, required bool) error {
			_, _, err := g.one(id, role, required)
			return err
		}
		switch c.Kind {
		case "block":
			if err := g.rejectOtherRoles(id, "statement"); err != nil {
				return err
			}
		case "expression":
			// An unsupported.* operator is a structured capability marker emitted
			// when MatrixIR has identified a construct but has not proved all
			// children required for a concrete UAST shape.  It is intentionally
			// accepted as a graph node so compatibility/runtime fallback can make
			// the final decision; executable expression nodes remain strict.
			if strings.HasPrefix(c.Operation.Operator, "unsupported.") {
				continue
			}
			if g.document != nil && g.document.Metadata != nil && g.document.Metadata["frontend_route"] == "CANONICALIZE_ONLY" && c.Operation.Operator == "" {
				if _, ok, _ := g.one(id, "expression", false); !ok {
					continue
				}
			}
			if err := one("expression", true); err != nil {
				return err
			}
			if err := g.rejectOtherRoles(id, "expression"); err != nil {
				return err
			}
		case "assign":
			if err := one("expression", false); err != nil {
				return err
			}
			if _, ok, _ := g.one(id, "expression", false); !ok {
				if err := one("value", true); err != nil {
					return err
				}
			}
			if err := g.rejectOtherRoles(id, "expression", "value", "target"); err != nil {
				return err
			}
		case "if":
			if err := one("condition", true); err != nil {
				return err
			}
			if err := one("then", true); err != nil {
				return err
			}
			if err := one("else", false); err != nil {
				return err
			}
			if err := g.rejectOtherRoles(id, "condition", "then", "else"); err != nil {
				return err
			}
		case "while":
			if err := one("condition", true); err != nil {
				return err
			}
			if err := one("body", true); err != nil {
				return err
			}
			if err := g.rejectOtherRoles(id, "condition", "body"); err != nil {
				return err
			}
		case "repeat":
			if err := one("body", true); err != nil {
				return err
			}
			if err := g.rejectOtherRoles(id, "body"); err != nil {
				return err
			}
		case "for":
			if c.Name == "" {
				return fmt.Errorf("universal for node %d lacks binding name", id)
			}
			if err := one("sequence", true); err != nil {
				return err
			}
			if err := one("body", true); err != nil {
				return err
			}
			if err := g.rejectOtherRoles(id, "sequence", "body", "binding"); err != nil {
				return err
			}
		case "return":
			if err := one("expression", false); err != nil {
				return err
			}
			if err := g.rejectOtherRoles(id, "expression"); err != nil {
				return err
			}
		case "break", "continue", "identifier", "literal", "missing_argument":
			if err := g.rejectOtherRoles(id); err != nil {
				return err
			}
		case "unary", "iteration":
			// MatrixIR may use the neutral `operand` role for unary/iteration
			// constructs.  Both roles describe the same single expression edge.
			if _, ok, _ := g.one(id, "value", false); !ok {
				if err := one("operand", true); err != nil {
					return err
				}
			}
			if err := g.rejectOtherRoles(id, "value", "operand"); err != nil {
				return err
			}
		case "binary":
			if err := one("left", true); err != nil {
				return err
			}
			if err := one("right", true); err != nil {
				return err
			}
			if err := g.rejectOtherRoles(id, "left", "right"); err != nil {
				return err
			}
		case "call":
			if err := one("value", false); err != nil {
				return err
			}
			if _, ok, _ := g.one(id, "value", false); !ok {
				if err := one("callee", true); err != nil {
					return err
				}
			}
			if err := g.rejectOtherRoles(id, "value", "callee", "argument"); err != nil {
				return err
			}
		case "index":
			if err := one("value", true); err != nil {
				return err
			}
			if err := g.rejectOtherRoles(id, "value", "argument"); err != nil {
				return err
			}
		case "typed_operation":
			if c.Operation.Typed == nil {
				return fmt.Errorf("universal typed operation node %d lacks operation", id)
			}
			if err := g.rejectOtherRoles(id, "argument"); err != nil {
				return err
			}
		case "function":
			if err := one("body", true); err != nil {
				return err
			}
			if err := g.rejectOtherRoles(id, "parameter", "body"); err != nil {
				return err
			}
		case "parameter":
			if c.Name == "" {
				return fmt.Errorf("universal parameter node %d lacks name", id)
			}
			if err := one("default", false); err != nil {
				return err
			}
			if err := g.rejectOtherRoles(id, "default"); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateDirectSignatureContracts(g *uastExecutionGraph) (bool, error) {
	exact := false
	ids := make([]int, 0, len(g.nodes))
	for id := range g.nodes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		c := g.common[id]
		if c.Kind != "function" {
			continue
		}
		binding, defaults := c.Operation.FunctionBinding, c.Operation.DefaultEvaluation
		params := g.many(id, "parameter")
		if binding == "" {
			if defaults != "" {
				return false, fmt.Errorf("default evaluation requires an explicit function binding contract")
			}
			for _, item := range params {
				if g.document == nil && g.common[item.ID].Operation.ParameterMode != "" {
					return false, fmt.Errorf("parameter modes require exact binding")
				}
			}
			continue
		}
		// Lexical function bindings are not the exact-signature feature. They
		// only identify a declaration for ordinary call resolution and therefore
		// remain valid without a default-evaluation contract.
		if binding != "exact_v1" {
			if defaults == "" {
				for _, item := range params {
					if g.document == nil && g.common[item.ID].Operation.ParameterMode != "" {
						return false, fmt.Errorf("parameter modes require exact binding")
					}
				}
				continue
			}
			return false, fmt.Errorf("unsupported function binding/default contract")
		}
		if defaults != "definition" && defaults != "call" {
			return false, fmt.Errorf("unsupported function binding/default contract")
		}
		exact = true
		var signature []SignatureParameter
		var arguments []SignatureArgument
		for _, item := range params {
			p := g.common[item.ID]
			hasDefault := len(g.many(item.ID, "default")) != 0
			signature = append(signature, SignatureParameter{Name: p.Name, Passing: p.Operation.ParameterMode, HasDefault: hasDefault})
			switch p.Operation.ParameterMode {
			case "positional_only", "positional_or_keyword":
				arguments = append(arguments, SignatureArgument{})
			case "keyword_only":
				arguments = append(arguments, SignatureArgument{Name: p.Name})
			case "variadic_positional", "variadic_keyword":
				if p.Operation.ParameterPassing == "value" {
					return false, fmt.Errorf("typed variadic parameters require aggregate element semantics")
				}
			}
		}
		if _, err := BindSignature(signature, arguments); err != nil {
			return false, err
		}
	}
	if exact && g.document.Evaluation != "eager_left_to_right" {
		return false, fmt.Errorf("exact signatures currently require explicit eager evaluation")
	}
	return exact, nil
}

func validateDirectCallResolutions(g *uastExecutionGraph) (bool, error) {
	exact := false
	for id, c := range g.common {
		if c.Operation.CallResolution == nil {
			continue
		}
		if c.Kind != "call" {
			return false, fmt.Errorf("call resolution attached to non-call expression")
		}
		exact = true
		if err := validateCallResolution(c.Operation.CallResolution, len(g.many(id, "argument"))); err != nil {
			return false, err
		}
	}
	return exact, nil
}

func directTypedRequirements(g *uastExecutionGraph) ([]string, error) {
	functions := map[string]int{}
	integerBindings := map[string]bool{}
	integerResults := map[string]bool{}
	for _, item := range g.many(g.root, "statement") {
		c := g.common[item.ID]
		if c.Kind != "assign" {
			continue
		}
		expr, _, _ := g.one(item.ID, "expression", true)
		if g.common[expr].Kind == "function" {
			functions[c.Name] = expr
		}
	}
	integerExpr := func(id int) bool { return false }
	integerExpr = func(id int) bool {
		c := g.common[id]
		if c.Operation.Typed != nil {
			return c.Operation.Typed.resultType().Kind == "integer"
		}
		if c.Kind == "identifier" {
			return integerBindings[c.Name]
		}
		if c.Kind == "call" {
			callee, ok, _ := g.one(id, "value", false)
			return ok && g.common[callee].Kind == "identifier" && integerResults[g.common[callee].Name]
		}
		return false
	}
	var scan func(int, string)
	scan = func(id int, function string) {
		c := g.common[id]
		if c.Kind == "assign" {
			if expr, ok, _ := g.one(id, "expression", false); ok {
				if integerExpr(expr) {
					integerBindings[c.Name] = true
				}
				if g.common[expr].Kind == "function" {
					function = c.Name
				}
			}
		}
		if c.Kind == "parameter" && c.Operation.ParameterPassing == "value" {
			_, integerBindings[c.Name] = uastExactIntegerParameterType(c.Type)
		}
		if c.Kind == "return" {
			if expr, ok, _ := g.one(id, "expression", false); ok && integerExpr(expr) {
				integerResults[function] = true
			}
		}
		for _, roles := range g.children[id] {
			for _, child := range roles {
				scan(child.ID, function)
			}
		}
	}
	scan(g.root, "")
	required := []string{}
	ids := make([]int, 0, len(g.nodes))
	for id := range g.nodes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		c := g.common[id]
		if c.Kind == "parameter" && c.Operation.ParameterPassing == "value" {
			exactType, isExactInteger := uastExactIntegerParameterType(c.Type)
			if !isExactInteger {
				continue
			}
			op := SemanticOperation{Name: "integer.value", Type: exactType}
			if err := op.validate(1); err != nil {
				return nil, err
			}
			required = append(required, op.Name)
		}
		if c.Kind == "if" || c.Kind == "while" {
			if q, ok, _ := g.one(id, "condition", false); ok && integerExpr(q) {
				return nil, fmt.Errorf("integer control/index input requires explicit modeled semantics")
			}
		}
		if c.Kind == "for" {
			if q, ok, _ := g.one(id, "sequence", false); ok && integerExpr(q) {
				return nil, fmt.Errorf("integer control/index input requires explicit modeled semantics")
			}
		}
		if c.Kind == "binary" {
			operands, err := g.relationNodes(id, "data.operand")
			if err != nil || len(operands) != 2 {
				if err != nil {
					return nil, err
				}
				return nil, fmt.Errorf("binary operation lacks two data.operand relations")
			}
			l, _, _ := g.one(id, "left", true)
			r, _, _ := g.one(id, "right", true)
			if c.Operation.Typed == nil && (integerExpr(l) || integerExpr(r)) {
				return nil, fmt.Errorf("integer operands require a typed operation")
			}
		}
		if c.Kind == "unary" || c.Kind == "index" {
			q, _, _ := g.one(id, "value", true)
			if c.Operation.Typed == nil && integerExpr(q) {
				return nil, fmt.Errorf("integer value requires an explicit typed operation")
			}
		}
		if c.Kind == "index" {
			for _, arg := range g.many(id, "argument") {
				if integerExpr(arg.ID) {
					return nil, fmt.Errorf("exact integer indexing requires explicit index semantics")
				}
			}
		}
		if c.Kind == "call" && c.Operation.Typed == nil {
			callee, _, _ := g.one(id, "value", true)
			// Output builtins are variadic sinks rather than user function
			// declarations.  Their arguments still retain exact integer
			// values, so they are valid without a typed-parameter contract.
			if callee >= 0 && g.common[callee].Kind == "identifier" {
				switch g.common[callee].Name {
				case "print", "println", "show", "fmt.Println", "fmt.Print", "fmt.Printf":
					continue
				}
			}
			fn := -1
			if g.common[callee].Kind == "identifier" {
				if q, ok := functions[g.common[callee].Name]; ok {
					fn = q
				}
			}
			for i, arg := range g.many(id, "argument") {
				if !integerExpr(arg.ID) {
					continue
				}
				params := []universalChild{}
				if fn >= 0 {
					params = g.many(fn, "parameter")
				}
				if fn < 0 || i >= len(params) || arg.Meta.Name != "" || g.common[params[i].ID].Operation.ParameterPassing != "value" {
					return nil, fmt.Errorf("integer argument requires a typed function parameter or explicit format")
				}
				actualOp := g.common[arg.ID].Operation.Typed
				if actualOp == nil {
					return nil, fmt.Errorf("integer argument needs an explicit typed load")
				}
				actual, expected := actualOp.resultType(), g.common[params[i].ID].Type
				if exactType, ok := uastExactIntegerParameterType(expected); ok {
					expected = exactType
				}
				actual.TypeOrigin, expected.TypeOrigin = "", ""
				if !reflect.DeepEqual(actual, expected) {
					return nil, fmt.Errorf("integer function argument type mismatch")
				}
			}
		}
		if c.Operation.Typed == nil {
			continue
		}
		op := c.Operation.Typed
		args := g.many(id, "argument")
		if err := op.validate(len(args)); err != nil {
			return nil, err
		}
		required = append(required, op.Name)
		for _, arg := range args {
			if arg.Meta.Missing {
				return nil, fmt.Errorf("missing operation operand")
			}
			a := g.common[arg.ID]
			if a.Operation.Typed == nil {
				if op.Name != "integer.value" || (a.Kind != "identifier" && a.Kind != "call") {
					return nil, fmt.Errorf("%s requires typed integer operands", op.Name)
				}
				continue
			}
			actual, expected := a.Operation.Typed.resultType(), op.Type
			actual.TypeOrigin, expected.TypeOrigin = "", ""
			if actual.Kind != "integer" || (op.Name != "integer.convert" && !reflect.DeepEqual(actual, expected)) {
				return nil, fmt.Errorf("%s has inconsistent operand type", op.Name)
			}
		}
	}
	return required, nil
}

func directUASTFieldJSON(n *UniversalASTNode, field string) json.RawMessage { return n.Fields[field] }
