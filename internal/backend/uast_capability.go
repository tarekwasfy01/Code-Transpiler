package backend

import (
	"fmt"
	"sort"
	"sync"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

type UASTExecutionStatus string

const (
	UASTDirect          UASTExecutionStatus = "direct"
	UASTLowering        UASTExecutionStatus = "lowerable"
	UASTRuntimeRequired UASTExecutionStatus = "runtime-required"
	UASTUnsupported     UASTExecutionStatus = "unsupported"
	UASTUnknown         UASTExecutionStatus = "unknown"
)

// UASTCapabilityPlane is a one-hot status tensor flattened into five sparse
// matrices. Rows are universal items and columns are registered targets.
type UASTCapabilityPlane struct {
	Rows             []string              `json:"rows"`
	Targets          []string              `json:"targets"`
	Direct           matrixir.SparseMatrix `json:"direct"`
	LoweringRequired matrixir.SparseMatrix `json:"lowering_required"`
	RuntimeRequired  matrixir.SparseMatrix `json:"runtime_required"`
	Unsupported      matrixir.SparseMatrix `json:"unsupported"`
	Unknown          matrixir.SparseMatrix `json:"unknown"`
}

type UASTTargetCapabilityMatrix struct {
	Structures   UASTCapabilityPlane                  `json:"structures"`
	Facets       UASTCapabilityPlane                  `json:"facets"`
	Relations    UASTCapabilityPlane                  `json:"relations"`
	Fields       UASTCapabilityPlane                  `json:"fields"`
	StatusCounts map[string]map[string]map[string]int `json:"status_counts"`
}

var projectedUASTRelations = map[string]bool{
	"syntax.child": true, "control.next": true, "data.def_use": true,
	"evaluation.before": true, "binding.refers": true,
	"binding.declares": true, "name.resolves": true, "effect.has": true,
	"type.has": true, "type.origin": true, "operation.kind": true,
	"data.operand": true, "control.true": true, "control.false": true,
	"call.calls": true, "scope.parent": true,
}

var directlyConsumedUASTRelations = map[string]bool{
	// validateDirectProjectedRelations consumes this complete relation batch by
	// rebuilding it from the UAST evidence matrices and rejecting any mismatch.
	// The runtime/codegen additionally reads syntax, control, call and operand
	// relations for lowering.  Both are direct UAST consumers.
	"syntax.child": true, "control.next": true,
	"control.true": true, "control.false": true,
	"call.calls": true, "data.operand": true,
	"data.def_use": true, "evaluation.before": true,
	"binding.refers": true, "binding.declares": true, "name.resolves": true,
	"effect.has": true, "type.has": true, "type.origin": true,
	"operation.kind": true, "scope.parent": true,
}

func newCapabilityPlane(rows, targets []string) UASTCapabilityPlane {
	shape := func() matrixir.SparseMatrix { return matrixir.NewSparseMatrix(len(rows), len(targets)) }
	return UASTCapabilityPlane{Rows: append([]string(nil), rows...), Targets: append([]string(nil), targets...), Direct: shape(), LoweringRequired: shape(), RuntimeRequired: shape(), Unsupported: shape(), Unknown: shape()}
}

func (p *UASTCapabilityPlane) set(row, col int, status UASTExecutionStatus) {
	switch status {
	case UASTDirect:
		p.Direct.Set(row, col, 1)
	case UASTLowering:
		p.LoweringRequired.Set(row, col, 1)
	case UASTRuntimeRequired:
		p.RuntimeRequired.Set(row, col, 1)
	case UASTUnsupported:
		p.Unsupported.Set(row, col, 1)
	default:
		p.Unknown.Set(row, col, 1)
	}
}

func (p UASTCapabilityPlane) Status(row, col int) UASTExecutionStatus {
	if p.Direct.At(row, col) != 0 {
		return UASTDirect
	}
	if p.LoweringRequired.At(row, col) != 0 {
		return UASTLowering
	}
	if p.RuntimeRequired.At(row, col) != 0 {
		return UASTRuntimeRequired
	}
	if p.Unsupported.At(row, col) != 0 {
		return UASTUnsupported
	}
	return UASTUnknown
}

// Target capability contracts are derived solely from immutable embedded UAST
// data and static TargetSpecs. Cache the completed matrix so the real
// Source→UAST→Target path does not reconstruct every projection plane for
// every emitted node or target cell.
var universalTargetCapabilityMatrixCache struct {
	once sync.Once
	m    UASTTargetCapabilityMatrix
	err  error
}

func UniversalTargetCapabilityMatrix() (UASTTargetCapabilityMatrix, error) {
	universalTargetCapabilityMatrixCache.once.Do(func() {
		universalTargetCapabilityMatrixCache.m, universalTargetCapabilityMatrixCache.err = buildUniversalTargetCapabilityMatrix()
	})
	return universalTargetCapabilityMatrixCache.m, universalTargetCapabilityMatrixCache.err
}

func buildUniversalTargetCapabilityMatrix() (UASTTargetCapabilityMatrix, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return UASTTargetCapabilityMatrix{}, err
	}
	targets := []string{}
	for _, backend := range Backends() {
		targets = append(targets, backend.ID)
	}
	projection, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return UASTTargetCapabilityMatrix{}, err
	}
	// The target capability plane follows the same structure contracts as the
	// projector.  This avoids the old independent direct-structure list.
	executableStructures := map[string]bool{}
	for _, contract := range projection.Contracts {
		if contract.Implemented {
			executableStructures[contract.StructureKind] = true
		}
	}
	executableFacets := map[string]bool{}
	for structural := range executableStructures {
		row := indexOf(uastEmbedded.Basis.StructuralKinds, structural)
		for col, facet := range uastEmbedded.Basis.Facets {
			if uastEmbedded.Basis.StructuralFacetSeed.At(row, col) != 0 {
				executableFacets[facet] = true
			}
		}
	}
	m := UASTTargetCapabilityMatrix{
		Structures: newCapabilityPlane(uastEmbedded.Basis.StructuralKinds, targets),
		Facets:     newCapabilityPlane(uastEmbedded.Basis.Facets, targets),
		Relations:  newCapabilityPlane(uastEmbedded.Basis.ConcreteRelations, targets),
		Fields:     newCapabilityPlane(uastEmbedded.Basis.Fields, targets),
	}
	profileRow := func(target string) int {
		return indexOf(uastEmbedded.Basis.Languages, universalASTProfileLanguage(target))
	}
	for row, structural := range m.Structures.Rows {
		seeded := []int{}
		for facet := range m.Facets.Rows {
			if uastEmbedded.Basis.StructuralFacetSeed.At(row, facet) != 0 {
				seeded = append(seeded, facet)
			}
		}
		for col, target := range targets {
			status := UASTUnknown
			if executableStructures[structural] {
				// The shared TargetSpec/NativeEmitter contract is already
				// runtime-free for every registered target.  Runtime remains a
				// last-resort decision made from the actual emitted source, not a
				// language-wide matrix default.
				status = UASTDirect
			} else if profile := profileRow(target); profile >= 0 {
				available := false
				for _, facet := range seeded {
					available = available || uastEmbedded.Basis.CoverageUpper.At(profile, facet) > 0
				}
				if !available && len(seeded) > 0 {
					status = UASTUnsupported
				}
			}
			m.Structures.set(row, col, status)
		}
	}
	for row, facet := range m.Facets.Rows {
		for col, target := range targets {
			status := UASTUnknown
			if executableFacets[facet] {
				status = UASTDirect
			} else if profile := profileRow(target); profile >= 0 && uastEmbedded.Basis.CoverageUpper.At(profile, row) == 0 {
				status = UASTUnsupported
			}
			m.Facets.set(row, col, status)
		}
	}
	for row, relation := range m.Relations.Rows {
		for col := range targets {
			status := UASTUnknown
			if directlyConsumedUASTRelations[relation] {
				status = UASTDirect
			} else if projectedUASTRelations[relation] {
				status = UASTLowering
			}
			m.Relations.set(row, col, status)
		}
	}
	for row, field := range m.Fields.Rows {
		for col := range targets {
			status := UASTUnknown
			if directUASTFields[field] || field == "source_span" || field == "semantic_facets" {
				status = UASTDirect
			}
			m.Fields.set(row, col, status)
		}
	}
	if err := validateUASTCapabilityMatrix(m); err != nil {
		return UASTTargetCapabilityMatrix{}, err
	}
	m.StatusCounts = uastCapabilityStatusCounts(m)
	return m, nil
}

func uastCapabilityStatusCounts(m UASTTargetCapabilityMatrix) map[string]map[string]map[string]int {
	out := map[string]map[string]map[string]int{}
	statuses := []UASTExecutionStatus{UASTDirect, UASTLowering, UASTRuntimeRequired, UASTUnsupported, UASTUnknown}
	for category, plane := range map[string]UASTCapabilityPlane{"structure": m.Structures, "facet": m.Facets, "relation": m.Relations, "field": m.Fields} {
		out[category] = map[string]map[string]int{}
		for col, target := range plane.Targets {
			counts := map[string]int{}
			for _, status := range statuses {
				counts[string(status)] = 0
			}
			for row := range plane.Rows {
				counts[string(plane.Status(row, col))]++
			}
			out[category][target] = counts
		}
	}
	return out
}

func validateUASTCapabilityMatrix(m UASTTargetCapabilityMatrix) error {
	for name, plane := range map[string]UASTCapabilityPlane{"structures": m.Structures, "facets": m.Facets, "relations": m.Relations, "fields": m.Fields} {
		for row := range plane.Rows {
			for col := range plane.Targets {
				ones := plane.Direct.At(row, col) + plane.LoweringRequired.At(row, col) + plane.RuntimeRequired.At(row, col) + plane.Unsupported.At(row, col) + plane.Unknown.At(row, col)
				if ones != 1 {
					return fmt.Errorf("UAST %s capability cell %d,%d is not one-hot", name, row, col)
				}
			}
		}
	}
	return nil
}

// validateUASTTargetCapabilities multiplies the demanded structure, facet and
// relation vectors by the target status planes. Unsupported or unknown demand
// is rejected; direct, lowerable and runtime-required demand is explicit.
func validateUASTTargetCapabilities(u *UniversalASTDocument, target string) error {
	m, err := UniversalTargetCapabilityMatrix()
	if err != nil {
		return err
	}
	target = NormalizeLanguage(target)
	structures, facets, relations, fields := []string{}, []string{}, []string{}, []string{}
	for i := range u.Nodes {
		structures = append(structures, u.Nodes[i].StructuralKind)
		facets = append(facets, u.Nodes[i].SemanticFacets...)
		for field := range u.Nodes[i].Fields {
			fields = append(fields, field)
		}
	}
	for _, relation := range u.Relations {
		relations = append(relations, relation.Kind)
	}
	for _, demand := range []struct {
		name   string
		values []string
		plane  UASTCapabilityPlane
	}{{"structures", structures, m.Structures}, {"facets", facets, m.Facets}, {"relations", relations, m.Relations}, {"fields", fields, m.Fields}} {
		if err := validateUASTCapabilityDemand(target, demand.name, demand.values, demand.plane); err != nil {
			return err
		}
	}
	return nil
}

func validateUASTCapabilityDemand(target, category string, values []string, plane UASTCapabilityPlane) error {
	col := indexOf(plane.Targets, target)
	if col < 0 {
		return fmt.Errorf("unknown target %q", target)
	}
	demand := matrixir.NewSparseMatrix(1, len(plane.Rows))
	for _, value := range values {
		row := indexOf(plane.Rows, value)
		if row >= 0 {
			demand.Set(0, row, 1)
		}
	}
	unsupported, err := demand.Multiply(plane.Unsupported)
	if err != nil {
		return err
	}
	unknown, err := demand.Multiply(plane.Unknown)
	if err != nil {
		return err
	}
	if unsupported.At(0, col) == 0 && unknown.At(0, col) == 0 {
		return nil
	}
	bad := []string{}
	for row, value := range plane.Rows {
		if demand.At(0, row) != 0 && (plane.Unsupported.At(row, col) != 0 || plane.Unknown.At(row, col) != 0) {
			bad = append(bad, value)
		}
	}
	sort.Strings(bad)
	return fmt.Errorf("target %q cannot execute UAST %s %v", target, category, bad)
}
