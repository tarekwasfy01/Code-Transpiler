package backend

import (
	"strings"
	"testing"
)

// R uses two binary spellings that are not portable target operators.  This
// validates the shared target-legalization relation through the real UAST
// direct projector, rather than testing a source-language special path.
func TestCanonicalRArithmeticIsTargetLegalized(t *testing.T) {
	for _, op := range []string{"%%", "%/%"} {
		p := NewSemanticProgram(&BlockStmt{List: []Stmt{
			&AssignStmt{Name: "result", Op: "<-", Value: &BinaryExpr{
				Op: op,
				L:  &LiteralExpr{Kind: "number", Text: "7"},
				R:  &LiteralExpr{Kind: "number", Text: "3"},
			}},
		}}, "eager_left_to_right")
		for _, target := range Backends() {
			t.Run(op+"/"+target.ID, func(t *testing.T) {
				out, err := EmitSemanticDirect(target.ID, p)
				if err != nil {
					t.Fatalf("direct emission: %v", err)
				}
				// R itself owns these spellings; every other target must receive
				// its legal native representation rather than R syntax.
				if target.ID != "r" && strings.Contains(out, op) {
					t.Fatalf("source-only operator %q leaked into %s output:\n%s", op, target.ID, out)
				}
			})
		}
	}
}
