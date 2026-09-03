package matrixir

import "testing"

func TestCanonicalSemanticEventsAreDeterministicTypedFacts(t *testing.T) {
	program, err := Canonicalize("python", "x = 1\nprint(x)\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(program.SemanticEvents) < len(program.Nodes) {
		t.Fatalf("typed events=%d nodes=%d", len(program.SemanticEvents), len(program.Nodes))
	}
	seen := map[int]bool{}
	for _, event := range program.SemanticEvents {
		if seen[event.ID] || event.Action == "" || event.StructureKind == "" || len(event.Semantic) != SemanticDimensions {
			t.Fatalf("invalid typed event %+v", event)
		}
		seen[event.ID] = true
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

func TestStructuredFactFamiliesUseGrammarTokens(t *testing.T) {
	cases := []struct {
		language, source string
		family           ParsedConstructFamily
		roles            []string
	}{
		{"python", "[x * 2 for x in xs]", ParsedIteration, []string{"binding", "iterable", "produced"}},
		{"python", "lambda x: x * 2", ParsedClosure, []string{"parameter", "return"}},
		{"python", "xs[1:8:2]", ParsedIndexSlice, []string{"base", "start", "end", "step"}},
		{"go", "[]float64{1,2,3}", ParsedContainer, []string{"element"}},
		{"go", "for i, v := range x {\n out[i] = v*2\n}", ParsedIteration, []string{"binding", "iterable"}},
		{"go", "func(v float64) float64 {\n return v*2\n}", ParsedClosure, []string{"parameter"}},
	}
	for _, tc := range cases {
		p, err := Canonicalize(tc.language, tc.source)
		if err != nil {
			t.Fatalf("%s: %v", tc.source, err)
		}
		found := map[string]bool{}
		for _, event := range p.SemanticEvents {
			if event.FactFamily != tc.family {
				continue
			}
			for _, role := range event.Roles {
				found[role.Role] = true
			}
		}
		for _, role := range tc.roles {
			if !found[role] {
				t.Fatalf("%s missing %s in %s", tc.source, role, tc.family)
			}
		}
	}
}

func TestSharedExpressionFamiliesRemainStructured(t *testing.T) {
	for _, tc := range []struct {
		language, source, root string
	}{
		{"rust", "(a, b, c)", "tuple"},
		{"cpp", "ready ? left : right", "binary"},
		{"julia", "ready ? left : right", "binary"},
		{"julia", "ready ?\n left :\n right", "binary"},
		{"c", "*pointer", "deref"},
		{"c", "&value", "address"},
	} {
		events, err := AnalyzeSemanticExpression(tc.language, tc.source)
		if err != nil {
			t.Fatalf("%s: %v", tc.source, err)
		}
		if len(events) == 0 || events[len(events)-1].StructureKind != tc.root {
			t.Fatalf("%s root=%+v", tc.source, events)
		}
	}
}
