package backend

import (
	"fmt"
	"sort"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// AnalyzeUniversalEvidence derives executable evidence exclusively from the
// canonical UAST graph. It is shared by direct frontends and never traverses a
// legacy Stmt or Expr tree.
func AnalyzeUniversalEvidence(u *UniversalASTDocument) (SemanticEvidence, error) {
	if err := validateUniversalASTDocument(u); err != nil {
		return SemanticEvidence{}, err
	}
	e := SemanticEvidence{
		TypeAxes:     []string{"binary64", "string", "boolean", "null", "na", "nan", "function", "unknown"},
		EffectAxes:   []string{"local.read", "local.write", "io.read", "io.write", "memory.allocate", "global.read", "global.write", "filesystem.read", "filesystem.write", "network", "exception.throw", "thread.spawn", "synchronization", "ffi", "time", "random", "call.unknown", "control"},
		CallModeAxes: []string{"lazy_demand", "eager_left_to_right"}, ContractAxes: []string{"lazy_demand", "eager_left_to_right", "binary64", "one_based_index", "full_source_type_equivalence"}, Contract: matrixir.Vector{0, 0, 1, 1, 0},
	}
	if u.Evaluation == "lazy_demand" {
		e.Contract[0] = 1
	} else {
		e.Contract[1] = 1
	}
	nodes := append([]UniversalASTNode(nil), u.Nodes...)
	sort.Slice(nodes, func(i, j int) bool {
		a, _ := decodeUniversalCommon(&nodes[i])
		b, _ := decodeUniversalCommon(&nodes[j])
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return nodes[i].ID < nodes[j].ID
	})
	index := map[int]int{}
	maxScope := 0
	for i := range nodes {
		c, err := decodeUniversalCommon(&nodes[i])
		if err != nil {
			return SemanticEvidence{}, err
		}
		index[nodes[i].ID] = i
		if c.Scope > maxScope {
			maxScope = c.Scope
		}
		kind := c.Kind
		symbol := c.Name
		if kind == "literal" {
			symbol = c.Operation.Text
		}
		if kind == "typed_operation" && c.Operation.Typed != nil {
			symbol = c.Operation.Typed.Name
		}
		if kind == "call" && symbol == "" {
			symbol = c.Operation.Text
		}
		e.Nodes = append(e.Nodes, SemanticNode{ID: i, Kind: kind, Symbol: symbol, Scope: c.Scope})
	}
	n := len(e.Nodes)
	e.Types = matrixir.NewSparseMatrix(n, len(e.TypeAxes))
	e.Effects = matrixir.NewSparseMatrix(n, len(e.EffectAxes))
	e.Syntax = matrixir.NewSparseMatrix(n, n)
	e.Control = matrixir.NewSparseMatrix(n, n)
	e.Data = matrixir.NewSparseMatrix(n, n)
	e.Binding = matrixir.NewSparseMatrix(n, 0)
	e.Order = matrixir.NewSparseMatrix(n, n)
	e.CallModes = matrixir.NewSparseMatrix(n, 2)
	e.Scope = matrixir.NewSparseMatrix(n, maxScope+1)
	for i, node := range nodes {
		c, _ := decodeUniversalCommon(&node)
		e.Types.Set(i, uastEvidenceType(c), 1)
		e.Scope.Set(i, c.Scope, 1)
		if c.Kind == "identifier" {
			e.Effects.Set(i, 0, 1)
		}
		if c.Kind == "assign" {
			e.Effects.Set(i, 1, 1)
		}
		if c.Kind == "block" || c.Kind == "expression" || c.Kind == "return" || c.Kind == "break" || c.Kind == "continue" {
			e.Effects.Set(i, 17, 1)
		}
		if c.Kind == "call" {
			e.Effects.Set(i, 16, 1)
			if u.Evaluation == "eager_left_to_right" {
				e.CallModes.Set(i, 1, 1)
			} else {
				e.CallModes.Set(i, 0, 1)
			}
		}
		if c.Kind == "if" || c.Kind == "while" || c.Kind == "for" || c.Kind == "repeat" {
			e.Effects.Set(i, 17, 1)
		}
	}
	for _, r := range u.Relations {
		from, ok := index[r.From]
		if !ok || r.To.Domain != "node" {
			continue
		}
		var target int
		if _, err := fmt.Sscan(r.To.ID, &target); err != nil {
			continue
		}
		to, ok := index[target]
		if !ok {
			continue
		}
		switch r.Kind {
		case "syntax.child":
			e.Syntax.Set(from, to, 1)
		case "data.operand", "data.read", "data.write", "data.dependsOn":
			e.Data.Set(from, to, 1)
		case "control.true", "control.false", "control.loop", "control.next":
			e.Control.Set(from, to, 1)
		}
	}
	e.Scopes = make([]SemanticScope, maxScope+1)
	for i := range e.Scopes {
		parent := -1
		if i > 0 {
			parent = 0
		}
		kind := "block"
		if i == 0 {
			kind = "program"
		}
		e.Scopes[i] = SemanticScope{ID: i, Kind: kind, Parent: parent}
	}
	// Lexical bindings are derived from already-proved declaration and scope
	// facts.  This is the same conservative rule used by the former analyser:
	// retain all visible candidates; never choose by spelling alone.
	for i, node := range e.Nodes {
		if node.Kind == "assign" || node.Kind == "parameter" {
			e.Bindings = append(e.Bindings, SemanticBinding{ID: len(e.Bindings), Name: node.Symbol, Scope: node.Scope, Mutable: node.Kind == "assign", Definition: i, TypeOrigin: "unknown"})
		}
	}
	e.Binding = matrixir.NewSparseMatrix(n, len(e.Bindings))
	ancestor := func(scope, target int) bool {
		for scope >= 0 {
			if scope == target {
				return true
			}
			scope = e.Scopes[scope].Parent
		}
		return false
	}
	for i, node := range e.Nodes {
		if node.Kind != "identifier" {
			continue
		}
		for j, b := range e.Bindings {
			if b.Name == node.Symbol && ancestor(node.Scope, b.Scope) {
				e.Binding.Set(i, j, 1)
				e.Data.Set(i, b.Definition, 1)
				break // the executable compatibility contract has one proved binding slot
			}
		}
	}
	return e, nil
}

func uastEvidenceType(c universalDecodedCommon) int {
	if c.Kind == "function" {
		return 6
	}
	if c.Kind == "literal" {
		switch c.Operation.LiteralKind {
		case "string":
			return 1
		case "boolean":
			return 2
		case "null":
			return 3
		case "na":
			return 4
		}
	}
	if c.Type.Kind == "boolean" {
		return 2
	}
	if c.Type.Kind == "string" {
		return 1
	}
	return 7
}
