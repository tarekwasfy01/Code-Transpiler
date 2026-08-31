package manytomany

import (
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"strings"
	"testing"
)

func TestRangeMatrixPreservesBoundsAndControl(t *testing.T) {
	cases := []struct{ name, source, code, want string }{
		{"empty", "python", "x = 0\nfor i in range(4, 2):\n    x = x + 1\nprint(x)", "0"},
		{"negative_start", "python", "x = 0\nfor i in range(-2, 2):\n    x = x + i\nprint(x)", "-2"},
		{"bound_snapshot", "python", "n = 4\nx = 0\nfor i in range(1, n):\n    x = x + i\n    n = 1\nprint(x)", "6"},
		{"condition_reevaluation", "c", "int main(void) { int n = 4; int x = 0; for(int i = 1; i <= n; ++i) { x = x + i; n = 2; } printf(\"%g\\n\", (double)x); return 0; }", "3"},
		{"continue_increment", "go", "func main() { x := 0; for i := 1; i <= 3; i++ { if i == 2 { continue }; x = x + i }; fmt.Println(x) }", "4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			program, err := Parse(tc.source, tc.code)
			if err != nil {
				t.Fatal(err)
			}
			output, err := backend.Run(program.CanonicalR)
			if err != nil {
				t.Fatalf("%v\n%s", err, program.CanonicalR)
			}
			if strings.TrimSpace(output) != tc.want {
				t.Fatalf("got %q, want %s\n%s", output, tc.want, program.CanonicalR)
			}
		})
	}
}

func TestRangeMatrixRejectsUnmodeledSteps(t *testing.T) {
	_, err := Parse("python", "for i in range(1, 5, 2):\n    print(i)")
	if err == nil {
		t.Fatal("unmodeled nonunit step must not silently become unit step")
	}
}
