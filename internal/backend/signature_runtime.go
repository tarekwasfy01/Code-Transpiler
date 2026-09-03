package backend

import "fmt"

// Exact call evaluation resolves the callee before evaluating argument columns.
// Values are then placed using the derived binding matrix; no argument is run twice.
func (st *runState) exactCall(env *runEnv, call *CallExpr) (any, error) {
	var callee any
	primitive := ""
	if id, ok := call.Fun.(*IdentExpr); ok {
		var found bool
		callee, found = env.get(id.Name)
		if !found {
			primitive = id.Name
		}
	} else {
		var err error
		callee, err = st.expr(env, call.Fun)
		if err != nil {
			return nil, err
		}
	}
	args := make([]any, len(call.Args))
	names := make([]string, len(call.Args))
	for i, arg := range call.Args {
		if arg.Missing {
			return nil, fmt.Errorf("missing/spread arguments require explicit expansion before exact call")
		}
		value, err := st.expr(env, arg.Value)
		if err != nil {
			return nil, err
		}
		args[i] = value
		names[i] = arg.Name
	}
	if primitive != "" {
		return st.primitive(primitive, args, names)
	}
	if fn, ok := callee.(*runFunction); ok {
		return st.callFunction(fn, args, names)
	}
	return nil, fmt.Errorf("attempt to call non-function")
}

func (st *runState) callExactFunction(fn *runFunction, args []any, names []string) (any, error) {
	params := make([]SignatureParameter, len(fn.params))
	columns := make([]SignatureArgument, len(args))
	for i, p := range fn.params {
		params[i] = SignatureParameter{Name: p.Name, Passing: p.Mode, HasDefault: p.Default != nil}
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
		switch p.Mode {
		case "variadic_positional":
			value = []any{}
		case "variadic_keyword":
			value = map[string]any{}
		}
		for col := range args {
			if binding.ParameterArguments.At(row, col) == 0 {
				continue
			}
			switch p.Mode {
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
				value, err = st.expr(env, p.Default)
				if err != nil {
					return nil, err
				}
			}
		}
		if p.Type != nil {
			if _, err := evaluateInteger(SemanticOperation{Name: "integer.value", Type: *p.Type}, []any{value}); err != nil {
				return nil, fmt.Errorf("parameter %s: %w", p.Name, err)
			}
		}
		env.set(p.Name, value)
	}
	value, signal, err := st.block(env, fn.body)
	if err != nil {
		return nil, err
	}
	if signal == runBreak || signal == runNext {
		return nil, fmt.Errorf("loop control escaped function")
	}
	return value, nil
}
