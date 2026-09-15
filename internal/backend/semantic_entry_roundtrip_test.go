// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"os"
	"testing"
)

func TestNativeGoFunctionBindingsSurviveSERoundTrip(t *testing.T) {
	source := `package main
func helper() int { return 37 }
func main() { _ = helper() }
`
	program, err := LowerNativeGo("input.go", source)
	if err != nil {
		t.Fatal(err)
	}
	bindings, ok := program.Extensions["function_entry_bindings"].(map[string]string)
	if !ok || bindings["main"] == "" || bindings["helper"] == "" {
		t.Fatalf("missing source function bindings: %#v", program.Extensions["function_entry_bindings"])
	}
	if bindings["main"] == bindings["helper"] {
		t.Fatalf("distinct functions share binding %q", bindings["main"])
	}
	encoded, err := program.MarshalSemanticSEReadable()
	if err != nil {
		t.Fatal(err)
	}
	roundTrip, err := ParseSemanticSE(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !semanticProgramHasFunction(roundTrip, "main") {
		t.Fatalf("SE round trip lost main entry; program extensions=%#v UAST extensions=%#v", roundTrip.Extensions, roundTrip.UniversalAST.Extensions)
	}
}

func TestSemanticEntryFixture(t *testing.T) {
	path := os.Getenv("SEMANTIC_ENTRY_FIXTURE")
	if path == "" {
		t.Skip("SEMANTIC_ENTRY_FIXTURE is not set")
	}
	program, err := loadSemanticUnitFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !semanticProgramHasFunction(program, "main") {
		t.Fatalf("fixture lost main entry; program extensions=%#v UAST extensions=%#v", program.Extensions, program.UniversalAST.Extensions)
	}
}

func TestSemanticProjectEntryFixture(t *testing.T) {
	path := os.Getenv("SEMANTIC_PROJECT_FIXTURE")
	if path == "" {
		t.Skip("SEMANTIC_PROJECT_FIXTURE is not set")
	}
	project, err := LoadSemanticProject(path, "main")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, unit := range project.Units {
		program, loadErr := loadSemanticUnitFile(unit.Path)
		if loadErr != nil {
			t.Fatal(loadErr)
		}
		if semanticProgramHasFunction(program, "main") {
			found++
		}
	}
	if found != 1 {
		t.Fatalf("project main entries=%d, want 1", found)
	}
	if err := buildSemanticProjectSummaries(project, false); err != nil {
		t.Fatal(err)
	}
	summaryEntries := 0
	for _, summary := range project.Index.Summaries {
		for _, function := range summary.Functions {
			if function.Name == "main" {
				summaryEntries++
			}
		}
	}
	if summaryEntries != 1 {
		t.Fatalf("project summary main entries=%d, want 1", summaryEntries)
	}
}

func TestSemanticBindingFixture(t *testing.T) {
	path := os.Getenv("SEMANTIC_BINDING_FIXTURE")
	if path == "" {
		t.Skip("SEMANTIC_BINDING_FIXTURE is not set")
	}
	program, err := loadSemanticUnitFile(path)
	if err != nil {
		t.Fatal(err)
	}
	u, err := canonicalUniversalAST(program)
	if err != nil {
		t.Fatal(err)
	}
	g, err := newUASTExecutionGraph(u)
	if err != nil {
		t.Fatal(err)
	}
	for id, common := range g.common {
		if id >= 1 && id <= 61 {
			t.Logf("range node=%d kind=%s name=%s children=%v", id, common.Kind, common.Name, g.children[id])
		}
		if common.Name == "inputs" {
			t.Logf("node=%d kind=%s binding=%s children=%v", id, common.Kind, common.Operation.FunctionBinding, g.children[id])
		}
		if common.Kind == "function" {
			t.Logf("function=%d name=%s binding=%s children=%v", id, common.Name, common.Operation.FunctionBinding, g.children[id])
		}
		if common.Kind == "function" || common.Kind == "block" {
			items := g.many(id, "statement")
			if len(items) > 0 {
				t.Logf("container=%d kind=%s name=%s statements=%v", id, common.Kind, common.Name, items)
			}
		}
	}
}
