package backend

import (
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"strings"
)

func columnVector(values []float64) matrixir.Matrix {
	m := matrixir.NewMatrix(len(values), 1)
	copy(m.Data, values)
	return m
}

// Each row represents an occurrence of a parameter in the function body.
// Multiplication by the argument effect vector exposes every unsafe textual
// substitution, including repeated use. Zero columns preserve unused args.
func parameterDemand(fn *FunctionExpr) matrixir.Matrix {
	var identifiers []string
	var visit func(Expr)
	visit = func(e Expr) {
		switch x := e.(type) {
		case *OperationExpr:
			for _, operand := range x.Operands {
				visit(operand)
			}
		case *IdentExpr:
			identifiers = append(identifiers, x.Name)
		case *UnaryExpr:
			visit(x.X)
		case *BinaryExpr:
			visit(x.L)
			visit(x.R)
		case *CallExpr:
			for _, a := range x.Args {
				visit(a.Value)
			}
		case *IndexExpr:
			visit(x.X)
			for _, a := range x.Args {
				visit(a.Value)
			}
		}
	}
	var statement func(Stmt)
	statement = func(s Stmt) {
		switch x := s.(type) {
		case *BlockStmt:
			for _, child := range x.List {
				statement(child)
			}
		case *IfStmt:
			visit(x.Cond)
			statement(x.Then)
			statement(x.Else)
		case *WhileStmt:
			visit(x.Cond)
			statement(x.Body)
		case *ForStmt:
			visit(x.Seq)
			statement(x.Body)
		case *ExprStmt:
			visit(x.X)
		case *ReturnStmt:
			visit(x.X)
		case *AssignStmt:
			visit(x.Value)
		}
	}
	statement(fn.Body)
	m := matrixir.NewMatrix(len(identifiers), len(fn.Params))
	for i, id := range identifiers {
		for j, p := range fn.Params {
			if p.Name == id {
				m.Set(i, j, 1)
			}
		}
	}
	return m
}

func (g *targetGen) effectFree(e Expr, active map[*FunctionExpr]bool) bool {
	switch x := e.(type) {
	case *OperationExpr:
		// Type checks can fail; conservatively preserve operation ordering.
		return x.Operation.Name == "integer.literal"
	case *LiteralExpr, *IdentExpr:
		return true
	case *UnaryExpr:
		return g.effectFree(x.X, active)
	case *BinaryExpr:
		return g.effectFree(x.L, active) && g.effectFree(x.R, active)
	case *IndexExpr:
		if !g.effectFree(x.X, active) {
			return false
		}
		for _, a := range x.Args {
			if !g.effectFree(a.Value, active) {
				return false
			}
		}
		return true
	case *CallExpr:
		for _, a := range x.Args {
			if !g.effectFree(a.Value, active) {
				return false
			}
		}
		id, ok := x.Fun.(*IdentExpr)
		if !ok {
			return false
		}
		if fn, ok := g.inline[g.name(id.Name)]; ok {
			if active[fn] || len(fn.Body.List) != 1 {
				return false
			}
			r, ok := fn.Body.List[0].(*ReturnStmt)
			if !ok {
				return false
			}
			active[fn] = true
			result := g.effectFree(r.X, active)
			delete(active, fn)
			return result
		}
		switch id.Name {
		case "c", "list", "length":
			return true
		}
	}
	return false
}

// Sequence expressions keep effects inside their original evaluation context;
// nothing is hoisted across a condition or loop. Order is explicit, including C.
func (g *targetGen) sequenceExpression(parts []string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	prefix, last := parts[:len(parts)-1], parts[len(parts)-1]
	switch g.target {
	case "python":
		return "(" + strings.Join(parts, ", ") + ")[-1]"
	case "c", "cpp":
		return "(" + strings.Join(parts, ", ") + ")"
	case "go":
		statements := make([]string, len(prefix))
		for i, expression := range prefix {
			statements[i] = exprStmt("go", expression)
		}
		return "func() any { " + strings.Join(statements, " ") + " return " + last + " }()"
	case "rust":
		return "{ " + strings.Join(prefix, "; ") + "; " + last + " }"
	case "java":
		return "((java.util.function.Supplier<Object>)(() -> { " + strings.Join(prefix, "; ") + "; return " + last + "; })).get()"
	case "csharp":
		return "((Func<object>)(() => { " + strings.Join(prefix, "; ") + "; return " + last + "; }))()"
	case "julia":
		return "(begin " + strings.Join(prefix, "; ") + "; " + last + " end)"
	case "kotlin":
		return "run { " + strings.Join(prefix, "; ") + "; " + last + " }"
	case "swift":
		return "{ () -> Any in _ = " + strings.Join(prefix, "; _ = ") + "; return " + last + " }()"
	case "zig":
		return "blk: { _ = " + strings.Join(prefix, "; _ = ") + "; break :blk " + last + "; }"
	case "nim":
		return "(block: discard " + strings.Join(prefix, "; discard ") + "; " + last + ")"
	}
	return last
}
