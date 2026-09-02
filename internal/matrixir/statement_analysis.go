package matrixir

import "strings"

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
