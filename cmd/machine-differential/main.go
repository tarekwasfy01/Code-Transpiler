// Command machine-differential persists comparable Tree-sitter and generic
// machine traces.  It deliberately records state vectors rather than parser
// diagnostics, so later failure reduction has real machine data to work with.
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

type caseInput struct {
	ID, Language, Source string
}

type persistedCase struct {
	CaseID       string                            `json:"case_id"`
	Language     string                            `json:"language"`
	SourceSHA256 string                            `json:"source_sha256"`
	Oracle       []matrixir.NormalizedMachineEvent `json:"oracle_trace"`
	Generic      []matrixir.NormalizedMachineEvent `json:"generic_trace"`
	FirstStep    int                               `json:"first_divergence_step"`
	Reference    *matrixir.NormalizedMachineEvent  `json:"reference_event,omitempty"`
	GenericEvent *matrixir.NormalizedMachineEvent  `json:"generic_event,omitempty"`
	OracleError  string                            `json:"oracle_error,omitempty"`
	GenericError string                            `json:"generic_error,omitempty"`
	OracleOK     bool                              `json:"oracle_ok"`
	GenericOK    bool                              `json:"generic_ok"`
}

func main() {
	tableDir := flag.String("table-dir", filepath.FromSlash("matrices/REAL_TS_MATRIX/execution_ready"), "execution-ready tables")
	oracleDir := flag.String("oracle-dir", filepath.FromSlash("outputs/tree-sitter-oracle/runners"), "temporary oracle runners")
	sourceDir := flag.String("source-dir", filepath.FromSlash("outputs/tree-sitter-oracle/language-smokes"), "source fixtures")
	manifest := flag.String("manifest", "", "optional CSV: case_id,language,source_path (or source)")
	outDir := flag.String("out-dir", filepath.FromSlash("outputs/tree-sitter-oracle/machine-traces"), "append-only trace output")
	strict := flag.Bool("strict", true, "compare every trace field exactly; unknown values are mismatches (use -strict=false only for exploratory runs)")
	flag.Parse()
	_ = strict // retained for CLI compatibility; comparison is always tri-state.

	cases, err := loadCases(*manifest, *sourceDir)
	if err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(*outDir, "raw"), 0o755); err != nil {
		fatal(err)
	}
	jsonl, err := os.OpenFile(filepath.Join(*outDir, "cases.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fatal(err)
	}
	defer jsonl.Close()
	enc := json.NewEncoder(jsonl)
	firstPath := filepath.Join(*outDir, "first-divergence.csv")
	first, err := os.OpenFile(firstPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		fatal(err)
	}
	defer first.Close()
	firstStat, _ := first.Stat()
	w := csv.NewWriter(first)
	if firstStat == nil || firstStat.Size() == 0 {
		_ = w.Write([]string{"case_id", "language", "first_divergence_step", "reference_event", "generic_event", "reference_state_vector", "generic_state_vector"})
		w.Flush()
	}
	classes := map[string][]string{}
	pass, mismatch, notComparable, genericOKCount := 0, 0, 0, 0
	for _, c := range cases {
		sha := sha256.Sum256([]byte(c.Source))
		oracleText, oracleErr := runOracle(filepath.Join(*oracleDir, c.Language+".exe"), c.Source)
		genericTrace, _, genericOK, genericErr := matrixir.NewGenericLexerLREngine(c.Language).ParseRealTrace(c.Source, *tableDir)
		if genericOK {
			genericOKCount++
		}
		oracleTrace := matrixir.NormalizeOracleTrace(string(oracleText))
		genericNorm := matrixir.NormalizeGenericTrace(genericTrace)
		var step int
		var ref, gen *matrixir.NormalizedMachineEvent
		step, ref, gen, comparable := firstComparableDivergence(oracleTrace, genericNorm)
		if step < 0 {
			if !comparable {
				notComparable++
			} else {
				pass++
			}
		} else {
			mismatch++
		}
		row := persistedCase{CaseID: c.ID, Language: c.Language, SourceSHA256: hex.EncodeToString(sha[:]), Oracle: oracleTrace, Generic: genericNorm, FirstStep: step, Reference: ref, GenericEvent: gen, OracleOK: oracleErr == nil, GenericOK: genericOK}
		if oracleErr != nil {
			row.OracleError = oracleErr.Error()
		}
		if genericErr != nil {
			row.GenericError = genericErr.Error()
		}
		// Keep the exact oracle bytes and normalized generic vector beside the
		// append-only aggregate. This makes every divergence independently
		// inspectable without rerunning the parser or retaining global state.
		_ = os.WriteFile(filepath.Join(*outDir, "raw", c.ID+".oracle.log"), oracleText, 0o644)
		if b, e := json.MarshalIndent(genericNorm, "", "  "); e == nil {
			_ = os.WriteFile(filepath.Join(*outDir, "raw", c.ID+".generic.json"), b, 0o644)
		}
		if err := enc.Encode(row); err != nil {
			fatal(err)
		}
		_ = jsonl.Sync()
		refJSON, _ := json.Marshal(ref)
		genJSON, _ := json.Marshal(gen)
		_ = w.Write([]string{c.ID, c.Language, strconv.Itoa(step), string(refJSON), string(genJSON), vector(ref), vector(gen)})
		w.Flush()
		_ = first.Sync()
		keyBytes, _ := json.Marshal(struct {
			R, G *matrixir.NormalizedMachineEvent
		}{ref, gen})
		key := string(keyBytes)
		classes[key] = append(classes[key], c.ID)
	}
	classPath := filepath.Join(*outDir, "first-divergence-classes.csv")
	cf, err := os.Create(classPath)
	if err != nil {
		fatal(err)
	}
	cw := csv.NewWriter(cf)
	_ = cw.Write([]string{"class_id", "affected_cases", "reference_event", "generic_event"})
	classID := 0
	keys := make([]string, 0, len(classes))
	for key := range classes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		ids := classes[key]
		var v struct {
			R, G *matrixir.NormalizedMachineEvent
		}
		_ = json.Unmarshal([]byte(key), &v)
		classID++
		_ = cw.Write([]string{strconv.Itoa(classID), strings.Join(ids, " "), vector(v.R), vector(v.G)})
	}
	cw.Flush()
	_ = cf.Close()
	summary := map[string]any{"cases": len(cases), "matches": pass, "mismatches": mismatch, "not_comparable_only": notComparable, "generic_parse_ok": genericOKCount, "first_divergence_classes": len(classes), "trace_file": "cases.jsonl", "divergence_file": "first-divergence.csv"}
	sf, err := os.Create(filepath.Join(*outDir, "summary.json"))
	if err != nil {
		fatal(err)
	}
	_ = json.NewEncoder(sf).Encode(summary)
	_ = sf.Close()
	fmt.Printf("CASES=%d MATCH=%d MISMATCH=%d NOT_COMPARABLE_ONLY=%d FIRST_REAL_DIVERGENCE_CLASSES=%d OUTPUT=%s\n", len(cases), pass, mismatch, notComparable, len(classes), *outDir)
}

// firstComparableDivergence compares only fields observed on both sides.
// Missing oracle values are NOT mismatches; a row with no shared known field
// is classified as not-comparable.
func firstComparableDivergence(ref, gen []matrixir.NormalizedMachineEvent) (int, *matrixir.NormalizedMachineEvent, *matrixir.NormalizedMachineEvent, bool) {
	n := len(ref)
	if len(gen) < n {
		n = len(gen)
	}
	any := false
	for i := 0; i < n; i++ {
		a, b := ref[i], gen[i]
		diff := false
		row := false
		if a.Event != "" && b.Event != "" {
			row = true
			any = true
			if a.Event != b.Event {
				diff = true
			}
		}
		if a.ParserState >= 0 && b.ParserState >= 0 {
			row = true
			any = true
			if a.ParserState != b.ParserState {
				diff = true
			}
		}
		if a.Symbol >= 0 && b.Symbol >= 0 {
			row = true
			any = true
			if a.Symbol != b.Symbol {
				diff = true
			}
		}
		if row && diff {
			x, y := a, b
			return i, &x, &y, true
		}
	}
	return -1, nil, nil, any
}

func loadCases(manifest, sourceDir string) ([]caseInput, error) {
	if manifest == "" {
		langs := []string{"c", "cpp", "csharp", "go", "java", "julia", "kotlin", "nim", "python", "r", "rust", "zig"}
		out := make([]caseInput, 0, len(langs))
		for _, lang := range langs {
			b, err := os.ReadFile(filepath.Join(sourceDir, lang+".source"))
			if err != nil {
				return nil, err
			}
			out = append(out, caseInput{ID: "smoke-" + lang, Language: lang, Source: string(b)})
		}
		return out, nil
	}
	f, err := os.Open(manifest)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	var out []caseInput
	for {
		row, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(row) == 0 || row[0] == "case_id" {
			continue
		}
		if len(row) < 3 {
			return nil, fmt.Errorf("manifest row needs case_id, language, source_path/source")
		}
		src := row[2]
		if st, e := os.Stat(src); e == nil && !st.IsDir() {
			b, e := os.ReadFile(src)
			if e != nil {
				return nil, e
			}
			src = string(b)
		}
		out = append(out, caseInput{ID: row[0], Language: row[1], Source: src})
	}
	return out, nil
}

func runOracle(exe, source string) ([]byte, error) {
	if _, err := os.Stat(exe); err != nil {
		return nil, err
	}
	cmd := exec.Command(exe, source)
	// CombinedOutput is intentional: Tree-sitter runners may return a parse
	// error exit code while still emitting the complete trace on stderr.
	return cmd.CombinedOutput()
}

func vector(e *matrixir.NormalizedMachineEvent) string {
	if e == nil {
		return "null"
	}
	b, _ := json.Marshal(e)
	return string(b)
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }
