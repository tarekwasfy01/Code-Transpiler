package backend

// This file contains the single structural closure shared by every MatrixIR
// frontend. It receives only a canonical UAST and its typed syntax.child
// roles. It never examines source text, grammar spelling, diagnostics, or a
// target language. Consequently definition/reference, scope, sequencing, and
// control facts are produced once for all 13 source languages.

import (
	"encoding/json"
	"sort"
	"strconv"
)

type frontendBindingDefinition struct {
	name, kind  string
	node, scope int
}

func appendFrontendStructuralClosure(u *UniversalASTDocument) error {
	if u == nil {
		return nil
	}
	children, err := universalChildrenByRole(u)
	if err != nil {
		return err
	}
	nodes := make(map[int]*UniversalASTNode, len(u.Nodes))
	common := make(map[int]universalDecodedCommon, len(u.Nodes))
	for i := range u.Nodes {
		n := &u.Nodes[i]
		c, err := decodeUniversalCommon(n)
		if err != nil {
			return err
		}
		nodes[n.ID], common[n.ID] = n, c
	}

	// The root lexical scope is the root Scope node. Every nested Scope owns a
	// fresh scope; all other children inherit their parent's scope. This uses
	// only the canonical containment tree and works for blocks, closures,
	// branches, exception bodies, and future structured forms alike.
	root := 0
	incoming := map[int]bool{}
	for _, r := range u.Relations {
		if r.Kind != "syntax.child" || r.To.Domain != "node" {
			continue
		}
		if id, e := strconv.Atoi(r.To.ID); e == nil {
			incoming[id] = true
		}
	}
	for _, n := range u.Nodes {
		if !incoming[n.ID] && common[n.ID].Kind == "block" {
			root = n.ID
			break
		}
	}
	scope := make(map[int]int, len(u.Nodes))
	parentScope := map[int]int{root: -1}
	visited := map[int]bool{}
	var walk func(int, int)
	walk = func(id, currentScope int) {
		if visited[id] {
			return
		}
		visited[id] = true
		if common[id].Kind == "block" {
			if id != root {
				parentScope[id] = currentScope
			}
			currentScope = id
		}
		scope[id] = currentScope
		for _, role := range children[id] {
			for _, child := range role {
				walk(child.ID, currentScope)
			}
		}
	}
	walk(root, root)
	for id := range nodes {
		if _, ok := scope[id]; !ok {
			// A valid normalized UAST has one root. Retaining an unreachable node
			// would be a validator error; assigning root here keeps this closure
			// conservative if it is invoked before the final normalizer.
			scope[id] = root
		}
	}

	putScope := func(id, scopeID int) error {
		n := nodes[id]
		if n == nil || !containsString(n.FieldMask, "scope_id") {
			return nil
		}
		if n.Fields == nil {
			n.Fields = map[string]json.RawMessage{}
		}
		value, err := json.Marshal(scopeID)
		if err != nil {
			return err
		}
		n.Fields["scope_id"] = value
		return nil
	}
	ids := make([]int, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		if err := putScope(id, scope[id]); err != nil {
			return err
		}
	}
	// Scope information has changed; refresh decoded common fields before
	// deriving definitions and references.
	for _, id := range ids {
		c, err := decodeUniversalCommon(nodes[id])
		if err != nil {
			return err
		}
		common[id] = c
	}

	seen := map[string]bool{}
	for _, r := range u.Relations {
		seen[r.Kind+"\x00"+strconv.Itoa(r.From)+"\x00"+r.To.Domain+"\x00"+r.To.ID] = true
	}
	addNodeRelation := func(kind string, from, to int) {
		n := nodes[from]
		if n == nil || nodes[to] == nil || !universalRelationAllowed(n, kind) {
			return
		}
		key := kind + "\x00" + strconv.Itoa(from) + "\x00node\x00" + strconv.Itoa(to)
		if seen[key] {
			return
		}
		seen[key] = true
		u.Relations = append(u.Relations, UniversalASTRelation{Kind: kind, From: from, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(to)}})
	}
	addBindingRelation := func(kind string, from, bindingID int) {
		n := nodes[from]
		if n == nil || !universalRelationAllowed(n, kind) {
			return
		}
		key := kind + "\x00" + strconv.Itoa(from) + "\x00binding\x00" + strconv.Itoa(bindingID)
		if seen[key] {
			return
		}
		seen[key] = true
		u.Relations = append(u.Relations, UniversalASTRelation{Kind: kind, From: from, To: UniversalASTReference{Domain: "binding", ID: strconv.Itoa(bindingID)}})
	}

	// Default structural relations. They are guarded by the existing UAST
	// relation basis; a new structural kind cannot silently receive a relation
	// that its current canonical contract does not permit.
	for _, parent := range ids {
		statements := children[parent]["statement"]
		for i := 0; i+1 < len(statements); i++ {
			addNodeRelation("evaluation.before", statements[i].ID, statements[i+1].ID)
		}
		// Expression operands have a canonical, role-defined evaluation order.
		// This is shared structure, not source-language syntax: all arguments in
		// a role are already sorted by their typed ordinal.
		var ordered []universalChild
		switch common[parent].Kind {
		case "binary":
			ordered = append(ordered, children[parent]["left"]...)
			ordered = append(ordered, children[parent]["right"]...)
		case "unary", "index", "slice":
			ordered = append(ordered, children[parent]["value"]...)
			ordered = append(ordered, children[parent]["argument"]...)
		case "call":
			ordered = append(ordered, children[parent]["value"]...)
			ordered = append(ordered, children[parent]["argument"]...)
		case "aggregate", "tuple", "expression":
			ordered = append(ordered, children[parent]["argument"]...)
			ordered = append(ordered, children[parent]["expression"]...)
		}
		for i := 0; i+1 < len(ordered); i++ {
			addNodeRelation("evaluation.before", ordered[i].ID, ordered[i+1].ID)
		}
		for _, child := range children[parent]["then"] {
			addNodeRelation("control.true", parent, child.ID)
		}
		for _, child := range children[parent]["else"] {
			addNodeRelation("control.false", parent, child.ID)
		}
		if common[parent].Kind == "while" || common[parent].Kind == "for" || common[parent].Kind == "repeat" {
			for _, child := range children[parent]["body"] {
				addNodeRelation("control.loop_back", child.ID, parent)
			}
		}
		if common[parent].Kind == "block" && parent != root {
			addNodeRelation("scope.parent", parent, parentScope[parent])
		}
	}

	definitions := make([]frontendBindingDefinition, 0)
	declarationNodes := map[int]bool{}
	// An assignment definition is read from its typed target child, never from
	// text. Write that name back into the existing assignment field so runtime,
	// emitters, evidence, and semantic trace share exactly the same binding.
	for _, id := range ids {
		if common[id].Kind != "assign" {
			continue
		}
		var target int = -1
		if items := children[id]["target"]; len(items) == 1 {
			target = items[0].ID
		}
		name := ""
		if target >= 0 && common[target].Kind == "identifier" {
			name = common[target].Name
			declarationNodes[target] = true
		}
		if name == "" {
			name = common[id].Name
		}
		if name == "" {
			continue
		}
		if containsString(nodes[id].FieldMask, "name") {
			value, _ := json.Marshal(name)
			if nodes[id].Fields == nil {
				nodes[id].Fields = map[string]json.RawMessage{}
			}
			nodes[id].Fields["name"] = value
			decoded := common[id]
			decoded.Name = name
			common[id] = decoded
		}
		definitions = append(definitions, frontendBindingDefinition{name: name, kind: "assignment", node: id, scope: scope[id]})
	}
	// Parameters are declarations too. A ClosureExpr has parameter children in
	// every language-specific parser profile, so the relation is generic.
	for _, id := range ids {
		if common[id].Kind != "function" {
			continue
		}
		if common[id].Name != "" {
			declarationNodes[id] = true
			definitions = append(definitions, frontendBindingDefinition{name: common[id].Name, kind: "function", node: id, scope: scope[id]})
		}
		for _, parameter := range children[id]["parameter"] {
			if common[parameter.ID].Kind != "identifier" || common[parameter.ID].Name == "" {
				continue
			}
			declarationNodes[parameter.ID] = true
			definitions = append(definitions, frontendBindingDefinition{name: common[parameter.ID].Name, kind: "parameter", node: parameter.ID, scope: scope[id]})
		}
	}
	// The canonical schemas also expose standalone Binding/Parameter nodes in
	// some frontends. Treat them as declarations through the same scope graph
	// as assignments and function children; this is not a source-language
	// rule and preserves module/global and capture visibility uniformly.
	for _, id := range ids {
		if (common[id].Kind != "binding" && common[id].Kind != "parameter") || common[id].Name == "" {
			continue
		}
		declarationNodes[id] = true
		kind := common[id].Kind
		definitions = append(definitions, frontendBindingDefinition{name: common[id].Name, kind: kind, node: id, scope: scope[id]})
	}

	depth := func(s int) int {
		d := 0
		for s >= 0 {
			d++
			s = parentScope[s]
		}
		return d
	}
	visible := func(referenceScope, definitionScope int) bool {
		for s := referenceScope; s >= 0; s = parentScope[s] {
			if s == definitionScope {
				return true
			}
		}
		return false
	}
	for _, ref := range ids {
		if common[ref].Kind != "identifier" || declarationNodes[ref] || common[ref].Name == "" {
			continue
		}
		best := -1
		bestDepth := -1
		for i, definition := range definitions {
			if definition.name != common[ref].Name || !visible(scope[ref], definition.scope) {
				continue
			}
			d := depth(definition.scope)
			if d > bestDepth || (d == bestDepth && (best < 0 || definition.node > definitions[best].node)) {
				best, bestDepth = i, d
			}
		}
		if best < 0 {
			continue
		}
		definition := definitions[best]
		addBindingRelation("binding.refers", ref, definition.node)
		addBindingRelation("name.resolves", ref, definition.node)
		addNodeRelation("data.def_use", ref, definition.node)
	}
	for _, definition := range definitions {
		addBindingRelation("binding.declares", definition.node, definition.node)
	}
	return nil
}
