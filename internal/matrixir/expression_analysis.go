package matrixir

import "fmt"

// AnalyzeSemanticExpression is the shared precedence/grouping pass used by matrix
// frontends. It emits typed transient events and never constructs a legacy AST.
func AnalyzeSemanticExpression(language, text string) ([]CanonicalSemanticEvent, error) {
	return AnalyzeSemanticTokens(language, Tokenize(language, text))
}

// AnalyzeSemanticTokens is the structured counterpart of
// AnalyzeSemanticExpression.  Its caller already owns parser/grammar tokens;
// consequently it never reparses an Event.Text payload.
func AnalyzeSemanticTokens(language string, tokens []Lexeme) ([]CanonicalSemanticEvent, error) {
	clean := significant(append([]Lexeme(nil), tokens...))
	for i, token := range clean {
		if token.Class == TokenOperator && token.Text == "=>" {
			return analyzeArrowClosure(language, clean, i)
		}
	}
	p := &semanticExpressionParser{tokens: clean}
	root, err := p.expression(0)
	if err != nil {
		return nil, err
	}
	if root < 0 {
		return nil, fmt.Errorf("empty expression")
	}
	return p.events, nil
}

// analyzeArrowClosure consumes the language-neutral token shape
// parameters => expression. Parser-specific spelling has already become
// lexemes; only proven identifier parameters and an expression body are
// emitted as ClosureExpr facts.
func analyzeArrowClosure(language string, tokens []Lexeme, arrow int) ([]CanonicalSemanticEvent, error) {
	if arrow == 0 || arrow+1 >= len(tokens) {
		return nil, fmt.Errorf("MISSING_STRUCTURED_PARSER_DATA: arrow closure")
	}
	params := tokens[:arrow]
	if len(params) >= 2 && params[0].Text == "(" && params[len(params)-1].Text == ")" {
		params = params[1 : len(params)-1]
	}
	events := []CanonicalSemanticEvent{}
	roles := []CanonicalRoleFact{}
	for _, token := range params {
		if token.Class != TokenIdentifier || isTypeWord(token.Text) || isFunctionWord(token.Text) {
			continue
		}
		id := len(events)
		events = append(events, CanonicalSemanticEvent{ID: id, StructureKind: "identifier", Text: token.Text, SourceOffset: token.Start, Fields: map[string]string{"name": token.Text}})
		roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: id, Role: "parameter", Ordinal: len(roles)})
	}
	bodyTokens := tokens[arrow+1:]
	if len(bodyTokens) == 0 || bodyTokens[0].Text == "{" {
		return nil, fmt.Errorf("MISSING_STRUCTURED_PARSER_DATA: arrow closure body")
	}
	body, err := AnalyzeSemanticTokens(language, bodyTokens)
	if err != nil {
		return nil, err
	}
	offset := len(events)
	for i := range body {
		body[i].ID += offset
		for j := range body[i].Roles {
			body[i].Roles[j].OwnerNodeID += offset
			body[i].Roles[j].ChildNodeID += offset
		}
	}
	events = append(events, body...)
	roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: events[len(events)-1].ID, Role: "return"})
	id := len(events)
	for i := range roles {
		roles[i].OwnerNodeID = id
	}
	events = append(events, CanonicalSemanticEvent{ID: id, Action: "function", StructureKind: "closure", SourceOffset: tokens[0].Start, Roles: roles, FactFamily: ParsedClosure})
	return events, nil
}

type semanticExpressionParser struct {
	tokens []Lexeme
	pos    int
	events []CanonicalSemanticEvent
}

var semanticPrecedence = map[string]int{"?": 1, "||": 2, "|": 2, "&&": 3, "&": 3, "==": 4, "!=": 4, "<": 4, "<=": 4, ">": 4, ">=": 4, "+": 5, "-": 5, "*": 6, "/": 6, "%%": 6, "%/%": 6, "^": 8, "**": 8, "$": 9, "::": 9, ":::": 9, "@": 9}

func (p *semanticExpressionParser) current() Lexeme {
	if p.pos >= len(p.tokens) {
		return Lexeme{}
	}
	return p.tokens[p.pos]
}
func (p *semanticExpressionParser) take() Lexeme { t := p.current(); p.pos++; return t }
func (p *semanticExpressionParser) add(kind, text string, start, end int, children []CanonicalRoleFact) int {
	id := len(p.events)
	fields := map[string]string{}
	switch kind {
	case "identifier":
		fields["name"] = text
	case "literal":
		fields["value"] = text
		if len(text) > 0 && (text[0] == '\'' || text[0] == '"' || text[0] == '`') {
			fields["literal_kind"] = "string"
		} else {
			fields["literal_kind"] = "number"
		}
	case "binary", "unary":
		fields["operator"] = text
	}
	p.events = append(p.events, CanonicalSemanticEvent{ID: id, StructureKind: kind, Text: text, SourceOffset: start, Fields: fields, Roles: children, FactFamily: ParsedFamilyForStructure(kind)})
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
		if t.Text == "?" {
			whenTrue, err := p.expression(0)
			if err != nil {
				return -1, err
			}
			if p.current().Text != ":" {
				return -1, fmt.Errorf("conditional expression missing ':'")
			}
			p.take()
			whenFalse, err := p.expression(pr)
			if err != nil {
				return -1, err
			}
			left = p.add("binary", "?:", p.events[left].SourceOffset, p.events[whenFalse].SourceOffset, []CanonicalRoleFact{
				{OwnerNodeID: -1, ChildNodeID: left, Role: "condition"},
				{OwnerNodeID: -1, ChildNodeID: whenTrue, Role: "consequent"},
				{OwnerNodeID: -1, ChildNodeID: whenFalse, Role: "alternate"},
			})
			for i := range p.events[left].Roles {
				p.events[left].Roles[i].OwnerNodeID = left
			}
			continue
		}
		next := pr + 1
		if t.Text == "^" || t.Text == "**" {
			next = pr
		}
		right, err := p.expression(next)
		if err != nil {
			return -1, err
		}
		kind, leftRole, rightRole := "binary", "left", "right"
		if t.Text == "$" || t.Text == "::" || t.Text == ":::" {
			kind, leftRole, rightRole = "member", "base", "member"
		}
		left = p.add(kind, t.Text, p.events[left].SourceOffset, t.End, []CanonicalRoleFact{{OwnerNodeID: -1, ChildNodeID: left, Role: leftRole}, {OwnerNodeID: -1, ChildNodeID: right, Role: rightRole}})
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
	slice := false
	part := 0
	ordinal := 0
	for p.current().Text != "]" {
		if p.current().Text == ":" {
			slice = true
			for i := range roles {
				if roles[i].Role == "index" {
					roles[i].Role = "start"
				}
			}
			part++
			p.take()
			continue
		}
		value, err := p.expression(0)
		if err != nil {
			return -1, err
		}
		role := "index"
		if slice {
			switch part {
			case 0:
				role = "start"
			case 1:
				role = "end"
			default:
				role = "step"
			}
		}
		roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: value, Role: role, Ordinal: ordinal})
		ordinal++
		if p.current().Text == ":" {
			continue
		}
		if p.current().Text != "," {
			break
		}
		p.take()
	}
	if p.current().Text != "]" {
		return -1, fmt.Errorf("missing closing index delimiter")
	}
	close := p.take()
	kind := "index"
	if slice {
		kind = "slice"
	}
	id := p.add(kind, "", open.Start, close.End, roles)
	for i := range p.events[id].Roles {
		p.events[id].Roles[i].OwnerNodeID = id
	}
	return id, nil
}
func (p *semanticExpressionParser) prefix() (int, error) {
	t := p.take()
	if t.Class == TokenOperator && (t.Text == "+" || t.Text == "-" || t.Text == "!" || t.Text == "*" || t.Text == "&") {
		x, e := p.expression(9)
		if e != nil {
			return -1, e
		}
		kind := "unary"
		if t.Text == "*" {
			kind = "deref"
		}
		if t.Text == "&" {
			kind = "address"
		}
		id := p.add(kind, t.Text, t.Start, t.End, []CanonicalRoleFact{{OwnerNodeID: -1, ChildNodeID: x, Role: "operand"}})
		p.events[id].Roles[0].OwnerNodeID = id
		return id, nil
	}
	switch t.Class {
	case TokenIdentifier:
		if t.Text == "lambda" {
			return p.lambda(t)
		}
		return p.add("identifier", t.Text, t.Start, t.End, nil), nil
	case TokenNumber, TokenString:
		return p.add("literal", t.Text, t.Start, t.End, nil), nil
	case TokenDelimiter:
		// A Go/Rust/C-family typed container prefix (for example []float64)
		// is grammar data.  When followed by a composite literal we emit the
		// same aggregate family as an untyped literal; when used as make's
		// first argument it remains a proven container type operand.
		if t.Text == "[" && p.current().Text == "]" {
			p.take()
			if p.current().Class == TokenIdentifier {
				p.take()
			}
			if p.current().Text == "{" {
				return p.aggregate(p.take())
			}
			return p.add("aggregate", "", t.Start, t.End, nil), nil
		}
		if t.Text == "[" || t.Text == "{" {
			return p.aggregate(t)
		}
		if t.Text == "(" {
			x, e := p.expression(0)
			if e != nil {
				return -1, e
			}
			if p.current().Text == "," {
				roles := []CanonicalRoleFact{{OwnerNodeID: -1, ChildNodeID: x, Role: "element", Ordinal: 0}}
				for p.current().Text == "," {
					p.take()
					if p.current().Text == ")" {
						break
					}
					element, err := p.expression(0)
					if err != nil {
						return -1, err
					}
					roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: element, Role: "element", Ordinal: len(roles)})
				}
				if p.current().Text != ")" {
					return -1, fmt.Errorf("missing closing tuple delimiter")
				}
				close := p.take()
				id := p.add("tuple", "", t.Start, close.End, roles)
				for i := range p.events[id].Roles {
					p.events[id].Roles[i].OwnerNodeID = id
				}
				return id, nil
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

func (p *semanticExpressionParser) aggregate(open Lexeme) (int, error) {
	closeText := "]"
	if open.Text == "{" {
		closeText = "}"
	}
	roles := []CanonicalRoleFact{}
	ordinal := 0
	for p.current().Text != closeText {
		value, err := p.expression(0)
		if err != nil {
			return -1, err
		}
		if p.current().Text == ":" {
			p.take()
			mapped, err := p.expression(0)
			if err != nil {
				return -1, err
			}
			roles = append(roles,
				CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: value, Role: "key", Ordinal: ordinal},
				CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: mapped, Role: "value", Ordinal: ordinal},
			)
		} else {
			roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: value, Role: "element", Ordinal: ordinal})
		}
		ordinal++
		if p.current().Text == "for" {
			return p.comprehension(open, value)
		}
		if p.current().Text != "," {
			break
		}
		p.take()
	}
	if p.current().Text != closeText {
		return -1, fmt.Errorf("missing closing aggregate delimiter")
	}
	close := p.take()
	id := p.add("aggregate", "", open.Start, close.End, roles)
	for i := range p.events[id].Roles {
		p.events[id].Roles[i].OwnerNodeID = id
	}
	return id, nil
}

// comprehension consumes the already-tokenised `for binding in iterable`
// clauses of a container expression.  It never receives the source spelling
// as a string and represents nested clauses as child iteration facts.
func (p *semanticExpressionParser) comprehension(open Lexeme, produced int) (int, error) {
	last := produced
	for p.current().Text == "for" {
		p.take()
		binding := p.take()
		if binding.Class != TokenIdentifier || p.current().Text != "in" {
			return -1, fmt.Errorf("MISSING_STRUCTURED_PARSER_DATA: comprehension binding")
		}
		bindingID := p.add("identifier", binding.Text, binding.Start, binding.End, nil)
		p.take()
		iterable, err := p.expression(0)
		if err != nil {
			return -1, err
		}
		roles := []CanonicalRoleFact{{OwnerNodeID: -1, ChildNodeID: bindingID, Role: "binding"}, {OwnerNodeID: -1, ChildNodeID: iterable, Role: "iterable"}, {OwnerNodeID: -1, ChildNodeID: last, Role: "produced"}}
		id := p.add("iteration", "", open.Start, p.events[iterable].SourceOffset, roles)
		for i := range p.events[id].Roles {
			p.events[id].Roles[i].OwnerNodeID = id
		}
		last = id
	}
	if p.current().Text != "]" {
		return -1, fmt.Errorf("missing closing comprehension delimiter")
	}
	close := p.take()
	id := p.add("comprehension", "", open.Start, close.End, []CanonicalRoleFact{{OwnerNodeID: -1, ChildNodeID: last, Role: "iteration"}})
	p.events[id].Roles[0].OwnerNodeID = id
	return id, nil
}

func (p *semanticExpressionParser) lambda(start Lexeme) (int, error) {
	roles := []CanonicalRoleFact{}
	ordinal := 0
	for p.current().Text != ":" {
		t := p.take()
		if t.Class == TokenIdentifier {
			id := p.add("identifier", t.Text, t.Start, t.End, nil)
			roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: id, Role: "parameter", Ordinal: ordinal})
			ordinal++
		}
		if p.current().Text == "," {
			p.take()
		}
		if p.current().Text == "" {
			return -1, fmt.Errorf("lambda missing body delimiter")
		}
	}
	p.take()
	value, err := p.expression(0)
	if err != nil {
		return -1, err
	}
	roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: value, Role: "return"})
	id := p.add("closure", "", start.Start, p.events[value].SourceOffset, roles)
	for i := range p.events[id].Roles {
		p.events[id].Roles[i].OwnerNodeID = id
	}
	return id, nil
}
