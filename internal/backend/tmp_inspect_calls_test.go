package backend

import (
	"fmt"
	"os"
	"testing"
)

func TestInspectCalls(t *testing.T) {
	b, err := os.ReadFile(`../../outputs/sp-total-closure/compiler-backend.sp`)
	if err != nil { t.Fatal(err) }
	p, err := ParseSemanticSP(b)
	if err != nil { t.Fatal(err) }
	u, err := canonicalUniversalAST(p)
	if err != nil { t.Fatal(err) }
	g, err := newUASTExecutionGraph(u)
	if err != nil { t.Fatal(err) }
	seen := 0
	for id, c := range g.common {
		if c.Kind != "call" { continue }
		target, ok, _ := g.callTarget(id)
		if !ok || (g.common[target].Kind != "identifier" && g.common[target].Kind != "function") {
			fmt.Printf("CALL id=%d target=%d target_kind=%s target_name=%q target_type=%+v op=%+v args=%d fields=%v\n", id, target, g.common[target].Kind, g.common[target].Name, g.common[target].Type, c.Operation, len(g.many(id, "argument")), g.nodes[target].Fields)
			seen++
			if seen >= 20 { break }
		}
	}
}
