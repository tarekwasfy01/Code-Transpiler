package backend

import (
	"encoding/hex"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"strconv"
	"strings"
)

// DecodeGenerated reads the actual emitted program. Only an exact known
// runtime prefix may be removed; every remaining token must be decoded and
// reproduced. There is no original-source payload, comment recovery or cache.
// The shared emitter functions define the inverse operation templates.
func DecodeGenerated(source, code string) (*SemanticProgram, bool, error) {
	prelude := targetPrelude(source)
	if prelude == "" || !strings.HasPrefix(code, prelude+"\n") {
		return nil, false, nil
	}
	body := strings.Trim(strings.TrimPrefix(code, prelude+"\n"), "\r\n")
	d := &generatedDecoder{source: source}
	if open := mainOpen(source); open != "" {
		if !strings.HasPrefix(body, open+"\n") || !strings.HasSuffix(body, mainClose(source)) {
			return nil, true, fmt.Errorf("DECODE_HELPER: generated helpers or unrecognized main envelope in %s", source)
		}
		body = strings.TrimSuffix(strings.TrimPrefix(body, open+"\n"), mainClose(source))
		if source == "c" || source == "cpp" {
			body = strings.TrimRight(body, " \r\n")
			if !strings.HasSuffix(body, "return 0;") {
				return nil, true, fmt.Errorf("DECODE_ENVELOPE: missing native main return")
			}
			body = strings.TrimSuffix(body, "return 0;")
		}
	}
	for _, line := range strings.Split(body, "\n") {
		text := strings.TrimSpace(line)
		if text == "" {
			continue
		}
		spaces := len(line) - len(strings.TrimLeft(line, " "))
		if strings.ContainsRune(line, '\t') || spaces%4 != 0 {
			return nil, true, fmt.Errorf("DECODE_LAYOUT: noncanonical generated indentation")
		}
		indent := spaces / 4
		if mainOpen(source) != "" && indent > 0 {
			indent--
		}
		if strings.HasPrefix(text, "} else") {
			d.lines = append(d.lines, decodedLine{indent, "}"}, decodedLine{indent, strings.TrimSpace(strings.TrimPrefix(text, "}"))})
		} else {
			d.lines = append(d.lines, decodedLine{indent, text})
		}
	}
	// Whitespace is structurally checked before token equivalence: Python/Nim
	// indentation must not disappear from the inverse-semantic proof.
	ast, err := d.block(0)
	if err != nil {
		return nil, true, err
	}
	if d.at != len(d.lines) {
		return nil, true, fmt.Errorf("DECODE_STATEMENT: unconsumed generated syntax at line %d", d.at+1)
	}
	p := NewSemanticProgram(ast, "eager_left_to_right")
	reproduced, err := EmitSemantic(source, p)
	if err != nil {
		return nil, true, err
	}
	if !sameTokens(source, code, reproduced) {
		return nil, true, fmt.Errorf("DECODE_ROUNDTRIP: decoded tree does not reproduce every emitted token")
	}
	return p, true, nil
}

type decodedLine struct {
	indent int
	text   string
}
type generatedDecoder struct {
	source string
	lines  []decodedLine
	at     int
	depth  int
}

const holePrefix = "R2M_DECODE_"

func codeTokens(source, s string) []matrixir.Lexeme {
	var out []matrixir.Lexeme
	for _, t := range matrixir.Tokenize(source, s) {
		if t.Class != matrixir.TokenNewline && t.Class != matrixir.TokenComment {
			if t.Class == matrixir.TokenIdentifier && strings.Contains(t.Text, ".") {
				for i, part := range strings.Split(t.Text, ".") {
					if i > 0 {
						out = append(out, matrixir.Lexeme{Class: matrixir.TokenDelimiter, Text: "."})
					}
					if part != "" {
						out = append(out, matrixir.Lexeme{Class: matrixir.TokenIdentifier, Text: part})
					}
				}
				continue
			}
			out = append(out, t)
		}
	}
	return out
}
func sameTokenList(a, b []matrixir.Lexeme) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Text != b[i].Text || a[i].Class != b[i].Class {
			return false
		}
	}
	return true
}
func sameTokens(source, a, b string) bool {
	return sameTokenList(codeTokens(source, a), codeTokens(source, b))
}
func tokenText(ts []matrixir.Lexeme) string {
	v := make([]string, len(ts))
	for i, t := range ts {
		v[i] = t.Text
	}
	return strings.Join(v, " ")
}

// Template matching binds only balanced token spans. Literal strings are atomic
// and can never inject a delimiter/operator into the structure matrix.
func matchTemplate(source, pattern string, input []matrixir.Lexeme) (map[string][]matrixir.Lexeme, bool) {
	pat := codeTokens(source, pattern)
	bindings := map[string][]matrixir.Lexeme{}
	var visit func(int, int) bool
	visit = func(i, j int) bool {
		if i == len(pat) {
			return j == len(input)
		}
		name := pat[i].Text
		if !strings.HasPrefix(name, holePrefix) {
			return j < len(input) && pat[i].Text == input[j].Text && pat[i].Class == input[j].Class && visit(i+1, j+1)
		}
		if old, ok := bindings[name]; ok {
			return j+len(old) <= len(input) && sameTokenList(old, input[j:j+len(old)]) && visit(i+1, j+len(old))
		}
		stack := []string{}
		closes := map[string]string{")": "(", "]": "[", "}": "{"}
		// An empty span is allowed only for a dispatch argument list.
		if strings.HasSuffix(name, "args") {
			bindings[name] = input[j:j]
			if visit(i+1, j) {
				return true
			}
			delete(bindings, name)
		}
		for end := j; end < len(input); end++ {
			t := input[end]
			if t.Class == matrixir.TokenDelimiter {
				switch t.Text {
				case "(", "[", "{":
					stack = append(stack, t.Text)
				case ")", "]", "}":
					if len(stack) == 0 || stack[len(stack)-1] != closes[t.Text] {
						return false
					}
					stack = stack[:len(stack)-1]
				}
			}
			if len(stack) == 0 {
				bindings[name] = input[j : end+1]
				if visit(i+1, end+1) {
					return true
				}
				delete(bindings, name)
			}
		}
		return false
	}
	ok := visit(0, 0)
	return bindings, ok
}
func splitArguments(ts []matrixir.Lexeme) [][]matrixir.Lexeme {
	if len(ts) == 0 {
		return nil
	}
	var out [][]matrixir.Lexeme
	depth, start := 0, 0
	for i, t := range ts {
		if t.Class == matrixir.TokenDelimiter {
			switch t.Text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			}
		}
		if depth == 0 && t.Text == "," {
			out = append(out, ts[start:i])
			start = i + 1
		}
	}
	return append(out, ts[start:])
}
func (d *generatedDecoder) expression(ts []matrixir.Lexeme) (Expr, error) {
	d.depth++
	defer func() { d.depth-- }()
	if d.depth > 128 {
		return nil, fmt.Errorf("RESOURCE_LIMIT: generated expression nesting exceeds 128")
	}
	fail := func() (Expr, error) {
		text := tokenText(ts)
		if len(text) > 160 {
			text = text[:160]
		}
		return nil, fmt.Errorf("DECODE_OPERATION: %s expression %s", d.source, text)
	}
	for _, v := range []struct{ text, name string }{{targetBool(d.source, true), "TRUE"}, {targetBool(d.source, false), "FALSE"}, {targetNull(d.source), "NULL"}, {targetNA(d.source), "NaN"}, {targetInf(d.source), "Inf"}} {
		if sameTokenList(ts, codeTokens(d.source, v.text)) {
			return &IdentExpr{Name: v.name}, nil
		}
	}
	// Runtime dispatch: kernel, name, list construction and C arity are checked
	// against the very same emitter, before operands become semantic nodes.
	pattern := emitDispatch(d.source, "R2M_DECODE_name_marker", []string{holePrefix + "args"})
	pattern = strings.ReplaceAll(pattern, strconv.Quote("runtime"), holePrefix+"kernel")
	pattern = strings.ReplaceAll(pattern, strconv.Quote("R2M_DECODE_name_marker"), holePrefix+"name")
	if d.source == "c" {
		pattern = strings.TrimSuffix(pattern, ", 1)") + ", " + holePrefix + "count)"
	}
	variants := []string{pattern}
	if d.source == "c" {
		z := emitDispatch("c", "R2M_DECODE_name_marker", nil)
		z = strings.ReplaceAll(z, strconv.Quote("runtime"), holePrefix+"kernel")
		z = strings.ReplaceAll(z, strconv.Quote("R2M_DECODE_name_marker"), holePrefix+"name")
		variants = append(variants, z)
	}
	for _, pattern := range variants {
		if m, ok := matchTemplate(d.source, pattern, ts); ok {
			nameTokens := m[holePrefix+"name"]
			if len(nameTokens) != 1 || nameTokens[0].Class != matrixir.TokenString {
				return fail()
			}
			name, err := strconv.Unquote(nameTokens[0].Text)
			if err != nil {
				return fail()
			}
			raw := splitArguments(m[holePrefix+"args"])
			values := make([]string, len(raw))
			for i, t := range raw {
				values[i] = tokenText(t)
			}
			if !sameTokens(d.source, tokenText(ts), emitDispatch(d.source, name, values)) {
				return fail()
			}
			args := make([]Arg, len(raw))
			for i, t := range raw {
				value, e := d.expression(t)
				if e != nil {
					return nil, e
				}
				args[i] = Arg{Value: value}
			}
			if strings.HasPrefix(name, "__binary_") {
				if len(args) != 2 {
					return fail()
				}
				return &BinaryExpr{Op: strings.TrimPrefix(name, "__binary_"), L: args[0].Value, R: args[1].Value}, nil
			}
			if strings.HasPrefix(name, "__unary_") {
				if len(args) != 1 {
					return fail()
				}
				return &UnaryExpr{Op: strings.TrimPrefix(name, "__unary_"), X: args[0].Value}, nil
			}
			if name == "[" || name == "[[" {
				if len(args) < 1 {
					return fail()
				}
				return &IndexExpr{X: args[0].Value, Args: args[1:], Double: name == "[["}, nil
			}
			if strings.HasPrefix(name, "__") {
				return fail()
			}
			return &CallExpr{Fun: &IdentExpr{Name: name}, Args: args}, nil
		}
	}
	g := &targetGen{target: d.source}
	for _, op := range []string{"&&", "||"} {
		if m, ok := matchTemplate(d.source, g.lowerLogical(op, holePrefix+"left", holePrefix+"right"), ts); ok {
			a, e := d.expression(m[holePrefix+"left"])
			if e != nil {
				return nil, e
			}
			b, e := d.expression(m[holePrefix+"right"])
			return &BinaryExpr{Op: op, L: a, R: b}, e
		}
	}
	not, _ := g.lowerUnary("!", holePrefix+"value")
	if m, ok := matchTemplate(d.source, not, ts); ok {
		a, e := d.expression(m[holePrefix+"value"])
		return &UnaryExpr{Op: "!", X: a}, e
	}
	numberPatterns := []string{targetNumber(d.source, holePrefix+"value")}
	if d.source == "go" {
		numberPatterns = []string{"float64(" + holePrefix + "value" + ")"}
	}
	if d.source == "kotlin" {
		numberPatterns = []string{"RValue.Num(" + holePrefix + "value" + ")"}
	}
	for _, pattern := range numberPatterns {
		if m, ok := matchTemplate(d.source, pattern, ts); ok {
			v := strings.ReplaceAll(tokenText(m[holePrefix+"value"]), " ", "")
			if _, e := strconv.ParseFloat(v, 64); e == nil {
				return &LiteralExpr{Kind: "number", Text: v}, nil
			}
		}
	}
	stringPattern := strings.ReplaceAll(targetString(d.source, "R2M_STRING_MARKER"), strconv.Quote("R2M_STRING_MARKER"), holePrefix+"value")
	if m, ok := matchTemplate(d.source, stringPattern, ts); ok {
		v := m[holePrefix+"value"]
		if len(v) == 1 && v[0].Class == matrixir.TokenString {
			return &LiteralExpr{Kind: "string", Text: v[0].Text}, nil
		}
	}
	if d.source == "rust" {
		if m, ok := matchTemplate(d.source, holePrefix+"name"+".clone()", ts); ok {
			return d.identifier(m[holePrefix+"name"])
		}
	}
	if len(ts) == 1 {
		if ts[0].Class == matrixir.TokenNumber {
			if _, e := strconv.ParseFloat(ts[0].Text, 64); e == nil {
				return &LiteralExpr{Kind: "number", Text: ts[0].Text}, nil
			}
		}
		if ts[0].Class == matrixir.TokenIdentifier {
			return d.identifier(ts)
		}
	}
	return fail()
}
func (d *generatedDecoder) identifier(ts []matrixir.Lexeme) (Expr, error) {
	if len(ts) != 1 || ts[0].Class != matrixir.TokenIdentifier {
		return nil, fmt.Errorf("DECODE_BINDING: expected one identifier")
	}
	name := ts[0].Text
	if d.source == "nim" {
		if !strings.HasPrefix(name, "r2ms") {
			return nil, fmt.Errorf("DECODE_BINDING: unmodeled generated Nim local")
		}
		raw, err := hex.DecodeString(strings.TrimPrefix(name, "r2ms"))
		if err != nil || len(raw) == 0 {
			return nil, fmt.Errorf("DECODE_BINDING: invalid Nim symbol encoding")
		}
		name = string(raw)
	}
	return &IdentExpr{Name: name}, nil
}
func (d *generatedDecoder) indented() bool { return d.source == "python" || d.source == "nim" }

// Obtain iterable syntax from the real statement emitter. C has a multi-line
// indexed lowering and is not claimed by this single-header inverse rule.
func generatedForPattern(source string) string {
	if source == "c" {
		return ""
	}
	name, sequence := holePrefix+"iterator", holePrefix+"sequence"
	g := &targetGen{target: source, cValues: map[string]bool{}}
	g.bindings = []map[string]string{{g.name(sequence): sequence}}
	if err := g.stmt(&ForStmt{Name: name, Seq: &IdentExpr{Name: sequence}, Body: &BlockStmt{}}); err != nil {
		return ""
	}
	line := strings.SplitN(g.b.String(), "\n", 2)[0]
	return strings.ReplaceAll(line, g.name(name), name)
}
func (d *generatedDecoder) closeBlock() error {
	if d.indented() {
		return nil
	}
	if d.at >= len(d.lines) {
		return fmt.Errorf("DECODE_CONTROL: missing block end")
	}
	want := "}"
	if d.source == "julia" {
		want = "end"
	}
	if d.lines[d.at].text != want {
		return fmt.Errorf("DECODE_CONTROL: expected %s", want)
	}
	d.at++
	return nil
}
func (d *generatedDecoder) block(level int) (*BlockStmt, error) {
	b := &BlockStmt{}
	for d.at < len(d.lines) {
		line := d.lines[d.at]
		text := line.text
		if line.indent < level || text == "}" || text == "end" || strings.HasPrefix(text, "else") {
			break
		}
		if line.indent != level {
			return nil, fmt.Errorf("DECODE_CONTROL: unexpected indentation at generated line %d", d.at+1)
		}
		d.at++
		cond := holePrefix + "condition"
		truth := truthCall(d.source, cond)
		ifPattern := "if (" + truth + ") {"
		whilePattern := "while (" + truth + ") {"
		if d.indented() {
			ifPattern = "if " + truth + ":"
			whilePattern = "while " + truth + ":"
		} else if d.source == "julia" {
			ifPattern = "if " + truth
			whilePattern = "while " + truth
		} else if d.source == "go" {
			whilePattern = "for " + truth + " {"
		}
		handled := false
		for _, h := range []struct{ kind, pattern string }{{"if", ifPattern}, {"while", whilePattern}} {
			if m, ok := matchTemplate(d.source, h.pattern, codeTokens(d.source, text)); ok {
				c, e := d.expression(m[cond])
				if e != nil {
					return nil, e
				}
				body, e := d.block(level + 1)
				if e != nil {
					return nil, e
				}
				if h.kind == "while" {
					if e = d.closeBlock(); e != nil {
						return nil, e
					}
					b.List = append(b.List, &WhileStmt{Cond: c, Body: body})
				} else {
					branch := &IfStmt{Cond: c, Then: body}
					if d.source != "julia" {
						if e = d.closeBlock(); e != nil {
							return nil, e
						}
					}
					if d.at < len(d.lines) && d.lines[d.at].indent == level && strings.HasPrefix(d.lines[d.at].text, "else") {
						expected := "else {"
						if d.indented() {
							expected = "else:"
						} else if d.source == "julia" {
							expected = "else"
						}
						if d.lines[d.at].text != expected {
							return nil, fmt.Errorf("DECODE_CONTROL: unmodeled else")
						}
						d.at++
						branch.Else, e = d.block(level + 1)
						if e != nil {
							return nil, e
						}
						if d.source != "julia" {
							if e = d.closeBlock(); e != nil {
								return nil, e
							}
						}
					}
					if d.source == "julia" {
						if e = d.closeBlock(); e != nil {
							return nil, e
						}
					}
					b.List = append(b.List, branch)
				}
				handled = true
				break
			}
		}
		if handled {
			continue
		}
		if text == "break"+stmtEnd(d.source) {
			b.List = append(b.List, &BreakStmt{})
			continue
		}
		if pattern := generatedForPattern(d.source); pattern != "" {
			if m, ok := matchTemplate(d.source, pattern, codeTokens(d.source, text)); ok {
				id, e := d.identifier(m[holePrefix+"iterator"])
				if e != nil {
					return nil, e
				}
				sequence, e := d.expression(m[holePrefix+"sequence"])
				if e != nil {
					return nil, e
				}
				body, e := d.block(level + 1)
				if e != nil {
					return nil, e
				}
				if e = d.closeBlock(); e != nil {
					return nil, e
				}
				b.List = append(b.List, &ForStmt{Name: id.(*IdentExpr).Name, Seq: sequence, Body: body})
				continue
			}
		}
		if text == "continue" || text == "continue;" {
			b.List = append(b.List, &NextStmt{})
			continue
		}
		for _, pattern := range []string{assignSyntax(d.source, holePrefix+"name", holePrefix+"value"), reassignSyntax(d.source, holePrefix+"name", holePrefix+"value")} {
			if m, ok := matchTemplate(d.source, pattern, codeTokens(d.source, text)); ok {
				// A Go blank assignment is the emitter's discard-expression
				// template, not a variable binding. Decode it below as such.
				if (d.source == "go" || d.source == "zig") && tokenText(m[holePrefix+"name"]) == "_" {
					continue
				}
				id, e := d.identifier(m[holePrefix+"name"])
				if e != nil {
					continue
				}
				v, e := d.expression(m[holePrefix+"value"])
				if e != nil {
					return nil, e
				}
				b.List = append(b.List, &AssignStmt{Name: id.(*IdentExpr).Name, Op: "<-", Value: v})
				handled = true
				break
			}
		}
		if handled {
			continue
		}
		if m, ok := matchTemplate(d.source, exprStmt(d.source, holePrefix+"value"), codeTokens(d.source, text)); ok {
			v, e := d.expression(m[holePrefix+"value"])
			if e != nil {
				return nil, e
			}
			b.List = append(b.List, &ExprStmt{X: v})
			continue
		}
		return nil, fmt.Errorf("DECODE_STATEMENT: unmodeled generated statement in %s at line %d", d.source, d.at)
	}
	return b, nil
}
