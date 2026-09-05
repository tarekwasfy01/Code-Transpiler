package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

type edge struct{ Case, Source, Target, File, Stage, Class string }

func main() {
	root, _ := os.Getwd()
	out := filepath.Join(root, "outputs", "structured-matrix-repair")
	_ = os.MkdirAll(out, 0755)
	f, _ := os.Open(filepath.Join(root, "outputs", "miner-cross-transpile", "transpile_edges.csv"))
	if f == nil {
		panic("missing transpile edges")
	}
	defer f.Close()
	r := csv.NewReader(f)
	h, _ := r.Read()
	ix := map[string]int{}
	for i, x := range h {
		ix[x] = i
	}
	get := func(a []string, k string) string {
		if i, ok := ix[k]; ok && i < len(a) {
			return a[i]
		}
		return ""
	}
	var cases []edge
	for {
		a, e := r.Read()
		if e != nil {
			break
		}
		if get(a, "status") == "PASS" {
			continue
		}
		s := get(a, "source_language")
		t := get(a, "target_language")
		p := get(a, "source_file")
		sum := sha256.Sum256([]byte(s + "\x00" + t + "\x00" + p))
		cases = append(cases, edge{hex.EncodeToString(sum[:]), s, t, p, get(a, "stage"), get(a, "diagnostic")})
	}
	type row struct{ CaseID, Source, Target, UASTHash, Primitive, Kernel, Evidence string }
	var rows []row
	missing := 0
	cache := map[string]struct {
		hash     string
		kernels  []string
		evidence string
	}{}
	for _, c := range cases {
		key := c.Source + "\x00" + c.File
		v, ok := cache[key]
		if !ok {
			info, statErr := os.Stat(c.File)
			if c.Source != "go" {
				v.evidence = "MISSING_DEMAND_EVIDENCE"
			} else if statErr != nil || info.Size() > 262144 {
				v.evidence = "MISSING_DEMAND_EVIDENCE"
			} else {
				b, e := os.ReadFile(c.File)
				if e != nil {
					v.evidence = "MISSING_DEMAND_EVIDENCE"
				} else if p, e := backend.LowerSource(c.Source, c.File, string(b)); e != nil || p.UniversalAST == nil {
					v.evidence = "MISSING_DEMAND_EVIDENCE"
				} else {
					d := backend.AnalyzeStructuredSemanticDemand(p.UniversalAST)
					v.hash, v.kernels = d.UASTHash, d.Kernels
					if len(v.kernels) == 0 {
						v.evidence = "STRUCTURED_ZERO_DEMAND"
					} else {
						v.evidence = "UAST"
					}
				}
			}
			cache[key] = v
		}
		if v.evidence == "MISSING_DEMAND_EVIDENCE" {
			rows = append(rows, row{c.Case, c.Source, c.Target, "", "", "", v.evidence})
			missing++
			continue
		}
		if v.evidence != "UAST" {
			rows = append(rows, row{c.Case, c.Source, c.Target, v.hash, "", "", v.evidence})
			continue
		}
		for _, k := range v.kernels {
			rows = append(rows, row{c.Case, c.Source, c.Target, v.hash, k, k, "UAST"})
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CaseID+rows[i].Kernel < rows[j].CaseID+rows[j].Kernel })
	// Do not marshal rows through map[string]string here. JSON uses Go field
	// names (CaseID), whereas these files intentionally use snake_case column
	// names.  The old generic writer silently erased all matrix values.
	flat := make([][]string, 0, len(rows))
	for _, x := range rows {
		flat = append(flat, []string{x.CaseID, x.Source, x.Target, x.UASTHash, x.Primitive, x.Kernel, x.Evidence})
	}
	writeStringRows(out, "case_primitive_matrix.csv", []string{"case_id", "source_language", "target_language", "uast_hash", "primitive", "kernel", "evidence"}, flat)
	writeStringRows(out, "case_kernel_matrix.csv", []string{"case_id", "source_language", "target_language", "uast_hash", "primitive", "kernel", "evidence"}, flat)
	uniq := map[string]bool{}
	for _, x := range rows {
		if x.Evidence == "UAST" {
			uniq[x.UASTHash+"|"+x.Kernel] = true
		}
	}
	ur := []map[string]string{}
	for k := range uniq {
		ur = append(ur, map[string]string{"structured_residual_signature": k})
	}
	writeMap(out, "structured_semantic_residuals.csv", []string{"structured_residual_signature"}, ur)
	s := map[string]any{"raw_failed_cases": len(cases), "structured_uast_demands_recovered": len(cases) - missing, "cases_missing_structured_demand_evidence": missing, "unique_structured_semantic_residuals": len(uniq), "no_diagnostic_text_semantic_classification": true, "no_unclassified_atomic": true}
	b, _ := json.MarshalIndent(s, "", "  ")
	_ = os.WriteFile(filepath.Join(out, "summary.json"), b, 0644)
}
func writeStringRows(out, n string, h []string, rows [][]string) {
	f, _ := os.Create(filepath.Join(out, n))
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write(h)
	_ = w.WriteAll(rows)
	w.Flush()
}
func write[T any](out, n string, h []string, rs []T) {
	f, _ := os.Create(filepath.Join(out, n))
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write(h)
	for _, x := range rs {
		b, _ := json.Marshal(x)
		var m map[string]string
		_ = json.Unmarshal(b, &m)
		a := make([]string, len(h))
		for i, k := range h {
			a[i] = m[k]
		}
		_ = w.Write(a)
	}
	w.Flush()
}
func writeMap(out, n string, h []string, rs []map[string]string) {
	f, _ := os.Create(filepath.Join(out, n))
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write(h)
	for _, m := range rs {
		a := make([]string, len(h))
		for i, k := range h {
			a[i] = m[k]
		}
		_ = w.Write(a)
	}
	w.Flush()
}
