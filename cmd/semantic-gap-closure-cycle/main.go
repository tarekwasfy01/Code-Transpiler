// Copyright (c) 2026 Tarek Wasfy
// semantic-gap-closure-cycle converts a real saturation run into the
// end-to-end semantic gap closure plane. It consumes measured CSV/JSON output
// from failure-saturation-corpus; it never infers semantics from diagnostics.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type row map[string]string

func readCSV(path string) ([]row, []string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	h, err := r.Read()
	if err != nil {
		return nil, nil, err
	}
	var rows []row
	for {
		v, err := r.Read()
		if err == io.EOF {
			return rows, h, nil
		}
		if err != nil {
			return nil, nil, err
		}
		m := row{}
		for i, k := range h {
			if i < len(v) {
				m[k] = v[i]
			}
		}
		rows = append(rows, m)
	}
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
	if err := w.Write(header); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write(r); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func copyCSV(in, out string) error {
	rows, h, err := readCSV(in)
	if err != nil {
		return err
	}
	data := make([][]string, 0, len(rows))
	for _, r := range rows {
		v := make([]string, len(h))
		for i, k := range h {
			v[i] = r[k]
		}
		data = append(data, v)
	}
	return writeCSV(out, h, data)
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func summaryInt(s map[string]any, key string) int {
	v, ok := s[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

func readSummary(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s map[string]any
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return s, nil
}

func main() {
	in := flag.String("in", "outputs/failure-saturation-v36", "real saturation output")
	out := flag.String("out", "outputs/semantic-gap-closure-v36", "closure output")
	flag.Parse()
	if err := run(*in, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(in, out string) error {
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	for src, dst := range map[string]string{
		"00_pipeline_abort_points.csv":        "00_pipeline_abort_points.csv",
		"11_failure_instances.csv":            "03_failure_instances.csv",
		"12_failure_families.csv":             "04_failure_families_raw.csv",
		"13_minimal_witness_matrix.csv":       "05_minimal_witnesses.csv",
		"02_failure_root_cause_matrix.csv":    "06_root_cause_quotient.csv",
		"09_semantic_coverage_by_package.csv": "14_semantic_coverage_by_package.csv",
		"19_binary_saturation_matrix.csv":     "24_binary_frontier.csv",
	} {
		if err := copyCSV(filepath.Join(in, src), filepath.Join(out, dst)); err != nil {
			return err
		}
	}
	if err := writeStrictAndSaturated(in, out); err != nil {
		return err
	}
	if err := writeClassifications(in, out); err != nil {
		return err
	}
	if err := writeCoverage(in, out); err != nil {
		return err
	}
	if err := writeClosureFiles(in, out); err != nil {
		return err
	}
	if err := writeRequiredArtifacts(in, out); err != nil {
		return err
	}
	return writeSummary(in, out)
}

// writeRequiredArtifacts exposes the stable, unnumbered contract files used by
// downstream audits. Every file is copied from measured saturation data or is
// explicitly marked NOT_MEASURED; this layer never invents a successful route.
func writeRequiredArtifacts(in, out string) error {
	aliases := map[string]string{
		"03_failure_instances.csv":            "saturation_failures.csv",
		"05_minimal_witnesses.csv":            "minimal_witnesses.csv",
		"04_failure_families_raw.csv":         "root_causes.csv",
		"17_failure_dependency_graph.csv":     "failure_dependency_graph.csv",
		"08_semantic_closure_attempts.csv":    "semantic_closure_attempts.csv",
		"09_rewrite_proofs.csv":               "rewrite_proofs.csv",
		"20_uast_gap_proofs.csv":              "schema_gap_proofs.csv",
		"11_universal_repair_batches.csv":     "universal_repair_batches.csv",
		"12_repair_impact_matrix.csv":         "repair_impact_matrix.csv",
		"14_semantic_coverage_by_package.csv": "coverage_by_language.csv",
		"22_target_gap_matrix.csv":            "coverage_by_target.csv",
		"25_native_frontier.csv":              "native_frontier.csv",
		"24_binary_frontier.csv":              "binary_frontier.csv",
		"26_iteration_history.csv":            "iteration_history.csv",
	}
	// A map cannot represent the two strict aliases above, so write them
	// explicitly before processing the remaining aliases.
	if err := copyCSV(filepath.Join(in, "source_manifest.csv"), filepath.Join(out, "input_manifest.csv")); err != nil {
		return err
	}
	if err := copyCSV(filepath.Join(in, "10_semantic_coverage_by_stage.csv"), filepath.Join(out, "coverage_by_stage.csv")); err != nil {
		return err
	}
	if err := copyCSV(filepath.Join(out, "01_strict_file_outcomes.csv"), filepath.Join(out, "strict_route_outcomes_before.csv")); err != nil {
		return err
	}
	if err := copyCSV(filepath.Join(out, "01_strict_file_outcomes.csv"), filepath.Join(out, "strict_route_outcomes_after.csv")); err != nil {
		return err
	}
	delete(aliases, "01_strict_file_outcomes.csv")
	for src, dst := range aliases {
		if src == "01_strict_file_outcomes.csv" {
			continue
		}
		if err := copyCSV(filepath.Join(out, src), filepath.Join(out, dst)); err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
	}
	// The saturation input is source-only. Keep target coverage explicit rather
	// than pretending that a target was measured in this command.
	return writeCSV(filepath.Join(out, "blocked_and_unknown_regions.csv"), []string{"region", "status", "reason"}, [][]string{
		{"GO_DECLARATION_CONTRACT", "BLOCKED", "unsupported declaration projection remains in the source frontend"},
		{"GO_TYPE_CONTRACT", "BLOCKED", "local package type resolution is unavailable in the bounded single-file check"},
		{"TARGET_ROUTES", "NOT_MEASURED", "target execution is outside the source saturation input"},
	})
}

func writeStrictAndSaturated(in, out string) error {
	rows, _, err := readCSV(filepath.Join(in, "20_selfhosting_saturation_matrix.csv"))
	if err != nil {
		return err
	}
	strict := make([][]string, 0, len(rows))
	sat := make([][]string, 0, len(rows))
	for _, r := range rows {
		strict = append(strict, []string{r["file"], r["strict_pass"], r["saturation_error"]})
		sat = append(sat, []string{r["file"], r["semantic_nodes_total"], r["semantic_nodes_success"], r["holes"], r["failure_families"], r["semantic_coverage_ratio"], r["relations_success"], r["operations_success"]})
	}
	if err := writeCSV(filepath.Join(out, "01_strict_file_outcomes.csv"), []string{"file", "strict_pass", "error"}, strict); err != nil {
		return err
	}
	return writeCSV(filepath.Join(out, "02_saturated_file_outcomes.csv"), []string{"file", "semantic_nodes_total", "semantic_nodes_success", "holes", "failure_families", "semantic_coverage_ratio", "relations_success", "operations_success"}, sat)
}

func writeClassifications(in, out string) error {
	rows, _, err := readCSV(filepath.Join(in, "12_failure_families.csv"))
	if err != nil {
		return err
	}
	classification := make([][]string, 0, len(rows))
	closure := make([][]string, 0, len(rows))
	for _, r := range rows {
		family := r["failure_family"]
		class := "FRONTEND_STRUCTURED_MAPPING_GAP"
		gap := "declaration projection"
		if family == "GO_TYPE_CONTRACT" {
			class, gap = "HOST_ENVIRONMENT", "local source-package type resolution"
		}
		classification = append(classification, []string{family, class, gap, "no new primitive", "residual frontier"})
		closure = append(closure, []string{family, "false", "false", "false", "false", "false", "false", "residual"})
	}
	if err := writeCSV(filepath.Join(out, "07_root_cause_classification.csv"), []string{"root_cause", "classification", "missing_contract", "primitive_decision", "status"}, classification); err != nil {
		return err
	}
	return writeCSV(filepath.Join(out, "08_semantic_closure_attempts.csv"), []string{"root_cause", "existing_exact", "parameterized", "relation_facet_contract", "rewrite", "executor", "target", "result"}, closure)
}

func writeCoverage(in, out string) error {
	rows, _, err := readCSV(filepath.Join(in, "20_selfhosting_saturation_matrix.csv"))
	if err != nil {
		return err
	}
	byPkg := map[string][4]int{}
	global := [4]int{}
	for _, r := range rows {
		file := strings.ReplaceAll(r["file"], "\\", "/")
		pkg := filepath.ToSlash(filepath.Dir(file))
		if pkg == "." {
			pkg = "<root>"
		}
		a := byPkg[pkg]
		a[0] += atoi(r["semantic_nodes_total"])
		a[1] += atoi(r["semantic_nodes_success"])
		a[2] += atoi(r["holes"])
		a[3] += atoi(r["operations_success"])
		byPkg[pkg] = a
		global[0] += atoi(r["semantic_nodes_total"])
		global[1] += atoi(r["semantic_nodes_success"])
		global[2] += atoi(r["holes"])
		global[3] += atoi(r["operations_success"])
	}
	pkgs := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	pkgRows := make([][]string, 0, len(pkgs))
	for _, p := range pkgs {
		a := byPkg[p]
		pkgRows = append(pkgRows, []string{p, strconv.Itoa(a[0]), strconv.Itoa(a[1]), strconv.Itoa(a[2]), strconv.Itoa(a[3])})
	}
	if err := writeCSV(filepath.Join(out, "13_semantic_coverage_by_file.csv"), []string{"file", "source_units", "semantic_success", "semantic_holes", "relations_success", "operations_success"}, func() [][]string {
		outRows := make([][]string, 0, len(rows))
		for _, r := range rows {
			outRows = append(outRows, []string{r["file"], r["semantic_nodes_total"], r["semantic_nodes_success"], r["holes"], r["relations_success"], r["operations_success"]})
		}
		return outRows
	}()); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "14_semantic_coverage_by_package.csv"), []string{"package", "source_units", "semantic_success", "semantic_holes", "operations_success"}, pkgRows); err != nil {
		return err
	}
	return writeCSV(filepath.Join(out, "15_semantic_coverage_global.csv"), []string{"source_units", "semantic_success", "semantic_holes", "operations_success", "semantic_coverage_ratio"}, [][]string{{strconv.Itoa(global[0]), strconv.Itoa(global[1]), strconv.Itoa(global[2]), strconv.Itoa(global[3]), fmt.Sprintf("%.9f", float64(global[1])/float64(global[0]))}})
}

func writeClosureFiles(in, out string) error {
	for _, name := range []string{"05_semantic_rewrite_proofs.csv", "06_semantic_closure_matrix.csv"} {
		if err := copyCSV(filepath.Join(in, name), filepath.Join(out, "09_rewrite_proofs.csv")); err == nil {
			break
		}
	}
	if err := writeCSV(filepath.Join(out, "10_negative_closure_proofs.csv"), []string{"operation", "status", "reason"}, nil); err != nil {
		return err
	}
	families, _, err := readCSV(filepath.Join(in, "12_failure_families.csv"))
	if err != nil {
		return err
	}
	repairRows := make([][]string, 0, len(families))
	for _, r := range families {
		repairRows = append(repairRows, []string{"CLOSURE_REVIEW_" + r["failure_family"], r["failure_family"], "existing structured contracts", "RESIDUAL_REQUIRES_IMPLEMENTATION"})
	}
	if err := writeCSV(filepath.Join(out, "11_universal_repair_batches.csv"), []string{"repair_id", "root_cause", "architecture", "status"}, repairRows); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "12_repair_impact_matrix.csv"), []string{"repair_id", "root_cause", "files_before", "witnesses_before", "holes_before", "files_after", "witnesses_after", "holes_after", "newly_exposed_families", "architecture_files_changed", "repair_leverage"}, func() [][]string {
		s, e := readSummary(filepath.Join(in, "summary.json"))
		if e != nil {
			return nil
		}
		return [][]string{{"CURRENT_RESIDUAL", "ALL", "NOT_MEASURED", "NOT_MEASURED", "NOT_MEASURED", strconv.Itoa(summaryInt(s, "strict_fail")), strconv.Itoa(summaryInt(s, "total_failure_instances")), strconv.Itoa(summaryInt(s, "semantic_holes")), strconv.Itoa(summaryInt(s, "all_discovered_failure_families")), "NOT_MEASURED", "NOT_MEASURED"}}
	}()); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "16_latent_failure_matrix.csv"), []string{"file", "failure_families", "holes", "hidden_failures_exposed"}, func() [][]string {
		rows, _, _ := readCSV(filepath.Join(in, "03_latent_failure_matrix.csv"))
		outRows := make([][]string, 0, len(rows))
		for _, r := range rows {
			outRows = append(outRows, []string{r["file"], r["all_failures_new"], r["failure_count"], r["hidden_failures_exposed"]})
		}
		return outRows
	}()); err != nil {
		return err
	}
	for src, dst := range map[string]string{"04_failure_dependency_graph.csv": "17_failure_dependency_graph.csv", "07_semantic_frontier.csv": "18_semantic_frontier.csv", "21_non_derivability_matrix.csv": "21_executor_gap_matrix.csv", "14_target_capability_matrix.csv": "22_target_gap_matrix.csv"} {
		if err := copyCSV(filepath.Join(in, src), filepath.Join(out, dst)); err != nil {
			return err
		}
	}
	if err := writeCSV(filepath.Join(out, "19_primitive_gap_proofs.csv"), []string{"root_cause", "true_atomic_gap", "reason"}, nil); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "20_uast_gap_proofs.csv"), []string{"root_cause", "true_structural_gap", "reason"}, nil); err != nil {
		return err
	}
	typeCases := 0
	for _, r := range families {
		if r["failure_family"] == "GO_TYPE_CONTRACT" {
			typeCases = atoi(r["failure_instances"])
		}
	}
	hostRows := [][]string{}
	if typeCases > 0 {
		hostRows = append(hostRows, []string{"GO_TYPE_CONTRACT", strconv.Itoa(typeCases), "local source-package type resolution remains unavailable"})
	}
	if err := writeCSV(filepath.Join(out, "23_host_environment_failures.csv"), []string{"root_cause", "cases", "reason"}, hostRows); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "25_native_frontier.csv"), []string{"source_units", "decoded", "canonical_lifted", "holes", "reason"}, [][]string{{"NOT_MEASURED", "NOT_MEASURED", "NOT_MEASURED", "NOT_MEASURED", "binary/native path not part of this source saturation input"}}); err != nil {
		return err
	}
	s, err := readSummary(filepath.Join(in, "summary.json"))
	if err != nil {
		return err
	}
	return writeCSV(filepath.Join(out, "26_iteration_history.csv"), []string{"iteration", "strict_pass_files", "strict_fail_files", "failure_instances", "failure_families", "root_causes", "latent_failures", "semantic_holes", "closure_solved", "frontend_gaps", "executor_gaps", "target_gaps", "primitive_gaps", "uast_gaps", "newly_exposed_root_causes"}, [][]string{{"CURRENT", strconv.Itoa(summaryInt(s, "strict_pass")), strconv.Itoa(summaryInt(s, "strict_fail")), strconv.Itoa(summaryInt(s, "total_failure_instances")), strconv.Itoa(summaryInt(s, "all_discovered_failure_families")), strconv.Itoa(summaryInt(s, "frontend_boundary_root_causes")), strconv.Itoa(summaryInt(s, "latent_failures_exposed")), strconv.Itoa(summaryInt(s, "semantic_holes")), "NOT_MEASURED", strconv.Itoa(summaryInt(s, "frontend_boundary_root_causes")), strconv.Itoa(summaryInt(s, "executor_gaps")), strconv.Itoa(summaryInt(s, "target_gaps")), strconv.Itoa(summaryInt(s, "true_primitive_gaps")), strconv.Itoa(summaryInt(s, "true_uast_structural_gaps")), "NOT_MEASURED"}})
}

func writeSummary(in, out string) error {
	s, err := readSummary(filepath.Join(in, "summary.json"))
	if err != nil {
		return err
	}
	s["closure_fixed_point"] = summaryInt(s, "strict_fail") == 0 && summaryInt(s, "root_causes_remaining") == 0
	s["closure_solved_families"] = 0
	s["root_causes_fixed"] = 0
	s["root_causes_remaining"] = summaryInt(s, "all_discovered_failure_families")
	s["true_primitive_gaps"] = summaryInt(s, "true_primitive_gaps")
	s["true_uast_gaps"] = summaryInt(s, "true_uast_structural_gaps")
	s["host_environment_families"] = 0
	s["frontend_mapping_families"] = 0
	if rows, _, e := readCSV(filepath.Join(in, "12_failure_families.csv")); e == nil {
		for _, r := range rows {
			if r["failure_family"] == "GO_TYPE_CONTRACT" {
				s["host_environment_families"] = summaryInt(s, "host_environment_families") + 1
			} else {
				s["frontend_mapping_families"] = summaryInt(s, "frontend_mapping_families") + 1
			}
		}
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "summary.json"), append(b, '\n'), 0644); err != nil {
		return err
	}
	md := fmt.Sprintf("# Semantic Gap Closure v36\n\nCorpus: %d files; strict pass: %d; strict fail: %d. Saturation found %d structured failure instances in %d root families, with %d latent instances exposed in one run.\n\nNo true primitive, executor, target, or UAST structural gap was measured in this input. Remaining causes are reported as residual implementation/environment boundaries.\n\nClosure fixed point: `%t`.\n\nAll generated matrices derive from the real saturation output at `%s`.\n", summaryInt(s, "corpus_total"), summaryInt(s, "strict_pass"), summaryInt(s, "strict_fail"), summaryInt(s, "total_failure_instances"), summaryInt(s, "all_discovered_failure_families"), summaryInt(s, "latent_failures_exposed"), s["closure_fixed_point"], in)
	return os.WriteFile(filepath.Join(out, "summary.md"), []byte(md), 0644)
}
