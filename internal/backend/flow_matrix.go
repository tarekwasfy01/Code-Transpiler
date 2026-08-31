package backend

import (
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// A, T and F are separately labelled transition matrices for unconditional, true and
// false edges. Node zero denotes falling out of the function without return.
// Return rows have no outgoing edge. Local bindings travel with each path;
// a join receives the bindings from the branch that actually executed.
type functionFlow struct {
	nodes                  []Stmt
	entry                  int
	A, T, F                matrixir.Matrix
	reachable              matrixir.Vector
	cycles                 matrixir.Vector
	stateMachine           bool
	slots                  []string
	initial                matrixir.Vector
	reads, writes, defined matrixir.Matrix
	iterations             []IterationEvidence
}

// A failed safety proof must never fall back to an unchecked closure emitter.
type flowSafetyError struct{ error }

// FunctionFlowEvidence exposes the very matrices used by the generator, not a
// second graph reconstructed from emitted text. It is intended for audit tools.
type FunctionFlowEvidence struct {
	Name         string              `json:"name"`
	Entry        int                 `json:"entry"`
	Nodes        []string            `json:"nodes"`
	Always       matrixir.Matrix     `json:"always"`
	WhenTrue     matrixir.Matrix     `json:"when_true"`
	WhenFalse    matrixir.Matrix     `json:"when_false"`
	Reachable    matrixir.Vector     `json:"reachable"`
	Error        string              `json:"error,omitempty"`
	Cycles       matrixir.Vector     `json:"cycles"`
	StateMachine bool                `json:"state_machine"`
	Slots        []string            `json:"slots"`
	Reads        matrixir.Matrix     `json:"reads"`
	Writes       matrixir.Matrix     `json:"writes"`
	Defined      matrixir.Matrix     `json:"defined"`
	Initial      matrixir.Vector     `json:"initial"`
	Iterations   []IterationEvidence `json:"iterations,omitempty"`
}

func AnalyzeFunctionFlows(canonical string) ([]FunctionFlowEvidence, error) {
	program, err := ParseSemantic("r", canonical)
	if err != nil {
		return nil, err
	}
	return AnalyzeSemanticFunctionFlows(program)
}

// AnalyzeSemanticFunctionFlows is the semantic-IR entry point. The older
// string function above remains solely a compatibility adapter for callers
// that still have R text; no target generator needs that text as state.
func AnalyzeSemanticFunctionFlows(program *SemanticProgram) ([]FunctionFlowEvidence, error) {
	if program == nil || program.Body == nil {
		return nil, fmt.Errorf("missing semantic program")
	}
	result := []FunctionFlowEvidence{}
	for _, s := range program.Body.List {
		a, ok := s.(*AssignStmt)
		if !ok {
			continue
		}
		fn, ok := a.Value.(*FunctionExpr)
		if !ok {
			continue
		}
		e := FunctionFlowEvidence{Name: a.Name}
		f, err := buildFunctionFlow(fn)
		if err != nil {
			e.Error = err.Error()
		} else {
			e.Entry, e.Always, e.WhenTrue, e.WhenFalse, e.Reachable = f.entry, f.A, f.T, f.F, f.reachable
			e.Cycles, e.StateMachine, e.Slots, e.Reads, e.Writes, e.Defined = f.cycles, f.stateMachine, f.slots, f.reads, f.writes, f.defined
			e.Initial = f.initial
			e.Iterations = f.iterations
			for _, node := range f.nodes {
				e.Nodes = append(e.Nodes, fmt.Sprintf("%T", node))
			}
		}
		result = append(result, e)
	}
	return result, nil
}

func buildFunctionFlow(fn *FunctionExpr) (*functionFlow, error) {
	if fn == nil || fn.Body == nil {
		return nil, fmt.Errorf("missing function body")
	}
	body, iterations := normalizeFunctionIterations(fn)
	f := &functionFlow{nodes: []Stmt{nil}, iterations: iterations}
	type edge struct{ from, to, kind int }
	var edges []edge
	var add func(Stmt, int, int, int) (int, error)
	add = func(s Stmt, next, breakTo, continueTo int) (int, error) {
		if s == nil {
			return next, nil
		}
		if b, ok := s.(*BlockStmt); ok {
			var err error
			for i := len(b.List) - 1; i >= 0; i-- {
				next, err = add(b.List[i], next, breakTo, continueTo)
				if err != nil {
					return 0, err
				}
			}
			return next, nil
		}
		if len(f.nodes) >= 256 {
			return 0, fmt.Errorf("function flow exceeds 255 statements")
		}
		i := len(f.nodes)
		f.nodes = append(f.nodes, s)
		switch x := s.(type) {
		case *ReturnStmt:
		case *IfStmt:
			yes, err := add(x.Then, next, breakTo, continueTo)
			if err != nil {
				return 0, err
			}
			no, err := add(x.Else, next, breakTo, continueTo)
			if err != nil {
				return 0, err
			}
			edges = append(edges, edge{i, yes, 1}, edge{i, no, 2})
		case *WhileStmt:
			f.stateMachine = true
			body, err := add(x.Body, i, next, i)
			if err != nil {
				return 0, err
			}
			edges = append(edges, edge{i, body, 1}, edge{i, next, 2})
		case *BreakStmt:
			if breakTo < 0 {
				return 0, fmt.Errorf("break outside a loop")
			}
			edges = append(edges, edge{i, breakTo, 0})
		case *NextStmt:
			if continueTo < 0 {
				return 0, fmt.Errorf("continue outside a loop")
			}
			edges = append(edges, edge{i, continueTo, 0})
		case *AssignStmt:
			if _, nested := x.Value.(*FunctionExpr); nested {
				return 0, fmt.Errorf("nested closure requires closure representation")
			}
			if x.Op == "<<-" {
				return 0, fmt.Errorf("nonlocal assignment requires an environment")
			}
			edges = append(edges, edge{i, next, 0})
		case *ExprStmt:
			edges = append(edges, edge{i, next, 0})
		default:
			return 0, fmt.Errorf("function flow does not model %T", s)
		}
		return i, nil
	}
	var err error
	f.entry, err = add(body, 0, -1, -1)
	if err != nil {
		return nil, err
	}
	n := len(f.nodes)
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
	entry, _ := matrixir.MatrixFromRows([][]float64{matrixir.Basis(n, f.entry)})
	reach, _ := entry.Multiply(closure)
	f.reachable = reach.Row(0)
	f.reachable[f.entry] = 1
	if f.reachable[0] != 0 {
		return nil, fmt.Errorf("function has a path without explicit return")
	}
	f.cycles = make(matrixir.Vector, n)
	for i := range f.cycles {
		f.cycles[i] = closure.At(i, i)
	}
	if err := analyzeFlowState(f, fn); err != nil {
		return nil, &flowSafetyError{err}
	}
	return f, nil
}

func flowSuccessor(m matrixir.Matrix, node int) (int, error) {
	basis, _ := matrixir.MatrixFromRows([][]float64{matrixir.Basis(m.Rows, node)})
	projected, err := basis.Multiply(m)
	if err != nil {
		return 0, err
	}
	next := -1
	for j, v := range projected.Data {
		if v == 0 {
			continue
		}
		if v != 1 || next >= 0 {
			return 0, fmt.Errorf("non-deterministic flow row %d", node)
		}
		next = j
	}
	if next < 0 {
		return 0, fmt.Errorf("missing flow edge at %d", node)
	}
	return next, nil
}

func (g *targetGen) lowerFunctionFlow(f *functionFlow, scope map[string]string) (string, error) {
	if f.stateMachine {
		return g.lowerStateFlow(f, scope)
	}
	budget := 4096 // Joining paths can duplicate continuations; reject expansion blowups.
	var lower func(int, map[string]string) (string, error)
	lower = func(node int, incoming map[string]string) (string, error) {
		budget--
		if budget < 0 {
			return "", fmt.Errorf("function flow expansion exceeds 4096 nodes")
		}
		local := make(map[string]string, len(incoming))
		for k, v := range incoming {
			local[k] = v
		}
		g.bindings = append(g.bindings, local)
		defer func() { g.bindings = g.bindings[:len(g.bindings)-1] }()
		switch x := f.nodes[node].(type) {
		case *ReturnStmt:
			if x.X == nil {
				return targetNull(g.target), nil
			}
			return g.expr(x.X)
		case *IfStmt:
			condition, err := g.expr(x.Cond)
			if err != nil {
				return "", err
			}
			t, err := flowSuccessor(f.T, node)
			if err != nil {
				return "", err
			}
			ff, err := flowSuccessor(f.F, node)
			if err != nil {
				return "", err
			}
			yes, err := lower(t, local)
			if err != nil {
				return "", err
			}
			no, err := lower(ff, local)
			if err != nil {
				return "", err
			}
			return g.conditionalValue(condition, yes, no), nil
		case *AssignStmt, *ExprStmt:
			var value string
			var err error
			var name string
			if a, ok := x.(*AssignStmt); ok {
				value, err = g.expr(a.Value)
				name = g.freshName("local")
				local[g.name(a.Name)] = name
				if g.target == "rust" {
					local[g.name(a.Name)] += ".clone()"
				}
			} else {
				value, err = g.expr(x.(*ExprStmt).X)
				name = g.freshName("effect")
			}
			if err != nil {
				return "", err
			}
			g.cValues[name] = true
			next, err := flowSuccessor(f.A, node)
			if err != nil {
				return "", err
			}
			result, err := lower(next, local)
			if err != nil {
				return "", err
			}
			return g.letExpression([]valueBinding{{name, value}}, result), nil
		default:
			return "", fmt.Errorf("invalid function flow node %d", node)
		}
	}
	return lower(f.entry, scope)
}

// Native conditional expressions evaluate the condition exactly once and only
// the selected value. Multiplying two eager values by 0/1 would be incorrect.
func (g *targetGen) conditionalValue(condition, yes, no string) string {
	c := truthCall(g.target, condition)
	switch g.target {
	case "python":
		return "(" + yes + " if " + c + " else " + no + ")"
	case "go":
		return "func() any { if " + c + " { return " + yes + " }; return " + no + " }()"
	case "rust":
		return "(if " + c + " { " + yes + " } else { " + no + " })"
	case "nim":
		return "(if " + c + ": " + yes + " else: " + no + ")"
	case "kotlin":
		return "(if (" + c + ") " + yes + " else " + no + ")"
	case "zig":
		return "(if (" + c + ") " + yes + " else " + no + ")"
	default:
		return "(" + c + " ? " + yes + " : " + no + ")"
	}
}
