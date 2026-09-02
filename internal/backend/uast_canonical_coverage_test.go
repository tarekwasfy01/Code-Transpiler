package backend

import "testing"

func TestCanonicalUniversalASTCoverage(t *testing.T) {
	coverage, err := CanonicalUniversalASTCoverage()
	if err != nil {
		t.Fatal(err)
	}
	if coverage.Structures != 109 || coverage.Relations != 55 || coverage.Facets != 334 || coverage.Fields != 57 || coverage.UASFReady != 334 {
		t.Fatalf("canonical UAST coverage changed: %+v", coverage)
	}
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	for i, facet := range uastEmbedded.Basis.Facets {
		want := "UASF_" + zeroPad(i+1, 4)
		if facet != want {
			t.Fatalf("facet axis[%d]=%q, want %q", i, facet, want)
		}
	}
}

func zeroPad(value, width int) string {
	s := ""
	for n := value; n > 0; n /= 10 {
		s = string(rune('0'+n%10)) + s
	}
	for len(s) < width {
		s = "0" + s
	}
	return s
}
