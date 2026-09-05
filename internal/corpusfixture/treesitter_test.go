package corpusfixture

import "testing"

func TestSplitTreeSitterFixture(t *testing.T) {
	long := "\n\n--------------------------------------------------------------------------------\n\n(program (identifier))"
	short := "\n---\n\n(program (identifier))"
	cases := []struct{ name, source, wantSource, wantTree string }{
		{"long", "x = 1" + long, "x = 1", "(program (identifier))"},
		{"short", "x = 1" + short, "x = 1", "(program (identifier))"},
		{"minus", "x---y\n// ------ comment\n", "x---y\n// ------ comment\n", ""},
		{"explicit-tree", "x\n---\n(program embedded)", "x", "(program explicit)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expected := ""
			if tc.name == "explicit-tree" {
				expected = "(program explicit)"
			}
			gotSource, gotTree := SplitTreeSitterFixture(tc.source, expected)
			if gotSource != tc.wantSource || gotTree != tc.wantTree {
				t.Fatalf("source=%q tree=%q", gotSource, gotTree)
			}
		})
	}
}
