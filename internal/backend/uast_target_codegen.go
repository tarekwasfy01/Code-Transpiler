package backend

import (
	"fmt"
	"strings"
)

func directVectorBinary(target, left, right, op string) (string, bool) {
	switch target {
	case "go":
		return "func() []float64 { out := make([]float64, len(" + left + ")); for i, v := range " + left + " { out[i] = v " + op + " " + right + " }; return out }()", true
	case "python":
		return "[v " + op + " " + right + " for v in " + left + "]", true
	case "rust":
		return left + ".iter().map(|v| *v " + op + " " + right + ").collect::<Vec<f64>>()", true
	case "cpp":
		return "[&](){ std::vector<double> out = " + left + "; for (double& v : out) v = v " + op + " " + right + "; return out; }()", true
	case "csharp":
		return left + ".Select(v => v " + op + " " + right + ").ToArray()", true
	case "java":
		return "java.util.Arrays.stream(" + left + ").map(v -> v " + op + " " + right + ").toArray()", true
	case "kotlin":
		return left + ".map { it " + op + " " + right + " }", true
	case "swift":
		return left + ".map { $0 " + op + " " + right + " }", true
	default:
		return "", false
	}
}

func isFallbackProjectionStructure(kind string) bool {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return false
	}
	for _, contract := range registry.Contracts {
		if contract.StructureKind == kind {
			return contract.ProjectionForm == projectionFormFallback
		}
	}
	return false
}

// uastFallbackExpression is the one shared residual primitive. It emits valid
// target syntax through the existing runtime dispatcher while retaining an
// explicit unsupported-operation error at execution time. No node-specific
// semantics or target-language heuristics are introduced.
func (g *targetGen) uastFallbackExpression(graph *uastExecutionGraph, id int) (string, error) {
	node := graph.nodes[id]
	if node == nil {
		return "", fmt.Errorf("missing fallback UAST node %d", id)
	}
	return emitDispatch(g.target, "__uast_unsupported", []string{targetString(g.target, node.StructuralKind), targetNumber(g.target, fmt.Sprintf("%d", node.ID))}), nil
}

func (g *targetGen) uastFallbackStatement(graph *uastExecutionGraph, id int) error {
	expression, err := g.uastFallbackExpression(graph, id)
	if err != nil {
		return err
	}
	g.line(exprStmt(g.target, expression))
	return nil
}

// uastStatement and uastExpression are the generic backend's direct UAST
// emitter. They read only graph nodes, crosswalk fields and proved relations.
// The only temporary compatibility object is an isolated function view used by
// the existing flow-safe inline lowering; it is never a full document and is
// never written back to the UAST.
func (g *targetGen) uastStatement(graph *uastExecutionGraph, id int) error {
	if node := graph.nodes[id]; node != nil && isMetadataOnlyProjectionStructure(node.StructuralKind) {
		// The schema-derived contract proves that this node has no target
		// syntax channel. Its facts were already consumed by validation and
		// requirements, so emitting no token preserves the program syntax
		// without fabricating a target construct.
		return nil
	}
	if node := graph.nodes[id]; node != nil && isFallbackProjectionStructure(node.StructuralKind) {
		return g.uastFallbackStatement(graph, id)
	}
	c := graph.common[id]
	one := func(role string, required bool) (int, bool, error) { return graph.one(id, role, required) }
	if genericMutableDeclarationStructures[graph.nodes[id].StructuralKind] {
		if c.Name == "" {
			return fmt.Errorf("variable declaration node %d lacks a binding name", id)
		}
		initializer, found, err := graph.firstChild(id, "initializer", "expression", "value")
		if err != nil {
			return err
		}
		value := targetNull(g.target)
		if found {
			value, err = g.uastExpression(graph, initializer)
			if err != nil {
				return err
			}
		}
		g.line(g.assignment(g.name(c.Name), value))
		return nil
	}
	if genericDeclarationGroupStructures[graph.nodes[id].StructuralKind] {
		for _, child := range graph.orderedChildren(id) {
			if child.Meta.Missing {
				return fmt.Errorf("declaration group node %d has a missing child", id)
			}
			if err := g.uastStatement(graph, child.ID); err != nil {
				return err
			}
		}
		return nil
	}
	switch c.Kind {
	case "block":
		if g.target == "python" {
			for _, item := range graph.many(id, "statement") {
				if err := g.uastStatement(graph, item.ID); err != nil {
					return err
				}
			}
			return nil
		}
		g.line("{")
		g.indent++
		for _, item := range graph.many(id, "statement") {
			if err := g.uastStatement(graph, item.ID); err != nil {
				return err
			}
		}
		g.indent--
		g.line("}")
	case "assign":
		expression, _, err := one("expression", true)
		if err != nil {
			return err
		}
		n := g.name(c.Name)
		if graph.common[expression].Kind == "function" {
			return g.uastFunctionAssign(graph, n, expression)
		}
		if g.nativeDirect && graph.common[expression].Kind == "call" {
			callee, _, _ := graph.oneRelationNode(expression, "call.calls", false)
			if callee >= 0 && graph.common[callee].Kind == "identifier" && graph.common[callee].Name == "c" {
				g.directVectors[n] = true
			}
		}
		value, err := g.uastExpression(graph, expression)
		if err != nil {
			return err
		}
		if g.nativeDirect && g.directVectors[n] {
			g.declared[len(g.declared)-1][n] = true
			switch g.target {
			case "go":
				g.line("var " + n + " []float64 = " + value)
			case "rust":
				g.line("let mut " + n + " = " + value + ";")
			case "cpp":
				g.line("std::vector<double> " + n + " = " + value + ";")
			case "java":
				g.line("var " + n + " = " + value + ";")
			default:
				g.line(g.assignment(n, value))
			}
		} else {
			g.line(g.assignment(n, value))
		}
	case "expression":
		expression, _, err := one("expression", true)
		if err != nil {
			return err
		}
		value, err := g.uastExpression(graph, expression)
		if err != nil {
			return err
		}
		g.line(exprStmt(g.target, value))
	case "if":
		condition, _, err := one("condition", true)
		if err != nil {
			return err
		}
		then, _, err := graph.oneRelationNode(id, "control.true", true)
		if err != nil {
			return err
		}
		other, hasElse, err := graph.oneRelationNode(id, "control.false", false)
		if err != nil {
			return err
		}
		conditionText, err := g.uastExpression(graph, condition)
		if err != nil {
			return err
		}
		switch g.target {
		case "python":
			g.line("if r_truth(" + conditionText + "):")
			g.indent++
			err = g.uastStatementBody(graph, then)
			g.indent--
			if hasElse {
				g.line("else:")
				g.indent++
				err = g.uastStatementBody(graph, other)
				g.indent--
			}
			return err
		case "julia":
			g.line("if r_truth(" + conditionText + ")")
			g.indent++
			if err = g.uastStatementBody(graph, then); err != nil {
				return err
			}
			g.indent--
			if hasElse {
				g.line("else")
				g.indent++
				if err = g.uastStatementBody(graph, other); err != nil {
					return err
				}
				g.indent--
			}
			g.line("end")
		case "nim":
			g.line("if rTruth(" + conditionText + "):")
			g.indent++
			if err = g.uastStatementBody(graph, then); err != nil {
				return err
			}
			g.indent--
			if hasElse {
				g.line("else:")
				g.indent++
				if err = g.uastStatementBody(graph, other); err != nil {
					return err
				}
				g.indent--
			}
		default:
			g.line("if (" + truthCall(g.target, conditionText) + ") {")
			g.indent++
			if err = g.uastStatementBody(graph, then); err != nil {
				return err
			}
			g.indent--
			if hasElse {
				if g.target == "go" {
					g.line("} else {")
				} else {
					g.line("}")
					g.line("else {")
				}
				g.indent++
				if err = g.uastStatementBody(graph, other); err != nil {
					return err
				}
				g.indent--
			}
			g.line("}")
		}
	case "while":
		condition, _, err := one("condition", true)
		if err != nil {
			return err
		}
		body, _, err := one("body", true)
		if err != nil {
			return err
		}
		conditionText, err := g.uastExpression(graph, condition)
		if err != nil {
			return err
		}
		if g.target == "python" {
			g.line("while r_truth(" + conditionText + "):")
			g.indent++
			err = g.uastStatementBody(graph, body)
			g.indent--
			return err
		}
		if g.target == "julia" {
			g.line("while r_truth(" + conditionText + ")")
			g.indent++
			err = g.uastStatementBody(graph, body)
			g.indent--
			g.line("end")
			return err
		}
		if g.target == "nim" {
			g.line("while rTruth(" + conditionText + "):")
			g.indent++
			err = g.uastStatementBody(graph, body)
			g.indent--
			return err
		}
		if g.target == "go" {
			g.line("for " + truthCall(g.target, conditionText) + " {")
		} else {
			g.line("while (" + truthCall(g.target, conditionText) + ") {")
		}
		g.indent++
		err = g.uastStatementBody(graph, body)
		g.indent--
		g.line("}")
		return err
	case "for":
		sequence, _, err := one("sequence", true)
		if err != nil {
			return err
		}
		body, _, err := one("body", true)
		if err != nil {
			return err
		}
		sequenceText, err := g.uastExpression(graph, sequence)
		if err != nil {
			return err
		}
		n := g.name(c.Name)
		switch g.target {
		case "python":
			g.line("for " + n + " in r_iter(" + sequenceText + "):")
		case "julia":
			g.line("for " + n + " in r_iter(" + sequenceText + ")")
		case "nim":
			g.line("for " + n + " in rIter(" + sequenceText + "):")
		case "go":
			g.line("for _, " + n + " := range rIter(" + sequenceText + ") {")
		case "rust":
			g.line("for " + n + " in r_iter(" + sequenceText + ") {")
		case "cpp":
			g.line("for (const auto& " + n + " : r_iter(" + sequenceText + ")) {")
		case "csharp":
			g.line("foreach (var " + n + " in R2.Iter(" + sequenceText + ")) {")
		case "java":
			g.line("for (Object " + n + " : R2.rIter(" + sequenceText + ")) {")
		case "c":
			g.temp++
			sequenceName, index := fmt.Sprintf("__sequence_%d", g.temp), fmt.Sprintf("__index_%d", g.temp)
			g.line("RValue " + sequenceName + " = " + sequenceText + ";")
			g.line("for (size_t " + index + " = 0; " + index + " < " + sequenceName + ".len; ++" + index + ") {")
			g.indent++
			g.line("RValue " + n + " = " + sequenceName + ".v[" + index + "];")
			err = g.uastStatementBody(graph, body)
			g.indent--
			g.line("}")
			return err
		case "kotlin":
			g.line("for (" + n + " in rIter(" + sequenceText + ")) {")
		case "swift":
			g.line("for " + n + " in rIter(" + sequenceText + ") {")
		case "zig":
			g.line("for (rIter(" + sequenceText + ")) |" + n + "| {")
		default:
			return fmt.Errorf("target %s has no iterable-loop lowering; refusing to omit its body", g.target)
		}
		g.indent++
		err = g.uastStatementBody(graph, body)
		g.indent--
		if g.target == "julia" {
			g.line("end")
		} else if g.target != "python" && g.target != "nim" {
			g.line("}")
		}
		return err
	case "repeat":
		body, _, err := one("body", true)
		if err != nil {
			return err
		}
		switch g.target {
		case "python":
			g.line("while True:")
		case "julia":
			g.line("while true")
		case "nim":
			g.line("while true:")
		default:
			g.line("for (;;)")
			return g.uastStatement(graph, body)
		}
		g.indent++
		err = g.uastStatementBody(graph, body)
		g.indent--
		if g.target == "julia" {
			g.line("end")
		}
		return err
	case "return":
		if expression, ok, err := one("expression", false); err != nil {
			return err
		} else if !ok {
			g.line(returnNull(g.target))
		} else {
			value, err := g.uastExpression(graph, expression)
			if err != nil {
				return err
			}
			g.line(returnExpr(g.target, value))
		}
	case "break":
		g.line("break" + stmtEnd(g.target))
	case "continue":
		if g.target == "python" || g.target == "julia" || g.target == "nim" {
			g.line("continue")
		} else {
			g.line("continue;")
		}
	default:
		return fmt.Errorf("universal node %d kind %q has no direct statement lowering", id, c.Kind)
	}
	return nil
}

func (g *targetGen) uastStatementBody(graph *uastExecutionGraph, id int) error {
	if graph.common[id].Kind == "block" {
		for _, item := range graph.many(id, "statement") {
			if err := g.uastStatement(graph, item.ID); err != nil {
				return err
			}
		}
		return nil
	}
	return g.uastStatement(graph, id)
}

func (g *targetGen) uastExpression(graph *uastExecutionGraph, id int) (string, error) {
	c := graph.common[id]
	one := func(role string) (int, error) { value, _, err := graph.one(id, role, true); return value, err }
	if node := graph.nodes[id]; node != nil && isFallbackProjectionStructure(node.StructuralKind) {
		return g.uastFallbackExpression(graph, id)
	}
	// AggregateExpr has one shared runtime-preserving lowering: its ordered
	// syntax children form a target runtime list.  It deliberately uses only
	// the UAST child relation and preserves the same value contract as the
	// direct UAST executor; no source-language aggregate syntax is assumed.
	if genericProjectionStructures[graph.nodes[id].StructuralKind] {
		return g.uastAggregateExpression(graph, id)
	}
	switch c.Kind {
	case "identifier":
		for i := len(g.bindings) - 1; i >= 0; i-- {
			if value, ok := g.bindings[i][g.name(c.Name)]; ok {
				return value, nil
			}
		}
		if strings.HasPrefix(c.Name, "\x00") {
			return "", fmt.Errorf("unbound internal state slot %q", c.Name)
		}
		switch c.Name {
		case "TRUE", "T":
			return targetBool(g.target, true), nil
		case "FALSE", "F":
			return targetBool(g.target, false), nil
		case "NULL":
			return targetNull(g.target), nil
		case "NA", "NA_real_", "NA_integer_", "NA_character_", "NA_complex_", "NaN":
			return targetNA(g.target), nil
		case "Inf":
			return targetInf(g.target), nil
		case "pi":
			return targetNumber(g.target, "3.14159265358979323846"), nil
		}
		name := g.name(c.Name)
		g.cValues[name] = true
		if g.target == "rust" {
			return name + ".clone()", nil
		}
		return name, nil
	case "literal":
		switch c.Operation.LiteralKind {
		case "string":
			return targetString(g.target, unquote(c.Operation.Text)), nil
		case "null":
			return targetNull(g.target), nil
		case "na", "nan":
			return targetNA(g.target), nil
		case "boolean":
			return targetBool(g.target, c.Operation.Text == "TRUE" || c.Operation.Text == "T"), nil
		}
		return targetNumber(g.target, strings.TrimSuffix(c.Operation.Text, "L")), nil
	case "unary":
		value, err := one("value")
		if err != nil {
			return "", err
		}
		text, err := g.uastExpression(graph, value)
		if err != nil {
			return "", err
		}
		return g.lowerUnary(c.Operation.Operator, text)
	case "binary":
		operands, err := graph.relationNodes(id, "data.operand")
		if err != nil || len(operands) != 2 {
			if err != nil {
				return "", err
			}
			return "", fmt.Errorf("binary node %d lacks two proved data.operand relations", id)
		}
		left, err := one("left")
		if err != nil {
			return "", err
		}
		right, err := one("right")
		if err != nil {
			return "", err
		}
		if operands[0] != left || operands[1] != right {
			return "", fmt.Errorf("binary node %d operand relation disagrees with syntax fields", id)
		}
		a, err := g.uastExpression(graph, left)
		if err != nil {
			return "", err
		}
		b, err := g.uastExpression(graph, right)
		if err != nil {
			return "", err
		}
		if c.Operation.Operator == "&&" || c.Operation.Operator == "||" {
			return g.lowerLogical(c.Operation.Operator, a, b), nil
		}
		if g.nativeDirect && g.directVectors[g.name(graph.common[left].Name)] {
			if direct, ok := directVectorBinary(g.target, a, b, c.Operation.Operator); ok {
				return direct, nil
			}
		}
		if !g.uastEffectFree(graph, left) || !g.uastEffectFree(graph, right) {
			leftName, rightName := g.freshName("left"), g.freshName("right")
			g.cValues[leftName], g.cValues[rightName] = true, true
			return g.letExpression([]valueBinding{{leftName, a}, {rightName, b}}, emitDispatch(g.target, "__binary_"+c.Operation.Operator, []string{leftName, rightName})), nil
		}
		return emitDispatch(g.target, "__binary_"+c.Operation.Operator, []string{a, b}), nil
	case "typed_operation":
		return g.uastTypedOperation(graph, id)
	case "index":
		value, err := one("value")
		if err != nil {
			return "", err
		}
		container, err := g.uastExpression(graph, value)
		if err != nil {
			return "", err
		}
		args, err := g.uastArguments(graph, id)
		if err != nil {
			return "", err
		}
		name := "["
		if c.Operation.DoubleIndex {
			name = "[["
		}
		return emitDispatch(g.target, name, append([]string{container}, args...)), nil
	case "call":
		callee, _, err := graph.oneRelationNode(id, "call.calls", true)
		if err != nil {
			return "", err
		}
		args, err := g.uastArguments(graph, id)
		if err != nil {
			return "", err
		}
		if graph.common[callee].Kind == "identifier" {
			name := g.name(graph.common[callee].Name)
			if g.nativeDirect {
				if direct := directNativeCall(g.target, name, args); direct != "" {
					return direct, nil
				}
			}
			if fn, ok := g.uastFunctions[name]; ok {
				if g.uastInline[name] {
					if c.Operation.Operator == "eager_left_to_right" {
						previous := g.evaluation
						g.evaluation = "eager_left_to_right"
						result, err := g.uastInlineCall(graph, fn, id, args)
						g.evaluation = previous
						return result, err
					}
					return g.uastInlineCall(graph, fn, id, args)
				}
				if g.evaluation == "lazy_demand" && g.uastEffectfulCall(graph, id) {
					return "", fmt.Errorf("lazy effectful argument requires a promise in this function body")
				}
				return callUser(g.target, name, args), nil
			}
			return emitDispatch(g.target, graph.common[callee].Name, args), nil
		}
		value, err := g.uastExpression(graph, callee)
		if err != nil {
			return "", err
		}
		return emitDispatch(g.target, "__call_value", append([]string{value}, args...)), nil
	case "iteration":
		value, err := one("value")
		if err != nil {
			return "", err
		}
		text, err := g.uastExpression(graph, value)
		if err != nil {
			return "", err
		}
		switch c.Operation.Operator {
		case "snapshot":
			return g.snapshotIteration(text)
		case "size":
			return emitDispatch(g.target, "length", []string{text}), nil
		default:
			return "", fmt.Errorf("unknown iteration intrinsic %q", c.Operation.Operator)
		}
	case "missing_argument":
		return "", nil
	case "function":
		return "", fmt.Errorf("anonymous function must be assigned to a name")
	default:
		return "", fmt.Errorf("universal node %d kind %q has no direct expression lowering", id, c.Kind)
	}
}

func directNativeCall(target, name string, args []string) string {
	joined := strings.Join(args, ", ")
	// A native call is valid only when its already-rendered arguments contain
	// no runtime value or dispatcher.  This keeps the decision data-driven at
	// the common call boundary: proven native expressions use target syntax,
	// mixed expressions continue through the explicit compatibility fallback.
	if strings.Contains(joined, "rCall(") || strings.Contains(joined, "r_call(") || strings.Contains(joined, "RValue") || strings.Contains(joined, "r_call") {
		return ""
	}
	switch target {
	case "go":
		if name == "c" {
			return "[]float64{" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "fmt.Println(" + joined + ")"
		}
	case "python":
		if name == "c" {
			return "[" + joined + "]"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "print(" + joined + ")"
		}
	case "julia":
		if name == "c" {
			return "[" + joined + "]"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "println(" + joined + ")"
		}
	case "nim":
		if name == "c" {
			return "@[" + joined + "]"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "echo " + joined
		}
	case "swift":
		if name == "c" {
			return "[" + joined + "]"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "print(" + joined + ")"
		}
	case "rust":
		if name == "c" {
			return "vec![" + joined + "]"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "println!(\"{:?}\", " + joined + ");"
		}
	case "cpp":
		if name == "c" {
			return "std::vector<double>{" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "std::cout << " + joined + " << std::endl"
		}
	case "c":
		if name == "c" {
			return "(double[]){" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "printf(\"%g\\n\", (double)(" + joined + "))"
		}
	case "zig":
		if name == "c" {
			return "[_]f64{" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "std.debug.print(\"{any}\\n\", .{" + joined + "})"
		}
	case "csharp":
		if name == "c" {
			return "new double[]{" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "Console.WriteLine(" + joined + ")"
		}
	case "java":
		if name == "c" {
			return "new double[]{" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "System.out.println(" + joined + ")"
		}
	case "kotlin":
		if name == "c" {
			return "listOf(" + joined + ")"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "println(" + joined + ")"
		}
	}
	return ""
}

func (g *targetGen) uastAggregateExpression(graph *uastExecutionGraph, id int) (string, error) {
	items := graph.orderedChildren(id)
	values := make([]string, 0, len(items))
	for _, item := range items {
		if item.Meta.Missing {
			values = append(values, targetNull(g.target))
			continue
		}
		value, err := g.uastExpression(graph, item.ID)
		if err != nil {
			return "", err
		}
		values = append(values, value)
	}
	return emitDispatch(g.target, "list", values), nil
}

func (g *targetGen) uastArguments(graph *uastExecutionGraph, id int) ([]string, error) {
	items := graph.many(id, "argument")
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item.Meta.Missing {
			out = append(out, targetNull(g.target))
			continue
		}
		value, err := g.uastExpression(graph, item.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

func (g *targetGen) uastTypedOperation(graph *uastExecutionGraph, id int) (string, error) {
	c := graph.common[id]
	operation := c.Operation.Typed
	args := graph.many(id, "argument")
	if operation == nil {
		return "", fmt.Errorf("typed operation node %d lacks operation", id)
	}
	if err := operation.validate(len(args)); err != nil {
		return "", err
	}
	if err := TypedImplementationMatrix().Check([]string{operation.Name}, "target."+g.target); err != nil {
		return "", err
	}
	bindings, values := []valueBinding{}, []string{}
	for _, arg := range args {
		if arg.Meta.Missing || arg.Meta.Name != "" {
			return "", fmt.Errorf("typed operation node %d has non-positional or missing operand", id)
		}
		value, err := g.uastExpression(graph, arg.ID)
		if err != nil {
			return "", err
		}
		name := g.freshName("integer")
		g.cValues[name] = true
		bindings, values = append(bindings, valueBinding{name, value}), append(values, name)
	}
	signed := "false"
	if *operation.Type.Signed {
		signed = "true"
	}
	spec, ok := targetSpec(g.target)
	if !ok || spec.TypedOperations.Form == "unsupported" {
		return "", fmt.Errorf("no typed operation specification for target %q", g.target)
	}
	adapter := spec.TypedOperations
	var result string
	switch adapter.Form {
	case "c":
		flag := 0
		if *operation.Type.Signed {
			flag = 1
		}
		payload := "NULL"
		if len(values) > 0 {
			payload = "(RValue[]){" + strings.Join(values, ", ") + "}"
		}
		result = fmt.Sprintf("%s(%q, %d, %d, %q, %s, %d)", adapter.Runtime, operation.Name, operation.Type.Bits, flag, operation.Text, payload, len(values))
	default:
		if *operation.Type.Signed {
			signed = adapter.SignedTrue
		} else {
			signed = adapter.SignedFalse
		}
		result = fmt.Sprintf("%s(%q, %d, %s, %q, %s%s%s)", adapter.Runtime, operation.Name, operation.Type.Bits, signed, operation.Text, adapter.ArgumentsOpen, strings.Join(values, ", "), adapter.ArgumentsClose)
	}
	return g.letExpression(bindings, result), nil
}

func (g *targetGen) uastEffectFree(graph *uastExecutionGraph, id int) bool {
	c := graph.common[id]
	if genericProjectionStructures[graph.nodes[id].StructuralKind] {
		for _, child := range graph.orderedChildren(id) {
			if !child.Meta.Missing && !g.uastEffectFree(graph, child.ID) {
				return false
			}
		}
		return true
	}
	switch c.Kind {
	case "literal", "identifier":
		return true
	case "typed_operation":
		return c.Operation.Typed != nil && c.Operation.Typed.Name == "integer.literal"
	case "unary", "iteration":
		value, ok, _ := graph.one(id, "value", false)
		return ok && g.uastEffectFree(graph, value)
	case "binary":
		left, lok, _ := graph.one(id, "left", false)
		right, rok, _ := graph.one(id, "right", false)
		return lok && rok && g.uastEffectFree(graph, left) && g.uastEffectFree(graph, right)
	case "index":
		value, ok, _ := graph.one(id, "value", false)
		if !ok || !g.uastEffectFree(graph, value) {
			return false
		}
		for _, item := range graph.many(id, "argument") {
			if !item.Meta.Missing && !g.uastEffectFree(graph, item.ID) {
				return false
			}
		}
		return true
	case "call":
		callee, ok, _ := graph.oneRelationNode(id, "call.calls", false)
		if !ok || graph.common[callee].Kind != "identifier" {
			return false
		}
		for _, item := range graph.many(id, "argument") {
			if !item.Meta.Missing && !g.uastEffectFree(graph, item.ID) {
				return false
			}
		}
		switch graph.common[callee].Name {
		case "c", "list", "length":
			return true
		}
	}
	return false
}
