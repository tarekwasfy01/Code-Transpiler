// Copyright (c) 2026 Tarek Wasfy
package backend

// csharpGo2CSEvidence is a source-backed contract catalogue.  It records
// reusable semantic obligations observed in go2cs without treating the
// converter's implementation strategy as C# language truth.  The frontend
// may attach these facts to the canonical UAST as provenance; the backend
// still requires the corresponding explicit contract before lowering.
type csharpGo2CSEvidence struct {
	ID                   string
	Family               string
	RequiredUASTFacts    []string
	RequiredBackendFacts []string
	Status               string
}

var csharpGo2CSEvidenceCatalog = []csharpGo2CSEvidence{
	{ID: "go2cs.slice-array-aliasing", Family: "aggregate", RequiredUASTFacts: []string{"element_type", "length", "capacity", "backing_storage", "aliasing"}, RequiredBackendFacts: []string{"aggregate_layout", "bounds", "storage"}, Status: "evidence_only"},
	{ID: "go2cs.map-runtime", Family: "aggregate", RequiredUASTFacts: []string{"key_type", "value_type", "lookup_effects", "nil_behavior"}, RequiredBackendFacts: []string{"map_runtime_contract"}, Status: "evidence_only"},
	{ID: "go2cs.pointer-escape-boxing", Family: "reference", RequiredUASTFacts: []string{"pointee_type", "addressability", "escape_lifetime"}, RequiredBackendFacts: []string{"reference", "lifetime", "allocation"}, Status: "evidence_only"},
	{ID: "go2cs.multiple-results-tuples", Family: "result", RequiredUASTFacts: []string{"result_elements", "result_order", "result_types"}, RequiredBackendFacts: []string{"result_abi", "aggregate_layout"}, Status: "evidence_only"},
	{ID: "go2cs.receiver-method-set", Family: "receiver", RequiredUASTFacts: []string{"receiver_type", "receiver_passing", "method_identity", "method_set"}, RequiredBackendFacts: []string{"receiver_abi", "dispatch"}, Status: "evidence_only"},
	{ID: "go2cs.function-values-closures", Family: "callable", RequiredUASTFacts: []string{"function_signature", "capture_bindings", "capture_storage", "environment_lifetime"}, RequiredBackendFacts: []string{"function_value_abi", "closure_environment"}, Status: "evidence_only"},
	{ID: "go2cs.variadic", Family: "callable", RequiredUASTFacts: []string{"fixed_parameters", "variadic_element_type", "argument_pack"}, RequiredBackendFacts: []string{"variadic_abi"}, Status: "evidence_only"},
	{ID: "go2cs.defer-panic-recover", Family: "control-effects", RequiredUASTFacts: []string{"defer_order", "panic_value", "recover_scope", "named_results"}, RequiredBackendFacts: []string{"cleanup", "exception_runtime"}, Status: "evidence_only"},
	{ID: "go2cs.goroutine-channel-select", Family: "concurrency", RequiredUASTFacts: []string{"spawn_callable", "channel_element_type", "send_receive_direction", "select_cases"}, RequiredBackendFacts: []string{"scheduler", "channel_runtime"}, Status: "evidence_only"},
	{ID: "go2cs.interface-structural-dispatch", Family: "dispatch", RequiredUASTFacts: []string{"method_set", "interface_identity", "satisfaction_relation", "type_assertion"}, RequiredBackendFacts: []string{"dispatch_table", "runtime_type_identity"}, Status: "evidence_only"},
	{ID: "go2cs.generics-constraints", Family: "generics", RequiredUASTFacts: []string{"type_parameters", "constraints", "underlying_type", "method_set"}, RequiredBackendFacts: []string{"generic_substitution", "constraint_check"}, Status: "evidence_only"},
	{ID: "go2cs.package-dependency-order", Family: "module", RequiredUASTFacts: []string{"module_identity", "imports", "dependency_order", "platform_selection"}, RequiredBackendFacts: []string{"module_linkage", "initializer_order"}, Status: "evidence_only"},
	{ID: "go2cs.platform-build-selection", Family: "target", RequiredUASTFacts: []string{"build_constraints", "target_os", "target_arch"}, RequiredBackendFacts: []string{"target_selection"}, Status: "evidence_only"},
	{ID: "cs2x.roslyn-to-portable-ir", Family: "projection", RequiredUASTFacts: []string{"bound_symbol_identity", "resolved_type", "source_span", "control_flow"}, RequiredBackendFacts: []string{"typed_operation_lowering", "target_layout"}, Status: "evidence_only"},
	{ID: "ts2cs.ast-normalization-stage", Family: "rewrite", RequiredUASTFacts: []string{"parent_child_edges", "source_order", "binding_identity", "evaluation_order"}, RequiredBackendFacts: []string{"structural_rewrite", "result_wiring"}, Status: "evidence_only"},
	{ID: "harmony.il-instruction-rewrite", Family: "rewrite", RequiredUASTFacts: []string{"instruction_opcode", "typed_operand", "label_target", "local_identity", "exception_region"}, RequiredBackendFacts: []string{"stack_effect", "branch_target", "exception_table"}, Status: "evidence_only"},
	{ID: "harmony.transpiler-composition", Family: "rewrite", RequiredUASTFacts: []string{"stable_instruction_identity", "rewrite_order", "preserved_labels", "preserved_exception_blocks"}, RequiredBackendFacts: []string{"deterministic_rewrite", "control_flow_validation"}, Status: "evidence_only"},
}

func attachCSharpGo2CSEvidence(f *FrontendSemanticFacts) {
	if f == nil {
		return
	}
	if f.Extensions == nil {
		f.Extensions = map[string]any{}
	}
	rows := make([]map[string]any, 0, len(csharpGo2CSEvidenceCatalog))
	for _, item := range csharpGo2CSEvidenceCatalog {
		rows = append(rows, map[string]any{
			"id":                     item.ID,
			"family":                 item.Family,
			"required_uast_facts":    append([]string(nil), item.RequiredUASTFacts...),
			"required_backend_facts": append([]string(nil), item.RequiredBackendFacts...),
			"status":                 item.Status,
			"provenance":             "SOURCE_EXPLICIT",
			"source":                 "https://github.com/ritchiecarroll/go2cs",
		})
	}
	f.Extensions["external_evidence.go2cs.v1"] = rows
}
