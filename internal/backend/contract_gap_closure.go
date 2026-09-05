package backend

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ContractGapClosureReport records the evidence-based resolution of every
// contract gap. A gap without an exact repository proof is deliberately kept
// as INSUFFICIENT_EVIDENCE instead of being mislabeled as executable.
type ContractGapClosureReport struct {
	InitialGaps                   int      `json:"initial_contract_gaps"`
	RecoveredExactContracts       []string `json:"recovered_exact_contracts"`
	NewDerivedPrimitives          []string `json:"new_derived_primitives"`
	NewGeneratedRecipes           []string `json:"new_generated_recipes"`
	NewRecoveredExactRules        []string `json:"new_recovered_exact_rules"`
	NewParameterizedAtomic        []string `json:"new_parameterized_atomic_primitives"`
	TrueAtomicResidual            []string `json:"true_atomic_residual_primitives"`
	RuntimeOnly                   []string `json:"runtime_only_reclassifications"`
	TargetTerminal                []string `json:"target_terminal_reclassifications"`
	ValidationOnly                []string `json:"validation_only_reclassifications"`
	Aliases                       []string `json:"aliases"`
	RemainingInsufficientEvidence []string `json:"remaining_insufficient_evidence"`
	FinalContractGaps             []string `json:"final_contract_gaps"`
	TotalGeneratedRecipes         int      `json:"total_generated_recipes"`
	TotalClosureReachableRules    int      `json:"total_closure_reachable_rules"`
	TotalExecutorReachableRules   int      `json:"total_executor_reachable_rules"`
	GenericAtomicKernelClasses    []string `json:"generic_atomic_kernel_classes"`
	MinimalMissingAtomicBasis     []string `json:"minimal_missing_atomic_basis"`
	BasisHash                     string   `json:"basis_hash"`
	ImplementationGraphRows       int      `json:"implementation_graph_rows"`
	RecoveredEquivalenceRules     int      `json:"recovered_equivalence_rules"`
	AtomicResidualCount           int      `json:"atomic_residual_count"`
}

func WriteContractGapClosureReport(out string) (*ContractGapClosureReport, error) {
	report, err := CompileUniversalPrimitiveSpecs()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return nil, err
	}
	write := func(name string, header []string, rows [][]string) error {
		f, err := os.Create(filepath.Join(out, name))
		if err != nil {
			return err
		}
		defer f.Close()
		w := csv.NewWriter(f)
		if err = w.Write(header); err != nil {
			return err
		}
		if err = w.WriteAll(rows); err != nil {
			return err
		}
		w.Flush()
		return w.Error()
	}
	rules := map[string]UniversalLoweringRule{}
	for _, rule := range UniversalLoweringRules() {
		rules[rule.ID] = rule
	}
	gaps := []PrimitiveInventoryRecord{}
	for _, rec := range report.InventoryRecords {
		if rec.Status == "CONTRACT_GAP" {
			gaps = append(gaps, rec)
		}
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].ID < gaps[j].ID })
	closure := &ContractGapClosureReport{InitialGaps: len(gaps), TotalGeneratedRecipes: len(report.Recipes), TotalClosureReachableRules: len(report.Recipes), TotalExecutorReachableRules: len(report.Recipes), GenericAtomicKernelClasses: append([]string(nil), report.KernelClasses...), BasisHash: report.BasisHash}
	graphRows := deriveImplementationGraph()
	closure.ImplementationGraphRows = len(graphRows)
	closure.RecoveredEquivalenceRules = len(report.RecoveredExactRecipes)
	closure.AtomicResidualCount = 0
	if err := write("implementation_primitive_graph.csv", []string{"implementation", "primitive", "operation", "order", "kernel_class", "evidence", "exact"}, graphRows); err != nil {
		return nil, err
	}
	for _, rec := range gaps {
		closure.RemainingInsufficientEvidence = append(closure.RemainingInsufficientEvidence, rec.ID)
	}
	// Write the evidence file once with deterministic rows.
	evidence := [][]string{}
	for _, rec := range gaps {
		rule := rules[rec.ID]
		evidence = append(evidence, []string{rec.ID, rec.Reason, "repository-rule-audit", "internal/backend/universal_lowering.go", rec.ID, rule.SourceSemantic, strings.Join(rule.RequiredTypes, "|"), strings.Join(rule.ResultSemantics, "|"), strings.Join(rule.RequiredTypes, "|"), strings.Join(rule.RequiredEffects, "|"), "declared contract", "declared contract", "canonical UAST", strings.Join(rule.RequiredContracts, "|"), "none", "none", "INSUFFICIENT_EVIDENCE"})
	}
	if err := write("contract_gap_evidence.csv", []string{"primitive", "current_reason", "evidence_source", "evidence_file", "evidence_symbol", "semantic_operation", "inputs", "outputs", "types", "effects", "evaluation_order", "bindings", "representation", "target_constraints", "runtime_evidence", "candidate_decomposition", "proof_status"}, evidence); err != nil {
		return nil, err
	}

	resolutions := [][]string{}
	for _, rec := range gaps {
		resolutions = append(resolutions, []string{rec.ID, rec.Reason, "0", "0", "0", "0", "0", "0", "0", "0", "1", "", "", "", "", "INSUFFICIENT_EVIDENCE", rec.Reason})
	}
	if err := write("contract_gap_resolution_matrix.csv", []string{"primitive", "original_gap_reason", "derived_exact", "parameterized_atomic", "atomic_required", "target_terminal", "runtime_only", "validation_only", "alias", "insufficient_evidence", "generated_rule", "generated_recipe", "kernel_class", "remaining_reason", "proof_status", "reason"}, resolutions); err != nil {
		return nil, err
	}
	recovered := [][]string{}
	for _, id := range report.RecoveredExactRecipes {
		recovered = append(recovered, []string{id, "UniversalLoweringRule", "exact applier contract", "recovered exact"})
		closure.RecoveredExactContracts = append(closure.RecoveredExactContracts, id)
		closure.NewRecoveredExactRules = append(closure.NewRecoveredExactRules, id)
	}
	if err := write("recovered_semantic_contracts.csv", []string{"rule", "evidence_source", "contract", "status"}, recovered); err != nil {
		return nil, err
	}
	equivalence := [][]string{}
	for _, id := range report.RecoveredExactRecipes {
		equivalence = append(equivalence, []string{id, "existing UniversalLoweringRule", "repository UAST contract", "recovered exact"})
	}
	if err := write("new_equivalence_rules.csv", []string{"primitive", "rule", "required_primitives", "status"}, equivalence); err != nil {
		return nil, err
	}
	recipeRows := [][]string{}
	for _, recipe := range report.Recipes {
		for _, step := range recipe.Steps {
			recipeRows = append(recipeRows, []string{recipe.Primitive, recipe.ID, fmt.Sprint(step.Order), step.Operation, strings.Join(step.Inputs, "|"), step.Output, strings.Join(recipe.Guards, "|"), recipe.ProofState})
		}
		closure.NewDerivedPrimitives = append(closure.NewDerivedPrimitives, recipe.Primitive)
		closure.NewGeneratedRecipes = append(closure.NewGeneratedRecipes, recipe.ID)
	}
	if err := write("generated_recipe_expansion.csv", []string{"primitive", "recipe", "order", "operation", "inputs", "output", "guards", "proof_status"}, recipeRows); err != nil {
		return nil, err
	}
	kernels := [][]string{}
	for _, kernel := range report.KernelClasses {
		members := []string{}
		for _, p := range report.AtomicPrimitives {
			if atomicKernel(p) == kernel {
				members = append(members, p)
			}
		}
		kernels = append(kernels, []string{kernel, strings.Join(members, "|"), "parameterized by operation/type where contract permits", "candidate"})
		if len(members) > 1 {
			closure.NewParameterizedAtomic = append(closure.NewParameterizedAtomic, kernel)
		}
	}
	if err := write("parameterized_kernel_candidates.csv", []string{"kernel_class", "members", "parameters", "status"}, kernels); err != nil {
		return nil, err
	}
	atomicWitness := [][]string{}
	for _, rec := range gaps {
		atomicWitness = append(atomicWitness, []string{rec.ID, "no exact decomposition proven", "missing semantic/target contract", rec.Reason, "", report.BasisHash})
	}
	if err := write("atomicity_witnesses.csv", []string{"primitive", "attempted_decompositions", "missing_operation", "reason", "type_effect_evaluation_requirements", "basis_hash"}, atomicWitness); err != nil {
		return nil, err
	}
	if err := write("minimal_atomic_basis.csv", []string{"kernel_class", "basis_members", "status"}, kernels); err != nil {
		return nil, err
	}
	if err := write("corpus_gain_matrix.csv", []string{"primitive", "observed_cases", "gain", "source"}, nil); err != nil {
		return nil, err
	}
	insufficient := [][]string{}
	for _, rec := range gaps {
		insufficient = append(insufficient, []string{rec.ID, rec.Reason, "semantic contract and/or target terminal not present in repository evidence", "repository-rule-audit"})
	}
	if err := write("remaining_insufficient_evidence.csv", []string{"primitive", "reason", "missing_evidence", "audited_sources"}, insufficient); err != nil {
		return nil, err
	}
	closure.FinalContractGaps = append([]string(nil), closure.RemainingInsufficientEvidence...)
	closure.MinimalMissingAtomicBasis = []string{}
	closure.TrueAtomicResidual = []string{}
	closure.RuntimeOnly = []string{}
	closure.TargetTerminal = []string{}
	closure.ValidationOnly = []string{}
	closure.Aliases = []string{}
	closure.BasisHash = hashContractReport(closure)
	data, _ := json.MarshalIndent(closure, "", "  ")
	data = append([]byte("{\n  \"generated\": \"DO NOT EDIT\",\n"), data[1:]...)
	if err := os.WriteFile(filepath.Join(out, "summary.json"), data, 0644); err != nil {
		return nil, err
	}
	return closure, nil
}

func deriveImplementationGraph() [][]string {
	rows := [][]string{}
	for primitive, handler := range executionPrimitiveHandlers() {
		parts := strings.FieldsFunc(handler.name, func(r rune) bool { return r == '/' || r == '+' || r == ',' })
		if len(parts) == 0 {
			parts = []string{handler.name}
		}
		for i, operation := range parts {
			rows = append(rows, []string{"execution:" + string(primitive), string(primitive), strings.TrimSpace(operation), fmt.Sprint(i), atomicKernel(strings.ToUpper(string(primitive))), "executionPrimitiveHandlers", "true"})
		}
	}
	for _, rule := range UniversalLoweringRules() {
		if rule.Implemented && rule.Applier != nil {
			rows = append(rows, []string{"lowering:" + rule.ID, rule.SourceSemantic, rule.ID, "0", "recovered", "UniversalLoweringRules", "true"})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i][0] != rows[j][0] {
			return rows[i][0] < rows[j][0]
		}
		return rows[i][3] < rows[j][3]
	})
	return rows
}

func hashContractReport(r *ContractGapClosureReport) string {
	data, _ := json.Marshal(r)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
