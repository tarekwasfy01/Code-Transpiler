package backend

import (
	"encoding/json"
	"testing"
)

func TestSemanticIntegrityMutationMatrix(t *testing.T) {
	mutations := map[string]func(*SemanticDocument){
		"node_id": func(d *SemanticDocument) { d.Root.ID = 99 },
		"scope":   func(d *SemanticDocument) { d.Root.Scope = 99 },
		"explicit_type": func(d *SemanticDocument) {
			d.Root.Statements[0].Expression.Type = SemanticType{Kind: "integer", Bits: 64, TypeOrigin: "explicit"}
		},
		"operation":      func(d *SemanticDocument) { d.Root.Statements[0].Expression.Semantics.Operation = "integer.multiply" },
		"binding":        func(d *SemanticDocument) { n := 999; d.Root.Statements[1].Expression.Arguments[0].Value.Binding = &n },
		"effect":         func(d *SemanticDocument) { d.Root.Effects = []string{"ffi"} },
		"source_span":    func(d *SemanticDocument) { d.Root.Source = &SemanticSourceSpan{File: "original.go", StartOffset: -1} },
		"node_extension": func(d *SemanticDocument) { d.Root.Extensions = map[string]any{"future": true} },
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			p, err := ParseSemantic("r", "x <- 1 + 2; print(x)")
			if err != nil {
				t.Fatal(err)
			}
			d, err := p.Document()
			if err != nil {
				t.Fatal(err)
			}
			mutate(&d)
			if _, err = ParseSemanticDocument(d); err == nil {
				t.Fatal("silently accepted changed or discarded semantic information")
			}
		})
	}
}

func TestSemanticJSONUnknownFields(t *testing.T) {
	p, _ := ParseSemantic("r", "print(1)")
	data, _ := p.MarshalSemanticJSON()
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	raw["future_semantics"] = true
	changed, _ := json.Marshal(raw)
	if _, err := ParseSemanticJSON(changed); err == nil {
		t.Fatal("unknown field discarded")
	}
	if _, err := ParseSemanticJSON(append(data, []byte(" {}")...)); err == nil {
		t.Fatal("second document accepted")
	}
}

func TestRequiredCapabilityTargetMatrix(t *testing.T) {
	for _, target := range Languages {
		t.Run(target.ID, func(t *testing.T) {
			p, _ := ParseSemantic("r", "print(1)")
			p.UniversalAST.Contracts.Requires = []string{"native.uint64.exact"}
			if _, err := EmitSemantic(target.ID, p); err == nil {
				t.Fatal("unsupported requirement ignored")
			}
			// The preceding emission established UAST as canonical. Changing its
			// detached legacy view must not remove the requirement.
			p.Contracts.Requires = []string{"core"}
			if _, err := EmitSemantic(target.ID, p); err == nil {
				t.Fatal("legacy contract mutation overwrote canonical UAST")
			}
			q, _ := ParseSemantic("r", "print(1)")
			q.Contracts.Requires = []string{"core"}
			if _, err := EmitSemantic(target.ID, q); err != nil {
				t.Fatal(err)
			}
		})
	}
}
