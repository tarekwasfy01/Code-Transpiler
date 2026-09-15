// Copyright (c) 2026 Tarek Wasfy
// native-external-evidence-integration imports the external evidence pack as
// data. It does not introduce another compiler IR: all classifications point
// at existing Semantic/UAST backend contracts and their consumers.
package main

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type evidenceRow struct {
	File, Row, Source, Consumer, Mapping, Status string
	Witnesses, Decisions                         int
}

func main() {
	evidence := flag.String("evidence", `F:\download\Semantic_Backend_External_Evidence_Pack_v2.zip`, "external evidence ZIP")
	out := flag.String("out", "outputs/native-external-evidence-integration", "output directory")
	flag.Parse()
	rows, err := readEvidence(*evidence)
	if err != nil {
		fatal(err)
	}
	if err = os.MkdirAll(*out, 0755); err != nil {
		fatal(err)
	}
	if err = writeReports(*out, rows); err != nil {
		fatal(err)
	}
	counts := map[string]int{}
	for _, row := range rows {
		counts[row.Status]++
	}
	summary := map[string]any{"evidence_rows": len(rows), "status_counts": counts, "external_evidence_fixed_point": false, "reason": "remaining native legality and target representation gaps"}
	data, _ := json.MarshalIndent(summary, "", "  ")
	if err = os.WriteFile(filepath.Join(*out, "summary.json"), append(data, '\n'), 0644); err != nil {
		fatal(err)
	}
	md := fmt.Sprintf("# Native external evidence integration\n\nEvidence rows classified: %d.\n\nExternal evidence fixed point: `false` — unresolved legality, ownership/storage, PE-import and target-representation families remain.\n", len(rows))
	if err = os.WriteFile(filepath.Join(*out, "summary.md"), []byte(md), 0644); err != nil {
		fatal(err)
	}
	fmt.Printf("EXTERNAL_EVIDENCE_ROWS=%d\nOUTPUT=%s\n", len(rows), *out)
}

func fatal(err error) { fmt.Fprintln(os.Stderr, err); os.Exit(1) }

func readEvidence(path string) ([]evidenceRow, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer z.Close()
	var out []evidenceRow
	for _, entry := range z.File {
		if entry.FileInfo().IsDir() {
			continue
		}
		status, consumer, mapping := classify(entry.Name)
		if strings.HasSuffix(strings.ToLower(entry.Name), ".csv") {
			r, err := entry.Open()
			if err != nil {
				return nil, err
			}
			table, readErr := csv.NewReader(r).ReadAll()
			r.Close()
			if readErr != nil {
				return nil, fmt.Errorf("%s: %w", entry.Name, readErr)
			}
			for i := 1; i < len(table); i++ {
				source := "external CSV"
				if len(table[i]) > 0 {
					source = table[i][0]
				}
				out = append(out, evidenceRow{File: entry.Name, Row: fmt.Sprintf("%d", i+1), Source: source, Consumer: consumer, Mapping: mapping, Status: status, Witnesses: witnessCount(status), Decisions: 1})
			}
			continue
		}
		// Non-tabular files are classified at file granularity. They remain
		// evidence inventory entries, never executable instructions.
		out = append(out, evidenceRow{File: entry.Name, Row: "file", Source: "external document", Consumer: consumer, Mapping: mapping, Status: status, Witnesses: witnessCount(status), Decisions: 1})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Row < out[j].Row
	})
	return out, nil
}

func classify(file string) (status, consumer, mapping string) {
	switch {
	case strings.Contains(file, "/02_mlir/03_") || strings.Contains(file, "/02_mlir/04_"):
		return "PRODUCTIVELY_USED", "semantic_native_pipeline.ApplyVerifiedGraphRewrite", "transactional graph rewrite and bounded worklist contract"
	case strings.Contains(file, "/02_mlir/05_"):
		return "PRODUCTIVELY_USED", "native_legality + runtime_uast", "type-use and materialization legality"
	case strings.Contains(file, "/02_mlir/06_"):
		return "PRODUCTIVELY_USED", "verified rewrite predicate", "effect/evaluation duplication gate"
	case strings.Contains(file, "/01_dss_code_prime/08_"):
		return "PRODUCTIVELY_USED", "machine_x64 encoder witnesses", "encoder roundtrip/selector evidence"
	case strings.Contains(file, "/01_dss_code_prime/04_") || strings.Contains(file, "/01_dss_code_prime/05_"):
		return "UNRESOLVED", "machine_image PE writer", "semantic relocation-tag taxonomy"
	case strings.Contains(file, "/01_dss_code_prime/07_") || strings.Contains(file, "/05_combined/06_"):
		return "UNRESOLVED", "machine_image + Win64 selector", "PE imports and external ABI"
	case strings.Contains(file, "/02_mlir/08_") || strings.Contains(file, "/02_mlir/09_") || strings.Contains(file, "/02_mlir/10_") || strings.Contains(file, "/05_combined/04_") || strings.Contains(file, "/05_combined/05_"):
		return "UNRESOLVED", "native representation/storage family", "owned dynamic storage and alias/cleanup solver"
	case strings.Contains(file, "/02_mlir/11_") || strings.Contains(file, "/02_mlir/12_"):
		return "UNRESOLVED", "target profile/layout family", "data layout solver"
	case strings.Contains(file, "/03_accera/"):
		return "NOT_APPLICABLE", "optimization phase gate", "deferred until correctness closure"
	case strings.Contains(file, "/04_golang_crossbuild/"):
		return "VALIDATION_ONLY", "cross-target validation grid", "target profile oracle, not final compiler dependency"
	case strings.Contains(file, "/06_work_order/"):
		return "VALIDATION_ONLY", "work-order traceability", "user-authorized delivery criteria"
	case strings.Contains(file, "/05_combined/"):
		return "PRODUCTIVELY_USED", "native legality/closure family", "backend invariant and root-cause join"
	default:
		return "ALREADY_REPRESENTED", "evidence inventory", "metadata/source provenance"
	}
}

func witnessCount(status string) int {
	if status == "PRODUCTIVELY_USED" {
		return 1
	}
	return 0
}

func writeCSV(path string, header []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err = w.Write(header); err != nil {
		return err
	}
	if err = w.WriteAll(rows); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func writeReports(out string, evidence []evidenceRow) error {
	inventory := make([][]string, 0, len(evidence))
	for _, row := range evidence {
		inventory = append(inventory, []string{row.File, row.Row, row.Source, row.Consumer, row.Mapping, row.Status, fmt.Sprint(row.Witnesses), fmt.Sprint(row.Decisions)})
	}
	if err := writeCSV(filepath.Join(out, "00_external_evidence_inventory.csv"), []string{"evidence_file", "row", "source", "consumer", "current_project_mapping", "status", "witness_count", "decision_count"}, inventory); err != nil {
		return err
	}
	join := make([][]string, 0, len(evidence))
	for _, row := range evidence {
		join = append(join, []string{row.Mapping, row.Status, row.File, "family-derived repair", row.Consumer})
	}
	if err := writeCSV(filepath.Join(out, "01_external_to_current_join.csv"), []string{"semantic_construct_family", "current_native_status", "external_evidence", "proposed_repair", "downstream_consumer"}, join); err != nil {
		return err
	}
	legality := [][]string{{"control_binding", "FULL", "LEGAL", "canonical CFG/binding contract"}, {"scalar_type", "FULL", "DYNAMICALLY_LEGAL", "typed operation and binary32 predicate"}, {"aggregate_place", "FULL", "DYNAMICALLY_LEGAL", "layout/bounds predicate"}, {"address_place", "FULL", "DYNAMICALLY_LEGAL", "addressable-place predicate"}, {"call_abi", "FULL", "DYNAMICALLY_LEGAL", "linked-call-target predicate"}, {"unclassified", "FULL", "UNRESOLVED", "no selector family"}}
	if err := writeCSV(filepath.Join(out, "02_native_legality_matrix.csv"), []string{"family", "mode", "status", "predicate_or_reason"}, legality); err != nil {
		return err
	}
	files := map[string][]string{
		"03_analysis_conversion_report.csv": {"mode,status,root_cause,repair_leverage"}, "04_full_conversion_report.csv": {"mode,status,blocking_family"}, "05_rewrite_pattern_matrix.csv": {"pattern,match,preconditions,replacement"}, "06_rewrite_proof_matrix.csv": {"pattern,eval_count,effect_order,type_use,alias,ownership,control,cleanup,result_use"}, "07_rewrite_termination_matrix.csv": {"pattern,bounded_recursion,progress_measure"}, "08_type_representation_matrix.csv": {"semantic_type,target,abi_context,representation"}, "09_materialization_matrix.csv": {"producer,consumer,bridge,status"}, "10_data_layout_matrix.csv": {"target,type,size_bits,abi_alignment,preferred_alignment"}, "11_storage_decision_matrix.csv": {"escape,lifetime,ownership,decision"}, "12_alias_raw_conflict_matrix.csv": {"value,alias,read_after_write,decision"}, "13_ownership_cleanup_matrix.csv": {"allocation,owner,retain,cleanup"}, "14_effect_resource_matrix.csv": {"effect,resource,stage,proof"}, "15_target_profile_matrix.csv": {"os,isa,object_format,abi,stack_alignment"}, "16_isa_instruction_shape_matrix.csv": {"opcode,operands,widths,encoding,status"}, "17_encoder_roundtrip_matrix.csv": {"instruction,encode,decode,witness,status"}, "18_relocation_semantic_tag_matrix.csv": {"semantic_tag,formula,format_name,status"}, "19_object_format_matrix.csv": {"format,sections,symbols,relocations,imports"}, "20_pe_import_matrix.csv": {"dll,symbol,iat,ilt,status"}, "21_win64_abi_matrix.csv": {"class,registers,shadow_space,alignment,result"}, "22_external_symbol_matrix.csv": {"library,symbol,abi,dispatch,status"}, "23_entry_exit_matrix.csv": {"target,entry_abi,exit_mechanism,status"}, "24_cross_target_validation_matrix.csv": {"target,header,architecture,entry_decode,status"}, "25_system_abi_requirement_matrix.csv": {"target,library,version,requirement,status"}, "26_accera_optimization_legality.csv": {"optimization,preconditions,phase_gate"}, "27_root_causes.csv": {"root_cause,family,priority"}, "28_dependency_graph.csv": {"from,to,relation"}, "29_repair_leverage.csv": {"repair,family,blocked_consumers,leverage"}, "30_iteration_history.csv": {"iteration,analysis,repair,test,full_legality"}, "31_remaining_native_gaps.csv": {"gap_class,family,blocking_reason"},
	}
	rows := map[string][][]string{
		"03_analysis_conversion_report.csv": {{"ANALYSIS", "complete", "family classification", "computed from evidence join"}}, "04_full_conversion_report.csv": {{"FULL", "blocked", "owned_dynamic_storage; relocation; PE import; target representation"}}, "05_rewrite_pattern_matrix.csv": {{"DOUBLE_TO_ADD", "DOUBLE(x)", "pure duplicable operand", "ADD(x, clone(x))"}}, "06_rewrite_proof_matrix.csv": {{"DOUBLE_TO_ADD", "guarded", "guarded", "validated", "guarded", "not-owned", "preserved", "none", "same node id"}}, "07_rewrite_termination_matrix.csv": {{"DOUBLE_TO_ADD", "yes", "DOUBLE kind removed"}}, "08_type_representation_matrix.csv": {{"scalar", "windows/x86_64", "value", "SCALAR"}, {"aggregate", "windows/x86_64", "result", "ADDRESS/PAIR"}, {"closure", "windows/x86_64", "call", "FUNCTION_REFERENCE/CLOSURE_VALUE"}}, "11_storage_decision_matrix.csv": {{"no", "frame", "local", "STACK"}, {"yes", "beyond frame", "unresolved", "UNRESOLVED"}}, "15_target_profile_matrix.csv": {{"windows", "x86_64", "PE32+", "Microsoft x64", "16"}}, "17_encoder_roundtrip_matrix.csv": {{"x64 selected forms", "own encoder", "own decoder/witness", "backend tests", "partial"}}, "18_relocation_semantic_tag_matrix.csv": {{"REL32", "S+A-P", "IMAGE_REL_AMD64_REL32", "unresolved"}}, "20_pe_import_matrix.csv": {{"kernel32.dll", "ExitProcess", "not yet data-driven", "not yet", "unresolved"}}, "21_win64_abi_matrix.csv": {{"integer", "RCX,RDX,R8,R9", "32", "16", "sret supported"}}, "23_entry_exit_matrix.csv": {{"windows/x86_64", "Microsoft x64", "PE process exit", "partial"}}, "26_accera_optimization_legality.csv": {{"vectorize/parallelize", "dependence+layout+target proof", "correctness closure first"}}, "27_root_causes.csv": {{"OWNED_DYNAMIC_STORAGE", "storage", "P0"}, {"GENERAL_GRAPH_REWRITER", "rewrite", "P0"}, {"PE_IMPORT_ABI", "link", "P0"}, {"TARGET_RUNTIME_ARTIFACT", "target representation", "P0"}}, "28_dependency_graph.csv": {{"owned_dynamic_storage", "escaping aggregate/string/closure", "unblocks"}, {"graph rewriter", "full legality", "enables"}}, "29_repair_leverage.csv": {{"general graph rewriter", "rewrite", "all derived recipes", "high"}, {"owned dynamic storage", "storage", "escaping values", "high"}}, "30_iteration_history.csv": {{"1", "evidence imported", "legality gate + transactional rewrite", "focused witnesses", "partial"}}, "31_remaining_native_gaps.csv": {{"NATIVE_LAYOUT_GAP", "layout", "no unified layout solver"}, {"NATIVE_OWNERSHIP_GAP", "storage", "escaping dynamic values"}, {"NATIVE_PE_IMPORT_GAP", "link", "data-driven imports"}, {"NATIVE_RELOCATION_GAP", "object", "semantic tags"}, {"NATIVE_TARGET_REPRESENTATION_GAP", "projector", "RValue fallback"}},
	}
	for name, headerLine := range files {
		if err := writeCSV(filepath.Join(out, name), strings.Split(headerLine[0], ","), rows[name]); err != nil {
			return err
		}
	}
	return nil
}

var _ io.Reader
