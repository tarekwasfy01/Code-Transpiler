package backend

import (
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// CapabilityMatrix is feature x target. Status planes are mutually exclusive;
// Unsupported is explicit so unknown features cannot look like supported zeros.
type CapabilityMatrix struct {
	Features    []string              `json:"features"`
	Targets     []string              `json:"targets"`
	Native      matrixir.SparseMatrix `json:"native"`
	Lowering    matrixir.SparseMatrix `json:"lowering"`
	Emulated    matrixir.SparseMatrix `json:"emulated"`
	Unsupported matrixir.SparseMatrix `json:"unsupported"`
}

func SemanticCapabilityMatrix(extra []string) CapabilityMatrix {
	features := []string{"core", "integer.int32.exact", "integer.uint32.exact", "integer.int64.exact", "integer.uint64.exact", "integer.arbitrary", "pointer", "ownership.borrow", "exceptions", "classes", "generics", "concurrency", "ffi", "reflection", "gpu.compute", "index.zero_based", "index.negative", "native.go.lowering", "native.go.scalar", "native.go.functions", "native.c.lowering", "native.rust.lowering"}
	features = append(features, "integer.int8.exact", "integer.uint8.exact", "integer.int16.exact", "integer.uint16.exact", "integer.operations.v1")
	features = append(features, ExactSignatureCapability)
	features = append(features, ExactCallResolutionCapability)
	seen := map[string]bool{}
	for _, f := range features {
		seen[f] = true
	}
	for _, f := range extra {
		if !seen[f] {
			features = append(features, f)
			seen[f] = true
		}
	}
	m := CapabilityMatrix{Features: features}
	for _, b := range Backends() {
		m.Targets = append(m.Targets, b.ID)
	}
	n, k := len(features), len(m.Targets)
	m.Native = matrixir.NewSparseMatrix(n, k)
	m.Lowering = matrixir.NewSparseMatrix(n, k)
	m.Emulated = matrixir.NewSparseMatrix(n, k)
	m.Unsupported = matrixir.NewSparseMatrix(n, k)
	for i, f := range features {
		for j, b := range m.Targets {
			switch BackendCapability(f, b).Status {
			case CapabilityNative:
				m.Native.Set(i, j, 1)
			case CapabilityLowering:
				m.Lowering.Set(i, j, 1)
			case CapabilityEmulated:
				m.Emulated.Set(i, j, 1)
			default:
				m.Unsupported.Set(i, j, 1)
			}
		}
	}
	return m
}

// RejectedTargets computes requirements^T * unsupported. A positive entry is
// the count of unavailable requirements for that target, not a quality score.
func (m CapabilityMatrix) RejectedTargets(requirements []string) (matrixir.SparseMatrix, error) {
	vector := matrixir.NewSparseMatrix(1, len(m.Features))
	for _, requirement := range requirements {
		found := false
		for i, f := range m.Features {
			if f == requirement {
				vector.Set(0, i, 1)
				found = true
				break
			}
		}
		if !found {
			return matrixir.SparseMatrix{}, fmt.Errorf("requirement %q missing from capability matrix", requirement)
		}
	}
	return vector.Multiply(m.Unsupported)
}
