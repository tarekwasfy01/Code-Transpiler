// Copyright (c) 2026 Tarek Wasfy

package backend

import (
	"fmt"
	"sort"
	"strconv"
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
	reachable := nativeLegalityReachableNodes(g)
	ids := make([]int, 0, len(g.common))
	for id := range g.common {
		if !reachable[id] {
			continue
		}
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		report.Decisions = append(report.Decisions, nativeLegalityDecision(g, id, basis))
	}
	return report, nil
}

// nativeLegalityReachableNodes mirrors the direct selector's execution
// boundary. A canonical document may contain many declarations and their
// bodies as evidence, but a declaration body is executable only after a
// canonical call edge reaches its function value. This keeps FULL legality
// aligned with the emitted graph instead of rejecting dead evidence code.
func nativeLegalityReachableNodes(g *uastExecutionGraph) map[int]bool {
	reachable := map[int]bool{}
	functions := map[string]int{}
	for parent, roles := range g.children {
		if g.common[parent].Kind != "assign" {
			continue
		}
		for _, child := range roles["expression"] {
			if g.common[child.ID].Kind == "function" && g.common[parent].Name != "" {
				functions[g.common[parent].Name] = child.ID
			}
		}
	}
	visiting := map[int]bool{}
	expandedFunctions := map[int]bool{}
	var walk func(int, bool)
	walk = func(id int, enterFunction bool) {
		if g.common[id].Kind == "" || visiting[id] {
			return
		}
		if g.common[id].Kind == "function" && !enterFunction {
			reachable[id] = true
			return
		}
		if reachable[id] && (g.common[id].Kind != "function" || expandedFunctions[id]) {
			return
		}
		if g.common[id].Kind == "function" {
			expandedFunctions[id] = true
		}
		visiting[id] = true
		reachable[id] = true
		if g.common[id].Kind == "call" {
			if callee, ok, _ := g.callTarget(id); ok {
				if g.common[callee].Kind == "function" {
					walk(callee, true)
				} else if g.common[callee].Kind == "identifier" {
					if fn, exists := functions[g.common[callee].Name]; exists {
						walk(fn, true)
					}
				}
			}
		}
		for _, roles := range g.children[id] {
			for _, child := range roles {
				if g.common[id].Kind == "function" && child.Meta.Role == "body" {
					walk(child.ID, true)
					continue
				}
				walk(child.ID, false)
			}
		}
		delete(visiting, id)
	}
	if g.root >= 0 {
		walk(g.root, true)
	}
	return reachable
}

func nativeLegalityDecision(g *uastExecutionGraph, id int, basis NativeCapabilityBasis) NativeLegalityDecision {
	c := g.common[id]
	d := NativeLegalityDecision{NodeID: id, StructuralKind: g.nodes[id].StructuralKind, SemanticKind: c.Kind, Status: NativeLegal, Family: "metadata"}
	switch c.Kind {
	case "block", "expression", "assign", "binding", "function", "parameter", "return", "if", "ifstmt", "while", "whilestmt", "repeat", "for", "forstmt", "break", "continue", "switch", "switchstmt":
		d.Family = "control_binding"
	case "type":
		// Type descriptors in a CANONICALIZE_ONLY document are compile-time
		// evidence. Their native consumer is the layout/type contract, not a
		// runtime instruction sequence.
		return d
	case "literal", "missing_argument", "identifier", "binary", "unary", "typed_operation", "operationexpr":
		d.Family = "scalar_type"
	case "aggregate", "tuple", "tuple_result", "index", "slice":
		d.Family = "aggregate_place"
	case "member":
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
		d.Reason = fmt.Sprintf("no native semantic family structural=%q semantic=%q", d.StructuralKind, d.SemanticKind)
		return d
	}
	// The selector may only see values whose target layout is known. This makes
	// the data-layout solver a real FULL-legality consumer rather than a report
	// generator; unknown/dynamic representations fail before x64 selection.
	if c.Type.Kind != "" && c.Type.Kind != "unknown" {
		typ := resolveNativeType(g.document, c.Type)
		// Matrix-exported CANONICALIZE_ONLY fragments can carry an integer type
		// fact without a width when the source frontend has only inferred the
		// machine value category. The native contract resolves that unspecified
		// width to the target word width; explicit nonzero widths remain strict.
		if typ.Kind == "integer" && typ.Bits == 0 && g.document != nil && g.document.Metadata["frontend_route"] == "CANONICALIZE_ONLY" {
			typ.Bits = 64
		}
		context := NativeABIValue
		if c.Kind == "parameter" {
			context = NativeABIArgument
		} else if c.Kind == "return" {
			context = NativeABIResult
		}
		if _, err := SolveNativeLayout(NativeWindowsX64TargetProfile(), typ, context); err != nil {
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
		callee, ok, _ := g.callTarget(id)
		if !ok {
			d.Status, d.Reason = NativeIllegal, "call lacks callee relation"
			return d
		}
		// A typed operation is also a first-class canonical call target.  The
		// frontend uses this form for intrinsic/builtin calls whose operation
		// contract (name, arity, type) is carried on the target node rather than
		// in a source identifier.  It is legal only when the operation is present
		// in the native basis; unknown typed operations remain fail-closed.
		typedOperationTarget := false
		if g.common[callee].Kind == "typed_operation" && g.common[callee].Operation.Typed != nil {
			typedOperationTarget = basis.Operations[strings.ToUpper(g.common[callee].Operation.Typed.Name)]
		}
		if g.common[callee].Kind != "identifier" && g.common[callee].Kind != "function" && g.common[callee].Kind != "call" && g.common[callee].Kind != "deref" && g.common[callee].Kind != "index" && g.common[callee].Kind != "aggregate" && g.common[callee].Kind != "type" && g.common[callee].Kind != "member" && !g.common[callee].Type.Reference && g.nodes[callee].StructuralKind != "SymbolRef" && g.nodes[callee].StructuralKind != "ClosureExpr" && g.nodes[callee].StructuralKind != "MemberAccessExpr" && !typedOperationTarget {
			d.Status, d.Reason = NativeUnresolved, fmt.Sprintf("call target lacks direct or function-reference contract target_node=%d structural=%q semantic=%q name=%q", callee, g.nodes[callee].StructuralKind, g.common[callee].Kind, g.common[callee].Name)
			return d
		}
		d.Status, d.Predicate = NativeDynamicallyLegal, "linked-call-target"
	}
	return d
}

// resolveNativeType projects nominal/document-local type references onto the
// structural type used by the target layout solver.  Semantic UASTs retain
// named identities for source fidelity, but native legality must consume the
// corresponding definition from the canonical type table.  Resolution is
// bounded and cycle-safe so recursive nominal types remain address-like rather
// than causing an infinite walk.
func resolveNativeType(doc *UniversalASTDocument, typ SemanticType) SemanticType {
	seen := map[string]bool{}
	var resolve func(SemanticType) SemanticType
	resolve = func(t SemanticType) SemanticType {
		if strings.EqualFold(t.Kind, "named") || strings.EqualFold(t.Kind, "alias") {
			if t.Element != nil {
				return resolve(*t.Element)
			}
			key := t.Identity
			if key == "" {
				key = t.Name
			}
			if key != "" && !seen[key] && doc != nil {
				seen[key] = true
				for _, def := range doc.TypeTable {
					d := def.Type
					if d.Identity == key || d.Name == key || strings.EqualFold(d.Identity, key) || strings.EqualFold(d.Name, key) {
						if d.Kind == "named" || d.Kind == "alias" {
							if d.Element != nil {
								return resolve(*d.Element)
							}
							continue
						}
						return d
					}
				}
				// Some serialized producers retain only the document-local type
				// number in a nominal identity (for example "type:17"). Resolve
				// that spelling directly against the canonical table as well.
				if strings.HasPrefix(strings.ToLower(key), "type:") {
					if id, err := strconv.Atoi(strings.TrimPrefix(strings.ToLower(key), "type:")); err == nil {
						for _, def := range doc.TypeTable {
							if def.ID == id && def.Type.Element != nil {
								return resolve(*def.Type.Element)
							}
							if def.ID == id && def.Type.Kind != "named" && def.Type.Kind != "alias" {
								return def.Type
							}
						}
					}
				}
			}
		}
		return t
	}
	return resolve(typ)
}

func nativeMetadataStructuralKind(kind string) bool {
	switch kind {
	case "ABIContract", "Annotation", "AnnotationDecl", "BindingResolution", "CaptureRelation", "DispatchResolution", "DispatchSemantics", "Effect", "ExecutionModel", "GenericDecl", "IRFact", "ImportDecl", "LayoutContract", "LifetimeRegion", "LoweringFact", "MemoryModelContract", "ModuleDecl", "OptimizationFact", "OwnershipSemantics", "SafetyRegion", "TypeDecl", "TypeInferenceFact", "TypeRelation":
		return true
	}
	return false
}
