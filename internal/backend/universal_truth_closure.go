package backend

import (
	"encoding/json"
	"strconv"
)

// ApplyUniversalTruthClosure applies only presence implications that are
// already witnessed by canonical syntax relations.  It never invents a value,
// binding, type, target, or operand identity: an explicit child relation is
// merely exposed through the existing data.operand relation used by the
// executor.  The rule is language and target neutral and therefore belongs in
// the existing UAST enrichment pass rather than in a second semantic system.
func ApplyUniversalTruthClosure(u *UniversalASTDocument) {
	if u == nil {
		return
	}
	allowed := map[string]bool{
		"left": true, "right": true, "value": true, "operand": true,
		"argument": true, "base": true, "receiver": true,
		"object": true, "condition": true, "sequence": true,
	}
	seen := map[string]bool{}
	for _, r := range u.Relations {
		seen[r.Kind+":"+strconv.Itoa(r.From)+":"+r.To.Domain+":"+r.To.ID] = true
	}
	for _, r := range u.Relations {
		if r.Kind != "syntax.child" || r.To.Domain != "node" || r.Attributes == nil {
			continue
		}
		var role string
		_ = json.Unmarshal(r.Attributes["role"], &role)
		if !allowed[role] {
			continue
		}
		key := "data.operand:" + strconv.Itoa(r.From) + ":node:" + r.To.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		u.Relations = append(u.Relations, UniversalASTRelation{
			Kind: "data.operand", From: r.From,
			To: UniversalASTReference{Domain: "node", ID: r.To.ID},
		})
	}
}
