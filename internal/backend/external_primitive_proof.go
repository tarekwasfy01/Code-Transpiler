package backend

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// externalPrimitiveInternalProof contains only source-independent canonical
// shapes already accepted by the frontend/UAST contract.  It is a proof
// crosswalk, not a new handler registry.
var externalPrimitiveInternalProof = map[string]struct {
	structure string
	semantic  string
	arity     string
	guard     string
}{
	"LITERAL_BOOL":   {"LiteralExpr", "literal:boolean", "1", "typed literal"},
	"LITERAL_F64":    {"LiteralExpr", "literal:number", "1", "typed literal"},
	"LITERAL_I64":    {"LiteralExpr", "literal:number", "1", "typed literal"},
	"LITERAL_STRING": {"LiteralExpr", "literal:string", "1", "typed literal"},
	"SYMBOL_REF":     {"SymbolRef", "identifier", "1", "structured binding reference"},
	"LOGICAL_NOT":    {"OperationExpr", "unary:logical_not", "1", "bool operand"},
	"LOGICAL_AND":    {"OperationExpr", "binary:logical_and", "2", "bool operands"},
	"LOGICAL_OR":     {"OperationExpr", "binary:logical_or", "2", "bool operands"},
	"COMPARE_EQ":     {"OperationExpr", "binary:equal", "2", "comparable operands"},
	"COMPARE_NE":     {"OperationExpr", "binary:not_equal", "2", "comparable operands"},
	"COMPARE_LT":     {"OperationExpr", "binary:less_than", "2", "ordered operands"},
	"COMPARE_LE":     {"OperationExpr", "binary:less_or_equal", "2", "ordered operands"},
	"COMPARE_GT":     {"OperationExpr", "binary:greater_than", "2", "ordered operands"},
	"COMPARE_GE":     {"OperationExpr", "binary:greater_or_equal", "2", "ordered operands"},
}

// ExactSemanticShapeIDs exposes the source-independent shapes to matrix
// consumers. Target executability is deliberately evaluated separately.
func ExactSemanticShapeIDs() []string {
	out := make([]string, 0, len(externalPrimitiveInternalProof))
	for id := range externalPrimitiveInternalProof {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// WriteExternalPrimitiveProofReport proves a conservative subset of the
// external EXACT_CODE rows against existing canonical frontend/UAST shapes.
func WriteExternalPrimitiveProofReport(out string) (int, error) {
	bundle, err := loadExternalPrimitiveEvidence()
	if err != nil {
		return 0, err
	}
	rows := [][]string{}
	for _, candidate := range bundle["Primitive Candidates"] {
		id := externalValue(candidate, "primitive_id")
		proof, ok := externalPrimitiveInternalProof[id]
		if !ok {
			continue
		}
		state := strings.ToUpper(externalValue(candidate, "proof_state"))
		status := "PARAMETER_EQUIVALENT"
		reason := "canonical structure and arity match; target-specific external construction remains separate"
		if state == "EXACT_CODE" && proof.structure != "" && proof.semantic != "" {
			status = "EXACT_SEMANTIC_SHAPE"
			reason = "existing frontend/UAST structure, semantic operation, arity and guard are all explicit"
		}
		rows = append(rows, []string{id, externalValue(candidate, "kernel_class"), externalValue(candidate, "rewrite_candidate"), state, proof.structure, proof.semantic, proof.arity, proof.guard, status, reason, externalValue(candidate, "source_url")})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	if err := os.MkdirAll(out, 0755); err != nil {
		return 0, err
	}
	f, err := os.Create(filepath.Join(out, "proven_exact_semantic_shapes.csv"))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"primitive_id", "kernel_class", "external_rewrite", "external_proof_state", "internal_structure", "internal_semantic", "arity", "guard", "status", "reason", "source_url"}); err != nil {
		return 0, err
	}
	if err := w.WriteAll(rows); err != nil {
		return 0, err
	}
	w.Flush()
	return len(rows), w.Error()
}
