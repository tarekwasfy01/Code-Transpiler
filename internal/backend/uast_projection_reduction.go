package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectionPrimitiveReduction is the proof-oriented residual reduction for
// the renderer primitive basis. It is a registry calculation, not an IR.
type ProjectionPrimitiveReduction struct {
	Before            int
	Primitives        []ProjectionObligationPrimitive
	DuplicatesRemoved int
	Compositions      map[string][]string
	ExistingRenderers map[string][]string
	TargetSpecOnly    map[string]bool
	Rewrite           map[string][]string
	Helpers           map[string][]string
	Runtime           map[string][]string
	Irreducible       []string
}

func primitiveObligationSet(p ProjectionObligationPrimitive) map[string]bool {
	set := map[string]bool{}
	for _, obligation := range p.Obligations {
		set[obligation] = true
	}
	return set
}

func primitiveExtentSet(p ProjectionObligationPrimitive) map[string]bool {
	set := map[string]bool{}
	for _, archetype := range p.Archetypes {
		set[archetype] = true
	}
	return set
}

func setContains(left, right map[string]bool) bool { return containsAll(left, right) }

func sameSet(left, right map[string]bool) bool {
	return len(left) == len(right) && setContains(left, right)
}

func strictSubset(left, right map[string]bool) bool {
	return len(left) < len(right) && setContains(right, left)
}

// exactPrimitiveCover returns a smallest union of candidate primitive vectors
// equal to required. Every candidate is already a subset, so boolean union
// cannot add a false obligation. The backtracker is practical at 28 rows and
// deliberately replaces a greedy approximation.
func exactPrimitiveCover(required map[string]bool, candidates []ProjectionObligationPrimitive) ([]string, bool) {
	byObligation := map[string][]int{}
	sets := make([]map[string]bool, len(candidates))
	for index, candidate := range candidates {
		sets[index] = primitiveObligationSet(candidate)
		for obligation := range sets[index] {
			byObligation[obligation] = append(byObligation[obligation], index)
		}
	}
	for obligation := range required {
		if len(byObligation[obligation]) == 0 {
			return nil, false
		}
	}
	best := []int(nil)
	var search func(map[string]bool, []int)
	search = func(covered map[string]bool, selected []int) {
		if best != nil && len(selected) >= len(best) {
			return
		}
		missing := ""
		choices := []int(nil)
		for obligation := range required {
			if covered[obligation] {
				continue
			}
			local := []int{}
			for _, index := range byObligation[obligation] {
				adds := false
				for value := range sets[index] {
					if !covered[value] {
						adds = true
						break
					}
				}
				if adds {
					local = append(local, index)
				}
			}
			if missing == "" || len(local) < len(choices) {
				missing, choices = obligation, local
			}
		}
		if missing == "" {
			best = append([]int(nil), selected...)
			return
		}
		if len(choices) == 0 {
			return
		}
		sort.Slice(choices, func(i, j int) bool {
			return len(sets[choices[i]]) > len(sets[choices[j]])
		})
		for _, index := range choices {
			next := map[string]bool{}
			for value := range covered {
				next[value] = true
			}
			for value := range sets[index] {
				next[value] = true
			}
			search(next, append(selected, index))
		}
	}
	search(map[string]bool{}, nil)
	if best == nil {
		return nil, false
	}
	result := make([]string, 0, len(best))
	for _, index := range best {
		result = append(result, candidates[index].ID)
	}
	sort.Strings(result)
	return result, true
}

func canonicalPrimitiveBasis(analysis ProjectionObligationAnalysis) ([]ProjectionObligationPrimitive, int) {
	byVector := map[string]*ProjectionObligationPrimitive{}
	for _, primitive := range analysis.Primitives {
		key := strings.Join(sortedUnique(primitive.Obligations), ";")
		current := byVector[key]
		if current == nil {
			copy := ProjectionObligationPrimitive{Archetypes: append([]string(nil), primitive.Archetypes...), Obligations: sortedUnique(primitive.Obligations)}
			byVector[key] = &copy
			continue
		}
		current.Archetypes = sortedUnique(append(current.Archetypes, primitive.Archetypes...))
	}
	out := make([]ProjectionObligationPrimitive, 0, len(byVector))
	for _, primitive := range byVector {
		out = append(out, *primitive)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i].Obligations, ";") < strings.Join(out[j].Obligations, ";")
	})
	for index := range out {
		out[index].ID = fmt.Sprintf("PRIM_%03d", index+1)
	}
	return out, len(analysis.Primitives) - len(out)
}

func primitiveTargetSpecOnly(p ProjectionObligationPrimitive) bool {
	for _, obligation := range p.Obligations {
		// TargetSpec supplies presentation parameters only. Child slots,
		// relation roles and field roles require a syntax combinator and are
		// therefore not data-only, even when the primitive itself has no
		// category token after factorization.
		if !strings.HasPrefix(obligation, "precedence=") && !strings.HasPrefix(obligation, "block=") && !strings.HasPrefix(obligation, "terminator=") {
			return false
		}
	}
	return len(p.Obligations) != 0
}

// UniversalProjectionPrimitiveReduction applies duplicate elimination, exact
// primitive composition and every currently registered positive path. A
// legacy renderer is never a positive match: it may appear in audit reports,
// but only direct UAST renderer bindings can cover a primitive.
func UniversalProjectionPrimitiveReduction() (ProjectionPrimitiveReduction, error) {
	analysis, err := UniversalProjectionObligationAnalysis()
	if err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	basis, duplicates := canonicalPrimitiveBasis(analysis)
	reduction := ProjectionPrimitiveReduction{
		Before: len(analysis.Primitives), Primitives: basis, DuplicatesRemoved: duplicates,
		Compositions: map[string][]string{}, ExistingRenderers: map[string][]string{}, TargetSpecOnly: map[string]bool{},
		Rewrite: map[string][]string{}, Helpers: map[string][]string{}, Runtime: map[string][]string{},
	}
	for _, primitive := range basis {
		required, extent := primitiveObligationSet(primitive), primitiveExtentSet(primitive)
		candidates := []ProjectionObligationPrimitive{}
		for _, other := range basis {
			if other.ID == primitive.ID {
				continue
			}
			otherSet := primitiveObligationSet(other)
			if strictSubset(otherSet, required) && setContains(primitiveExtentSet(other), extent) {
				candidates = append(candidates, other)
			}
		}
		if composition, ok := exactPrimitiveCover(required, candidates); ok {
			reduction.Compositions[primitive.ID] = composition
		}
		reduction.TargetSpecOnly[primitive.ID] = primitiveTargetSpecOnly(primitive)
	}
	// All renderer hits must come from direct UAST bindings. The current
	// generated binding table has no residual primitive binding; still build
	// the full M_EXISTING_RENDERER_PRIMITIVE table in Write... below.
	for _, primitive := range basis {
		reduction.ExistingRenderers[primitive.ID] = []string{}
	}
	for _, primitive := range basis {
		if len(reduction.Compositions[primitive.ID]) == 0 && len(reduction.ExistingRenderers[primitive.ID]) == 0 && !reduction.TargetSpecOnly[primitive.ID] && len(reduction.Rewrite[primitive.ID]) == 0 && len(reduction.Helpers[primitive.ID]) == 0 && len(reduction.Runtime[primitive.ID]) == 0 {
			reduction.Irreducible = append(reduction.Irreducible, primitive.ID)
		}
	}
	sort.Strings(reduction.Irreducible)
	return reduction, nil
}

func validatePrimitiveCompositionGraph(compositions map[string][]string) error {
	state := map[string]int{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("CYCLIC_PRIMITIVE_COMPOSITION: %s", id)
		}
		if state[id] == 2 {
			return nil
		}
		state[id] = 1
		for _, dependency := range compositions[id] {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = 2
		return nil
	}
	for id := range compositions {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func WriteProjectionPrimitiveReduction(dir string) (ProjectionPrimitiveReduction, error) {
	reduction, err := UniversalProjectionPrimitiveReduction()
	if err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	if err := validatePrimitiveCompositionGraph(reduction.Compositions); err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	rows, edges, existing, helpers, runtimes, status, irreducible := [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}
	catalog, err := HarvestProjectionRenderers()
	if err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	for _, primitive := range reduction.Primitives {
		rows = append(rows, []string{primitive.ID, strings.Join(primitive.Archetypes, ";"), strings.Join(primitive.Obligations, ";")})
		for _, dependency := range reduction.Compositions[primitive.ID] {
			edges = append(edges, []string{primitive.ID, dependency, "COMPOSABLE_FROM_PRIMITIVES"})
		}
		for _, renderer := range catalog {
			covered := false
			for _, match := range reduction.ExistingRenderers[primitive.ID] {
				if match == renderer.RendererID {
					covered = true
					break
				}
			}
			existing = append(existing, []string{renderer.RendererID, primitive.ID, fmt.Sprintf("%t", covered)})
		}
		helpers = append(helpers, []string{"registered_helpers", primitive.ID, "false"})
		runtimes = append(runtimes, []string{"uast.core", primitive.ID, "false"})
		classification := "GENUINELY_MISSING_RENDERER_PRIMITIVE"
		if len(reduction.Compositions[primitive.ID]) != 0 {
			classification = "COMPOSABLE_FROM_PRIMITIVES"
		}
		if reduction.TargetSpecOnly[primitive.ID] {
			classification = "TARGETSPEC_DATA_ONLY"
		}
		if len(reduction.ExistingRenderers[primitive.ID]) != 0 {
			classification = "COVERED_BY_EXISTING_RENDERER"
		}
		status = append(status, []string{primitive.ID, classification})
	}
	for _, primitive := range reduction.Irreducible {
		irreducible = append(irreducible, []string{primitive})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "primitive_atomic_obligation_matrix.csv"), []string{"primitive_id", "archetypes", "obligations"}, rows); err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "primitive_composition_graph.csv"), []string{"primitive_id", "subprimitive_id", "kind"}, edges); err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "existing_renderer_primitive_matrix.csv"), []string{"renderer_id", "primitive_id", "compatible"}, existing); err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "existing_helper_primitive_matrix.csv"), []string{"helper_id", "primitive_id", "compatible"}, helpers); err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "existing_runtime_primitive_matrix.csv"), []string{"runtime_capability", "primitive_id", "compatible"}, runtimes); err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "primitive_reduction_status.csv"), []string{"primitive_id", "classification"}, status); err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "irreducible_missing_primitives.csv"), []string{"primitive_id"}, irreducible); err != nil {
		return ProjectionPrimitiveReduction{}, err
	}
	return reduction, nil
}
