package backend

import "strings"

// RuntimeTaintReport is an observation of generated target source. It is not
// an IR and carries no program semantics: it records only whether a final
// target document reaches a known compatibility-runtime artifact.
type RuntimeTaintReport struct {
	Artifacts []string `json:"artifacts"`
}

func (r RuntimeTaintReport) Tainted() bool { return len(r.Artifacts) != 0 }

var runtimeArtifacts = []struct {
	id, marker string
}{
	{"emitDispatch", "emitDispatch"},
	{"RuntimeContract", "RuntimeContract"},
	{"targetPrelude", "targetPrelude"},
	{"RValue", "RValue"},
	{"rCall", "rCall("},
	{"r_call", "r_call("},
	{"rNum", "rNum("},
	{"r_num", "r_num("},
	{"r_truth", "r_truth("},
	{"rTruth", "rTruth("},
	{"r_iter", "r_iter("},
	{"rIter", "rIter("},
	{"R2.Iter", "R2.Iter("},
	{"R2", "R2."},
	{"r_exact", "r_exact"},
	{"rExact", "rExact"},
	{"RExact", "RExact"},
	{"fallback.runtime", "fallback.runtime"},
	{"r_output_init", "r_output_init"},
}

// AnalyzeRuntimeTaint is the final-source half of the runtime-taint matrix.
// A native UAST result is accepted only when this report is empty. The same
// detector is used before entering the explicit compatibility fallback.
func AnalyzeRuntimeTaint(source string, helperIDs []string) RuntimeTaintReport {
	seen := map[string]bool{}
	// Helper IDs are intent metadata, not runtime evidence. Native helpers and
	// compatibility helpers share the same registration mechanism; classify the
	// generated final source by its actual runtime markers below.
	_ = helperIDs
	for _, artifact := range runtimeArtifacts {
		if strings.Contains(source, artifact.marker) {
			seen[artifact.id] = true
		}
	}
	artifacts := make([]string, 0, len(seen))
	for artifact := range seen {
		artifacts = append(artifacts, artifact)
	}
	return RuntimeTaintReport{Artifacts: uniqueSorted(artifacts)}
}
