// Copyright (c) 2026 Tarek Wasfy
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
	// Scope-parent edges are derived from the current containment tree. Remove
	// any previously materialized copies before recomputing them so repeated
	// canonicalization (for example after a JSON round trip) cannot retain stale
	// parents or accumulate duplicate scope edges.
	if len(u.Relations) != 0 {
		filtered := u.Relations[:0]
		for _, relation := range u.Relations {
			if relation.Kind != "scope.parent" {
				filtered = append(filtered, relation)
			}
		}
		u.Relations = filtered
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
	// Scope identities are compact, document-local lexical IDs. They are
	// deliberately independent of UAST node IDs so the canonical UAST and its
	// SemanticDocument compatibility view use the same stable scope graph.
	parentScope := map[int]int{0: -1}
	visited := map[int]bool{}
	var walk func(int, int)
	walk = func(id, currentScope int) {
		if visited[id] {
			return
		}
		visited[id] = true
		if common[id].Kind == "block" {
			// Preserve the scope identity already carried by the canonical
			// node.  Only a block whose structured scope differs from its
			// containing scope introduces a new lexical scope (function bodies
			// are the common case); ordinary branch/loop blocks stay in the
			// surrounding scope.
			if id == root {
				currentScope = 0
			} else if common[id].Scope >= 0 && common[id].Scope != currentScope {
				parentScope[common[id].Scope] = currentScope
				currentScope = common[id].Scope
			}
			if id == root {
				currentScope = 0
			}
		}
		scope[id] = currentScope
		roles := make([]string, 0, len(children[id]))
		for role := range children[id] {
			roles = append(roles, role)
		}
		sort.Strings(roles)
		for _, role := range roles {
			for _, child := range children[id][role] {
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
	// Parameters and their defaults are evaluated in the function lexical
	// scope, represented by the function body block's compact scope ID.
	var assignFunctionScope func(int, int)
	assignFunctionScope = func(id, scopeID int) {
		if _, ok := nodes[id]; !ok {
			return
		}
		scope[id] = scopeID
		roles := make([]string, 0, len(children[id]))
		for role := range children[id] {
			roles = append(roles, role)
		}
		sort.Strings(roles)
		for _, role := range roles {
			for _, child := range children[id][role] {
				if common[child.ID].Kind == "block" {
					continue
				}
				assignFunctionScope(child.ID, scopeID)
			}
		}
	}
	for id := range nodes {
		if common[id].Kind != "function" {
			continue
		}
		body := children[id]["body"]
		if len(body) != 1 || common[body[0].ID].Kind != "block" {
			continue
		}
		defaultScope := scope[body[0].ID]
		if common[id].Operation.DefaultEvaluation == "definition" {
			defaultScope = scope[id]
		}
		for _, parameter := range children[id]["parameter"] {
			scope[parameter.ID] = scope[body[0].ID]
			roles := make([]string, 0, len(children[parameter.ID]))
			for role := range children[parameter.ID] {
				roles = append(roles, role)
			}
			sort.Strings(roles)
			for _, role := range roles {
				for _, child := range children[parameter.ID][role] {
					if common[child.ID].Kind != "block" {
						assignFunctionScope(child.ID, defaultScope)
					}
				}
			}
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
	// Evidence projection may already provide the canonical binding target
	// (including its stable binding-domain id).  In that case the structural
	// closure must not add a second node-id-derived target for the same source
	// node.  Keep the closure productive for UASTs without evidence, while
	// preserving the authoritative relation that was already projected.
	bindingRelationPresent := map[string]bool{}
	for _, r := range u.Relations {
		seen[r.Kind+"\x00"+strconv.Itoa(r.From)+"\x00"+r.To.Domain+"\x00"+r.To.ID] = true
		if (r.Kind == "binding.declares" || r.Kind == "binding.refers" || r.Kind == "name.resolves") && r.To.Domain == "binding" {
			bindingRelationPresent[r.Kind+"\x00"+strconv.Itoa(r.From)] = true
		}
	}
	existingRelationKinds := map[string]bool{}
	for _, r := range u.Relations {
		existingRelationKinds[r.Kind] = true
	}
	addNodeRelation := func(kind string, from, to int) {
		n := nodes[from]
		if n == nil || (kind != "scope.parent" && nodes[to] == nil) || !universalRelationAllowed(n, kind) {
			return
		}
		domain := "node"
		if kind == "scope.parent" {
			domain = "scope"
		}
		key := kind + "\x00" + strconv.Itoa(from) + "\x00" + domain + "\x00" + strconv.Itoa(to)
		if seen[key] {
			return
		}
		seen[key] = true
		u.Relations = append(u.Relations, UniversalASTRelation{Kind: kind, From: from, To: UniversalASTReference{Domain: domain, ID: strconv.Itoa(to)}})
	}
	addBindingRelation := func(kind string, from, bindingID int) {
		n := nodes[from]
		if n == nil || !universalRelationAllowed(n, kind) {
			return
		}
		if bindingRelationPresent[kind+"\x00"+strconv.Itoa(from)] {
			return
		}
		key := kind + "\x00" + strconv.Itoa(from) + "\x00binding\x00" + strconv.Itoa(bindingID)
		if seen[key] {
			return
		}
		seen[key] = true
		u.Relations = append(u.Relations, UniversalASTRelation{Kind: kind, From: from, To: UniversalASTReference{Domain: "binding", ID: strconv.Itoa(bindingID)}})
		bindingRelationPresent[kind+"\x00"+strconv.Itoa(from)] = true
	}

	// Default structural relations. They are guarded by the existing UAST
	// relation basis; a new structural kind cannot silently receive a relation
	// that its current canonical contract does not permit.
	for _, parent := range ids {
		statements := children[parent]["statement"]
		if !existingRelationKinds["evaluation.before"] {
			for i := 0; i+1 < len(statements); i++ {
				addNodeRelation("evaluation.before", statements[i].ID, statements[i+1].ID)
			}
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
		if !existingRelationKinds["evaluation.before"] {
			for i := 0; i+1 < len(ordered); i++ {
				addNodeRelation("evaluation.before", ordered[i].ID, ordered[i+1].ID)
			}
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
			if parentID, ok := parentScope[scope[parent]]; ok && parentID >= 0 {
				addNodeRelation("scope.parent", parent, parentID)
			}
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
	// Relation order is part of the canonical JSON digest. Normalize it after
	// closure so repeated imports and compatibility projections are byte-stable
	// even when intermediate role maps were populated in different orders.
	sort.SliceStable(u.Relations, func(i, j int) bool {
		a, b := u.Relations[i], u.Relations[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To.Domain != b.To.Domain {
			return a.To.Domain < b.To.Domain
		}
		return a.To.ID < b.To.ID
	})
	return nil
}
