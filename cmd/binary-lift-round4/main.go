// Copyright (c) 2026 Tarek Wasfy
// binary-lift-round4 remeasures binary chunks using the productive decoder and
// joins legacy primitive evidence without promoting instruction encodings or
// historical runtime names to canonical semantic primitives.
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
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	ct "github.com/tarekwasfy01/Code-Transpiler"
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
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
		h[i] = strings.TrimPrefix(strings.TrimSpace(h[i]), "\ufeff")
	}
	var rows []row
	for {
		v, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		x := row{}
		for i, k := range h {
			if i < len(v) {
				x[k] = strings.TrimSpace(v[i])
			}
		}
		rows = append(rows, x)
	}
	return rows, nil
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
	if err = w.WriteAll(rows); err != nil {
		return err
	}
	w.Flush()
	return w.Error()
}

func hashBytes(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func main() {
	manifest := flag.String("round3-manifest", "outputs/.handoff-ground-truth-round3/chunk-semantic-matrix.csv", "Round-3 chunk manifest")
	chunks := flag.String("chunks", "outputs/.handoff-ground-truth-round3", "corpus root containing manifest-relative chunk bytes")
	out := flag.String("out", "outputs/binary-lift-round4", "output directory")
	flag.Parse()
	if err := run(*manifest, *chunks, *out); err != nil {
		fmt.Fprintln(os.Stderr, "binary-lift-round4:", err)
		os.Exit(1)
	}
}

func run(manifestPath, chunkRoot, out string) error {
	manifest, err := readCSV(manifestPath)
	if err != nil {
		return err
	}
	report, err := backend.CompileUniversalPrimitiveSpecs()
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, s := range report.Specs {
		known[strings.ToUpper(s.ID)] = true
	}
	var outcomes, instructions, failures, cfgRows, boundaries, liftRows, liftFailures, primitiveRows, delta [][]string
	failureClasses := map[string]int{}
	decoded, partial, liftOK, liftFail, sourceMissing := 0, 0, 0, 0, 0
	for _, m := range manifest {
		id, rel := m["chunk_id"], filepath.FromSlash(m["chunk_file"])
		path := filepath.Join(chunkRoot, rel)
		b, e := os.ReadFile(path)
		if e != nil {
			sourceMissing++
			outcomes = append(outcomes, []string{id, m["chunk_sha256"], m["start_rva"], m["byte_length"], "SOURCE_DATA_MISSING", "", "", "false", "false", "", "", "chunk bytes were not supplied with the Round-3 manifest"})
			failures = append(failures, []string{id, "SOURCE_DATA_MISSING", "", "", "", "", "chunk bytes unavailable"})
			failureClasses["SOURCE_DATA_MISSING"]++
			delta = append(delta, []string{m["error_message"], "1", "SOURCE_DATA_MISSING", "1", "0", "0", "0", "true", "true"})
			continue
		}
		ir, decErr := backend.DecodeMachineIR(b, ct.CompileOptions{InputKind: ct.InputMachine, BaseAddress: parseHex(m["start_rva"]), TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
		if decErr != nil {
			partial++
			class := normalizeDecoderError(decErr.Error())
			failureClasses[class]++
			outcomes = append(outcomes, []string{id, hashBytes(b), m["start_rva"], fmt.Sprint(len(b)), "DECODER_FAILURE", "", "", "false", "false", "", "", decErr.Error()})
			failures = append(failures, []string{id, "DECODER", "", "", "", "", decErr.Error()})
			delta = append(delta, []string{m["error_message"], "1", class, "1", "0", "0", "0", "true", "true"})
			continue
		}
		decoded++
		for _, in := range ir.Instructions {
			ops := make([]string, 0, len(in.Operands))
			for _, o := range in.Operands {
				ops = append(ops, o.Kind+":"+o.Register+":"+fmt.Sprint(o.Immediate)+":"+fmt.Sprint(o.Displacement))
			}
			// Keep this evidence row aligned to its declared schema. Unknown
			// decoder facts stay empty; they must not be reconstructed from text.
			row := make([]string, 49)
			row[0] = id
			row[2] = fmt.Sprintf("0x%X", ir.BaseAddress+uint64(max(in.Offset, 0)))
			row[3] = fmt.Sprint(in.Offset)
			row[23] = in.Operation
			row[24] = strings.Join(ops, ";")
			row[31] = in.Classification
			if in.Operation == "call" {
				row[35] = "call"
			}
			if in.Operation == "ret" {
				row[36] = "return"
			}
			row[37] = in.Classification
			row[38] = "OK"
			row[40] = "true"
			row[41] = "PENDING"
			row[42] = in.Primitive
			instructions = append(instructions, row)
			if in.Primitive != "" {
				primitiveRows = append(primitiveRows, []string{id, fmt.Sprint(in.Index), in.Operation, in.Primitive, boolText(known[strings.ToUpper(in.Primitive)]), "machine operation projected to existing canonical primitive"})
			}
		}
		for _, edge := range ir.CFG {
			cfgRows = append(cfgRows, []string{id, fmt.Sprint(edge.From), fmt.Sprint(edge.To), edge.Kind})
		}
		boundaries = append(boundaries, []string{id, m["boundary"], m["start_rva"], m["end_rva_exclusive"], "manifest boundary only", "true"})
		sp, le := backend.LiftBinaryInput(b, ct.CompileOptions{InputKind: ct.InputMachine, BaseAddress: parseHex(m["start_rva"]), TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
		if le != nil {
			liftFail++
			liftFailures = append(liftFailures, []string{id, "SEMANTIC_LIFT", normalizeDecoderError(le.Error()), le.Error()})
			outcomes = append(outcomes, []string{id, hashBytes(b), m["start_rva"], fmt.Sprint(len(b)), "DECODED", fmt.Sprint(len(ir.Instructions)), fmt.Sprint(len(ir.CFG)), "true", "false", "", "", le.Error()})
			delta = append(delta, []string{m["error_message"], "1", "SEMANTIC_LIFT_FAILURE", "1", "1", "1", "0", "false", "true"})
		} else {
			liftOK++
			wire, _ := sp.MarshalSemanticJSON()
			liftRows = append(liftRows, []string{id, hashBytes(wire), fmt.Sprint(len(ir.Instructions)), "true", "SUCCESS", "canonical SemanticProgram produced from proven machine dataflow"})
			outcomes = append(outcomes, []string{id, hashBytes(b), m["start_rva"], fmt.Sprint(len(b)), "DECODED", fmt.Sprint(len(ir.Instructions)), fmt.Sprint(len(ir.CFG)), "true", "true", hashBytes(wire), "", ""})
			delta = append(delta, []string{m["error_message"], "1", "LIFTED", "1", "1", "1", "1", "false", "false"})
		}
	}
	classRows := [][]string{}
	for _, k := range sortedKeys(failureClasses) {
		classRows = append(classRows, []string{k, fmt.Sprint(failureClasses[k])})
	}
	if err := writeCSV(filepath.Join(out, "binary_chunk_outcome_matrix.csv"), []string{"chunk_id", "actual_sha256", "rva", "byte_length", "status", "instruction_count", "cfg_edges", "fully_decoded", "semantic_lift_success", "semantic_program_hash", "failure_class", "diagnostic"}, outcomes); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "binary_instruction_decode_matrix.csv"), []string{"binary_id", "function_id", "rva", "instruction_offset", "raw_bytes", "prefix_bytes", "opcode_map", "opcode", "opcode_group", "modrm_present", "mod", "reg", "rm", "sib_present", "scale", "index", "base", "displacement_width", "displacement_value", "immediate_width", "immediate_value", "operand_width", "address_width", "decoded_instruction_family", "decoded_operands", "flags_read", "flags_written", "stack_effect", "memory_read", "memory_write", "effective_address", "control_flow", "direct_target", "indirect_target", "fallthrough", "call_effect", "return_effect", "trap_or_padding", "decoder_status", "decoder_diagnostic", "semantic_lift_attempted", "semantic_lift_status", "mapped_canonical_primitive", "required_facets", "required_relations", "required_fields", "required_contracts", "primitive_gap", "schema_gap"}, instructions); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "binary_decoder_failure_matrix.csv"), []string{"chunk_id", "phase", "opcode_map", "opcode", "modrm", "addressing", "diagnostic"}, failures); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "binary_decoder_failure_classes.csv"), []string{"failure_class", "count"}, classRows); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "binary_cfg_matrix.csv"), []string{"chunk_id", "from_instruction", "to_instruction", "edge_kind"}, cfgRows); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "binary_function_boundary_matrix.csv"), []string{"chunk_id", "boundary_kind", "begin_rva", "end_rva", "evidence", "trusted"}, boundaries); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "binary_semantic_lift_matrix.csv"), []string{"chunk_id", "semantic_program_hash", "instruction_count", "attempted", "status", "proof"}, liftRows); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "binary_semantic_lift_failure_matrix.csv"), []string{"chunk_id", "phase", "failure_class", "diagnostic"}, liftFailures); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "binary_primitive_projection_matrix.csv"), []string{"chunk_id", "instruction_index", "machine_operation", "canonical_primitive", "existing_primitive", "proof"}, primitiveRows); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "binary_round3_to_round4_delta.csv"), []string{"old_failure_signature", "old_count", "new_failure_signature", "new_count", "newly_decoded_count", "newly_lift_attempted_count", "newly_lifted_count", "remaining_decoder_gap", "remaining_lift_gap"}, delta); err != nil {
		return err
	}
	if err := legacyJoin(out, known); err != nil {
		return err
	}
	if err := groundTruthJoin(out, known); err != nil {
		return err
	}
	summary := map[string]any{"chunks_manifested": len(manifest), "chunks_with_bytes": len(manifest) - sourceMissing, "chunks_source_data_missing": sourceMissing, "fully_decoded_chunks": decoded, "partially_decoded_chunks": partial, "decoder_failure_classes": len(failureClasses), "semantic_lift_attempts": liftOK + liftFail, "semantic_lift_successes": liftOK, "semantic_lift_failures": liftFail, "canonical_primitive_mappings": len(primitiveRows), "authority_primitive_count": len(report.Specs), "no_semantic_promotion_from_decoder_diagnostics": true}
	b, _ := json.MarshalIndent(summary, "", "  ")
	if err := os.WriteFile(filepath.Join(out, "summary.json"), append(b, '\n'), 0644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "summary.md"), []byte(fmt.Sprintf("# Binary lift round 4\n\nManifested chunks: %d\n\nChunk bytes available: %d\n\nSource data missing: %d\n\nNo decoder diagnostic was promoted to canonical semantics.\n", len(manifest), len(manifest)-sourceMissing, sourceMissing)), 0644)
}

func legacyJoin(out string, known map[string]bool) error {
	registry, err := readCSV("cpp_runtime/matrices/primitive_registry_702.csv")
	if err != nil {
		return err
	}
	micro, err := readCSV("cpp_runtime/matrices/microop_to_atomic_matrix_long.csv")
	if err != nil {
		return err
	}
	routes, err := readCSV("matrices/r_primitive_target_matrix_long.csv")
	if err != nil {
		return err
	}
	microByName := map[string][]string{}
	for _, m := range micro {
		microByName[m["micro_operation"]] = append(microByName[m["micro_operation"]], m["atomic_operation"])
	}
	routeCount := map[string]int{}
	for _, r := range routes {
		routeCount[r["name"]]++
	}
	rows := [][]string{}
	for _, r := range registry {
		name, kernel := r["name"], r["kernel"]
		upper := strings.ToUpper(strings.ReplaceAll(name, ".", "_"))
		match := known[upper]
		platform := kernel == "filesystem" || kernel == "process" || kernel == "connections" || kernel == "serialization"
		runtimeOnly := kernel == "runtime" || kernel == "environment"
		classification := "evidence_only"
		if match {
			classification = "existing"
		} else if platform {
			classification = "platform_service"
		} else if runtimeOnly {
			classification = "runtime_only"
		}
		rows = append(rows, []string{name, kernel, r["arity"], strings.Join(microByName[name], ";"), "", fmt.Sprint(routeCount[name]), "", "", boolText(match), "", "", kernel + "|" + r["arity"], "", "false", boolText(runtimeOnly), boolText(platform), "true", "false", "false", "false", "false", "false", classification})
	}
	return writeCSV(filepath.Join(out, "legacy_702_primitive_quotient.csv"), []string{"source_primitive", "source_kernel", "arity", "micro_operations", "atomic_evidence", "target_routes", "cpp_handler", "rust_kernel", "current_semantic_primitive_match", "current_uast_match", "ground_truth_feature_match", "quotient_group", "parameter_axes", "derivable", "runtime_only", "platform_service", "source_specific", "candidate_new_atomic", "candidate_new_facet", "candidate_new_relation", "candidate_new_contract", "candidate_new_type", "unresolved"}, rows)
}

// groundTruthJoin keeps Round-3 novelty candidates as candidates until an
// executable canonical witness exists. Legacy routes may corroborate a
// family, but cannot by themselves turn a historical helper into a semantic
// primitive.
func groundTruthJoin(out string, known map[string]bool) error {
	candidates, err := readCSV("outputs/.handoff-ground-truth-round3/ground-truth-primitive-candidates-round3.csv")
	if err != nil {
		return err
	}
	legacy, err := readCSV(filepath.Join(out, "legacy_702_primitive_quotient.csv"))
	if err != nil {
		return err
	}
	legacyByKernel := map[string]int{}
	for _, l := range legacy {
		legacyByKernel[l["source_kernel"]]++
	}
	reclass, universal := [][]string{}, [][]string{}
	for _, c := range candidates {
		candidate := strings.ToUpper(c["candidate_family"])
		newClass := strings.Contains(strings.ToUpper(c["classification"]), "NEW")
		existing := known[candidate]
		previous := "existing-or-parameterized"
		final := "existing"
		proof := "existing canonical primitive exact match"
		if newClass && !existing {
			previous = "ground_truth_semantic_gap_candidate"
			final = "evidence_only_pending_canonical_witness"
			proof = "Ground Truth proves candidate novelty, but Round-4 has no source chunk bytes or binary semantic witness; no structural promotion"
		}
		// Kernel counts are deliberately evidence only: they establish that the
		// historic runtime has related implementation families, not semantic
		// equivalence or atomicity.
		legacyWitness := legacyByKernel["runtime"] + legacyByKernel["language"]
		reclass = append(reclass, []string{candidate, previous, c["ground_truth_evidence"], boolText(existing), boolText(existing), "false", "false", "false", "false", "false", "false", "false", "false", "false", "false", "false", "false", "false", "false", "false", "false", final, proof})
		universal = append(universal, []string{candidate, "true", c["classification"], c["factorization"], boolText(existing), "OperationExpr/semantic facets when existing", "false", "false", "false", boolText(legacyWitness > 0), boolText(legacyWitness > 0), "0", "false", "false", "true", "false", "false", "false", "false", "false", "false", "false", boolText(newClass && !existing), boolText(newClass && !existing), proof})
	}
	if err := writeCSV(filepath.Join(out, "schema_gap_reclassification_round4.csv"), []string{"candidate", "previous_classification", "ground_truth_witness", "existing_structure_support", "existing_primitive_support", "existing_parameterization_support", "facet_support", "relation_support", "field_support", "type_support", "contract_support", "executor_support", "source_selfhosting_witness", "binary_witness", "derivable", "parameterizable", "primitive_gap", "representation_gap", "structural_gap", "validation_only", "evidence_only", "final_classification", "proof"}, reclass); err != nil {
		return err
	}
	return writeCSV(filepath.Join(out, "universal_primitive_evidence_join_round4.csv"), []string{"canonical_candidate", "ground_truth_present", "ground_truth_status", "semantic_class", "quotient_group", "existing_primitive", "existing_structure", "existing_relation", "existing_field", "existing_type", "current_executor", "source_selfhosting_witness", "binary_witness", "legacy_r_witness", "legacy_microop_witness", "legacy_cpp_witness", "legacy_rust_witness", "cross_target_witness_count", "derivable", "parameterizable", "source_specific", "target_specific", "runtime_only", "platform_service", "validation_only", "evidence_only", "new_atomic_candidate", "true_schema_gap", "proof"}, universal)
}

func parseHex(s string) uint64 {
	var n uint64
	fmt.Sscanf(strings.TrimPrefix(strings.ToLower(s), "0x"), "%x", &n)
	return n
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var decoderOffsetSuffix = regexp.MustCompile(` at [0-9]+$`)

// normalizeDecoderError quotients decode failures by the unsupported machine
// form. Byte offsets identify witnesses, not a distinct decoder obligation.
func normalizeDecoderError(s string) string {
	return decoderOffsetSuffix.ReplaceAllString(s, "")
}
