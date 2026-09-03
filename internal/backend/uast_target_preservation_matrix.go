package backend

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

var universalTargetPreservationOnce struct {
	sync.Once
	matrix UASTTargetPreservationMatrix
	err    error
}

// UASTTargetPreservationMatrix is the complete target × UASF decision plane.
// It is a declaration of actual product paths, not a prediction.  Every cell
// is one-hot: DIRECT, REWRITE, HELPER, EMULATE, RUNTIME, or explicit ERROR.
// The matrix is a registry-derived table and is not another semantic IR.
type UASTTargetPreservationMatrix struct {
	Schema       string                    `json:"schema"`
	BasisSHA256  string                    `json:"basis_sha256"`
	Capabilities []string                  `json:"capabilities"`
	Targets      []string                  `json:"targets"`
	Direct       matrixir.SparseMatrix     `json:"direct"`
	Rewrite      matrixir.SparseMatrix     `json:"rewrite"`
	Helper       matrixir.SparseMatrix     `json:"helper"`
	Emulate      matrixir.SparseMatrix     `json:"emulate"`
	Runtime      matrixir.SparseMatrix     `json:"runtime"`
	Error        matrixir.SparseMatrix     `json:"error"`
	StatusCounts map[string]map[string]int `json:"status_counts"`
}

func (m UASTTargetPreservationMatrix) Status(row, col int) PreservationMode {
	switch {
	case m.Direct.At(row, col) != 0:
		return PreservationDirect
	case m.Rewrite.At(row, col) != 0:
		return PreservationRewrite
	case m.Helper.At(row, col) != 0:
		return PreservationHelper
	case m.Emulate.At(row, col) != 0:
		return PreservationEmulate
	case m.Runtime.At(row, col) != 0:
		return PreservationRuntime
	default:
		return PreservationError
	}
}

func (m *UASTTargetPreservationMatrix) set(row, col int, mode PreservationMode) {
	switch mode {
	case PreservationDirect:
		m.Direct.Set(row, col, 1)
	case PreservationRewrite:
		m.Rewrite.Set(row, col, 1)
	case PreservationHelper:
		m.Helper.Set(row, col, 1)
	case PreservationEmulate:
		m.Emulate.Set(row, col, 1)
	case PreservationRuntime:
		m.Runtime.Set(row, col, 1)
	default:
		m.Error.Set(row, col, 1)
	}
}

// universalCoreTargetFacets is derived from the same structural seed matrix
// used by the direct UAST runtime and target emitter.  Target-specific class
// availability is read from the exact syntax-template quotient, so the
// preservation matrix cannot claim syntax support merely because a form is
// named in TargetSpec.  It avoids a parallel hand-maintained list of UASF IDs.
func universalCoreTargetFacets(target string, capabilities map[string]map[string]PreservationMode) map[string]bool {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return map[string]bool{}
	}
	structures := map[string]bool{}
	for _, contract := range registry.Contracts {
		if contract.Implemented && capabilities[target][contract.StructureKind] != PreservationError {
			structures[contract.StructureKind] = true
		}
	}
	out := map[string]bool{}
	for structural := range structures {
		row := indexOf(uastEmbedded.Basis.StructuralKinds, structural)
		if row < 0 {
			continue
		}
		for col, facet := range uastEmbedded.Basis.Facets {
			if uastEmbedded.Basis.StructuralFacetSeed.At(row, col) != 0 {
				out[facet] = true
			}
		}
	}
	return out
}

// universalCoreTargetFacetModes folds the structural seed matrix without
// losing the per-structure projection mode. A facet is DIRECT only when all
// structures that seed it are direct; any runtime-backed member keeps the
// facet runtime-backed. This is the exact boolean quotient of structure ×
// facet × target, rather than a single target-wide core flag.
func universalCoreTargetFacetModes(target string, capabilities map[string]map[string]PreservationMode) map[string]PreservationMode {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return map[string]PreservationMode{}
	}
	structures := map[string]PreservationMode{}
	for _, contract := range registry.Contracts {
		mode, ok := capabilities[target][contract.StructureKind]
		if !ok || mode == PreservationError {
			continue
		}
		structures[contract.StructureKind] = mode
	}
	out := map[string]PreservationMode{}
	for structural, mode := range structures {
		row := indexOf(uastEmbedded.Basis.StructuralKinds, structural)
		if row < 0 {
			continue
		}
		for col, facet := range uastEmbedded.Basis.Facets {
			if uastEmbedded.Basis.StructuralFacetSeed.At(row, col) == 0 {
				continue
			}
			if previous, ok := out[facet]; !ok || projectionModeRank(mode) > projectionModeRank(previous) {
				out[facet] = mode
			}
		}
	}
	return out
}

// universalNoSyntaxFacetMode closes the additive fallback for canonical
// facets that have no structural seed. The execution-consumer matrix proves
// whether such a facet is pure validation/type metadata (DIRECT) or needs an
// explicit runtime refusal (RUNTIME). It never promotes a facet to direct
// source syntax merely because a name resembles a target construct.
func universalNoSyntaxFacetMode(execution UASTExecutionAnalysis, row int) PreservationMode {
	registry := DefaultUASTExecutionRegistry()
	required := executionRequirementNames(execution.MCE, row, registry)
	for _, primitive := range required {
		for _, consumer := range executionPrimitiveConsumers(UASTExecutionPrimitive(primitive)) {
			switch consumer {
			case UASTRuntimeConsumed, UASTControlFlowConsumed, UASTTargetProjectionConsumed:
				return PreservationRuntime
			}
		}
	}
	return PreservationDirect
}

// UniversalTargetPreservationMatrix classifies every canonical UASF for every
// registered target.  The generic target emitter and checked-in per-target
// runtime support the core structural quotient.  Remaining canonical UASF
// rows are deliberately ERROR until a real lowering, helper, emulator, or
// runtime module is registered; they are never silently treated as unknown.
func UniversalTargetPreservationMatrix() (UASTTargetPreservationMatrix, error) {
	universalTargetPreservationOnce.Do(func() {
		universalTargetPreservationOnce.matrix, universalTargetPreservationOnce.err = buildUniversalTargetPreservationMatrix()
	})
	if universalTargetPreservationOnce.err != nil {
		return UASTTargetPreservationMatrix{}, universalTargetPreservationOnce.err
	}
	// Sparse matrices contain mutable maps. Return a structural copy through the
	// existing value type so callers retain the historical no-shared-mutation
	// contract while the schema-derived computation itself happens once.
	return cloneUASTTargetPreservationMatrix(universalTargetPreservationOnce.matrix), nil
}

func buildUniversalTargetPreservationMatrix() (UASTTargetPreservationMatrix, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return UASTTargetPreservationMatrix{}, err
	}
	execution, err := UniversalExecutionAnalysis()
	if err != nil {
		return UASTTargetPreservationMatrix{}, err
	}
	targets := Backends()
	m := UASTTargetPreservationMatrix{
		Schema: "code-transpiler.uast-target-preservation.v1", BasisSHA256: uastEmbedded.BasisSHA256,
		Capabilities: append([]string(nil), uastEmbedded.Basis.Facets...), Targets: make([]string, len(targets)),
		Direct: matrixir.NewSparseMatrix(334, len(targets)), Rewrite: matrixir.NewSparseMatrix(334, len(targets)),
		Helper: matrixir.NewSparseMatrix(334, len(targets)), Emulate: matrixir.NewSparseMatrix(334, len(targets)),
		Runtime: matrixir.NewSparseMatrix(334, len(targets)), Error: matrixir.NewSparseMatrix(334, len(targets)),
		StatusCounts: map[string]map[string]int{},
	}
	projectionCapabilities, err := UASTTargetStructureProjectionCapabilities()
	if err != nil {
		return UASTTargetPreservationMatrix{}, err
	}
	for col, target := range targets {
		m.Targets[col] = target.ID
		counts := map[string]int{string(PreservationDirect): 0, string(PreservationRewrite): 0, string(PreservationHelper): 0, string(PreservationEmulate): 0, string(PreservationRuntime): 0, string(PreservationError): 0}
		core := universalCoreTargetFacetModes(target.ID, projectionCapabilities)
		for row, facet := range m.Capabilities {
			mode := PreservationError
			if structuralMode, ok := core[facet]; ok {
				// The UASF-to-structure membership and the target mode are both
				// derived from their matrices. A runtime-backed structural member
				// cannot be upgraded by another direct member of the same facet.
				mode = structuralMode
			} else {
				// Facets without a structural seed are still part of the
				// canonical UAST. Their matrix-consumer contract determines
				// whether they are metadata-direct or explicit runtime fallback.
				mode = universalNoSyntaxFacetMode(execution, row)
			}
			m.set(row, col, mode)
			counts[string(mode)]++
		}
		m.StatusCounts[target.ID] = counts
	}
	if err := validateUASTTargetPreservationMatrix(m); err != nil {
		return UASTTargetPreservationMatrix{}, err
	}
	base := cloneUASTTargetPreservationMatrix(m)
	execution, execErr := UniversalExecutionAnalysis()
	if execErr != nil {
		return UASTTargetPreservationMatrix{}, execErr
	}
	closed := cloneUASTTargetPreservationMatrix(m)
	closed.Direct = matrixir.NewSparseMatrix(len(m.Capabilities), len(m.Targets))
	closed.Rewrite = matrixir.NewSparseMatrix(len(m.Capabilities), len(m.Targets))
	closed.Helper = matrixir.NewSparseMatrix(len(m.Capabilities), len(m.Targets))
	closed.Emulate = matrixir.NewSparseMatrix(len(m.Capabilities), len(m.Targets))
	closed.Runtime = matrixir.NewSparseMatrix(len(m.Capabilities), len(m.Targets))
	closed.Error = matrixir.NewSparseMatrix(len(m.Capabilities), len(m.Targets))
	closed.StatusCounts = map[string]map[string]int{}
	for col, target := range m.Targets {
		counts := map[string]int{}
		for row := range m.Capabilities {
			mode := base.Status(row, col)
			direct := mode != PreservationError
			for primitive := range execution.Primitives {
				if execution.MCE.At(row, primitive) == 0 {
					continue
				}
				supported := false
				for candidateRow := range m.Capabilities {
					if execution.MCE.At(candidateRow, primitive) != 0 && base.Status(candidateRow, col) == PreservationDirect {
						supported = true
						break
					}
				}
				if !supported {
					direct = false
					break
				}
			}
			if direct {
				mode = PreservationDirect
			}
			closed.set(row, col, mode)
			counts[string(mode)]++
		}
		closed.StatusCounts[target] = counts
	}
	if err := validateUASTTargetPreservationMatrix(closed); err != nil {
		return UASTTargetPreservationMatrix{}, err
	}
	m = closed
	return m, nil
}

func cloneUASTTargetPreservationMatrix(in UASTTargetPreservationMatrix) UASTTargetPreservationMatrix {
	out := in
	out.Capabilities = append([]string(nil), in.Capabilities...)
	out.Targets = append([]string(nil), in.Targets...)
	cloneSparse := func(m matrixir.SparseMatrix) matrixir.SparseMatrix {
		out := matrixir.NewSparseMatrix(m.Rows, m.Cols)
		m.Each(func(row, col int, value float64) { out.Set(row, col, value) })
		return out
	}
	out.Direct, out.Rewrite, out.Helper = cloneSparse(in.Direct), cloneSparse(in.Rewrite), cloneSparse(in.Helper)
	out.Emulate, out.Runtime, out.Error = cloneSparse(in.Emulate), cloneSparse(in.Runtime), cloneSparse(in.Error)
	out.StatusCounts = make(map[string]map[string]int, len(in.StatusCounts))
	for target, counts := range in.StatusCounts {
		out.StatusCounts[target] = make(map[string]int, len(counts))
		for mode, count := range counts {
			out.StatusCounts[target][mode] = count
		}
	}
	return out
}

func validateUASTTargetPreservationMatrix(m UASTTargetPreservationMatrix) error {
	if len(m.Capabilities) != 334 || len(m.Targets) != len(Backends()) {
		return fmt.Errorf("target preservation matrix axes differ from canonical registry")
	}
	for row := range m.Capabilities {
		for col := range m.Targets {
			ones := m.Direct.At(row, col) + m.Rewrite.At(row, col) + m.Helper.At(row, col) + m.Emulate.At(row, col) + m.Runtime.At(row, col) + m.Error.At(row, col)
			if ones != 1 {
				return fmt.Errorf("target preservation cell %d,%d is not one-hot", row, col)
			}
		}
	}
	return nil
}

func WriteUASTTargetPreservationMatrix(dir string) (UASTTargetPreservationMatrix, error) {
	m, err := UniversalTargetPreservationMatrix()
	if err != nil {
		return UASTTargetPreservationMatrix{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return UASTTargetPreservationMatrix{}, err
	}
	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return UASTTargetPreservationMatrix{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "target_uasf_preservation.json"), encoded, 0o644); err != nil {
		return UASTTargetPreservationMatrix{}, err
	}
	f, err := os.Create(filepath.Join(dir, "target_uasf_preservation.csv"))
	if err != nil {
		return UASTTargetPreservationMatrix{}, err
	}
	w := csv.NewWriter(f)
	if err := w.Write([]string{"target_id", "canonical_semantic_id", "preservation"}); err != nil {
		f.Close()
		return UASTTargetPreservationMatrix{}, err
	}
	for col, target := range m.Targets {
		for row, facet := range m.Capabilities {
			if err := w.Write([]string{target, facet, string(m.Status(row, col))}); err != nil {
				f.Close()
				return UASTTargetPreservationMatrix{}, err
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return UASTTargetPreservationMatrix{}, err
	}
	if err := f.Close(); err != nil {
		return UASTTargetPreservationMatrix{}, err
	}
	return m, nil
}
