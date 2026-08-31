package backend

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokIdent
	tokNumber
	tokString
	tokOp
	tokLParen
	tokRParen
	tokLBrace
	tokRBrace
	tokLBracket
	tokRBracket
	tokComma
	tokSemicolon
)

type token struct {
	kind tokenKind
	text string
	pos  int
}

func lex(src string) ([]token, error) {
	r := []rune(src)
	out := make([]token, 0, len(r)/2)
	expressionDepth := 0
	for i := 0; i < len(r); {
		c := r[i]
		if unicode.IsSpace(c) {
			if c == '\n' && expressionDepth == 0 {
				out = append(out, token{tokSemicolon, ";", i})
			}
			i++
			continue
		}
		if c == '#' {
			for i < len(r) && r[i] != '\n' {
				i++
			}
			continue
		}
		if unicode.IsLetter(c) || c == '_' || c == '.' {
			s := i
			i++
			for i < len(r) && (unicode.IsLetter(r[i]) || unicode.IsDigit(r[i]) || r[i] == '_' || r[i] == '.') {
				i++
			}
			out = append(out, token{tokIdent, string(r[s:i]), s})
			continue
		}
		if unicode.IsDigit(c) || (c == '.' && i+1 < len(r) && unicode.IsDigit(r[i+1])) {
			s := i
			i++
			for i < len(r) && (unicode.IsDigit(r[i]) || strings.ContainsRune(".eExXaAbBcCdDfF+-", r[i])) {
				if (r[i] == '+' || r[i] == '-') && !(r[i-1] == 'e' || r[i-1] == 'E') {
					break
				}
				i++
			}
			if i < len(r) && (r[i] == 'L' || r[i] == 'i') {
				i++
			}
			out = append(out, token{tokNumber, string(r[s:i]), s})
			continue
		}
		if c == '"' || c == '\'' {
			q := c
			s := i
			i++
			for i < len(r) {
				if r[i] == '\\' && i+1 < len(r) {
					i += 2
					continue
				}
				if r[i] == q {
					i++
					break
				}
				i++
			}
			out = append(out, token{tokString, string(r[s:i]), s})
			continue
		}
		switch c {
		case '(':
			out = append(out, token{tokLParen, "(", i})
			expressionDepth++
			i++
			continue
		case ')':
			out = append(out, token{tokRParen, ")", i})
			if expressionDepth > 0 {
				expressionDepth--
			}
			i++
			continue
		case '{':
			out = append(out, token{tokLBrace, "{", i})
			i++
			continue
		case '}':
			out = append(out, token{tokRBrace, "}", i})
			i++
			continue
		case '[':
			out = append(out, token{tokLBracket, "[", i})
			expressionDepth++
			i++
			continue
		case ']':
			out = append(out, token{tokRBracket, "]", i})
			if expressionDepth > 0 {
				expressionDepth--
			}
			i++
			continue
		case ',':
			out = append(out, token{tokComma, ",", i})
			i++
			continue
		case ';':
			out = append(out, token{tokSemicolon, ";", i})
			i++
			continue
		}
		ops := []string{"<<-", "->>", ":::", "[[", "]]", "<-", "->", "<=", ">=", "==", "!=", "&&", "||", "%%", "%/%", "%in%", "::", "**"}
		matched := ""
		rest := string(r[i:])
		for _, op := range ops {
			if strings.HasPrefix(rest, op) {
				matched = op
				break
			}
		}
		if matched != "" {
			out = append(out, token{tokOp, matched, i})
			i += len([]rune(matched))
			continue
		}
		if strings.ContainsRune("+-*/^:~!?<>=&|$@%", c) {
			out = append(out, token{tokOp, string(c), i})
			i++
			continue
		}
		return nil, fmt.Errorf("unexpected character %q at %d", c, i)
	}
	out = append(out, token{tokEOF, "", len(r)})
	return out, nil
}

type Expr interface{ exprNode() }
type IdentExpr struct{ Name string }

func (*IdentExpr) exprNode() {}

type LiteralExpr struct{ Kind, Text string }

func (*LiteralExpr) exprNode() {}

type UnaryExpr struct {
	Op string
	X  Expr
}

func (*UnaryExpr) exprNode() {}

type BinaryExpr struct {
	Op   string
	L, R Expr
}

func (*BinaryExpr) exprNode() {}

type Arg struct {
	Name    string
	Value   Expr
	Missing bool
}
type CallExpr struct {
	Fun   Expr
	Args  []Arg
	Eager bool // explicit call boundary, including decoded R force wrappers
}

func (*CallExpr) exprNode() {}

type IndexExpr struct {
	X      Expr
	Args   []Arg
	Double bool
}

func (*IndexExpr) exprNode() {}

type FunctionExpr struct {
	Params []Param
	Body   *BlockStmt
}

func (*FunctionExpr) exprNode() {}

type Param struct {
	Name    string
	Default Expr
}

type Stmt interface{ stmtNode() }
type BlockStmt struct{ List []Stmt }

func (*BlockStmt) stmtNode() {}

type ExprStmt struct{ X Expr }

func (*ExprStmt) stmtNode() {}

type AssignStmt struct {
	Name, Op string
	Value    Expr
}

func (*AssignStmt) stmtNode() {}

type IfStmt struct {
	Cond Expr
	Then Stmt
	Else Stmt
}

func (*IfStmt) stmtNode() {}

type WhileStmt struct {
	Cond Expr
	Body Stmt
}

func (*WhileStmt) stmtNode() {}

type ForStmt struct {
	Name string
	Seq  Expr
	Body Stmt
}

func (*ForStmt) stmtNode() {}

type RepeatStmt struct{ Body Stmt }

func (*RepeatStmt) stmtNode() {}

type ReturnStmt struct{ X Expr }

func (*ReturnStmt) stmtNode() {}

type BreakStmt struct{}

func (*BreakStmt) stmtNode() {}

type NextStmt struct{}

func (*NextStmt) stmtNode() {}

type parser struct {
	t []token
	i int
}

func parse(src string) (*BlockStmt, error) {
	ts, e := lex(src)
	if e != nil {
		return nil, e
	}
	p := &parser{t: ts}
	return p.program()
}
func (p *parser) cur() token { return p.t[p.i] }
func (p *parser) next() token {
	t := p.cur()
	if p.i < len(p.t)-1 {
		p.i++
	}
	return t
}
func (p *parser) accept(k tokenKind, text string) bool {
	if p.cur().kind == k && (text == "" || p.cur().text == text) {
		p.i++
		return true
	}
	return false
}
func (p *parser) expect(k tokenKind, text string) (token, error) {
	if p.cur().kind != k || (text != "" && p.cur().text != text) {
		return token{}, fmt.Errorf("expected %q near %q", text, p.cur().text)
	}
	return p.next(), nil
}
func (p *parser) skipSep() {
	for p.accept(tokSemicolon, "") {
	}
}
func (p *parser) program() (*BlockStmt, error) {
	b := &BlockStmt{}
	p.skipSep()
	for p.cur().kind != tokEOF {
		s, e := p.statement()
		if e != nil {
			return nil, e
		}
		b.List = append(b.List, s)
		p.skipSep()
	}
	return b, nil
}
func (p *parser) statement() (Stmt, error) {
	if p.accept(tokLBrace, "") {
		return p.blockAfterOpen()
	}
	if p.cur().kind == tokIdent {
		switch p.cur().text {
		case "if":
			return p.parseIf()
		case "while":
			return p.parseWhile()
		case "for":
			return p.parseFor()
		case "repeat":
			p.next()
			b, e := p.statement()
			return &RepeatStmt{Body: b}, e
		case "return":
			p.next()
			if p.accept(tokLParen, "") {
				x, e := p.expression(0)
				if e != nil {
					return nil, e
				}
				_, e = p.expect(tokRParen, "")
				return &ReturnStmt{X: x}, e
			}
			x, e := p.expression(0)
			return &ReturnStmt{X: x}, e
		case "break":
			p.next()
			return &BreakStmt{}, nil
		case "next":
			p.next()
			return &NextStmt{}, nil
		}
		if p.i+1 < len(p.t) && p.t[p.i+1].kind == tokOp && (p.t[p.i+1].text == "<-" || p.t[p.i+1].text == "<<-" || p.t[p.i+1].text == "=") {
			n := p.next().text
			op := p.next().text
			x, e := p.expression(0)
			if e != nil {
				return nil, e
			}
			return &AssignStmt{Name: n, Op: op, Value: x}, nil
		}
	}
	x, e := p.expression(0)
	if e != nil {
		return nil, e
	}
	// right assignment a -> x
	if p.cur().kind == tokOp && (p.cur().text == "->" || p.cur().text == "->>") {
		op := p.next().text
		n, e2 := p.expect(tokIdent, "")
		if e2 != nil {
			return nil, e2
		}
		return &AssignStmt{Name: n.text, Op: op, Value: x}, nil
	}
	return &ExprStmt{X: x}, nil
}
func (p *parser) blockAfterOpen() (*BlockStmt, error) {
	b := &BlockStmt{}
	p.skipSep()
	for p.cur().kind != tokRBrace && p.cur().kind != tokEOF {
		s, e := p.statement()
		if e != nil {
			return nil, e
		}
		b.List = append(b.List, s)
		p.skipSep()
	}
	_, e := p.expect(tokRBrace, "")
	return b, e
}
func (p *parser) parseIf() (Stmt, error) {
	p.next()
	_, e := p.expect(tokLParen, "")
	if e != nil {
		return nil, e
	}
	c, e := p.expression(0)
	if e != nil {
		return nil, e
	}
	if _, e = p.expect(tokRParen, ""); e != nil {
		return nil, e
	}
	p.skipSep()
	th, e := p.statement()
	if e != nil {
		return nil, e
	}
	p.skipSep()
	var el Stmt
	if p.cur().kind == tokIdent && p.cur().text == "else" {
		p.next()
		p.skipSep()
		el, e = p.statement()
	}
	return &IfStmt{Cond: c, Then: th, Else: el}, e
}
func (p *parser) parseWhile() (Stmt, error) {
	p.next()
	_, e := p.expect(tokLParen, "")
	if e != nil {
		return nil, e
	}
	c, e := p.expression(0)
	if e != nil {
		return nil, e
	}
	if _, e = p.expect(tokRParen, ""); e != nil {
		return nil, e
	}
	p.skipSep()
	b, e := p.statement()
	return &WhileStmt{Cond: c, Body: b}, e
}
func (p *parser) parseFor() (Stmt, error) {
	p.next()
	_, e := p.expect(tokLParen, "")
	if e != nil {
		return nil, e
	}
	n, e := p.expect(tokIdent, "")
	if e != nil {
		return nil, e
	}
	in, e := p.expect(tokIdent, "")
	if e != nil || in.text != "in" {
		return nil, fmt.Errorf("expected in in for")
	}
	seq, e := p.expression(0)
	if e != nil {
		return nil, e
	}
	if _, e = p.expect(tokRParen, ""); e != nil {
		return nil, e
	}
	p.skipSep()
	b, e := p.statement()
	return &ForStmt{Name: n.text, Seq: seq, Body: b}, e
}

var prec = map[string]int{"~": 1, "||": 2, "|": 2, "&&": 3, "&": 3, "==": 4, "!=": 4, "<": 4, "<=": 4, ">": 4, ">=": 4, "%in%": 4, "+": 5, "-": 5, "*": 6, "/": 6, "%%": 6, "%/%": 6, ":": 7, "^": 8, "**": 8, "$": 9, "@": 9, "::": 10, ":::": 10}

func (p *parser) expression(min int) (Expr, error) {
	l, e := p.prefix()
	if e != nil {
		return nil, e
	}
	for {
		// postfix calls/index
		if p.cur().kind == tokLParen {
			args, e := p.args(tokRParen)
			if e != nil {
				return nil, e
			}
			l = &CallExpr{Fun: l, Args: args}
			continue
		}
		if p.cur().kind == tokLBracket {
			p.next()
			dbl := p.accept(tokLBracket, "")
			args, e := p.argsNoOpen(tokRBracket)
			if e != nil {
				return nil, e
			}
			if dbl {
				if _, e = p.expect(tokRBracket, ""); e != nil {
					return nil, e
				}
			}
			l = &IndexExpr{X: l, Args: args, Double: dbl}
			continue
		}
		if p.cur().kind != tokOp {
			break
		}
		op := p.cur().text
		pr, ok := prec[op]
		if !ok || pr < min {
			break
		}
		p.next()
		nextMin := pr + 1
		if op == "^" || op == "**" {
			nextMin = pr
		}
		r, e := p.expression(nextMin)
		if e != nil {
			return nil, e
		}
		l = &BinaryExpr{Op: op, L: l, R: r}
	}
	return l, nil
}
func (p *parser) prefix() (Expr, error) {
	t := p.cur()
	if t.kind == tokOp && (t.text == "+" || t.text == "-" || t.text == "!" || t.text == "~") {
		p.next()
		x, e := p.expression(9)
		return &UnaryExpr{Op: t.text, X: x}, e
	}
	if t.kind == tokNumber {
		p.next()
		return &LiteralExpr{Kind: "number", Text: t.text}, nil
	}
	if t.kind == tokString {
		p.next()
		return &LiteralExpr{Kind: "string", Text: t.text}, nil
	}
	if t.kind == tokIdent {
		p.next()
		if t.text == "function" {
			return p.functionExpr()
		}
		return &IdentExpr{Name: t.text}, nil
	}
	if p.accept(tokLParen, "") {
		x, e := p.expression(0)
		if e != nil {
			return nil, e
		}
		_, e = p.expect(tokRParen, "")
		return x, e
	}
	return nil, fmt.Errorf("expected expression near %q", t.text)
}
func (p *parser) functionExpr() (Expr, error) {
	_, e := p.expect(tokLParen, "")
	if e != nil {
		return nil, e
	}
	var ps []Param
	if !p.accept(tokRParen, "") {
		for {
			n, e := p.expect(tokIdent, "")
			if e != nil {
				return nil, e
			}
			var d Expr
			if p.cur().kind == tokOp && p.cur().text == "=" {
				p.next()
				d, e = p.expression(0)
				if e != nil {
					return nil, e
				}
			}
			ps = append(ps, Param{Name: n.text, Default: d})
			if p.accept(tokRParen, "") {
				break
			}
			if _, e = p.expect(tokComma, ""); e != nil {
				return nil, e
			}
		}
	}
	p.skipSep()
	if !p.accept(tokLBrace, "") {
		return nil, fmt.Errorf("function body must be a block")
	}
	b, e := p.blockAfterOpen()
	return &FunctionExpr{Params: ps, Body: b}, e
}
func (p *parser) args(close tokenKind) ([]Arg, error) { p.next(); return p.argsNoOpen(close) }
func (p *parser) argsNoOpen(close tokenKind) ([]Arg, error) {
	var a []Arg
	if p.accept(close, "") {
		return a, nil
	}
	for {
		if p.cur().kind == tokComma {
			a = append(a, Arg{Missing: true})
			p.next()
			continue
		}
		name := ""
		if p.cur().kind == tokIdent && p.i+1 < len(p.t) && p.t[p.i+1].kind == tokOp && p.t[p.i+1].text == "=" {
			name = p.next().text
			p.next()
		}
		x, e := p.expression(0)
		if e != nil {
			return nil, e
		}
		a = append(a, Arg{Name: name, Value: x})
		if p.accept(close, "") {
			break
		}
		if _, e = p.expect(tokComma, ""); e != nil {
			return nil, e
		}
		if p.accept(close, "") {
			a = append(a, Arg{Missing: true})
			break
		}
	}
	return a, nil
}

func unquote(s string) string {
	if len(s) >= 2 {
		q := s[0]
		if (q == '"' || q == '\'') && s[len(s)-1] == q {
			u, e := strconv.Unquote("\"" + strings.ReplaceAll(s[1:len(s)-1], "\"", "\\\"") + "\"")
			if e == nil {
				return u
			}
		}
	}
	return s
}
