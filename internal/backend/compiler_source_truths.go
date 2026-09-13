// Copyright (c) 2026 Tarek Wasfy
package backend

// CompilerSourceTruth is the typed bridge from compiler-source evidence to
// existing canonical contracts. It is deliberately a contract catalogue, not
// a second IR and not a permission to infer missing values.
type CompilerSourceTruth struct {
	ID             string
	Layer          string
	SemanticAxes   []string
	RequiredFacts  []string
	BackendDemand  []string
	FailureIfUnset string
}

var compilerSourceTruths = []CompilerSourceTruth{
	{ID: "frontend.syntax_structure", Layer: "FRONTEND", SemanticAxes: []string{"syntax", "control", "data"}, RequiredFacts: []string{"node kind", "operation", "child/operand relation", "evaluation order"}, BackendDemand: []string{"structured UAST graph", "terminator/control target"}, FailureIfUnset: "PROJECT_SEMANTIC_FACT_MISSING"},
	{ID: "frontend.typing", Layer: "FRONTEND", SemanticAxes: []string{"types"}, RequiredFacts: []string{"type identity", "type origin", "width", "signedness", "element/pointee type"}, BackendDemand: []string{"LLVM/native value type", "conversion contract"}, FailureIfUnset: "TYPE_CONTRACT_MISSING"},
	{ID: "shared.callable", Layer: "SHARED", SemanticAxes: []string{"binding", "call_modes", "scope"}, RequiredFacts: []string{"function identity", "parameter/result contract", "receiver", "closure environment"}, BackendDemand: []string{"call ABI", "symbol relocation", "indirect-call target"}, FailureIfUnset: "CALL_ABI_CONTRACT_MISSING"},
	{ID: "shared.module", Layer: "SHARED", SemanticAxes: []string{"scope", "binding", "order"}, RequiredFacts: []string{"module identity", "imports/exports", "dependency order", "initializer"}, BackendDemand: []string{"global symbol index", "project startup order"}, FailureIfUnset: "MODULE_CONTRACT_MISSING"},
	{ID: "backend.storage_layout", Layer: "BACKEND", SemanticAxes: []string{"data", "types"}, RequiredFacts: []string{"storage class", "mutability", "aggregate shape", "layout", "alignment", "bounds"}, BackendDemand: []string{"section placement", "GEP/index lowering", "global relocation"}, FailureIfUnset: "LAYOUT_CONTRACT_MISSING"},
	{ID: "backend.abi_target", Layer: "BACKEND", SemanticAxes: []string{"call_modes", "types", "scope"}, RequiredFacts: []string{"target triple", "calling convention", "linkage", "external identity", "relocation kind"}, BackendDemand: []string{"object emission", "PE/COFF link", "import table"}, FailureIfUnset: "ABI_CONTRACT_MISSING"},
	{ID: "backend.effects_control", Layer: "BACKEND", SemanticAxes: []string{"effects", "control", "order"}, RequiredFacts: []string{"throw/cleanup", "async continuation", "synchronization", "termination"}, BackendDemand: []string{"unwind metadata", "scheduler/runtime primitive", "control-flow emission"}, FailureIfUnset: "EFFECT_CONTRACT_MISSING"},
}

// CompilerSourceTruths returns a defensive copy so callers cannot mutate the
// shared contract catalogue. Every field maps to an existing SemanticProgram
// axis or to an existing backend demand; no source-language branch is used.
func CompilerSourceTruths() []CompilerSourceTruth {
	out := make([]CompilerSourceTruth, len(compilerSourceTruths))
	for i, truth := range compilerSourceTruths {
		out[i] = truth
		out[i].SemanticAxes = append([]string(nil), truth.SemanticAxes...)
		out[i].RequiredFacts = append([]string(nil), truth.RequiredFacts...)
		out[i].BackendDemand = append([]string(nil), truth.BackendDemand...)
	}
	return out
}
