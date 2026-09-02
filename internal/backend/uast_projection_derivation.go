package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ProjectionDerivationRule is a declarative typed transform over existing
// UAST/schema information. It is not a semantic IR and cannot create a new
// semantic fact; it only exposes a proved projection input.
type ProjectionDerivationRule struct {
	ID        string
	Input     string
	Output    string
	Transform string
}

type ProjectionInformationIntegration struct {
	DirectBindings               []ProjectionGapInformation
	Derivations                  []ProjectionDerivationRule
	Syntax                       []ProjectionGapInformation
	PrimitiveInformationComplete map[string]bool
	PrimitiveEmissionComplete    map[string]bool
	DerivationClasses            []string
	TargetSyntaxCapabilities     []string
}

func derivationRuleFor(row ProjectionGapInformation) ProjectionDerivationRule {
	transform := row.DerivationRule
	if transform == "" {
		transform = "direct"
	}
	return ProjectionDerivationRule{ID: "derive." + strings.ReplaceAll(strings.ToLower(strings.ReplaceAll(transform, " ", "_")), ".", "_"), Input: row.ExistingUASTElement, Output: row.RequiredInformation, Transform: transform}
}

// UniversalProjectionInformationIntegration compiles A/B/C into projection
// data. The separate EmissionComplete bit is essential: information being
// available never pretends that a missing syntax combinator exists.
func UniversalProjectionInformationIntegration() (ProjectionInformationIntegration, error) {
	gaps, err := UniversalProjectionGapInformationAnalysis()
	if err != nil {
		return ProjectionInformationIntegration{}, err
	}
	reduction, err := UniversalProjectionPrimitiveReduction()
	if err != nil {
		return ProjectionInformationIntegration{}, err
	}
	recipeRegistry, err := UniversalEmissionRecipeRegistry()
	if err != nil {
		return ProjectionInformationIntegration{}, err
	}
	result := ProjectionInformationIntegration{PrimitiveInformationComplete: map[string]bool{}, PrimitiveEmissionComplete: map[string]bool{}}
	derivationSet, syntaxSet := map[string]bool{}, map[string]bool{}
	covered := map[string]map[string]bool{}
	for _, row := range gaps.Rows {
		if covered[row.ProjectionPrimitive] == nil {
			covered[row.ProjectionPrimitive] = map[string]bool{}
		}
		switch row.Class {
		case ProjectionInfoAlready:
			result.DirectBindings = append(result.DirectBindings, row)
			covered[row.ProjectionPrimitive][row.RequiredInformation] = true
		case ProjectionInfoDerivable:
			rule := derivationRuleFor(row)
			result.Derivations = append(result.Derivations, rule)
			derivationSet[rule.Transform] = true
			covered[row.ProjectionPrimitive][row.RequiredInformation] = true
		case ProjectionInfoSyntax:
			result.Syntax = append(result.Syntax, row)
			syntaxSet[row.TargetSyntaxParameter] = true
			covered[row.ProjectionPrimitive][row.RequiredInformation] = true
		}
	}
	for _, primitive := range reduction.Primitives {
		complete := true
		for _, obligation := range primitive.Obligations {
			if !covered[primitive.ID][obligation] {
				complete = false
				break
			}
		}
		result.PrimitiveInformationComplete[primitive.ID] = complete
		// A generated recipe is a registered syntax-only Doc combinator. It
		// deliberately does not upgrade a target preservation mode: that still
		// requires a proved TargetSpec semantic syntax contract.
		_, result.PrimitiveEmissionComplete[primitive.ID] = recipeRegistry.Recipes[primitive.ID]
	}
	result.DerivationClasses = setKeys(derivationSet)
	result.TargetSyntaxCapabilities = setKeys(syntaxSet)
	sort.Slice(result.DirectBindings, func(i, j int) bool { return result.DirectBindings[i].GapID < result.DirectBindings[j].GapID })
	sort.Slice(result.Derivations, func(i, j int) bool {
		if result.Derivations[i].ID != result.Derivations[j].ID {
			return result.Derivations[i].ID < result.Derivations[j].ID
		}
		return result.Derivations[i].Output < result.Derivations[j].Output
	})
	return result, nil
}

func WriteProjectionInformationIntegration(dir string) (ProjectionInformationIntegration, error) {
	integration, err := UniversalProjectionInformationIntegration()
	if err != nil {
		return ProjectionInformationIntegration{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ProjectionInformationIntegration{}, err
	}
	directRows := [][]string{}
	for _, row := range integration.DirectBindings {
		directRows = append(directRows, []string{row.ProjectionPrimitive, row.ExistingUASTElement, row.RequiredInformation})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "uast_projection_direct_bindings.csv"), []string{"projection_primitive", "uast_element", "projection_obligation"}, directRows); err != nil {
		return ProjectionInformationIntegration{}, err
	}
	derivationRows := [][]string{}
	for _, rule := range integration.Derivations {
		derivationRows = append(derivationRows, []string{rule.ID, rule.Input, rule.Output, rule.Transform})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "uast_projection_derivation_matrix.csv"), []string{"derivation_id", "uast_information", "derived_projection_information", "transform"}, derivationRows); err != nil {
		return ProjectionInformationIntegration{}, err
	}

	targetRows, parameterRows := [][]string{}, [][]string{}
	for _, target := range Backends() {
		spec, ok := targetSpec(target.ID)
		if !ok {
			return ProjectionInformationIntegration{}, fmt.Errorf("missing target spec %q", target.ID)
		}
		params := map[string]string{
			"TargetSpec.Operators":                   "registered",
			"TargetSpec.BlockOpen/BlockClose/Indent": spec.BlockOpen + "|" + spec.BlockClose + "|" + spec.Indent,
			"TargetSpec.StatementTerminator":         spec.StatementTerminator,
		}
		for _, capability := range integration.TargetSyntaxCapabilities {
			value, supported := params[capability]
			targetRows = append(targetRows, []string{target.ID, capability, fmt.Sprintf("%t", supported)})
			if supported {
				parameterRows = append(parameterRows, []string{target.ID, capability, value})
			}
		}
	}
	if err := writeProjectionCSV(filepath.Join(dir, "target_syntax_capability_matrix.csv"), []string{"target", "target_syntax_capability", "supported"}, targetRows); err != nil {
		return ProjectionInformationIntegration{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "target_syntax_parameters.csv"), []string{"target", "target_syntax_capability", "value"}, parameterRows); err != nil {
		return ProjectionInformationIntegration{}, err
	}

	primitiveRows := [][]string{}
	ids := make([]string, 0, len(integration.PrimitiveInformationComplete))
	for id := range integration.PrimitiveInformationComplete {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		primitiveRows = append(primitiveRows, []string{id, fmt.Sprintf("%t", integration.PrimitiveInformationComplete[id]), fmt.Sprintf("%t", integration.PrimitiveEmissionComplete[id])})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "projection_primitive_information_coverage.csv"), []string{"primitive_id", "a_b_c_information_complete", "generic_emission_combinator_registered"}, primitiveRows); err != nil {
		return ProjectionInformationIntegration{}, err
	}
	return integration, nil
}
