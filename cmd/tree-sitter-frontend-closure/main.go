// tree-sitter-frontend-closure derives the conservative, reproducible
// Tree-sitter CST -> current frontend coverage matrices.  It intentionally
// does not promote a grammar rule to semantic/UAST support: only a real
// frontend lowerer may do that.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type row map[string]string

func readCSV(path string) ([]row, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	h, err := r.Read()
	if err != nil {
		return nil, err
	}
	for i := range h {
		h[i] = strings.TrimPrefix(h[i], "\ufeff")
	}
	var out []row
	for {
		record, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		x := row{}
		for i, k := range h {
			if i < len(record) {
				x[k] = record[i]
			}
		}
		out = append(out, x)
	}
	return out, nil
}

func writeCSV(path string, header []string, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err = w.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		if err = w.Write(r); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func main() {
	in := flag.String("in", filepath.FromSlash("matrices/frontend_closure/tree_sitter_input"), "normalised Tree-sitter evidence directory")
	out := flag.String("out", filepath.FromSlash("matrices/frontend_closure"), "output matrix directory")
	upi := flag.String("upi", filepath.FromSlash(".cache/handoff-upi-complete"), "UPI handoff directory")
	legacyBootstrap := flag.Bool("legacy-bootstrap-report", false, "also emit the obsolete empty-template comparison")
	flag.Parse()
	if err := runCorpusClosure(*in, *out); err != nil {
		panic(err)
	}
	if err := runProducerSpec(*upi, filepath.Join(*upi, "tree_sitter_full"), filepath.Join(*out, "producer_spec")); err != nil {
		panic(err)
	}
	if err := compileGrammarTables(filepath.Join(*upi, "tree_sitter_full"), filepath.Join(*out, "grammar_tables")); err != nil { panic(err) }
	if !*legacyBootstrap {
		return
	}
	/* Legacy bootstrap report retained below only so older generated inputs can
	be compared; the productive command returns from the real corpus join above. */
	rules, err := readCSV(filepath.Join(*in, "03_rules.csv"))
	if err != nil {
		panic(err)
	}
	nodes, err := readCSV(filepath.Join(*in, "08_node_types.csv"))
	if err != nil {
		panic(err)
	}
	join, err := readCSV(filepath.Join(*in, "21_uast_join_template.csv"))
	if err != nil {
		panic(err)
	}
	// A syntax rule is ingested evidence.  PARSED and later stages remain zero
	// until a real Tree-sitter CST adapter is linked; this avoids treating the
	// supplied grammar JSON as proof of a semantic mapping.
	stages := []string{"TOKEN_RECOGNIZED", "PARSED", "CST_STRUCTURED", "LOWERING_RULE_PRESENT", "UAST_STRUCTURE_MAPPED", "UASF_MAPPED", "RELATIONS_MAPPED", "FIELDS_MAPPED", "UAST_VALID", "SEMANTIC_PRESERVATION_PROVEN"}
	coverage := [][]string{}
	appendEvidence := func(kind string, data []row) {
		for _, x := range data {
			for i, stage := range stages {
				value := "0"
				if i == 0 {
					value = "1"
				}
				coverage = append(coverage, []string{x["language"], x[kind], kind, stage, value})
			}
		}
	}
	appendEvidence("rule", rules)
	appendEvidence("node_type", nodes)
	sort.Slice(coverage, func(i, j int) bool { return strings.Join(coverage[i], "\x00") < strings.Join(coverage[j], "\x00") })
	if err = writeCSV(filepath.Join(*out, "tree_sitter_to_current_frontend_matrix.csv"), []string{"language", "source_node_or_rule", "source_kind", "frontend_stage", "available"}, coverage); err != nil {
		panic(err)
	}

	features := [][]string{}
	missing := [][]string{}
	for _, x := range join {
		features = append(features, []string{x["language"], x["source_node_or_rule"], x["source_kind"], x["uast_structure"], x["uasf_facets"], x["uast_relations"], x["uast_fields"], x["mapping_status"], x["evidence"]})
		if x["mapping_status"] != "MAPPED" {
			missing = append(missing, []string{x["language"], x["source_node_or_rule"], x["source_kind"], "CST_TO_UAST_MISSING", "TREE_SITTER_CST_ADAPTER_REQUIRED"})
		}
	}
	if err = writeCSV(filepath.Join(*out, "source_feature_to_uast_matrix.csv"), []string{"language", "source_node_or_rule", "source_kind", "uast_structure", "uasf_facets", "uast_relations", "uast_fields", "mapping_status", "evidence"}, features); err != nil {
		panic(err)
	}
	if err = writeCSV(filepath.Join(*out, "frontend_missing_matrix.csv"), []string{"language", "source_node_or_rule", "source_kind", "missing_kind", "requirement"}, missing); err != nil {
		panic(err)
	}

	// Exact quotient: all rows with exactly the same stage vector belong to one
	// class.  The same rule applies to columns, without ranking or similarity.
	rowClass := map[string]int{}
	rowRows := [][]string{}
	for _, x := range rules {
		sig := "1000000000"
		if _, ok := rowClass[sig]; !ok {
			rowClass[sig] = len(rowClass) + 1
		}
		rowRows = append(rowRows, []string{x["language"], x["rule"], fmt.Sprintf("row_%03d", rowClass[sig]), sig})
	}
	if err = writeCSV(filepath.Join(*out, "frontend_exact_row_classes.csv"), []string{"language", "rule", "exact_row_class", "stage_vector"}, rowRows); err != nil {
		panic(err)
	}
	colRows := [][]string{}
	for i, stage := range stages {
		vector := strings.Repeat("1", len(rules)+len(nodes))
		if i > 0 {
			vector = strings.Repeat("0", len(rules)+len(nodes))
		}
		colRows = append(colRows, []string{stage, fmt.Sprintf("column_%03d", i+1), vector})
	}
	if err = writeCSV(filepath.Join(*out, "frontend_exact_column_classes.csv"), []string{"frontend_stage", "exact_column_class", "availability_vector"}, colRows); err != nil {
		panic(err)
	}
	if err = writeCSV(filepath.Join(*out, "frontend_fix_requirement_matrix.csv"), []string{"requirement", "source_kind", "missing_cells"}, [][]string{{"TREE_SITTER_CST_ADAPTER_REQUIRED", "node_type", fmt.Sprint(len(missing))}}); err != nil {
		panic(err)
	}
	if err = writeCSV(filepath.Join(*out, "frontend_minimal_fix_basis.csv"), []string{"fix_class", "covers_missing_kind", "evidence"}, [][]string{{"TREE_SITTER_CST_ADAPTER_REQUIRED", "CST_TO_UAST_MISSING", "grammar/node evidence is syntax-only"}}); err != nil {
		panic(err)
	}
	langs := map[string][2]int{}
	for _, x := range nodes {
		v := langs[x["language"]]
		v[1]++
		langs[x["language"]] = v
	}
	for _, x := range join {
		v := langs[x["language"]]
		if x["mapping_status"] == "MAPPED" {
			v[0]++
		}
		langs[x["language"]] = v
	}
	keys := []string{}
	for k := range langs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lr := [][]string{}
	for _, k := range keys {
		v := langs[k]
		lr = append(lr, []string{k, fmt.Sprint(v[0]), fmt.Sprint(v[1]), "0"})
	}
	if err = writeCSV(filepath.Join(*out, "frontend_language_coverage.csv"), []string{"language", "mapped_source_features", "tree_sitter_node_types", "valid_uast_samples"}, lr); err != nil {
		panic(err)
	}
	if err = writeCSV(filepath.Join(*out, "frontend_rule_coverage.csv"), []string{"language", "rule", "token_recognized", "parsed", "cst_to_uast"}, func() [][]string {
		r := [][]string{}
		for _, x := range rules {
			r = append(r, []string{x["language"], x["rule"], "1", "0", "0"})
		}
		return r
	}()); err != nil {
		panic(err)
	}
	if err = writeCSV(filepath.Join(*out, "frontend_corpus_validation.csv"), []string{"language", "case_name", "source_valid", "parse_pass", "source_to_uast_pass", "uast_valid_pass", "semantic_mapping_pass"}, nil); err != nil {
		panic(err)
	}
	if err = writeCSV(filepath.Join(*out, "frontend_unrepresentable_evidence.csv"), []string{"language", "source_node_or_rule", "status", "evidence"}, nil); err != nil {
		panic(err)
	}
	fmt.Printf("TREE_SITTER_RULES_INGESTED=%d\nTREE_SITTER_NODE_TYPES_INGESTED=%d\nCURRENT_FRONTEND_COVERED_CELLS=%d\nMISSING_CELLS=%d\nEXACT_ROW_CLASSES=%d\nEXACT_COLUMN_CLASSES=%d\nMINIMAL_SHARED_FIX_CLASSES=1\n", len(rules), len(nodes), 0, len(missing), len(rowClass), len(stages))
}
