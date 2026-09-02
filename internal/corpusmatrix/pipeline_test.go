package corpusmatrix

import (
	"archive/zip"
	"compress/gzip"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeAndDedupePreservesProvenance(t *testing.T) {
	a := makeRecord("FOLDER", "a", "python", "x = 1\r\n", "pkg-a", "a.py", "", 0, "a.py")
	b := makeRecord("MLCPD", "b", "python", "x = 1\n", "pkg-b", "b.py", "MLCPD_STRUCTURAL", 0, "b.py")
	out, dup := Dedupe([]CorpusRecord{b, a})
	if dup != 1 || len(out) != 1 {
		t.Fatalf("dedupe=%d len=%d", dup, len(out))
	}
	if !strings.Contains(out[0].Provenance, "a.py") || !strings.Contains(out[0].Provenance, "MLCPD:b") {
		t.Fatalf("provenance not preserved: %q", out[0].Provenance)
	}
}

func TestLoadMLCPDCSVFixture(t *testing.T) {
	p := filepath.Join(t.TempDir(), "mlcpd.csv")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"id", "language", "code", "lang_specific_parse", "universal_schema", "num_errors"})
	_ = w.Write([]string{"r1", "Python", "x = 1", `{"node":"Assign"}`, `{"kind":"assignment"}`, "0"})
	w.Flush()
	_ = f.Close()
	rs, err := LoadMLCPD(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].CorpusSource != "MLCPD" || rs[0].LanguageID != "python" {
		t.Fatalf("unexpected MLCPD rows: %#v", rs)
	}
	if len(rs[0].MLCPDNodes) == 0 {
		t.Fatal("MLCPD structural nodes not retained")
	}
}

func TestLoadMLCPDGzipJSONLFixture(t *testing.T) {
	p := filepath.Join(t.TempDir(), "records.jsonl.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	_, _ = gz.Write([]byte(`{"id":"r1","language":"go","code":"package p"}` + "\n"))
	_ = gz.Close()
	_ = f.Close()
	rs, err := LoadMLCPD(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].LanguageID != "go" || rs[0].SourceCode != "package p" {
		t.Fatalf("gzip JSONL adapter failed: %#v", rs)
	}
}

func TestLoadMLCPDGzipJSONLCurrentShardShape(t *testing.T) {
	p := filepath.Join(t.TempDir(), "records.jsonl.gz")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	gz := gzip.NewWriter(f)
	_, _ = gz.Write([]byte(`{"corpus_record_id":"mlcpd:c:1","language_id":"c","source_code":"int main(void) { return 0; }","mlcpd_node_types":["function_definition"],"structural_evidence_kind":"MLCPD_TREE_SITTER_UNIVERSAL_SCHEMA","parser_error_count":0}` + "\n"))
	_ = gz.Close()
	_ = f.Close()
	rs, err := LoadMLCPD(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].CorpusRecordID != "mlcpd:c:1" || rs[0].LanguageID != "c" || rs[0].SourceCode == "" {
		t.Fatalf("current MLCPD JSONL shape was not loaded: %#v", rs)
	}
	if len(rs[0].MLCPDNodes) == 0 || !rs[0].StructuralEvidenceAvailable {
		t.Fatalf("current MLCPD structural data was not retained: %#v", rs[0])
	}
}

func TestCSourceEligibilitySeparatesPreprocessorAndForeignSyntax(t *testing.T) {
	cases := []struct{ code, want string }{
		{"#define FLAG 1\n#include <x.h>\n", ResultPreprocessor},
		{"@interface Widget\n@end\n", ResultForeign},
		{"value <- 1\n", ResultForeign},
		{"int main(void) { return 0; }\n", "VALID_C"},
	}
	for _, tc := range cases {
		if got := sourceInputClass(makeRecord("MLCPD", "id", "c", tc.code, "", "", "", 0, "")); got != tc.want {
			t.Fatalf("source class=%q want %q", got, tc.want)
		}
	}
}

func TestPipelineGapQuotientCheckpointAndDeterminism(t *testing.T) {
	root := t.TempDir()
	out := filepath.Join(root, "out")
	cp := filepath.Join(root, "checkpoint.json")
	recs := []CorpusRecord{
		makeRecord("FOLDER", "ok", "python", "x = 1\n", "", "ok.py", "", 0, "ok.py"),
		makeRecord("FOLDER", "bad-a", "python", "???\n", "", "a.py", "", 0, "a.py"),
		makeRecord("FOLDER", "bad-b", "python", "???\n", "", "b.py", "", 0, "b.py"),
	}
	s, err := Run(Config{Project: root, Out: out, Workers: 2, Checkpoint: cp}, recs)
	if err != nil {
		t.Fatal(err)
	}
	if s.FilesTotal != 2 || s.SourceHashDuplicates != 1 || s.FilesUASTFull == 0 || s.UniqueGapClasses != 1 {
		t.Fatalf("unexpected summary: %+v", s)
	}
	if _, err := os.Stat(cp); err != nil {
		t.Fatalf("checkpoint missing: %v", err)
	}
	checkpointData, err := os.ReadFile(cp)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(checkpointData), "x = 1") || strings.Contains(string(checkpointData), "???") {
		t.Fatal("checkpoint retained source text")
	}
	first, _ := os.ReadFile(filepath.Join(out, "gap_equivalence_classes.csv"))
	if !strings.Contains(string(first), "FRONTEND_REJECTION") {
		t.Fatalf("gap class missing: %s", first)
	}
	// A second run restores translated rows from the checkpoint and must be byte stable.
	if _, err := Run(Config{Project: root, Out: out, Workers: 1, Checkpoint: cp}, recs); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(filepath.Join(out, "gap_equivalence_classes.csv"))
	if string(first) != string(second) {
		t.Fatal("gap quotient is not deterministic across resume")
	}
	failure, err := os.ReadFile(filepath.Join(out, "failure_equivalence_classes.csv"))
	if err != nil || !strings.Contains(string(failure), "files_unlocked_if_fixed") {
		t.Fatalf("failure quotient report missing: %v %s", err, failure)
	}
	eligibility, err := os.ReadFile(filepath.Join(out, "source_eligibility_matrix.csv"))
	if err != nil || !strings.Contains(string(eligibility), "VALID_PYTHON_CANDIDATE") {
		t.Fatalf("eligibility report missing: %v %s", err, eligibility)
	}
}

func TestPythonSourceClassificationSeparatesCythonWithoutPathRules(t *testing.T) {
	foreign := makeRecord("FOLDER", "foreign", "python", "cdef int value\n", "", "ordinary.py", "", 0, "ordinary.py")
	if got := sourceInputClass(foreign); got != ResultForeign {
		t.Fatalf("Cython marker class=%q", got)
	}
	python := makeRecord("FOLDER", "python", "python", "import cython\nvalue = 1\n", "", "cython_named.py", "", 0, "cython_named.py")
	if got := sourceInputClass(python); got != "VALID_PYTHON_CANDIDATE" {
		t.Fatalf("ordinary Python import class=%q", got)
	}
	for _, test := range []struct{ diagnostic, want string }{
		{"python range matrix: range binding is not a single identifier", ResultUnsupported},
		{"expected expression near \"@\"", ResultUnsupported},
		{"expected expression near \",\"", ResultParserLimit},
	} {
		if got := classifyResultState("python", "VALID_PYTHON_CANDIDATE", test.diagnostic); got != test.want {
			t.Fatalf("%q -> %q, want %q", test.diagnostic, got, test.want)
		}
	}
}

func TestLoadMinerZipFixture(t *testing.T) {
	p := filepath.Join(t.TempDir(), "miner.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	w, _ := z.Create("run/go/results/artifact_summary.csv")
	_, _ = w.Write([]byte("source_id,language,ecosystem,name,version,status,archive_sha256\ngomod:pkg@v1,go,gomod,pkg,v1,ok,abc123\n"))
	w, _ = z.Create("run/go/results/language_feature_matrix.csv")
	_, _ = w.Write([]byte("language_id,source_feature_id,observed\ngo,GEN::function,1\n"))
	_ = z.Close()
	_ = f.Close()
	rs, err := LoadMinerZip(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || len(rs[0].Features) != 1 || rs[0].Features[0] != "GEN::function" {
		t.Fatalf("miner adapter lost features: %#v", rs)
	}
}

func TestLoadMinerDirectoryArtifactRecords(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "go", "results")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	line := `{"task":{"ID":"gomod:pkg@v1","Language":"go","Ecosystem":"gomod","Name":"pkg","Version":"v1"},"result":{"Status":"ok","ArchiveSHA256":"abc","Features":{"GEN::function":2}}}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "artifact_records.jsonl"), []byte(line), 0644); err != nil {
		t.Fatal(err)
	}
	rs, err := LoadMiner(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 1 || rs[0].LanguageID != "go" || len(rs[0].Features) != 1 || rs[0].SourceHash != "abc" {
		t.Fatalf("miner directory adapter failed: %#v", rs)
	}
}
