package backend

// This file is the productive matrix frontend parser.  It intentionally uses
// the lexer and grammar decisions from parser.go, but emits transient frontend
// facts immediately.  ParsedNode is a parser handle, not a second AST: its ID
// refers to a node already written to FrontendFactSink.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"sync/atomic"
)

// textSemanticParseCalls is test instrumentation for the migration boundary.
// The canonical MatrixIR frontend must leave this counter unchanged; only the
// explicitly named compatibility entry points may invoke the historical text
// fact parser.
var textSemanticParseCalls atomic.Uint64

type ParsedNode struct {
	ID     int
	Kind   string
	Source *SemanticSourceSpan
}

// canonicalizeFrontendFactOrder gives direct facts the same deterministic
// depth-first identity contract as the historical document projection.  It
// operates solely on syntax facts produced above; it does not construct a
// legacy tree.
func canonicalizeFrontendFactOrder(f *FrontendSemanticFacts, root int) error {
	byParent := map[int][]int{}
	for _, r := range f.Relations {
		if r.Kind != "syntax.child" || r.To.Domain != "node" {
			continue
		}
		id, err := strconv.Atoi(r.To.ID)
		if err != nil {
			return err
		}
		byParent[r.From] = append(byParent[r.From], id)
	}
	seen, order := map[int]bool{}, []int{}
	var visit func(int)
	visit = func(id int) {
		if seen[id] {
			return
		}
		seen[id] = true
		order = append(order, id)
		for _, child := range byParent[id] {
			visit(child)
		}
	}
	visit(root)
	for _, n := range f.Nodes {
		visit(n.ID)
	}
	remap := map[int]int{}
	for i, old := range order {
		remap[old] = i
	}
	byID := map[int]UniversalASTNode{}
	for _, n := range f.Nodes {
		byID[n.ID] = n
	}
	f.Nodes = f.Nodes[:0]
	for _, old := range order {
		n := byID[old]
		n.ID = remap[old]
		if raw, err := json.Marshal(n.ID); err == nil && n.Fields != nil {
			n.Fields["id"] = raw
		}
		f.Nodes = append(f.Nodes, n)
	}
	rewrite := func(id int) int {
		if n, ok := remap[id]; ok {
			return n
		}
		return id
	}
	for i := range f.Fields {
		f.Fields[i].NodeID = rewrite(f.Fields[i].NodeID)
		if f.Fields[i].Name == "id" {
			f.Fields[i].Value, _ = json.Marshal(f.Fields[i].NodeID)
		}
	}
	for i := range f.Sources {
		f.Sources[i].NodeID = rewrite(f.Sources[i].NodeID)
	}
	for i := range f.TypesFact {
		f.TypesFact[i].NodeID = rewrite(f.TypesFact[i].NodeID)
	}
	for i := range f.Symbols {
		f.Symbols[i].NodeID = rewrite(f.Symbols[i].NodeID)
	}
	fixRelation := func(r *FrontendRelationFact) {
		r.From = rewrite(r.From)
		if r.To.Domain == "node" {
			if id, e := strconv.Atoi(r.To.ID); e == nil {
				r.To.ID = strconv.Itoa(rewrite(id))
			}
		}
	}
	for i := range f.Bindings {
		fixRelation(&f.Bindings[i])
	}
	for i := range f.Relations {
		fixRelation(&f.Relations[i])
	}
	for i := range f.Returns {
		f.Returns[i].ReturnNodeID = rewrite(f.Returns[i].ReturnNodeID)
		f.Returns[i].FunctionNodeID = rewrite(f.Returns[i].FunctionNodeID)
		f.Returns[i].ValueNodeID = rewrite(f.Returns[i].ValueNodeID)
	}
	return nil
}

type factParser struct {
	t         []token
	i         int
	sink      FrontendFactSink
	scope     int
	nextScope int
}

func parseFrontendFacts(language, code string, sink FrontendFactSink) (FrontendSemanticFacts, error) {
	textSemanticParseCalls.Add(1)
	if sink == nil {
		return FrontendSemanticFacts{}, fmt.Errorf("missing frontend fact sink")
	}
	b, ok := sink.(*FrontendFactsBuilder)
	if !ok {
		return FrontendSemanticFacts{}, fmt.Errorf("frontend fact sink does not expose collected facts")
	}
	header, err := NewUniversalASTDocument(language)
	if err != nil {
		return FrontendSemanticFacts{}, err
	}
	ts, err := lex(code)
	if err != nil {
		return FrontendSemanticFacts{}, err
	}
	p := &factParser{t: ts, sink: sink, nextScope: 1}
	root, err := p.program()
	if err != nil {
		return FrontendSemanticFacts{}, err
	}
	if err := canonicalizeFrontendFactOrder(&b.Facts, root.ID); err != nil {
		return FrontendSemanticFacts{}, err
	}
	evaluation := "eager_left_to_right"
	if language == "r" {
		evaluation = "lazy_demand"
	}
	// The builder has collected every parser fact.  Header values are the same
	// executable contract formerly supplied by ParseSemantic/NewSemanticProgram.
	b.Facts.SchemaVersion = header.SchemaVersion
	b.Facts.BasisSHA256 = header.BasisSHA256
	b.Facts.LanguageProfile = header.LanguageProfile
	b.Facts.LanguageFacet = append([]float64(nil), header.LanguageFacet...)
	b.Facts.Projection = "frontend_facts.v1"
	b.Facts.Evaluation = evaluation
	b.Facts.ValueModel = "tagged_dynamic_binary64"
	b.Facts.IndexBase = 1
	b.Facts.Types = defaultSemanticTypeContract()
	b.Facts.Origin = SemanticOrigin{SourceLanguage: language, EntryPoint: "main"}
	b.Facts.Metadata = map[string]string{"frontend": "matrix-facts-v1"}
	if semanticProfileLanguage(language) != "" {
		profile := &SemanticProgram{}
		if err := profile.AttachSemanticFeatureProfile(language); err != nil {
			return FrontendSemanticFacts{}, err
		}
		b.Facts.SemanticFeatures = profile.SemanticFeatures
	}
	return b.Facts, nil
}

func (p *factParser) cur() token { return p.t[p.i] }
func (p *factParser) next() token {
	t := p.cur()
	if p.i < len(p.t)-1 {
		p.i++
	}
	return t
}
func (p *factParser) accept(k tokenKind, text string) bool {
	if p.cur().kind == k && (text == "" || p.cur().text == text) {
		p.i++
		return true
	}
	return false
}
func (p *factParser) expect(k tokenKind, text string) (token, error) {
	if p.cur().kind != k || (text != "" && p.cur().text != text) {
		return token{}, fmt.Errorf("expected %q near %q", text, p.cur().text)
	}
	return p.next(), nil
}
func (p *factParser) skipSep() {
	for p.accept(tokSemicolon, "") {
	}
}

func (p *factParser) emit(structural, kind, name string, operation universalOperationRecord) (ParsedNode, error) {
	n := UniversalASTNode{ID: -1, StructuralKind: structural, SemanticFacets: defaultUniversalFacets(structural), Fields: map[string]json.RawMessage{}}
	mask, err := universalFieldMask(&n)
	if err != nil {
		return ParsedNode{}, err
	}
	n.FieldMask = mask
	n.ID = p.countNodes()
	put := func(key string, value any) {
		if !containsString(n.FieldMask, key) {
			return
		}
		data, e := json.Marshal(value)
		if e == nil && string(data) != "null" {
			n.Fields[key] = data
		}
	}
	put("id", n.ID)
	put("kind", kind)
	put("scope_id", p.scope)
	if name != "" {
		put("name", name)
	}
	if operation.Operator != "" || operation.LiteralKind != "" || operation.Text != "" || operation.AssignOp != "" || operation.Semantics.Operation != "" {
		put("operation", operation)
	}
	p.sink.AddNode(n)
	p.sink.AddField(FrontendFieldFact{NodeID: n.ID, Name: "id", Value: append(json.RawMessage(nil), n.Fields["id"]...)})
	return ParsedNode{ID: n.ID, Kind: kind}, nil
}

func (p *factParser) countNodes() int { b := p.sink.(*FrontendFactsBuilder); return len(b.Facts.Nodes) }
func (p *factParser) child(parent, child ParsedNode, role string, ordinal int, name string, missing bool) {
	attrs := map[string]json.RawMessage{}
	for k, v := range map[string]any{"role": role, "ordinal": ordinal, "name": name, "missing": missing} {
		raw, _ := json.Marshal(v)
		attrs[k] = raw
	}
	p.sink.AddRole(FrontendRelationFact{Kind: "syntax.child", From: parent.ID, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(child.ID)}, Role: role, Ordinal: ordinal, Attributes: attrs})
}

func (p *factParser) program() (ParsedNode, error) {
	root, err := p.emit("Scope", "block", "", universalOperationRecord{})
	if err != nil {
		return ParsedNode{}, err
	}
	p.skipSep()
	ordinal := 0
	for p.cur().kind != tokEOF {
		s, e := p.statement()
		if e != nil {
			return ParsedNode{}, e
		}
		p.child(root, s, "statement", ordinal, "", false)
		ordinal++
		p.skipSep()
	}
	return root, nil
}

func (p *factParser) statement() (ParsedNode, error) {
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
			n, e := p.emit("LoopStmt", "repeat", "", universalOperationRecord{})
			if e != nil {
				return n, e
			}
			b, e := p.statement()
			if e == nil {
				p.child(n, b, "body", 0, "", false)
			}
			return n, e
		case "return":
			p.next()
			n, e := p.emit("ReturnStmt", "return", "", universalOperationRecord{})
			if e != nil {
				return n, e
			}
			if p.accept(tokLParen, "") {
				x, e := p.expression(0)
				if e != nil {
					return n, e
				}
				if _, e = p.expect(tokRParen, ""); e != nil {
					return n, e
				}
				p.child(n, x, "expression", 0, "", false)
				return n, nil
			}
			x, e := p.expression(0)
			if e == nil {
				p.child(n, x, "expression", 0, "", false)
			}
			return n, e
		case "break":
			p.next()
			return p.emit("BreakStmt", "break", "", universalOperationRecord{})
		case "next":
			p.next()
			return p.emit("ContinueStmt", "continue", "", universalOperationRecord{})
		}
		if p.i+1 < len(p.t) && p.t[p.i+1].kind == tokOp && (p.t[p.i+1].text == "<-" || p.t[p.i+1].text == "<<-" || p.t[p.i+1].text == "=") {
			name := p.next().text
			op := p.next().text
			n, e := p.emit("AssignStmt", "assign", name, universalOperationRecord{AssignOp: op})
			if e != nil {
				return n, e
			}
			x, e := p.expression(0)
			if e == nil {
				p.child(n, x, "expression", 0, "", false)
			}
			return n, e
		}
	}
	x, e := p.expression(0)
	if e != nil {
		return ParsedNode{}, e
	}
	if p.cur().kind == tokOp && (p.cur().text == "->" || p.cur().text == "->>") {
		op := p.next().text
		name, e2 := p.expect(tokIdent, "")
		if e2 != nil {
			return ParsedNode{}, e2
		}
		n, e2 := p.emit("AssignStmt", "assign", name.text, universalOperationRecord{AssignOp: op})
		if e2 == nil {
			p.child(n, x, "expression", 0, "", false)
		}
		return n, e2
	}
	n, e := p.emit("OperationExpr", "expression", "", universalOperationRecord{})
	if e == nil {
		p.child(n, x, "expression", 0, "", false)
	}
	return n, e
}

func (p *factParser) blockAfterOpen() (ParsedNode, error) {
	return p.blockAfterOpenInScope(false)
}

func (p *factParser) blockAfterOpenInScope(enterScope bool) (ParsedNode, error) {
	old := p.scope
	if enterScope {
		p.scope = p.nextScope
		p.nextScope++
	}
	b, e := p.emit("Scope", "block", "", universalOperationRecord{})
	if e != nil {
		p.scope = old
		return b, e
	}
	p.skipSep()
	ord := 0
	for p.cur().kind != tokRBrace && p.cur().kind != tokEOF {
		s, e := p.statement()
		if e != nil {
			p.scope = old
			return b, e
		}
		p.child(b, s, "statement", ord, "", false)
		ord++
		p.skipSep()
	}
	_, e = p.expect(tokRBrace, "")
	p.scope = old
	return b, e
}
func (p *factParser) parseIf() (ParsedNode, error) {
	p.next()
	if _, e := p.expect(tokLParen, ""); e != nil {
		return ParsedNode{}, e
	}
	n, e := p.emit("IfStmt", "if", "", universalOperationRecord{})
	if e != nil {
		return n, e
	}
	c, e := p.expression(0)
	if e != nil {
		return n, e
	}
	if _, e = p.expect(tokRParen, ""); e != nil {
		return n, e
	}
	p.child(n, c, "condition", 0, "", false)
	p.skipSep()
	th, e := p.statement()
	if e != nil {
		return n, e
	}
	p.child(n, th, "then", 0, "", false)
	p.skipSep()
	if p.cur().kind == tokIdent && p.cur().text == "else" {
		p.next()
		p.skipSep()
		el, e := p.statement()
		if e != nil {
			return n, e
		}
		p.child(n, el, "else", 0, "", false)
	}
	return n, nil
}
func (p *factParser) parseWhile() (ParsedNode, error) {
	p.next()
	if _, e := p.expect(tokLParen, ""); e != nil {
		return ParsedNode{}, e
	}
	n, e := p.emit("LoopStmt", "while", "", universalOperationRecord{})
	if e != nil {
		return n, e
	}
	c, e := p.expression(0)
	if e != nil {
		return n, e
	}
	if _, e = p.expect(tokRParen, ""); e != nil {
		return n, e
	}
	p.child(n, c, "condition", 0, "", false)
	p.skipSep()
	b, e := p.statement()
	if e == nil {
		p.child(n, b, "body", 0, "", false)
	}
	return n, e
}
func (p *factParser) parseFor() (ParsedNode, error) {
	p.next()
	if _, e := p.expect(tokLParen, ""); e != nil {
		return ParsedNode{}, e
	}
	name, pattern, e := p.parseForBinding()
	if e != nil {
		return ParsedNode{}, e
	}
	in, e := p.expect(tokIdent, "")
	if e != nil || in.text != "in" {
		return ParsedNode{}, fmt.Errorf("expected in in for")
	}
	n, e := p.emit("ForEachStmt", "for", name.text, universalOperationRecord{})
	if e != nil {
		return n, e
	}
	if pattern.ID >= 0 {
		p.child(n, pattern, "binding", 0, "", false)
	}
	seq, e := p.expression(0)
	if e != nil {
		return n, e
	}
	if _, e = p.expect(tokRParen, ""); e != nil {
		return n, e
	}
	p.child(n, seq, "sequence", 0, "", false)
	p.skipSep()
	b, e := p.statement()
	if e == nil {
		p.child(n, b, "body", 0, "", false)
	}
	return n, e
}

// parseForBinding consumes the exact matrix-supported binding quotient: a
// single identifier or an ordered tuple/list of identifier bindings.  The
// latter is represented by the schema's existing BindingPattern structure.
// Starred, nested, attribute and repeated bindings stay rejected because their
// cardinality or assignment semantics are not established by this frontend.
func (p *factParser) parseForBinding() (token, ParsedNode, error) {
	if p.cur().kind == tokIdent {
		name := p.next()
		return name, ParsedNode{ID: -1}, nil
	}
	if p.cur().kind != tokLParen && p.cur().kind != tokLBracket {
		return token{}, ParsedNode{}, fmt.Errorf("expected loop binding near %q", p.cur().text)
	}
	open := p.next().kind
	close := tokRParen
	if open == tokLBracket {
		close = tokRBracket
	}
	pattern, err := p.emit("BindingPattern", "binding_pattern", "", universalOperationRecord{})
	if err != nil {
		return token{}, pattern, err
	}
	ordinal := 0
	for {
		name, e := p.expect(tokIdent, "")
		if e != nil || name.text == "_" {
			if e == nil {
				e = fmt.Errorf("unsupported loop binding %q", name.text)
			}
			return token{}, pattern, e
		}
		binding, e := p.emit("SymbolRef", "identifier", name.text, universalOperationRecord{})
		if e != nil {
			return token{}, pattern, e
		}
		p.child(pattern, binding, "binding", ordinal, name.text, false)
		ordinal++
		if p.accept(close, "") {
			break
		}
		if _, e = p.expect(tokComma, ""); e != nil {
			return token{}, pattern, e
		}
	}
	if ordinal < 2 {
		return token{}, pattern, fmt.Errorf("loop pattern requires two or more bindings")
	}
	return token{text: ""}, pattern, nil
}

func (p *factParser) expression(min int) (ParsedNode, error) {
	l, e := p.prefix()
	if e != nil {
		return l, e
	}
	for {
		if p.cur().kind == tokLParen {
			args, e := p.args(tokRParen)
			if e != nil {
				return l, e
			}
			n, e := p.emit("CallExpr", "call", "", universalOperationRecord{Semantics: SemanticSemantics{Operation: "call", Dispatch: "unknown", EvaluationOrder: "source_defined"}})
			if e != nil {
				return l, e
			}
			p.child(n, l, "value", 0, "", false)
			for i, a := range args {
				if a.node.ID >= 0 {
					p.child(n, a.node, "argument", i, a.name, a.missing)
				} else {
					m, e := p.emit("LiteralExpr", "missing_argument", "", universalOperationRecord{LiteralKind: "missing"})
					if e != nil {
						return l, e
					}
					p.child(n, m, "argument", i, a.name, true)
				}
			}
			l = n
			continue
		}
		if p.cur().kind == tokLBracket {
			p.next()
			dbl := p.accept(tokLBracket, "")
			args, e := p.argsNoOpen(tokRBracket)
			if e != nil {
				return l, e
			}
			if dbl {
				if _, e = p.expect(tokRBracket, ""); e != nil {
					return l, e
				}
			}
			n, e := p.emit("IndexExpr", "index", "", universalOperationRecord{DoubleIndex: dbl, Semantics: SemanticSemantics{Operation: "index", IndexBase: 1, NegativeIndex: "unknown", OutOfBounds: "unknown", Slicing: "unknown"}})
			if e != nil {
				return l, e
			}
			p.child(n, l, "value", 0, "", false)
			for i, a := range args {
				if a.node.ID >= 0 {
					p.child(n, a.node, "argument", i, a.name, a.missing)
				}
			}
			l = n
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
		next := pr + 1
		if op == "^" || op == "**" {
			next = pr
		}
		r, e := p.expression(next)
		if e != nil {
			return l, e
		}
		sem := SemanticSemantics{Dispatch: "builtin", EvaluationOrder: "left_to_right", ShortCircuit: op == "&&" || op == "||"}
		sem.Operation = map[string]string{"+": "add", "-": "subtract", "*": "multiply", "/": "divide", "%%": "remainder", "==": "equal", "!=": "not_equal", "<": "less_than", "<=": "less_or_equal", ">": "greater_than", ">=": "greater_or_equal", "&&": "logical_and", "||": "logical_or"}[op]
		n, e := p.emit("OperationExpr", "binary", "", universalOperationRecord{Operator: op, Semantics: sem})
		if e != nil {
			return l, e
		}
		p.child(n, l, "left", 0, "", false)
		p.child(n, r, "right", 0, "", false)
		l = n
	}
	return l, nil
}

type parsedArgument struct {
	name    string
	missing bool
	node    ParsedNode
}

func (p *factParser) prefix() (ParsedNode, error) {
	t := p.cur()
	if t.kind == tokOp && (t.text == "+" || t.text == "-" || t.text == "!" || t.text == "~") {
		p.next()
		n, e := p.emit("OperationExpr", "unary", "", universalOperationRecord{Operator: t.text, Semantics: SemanticSemantics{Operation: map[string]string{"-": "negate", "+": "identity", "!": "logical_not"}[t.text], Dispatch: "builtin"}})
		if e != nil {
			return n, e
		}
		x, e := p.expression(9)
		if e == nil {
			p.child(n, x, "value", 0, "", false)
		}
		return n, e
	}
	if t.kind == tokNumber {
		p.next()
		return p.emit("LiteralExpr", "literal", "", universalOperationRecord{LiteralKind: "number", Text: t.text})
	}
	if t.kind == tokString {
		p.next()
		return p.emit("LiteralExpr", "literal", "", universalOperationRecord{LiteralKind: "string", Text: t.text})
	}
	if t.kind == tokIdent {
		p.next()
		if t.text == "function" {
			return p.functionExpr()
		}
		if t.text == "NULL" {
			return p.emit("NilLiteral", "literal", "", universalOperationRecord{LiteralKind: "null", Text: t.text})
		}
		if t.text == "NA" || t.text == "NA_integer_" || t.text == "NA_real_" || t.text == "NA_character_" || t.text == "NA_complex_" {
			return p.emit("NilLiteral", "literal", "", universalOperationRecord{LiteralKind: "na", Text: t.text})
		}
		if t.text == "NaN" {
			return p.emit("LiteralExpr", "literal", "", universalOperationRecord{LiteralKind: "nan", Text: t.text})
		}
		if t.text == "TRUE" || t.text == "FALSE" || t.text == "T" || t.text == "F" {
			return p.emit("LiteralExpr", "literal", "", universalOperationRecord{LiteralKind: "boolean", Text: t.text})
		}
		return p.emit("SymbolRef", "identifier", t.text, universalOperationRecord{})
	}
	if p.accept(tokLParen, "") {
		x, e := p.expression(0)
		if e != nil {
			return x, e
		}
		_, e = p.expect(tokRParen, "")
		return x, e
	}
	return ParsedNode{}, fmt.Errorf("expected expression near %q", t.text)
}

func (p *factParser) functionExpr() (ParsedNode, error) {
	if _, e := p.expect(tokLParen, ""); e != nil {
		return ParsedNode{}, e
	}
	n, e := p.emit("ClosureExpr", "function", "", universalOperationRecord{})
	if e != nil {
		return n, e
	}
	functionScope := p.nextScope
	p.nextScope++
	params := []parsedArgument{}
	if !p.accept(tokRParen, "") {
		for {
			name, e := p.expect(tokIdent, "")
			if e != nil {
				return n, e
			}
			param, e := p.emit("ParameterDecl", "parameter", name.text, universalOperationRecord{})
			if e != nil {
				return n, e
			}
			if p.cur().kind == tokOp && p.cur().text == "=" {
				p.next()
				outerScope := p.scope
				p.scope = functionScope
				d, e := p.expression(0)
				p.scope = outerScope
				if e != nil {
					return n, e
				}
				p.child(param, d, "default", 0, "", false)
			}
			params = append(params, parsedArgument{node: param})
			if p.accept(tokRParen, "") {
				break
			}
			if _, e = p.expect(tokComma, ""); e != nil {
				return n, e
			}
		}
	}
	for i, param := range params {
		p.child(n, param.node, "parameter", i, "", false)
	}
	p.skipSep()
	if !p.accept(tokLBrace, "") {
		return n, fmt.Errorf("function body must be a block")
	}
	outerScope := p.scope
	p.scope = functionScope
	b, e := p.blockAfterOpen()
	p.scope = outerScope
	if e == nil {
		p.child(n, b, "body", 0, "", false)
	}
	return n, e
}
func (p *factParser) args(close tokenKind) ([]parsedArgument, error) {
	p.next()
	return p.argsNoOpen(close)
}
func (p *factParser) argsNoOpen(close tokenKind) ([]parsedArgument, error) {
	out := []parsedArgument{}
	if p.accept(close, "") {
		return out, nil
	}
	for {
		if p.cur().kind == tokComma {
			out = append(out, parsedArgument{missing: true, node: ParsedNode{ID: -1}})
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
		out = append(out, parsedArgument{name: name, node: x})
		if p.accept(close, "") {
			break
		}
		if _, e = p.expect(tokComma, ""); e != nil {
			return nil, e
		}
		if p.accept(close, "") {
			out = append(out, parsedArgument{missing: true, node: ParsedNode{ID: -1}})
			break
		}
	}
	return out, nil
}
