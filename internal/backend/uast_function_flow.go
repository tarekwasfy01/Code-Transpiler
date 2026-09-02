package backend

import (
	"fmt"
	"sort"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// uastFunctionFlow is the flow matrix model for one UAST closure. Node IDs
// remain the identity throughout; no statement or expression tree is built.
type uastFunctionFlow struct {
	ids                    []int
	kinds                  []string
	entry                  int
	graph                  *uastExecutionGraph
	A, T, F                matrixir.Matrix
	reachable              matrixir.Vector
	cycles                 matrixir.Vector
	stateMachine           bool
	slots                  []string
	initial                matrixir.Vector
	reads, writes, defined matrixir.Matrix
}

func buildUASTFunctionFlow(graph *uastExecutionGraph, functionID int) (*uastFunctionFlow, error) {
	if graph == nil || graph.common[functionID].Kind != "function" {
		return nil, fmt.Errorf("missing UAST function node")
	}
	body, ok, err := graph.one(functionID, "body", true)
	if err != nil || !ok {
		return nil, fmt.Errorf("function node %d lacks body", functionID)
	}
	f := &uastFunctionFlow{graph: graph, ids: []int{-1}, kinds: []string{"entry"}}
	type edge struct{ from, to, kind int }
	edges := []edge{}
	var add func(int, int, int, int) (int, error)
	add = func(id, next, breakTo, continueTo int) (int, error) {
		c := graph.common[id]
		if c.Kind == "block" {
			for i := len(graph.many(id, "statement")) - 1; i >= 0; i-- {
				var err error
				next, err = add(graph.many(id, "statement")[i].ID, next, breakTo, continueTo)
				if err != nil {
					return 0, err
				}
			}
			return next, nil
		}
		if len(f.ids) >= 256 {
			return 0, fmt.Errorf("function flow exceeds 255 nodes")
		}
		i := len(f.ids)
		f.ids = append(f.ids, id)
		f.kinds = append(f.kinds, c.Kind)
		switch c.Kind {
		case "return":
		case "if":
			yes, _, err := graph.oneRelationNode(id, "control.true", true)
			if err != nil {
				return 0, err
			}
			no, hasNo, err := graph.oneRelationNode(id, "control.false", false)
			if err != nil {
				return 0, err
			}
			yesEntry, err := add(yes, next, breakTo, continueTo)
			if err != nil {
				return 0, err
			}
			noEntry := next
			if hasNo {
				noEntry, err = add(no, next, breakTo, continueTo)
				if err != nil {
					return 0, err
				}
			}
			edges = append(edges, edge{i, yesEntry, 1}, edge{i, noEntry, 2})
		case "while":
			f.stateMachine = true
			bodyID, _, err := graph.one(id, "body", true)
			if err != nil {
				return 0, err
			}
			bodyEntry, err := add(bodyID, i, next, i)
			if err != nil {
				return 0, err
			}
			edges = append(edges, edge{i, bodyEntry, 1}, edge{i, next, 2})
		case "break":
			if breakTo < 0 {
				return 0, fmt.Errorf("break outside a loop")
			}
			edges = append(edges, edge{i, breakTo, 0})
		case "continue":
			if continueTo < 0 {
				return 0, fmt.Errorf("next outside a loop")
			}
			edges = append(edges, edge{i, continueTo, 0})
		case "assign":
			if c.Operation.AssignOp == "<<-" {
				return 0, fmt.Errorf("nonlocal assignment requires an environment")
			}
			expression, _, err := graph.one(id, "expression", true)
			if err != nil {
				return 0, err
			}
			if graph.common[expression].Kind == "function" {
				return 0, fmt.Errorf("nested closure requires closure representation")
			}
			edges = append(edges, edge{i, next, 0})
		case "expression":
			edges = append(edges, edge{i, next, 0})
		default:
			return 0, fmt.Errorf("function flow does not model UAST kind %q", c.Kind)
		}
		return i, nil
	}
	entry, err := add(body, 0, -1, -1)
	if err != nil {
		return nil, err
	}
	f.entry = entry
	n := len(f.ids)
	f.A, f.T, f.F = matrixir.NewMatrix(n, n), matrixir.NewMatrix(n, n), matrixir.NewMatrix(n, n)
	adj := matrixir.NewMatrix(n, n)
	for _, e := range edges {
		switch e.kind {
		case 0:
			f.A.Set(e.from, e.to, 1)
		case 1:
			f.T.Set(e.from, e.to, 1)
		case 2:
			f.F.Set(e.from, e.to, 1)
		}
		adj.Set(e.from, e.to, 1)
	}
	closure, err := adj.BooleanClosure()
	if err != nil {
		return nil, err
	}
	entryVector, _ := matrixir.MatrixFromRows([][]float64{matrixir.Basis(n, f.entry)})
	reach, _ := entryVector.Multiply(closure)
	f.reachable = reach.Row(0)
	f.reachable[f.entry] = 1
	if f.reachable[0] != 0 {
		return nil, fmt.Errorf("function has a path without explicit return")
	}
	f.cycles = make(matrixir.Vector, n)
	for i := range f.cycles {
		f.cycles[i] = closure.At(i, i)
	}
	if err := f.analyzeDefiniteAssignments(functionID); err != nil {
		return nil, err
	}
	return f, nil
}

func (f *uastFunctionFlow) analyzeDefiniteAssignments(functionID int) error {
	names := map[string]bool{}
	for _, item := range f.graph.many(functionID, "parameter") {
		names[f.graph.common[item.ID].Name] = true
	}
	for i := 1; i < len(f.ids); i++ {
		c := f.graph.common[f.ids[i]]
		if c.Kind == "assign" {
			names[c.Name] = true
		}
	}
	for name := range names {
		f.slots = append(f.slots, name)
	}
	sort.Strings(f.slots)
	index := map[string]int{}
	for i, name := range f.slots {
		index[name] = i
	}
	n, k := len(f.ids), len(f.slots)
	f.reads, f.writes, f.defined = matrixir.NewMatrix(n, k), matrixir.NewMatrix(n, k), matrixir.NewMatrix(n, k)
	var read func(int, int)
	read = func(node, id int) {
		c := f.graph.common[id]
		if c.Kind == "identifier" {
			if j, ok := index[c.Name]; ok {
				f.reads.Set(node, j, 1)
			}
		}
		for _, roles := range f.graph.children[id] {
			for _, child := range roles {
				read(node, child.ID)
			}
		}
	}
	for i := 1; i < n; i++ {
		c := f.graph.common[f.ids[i]]
		switch c.Kind {
		case "assign":
			expression, _, _ := f.graph.one(f.ids[i], "expression", true)
			read(i, expression)
			f.writes.Set(i, index[c.Name], 1)
		case "expression", "return":
			if expression, ok, _ := f.graph.one(f.ids[i], "expression", false); ok {
				read(i, expression)
			}
		case "if", "while":
			if condition, ok, _ := f.graph.one(f.ids[i], "condition", false); ok {
				read(i, condition)
			}
		}
	}
	seed := make([]float64, k)
	for _, item := range f.graph.many(functionID, "parameter") {
		seed[index[f.graph.common[item.ID].Name]] = 1
	}
	f.initial = seed
	for i := range f.defined.Data {
		f.defined.Data[i] = 1
	}
	predecessors := make([][]int, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if f.reachable[i] != 0 && (f.A.At(i, j) != 0 || f.T.At(i, j) != 0 || f.F.At(i, j) != 0) {
				predecessors[j] = append(predecessors[j], i)
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for i := 0; i < n; i++ {
			if f.reachable[i] == 0 {
				continue
			}
			for j := 0; j < k; j++ {
				v := float64(1)
				if i == f.entry {
					v = seed[j]
				}
				for _, p := range predecessors[i] {
					if f.defined.At(p, j) == 0 && f.writes.At(p, j) == 0 {
						v = 0
					}
				}
				if f.defined.At(i, j) != v {
					f.defined.Set(i, j, v)
					changed = true
				}
			}
		}
	}
	for i := 0; i < n; i++ {
		if f.reachable[i] == 0 {
			continue
		}
		for j, name := range f.slots {
			if f.reads.At(i, j) > f.defined.At(i, j) {
				return fmt.Errorf("function flow reads local %s before definite assignment", name)
			}
		}
	}
	return nil
}

func (f *uastFunctionFlow) evidence(name string) FunctionFlowEvidence {
	e := FunctionFlowEvidence{Name: name, Entry: f.entry, Always: f.A, WhenTrue: f.T, WhenFalse: f.F, Reachable: f.reachable, Cycles: f.cycles, StateMachine: f.stateMachine, Slots: f.slots, Reads: f.reads, Writes: f.writes, Defined: f.defined, Initial: f.initial}
	for i, kind := range f.kinds {
		e.Nodes = append(e.Nodes, fmt.Sprintf("uast:%d:%s", f.ids[i], kind))
	}
	return e
}
