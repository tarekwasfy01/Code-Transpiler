package matrixir

import "fmt"

// Matrix is a dense row-major matrix. The semantic graphs are sparse in
// meaning, but dense storage keeps dimensions and multiplication auditable at
// the current program sizes. Storage can later change without changing the IR.
type Matrix struct {
	Rows int
	Cols int
	Data []float64
}

func NewMatrix(rows, cols int) Matrix {
	if rows < 0 || cols < 0 {
		panic("matrix dimensions must be non-negative")
	}
	return Matrix{Rows: rows, Cols: cols, Data: make([]float64, rows*cols)}
}

func MatrixFromRows(rows [][]float64) (Matrix, error) {
	if len(rows) == 0 {
		return NewMatrix(0, 0), nil
	}
	m := NewMatrix(len(rows), len(rows[0]))
	for i, row := range rows {
		if len(row) != m.Cols {
			return Matrix{}, fmt.Errorf("ragged matrix at row %d: got %d columns, want %d", i, len(row), m.Cols)
		}
		copy(m.Data[i*m.Cols:(i+1)*m.Cols], row)
	}
	return m, nil
}

func (m Matrix) At(row, col int) float64         { return m.Data[row*m.Cols+col] }
func (m Matrix) Set(row, col int, value float64) { m.Data[row*m.Cols+col] = value }

func (m Matrix) Row(row int) Vector {
	out := make(Vector, m.Cols)
	copy(out, m.Data[row*m.Cols:(row+1)*m.Cols])
	return out
}

func (m Matrix) Transpose() Matrix {
	out := NewMatrix(m.Cols, m.Rows)
	for i := 0; i < m.Rows; i++ {
		for j := 0; j < m.Cols; j++ {
			out.Set(j, i, m.At(i, j))
		}
	}
	return out
}

func (m Matrix) Multiply(right Matrix) (Matrix, error) {
	if m.Cols != right.Rows {
		return Matrix{}, fmt.Errorf("matrix product dimension mismatch: %dx%d times %dx%d", m.Rows, m.Cols, right.Rows, right.Cols)
	}
	out := NewMatrix(m.Rows, right.Cols)
	for i := 0; i < m.Rows; i++ {
		for k := 0; k < m.Cols; k++ {
			left := m.At(i, k)
			if left == 0 {
				continue
			}
			for j := 0; j < right.Cols; j++ {
				out.Data[i*out.Cols+j] += left * right.At(k, j)
			}
		}
	}
	return out, nil
}

func (m Matrix) Hadamard(right Matrix) (Matrix, error) {
	if m.Rows != right.Rows || m.Cols != right.Cols {
		return Matrix{}, fmt.Errorf("Hadamard dimension mismatch: %dx%d and %dx%d", m.Rows, m.Cols, right.Rows, right.Cols)
	}
	out := NewMatrix(m.Rows, m.Cols)
	for i := range out.Data {
		out.Data[i] = m.Data[i] * right.Data[i]
	}
	return out, nil
}

func (m Matrix) Threshold() Matrix {
	out := NewMatrix(m.Rows, m.Cols)
	for i, value := range m.Data {
		if value != 0 {
			out.Data[i] = 1
		}
	}
	return out
}

// BooleanClosure computes the transitive closure with boolean addition and
// multiplication. It deliberately does not add identity edges.
func (m Matrix) BooleanClosure() (Matrix, error) {
	if m.Rows != m.Cols {
		return Matrix{}, fmt.Errorf("boolean closure requires square matrix, got %dx%d", m.Rows, m.Cols)
	}
	out := m.Threshold()
	for k := 0; k < out.Rows; k++ {
		for i := 0; i < out.Rows; i++ {
			if out.At(i, k) == 0 {
				continue
			}
			for j := 0; j < out.Cols; j++ {
				if out.At(k, j) != 0 {
					out.Set(i, j, 1)
				}
			}
		}
	}
	return out, nil
}

type Vector []float64

func Basis(size, index int) Vector {
	v := make(Vector, size)
	v[index] = 1
	return v
}

func (v Vector) Dot(right Vector) (float64, error) {
	if len(v) != len(right) {
		return 0, fmt.Errorf("vector dimension mismatch: %d and %d", len(v), len(right))
	}
	var out float64
	for i := range v {
		out += v[i] * right[i]
	}
	return out, nil
}

func (v Vector) Or(right Vector) (Vector, error) {
	if len(v) != len(right) {
		return nil, fmt.Errorf("vector dimension mismatch: %d and %d", len(v), len(right))
	}
	out := make(Vector, len(v))
	for i := range v {
		if v[i] != 0 || right[i] != 0 {
			out[i] = 1
		}
	}
	return out, nil
}
