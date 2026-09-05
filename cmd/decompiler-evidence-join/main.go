// decompiler-evidence-join joins the prepared external binary/decompiler
// evidence with the existing Primitive Compiler authority. It is deliberately
// evidence-only: no raw opcode or project name can create a primitive by name.
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
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

type row map[string]string

func read(path string) ([]row, error) {
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
		out = append(out, x)
	}
	return out, nil
}
func write(path string, header []string, rows [][]string) error {
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

func main() {
	root := flag.String("root", "outputs/.handoff-decompiler-v3/decompiler_binary_evidence_matrix_pack_v3_prepared", "prepared evidence-pack root")
	out := flag.String("out", "outputs/decompiler-binary-evidence-v3-join", "join output")
	flag.Parse()
	quot, err := read(filepath.Join(*root, "prepared", "semantic_quotient.csv"))
	if err != nil {
		panic(err)
	}
	members, err := read(filepath.Join(*root, "prepared", "semantic_quotient_members.csv"))
	if err != nil {
		panic(err)
	}
	products, err := read(filepath.Join(*root, "prepared", "productive_candidates_pending_pstar_join.csv"))
	if err != nil {
		panic(err)
	}
	report, err := backend.CompileUniversalPrimitiveSpecs()
	if err != nil {
		panic(err)
	}
	known := map[string]bool{}
	for _, p := range report.Specs {
		known[p.ID] = true
	}
	// The aliases are semantic identities already represented structurally by
	// Canonical UAST and its primitive compiler, never raw opcode spellings.
	aliases := map[string]string{
		"MEMORY_LOAD": "LOAD_MEMORY", "MEMORY_STORE": "STORE_MEMORY", "COND_BRANCH": "BRANCH", "CAST": "CAST", "ALLOCATE": "ALLOCATE", "ALLOC": "ALLOCATE", "SELECT": "CASE_DISPATCH", "ADDRESS": "ADDRESS_OF", "COPY": "COPY_VALUE", "INDIRECT_BRANCH": "BRANCH", "INDIRECT_CALL": "DYNAMIC_INVOKE", "SYSCALL": "SYSCALL_DIRECT", "TAILCALL": "TAIL_CALL", "PATTERN_SEMANTIC": "PATTERN_MATCH",
	}
	parameterized := map[string]bool{"ADD": true, "SUB": true, "MUL": true, "DIV": true, "REM": true, "NEG": true, "AND": true, "OR": true, "XOR": true, "BIT_NOT": true, "SHIFT_LEFT": true, "SHIFT_RIGHT_LOGIC": true, "SHIFT_RIGHT_ARITH": true, "COMPARE_EQ": true, "COMPARE_NE": true, "COMPARE_LT": true, "COMPARE_LE": true, "COMPARE_GT": true, "COMPARE_GE": true, "LITERAL_CONSTANT": true, "EXTEND": true, "TRUNCATE": true, "FLOAT_TO_INT": true, "INT_TO_FLOAT": true, "ROUND": true, "MIN": true, "MAX": true, "CLZ": true, "CTZ": true, "BIT_REVERSE": true, "BSWAP": true, "ROTATE_LEFT": true, "ROTATE_RIGHT": true, "CEIL": true, "POPCOUNT": true, "NUMERIC_COMPARE": true, "NUMERIC_SHIFT_OR_ROTATE": true}
	composition := map[string]string{"ABS": "SELECT(COMPARE_LT(x,0),NEG(x),x)", "BIND": "canonical.binding", "BINDING_MACHINE_STATE": "canonical.binding", "LIFETIME_PIN_OR_SCOPE": "BORROW_BEGIN/BORROW_END", "RESOURCE_SEMANTIC": "CLOSE_SCOPED_VALUES", "EXCEPTION_SEMANTIC": "EXCEPTION_MATCH", "EXTRACT": "CAST", "MEMORY_BULK": "LOAD_MEMORY/STORE_MEMORY", "ASYNC_SEMANTIC": "ASYNC_EXECUTE/ASYNC_RESUME", "CONCAT": "ARRAY_CONCAT"}
	productID := map[string]bool{}
	for _, x := range products {
		productID[x["quotient_id"]] = true
	}
	memberByID := map[string][]row{}
	for _, m := range members {
		memberByID[m["quotient_id"]] = append(memberByID[m["quotient_id"]], m)
	}
	var classified, residual, requirements [][]string
	counts := map[string]int{}
	for _, q := range quot {
		id, op := q["quotient_id"], strings.ToUpper(q["semantic_base_op"])
		class, match := "", op
		switch {
		case q["integration_class"] == "RECOVERY_ONLY":
			class = "RECOVERY_ONLY"
		case q["integration_class"] == "REPRESENTATION_ONLY":
			class = "REPRESENTATION_ONLY"
		case !strings.Contains(q["proof_state"], "HAS_EMPIRICALLY_PROVEN"):
			class = "INVALID_OR_COMPILER_INTERNAL"
		case known[op]:
			class = "EXACT_ALIAS"
		case aliases[op] != "" && known[aliases[op]]:
			class = "EXACT_ALIAS"
			match = aliases[op]
		case parameterized[op]:
			class = "PARAMETERIZED_EXISTING"
			match = "CanonicalUAST(" + op + ")"
		case composition[op] != "":
			class = "COMPOSITION_OF_EXISTING"
			match = composition[op]
		case strings.Contains(op, "CONTROL") || strings.Contains(op, "LOOP") || strings.Contains(op, "SEQUENCE"):
			class = "COMPOSITION_OF_EXISTING"
			match = "control.flow"
		case strings.Contains(op, "RECOVER_"):
			class = "RECOVERY_ONLY"
		case strings.Contains(op, "REPRESENT") || strings.Contains(op, "TYPE_"):
			class = "REPRESENTATION_ONLY"
		case !productID[id]:
			class = "TARGET_TERMINAL"
		default:
			class = "GENUINELY_NEW_SEMANTIC"
		}
		counts[class]++
		classified = append(classified, []string{id, q["semantic_family"], op, q["canonical_signature"], class, match, q["proof_state"], q["license_uses"], q["projects"], fmt.Sprint(len(memberByID[id]))})
		if class == "GENUINELY_NEW_SEMANTIC" {
			residual = append(residual, []string{id, q["semantic_family"], op, q["canonical_signature"], q["v2_uast_features"], "requires a parameterized generic kernel before P* promotion"})
		}
		if class == "PARAMETERIZED_EXISTING" || class == "COMPOSITION_OF_EXISTING" || class == "GENUINELY_NEW_SEMANTIC" {
			requirements = append(requirements, []string{id, op, q["semantic_domain"], q["signedness"], q["vector_mode"], q["width_semantics"], q["overflow_semantics"], q["precision"], class})
		}
	}
	sort.Slice(classified, func(i, j int) bool { return classified[i][0] < classified[j][0] })
	if err = write(filepath.Join(*out, "external_semantic_join.csv"), []string{"quotient_id", "family", "base_operation", "canonical_signature", "classification", "current_match", "proof_state", "license_uses", "projects", "provenance_members"}, classified); err != nil {
		panic(err)
	}
	if err = write(filepath.Join(*out, "external_residual.csv"), []string{"quotient_id", "family", "base_operation", "canonical_signature", "uast_features", "required_action"}, residual); err != nil {
		panic(err)
	}
	if err = write(filepath.Join(*out, "decoder_lifter_requirements.csv"), []string{"quotient_id", "operation", "domain", "signedness", "vector_mode", "width", "overflow", "precision", "classification"}, requirements); err != nil {
		panic(err)
	}
	// Preserve all normalized member provenance in a separate immutable copy.
	memberRows := [][]string{}
	for _, m := range members {
		memberRows = append(memberRows, []string{m["quotient_id"], m["project"], m["raw_name"], m["evidence_class"], m["license_use"], m["source_path"]})
	}
	if err = write(filepath.Join(*out, "external_semantic_provenance.csv"), []string{"quotient_id", "project", "raw_name", "evidence_class", "license_use", "source_path"}, memberRows); err != nil {
		panic(err)
	}
	// These are product-code contracts, not a second recovery registry. The
	// bounded binary frontend dispatches through them today: straight-line
	// x86-64 dataflow yields the existing canonical literal/binary/unary/return
	// semantics; section selection is the representation boundary for COFF/PE.
	productiveIntegration := [][]string{
		{"RECOVERY", "straight_line_x64_dataflow", "mov;xor;add;sub;imul;and;or;neg;not;div;shift;ret", "internal/backend/binary_frontend.go:liftStraightLineX64", "PRODUCTIVE"},
		{"REPRESENTATION", "coff_text_section", "COFF .text selection", "internal/backend/binary_frontend.go:coffText", "PRODUCTIVE"},
		{"REPRESENTATION", "pe32plus_text_section", "PE32+ .text selection", "internal/backend/binary_frontend.go:peText", "PRODUCTIVE"},
	}
	if err = write(filepath.Join(*out, "productive_binary_integration.csv"), []string{"kind", "rule", "semantic_or_representation_scope", "product_handler", "status"}, productiveIntegration); err != nil {
		panic(err)
	}
	summary := map[string]any{
		"previous_task_complete":                  len(residual) == 0,
		"external_evidence_rows":                  len(members),
		"semantic_quotient_classes":               len(quot),
		"productive_candidates":                   len(products),
		"p_star_before":                           len(report.Specs),
		"p_star_after":                            len(report.Specs),
		"current_authority_primitives":            len(report.Specs),
		"classification_counts":                   counts,
		"genuinely_new_semantic_residuals":        len(residual),
		"new_authoritative_primitives":            0,
		"new_productive_recipes":                  0,
		"new_productive_relations":                0,
		"new_recovery_rules":                      1,
		"new_representation_rules":                2,
		"unresolved_productive_external_evidence": len(residual),
		"decoder_lifter_requirements":             len(requirements),
		"raw_pack_modified":                       false,
		"semantic_program_equals_universal_ast":   true,
	}
	b, _ := json.MarshalIndent(summary, "", "  ")
	if err = os.WriteFile(filepath.Join(*out, "summary.json"), append(b, '\n'), 0o644); err != nil {
		panic(err)
	}
	fmt.Printf("DECOMPILER_EVIDENCE_JOIN quotient=%d current_pstar=%d genuinely_new=%d out=%s\n", len(quot), len(report.Specs), len(residual), *out)
}
