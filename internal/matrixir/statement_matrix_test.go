package matrixir

import "testing"

func TestSegmentScaffold(t *testing.T) {
	for _, source := range []string{"c", "java", "cpp", "go"} {
		code := map[string]string{"c": "#include <stdio.h>\nint main(void) {\nprintf(\"%g\\n\", (double)(2 + 3 * 4));\nreturn 0;\n}\n", "java": "public class Main {\npublic static void main(String[] args) {\nSystem.out.println(2 + 3 * 4);\n}\n}\n", "cpp": "#include <iostream>\nint main() {\nauto x = 7;\nstd::cout << (x) << std::endl;\nreturn 0;\n}\n", "go": "for i := 1; i <= 4; i++ {\nx = x+i\n}"}[source]
		ts := Tokenize(source, code)
		st, err := AnalyzeTokenStructure(ts)
		if err != nil {
			t.Fatal(err)
		}
		segments := statementSegments(source, code, ts, st)
		minimum := map[string]int{"c": 5, "java": 5, "cpp": 6, "go": 3}[source]
		if len(segments) != minimum {
			t.Fatalf("%s: got %d segments, want %d: %+v", source, len(segments), minimum, segments)
		}
		if source == "go" && segments[0].trim != "for i := 1; i <= 4; i++ {" {
			t.Fatalf("split loop header: %s", segments[0].trim)
		}
	}
}

func TestNestedElseClosesDeeperIndentation(t *testing.T) {
	for _, source := range []string{"python", "nim"} {
		header := "def f(x):"
		if source == "nim" {
			header = "proc f(x: int): int ="
		}
		code := header + "\n    if x > 0:\n        if x > 2:\n            return 9\n        else:\n            return 8\n    else:\n        return 7\n"
		got, err := Canonicalize(source, code)
		if err != nil {
			t.Fatal(err)
		}
		want := "f <- function(x) {\nif (x > 0) {\nif (x > 2) {\nreturn(9)\n}\nelse {\nreturn(8)\n}\n}\nelse {\nreturn(7)\n}\n}\n"
		if got.R != want {
			t.Fatalf("%s nested branches:\n%s", source, got.R)
		}
	}
}
