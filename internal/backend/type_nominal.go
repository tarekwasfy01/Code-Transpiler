// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"fmt"
	"sort"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// Identity columns group views of the same nominal declaration/instantiation.
// Resolution links a finite reference to all captured definition views of that
// identity. It is not structural equivalence, assignability or alias erasure.
type SemanticNominalRelations struct {
	Identities  []string              `json:"identities"`
	Definitions matrixir.SparseMatrix `json:"definitions"`
	References  matrixir.SparseMatrix `json:"references"`
	Resolution  matrixir.SparseMatrix `json:"resolution"`
	Unresolved  []int                 `json:"unresolved"`
}

func deriveNominalRelations(table []SemanticTypeDefinition) (*SemanticNominalRelations, error) {
	set := map[string]bool{}
	for _, entry := range table {
		t := entry.Type
		if t.Reference && t.Identity == "" {
			return nil, fmt.Errorf("type reference %d has no identity", entry.ID)
		}
		// A frontend may retain instantiated shape details on a reference
		// (notably for isolated Go files).  Nominal resolution is identity-only;
		// those details belong to the structural type plane and must not turn a
		// valid reference into a rejected definition.
		if t.Identity != "" {
			set[t.Identity] = true
		}
	}
	r := &SemanticNominalRelations{Identities: []string{}, Unresolved: make([]int, len(table))}
	for id := range set {
		r.Identities = append(r.Identities, id)
	}
	sort.Strings(r.Identities)
	ids := map[string]int{}
	for i, id := range r.Identities {
		ids[id] = i
	}
	r.Definitions = matrixir.NewSparseMatrix(len(table), len(ids))
	r.References = matrixir.NewSparseMatrix(len(table), len(ids))
	for i, e := range table {
		if e.Type.Identity == "" {
			continue
		}
		if e.Type.Reference {
			r.References.Set(i, ids[e.Type.Identity], 1)
		} else {
			r.Definitions.Set(i, ids[e.Type.Identity], 1)
		}
	}
	var err error
	r.Resolution, err = r.References.Multiply(r.Definitions.Transpose())
	if err != nil {
		return nil, err
	}
	resolved := make([]bool, len(table))
	r.Resolution.Each(func(row, col int, value float64) { resolved[row] = true })
	for i, e := range table {
		if e.Type.Reference && !resolved[i] {
			r.Unresolved[i] = 1
		}
	}
	return r, nil
}
