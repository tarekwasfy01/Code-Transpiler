package backend

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

func TestUniversalExecutionAnalysisUsesExactUASFQuotient(t *testing.T) {
	analysis, err := UniversalExecutionAnalysis()
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Capabilities) != 334 || len(analysis.Structures) != 109 || len(analysis.Relations) != 55 || len(analysis.Fields) != 57 {
		t.Fatalf("unexpected execution axes: caps=%d structures=%d relations=%d fields=%d", len(analysis.Capabilities), len(analysis.Structures), len(analysis.Relations), len(analysis.Fields))
	}
	if analysis.MCE.Rows != 334 || analysis.MSE.Rows != 109 || analysis.MRE.Rows != 55 || analysis.MDE.Rows != 57 || analysis.MCE.Cols != len(analysis.Primitives) {
		t.Fatalf("unexpected execution matrix dimensions")
	}
	for name, vector := range map[string]matrixir.Vector{"UASF": analysis.Executable, "productively consumed UASF": analysis.ProductivelyConsumed, "structures": analysis.ExecutableStructures, "relations": analysis.ExecutableRelations, "fields": analysis.ExecutableFields} {
		for row, value := range vector {
			if value != 1 {
				t.Fatalf("%s execution row %d is not covered by a primitive handler", name, row)
			}
		}
	}
	members := 0
	seen := map[string]bool{}
	for _, class := range analysis.EquivalenceClasses {
		if class.ID == "" || class.ExecutionSignature == "" || len(class.UASFMembers) == 0 {
			t.Fatalf("invalid execution class: %+v", class)
		}
		members += len(class.UASFMembers)
		for _, capability := range class.UASFMembers {
			if seen[capability] {
				t.Fatalf("capability %q occurs in multiple exact quotient classes", capability)
			}
			seen[capability] = true
		}
	}
	if members != 334 {
		t.Fatalf("execution quotient covers %d UASF, want 334", members)
	}
	output := t.TempDir()
	if _, err := WriteUniversalExecutionAnalysis(output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"execution_analysis.json", "execution_equivalence_classes.csv", "missing_primitives.csv", "execution_primitives.csv", "capability_features.csv", "productive_consumer_coverage.csv", "capability_execution_matrix.csv", "structure_execution_matrix.csv", "relation_execution_matrix.csv", "field_execution_matrix.csv"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("missing generated execution report %s: %v", name, err)
		}
	}
}

func TestUniversalTargetPreservationMatrixIsCompleteAndExplicit(t *testing.T) {
	m, err := UniversalTargetPreservationMatrix()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateUASTTargetPreservationMatrix(m); err != nil {
		t.Fatal(err)
	}
	if got, want := len(m.Capabilities)*len(m.Targets), 13*334; got != want {
		t.Fatalf("preservation cells=%d want %d", got, want)
	}
	for _, target := range m.Targets {
		counts := m.StatusCounts[target]
		total := 0
		for _, n := range counts {
			total += n
		}
		if total != len(m.Capabilities) {
			t.Fatalf("target %s has %d classifications, want %d", target, total, len(m.Capabilities))
		}
	}
	output := t.TempDir()
	if _, err := WriteUASTTargetPreservationMatrix(output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"target_uasf_preservation.json", "target_uasf_preservation.csv"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("missing generated preservation report %s: %v", name, err)
		}
	}
}

func TestUniversalTargetPreservationMatrixIsConcurrentAndDefensive(t *testing.T) {
	const readers = 12
	matrices := make([]UASTTargetPreservationMatrix, readers)
	errs := make([]error, readers)
	var wg sync.WaitGroup
	for i := range matrices {
		wg.Add(1)
		go func(i int) { defer wg.Done(); matrices[i], errs[i] = UniversalTargetPreservationMatrix() }(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("reader %d: %v", i, err)
		}
	}
	base := matrices[0]
	initial := base.Status(0, 0)
	for i := 1; i < len(matrices); i++ {
		if matrices[i].BasisSHA256 != base.BasisSHA256 || matrices[i].Status(0, 0) != base.Status(0, 0) {
			t.Fatalf("reader %d returned a different preservation matrix", i)
		}
	}
	// Mutating a returned sparse matrix must not mutate the cached schema result.
	matrices[0].Direct.Set(0, 0, 0)
	again, err := UniversalTargetPreservationMatrix()
	if err != nil {
		t.Fatal(err)
	}
	if again.Status(0, 0) != initial {
		t.Fatal("caller mutation leaked into cached preservation matrix")
	}
}

func TestUniversalTargetPreservationAnalysisIsExactAndExplainsErrors(t *testing.T) {
	analysis, err := UniversalTargetPreservationAnalysis()
	if err != nil {
		t.Fatal(err)
	}
	if analysis.MCP.Rows != 334 || analysis.MTP.Rows != 13 || len(analysis.Contracts) != 13*334 || analysis.Unclassified != 0 {
		t.Fatalf("invalid preservation analysis dimensions: MCP=%dx%d MTP=%dx%d contracts=%d unclassified=%d", analysis.MCP.Rows, analysis.MCP.Cols, analysis.MTP.Rows, analysis.MTP.Cols, len(analysis.Contracts), analysis.Unclassified)
	}
	classified := 0
	for _, contract := range analysis.Contracts {
		if contract.CurrentMode == PreservationError && (contract.ErrorReason == "" || len(contract.MissingPrimitives) == 0) {
			t.Fatalf("unexplained preservation error: %+v", contract)
		}
		classified++
	}
	if classified != 13*334 || len(analysis.EquivalenceClasses) == 0 {
		t.Fatalf("preservation quotient incomplete: cells=%d classes=%d", classified, len(analysis.EquivalenceClasses))
	}
	output := t.TempDir()
	if _, err := WriteUASTPreservationAnalysis(output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"preservation_analysis.json", "preservation_equivalence_classes.csv", "target_uasf_preservation_contracts.csv", "target_preservation_errors.csv"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("missing preservation artifact %s: %v", name, err)
		}
	}
}

func TestUniversalStructureProjectionRegistryUsesCompleteSchemaQuotient(t *testing.T) {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if len(registry.Contracts) != 109 || len(registry.Classes) == 0 || len(registry.ClassProjectable) != len(registry.Classes) || len(registry.FieldUse) != 57 || len(registry.RelationUse) != 55 {
		t.Fatalf("incomplete structure projection registry: contracts=%d classes=%d fields=%d relations=%d", len(registry.Contracts), len(registry.Classes), len(registry.FieldUse), len(registry.RelationUse))
	}
	seen := map[string]bool{}
	metadataContracts := 0
	for _, contract := range registry.Contracts {
		if contract.StructureKind == "" || contract.ProjectionClass == "" || contract.ProjectionForm == "" || seen[contract.StructureKind] {
			t.Fatalf("invalid projection contract: %+v", contract)
		}
		seen[contract.StructureKind] = true
		if contract.EmissionPolicy == "METADATA_ONLY" {
			metadataContracts++
			if !contract.Implemented {
				t.Fatalf("metadata-only contract %q was not enabled", contract.StructureKind)
			}
		}
	}
	if metadataContracts == 0 {
		t.Fatal("expected schema-derived metadata-only projection contracts")
	}
	for field, role := range registry.FieldUse {
		switch role {
		case "SYNTAX_REQUIRED", "SEMANTIC_ONLY", "VALIDATION_ONLY", "REQUIREMENT_ONLY", "TARGET_DEPENDENT":
		default:
			t.Fatalf("field %q has invalid projection role %q", field, role)
		}
	}
	for relation, role := range registry.RelationUse {
		switch role {
		case "SYNTAX_STRUCTURAL", "EVALUATION_ORDER", "BINDING", "CONTROL", "TYPE", "OWNERSHIP_LIFETIME", "METADATA_ONLY":
		default:
			t.Fatalf("relation %q has invalid projection role %q", relation, role)
		}
	}
	output := t.TempDir()
	if _, err := WriteUASTStructureProjectionRegistry(output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"structure_projection_contracts.csv", "structure_projection_matrix.csv", "projection_equivalence_classes.csv", "field_projection_roles.csv", "relation_projection_roles.csv", "target_projection_capabilities.csv"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("missing projection artifact %s: %v", name, err)
		}
	}
}

func TestProjectionRendererHarvestUsesDirectUASTRenderersOnly(t *testing.T) {
	harvest, err := UniversalProjectionRendererHarvest()
	if err != nil {
		t.Fatal(err)
	}
	if len(harvest.Catalog) == 0 || len(harvest.Archetypes) == 0 || len(harvest.Assignments) == 0 {
		t.Fatalf("incomplete renderer harvest: %+v", harvest)
	}
	if want := len(harvest.Archetypes) * len(harvest.Catalog); len(harvest.Matches) != want {
		t.Fatalf("M_AR shape = %d, want %d", len(harvest.Matches), want)
	}
	for _, entry := range harvest.Catalog {
		if entry.Reusable && entry.SourceFile == "codegen_generic.go" {
			t.Fatalf("legacy renderer %q was accepted as direct UAST reusable", entry.RendererID)
		}
	}
	output := t.TempDir()
	if _, err := WriteProjectionRendererHarvest(output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"projection_renderer_catalog.csv", "renderer_equivalence_classes.csv", "projection_renderer_matrix.csv"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("missing renderer harvest artifact %s: %v", name, err)
		}
	}
}

func TestProjectionObligationFactorizationIsExact(t *testing.T) {
	analysis, err := UniversalProjectionObligationAnalysis()
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Archetypes) == 0 || len(analysis.Obligations) == 0 || len(analysis.Primitives) == 0 {
		t.Fatalf("incomplete projection obligation analysis: %+v", analysis)
	}
	if err := analysis.ValidateExact(); err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	if _, err := WriteProjectionObligationAnalysis(output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"projection_atomic_obligation_matrix.csv", "projection_archetype_primitive_matrix.csv", "projection_primitive_obligation_matrix.csv", "projection_renderer_primitives.csv", "global_missing_renderer_primitives.csv", "target_primitive_support.csv", "target_primitive_parameters.csv", "projection_renderer_compositions.csv"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("missing projection obligation artifact %s: %v", name, err)
		}
	}
}

func TestProjectionPrimitiveReductionHasAcyclicExactCompositions(t *testing.T) {
	reduction, err := UniversalProjectionPrimitiveReduction()
	if err != nil {
		t.Fatal(err)
	}
	if reduction.Before == 0 || len(reduction.Primitives) == 0 {
		t.Fatalf("empty primitive reduction: %+v", reduction)
	}
	if err := validatePrimitiveCompositionGraph(reduction.Compositions); err != nil {
		t.Fatal(err)
	}
	output := t.TempDir()
	if _, err := WriteProjectionPrimitiveReduction(output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"primitive_atomic_obligation_matrix.csv", "primitive_composition_graph.csv", "existing_renderer_primitive_matrix.csv", "existing_helper_primitive_matrix.csv", "existing_runtime_primitive_matrix.csv", "primitive_reduction_status.csv", "irreducible_missing_primitives.csv"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("missing primitive reduction artifact %s: %v", name, err)
		}
	}
}

func TestProjectionGapInformationDoesNotInventUASTSchemaGaps(t *testing.T) {
	analysis, err := UniversalProjectionGapInformationAnalysis()
	if err != nil {
		t.Fatal(err)
	}
	if len(analysis.Rows) == 0 {
		t.Fatal("missing projection gap information rows")
	}
	if analysis.Counts[ProjectionInfoSemantic] != 0 {
		t.Fatalf("unexpected unproved schema expansion gaps: %d", analysis.Counts[ProjectionInfoSemantic])
	}
	output := t.TempDir()
	if _, err := WriteProjectionGapInformationAnalysis(output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"projection_gap_information_matrix.csv", "uast_additive_expansion_plan.csv"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("missing gap information artifact %s: %v", name, err)
		}
	}
}

func TestProjectionInformationIntegrationKeepsSyntaxSeparate(t *testing.T) {
	integration, err := UniversalProjectionInformationIntegration()
	if err != nil {
		t.Fatal(err)
	}
	for primitive, complete := range integration.PrimitiveInformationComplete {
		if !complete {
			t.Fatalf("primitive %s has unintegrated A/B/C information", primitive)
		}
		if !integration.PrimitiveEmissionComplete[primitive] {
			t.Fatalf("generated residual emission recipe %s was not registered", primitive)
		}
	}
	output := t.TempDir()
	if _, err := WriteProjectionInformationIntegration(output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"uast_projection_direct_bindings.csv", "uast_projection_derivation_matrix.csv", "target_syntax_capability_matrix.csv", "target_syntax_parameters.csv", "projection_primitive_information_coverage.csv"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("missing integration artifact %s: %v", name, err)
		}
	}
}
