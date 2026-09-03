package backend

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestTypeRelationIncidencePreservesRolesAndMultiplicity(t *testing.T) {
	integer := SemanticType{Kind: "integer", Bits: 32}
	fn := SemanticType{Kind: "function", Parameters: []SemanticType{integer, integer}, Result: &integer}
	root := &SemanticStatement{Type: fn, Expression: &SemanticExpression{Type: integer,
		Attributes: map[string]any{"type": SemanticType{Kind: "fake"}},
		Function:   &SemanticFunction{Parameters: []SemanticParameter{{Type: integer, Default: &SemanticExpression{Type: integer}}}},
	}}
	table, graph, err := deriveTypeTable(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := deriveTypeRelations(root, table)
	if err != nil {
		t.Fatal(err)
	}
	if len(table) != 2 || len(r.Edges) != 3 || len(r.Occurrences) != 4 {
		t.Fatalf("unexpected dimensions: %+v", r)
	}
	wantPaths := []string{"/root/type", "/root/expression/type", "/root/expression/function/parameters/0/type", "/root/expression/function/parameters/0/default/type"}
	if !reflect.DeepEqual(r.Occurrences, wantPaths) {
		t.Fatalf("occurrences=%v", r.Occurrences)
	}
	if r.Edges[0].Role != "result" || r.Edges[1].Role != "parameter" || r.Edges[1].Index != 0 || r.Edges[2].Index != 1 {
		t.Fatalf("lost roles: %v", r.Edges)
	}
	var fnID, intID int
	for i, e := range table {
		if e.Type.Kind == "function" {
			fnID = i
		} else {
			intID = i
		}
	}
	if r.UsageCounts[fnID] != 1 || r.UsageCounts[intID] != 3 {
		t.Fatalf("usage vector=%v", r.UsageCounts)
	}
	product, err := r.Parents.Transpose().Multiply(r.Children)
	if err != nil {
		t.Fatal(err)
	}
	if product.At(fnID, intID) != 3 || product.NonZeros() != 1 || graph.At(fnID, intID) != 1 {
		t.Fatal("incidence product must preserve 3 distinct edges")
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var restored SemanticTypeRelations
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(r, &restored) {
		t.Fatal("relation JSON roundtrip changed projection")
	}
}

func TestTypeRelationTamperingRejected(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{&AssignStmt{Name: "x", Op: "<-", Value: &LiteralExpr{Kind: "number", Text: "1"}}}}, "eager_left_to_right")
	for _, name := range []string{"occurrence", "uses", "counts", "edge", "parents", "children", "nominal", "equivalence"} {
		t.Run(name, func(t *testing.T) {
			doc, err := p.Document()
			if err != nil {
				t.Fatal(err)
			}
			r := doc.TypeRelations
			switch name {
			case "occurrence":
				r.Occurrences[0] = "/forged"
			case "uses":
				r.Uses.Set(0, 0, 2)
			case "counts":
				r.UsageCounts[0]++
			case "edge":
				r.Edges = append(r.Edges, SemanticTypeEdge{Role: "fake"})
			case "parents":
				r.Parents.Rows++
			case "children":
				r.Children.Cols++
			case "nominal":
				r.Nominal.Unresolved[0]++
			case "equivalence":
				r.Equivalence.Unknown[0]++
			}
			if _, err := ParseSemanticDocument(doc); err == nil {
				t.Fatal("tampering accepted")
			}
		})
	}
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	var restored SemanticDocument
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if _, err := ParseSemanticDocument(restored); err != nil {
		t.Fatal(err)
	}
	doc.TypeRelations = nil
	doc.UniversalAST = nil // explicit legacy table-only import
	if _, err := ParseSemanticDocument(doc); err != nil {
		t.Fatalf("legacy table-only input: %v", err)
	}
}

func TestTypeRelationAllStructuralRoles(t *testing.T) {
	leaf := SemanticType{Kind: "string"}
	root := &SemanticStatement{Type: SemanticType{
		Kind: "test_composite", Element: &leaf, Key: &leaf, Value: &leaf, Result: &leaf, Constraint: &leaf,
		Parameters: []SemanticType{leaf}, TypeParameters: []SemanticType{leaf}, TypeArguments: []SemanticType{leaf}, Embedded: []SemanticType{leaf},
		Fields: []SemanticField{{Name: "field_name", Type: leaf}}, Methods: []SemanticField{{Name: "method_name", Type: leaf}},
		Terms: []SemanticTypeTerm{{Type: leaf, Underlying: true}},
	}}
	table, _, err := deriveTypeTable(root)
	if err != nil {
		t.Fatal(err)
	}
	r, err := deriveTypeRelations(root, table)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"element", "key", "value", "result", "constraint", "parameter", "type_parameter", "type_argument", "embedded", "field", "method", "term"}
	if len(r.Edges) != len(want) {
		t.Fatalf("edges: %v", r.Edges)
	}
	for i, role := range want {
		if r.Edges[i].Role != role {
			t.Fatalf("edge %d: %v", i, r.Edges[i])
		}
	}
	if r.Edges[9].Name != "field_name" || r.Edges[10].Name != "method_name" || !r.Edges[11].Underlying {
		t.Fatal("edge metadata lost")
	}
}
