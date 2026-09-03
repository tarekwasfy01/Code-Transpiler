package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectionGapInformationClass separates UAST facts from their projection
// consumption. Only class D is allowed to request an additive schema change.
type ProjectionGapInformationClass string

const (
	ProjectionInfoAlready   ProjectionGapInformationClass = "A_ALREADY_IN_UAST"
	ProjectionInfoDerivable ProjectionGapInformationClass = "B_DERIVABLE"
	ProjectionInfoSyntax    ProjectionGapInformationClass = "C_TARGET_SYNTAX"
	ProjectionInfoSemantic  ProjectionGapInformationClass = "D_TRUE_UAST_SEMANTIC_GAP"
)

type ProjectionGapInformation struct {
	GapID                 string
	ProjectionPrimitive   string
	RequiredInformation   string
	Class                 ProjectionGapInformationClass
	ExistingUASTElement   string
	DerivationRule        string
	TargetSyntaxParameter string
	SemanticGapID         string
	EvidenceSource        string
	Conflict              bool
}

type ProjectionGapInformationAnalysis struct {
	Rows   []ProjectionGapInformation
	Counts map[ProjectionGapInformationClass]int
}

func classifyProjectionObligation(obligation string) (ProjectionGapInformationClass, string, string, string) {
	switch {
	case strings.HasPrefix(obligation, "child="):
		return ProjectionInfoAlready, "field." + strings.TrimPrefix(obligation, "child="), "", ""
	case strings.HasPrefix(obligation, "category="):
		return ProjectionInfoDerivable, "schema.structural_layer", "StructuralLayer matrix projection", ""
	case strings.HasPrefix(obligation, "field_role="):
		return ProjectionInfoDerivable, "schema.field", "projectionFieldUse", ""
	case strings.HasPrefix(obligation, "relation_role="):
		return ProjectionInfoDerivable, "schema.relation", "projectionRelationUse", ""
	case strings.HasPrefix(obligation, "precedence="):
		return ProjectionInfoSyntax, "", "", "TargetSpec.Operators"
	case strings.HasPrefix(obligation, "block="):
		return ProjectionInfoSyntax, "", "", "TargetSpec.BlockOpen/BlockClose/Indent"
	case strings.HasPrefix(obligation, "terminator="):
		return ProjectionInfoSyntax, "", "", "TargetSpec.StatementTerminator"
	default:
		// This is intentionally the only route to class D. The current
		// primitive basis is schema-derived, so an unknown requirement is a
		// diagnosable missing universal semantic distinction rather than a
		// target syntax guess.
		return ProjectionInfoSemantic, "", "", ""
	}
}

// UniversalProjectionGapInformationAnalysis classifies every remaining
// primitive obligation. The matrix is the proof boundary for any future UAST
// expansion: a schema mutation is impossible unless a D row is present.
func UniversalProjectionGapInformationAnalysis() (ProjectionGapInformationAnalysis, error) {
	reduction, err := UniversalProjectionPrimitiveReduction()
	if err != nil {
		return ProjectionGapInformationAnalysis{}, err
	}
	analysis := ProjectionGapInformationAnalysis{Counts: map[ProjectionGapInformationClass]int{}}
	for _, primitive := range reduction.Primitives {
		for _, obligation := range sortedUnique(primitive.Obligations) {
			class, element, rule, parameter := classifyProjectionObligation(obligation)
			row := ProjectionGapInformation{
				GapID: primitive.ID + ":" + obligation, ProjectionPrimitive: primitive.ID, RequiredInformation: obligation,
				Class: class, ExistingUASTElement: element, DerivationRule: rule, TargetSyntaxParameter: parameter,
				EvidenceSource: "StructureProjectionContract+PrimitiveObligationMatrix",
			}
			if class == ProjectionInfoSemantic {
				row.SemanticGapID = "semantic-gap:" + primitive.ID + ":" + obligation
			}
			analysis.Rows = append(analysis.Rows, row)
			analysis.Counts[class]++
		}
	}
	sort.Slice(analysis.Rows, func(i, j int) bool { return analysis.Rows[i].GapID < analysis.Rows[j].GapID })
	return analysis, nil
}

func WriteProjectionGapInformationAnalysis(dir string) (ProjectionGapInformationAnalysis, error) {
	analysis, err := UniversalProjectionGapInformationAnalysis()
	if err != nil {
		return ProjectionGapInformationAnalysis{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ProjectionGapInformationAnalysis{}, err
	}
	rows := make([][]string, 0, len(analysis.Rows))
	for _, row := range analysis.Rows {
		rows = append(rows, []string{row.GapID, row.ProjectionPrimitive, row.RequiredInformation, string(row.Class), row.ExistingUASTElement, row.DerivationRule, row.TargetSyntaxParameter, row.SemanticGapID, row.EvidenceSource, fmt.Sprintf("%t", row.Conflict)})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "projection_gap_information_matrix.csv"), []string{"gap_id", "projection_primitive", "required_information", "information_class_A_B_C_D", "existing_uast_element", "derivation_rule", "target_syntax_parameter", "semantic_gap_id", "evidence_source", "conflict"}, rows); err != nil {
		return ProjectionGapInformationAnalysis{}, err
	}
	// No D row means that the exact minimal additive delta is the empty set.
	// Keep the plan as a valid, explicit empty-schema report rather than
	// introducing placeholder UAST elements.
	if err := writeProjectionCSV(filepath.Join(dir, "uast_additive_expansion_plan.csv"), []string{"semantic_gap_id", "new_or_existing", "element_type", "element_id", "semantic_contract", "required_by_gaps", "affected_languages", "existing_execution_primitive_coverage", "existing_projection_primitive_coverage", "schema_version"}, [][]string{}); err != nil {
		return ProjectionGapInformationAnalysis{}, err
	}
	return analysis, nil
}
