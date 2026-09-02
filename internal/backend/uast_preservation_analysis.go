package backend

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// UASTPreservationPrimitive is a target-side realization of one already
// canonical execution contract. It is a registry row, not an IR node.
type UASTPreservationPrimitive struct {
	ID        string                 `json:"id"`
	Execution UASTExecutionPrimitive `json:"execution_primitive"`
}

// UASTPreservationContract is one flattened Target × UASF × Primitive cell.
// Required, Available and Missing make M_CTP inspectable without inventing a
// tensor type or a second semantic representation.
type UASTPreservationContract struct {
	Target              string           `json:"target"`
	UASF                string           `json:"uasf"`
	CurrentMode         PreservationMode `json:"current_mode"`
	RequiredPrimitives  []string         `json:"required_primitives"`
	AvailablePrimitives []string         `json:"available_primitives"`
	MissingPrimitives   []string         `json:"missing_primitives"`
	ErrorReason         string           `json:"error_reason,omitempty"`
	FallbacksChecked    []string         `json:"fallbacks_checked"`
}

type UASTPreservationEquivalenceClass struct {
	ID                 string           `json:"class_id"`
	Targets            []string         `json:"targets"`
	UASFMembers        []string         `json:"uasf_members"`
	RequiredPrimitives []string         `json:"required_primitives"`
	CurrentMode        PreservationMode `json:"current_mode"`
	MissingPrimitives  []string         `json:"missing_primitives"`
	ErrorReason        string           `json:"error_reason,omitempty"`
}

// UASTPreservationAnalysis is the complete matrix-derived target preservation
// contract. MCP is UASF × PreservationPrimitive, MTP is
// Target × PreservationPrimitive, and MCTP is the explicit flattened product
// in Contracts. No source-language data occurs in this analysis.
type UASTPreservationAnalysis struct {
	Schema               string                             `json:"schema"`
	BasisSHA256          string                             `json:"basis_sha256"`
	Capabilities         []string                           `json:"capabilities"`
	Targets              []string                           `json:"targets"`
	Primitives           []UASTPreservationPrimitive        `json:"primitives"`
	MCP                  matrixir.SparseMatrix              `json:"m_cp"`
	MTP                  matrixir.SparseMatrix              `json:"m_tp"`
	Contracts            []UASTPreservationContract         `json:"m_ctp"`
	EquivalenceClasses   []UASTPreservationEquivalenceClass `json:"preservation_equivalence_classes"`
	GlobalMissing        map[string]int                     `json:"global_missing_preservation_primitives"`
	ErrorReasons         map[string]int                     `json:"error_reasons"`
	Unclassified         int                                `json:"unclassified"`
	ProductiveLegacyDeps int                                `json:"productive_legacy_semantic_dependencies"`
}

func preservationPrimitives(registry UASTExecutionRegistry) []UASTPreservationPrimitive {
	out := make([]UASTPreservationPrimitive, 0, len(registry.Primitives))
	for _, primitive := range registry.Primitives {
		out = append(out, UASTPreservationPrimitive{ID: "target." + string(primitive.ID), Execution: primitive.ID})
	}
	return out
}

func preservationPrimitiveIndex(primitives []UASTPreservationPrimitive) map[UASTExecutionPrimitive]int {
	out := make(map[UASTExecutionPrimitive]int, len(primitives))
	for i, primitive := range primitives {
		out[primitive.Execution] = i
	}
	return out
}

func preservationNames(primitives []UASTPreservationPrimitive, row matrixir.SparseMatrix, r int) []string {
	out := []string{}
	for col, primitive := range primitives {
		if row.At(r, col) != 0 {
			out = append(out, primitive.ID)
		}
	}
	return out
}

func targetSyntaxMissingForFacet(target, facet string) bool {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return false
	}
	templates, err := UniversalTargetSyntaxTemplateAnalysis()
	if err != nil {
		return false
	}
	facetCol := indexOf(uastEmbedded.Basis.Facets, facet)
	if facetCol < 0 {
		return false
	}
	for _, contract := range registry.Contracts {
		structureRow := indexOf(uastEmbedded.Basis.StructuralKinds, contract.StructureKind)
		if structureRow < 0 || uastEmbedded.Basis.StructuralFacetSeed.At(structureRow, facetCol) == 0 {
			continue
		}
		cell, ok := templates.Cell(target, contract.ProjectionClass)
		if ok && !cell.Complete && cell.MissingReason == "MISSING_TARGET_TEMPLATE" {
			return true
		}
	}
	return false
}

func preservationReason(target, facet string, mode PreservationMode, missing []string) string {
	if mode != PreservationError {
		return ""
	}
	for _, primitive := range missing {
		if primitive == "target.syntax" {
			if targetSyntaxMissingForFacet(target, facet) {
				// The quotient proves a syntactic implementation gap for an
				// exact projection class.  It says nothing about the target
				// language's theoretical expressiveness.
				return "MISSING_TARGET_TEMPLATE"
			}
			return "PROJECTION_GAP"
		}
	}
	for _, primitive := range missing {
		if strings.Contains(primitive, "runtime") {
			return "ERROR_MISSING_RUNTIME"
		}
	}
	return "ERROR_MISSING_REWRITE"
}

// UniversalTargetPreservationAnalysis computes the target contract quotient
// from the canonical execution matrix and the existing proven target paths.
// Target primitive availability is proved by a currently executable UAST
// facet that requires the primitive; an unused primitive is not promoted by
// declaration alone. The target.syntax requirement is then added per cell
// from the real direct-UAST emitter boundary, so broader UASF rows cannot
// borrow syntax support from the small tested core.
func UniversalTargetPreservationAnalysis() (UASTPreservationAnalysis, error) {
	execution, err := UniversalExecutionAnalysis()
	if err != nil {
		return UASTPreservationAnalysis{}, err
	}
	preservation, err := UniversalTargetPreservationMatrix()
	if err != nil {
		return UASTPreservationAnalysis{}, err
	}
	registry := DefaultUASTExecutionRegistry()
	primitives := preservationPrimitives(registry)
	index := preservationPrimitiveIndex(primitives)
	analysis := UASTPreservationAnalysis{
		Schema: "code-transpiler.uast-preservation-analysis.v1", BasisSHA256: execution.BasisSHA256,
		Capabilities: append([]string(nil), execution.Capabilities...), Targets: append([]string(nil), preservation.Targets...), Primitives: primitives,
		MCP: matrixir.NewSparseMatrix(len(execution.Capabilities), len(primitives)), MTP: matrixir.NewSparseMatrix(len(preservation.Targets), len(primitives)),
		GlobalMissing: map[string]int{}, ErrorReasons: map[string]int{}, ProductiveLegacyDeps: execution.ProductiveLegacyRelations,
	}
	for row := range execution.Capabilities {
		for col, executionPrimitive := range execution.Primitives {
			if execution.MCE.At(row, col) == 0 {
				continue
			}
			if preservationColumn, ok := index[executionPrimitive.ID]; ok {
				analysis.MCP.Set(row, preservationColumn, 1)
			}
		}
	}
	// MTP is derived solely from proven current target paths.  A primitive is
	// available for a target when at least one non-error UASF path requires it.
	for targetCol := range preservation.Targets {
		for primitiveCol := range primitives {
			for capabilityRow := range execution.Capabilities {
				if analysis.MCP.At(capabilityRow, primitiveCol) != 0 && preservation.Status(capabilityRow, targetCol) != PreservationError {
					analysis.MTP.Set(targetCol, primitiveCol, 1)
					break
				}
			}
		}
	}
	type grouped struct {
		targets, facets   map[string]bool
		required, missing []string
		mode              PreservationMode
		reason            string
	}
	groups := map[string]*grouped{}
	for targetCol, target := range preservation.Targets {
		for capabilityRow, facet := range execution.Capabilities {
			required := preservationNames(primitives, analysis.MCP, capabilityRow)
			available, missing := []string{}, []string{}
			for primitiveCol, primitive := range primitives {
				if analysis.MCP.At(capabilityRow, primitiveCol) == 0 {
					continue
				}
				if analysis.MTP.At(targetCol, primitiveCol) != 0 {
					available = append(available, primitive.ID)
				} else {
					missing = append(missing, primitive.ID)
				}
			}
			mode := preservation.Status(capabilityRow, targetCol)
			if mode == PreservationError {
				// The generic target emitter is only proved for its structural
				// core.  This cell-level fact is part of M_CTP, not an invented
				// source-language rule.
				missing = append(missing, "target.syntax")
			}
			missing = uniqueSorted(missing)
			reason := preservationReason(target, facet, mode, missing)
			contract := UASTPreservationContract{Target: target, UASF: facet, CurrentMode: mode, RequiredPrimitives: required, AvailablePrimitives: available, MissingPrimitives: missing, ErrorReason: reason, FallbacksChecked: []string{"DIRECT", "REWRITE", "HELPER", "EMULATE", "RUNTIME"}}
			analysis.Contracts = append(analysis.Contracts, contract)
			if mode == PreservationError {
				analysis.ErrorReasons[reason]++
				for _, primitive := range missing {
					analysis.GlobalMissing[primitive]++
				}
			}
			signature := strings.Join([]string{string(mode), strings.Join(required, ";"), strings.Join(missing, ";"), reason}, "|")
			g := groups[signature]
			if g == nil {
				g = &grouped{targets: map[string]bool{}, facets: map[string]bool{}, required: required, missing: missing, mode: mode, reason: reason}
				groups[signature] = g
			}
			g.targets[target], g.facets[facet] = true, true
		}
	}
	signatures := make([]string, 0, len(groups))
	for signature := range groups {
		signatures = append(signatures, signature)
	}
	sort.Strings(signatures)
	for i, signature := range signatures {
		g := groups[signature]
		targets, facets := []string{}, []string{}
		for target := range g.targets {
			targets = append(targets, target)
		}
		for facet := range g.facets {
			facets = append(facets, facet)
		}
		sort.Strings(targets)
		sort.Strings(facets)
		analysis.EquivalenceClasses = append(analysis.EquivalenceClasses, UASTPreservationEquivalenceClass{ID: fmt.Sprintf("PRES_%03d", i+1), Targets: targets, UASFMembers: facets, RequiredPrimitives: g.required, CurrentMode: g.mode, MissingPrimitives: g.missing, ErrorReason: g.reason})
	}
	return analysis, nil
}

func WriteUASTPreservationAnalysis(dir string) (UASTPreservationAnalysis, error) {
	analysis, err := UniversalTargetPreservationAnalysis()
	if err != nil {
		return UASTPreservationAnalysis{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return UASTPreservationAnalysis{}, err
	}
	encoded, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return UASTPreservationAnalysis{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "preservation_analysis.json"), encoded, 0o644); err != nil {
		return UASTPreservationAnalysis{}, err
	}
	if err := writePreservationClasses(filepath.Join(dir, "preservation_equivalence_classes.csv"), analysis); err != nil {
		return UASTPreservationAnalysis{}, err
	}
	if err := writePreservationContracts(filepath.Join(dir, "target_uasf_preservation_contracts.csv"), analysis); err != nil {
		return UASTPreservationAnalysis{}, err
	}
	return analysis, writePreservationErrors(filepath.Join(dir, "target_preservation_errors.csv"), analysis)
}

func writePreservationClasses(path string, analysis UASTPreservationAnalysis) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"class_id", "targets", "uasf_members", "required_primitives", "current_mode", "missing_primitives", "error_reason"}); err != nil {
		return err
	}
	for _, class := range analysis.EquivalenceClasses {
		if err := w.Write([]string{class.ID, strings.Join(class.Targets, ";"), strings.Join(class.UASFMembers, ";"), strings.Join(class.RequiredPrimitives, ";"), string(class.CurrentMode), strings.Join(class.MissingPrimitives, ";"), class.ErrorReason}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func writePreservationContracts(path string, analysis UASTPreservationAnalysis) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"target", "uasf", "current_mode", "required_primitives", "available_primitives", "missing_primitives", "error_reason", "candidate_fallbacks_checked"}); err != nil {
		return err
	}
	for _, contract := range analysis.Contracts {
		if err := w.Write([]string{contract.Target, contract.UASF, string(contract.CurrentMode), strings.Join(contract.RequiredPrimitives, ";"), strings.Join(contract.AvailablePrimitives, ";"), strings.Join(contract.MissingPrimitives, ";"), contract.ErrorReason, strings.Join(contract.FallbacksChecked, ";")}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func writePreservationErrors(path string, analysis UASTPreservationAnalysis) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"target", "uasf", "error_reason", "required_semantics", "missing_primitive", "formal_limitation_reference", "candidate_fallbacks_checked"}); err != nil {
		return err
	}
	for _, contract := range analysis.Contracts {
		if contract.CurrentMode != PreservationError {
			continue
		}
		ref := "UniversalTargetProjector direct-UAST structural capability matrix"
		if contract.ErrorReason == "MISSING_TARGET_TEMPLATE" {
			ref = "target_projection_syntax_matrix.csv"
		}
		if err := w.Write([]string{contract.Target, contract.UASF, contract.ErrorReason, strings.Join(contract.RequiredPrimitives, ";"), strings.Join(contract.MissingPrimitives, ";"), ref, strings.Join(contract.FallbacksChecked, ";")}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
