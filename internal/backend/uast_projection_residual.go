package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectionResidualCell is one exact Target × renderer-archetype row.  It
// contains no program data; it is the boolean residual of the existing
// renderer obligation matrix against the target syntax/template matrix.
type ProjectionResidualCell struct {
	Target             string
	Archetype          string
	RequiredPrimitives []string
	Supported          []string
	Missing            []string
	AffectedUASFCells  int
}

// ProjectionResidualClass groups rows only when their missing primitive
// vector is byte-for-byte identical after sorting.  Similarity and heuristic
// scores are deliberately not used.
type ProjectionResidualClass struct {
	ID                string
	MemberCount       int
	Targets           []string
	Archetypes        []string
	MissingPrimitives []string
	AffectedUASFCells int
}

// ProjectionResidualAnalysis is the exact residual plane used to select the
// next common renderer contract.  PrimitiveImpact counts only rows whose
// complete residual would become empty if the primitive (or exact set) were
// supported; it does not count partial improvements.
type ProjectionResidualAnalysis struct {
	Targets         []string
	Archetypes      []string
	Primitives      []string
	Rows            []ProjectionResidualCell
	Classes         []ProjectionResidualClass
	ColumnGapCounts map[string]int
	PrimitiveImpact map[string]int
	SetCounts       map[string]int
}

func residualKey(values []string) string {
	return strings.Join(uniqueSorted(values), ";")
}

func rendererPrimitiveTargetSupport(analysis ProjectionObligationAnalysis, target string, primitive string, recipes EmissionRecipeRegistry, spec TargetSpec) bool {
	for _, recipe := range recipes.Recipes {
		if recipe.ID != primitive {
			continue
		}
		for _, operation := range recipe.Operations {
			switch operation.Handler {
			case emissionHandlerValidate, emissionHandlerChild:
				// These handlers have no target-dependent syntax parameter.
			case emissionHandlerTarget:
				switch {
				case strings.HasPrefix(operation.Atomic, "precedence=") && len(spec.Operators) == 0:
					return false
				case strings.HasPrefix(operation.Atomic, "block=") && spec.BlockOpen == "" && spec.BlockClose == "" && spec.Indent == "":
					return false
				}
			default:
				return false
			}
		}
		return true
	}
	_ = analysis
	_ = target
	return false
}

func archetypeUASFCoverage(archetype ProjectionRendererArchetype, registry UASTStructureProjectionRegistry) map[string]bool {
	covered := map[string]bool{}
	for _, class := range archetype.ProjectionClasses {
		for _, contract := range registry.Contracts {
			if contract.ProjectionClass != class {
				continue
			}
			row := indexOf(uastEmbedded.Basis.StructuralKinds, contract.StructureKind)
			if row < 0 {
				continue
			}
			for col, facet := range uastEmbedded.Basis.Facets {
				if uastEmbedded.Basis.StructuralFacetSeed.At(row, col) != 0 {
					covered[facet] = true
				}
			}
		}
	}
	return covered
}

// UniversalProjectionResidualAnalysis computes M_RESIDUAL directly from the
// existing renderer composition and target syntax matrices.  The synthetic
// form:<projection-form> atoms are syntax obligations, not new UAST
// structures; they expose exactly which generic form contract is absent.
func UniversalProjectionResidualAnalysis() (ProjectionResidualAnalysis, error) {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return ProjectionResidualAnalysis{}, err
	}
	harvest, err := UniversalProjectionRendererHarvest()
	if err != nil {
		return ProjectionResidualAnalysis{}, err
	}
	obligations, err := UniversalProjectionObligationAnalysis()
	if err != nil {
		return ProjectionResidualAnalysis{}, err
	}
	recipes, err := UniversalEmissionRecipeRegistry()
	if err != nil {
		return ProjectionResidualAnalysis{}, err
	}
	compositions, err := GeneratedProjectionRendererCompositions()
	if err != nil {
		return ProjectionResidualAnalysis{}, err
	}
	compositionByArchetype := map[string][]string{}
	for _, composition := range compositions {
		compositionByArchetype[composition.ArchetypeID] = uniqueSorted(composition.Primitives)
	}
	templates, err := UniversalTargetSyntaxTemplateAnalysis()
	if err != nil {
		return ProjectionResidualAnalysis{}, err
	}
	if err := loadUniversalASTBasis(); err != nil {
		return ProjectionResidualAnalysis{}, err
	}

	result := ProjectionResidualAnalysis{ColumnGapCounts: map[string]int{}, PrimitiveImpact: map[string]int{}, SetCounts: map[string]int{}}
	for _, target := range Backends() {
		result.Targets = append(result.Targets, target.ID)
	}
	for _, archetype := range harvest.Archetypes {
		result.Archetypes = append(result.Archetypes, archetype.ID)
	}
	for _, primitive := range obligations.Primitives {
		result.Primitives = append(result.Primitives, primitive.ID)
	}
	sort.Strings(result.Targets)
	sort.Strings(result.Archetypes)
	sort.Strings(result.Primitives)

	byArchetype := map[string]ProjectionRendererArchetype{}
	for _, archetype := range harvest.Archetypes {
		byArchetype[archetype.ID] = archetype
	}
	for _, target := range result.Targets {
		spec, ok := targetSpec(target)
		if !ok {
			return ProjectionResidualAnalysis{}, fmt.Errorf("missing target spec %q", target)
		}
		for _, archetypeID := range result.Archetypes {
			archetype := byArchetype[archetypeID]
			required := append([]string(nil), compositionByArchetype[archetypeID]...)
			supported := []string{}
			missing := []string{}
			for _, primitive := range required {
				if rendererPrimitiveTargetSupport(obligations, target, primitive, recipes, spec) {
					supported = append(supported, primitive)
				} else {
					missing = append(missing, primitive)
				}
			}
			// A renderer primitive sequence is only syntactically usable when
			// every member structure has a complete target form contract.
			coveredForms := map[string]bool{}
			for _, class := range archetype.ProjectionClasses {
				for _, contract := range registry.Contracts {
					if contract.ProjectionClass != class {
						continue
					}
					if cell, ok := templates.StructureCell(target, contract.StructureKind); ok && cell.Complete {
						coveredForms["form:"+contract.ProjectionForm] = true
					} else {
						missing = append(missing, "form:"+contract.ProjectionForm)
					}
				}
			}
			for form := range coveredForms {
				supported = append(supported, form)
			}
			required = uniqueSorted(required)
			supported = uniqueSorted(supported)
			missing = uniqueSorted(missing)
			if len(missing) == 0 {
				continue
			}
			affected := archetypeUASFCoverage(archetype, registry)
			result.Rows = append(result.Rows, ProjectionResidualCell{Target: target, Archetype: archetypeID, RequiredPrimitives: required, Supported: supported, Missing: missing, AffectedUASFCells: len(affected)})
			for _, primitive := range missing {
				result.ColumnGapCounts[primitive]++
			}
			result.SetCounts[residualKey(missing)]++
		}
	}

	groups := map[string]*struct {
		rows, targets, archetypes, uasf map[string]bool
		missing                         []string
	}{}
	for _, row := range result.Rows {
		key := residualKey(row.Missing)
		group := groups[key]
		if group == nil {
			group = &struct {
				rows, targets, archetypes, uasf map[string]bool
				missing                         []string
			}{rows: map[string]bool{}, targets: map[string]bool{}, archetypes: map[string]bool{}, uasf: map[string]bool{}, missing: uniqueSorted(row.Missing)}
			groups[key] = group
		}
		group.rows[row.Target+"|"+row.Archetype] = true
		group.targets[row.Target] = true
		group.archetypes[row.Archetype] = true
		covered := archetypeUASFCoverage(byArchetype[row.Archetype], registry)
		for facet := range covered {
			group.uasf[row.Target+"|"+facet] = true
		}
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for i, key := range keys {
		group := groups[key]
		result.Classes = append(result.Classes, ProjectionResidualClass{ID: fmt.Sprintf("RESID_%03d", i+1), MemberCount: len(group.rows), Targets: mapKeysSorted(group.targets), Archetypes: mapKeysSorted(group.archetypes), MissingPrimitives: group.missing, AffectedUASFCells: len(group.uasf)})
	}
	// A primitive is a complete one-step fix only for rows whose residual set
	// is exactly that primitive.  This is the requested unlocked-cell impact,
	// rather than a partial-coverage score.
	for _, row := range result.Rows {
		if len(row.Missing) == 1 {
			result.PrimitiveImpact[row.Missing[0]]++
		}
	}
	return result, nil
}

func mapKeysSorted(values map[string]bool) []string {
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// WriteProjectionResidualAnalysis emits the residual quotient and exact
// column/impact tables consumed by the next generic projection iteration.
func WriteProjectionResidualAnalysis(dir string) (ProjectionResidualAnalysis, error) {
	a, err := UniversalProjectionResidualAnalysis()
	if err != nil {
		return ProjectionResidualAnalysis{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ProjectionResidualAnalysis{}, err
	}
	classRows := [][]string{}
	for _, class := range a.Classes {
		classRows = append(classRows, []string{class.ID, fmt.Sprintf("%d", class.MemberCount), strings.Join(class.Targets, ";"), strings.Join(class.Archetypes, ";"), strings.Join(class.MissingPrimitives, ";"), fmt.Sprintf("%d", class.AffectedUASFCells)})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "residual_equivalence_classes.csv"), []string{"class_id", "member_count", "targets", "archetypes", "missing_primitives", "affected_uasf_cells"}, classRows); err != nil {
		return a, err
	}
	columnRows := [][]string{}
	for _, primitive := range sortedMapKeys(a.ColumnGapCounts) {
		columnRows = append(columnRows, []string{primitive, fmt.Sprintf("%d", a.ColumnGapCounts[primitive])})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "residual_primitive_column_gaps.csv"), []string{"primitive", "column_gap_count"}, columnRows); err != nil {
		return a, err
	}
	impactRows := [][]string{}
	for _, primitive := range sortedMapKeys(a.PrimitiveImpact) {
		impactRows = append(impactRows, []string{primitive, fmt.Sprintf("%d", a.PrimitiveImpact[primitive])})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "residual_primitive_impact.csv"), []string{"primitive", "cells_unlocked_if_supported"}, impactRows); err != nil {
		return a, err
	}
	setRows := [][]string{}
	for _, key := range sortedMapKeys(a.SetCounts) {
		setRows = append(setRows, []string{key, fmt.Sprintf("%d", a.SetCounts[key])})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "residual_primitive_sets.csv"), []string{"missing_primitive_set", "member_count"}, setRows); err != nil {
		return a, err
	}
	return a, nil
}

func sortedMapKeys(values map[string]int) []string {
	out := make([]string, 0, len(values))
	for key := range values {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}
