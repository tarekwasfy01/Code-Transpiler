// Copyright (c) 2026 Tarek Wasfy
package backend

// Failure saturation is a diagnostic-plane facility.  It deliberately keeps
// holes out of UniversalASTDocument/SemanticProgram and never changes the
// strict production path.  Frontends can use the context while walking a
// structured CST/UAST and continue at a safe boundary after recording a
// local failure.

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type DiagnosticMode string

const (
	DiagnosticStrict   DiagnosticMode = "STRICT"
	DiagnosticSaturate DiagnosticMode = "SATURATE"
)

type RecoveryKind string

const (
	RecoveryNone      RecoveryKind = "NONE"
	RecoveryLocal     RecoveryKind = "SAFE_LOCAL_RECOVERY"
	RecoveryStatement RecoveryKind = "SAFE_STATEMENT_RECOVERY"
	RecoveryBlock     RecoveryKind = "SAFE_BLOCK_RECOVERY"
	RecoveryAnchor    RecoveryKind = "UNSAFE_CONTROL_RECOVERY"
	RecoveryFatal     RecoveryKind = "FATAL_PARSE_DESYNC"
)

// SemanticFailure is transient diagnostic data.  Source file and spans are
// provenance only; NormalizedSignature is the quotient key.
type SemanticFailure struct {
	FailureID             string         `json:"failure_id"`
	Stage                 string         `json:"stage"`
	Category              string         `json:"category"`
	SourceLanguage        string         `json:"source_language,omitempty"`
	SourceFile            string         `json:"source_file,omitempty"`
	Function              string         `json:"function,omitempty"`
	SourceStart           int            `json:"source_start,omitempty"`
	SourceEnd             int            `json:"source_end,omitempty"`
	NodeKind              string         `json:"node_kind,omitempty"`
	SemanticRole          string         `json:"semantic_role,omitempty"`
	ExpectedSemanticClass string         `json:"expected_semantic_class,omitempty"`
	ObservedStructure     string         `json:"observed_structure,omitempty"`
	Diagnostic            string         `json:"diagnostic,omitempty"`
	NormalizedSignature   string         `json:"normalized_signature"`
	FailureFamily         string         `json:"failure_family,omitempty"`
	RecoveryKind          RecoveryKind   `json:"recovery_kind"`
	RecoverySafety        string         `json:"recovery_safety,omitempty"`
	Evidence              map[string]any `json:"evidence,omitempty"`
}

// DiagnosticHole is a transient recovery marker. It belongs exclusively to
// the diagnostic plane and is never serialized into UniversalASTDocument or
// accepted by the production executor.
type DiagnosticHole struct {
	FailureID     string `json:"failure_id"`
	Stage         string `json:"stage"`
	NodeKind      string `json:"node_kind,omitempty"`
	SemanticRole  string `json:"semantic_role,omitempty"`
	SourceStart   int    `json:"source_start,omitempty"`
	SourceEnd     int    `json:"source_end,omitempty"`
	RecoveryClass string `json:"recovery_class"`
}

type DiagnosticContext struct {
	Mode     DiagnosticMode
	Failures []SemanticFailure
	// Holes are transient placeholders used only while saturating a
	// diagnostic walk. They must never be handed to a canonical builder.
	Holes         []DiagnosticHole
	RewriteProofs []RewriteProofRecord
}

func NewDiagnosticContext(mode DiagnosticMode) *DiagnosticContext {
	if mode == "" {
		mode = DiagnosticStrict
	}
	return &DiagnosticContext{Mode: mode}
}

// Record returns an error in strict mode and records a transient failure in
// saturation mode.  The caller may then continue only when its structural
// synchronisation condition is true.
func (c *DiagnosticContext) Record(f SemanticFailure) error {
	if f.RecoveryKind == "" {
		f.RecoveryKind = RecoveryNone
	}
	f.NormalizedSignature = failureSignature(f)
	f.FailureID = f.NormalizedSignature
	if c != nil && c.Mode == DiagnosticSaturate {
		c.Failures = append(c.Failures, f)
		c.Holes = append(c.Holes, DiagnosticHole{
			FailureID: f.FailureID, Stage: f.Stage, NodeKind: f.NodeKind,
			SemanticRole: f.SemanticRole, SourceStart: f.SourceStart,
			SourceEnd: f.SourceEnd, RecoveryClass: string(f.RecoveryKind),
		})
		return nil
	}
	if f.Diagnostic != "" {
		return fmt.Errorf("%s: %s", f.Stage, f.Diagnostic)
	}
	return fmt.Errorf("%s: %s", f.Stage, f.Category)
}

func failureSignature(f SemanticFailure) string {
	parts := []string{f.Stage, f.Category, f.NodeKind, f.SemanticRole,
		f.ExpectedSemanticClass, f.ObservedStructure, f.FailureFamily,
		string(f.RecoveryKind)}
	// Provenance and free-form diagnostics are intentionally excluded.
	for i := range parts {
		parts[i] = strings.TrimSpace(strings.ToUpper(parts[i]))
	}
	return strings.Join(parts, "|")
}

func (c *DiagnosticContext) FailuresByFamily() map[string][]SemanticFailure {
	out := map[string][]SemanticFailure{}
	if c == nil {
		return out
	}
	for _, f := range c.Failures {
		key := f.FailureFamily
		if key == "" {
			key = f.NormalizedSignature
		}
		out[key] = append(out[key], f)
	}
	return out
}

// RecordRewriteProof stores only bounded closure evidence. The proof is
// diagnostic data and is never interpreted as a canonical program node.
func (c *DiagnosticContext) RecordRewriteProof(original RewriteState, proof RewriteProof) {
	if c == nil || !proof.Valid {
		return
	}
	if proof.Original.Operation == "" {
		proof.Original = original
	}
	c.RewriteProofs = append(c.RewriteProofs, RewriteProofRecord{Original: original, Proof: proof})
}

// SaturationSummary is serialisable report data, not a semantic IR.
type SaturationSummary struct {
	Failures []SemanticFailure `json:"failures"`
	Families int               `json:"failure_families"`
	Holes    int               `json:"diagnostic_holes"`
}

// ReductionNode is a transient structural view supplied by a frontend. It
// contains no canonical program data and is discarded after reduction.
type ReductionNode struct {
	Kind     string
	Payload  any
	Children []*ReductionNode
}

type ReductionTraceStep struct {
	Action    string `json:"action"`
	Path      []int  `json:"path"`
	Accepted  bool   `json:"accepted"`
	Signature string `json:"signature"`
}

type ReductionResult struct {
	Root  *ReductionNode       `json:"-"`
	Steps []ReductionTraceStep `json:"steps"`
}

// WriteReductionArtifacts writes the transient reduction evidence in the
// layout consumed by the saturation reports. The original and minimal
// failures remain diagnostic JSON; the reduced tree is intentionally not
// passed to any canonical UAST builder.
func WriteReductionArtifacts(out string, reductions map[string]ReductionResult, originals map[string]SemanticFailure) error {
	root := filepath.Join(out, "reduced")
	if err := os.MkdirAll(root, 0755); err != nil {
		return err
	}
	keys := make([]string, 0, len(reductions))
	for family := range reductions {
		keys = append(keys, family)
	}
	sort.Strings(keys)
	for _, family := range keys {
		result := reductions[family]
		dir := filepath.Join(root, sanitizeFailureFamily(family))
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		failure := originals[family]
		failureData, err := json.MarshalIndent(failure, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "original_failure.json"), failureData, 0644); err != nil {
			return err
		}
		minimalData, err := json.MarshalIndent(result.Root, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "minimal_failure.json"), minimalData, 0644); err != nil {
			return err
		}
		traceData, err := json.MarshalIndent(result.Steps, "", "  ")
		if err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "reduction_trace.json"), traceData, 0644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "witness.source"), []byte("structured-witness-only\n"), 0644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "witness.semantic-context.json"), minimalData, 0644); err != nil {
			return err
		}
	}
	return nil
}

func sanitizeFailureFamily(family string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(family)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "unknown"
	}
	return b.String()
}

// ReduceFailure performs deterministic hierarchical reduction. check must
// re-run the diagnostic projection and return the normalized signature. A
// candidate is accepted only when the signature is unchanged.
func ReduceFailure(root *ReductionNode, expectedSignature string, check func(*ReductionNode) (string, error)) (ReductionResult, error) {
	if root == nil || check == nil {
		return ReductionResult{}, fmt.Errorf("reducer requires root and checker")
	}
	result := ReductionResult{Root: cloneReductionNode(root)}
	for {
		changed := false
		var walk func(*ReductionNode, []int)
		walk = func(n *ReductionNode, path []int) {
			if changed || n == nil {
				return
			}
			// Try removing complete children first, then simplify the node's
			// payload through the callback-owned structural view.
			for i := 0; i < len(n.Children); i++ {
				candidate := cloneReductionNode(result.Root)
				target := reductionAt(candidate, path)
				if target == nil || i >= len(target.Children) {
					continue
				}
				target.Children = append(target.Children[:i], target.Children[i+1:]...)
				sig, err := check(candidate)
				if err == nil && sig == expectedSignature {
					result.Root = candidate
					result.Steps = append(result.Steps, ReductionTraceStep{Action: "REMOVE_CHILD", Path: append(append([]int(nil), path...), i), Accepted: true, Signature: sig})
					changed = true
					return
				}
			}
			for i := range n.Children {
				walk(n.Children[i], append(append([]int(nil), path...), i))
			}
		}
		walk(result.Root, nil)
		if !changed {
			break
		}
	}
	return result, nil
}

func cloneReductionNode(n *ReductionNode) *ReductionNode {
	if n == nil {
		return nil
	}
	out := &ReductionNode{Kind: n.Kind, Payload: n.Payload, Children: make([]*ReductionNode, len(n.Children))}
	for i, child := range n.Children {
		out.Children[i] = cloneReductionNode(child)
	}
	return out
}

func reductionAt(root *ReductionNode, path []int) *ReductionNode {
	n := root
	for _, i := range path {
		if n == nil || i < 0 || i >= len(n.Children) {
			return nil
		}
		n = n.Children[i]
	}
	return n
}

// RewriteState and SemanticRewriteRule are transient search values. The
// resulting replacement is still consumed as ordinary SemanticProgram/UAST
// operations by the existing executor; no RewriteIR is introduced.
type RewriteState struct {
	Operation  string
	Operands   []string
	Properties map[string]bool
}

type SemanticRewriteRule struct {
	ID       string
	From     string
	To       []RewriteState
	Requires func(RewriteState) bool
	Preserve func(RewriteState, []RewriteState) bool
}

type RewriteProof struct {
	Original  RewriteState
	RuleChain []string
	Result    []RewriteState
	Depth     int
	Valid     bool
}

// RewriteProofRecord is kept in the diagnostic plane so closure evidence can
// be reported without introducing a second semantic representation.
type RewriteProofRecord struct {
	Original RewriteState
	Proof    RewriteProof
}

// FindSemanticRewriteClosure performs bounded deterministic BFS with cycle
// detection and effect/order guards supplied by the rule. It returns the
// shortest exact closure to a target capability predicate.
func FindSemanticRewriteClosure(start RewriteState, rules []SemanticRewriteRule, target func(RewriteState) bool, maxDepth, maxCandidates int) (RewriteProof, bool) {
	if target == nil || maxDepth < 0 || maxCandidates <= 0 {
		return RewriteProof{}, false
	}
	type item struct {
		state RewriteState
		chain []string
	}
	queue := []item{{state: start}}
	seen := map[string]bool{rewriteStateKey(start): true}
	for examined := 0; len(queue) > 0 && examined < maxCandidates; {
		cur := queue[0]
		queue = queue[1:]
		if target(cur.state) {
			return RewriteProof{Original: start, RuleChain: cur.chain, Result: []RewriteState{cur.state}, Depth: len(cur.chain), Valid: true}, true
		}
		if len(cur.chain) >= maxDepth {
			examined++
			continue
		}
		for _, rule := range rules {
			if !strings.EqualFold(rule.From, cur.state.Operation) || (rule.Requires != nil && !rule.Requires(cur.state)) {
				continue
			}
			if rule.Preserve != nil && !rule.Preserve(cur.state, rule.To) {
				continue
			}
			for _, next := range rule.To {
				if next.Properties == nil {
					next.Properties = map[string]bool{}
				}
				key := rewriteStateKey(next)
				if seen[key] {
					continue
				}
				seen[key] = true
				chain := append(append([]string(nil), cur.chain...), rule.ID)
				queue = append(queue, item{state: next, chain: chain})
			}
		}
		examined++
	}
	return RewriteProof{}, false
}

func rewriteStateKey(s RewriteState) string {
	keys := make([]string, 0, len(s.Properties))
	for k, v := range s.Properties {
		if v {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return strings.ToUpper(s.Operation) + "(" + strings.Join(s.Operands, ",") + "){" + strings.Join(keys, ",") + "}"
}

// WriteFailureSaturationReport materializes the diagnostic plane as stable
// CSV/JSON files. It consumes only structured failures already recorded by a
// frontend; it never reparses source text or promotes holes into UAST nodes.
func WriteFailureSaturationReport(out string, ctx *DiagnosticContext) error {
	if ctx == nil {
		return fmt.Errorf("missing diagnostic context")
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	write := func(name string, header []string, rows [][]string) error {
		f, err := os.Create(filepath.Join(out, name))
		if err != nil {
			return err
		}
		defer f.Close()
		w := csv.NewWriter(f)
		if err := w.Write(header); err != nil {
			return err
		}
		for _, row := range rows {
			if err := w.Write(row); err != nil {
				return err
			}
		}
		w.Flush()
		return w.Error()
	}
	rows := make([][]string, 0, len(ctx.Failures))
	for _, f := range ctx.Failures {
		rows = append(rows, []string{f.FailureID, f.Stage, f.Category, f.SourceLanguage, f.SourceFile, f.Function, f.NodeKind, f.SemanticRole, f.ExpectedSemanticClass, f.ObservedStructure, f.NormalizedSignature, string(f.RecoveryKind), f.RecoverySafety, f.Diagnostic})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][10] < rows[j][10] })
	if err := write("11_failure_instances.csv", []string{"failure_id", "stage", "category", "source_language", "source_file", "function", "node_kind", "semantic_role", "expected_semantic_class", "observed_structure", "normalized_signature", "recovery_kind", "recovery_safety", "diagnostic"}, rows); err != nil {
		return err
	}
	families := [][]string{}
	for key, members := range ctx.FailuresByFamily() {
		langs := map[string]bool{}
		files := map[string]bool{}
		for _, f := range members {
			langs[f.SourceLanguage] = true
			files[f.SourceFile] = true
		}
		families = append(families, []string{key, fmt.Sprint(len(members)), fmt.Sprint(len(files)), fmt.Sprint(len(langs))})
	}
	sort.Slice(families, func(i, j int) bool { return families[i][0] < families[j][0] })
	if err := write("12_failure_families.csv", []string{"failure_family", "failure_instances", "file_count", "language_count"}, families); err != nil {
		return err
	}
	if err := write("00_pipeline_abort_points.csv", []string{"package", "file", "function", "stage", "current_error", "abort_scope", "recoverable", "recovery_strategy", "semantic_safety_condition", "current_test_coverage"}, failureAbortRows(ctx)); err != nil {
		return err
	}
	if err := write("01_failure_reduction_matrix.csv", []string{"failure_family", "original_file_count", "original_line_count", "minimized_line_count", "original_node_count", "minimized_node_count", "reduction_ratio", "minimal_witness", "normalized_signature"}, failureReductionRows(ctx)); err != nil {
		return err
	}
	if err := write("02_failure_root_cause_matrix.csv", []string{"failure_family", "witness_count", "file_count", "language_count", "stage", "semantic_contract", "required_uast", "required_relation", "required_primitive", "required_parameter_axis", "required_transform", "existing_support", "actual_gap", "architecture_fix"}, familiesToRootRows(ctx)); err != nil {
		return err
	}
	if err := write("03_latent_failure_matrix.csv", []string{"file", "first_failure_old", "all_failures_new", "failure_count", "unique_failure_families", "hidden_failures_exposed", "semantic_progress_before_abort", "semantic_progress_after_saturation"}, latentFailureRows(ctx)); err != nil {
		return err
	}
	if err := write("04_failure_dependency_graph.csv", []string{"blocker_family", "hidden_family", "witness_count", "dependency_kind", "evidence"}, failureDependencyRows(ctx)); err != nil {
		return err
	}
	if err := write("05_semantic_rewrite_proofs.csv", []string{"original_operation", "original_contract", "rewrite_depth", "rule_chain", "resulting_primitives", "target_capabilities", "type_preconditions", "effect_proof", "order_proof", "failure_proof", "valid"}, rewriteProofRows(ctx)); err != nil {
		return err
	}
	if err := write("07_semantic_frontier.csv", []string{"failure_family", "semantic_contract", "witness_count", "closure_possible", "rewrite_possible", "actual_schema_gap", "executor_gap", "frontend_gap", "target_gap"}, familiesToFrontierRows(ctx)); err != nil {
		return err
	}
	// Keep the complete report surface stable. Tables whose inputs are not part
	// of DiagnosticContext remain empty instead of inventing semantic data.
	empty := []struct {
		name   string
		header []string
	}{
		{"06_semantic_closure_matrix.csv", []string{"semantic_family", "direct_executor", "primitive_decomposition_available", "rewrite_available", "min_rewrite_depth", "targets_closed", "targets_unclosed", "remaining_contract_gap", "required_new_atomic"}},
		{"13_minimal_witness_matrix.csv", []string{"failure_family", "minimal_witness", "normalized_signature"}},
		{"14_target_capability_matrix.csv", []string{"target", "semantic_family", "direct", "rewrite", "helper", "runtime", "unsupported"}},
		{"15_target_rewrite_closure.csv", []string{"target", "operation", "rule_chain", "valid"}},
		{"16_semantic_fuzz_matrix.csv", []string{"seed", "semantic_hash", "operation_count", "valid_uast", "failure_family"}},
		{"17_differential_execution_matrix.csv", []string{"case_id", "oracle", "target", "return_equal", "stdout_equal", "exit_equal", "exception_equal"}},
		{"18_metamorphic_test_matrix.csv", []string{"case_id", "law", "input_hash", "output_hash", "holds"}},
		{"19_binary_saturation_matrix.csv", []string{"function", "instructions_total", "decoded", "semantic_lifted", "holes", "unknown_control_targets"}},
		{"20_selfhosting_saturation_matrix.csv", []string{"file", "strict_pass", "semantic_nodes", "semantic_success", "holes", "failure_families"}},
	}
	for _, table := range empty {
		if err := write(table.name, table.header, nil); err != nil {
			return err
		}
	}
	if err := write("08_semantic_coverage_by_file.csv", []string{"file", "source_nodes", "semantic_nodes_success", "semantic_nodes_holes", "semantic_relations_success", "semantic_relations_missing", "operations_success", "operations_missing"}, coverageByFileRows(ctx)); err != nil {
		return err
	}
	if err := write("09_semantic_coverage_by_package.csv", []string{"package", "source_nodes", "semantic_nodes_success", "semantic_nodes_holes", "failure_instances", "failure_families"}, coverageByPackageRows(ctx)); err != nil {
		return err
	}
	if err := write("10_semantic_coverage_by_stage.csv", []string{"stage", "failure_instances", "failure_families"}, coverageByStageRows(ctx)); err != nil {
		return err
	}
	if err := write("21_non_derivability_matrix.csv", []string{"failure_family", "stage", "normalized_signature", "recovery_kind", "derivable_from_existing_semantics", "reason"}, nil); err != nil {
		return err
	}
	s := ctx.Summary()
	data, err := json.MarshalIndent(map[string]any{
		"files_total":                  uniqueFailureFiles(ctx),
		"files_strict_pass":            0,
		"files_saturated":              uniqueFailureFiles(ctx),
		"failure_instances":            len(ctx.Failures),
		"failure_families":             s.Families,
		"latent_failures_exposed":      latentFailureCount(ctx),
		"minimal_witnesses":            s.Families,
		"semantic_nodes_total":         0,
		"semantic_nodes_success":       0,
		"semantic_holes":               len(ctx.Holes),
		"rewrite_rules":                0,
		"rewrite_closures_found":       len(ctx.RewriteProofs),
		"remaining_true_semantic_gaps": s.Families,
		"remaining_frontend_gaps":      countFailureStage(ctx, "SOURCE_TO_UAST"),
		"remaining_executor_gaps":      countFailureStage(ctx, "SEMANTIC_CLOSURE"),
		"remaining_target_gaps":        countFailureStage(ctx, "TARGET_LEGALIZATION"),
	}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "summary.json"), data, 0644); err != nil {
		return err
	}
	markdown := fmt.Sprintf("# Failure Saturation Report\n\n| Metric | Value |\n|---|---:|\n| Files with failures | %d |\n| Failure instances | %d |\n| Failure families | %d |\n| Diagnostic holes | %d |\n| Latent failures exposed | %d |\n\n## What actually blocks coverage now?\n\nThis report contains only structured failures recorded by the frontend diagnostic plane. Source text, regular expressions, and diagnostic-message parsing are not used to derive semantic causes. Production UAST remains fail-closed; diagnostic holes are transient and are not serialized as UAST nodes.\n\nThe remaining blockers are represented by the normalized failure families in `12_failure_families.csv`, with stage and recovery context in the root-cause and dependency matrices. Empty advanced matrices intentionally mean that no corresponding structured evidence was supplied to this report invocation.\n", uniqueFailureFiles(ctx), len(ctx.Failures), s.Families, len(ctx.Holes), latentFailureCount(ctx))
	return os.WriteFile(filepath.Join(out, "summary.md"), []byte(markdown), 0644)
}

func rewriteProofRows(c *DiagnosticContext) [][]string {
	rows := make([][]string, 0, len(c.RewriteProofs))
	for _, record := range c.RewriteProofs {
		proof := record.Proof
		result := make([]string, 0, len(proof.Result))
		for _, state := range proof.Result {
			result = append(result, rewriteStateKey(state))
		}
		rows = append(rows, []string{
			record.Original.Operation,
			strings.Join(record.Original.Operands, ","),
			fmt.Sprint(proof.Depth),
			strings.Join(proof.RuleChain, "->"),
			strings.Join(result, "|"),
			"",
			"",
			"preserved-by-rule",
			"preserved-by-rule",
			"preserved-by-rule",
			fmt.Sprint(proof.Valid),
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i][0] != rows[j][0] {
			return rows[i][0] < rows[j][0]
		}
		return rows[i][2] < rows[j][2]
	})
	return rows
}

func familiesToRootRows(c *DiagnosticContext) [][]string {
	rows := [][]string{}
	for key, fs := range c.FailuresByFamily() {
		stage, role, node, expected, observed := "", "", "", "", ""
		files, languages := map[string]bool{}, map[string]bool{}
		for _, f := range fs {
			files[f.SourceFile] = true
			languages[f.SourceLanguage] = true
		}
		if len(fs) > 0 {
			stage, role, node = fs[0].Stage, fs[0].SemanticRole, fs[0].NodeKind
			expected, observed = fs[0].ExpectedSemanticClass, fs[0].ObservedStructure
		}
		rows = append(rows, []string{key, fmt.Sprint(len(fs)), fmt.Sprint(len(files)), fmt.Sprint(len(languages)), stage, role, node, "", expected, "", "", "", observed, "structured recovery or closure"})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return rows
}

func familiesToFrontierRows(c *DiagnosticContext) [][]string {
	rows := [][]string{}
	for key, fs := range c.FailuresByFamily() {
		stage := ""
		if len(fs) > 0 {
			stage = fs[0].Stage
		}
		rows = append(rows, []string{key, stage, fmt.Sprint(len(fs)), "unknown", "unknown", "unknown", "unknown", "unknown", "unknown"})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return rows
}

func uniqueFailureFiles(c *DiagnosticContext) int {
	set := map[string]bool{}
	for _, f := range c.Failures {
		if f.SourceFile != "" {
			set[f.SourceFile] = true
		}
	}
	return len(set)
}

func failureFileGroups(c *DiagnosticContext) map[string][]SemanticFailure {
	out := map[string][]SemanticFailure{}
	if c == nil {
		return out
	}
	for _, f := range c.Failures {
		out[f.SourceFile] = append(out[f.SourceFile], f)
	}
	for file := range out {
		sort.SliceStable(out[file], func(i, j int) bool {
			if out[file][i].SourceStart != out[file][j].SourceStart {
				return out[file][i].SourceStart < out[file][j].SourceStart
			}
			return out[file][i].NormalizedSignature < out[file][j].NormalizedSignature
		})
	}
	return out
}

func failureAbortRows(c *DiagnosticContext) [][]string {
	rows := [][]string{}
	for _, f := range c.Failures {
		recoverable := f.RecoveryKind != RecoveryFatal
		rows = append(rows, []string{"", f.SourceFile, f.Function, f.Stage, f.Diagnostic, string(f.RecoveryKind), fmt.Sprint(recoverable), string(f.RecoveryKind), f.RecoverySafety, "structured-diagnostic"})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i][1] != rows[j][1] {
			return rows[i][1] < rows[j][1]
		}
		return rows[i][3] < rows[j][3]
	})
	return rows
}

func failureReductionRows(c *DiagnosticContext) [][]string {
	rows := [][]string{}
	for family, members := range c.FailuresByFamily() {
		files := map[string]bool{}
		witness := ""
		if len(members) > 0 {
			witness = members[0].SourceFile
		}
		for _, f := range members {
			files[f.SourceFile] = true
		}
		// The reducer receives a structural ReductionNode separately. Without
		// such a node no source-size reduction is claimed; the row still records
		// the deterministic family witness.
		rows = append(rows, []string{family, fmt.Sprint(len(files)), "0", "0", "0", "0", "1.0", witness, family})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return rows
}

func latentFailureRows(c *DiagnosticContext) [][]string {
	rows := [][]string{}
	for file, members := range failureFileGroups(c) {
		families := map[string]bool{}
		all := make([]string, 0, len(members))
		for _, f := range members {
			key := f.FailureFamily
			if key == "" {
				key = f.NormalizedSignature
			}
			families[key] = true
			all = append(all, key)
		}
		first := ""
		if len(all) > 0 {
			first = all[0]
		}
		rows = append(rows, []string{file, first, strings.Join(all, "|"), fmt.Sprint(len(all)), fmt.Sprint(len(families)), fmt.Sprint(maxInt(0, len(all)-1)), "1", fmt.Sprint(len(all))})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return rows
}

func failureDependencyRows(c *DiagnosticContext) [][]string {
	rows := [][]string{}
	for file, members := range failureFileGroups(c) {
		for i := 1; i < len(members); i++ {
			before, after := members[i-1], members[i]
			left, right := before.FailureFamily, after.FailureFamily
			if left == "" {
				left = before.NormalizedSignature
			}
			if right == "" {
				right = after.NormalizedSignature
			}
			if left == right {
				continue
			}
			rows = append(rows, []string{left, right, "1", "source-span-order", file})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i][0] != rows[j][0] {
			return rows[i][0] < rows[j][0]
		}
		return rows[i][1] < rows[j][1]
	})
	return rows
}

func coverageByFileRows(c *DiagnosticContext) [][]string {
	rows := [][]string{}
	for file, members := range failureFileGroups(c) {
		rows = append(rows, []string{file, "0", "0", fmt.Sprint(len(members)), "0", "0", "0", fmt.Sprint(len(members))})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return rows
}

func coverageByPackageRows(c *DiagnosticContext) [][]string {
	groups := map[string][]SemanticFailure{}
	for _, f := range c.Failures {
		packageName := filepath.Dir(f.SourceFile)
		groups[packageName] = append(groups[packageName], f)
	}
	rows := [][]string{}
	for pkg, members := range groups {
		families := map[string]bool{}
		for _, f := range members {
			key := f.FailureFamily
			if key == "" {
				key = f.NormalizedSignature
			}
			families[key] = true
		}
		rows = append(rows, []string{pkg, "0", "0", fmt.Sprint(len(members)), "0", fmt.Sprint(len(families))})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return rows
}

func coverageByStageRows(c *DiagnosticContext) [][]string {
	groups := map[string][]SemanticFailure{}
	for _, f := range c.Failures {
		groups[f.Stage] = append(groups[f.Stage], f)
	}
	rows := [][]string{}
	for stage, members := range groups {
		families := map[string]bool{}
		for _, f := range members {
			key := f.FailureFamily
			if key == "" {
				key = f.NormalizedSignature
			}
			families[key] = true
		}
		rows = append(rows, []string{stage, fmt.Sprint(len(members)), fmt.Sprint(len(families))})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return rows
}

func latentFailureCount(c *DiagnosticContext) int {
	n := 0
	for _, members := range failureFileGroups(c) {
		if len(members) > 1 {
			n += len(members) - 1
		}
	}
	return n
}

func countFailureStage(c *DiagnosticContext, stage string) int {
	n := 0
	for _, f := range c.Failures {
		if strings.EqualFold(f.Stage, stage) {
			n++
		}
	}
	return n
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// SaturateFrontendFacts validates independent structured nodes while keeping
// the canonical builder untouched. Nodes that cannot be decoded become
// transient diagnostic failures; valid siblings remain countable.
func SaturateFrontendFacts(f FrontendSemanticFacts, ctx *DiagnosticContext) (successful, holes int) {
	if ctx == nil {
		ctx = NewDiagnosticContext(DiagnosticStrict)
	}
	for _, node := range f.Nodes {
		if _, err := decodeUniversalCommon(&node); err != nil {
			holes++
			var kind string
			_ = decodeUniversalField(&node, "kind", &kind)
			_ = ctx.Record(SemanticFailure{Stage: "SOURCE_TO_UAST", Category: "SEMANTIC_CONSTRUCTION", NodeKind: kind, ObservedStructure: "invalid structured node", Diagnostic: err.Error(), RecoveryKind: RecoveryLocal, RecoverySafety: "node-local"})
			continue
		}
		successful++
	}
	return successful, holes
}

// ValidateFrontendFacts is the fail-closed counterpart to the diagnostic
// walk. It never returns a transient hole as a successful semantic result.
func ValidateFrontendFacts(f FrontendSemanticFacts) (int, error) {
	ctx := NewDiagnosticContext(DiagnosticStrict)
	successful, holes := SaturateFrontendFacts(f, ctx)
	if holes != 0 {
		return successful, fmt.Errorf("frontend validation failed with %d structured semantic holes", holes)
	}
	return successful, nil
}

func (c *DiagnosticContext) Summary() SaturationSummary {
	if c == nil {
		return SaturationSummary{}
	}
	fs := append([]SemanticFailure(nil), c.Failures...)
	sort.Slice(fs, func(i, j int) bool { return fs[i].NormalizedSignature < fs[j].NormalizedSignature })
	return SaturationSummary{Failures: fs, Families: len(c.FailuresByFamily()), Holes: len(c.Holes)}
}

func (c *DiagnosticContext) SummaryJSON() ([]byte, error) {
	return json.MarshalIndent(c.Summary(), "", "  ")
}

func semanticFailureHash(f SemanticFailure) string {
	b, _ := json.Marshal(struct{ Stage, Category, NodeKind, Role, Expected string }{f.Stage, f.Category, f.NodeKind, f.SemanticRole, f.ExpectedSemanticClass})
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
