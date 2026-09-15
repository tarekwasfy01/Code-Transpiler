// Copyright (c) 2026 Tarek Wasfy
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
	BindingNames     []string
	BindingRestIndex int
	// [begin,end,1] * Affine normalizes an exclusive upper endpoint.
	Affine Matrix
}

func planRange(source, text string, profile Vector) (rangeLowering, error) {
	p := rangeLowering{Affine: NewMatrix(3, 3), BindingRestIndex: -1}
	for i := 0; i < 3; i++ {
		p.Affine.Set(i, i, 1)
	}
	h := headerExpression(text, "for")
	// Generated C-family output may put a complete counting loop and its body
	// on one physical line. Only counting headers use semicolon clauses, so
	// extract their balanced parenthesized header here without changing the
	// grammar shapes of Zig's `for (range) |binding|` and other range forms.
	if strings.HasPrefix(h, "(") {
		runes := []rune(h)
		if close := wrappingParenEnd(runes); close >= 0 {
			candidate := string(runes[1:close])
			if len(splitTopLevel(candidate, ';')) == 3 || source == "cpp" {
				// C++ range-for headers also use a parenthesized clause,
				// but contain no semicolon triplet.
				h = candidate
			}
		}
	}
	clauses := splitTopLevel(h, ';')
	if len(clauses) == 3 {
		var ok bool
		initText := strings.TrimSpace(clauses[0])
		if initText == "" {
			// Go permits an omitted initializer (`for ; cond; post`).  Keep
			// the binding empty and preserve the condition/post contracts.
			p.Name, p.Begin, ok = "", "", true
		} else {
			p.Name, p.Begin, ok = assignmentExpression(significant(Tokenize(source, initText)), initText)
		}
		// Go compiler sources commonly initialize a loop binding from a
		// method/function value (for example `it := live.Iterator()`).
		// assignmentExpression intentionally handles ordinary assignments, but
		// this is the same canonical binding contract: one identifier receives
		// one expression. Preserve the expression structurally instead of
		// rejecting it merely because its RHS is a call.
		if !ok {
			p.Name, p.Begin, ok = rangeBindingAssignment(source, initText)
		}
		if !ok {
			if name, value, updateOK := simpleUnaryLoopUpdate(source, initText); updateOK {
				p.Name, p.Begin, ok = name, value, true
			}
		}
		if !ok {
			return p, fmt.Errorf("counting-loop initialization is not supported")
		}
		p.Counting = true
		p.Condition = normalizeExpression(source, clauses[1], profile)
		// With an omitted initializer (`for ; cond; s = next(s)`), the post
		// assignment still identifies the loop binding. Recover that binding
		// structurally before interpreting the update expression.
		if p.Name == "" {
			if name, _, stepOK := rangeBindingAssignment(source, clauses[2]); stepOK {
				p.Name = name
			}
			if name, _, _, stepOK := compoundLoopUpdate(source, clauses[2]); stepOK {
				p.Name = name
			}
			if name, _, stepOK := simpleUnaryLoopUpdate(source, clauses[2]); stepOK {
				p.Name = name
			}
		}
		step := strings.Join(strings.Fields(clauses[2]), "")
		if step == "" {
			// A Go three-clause loop may omit its post statement. The loop
			// condition and body remain structured facts; an empty advance is
			// semantically meaningful and must not be rejected as missing data.
			p.Advance = ""
			p.Begin = normalizeExpression(source, p.Begin, profile)
			return p, nil
		}
		if name, value, updateOK := simpleUnaryLoopUpdate(source, clauses[2]); updateOK {
			if p.Name == "" {
				p.Name = name
			}
			p.Advance = p.Name + " <- " + value
			p.Begin = normalizeExpression(source, p.Begin, profile)
			return p, nil
		}
		switch step {
		case p.Name + "++", "++" + p.Name, p.Name + "+=1":
			p.Advance = p.Name + " <- " + p.Name + " + 1"
		case p.Name + "--", "--" + p.Name, p.Name + "-=1":
			p.Advance = p.Name + " <- " + p.Name + " - 1"
		default:
			if name, op, rhs, updateOK := compoundLoopUpdate(source, clauses[2]); updateOK && (p.Name == "" || p.Name == name) {
				if p.Name == "" {
					p.Name = name
				}
				p.Advance = loopUpdateExpression(name, op, normalizeExpression(source, rhs, profile))
				p.Begin = normalizeExpression(source, p.Begin, profile)
				return p, nil
			}
			// Compound updates are the same parameterized ADD/SUB primitive as
			// ++/--.  Preserve the update operand structurally instead of
			// rejecting generated loops merely because the stride is symbolic.
			// Assignment to a different binding remains fail-closed.
			if strings.HasPrefix(step, p.Name+"+=") && len(step) > len(p.Name)+2 {
				operand := normalizeExpression(source, step[len(p.Name)+2:], profile)
				p.Advance = p.Name + " <- " + p.Name + " + " + operand
			} else if strings.HasPrefix(step, p.Name+"-=") && len(step) > len(p.Name)+2 {
				operand := normalizeExpression(source, step[len(p.Name)+2:], profile)
				p.Advance = p.Name + " <- " + p.Name + " - " + operand
			} else if strings.HasPrefix(step, p.Name+"<<=") && len(step) > len(p.Name)+3 {
				operand := normalizeExpression(source, step[len(p.Name)+3:], profile)
				p.Advance = p.Name + " <- " + p.Name + " << " + operand
			} else if strings.HasPrefix(step, p.Name+">>=") && len(step) > len(p.Name)+3 {
				operand := normalizeExpression(source, step[len(p.Name)+3:], profile)
				p.Advance = p.Name + " <- " + p.Name + " >> " + operand
			} else {
				// C/C++ permits a comma expression in the post clause. Lower
				// each update independently and preserve their source order as
				// one canonical advance relation.
				updates := splitTopLevel(clauses[2], ',')
				if len(updates) > 1 {
					advances := make([]string, 0, len(updates))
					valid := true
					for _, update := range updates {
						part := strings.Join(strings.Fields(update), "")
						switch {
						case strings.HasPrefix(part, "++") && len(part) > 2:
							name := part[2:]
							advances = append(advances, name+" <- "+name+" + 1")
						case strings.HasSuffix(part, "++") && len(part) > 2:
							name := part[:len(part)-2]
							advances = append(advances, name+" <- "+name+" + 1")
						case strings.HasPrefix(part, "--") && len(part) > 2:
							name := part[2:]
							advances = append(advances, name+" <- "+name+" - 1")
						case strings.HasSuffix(part, "--") && len(part) > 2:
							name := part[:len(part)-2]
							advances = append(advances, name+" <- "+name+" - 1")
						default:
							if name, op, rhs, ok := compoundLoopUpdate(source, part); ok {
								advances = append(advances, loopUpdateExpression(name, op, normalizeExpression(source, rhs, profile)))
							} else if name, rhs, ok := rangeBindingAssignment(source, part); ok {
								advances = append(advances, name+" <- "+normalizeExpression(source, rhs, profile))
							} else {
								valid = false
							}
						}
					}
					if valid && len(advances) > 0 {
						p.Advance = strings.Join(advances, "; ")
						p.Begin = normalizeExpression(source, p.Begin, profile)
						return p, nil
					}
				}
				// General assignment updates (for example `s = (s - x) & mask`)
				// are already a structured binding plus expression. Preserve the
				// complete RHS as the loop-step contract instead of rejecting it
				// merely because it is not one of the shorthand operators above.
				if name, rhs, ok := rangeBindingAssignment(source, step); ok {
					p.Advance = name + " <- " + normalizeExpression(source, rhs, profile)
				} else if p.Name == "" && isSideEffectCall(source, clauses[2]) {
					// A C/C++ post-clause may call an iterator/helper update
					// function (for example SkipLine(&buffer, end)). Preserve it as
					// the loop's ordered advance effect rather than dropping it.
					p.Advance = normalizeExpression(source, clauses[2], profile)
				} else if isSideEffectCall(source, clauses[2]) && !strings.Contains(clauses[2], ",") {
					p.Advance = normalizeExpression(source, clauses[2], profile)
				} else {
					return p, fmt.Errorf("counting-loop step %q requires explicit lowering", clauses[2])
				}
			}
		}
		p.Begin = normalizeExpression(source, p.Begin, profile)
		return p, nil
	}
	expression := ""
	if in := strings.Index(h, " in "); in >= 0 {
		p.Name = strings.TrimSpace(h[:in])
		expression = strings.TrimSpace(h[in+4:])
	} else if source == "cpp" {
		if at := cppRangeSeparator(h); at >= 0 {
			p.Name = strings.TrimSpace(h[:at])
			expression = strings.TrimSpace(h[at+1:])
		}
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
	if source == "cpp" {
		p.Name = normalizeCPPRangeBinding(source, p.Name)
	}
	names := significant(Tokenize(source, p.Name))
	if len(names) != 1 || names[0].Class != TokenIdentifier {
		pattern, restIndex := bindingPattern(source, p.Name)
		if len(pattern) == 0 {
			return p, fmt.Errorf("range binding %q in header %q is not a supported binding pattern", p.Name, h)
		}
		if len(pattern) == 1 {
			p.Name = pattern[0]
		} else {
			p.BindingNames = pattern
			p.BindingRestIndex = restIndex
			parts := append([]string(nil), pattern...)
			if restIndex >= 0 {
				parts[restIndex] = "*" + parts[restIndex]
			}
			p.Name = "(" + strings.Join(parts, ",") + ")"
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
				// Preserve Python's built-in range call as an iterable operation
				// when its step is dynamic or zero. This retains runtime direction
				// and ValueError behavior instead of approximating it as a numeric
				// interval.
				p.Iterable = true
				p.Sequence = normalizeExpression(source, expression, profile)
				return p, nil
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

func compoundLoopUpdate(source, text string) (name, operator, rhs string, ok bool) {
	tokens := significant(Tokenize(source, text))
	for i, token := range tokens {
		if token.Class != TokenOperator || i == 0 || tokens[i-1].Class != TokenIdentifier {
			continue
		}
		switch token.Text {
		case "+=", "-=", "*=", "/=", "%=", "<<=", ">>=", "&=", "|=", "^=":
			at := strings.Index(text, token.Text)
			if at >= 0 && strings.TrimSpace(text[at+len(token.Text):]) != "" {
				return tokens[i-1].Text, token.Text, strings.TrimSpace(text[at+len(token.Text):]), true
			}
		}
	}
	return "", "", "", false
}

func loopUpdateExpression(name, operator, rhs string) string {
	operator = strings.TrimSuffix(operator, "=")
	return name + " <- " + name + " " + operator + " " + rhs
}

// simpleUnaryLoopUpdate recognizes the side-effect update forms accepted in
// C-family for clauses and returns their explicit assignment equivalent.
func simpleUnaryLoopUpdate(source, text string) (name, value string, ok bool) {
	tokens := significant(Tokenize(source, strings.TrimSpace(text)))
	if len(tokens) != 2 || tokens[0].Class != TokenOperator && tokens[1].Class != TokenOperator {
		return "", "", false
	}
	if tokens[0].Class == TokenOperator && (tokens[0].Text == "++" || tokens[0].Text == "--") && tokens[1].Class == TokenIdentifier {
		name = tokens[1].Text
	} else if tokens[1].Class == TokenOperator && (tokens[1].Text == "++" || tokens[1].Text == "--") && tokens[0].Class == TokenIdentifier {
		name = tokens[0].Text
	} else {
		return "", "", false
	}
	delta := "+ 1"
	if strings.Contains(text, "--") {
		delta = "- 1"
	}
	return name, name + " " + delta, true
}

func isSideEffectCall(source, text string) bool {
	tokens := significant(Tokenize(source, strings.TrimSpace(text)))
	if len(tokens) < 3 || tokens[0].Class != TokenIdentifier || tokens[1].Text != "(" || tokens[len(tokens)-1].Text != ")" {
		return false
	}
	depth := 0
	for i, token := range tokens {
		if token.Text == "(" {
			depth++
		} else if token.Text == ")" {
			depth--
			if depth == 0 && i != len(tokens)-1 {
				return false
			}
		}
	}
	return depth == 0
}

// normalizeCPPRangeBinding projects a C++ range-for declaration such as
// `const Foo &item` onto the canonical binding identifier. Type and reference
// tokens are declaration syntax, not part of the loop binding relation.
func normalizeCPPRangeBinding(source, text string) string {
	t := strings.TrimSpace(text)
	if strings.Contains(t, "[") && strings.Contains(t, "]") {
		if at := strings.Index(t, "["); at >= 0 {
			return t[at:]
		}
	}
	tokens := significant(Tokenize(source, t))
	for i := len(tokens) - 1; i >= 0; i-- {
		if tokens[i].Class == TokenIdentifier && tokens[i].Text != "const" && tokens[i].Text != "volatile" && tokens[i].Text != "auto" {
			return tokens[i].Text
		}
	}
	return t
}

// cppRangeSeparator locates a C++ range-for separator while ignoring scope
// resolution, nested declarators, template arguments, and nested dimensions.
func cppRangeSeparator(text string) int {
	paren, bracket, brace, angle := 0, 0, 0, 0
	r := []rune(text)
	for i, c := range r {
		switch c {
		case '(':
			paren++
		case ')':
			paren--
		case '[':
			bracket++
		case ']':
			bracket--
		case '{':
			brace++
		case '}':
			brace--
		case '<':
			if paren == 0 && bracket == 0 && brace == 0 {
				angle++
			}
		case '>':
			if angle > 0 && paren == 0 && bracket == 0 && brace == 0 {
				angle--
			}
		case ':':
			if paren == 0 && bracket == 0 && brace == 0 && angle == 0 &&
				(i == 0 || r[i-1] != ':') && (i+1 == len(r) || r[i+1] != ':') {
				return i
			}
		}
	}
	return -1
}

// rangeBindingAssignment extracts the binding side of a counting-loop
// initializer from lexer tokens. It is deliberately structural (token based),
// never a source-text semantic heuristic, and accepts both := and = forms.
func rangeBindingAssignment(source, text string) (string, string, bool) {
	tokens := significant(Tokenize(source, text))
	for i, token := range tokens {
		if token.Class != TokenOperator || (token.Text != ":=" && token.Text != "=") {
			continue
		}
		if i == 0 || tokens[i-1].Class != TokenIdentifier {
			continue
		}
		name := tokens[i-1].Text
		if name == "_" || isTypeWord(name) {
			continue
		}
		at := strings.Index(text, token.Text)
		if at < 0 {
			continue
		}
		rhs := strings.TrimSpace(text[at+len(token.Text):])
		if rhs == "" {
			continue
		}
		return name, rhs, true
	}
	return "", "", false
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
	names, _ := bindingPattern(source, text)
	return names
}

// bindingPattern preserves one Python starred loop binding as an ordered
// rest-binding. The star is retained in the canonical loop spelling and is
// later recorded as a rest_binding relation; it is never flattened into an
// ordinary product component.
func bindingPattern(source, text string) ([]string, int) {
	t := strings.TrimSpace(text)
	if hasWrappingParens(t) || (strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]")) {
		t = strings.TrimSpace(t[1 : len(t)-1])
	}
	parts := splitTopLevel(t, ',')
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	restIndex := -1
	for index, part := range parts {
		part = strings.TrimSpace(part)
		starred := strings.HasPrefix(part, "*")
		if starred {
			if restIndex >= 0 {
				return nil, -1
			}
			restIndex = index
			part = strings.TrimSpace(strings.TrimPrefix(part, "*"))
		}
		if source == "python" && !starred && ((hasWrappingParens(part)) || (strings.HasPrefix(part, "[") && strings.HasSuffix(part, "]"))) {
			nested, nestedRest := bindingPattern(source, part)
			if len(nested) < 2 || nestedRest >= 0 {
				return nil, -1
			}
			open, close := "(", ")"
			if strings.HasPrefix(part, "[") {
				open, close = "[", "]"
			}
			out = append(out, open+strings.Join(nested, ",")+close)
			continue
		}
		tokens := significant(Tokenize(source, part))
		pythonName := source == "python"
		wildcardName := len(tokens) == 1 && tokens[0].Text == "_" && source != "python" && source != "cpp"
		duplicateName := len(tokens) == 1 && seen[tokens[0].Text] && !pythonName
		bindingModifier := len(tokens) == 1 && isBindingModifier(tokens[0].Text) && source != "python"
		reservedPython := len(tokens) == 1 && pythonBindingKeyword(tokens[0].Text) && source == "python"
		reservedOther := len(tokens) == 1 && source != "python" && (isTypeWord(tokens[0].Text) || isFunctionWord(tokens[0].Text) || bindingModifier)
		if len(tokens) != 1 || tokens[0].Class != TokenIdentifier || wildcardName || reservedPython || reservedOther || duplicateName {
			return nil, -1
		}
		seen[tokens[0].Text] = true
		out = append(out, tokens[0].Text)
	}
	return out, restIndex
}

func pythonBindingKeyword(text string) bool {
	switch text {
	case "False", "None", "True", "and", "as", "assert", "async", "await", "break", "class", "continue", "def", "del", "elif", "else", "except", "finally", "for", "from", "global", "if", "import", "in", "is", "lambda", "nonlocal", "not", "or", "pass", "raise", "return", "try", "while", "with", "yield":
		return true
	default:
		return false
	}
}

func isBindingModifier(text string) bool {
	switch strings.ToLower(text) {
	case "let", "var", "val", "mut", "const", "final", "ref", "inout", "auto", "public", "private", "static":
		return true
	}
	return false
}
