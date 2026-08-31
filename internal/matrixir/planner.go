package matrixir

import "fmt"

// CapabilityModel holds support and observation masks. A zero in Support is
// meaningful only when the corresponding Known cell is one.
type CapabilityModel struct {
	Support Matrix // language x feature
	Known   Matrix // language x feature
}

func NewCapabilityModel(support, known Matrix) (CapabilityModel, error) {
	if support.Rows != len(Languages) || support.Cols != len(Features) {
		return CapabilityModel{}, fmt.Errorf("support matrix is %dx%d, want %dx%d", support.Rows, support.Cols, len(Languages), len(Features))
	}
	if known.Rows != support.Rows || known.Cols != support.Cols {
		return CapabilityModel{}, fmt.Errorf("known matrix is %dx%d, want %dx%d", known.Rows, known.Cols, support.Rows, support.Cols)
	}
	for i := range support.Data {
		if support.Data[i] < 0 || support.Data[i] > 1 || known.Data[i] < 0 || known.Data[i] > 1 || support.Data[i] > known.Data[i] {
			return CapabilityModel{}, fmt.Errorf("invalid masked capability at cell %d", i)
		}
	}
	return CapabilityModel{Support: support, Known: known}, nil
}

type TargetPlan struct {
	Target     string
	Required   Vector
	Supported  Vector
	Missing    Vector
	Unknown    Vector
	RequiredN  int
	SupportedN int
	MissingN   int
	UnknownN   int
}

func (m CapabilityModel) Plan(requirements Vector, target string) (TargetPlan, error) {
	if len(requirements) != len(Features) {
		return TargetPlan{}, fmt.Errorf("requirement vector has %d dimensions, want %d", len(requirements), len(Features))
	}
	targetIndex, ok := LanguageIndex(target)
	if !ok {
		return TargetPlan{}, fmt.Errorf("unknown target %q", target)
	}
	plan := TargetPlan{Target: target, Required: append(Vector(nil), requirements...), Supported: make(Vector, len(Features)), Missing: make(Vector, len(Features)), Unknown: make(Vector, len(Features))}
	for feature, required := range requirements {
		if required == 0 {
			continue
		}
		plan.RequiredN++
		known := m.Known.At(targetIndex, feature)
		support := m.Support.At(targetIndex, feature)
		switch {
		case known == 0:
			plan.Unknown[feature] = 1
			plan.UnknownN++
		case support != 0:
			plan.Supported[feature] = 1
			plan.SupportedN++
		default:
			plan.Missing[feature] = 1
			plan.MissingN++
		}
	}
	return plan, nil
}

// DeficitMatrices computes D=R*(K-S)^T and Q=R*(1-K)^T. R is a
// source/program x feature requirement matrix. D counts known missing
// contracts per target, Q counts unknown required contracts per target.
func (m CapabilityModel) DeficitMatrices(requirements Matrix) (missing Matrix, unknown Matrix, err error) {
	if requirements.Cols != len(Features) {
		return Matrix{}, Matrix{}, fmt.Errorf("requirement matrix is %dx%d, want %d feature columns", requirements.Rows, requirements.Cols, len(Features))
	}
	knownMissing := NewMatrix(len(Languages), len(Features))
	unknownCapability := NewMatrix(len(Languages), len(Features))
	for i := range m.Support.Data {
		knownMissing.Data[i] = m.Known.Data[i] - m.Support.Data[i]
		unknownCapability.Data[i] = 1 - m.Known.Data[i]
	}
	missing, err = requirements.Multiply(knownMissing.Transpose())
	if err != nil {
		return Matrix{}, Matrix{}, err
	}
	unknown, err = requirements.Multiply(unknownCapability.Transpose())
	return missing, unknown, err
}
