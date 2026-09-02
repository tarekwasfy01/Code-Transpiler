package backend

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// SemanticTypeRelations is a lossless incidence projection of type uses and
// direct structural edges. Edge rows preserve order and repeated children.
// The matrices do not assert assignability or executable backend support.
type SemanticTypeRelations struct {
	Occurrences []string                  `json:"occurrences"`
	Uses        matrixir.SparseMatrix     `json:"uses"`
	Edges       []SemanticTypeEdge        `json:"edges"`
	Parents     matrixir.SparseMatrix     `json:"parents"`
	Children    matrixir.SparseMatrix     `json:"children"`
	UsageCounts []int                     `json:"usage_counts"`
	Nominal     *SemanticNominalRelations `json:"nominal,omitempty"`
	Equivalence *SemanticTypeEquivalence  `json:"equivalence,omitempty"`
}

type SemanticTypeEdge struct {
	Role       string `json:"role"`
	Index      int    `json:"index"`
	Name       string `json:"name,omitempty"`
	Underlying bool   `json:"underlying,omitempty"`
}

type semanticTypeChild struct {
	SemanticTypeEdge
	Type *SemanticType
}

// Single child vocabulary shared by the structural table and relation planes.
func semanticTypeChildren(t *SemanticType) []semanticTypeChild {
	var out []semanticTypeChild
	add := func(role string, index int, name string, underlying bool, child *SemanticType) {
		if child != nil && child.Kind != "" {
			out = append(out, semanticTypeChild{SemanticTypeEdge{role, index, name, underlying}, child})
		}
	}
	for _, c := range []struct {
		role  string
		child *SemanticType
	}{
		{"element", t.Element}, {"key", t.Key}, {"value", t.Value}, {"result", t.Result}, {"constraint", t.Constraint},
	} {
		add(c.role, 0, "", false, c.child)
	}
	for _, c := range []struct {
		role string
		list []SemanticType
	}{
		{"parameter", t.Parameters}, {"type_parameter", t.TypeParameters}, {"type_argument", t.TypeArguments}, {"embedded", t.Embedded},
	} {
		for i := range c.list {
			add(c.role, i, "", false, &c.list[i])
		}
	}
	for i := range t.Fields {
		add("field", i, t.Fields[i].Name, false, &t.Fields[i].Type)
	}
	for i := range t.Methods {
		add("method", i, t.Methods[i].Name, false, &t.Methods[i].Type)
	}
	for i := range t.Terms {
		add("term", i, "", t.Terms[i].Underlying, &t.Terms[i].Type)
	}
	return out
}

func deriveTypeRelations(root *SemanticStatement, table []SemanticTypeDefinition) (*SemanticTypeRelations, error) {
	// Also protects callers that construct cyclic HIR directly through the API.
	if _, err := json.Marshal(root); err != nil {
		return nil, err
	}
	ids := make(map[string]int, len(table))
	for i, entry := range table {
		if entry.ID != i {
			return nil, fmt.Errorf("noncanonical type ID %d", entry.ID)
		}
		key, err := json.Marshal(entry.Type)
		if err != nil {
			return nil, err
		}
		ids[string(key)] = i
	}
	idOf := func(t SemanticType) (int, error) {
		key, err := json.Marshal(t)
		if err != nil {
			return 0, err
		}
		id, ok := ids[string(key)]
		if !ok {
			return 0, fmt.Errorf("type occurrence absent from type table")
		}
		return id, nil
	}
	out := &SemanticTypeRelations{Occurrences: []string{}, Edges: []SemanticTypeEdge{}, UsageCounts: make([]int, len(table))}
	var useIDs []int
	// Walk only typed struct fields and slices, never arbitrary Attributes or
	// Extensions maps. Stop at each type: its internals belong to edge planes.
	var visit func(reflect.Value, string) error
	typeOfType := reflect.TypeOf(SemanticType{})
	visit = func(v reflect.Value, path string) error {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				return nil
			}
			return visit(v.Elem(), path)
		}
		if v.Type() == typeOfType {
			t := v.Interface().(SemanticType)
			if t.Kind == "" {
				return nil
			}
			id, err := idOf(t)
			if err != nil {
				return err
			}
			out.Occurrences = append(out.Occurrences, path)
			useIDs = append(useIDs, id)
			return nil
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				field := v.Type().Field(i)
				name := strings.Split(field.Tag.Get("json"), ",")[0]
				if name == "" || name == "-" || !v.Field(i).CanInterface() {
					continue
				}
				if err := visit(v.Field(i), path+"/"+name); err != nil {
					return err
				}
			}
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				if err := visit(v.Index(i), path+"/"+strconv.Itoa(i)); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if root != nil {
		if err := visit(reflect.ValueOf(root), "/root"); err != nil {
			return nil, err
		}
	}
	out.Uses = matrixir.NewSparseMatrix(len(useIDs), len(table))
	for row, id := range useIDs {
		out.Uses.Set(row, id, 1)
	}
	// U^T * 1 yields the usage vector; each occurrence contributes once.
	ones := matrixir.NewSparseMatrix(len(useIDs), 1)
	for i := range useIDs {
		ones.Set(i, 0, 1)
	}
	counts, err := out.Uses.Transpose().Multiply(ones)
	if err != nil {
		return nil, err
	}
	for i := range out.UsageCounts {
		out.UsageCounts[i] = int(counts.At(i, 0))
	}
	var parents, children []int
	for i := range table {
		for _, child := range semanticTypeChildren(&table[i].Type) {
			id, err := idOf(*child.Type)
			if err != nil {
				return nil, err
			}
			out.Edges = append(out.Edges, child.SemanticTypeEdge)
			parents = append(parents, i)
			children = append(children, id)
		}
	}
	out.Parents = matrixir.NewSparseMatrix(len(parents), len(table))
	out.Children = matrixir.NewSparseMatrix(len(children), len(table))
	for row := range parents {
		out.Parents.Set(row, parents[row], 1)
		out.Children.Set(row, children[row], 1)
	}
	out.Nominal, err = deriveNominalRelations(table)
	if err != nil {
		return nil, err
	}
	out.Equivalence, err = deriveTypeEquivalence(table)
	if err != nil {
		return nil, err
	}
	return out, nil
}
