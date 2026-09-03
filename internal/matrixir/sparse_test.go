package matrixir

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"testing"
)

func TestSparseJSONRoundTripAndRejectsAmbiguity(t *testing.T) {
	m := NewSparseMatrix(4, 3)
	m.Set(1, 2, 7)
	m.Set(3, 0, -2)
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var n SparseMatrix
	if err = json.Unmarshal(data, &n); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Dense(), n.Dense()) {
		t.Fatal("COO JSON roundtrip differs")
	}
	for _, bad := range []string{`{"rows":1,"cols":1,"storage":"coo","entries":[[0,0,1],[0,0,2]]}`, `{"rows":1,"cols":1,"storage":"dense","entries":[]}`, `{"rows":1,"cols":1,"storage":"coo","entries":[[1,0,1]]}`} {
		if err = json.Unmarshal([]byte(bad), &n); err == nil {
			t.Fatal("invalid COO accepted")
		}
	}
}

func TestSparseProductsAndClosureAgainstDense(t *testing.T) {
	rng := rand.New(rand.NewSource(714))
	for trial := 0; trial < 30; trial++ {
		n := 1 + rng.Intn(15)
		a, b := NewSparseMatrix(n, n), NewSparseMatrix(n, n)
		da, db := NewMatrix(n, n), NewMatrix(n, n)
		for i := 0; i < n*n/3; i++ {
			r, c := rng.Intn(n), rng.Intn(n)
			v := float64(rng.Intn(7) - 3)
			a.Set(r, c, v)
			da.Set(r, c, v)
			r, c = rng.Intn(n), rng.Intn(n)
			v = float64(rng.Intn(7) - 3)
			b.Set(r, c, v)
			db.Set(r, c, v)
		}
		product, _ := a.Multiply(b)
		expected, _ := da.Multiply(db)
		if !reflect.DeepEqual(product.Dense(), expected) {
			t.Fatal("sparse multiplication differs")
		}
		closure, _ := a.BooleanClosure()
		want, _ := da.BooleanClosure()
		if !reflect.DeepEqual(closure.Dense(), want) {
			t.Fatal("sparse closure differs")
		}
		if !reflect.DeepEqual(a.Transpose().Dense(), da.Transpose()) {
			t.Fatal("sparse transpose differs")
		}
	}
}
func TestLargeGraphStorageAndSparseGrowth(t *testing.T) {
	g, _ := NewGraph("python")
	const n = 20000
	for i := 0; i < n; i++ {
		g.AddNode(Basis(SemanticDimensions, SemIdentifier), "x", "x", i)
		if i > 0 {
			if e := g.Connect(Control, i-1, i); e != nil {
				t.Fatal(e)
			}
		}
	}
	if g.Edges[Control].Rows != n || g.Edges[Control].NonZeros() != n-1 {
		t.Fatal("lost sparse chain")
	}
	if g.Edges[Syntax].NonZeros() != 0 {
		t.Fatal("empty relation allocated entries")
	}
	g.Edges[Control].Set(0, 1, 0)
	if g.Edges[Control].NonZeros() != n-2 {
		t.Fatal("zero did not remove entry")
	}
}
