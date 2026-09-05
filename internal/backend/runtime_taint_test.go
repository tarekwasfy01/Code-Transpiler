package backend

import "testing"

func TestAnalyzeRuntimeTaintUsesFinalSourceOnly(t *testing.T) {
	if taint := AnalyzeRuntimeTaint("package main\nfunc main() {}\n", nil); taint.Tainted() {
		t.Fatalf("native source tainted: %v", taint.Artifacts)
	}
	taint := AnalyzeRuntimeTaint("R2.Call(\"runtime\")\nr_call(\"x\")\n", []string{"runtime.list"})
	if !taint.Tainted() {
		t.Fatal("runtime source was accepted")
	}
	// Helper IDs are not taint evidence by themselves; only markers in the
	// generated final source are classified as compatibility-runtime artifacts.
	want := map[string]bool{"R2": true, "r_call": true}
	for _, artifact := range taint.Artifacts {
		delete(want, artifact)
	}
	if len(want) != 0 {
		t.Fatalf("missing taint artifacts: %v", want)
	}
}
