package matrixir

import (
	"fmt"
	"strings"
)

// AnalyzeSemanticStatementTokens consumes the grammar tokens attached to a
// recognised statement. It is deliberately separate from the legacy text
// helper below: structured MatrixIR paths must never parse Event.Text again.
func AnalyzeSemanticStatementTokens(language string, event CanonicalSemanticEvent, tokens []Lexeme) ([]CanonicalSemanticEvent, error) {
	tokens = significant(append([]Lexeme(nil), tokens...))
	switch event.Action {
	case "assign":
		for at, tok := range tokens {
			if tok.Class != TokenOperator || (tok.Text != "<-" && tok.Text != "<<-" && tok.Text != "=" && tok.Text != "->" && tok.Text != "->>") {
				continue
			}
			left, err := analyzeAssignmentTarget(language, tokens[:at])
			if err != nil {
				return nil, err
			}
			right, err := AnalyzeSemanticTokens(language, tokens[at+1:])
			if err != nil {
				return nil, err
			}
			return joinAssignmentFacts(event, left, right, tok.Text), nil
		}
	case "return":
		if len(tokens) > 1 {
			children, err := AnalyzeSemanticTokens(language, tokens[1:])
			if err != nil {
				return nil, err
			}
			id := len(children)
			return append(children, CanonicalSemanticEvent{ID: id, Action: "return", StructureKind: "return", SourceOffset: event.SourceOffset, Roles: []CanonicalRoleFact{{OwnerNodeID: id, ChildNodeID: children[len(children)-1].ID, Role: "value"}}}), nil
		}
	case "for":
		return emitStructuredIteration(language, event, tokens)
	case "function":
		return emitStructuredClosure(language, event, tokens)
	}
	// Expressions include aggregates, indexing, slices and Python lambdas.
	if event.Action == "expression" || event.Action == "print" {
		// `print(...)` is already canonical syntax. Keeping its callee lets the
		// ordinary CallExpr path represent it; stripping `print` would turn the
		// remaining parenthesised argument list into a TupleExpr.
		if len(tokens) > 0 && tokens[0].Text == "func" {
			return emitStructuredClosure(language, event, tokens)
		}
		return AnalyzeSemanticTokens(language, tokens)
	}
	// No structured lowering was performed. Returning the already-global base
	// event here would make Canonicalize offset its ID and source position a
	// second time, producing duplicate IDs and dangling syntax.child targets.
	// An empty result tells Canonicalize to retain the original grammar event.
	return nil, nil
}

// analyzeAssignmentTarget distinguishes a proven declaration binding from a
// general lvalue. Typed/modifier-prefixed declarations share this token shape
// across the supported languages; indexed and member lvalues retain their
// normal expression structure.
func analyzeAssignmentTarget(language string, tokens []Lexeme) ([]CanonicalSemanticEvent, error) {
	for _, token := range tokens {
		if token.Text == "[" || token.Text == "." || token.Text == "$" {
			return AnalyzeSemanticTokens(language, tokens)
		}
	}
	identifiers := []Lexeme{}
	hasDeclarationPrefix := false
	for _, token := range tokens {
		if token.Class != TokenIdentifier {
			continue
		}
		if isTypeWord(token.Text) || isBindingModifier(token.Text) {
			hasDeclarationPrefix = true
			continue
		}
		identifiers = append(identifiers, token)
	}
	if hasDeclarationPrefix && len(identifiers) > 0 {
		name := identifiers[len(identifiers)-1]
		return []CanonicalSemanticEvent{{ID: 0, StructureKind: "identifier", Text: name.Text, SourceOffset: name.Start, Fields: map[string]string{"name": name.Text}}}, nil
	}
	return AnalyzeSemanticTokens(language, tokens)
}

func joinAssignmentFacts(event CanonicalSemanticEvent, left, right []CanonicalSemanticEvent, op string) []CanonicalSemanticEvent {
	offset := len(left)
	for i := range right {
		right[i].ID += offset
		for j := range right[i].Roles {
			right[i].Roles[j].OwnerNodeID += offset
			right[i].Roles[j].ChildNodeID += offset
		}
	}
	id := len(left) + len(right)
	assignment := CanonicalSemanticEvent{ID: id, Action: "assign", StructureKind: "assign", Text: op, SourceOffset: event.SourceOffset, Roles: []CanonicalRoleFact{{OwnerNodeID: id, ChildNodeID: left[len(left)-1].ID, Role: "target"}, {OwnerNodeID: id, ChildNodeID: right[len(right)-1].ID, Role: "value"}}}
	return append(append(left, right...), assignment)
}

func emitStructuredIteration(language string, event CanonicalSemanticEvent, tokens []Lexeme) ([]CanonicalSemanticEvent, error) {
	roles := []CanonicalRoleFact{}
	forAt, inAt, rangeAt := -1, -1, -1
	for i, t := range tokens {
		if t.Text == "for" {
			forAt = i
		}
		if t.Text == "in" {
			inAt = i
		}
		if t.Text == "range" {
			rangeAt = i
		}
	}
	_ = forAt
	bindEnd := inAt
	if bindEnd < 0 {
		bindEnd = rangeAt
	}
	if bindEnd < 0 {
		return nil, fmt.Errorf("MISSING_STRUCTURED_PARSER_DATA: iteration iterable")
	}
	events := []CanonicalSemanticEvent{}
	bindingIDs := []int{}
	for _, t := range tokens[1:bindEnd] {
		if t.Class == TokenIdentifier && !isBindingModifier(t.Text) && !isTypeWord(t.Text) {
			id := len(events)
			events = append(events, CanonicalSemanticEvent{ID: id, StructureKind: "identifier", Text: t.Text, SourceOffset: t.Start})
			bindingIDs = append(bindingIDs, id)
		}
	}
	if len(bindingIDs) == 1 {
		roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: bindingIDs[0], Role: "binding"})
	} else if len(bindingIDs) > 1 {
		patternID := len(events)
		patternRoles := make([]CanonicalRoleFact, len(bindingIDs))
		for i, bindingID := range bindingIDs {
			patternRoles[i] = CanonicalRoleFact{OwnerNodeID: patternID, ChildNodeID: bindingID, Role: "binding", Ordinal: i}
		}
		events = append(events, CanonicalSemanticEvent{ID: patternID, StructureKind: "binding", SourceOffset: event.SourceOffset, Roles: patternRoles})
		roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: patternID, Role: "binding"})
	}
	iterStart := bindEnd + 1
	if rangeAt >= 0 {
		iterStart = rangeAt + 1
	}
	iterTokens := tokens[iterStart:]
	for len(iterTokens) > 0 && (iterTokens[len(iterTokens)-1].Text == "{" || iterTokens[len(iterTokens)-1].Text == ":") {
		iterTokens = iterTokens[:len(iterTokens)-1]
	}
	iter, err := AnalyzeSemanticTokens(language, iterTokens)
	if err != nil {
		return nil, err
	}
	offset := len(events)
	for i := range iter {
		iter[i].ID += offset
		for j := range iter[i].Roles {
			iter[i].Roles[j].OwnerNodeID += offset
			iter[i].Roles[j].ChildNodeID += offset
		}
	}
	events = append(events, iter...)
	roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: events[len(events)-1].ID, Role: "iterable"})
	id := len(events)
	for i := range roles {
		roles[i].OwnerNodeID = id
	}
	events = append(events, CanonicalSemanticEvent{ID: id, Action: "for", StructureKind: "iteration", SourceOffset: event.SourceOffset, Roles: roles, FactFamily: ParsedIteration})
	return events, nil
}

func emitStructuredClosure(language string, event CanonicalSemanticEvent, tokens []Lexeme) ([]CanonicalSemanticEvent, error) {
	events := []CanonicalSemanticEvent{}
	roles := []CanonicalRoleFact{}
	open, close := -1, -1
	for i, t := range tokens {
		if t.Text == "(" && open < 0 {
			open = i
		}
		if t.Text == ")" {
			close = i
			break
		}
	}
	if open < 0 || close < open {
		return nil, fmt.Errorf("MISSING_STRUCTURED_PARSER_DATA: closure parameters")
	}
	for _, t := range tokens[open+1 : close] {
		if t.Class == TokenIdentifier {
			id := len(events)
			events = append(events, CanonicalSemanticEvent{ID: id, StructureKind: "identifier", Text: t.Text, SourceOffset: t.Start})
			roles = append(roles, CanonicalRoleFact{OwnerNodeID: -1, ChildNodeID: id, Role: "parameter", Ordinal: len(roles)})
		}
	}
	id := len(events)
	for i := range roles {
		roles[i].OwnerNodeID = id
	}
	events = append(events, CanonicalSemanticEvent{ID: id, Action: "function", StructureKind: "closure", SourceOffset: event.SourceOffset, Roles: roles, FactFamily: ParsedClosure})
	return events, nil
}

// AnalyzeSemanticStatement recognizes the statement forms already selected by
// the matrix action layer. It is a temporary frontend analysis and does not
// create legacy Stmt values.
func AnalyzeSemanticStatement(language string, event CanonicalSemanticEvent) ([]CanonicalSemanticEvent, error) {
	text := strings.TrimSpace(event.Text)
	switch event.Action {
	case "assign":
		for _, op := range []string{"<-", "<<-", "=", "->", "->>"} {
			if at := strings.Index(text, op); at >= 0 {
				left, err := AnalyzeSemanticExpression(language, strings.TrimSpace(text[:at]))
				if err != nil {
					return nil, err
				}
				right, err := AnalyzeSemanticExpression(language, strings.TrimSpace(text[at+len(op):]))
				if err != nil {
					return nil, err
				}
				offset := len(left)
				for i := range right {
					right[i].ID += offset
					for j := range right[i].Roles {
						right[i].Roles[j].OwnerNodeID += offset
						right[i].Roles[j].ChildNodeID += offset
					}
				}
				base := len(left) + len(right)
				assignment := CanonicalSemanticEvent{ID: base, Action: "assign", StructureKind: "assign", Text: op, SourceOffset: event.SourceOffset, Roles: []CanonicalRoleFact{{OwnerNodeID: base, ChildNodeID: left[len(left)-1].ID, Role: "target"}, {OwnerNodeID: base, ChildNodeID: right[len(right)-1].ID, Role: "value"}}}
				return append(append(left, right...), assignment), nil
			}
		}
	case "return":
		value := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(text, "return("), ")"))
		children, err := AnalyzeSemanticExpression(language, value)
		if err != nil {
			return nil, err
		}
		id := len(children)
		return append(children, CanonicalSemanticEvent{ID: id, Action: "return", StructureKind: "return", SourceOffset: event.SourceOffset, Roles: []CanonicalRoleFact{{OwnerNodeID: id, ChildNodeID: children[len(children)-1].ID, Role: "value"}}}), nil
	}
	return []CanonicalSemanticEvent{event}, nil
}
