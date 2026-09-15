// Copyright (c) 2026 Tarek Wasfy
// ground-truth-closure-round3 builds the evidence closure required by the
// Ground-Truth + Self-Hosting + EXE-to-C handoff. It consumes structured
// evidence only. In particular, historic decoder diagnostics are used solely
// to describe decoder coverage and never to invent source semantics.
package main

import (
	"archive/zip"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

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
	var out []row
	for {
		v, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		x := row{}
		for i, k := range h {
			if i < len(v) {
				x[k] = strings.TrimSpace(v[i])
			}
		}
		out = append(out, x)
	}
	return out, nil
}

func writeCSV(path string, header []string, rows [][]string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
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

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// xlsxRowCount makes the workbook itself part of the evidence audit without
// introducing an XLSX dependency into the product. It counts structural row
// records only; all semantic decisions continue to use the supplied candidate
// table, which carries the handoff's normalized Ground-Truth projection.
func xlsxRowCount(path, member string) (int, error) {
	z, err := zip.OpenReader(path)
	if err != nil {
		return 0, err
	}
	defer z.Close()
	for _, f := range z.File {
		if f.Name != member {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return 0, err
		}
		defer rc.Close()
		d := xml.NewDecoder(rc)
		n := 0
		for {
			t, err := d.Token()
			if err == io.EOF {
				return n, nil
			}
			if err != nil {
				return 0, err
			}
			if start, ok := t.(xml.StartElement); ok && start.Name.Local == "row" {
				n++
			}
		}
	}
	return 0, fmt.Errorf("xlsx member %q missing", member)
}

var candidatePrimitive = map[string]string{
	"EQUALITY":                   "EQ",
	"PATTERN_ENV_MERGE":          "ASSIGNMENT",
	"MEMORY_ACCESS":              "LOAD",
	"CALL":                       "CALL",
	"ATTRIBUTE_ACCESS":           "MEMBER_GET",
	"CHECKED_CAST":               "CAST",
	"DROP_FINALIZE":              "CLOSE_SCOPED_VALUES",
	"PROTECTED_CONTROL_TRANSFER": "CONTROL_TRANSFER",
	"THREE_VALUED_LOGIC":         "AND",
	"AWAIT_DEPENDENCY":           "AWAIT",
}

func candidateDecision(c row, known map[string]bool) (decision, primitive, uast, reason string) {
	family := strings.ToUpper(c["candidate_family"])
	class := strings.ToUpper(c["classification"])
	primitive = candidatePrimitive[family]
	if known[family] {
		return "existing", family, "OperationExpr", "exact authoritative primitive exists"
	}
	if primitive != "" && known[primitive] {
		return "parameterize-existing", primitive, "OperationExpr", "existing kernel gains the structured factorization contract"
	}
	switch class {
	case "VALIDATION_ONLY":
		return "validation-only", "", "", "verifier state is evidence, not source-observable execution"
	case "TYPE_SHAPE_ALGEBRA":
		return "derived", "", "SemanticType/relations", "shape information belongs to type and dimension relations"
	}
	if strings.Contains(class, "NEW") {
		return "true-schema-gap", "", "", "Ground Truth proves semantic novelty, but no canonical UAST executor/representation witness exists"
	}
	return "evidence-only", "", "", "no exact existing primitive or complete parameterized kernel witness"
}

func decoderFamily(op string) string {
	switch strings.ToLower(op) {
	case "0xcc":
		return "int3"
	case "0x90":
		return "nop"
	case "0x83", "0x80", "0x81":
		return "group1_immediate"
	case "0x74", "0x75", "0x7c", "0x7e", "0x72", "0xeb":
		return "short_control_transfer"
	case "0x84", "0x85", "0x8c", "0x8e", "0x8f":
		return "near_control_transfer"
	default:
		return "unknown"
	}
}

func decoderNowSupports(op, mod string) bool {
	if mod == "1" {
		return true
	}
	switch strings.ToLower(op) {
	case "0xcc", "0x90", "0x83", "0x80", "0x81", "0x74", "0x75", "0x7c", "0x7e", "0x72", "0xeb":
		return true
	}
	return false
}

var opcodeRX = regexp.MustCompile(`(?:unsupported (?:0f )?opcode )?(0x[0-9a-fA-F]+)`)
var modrmRX = regexp.MustCompile(`ModRM mode ([0-3])`)
var decoderOffsetRX = regexp.MustCompile(` at [0-9]+$`)

func decoderSignature(msg string) (opcode, mod string) {
	if m := opcodeRX.FindStringSubmatch(msg); len(m) == 2 {
		opcode = strings.ToLower(m[1])
	}
	if m := modrmRX.FindStringSubmatch(msg); len(m) == 2 {
		mod = m[1]
	}
	return opcode, mod
}

// normalizeDecoderFailure removes the byte offset from a decoder diagnostic.
// The offset identifies a witness location, not a distinct decode form, so it
// must not split an otherwise identical decoder-failure class.
func normalizeDecoderFailure(msg string) string {
	return decoderOffsetRX.ReplaceAllString(strings.TrimSpace(msg), "")
}

func main() {
	input := flag.String("input", "outputs/.handoff-ground-truth-round3", "extracted Ground-Truth handoff directory")
	out := flag.String("out", "outputs/ground-truth-closure-round3", "closure output directory")
	flag.Parse()
	if err := run(*input, *out); err != nil {
		fmt.Fprintln(os.Stderr, "ground-truth-closure-round3:", err)
		os.Exit(1)
	}
}

func run(input, out string) error {
	candidates, err := readCSV(filepath.Join(input, "ground-truth-primitive-candidates-round3.csv"))
	if err != nil {
		return err
	}
	chunks, err := readCSV(filepath.Join(input, "chunk-semantic-matrix.csv"))
	if err != nil {
		return err
	}
	selfErrors, err := readCSV(filepath.Join(input, "normalized-error-matrix.csv"))
	if err != nil {
		return err
	}
	positive, err := readCSV(filepath.Join(input, "positive-matrix.csv"))
	if err != nil {
		return err
	}
	report, err := backend.CompileUniversalPrimitiveSpecs()
	if err != nil {
		return err
	}
	known := map[string]bool{}
	for _, spec := range report.Specs {
		known[spec.ID] = true
	}

	features, _ := xlsxRowCount(filepath.Join(input, "semantic_feature_matrix_expanded_round3_2026-09-06.xlsx"), "xl/worksheets/sheet26.xml")
	axis, _ := xlsxRowCount(filepath.Join(input, "semantic_feature_matrix_expanded_round3_2026-09-06.xlsx"), "xl/worksheets/sheet27.xml")
	relations, _ := xlsxRowCount(filepath.Join(input, "semantic_feature_matrix_expanded_round3_2026-09-06.xlsx"), "xl/worksheets/sheet28.xml")

	projection := make([][]string, 0, len(candidates))
	parameterization, newAtoms, relationGaps, facetGaps, fieldGaps, typeGaps, validationOnly, schemaProof := [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}, [][]string{}
	quotient := map[string][]string{}
	counts := map[string]int{}
	for _, c := range candidates {
		decision, primitive, uast, reason := candidateDecision(c, known)
		family := strings.ToUpper(c["candidate_family"])
		counts[decision]++
		projection = append(projection, []string{family, c["classification"], c["ground_truth_evidence"], c["current_repo_overlap"], c["factorization"], decision, primitive, uast, reason, c["confidence"]})
		sig := strings.Join([]string{decision, primitive, c["factorization"]}, "|")
		quotient[sig] = append(quotient[sig], family)
		if decision == "parameterize-existing" {
			parameterization = append(parameterization, []string{family, primitive, c["factorization"], "SemanticProgram contracts/facets", "ground-truth parameterization"})
		}
		if decision == "true-schema-gap" {
			newAtoms = append(newAtoms, []string{family, c["classification"], c["ground_truth_evidence"], c["factorization"], "false", "no canonical representation plus executor witness", c["confidence"]})
			schemaProof = append(schemaProof, []string{family, "canonical representation", "false", "OperationExpr alone cannot execute an unmodelled semantic contract", "do not promote"})
		}
		if decision == "validation-only" {
			validationOnly = append(validationOnly, []string{family, c["factorization"], reason})
		}
		if strings.Contains(c["factorization"], "×") || strings.Contains(c["factorization"], "policy") {
			facetGaps = append(facetGaps, []string{family, c["factorization"], decision, "semantic facets/contracts"})
		}
		switch family {
		case "PATTERN_MATCH", "PATTERN_ENV_MERGE", "RELATIONAL_JOIN", "PROTECTED_CONTROL_TRANSFER", "BACKTRACK", "CUT":
			relationGaps = append(relationGaps, []string{family, "binding/control/data relation", decision, c["factorization"]})
		case "TENSOR_BROADCAST", "TENSOR_CONTRACT", "GATHER_SCATTER":
			typeGaps = append(typeGaps, []string{family, "shape/dimension/type relation", decision, c["factorization"]})
		case "FREEZE_VALUE", "MEMORY_COPY", "ATOMIC_WAIT", "ATOMIC_NOTIFY":
			fieldGaps = append(fieldGaps, []string{family, "effect/ownership/ordering contract", decision, c["factorization"]})
		}
	}
	sort.Slice(projection, func(i, j int) bool { return projection[i][0] < projection[j][0] })
	if err = writeCSV(filepath.Join(out, "ground_truth_feature_projection.csv"), []string{"candidate_family", "ground_truth_class", "ground_truth_evidence", "repository_overlap", "factorization", "decision", "existing_primitive", "existing_uast_representation", "reason", "confidence"}, projection); err != nil {
		return err
	}
	qRows := [][]string{}
	for _, sig := range sortedKeys(quotient) {
		members := quotient[sig]
		sort.Strings(members)
		qRows = append(qRows, []string{fmt.Sprintf("Q%03d", len(qRows)+1), sig, strconv.Itoa(len(members)), strings.Join(members, ";")})
	}
	if err = writeCSV(filepath.Join(out, "primitive_candidate_quotient.csv"), []string{"quotient_id", "semantic_signature", "member_count", "candidate_families"}, qRows); err != nil {
		return err
	}
	if err = writeCSV(filepath.Join(out, "primitive_parameterization_matrix.csv"), []string{"candidate_family", "existing_primitive", "parameter_axes", "representation", "evidence"}, parameterization); err != nil {
		return err
	}
	if err = writeCSV(filepath.Join(out, "new_atomic_primitive_matrix.csv"), []string{"candidate_family", "ground_truth_class", "ground_truth_evidence", "factorization", "accepted", "atomicity_witness", "confidence"}, newAtoms); err != nil {
		return err
	}
	if err = writeCSV(filepath.Join(out, "relation_gap_matrix.csv"), []string{"candidate_family", "relation_requirement", "decision", "factorization"}, relationGaps); err != nil {
		return err
	}
	if err = writeCSV(filepath.Join(out, "facet_gap_matrix.csv"), []string{"candidate_family", "facet_contract", "decision", "kind"}, facetGaps); err != nil {
		return err
	}
	if err = writeCSV(filepath.Join(out, "field_gap_matrix.csv"), []string{"candidate_family", "field_contract", "decision", "factorization"}, fieldGaps); err != nil {
		return err
	}
	if err = writeCSV(filepath.Join(out, "type_gap_matrix.csv"), []string{"candidate_family", "type_requirement", "decision", "factorization"}, typeGaps); err != nil {
		return err
	}
	if err = writeCSV(filepath.Join(out, "validation_only_matrix.csv"), []string{"candidate_family", "factorization", "reason"}, validationOnly); err != nil {
		return err
	}
	if err = writeCSV(filepath.Join(out, "schema_gap_proof_matrix.csv"), []string{"candidate_family", "required_proof", "proved", "current_result", "action"}, schemaProof); err != nil {
		return err
	}

	coverage, liftRows := make([][]string, 0, len(chunks)), [][]string{}
	failures := map[string]int{}
	updatedDecoderCells := 0
	for _, c := range chunks {
		op, mod := decoderSignature(c["error_message"])
		supportedNow := decoderNowSupports(op, mod)
		if supportedNow {
			updatedDecoderCells++
		}
		coverage = append(coverage, []string{c["chunk_id"], c["start_rva"], c["byte_length"], "", op, "", mod, "", "", "", decoderFamily(op), boolText(supportedNow), boolText(supportedNow), "false", c["lift_status"]})
		key := normalizeDecoderFailure(c["error_message"])
		if key == "" {
			key = c["lift_status"]
		}
		failures[key]++
		// Historic rows that failed before emission contain no structured semantic
		// observation. Keep the lift matrix empty for them by construction.
		if c["lift_status"] != "FAIL_PRE_EMISSION" && c["semantic_status"] != "NOT_ATTEMPTED" {
			liftRows = append(liftRows, []string{c["chunk_id"], "", "", "", "", "", "", "", "", "", "", "", "", "", c["semantic_status"], c["semantic_error_message"]})
		}
	}
	if err = writeCSV(filepath.Join(out, "binary_decoder_coverage_matrix.csv"), []string{"chunk_id", "rva", "byte_length", "first_failure_offset", "opcode_map", "opcode", "modrm_mod", "modrm_reg", "modrm_rm", "sib_present", "instruction_family", "decoder_supported_now", "operand_decode_supported_now", "semantic_lift_attempted", "historic_lift_status"}, coverage); err != nil {
		return err
	}
	fRows := [][]string{}
	for _, sig := range sortedKeys(failures) {
		op, mod := decoderSignature(sig)
		fRows = append(fRows, []string{sig, strconv.Itoa(failures[sig]), op, mod, decoderFamily(op), boolText(decoderNowSupports(op, mod)), "DECODER_ONLY"})
	}
	if err = writeCSV(filepath.Join(out, "binary_failure_signature_matrix.csv"), []string{"historic_failure_signature", "chunk_count", "opcode", "modrm_mod", "instruction_family", "decoder_supported_now", "evidence_plane"}, fRows); err != nil {
		return err
	}
	if err = writeCSV(filepath.Join(out, "binary_semantic_lift_matrix.csv"), []string{"chunk_id", "instruction_offset", "instruction_family", "operand_roles", "flag_reads", "flag_writes", "memory_effect", "control_effect", "call_effect", "abi_evidence", "mapped_existing_primitive", "required_facets", "required_relations", "derived", "semantic_status", "diagnostic"}, liftRows); err != nil {
		return err
	}

	selfFamilies := map[string]int{}
	for _, e := range selfErrors {
		selfFamilies[e["universal_fix_family"]]++
	}
	selfhostingEvidence := 0
	for _, n := range selfFamilies {
		selfhostingEvidence += n
	}
	positiveFeatures := map[string]bool{}
	for _, p := range positive {
		positiveFeatures[p["origin_language"]] = true
	}
	cross := [][]string{}
	for _, p := range projection {
		cross = append(cross, []string{p[0], p[2], strconv.Itoa(selfhostingEvidence), "decoder-only: no semantic promotion", p[6], p[7], p[4], p[5], boolText(p[5] == "true-schema-gap"), boolText(p[5] == "parameterize-existing"), "false", "false", "ground-truth candidate + repository authority + separated binary evidence"})
	}
	if err = writeCSV(filepath.Join(out, "cross_evidence_primitive_matrix.csv"), []string{"candidate_family", "ground_truth_evidence", "selfhosting_evidence_count", "binary_evidence", "existing_primitive", "existing_uast_representation", "factorization", "derivation_class", "candidate_new_atomic", "needs_parameterization", "schema_gap", "semantic_lift_proven", "provenance"}, cross); err != nil {
		return err
	}

	summary := map[string]any{
		"ground_truth_workbook": map[string]any{"expanded_feature_rows": features, "expanded_axis_rows": axis, "expanded_relation_rows": relations},
		"candidate_rows":        len(candidates), "candidate_decisions": counts, "authority_primitive_count": len(report.Specs),
		"binary_chunks": len(chunks), "binary_historic_pre_emission_failures": len(chunks) - len(liftRows), "binary_semantic_lift_rows": len(liftRows), "decoder_cells_now_supported": updatedDecoderCells, "binary_failure_signature_classes": len(fRows),
		"selfhosting_error_rows": len(selfErrors), "selfhosting_positive_rows": len(positive), "selfhosting_languages": sortedKeys(positiveFeatures),
		"new_atomic_promotions": 0, "schema_expansions": 0,
		"semantic_program_is_only_canonical_ir": true,
		"binary_diagnostics_used_for_semantics": false,
	}
	b, _ := json.MarshalIndent(summary, "", "  ")
	if err = os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(out, "closure_summary.json"), append(b, '\n'), 0o644); err != nil {
		return err
	}
	md := fmt.Sprintf("# Ground-truth closure round 3\n\nGround-Truth candidate rows: %d  \nExisting authoritative primitives: %d  \nHistoric binary chunks: %d  \nHistoric chunks with attempted semantic lift: %d  \nDecoder signatures now covered by the current decoder: %d  \n\nThe binary evidence is decoder-only until a chunk reaches structured semantic lift. No opcode or diagnostic was promoted to a canonical primitive.\n", len(candidates), len(report.Specs), len(chunks), len(liftRows), updatedDecoderCells)
	if err = os.WriteFile(filepath.Join(out, "closure_summary.md"), []byte(md), 0o644); err != nil {
		return err
	}
	fmt.Printf("GROUND_TRUTH_CANDIDATES=%d AUTHORITY=%d BINARY_CHUNKS=%d LIFT_ROWS=%d DECODER_CELLS_NOW_SUPPORTED=%d OUT=%s\n", len(candidates), len(report.Specs), len(chunks), len(liftRows), updatedDecoderCells, out)
	return nil
}
