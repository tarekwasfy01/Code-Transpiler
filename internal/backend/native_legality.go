// Copyright (c) 2026 Tarek Wasfy

package backend

import (
	"fmt"
	"sort"
	"strings"
)

// NativeLegalityMode controls whether a caller only inspects the canonical
// graph, permits a partial repair search, or requires a selector-safe fixed
// point. It never owns another persistent program representation.
type NativeLegalityMode string

const (
	NativeLegalityAnalysis NativeLegalityMode = "ANALYSIS"
	NativeLegalityPartial  NativeLegalityMode = "PARTIAL"
	NativeLegalityFull     NativeLegalityMode = "FULL"
)

type NativeLegalityStatus string

const (
	NativeLegal            NativeLegalityStatus = "LEGAL"
	NativeDynamicallyLegal NativeLegalityStatus = "DYNAMICALLY_LEGAL"
	NativeIllegal          NativeLegalityStatus = "ILLEGAL"
	NativeUnresolved       NativeLegalityStatus = "UNRESOLVED"
)

type NativeLegalityDecision struct {
	NodeID         int                  `json:"node_id"`
	StructuralKind string               `json:"structural_kind"`
	SemanticKind   string               `json:"semantic_kind"`
	Family         string               `json:"family"`
	Status         NativeLegalityStatus `json:"status"`
	Predicate      string               `json:"predicate,omitempty"`
	Reason         string               `json:"reason,omitempty"`
}

type NativeLegalityReport struct {
	Target    string                   `json:"target"`
	Mode      NativeLegalityMode       `json:"mode"`
	Decisions []NativeLegalityDecision `json:"decisions"`
}

func (r NativeLegalityReport) FullLegal() bool {
	for _, decision := range r.Decisions {
		if decision.Status == NativeIllegal || decision.Status == NativeUnresolved {
			return false
		}
	}
	return true
}

func (r NativeLegalityReport) Blocking() []NativeLegalityDecision {
	var out []NativeLegalityDecision
	for _, decision := range r.Decisions {
		if decision.Status == NativeIllegal || decision.Status == NativeUnresolved {
			out = append(out, decision)
		}
	}
	return out
}

// AnalyzeNativeLegality is the non-mutating legality classification used by
// the semantic/native closure and by evidence reporting. A dynamic result
// records the predicate proved for the current UAST; it is selector-safe.
func AnalyzeNativeLegality(program *SemanticProgram, target string, mode NativeLegalityMode) (NativeLegalityReport, error) {
	if mode != NativeLegalityAnalysis && mode != NativeLegalityPartial && mode != NativeLegalityFull {
		return NativeLegalityReport{}, fmt.Errorf("unknown native legality mode %q", mode)
	}
	u, err := canonicalUniversalAST(program)
	if err != nil {
		return NativeLegalityReport{}, err
	}
	g, err := newUASTExecutionGraph(u)
	if err != nil {
		return NativeLegalityReport{}, err
	}
	return analyzeNativeLegalityGraph(g, target, mode)
}

func analyzeNativeLegalityGraph(g *uastExecutionGraph, target string, mode NativeLegalityMode) (NativeLegalityReport, error) {
	basis, err := BuildNativeCapabilityBasis(target)
	if err != nil {
		return NativeLegalityReport{}, err
	}
	report := NativeLegalityReport{Target: target, Mode: mode}
	ids := make([]int, 0, len(g.common))
	for id := range g.common {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		report.Decisions = append(report.Decisions, nativeLegalityDecision(g, id, basis))
	}
	return report, nil
}

func nativeLegalityDecision(g *uastExecutionGraph, id int, basis NativeCapabilityBasis) NativeLegalityDecision {
	c := g.common[id]
	d := NativeLegalityDecision{NodeID: id, StructuralKind: g.nodes[id].StructuralKind, SemanticKind: c.Kind, Status: NativeLegal, Family: "metadata"}
	switch c.Kind {
	case "block", "expression", "assign", "binding", "function", "parameter", "return", "if", "while", "repeat", "for", "break", "continue":
		d.Family = "control_binding"
	case "literal", "missing_argument", "identifier", "binary", "unary", "typed_operation":
		d.Family = "scalar_type"
	case "aggregate", "index", "slice":
		d.Family = "aggregate_place"
	case "address_of", "address", "deref":
		d.Family = "address_place"
	case "call":
		d.Family = "call_abi"
	default:
		// Facts, contracts and type-only nodes are carried by the canonical UAST
		// but do not reach instruction selection. An otherwise executable node
		// without a machine family stays unresolved and stops FULL conversion.
		if nativeMetadataStructuralKind(d.StructuralKind) {
			return d
		}
		d.Family, d.Status = "unclassified", NativeUnresolved
		d.Reason = "no native semantic family"
		return d
	}
	// The selector may only see values whose target layout is known. This makes
	// the data-layout solver a real FULL-legality consumer rather than a report
	// generator; unknown/dynamic representations fail before x64 selection.
	if c.Type.Kind != "" && c.Type.Kind != "unknown" {
		context := NativeABIValue
		if c.Kind == "parameter" {
			context = NativeABIArgument
		} else if c.Kind == "return" {
			context = NativeABIResult
		}
		if _, err := SolveNativeLayout(NativeWindowsX64TargetProfile(), c.Type, context); err != nil {
			d.Status, d.Reason = NativeIllegal, err.Error()
			return d
		}
	}

	if c.Kind == "typed_operation" && c.Operation.Typed != nil {
		operation := strings.ToUpper(c.Operation.Typed.Name)
		if !basis.Operations[operation] {
			d.Status, d.Reason = NativeIllegal, "typed operation has no native terminal"
			return d
		}
	}
	if c.Kind == "literal" && c.Type.Bits == 32 && c.Type.IEEE754 {
		d.Status, d.Predicate, d.Reason = NativeIllegal, "binary32-rounding", "x64 selector has no binary32 materialization"
		return d
	}
	if c.Kind == "address_of" || c.Kind == "address" {
		place, ok, _ := g.one(id, "value", false)
		if !ok {
			place, ok, _ = g.one(id, "place", false)
		}
		if !ok {
			place, ok, _ = g.one(id, "operand", false)
		}
		if !ok || (g.common[place].Kind != "identifier" && g.common[place].Kind != "index") {
			d.Status, d.Reason = NativeIllegal, "address-of requires identifier or index place"
			return d
		}
		d.Status, d.Predicate = NativeDynamicallyLegal, "addressable-place"
	}
	if c.Kind == "call" {
		callee, ok, _ := g.one(id, "value", false)
		if !ok {
			d.Status, d.Reason = NativeIllegal, "call lacks callee relation"
			return d
		}
		if g.common[callee].Kind != "identifier" && g.common[callee].Kind != "function" && !g.common[callee].Type.Reference {
			d.Status, d.Reason = NativeUnresolved, "call target lacks direct or function-reference contract"
			return d
		}
		d.Status, d.Predicate = NativeDynamicallyLegal, "linked-call-target"
	}
	return d
}

func nativeMetadataStructuralKind(kind string) bool {
	switch kind {
	case "ABIContract", "Annotation", "AnnotationDecl", "BindingResolution", "CaptureRelation", "DispatchResolution", "DispatchSemantics", "Effect", "ExecutionModel", "GenericDecl", "IRFact", "ImportDecl", "LayoutContract", "LifetimeRegion", "LoweringFact", "MemoryModelContract", "ModuleDecl", "OptimizationFact", "OwnershipSemantics", "SafetyRegion", "TypeDecl", "TypeInferenceFact", "TypeRelation":
		return true
	}
	return false
}
