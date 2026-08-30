package cpp

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type generator struct {
	b      strings.Builder
	indent int
	scopes []map[string]bool
	funcs  map[string]bool
	temp   int
}

func newGenerator() *generator {
	return &generator{scopes: []map[string]bool{{}}, funcs: map[string]bool{}}
}
func (g *generator) line(s string) {
	g.b.WriteString(strings.Repeat("    ", g.indent))
	g.b.WriteString(s)
	g.b.WriteByte('\n')
}
func (g *generator) push() { g.scopes = append(g.scopes, map[string]bool{}) }
func (g *generator) pop()  { g.scopes = g.scopes[:len(g.scopes)-1] }
func (g *generator) declared(n string) bool {
	for i := len(g.scopes) - 1; i >= 0; i-- {
		if g.scopes[i][n] {
			return true
		}
	}
	return false
}
func (g *generator) declare(n string) { g.scopes[len(g.scopes)-1][n] = true }
func (g *generator) tmp() string      { g.temp++; return fmt.Sprintf("__r2cpp_%d", g.temp) }

func generate(ast *BlockStmt) (string, error) {
	g := newGenerator()
	// Pre-register top-level function assignments, enabling forward calls.
	for _, s := range ast.List {
		if a, ok := s.(*AssignStmt); ok {
			if _, ok := a.Value.(*FunctionExpr); ok {
				g.funcs[cppIdent(a.Name)] = true
			}
		}
	}
	g.line("int main() {")
	g.indent++
	vars, fns := collectNames(ast)
	for _, n := range vars {
		g.line("r2cpp::Value " + n + " = r2cpp::Value::null();")
		g.declare(n)
	}
	for _, n := range fns {
		g.line("std::function<r2cpp::Value(std::vector<r2cpp::NamedArg>)> " + n + ";")
		g.declare(n)
		g.funcs[n] = true
	}
	for _, s := range ast.List {
		if err := g.stmt(s); err != nil {
			return "", err
		}
	}
	g.line("return 0;")
	g.indent--
	g.line("}")
	return runtimeSource + "\n" + g.b.String(), nil
}

func (g *generator) stmt(s Stmt) error {
	switch x := s.(type) {
	case *BlockStmt:
		g.line("{")
		g.indent++
		for _, ss := range x.List {
			if e := g.stmt(ss); e != nil {
				return e
			}
		}
		g.indent--
		g.line("}")
	case *AssignStmt:
		n := cppIdent(x.Name)
		if fn, ok := x.Value.(*FunctionExpr); ok {
			return g.functionAssign(n, fn)
		}
		e, err := g.expr(x.Value)
		if err != nil {
			return err
		}
		if !g.declared(n) {
			g.line("r2cpp::Value " + n + " = " + e + ";")
			g.declare(n)
		} else {
			g.line(n + " = " + e + ";")
		}
	case *ExprStmt:
		e, err := g.expr(x.X)
		if err != nil {
			return err
		}
		g.line("(void)(" + e + ");")
	case *IfStmt:
		c, err := g.expr(x.Cond)
		if err != nil {
			return err
		}
		g.line("if (r2cpp::truth(" + c + "))")
		if err = g.stmt(x.Then); err != nil {
			return err
		}
		if x.Else != nil {
			g.line("else")
			return g.stmt(x.Else)
		}
	case *WhileStmt:
		c, err := g.expr(x.Cond)
		if err != nil {
			return err
		}
		g.line("while (r2cpp::truth(" + c + "))")
		return g.stmt(x.Body)
	case *ForStmt:
		seq, err := g.expr(x.Seq)
		if err != nil {
			return err
		}
		tmp := g.tmp()
		n := cppIdent(x.Name)
		if !g.declared(n) {
			g.line("r2cpp::Value " + n + " = r2cpp::Value::null();")
			g.declare(n)
		}
		g.line("for (const auto& " + tmp + " : r2cpp::as_vector(" + seq + ")) {")
		g.indent++
		g.line(n + " = " + tmp + ";")
		if err = g.stmtBody(x.Body); err != nil {
			return err
		}
		g.indent--
		g.line("}")
	case *RepeatStmt:
		g.line("for (;;) ")
		return g.stmt(x.Body)
	case *ReturnStmt:
		if x.X == nil {
			g.line("return r2cpp::Value::null();")
		} else {
			e, err := g.expr(x.X)
			if err != nil {
				return err
			}
			g.line("return " + e + ";")
		}
	case *BreakStmt:
		g.line("break;")
	case *NextStmt:
		g.line("continue;")
	default:
		return fmt.Errorf("unsupported statement %T", s)
	}
	return nil
}
func (g *generator) stmtBody(s Stmt) error {
	if b, ok := s.(*BlockStmt); ok {
		for _, ss := range b.List {
			if e := g.stmt(ss); e != nil {
				return e
			}
		}
		return nil
	}
	return g.stmt(s)
}

func (g *generator) functionAssign(n string, fn *FunctionExpr) error {
	g.funcs[n] = true
	if !g.declared(n) {
		g.line("std::function<r2cpp::Value(std::vector<r2cpp::NamedArg>)> " + n + ";")
		g.declare(n)
	}
	g.line(n + " = [&](std::vector<r2cpp::NamedArg> __args) -> r2cpp::Value {")
	g.indent++
	g.push()
	for i, p := range fn.Params {
		d := "r2cpp::Value::missing()"
		var err error
		if p.Default != nil {
			d, err = g.expr(p.Default)
			if err != nil {
				return err
			}
		}
		pn := cppIdent(p.Name)
		g.line(fmt.Sprintf("r2cpp::Value %s = r2cpp::bind_arg(__args, %q, %d, %s);", pn, p.Name, i, d))
		g.declare(pn)
	}
	vars, fns := collectNames(fn.Body)
	for _, local := range vars {
		if !g.declared(local) {
			g.line("r2cpp::Value " + local + " = r2cpp::Value::null();")
			g.declare(local)
		}
	}
	for _, local := range fns {
		if !g.declared(local) {
			g.line("std::function<r2cpp::Value(std::vector<r2cpp::NamedArg>)> " + local + ";")
			g.declare(local)
		}
		g.funcs[local] = true
	}
	for _, s := range fn.Body.List {
		if e := g.stmt(s); e != nil {
			return e
		}
	}
	g.line("return r2cpp::Value::null();")
	g.pop()
	g.indent--
	g.line("};")
	return nil
}

func (g *generator) expr(e Expr) (string, error) {
	switch x := e.(type) {
	case *IdentExpr:
		switch x.Name {
		case "TRUE", "T":
			return "r2cpp::Value(true)", nil
		case "FALSE", "F":
			return "r2cpp::Value(false)", nil
		case "NULL":
			return "r2cpp::Value::null()", nil
		case "NA", "NA_real_", "NA_integer_", "NA_character_", "NA_complex_":
			return "r2cpp::Value::na()", nil
		case "NaN":
			return "r2cpp::Value(std::numeric_limits<double>::quiet_NaN())", nil
		case "Inf":
			return "r2cpp::Value(std::numeric_limits<double>::infinity())", nil
		case "pi":
			return "r2cpp::Value(3.141592653589793238462643383279502884)", nil
		}
		return cppIdent(x.Name), nil
	case *LiteralExpr:
		if x.Kind == "string" {
			return "r2cpp::Value(" + strconv.Quote(unquote(x.Text)) + ")", nil
		}
		s := strings.TrimSuffix(x.Text, "L")
		if strings.HasSuffix(s, "i") {
			s = strings.TrimSuffix(s, "i")
			return "r2cpp::Value(std::complex<double>(0.0," + s + "))", nil
		}
		if !strings.ContainsAny(s, ".eE") {
			return "r2cpp::Value((std::int64_t)" + s + ")", nil
		}
		return "r2cpp::Value((double)" + s + ")", nil
	case *UnaryExpr:
		a, err := g.expr(x.X)
		if err != nil {
			return "", err
		}
		return "r2cpp::unary(" + strconv.Quote(x.Op) + ", " + a + ")", nil
	case *BinaryExpr:
		a, err := g.expr(x.L)
		if err != nil {
			return "", err
		}
		b, err := g.expr(x.R)
		if err != nil {
			return "", err
		}
		return "r2cpp::binary(" + strconv.Quote(x.Op) + ", " + a + ", " + b + ")", nil
	case *IndexExpr:
		a, err := g.expr(x.X)
		if err != nil {
			return "", err
		}
		args, err := g.argList(x.Args)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("r2cpp::subset(%s, {%s}, %v)", a, args, x.Double), nil
	case *CallExpr:
		args, err := g.argList(x.Args)
		if err != nil {
			return "", err
		}
		if id, ok := x.Fun.(*IdentExpr); ok {
			n := cppIdent(id.Name)
			if g.funcs[n] {
				return n + "(std::vector<r2cpp::NamedArg>{" + args + "})", nil
			}
			return "r2cpp::call(" + strconv.Quote(id.Name) + ", {" + args + "})", nil
		}
		f, err := g.expr(x.Fun)
		if err != nil {
			return "", err
		}
		return "r2cpp::call_value(" + f + ", {" + args + "})", nil
	case *FunctionExpr:
		return "", fmt.Errorf("anonymous function is supported when assigned to a name")
	default:
		return "", fmt.Errorf("unsupported expression %T", e)
	}
}
func (g *generator) argList(args []Arg) (string, error) {
	out := make([]string, 0, len(args))
	for _, a := range args {
		if a.Missing {
			out = append(out, "r2cpp::NamedArg{\"\", r2cpp::Value::missing()}")
			continue
		}
		e, err := g.expr(a.Value)
		if err != nil {
			return "", err
		}
		out = append(out, "r2cpp::NamedArg{"+strconv.Quote(a.Name)+", "+e+"}")
	}
	return strings.Join(out, ", "), nil
}

func collectNames(b *BlockStmt) ([]string, []string) {
	vars := map[string]bool{}
	fns := map[string]bool{}
	var walk func(Stmt)
	walk = func(s Stmt) {
		switch x := s.(type) {
		case *AssignStmt:
			n := cppIdent(x.Name)
			if _, ok := x.Value.(*FunctionExpr); ok {
				fns[n] = true
			} else {
				vars[n] = true
			}
		case *BlockStmt:
			for _, q := range x.List {
				walk(q)
			}
		case *IfStmt:
			walk(x.Then)
			if x.Else != nil {
				walk(x.Else)
			}
		case *WhileStmt:
			walk(x.Body)
		case *ForStmt:
			vars[cppIdent(x.Name)] = true
			walk(x.Body)
		case *RepeatStmt:
			walk(x.Body)
		}
	}
	for _, s := range b.List {
		walk(s)
	}
	for n := range fns {
		delete(vars, n)
	}
	return sortedKeys(vars), sortedKeys(fns)
}

// Used by status/report generation and deterministic function list output.
func sortedKeys(m map[string]bool) []string {
	k := make([]string, 0, len(m))
	for x := range m {
		k = append(k, x)
	}
	sort.Strings(k)
	return k
}
