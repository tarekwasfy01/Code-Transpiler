package backend

import (
	"fmt"
	"math"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

const ExactCallResolutionCapability = "call.resolution.exact.v1"

type callResolutionVisitor struct{ exact bool }

func (*callResolutionVisitor) EnterStatement(*SemanticStatement) error   { return nil }
func (*callResolutionVisitor) LeaveStatement(*SemanticStatement) error   { return nil }
func (*callResolutionVisitor) LeaveExpression(*SemanticExpression) error { return nil }
func (v *callResolutionVisitor) EnterExpression(e *SemanticExpression) error {
	if e.Resolution == nil {
		return nil
	}
	if e.Kind != "call" {
		return fmt.Errorf("call resolution attached to non-call expression")
	}
	v.exact = true
	return validateCallResolution(e.Resolution, len(e.Arguments))
}

func validateCallResolutions(doc *SemanticDocument) (bool, error) {
	v := &callResolutionVisitor{}
	if err := WalkSemanticDocument(doc, v); err != nil {
		return false, err
	}
	return v.exact, nil
}

func validateCallResolution(r *SemanticCallResolution, arguments int) error {
	n, obligations := len(r.Candidates), len(r.Obligations)
	if n == 0 || r.Selected == nil || *r.Selected < 0 || *r.Selected >= n {
		return fmt.Errorf("call resolution has no valid selected candidate")
	}
	if !validDenseMatrix(r.Required) || !validDenseMatrix(r.Satisfied) || !validDenseMatrix(r.ConversionCost) || r.Required.Rows != n || r.Required.Cols != obligations || r.Satisfied.Rows != n || r.Satisfied.Cols != obligations || r.ConversionCost.Rows != n || r.ConversionCost.Cols != arguments || len(r.Priority) != n {
		return fmt.Errorf("call resolution matrix dimensions do not match candidates, obligations, and arguments")
	}
	seenCandidates, seenObligations := map[string]bool{}, map[string]bool{}
	for _, obligation := range r.Obligations {
		if obligation == "" || seenObligations[obligation] {
			return fmt.Errorf("call resolution obligation names must be unique and nonempty")
		}
		seenObligations[obligation] = true
	}
	for _, candidate := range r.Candidates {
		key := candidate.Name + "\x00" + candidate.Declaration
		if candidate.Name == "" || seenCandidates[key] {
			return fmt.Errorf("call resolution candidates must be unique and named")
		}
		seenCandidates[key] = true
	}
	missingPlane := matrixir.NewMatrix(n, obligations)
	for row := 0; row < n; row++ {
		if !finiteNonnegative(r.Priority[row]) {
			return fmt.Errorf("call resolution priority must be finite and nonnegative")
		}
		for col := 0; col < obligations; col++ {
			required, satisfied := r.Required.At(row, col), r.Satisfied.At(row, col)
			if (required != 0 && required != 1) || (satisfied != 0 && satisfied != 1) || satisfied > required {
				return fmt.Errorf("call resolution obligation planes must be binary and satisfied implies required")
			}
			missingPlane.Set(row, col, required*(1-satisfied))
		}
		for col := 0; col < arguments; col++ {
			if !finiteNonnegative(r.ConversionCost.At(row, col)) {
				return fmt.Errorf("call resolution conversion costs must be finite and nonnegative")
			}
		}
	}
	onesObligations := matrixir.NewMatrix(obligations, 1)
	for row := 0; row < obligations; row++ {
		onesObligations.Set(row, 0, 1)
	}
	missing, _ := missingPlane.Multiply(onesObligations)
	onesArguments := matrixir.NewMatrix(arguments, 1)
	for row := 0; row < arguments; row++ {
		onesArguments.Set(row, 0, 1)
	}
	cost, _ := r.ConversionCost.Multiply(onesArguments)
	best, ties := 0, 0
	for row := 0; row < n; row++ {
		if row == 0 || lessResolutionScore(missing.At(row, 0), cost.At(row, 0), r.Priority[row], missing.At(best, 0), cost.At(best, 0), r.Priority[best]) {
			best, ties = row, 1
		} else if sameResolutionScore(missing.At(row, 0), cost.At(row, 0), r.Priority[row], missing.At(best, 0), cost.At(best, 0), r.Priority[best]) {
			ties++
		}
	}
	if ties != 1 {
		return fmt.Errorf("call resolution is ambiguous at the minimum matrix score")
	}
	if *r.Selected != best {
		return fmt.Errorf("selected call candidate %d does not match matrix result %d", *r.Selected, best)
	}
	return nil
}

func finiteNonnegative(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }
func validDenseMatrix(m matrixir.Matrix) bool {
	maxInt := int(^uint(0) >> 1)
	return m.Rows >= 0 && m.Cols >= 0 && (m.Rows == 0 || m.Cols <= maxInt/m.Rows) && len(m.Data) == m.Rows*m.Cols
}
func lessResolutionScore(am, ac, ap, bm, bc, bp float64) bool {
	return am < bm || (am == bm && (ac < bc || (ac == bc && ap < bp)))
}
func sameResolutionScore(am, ac, ap, bm, bc, bp float64) bool {
	return am == bm && ac == bc && ap == bp
}
