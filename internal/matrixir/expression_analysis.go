package matrixir

import "fmt"

// AnalyzeSemanticExpression is the shared precedence/grouping pass used by matrix
// frontends. It emits typed transient events and never constructs a legacy AST.
func AnalyzeSemanticExpression(language, text string) ([]CanonicalSemanticEvent, error) {
	tokens := Tokenize(language, text)
	p := &semanticExpressionParser{tokens: tokens}
	root, err := p.expression(0)
	if err != nil {
		return nil, err
	}
	if root < 0 {
		return nil, fmt.Errorf("empty expression")
	}
	return p.events, nil
}

type semanticExpressionParser struct {
	tokens []Lexeme
	pos    int
	events []CanonicalSemanticEvent
}

var semanticPrecedence = map[string]int{"||": 2, "|": 2, "&&": 3, "&": 3, "==": 4, "!=": 4, "<": 4, "<=": 4, ">": 4, ">=": 4, "+": 5, "-": 5, "*": 6, "/": 6, "%%": 6, "%/%": 6, "^": 8, "**": 8, "$": 9, "@": 9}

func (p *semanticExpressionParser) current() Lexeme {
	if p.pos >= len(p.tokens) {
		return Lexeme{}
	}
	return p.tokens[p.pos]
}
func (p *semanticExpressionParser) take() Lexeme { t := p.current(); p.pos++; return t }
func (p *semanticExpressionParser) add(kind, text string, start, end int, children []CanonicalRoleFact) int {
	id := len(p.events)
	p.events = append(p.events, CanonicalSemanticEvent{ID: id, StructureKind: kind, Text: text, SourceOffset: start, Roles: children})
	return id
}
func (p *semanticExpressionParser) expression(min int) (int, error) {
	left, err := p.prefix()
	if err != nil {
		return -1, err
	}
	for {
		t := p.current()
		if t.Class == TokenDelimiter && t.Text == "(" {
			left, err = p.call(left)
			if err != nil {
				return -1, err
			}
			continue
		}
		if t.Class == TokenDelimiter && t.Text == "[" {
			left, err = p.index(left)
			if err != nil {
				return -1, err
			}
			continue
		}
		pr, ok := semanticPrecedence[t.Text]
		if t.Class != TokenOperator || !ok || pr < min {
			break
		}
		p.take()
		next := pr + 1
		if t.Text == "^" || t.Text == "**" {
			next = pr
		}
		right, err := p.expression(next)
		if err != nil {
			return -1, err
		}
		left = p.add("binary", t.Text, p.events[left].SourceOffset, t.End, []CanonicalRoleFact{{OwnerNodeID: -1, ChildNodeID: left, Role: "left"}, {OwnerNodeID: -1, ChildNodeID: right, Role: "right"}})
		p.events[left].Roles[0].OwnerNodeID = left
		p.events[left].Roles[1].OwnerNodeID = left
	}
	return left, nil
}

func (p *semanticExpressionParser) call(callee int) (int, error) {
	open := p.take()
	roles := []CanonicalRoleFact{{OwnerNodeID: -1, ChildNodeID: callee, Role: "callee"}}
	ordinal := 0
	for p.current().Text != ")" {
		argument, err := p.expression(0)
		if err != nil {
			return -1, err
		}
		roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: argument, Role: "argument", Ordinal: ordinal})
		ordinal++
		if p.current().Text != "," {
			break
		}
		p.take()
	}
	if p.current().Text != ")" {
		return -1, fmt.Errorf("missing closing call delimiter")
	}
	close := p.take()
	id := p.add("call", "", open.Start, close.End, roles)
	for i := range p.events[id].Roles {
		p.events[id].Roles[i].OwnerNodeID = id
	}
	return id, nil
}

func (p *semanticExpressionParser) index(base int) (int, error) {
	open := p.take()
	roles := []CanonicalRoleFact{{OwnerNodeID: -1, ChildNodeID: base, Role: "base"}}
	ordinal := 0
	for p.current().Text != "]" {
		value, err := p.expression(0)
		if err != nil {
			return -1, err
		}
		roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: value, Role: "index", Ordinal: ordinal})
		ordinal++
		if p.current().Text != "," {
			break
		}
		p.take()
	}
	if p.current().Text != "]" {
		return -1, fmt.Errorf("missing closing index delimiter")
	}
	close := p.take()
	id := p.add("index", "", open.Start, close.End, roles)
	for i := range p.events[id].Roles {
		p.events[id].Roles[i].OwnerNodeID = id
	}
	return id, nil
}
func (p *semanticExpressionParser) prefix() (int, error) {
	t := p.take()
	if t.Class == TokenOperator && (t.Text == "+" || t.Text == "-" || t.Text == "!") {
		x, e := p.expression(9)
		if e != nil {
			return -1, e
		}
		id := p.add("unary", t.Text, t.Start, t.End, []CanonicalRoleFact{{OwnerNodeID: -1, ChildNodeID: x, Role: "operand"}})
		p.events[id].Roles[0].OwnerNodeID = id
		return id, nil
	}
	switch t.Class {
	case TokenIdentifier:
		return p.add("identifier", t.Text, t.Start, t.End, nil), nil
	case TokenNumber, TokenString:
		return p.add("literal", t.Text, t.Start, t.End, nil), nil
	case TokenDelimiter:
		if t.Text == "(" {
			x, e := p.expression(0)
			if e != nil {
				return -1, e
			}
			if p.current().Text != ")" {
				return -1, fmt.Errorf("missing closing parenthesis")
			}
			p.take()
			return x, nil
		}
	}
	return -1, fmt.Errorf("expected expression near %q", t.Text)
}
