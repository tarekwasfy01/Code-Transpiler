package backend

// This file contains the executable part of the automatic repair closure.
// It is intentionally small and schema driven: it completes canonical
// relations only when the same UAST node already carries an unambiguous
// structured operand relation.  It never reads source text, diagnostics or
// target syntax.  The repair synthesizer writes its audit matrices separately;
// this closure is the productive rule those matrices select.

import (
	"encoding/json"
	"strconv"
	"strings"
)

func automaticSyntaxAttributes(role string, ordinal int) map[string]json.RawMessage {
	a := map[string]json.RawMessage{}
	r, _ := json.Marshal(role)
	o, _ := json.Marshal(ordinal)
	a["role"], a["ordinal"] = r, o
	return a
}

func automaticRelationExists(u *UniversalASTDocument, from, to int, role string) bool {
	for _, r := range u.Relations {
		if r.Kind != "syntax.child" || r.From != from || r.To.Domain != "node" || r.To.ID != strconv.Itoa(to) {
			continue
		}
		var got string
		_ = json.Unmarshal(r.Attributes["role"], &got)
		// Preserve the existing canonical edge when the pair already carries
		// any explicit role.  The schema treats one source/target pair as a
		// single executable child relation; adding another role would violate
		// the relation uniqueness contract.
		if got == role || got != "" {
			return true
		}
	}
	return false
}

func automaticDataOperands(u *UniversalASTDocument, from int) []int {
	ids := []int{}
	seen := map[int]bool{}
	for _, r := range u.Relations {
		if r.Kind != "data.operand" || r.From != from || r.To.Domain != "node" {
			continue
		}
		id, err := strconv.Atoi(r.To.ID)
		if err == nil && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	return ids
}

// ApplyAutomaticSemanticRepairClosure is the generated-rule kernel selected
// by the V6 residual synthesizer.  The rules are pure relation completions:
// they preserve existing nodes and only expose already-proved operands through
// the canonical syntax roles required by the existing executor.
func ApplyAutomaticSemanticRepairClosure(u *UniversalASTDocument) {
	if u == nil {
		return
	}
	for _, n := range u.Nodes {
		kind := ""
		if raw := n.Fields["kind"]; len(raw) != 0 {
			_ = json.Unmarshal(raw, &kind)
		}
		kind = strings.ToLower(kind)
		operands := automaticDataOperands(u, n.ID)
		if len(operands) == 0 {
			continue
		}
		roles := []string{}
		switch kind {
		case "expression":
			roles = []string{"expression"}
		case "binary":
			roles = []string{"left", "right"}
		case "unary":
			roles = []string{"value"}
		case "assign":
			roles = []string{"value"}
		case "if", "while":
			roles = []string{"condition"}
		case "call":
			roles = []string{"argument"}
		case "index", "slice":
			roles = []string{"argument"}
		case "for":
			roles = []string{"sequence"}
		}
		for i, child := range operands {
			if i >= len(roles) {
				break
			}
			role := roles[i]
			if automaticRelationExists(u, n.ID, child, role) {
				continue
			}
			u.Relations = append(u.Relations, UniversalASTRelation{
				Kind: "syntax.child", From: n.ID,
				To:         UniversalASTReference{Domain: "node", ID: strconv.Itoa(child)},
				Attributes: automaticSyntaxAttributes(role, i),
			})
		}
	}
}
