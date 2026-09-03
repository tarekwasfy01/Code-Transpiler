package backend

import "testing"

func TestCapabilityMatrixProjection(t *testing.T) {
	m := SemanticCapabilityMatrix([]string{"future.feature", "core"})
	for i := range m.Features {
		for j := range m.Targets {
			total := m.Native.At(i, j) + m.Lowering.At(i, j) + m.Emulated.At(i, j) + m.Unsupported.At(i, j)
			if total != 1 {
				t.Fatalf("status planes not exclusive at %d,%d", i, j)
			}
		}
	}
	for _, requirements := range [][]string{{"core"}, {"future.feature"}, {"future.feature", "integer.uint64.exact"}} {
		rejected, err := m.RejectedTargets(requirements)
		if err != nil {
			t.Fatal(err)
		}
		for j, target := range m.Targets {
			want := 0.0
			for _, f := range requirements {
				if BackendCapability(f, target).Status == CapabilityUnsupported {
					want++
				}
			}
			if rejected.At(0, j) != want {
				t.Fatalf("wrong projection for %s", target)
			}
		}
	}
	if _, err := m.RejectedTargets([]string{"not.in.matrix"}); err == nil {
		t.Fatal("unknown requirement accepted")
	}
}
