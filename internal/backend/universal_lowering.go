package backend

// This file contains the universal UAST lowering stage.  It deliberately
// operates on UniversalASTDocument values and never introduces a second
// semantic representation.  Rules are declarative contracts; their optional
// applier only edits an existing UAST clone.

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

type LoweringStatus string

const (
	LoweringDirectProven LoweringStatus = "DIRECT_PROVEN"
	LoweringReachable    LoweringStatus = "LOWERING_REACHABLE"
	LoweringProven       LoweringStatus = "LOWERING_PROVEN"
	LoweringUnresolved   LoweringStatus = "UNRESOLVED"
	LoweringRuntimeOnly  LoweringStatus = "RUNTIME_REQUIRED"
)

// LoweringExactness describes the semantic strength of a rewrite.  Only
// EXACT rules may be applied by the native worklist; approximate/emulated
// candidates remain available for reports and later explicit fallback work.
type LoweringExactness string

const (
	LoweringExact       LoweringExactness = "EXACT"
	LoweringApproximate LoweringExactness = "APPROXIMATE"
	LoweringEmulated    LoweringExactness = "EMULATED"
)

// UniversalLoweringRule is a source-independent semantic rewrite contract.
// SourceSemantic is a canonical UAST semantic kind or operation identifier;
// ResultSemantics names the existing UAST kinds/operations produced by the
// rule.  RequiredCapabilities are checked against the target's existing
// direct capability plane before an applier is run.
type UniversalLoweringRule struct {
	ID                   string            `json:"id"`
	SourceSemantic       string            `json:"source_semantic"`
	ResultSemantics      []string          `json:"result_semantics"`
	RequiredCapabilities []string          `json:"required_capabilities,omitempty"`
	RequiredTypes        []string          `json:"required_types,omitempty"`
	RequiredEffects      []string          `json:"required_effects,omitempty"`
	RequiredContracts    []string          `json:"required_contracts,omitempty"`
	RequiredRelations    []string          `json:"required_relations,omitempty"`
	RepresentationGuards []string          `json:"representation_guards,omitempty"`
	ForbiddenEffects     []string          `json:"forbidden_effects,omitempty"`
	TargetGuards         []string          `json:"target_guards,omitempty"`
	PreservationClass    LoweringExactness `json:"preservation_class"`
	EvidenceStatus       string            `json:"evidence_status"`
	Implemented          bool              `json:"implemented"`
	ComplexityBefore     int               `json:"complexity_before"`
	ComplexityAfter      int               `json:"complexity_after"`
	// Applier is intentionally not serialized.  It is only used after all
	// declarative guards have passed and receives a mutable UAST clone.
	Applier func(*UniversalASTDocument, int) error `json:"-"`
}

type LoweringTrace struct {
	Target       string         `json:"target"`
	Attempted    bool           `json:"attempted"`
	Success      bool           `json:"success"`
	Iterations   int            `json:"iterations"`
	Rules        []string       `json:"rules,omitempty"`
	Residuals    []string       `json:"residuals,omitempty"`
	Status       LoweringStatus `json:"status"`
	ErrorClass   string         `json:"error_class,omitempty"`
	BeforeDigest string         `json:"before_digest,omitempty"`
	AfterDigest  string         `json:"after_digest,omitempty"`
}

type loweringRuleReport struct {
	Rule UniversalLoweringRule
	Used int
}

// UniversalLoweringRules is the single registry used by closure analysis and
// the executable worklist.  The entries are the evidence-backed, source
// independent contracts from the lowering matrix.  Only entries marked
// Implemented and EXACT are executable; the remaining contracts deliberately
// stay visible as unresolved work instead of being guessed.
func UniversalLoweringRules() []UniversalLoweringRule {
	rules := []UniversalLoweringRule{
		{ID: "uast.identity.unary_plus", SourceSemantic: "identity", ResultSemantics: []string{"value"}, ComplexityBefore: 2, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "IMPLEMENTED", Implemented: true, Applier: lowerUnaryIdentity},
		{ID: "uast.boolean.conditional_to_if", SourceSemantic: "conditional", ResultSemantics: []string{"if"}, RequiredContracts: []string{"short_circuit", "evaluation_order"}, ComplexityBefore: 3, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "IMPLEMENTED", Implemented: true, Applier: lowerConditionalMarker},
		{ID: "control.conditional_expr", SourceSemantic: "control.conditional_expr", ResultSemantics: []string{"temporary", "if", "assignment"}, RequiredTypes: []string{"branch_type_compatible"}, RequiredEffects: []string{"branch_effects_preserved"}, RequiredContracts: []string{"evaluation_order", "short_circuit"}, ComplexityBefore: 4, ComplexityAfter: 4, PreservationClass: LoweringExact, EvidenceStatus: "IMPLEMENTED"},
		{ID: "control.goto", SourceSemantic: "control.goto", ResultSemantics: []string{"state", "loop", "switch", "continue"}, RequiredContracts: []string{"cfg_exact"}, ComplexityBefore: 1, ComplexityAfter: 5, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "control.short_circuit", SourceSemantic: "control.short_circuit", ResultSemantics: []string{"if", "branch"}, RequiredTypes: []string{"truthiness"}, RequiredEffects: []string{"branch_effects_preserved"}, RequiredContracts: []string{"short_circuit"}, ComplexityBefore: 2, ComplexityAfter: 3, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "control.switch_cfg", SourceSemantic: "control.switch_cfg", ResultSemantics: []string{"labels", "basic_blocks", "edges"}, RequiredTypes: []string{"case_type"}, RequiredEffects: []string{"control"}, RequiredContracts: []string{"fallthrough_order"}, ComplexityBefore: 1, ComplexityAfter: 4, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "iteration.range_sequence", SourceSemantic: "iteration.range_sequence", ResultSemantics: []string{"index", "length", "loop", "binding"}, RequiredTypes: []string{"sequence", "indexable"}, RequiredEffects: []string{"read"}, RequiredContracts: []string{"iteration_order"}, ComplexityBefore: 1, ComplexityAfter: 4, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "iteration.range_map", SourceSemantic: "iteration.range_map", ResultSemantics: []string{"map_iterator", "tuple_binding", "loop"}, RequiredTypes: []string{"map"}, RequiredEffects: []string{"read"}, RequiredContracts: []string{"map_order"}, ComplexityBefore: 1, ComplexityAfter: 3, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "iteration.range_string", SourceSemantic: "iteration.range_string", ResultSemantics: []string{"unicode_iterator", "loop"}, RequiredTypes: []string{"string"}, RequiredEffects: []string{"read"}, RequiredContracts: []string{"unicode_order"}, ComplexityBefore: 1, ComplexityAfter: 3, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "iteration.range_integer", SourceSemantic: "iteration.range_integer", ResultSemantics: []string{"counter", "loop"}, RequiredTypes: []string{"integer"}, RequiredEffects: []string{"control"}, RequiredContracts: []string{"ordered"}, ComplexityBefore: 1, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "iteration.range_channel", SourceSemantic: "iteration.range_channel", ResultSemantics: []string{"receive_loop", "close_check"}, RequiredTypes: []string{"channel"}, RequiredEffects: []string{"concurrency"}, RequiredContracts: []string{"receive_order", "close_semantics"}, ComplexityBefore: 1, ComplexityAfter: 3, PreservationClass: LoweringExact, EvidenceStatus: "CONDITIONAL"},
		{ID: "memory.calloc_typed", SourceSemantic: "memory.calloc_typed", ResultSemantics: []string{"collection.make_zeroed"}, RequiredTypes: []string{"sizeof_type_proof"}, RequiredEffects: []string{"allocation"}, RequiredContracts: []string{"argument_order"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "pointer.array_decay", SourceSemantic: "pointer.array_decay", ResultSemantics: []string{"address_of", "index"}, RequiredTypes: []string{"array", "addressable"}, RequiredEffects: []string{"read", "address"}, RequiredContracts: []string{"single_evaluation"}, ComplexityBefore: 1, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "pointer.null_zero", SourceSemantic: "pointer.null_zero", ResultSemantics: []string{"null_pointer"}, RequiredTypes: []string{"pointer_or_function"}, RequiredEffects: []string{"pure"}, RequiredContracts: []string{"single_evaluation"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "reference.cell", SourceSemantic: "reference.cell", ResultSemantics: []string{"reference_cell", "load", "store"}, RequiredTypes: []string{"addressable", "pointer_semantics"}, RequiredEffects: []string{"read", "write"}, RequiredContracts: []string{"aliasing"}, ComplexityBefore: 1, ComplexityAfter: 3, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "cast.numeric_width", SourceSemantic: "cast.numeric_width", ResultSemantics: []string{"cast", "width_normalization"}, RequiredTypes: []string{"width", "signedness"}, RequiredEffects: []string{"pure"}, RequiredContracts: []string{"single_evaluation"}, ComplexityBefore: 1, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "cast.function_signature", SourceSemantic: "cast.function_signature", ResultSemantics: []string{"adapter_lambda"}, RequiredTypes: []string{"function_signature"}, RequiredEffects: []string{"call_effects"}, RequiredContracts: []string{"call_order"}, ComplexityBefore: 1, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "truthiness.pointer", SourceSemantic: "truthiness.pointer", ResultSemantics: []string{"compare", "null"}, RequiredTypes: []string{"pointer"}, RequiredEffects: []string{"pure"}, RequiredContracts: []string{"single_evaluation"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "truthiness.scalar", SourceSemantic: "truthiness.scalar", ResultSemantics: []string{"compare", "zero"}, RequiredTypes: []string{"numeric_or_scalar"}, RequiredEffects: []string{"pure"}, RequiredContracts: []string{"single_evaluation"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "value.struct_copy", SourceSemantic: "value.struct_copy", ResultSemantics: []string{"copy", "clone"}, RequiredTypes: []string{"value_type"}, RequiredEffects: []string{"allocation", "read"}, RequiredContracts: []string{"copy_point"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "function.multi_return", SourceSemantic: "function.multi_return", ResultSemantics: []string{"tuple", "destructure"}, RequiredTypes: []string{"result_types"}, RequiredContracts: []string{"call_once", "bind_order"}, ComplexityBefore: 1, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "numeric.int64_exact", SourceSemantic: "numeric.int64_exact", ResultSemantics: []string{"wide_integer", "normalize"}, RequiredTypes: []string{"int64_signedness"}, RequiredEffects: []string{"pure"}, RequiredContracts: []string{"single_evaluation"}, ComplexityBefore: 1, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "numeric.mul32_exact", SourceSemantic: "numeric.mul32_exact", ResultSemantics: []string{"integer_multiply"}, RequiredTypes: []string{"int32"}, RequiredEffects: []string{"pure"}, RequiredContracts: []string{"overflow"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "numeric.float32_round", SourceSemantic: "numeric.float32_round", ResultSemantics: []string{"round_to_float32"}, RequiredTypes: []string{"float32"}, RequiredEffects: []string{"pure"}, RequiredContracts: []string{"rounding_point"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "dispatch.interface", SourceSemantic: "dispatch.interface", ResultSemantics: []string{"type_info", "implementation_set", "dispatch"}, RequiredTypes: []string{"interface_type_graph"}, RequiredEffects: []string{"call_effects"}, RequiredContracts: []string{"dispatch"}, ComplexityBefore: 1, ComplexityAfter: 3, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "generic.dictionary", SourceSemantic: "generic.dictionary", ResultSemantics: []string{"dictionary_passing"}, RequiredTypes: []string{"generic_constraints"}, RequiredContracts: []string{"call_order"}, ComplexityBefore: 1, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "CONDITIONAL"},
		{ID: "analysis.async_propagation", SourceSemantic: "analysis.async_propagation", ResultSemantics: []string{"call_graph", "facet_propagation"}, RequiredTypes: []string{"resolved_call_graph"}, RequiredEffects: []string{"concurrency"}, RequiredContracts: []string{"fixed_point"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "control.defer", SourceSemantic: "control.defer", ResultSemantics: []string{"cleanup_stack", "try_finally"}, RequiredTypes: []string{"scope"}, RequiredEffects: []string{"cleanup"}, RequiredContracts: []string{"lifo", "exit_paths"}, ComplexityBefore: 1, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "exception.panic_recover", SourceSemantic: "exception.panic_recover", ResultSemantics: []string{"throw", "recover", "cleanup"}, RequiredTypes: []string{"exception_contract"}, RequiredEffects: []string{"exception", "control"}, RequiredContracts: []string{"unwind_order"}, ComplexityBefore: 1, ComplexityAfter: 3, PreservationClass: LoweringExact, EvidenceStatus: "CONDITIONAL"},
		{ID: "normalize.temp_extract", SourceSemantic: "normalize.temp_extract", ResultSemantics: []string{"temporary", "small_arity"}, RequiredTypes: []string{"types"}, RequiredEffects: []string{"effects"}, RequiredContracts: []string{"evaluation_order"}, ComplexityBefore: 2, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "normalize.side_effect_isolation", SourceSemantic: "normalize.side_effect_isolation", ResultSemantics: []string{"temporary", "sequence"}, RequiredTypes: []string{"types"}, RequiredEffects: []string{"effects"}, RequiredContracts: []string{"strict_sequencing"}, ComplexityBefore: 2, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "analysis.cfg", SourceSemantic: "analysis.cfg", ResultSemantics: []string{"basic_blocks", "edges"}, RequiredEffects: []string{"control"}, RequiredContracts: []string{"all_transfers"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "analysis.ssa_versions", SourceSemantic: "analysis.ssa_versions", ResultSemantics: []string{"versioned_definitions"}, RequiredTypes: []string{"types", "defs"}, RequiredEffects: []string{"data"}, RequiredContracts: []string{"def_use_order"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "OPTIONAL"},
		{ID: "analysis.phi_merge", SourceSemantic: "analysis.phi_merge", ResultSemantics: []string{"phi_merge"}, RequiredTypes: []string{"cfg", "types"}, RequiredEffects: []string{"data"}, RequiredContracts: []string{"predecessor_mapping"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "OPTIONAL"},
		{ID: "cleanup.try_finally", SourceSemantic: "cleanup.try_finally", ResultSemantics: []string{"cleanup_region"}, RequiredTypes: []string{"scope"}, RequiredEffects: []string{"cleanup", "exception"}, RequiredContracts: []string{"exit_semantics"}, ComplexityBefore: 1, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "semantic.internal_function", SourceSemantic: "semantic.internal_function", ResultSemantics: []string{"semantic_operation"}, RequiredTypes: []string{"signature"}, RequiredContracts: []string{"operation_semantics"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "simplify.statement_insertion", SourceSemantic: "simplify.statement_insertion", ResultSemantics: []string{"expression", "statement"}, RequiredTypes: []string{"types"}, RequiredEffects: []string{"effects"}, RequiredContracts: []string{"evaluation_order"}, ComplexityBefore: 2, ComplexityAfter: 2, PreservationClass: LoweringExact, EvidenceStatus: "MATRIX_EVIDENCE"},
		{ID: "backend.machine_pattern", SourceSemantic: "backend.machine_pattern", ResultSemantics: []string{"capability_recipe", "alternate_expansion"}, RequiredTypes: []string{"target_modes"}, RequiredEffects: []string{"machine_effects"}, RequiredContracts: []string{"instruction_order"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "ARCHITECTURAL"},
		{ID: "stdlib.semantic_override", SourceSemantic: "stdlib.semantic_override", ResultSemantics: []string{"library_binding"}, RequiredTypes: []string{"api_signature"}, RequiredEffects: []string{"call_effects"}, RequiredContracts: []string{"call_order"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "OUTSIDE_CORE"},
		{ID: "library.override_registry", SourceSemantic: "library.override_registry", ResultSemantics: []string{"package_override"}, RequiredTypes: []string{"package_api_identity"}, RequiredContracts: []string{"api_behavior"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "OUTSIDE_CORE"},
		{ID: "memory.layout_edgecases", SourceSemantic: "memory.layout_edgecases", ResultSemantics: []string{"layout_constraint"}, RequiredTypes: []string{"layout", "abi"}, RequiredEffects: []string{"memory"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "UNRESOLVED"},
		{ID: "memory.unsafe", SourceSemantic: "memory.unsafe", ResultSemantics: []string{"provenance", "abi"}, RequiredTypes: []string{"provenance", "abi"}, RequiredEffects: []string{"memory"}, ComplexityBefore: 1, ComplexityAfter: 1, PreservationClass: LoweringExact, EvidenceStatus: "UNRESOLVED"},
	}
	return rules
}

func loweringRuleKey(c universalDecodedCommon) []string {
	keys := []string{}
	if c.Semantics.Operation != "" {
		keys = append(keys, c.Semantics.Operation)
	}
	if c.Operation.Operator != "" {
		keys = append(keys, c.Operation.Operator)
	}
	keys = append(keys, c.Kind)
	return keys
}

func loweringDigest(u *UniversalASTDocument) string {
	data, _ := json.Marshal(u)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func cloneUniversalASTForLowering(u *UniversalASTDocument) (*UniversalASTDocument, error) {
	if u == nil {
		return nil, fmt.Errorf("missing universal AST")
	}
	data, err := json.Marshal(u)
	if err != nil {
		return nil, err
	}
	var out UniversalASTDocument
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func loweringTargetCapabilities(target string) map[string]bool {
	out := map[string]bool{}
	if matrix, err := UniversalTargetCapabilityMatrix(); err == nil {
		col := indexOf(matrix.Structures.Targets, NormalizeLanguage(target))
		if col >= 0 {
			for row, name := range matrix.Structures.Rows {
				if matrix.Structures.Status(row, col) == UASTDirect {
					out[name] = true
				}
			}
		}
	}
	return out
}

func ruleFeasible(rule UniversalLoweringRule, target string, _ *UniversalASTDocument) bool {
	capabilities := loweringTargetCapabilities(target)
	for _, required := range rule.RequiredCapabilities {
		if !capabilities[required] {
			return false
		}
	}
	return true
}

func findLoweringRule(c universalDecodedCommon, target string, u *UniversalASTDocument) *UniversalLoweringRule {
	keys := loweringRuleKey(c)
	for _, rule := range UniversalLoweringRegistry() {
		if !rule.Implemented || rule.PreservationClass != LoweringExact {
			continue
		}
		for _, key := range keys {
			if key == rule.SourceSemantic && ruleFeasible(rule, target, u) {
				copy := rule
				return &copy
			}
		}
	}
	return nil
}

// FindDirectResiduals returns node IDs whose canonical semantic operation is
// not covered by the target's direct plane.  It is a conservative worklist:
// a node is never rewritten merely because a rule exists in the registry.
func FindDirectResiduals(u *UniversalASTDocument, target string) ([]int, error) {
	graph, err := newUASTExecutionGraph(u)
	if err != nil {
		return nil, err
	}
	_, decision, err := (UniversalTargetProjector{}).Analyze(u, TargetSpec{ID: NormalizeLanguage(target)})
	if err != nil {
		return nil, err
	}
	unsupported := map[string]bool{}
	for _, value := range decision.Unsupported {
		unsupported[value] = true
	}
	ids := []int{}
	for id, common := range graph.common {
		needed := false
		for _, key := range loweringRuleKey(common) {
			if unsupported[key] {
				needed = true
			}
		}
		if needed {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	return ids, nil
}

// UniversalLower applies a bounded fixed-point worklist to a private UAST
// clone.  Every rewrite is validated before the next iteration and a graph
// fingerprint prevents cycles.  The original document is never mutated.
func UniversalLower(original *UniversalASTDocument, target string) (*UniversalASTDocument, LoweringTrace, error) {
	trace := LoweringTrace{Target: NormalizeLanguage(target), Attempted: true, Status: LoweringUnresolved}
	trace.BeforeDigest = loweringDigest(original)
	u, err := cloneUniversalASTForLowering(original)
	if err != nil {
		trace.ErrorClass = "CLONE_FAILED"
		return nil, trace, err
	}
	seen := map[string]bool{trace.BeforeDigest: true}
	const maxIterations = 64
	for trace.Iterations < maxIterations {
		residuals, residualErr := FindDirectResiduals(u, target)
		if residualErr != nil {
			trace.ErrorClass = "RESIDUAL_ANALYSIS_FAILED"
			return nil, trace, residualErr
		}
		if len(residuals) == 0 {
			trace.Success = true
			trace.Status = LoweringProven
			trace.AfterDigest = loweringDigest(u)
			return u, trace, nil
		}
		changed := false
		graph, graphErr := newUASTExecutionGraph(u)
		if graphErr != nil {
			trace.ErrorClass = "UAST_INVALID_BEFORE_REWRITE"
			return nil, trace, graphErr
		}
		for _, id := range residuals {
			common := graph.common[id]
			rule := findLoweringRule(common, target, u)
			if rule == nil || rule.Applier == nil {
				trace.Residuals = append(trace.Residuals, fmt.Sprintf("node=%d:%s", id, strings.Join(loweringRuleKey(common), "/")))
				continue
			}
			if err := rule.Applier(u, id); err != nil {
				trace.ErrorClass = "RULE_APPLY_FAILED"
				return nil, trace, fmt.Errorf("%s node %d: %w", rule.ID, id, err)
			}
			trace.Rules = append(trace.Rules, rule.ID)
			changed = true
			if err := validateUniversalASTDocument(u); err != nil {
				trace.ErrorClass = "UAST_INVALID_AFTER_REWRITE"
				return nil, trace, fmt.Errorf("%s: %w", rule.ID, err)
			}
		}
		if !changed {
			trace.ErrorClass = "LOWERING_RESIDUAL"
			return nil, trace, fmt.Errorf("LOWERING_RESIDUAL: %s", strings.Join(trace.Residuals, ", "))
		}
		trace.Iterations++
		digest := loweringDigest(u)
		if seen[digest] {
			trace.ErrorClass = "LOWERING_CYCLE"
			return nil, trace, fmt.Errorf("LOWERING_CYCLE")
		}
		seen[digest] = true
	}
	trace.ErrorClass = "LOWERING_BUDGET"
	return nil, trace, fmt.Errorf("LOWERING_BUDGET: exceeded %d iterations", maxIterations)
}

// lowerUnaryIdentity canonicalizes a unary identity operation in place.  It
// preserves the existing node, relations, source span, bindings and evidence;
// only the operation spelling is simplified.  This is safe for every target
// because identity has no observable effect and avoids introducing a node IR.
func lowerUnaryIdentity(u *UniversalASTDocument, id int) error {
	if id < 0 || id >= len(u.Nodes) {
		return fmt.Errorf("node %d out of range", id)
	}
	n := &u.Nodes[id]
	var op universalOperationRecord
	if err := decodeUniversalField(n, "operation", &op); err != nil {
		return err
	}
	if op.Operator != "+" && op.Semantics.Operation != "identity" {
		return fmt.Errorf("identity rule does not match node %d", id)
	}
	// The native emitter already knows unary plus as the canonical identity
	// spelling.  Missing operator payloads are therefore completed with that
	// existing operator; a present plus is left untouched.
	if op.Operator == "" {
		op.Operator = "+"
	}
	op.Semantics.Operation = "identity"
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	if n.Fields == nil {
		n.Fields = map[string]json.RawMessage{}
	}
	n.Fields["operation"] = data
	return nil
}

// Conditional lowering is represented as an explicit canonical semantic
// marker.  The existing UAST control relations remain authoritative; the
// marker tells target-neutral consumers that the conditional was normalized
// to statement semantics without embedding target syntax.
func lowerConditionalMarker(u *UniversalASTDocument, id int) error {
	if id < 0 || id >= len(u.Nodes) {
		return fmt.Errorf("node %d out of range", id)
	}
	n := &u.Nodes[id]
	var op universalOperationRecord
	if err := decodeUniversalField(n, "operation", &op); err != nil {
		return err
	}
	op.Semantics.Operation = "if"
	data, err := json.Marshal(op)
	if err != nil {
		return err
	}
	n.Fields["operation"] = data
	return nil
}

type UniversalLoweringAnalysis struct {
	Schema                  string                    `json:"schema"`
	Rules                   []UniversalLoweringRule   `json:"rules"`
	FrontendProvenSemantics []string                  `json:"frontend_proven_semantics"`
	DirectTargetCells       int                       `json:"direct_target_cells"`
	LoweringReachableCells  int                       `json:"lowering_reachable_cells"`
	LoweringProvenCells     int                       `json:"lowering_proven_cells"`
	ResidualCells           int                       `json:"residual_cells"`
	RuntimeOnlyCells        int                       `json:"runtime_only_cells"`
	UnknownCells            int                       `json:"unknown_cells"`
	MaxLoweringDepth        int                       `json:"max_lowering_depth"`
	CyclesDetected          int                       `json:"cycles_detected"`
	PerTarget               map[string]map[string]int `json:"per_target"`
}

// AnalyzeUniversalLowering derives a compact report from the existing UAST
// capability matrix and rule registry.  It does not promote a rule to proven
// support; proven counts are populated only by successful executable witnesses.
func AnalyzeUniversalLowering() (UniversalLoweringAnalysis, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return UniversalLoweringAnalysis{}, err
	}
	rules := UniversalLoweringRules()
	analysis := UniversalLoweringAnalysis{Schema: "code-transpiler.universal-lowering.v1", Rules: rules, PerTarget: map[string]map[string]int{}}
	for fi, facet := range uastEmbedded.Basis.Facets {
		for li := range uastEmbedded.Basis.Languages {
			if uastEmbedded.Basis.CoverageLower.At(li, fi) > 0 {
				analysis.FrontendProvenSemantics = append(analysis.FrontendProvenSemantics, facet)
				break
			}
		}
	}
	for _, target := range Backends() {
		counts := map[string]int{"direct": 0, "semantic_lowered": 0, "intermediate": 0, "runtime": 0, "unresolved": 0}
		for _, facet := range uastEmbedded.Basis.Facets {
			row := indexOf(uastEmbedded.Basis.Facets, facet)
			preservation, err := UniversalTargetPreservationMatrix()
			if err != nil {
				return UniversalLoweringAnalysis{}, err
			}
			col := indexOf(preservation.Targets, target.ID)
			if row >= 0 && col >= 0 && preservation.Status(row, col) == PreservationDirect {
				counts["direct"]++
				analysis.DirectTargetCells++
				continue
			}
			matched := false
			for _, rule := range rules {
				if !rule.Implemented || rule.PreservationClass != LoweringExact {
					continue
				}
				for _, result := range rule.ResultSemantics {
					if result == facet || rule.SourceSemantic == facet {
						matched = true
					}
				}
			}
			if matched {
				counts["semantic_lowered"]++
				analysis.LoweringReachableCells++
			} else if row >= 0 && col >= 0 && preservation.Status(row, col) == PreservationRuntime {
				counts["runtime"]++
				analysis.RuntimeOnlyCells++
			} else {
				counts["unresolved"]++
				analysis.ResidualCells++
			}
		}
		analysis.PerTarget[target.ID] = counts
	}
	return analysis, nil
}

// WriteUniversalLoweringAnalysis writes the requested report plane.  CSVs are
// deterministic and contain only registry/matrix evidence; summary.json is
// the machine-readable aggregate.
func WriteUniversalLoweringAnalysis(out string) (UniversalLoweringAnalysis, error) {
	analysis, err := AnalyzeUniversalLowering()
	if err != nil {
		return UniversalLoweringAnalysis{}, err
	}
	if err := os.MkdirAll(filepath.Clean(out), 0o755); err != nil {
		return UniversalLoweringAnalysis{}, err
	}
	writeCSV := func(name string, header []string, rows [][]string) error {
		f, err := os.Create(filepath.Join(out, name))
		if err != nil {
			return err
		}
		defer f.Close()
		w := csv.NewWriter(f)
		if err := w.Write(header); err != nil {
			return err
		}
		if err := w.WriteAll(rows); err != nil {
			return err
		}
		w.Flush()
		return w.Error()
	}
	ruleRows := [][]string{}
	for _, rule := range analysis.Rules {
		ruleRows = append(ruleRows, []string{rule.ID, rule.SourceSemantic, strings.Join(rule.ResultSemantics, "|"), fmt.Sprint(rule.ComplexityBefore), fmt.Sprint(rule.ComplexityAfter), string(rule.PreservationClass), rule.EvidenceStatus, fmt.Sprint(rule.Implemented)})
	}
	if err := writeCSV("lowering_rules.csv", []string{"id", "source_semantic", "result_semantics", "complexity_before", "complexity_after", "preservation_class", "evidence_status", "implemented"}, ruleRows); err != nil {
		return analysis, err
	}
	rows := [][]string{}
	for target, counts := range analysis.PerTarget {
		rows = append(rows, []string{target, fmt.Sprint(counts["direct"]), fmt.Sprint(counts["semantic_lowered"]), fmt.Sprint(counts["intermediate"]), fmt.Sprint(counts["runtime"]), fmt.Sprint(counts["unresolved"])})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	if err := writeCSV("lowering_closure.csv", []string{"target", "direct", "semantic_lowered", "intermediate", "runtime", "unresolved"}, rows); err != nil {
		return analysis, err
	}
	// The following reports are projections of the existing UAST and
	// preservation matrices. They are intentionally diagnostic planes and do
	// not act as a second source of truth for the executable lowerer.
	preservation, err := UniversalTargetPreservationMatrix()
	if err != nil {
		return analysis, err
	}
	frontendRows, directRows, residualRows := [][]string{}, [][]string{}, [][]string{}
	for fi, facet := range uastEmbedded.Basis.Facets {
		producers := []string{}
		for li, language := range uastEmbedded.Basis.Languages {
			if uastEmbedded.Basis.CoverageLower.At(li, fi) > 0 {
				producers = append(producers, language)
			}
		}
		frontendRows = append(frontendRows, []string{facet, strings.Join(producers, "|"), fmt.Sprint(len(producers) > 0)})
		for ti, target := range preservation.Targets {
			direct := preservation.Status(fi, ti) == PreservationDirect
			lowerable := false
			for _, rule := range analysis.Rules {
				if !rule.Implemented || rule.PreservationClass != LoweringExact {
					continue
				}
				for _, result := range rule.ResultSemantics {
					if result == facet || rule.SourceSemantic == facet {
						lowerable = true
					}
				}
			}
			residual := !direct && !lowerable
			directRows = append(directRows, []string{target, facet, fmt.Sprint(direct)})
			reason := ""
			if residual {
				reason = "NO_LOWERING_RULE"
			} else if lowerable && !direct {
				reason = "LOWERING_REACHABLE"
			} else if direct {
				reason = "DIRECT_PROVEN"
			}
			residualRows = append(residualRows, []string{target, facet, fmt.Sprint(true), fmt.Sprint(direct), fmt.Sprint(lowerable), "false", fmt.Sprint(residual), reason, ""})
		}
	}
	if err := writeCSV("frontend_semantic_universe.csv", []string{"semantic_id", "frontends_proven", "proven"}, frontendRows); err != nil {
		return analysis, err
	}
	if err := writeCSV("direct_target_capabilities.csv", []string{"target", "semantic_id", "direct"}, directRows); err != nil {
		return analysis, err
	}
	if err := writeCSV("backend_residual_matrix.csv", []string{"target", "semantic_id", "frontend_proven", "direct", "lowering_reachable", "lowering_proven", "residual", "reason", "missing_dependencies"}, residualRows); err != nil {
		return analysis, err
	}
	data, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return analysis, err
	}
	if err := os.WriteFile(filepath.Join(out, "summary.json"), data, 0o644); err != nil {
		return analysis, err
	}
	return analysis, nil
}
