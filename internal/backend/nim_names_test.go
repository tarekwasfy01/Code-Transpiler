package backend

import (
	"strings"
	"testing"
)

func TestNimNamesRemainDistinctUnderStyleEquality(t *testing.T) {
	g := &targetGen{target: "nim", usedNames: map[string]bool{}}
	seen := map[string]string{}
	for _, source := range []string{"rateLimit", "rate_limit", "ratelimit", "x.y", "x_y", "type", "result", "rCall", "r_call", "r2mtarg1", "_x", "x_"} {
		name := g.name(source)
		if name[0] < 'a' || name[0] > 'z' || strings.Contains(name, "__") || strings.HasSuffix(name, "_") {
			t.Fatalf("invalid Nim identifier %q", name)
		}
		key := strings.ToLower(strings.ReplaceAll(name, "_", ""))
		if old, ok := seen[key]; ok {
			t.Fatalf("%s and %s alias under Nim identifier equality", old, source)
		}
		seen[key] = source
	}
	for i := 0; i < 100; i++ {
		name := g.freshName("arg")
		key := strings.ToLower(strings.ReplaceAll(name, "_", ""))
		if old, ok := seen[key]; ok {
			t.Fatalf("temporary aliases %s", old)
		}
		seen[key] = "temporary"
	}
}
