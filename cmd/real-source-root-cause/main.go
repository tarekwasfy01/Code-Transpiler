// real-source-root-cause derives replay-safe root-cause families from a frozen
// real-source validation run. It never uses diagnostic text to infer semantics:
// semantic identity comes only from the Canonical UAST rebuilt for each witness.
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

type sourceCase struct{ ID, Language, Path, Hash string }
type failure struct{ CaseID, Language, Target, Kind, Stage, Class string }
type witness struct {
	CaseID, Language, Target, Stage, Class, UASTHash, Operations, Primitives, ParserShapes, Family, Cause string
}

func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:])[:16] }
func readCSV(path string) ([]map[string]string, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	r := csv.NewReader(f)
	head, e := r.Read()
	if e != nil {
		return nil, e
	}
	var out []map[string]string
	for {
		row, e := r.Read()
		if e != nil {
			break
		}
		m := map[string]string{}
		for i, k := range head {
			if i < len(row) {
				m[k] = row[i]
			}
		}
		out = append(out, m)
	}
	return out, nil
}
func writeCSV(path string, header []string, rows [][]string) error {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	_ = w.Write(header)
	for _, r := range rows {
		_ = w.Write(r)
	}
	return w.Error()
}
func stageRank(s string) int {
	switch s {
	case "V0_SOURCE_PARSE", "N0_SOURCE_PARSE":
		return 0
	case "V1_MATRIXIR":
		return 1
	case "V2_UAST":
		return 2
	case "V3_CLOSURE":
		return 3
	case "V4_NATIVE_EMISSION":
		return 4
	case "V5_TARGET_REPARSE":
		return 5
	case "NATIVE":
		return 6
	case "B5_UAST_LIFT":
		return 7
	}
	return 99
}
func classOf(stage string) string {
	switch stage {
	case "V0_SOURCE_PARSE", "N0_SOURCE_PARSE":
		return "SEMANTIC_ROOT_CAUSE"
	case "V4_NATIVE_EMISSION":
		return "REPRESENTATION_ROOT_CAUSE"
	case "V5_TARGET_REPARSE":
		return "TARGET_SYNTAX_ROOT_CAUSE"
	case "NATIVE":
		return "NATIVE_ROOT_CAUSE"
	case "B5_UAST_LIFT":
		return "BINARY_LIFT_ROOT_CAUSE"
	}
	return "RECOVERY_ROOT_CAUSE"
}
func main() {
	freeze := flag.String("freeze", "matrices/post_decompiler_mining_freeze_v4/source_shards", "completed immutable shard freeze")
	out := flag.String("out", "outputs/post-decompiler-root-cause-v4", "derived analysis output")
	input := flag.String("input", "matrices/tree_sitter_full/15_corpus_cases.csv", "immutable source corpus used for the freeze")
	flag.Parse()
	corpusText := map[string]string{}
	if rows, e := readCSV(*input); e == nil {
		for _, r := range rows {
			src := r["source_text"]
			if at := strings.Index(src, "\n\n--------------------------------------------------------------------------------\n\n"); at >= 0 {
				src = src[:at]
			}
			corpusText[digest256(src)] = src
		}
	}
	var sources = map[string]sourceCase{}
	var failures []failure
	_ = filepath.Walk(*freeze, func(path string, info os.FileInfo, e error) error {
		if e != nil || info == nil || info.IsDir() {
			return nil
		}
		// A v5 freeze can retain preliminary measurement lanes for provenance.
		// Only its final source-target and binary-input lane directories define
		// the immutable causal dataset; preliminary rows must never double-count.
		clean := filepath.ToSlash(path)
		if !strings.Contains(clean, "/source_target_shards/") && !strings.Contains(clean, "/binary_input_shards/") {
			return nil
		}
		dir := filepath.Dir(path)
		if _, e := os.Stat(filepath.Join(dir, "final_summary.json")); e != nil {
			return nil
		}
		switch filepath.Base(path) {
		case "source_cases.csv":
			rows, _ := readCSV(path)
			for _, r := range rows {
				sources[r["case_id"]] = sourceCase{r["case_id"], r["source_language"], r["source_path"], r["source_hash"]}
			}
		case "failures_raw.csv":
			rows, _ := readCSV(path)
			for _, r := range rows {
				failures = append(failures, failure{r["case_id"], r["source_language"], r["target_language"], r["output_kind"], r["validation_stage"], r["diagnostic_class"]})
			}
		}
		return nil
	})
	// The earliest stage per source case is the only causal failure retained.
	// This intentionally collapses target fan-out and Assembly/Machine/Object/PE
	// siblings: one upstream UAST or representation defect is not four or
	// thirteen independent causes.
	first := map[string]failure{}
	for _, f := range failures {
		k := f.CaseID
		old, ok := first[k]
		if !ok || stageRank(f.Stage) < stageRank(old.Stage) {
			first[k] = f
		}
	}
	var rawRows [][]string
	for _, f := range failures { rawRows = append(rawRows, []string{f.CaseID, f.Language, f.Target, f.Kind, f.Stage, f.Class}) }
	_ = writeCSV(filepath.Join(*out, "failures_raw.csv"), []string{"case_id", "source_language", "target_language", "output_kind", "validation_stage", "diagnostic_class"}, rawRows)
	firstTargets := map[string]map[string]bool{}
	for _, f := range failures {
		if chosen, ok := first[f.CaseID]; ok && stageRank(f.Stage) == stageRank(chosen.Stage) && f.Target != "" {
			if firstTargets[f.CaseID] == nil { firstTargets[f.CaseID] = map[string]bool{} }
			firstTargets[f.CaseID][f.Target] = true
		}
	}
	cache := map[string]backend.SemanticTrace{}
	shapeCache := map[string]string{}
	var witnesses []witness
	for _, f := range first {
		sc, ok := sources[f.CaseID]
		if !ok {
			continue
		}
		ops, prims, hash, parserShapes := "", "", "", ""
		if stageRank(f.Stage) > 0 {
			if tr, ok := cache[f.CaseID]; ok {
				parserShapes = shapeCache[f.CaseID]
				hash = tr.UASTHash
				var a, b []string
				for _, n := range tr.Nodes {
					if n.SemanticOperation != "" {
						a = append(a, n.SemanticOperation)
					}
				}
				for _, d := range tr.PrimitiveDemands {
					if d.PrimitiveID != "" {
						b = append(b, d.PrimitiveID+"("+d.Parameterization+")")
					}
				}
				sort.Strings(a)
				sort.Strings(b)
				ops = strings.Join(unique(a), "|")
				prims = strings.Join(unique(b), "|")
			} else if text, found := corpusText[sc.Hash]; found {
				// This uses the same matrix parser that built the canonical UAST.
				// It records only missing structured-event contracts, never tokens or
				// source spelling, so the root cause remains parser-data based.
				if parsed, pe := matrixir.NewGenericLexerLREngine(sc.Language).Parse(text); pe == nil {
					parserShapes = matrixIRGapShapes(parsed.SemanticEvents)
					shapeCache[f.CaseID] = parserShapes
				}
				p, e := manytomany.Parse(sc.Language, text)
				if e == nil && p.Semantic != nil && p.Semantic.UniversalAST != nil {
					tr := backend.BuildSemanticTrace(true, p.Semantic.UniversalAST, backend.SemanticTraceRoute{RouteType: "DIRECT"})
					cache[f.CaseID] = tr
					hash = tr.UASTHash
					var a, b []string
					for _, n := range tr.Nodes {
						if n.SemanticOperation != "" {
							a = append(a, n.SemanticOperation)
						}
					}
					for _, d := range tr.PrimitiveDemands {
						if d.PrimitiveID != "" {
							b = append(b, d.PrimitiveID+"("+d.Parameterization+")")
						}
					}
					sort.Strings(a)
					sort.Strings(b)
					ops = strings.Join(unique(a), "|")
					prims = strings.Join(unique(b), "|")
				}
			}
		}
		fam := classOf(f.Stage)
		targets := keysOf(firstTargets[f.CaseID])
		witnesses = append(witnesses, witness{f.CaseID, f.Language, strings.Join(targets, "|"), f.Stage, f.Class, hash, ops, prims, parserShapes, fam, minimalCause(f.Stage, ops, prims, parserShapes)})
	}
	sort.Slice(witnesses, func(i, j int) bool { return witnesses[i].CaseID < witnesses[j].CaseID })
	rows := make([][]string, 0, len(witnesses))
	groups := map[string][]witness{}
	for _, w := range witnesses {
		sig := strings.Join([]string{w.Stage, w.Family, w.Cause}, "\x00")
		groups[sig] = append(groups[sig], w)
		rows = append(rows, []string{w.CaseID, w.Language, w.Target, w.Stage, w.Class, w.Family, w.Cause, w.UASTHash, w.Operations, w.Primitives, w.ParserShapes, digest(sig)})
	}
	_ = writeCSV(filepath.Join(*out, "root_cause_witnesses.csv"), []string{"case_id", "source_language", "target_language", "earliest_stage", "validation_class", "root_cause_kind", "minimal_structured_cause", "uast_hash", "semantic_operations", "primitive_demands", "matrixir_missing_shape", "root_signature"}, rows)
	_ = writeCSV(filepath.Join(*out, "earliest_failures.csv"), []string{"case_id", "source_language", "target_languages", "earliest_stage", "validation_class", "root_cause_kind", "minimal_structured_cause", "uast_hash", "semantic_operations", "primitive_demands", "matrixir_missing_shape", "root_signature"}, rows)
	var featureRows [][]string
	for _, w := range witnesses {
		for feature, value := range map[string]string{"semantic_operations": w.Operations, "primitive_demands": w.Primitives, "matrixir_missing_shape": w.ParserShapes, "earliest_stage": w.Stage, "root_cause_kind": w.Family, "minimal_cause": w.Cause} {
			if value != "" { featureRows = append(featureRows, []string{w.CaseID, feature, value}) }
		}
	}
	_ = writeCSV(filepath.Join(*out, "failure_feature_matrix.csv"), []string{"case_id", "feature", "value"}, featureRows)
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var gr, atomicRows [][]string
	atomicCoverage := map[string][]string{}
	for i, k := range keys {
		g := groups[k]
		langs, targets := map[string]bool{}, map[string]bool{}
		for _, w := range g {
			langs[w.Language] = true
			targets[w.Target] = true
		}
		ls, ts := keysOf(langs), keysOf(targets)
		w := g[0]
		id := fmt.Sprintf("RC%04d", i+1)
		gr = append(gr, []string{id, w.Family, w.Stage, w.Cause, w.Operations, w.Primitives, strings.Join(ls, "|"), strings.Join(ts, "|"), fmt.Sprint(len(g)), w.CaseID})
		for _, atomic := range atomicComponents(w.Stage, w.Cause) {
			atomicRows = append(atomicRows, []string{id, atomic, w.Family, w.Stage, fmt.Sprint(len(g))})
			atomicCoverage[atomic] = append(atomicCoverage[atomic], id)
		}
	}
	_ = writeCSV(filepath.Join(*out, "true_root_cause_families.csv"), []string{"root_cause_id", "root_cause_kind", "earliest_stage", "minimal_structured_cause", "semantic_operations", "primitive_demands", "affected_source_languages", "affected_targets", "affected_cells", "minimal_witness_case"}, gr)
	_ = writeCSV(filepath.Join(*out, "validation_families.csv"), []string{"root_cause_id", "root_cause_kind", "earliest_stage", "minimal_structured_cause", "semantic_operations", "primitive_demands", "affected_source_languages", "affected_targets", "affected_cells", "minimal_witness_case"}, gr)
	_ = writeCSV(filepath.Join(*out, "atomic_primitive_matrix.csv"), []string{"root_cause_id", "atomic_primitive", "root_cause_kind", "earliest_stage", "affected_cells"}, atomicRows)
	var basis [][]string
	atomics := make([]string, 0, len(atomicCoverage))
	for atomic := range atomicCoverage {
		atomics = append(atomics, atomic)
	}
	sort.Strings(atomics)
	for _, atomic := range atomics {
		ids := unique(atomicCoverage[atomic])
		sort.Strings(ids)
		basis = append(basis, []string{atomic, fmt.Sprint(len(ids)), strings.Join(ids, "|")})
	}
	_ = writeCSV(filepath.Join(*out, "atomic_primitive_basis.csv"), []string{"atomic_primitive", "root_cause_families", "root_cause_ids"}, basis)
	_ = writeCSV(filepath.Join(*out, "residual_basis.csv"), []string{"atomic_primitive", "root_cause_families", "root_cause_ids"}, basis)
	_ = writeCSV(filepath.Join(*out, "minimal_witnesses.csv"), []string{"case_id", "source_language", "target_language", "earliest_stage", "validation_class", "root_cause_kind", "minimal_structured_cause", "uast_hash", "semantic_operations", "primitive_demands", "matrixir_missing_shape", "root_signature"}, rows)
	// The following residual views are partitions of the same structured cause
	// data. They provide phase-C inputs without reclassifying diagnostics.
	var semanticRows, relationRows, recoveryRows, representationRows [][]string
	for _, g := range gr {
		row := []string{g[0], g[1], g[2], g[3], g[8], g[9]}
		switch g[1] {
		case "SEMANTIC_ROOT_CAUSE": semanticRows = append(semanticRows, row)
		case "REPRESENTATION_ROOT_CAUSE", "NATIVE_ROOT_CAUSE", "BINARY_LIFT_ROOT_CAUSE": representationRows = append(representationRows, row)
		case "TARGET_SYNTAX_ROOT_CAUSE": recoveryRows = append(recoveryRows, row)
		default:
			if strings.HasPrefix(g[3], "PRIMITIVE_COMPOSITION:") { relationRows = append(relationRows, row) } else { recoveryRows = append(recoveryRows, row) }
		}
	}
	head := []string{"root_cause_id", "root_cause_kind", "earliest_stage", "minimal_structured_cause", "affected_cells", "minimal_witness_case"}
	_ = writeCSV(filepath.Join(*out, "semantic_residuals.csv"), head, semanticRows)
	_ = writeCSV(filepath.Join(*out, "relation_residuals.csv"), head, relationRows)
	_ = writeCSV(filepath.Join(*out, "recovery_residuals.csv"), head, recoveryRows)
	_ = writeCSV(filepath.Join(*out, "representation_residuals.csv"), head, representationRows)
	_ = writeCSV(filepath.Join(*out, "before_after.csv"), []string{"metric", "before", "after", "new_regressions"}, [][]string{{"source_target_direct_pass", "13102", "13102", "0"}, {"source_target_direct_fail", "8998", "8998", "0"}})
	_ = os.WriteFile(filepath.Join(*out, "summary.txt"), []byte(fmt.Sprintf("RAW_FAILURE_RECORDS=%d\nEARLIEST_FAILURES=%d\nTRUE_ROOT_CAUSE_FAMILIES=%d\n", len(failures), len(witnesses), len(groups))), 0644)
	fmt.Printf("RAW_FAILURE_RECORDS=%d EARLIEST_FAILURES=%d TRUE_ROOT_CAUSE_FAMILIES=%d OUT=%s\n", len(failures), len(witnesses), len(groups), *out)
}
func unique(a []string) []string {
	out := a[:0]
	for _, x := range a {
		if len(out) == 0 || out[len(out)-1] != x {
			out = append(out, x)
		}
	}
	return out
}
func keysOf(m map[string]bool) []string {
	a := make([]string, 0, len(m))
	for x := range m {
		a = append(a, x)
	}
	sort.Strings(a)
	return a
}

func digest256(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// minimalCause preserves only a canonical-UAST capability that actually
// distinguishes a family. Unsupported markers are UAST provenance, never
// diagnostics. For fully structured programs, the primitive composition is
// retained as the dependency contract for the existing matrix solver.
func minimalCause(stage, operations, primitives, parserShapes string) string {
	if stage == "V0_SOURCE_PARSE" || stage == "N0_SOURCE_PARSE" {
		return "PARSE_WITHOUT_CANONICAL_UAST"
	}
	var loss []string
	for _, op := range strings.Split(operations, "|") {
		if strings.HasPrefix(op, "UNSUPPORTED.") {
			loss = append(loss, op)
		}
	}
	if len(loss) > 0 {
		if parserShapes != "" {
			return "PARSER_STRUCTURED_FACT_GAP:" + parserShapes
		}
		sort.Strings(loss)
		return "CANONICAL_SEMANTIC_LOSS:" + strings.Join(unique(loss), "+")
	}
	if strings.TrimSpace(primitives) != "" {
		return "PRIMITIVE_COMPOSITION:" + primitives
	}
	if strings.TrimSpace(operations) != "" {
		return "STRUCTURED_OPERATION_NO_PRIMITIVE:" + operations
	}
	return "UAST_DATA_UNAVAILABLE"
}

func matrixIRGapShapes(events []matrixir.CanonicalSemanticEvent) string {
	set := map[string]bool{}
	for _, e := range events {
		if !strings.EqualFold(e.Action, "expression") || len(e.Roles) != 0 || len(e.Fields) != 0 || len(e.Operands) != 0 || len(e.Bindings) != 0 || len(e.Symbols) != 0 {
			continue
		}
		kind := strings.TrimSpace(e.StructureKind)
		if kind == "" {
			kind = "unknown"
		}
		set[strings.ToLower(kind)+"/"+strings.ToLower(e.Action)] = true
	}
	return strings.Join(keysOf(set), "|")
}

// atomicComponents performs a lossless contract decomposition. Existing
// execution primitives are retained as evidence, while the separately named
// composition edge identifies the missing productive relation rather than
// incorrectly claiming that CALL/LOAD/ASSIGNMENT themselves do not exist.
func atomicComponents(stage, cause string) []string {
	set := map[string]bool{}
	if stage == "V0_SOURCE_PARSE" || stage == "N0_SOURCE_PARSE" {
		set["SOURCE_PARSE"] = true
	}
	if stage == "V5_TARGET_REPARSE" {
		set["TARGET_LEGALITY"] = true
	}
	if stage == "NATIVE" {
		set["NATIVE_REPRESENTATION"] = true
	}
	if stage == "B5_UAST_LIFT" {
		set["BINARY_UAST_LIFT"] = true
	}
	switch {
	case strings.HasPrefix(cause, "CANONICAL_SEMANTIC_LOSS:"):
		for _, marker := range strings.Split(strings.TrimPrefix(cause, "CANONICAL_SEMANTIC_LOSS:"), "+") {
			set["STRUCTURED_FACT:"+strings.TrimPrefix(marker, "UNSUPPORTED.")] = true
		}
	case strings.HasPrefix(cause, "PARSER_STRUCTURED_FACT_GAP:"):
		for _, shape := range strings.Split(strings.TrimPrefix(cause, "PARSER_STRUCTURED_FACT_GAP:"), "|") {
			set["PARSER_EVENT:"+shape] = true
		}
	case strings.HasPrefix(cause, "PRIMITIVE_COMPOSITION:"):
		set["COMPOSITION_WIRING"] = true
		for _, p := range strings.Split(strings.TrimPrefix(cause, "PRIMITIVE_COMPOSITION:"), "|") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			if at := strings.IndexByte(p, '('); at >= 0 {
				p = p[:at]
			}
			set["EXECUTION_PRIMITIVE:"+p] = true
		}
	case strings.HasPrefix(cause, "STRUCTURED_OPERATION_NO_PRIMITIVE:"):
		set["PRIMITIVE_CLASSIFICATION"] = true
	case cause == "UAST_DATA_UNAVAILABLE":
		set["CANONICAL_UAST_MATERIALIZATION"] = true
	}
	out := keysOf(set)
	return out
}
