package backend

import (
	_ "embed"
	"encoding/csv"
	"strings"
)

//go:embed compiler_oracle_evidence.csv
var embeddedCompilerOracleEvidence []byte

// CompilerOracleEvidence returns the structured compiler-operation evidence
// harvested from LLVM/Clang sources. It is read-only evidence used by the
// primitive compiler; it is never treated as source syntax or a parser.
func CompilerOracleEvidence() []map[string]string {
	r := csv.NewReader(strings.NewReader(string(embeddedCompilerOracleEvidence)))
	rows, err := r.ReadAll()
	if err != nil || len(rows) == 0 {
		return nil
	}
	out := make([]map[string]string, 0, len(rows)-1)
	for _, row := range rows[1:] {
		if len(row) != len(rows[0]) {
			continue
		}
		m := make(map[string]string, len(row))
		for i, k := range rows[0] {
			m[k] = row[i]
		}
		out = append(out, m)
	}
	return out
}

func compilerOraclePrimitiveIDs() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, row := range CompilerOracleEvidence() {
		if p := strings.TrimSpace(row["canonical_primitive"]); p != "" && p != "UNMAPPED_ORACLE_SEMANTICS" && p != "COMPILER_IMPLEMENTATION_DETAIL" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}
