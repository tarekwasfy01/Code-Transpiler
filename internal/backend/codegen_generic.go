package backend

import (
	"fmt"
	"strings"
)

type targetGen struct {
	source string
	// evaluation belongs to canonical UAST. Direct UAST generation never reads
	// source provenance; source remains only for the legacy compatibility path.
	evaluation         string
	target             string
	indent             int
	b                  strings.Builder
	declared           []map[string]bool
	funcs              map[string]bool
	inline             map[string]*FunctionExpr
	bindings           []map[string]string
	temp               int
	activeInline       map[*FunctionExpr]bool
	helpers            []string
	helperRequirements []string
	helperSources      map[string]string
	usedNames          map[string]bool
	cValues            map[string]bool
	directVectors      map[string]bool
	// nativeDirect is enabled for native source frontends (R and Go).  The
	// compatibility API also accepts R-shaped snippets tagged as another
	// language; those retain the established runtime encoding and decoder
	// contract.
	nativeDirect     bool
	generatedAt      map[string]int
	uastFunctions    map[string]int
	uastInline       map[string]bool
	uastActiveInline map[int]bool
}

// requireHelper records a semantic support requirement before retaining the
// proven target renderer output as a syntax-only fragment.
func (g *targetGen) requireHelper(id, source string) {
	if g.helperSources == nil {
		g.helperSources = map[string]string{}
	}
	if _, exists := g.helperSources[id]; !exists {
		g.helperRequirements = append(g.helperRequirements, id)
		g.helperSources[id] = source
	}
}

func (g *targetGen) dispatch(name string, args []string) (string, error) {
	if g.nativeDirect {
		return g.nativeDispatch(name, args)
	}
	return emitDispatch(g.target, name, args), nil
}

func generateTarget(target string, ast *BlockStmt) (string, error) {
	return generateTargetFromMode("r", target, ast, true)
}
func generateTargetFrom(source, target string, ast *BlockStmt) (string, error) {
	return generateTargetFromMode(source, target, ast, false)
}
func generateTargetFromMode(source, target string, ast *BlockStmt, nativeDirect bool) (string, error) {
	g := &targetGen{source: source, target: target, declared: []map[string]bool{{}}, funcs: map[string]bool{}, inline: map[string]*FunctionExpr{}, activeInline: map[*FunctionExpr]bool{}}
	g.nativeDirect = nativeDirect
	g.usedNames = reserveSymbols(ast)
	g.cValues = map[string]bool{}
	for _, s := range ast.List {
		if a, ok := s.(*AssignStmt); ok {
			if _, ok := a.Value.(*FunctionExpr); ok {
				name := g.name(a.Name)
				g.funcs[name] = true
				if inlineFunction(a.Value.(*FunctionExpr)) {
					g.inline[name] = a.Value.(*FunctionExpr)
				}
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
		body := g.b.String()
		if nativeDirect {
			return nativeTargetPrefix(target) + body, nil
		}
		return targetPrelude(target) + "\n" + strings.Join(g.helpers, "\n") + "\n" + body, nil
	default:
		if nativeDirect {
			g.line(nativeMainOpen(target))
		} else {
			g.line(mainOpen(target))
		}
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
		body := g.b.String()
		if nativeDirect {
			return nativeTargetPrefix(target) + body, nil
		}
		return targetPrelude(target) + "\n" + strings.Join(g.helpers, "\n") + "\n" + body, nil
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
		return "int main() { r_output_init();"
	case "c":
		return "int main(void) { r_output_init();"
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
		n := g.name(x.Name)
		if fn, ok := x.Value.(*FunctionExpr); ok {
			return g.functionAssign(n, fn)
		}
		e, err := g.expr(x.Value)
		if err != nil {
			return err
		}
		g.line(g.assignment(n, e))
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
			g.line("if (" + truthCall(g.target, c) + ") {")
			g.indent++
			if err = g.stmtBody(x.Then); err != nil {
				return err
			}
			g.indent--
			if x.Else != nil {
				if g.target == "go" {
					g.line("} else {")
				} else {
					g.line("}")
					g.line("else {")
				}
				g.indent++
				if err = g.stmtBody(x.Else); err != nil {
					return err
				}
				g.indent--
				g.line("}")
			} else {
				g.line("}")
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
		if g.target == "go" {
			g.line("for " + truthCall(g.target, c) + " {")
		} else {
			g.line("while (" + truthCall(g.target, c) + ") {")
		}
		g.indent++
		err = g.stmtBody(x.Body)
		g.indent--
		g.line("}")
		return err
	case *ForStmt:
		seq, err := g.expr(x.Seq)
		if err != nil {
			return err
		}
		n := g.name(x.Name)
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
			g.line("foreach (var " + n + " in R2.Iter(" + seq + ")) {")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("}")
			return err
		case "java":
			g.line("for (Object " + n + " : R2.rIter(" + seq + ")) {")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("}")
			return err
		case "c":
			g.temp++
			sequence := fmt.Sprintf("__sequence_%d", g.temp)
			index := fmt.Sprintf("__index_%d", g.temp)
			g.line("RValue " + sequence + " = " + seq + ";")
			g.line("for (size_t " + index + " = 0; " + index + " < " + sequence + ".len; ++" + index + ") {")
			g.indent++
			g.line("RValue " + n + " = " + sequence + ".v[" + index + "];")
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
		case "zig":
			g.line("for (rIter(" + seq + ")) |" + n + "| {")
			g.indent++
			err = g.stmtBody(x.Body)
			g.indent--
			g.line("}")
			return err
		default:
			return fmt.Errorf("target %s has no iterable-loop lowering; refusing to omit its body", g.target)
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
	_, flowErr := buildFunctionFlow(fn)
	if flowErr == nil {
		return nil
	}
	if _, unsafe := flowErr.(*flowSafetyError); unsafe {
		return flowErr
	}
	// Functions are emitted as target-native closures/named functions using a universal argument vector.
	switch g.target {
	case "python":
		g.line("def " + n + "(*__args):")
		g.indent++
		for i, p := range fn.Params {
			g.line(fmt.Sprintf("%s = r_bind(__args, %d, %s)", g.name(p.Name), i, defaultExprPy(g, p.Default)))
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
			g.line(fmt.Sprintf("%s = r_bind(__args, %d, %s)", g.name(p.Name), i+1, defaultExprGeneric(g, p.Default)))
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
			g.line(fmt.Sprintf("%s := rBind(__args, %d, %s)", g.name(p.Name), i, defaultExprGeneric(g, p.Default)))
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
			g.line(fmt.Sprintf("let %s = r_bind(&__args, %d, %s);", g.name(p.Name), i, defaultExprGeneric(g, p.Default)))
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
			g.line(fmt.Sprintf("RValue %s = r_bind(__args, %d, %s);", g.name(p.Name), i, defaultExprGeneric(g, p.Default)))
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
		return fmt.Errorf("target %s has no general closure lowering for %s", g.target, n)
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
	if x, ok := e.(*IterationExpr); ok {
		value, err := g.expr(x.Value)
		if err != nil {
			return "", err
		}
		switch x.Kind {
		case "snapshot":
			return g.snapshotIteration(value)
		case "size":
			return emitDispatch(g.target, "length", []string{value}), nil
		default:
			return "", fmt.Errorf("unknown iteration intrinsic %q", x.Kind)
		}
	}
	switch x := e.(type) {
	case *IdentExpr:
		for i := len(g.bindings) - 1; i >= 0; i-- {
			if value, ok := g.bindings[i][g.name(x.Name)]; ok {
				return value, nil
			}
		}
		if strings.HasPrefix(x.Name, "\x00") {
			return "", fmt.Errorf("unbound internal state slot %q", x.Name)
		}
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
		name := g.name(x.Name)
		g.cValues[name] = true
		if g.target == "rust" {
			return name + ".clone()", nil
		}
		return name, nil
	case *LiteralExpr:
		if g.nativeDirect {
			return nativeLiteral(g.target, x.Kind, x.Text), nil
		}
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
		return g.lowerUnary(x.Op, a)
	case *BinaryExpr:
		a, err := g.expr(x.L)
		if err != nil {
			return "", err
		}
		b, err := g.expr(x.R)
		if err != nil {
			return "", err
		}
		if x.Op == "&&" || x.Op == "||" {
			return g.lowerLogical(x.Op, a, b), nil
		}
		// Preserve ordered operand effects even on targets whose argument or
		// initializer evaluation order is unspecified. Keep bindings inside the
		// expression so an enclosing branch or short circuit still gates them.
		if !g.effectFree(x.L, map[*FunctionExpr]bool{}) || !g.effectFree(x.R, map[*FunctionExpr]bool{}) {
			left, right := g.freshName("left"), g.freshName("right")
			g.cValues[left], g.cValues[right] = true, true
			result, err := g.dispatch("__binary_"+x.Op, []string{left, right})
			if err != nil {
				return "", err
			}
			return g.letExpression([]valueBinding{{left, a}, {right, b}}, result), nil
		}
		return g.dispatch("__binary_"+x.Op, []string{a, b})
	case *OperationExpr:
		return g.lowerTypedOperation(x)
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
		return g.dispatch(name, append([]string{a}, args...))
	case *CallExpr:
		args, err := g.args(x.Args)
		if err != nil {
			return "", err
		}
		if id, ok := x.Fun.(*IdentExpr); ok {
			n := g.name(id.Name)
			if g.funcs[n] {
				if fn, ok := g.inline[n]; ok {
					if x.Eager {
						previous := g.source
						g.source = "eager"
						result, err := g.inlineCall(fn, x.Args, args)
						g.source = previous
						return result, err
					}
					return g.inlineCall(fn, x.Args, args)
				}
				return callUser(g.target, n, args), nil
			}
			if _, ok := PrimitiveRoute(g.target, id.Name); ok {
				return g.dispatch(id.Name, args)
			}
			return g.dispatch(id.Name, args)
		}
		f, err := g.expr(x.Fun)
		if err != nil {
			return "", err
		}
		return g.dispatch("__call_value", append([]string{f}, args...))
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

// Encode source names injectively for Nim's style-insensitive identifier rules.
// Source names and generated temporaries use disjoint prefixes, so keywords,
// underscores, case differences and runtime helper names cannot collide.
func (g *targetGen) name(s string) string {
	if strings.HasPrefix(s, "\x00") {
		return s
	}
	spec, ok := targetSpec(g.target)
	if !ok {
		return safeName(s)
	}
	return (UniversalTargetNameResolver{}).Resolve(s, spec)
}

func inlineFunction(fn *FunctionExpr) bool {
	_, err := buildFunctionFlow(fn)
	return err == nil
}

func (g *targetGen) assignment(name, expression string) string {
	for i := len(g.declared) - 1; i >= 0; i-- {
		if g.declared[i][name] {
			return reassignSyntax(g.target, name, expression)
		}
	}
	g.declared[len(g.declared)-1][name] = true
	return assignSyntax(g.target, name, expression)
}
func stmtEnd(t string) string {
	if spec, ok := targetSpec(t); ok {
		return spec.StatementTerminator
	}
	return ";"
}
func exprStmt(t, e string) string {
	if t == "go" {
		if strings.HasPrefix(e, "fmt.Println(") {
			return e + ";"
		}
		// Lowered calls can become literals (including untyped nil). Keep their
		// effects and explicitly discard the value in a legal Go statement.
		return "_ = any(" + e + ");"
	}
	if t == "nim" {
		return "discard " + e
	}
	if t == "zig" {
		return "_ = " + e + ";"
	}
	if t == "java" {
		return "R2.discard(" + e + ");"
	}
	if t == "csharp" {
		return "R2.Discard(" + e + ");"
	}
	return e + stmtEnd(t)
}
func assignSyntax(t, n, e string) string {
	switch t {
	case "python", "julia":
		return n + " = " + e
	case "nim":
		return "var " + n + " = " + e
	case "go":
		return "var " + n + " any = " + e
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

func reassignSyntax(t, n, e string) string {
	return n + " = " + e + stmtEnd(t)
}
func truthCall(t, e string) string {
	switch t {
	case "nim":
		return "rTruth(" + e + ")"
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
		return "R2.Truth(" + e + ")"
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
