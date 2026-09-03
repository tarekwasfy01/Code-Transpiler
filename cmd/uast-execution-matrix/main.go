package main

import (
	"flag"
	"fmt"
	"path/filepath"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

func main() {
	out := flag.String("out", "outputs/uast-execution", "execution analysis output directory")
	flag.Parse()
	analysis, err := backend.WriteUniversalExecutionAnalysis(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	preservation, err := backend.WriteUASTTargetPreservationMatrix(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	preservationAnalysis, err := backend.WriteUASTPreservationAnalysis(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	projection, err := backend.WriteUASTStructureProjectionRegistry(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	renderers, err := backend.WriteProjectionRendererHarvest(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	obligations, err := backend.WriteProjectionObligationAnalysis(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	reduction, err := backend.WriteProjectionPrimitiveReduction(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	gapInformation, err := backend.WriteProjectionGapInformationAnalysis(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	integration, err := backend.WriteProjectionInformationIntegration(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	emission, err := backend.WriteEmissionContractAnalysis(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	templates, err := backend.WriteTargetSyntaxTemplateAnalysis(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	residual, err := backend.WriteProjectionResidualAnalysis(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	promotion, err := backend.WriteRuntimePromotionAnalysis(filepath.Clean(*out))
	if err != nil {
		panic(err)
	}
	implemented, executable := 0, 0
	for _, primitive := range analysis.Primitives {
		if primitive.Implemented {
			implemented++
		}
	}
	for _, value := range analysis.Executable {
		if value != 0 {
			executable++
		}
	}
	productivelyConsumed := 0
	for _, value := range analysis.ProductivelyConsumed {
		if value != 0 {
			productivelyConsumed++
		}
	}
	count := func(values []float64) int {
		n := 0
		for _, value := range values {
			if value != 0 {
				n++
			}
		}
		return n
	}
	preservationCounts := map[string]int{}
	for _, counts := range preservation.StatusCounts {
		for mode, n := range counts {
			preservationCounts[mode] += n
		}
	}
	infoComplete, emitterComplete := 0, 0
	for id := range integration.PrimitiveInformationComplete {
		if integration.PrimitiveInformationComplete[id] {
			infoComplete++
		}
		if integration.PrimitiveEmissionComplete[id] {
			emitterComplete++
		}
	}
	structureTemplateComplete := 0
	for _, cell := range templates.StructureCells {
		if cell.Complete {
			structureTemplateComplete++
		}
	}
	runtimeUASF, runtimeClasses, directClasses := 0, 0, 0
	for _, facets := range promotion.RuntimeUASF {
		runtimeUASF += len(facets)
	}
	for _, classes := range promotion.RuntimeProjectionClass {
		for supported := range classes {
			if classes[supported] {
				runtimeClasses++
			}
		}
	}
	for _, classes := range promotion.DirectProjectionClass {
		for supported := range classes {
			if classes[supported] {
				directClasses++
			}
		}
	}
	fmt.Printf("UAST execution matrix: UASF=%d/%d productively-consumed=%d/%d structures=%d/%d relations=%d/%d fields=%d/%d primitives=%d/%d classes=%d missing-primitives=%d preservation=%d cells DIRECT=%d REWRITE=%d HELPER=%d EMULATE=%d RUNTIME=%d ERROR=%d preservation-primitives=%d preservation-classes=%d projection-classes=%d renderer-catalog=%d renderer-archetypes=%d renderer-obligations=%d renderer-primitives=%d duplicate-primitives-removed=%d composable-primitives=%d irreducible-renderer-primitives=%d gap-A=%d gap-B=%d gap-C=%d gap-D=%d information-complete=%d emission-complete=%d derivation-classes=%d target-syntax-capabilities=%d atomic-emission-operations=%d irreducible-emission-combinators=%d syntax-templates=%d syntax-template-cells=%d syntax-template-complete=%d syntax-template-missing=%d syntax-parameter-missing=%d structure-syntax-cells=%d structure-syntax-complete=%d residual-rows=%d residual-classes=%d residual-primitives=%d promotion-runtime-uasf=%d promotion-runtime-classes=%d promotion-direct-classes=%d unclassified=%d\n", executable, len(analysis.Capabilities), productivelyConsumed, len(analysis.Capabilities), count(analysis.ExecutableStructures), len(analysis.Structures), count(analysis.ExecutableRelations), len(analysis.Relations), count(analysis.ExecutableFields), len(analysis.Fields), implemented, len(analysis.Primitives), len(analysis.EquivalenceClasses), len(analysis.GlobalMissing), len(preservation.Capabilities)*len(preservation.Targets), preservationCounts["DIRECT"], preservationCounts["REWRITE"], preservationCounts["HELPER"], preservationCounts["EMULATE"], preservationCounts["RUNTIME"], preservationCounts["ERROR"], len(preservationAnalysis.Primitives), len(preservationAnalysis.EquivalenceClasses), len(projection.Classes), len(renderers.Catalog), len(renderers.Archetypes), len(obligations.Obligations), len(obligations.Primitives), reduction.DuplicatesRemoved, len(reduction.Compositions), len(reduction.Irreducible), gapInformation.Counts[backend.ProjectionInfoAlready], gapInformation.Counts[backend.ProjectionInfoDerivable], gapInformation.Counts[backend.ProjectionInfoSyntax], gapInformation.Counts[backend.ProjectionInfoSemantic], infoComplete, emitterComplete, len(integration.DerivationClasses), len(integration.TargetSyntaxCapabilities), len(emission.Operations), len(emission.Combinators), len(templates.Templates), len(templates.Cells), templates.ExistingMatches, templates.MissingTemplate, templates.MissingParameter, len(templates.StructureCells), structureTemplateComplete, len(residual.Rows), len(residual.Classes), len(residual.ColumnGapCounts), runtimeUASF, runtimeClasses, directClasses, preservationAnalysis.Unclassified)
}
