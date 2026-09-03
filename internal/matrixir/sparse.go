package matrixir

import (
	"encoding/json"
	"fmt"
	"sort"
)

// SparseMatrix stores only nonzero entries, indexed by row then column. Its
// coordinates do not depend on dimensions, so graph growth never copies edges.
// Small semantic/projection matrices deliberately retain the dense Matrix type.
type SparseMatrix struct {
	Rows    int
	Cols    int
	entries map[int]map[int]float64
}

func NewSparseMatrix(rows, cols int) SparseMatrix {
	if rows < 0 || cols < 0 {
		panic("negative sparse matrix dimensions")
	}
	return SparseMatrix{Rows: rows, Cols: cols, entries: map[int]map[int]float64{}}
}
func (m SparseMatrix) check(r, c int) {
	if r < 0 || c < 0 || r >= m.Rows || c >= m.Cols {
		panic("sparse coordinate outside dimensions")
	}
}
func (m SparseMatrix) At(r, c int) float64 { m.check(r, c); return m.entries[r][c] }
func (m SparseMatrix) Set(r, c int, v float64) {
	m.check(r, c)
	if v == 0 {
		delete(m.entries[r], c)
		if len(m.entries[r]) == 0 {
			delete(m.entries, r)
		}
		return
	}
	if m.entries[r] == nil {
		m.entries[r] = map[int]float64{}
	}
	m.entries[r][c] = v
}
func (m *SparseMatrix) Grow(rows, cols int) {
	if rows < m.Rows || cols < m.Cols {
		panic("sparse Grow cannot shrink")
	}
	if m.entries == nil {
		m.entries = map[int]map[int]float64{}
	}
	m.Rows, m.Cols = rows, cols
}
func (m SparseMatrix) NonZeros() int {
	n := 0
	for _, r := range m.entries {
		n += len(r)
	}
	return n
}
func (m SparseMatrix) Sum() float64 {
	var sum float64
	m.Each(func(_, _ int, v float64) { sum += v })
	return sum
}

// Each uses stable coordinate order, including in floating-point reductions.
func (m SparseMatrix) Each(f func(int, int, float64)) {
	rows := make([]int, 0, len(m.entries))
	for r := range m.entries {
		rows = append(rows, r)
	}
	sort.Ints(rows)
	for _, r := range rows {
		cols := make([]int, 0, len(m.entries[r]))
		for c := range m.entries[r] {
			cols = append(cols, c)
		}
		sort.Ints(cols)
		for _, c := range cols {
			f(r, c, m.entries[r][c])
		}
	}
}
func (m SparseMatrix) Dense() Matrix {
	out := NewMatrix(m.Rows, m.Cols)
	m.Each(func(r, c int, v float64) { out.Set(r, c, v) })
	return out
}
func (m SparseMatrix) Transpose() SparseMatrix {
	out := NewSparseMatrix(m.Cols, m.Rows)
	m.Each(func(r, c int, v float64) { out.Set(c, r, v) })
	return out
}
func (m SparseMatrix) Multiply(b SparseMatrix) (SparseMatrix, error) {
	if m.Cols != b.Rows {
		return SparseMatrix{}, fmt.Errorf("sparse product dimension mismatch")
	}
	out := NewSparseMatrix(m.Rows, b.Cols)
	m.Each(func(r, k int, a float64) {
		cols := make([]int, 0, len(b.entries[k]))
		for c := range b.entries[k] {
			cols = append(cols, c)
		}
		sort.Ints(cols)
		for _, c := range cols {
			out.Set(r, c, out.At(r, c)+a*b.entries[k][c])
		}
	})
	return out, nil
}

// BooleanClosure enumerates reachable coordinates, not an n*n temporary array.
// Dense reachability still requires quadratic output; sparsity is no guarantee
// that the mathematical result itself is sparse. No identity edges are added.
func (m SparseMatrix) BooleanClosure() (SparseMatrix, error) {
	if m.Rows != m.Cols {
		return SparseMatrix{}, fmt.Errorf("closure requires square matrix")
	}
	out := NewSparseMatrix(m.Rows, m.Cols)
	for r := range m.entries {
		seen := map[int]bool{}
		queue := []int{}
		for c := range m.entries[r] {
			queue = append(queue, c)
			seen[c] = true
		}
		for head := 0; head < len(queue); head++ {
			c := queue[head]
			out.Set(r, c, 1)
			for next := range m.entries[c] {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
	}
	return out, nil
}

// Sparse serialization is explicit; legacy dense audit fields call Dense at
// the boundary only. Encoding must never allocate rows*cols implicitly.
func (m SparseMatrix) MarshalJSON() ([]byte, error) {
	entries := make([][3]float64, 0, m.NonZeros())
	m.Each(func(r, c int, v float64) { entries = append(entries, [3]float64{float64(r), float64(c), v}) })
	return json.Marshal(struct {
		Rows    int          `json:"rows"`
		Cols    int          `json:"cols"`
		Storage string       `json:"storage"`
		Entries [][3]float64 `json:"entries"`
	}{m.Rows, m.Cols, "coo", entries})
}

// UnmarshalJSON accepts only the explicit COO representation written above.
// It rejects duplicate or out-of-range coordinates so a serialized semantic
// graph cannot silently change meaning while it is loaded.
func (m *SparseMatrix) UnmarshalJSON(data []byte) error {
	var wire struct {
		Rows    int          `json:"rows"`
		Cols    int          `json:"cols"`
		Storage string       `json:"storage"`
		Entries [][3]float64 `json:"entries"`
	}
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if wire.Rows < 0 || wire.Cols < 0 || wire.Storage != "coo" {
		return fmt.Errorf("invalid sparse COO matrix")
	}
	out := NewSparseMatrix(wire.Rows, wire.Cols)
	for _, entry := range wire.Entries {
		r, c := int(entry[0]), int(entry[1])
		if float64(r) != entry[0] || float64(c) != entry[1] || r < 0 || c < 0 || r >= wire.Rows || c >= wire.Cols || entry[2] == 0 {
			return fmt.Errorf("invalid sparse COO entry")
		}
		if out.At(r, c) != 0 {
			return fmt.Errorf("duplicate sparse COO entry")
		}
		out.Set(r, c, entry[2])
	}
	*m = out
	return nil
}
