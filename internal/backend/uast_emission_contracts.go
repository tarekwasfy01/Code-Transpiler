package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// EmissionContractAnalysis is the syntax-only M_EO factorization. Atomic
// operations are the already validated projection obligations; no semantic
// datum or UAST node is copied into this registry.
type EmissionContractAnalysis struct {
	Contracts   []string
	Operations  []string
	Combinators []ProjectionObligationPrimitive
}

func UniversalEmissionContractAnalysis() (EmissionContractAnalysis, error) {
	projection, err := UniversalProjectionObligationAnalysis()
	if err != nil {
		return EmissionContractAnalysis{}, err
	}
	result := EmissionContractAnalysis{Combinators: append([]ProjectionObligationPrimitive(nil), projection.Primitives...)}
	for _, primitive := range result.Combinators {
		result.Contracts = append(result.Contracts, primitive.ID)
	}
	result.Operations = append([]string(nil), projection.Obligations...)
	sort.Strings(result.Contracts)
	sort.Strings(result.Operations)
	// ProjectionObligationAnalysis already proves the required boolean product.
	if err := projection.ValidateExact(); err != nil {
		return EmissionContractAnalysis{}, err
	}
	return result, nil
}

func WriteEmissionContractAnalysis(dir string) (EmissionContractAnalysis, error) {
	a, err := UniversalEmissionContractAnalysis()
	if err != nil {
		return EmissionContractAnalysis{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return EmissionContractAnalysis{}, err
	}
	rows := [][]string{}
	for _, c := range a.Combinators {
		for _, o := range c.Obligations {
			rows = append(rows, []string{c.ID, o, "true"})
		}
	}
	if err := writeProjectionCSV(filepath.Join(dir, "emission_contract_operation_matrix.csv"), []string{"emission_contract", "atomic_emission_operation", "required"}, rows); err != nil {
		return EmissionContractAnalysis{}, err
	}
	combRows := [][]string{}
	for _, c := range a.Combinators {
		combRows = append(combRows, []string{c.ID, fmt.Sprintf("%d", len(c.Archetypes)), fmt.Sprintf("%d", len(c.Obligations))})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "irreducible_emission_combinators.csv"), []string{"combinator_id", "contract_count", "operation_count"}, combRows); err != nil {
		return EmissionContractAnalysis{}, err
	}
	registry, err := UniversalEmissionRecipeRegistry()
	if err != nil {
		return EmissionContractAnalysis{}, err
	}
	recipeRows, handlerRows := [][]string{}, [][]string{}
	ids := make([]string, 0, len(registry.Recipes))
	for id := range registry.Recipes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		recipe := registry.Recipes[id]
		recipeRows = append(recipeRows, []string{recipe.ID, strings.Join(recipe.Archetypes, ";"), fmt.Sprintf("%d", len(recipe.Operations)), "true"})
		for _, operation := range recipe.Operations {
			handlerRows = append(handlerRows, []string{recipe.ID, operation.Atomic, string(operation.Handler), operation.Slot})
		}
	}
	if err := writeProjectionCSV(filepath.Join(dir, "emission_recipes.csv"), []string{"recipe_id", "archetypes", "operation_count", "executable"}, recipeRows); err != nil {
		return EmissionContractAnalysis{}, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "emission_recipe_handler_matrix.csv"), []string{"recipe_id", "atomic_operation", "handler_class", "slot"}, handlerRows); err != nil {
		return EmissionContractAnalysis{}, err
	}
	classes := [][]string{}
	for _, handler := range registry.HandlerClasses {
		classes = append(classes, []string{string(handler), "true"})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "atomic_execution_handler_classes.csv"), []string{"handler_class", "implemented"}, classes); err != nil {
		return EmissionContractAnalysis{}, err
	}
	return a, nil
}
