package backend

import (
	"encoding/json"
	"testing"
)

func TestFrontendSemanticFactsBuildCanonicalUAST(t *testing.T) {
	program, err := LowerNativeGo("facts.go", `package main
import "fmt"
func main() { fmt.Println("fact") }
`)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := frontendSemanticFactsFromUniversalAST(program.UniversalAST, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(facts.Nodes) == 0 || len(facts.Fields) == 0 || len(facts.Sources) == 0 || len(facts.Relations) == 0 {
		t.Fatalf("incomplete shared frontend facts: nodes=%d fields=%d sources=%d relations=%d", len(facts.Nodes), len(facts.Fields), len(facts.Sources), len(facts.Relations))
	}
	rebuilt, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		t.Fatal(err)
	}
	want, err := json.Marshal(program.UniversalAST)
	if err != nil {
		t.Fatal(err)
	}
	got, err := json.Marshal(rebuilt)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("shared frontend facts changed canonical UAST")
	}
}

func TestFrontendSemanticFactsRejectUnknownRelationEndpoint(t *testing.T) {
	facts := FrontendSemanticFacts{
		LanguageProfile: "go",
		Nodes:           []UniversalASTNode{{ID: 1}},
		Relations:       []FrontendRelationFact{{Kind: "syntax.child", From: 2, To: UniversalASTReference{Domain: "node", ID: "1"}}},
	}
	if _, err := BuildRawUniversalASTFromFrontendFacts(facts); err == nil {
		t.Fatal("shared frontend builder accepted unknown relation endpoint")
	}
}
