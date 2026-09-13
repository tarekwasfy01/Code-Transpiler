// Copyright (c) 2026 Tarek Wasfy
package backend

// This file adapts an executed LLVM projection plan into the existing
// compiler-evidence matrix. It is evidence ingestion only: it does not create
// a second registry or alter lowering decisions.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// ImportLLVMProjectionEvidence reads a projection report produced by
// compile-llvm and appends one structured evidence rule per projected UAST
// cell. The report remains the source of provenance; no operation name is
// promoted to a canonical equivalence here.
func ImportLLVMProjectionEvidence(m *CompilerEvidenceMatrix, reportPath string) error {
	if m == nil {
		return fmt.Errorf("nil compiler evidence matrix")
	}
	b, err := os.ReadFile(reportPath)
	if err != nil {
		return err
	}
	var plan LLVMProjectionPlan
	if err := json.Unmarshal(b, &plan); err != nil {
		return fmt.Errorf("decode LLVM projection report: %w", err)
	}
	if plan.Schema == "" {
		return fmt.Errorf("LLVM projection report has no schema")
	}
	reportHash := sha256.Sum256(b)
	location := CompilerEvidenceLocation{Repository: "local:llvm-projection", Commit: plan.BasisSHA256, File: filepath.ToSlash(reportPath), SHA256: hex.EncodeToString(reportHash[:]), Symbol: "projection.cells", StartLine: 1, EndLine: 1}
	seen := make(map[string]bool, len(m.Rules))
	for _, r := range m.Rules {
		seen[r.ID] = true
	}
	for _, cell := range plan.Cells {
		id := fmt.Sprintf("llvm.projection.%s.%d", sanitizeEvidenceID(plan.Schema), cell.NodeID)
		if seen[id] {
			continue
		}
		confidence := EvidenceDerived
		if cell.Mode == "DIRECT" && len(plan.Gaps) == 0 {
			confidence = EvidenceDirect
		}
		conditions := []string{
			"projection_schema=" + plan.Schema,
			"projection_mode=" + cell.Mode,
			"structural_kind=" + cell.StructuralKind,
			"semantic_kind=" + cell.SemanticKind,
			"family=" + cell.Family,
			"projection_form=" + cell.ProjectionForm,
		}
		m.Rules = append(m.Rules, CompilerEvidence{
			ID: id, Compiler: "LLVM projection", Version: plan.BasisSHA256,
			Stage: CompilerStageCodeGeneration, TargetArchitecture: "LLVM",
			SemanticIdentity: cell.SemanticKind,
			InputPattern:     "uast.node:" + cell.StructuralKind,
			Conditions:       conditions, OutputPattern: append([]string(nil), cell.ExecutionPrimitives...),
			ReferencedOperations: append([]string(nil), cell.ExecutionPrimitives...),
			Relation:             "validated_llvm_projection", Confidence: confidence, Locations: []CompilerEvidenceLocation{location},
		})
		seen[id] = true
	}
	return nil
}

// WriteLLVMProjectionEvidence writes the same structured rules as JSONL for
// consumers that do not need to construct a full CompilerEvidenceMatrix.
func WriteLLVMProjectionEvidence(plan *LLVMProjectionPlan, path string) error {
	if plan == nil {
		return fmt.Errorf("nil LLVM projection plan")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, cell := range plan.Cells {
		confidence := EvidenceDerived
		if cell.Mode == "DIRECT" && len(plan.Gaps) == 0 {
			confidence = EvidenceDirect
		}
		row := CompilerEvidence{
			ID:       fmt.Sprintf("llvm.projection.%s.%d", sanitizeEvidenceID(plan.Schema), cell.NodeID),
			Compiler: "LLVM projection", Version: plan.BasisSHA256,
			Stage: CompilerStageCodeGeneration, TargetArchitecture: "LLVM",
			SemanticIdentity:     cell.SemanticKind,
			InputPattern:         "uast.node:" + cell.StructuralKind,
			Conditions:           []string{"projection_schema=" + plan.Schema, "projection_mode=" + cell.Mode, "family=" + cell.Family, "projection_form=" + cell.ProjectionForm},
			OutputPattern:        append([]string(nil), cell.ExecutionPrimitives...),
			ReferencedOperations: append([]string(nil), cell.ExecutionPrimitives...),
			Relation:             "validated_llvm_projection", Confidence: confidence,
		}
		if err := enc.Encode(row); err != nil {
			return err
		}
	}
	return nil
}
