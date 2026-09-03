package matrixir

import (
	"fmt"
	"strconv"
	"strings"
)

type rangeLowering struct {
	Name, Begin, End, Condition, Advance string
	Counting, Iterable                   bool
	Sequence                             string
	// BindingNames is an ordered, non-resting destructuring pattern. It is a
	// frontend fact for the existing BindingPattern UAST structure, never a
	// source-specific AST node.
	BindingNames []string
	// [begin,end,1] * Affine normalizes an exclusive upper endpoint.
	Affine Matrix
}

func planRange(source, text string, profile Vector) (rangeLowering, error) {
	p := rangeLowering{Affine: NewMatrix(3, 3)}
	for i := 0; i < 3; i++ {
		p.Affine.Set(i, i, 1)
	}
	h := headerExpression(text, "for")
	clauses := splitTopLevel(h, ';')
	if len(clauses) == 3 {
		var ok bool
		p.Name, p.Begin, ok = assignmentExpression(significant(Tokenize(source, clauses[0])), clauses[0])
		if !ok {
			return p, fmt.Errorf("counting-loop initialization is not supported")
		}
		p.Counting = true
		p.Condition = normalizeExpression(source, clauses[1], profile)
		step := strings.Join(strings.Fields(clauses[2]), "")
		switch step {
		case p.Name + "++", "++" + p.Name, p.Name + "+=1":
			p.Advance = p.Name + " <- " + p.Name + " + 1"
		case p.Name + "--", "--" + p.Name, p.Name + "-=1":
			p.Advance = p.Name + " <- " + p.Name + " - 1"
		default:
			return p, fmt.Errorf("counting-loop step %q requires explicit lowering", clauses[2])
		}
		p.Begin = normalizeExpression(source, p.Begin, profile)
		return p, nil
	}
	expression := ""
	if in := strings.Index(h, " in "); in >= 0 {
		p.Name = strings.TrimSpace(h[:in])
		expression = strings.TrimSpace(h[in+4:])
	} else if at := strings.Index(h, ":"); at >= 0 && !strings.Contains(h, "::") && !strings.Contains(h, "..") {
		// C-family range-for uses a binding : iterable contract.  It has the
		// same existing ForEach/BindingPattern representation as `in` forms;
		// this is a grammar-shape projection, not a target-specific rewrite.
		p.Name = strings.TrimSpace(h[:at])
		expression = strings.TrimSpace(h[at+1:])
	} else if source == "zig" {
		open, close := strings.Index(h, "("), strings.Index(h, ")")
		if open >= 0 && close > open {
			expression = h[open+1 : close]
			p.Name = strings.Trim(strings.TrimSpace(h[close+1:]), "| ")
		}
	}
	names := significant(Tokenize(source, p.Name))
	if len(names) != 1 || names[0].Class != TokenIdentifier {
		pattern := bindingNames(source, p.Name)
		if len(pattern) == 0 {
			return p, fmt.Errorf("range binding is not a single identifier")
		}
		if len(pattern) == 1 {
			p.Name = pattern[0]
		} else {
			p.BindingNames = pattern
			p.Name = "(" + strings.Join(pattern, ",") + ")"
		}
	}
	exclusive := 0.0
	if strings.HasPrefix(expression, "range(") && strings.HasSuffix(expression, ")") {
		parts := splitTopLevel(expression[len("range("):len(expression)-1], ',')
		switch len(parts) {
		case 1:
			p.Begin, p.End = "0", parts[0]
		case 2:
			p.Begin, p.End = parts[0], parts[1]
		case 3:
			// A literal non-zero Python step has a complete common contract:
			// evaluate start/end once, exclude end in the step direction, then
			// iterate the existing `seq` runtime primitive.  Symbolic steps still
			// need a separate zero/direction proof and stay explicit gaps.
			step, err := strconv.Atoi(strings.TrimSpace(parts[2]))
			if err != nil || step == 0 {
				return p, fmt.Errorf("range step requires explicit signed-step semantics")
			}
			if step == 1 {
				p.Begin, p.End = parts[0], parts[1]
				break
			}
			begin := normalizeExpression(source, parts[0], profile)
			end := normalizeExpression(source, parts[1], profile)
			adjust := "- 1"
			if step < 0 {
				adjust = "+ 1"
			}
			p.Iterable = true
			p.Sequence = fmt.Sprintf("seq(%s, (%s) %s, by = %d)", begin, end, adjust, step)
			return p, nil
		default:
			return p, fmt.Errorf("range step requires explicit signed-step semantics")
		}
		exclusive = 1
	} else {
		found := false
		for _, op := range []string{"..=", "...", "..", ":"} {
			if at := strings.Index(expression, op); at >= 0 {
				p.Begin, p.End = expression[:at], expression[at+len(op):]
				found = true
				if op == ".." && (source == "rust" || profile[GrammarExclusiveRangeEnd] != 0) {
					exclusive = 1
				}
				break
			}
		}
		if !found {
			// Python's `for name in iterable` is already represented by the
			// canonical ForEachStmt and its target emitters.  It is not a numeric
			// range, so forcing it through endpoint arithmetic both loses semantics
			// and rejects otherwise proven source programs.
			if strings.TrimSpace(expression) != "" {
				p.Iterable = true
				p.Sequence = normalizeExpression(source, expression, profile)
				return p, nil
			}
			return p, fmt.Errorf("iterable range %q requires an iterable representation", expression)
		}
	}
	if strings.TrimSpace(p.Begin) == "" || strings.TrimSpace(p.End) == "" {
		return p, fmt.Errorf("range endpoint is missing")
	}
	p.Begin = normalizeExpression(source, p.Begin, profile)
	p.End = normalizeExpression(source, p.End, profile)
	p.Affine.Set(2, 1, -exclusive)
	// Numeric endpoints are computed with the same affine matrix as symbolic
	// ones. Symbolic expressions retain their dependency on the bound value.
	if end, err := strconv.Atoi(strings.TrimSpace(p.End)); err == nil {
		values, _ := MatrixFromRows([][]float64{{0, float64(end), 1}})
		normalized, _ := values.Multiply(p.Affine)
		p.End = strconv.FormatFloat(normalized.At(0, 1), 'f', -1, 64)
	} else if coefficient := p.Affine.At(2, 1); coefficient != 0 {
		p.End = "(" + p.End + ") - " + strconv.FormatFloat(-coefficient, 'f', -1, 64)
	}
	return p, nil
}

// simpleBindingPattern accepts only an ordered tuple/list of distinct
// identifier bindings. It deliberately ignores declaration modifiers and
// known type words, so `auto [key, value]`, `(key, value)` and `[key, value]`
// share the existing BindingPattern UAST contract. Starred, nested and
// attribute patterns remain explicit gaps.
func simpleBindingPattern(source, text string) []string {
	out := bindingNames(source, text)
	if len(out) < 2 {
		return nil
	}
	return out
}

func bindingNames(source, text string) []string {
	t := strings.TrimSpace(text)
	// Rest/starred bindings have an arity contract that the existing
	// BindingPattern structure does not prove.  Do not silently turn one into
	// an ordinary identifier tuple merely because its name token is available.
	if strings.Contains(t, "*") {
		return nil
	}
	if (strings.HasPrefix(t, "(") && strings.HasSuffix(t, ")")) || (strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]")) {
		t = strings.TrimSpace(t[1 : len(t)-1])
	}
	parts := splitTopLevel(t, ',')
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		var name string
		for _, token := range significant(Tokenize(source, part)) {
			if token.Class == TokenIdentifier && token.Text != "_" && !isTypeWord(token.Text) && !isFunctionWord(token.Text) && !isBindingModifier(token.Text) {
				name = token.Text
			}
		}
		if name == "" || seen[name] {
			return nil
		}
		seen[name] = true
		out = append(out, name)
	}
	return out
}

func isBindingModifier(text string) bool {
	switch strings.ToLower(text) {
	case "let", "var", "val", "mut", "const", "final", "ref", "inout", "auto", "public", "private", "static":
		return true
	}
	return false
}
