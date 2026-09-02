package backend

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

type runUASTParameter struct {
	name        string
	mode        string
	passing     string
	typ         *SemanticType
	defaultNode int
}

type runUASTFunction struct {
	binding           string
	defaultEvaluation string
	defaults          []any
	params            []runUASTParameter
	body              int
	graph             *uastExecutionGraph
	env               *runEnv
}

func (st *runState) uastBlock(env *runEnv, g *uastExecutionGraph, id int) (last any, signal runSignal, runErr error) {
	if g.common[id].Kind != "block" {
		return nil, runNormal, fmt.Errorf("embedded UAST runtime: node %d is not a block", id)
	}
	deferredStart := len(st.deferred)
	defer func() {
		cleanupValue, cleanupSignal, cleanupErr := st.runDeferred(deferredStart)
		if cleanupErr != nil {
			last, signal, runErr = cleanupValue, cleanupSignal, cleanupErr
			return
		}
		if cleanupSignal != runNormal {
			last, signal, runErr = cleanupValue, cleanupSignal, fmt.Errorf("deferred cleanup escaped its block")
		}
	}()
	items := g.many(id, "statement")
	positions := map[int]int{}
	for i, child := range items {
		positions[child.ID] = i
	}
	for pos := 0; pos < len(items); {
		child := items[pos]
		if err := st.tick(); err != nil {
			return nil, runNormal, err
		}
		value, signal, err := st.uastStmt(env, g, child.ID)
		if err != nil {
			return nil, runNormal, err
		}
		last = value
		if signal != runNormal {
			return value, signal, nil
		}
		if st.jumpTarget >= 0 {
			target := st.jumpTarget
			next, ok := positions[target]
			st.jumpTarget = -1
			if !ok {
				return nil, runNormal, fmt.Errorf("goto target node %d is outside its lexical block", target)
			}
			pos = next
			continue
		}
		pos++
	}
	return last, runNormal, nil
}

func (st *runState) uastStmt(env *runEnv, g *uastExecutionGraph, id int) (any, runSignal, error) {
	c := g.common[id]
	child := func(role string, required bool) (int, bool, error) { return g.one(id, role, required) }
	switch c.Kind {
	case "block":
		return st.uastBlock(env, g, id)
	case "expression":
		x, _, err := child("expression", true)
		if err != nil {
			return nil, runNormal, err
		}
		value, err := st.uastExpr(env, g, x)
		return value, runNormal, err
	case "assign":
		x, _, err := child("expression", true)
		if err != nil {
			return nil, runNormal, err
		}
		value, err := st.uastExpr(env, g, x)
		if err != nil {
			return nil, runNormal, err
		}
		if err := env.assign(c.Name, value); err != nil {
			return nil, runNormal, err
		}
		return value, runNormal, nil
	case "if":
		condition, _, err := child("condition", true)
		if err != nil {
			return nil, runNormal, err
		}
		value, err := st.uastExpr(env, g, condition)
		if err != nil {
			return nil, runNormal, err
		}
		truth, err := runTruth(value)
		if err != nil {
			return nil, runNormal, err
		}
		if truth {
			then, _, err := g.oneRelationNode(id, "control.true", true)
			if err != nil {
				return nil, runNormal, err
			}
			return st.uastStmt(env, g, then)
		}
		if other, ok, err := g.oneRelationNode(id, "control.false", false); err != nil {
			return nil, runNormal, err
		} else if ok {
			return st.uastStmt(env, g, other)
		}
		return nil, runNormal, nil
	case "while":
		condition, _, err := child("condition", true)
		if err != nil {
			return nil, runNormal, err
		}
		body, _, err := child("body", true)
		if err != nil {
			return nil, runNormal, err
		}
		var last any
		for {
			if err := st.tick(); err != nil {
				return nil, runNormal, err
			}
			value, err := st.uastExpr(env, g, condition)
			if err != nil {
				return nil, runNormal, err
			}
			truth, err := runTruth(value)
			if err != nil {
				return nil, runNormal, err
			}
			if !truth {
				return last, runNormal, nil
			}
			value, signal, err := st.uastStmt(env, g, body)
			if err != nil {
				return nil, runNormal, err
			}
			last = value
			if signal == runBreak {
				return last, runNormal, nil
			}
			if signal == runReturn {
				return value, signal, nil
			}
		}
	case "for":
		sequence, _, err := child("sequence", true)
		if err != nil {
			return nil, runNormal, err
		}
		body, _, err := child("body", true)
		if err != nil {
			return nil, runNormal, err
		}
		value, err := st.uastExpr(env, g, sequence)
		if err != nil {
			return nil, runNormal, err
		}
		var last any
		for _, item := range runVec(value) {
			env.set(c.Name, item)
			value, signal, err := st.uastStmt(env, g, body)
			if err != nil {
				return nil, runNormal, err
			}
			last = value
			if signal == runBreak {
				return last, runNormal, nil
			}
			if signal == runReturn {
				return value, signal, nil
			}
		}
		return last, runNormal, nil
	case "repeat":
		body, _, err := child("body", true)
		if err != nil {
			return nil, runNormal, err
		}
		for {
			if err := st.tick(); err != nil {
				return nil, runNormal, err
			}
			value, signal, err := st.uastStmt(env, g, body)
			if err != nil {
				return nil, runNormal, err
			}
			if signal == runBreak {
				return value, runNormal, nil
			}
			if signal == runReturn {
				return value, signal, nil
			}
		}
	case "return":
		if x, ok, err := child("expression", false); err != nil {
			return nil, runNormal, err
		} else if ok {
			value, err := st.uastExpr(env, g, x)
			return value, runReturn, err
		}
		return nil, runReturn, nil
	case "break":
		return nil, runBreak, nil
	case "continue":
		return nil, runNext, nil
	default:
		return st.uastPrimitiveStatement(env, g, id)
	}
}

func (st *runState) uastExpr(env *runEnv, g *uastExecutionGraph, id int) (any, error) {
	c := g.common[id]
	one := func(role string) (int, error) {
		q, _, err := g.one(id, role, true)
		return q, err
	}
	switch c.Kind {
	case "identifier":
		switch c.Name {
		case "TRUE", "T":
			return true, nil
		case "FALSE", "F":
			return false, nil
		case "NULL":
			return nil, nil
		case "NA", "NA_real_", "NA_integer_", "NA_complex_", "NaN":
			return math.NaN(), nil
		case "NA_character_":
			return nil, nil
		case "Inf":
			return math.Inf(1), nil
		case "pi":
			return math.Pi, nil
		}
		if value, ok := env.get(c.Name); ok {
			return value, nil
		}
		return nil, fmt.Errorf("object %q not found", c.Name)
	case "literal":
		kind, text := c.Operation.LiteralKind, c.Operation.Text
		switch kind {
		case "null":
			return nil, nil
		case "na", "nan":
			return math.NaN(), nil
		case "boolean":
			return text == "TRUE" || text == "T", nil
		case "string":
			return unquote(text), nil
		}
		number := strings.TrimSuffix(text, "L")
		if strings.HasSuffix(number, "i") {
			return complex(0, runNum(strings.TrimSuffix(number, "i"))), nil
		}
		value, err := strconv.ParseFloat(number, 64)
		if err != nil {
			return nil, err
		}
		return value, nil
	case "typed_operation":
		if c.Operation.Typed == nil {
			return nil, fmt.Errorf("typed operation node %d lacks operation", id)
		}
		values := make([]any, len(g.many(id, "argument")))
		for i, arg := range g.many(id, "argument") {
			value, err := st.uastExpr(env, g, arg.ID)
			if err != nil {
				return nil, err
			}
			values[i] = value
		}
		return evaluateInteger(*c.Operation.Typed, values)
	case "unary":
		x, err := one("value")
		if err != nil {
			return nil, err
		}
		value, err := st.uastExpr(env, g, x)
		if err != nil {
			return nil, err
		}
		switch c.Operation.Operator {
		case "+":
			return runMap(value, func(q any) any { return runNumber(q) }), nil
		case "-":
			return runMap(value, func(q any) any { return -runNumber(q) }), nil
		case "!":
			return runMap(value, func(q any) any { b, _ := runTruth(q); return !b }), nil
		case "~":
			return value, nil
		}
		return nil, fmt.Errorf("unsupported operator %q", c.Operation.Operator)
	case "binary":
		left, err := one("left")
		if err != nil {
			return nil, err
		}
		a, err := st.uastExpr(env, g, left)
		if err != nil {
			return nil, err
		}
		if value, ok := a.(bool); ok {
			if c.Operation.Operator == "&&" && !value {
				return false, nil
			}
			if c.Operation.Operator == "||" && value {
				return true, nil
			}
		}
		right, err := one("right")
		if err != nil {
			return nil, err
		}
		b, err := st.uastExpr(env, g, right)
		if err != nil {
			return nil, err
		}
		return runBinary(c.Operation.Operator, a, b)
	case "index":
		valueNode, err := one("value")
		if err != nil {
			return nil, err
		}
		value, err := st.uastExpr(env, g, valueNode)
		if err != nil {
			return nil, err
		}
		args := g.many(id, "argument")
		if len(args) == 0 {
			return value, nil
		}
		index, err := st.uastExpr(env, g, args[0].ID)
		if err != nil {
			return nil, err
		}
		return runSubset(value, index, c.Operation.DoubleIndex), nil
	case "function":
		body, err := one("body")
		if err != nil {
			return nil, err
		}
		fn := &runUASTFunction{binding: c.Operation.FunctionBinding, defaultEvaluation: c.Operation.DefaultEvaluation, body: body, graph: g, env: env}
		for _, item := range g.many(id, "parameter") {
			p := g.common[item.ID]
			param := runUASTParameter{name: p.Name, mode: p.Operation.ParameterMode, passing: p.Operation.ParameterPassing, defaultNode: -1}
			if p.Operation.ParameterPassing == "value" {
				typ := p.Type
				param.typ = &typ
			}
			if q, ok, err := g.one(item.ID, "default", false); err != nil {
				return nil, err
			} else if ok {
				param.defaultNode = q
			}
			fn.params = append(fn.params, param)
		}
		fn.defaults = make([]any, len(fn.params))
		if fn.binding == "exact_v1" && fn.defaultEvaluation == "definition" {
			for i, p := range fn.params {
				if p.defaultNode < 0 {
					continue
				}
				value, err := st.uastExpr(env, g, p.defaultNode)
				if err != nil {
					return nil, err
				}
				fn.defaults[i] = value
			}
		}
		return fn, nil
	case "call":
		return st.uastCall(env, g, id)
	case "iteration":
		return nil, fmt.Errorf("embedded UAST runtime: iteration expression requires backend lowering")
	case "missing_argument":
		return nil, fmt.Errorf("missing argument cannot be evaluated as a value")
	default:
		return st.uastPrimitiveExpression(env, g, id)
	}
}

func (st *runState) uastCall(env *runEnv, g *uastExecutionGraph, id int) (any, error) {
	c := g.common[id]
	calleeNode, _, err := g.oneRelationNode(id, "call.calls", true)
	if err != nil {
		return nil, err
	}
	resolvedName := ""
	if resolution := c.Operation.CallResolution; resolution != nil {
		selected := *resolution.Selected
		resolvedName = resolution.Candidates[selected].Declaration
		if resolvedName == "" {
			resolvedName = resolution.Candidates[selected].Name
		}
	}
	if st.exactCalls {
		return st.uastExactCall(env, g, calleeNode, resolvedName, g.many(id, "argument"))
	}
	args, names, err := st.uastArguments(env, g, g.many(id, "argument"), true)
	if err != nil {
		return nil, err
	}
	if resolvedName != "" {
		return st.uastNamedCall(env, resolvedName, args, names)
	}
	callee := g.common[calleeNode]
	if callee.Kind == "identifier" {
		return st.uastNamedCall(env, callee.Name, args, names)
	}
	value, err := st.uastExpr(env, g, calleeNode)
	if err != nil {
		return nil, err
	}
	if fn, ok := value.(*runUASTFunction); ok {
		return st.callUASTFunction(fn, args, names)
	}
	return nil, fmt.Errorf("attempt to call non-function")
}

func (st *runState) uastArguments(env *runEnv, g *uastExecutionGraph, items []universalChild, allowMissing bool) ([]any, []string, error) {
	args, names := make([]any, len(items)), make([]string, len(items))
	for i, item := range items {
		names[i] = item.Meta.Name
		if item.Meta.Missing {
			if !allowMissing {
				return nil, nil, fmt.Errorf("missing/spread arguments require explicit expansion before exact call")
			}
			continue
		}
		value, err := st.uastExpr(env, g, item.ID)
		if err != nil {
			return nil, nil, err
		}
		args[i] = value
	}
	return args, names, nil
}

func (st *runState) uastNamedCall(env *runEnv, name string, args []any, names []string) (any, error) {
	if value, ok := env.get(name); ok {
		if fn, ok := value.(*runUASTFunction); ok {
			return st.callUASTFunction(fn, args, names)
		}
	}
	return st.primitive(name, args, names)
}

func (st *runState) uastExactCall(env *runEnv, g *uastExecutionGraph, calleeNode int, resolvedName string, items []universalChild) (any, error) {
	var callee any
	primitive := ""
	if resolvedName != "" {
		var ok bool
		callee, ok = env.get(resolvedName)
		if !ok {
			primitive = resolvedName
		}
	} else if c := g.common[calleeNode]; c.Kind == "identifier" {
		var ok bool
		callee, ok = env.get(c.Name)
		if !ok {
			primitive = c.Name
		}
	} else {
		var err error
		callee, err = st.uastExpr(env, g, calleeNode)
		if err != nil {
			return nil, err
		}
	}
	args, names, err := st.uastArguments(env, g, items, false)
	if err != nil {
		return nil, err
	}
	if primitive != "" {
		return st.primitive(primitive, args, names)
	}
	if fn, ok := callee.(*runUASTFunction); ok {
		return st.callUASTFunction(fn, args, names)
	}
	return nil, fmt.Errorf("attempt to call non-function")
}

func (st *runState) callUASTFunction(fn *runUASTFunction, args []any, names []string) (any, error) {
	if fn.binding == "exact_v1" {
		return st.callExactUASTFunction(fn, args, names)
	}
	env := newRunEnv(fn.env)
	used := make([]bool, len(args))
	for i, p := range fn.params {
		var value any
		found := false
		for j, name := range names {
			if !used[j] && name == p.name {
				value, used[j], found = args[j], true, true
				break
			}
		}
		if !found && i < len(args) && !used[i] && names[i] == "" {
			value, used[i], found = args[i], true, true
		}
		if !found && p.defaultNode >= 0 {
			var err error
			value, err = st.uastExpr(env, fn.graph, p.defaultNode)
			if err != nil {
				return nil, err
			}
			found = true
		}
		if !found {
			value = nil
		}
		if p.typ != nil {
			if _, err := evaluateInteger(SemanticOperation{Name: "integer.value", Type: *p.typ}, []any{value}); err != nil {
				return nil, fmt.Errorf("parameter %s: %w", p.name, err)
			}
		}
		env.set(p.name, value)
	}
	value, signal, err := st.uastStmt(env, fn.graph, fn.body)
	if err != nil {
		return nil, err
	}
	if signal == runBreak || signal == runNext {
		return nil, fmt.Errorf("loop control escaped function")
	}
	return value, nil
}

func (st *runState) callExactUASTFunction(fn *runUASTFunction, args []any, names []string) (any, error) {
	params := make([]SignatureParameter, len(fn.params))
	columns := make([]SignatureArgument, len(args))
	for i, p := range fn.params {
		params[i] = SignatureParameter{Name: p.name, Passing: p.mode, HasDefault: p.defaultNode >= 0}
	}
	for i, name := range names {
		columns[i] = SignatureArgument{Name: name}
	}
	binding, err := BindSignature(params, columns)
	if err != nil {
		return nil, err
	}
	env := newRunEnv(fn.env)
	for row, p := range fn.params {
		var value any
		switch p.mode {
		case "variadic_positional":
			value = []any{}
		case "variadic_keyword":
			value = map[string]any{}
		}
		for col := range args {
			if binding.ParameterArguments.At(row, col) == 0 {
				continue
			}
			switch p.mode {
			case "variadic_positional":
				value = append(value.([]any), args[col])
			case "variadic_keyword":
				value.(map[string]any)[names[col]] = args[col]
			default:
				value = args[col]
			}
		}
		if binding.UseDefaults[row] != 0 {
			if fn.defaultEvaluation == "definition" {
				value = fn.defaults[row]
			} else {
				value, err = st.uastExpr(env, fn.graph, p.defaultNode)
				if err != nil {
					return nil, err
				}
			}
		}
		if p.typ != nil {
			if _, err := evaluateInteger(SemanticOperation{Name: "integer.value", Type: *p.typ}, []any{value}); err != nil {
				return nil, fmt.Errorf("parameter %s: %w", p.name, err)
			}
		}
		env.set(p.name, value)
	}
	value, signal, err := st.uastStmt(env, fn.graph, fn.body)
	if err != nil {
		return nil, err
	}
	if signal == runBreak || signal == runNext {
		return nil, fmt.Errorf("loop control escaped function")
	}
	return value, nil
}
