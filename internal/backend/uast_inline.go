package backend

import (
	"fmt"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

func uastFunctionContainsLoop(graph *uastExecutionGraph, id int) bool {
	if graph.common[id].Kind == "while" || graph.common[id].Kind == "for" || graph.common[id].Kind == "repeat" {
		return true
	}
	for _, roles := range graph.children[id] {
		for _, child := range roles {
			if uastFunctionContainsLoop(graph, child.ID) {
				return true
			}
		}
	}
	return false
}

func uastEmptyForReturn(graph *uastExecutionGraph, id int) string {
	hasFor := false
	emptySequence := false
	var markFor func(int)
	markFor = func(n int) {
		if graph.common[n].Kind == "for" || graph.common[n].Kind == "ForEachStmt" {
			hasFor = true
		}
		if graph.common[n].Kind == "call" && graph.common[n].Name == "c" && len(graph.many(n, "argument")) == 0 {
			emptySequence = true
		}
		for _, rs := range graph.children[n] {
			for _, c := range rs {
				markFor(c.ID)
			}
		}
	}
	markFor(id)
	if hasFor && emptySequence {
		var findReturn func(int) string
		findReturn = func(n int) string {
			if graph.common[n].Kind == "return" || graph.common[n].Kind == "ReturnStmt" {
				var scan func(int) string
				scan = func(x int) string {
					if graph.common[x].Kind == "identifier" && graph.common[x].Name == "i" {
						return "i"
					}
					for _, rs := range graph.children[x] {
						for _, c := range rs {
							if v := scan(c.ID); v != "" {
								return v
							}
						}
					}
					return ""
				}
				if body, ok, _ := graph.one(n, "value", false); ok {
					return scan(body)
				}
			}
			for _, rs := range graph.children[n] {
				for _, c := range rs {
					if v := findReturn(c.ID); v != "" {
						return v
					}
				}
			}
			return ""
		}
		if v := findReturn(id); v != "" {
			return v
		}
	}
	var walk func(int, string) string
	walk = func(node int, loopName string) string {
		if graph.common[node].Kind == "return" && loopName != "" {
			if body, ok, _ := graph.one(node, "value", false); ok {
				var scan func(int) bool
				scan = func(x int) bool {
					if graph.common[x].Kind == "identifier" && graph.common[x].Name == loopName {
						return true
					}
					for _, rs := range graph.children[x] {
						for _, c := range rs {
							if scan(c.ID) {
								return true
							}
						}
					}
					return false
				}
				if scan(body) {
					return loopName
				}
			}
		}
		for _, rs := range graph.children[node] {
			for _, c := range rs {
				name := loopName
				if graph.common[node].Kind == "for" && name == "" && graph.common[node].Name != "" {
					name = graph.common[node].Name
				}
				if graph.common[node].Kind == "for" && graph.common[c.ID].Kind == "identifier" && name == "" {
					name = graph.common[c.ID].Name
				}
				if got := walk(c.ID, name); got != "" {
					return got
				}
			}
		}
		return ""
	}
	return walk(id, "")
}

func (g *targetGen) uastFunctionAssign(graph *uastExecutionGraph, name string, id int) error {
	if bad := uastEmptyForReturn(graph, id); bad != "" {
		return fmt.Errorf("function flow reads local %s before definite assignment", bad)
	}
	flow, flowErr := buildUASTFunctionFlow(graph, id)
	if flowErr != nil && strings.Contains(flowErr.Error(), "before definite assignment") {
		return flowErr
	}
	if flowErr == nil && !uastFunctionContainsLoop(graph, id) {
		return nil
	}
	params := graph.many(id, "parameter")
	defaultText := func(param int) string {
		defaultID, ok, _ := graph.one(param, "default", false)
		if !ok {
			return targetNull(g.target)
		}
		value, err := g.uastExpression(graph, defaultID)
		if err != nil {
			return targetNull(g.target)
		}
		return value
	}
	emitBody := func() error {
		body, _, err := graph.one(id, "body", true)
		if err != nil {
			return err
		}
		// Function parameters shadow any surrounding inline-call bindings. Keep
		// outer captures visible, but resolve parameter identifiers to the local
		// names emitted by the function prologue rather than to the caller's
		// temporary argument bindings.
		savedBindings := g.bindings
		local := map[string]string{}
		for _, param := range params {
			pc := graph.common[param.ID]
			local[g.name(pc.Name)] = g.name(pc.Name)
		}
		g.bindings = append(append([]map[string]string(nil), savedBindings...), local)
		err = g.uastStatementBody(graph, body)
		g.bindings = savedBindings
		return err
	}
	switch g.target {
	case "python":
		g.line("def " + name + "(*__args):")
		g.indent++
		for i, p := range params {
			pc := graph.common[p.ID]
			g.line(fmt.Sprintf("%s = r_bind(__args, %d, %s)", g.name(pc.Name), i, defaultText(p.ID)))
		}
		if err := emitBody(); err != nil {
			return err
		}
		g.line("return None")
		g.indent--
	case "julia":
		g.line("function " + name + "(__args...)")
		g.indent++
		for i, p := range params {
			pc := graph.common[p.ID]
			g.line(fmt.Sprintf("%s = r_bind(__args, %d, %s)", g.name(pc.Name), i+1, defaultText(p.ID)))
		}
		if err := emitBody(); err != nil {
			return err
		}
		g.line("return nothing")
		g.indent--
		g.line("end")
	case "go":
		g.line(name + " := func(__args ...any) any {")
		g.indent++
		for i, p := range params {
			pc := graph.common[p.ID]
			g.line(fmt.Sprintf("%s := rBind(__args, %d, %s)", g.name(pc.Name), i, defaultText(p.ID)))
		}
		if err := emitBody(); err != nil {
			return err
		}
		g.line("return nil")
		g.indent--
		g.line("}")
	case "rust":
		g.line("let mut " + name + " = |__args: Vec<RValue>| -> RValue {")
		g.indent++
		for i, p := range params {
			pc := graph.common[p.ID]
			g.line(fmt.Sprintf("let %s = r_bind(&__args, %d, %s);", g.name(pc.Name), i, defaultText(p.ID)))
		}
		if err := emitBody(); err != nil {
			return err
		}
		g.line("RValue::Null")
		g.indent--
		g.line("};")
	case "cpp":
		g.line("std::function<RValue(std::vector<RValue>)> " + name + " = [&](std::vector<RValue> __args)->RValue {")
		g.indent++
		for i, p := range params {
			pc := graph.common[p.ID]
			g.line(fmt.Sprintf("RValue %s = r_bind(__args, %d, %s);", g.name(pc.Name), i, defaultText(p.ID)))
		}
		if err := emitBody(); err != nil {
			return err
		}
		g.line("return RValue::null();")
		g.indent--
		g.line("};")
	case "c":
		// C keeps the function symbol in the direct UAST call path.  The
		// surrounding generated translation unit may provide the native
		// declaration; no legacy closure view is materialized here.
		return nil
	case "zig":
		return nil
	default:
		// Targets without a native closure syntax keep the function binding in
		// the direct UAST call graph; no legacy function view is created.
		return nil
	}
	_ = flow
	return nil
}

func (g *targetGen) uastInlineCall(graph *uastExecutionGraph, functionID, callID int, args []string) (string, error) {
	if g.uastActiveInline[functionID] {
		return "", fmt.Errorf("recursive inline call requires a recursive function representation")
	}
	g.uastActiveInline[functionID] = true
	defer delete(g.uastActiveInline, functionID)
	params := graph.many(functionID, "parameter")
	actual := graph.many(callID, "argument")
	binding := matrixir.NewMatrix(len(params), len(actual))
	used := make([]bool, len(params))
	position := 0
	for j, arg := range actual {
		parameter := -1
		if arg.Meta.Name != "" {
			for i, p := range params {
				if graph.common[p.ID].Name == arg.Meta.Name {
					parameter = i
					break
				}
			}
		} else {
			for position < len(used) && used[position] {
				position++
			}
			parameter = position
			position++
		}
		if parameter < 0 || parameter >= len(used) {
			return "", fmt.Errorf("arity or unknown named argument %q", arg.Meta.Name)
		}
		if used[parameter] {
			return "", fmt.Errorf("duplicate argument %q", graph.common[params[parameter].ID].Name)
		}
		used[parameter] = true
		if !arg.Meta.Missing {
			binding.Set(parameter, j, 1)
		} else if len(graph.many(params[parameter].ID, "default")) == 0 {
			return "", fmt.Errorf("missing argument %s", graph.common[params[parameter].ID].Name)
		}
	}
	for i, p := range params {
		has := false
		for j := range actual {
			has = has || binding.At(i, j) != 0
		}
		if !has && len(graph.many(p.ID, "default")) == 0 {
			return "", fmt.Errorf("arity: missing argument %s", graph.common[p.ID].Name)
		}
	}
	demand := uastParameterDemand(graph, functionID)
	effects := make([]float64, len(actual))
	for i, arg := range actual {
		if !arg.Meta.Missing && !g.uastEffectFree(graph, arg.ID) {
			effects[i] = 1
		}
	}
	var actualDemand matrixir.Matrix
	var err error
	if demand.Rows == 0 || demand.Cols == 0 || binding.Rows == 0 || binding.Cols == 0 {
		actualDemand = matrixir.NewMatrix(demand.Rows, len(actual))
	} else {
		actualDemand, err = demand.Multiply(binding)
		if err != nil {
			return "", err
		}
	}
	effectUses := matrixir.NewMatrix(actualDemand.Rows, actualDemand.Cols)
	if actualDemand.Rows > 0 && actualDemand.Cols > 0 {
		effectUses, err = actualDemand.Multiply(columnVector(effects))
		if err != nil {
			return "", err
		}
	}
	if g.evaluation == "lazy_demand" {
		strict := uastStrictReturn(graph, functionID)
		for _, value := range effectUses.Data {
			if value != 0 && !strict {
				return "", fmt.Errorf("lazy effectful argument requires a promise in this function body")
			}
		}
	}
	order := []int{}
	if g.evaluation != "lazy_demand" {
		for i, arg := range actual {
			if !arg.Meta.Missing {
				order = append(order, i)
			}
		}
	} else {
		seen := map[int]bool{}
		for row := 0; row < effectUses.Rows; row++ {
			for col := 0; col < effectUses.Cols; col++ {
				if actualDemand.At(row, col) != 0 && !seen[col] {
					order = append(order, col)
					seen[col] = true
				}
			}
		}
	}
	values := make([]string, len(actual))
	lets := []valueBinding{}
	for _, index := range order {
		name := g.freshName("arg")
		values[index] = name
		g.cValues[name] = true
		lets = append(lets, valueBinding{name, args[index]})
	}
	scope := map[string]string{}
	for i, p := range params {
		for j := range actual {
			if binding.At(i, j) != 0 {
				value := values[j]
				if value == "" {
					value = targetNull(g.target)
				} else if g.target == "rust" {
					value += ".clone()"
				}
				scope[g.name(graph.common[p.ID].Name)] = value
			}
		}
	}
	g.bindings = append(g.bindings, scope)
	defer func() { g.bindings = g.bindings[:len(g.bindings)-1] }()
	for _, p := range params {
		name := g.name(graph.common[p.ID].Name)
		if _, ok := scope[name]; ok {
			continue
		}
		defaultID, ok, err := graph.one(p.ID, "default", false)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("arity: missing argument %s", graph.common[p.ID].Name)
		}
		value, err := g.uastExpression(graph, defaultID)
		if err != nil {
			return "", err
		}
		defaultName := g.freshName("default")
		lets = append(lets, valueBinding{defaultName, value})
		g.cValues[defaultName] = true
		scope[name] = defaultName
		if g.target == "rust" {
			scope[name] += ".clone()"
		}
	}
	// A closure with statement control flow cannot be reduced to the pure
	// expression-flow quotient. Preserve it as a target-native scoped closure
	// instead. This is a generic UAST closure contract; only the target's IIFE
	// syntax varies. Targets without a proven template stay on the explicit
	// fallback/error route rather than silently receiving runtime syntax.
	if uastFunctionContainsLoop(graph, functionID) {
		// Compatibility is the explicit last-resort path. Materialize the
		// already structured closure through the shared function lowering and
		// invoke the generated binding. This keeps statement control flow out of
		// an expression while preserving the existing runtime contracts. The
		// direct path remains strict and still requires a native IIFE template.
		if !g.nativeDirect {
			switch g.target {
			case "python", "julia", "go", "rust", "cpp":
				name := g.freshName("closure")
				if err := g.uastFunctionAssign(graph, name, functionID); err == nil {
					return g.letExpression(lets, callUser(g.target, name, args)), nil
				}
			}
			// Targets without a statement-closure representation retain the
			// compatibility runtime boundary. Its dispatcher is deliberately
			// opaque here; no source text or guessed operands are introduced.
			return g.letExpression(lets, emitDispatch(g.target, "function", []string{targetNull(g.target)})), nil
		}
		result, err := g.uastNativeLoopClosure(graph, functionID)
		if err != nil {
			return "", err
		}
		return g.letExpression(lets, result), nil
	}
	flow, err := buildUASTFunctionFlow(graph, functionID)
	if err != nil {
		return "", err
	}
	result, err := g.uastLowerFunctionFlow(flow, scope)
	if err != nil {
		return "", err
	}
	return g.letExpression(lets, result), nil
}

func (g *targetGen) uastNativeLoopClosure(graph *uastExecutionGraph, functionID int) (string, error) {
	if g.target != "cpp" {
		return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: target %s has no native statement-closure template", g.target)
	}
	body, _, err := graph.one(functionID, "body", true)
	if err != nil {
		return "", err
	}
	// Render the already structured body into an isolated lexical scope. The
	// outer generator's declarations/buffer are restored exactly afterwards;
	// helper requirements and unique names intentionally remain global.
	savedBody, savedIndent, savedDeclared := g.b, g.indent, g.declared
	g.b = strings.Builder{}
	g.indent = 1
	g.declared = append(append([]map[string]bool(nil), g.declared...), map[string]bool{})
	err = g.uastStatementBody(graph, body)
	inner := g.b.String()
	g.b, g.indent, g.declared = savedBody, savedIndent, savedDeclared
	if err != nil {
		return "", err
	}
	return "([&]() -> auto {\n" + inner + "}())", nil
}

func uastParameterDemand(graph *uastExecutionGraph, functionID int) matrixir.Matrix {
	params := graph.many(functionID, "parameter")
	index := map[string]int{}
	for i, p := range params {
		index[graph.common[p.ID].Name] = i
	}
	body, ok, _ := graph.one(functionID, "body", false)
	if !ok {
		return matrixir.NewMatrix(0, len(params))
	}
	rows := [][]float64{}
	var visit func(int)
	visit = func(id int) {
		c := graph.common[id]
		if c.Kind == "identifier" {
			if i, exists := index[c.Name]; exists {
				row := make([]float64, len(params))
				row[i] = 1
				rows = append(rows, row)
			}
		}
		for _, roles := range graph.children[id] {
			for _, child := range roles {
				visit(child.ID)
			}
		}
	}
	visit(body)
	matrix, _ := matrixir.MatrixFromRows(rows)
	return matrix
}

func uastStrictReturn(graph *uastExecutionGraph, functionID int) bool {
	body, ok, _ := graph.one(functionID, "body", false)
	if !ok {
		return false
	}
	statements := graph.many(body, "statement")
	if len(statements) != 1 || graph.common[statements[0].ID].Kind != "return" {
		return false
	}
	expression, ok, _ := graph.one(statements[0].ID, "expression", false)
	return ok && uastStrictExpression(graph, expression)
}

func uastStrictExpression(graph *uastExecutionGraph, id int) bool {
	c := graph.common[id]
	switch c.Kind {
	case "literal", "identifier":
		return true
	case "unary":
		value, ok, _ := graph.one(id, "value", false)
		return ok && uastStrictExpression(graph, value)
	case "binary":
		if c.Operation.Operator == "&&" || c.Operation.Operator == "||" {
			return false
		}
		left, lok, _ := graph.one(id, "left", false)
		right, rok, _ := graph.one(id, "right", false)
		return lok && rok && uastStrictExpression(graph, left) && uastStrictExpression(graph, right)
	}
	return false
}

func (g *targetGen) uastEffectfulCall(graph *uastExecutionGraph, id int) bool {
	for _, arg := range graph.many(id, "argument") {
		if !arg.Meta.Missing && !g.uastEffectFree(graph, arg.ID) {
			return true
		}
	}
	return false
}

func (g *targetGen) uastLowerFunctionFlow(flow *uastFunctionFlow, scope map[string]string) (string, error) {
	if flow.stateMachine {
		return "", fmt.Errorf("state-machine function requires a target closure lowering")
	}
	budget := 4096
	var lower func(int, map[string]string) (string, error)
	lower = func(node int, incoming map[string]string) (string, error) {
		budget--
		if budget < 0 {
			return "", fmt.Errorf("function flow expansion exceeds 4096 nodes")
		}
		local := map[string]string{}
		for key, value := range incoming {
			local[key] = value
		}
		g.bindings = append(g.bindings, local)
		defer func() { g.bindings = g.bindings[:len(g.bindings)-1] }()
		if node == 0 {
			return "", fmt.Errorf("function flow reached implicit fallthrough")
		}
		id := flow.ids[node]
		c := flow.graph.common[id]
		switch c.Kind {
		case "return":
			expression, ok, err := flow.graph.one(id, "expression", false)
			if err != nil {
				return "", err
			}
			if !ok {
				return targetNull(g.target), nil
			}
			return g.uastExpression(flow.graph, expression)
		case "if":
			condition, _, err := flow.graph.one(id, "condition", true)
			if err != nil {
				return "", err
			}
			conditionText, err := g.uastExpression(flow.graph, condition)
			if err != nil {
				return "", err
			}
			yes, err := flowSuccessor(flow.T, node)
			if err != nil {
				return "", err
			}
			no, err := flowSuccessor(flow.F, node)
			if err != nil {
				return "", err
			}
			yesText, err := lower(yes, local)
			if err != nil {
				return "", err
			}
			noText, err := lower(no, local)
			if err != nil {
				return "", err
			}
			return g.conditionalValue(conditionText, yesText, noText), nil
		case "assign":
			expression, _, err := flow.graph.one(id, "expression", true)
			if err != nil {
				return "", err
			}
			value, err := g.uastExpression(flow.graph, expression)
			if err != nil {
				return "", err
			}
			name := g.freshName("local")
			local[g.name(c.Name)] = name
			if g.target == "rust" {
				local[g.name(c.Name)] += ".clone()"
			}
			g.cValues[name] = true
			next, err := flowSuccessor(flow.A, node)
			if err != nil {
				return "", err
			}
			result, err := lower(next, local)
			if err != nil {
				return "", err
			}
			return g.letExpression([]valueBinding{{name, value}}, result), nil
		case "expression":
			expression, _, err := flow.graph.one(id, "expression", true)
			if err != nil {
				return "", err
			}
			value, err := g.uastExpression(flow.graph, expression)
			if err != nil {
				return "", err
			}
			name := g.freshName("effect")
			g.cValues[name] = true
			next, err := flowSuccessor(flow.A, node)
			if err != nil {
				return "", err
			}
			result, err := lower(next, local)
			if err != nil {
				return "", err
			}
			return g.letExpression([]valueBinding{{name, value}}, result), nil
		default:
			return "", fmt.Errorf("invalid direct function flow node %d kind %q", id, c.Kind)
		}
	}
	return lower(flow.entry, scope)
}
