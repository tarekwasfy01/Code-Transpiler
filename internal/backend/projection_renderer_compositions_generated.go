package backend

import "sort"

// ProjectionRendererComposition is the generated, syntax-only sequence for a
// renderer archetype. It is a registry entry rather than another program IR:
// nodes and semantic values are still read directly from UniversalASTDocument.
type ProjectionRendererComposition struct {
	ArchetypeID string
	Primitives  []string
}

// GeneratedProjectionRendererCompositions is derived from the exact boolean
// factorization M_AO = M_AP odot M_PO. It makes later renderer additions a
// primitive registration: all archetype compositions update at once.
func GeneratedProjectionRendererCompositions() ([]ProjectionRendererComposition, error) {
	analysis, err := UniversalProjectionObligationAnalysis()
	if err != nil {
		return nil, err
	}
	byArchetype := map[string][]string{}
	for _, primitive := range analysis.Primitives {
		for _, archetype := range primitive.Archetypes {
			byArchetype[archetype] = append(byArchetype[archetype], primitive.ID)
		}
	}
	out := make([]ProjectionRendererComposition, 0, len(analysis.Archetypes))
	for _, archetype := range analysis.Archetypes {
		primitives := sortedUnique(byArchetype[archetype])
		out = append(out, ProjectionRendererComposition{ArchetypeID: archetype, Primitives: primitives})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ArchetypeID < out[j].ArchetypeID })
	return out, nil
}
