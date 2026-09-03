package matrixir

import "fmt"

// LegacyTraceRow is a structured teacher observation. It contains no source
// text and is suitable for joining against offline lexer/parser tables.
type LegacyTraceRow struct {
	Language string
	Token string
	TokenClass string
	LexerContext string
	ParserContext string
	Lookahead string
	ConsumedSymbol string
	ProducedNode int
	ChildRole string
	FieldRole string
	OperatorRole string
	BindingRole string
	ControlRole string
	SemanticEvent int
}

// TraceCanonicalize records the stable semantic-event/role observations of
// the existing parser for bootstrap and differential validation.
func TraceCanonicalize(language, source string) (CanonicalProgram, []LegacyTraceRow, error) {
	p, err := Canonicalize(language, source)
	if err != nil { return CanonicalProgram{}, nil, err }
	lexemes := Tokenize(language, source)
	rows := make([]LegacyTraceRow, 0, len(lexemes)+len(p.SemanticEvents))
	for i, token := range lexemes {
		lookahead := ""
		if i+1 < len(lexemes) { lookahead = lexemes[i+1].Text }
		rows = append(rows, LegacyTraceRow{
			Language: language, Token: token.Text, TokenClass: fmt.Sprint(token.Class),
			LexerContext: language, ParserContext: "token", Lookahead: lookahead,
			ConsumedSymbol: token.Text,
		})
	}
	for _, e := range p.SemanticEvents {
		for _, r := range e.Roles {
			rows = append(rows, LegacyTraceRow{Language: language, ProducedNode: r.OwnerNodeID, ChildRole: r.Role, SemanticEvent: e.ID, ParserContext: e.StructureKind})
		}
		if len(e.Roles)==0 { rows = append(rows, LegacyTraceRow{Language: language, ProducedNode: e.ID, SemanticEvent: e.ID, ParserContext: e.StructureKind}) }
	}
	return p, rows, nil
}
