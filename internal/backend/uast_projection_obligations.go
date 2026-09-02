package backend

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ProjectionObligationAnalysis is a declarative factorization of the residual
// renderer contracts. It contains no AST values and is not another IR: it is
// the checked, boolean requirement table consumed by TargetSpec/renderer
// registration.
type ProjectionObligationAnalysis struct {
	Archetypes   []string
	Obligations  []string
	Primitives   []ProjectionObligationPrimitive
	Requirements map[string]map[string]bool // M_AO
}

var universalProjectionObligationOnce struct {
	sync.Once
	analysis ProjectionObligationAnalysis
	err      error
}

// ProjectionObligationPrimitive represents one formal concept (a complete
// rectangle of true cells). Its extent is M_AP and its intent is M_PO.
type ProjectionObligationPrimitive struct {
	ID          string
	Archetypes  []string
	Obligations []string
}

// ProjectionPrimitiveTargetSupport is M_TP plus the target parameters used by
// the syntax-only composition. A target is marked available only if it has a
// registered direct UAST renderer for the primitive; parameters alone never
// upgrade an unsupported semantic contract.
type ProjectionPrimitiveTargetSupport struct {
	Target     string
	Primitive  string
	Available  bool
	Mode       PreservationMode
	Parameters map[string]string
}

func obligationSetForSignature(signature string) map[string]bool {
	set := map[string]bool{}
	for _, contract := range strings.Split(signature, "||") {
		parts := strings.Split(contract, "|")
		if len(parts) != 7 {
			continue
		}
		// A renderer primitive is reusable across all contracts in one
		// archetype. Contract order determines application order at emission
		// time; it must not manufacture a second primitive for the same atomic
		// operation. The set is therefore the exact union of obligations.
		prefix := ""
		set[prefix+"category="+parts[0]] = true
		for _, child := range strings.Split(parts[1], ";") {
			if child != "" {
				set[prefix+"child="+child] = true
			}
		}
		for _, role := range strings.Split(parts[2], ";") {
			if role != "" {
				set[prefix+"field_role="+role] = true
			}
		}
		for _, role := range strings.Split(parts[3], ";") {
			if role != "" {
				set[prefix+"relation_role="+role] = true
			}
		}
		set[prefix+"precedence="+parts[4]] = true
		set[prefix+"block="+parts[5]] = true
		set[prefix+"terminator="+parts[6]] = true
	}
	return set
}

func setKeys(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func setSignature(values map[string]bool) string { return strings.Join(setKeys(values), ";") }

func containsAll(have, need map[string]bool) bool {
	for value := range need {
		if !have[value] {
			return false
		}
	}
	return true
}

// minimumPrimitiveCover computes the smallest exact rectangle cover of the
// boolean obligation matrix.  Each rectangle is a true-only formal concept
// (or the deterministic singleton fallback for one row), so the boolean union
// can neither invent nor remove an obligation.  This is the matrix quotient
// used for renderer composition; it contains no program or language data.
func minimumPrimitiveCover(rows []string, requirements map[string]map[string]bool, candidates []ProjectionObligationPrimitive) []ProjectionObligationPrimitive {
	type mask []uint64
	edges := []string{}
	for _, row := range rows {
		for column := range requirements[row] {
			edges = append(edges, row+"\x00"+column)
		}
	}
	sort.Strings(edges)
	edgeIndex := map[string]int{}
	for index, edge := range edges {
		edgeIndex[edge] = index
	}
	words := (len(edges) + 63) / 64
	makeMask := func(p ProjectionObligationPrimitive) mask {
		out := make(mask, words)
		for _, row := range p.Archetypes {
			for _, column := range p.Obligations {
				if !requirements[row][column] {
					continue
				}
				index, ok := edgeIndex[row+"\x00"+column]
				if ok {
					out[index/64] |= uint64(1) << uint(index%64)
				}
			}
		}
		return out
	}
	maskKey := func(value mask) string {
		var b strings.Builder
		for _, word := range value {
			fmt.Fprintf(&b, "%016x", word)
		}
		return b.String()
	}
	orMask := func(left, right mask) mask {
		out := make(mask, words)
		for i := range out {
			out[i] = left[i] | right[i]
		}
		return out
	}
	containsMask := func(have, need mask) bool {
		for i := range have {
			if have[i]|need[i] != have[i] {
				return false
			}
		}
		return true
	}
	bitCount := func(value mask) int {
		count := 0
		for _, word := range value {
			for word != 0 {
				word &= word - 1
				count++
			}
		}
		return count
	}
	// Singleton rectangles guarantee coverage even when a row has no closed
	// concept carrying all of its true columns.
	allCandidates := append([]ProjectionObligationPrimitive(nil), candidates...)
	for _, row := range rows {
		columns := setKeys(requirements[row])
		allCandidates = append(allCandidates, ProjectionObligationPrimitive{Archetypes: []string{row}, Obligations: columns})
	}
	type candidate struct {
		primitive ProjectionObligationPrimitive
		mask      mask
	}
	byMask := map[string]candidate{}
	for _, p := range allCandidates {
		m := makeMask(p)
		if bitCount(m) == 0 {
			continue
		}
		key := maskKey(m)
		if _, exists := byMask[key]; !exists {
			byMask[key] = candidate{primitive: p, mask: m}
		}
	}
	unique := make([]candidate, 0, len(byMask))
	for _, c := range byMask {
		unique = append(unique, c)
	}
	sort.Slice(unique, func(i, j int) bool {
		left, right := maskKey(unique[i].mask), maskKey(unique[j].mask)
		return left < right
	})
	// A rectangle contained in another one is never needed in a minimum cover.
	maximal := make([]candidate, 0, len(unique))
	for i, c := range unique {
		dominated := false
		for j, other := range unique {
			if i == j || bitCount(c.mask) >= bitCount(other.mask) {
				continue
			}
			if containsMask(other.mask, c.mask) {
				dominated = true
				break
			}
		}
		if !dominated {
			maximal = append(maximal, c)
		}
	}
	unique = maximal
	full := make(mask, words)
	for i := range full {
		full[i] = ^uint64(0)
	}
	if remainder := len(edges) % 64; remainder != 0 {
		full[len(full)-1] = (uint64(1) << uint(remainder)) - 1
	}
	byEdge := make([][]int, len(edges))
	for index := range unique {
		for edge := range edges {
			if unique[index].mask[edge/64]&(uint64(1)<<uint(edge%64)) != 0 {
				byEdge[edge] = append(byEdge[edge], index)
			}
		}
	}
	order := make([]int, len(edges))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(i, j int) bool { return len(byEdge[order[i]]) < len(byEdge[order[j]]) })
	// Greedy provides a deterministic upper bound for the exact search.
	greedy := []int{}
	covered := make(mask, words)
	for !containsMask(covered, full) {
		best, gain := -1, -1
		for index, c := range unique {
			gainMask := make(mask, words)
			for word := range gainMask {
				gainMask[word] = c.mask[word] &^ covered[word]
			}
			if value := bitCount(gainMask); value > gain {
				best, gain = index, value
			}
		}
		if best < 0 || gain == 0 {
			return nil
		}
		greedy = append(greedy, best)
		covered = orMask(covered, unique[best].mask)
	}
	best := append([]int(nil), greedy...)
	seen := map[string]int{}
	var search func(mask, []int)
	search = func(current mask, selected []int) {
		if containsMask(current, full) {
			if len(selected) < len(best) {
				best = append([]int(nil), selected...)
			}
			return
		}
		if len(selected) >= len(best) {
			return
		}
		state := maskKey(current)
		if previous, ok := seen[state]; ok && previous <= len(selected) {
			return
		}
		seen[state] = len(selected)
		chosenEdge := -1
		var choices []int
		for _, edge := range order {
			if current[edge/64]&(uint64(1)<<uint(edge%64)) != 0 {
				continue
			}
			local := byEdge[edge]
			if chosenEdge < 0 || len(local) < len(choices) {
				chosenEdge, choices = edge, local
			}
			if len(choices) <= 1 {
				break
			}
		}
		if chosenEdge < 0 {
			return
		}
		sort.SliceStable(choices, func(i, j int) bool {
			left, right := bitCount(unique[choices[i]].mask), bitCount(unique[choices[j]].mask)
			return left > right
		})
		for _, index := range choices {
			search(orMask(current, unique[index].mask), append(selected, index))
		}
	}
	search(make(mask, words), nil)
	if len(best) == 0 {
		return nil
	}
	out := make([]ProjectionObligationPrimitive, 0, len(best))
	for _, index := range best {
		out = append(out, unique[index].primitive)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.Join(out[i].Archetypes, ";")+"|"+strings.Join(out[i].Obligations, ";") < strings.Join(out[j].Archetypes, ";")+"|"+strings.Join(out[j].Obligations, ";")
	})
	return out
}

func intersection(left, right map[string]bool) map[string]bool {
	out := map[string]bool{}
	for value := range left {
		if right[value] {
			out[value] = true
		}
	}
	return out
}

func closureExtent(seed map[string]bool, rows map[string]map[string]bool, columns []string) map[string]bool {
	intent := map[string]bool{}
	first := true
	for row := range seed {
		if first {
			for column := range rows[row] {
				intent[column] = true
			}
			first = false
		} else {
			intent = intersection(intent, rows[row])
		}
	}
	extent := map[string]bool{}
	if first {
		return extent
	}
	for row, rowValues := range rows {
		if containsAll(rowValues, intent) {
			extent[row] = true
		}
	}
	return extent
}

func intentForExtent(extent map[string]bool, rows map[string]map[string]bool) map[string]bool {
	first := true
	intent := map[string]bool{}
	for row := range extent {
		if first {
			for column := range rows[row] {
				intent[column] = true
			}
			first = false
		} else {
			intent = intersection(intent, rows[row])
		}
	}
	return intent
}

// UniversalProjectionObligationAnalysis builds M_AO and then enumerates the
// closed formal concepts. A deterministic irredundant cover of those concepts
// yields M_AP and M_PO. Every selected concept is a true-only rectangle and
// singleton concepts guarantee coverage, hence the resulting boolean product
// is exact rather than heuristic.
func buildUniversalProjectionObligationAnalysis() (ProjectionObligationAnalysis, error) {
	harvest, err := UniversalProjectionRendererHarvest()
	if err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	rows := map[string]map[string]bool{}
	for _, archetype := range harvest.Archetypes {
		if !archetype.Implemented {
			rows[archetype.ID] = obligationSetForSignature(archetype.Signature)
		}
	}
	result := ProjectionObligationAnalysis{Requirements: rows}
	result.Archetypes = setKeys(func() map[string]bool {
		out := map[string]bool{}
		for row := range rows {
			out[row] = true
		}
		return out
	}())
	columnSet := map[string]bool{}
	for _, row := range rows {
		for column := range row {
			columnSet[column] = true
		}
	}
	result.Obligations = setKeys(columnSet)

	// Enumerate the closure system by intersecting closed extents with every
	// column extent.  The matrix is small (the residual renderer quotient), and
	// this is exhaustive for its formal concepts.
	columnExtents := map[string]map[string]bool{}
	for _, column := range result.Obligations {
		extent := map[string]bool{}
		for row, values := range rows {
			if values[column] {
				extent[row] = true
			}
		}
		columnExtents[column] = extent
	}
	all := map[string]bool{}
	for row := range rows {
		all[row] = true
	}
	queue := []map[string]bool{closureExtent(all, rows, result.Obligations)}
	seen := map[string]bool{}
	candidates := []ProjectionObligationPrimitive{}
	for len(queue) > 0 {
		extent := queue[0]
		queue = queue[1:]
		key := setSignature(extent)
		if len(extent) == 0 || seen[key] {
			continue
		}
		seen[key] = true
		intent := intentForExtent(extent, rows)
		if len(intent) > 0 {
			candidates = append(candidates, ProjectionObligationPrimitive{Archetypes: setKeys(extent), Obligations: setKeys(intent)})
		}
		for _, column := range result.Obligations {
			next := closureExtent(intersection(extent, columnExtents[column]), rows, result.Obligations)
			if len(next) != 0 && !seen[setSignature(next)] {
				queue = append(queue, next)
			}
		}
	}
	// Select the minimum exact boolean rectangle cover.  The old greedy pass
	// stopped at a locally irredundant 34-rectangle cover; the matrix itself
	// admits a smaller exact quotient.  Formal concepts and row singletons are
	// the complete candidate basis, so this selection does not add obligations.
	sort.Slice(candidates, func(i, j int) bool {
		left, right := len(candidates[i].Archetypes)*len(candidates[i].Obligations), len(candidates[j].Archetypes)*len(candidates[j].Obligations)
		if left != right {
			return left > right
		}
		if len(candidates[i].Obligations) != len(candidates[j].Obligations) {
			return len(candidates[i].Obligations) > len(candidates[j].Obligations)
		}
		return strings.Join(candidates[i].Archetypes, ";")+"|"+strings.Join(candidates[i].Obligations, ";") < strings.Join(candidates[j].Archetypes, ";")+"|"+strings.Join(candidates[j].Obligations, ";")
	})
	result.Primitives = minimumPrimitiveCover(result.Archetypes, rows, candidates)
	if len(result.Primitives) == 0 {
		return ProjectionObligationAnalysis{}, fmt.Errorf("projection obligation matrix has no exact rectangle cover")
	}
	for index := range result.Primitives {
		result.Primitives[index].ID = fmt.Sprintf("PRIM_%03d", index+1)
	}
	if err := result.ValidateExact(); err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	return result, nil
}

// UniversalProjectionObligationAnalysis returns the immutable, exact matrix
// quotient cached for this process.  All consumers therefore use the same
// minimum primitive basis and do not repeat the exponential cover search.
func UniversalProjectionObligationAnalysis() (ProjectionObligationAnalysis, error) {
	universalProjectionObligationOnce.Do(func() {
		universalProjectionObligationOnce.analysis, universalProjectionObligationOnce.err = buildUniversalProjectionObligationAnalysis()
	})
	return universalProjectionObligationOnce.analysis, universalProjectionObligationOnce.err
}

// ValidateExact proves M_AO = M_AP odot M_PO. It rejects both added and lost
// cells, protecting the projection contract against accidental broadening.
func (a ProjectionObligationAnalysis) ValidateExact() error {
	for _, row := range a.Archetypes {
		for _, column := range a.Obligations {
			actual := false
			for _, primitive := range a.Primitives {
				inExtent, inIntent := false, false
				for _, value := range primitive.Archetypes {
					if value == row {
						inExtent = true
						break
					}
				}
				for _, value := range primitive.Obligations {
					if value == column {
						inIntent = true
						break
					}
				}
				if inExtent && inIntent {
					actual = true
					break
				}
			}
			if actual != a.Requirements[row][column] {
				return fmt.Errorf("projection obligation factorization mismatch at %s × %s", row, column)
			}
		}
	}
	return nil
}

func writeProjectionCSV(path string, header []string, rows [][]string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	w := csv.NewWriter(file)
	if err := w.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// WriteProjectionObligationAnalysis exports the factor matrices. These are
// declarative registries used for deterministic renderer composition.
func WriteProjectionObligationAnalysis(dir string) (ProjectionObligationAnalysis, error) {
	a, err := UniversalProjectionObligationAnalysis()
	if err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	rows := [][]string{}
	for _, row := range a.Archetypes {
		for _, column := range a.Obligations {
			rows = append(rows, []string{row, column, fmt.Sprintf("%t", a.Requirements[row][column])})
		}
	}
	if err := writeProjectionCSV(filepath.Join(dir, "projection_atomic_obligation_matrix.csv"), []string{"archetype_id", "obligation", "required"}, rows); err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	apRows, poRows, primitiveRows := [][]string{}, [][]string{}, [][]string{}
	for _, p := range a.Primitives {
		primitiveRows = append(primitiveRows, []string{p.ID, strings.Join(p.Archetypes, ";"), strings.Join(p.Obligations, ";")})
		for _, row := range p.Archetypes {
			apRows = append(apRows, []string{row, p.ID, "true"})
		}
		for _, column := range p.Obligations {
			poRows = append(poRows, []string{p.ID, column, "true"})
		}
	}
	if err := writeProjectionCSV(filepath.Join(dir, "projection_archetype_primitive_matrix.csv"), []string{"archetype_id", "primitive_id", "required"}, apRows); err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "projection_primitive_obligation_matrix.csv"), []string{"primitive_id", "obligation", "provides"}, poRows); err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "projection_renderer_primitives.csv"), []string{"primitive_id", "archetypes", "obligations"}, primitiveRows); err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "global_missing_renderer_primitives.csv"), []string{"primitive_id", "archetypes", "obligations"}, primitiveRows); err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	support, err := ProjectionPrimitiveTargetSupports(a)
	if err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	supportRows, parameterRows := [][]string{}, [][]string{}
	for _, entry := range support {
		supportRows = append(supportRows, []string{entry.Target, entry.Primitive, fmt.Sprintf("%t", entry.Available), string(entry.Mode)})
		keys := make([]string, 0, len(entry.Parameters))
		for key := range entry.Parameters {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			parameterRows = append(parameterRows, []string{entry.Target, entry.Primitive, key, entry.Parameters[key]})
		}
	}
	if err := writeProjectionCSV(filepath.Join(dir, "target_primitive_support.csv"), []string{"target", "primitive_id", "available", "mode"}, supportRows); err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "target_primitive_parameters.csv"), []string{"target", "primitive_id", "parameter", "value"}, parameterRows); err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	compositions, err := GeneratedProjectionRendererCompositions()
	if err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	compositionRows := [][]string{}
	for _, composition := range compositions {
		for _, primitive := range composition.Primitives {
			compositionRows = append(compositionRows, []string{composition.ArchetypeID, primitive, "true"})
		}
	}
	if err := writeProjectionCSV(filepath.Join(dir, "projection_renderer_compositions.csv"), []string{"archetype_id", "primitive_id", "required"}, compositionRows); err != nil {
		return ProjectionObligationAnalysis{}, err
	}
	return a, nil
}

// ProjectionPrimitiveTargetSupports derives M_TP directly from TargetSpec.
// Residual primitives intentionally remain ERROR until a generic direct UAST
// renderer registers their form. This makes the next implementation step a
// small primitive registration rather than a Target×UASF special case.
func ProjectionPrimitiveTargetSupports(analysis ProjectionObligationAnalysis) ([]ProjectionPrimitiveTargetSupport, error) {
	result := []ProjectionPrimitiveTargetSupport{}
	for _, target := range Backends() {
		spec, ok := targetSpec(target.ID)
		if !ok {
			return nil, fmt.Errorf("missing target specification for %q", target.ID)
		}
		parameters := map[string]string{
			"block_open": spec.BlockOpen, "block_close": spec.BlockClose,
			"indent": spec.Indent, "terminator": spec.StatementTerminator,
			"runtime_requirement": spec.Imports.RuntimeRequirement,
		}
		for _, primitive := range analysis.Primitives {
			copyParameters := map[string]string{}
			for key, value := range parameters {
				copyParameters[key] = value
			}
			result = append(result, ProjectionPrimitiveTargetSupport{Target: target.ID, Primitive: primitive.ID, Available: false, Mode: PreservationError, Parameters: copyParameters})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Target != result[j].Target {
			return result[i].Target < result[j].Target
		}
		return result[i].Primitive < result[j].Primitive
	})
	return result, nil
}
