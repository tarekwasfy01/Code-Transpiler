package main

import (
	"testing"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

func TestCanonicalSemanticEventIDsUnique(t *testing.T) {
	rows, err := readCSV("../../matrices/frontend_closure/tree_sitter_input/15_corpus_cases.csv")
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		source, tree := corpusSourceAndTree(row["source_text"], row["expected_tree"])
		if tree == "" {
			continue
		}
		p, err := matrixir.Canonicalize(row["language"], source)
		if err != nil {
			continue
		}
		seen := map[int]bool{}
		for _, event := range p.SemanticEvents {
			if seen[event.ID] {
				t.Fatalf("%s/%s duplicate %d events=%+v", row["language"], row["case_name"], event.ID, p.SemanticEvents)
			}
			seen[event.ID] = true
		}
		for _, event := range p.SemanticEvents {
			for _, role := range event.Roles {
				if !seen[role.ChildNodeID] {
					t.Fatalf("%s/%s dangling %d -> %d events=%+v", row["language"], row["case_name"], event.ID, role.ChildNodeID, p.SemanticEvents)
				}
			}
		}
	}
}
