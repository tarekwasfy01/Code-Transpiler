package backend

import (
	"fmt"
	"strconv"
	"strings"
)

func universalRSource(u *UniversalASTDocument, enforce bool) (string, error) {
	g, err := newUASTExecutionGraph(u)
	if err != nil {
		return "", err
	}
	w := &semanticWriter{eager: enforce && u.Evaluation == "eager_left_to_right", enforce: enforce, used: reserveUASTSymbols(g)}
	out, err := w.uastStatement(g, g.root)
	if err != nil {
		return "", err
	}
	return strings.Join(w.helpers, "\n") + out, nil
}

func reserveUASTSymbols(g *uastExecutionGraph) map[string]bool {
	names := map[string]bool{}
	for _, c := range g.common {
		if c.Name != "" {
			names[safeName(c.Name)] = true
		}
	}
	return names
}

func (w *semanticWriter) uastExpression(g *uastExecutionGraph, id int) (string, error) {
	c := g.common[id]
	one := func(role string) (int, error) { q, _, err := g.one(id, role, true); return q, err }
	if genericProjectionStructures[g.nodes[id].StructuralKind] {
		items := g.orderedChildren(id)
		values := make([]string, 0, len(items))
		for _, item := range items {
			if item.Meta.Missing {
				values = append(values, "NULL")
				continue
			}
			value, err := w.uastExpression(g, item.ID)
			if err != nil {
				return "", err
			}
			values = append(values, value)
		}
		return "list(" + strings.Join(values, ", ") + ")", nil
	}
	switch c.Kind {
	case "literal":
		if c.Operation.LiteralKind == "string" {
			return strconv.Quote(unquote(c.Operation.Text)), nil
		}
		return c.Operation.Text, nil
	case "identifier":
		return c.Name, nil
	case "unary":
		x, err := one("value")
		if err != nil {
			return "", err
		}
		a, err := w.uastExpression(g, x)
		return "(" + c.Operation.Operator + a + ")", err
	case "binary":
		left, err := one("left")
		if err != nil {
			return "", err
		}
		right, err := one("right")
		if err != nil {
			return "", err
		}
		a, err := w.uastExpression(g, left)
		if err != nil {
			return "", err
		}
		b, err := w.uastExpression(g, right)
		return "(" + a + " " + c.Operation.Operator + " " + b + ")", err
	case "index":
		value, err := one("value")
		if err != nil {
			return "", err
		}
		a, err := w.uastExpression(g, value)
		if err != nil {
			return "", err
		}
		args, _, err := w.uastArguments(g, id)
		if err != nil {
			return "", err
		}
		open, close := "[", "]"
		if c.Operation.DoubleIndex {
			open, close = "[[", "]]"
		}
		return a + open + strings.Join(args, ", ") + close, nil
	case "call":
		callee, err := one("value")
		if err != nil {
			return "", err
		}
		fn, err := w.uastExpression(g, callee)
		if err != nil {
			return "", err
		}
		args, records, err := w.uastArguments(g, id)
		if err != nil {
			return "", err
		}
		if (w.eager || (w.enforce && c.Operation.Operator == "eager_left_to_right")) && len(args) > 0 {
			return w.eagerCall(fn, records, args)
		}
		return fn + "(" + strings.Join(args, ", ") + ")", nil
	case "function":
		if c.Operation.FunctionBinding != "" {
			return "", fmt.Errorf("R text cannot preserve %s", ExactSignatureCapability)
		}
		params := make([]string, len(g.many(id, "parameter")))
		for i, item := range g.many(id, "parameter") {
			p := g.common[item.ID]
			params[i] = p.Name
			if value, ok, err := g.one(item.ID, "default", false); err != nil {
				return "", err
			} else if ok {
				text, err := w.uastExpression(g, value)
				if err != nil {
					return "", err
				}
				params[i] += " = " + text
			}
		}
		body, err := one("body")
		if err != nil {
			return "", err
		}
		text, err := w.uastStatement(g, body)
		return "function(" + strings.Join(params, ", ") + ") {\n" + text + "}", err
	case "typed_operation":
		return "", fmt.Errorf("R text cannot preserve typed operation %q", c.Operation.Typed.Name)
	case "iteration":
		return "", fmt.Errorf("R text cannot serialize iteration expression")
	case "missing_argument":
		return "", nil
	default:
		return "", fmt.Errorf("no R serialization for universal expression kind %q", c.Kind)
	}
}

func (w *semanticWriter) uastArguments(g *uastExecutionGraph, id int) ([]string, []Arg, error) {
	items := g.many(id, "argument")
	out, records := make([]string, len(items)), make([]Arg, len(items))
	for i, item := range items {
		records[i] = Arg{Name: item.Meta.Name, Missing: item.Meta.Missing}
		if item.Meta.Missing {
			continue
		}
		value, err := w.uastExpression(g, item.ID)
		if err != nil {
			return nil, nil, err
		}
		if item.Meta.Name != "" {
			value = item.Meta.Name + " = " + value
		}
		out[i] = value
	}
	return out, records, nil
}

func (w *semanticWriter) uastStatement(g *uastExecutionGraph, id int) (string, error) {
	c := g.common[id]
	one := func(role string, required bool) (int, bool, error) { return g.one(id, role, required) }
	if genericMutableDeclarationStructures[g.nodes[id].StructuralKind] {
		if c.Name == "" {
			return "", fmt.Errorf("variable declaration node %d lacks a binding name", id)
		}
		initializer, found, err := g.firstChild(id, "initializer", "expression", "value")
		if err != nil {
			return "", err
		}
		value := "NULL"
		if found {
			value, err = w.uastExpression(g, initializer)
			if err != nil {
				return "", err
			}
		}
		return c.Name + " <- " + value + "\n", nil
	}
	if genericDeclarationGroupStructures[g.nodes[id].StructuralKind] {
		var out strings.Builder
		for _, child := range g.orderedChildren(id) {
			if child.Meta.Missing {
				return "", fmt.Errorf("declaration group node %d has a missing child", id)
			}
			text, err := w.uastStatement(g, child.ID)
			if err != nil {
				return "", err
			}
			out.WriteString(text)
		}
		return out.String(), nil
	}
	switch c.Kind {
	case "block":
		var out strings.Builder
		for _, item := range g.many(id, "statement") {
			text, err := w.uastStatement(g, item.ID)
			if err != nil {
				return "", err
			}
			out.WriteString(text)
		}
		return out.String(), nil
	case "assign":
		x, _, err := one("expression", true)
		if err != nil {
			return "", err
		}
		value, err := w.uastExpression(g, x)
		op := c.Operation.AssignOp
		if op == "" || op == "=" {
			op = "<-"
		}
		return c.Name + " " + op + " " + value + "\n", err
	case "expression":
		x, _, err := one("expression", true)
		if err != nil {
			return "", err
		}
		value, err := w.uastExpression(g, x)
		return value + "\n", err
	case "return":
		value := "NULL"
		if x, ok, err := one("expression", false); err != nil {
			return "", err
		} else if ok {
			value, err = w.uastExpression(g, x)
			if err != nil {
				return "", err
			}
		}
		return "return(" + value + ")\n", nil
	case "break":
		return "break\n", nil
	case "continue":
		return "next\n", nil
	case "if":
		condition, _, err := one("condition", true)
		if err != nil {
			return "", err
		}
		then, _, err := one("then", true)
		if err != nil {
			return "", err
		}
		conditionText, err := w.uastExpression(g, condition)
		if err != nil {
			return "", err
		}
		thenText, err := w.uastStatement(g, then)
		if err != nil {
			return "", err
		}
		out := "if (" + conditionText + ") {\n" + thenText + "}"
		if other, ok, err := one("else", false); err != nil {
			return "", err
		} else if ok {
			otherText, err := w.uastStatement(g, other)
			if err != nil {
				return "", err
			}
			out += " else {\n" + otherText + "}"
		}
		return out + "\n", nil
	case "while":
		condition, _, err := one("condition", true)
		if err != nil {
			return "", err
		}
		body, _, err := one("body", true)
		if err != nil {
			return "", err
		}
		conditionText, err := w.uastExpression(g, condition)
		if err != nil {
			return "", err
		}
		bodyText, err := w.uastStatement(g, body)
		return "while (" + conditionText + ") {\n" + bodyText + "}\n", err
	case "for":
		sequence, _, err := one("sequence", true)
		if err != nil {
			return "", err
		}
		body, _, err := one("body", true)
		if err != nil {
			return "", err
		}
		sequenceText, err := w.uastExpression(g, sequence)
		if err != nil {
			return "", err
		}
		bodyText, err := w.uastStatement(g, body)
		return "for (" + c.Name + " in " + sequenceText + ") {\n" + bodyText + "}\n", err
	case "repeat":
		body, _, err := one("body", true)
		if err != nil {
			return "", err
		}
		bodyText, err := w.uastStatement(g, body)
		return "repeat {\n" + bodyText + "}\n", err
	default:
		return "", fmt.Errorf("no R serialization for universal statement kind %q", c.Kind)
	}
}
