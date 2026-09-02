package backend

import (
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"sort"
	"strings"
)

type valueBinding struct{ Name, Value string }

// B maps actual arguments to formal parameters. It is a partial permutation
// matrix; zero rows must be supplied by explicit defaults, never guessed NULLs.
func argumentBindingMatrix(fn *FunctionExpr, args []Arg) (matrixir.Matrix, error) {
	b := matrixir.NewMatrix(len(fn.Params), len(args))
	used := make([]bool, len(fn.Params))
	position := 0
	for j, arg := range args {
		parameter := -1
		if arg.Name != "" {
			for i, p := range fn.Params {
				if p.Name == arg.Name {
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
			return b, fmt.Errorf("arity or unknown named argument %q", arg.Name)
		}
		if used[parameter] {
			return b, fmt.Errorf("duplicate argument %q", fn.Params[parameter].Name)
		}
		used[parameter] = true
		if !arg.Missing {
			b.Set(parameter, j, 1)
		} else if fn.Params[parameter].Default == nil {
			return b, fmt.Errorf("missing argument %s", fn.Params[parameter].Name)
		}
	}
	for i, p := range fn.Params {
		has := false
		for j := range args {
			has = has || b.At(i, j) != 0
		}
		if !has && p.Default == nil {
			return b, fmt.Errorf("arity: missing argument %s", p.Name)
		}
	}
	return b, nil
}

func (g *targetGen) inlineCall(fn *FunctionExpr, actual []Arg, args []string) (string, error) {
	if g.activeInline[fn] {
		return "", fmt.Errorf("recursive inline call requires a recursive function representation")
	}
	g.activeInline[fn] = true
	defer delete(g.activeInline, fn)
	binding, err := argumentBindingMatrix(fn, actual)
	if err != nil {
		return "", err
	}
	demand := parameterDemand(fn)
	actualDemand, err := demand.Multiply(binding)
	if err != nil {
		return "", err
	}
	effects := make([]float64, len(actual))
	for i, a := range actual {
		if !a.Missing && !g.effectFree(a.Value, map[*FunctionExpr]bool{}) {
			effects[i] = 1
		}
	}
	effectUses, _ := actualDemand.Multiply(columnVector(effects))
	if g.source == "r" {
		strictReturn := false
		if len(fn.Body.List) == 1 {
			if r, ok := fn.Body.List[0].(*ReturnStmt); ok {
				strictReturn = strictExpression(r.X)
			}
		}
		for _, v := range effectUses.Data {
			if v != 0 && !strictReturn {
				return "", fmt.Errorf("lazy effectful argument requires a promise in this function body")
			}
		}
	}
	var order []int
	if g.source != "r" {
		for i, a := range actual {
			if !a.Missing {
				order = append(order, i)
			}
		}
	} else {
		seen := map[int]bool{}
		for row := 0; row < actualDemand.Rows; row++ {
			for col := 0; col < actualDemand.Cols; col++ {
				if actualDemand.At(row, col) != 0 && !seen[col] {
					order = append(order, col)
					seen[col] = true
				}
			}
		}
	}
	values := make([]string, len(actual))
	var lets []valueBinding
	for _, i := range order {
		name := g.freshName("arg")
		values[i] = name
		g.cValues[name] = true
		lets = append(lets, valueBinding{name, args[i]})
	}
	scope := map[string]string{}
	for i, p := range fn.Params {
		for j := range actual {
			if binding.At(i, j) != 0 {
				value := values[j]
				if value == "" {
					value = targetNull(g.target)
				} else if g.target == "rust" {
					value += ".clone()"
				}
				scope[g.name(p.Name)] = value
			}
		}
	}
	g.bindings = append(g.bindings, scope)
	defer func() { g.bindings = g.bindings[:len(g.bindings)-1] }()
	for _, p := range fn.Params {
		if _, ok := scope[g.name(p.Name)]; !ok {
			value, err := g.expr(p.Default)
			if err != nil {
				return "", err
			}
			name := g.freshName("default")
			lets = append(lets, valueBinding{name, value})
			g.cValues[name] = true
			scope[g.name(p.Name)] = name
			if g.target == "rust" {
				scope[g.name(p.Name)] += ".clone()"
			}
		}
	}
	flow, err := buildFunctionFlow(fn)
	if err != nil {
		return "", err
	}
	result, err := g.lowerFunctionFlow(flow, scope)
	if err != nil {
		return "", err
	}
	return g.letExpression(lets, result), nil
}

// Strict scalar expressions force their operands. Calls and short-circuit
// expressions may defer them, so R's effectful arguments need real promises.
func strictExpression(e Expr) bool {
	switch x := e.(type) {
	case *LiteralExpr, *IdentExpr:
		return true
	case *UnaryExpr:
		return strictExpression(x.X)
	case *BinaryExpr:
		return x.Op != "&&" && x.Op != "||" && strictExpression(x.L) && strictExpression(x.R)
	}
	return false
}

func (g *targetGen) freshName(kind string) string {
	for {
		g.temp++
		n := fmt.Sprintf("__r2m_%s_%d", kind, g.temp)
		if g.target == "nim" {
			n = fmt.Sprintf("r2mt%s%d", kind, g.temp)
		}
		if !g.usedNames[n] {
			g.usedNames[n] = true
			if g.generatedAt == nil {
				g.generatedAt = map[string]int{}
			}
			g.generatedAt[n] = g.temp
			return n
		}
	}
}

func (g *targetGen) letExpression(bindings []valueBinding, result string) string {
	if len(bindings) == 0 {
		return result
	}
	var body strings.Builder
	switch g.target {
	case "python":
		for i := len(bindings) - 1; i >= 0; i-- {
			b := bindings[i]
			result = "(lambda " + b.Name + ": " + result + ")(" + b.Value + ")"
		}
		return result
	case "c":
		// Standard C has no local expression block. Lift it into a static function
		// with explicit value captures; argument effects occur in ordered statements
		// INSIDE that function, not in C's unordered call argument list.
		bound := map[string]bool{}
		for _, b := range bindings {
			bound[b.Name] = true
		}
		all := result
		for _, b := range bindings {
			all += " " + b.Value
		}
		captureSet := map[string]bool{}
		for _, t := range matrixir.Tokenize("c", all) {
			if t.Class == matrixir.TokenIdentifier && g.cValues[t.Text] && !bound[t.Text] {
				captureSet[t.Text] = true
			}
		}
		captures := make([]string, 0, len(captureSet))
		for n := range captureSet {
			captures = append(captures, n)
		}
		sort.Strings(captures)
		parameters := make([]string, len(captures))
		for i, n := range captures {
			parameters[i] = "RValue " + n
		}
		signature := strings.Join(parameters, ", ")
		if signature == "" {
			signature = "void"
		}
		name := g.freshName("bind")
		body.WriteString("static RValue " + name + "(" + signature + ") { ")
		for _, b := range bindings {
			body.WriteString("RValue " + b.Name + " = " + b.Value + "; ")
		}
		body.WriteString("return " + result + "; }")
		// Binding/capture facts and the helper name were resolved before this
		// renderer runs; the source is payload of a named requirement only.
		g.requireHelper("helper.binding.capture."+name, body.String())
		return name + "(" + strings.Join(captures, ", ") + ")"
	case "go":
		body.WriteString("func() any { ")
		for _, b := range bindings {
			body.WriteString("var " + b.Name + " any = " + b.Value + "; _ = " + b.Name + "; ")
		}
		body.WriteString("return " + result + " }()")
	case "rust":
		body.WriteString("{ ")
		for _, b := range bindings {
			body.WriteString("let " + b.Name + " = " + b.Value + "; ")
		}
		body.WriteString(result + " }")
	case "cpp":
		body.WriteString("[&]() -> RValue { ")
		for _, b := range bindings {
			body.WriteString("RValue " + b.Name + " = " + b.Value + "; ")
		}
		body.WriteString("return " + result + "; }()")
	case "java":
		// Java lambdas cannot capture a local that is reassigned later. The
		// same capture vector used by the C adapter is therefore passed as
		// explicit method arguments. This preserves the ordered binding
		// expression without imposing Java's effectively-final restriction.
		bound := map[string]bool{}
		for _, b := range bindings {
			bound[b.Name] = true
		}
		all := result
		for _, b := range bindings {
			all += " " + b.Value
		}
		captureSet := map[string]bool{}
		for _, t := range matrixir.Tokenize("java", all) {
			// Generated temporaries belong to nested binding expressions, not the
			// surrounding Java scope; only source bindings can be captured here.
			if t.Class == matrixir.TokenIdentifier && g.cValues[t.Text] && !bound[t.Text] && g.generatedAt[t.Text] == 0 {
				captureSet[t.Text] = true
			}
		}
		captures := make([]string, 0, len(captureSet))
		for n := range captureSet {
			captures = append(captures, n)
		}
		sort.Strings(captures)
		parameters := make([]string, len(captures))
		for i, n := range captures {
			parameters[i] = "Object " + n
		}
		body.WriteString("(new Object(){ Object eval(" + strings.Join(parameters, ", ") + "){ ")
		for _, b := range bindings {
			body.WriteString("Object " + b.Name + " = " + b.Value + "; ")
		}
		body.WriteString("return " + result + "; } }).eval(" + strings.Join(captures, ", ") + ")")
	case "csharp":
		body.WriteString("((Func<object>)(() => { ")
		for _, b := range bindings {
			body.WriteString("object " + b.Name + " = " + b.Value + "; ")
		}
		body.WriteString("return " + result + "; }))()")
	case "julia":
		body.WriteString("(let ")
		for _, b := range bindings {
			body.WriteString(b.Name + " = " + b.Value + "; ")
		}
		body.WriteString(result + " end)")
	case "nim":
		for i := len(bindings) - 1; i >= 0; i-- {
			b := bindings[i]
			result = "(proc (" + b.Name + ": RValue): RValue = " + result + ")(" + b.Value + ")"
		}
		return result
	case "kotlin":
		body.WriteString("run { ")
		for _, b := range bindings {
			body.WriteString("val " + b.Name + " = " + b.Value + "; ")
		}
		body.WriteString(result + " }")
	case "swift":
		body.WriteString("{ () -> Any in ")
		for _, b := range bindings {
			body.WriteString("let " + b.Name + " = " + b.Value + "; ")
		}
		body.WriteString("return " + result + " }()")
	case "zig":
		label := g.freshName("block")
		body.WriteString(label + ": { ")
		for _, b := range bindings {
			body.WriteString("const " + b.Name + " = " + b.Value + "; ")
		}
		body.WriteString("break :" + label + " " + result + "; }")
	}
	return body.String()
}

func reserveSymbols(ast *BlockStmt) map[string]bool {
	names := map[string]bool{}
	var expr func(Expr)
	var stmt func(Stmt)
	expr = func(e Expr) {
		switch x := e.(type) {
		case *OperationExpr:
			for _, operand := range x.Operands {
				expr(operand)
			}
		case *IdentExpr:
			names[safeName(x.Name)] = true
		case *UnaryExpr:
			expr(x.X)
		case *BinaryExpr:
			expr(x.L)
			expr(x.R)
		case *CallExpr:
			expr(x.Fun)
			for _, a := range x.Args {
				expr(a.Value)
			}
		case *IndexExpr:
			expr(x.X)
			for _, a := range x.Args {
				expr(a.Value)
			}
		case *FunctionExpr:
			for _, p := range x.Params {
				names[safeName(p.Name)] = true
				expr(p.Default)
			}
			stmt(x.Body)
		}
	}
	stmt = func(s Stmt) {
		switch x := s.(type) {
		case *BlockStmt:
			for _, s := range x.List {
				stmt(s)
			}
		case *AssignStmt:
			names[safeName(x.Name)] = true
			expr(x.Value)
		case *ExprStmt:
			expr(x.X)
		case *ReturnStmt:
			expr(x.X)
		case *IfStmt:
			expr(x.Cond)
			stmt(x.Then)
			stmt(x.Else)
		case *WhileStmt:
			expr(x.Cond)
			stmt(x.Body)
		case *ForStmt:
			names[safeName(x.Name)] = true
			expr(x.Seq)
			stmt(x.Body)
		case *RepeatStmt:
			stmt(x.Body)
		}
	}
	stmt(ast)
	return names
}
