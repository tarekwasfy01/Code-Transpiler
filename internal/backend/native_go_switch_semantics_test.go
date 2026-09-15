// Copyright (c) 2026 Tarek Wasfy
package backend

import "testing"

func TestNativeGoSwitchClausesSurviveCanonicalUASTRoundTrip(t *testing.T) {
	source := `package main
func main() {
	x := float64(1)
	switch {
	case true, false:
		x = 37.0
	default:
		x = 42.0
	}
	_ = x
}
`
	p, err := LowerNativeGo("switch.go", source)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	u, err := ProjectSemanticDocumentToUniversal(doc)
	if err != nil {
		t.Fatal(err)
	}
	back, err := SemanticDocumentFromUniversalAST(u)
	if err != nil {
		t.Fatal(err)
	}
	var find func(*SemanticStatement) *SemanticStatement
	find = func(s *SemanticStatement) *SemanticStatement {
		if s == nil {
			return nil
		}
		if s.Kind == "switch" {
			return s
		}
		if found := find(s.Body); found != nil {
			return found
		}
		if found := find(s.Then); found != nil {
			return found
		}
		if found := find(s.Else); found != nil {
			return found
		}
		for i := range s.Statements {
			if found := find(&s.Statements[i]); found != nil {
				return found
			}
		}
		return nil
	}
	switchStmt := find(&back.Root)
	if switchStmt == nil {
		t.Fatal("switch node was lost in SemanticProgram -> UAST -> SemanticProgram roundtrip")
	}
	if len(switchStmt.Statements) != 2 {
		t.Fatalf("switch retained %d clauses; want case + default", len(switchStmt.Statements))
	}
	caseStmt, defaultStmt := switchStmt.Statements[0], switchStmt.Statements[1]
	if caseStmt.Kind != "switch_case" || len(caseStmt.Patterns) != 2 {
		t.Fatalf("case clause lost kind/pattern alternatives: kind=%q patterns=%d", caseStmt.Kind, len(caseStmt.Patterns))
	}
	if defaultStmt.Kind != "switch_default" || defaultStmt.Body == nil {
		t.Fatalf("default clause lost its explicit body: kind=%q body=%v", defaultStmt.Kind, defaultStmt.Body != nil)
	}
}
