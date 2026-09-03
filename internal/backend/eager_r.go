package backend

import (
	"fmt"
	"strings"
)

// Recognize the semantics of actual force-wrapper bodies, not their names or
// a source comment. Changing a force, order, return, arity or argument mapping
// prevents recognition. Wrapper references as values/shadowed bindings reject.
func eagerWrapper(fn *FunctionExpr) ([]string, bool) {
	if len(fn.Params) < 2 || len(fn.Body.List) != len(fn.Params)+1 || fn.Params[0].Name != "r2m_fun" {
		return nil, false
	}
	for i, p := range fn.Params {
		if p.Default != nil {
			return nil, false
		}
		if i > 0 && p.Name != fmt.Sprintf("r2m_value_%d", i-1) {
			return nil, false
		}
		s, ok := fn.Body.List[i].(*ExprStmt)
		if !ok {
			return nil, false
		}
		c, ok := s.X.(*CallExpr)
		if !ok || len(c.Args) != 1 {
			return nil, false
		}
		id, ok := c.Fun.(*IdentExpr)
		if !ok || id.Name != "force" {
			return nil, false
		}
		v, ok := c.Args[0].Value.(*IdentExpr)
		if !ok || v.Name != p.Name || c.Args[0].Name != "" || c.Args[0].Missing {
			return nil, false
		}
	}
	r, ok := fn.Body.List[len(fn.Params)].(*ReturnStmt)
	if !ok {
		return nil, false
	}
	c, ok := r.X.(*CallExpr)
	if !ok || len(c.Args) != len(fn.Params)-1 {
		return nil, false
	}
	id, ok := c.Fun.(*IdentExpr)
	if !ok || id.Name != "r2m_fun" {
		return nil, false
	}
	names := make([]string, len(c.Args))
	for i, a := range c.Args {
		v, ok := a.Value.(*IdentExpr)
		if !ok || a.Missing || v.Name != fn.Params[i+1].Name {
			return nil, false
		}
		names[i] = a.Name
	}
	return names, true
}
func decodeEagerR(body *BlockStmt) error {
	helpers := map[string][]string{}
	defs := map[*AssignStmt]bool{}
	for _, s := range body.List {
		a, ok := s.(*AssignStmt)
		if !ok || !strings.HasPrefix(a.Name, "r2m_eager_") {
			continue
		}
		fn, ok := a.Value.(*FunctionExpr)
		if !ok {
			continue
		}
		names, ok := eagerWrapper(fn)
		if !ok {
			continue
		}
		if _, exists := helpers[a.Name]; exists {
			return fmt.Errorf("DECODE_EVALUATION: duplicate eager helper")
		}
		helpers[a.Name] = names
		defs[a] = true
	}
	if len(helpers) == 0 {
		return nil
	}
	var expr func(Expr) (Expr, error)
	var stmt func(Stmt) error
	invalidBinding := func(name string) bool { _, helper := helpers[name]; return helper || name == "force" }
	expr = func(v Expr) (Expr, error) {
		switch x := v.(type) {
		case *IdentExpr:
			if _, ok := helpers[x.Name]; ok {
				return nil, fmt.Errorf("DECODE_EVALUATION: eager helper used as value")
			}
		case *UnaryExpr:
			a, e := expr(x.X)
			x.X = a
			return x, e
		case *BinaryExpr:
			a, e := expr(x.L)
			if e != nil {
				return nil, e
			}
			b, e := expr(x.R)
			x.L, x.R = a, b
			return x, e
		case *CallExpr:
			if id, ok := x.Fun.(*IdentExpr); ok {
				if names, ok := helpers[id.Name]; ok {
					if len(x.Args) != len(names)+1 {
						return nil, fmt.Errorf("DECODE_EVALUATION: wrapper arity")
					}
					values := make([]Expr, len(x.Args))
					for i, a := range x.Args {
						if a.Missing || a.Name != "" {
							return nil, fmt.Errorf("DECODE_EVALUATION: wrapper arguments")
						}
						v, e := expr(a.Value)
						if e != nil {
							return nil, e
						}
						values[i] = v
					}
					args := make([]Arg, len(names))
					for i, name := range names {
						args[i] = Arg{Name: name, Value: values[i+1]}
					}
					return &CallExpr{Fun: values[0], Args: args, Eager: true}, nil
				}
			}
			fn, e := expr(x.Fun)
			if e != nil {
				return nil, e
			}
			x.Fun = fn
			for i, a := range x.Args {
				v, e := expr(a.Value)
				if e != nil {
					return nil, e
				}
				x.Args[i].Value = v
			}
		case *IndexExpr:
			a, e := expr(x.X)
			if e != nil {
				return nil, e
			}
			x.X = a
			for i, a := range x.Args {
				v, e := expr(a.Value)
				if e != nil {
					return nil, e
				}
				x.Args[i].Value = v
			}
		case *FunctionExpr:
			for i, p := range x.Params {
				if invalidBinding(p.Name) {
					return nil, fmt.Errorf("DECODE_EVALUATION: helper or force shadowed by parameter")
				}
				v, e := expr(p.Default)
				if e != nil {
					return nil, e
				}
				x.Params[i].Default = v
			}
			if e := stmt(x.Body); e != nil {
				return nil, e
			}
		}
		return v, nil
	}
	stmt = func(s Stmt) error {
		switch x := s.(type) {
		case *BlockStmt:
			for _, s := range x.List {
				if e := stmt(s); e != nil {
					return e
				}
			}
		case *AssignStmt:
			if invalidBinding(x.Name) {
				return fmt.Errorf("DECODE_EVALUATION: helper or force rebound")
			}
			v, e := expr(x.Value)
			x.Value = v
			return e
		case *ExprStmt:
			v, e := expr(x.X)
			x.X = v
			return e
		case *ReturnStmt:
			v, e := expr(x.X)
			x.X = v
			return e
		case *IfStmt:
			v, e := expr(x.Cond)
			if e != nil {
				return e
			}
			x.Cond = v
			if e = stmt(x.Then); e != nil {
				return e
			}
			return stmt(x.Else)
		case *WhileStmt:
			v, e := expr(x.Cond)
			if e != nil {
				return e
			}
			x.Cond = v
			return stmt(x.Body)
		case *ForStmt:
			if invalidBinding(x.Name) {
				return fmt.Errorf("DECODE_EVALUATION: helper or force rebound by loop")
			}
			v, e := expr(x.Seq)
			if e != nil {
				return e
			}
			x.Seq = v
			return stmt(x.Body)
		case *RepeatStmt:
			return stmt(x.Body)
		}
		return nil
	}
	out := make([]Stmt, 0, len(body.List))
	for _, s := range body.List {
		if a, ok := s.(*AssignStmt); ok && defs[a] {
			continue
		}
		if err := stmt(s); err != nil {
			return err
		}
		out = append(out, s)
	}
	body.List = out
	return nil
}
