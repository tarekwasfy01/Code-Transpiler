package backend

import (
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"sort"
	"strings"
)

// R and W are node-by-slot incidence matrices. D is the greatest fixed point
// of definitely initialized slots, intersecting all predecessor states. Reads
// must satisfy R <= D on reachable nodes, including loop backedges.
func analyzeFlowState(f *functionFlow, fn *FunctionExpr) error {
	names := map[string]bool{}
	for _, p := range fn.Params {
		names[p.Name] = true
	}
	for _, s := range f.nodes {
		if a, ok := s.(*AssignStmt); ok {
			names[a.Name] = true
		}
	}
	for name := range names {
		f.slots = append(f.slots, name)
	}
	sort.Strings(f.slots)
	index := map[string]int{}
	for i, name := range f.slots {
		index[name] = i
	}
	n, k := len(f.nodes), len(f.slots)
	f.reads, f.writes, f.defined = matrixir.NewMatrix(n, k), matrixir.NewMatrix(n, k), matrixir.NewMatrix(n, k)
	var read func(int, Expr)
	read = func(i int, e Expr) {
		switch x := e.(type) {
		case *OperationExpr:
			for _, operand := range x.Operands {
				read(i, operand)
			}
		case *IterationExpr:
			read(i, x.Value)
		case *IdentExpr:
			if j, ok := index[x.Name]; ok {
				f.reads.Set(i, j, 1)
			}
		case *UnaryExpr:
			read(i, x.X)
		case *BinaryExpr:
			read(i, x.L)
			read(i, x.R)
		case *CallExpr:
			for _, a := range x.Args {
				read(i, a.Value)
			}
		case *IndexExpr:
			read(i, x.X)
			for _, a := range x.Args {
				read(i, a.Value)
			}
		}
	}
	for i, s := range f.nodes {
		switch x := s.(type) {
		case *AssignStmt:
			read(i, x.Value)
			f.writes.Set(i, index[x.Name], 1)
		case *ExprStmt:
			read(i, x.X)
		case *ReturnStmt:
			read(i, x.X)
		case *IfStmt:
			read(i, x.Cond)
		case *WhileStmt:
			read(i, x.Cond)
		}
	}
	seed := make([]float64, k)
	for _, p := range fn.Params {
		seed[index[p.Name]] = 1
	}
	f.initial = seed
	for i := range f.defined.Data {
		f.defined.Data[i] = 1
	}
	predecessors := make([][]int, n)
	for i := 0; i < n; i++ {
		for j := 0; j < n; j++ {
			if f.reachable[i] != 0 && (f.A.At(i, j) != 0 || f.T.At(i, j) != 0 || f.F.At(i, j) != 0) {
				predecessors[j] = append(predecessors[j], i)
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for i := 0; i < n; i++ {
			if f.reachable[i] == 0 {
				continue
			}
			for j := 0; j < k; j++ {
				v := float64(1)
				if i == f.entry {
					v = seed[j]
				}
				for _, p := range predecessors[i] {
					if f.defined.At(p, j) == 0 && f.writes.At(p, j) == 0 {
						v = 0
					}
				}
				if f.defined.At(i, j) != v {
					f.defined.Set(i, j, v)
					changed = true
				}
			}
		}
	}
	for i := 0; i < n; i++ {
		if f.reachable[i] == 0 {
			continue
		}
		for j, name := range f.slots {
			if f.reads.At(i, j) > f.defined.At(i, j) {
				return fmt.Errorf("function flow reads local %s before definite assignment", name)
			}
		}
	}
	return nil
}

func (g *targetGen) lowerStateFlow(f *functionFlow, scope map[string]string) (string, error) {
	// A generated binding introduced while lowering this body belongs inside
	// it. Only earlier generated values can be captured from the enclosing scope.
	captureBoundary := g.temp
	state, pc := g.freshName("state"), g.freshName("pc")
	g.cValues[state] = true // C stores slots in an RValue vector, safe to capture by value.
	bindings := map[string]string{}
	initial := make([]string, len(f.slots))
	slot := func(i int) string {
		if g.target == "nim" {
			return fmt.Sprintf("%s.values[%d]", state, i)
		}
		if g.target == "c" {
			return fmt.Sprintf("%s.v[%d]", state, i)
		}
		if g.target == "julia" {
			i++
		}
		return fmt.Sprintf("%s[%d]", state, i)
	}
	for i, name := range f.slots {
		value, ok := scope[g.name(name)]
		if !ok {
			value = targetNull(g.target)
		}
		initial[i] = value
		bindings[g.name(name)] = slot(i)
		if g.target == "rust" {
			bindings[g.name(name)] += ".clone()"
		}
	}
	g.bindings = append(g.bindings, bindings)
	defer func() { g.bindings = g.bindings[:len(g.bindings)-1] }()
	label := g.freshName("machine")
	var body strings.Builder
	indent := 0
	line := func(s string) { body.WriteString(strings.Repeat("    ", indent) + s + "\n") }
	end := func() {
		indent--
		if g.target == "julia" {
			line("end")
		} else if g.target != "python" && g.target != "nim" {
			line("}")
		}
	}
	ifOpen := func(cond string) {
		if g.target == "python" || g.target == "nim" {
			line("if " + cond + ":")
		} else if g.target == "julia" {
			line("if " + cond)
		} else {
			line("if (" + cond + ") {")
		}
		indent++
	}
	join := strings.Join(initial, ", ")
	switch g.target {
	case "nim":
		line("var " + state + " = rVec(@[" + join + "])")
		line("var " + pc + " = " + fmt.Sprint(f.entry))
		line("while true:")
	case "python":
		line(state + " = [" + join + "]")
		line(pc + " = " + fmt.Sprint(f.entry))
		line("while True:")
	case "go":
		line(state + " := []any{" + join + "}")
		line("_ = " + state)
		line(pc + " := " + fmt.Sprint(f.entry))
		line("for {")
	case "rust":
		line("let mut " + state + ": Vec<RValue> = vec![" + join + "];")
		line("let mut " + pc + " = " + fmt.Sprint(f.entry) + ";")
		line("loop {")
	case "cpp":
		line("std::vector<RValue> " + state + " = {" + join + "};")
		line("int " + pc + " = " + fmt.Sprint(f.entry) + ";")
		line("while (true) {")
	case "c":
		initialVector := emitDispatch("c", "c", initial)
		if len(initial) == 0 {
			initialVector = targetNull("c")
		}
		line("RValue " + state + " = " + initialVector + ";")
		line("int " + pc + " = " + fmt.Sprint(f.entry) + ";")
		line("while (1) {")
	case "java":
		line("Object[] " + state + " = new Object[]{" + join + "};")
		line("int " + pc + " = " + fmt.Sprint(f.entry) + ";")
		line("while (true) {")
	case "csharp":
		line("object[] " + state + " = new object[]{" + join + "};")
		line("int " + pc + " = " + fmt.Sprint(f.entry) + ";")
		line("while (true) {")
	case "julia":
		line(state + " = Any[" + join + "]")
		line(pc + " = " + fmt.Sprint(f.entry))
		line("while true")
	case "kotlin":
		line("val " + state + " = mutableListOf<Any?>(" + join + ")")
		line("var " + pc + " = " + fmt.Sprint(f.entry))
		line("while (true) {")
	case "swift":
		line("var " + state + ": [Any] = [" + join + "]")
		line("var " + pc + " = " + fmt.Sprint(f.entry))
		line("while true {")
	case "zig":
		line("var " + state + " = [_]RValue{" + join + "};")
		line("var " + pc + ": usize = " + fmt.Sprint(f.entry) + ";")
		line("while (true) {")
	default:
		return "", fmt.Errorf("target %s has no typed mutable state-vector runtime", g.target)
	}
	indent++
	for i, s := range f.nodes {
		if f.reachable[i] == 0 {
			continue
		}
		ifOpen(pc + " == " + fmt.Sprint(i))
		switch x := s.(type) {
		case *ReturnStmt:
			value := targetNull(g.target)
			var err error
			if x.X != nil {
				value, err = g.expr(x.X)
			}
			if err != nil {
				return "", err
			}
			if g.target == "c" {
				// Return a value copy before releasing only the private slot array.
				// Nested user vectors retain their separate existing allocations.
				result := g.freshName("result")
				line("RValue " + result + " = " + value + ";")
				line("free(" + state + ".v);")
				value = result
			}
			prefix := "return "
			if g.target == "kotlin" {
				prefix = "return@" + label + " "
			}
			if g.target == "zig" {
				prefix = "break :" + label + " "
			}
			line(prefix + value + stmtEnd(g.target))
		case *IfStmt, *WhileStmt:
			var condition Expr
			if x, ok := s.(*IfStmt); ok {
				condition = x.Cond
			} else {
				condition = s.(*WhileStmt).Cond
			}
			c, err := g.expr(condition)
			if err != nil {
				return "", err
			}
			yes, err := flowSuccessor(f.T, i)
			if err != nil {
				return "", err
			}
			no, err := flowSuccessor(f.F, i)
			if err != nil {
				return "", err
			}
			line(pc + " = " + fmt.Sprint(no) + stmtEnd(g.target))
			ifOpen(truthCall(g.target, c))
			line(pc + " = " + fmt.Sprint(yes) + stmtEnd(g.target))
			end()
			line("continue" + stmtEnd(g.target))
		default:
			switch x := x.(type) {
			case *AssignStmt:
				value, err := g.expr(x.Value)
				if err != nil {
					return "", err
				}
				j := sort.SearchStrings(f.slots, x.Name)
				line(slot(j) + " = " + value + stmtEnd(g.target))
			case *ExprStmt:
				value, err := g.expr(x.X)
				if err != nil {
					return "", err
				}
				prefix := ""
				if g.target == "nim" {
					prefix = "discard "
				}
				if g.target == "zig" || g.target == "swift" {
					prefix = "_ = "
				}
				line(prefix + value + stmtEnd(g.target))
			case *BreakStmt, *NextStmt:
			default:
				return "", fmt.Errorf("unsupported state instruction %T", s)
			}
			next, err := flowSuccessor(f.A, i)
			if err != nil {
				return "", err
			}
			line(pc + " = " + fmt.Sprint(next) + stmtEnd(g.target))
			line("continue" + stmtEnd(g.target))
		}
		end()
	}
	switch g.target {
	case "nim":
		line("raise newException(ValueError, \"invalid function state\")")
	case "python":
		line("raise RuntimeError('invalid function state')")
	case "go":
		line("panic(\"invalid function state\")")
	case "rust":
		line("panic!(\"invalid function state\");")
	case "c":
		line("abort();")
	case "cpp":
		line("throw \"invalid function state\";")
	case "java":
		line("throw new IllegalStateException(\"invalid function state\");")
	case "csharp":
		line("throw new InvalidOperationException(\"invalid function state\");")
	case "julia":
		line("error(\"invalid function state\")")
	case "kotlin":
		line("error(\"invalid function state\")")
	case "swift":
		line("fatalError(\"invalid function state\")")
	case "zig":
		line("unreachable;")
	}
	end()
	code := body.String()
	switch g.target {
	case "python", "c", "nim":
		// These targets need named helpers for statement bodies used as expressions.
		// Capture only existing value names; state and PC are local to this helper.
		set := map[string]bool{}
		for _, t := range matrixir.Tokenize(g.target, code) {
			if t.Class == matrixir.TokenIdentifier && g.cValues[t.Text] && t.Text != state && t.Text != pc && g.generatedAt[t.Text] <= captureBoundary {
				set[t.Text] = true
			}
		}
		var captures []string
		for n := range set {
			captures = append(captures, n)
		}
		sort.Strings(captures)
		helper := g.freshName("flow")
		if g.target == "nim" {
			parameters := make([]string, len(captures))
			for i, n := range captures {
				parameters[i] = n + ": RValue"
			}
			g.requireHelper("helper.state."+helper, "proc "+helper+"("+strings.Join(parameters, ", ")+"): RValue =\n    "+strings.ReplaceAll(strings.TrimSuffix(code, "\n"), "\n", "\n    ")+"\n")
		} else if g.target == "python" {
			g.requireHelper("helper.state."+helper, "def "+helper+"("+strings.Join(captures, ", ")+"):\n    "+strings.ReplaceAll(strings.TrimSuffix(code, "\n"), "\n", "\n    ")+"\n")
		} else {
			parameters := make([]string, len(captures))
			for i, n := range captures {
				parameters[i] = "RValue " + n
			}
			signature := strings.Join(parameters, ", ")
			if signature == "" {
				signature = "void"
			}
			g.requireHelper("helper.state."+helper, "static RValue "+helper+"("+signature+") {\n"+code+"}\n")
		}
		return helper + "(" + strings.Join(captures, ", ") + ")", nil
	case "go":
		return "func() any {\n" + code + "}()", nil
	case "rust":
		return "(|| -> RValue {\n" + code + "})()", nil
	case "cpp":
		return "[&]() -> RValue {\n" + code + "}()", nil
	case "java":
		return "((java.util.function.Supplier<Object>)(() -> {\n" + code + "})).get()", nil
	case "csharp":
		return "((Func<object>)(() => {\n" + code + "}))()", nil
	case "julia":
		return "(() -> begin\n" + code + "end)()", nil
	case "kotlin":
		return "run " + label + "@ {\n" + code + "error(\"invalid function state\")\n}", nil
	case "swift":
		return "{ () -> Any in\n" + code + "}()", nil
	case "zig":
		return label + ": {\n" + code + "}", nil
	}
	return "", fmt.Errorf("missing state wrapper for %s", g.target)
}
