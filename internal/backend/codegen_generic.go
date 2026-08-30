package backend

import (
	"fmt"
	"strings"
)

type targetGen struct {
	target   string
	indent   int
	b        strings.Builder
	declared []map[string]bool
	funcs    map[string]bool
	temp     int
}

func generateTarget(target string, ast *BlockStmt) (string, error) {
	g := &targetGen{target: target, declared: []map[string]bool{{}}, funcs: map[string]bool{}}
	for _, s := range ast.List {
		if a, ok := s.(*AssignStmt); ok {
			if _, ok := a.Value.(*FunctionExpr); ok {
				g.funcs[safeName(a.Name)] = true
			}
		}
	}
	switch target {
	case "python", "julia", "nim", "swift":
		for _, s := range ast.List {
			if err := g.stmt(s); err != nil {
				return "", err
			}
		}
		return targetPrelude(target) + "\n" + g.b.String(), nil
	default:
		g.line(mainOpen(target))
		g.indent++
		for _, s := range ast.List {
			if err := g.stmt(s); err != nil {
				return "", err
			}
		}
		if target == "cpp" || target == "c" {
			g.line("return 0;")
		}
		g.indent--
		g.line(mainClose(target))
		return targetPrelude(target) + "\n" + g.b.String(), nil
	}
}
func (g *targetGen) line(s string) {
	g.b.WriteString(strings.Repeat(indentUnit(g.target), g.indent))
	g.b.WriteString(s)
	g.b.WriteByte('\n')
}
func indentUnit(t string) string {
	if t == "python" {
		return "    "
	}
	return "    "
}
func mainOpen(t string) string {
	switch t {
	case "go":
		return "func main() {"
	case "rust":
		return "fn main() {"
	case "cpp":
		return "int main() {"
	case "c":
		return "int main(void) {"
	case "zig":
		return "pub fn main() !void {"
	case "csharp":
		return "class Program { static void Main() {"
	case "java":
		return "public class Main { public static void main(String[] args) {"
	case "kotlin":
		return "fun main() {"
	}
	return ""
}
func mainClose(t string) string {
	switch t {
	case "csharp", "java":
		return "} }"
	default:
		return "}"
	}
}

func (g *targetGen) stmt(s Stmt) error {
	switch x := s.(type) {
	case *BlockStmt:
		if g.target == "python" {
			for _, ss := range x.List {
				if err := g.stmt(ss); err != nil {
					return err
				}
			}
			return nil
		}
		g.line("{")
		g.indent++
		for _, ss := range x.List {
			if err := g.stmt(ss); err != nil {
				return err
			}
		}
		g.indent--
		g.line("}")
	case *AssignStmt:
		n := safeName(x.Name)
		if fn, ok := x.Value.(*FunctionExpr); ok {
			return g.functionAssign(n, fn)
		}
		e, err := g.expr(x.Value)
		if err != nil {
			return err
		}
		g.line(assignSyntax(g.target, n, e))
	case *ExprStmt:
		e, err := g.expr(x.X)
		if err != nil {
			return err
		}
		g.line(exprStmt(g.target, e))
	case *IfStmt:
		c, err := g.expr(x.Cond)
		if err != nil {
			return err
		}
		switch g.target {
		case "python":
			g.line("if r_truth(" + c + "):")
			g.indent++
			if err = g.stmtBody(x.Then); err != nil {
				return err
			}
			g.indent--
			if x.Else != nil {
				g.line("else:")
				g.indent++
				if err = g.stmtBody(x.Else); err != nil {
					return err
				}
				g.indent--
			}
		case "julia":
			g.line("if r_truth(" + c + ")")
			g.indent++
			if err = g.stmtBody(x.Then); err != nil {
				return err
			}
			g.indent--
			if x.Else != nil {
				g.line("else")
				g.indent++
				if err = g.stmtBody(x.Else); err != nil {
					return err
				}
				g.indent--
			}
			g.line("end")
		case "nim":
			g.line("if rTruth(" + c + "):")
			g.indent++
			if err = g.stmtBody(x.Then); err != nil {
				return err
			}
			g.indent--
			if x.Else != nil {
				g.line("else:")
				g.indent++
				if err = g.stmtBody(x.Else); err != nil {
					return err
				}
				g.indent--
			}
		default:
			g.line("if (" + truthCall(g.target, c) + ")")
			if err = g.stmt(x.Then); err != nil {
				return err
			}
			if x.Else != nil {
				g.line("else")
				return g.stmt(x.Else)
			}
		}
	case *WhileStmt:
		c, err := g.expr(x.Cond)
		if err != nil {
			return err
		}
		if g.target == "python" {
			g.line("while r_truth(" + c + "):")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			return err
		}
		if g.target == "julia" {
			g.line("while r_truth(" + c + ")")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("end")
			return err
		}
		if g.target == "nim" {
			g.line("while rTruth(" + c + "):")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			return err
		}
		g.line("while (" + truthCall(g.target, c) + ")")
		return g.stmt(x.Body)
	case *ForStmt:
		seq, err := g.expr(x.Seq)
		if err != nil {
			return err
		}
		n := safeName(x.Name)
		switch g.target {
		case "python":
			g.line("for " + n + " in r_iter(" + seq + "):")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			return err
		case "julia":
			g.line("for " + n + " in r_iter(" + seq + ")")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("end")
			return err
		case "nim":
			g.line("for " + n + " in rIter(" + seq + "):")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			return err
		case "go":
			g.line("for _, " + n + " := range rIter(" + seq + ") {")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("}")
			return err
		case "rust":
			g.line("for " + n + " in r_iter(" + seq + ") {")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("}")
			return err
		case "cpp":
			g.line("for (const auto& " + n + " : r_iter(" + seq + ")) {")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("}")
			return err
		case "csharp":
			g.line("foreach (var " + n + " in RIter(" + seq + ")) {")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("}")
			return err
		case "java":
			g.line("for (Object " + n + " : rIter(" + seq + ")) {")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("}")
			return err
		case "kotlin":
			g.line("for (" + n + " in rIter(" + seq + ")) {")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("}")
			return err
		case "swift":
			g.line("for " + n + " in rIter(" + seq + ") {")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("}")
			return err
		default:
			g.line(targetComment(g.target, "for loop lowered through r_iter; exact syntax backend pending"))
		}
	case *RepeatStmt:
		switch g.target {
		case "python":
			g.line("while True:")
			g.indent++
			err := g.stmtBody(x.Body)
			g.indent--
			return err
		case "julia":
			g.line("while true")
			g.indent++
			err := g.stmtBody(x.Body)
			g.indent--
			g.line("end")
			return err
		case "nim":
			g.line("while true:")
			g.indent++
			err := g.stmtBody(x.Body)
			g.indent--
			return err
		default:
			g.line("for (;;)")
			return g.stmt(x.Body)
		}
	case *ReturnStmt:
		if x.X == nil {
			g.line(returnNull(g.target))
			return nil
		}
		e, err := g.expr(x.X)
		if err != nil {
			return err
		}
		g.line(returnExpr(g.target, e))
	case *BreakStmt:
		g.line("break" + stmtEnd(g.target))
	case *NextStmt:
		if g.target == "python" || g.target == "julia" || g.target == "nim" {
			g.line("continue")
		} else {
			g.line("continue;")
		}
	default:
		return fmt.Errorf("unsupported statement %T", s)
	}
	return nil
}
func (g *targetGen) stmtBody(s Stmt) error {
	if b, ok := s.(*BlockStmt); ok {
		for _, ss := range b.List {
			if err := g.stmt(ss); err != nil {
				return err
			}
		}
		return nil
	}
	return g.stmt(s)
}

func (g *targetGen) functionAssign(n string, fn *FunctionExpr) error {
	// Functions are emitted as target-native closures/named functions using a universal argument vector.
	switch g.target {
	case "python":
		g.line("def " + n + "(*__args):")
		g.indent++
		for i, p := range fn.Params {
			g.line(fmt.Sprintf("%s = r_bind(__args, %d, %s)", safeName(p.Name), i, defaultExprPy(g, p.Default)))
		}
		for _, s := range fn.Body.List {
			if err := g.stmt(s); err != nil {
				return err
			}
		}
		g.line("return None")
		g.indent--
	case "julia":
		g.line("function " + n + "(__args...)")
		g.indent++
		for i, p := range fn.Params {
			g.line(fmt.Sprintf("%s = r_bind(__args, %d, %s)", safeName(p.Name), i+1, defaultExprGeneric(g, p.Default)))
		}
		for _, s := range fn.Body.List {
			if err := g.stmt(s); err != nil {
				return err
			}
		}
		g.line("return nothing")
		g.indent--
		g.line("end")
	case "go":
		g.line(n + " := func(__args ...any) any {")
		g.indent++
		for i, p := range fn.Params {
			g.line(fmt.Sprintf("%s := rBind(__args, %d, %s)", safeName(p.Name), i, defaultExprGeneric(g, p.Default)))
		}
		for _, s := range fn.Body.List {
			if err := g.stmt(s); err != nil {
				return err
			}
		}
		g.line("return nil")
		g.indent--
		g.line("}")
	case "rust":
		g.line("let mut " + n + " = |__args: Vec<RValue>| -> RValue {")
		g.indent++
		for i, p := range fn.Params {
			g.line(fmt.Sprintf("let %s = r_bind(&__args, %d, %s);", safeName(p.Name), i, defaultExprGeneric(g, p.Default)))
		}
		for _, s := range fn.Body.List {
			if err := g.stmt(s); err != nil {
				return err
			}
		}
		g.line("RValue::Null")
		g.indent--
		g.line("};")
	case "cpp":
		g.line("std::function<RValue(std::vector<RValue>)> " + n + " = [&](std::vector<RValue> __args)->RValue {")
		g.indent++
		for i, p := range fn.Params {
			g.line(fmt.Sprintf("RValue %s = r_bind(__args, %d, %s);", safeName(p.Name), i, defaultExprGeneric(g, p.Default)))
		}
		for _, s := range fn.Body.List {
			if err := g.stmt(s); err != nil {
				return err
			}
		}
		g.line("return RValue::null();")
		g.indent--
		g.line("};")
	default:
		g.line(targetComment(g.target, "function "+n+" lowered to runtime closure"))
		g.line(assignSyntax(g.target, n, emitDispatch(g.target, "function", []string{targetString(g.target, n)})))
	}
	return nil
}
func defaultExprPy(g *targetGen, e Expr) string {
	if e == nil {
		return "None"
	}
	s, _ := g.expr(e)
	return s
}
func defaultExprGeneric(g *targetGen, e Expr) string {
	if e == nil {
		return targetNull(g.target)
	}
	s, _ := g.expr(e)
	return s
}

func (g *targetGen) expr(e Expr) (string, error) {
	switch x := e.(type) {
	case *IdentExpr:
		switch x.Name {
		case "TRUE", "T":
			return targetBool(g.target, true), nil
		case "FALSE", "F":
			return targetBool(g.target, false), nil
		case "NULL":
			return targetNull(g.target), nil
		case "NA", "NA_real_", "NA_integer_", "NA_character_", "NA_complex_":
			return targetNA(g.target), nil
		case "NaN":
			return targetNA(g.target), nil
		case "Inf":
			return targetInf(g.target), nil
		case "pi":
			return targetNumber(g.target, "3.14159265358979323846"), nil
		}
		return safeName(x.Name), nil
	case *LiteralExpr:
		if x.Kind == "string" {
			return targetString(g.target, unquote(x.Text)), nil
		}
		s := strings.TrimSuffix(x.Text, "L")
		return targetNumber(g.target, s), nil
	case *UnaryExpr:
		a, err := g.expr(x.X)
		if err != nil {
			return "", err
		}
		return emitDispatch(g.target, "__unary_"+x.Op, []string{a}), nil
	case *BinaryExpr:
		a, err := g.expr(x.L)
		if err != nil {
			return "", err
		}
		b, err := g.expr(x.R)
		if err != nil {
			return "", err
		}
		return emitDispatch(g.target, "__binary_"+x.Op, []string{a, b}), nil
	case *IndexExpr:
		a, err := g.expr(x.X)
		if err != nil {
			return "", err
		}
		args, err := g.args(x.Args)
		if err != nil {
			return "", err
		}
		name := "["
		if x.Double {
			name = "[["
		}
		return emitDispatch(g.target, name, append([]string{a}, args...)), nil
	case *CallExpr:
		args, err := g.args(x.Args)
		if err != nil {
			return "", err
		}
		if id, ok := x.Fun.(*IdentExpr); ok {
			n := safeName(id.Name)
			if g.funcs[n] {
				return callUser(g.target, n, args), nil
			}
			if _, ok := PrimitiveRoute(g.target, id.Name); ok {
				return emitDispatch(g.target, id.Name, args), nil
			}
			return emitDispatch(g.target, id.Name, args), nil
		}
		f, err := g.expr(x.Fun)
		if err != nil {
			return "", err
		}
		return emitDispatch(g.target, "__call_value", append([]string{f}, args...)), nil
	case *FunctionExpr:
		return "", fmt.Errorf("anonymous function must be assigned to a name")
	default:
		return "", fmt.Errorf("unsupported expression %T", e)
	}
}
func (g *targetGen) args(args []Arg) ([]string, error) {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a.Missing {
			out = append(out, targetNull(g.target))
			continue
		}
		e, err := g.expr(a.Value)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

func safeName(s string) string { return strings.NewReplacer(".", "_", "$", "_").Replace(s) }
func stmtEnd(t string) string {
	if t == "python" || t == "julia" || t == "nim" || t == "kotlin" || t == "swift" {
		return ""
	}
	return ";"
}
func exprStmt(t, e string) string { return e + stmtEnd(t) }
func assignSyntax(t, n, e string) string {
	switch t {
	case "python", "julia":
		return n + " = " + e
	case "nim":
		return "var " + n + " = " + e
	case "go":
		return n + " := " + e
	case "rust":
		return "let mut " + n + " = " + e + ";"
	case "cpp":
		return "auto " + n + " = " + e + ";"
	case "c":
		return "RValue " + n + " = " + e + ";"
	case "zig":
		return "var " + n + " = " + e + ";"
	case "csharp":
		return "var " + n + " = " + e + ";"
	case "java":
		return "Object " + n + " = " + e + ";"
	case "kotlin":
		return "var " + n + " = " + e
	case "swift":
		return "var " + n + " = " + e
	}
	return n + " = " + e
}
func truthCall(t, e string) string {
	switch t {
	case "go":
		return "rTruth(" + e + ")"
	case "rust":
		return "r_truth(&" + e + ")"
	case "cpp":
		return "r_truth(" + e + ")"
	case "c":
		return "r_truth(" + e + ")"
	case "zig":
		return "rTruth(" + e + ")"
	case "csharp":
		return "RTruth(" + e + ")"
	case "java":
		return "R2.rTruth(" + e + ")"
	case "kotlin":
		return "rTruth(" + e + ")"
	case "swift":
		return "rTruth(" + e + ")"
	}
	return "r_truth(" + e + ")"
}
func returnNull(t string) string {
	if t == "python" {
		return "return None"
	}
	if t == "julia" {
		return "return nothing"
	}
	return returnExpr(t, targetNull(t))
}
func returnExpr(t, e string) string {
	if t == "python" || t == "julia" || t == "nim" || t == "kotlin" || t == "swift" {
		return "return " + e
	}
	return "return " + e + ";"
}
func callUser(t, n string, args []string) string {
	a := strings.Join(args, ", ")
	switch t {
	case "go":
		return n + "(" + a + ")"
	case "rust":
		return n + "(vec![" + a + "])"
	case "cpp":
		return n + "({" + a + "})"
	default:
		return n + "(" + a + ")"
	}
}

func targetComment(t, s string) string {
	if t == "python" || t == "julia" || t == "nim" {
		return "# " + s
	}
	return "// " + s
}
