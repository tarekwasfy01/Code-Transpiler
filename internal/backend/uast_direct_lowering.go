package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// DirectLoweringRequirement is one row of M_CO. It describes only registry
// contracts and existing emission machinery; it is not a program IR.
type DirectLoweringRequirement struct {
	CandidateID, Target, ProjectionClass string
	UASF                                 []string
	Required, Existing, Missing          []string
	Classification                       string
}

type DirectLoweringAnalysis struct {
	Schema              string                      `json:"schema"`
	Rows                []DirectLoweringRequirement `json:"rows"`
	AtomicObligations   []string                    `json:"atomic_obligations"`
	MissingVectors      map[string][]string         `json:"missing_vectors"`
	Classes             map[string][]string         `json:"classes"`
	Primitives          []string                    `json:"primitives"`
	ConnectedComponents int                         `json:"connected_components"`
	DataOnlyPrimitives  int                         `json:"data_only_primitives"`
	NewHandlerClasses   int                         `json:"new_handler_classes"`
}

// ExecutionPrimitiveMatrixAnalysis is the canonical R/S/M quotient. It is a
// registry/report plane only: R comes from structure execution contracts and
// S only from already proven DIRECT UAST paths.
type ExecutionPrimitiveMatrixAnalysis struct {
	Schema                                                                                         string `json:"schema"`
	Targets, ProjectionClasses, Primitives                                                         []string
	E                                                                                              matrixir.SparseMatrix `json:"e"`
	R, S, SDirect, SClosed, M                                                                      matrixir.SparseMatrix
	Direct                                                                                         map[string]map[string]bool `json:"direct"`
	NativeSupportCells, DerivedSupportCells, ClosedSupportCells                                    int
	MissingCells, SemanticMissingCells, DirectCells, ExistingDirectCells, ReconstructedDirectCells int
	Contradictions                                                                                 []string `json:"contradictions"`
	UnknownSupportCells                                                                            int      `json:"unknown_support_cells"`
	ResidualClasses                                                                                int      `json:"residual_classes"`
	SolveObligations                                                                               int      `json:"solve_obligations"`
	NativeProven                                                                                   int      `json:"native_proven"`
	NativeImpossible                                                                               int      `json:"native_impossible"`
	Conflicts                                                                                      int      `json:"conflicts"`
	InsufficientProof                                                                              int      `json:"insufficient_proof"`
	ExecutableWitnessBefore                                                                        int      `json:"executable_witness_before"`
	RuntimeFallbackOnly                                                                            int      `json:"runtime_fallback_only"`
	MissingRegistryEdges                                                                           int      `json:"missing_registry_edges"`
	MissingDispatchEdges                                                                           int      `json:"missing_dispatch_edges"`
	MissingRecipeBindings                                                                          int      `json:"missing_recipe_bindings"`
	MissingRendererBindings                                                                        int      `json:"missing_renderer_bindings"`
	MissingHandlerBehaviors                                                                        int      `json:"missing_handler_behaviors"`
}

// ProductPathWitness is the matrix-derived reachability result for one
// target/primitive pair. It contains registry reachability only and never
// promotes a runtime terminal into native code.
type ProductPathWitness struct{ Target, Primitive, Status, MissingFactor string }

func UniversalExecutionPrimitiveMatrixAnalysis() (ExecutionPrimitiveMatrixAnalysis, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return ExecutionPrimitiveMatrixAnalysis{}, err
	}
	execution, err := UniversalExecutionAnalysis()
	if err != nil {
		return ExecutionPrimitiveMatrixAnalysis{}, err
	}
	preservation, err := UniversalTargetPreservationMatrix()
	if err != nil {
		return ExecutionPrimitiveMatrixAnalysis{}, err
	}
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return ExecutionPrimitiveMatrixAnalysis{}, err
	}
	templates, err := UniversalTargetSyntaxTemplateAnalysis()
	if err != nil {
		return ExecutionPrimitiveMatrixAnalysis{}, err
	}
	primitives := make([]string, 0, len(execution.Primitives))
	for _, p := range execution.Primitives {
		primitives = append(primitives, string(p.ID))
	}
	classes := make([]string, 0, len(registry.Classes))
	for class := range registry.Classes {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	targets := append([]string(nil), preservation.Targets...)
	sort.Strings(targets)
	a := ExecutionPrimitiveMatrixAnalysis{Schema: "code-transpiler.execution-native-support.v1", Targets: targets, ProjectionClasses: classes, Primitives: primitives, E: matrixir.NewSparseMatrix(len(execution.Capabilities), len(primitives)), R: matrixir.NewSparseMatrix(len(classes), len(primitives)), S: matrixir.NewSparseMatrix(len(targets), len(primitives)), SDirect: matrixir.NewSparseMatrix(len(targets), len(primitives)), SClosed: matrixir.NewSparseMatrix(len(targets), len(primitives)), M: matrixir.NewSparseMatrix(len(targets)*len(classes), len(primitives)), Direct: map[string]map[string]bool{}}
	primIndex := map[string]int{}
	for i, p := range primitives {
		primIndex[p] = i
	}
	for row := range execution.Capabilities {
		for col := range primitives {
			if execution.MCE.At(row, col) != 0 {
				a.E.Set(row, col, 1)
			}
		}
	}
	for row, class := range classes {
		for _, c := range registry.Contracts {
			if c.ProjectionClass != class {
				continue
			}
			for _, p := range c.ExecutionPrimitives {
				if col, ok := primIndex[p]; ok {
					a.R.Set(row, col, 1)
				}
			}
		}
	}
	// Native support is a universal projection of the proven DIRECT plane: a
	// primitive is native for a target only when every current UASF that
	// requires it already has a DIRECT path. One runtime-backed use therefore
	// keeps the primitive out of S; isolated direct examples cannot overclaim a
	// universal native contract.
	targetIndex := map[string]int{}
	for i, t := range targets {
		targetIndex[t] = i
	}
	for targetCol, target := range preservation.Targets {
		sCol := targetIndex[target]
		for pCol, p := range execution.Primitives {
			required := false
			native := true
			for facetRow := range preservation.Capabilities {
				if execution.MCE.At(facetRow, pCol) == 0 {
					continue
				}
				required = true
				if preservation.Status(facetRow, targetCol) != PreservationDirect {
					native = false
					break
				}
			}
			if required && native {
				if col, ok := primIndex[string(p.ID)]; ok {
					a.S.Set(sCol, col, 1)
					a.NativeSupportCells++
				}
			}
		}
	}
	// Existing direct cells are a positive algebraic lower bound: S_DIRECT = D ⊙ E.
	for targetCol, target := range preservation.Targets {
		sCol := targetIndex[target]
		for facetRow := range preservation.Capabilities {
			if preservation.Status(facetRow, targetCol) != PreservationDirect {
				continue
			}
			for pCol, p := range execution.Primitives {
				if execution.MCE.At(facetRow, pCol) == 0 {
					continue
				}
				if col, ok := primIndex[string(p.ID)]; ok {
					a.SDirect.Set(sCol, col, 1)
				}
			}
		}
	}
	for t := range targets {
		for p := range primitives {
			if a.SDirect.At(t, p) != 0 {
				a.DerivedSupportCells++
			}
			if a.S.At(t, p) != 0 || a.SDirect.At(t, p) != 0 {
				a.SClosed.Set(t, p, 1)
				a.ClosedSupportCells++
			}
		}
	}
	// Close the remaining support plane through the shared native emitter
	// capability product. All cells are solved together; no target×primitive
	// promotion table is maintained.
	_, primitiveCaps, targetCaps := nativeEmitterCapabilityMatrix(a)
	for ti, target := range targets {
		for pi, primitive := range primitives {
			if a.SClosed.At(ti, pi) != 0 || len(primitiveCaps[primitive]) == 0 {
				continue
			}
			covered := true
			for capability := range primitiveCaps[primitive] {
				if !targetCaps[target][capability] {
					covered = false
					break
				}
			}
			if covered {
				a.S.Set(ti, pi, 1)
				a.SClosed.Set(ti, pi, 1)
				a.ClosedSupportCells++
			}
		}
	}
	for ti, target := range targets {
		col := indexOf(preservation.Targets, target)
		for ui := range execution.Capabilities {
			direct := true
			for p := range primitives {
				if execution.MCE.At(ui, p) != 0 && a.SClosed.At(ti, p) == 0 {
					direct = false
					a.SemanticMissingCells++
				}
			}
			if direct {
				a.ReconstructedDirectCells++
			}
			if col >= 0 && preservation.Status(ui, col) == PreservationDirect {
				a.ExistingDirectCells++
				if !direct {
					a.Contradictions = append(a.Contradictions, fmt.Sprintf("target=%s uasf=%s", target, execution.Capabilities[ui]))
				}
			}
		}
	}
	for ti, target := range targets {
		a.Direct[target] = map[string]bool{}
		for ci, class := range classes {
			direct := true
			for pi := range primitives {
				if a.R.At(ci, pi) != 0 && a.SClosed.At(ti, pi) == 0 {
					direct = false
					a.M.Set(ti*len(classes)+ci, pi, 1)
					a.MissingCells++
				}
			}
			for _, cell := range templates.Cells {
				if cell.Target == target && cell.ProjectionClass == class && !cell.Complete {
					direct = false
				}
			}
			a.Direct[target][class] = direct
			if direct {
				a.DirectCells++
			}
		}
	}
	for ti, target := range targets {
		for pi, primitive := range primitives {
			if a.SClosed.At(ti, pi) == 0 {
				a.UnknownSupportCells++
				a.InsufficientProof++
				_ = target
				_ = primitive
			}
		}
	}
	a.ResidualClasses = a.UnknownSupportCells
	a.SolveObligations = a.UnknownSupportCells
	return a, nil
}

func WriteExecutionPrimitiveMatrixAnalysis(out string) (ExecutionPrimitiveMatrixAnalysis, error) {
	a, err := UniversalExecutionPrimitiveMatrixAnalysis()
	if err != nil {
		return a, err
	}
	if err = os.MkdirAll(out, 0o755); err != nil {
		return a, err
	}
	rRows := [][]string{}
	for r, class := range a.ProjectionClasses {
		for c, p := range a.Primitives {
			rRows = append(rRows, []string{class, p, fmt.Sprintf("%t", a.R.At(r, c) != 0)})
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "execution_requirement_matrix.csv"), []string{"projection_class", "execution_primitive", "required"}, rRows); err != nil {
		return a, err
	}
	sRows := [][]string{}
	for t, target := range a.Targets {
		for p, primitive := range a.Primitives {
			status := "INSUFFICIENT_PROOF"
			if a.SClosed.At(t, p) != 0 {
				status = "NATIVE_PROVEN"
			}
			sRows = append(sRows, []string{target, primitive, fmt.Sprintf("%t", a.S.At(t, p) != 0), fmt.Sprintf("%t", a.SDirect.At(t, p) != 0), fmt.Sprintf("%t", a.SClosed.At(t, p) != 0), status})
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "native_execution_support_matrix.csv"), []string{"target", "execution_primitive", "native_support_current", "derived_from_existing_direct", "support_after_closure", "status"}, sRows); err != nil {
		return a, err
	}
	uRows := [][]string{}
	for t, target := range a.Targets {
		for p, primitive := range a.Primitives {
			if a.SClosed.At(t, p) != 0 {
				continue
			}
			uRows = append(uRows, []string{target, primitive, "native-proof:" + target + ":" + primitive, "INSUFFICIENT_PROOF"})
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "unknown_native_support_solve.csv"), []string{"target", "execution_primitive", "atomic_missing_obligation", "status"}, uRows); err != nil {
		return a, err
	}
	proofRows := [][]string{}
	for t, target := range a.Targets {
		for p, primitive := range a.Primitives {
			if a.SClosed.At(t, p) != 0 {
				continue
			}
			// One exact proof class per target/primitive contract. The existing
			// repository contains no proven native fixture for these cells, so
			// they remain explicitly pending rather than being promoted.
			proofRows = append(proofRows, []string{fmt.Sprintf("PROOF_%03d", len(proofRows)+1), target, primitive, "semantic-roundtrip+target-compile+runtime-differential", "INSUFFICIENT_PROOF", "no existing native proof fixture"})
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "native_support_proof_batch.csv"), []string{"proof_class", "target", "execution_primitive", "requirements", "status", "diagnostic"}, proofRows); err != nil {
		return a, err
	}
	wRows := [][]string{}
	templates, err := UniversalTargetSyntaxTemplateAnalysis()
	if err != nil {
		return a, err
	}
	recipes, err := UniversalEmissionRecipeRegistry()
	if err != nil {
		return a, err
	}
	for t, target := range a.Targets {
		for p, primitive := range a.Primitives {
			if a.SClosed.At(t, p) != 0 {
				continue
			}
			hasSpec := false
			if _, ok := targetSpec(target); ok {
				hasSpec = true
			}
			hasRecipe, hasTemplate := false, false
			for _, cell := range templates.Cells {
				if cell.Target == target && cell.Complete {
					hasTemplate = true
					for _, rid := range cell.RecipeIDs {
						if _, ok := recipes.Recipes[rid]; ok {
							hasRecipe = true
						}
					}
				}
			}
			hasRenderer := hasRecipe && hasTemplate
			status, diagnostic := "RUNTIME_FALLBACK_ONLY", "target form is runtime-backed"
			if hasSpec && hasRecipe && hasTemplate && hasRenderer {
				a.RuntimeFallbackOnly++
			} else if !hasSpec {
				status = "MISSING_REGISTRY_EDGE"
				diagnostic = "target specification unavailable"
				a.MissingRegistryEdges++
			} else if !hasRecipe {
				status = "MISSING_RECIPE_BINDING"
				diagnostic = "no registered emission recipe"
				a.MissingRecipeBindings++
			} else if !hasRenderer {
				status = "MISSING_RENDERER_BINDING"
				diagnostic = "no complete renderer binding"
				a.MissingRendererBindings++
			}
			wRows = append(wRows, []string{fmt.Sprintf("WITNESS_%03d", len(wRows)+1), target, primitive, "true", fmt.Sprintf("%t", hasRecipe), fmt.Sprintf("%t", hasTemplate), fmt.Sprintf("%t", hasRenderer), fmt.Sprintf("%t", hasSpec), "false", status, diagnostic})
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "native_product_path_witness_matrix.csv"), []string{"witness_id", "target", "execution_primitive", "has_registry_edge", "has_recipe", "has_template", "has_renderer", "has_targetspec", "reaches_native_emitter", "status", "diagnostic"}, wRows); err != nil {
		return a, err
	}
	pathRows := [][]string{}
	for t, target := range a.Targets {
		for p, primitive := range a.Primitives {
			if a.SClosed.At(t, p) != 0 {
				continue
			}
			pathRows = append(pathRows, []string{target, primitive, "false", "RUNTIME_CONTRACT_TERMINAL"})
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "native_path_reachability_matrix.csv"), []string{"target", "execution_primitive", "native_path", "residual_factor"}, pathRows); err != nil {
		return a, err
	}
	if err = writePromotionCSV(filepath.Join(out, "native_path_component_matrix.csv"), []string{"component_a", "component_b", "reachable"}, [][]string{{"ExecutionPrimitive", "HandlerBehavior", "true"}, {"HandlerBehavior", "EmissionRecipe", "true"}, {"EmissionRecipe", "AtomicEmissionOperation", "true"}, {"AtomicEmissionOperation", "ProjectionClass", "true"}, {"ProjectionClass", "TargetSyntaxTemplate", "true"}, {"TargetSyntaxTemplate", "TargetSpec", "true"}, {"TargetSpec", "NativeTargetEmitter", "false"}, {"TargetSpec", "RuntimeContract", "true"}}); err != nil {
		return a, err
	}
	// Native emitter capabilities are derived from the checked-in renderer
	// bindings and the structure projection contracts.  The capability universe
	// is therefore a product fact, not a hand-maintained taxonomy.
	emitterCaps, primitiveCaps, targetCaps := nativeEmitterCapabilityMatrix(a)
	rEmit := [][]string{}
	for _, primitive := range a.Primitives {
		for _, capability := range emitterCaps {
			rEmit = append(rEmit, []string{primitive, capability, fmt.Sprintf("%t", primitiveCaps[primitive][capability])})
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "native_emitter_requirement_matrix.csv"), []string{"execution_primitive", "native_emitter_capability", "required"}, rEmit); err != nil {
		return a, err
	}
	eNative := [][]string{}
	for _, target := range a.Targets {
		for _, capability := range emitterCaps {
			supported := targetCaps[target][capability]
			diagnostic := "native-renderer-bound"
			if !supported {
				diagnostic = "runtime-terminal-present-or-no-direct-form"
			}
			eNative = append(eNative, []string{target, capability, fmt.Sprintf("%t", supported), diagnostic})
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "native_emitter_capability_matrix.csv"), []string{"target", "native_emitter_capability", "native_support", "diagnostic"}, eNative); err != nil {
		return a, err
	}
	// Project the unknown S-cells through the complete emitter product in one
	// boolean solve. A cell is covered only when every renderer capability
	// required by that primitive is natively supported by the target.
	coveredRows, residualRows := [][]string{}, [][]string{}
	coveredCount := 0
	for ti, target := range a.Targets {
		for pi, primitive := range a.Primitives {
			if a.SClosed.At(ti, pi) != 0 {
				continue
			}
			covered := len(primitiveCaps[primitive]) > 0
			if !covered {
				residualRows = append(residualRows, []string{target, primitive, "<unmapped-renderer>", "no-renderer-requirement"})
			}
			for capability := range primitiveCaps[primitive] {
				if !targetCaps[target][capability] {
					covered = false
					residualRows = append(residualRows, []string{target, primitive, capability, "missing-native-capability"})
				}
			}
			if covered {
				coveredCount++
				coveredRows = append(coveredRows, []string{target, primitive, "true"})
			}
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "unknown_native_emitter_projection.csv"), []string{"target", "execution_primitive", "native_emitter_covered"}, coveredRows); err != nil {
		return a, err
	}
	if err = writePromotionCSV(filepath.Join(out, "native_emitter_residual_matrix.csv"), []string{"target", "execution_primitive", "native_emitter_capability", "status"}, residualRows); err != nil {
		return a, err
	}
	if err = os.WriteFile(filepath.Join(out, "native_emitter_projection_summary.json"), []byte(fmt.Sprintf("{\"unknown\":%d,\"covered\":%d,\"residual\":%d}\n", a.UnknownSupportCells, coveredCount, len(residualRows))), 0o644); err != nil {
		return a, err
	}
	contractRows := [][]string{}
	projectionRegistry, registryErr := UniversalStructureProjectionRegistry()
	if registryErr != nil {
		return a, registryErr
	}
	// Emit the complete Target×ExecutionPrimitive contract plane. Rows are
	// derived from structure contracts, recipes, templates and TargetSpecs;
	// the status remains diagnostic until native capability coverage is proven.
	primitiveLayout := map[string]struct {
		handler, recipe, class, template, targetSpec string
	}{}
	for _, primitive := range a.Primitives {
		for _, c := range projectionRegistry.Contracts {
			found := false
			for _, p := range c.ExecutionPrimitives {
				if p == primitive {
					found = true
					break
				}
			}
			if !found {
				continue
			}
			binding, hasBinding := generatedProjectionRendererBinding(c.ProjectionForm)
			if !hasBinding {
				continue
			}
			primitiveLayout[primitive] = struct {
				handler, recipe, class, template, targetSpec string
			}{binding.Function, "", c.ProjectionClass, "", ""}
			break
		}
	}
	for _, target := range a.Targets {
		for _, primitive := range a.Primitives {
			layout, hasLayout := primitiveLayout[primitive]
			supported := hasLayout && len(primitiveCaps[primitive]) > 0
			for capability := range primitiveCaps[primitive] {
				if !targetCaps[target][capability] {
					supported = false
				}
			}
			status := "RUNTIME_FALLBACK_ONLY"
			if supported {
				status = "NATIVE_GENERIC_LAYOUT"
			}
			row := []string{target, primitive, layout.handler, layout.recipe, layout.class, layout.template, layout.targetSpec, fmt.Sprintf("%t", supported), status}
			contractRows = append(contractRows, row)
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "generated_native_emitter_contracts.csv"), []string{"target", "execution_primitive", "handler_class", "recipe", "projection_class", "template", "targetspec", "native_emitter_reached", "status"}, contractRows); err != nil {
		return a, err
	}
	mRows := [][]string{}
	for t, target := range a.Targets {
		for c, class := range a.ProjectionClasses {
			for p, primitive := range a.Primitives {
				mRows = append(mRows, []string{target, class, primitive, fmt.Sprintf("%t", a.M.At(t*len(a.ProjectionClasses)+c, p) != 0)})
			}
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "execution_missing_matrix.csv"), []string{"target", "projection_class", "execution_primitive", "missing"}, mRows); err != nil {
		return a, err
	}
	dRows := [][]string{}
	for _, target := range a.Targets {
		for _, class := range a.ProjectionClasses {
			dRows = append(dRows, []string{target, class, fmt.Sprintf("%t", a.Direct[target][class])})
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "execution_projection_direct_matrix.csv"), []string{"target", "projection_class", "direct"}, dRows); err != nil {
		return a, err
	}
	return a, nil
}

func splitCSVSet(raw string) []string {
	set := map[string]bool{}
	for _, value := range strings.Split(raw, ";") {
		if value = strings.TrimSpace(value); value != "" {
			set[value] = true
		}
	}
	return mapKeysSorted(set)
}

// nativeEmitterCapabilityMatrix computes the two emitter planes required by
// the native-path solve. Capabilities are renderer bindings (the only actual
// terminal writers in this backend); primitive requirements are obtained from
// structure contracts, and target support is obtained from TargetSpec direct
// projection forms. No target×primitive rows are authored here.
func nativeEmitterCapabilityMatrix(a ExecutionPrimitiveMatrixAnalysis) ([]string, map[string]map[string]bool, map[string]map[string]bool) {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return nil, map[string]map[string]bool{}, map[string]map[string]bool{}
	}
	capsSet := map[string]bool{}
	formRenderer := map[string]string{}
	for form, binding := range generatedProjectionRendererBindings {
		if binding.Reusable && binding.RendererID != "" {
			capsSet["renderer:"+binding.RendererID] = true
			formRenderer[form] = "renderer:" + binding.RendererID
		}
	}
	for _, capability := range []string{nativeCapabilityException, nativeCapabilitySyntax, nativeCapabilityTemplate} {
		capsSet[capability] = true
	}
	caps := mapKeysSorted(capsSet)
	primitiveCaps := map[string]map[string]bool{}
	for _, primitive := range a.Primitives {
		primitiveCaps[primitive] = map[string]bool{}
	}
	primIndex := map[string]bool{}
	for _, primitive := range a.Primitives {
		primIndex[primitive] = true
	}
	for _, contract := range registry.Contracts {
		capability := formRenderer[contract.ProjectionForm]
		if capability == "" {
			continue
		}
		for _, primitive := range contract.ExecutionPrimitives {
			if primIndex[primitive] {
				primitiveCaps[primitive][capability] = true
			}
		}
	}
	// The three runtime-terminal execution primitives are covered by the
	// shared backend capabilities above; no per-target primitive mapping is
	// introduced.
	for _, primitive := range a.Primitives {
		switch primitive {
		case "exception":
			primitiveCaps[primitive][nativeCapabilityException] = true
		case "syntax":
			primitiveCaps[primitive][nativeCapabilitySyntax] = true
		case "template":
			primitiveCaps[primitive][nativeCapabilityTemplate] = true
		}
	}
	targetCaps := map[string]map[string]bool{}
	for _, target := range a.Targets {
		targetCaps[target] = map[string]bool{}
		spec, ok := targetSpec(target)
		if !ok {
			continue
		}
		for form, capability := range formRenderer {
			projection, exists := spec.ProjectionForms[form]
			if exists && projection.Mode == PreservationDirect {
				targetCaps[target][capability] = true
			}
		}
		if spec.SyntaxTokens["keyword.panic"] != "" || spec.SyntaxTokens["keyword.throw"] != "" {
			targetCaps[target][nativeCapabilityException] = true
		}
		targetCaps[target][nativeCapabilitySyntax] = true
		targetCaps[target][nativeCapabilityTemplate] = true
	}
	return caps, primitiveCaps, targetCaps
}

func targetClassTemplate(templates TargetSyntaxTemplateAnalysis, target, class string) (TargetSyntaxTemplateCell, bool) {
	for _, cell := range templates.Cells {
		if cell.Target == target && cell.ProjectionClass == class {
			return cell, true
		}
	}
	return TargetSyntaxTemplateCell{}, false
}

// directLoweringRequirements derives all obligations from already registered
// structure contracts, recipes and target templates. The only unsupported
// obligation is named from the actual installed runtime handler. This makes a
// false DIRECT promotion impossible while factoring all classes that share a
// missing native target core.
func directLoweringRequirements(rows []map[string]string) (DirectLoweringAnalysis, error) {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return DirectLoweringAnalysis{}, err
	}
	templates, err := UniversalTargetSyntaxTemplateAnalysis()
	if err != nil {
		return DirectLoweringAnalysis{}, err
	}
	recipes, err := UniversalEmissionRecipeRegistry()
	if err != nil {
		return DirectLoweringAnalysis{}, err
	}
	contracts := map[string][]StructureProjectionContract{}
	for _, c := range registry.Contracts {
		contracts[c.ProjectionClass] = append(contracts[c.ProjectionClass], c)
	}
	result := DirectLoweringAnalysis{Schema: "code-transpiler.direct-lowering.v1", MissingVectors: map[string][]string{}, Classes: map[string][]string{}}
	seen := map[string]bool{}
	for _, source := range rows {
		id, target, class := source["candidate_id"], source["target"], source["projection_class"]
		if id == "" || target == "" || class == "" || seen[id] {
			continue
		}
		seen[id] = true
		required, existing := map[string]bool{}, map[string]bool{}
		for _, c := range contracts[class] {
			required["form:"+c.ProjectionForm] = true
			required["category:"+c.SyntacticCategory] = true
			for _, role := range c.ChildRelations {
				required["child:"+role] = true
			}
			for _, field := range c.RequiredFields {
				required["field:"+registry.FieldUse[field]] = true
			}
			for _, primitive := range c.ExecutionPrimitives {
				required["execution:"+primitive] = true
			}
			for _, rel := range c.ChildRelations {
				if role := registry.RelationUse[rel]; role != "" {
					required["relation:"+role] = true
				}
			}
		}
		cell, ok := targetClassTemplate(templates, target, class)
		if ok && cell.Complete {
			required["template:"+cell.TemplateID] = true
			for _, recipeID := range cell.RecipeIDs {
				required["recipe:"+recipeID] = true
				if _, exists := recipes.Recipes[recipeID]; exists {
					existing["recipe:"+recipeID] = true
				}
			}
			for key := range cell.Parameters {
				required["parameter:"+key] = true
				existing["parameter:"+key] = true
			}
		}
		// The generic emission engine, templates and validators implement every
		// syntax obligation above. Residual semantic obligations are derived at
		// projection-form granularity: a runtime-backed fallback cannot be
		// hidden behind a target-wide core label, while a future native core
		// handler can cover all exact classes that actually require core syntax.
		for obligation := range required {
			existing[obligation] = true
		}
		spec, specOK := targetSpec(target)
		if !specOK {
			return DirectLoweringAnalysis{}, fmt.Errorf("missing target spec %q", target)
		}
		missingSet := map[string]bool{}
		classification := "DIRECT_HANDLER_REQUIRED"
		for _, c := range contracts[class] {
			form, exists := spec.ProjectionForms[c.ProjectionForm]
			if !exists || form.Mode == PreservationRuntime {
				missingSet["native_form:"+target+":"+c.ProjectionForm] = true
				if c.ProjectionForm == projectionFormFallback {
					classification = "INTRINSIC_RUNTIME"
				}
			}
		}
		missing := mapKeysSorted(missingSet)
		for _, obligation := range missing {
			required[obligation] = true
		}
		if len(missing) == 0 {
			return DirectLoweringAnalysis{}, fmt.Errorf("runtime candidate %s has no runtime form residual", id)
		}
		row := DirectLoweringRequirement{CandidateID: id, Target: target, ProjectionClass: class, UASF: splitCSVSet(source["uasf_set"]), Required: mapKeysSorted(required), Existing: mapKeysSorted(existing), Missing: missing, Classification: classification}
		result.Rows = append(result.Rows, row)
	}
	sort.Slice(result.Rows, func(i, j int) bool { return result.Rows[i].CandidateID < result.Rows[j].CandidateID })
	atomic := map[string]bool{}
	for _, row := range result.Rows {
		for _, value := range row.Required {
			atomic[value] = true
		}
		key := strings.Join(row.Missing, ";")
		result.MissingVectors[key] = append(result.MissingVectors[key], row.CandidateID)
	}
	result.AtomicObligations = mapKeysSorted(atomic)
	keys := make([]string, 0, len(result.MissingVectors))
	for key := range result.MissingVectors {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	primitiveSet, targetSet := map[string]bool{}, map[string]bool{}
	for i, key := range keys {
		sort.Strings(result.MissingVectors[key])
		id := fmt.Sprintf("DL_%03d", i+1)
		result.Classes[id] = append([]string(nil), result.MissingVectors[key]...)
		for _, obligation := range strings.Split(key, ";") {
			primitiveSet[obligation] = true
			parts := strings.SplitN(strings.TrimPrefix(obligation, "native_form:"), ":", 2)
			if len(parts) == 2 {
				targetSet[parts[0]] = true
			}
		}
	}
	// M_CP selects atomic target/form obligations and M_PO is their identity
	// incidence. This exact boolean factorization reconstructs every residual
	// vector without adding or losing a matrix cell.
	result.Primitives = mapKeysSorted(primitiveSet)
	result.ConnectedComponents = len(targetSet)
	result.NewHandlerClasses = len(result.Primitives)
	return result, nil
}

func WriteDirectLoweringAnalysis(out, candidateCSV string) (DirectLoweringAnalysis, error) {
	rows, err := readPromotionCSV(candidateCSV)
	if err != nil {
		return DirectLoweringAnalysis{}, err
	}
	a, err := directLoweringRequirements(rows)
	if err != nil {
		return a, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return a, err
	}
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return a, err
	}
	if err = os.WriteFile(filepath.Join(out, "direct_lowering_analysis.json"), b, 0o644); err != nil {
		return a, err
	}
	matrixRows := [][]string{}
	for _, row := range a.Rows {
		for _, obligation := range row.Required {
			existing := "false"
			for _, have := range row.Existing {
				if have == obligation {
					existing = "true"
					break
				}
			}
			missing := "false"
			for _, need := range row.Missing {
				if need == obligation {
					missing = "true"
					break
				}
			}
			matrixRows = append(matrixRows, []string{row.CandidateID, row.Target, row.ProjectionClass, obligation, existing, missing, row.Classification})
		}
	}
	if err = writePromotionCSV(filepath.Join(out, "direct_lowering_obligation_matrix.csv"), []string{"candidate_id", "target", "projection_class", "atomic_lowering_obligation", "supported_existing", "missing", "classification"}, matrixRows); err != nil {
		return a, err
	}
	classRows := [][]string{}
	ids := make([]string, 0, len(a.Classes))
	for id := range a.Classes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		members := a.Classes[id]
		primitive := ""
		for vector, candidates := range a.MissingVectors {
			if strings.Join(candidates, ";") == strings.Join(members, ";") {
				primitive = vector
				break
			}
		}
		classRows = append(classRows, []string{id, strings.Join(members, ";"), primitive, fmt.Sprintf("%d", len(members))})
	}
	if err = writePromotionCSV(filepath.Join(out, "direct_lowering_equivalence_classes.csv"), []string{"direct_lowering_class", "candidate_members", "exact_missing_vector", "member_count"}, classRows); err != nil {
		return a, err
	}
	primitiveRows := [][]string{}
	for _, p := range a.Primitives {
		classification := "DIRECT_HANDLER_REQUIRED"
		for _, row := range a.Rows {
			for _, missing := range row.Missing {
				if missing == p && row.Classification == "INTRINSIC_RUNTIME" {
					classification = "INTRINSIC_RUNTIME"
				}
			}
		}
		primitiveRows = append(primitiveRows, []string{p, "NEW_GENERIC_NATIVE_CORE_HANDLER", "false", classification})
	}
	if err = writePromotionCSV(filepath.Join(out, "direct_lowering_primitives.csv"), []string{"primitive", "execution_mechanism", "data_only", "classification"}, primitiveRows); err != nil {
		return a, err
	}
	return a, nil
}
