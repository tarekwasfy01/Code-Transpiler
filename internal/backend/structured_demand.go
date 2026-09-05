package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// StructuredSemanticDemand is derived only from canonical UAST nodes and
// facets. Diagnostics are deliberately absent from this type.
type StructuredSemanticDemand struct {
	UASTHash       string
	Nodes          []string
	Operations     []string
	Primitives     []string
	Kernels        []string
	Types          []string
	Effects        []string
	Evaluation     []string
	Bindings       []string
	Ownership      []string
	Representation []string
}

// AnalyzeStructuredSemanticDemand creates the case x primitive input matrix.
// It is the only supported demand classifier for corpus evidence.
func AnalyzeStructuredSemanticDemand(u *UniversalASTDocument) StructuredSemanticDemand {
	if u == nil {
		return StructuredSemanticDemand{}
	}
	b, _ := json.Marshal(u)
	sum := sha256.Sum256(b)
	d := StructuredSemanticDemand{UASTHash: hex.EncodeToString(sum[:])}
	set := func(dst *[]string, v string) {
		for _, x := range *dst {
			if x == v {
				return
			}
		}
		*dst = append(*dst, v)
	}
	for _, n := range u.Nodes {
		set(&d.Nodes, n.StructuralKind)
		for _, f := range n.SemanticFacets {
			set(&d.Operations, f)
		}
		switch n.StructuralKind {
		case "LiteralExpr":
			set(&d.Primitives, "LITERAL")
			set(&d.Kernels, "LITERAL")
		case "CallExpr":
			set(&d.Primitives, "CALL")
			set(&d.Kernels, "CALL")
		case "SwitchMatchStmt", "SelectExpr":
			set(&d.Primitives, "SELECT")
			set(&d.Kernels, "SELECT")
		case "LoopStmt", "ForEachStmt":
			set(&d.Primitives, "ITERATION")
			set(&d.Kernels, "ITERATION")
		case "TryStmt", "RaisePanicStmt":
			set(&d.Primitives, "EXCEPTION")
			set(&d.Kernels, "EXCEPTION")
		}
	}
	for _, t := range u.TypeTable {
		set(&d.Types, t.Type.Kind)
	}
	set(&d.Evaluation, u.Evaluation)
	set(&d.Representation, u.ValueModel)
	for _, r := range u.Relations {
		if r.Kind == "scope.binding" {
			set(&d.Bindings, "scope.binding")
		}
		if r.Kind == "effect.graph" {
			set(&d.Effects, "effect.graph")
		}
	}
	for _, p := range [][]string{d.Nodes, d.Operations, d.Primitives, d.Kernels, d.Types, d.Effects, d.Evaluation, d.Bindings, d.Ownership, d.Representation} {
		sort.Strings(p)
	}
	return d
}
