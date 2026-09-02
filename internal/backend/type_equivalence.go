package backend

import (
	"encoding/json"
	"fmt"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// SemanticTypeEquivalence proves exact type-domain equality only. It is not
// implicit conversion, language-specific assignability, subtyping or ABI equality.
// Unknown marks types whose identity/shape is insufficient; zero matrix cells
// involving such types must not be interpreted as a proof of incompatibility.
type SemanticTypeEquivalence struct {
	Equivalent matrixir.SparseMatrix `json:"equivalent"`
	Unknown    []int                 `json:"unknown"`
	Rounds     int                   `json:"refinement_rounds"`
}

// deriveTypeEquivalence uses alias/reference resolution followed by greatest
// fixed-point refinement of a structural candidate matrix. Cyclic structural
// types can match; ungrounded alias cycles cannot manufacture a proof.
func deriveTypeEquivalence(table []SemanticTypeDefinition) (*SemanticTypeEquivalence, error) {
	n := len(table)
	r := &SemanticTypeEquivalence{Equivalent: matrixir.NewSparseMatrix(n, n), Unknown: make([]int, n)}
	keys := map[string]int{}
	definitions := map[string][]int{}
	for i, e := range table {
		if e.ID != i {
			return nil, fmt.Errorf("noncanonical type table")
		}
		data, err := json.Marshal(e.Type)
		if err != nil {
			return nil, err
		}
		keys[string(data)] = i
		if e.Type.Identity != "" && !e.Type.Reference {
			definitions[e.Type.Identity] = append(definitions[e.Type.Identity], i)
		}
	}
	idOf := func(t *SemanticType) (int, error) {
		data, err := json.Marshal(t)
		if err != nil {
			return 0, err
		}
		id, ok := keys[string(data)]
		if !ok {
			return 0, fmt.Errorf("type child absent from table")
		}
		return id, nil
	}
	resolved := make([]int, n)
	for start := range table {
		seen := map[int]bool{}
		at := start
		for {
			if seen[at] {
				at = -1
				break
			}
			seen[at] = true
			t := table[at].Type
			if t.Reference {
				residual := t
				residual.Kind = ""
				residual.Name = ""
				residual.Identity = ""
				residual.Reference = false
				residual.TypeOrigin = ""
				encoded, err := json.Marshal(residual)
				if err != nil {
					return nil, err
				}
				if string(encoded) != "{}" {
					at = -1
					break
				}
				views := definitions[t.Identity]
				// Multiple definition views require a separate identity consistency
				// proof; do not arbitrarily pick whichever appeared first.
				if len(views) != 1 {
					at = -1
					break
				}
				at = views[0]
				continue
			}
			if t.Kind == "alias" {
				// An alias with semantic qualifiers is not automatically transparent.
				residual := t
				residual.Kind = ""
				residual.Name = ""
				residual.Identity = ""
				residual.TypeOrigin = ""
				residual.Element = nil
				residual.TypeParameters = nil
				residual.TypeArguments = nil
				encoded, err := json.Marshal(residual)
				if err != nil {
					return nil, err
				}
				if string(encoded) != "{}" {
					at = -1
					break
				}
				if t.Element == nil {
					at = -1
					break
				}
				at, err = idOf(t.Element)
				if err != nil {
					return nil, err
				}
				continue
			}
			break
		}
		resolved[start] = at
	}
	shapes := make([]string, n)
	children := make([][]int, n)
	known := map[string]bool{}
	for _, kind := range []string{"integer", "arbitrary_integer", "float", "floating", "complex", "boolean", "string", "bytes", "unit", "null", "never", "tuple", "function", "array", "slice", "vector", "map", "pointer", "reference", "struct", "named"} {
		known[kind] = true
	}
	for i := range table {
		at := resolved[i]
		if at < 0 {
			r.Unknown[i] = 1
			continue
		}
		t := table[at].Type
		valid := known[t.Kind]
		for _, child := range []*SemanticType{t.Element, t.Key, t.Value, t.Result, t.Constraint} {
			if child != nil && child.Kind == "" {
				valid = false
			}
		}
		for _, list := range [][]SemanticType{t.Parameters, t.TypeParameters, t.TypeArguments, t.Embedded} {
			for _, child := range list {
				if child.Kind == "" {
					valid = false
				}
			}
		}
		for _, list := range [][]SemanticField{t.Fields, t.Methods} {
			for _, field := range list {
				if field.Type.Kind == "" {
					valid = false
				}
			}
		}
		for _, term := range t.Terms {
			if term.Type.Kind == "" {
				valid = false
			}
		}
		switch t.Kind {
		case "integer":
			valid = valid && t.Bits > 0 && t.Signed != nil
		case "float", "floating", "complex":
			valid = valid && t.Bits > 0
		case "array", "slice", "vector", "pointer", "reference":
			valid = valid && t.Element != nil
		case "map":
			valid = valid && t.Key != nil && t.Value != nil
		case "function":
			valid = valid && t.Result != nil
		case "named":
			valid = valid && t.Identity != "" && t.Element != nil
		}
		if !valid {
			r.Unknown[i] = 1
			continue
		}
		edges := semanticTypeChildren(&t)
		roles := make([]SemanticTypeEdge, 0, len(edges))
		for _, c := range edges {
			id, err := idOf(c.Type)
			if err != nil {
				return nil, err
			}
			children[i] = append(children[i], id)
			roles = append(roles, c.SemanticTypeEdge)
		}
		// Source spelling and provenance are not structural type properties.
		t.Name = ""
		t.TypeOrigin = ""
		t.Element = nil
		t.Key = nil
		t.Value = nil
		t.Result = nil
		t.Constraint = nil
		t.Parameters = nil
		t.TypeParameters = nil
		t.TypeArguments = nil
		t.Embedded = nil
		t.Fields = nil
		t.Methods = nil
		t.Terms = nil
		if t.Kind == "floating" {
			t.Kind = "float"
		}
		data, err := json.Marshal(struct {
			Type  SemanticType
			Roles []SemanticTypeEdge
		}{t, roles})
		if err != nil {
			return nil, err
		}
		shapes[i] = string(data)
	}
	// Unknown child domains propagate through the structural dependency matrix.
	dependency := matrixir.NewSparseMatrix(n, n)
	for i := range children {
		for _, c := range children[i] {
			dependency.Set(i, c, 1)
		}
	}
	for changed := true; changed; {
		changed = false
		unknown := matrixir.NewSparseMatrix(n, 1)
		for i, v := range r.Unknown {
			if v != 0 {
				unknown.Set(i, 0, 1)
			}
		}
		propagated, err := dependency.Multiply(unknown)
		if err != nil {
			return nil, err
		}
		propagated.Each(func(i, j int, v float64) {
			if r.Unknown[i] == 0 {
				r.Unknown[i] = 1
				changed = true
			}
		})
	}
	buckets := map[string][]int{}
	for i, shape := range shapes {
		if r.Unknown[i] == 0 {
			buckets[shape] = append(buckets[shape], i)
		}
	}
	for _, ids := range buckets {
		for _, i := range ids {
			for _, j := range ids {
				r.Equivalent.Set(i, j, 1)
			}
		}
	}
	for {
		var remove [][2]int
		r.Equivalent.Each(func(i, j int, v float64) {
			for k, c := range children[i] {
				if r.Equivalent.At(c, children[j][k]) == 0 {
					remove = append(remove, [2]int{i, j})
					break
				}
			}
		})
		r.Rounds++
		if len(remove) == 0 {
			break
		}
		for _, pair := range remove {
			r.Equivalent.Set(pair[0], pair[1], 0)
		}
	}
	return r, nil
}
