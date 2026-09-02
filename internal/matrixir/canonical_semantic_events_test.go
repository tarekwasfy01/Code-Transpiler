package matrixir

import "testing"

func TestCanonicalSemanticEventsAreDeterministicTypedFacts(t *testing.T) {
	program, err := Canonicalize("python", "x = 1\nprint(x)\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(program.SemanticEvents) != len(program.Nodes) {
		t.Fatalf("typed events=%d nodes=%d", len(program.SemanticEvents), len(program.Nodes))
	}
	for id, event := range program.SemanticEvents {
		if event.ID != id || event.Action == "" || event.StructureKind == "" || len(event.Semantic) != SemanticDimensions {
			t.Fatalf("invalid typed event %+v", event)
		}
	}
}

func TestAnalyzeSemanticExpressionPreservesPrecedenceAndRoles(t *testing.T) {
	events, err := AnalyzeSemanticExpression("python", "f(a, g(b, c))[i] + d * e")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) == 0 || events[len(events)-1].StructureKind != "binary" {
		t.Fatalf("missing binary root: %+v", events)
	}
	seenCall, seenIndex := false, false
	for _, event := range events {
		if event.StructureKind == "call" && len(event.Roles) == 3 {
			seenCall = true
		}
		if event.StructureKind == "index" && len(event.Roles) == 2 {
			seenIndex = true
		}
	}
	if !seenCall || !seenIndex {
		t.Fatalf("call=%v index=%v", seenCall, seenIndex)
	}
}
