package backend

import "testing"

func TestStripCSharpTriviaDirectivesPreservesLines(t *testing.T) {
	in := "#nullable disable\r\nclass C {\n#pragma warning disable\n#region body\nint F() { return 1; }\n#endregion\n#line 42 \"x.cs\"\n}\n"
	out := stripCSharpTriviaDirectives(in)
	if len(out) != len(in) {
		t.Fatalf("directive filtering changed source length: got %d want %d", len(out), len(in))
	}
	for _, want := range []string{"class C", "int F() { return 1; }"} {
		if !containsText(out, want) {
			t.Fatalf("filtered source lost executable text %q: %q", want, out)
		}
	}
	for _, line := range []string{"#nullable", "#pragma", "#region", "#endregion", "#line"} {
		if containsText(out, line) {
			t.Fatalf("trivia directive %q remained in filtered source", line)
		}
	}
}

func containsText(s, needle string) bool {
	for i := 0; i+len(needle) <= len(s); i++ {
		if s[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
