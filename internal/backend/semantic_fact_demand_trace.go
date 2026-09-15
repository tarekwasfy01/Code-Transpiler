// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"fmt"
	"strings"
)

// AuditFactProvenance is diagnostic evidence only. It is intentionally kept
// outside the lowering contracts: the audit observes the canonical UAST but
// never enriches, rewrites, or repairs it.
type AuditFactProvenance struct {
	Node      int    `json:"node"`
	Operation string `json:"operation"`
	Fact      string `json:"fact"`
	Source    string `json:"source_of_fact"`
	Resolved  bool   `json:"resolved"`
	Reachable bool   `json:"reachable"`
	Evidence  string `json:"evidence,omitempty"`
}

type AuditBackendFactDemand struct {
	Consumer           string `json:"consumer"`
	Node               int    `json:"node"`
	Operation          string `json:"operation"`
	RequestedFact      string `json:"requested_fact"`
	SourceOfFact       string `json:"source_of_fact"`
	Resolved           bool   `json:"resolved"`
	Reachable          bool   `json:"reachable"`
	RequiredForCodegen bool   `json:"required_for_codegen"`
}

const (
	AuditSourceExplicit  = "SOURCE_EXPLICIT"
	AuditFrontendDerived = "FRONTEND_DERIVED"
	AuditUASTExplicit    = "UAST_EXPLICIT"
	AuditUASTDerived     = "UAST_DERIVED"
	AuditABIIntent       = "ABI_INTENT"
	AuditTargetDerived   = "TARGET_DERIVED"
	AuditUnknown         = "UNKNOWN"
)

var auditFactAliases = map[string][]string{
	"callable_signature":           {"type_ref", "type_shape", "signature", "function_type"},
	"function_value_signature":     {"type_ref", "type_shape", "signature", "function_type"},
	"receiver_contract":            {"receiver", "receiver_type", "self", "this"},
	"multiple_results":             {"results", "result_types", "return_types"},
	"external_symbol_identity":     {"symbol", "linkage", "external_symbol"},
	"calling_convention":           {"calling_convention", "abi"},
	"argument_passing":             {"parameters", "arguments", "parameter_types", "pass_mode"},
	"aggregate_element_type":       {"element_type", "element", "type_ref"},
	"aggregate_layout_constraints": {"layout", "alignment", "offset", "field_order", "type_shape"},
	"dynamic_bounds":               {"bounds", "length", "dynamic_length", "shape"},
	"array_slice_distinction":      {"array", "slice", "view", "collection"},
	"width_signedness":             {"bits", "signed", "integer_width", "type_ref"},
	"overflow_policy":              {"overflow", "error_model", "semantics"},
	"ownership_borrowing":          {"ownership", "borrow", "aliasing", "pointer"},
	"lifetime_cleanup":             {"lifetime", "cleanup", "destructor", "defer"},
	"dispatch":                     {"dispatch", "virtual", "dynamic_dispatch"},
	"exceptions":                   {"exception", "throws", "handler", "error_model"},
	"synchronization":              {"atomic", "memory_order", "synchronization", "mutex"},
	"async":                        {"async", "await", "cancellation", "scheduler"},
	"module_declaration_identity":  {"module", "declaration", "symbol", "visibility"},
}

func auditNodeText(n UniversalASTNode) string {
	return strings.ToLower(n.StructuralKind + " " + strings.Join(n.SemanticFacets, " "))
}

func auditHasField(n UniversalASTNode, aliases []string) string {
	for _, alias := range aliases {
		for field := range n.Fields {
			if strings.EqualFold(field, alias) || strings.Contains(strings.ToLower(field), strings.ToLower(alias)) {
				return "node.fields." + field
			}
		}
		for field := range n.Attributes {
			if strings.EqualFold(field, alias) || strings.Contains(strings.ToLower(field), strings.ToLower(alias)) {
				return "node.attributes." + field
			}
		}
	}
	return ""
}

func auditRelevantFacts(n UniversalASTNode) []string {
	t := auditNodeText(n)
	result := []string{}
	add := func(facts ...string) { result = append(result, facts...) }
	if strings.Contains(t, "call") || strings.Contains(t, "invoke") || strings.Contains(t, "function") || strings.Contains(t, "lambda") || strings.Contains(t, "closure") {
		add("callable_signature", "function_value_signature", "receiver_contract", "multiple_results", "external_symbol_identity", "calling_convention", "argument_passing")
	}
	if strings.Contains(t, "aggregate") || strings.Contains(t, "array") || strings.Contains(t, "slice") || strings.Contains(t, "index") || strings.Contains(t, "list") || strings.Contains(t, "tuple") {
		add("aggregate_element_type", "aggregate_layout_constraints", "dynamic_bounds", "array_slice_distinction")
	}
	if strings.Contains(t, "literal") || strings.Contains(t, "integer") || strings.Contains(t, "binary") || strings.Contains(t, "unary") || strings.Contains(t, "operation") || strings.Contains(t, "numeric") {
		add("width_signedness", "overflow_policy")
	}
	if strings.Contains(t, "function") || strings.Contains(t, "closure") || strings.Contains(t, "scope") || strings.Contains(t, "binding") {
		add("ownership_borrowing", "lifetime_cleanup", "dispatch")
	}
	if strings.Contains(t, "exception") || strings.Contains(t, "throw") || strings.Contains(t, "error") {
		add("exceptions")
	}
	if strings.Contains(t, "atomic") || strings.Contains(t, "thread") || strings.Contains(t, "mutex") || strings.Contains(t, "channel") || strings.Contains(t, "sync") {
		add("synchronization")
	}
	if strings.Contains(t, "async") || strings.Contains(t, "await") || strings.Contains(t, "actor") {
		add("async")
	}
	if strings.Contains(t, "module") || strings.Contains(t, "decl") || strings.Contains(t, "import") {
		add("module_declaration_identity")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(result))
	for _, fact := range result {
		if !seen[fact] {
			seen[fact] = true
			out = append(out, fact)
		}
	}
	return out
}

func auditProvenanceForNode(p *SemanticProgram, n UniversalASTNode, fact string) (string, bool, string) {
	if evidence := auditHasField(n, auditFactAliases[fact]); evidence != "" {
		return AuditUASTExplicit, true, evidence
	}
	if len(n.SemanticFacets) > 0 || len(p.Evidence.Nodes) > 0 {
		return AuditUASTDerived, true, "canonical UAST facet/evidence plane"
	}
	if p.SemanticFeatures != nil {
		return AuditFrontendDerived, true, "frontend semantic feature profile"
	}
	if fact == "calling_convention" || fact == "argument_passing" || fact == "external_symbol_identity" {
		if p.Types.ABI != "" && p.Types.ABI != "unknown" {
			return AuditABIIntent, true, "semantic type contract ABI"
		}
	}
	if strings.Contains(fact, "layout") || fact == "width_signedness" {
		return AuditTargetDerived, false, "target layout/representation is not a UAST fact"
	}
	return AuditUnknown, false, "no explicit or uniquely derivable fact observed"
}

func AuditSemanticFactProvenance(p *SemanticProgram) ([]AuditFactProvenance, error) {
	u, err := canonicalUniversalAST(p)
	if err != nil {
		return nil, fmt.Errorf("audit canonical UAST: %w", err)
	}
	rows := make([]AuditFactProvenance, 0)
	for _, n := range u.Nodes {
		for _, fact := range auditRelevantFacts(n) {
			source, resolved, evidence := auditProvenanceForNode(p, n, fact)
			rows = append(rows, AuditFactProvenance{Node: n.ID, Operation: n.StructuralKind, Fact: fact, Source: source, Resolved: resolved, Reachable: true, Evidence: evidence})
		}
	}
	return rows, nil
}

func AuditBackendFactDemandTrace(p *SemanticProgram) ([]AuditBackendFactDemand, error) {
	provenance, err := AuditSemanticFactProvenance(p)
	if err != nil {
		return nil, err
	}
	consumers := []string{"direct-native", "llvm", "machine-native", "generic-executor", "target-projection", "module-linker-resolver"}
	rows := make([]AuditBackendFactDemand, 0, len(provenance)*len(consumers))
	for _, fact := range provenance {
		for _, consumer := range consumers {
			required := consumer != "module-linker-resolver" || fact.Fact == "module_declaration_identity"
			rows = append(rows, AuditBackendFactDemand{Consumer: consumer, Node: fact.Node, Operation: fact.Operation, RequestedFact: fact.Fact, SourceOfFact: fact.Source, Resolved: fact.Resolved, Reachable: fact.Reachable, RequiredForCodegen: required})
		}
	}
	return rows, nil
}
