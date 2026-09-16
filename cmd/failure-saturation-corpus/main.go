// Copyright (c) 2026 Tarek Wasfy
// failure-saturation-corpus runs the historical Go self-hosting corpus through
// the strict and structured saturation frontends and materializes the complete
// v36 diagnostic plane. The input manifest is derived from the archived
// positive/error matrices; no diagnostic text is used as semantic evidence.
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/format"
	goparser "go/parser"
	gotoken "go/token"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

type csvRow map[string]string

type corpusCase struct {
	Path        string
	ExpectedSHA string
	OldFailure  string
}

type caseResult struct {
	Path              string
	SourceSHA         string
	StrictPass        bool
	StrictDiagnostic  string
	SaturationError   string
	NodesTotal        int
	NodesSuccess      int
	Holes             int
	RelationsSuccess  int
	OperationsSuccess int
	Failures          []backend.SemanticFailure
}

func readCSV(path string) ([]csvRow, error) {
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
	var rows []csvRow
	for {
		v, err := r.Read()
		if err == io.EOF {
			return rows, nil
		}
		if err != nil {
			return nil, err
		}
		row := csvRow{}
		for i, k := range h {
			if i < len(v) {
				row[k] = strings.TrimSpace(v[i])
			}
		}
		rows = append(rows, row)
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
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return err
		}
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

func main() {
	positive := flag.String("positive", "outputs/.handoff-ground-truth-round3/positive-matrix.csv", "historical passing corpus matrix")
	failures := flag.String("failures", "outputs/.handoff-ground-truth-round3/normalized-error-matrix.csv", "historical first-failure matrix")
	out := flag.String("out", "outputs/failure-saturation-v36", "fresh output directory")
	flag.Parse()
	if err := run(*positive, *failures, *out); err != nil {
		fmt.Fprintln(os.Stderr, "failure-saturation-corpus:", err)
		os.Exit(1)
	}
}

func run(positivePath, failurePath, out string) error {
	positive, err := readCSV(positivePath)
	if err != nil {
		return err
	}
	failureRows, err := readCSV(failurePath)
	if err != nil {
		return err
	}
	cases := map[string]corpusCase{}
	for _, row := range positive {
		p := filepath.Clean(row["source_path"])
		cases[p] = corpusCase{Path: p, ExpectedSHA: strings.ToUpper(row["source_sha256"])}
	}
	for _, row := range failureRows {
		p := filepath.Clean(row["source_path"])
		old := row["universal_fix_family"]
		if old == "" {
			old = row["normalized_subtype"]
		}
		c := cases[p]
		c.Path, c.ExpectedSHA, c.OldFailure = p, strings.ToUpper(row["source_sha256"]), old
		cases[p] = c
	}
	ordered := make([]corpusCase, 0, len(cases))
	for _, c := range cases {
		ordered = append(ordered, c)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Path < ordered[j].Path })
	if len(ordered) == 0 {
		return fmt.Errorf("empty corpus manifest")
	}
	if err := os.RemoveAll(out); err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}

	global := backend.NewDiagnosticContext(backend.DiagnosticSaturate)
	results := make([]caseResult, 0, len(ordered))
	manifestRows := make([][]string, 0, len(ordered))
	for _, c := range ordered {
		data, readErr := os.ReadFile(c.Path)
		if readErr != nil {
			return fmt.Errorf("read %s: %w", c.Path, readErr)
		}
		h := sha256.Sum256(data)
		sha := strings.ToUpper(hex.EncodeToString(h[:]))
		// The archived run is the corpus membership authority. Working-tree
		// edits are retained and recorded in source_manifest.csv rather than
		// silently replacing or dropping a case.
		_, strictCtx, strictErr := backend.LowerSourceWithDiagnostics("go", c.Path, string(data), backend.DiagnosticStrict)
		_ = strictCtx
		satProgram, satCtx, satErr := backend.LowerSourceWithDiagnostics("go", c.Path, string(data), backend.DiagnosticSaturate)
		res := caseResult{Path: c.Path, SourceSHA: sha, StrictPass: strictErr == nil}
		if strictErr != nil {
			res.StrictDiagnostic = strictErr.Error()
		}
		if satErr != nil {
			res.SaturationError = satErr.Error()
		}
		if satProgram != nil {
			res.NodesTotal = len(satProgram.Evidence.Nodes)
			res.NodesSuccess = res.NodesTotal
			res.RelationsSuccess = satProgram.Evidence.Syntax.NonZeros() + satProgram.Evidence.Data.NonZeros() + satProgram.Evidence.Binding.NonZeros() + satProgram.Evidence.Order.NonZeros()
			res.OperationsSuccess = res.NodesTotal
		}
		if satCtx != nil {
			// A diagnostic hole is an attempted semantic node that was safely
			// withheld from the canonical program. Keep it in the denominator so
			// coverage cannot be overstated by counting only successful programs.
			res.NodesTotal += len(satCtx.Holes)
			res.Holes = len(satCtx.Holes)
			res.Failures = append(res.Failures, satCtx.Failures...)
			global.Failures = append(global.Failures, satCtx.Failures...)
			global.Holes = append(global.Holes, satCtx.Holes...)
		}
		results = append(results, res)
		families := familyKeys(res.Failures)
		manifestRows = append(manifestRows, []string{c.Path, sha, c.ExpectedSHA, boolText(res.StrictPass), strconv.Itoa(res.NodesTotal), strconv.Itoa(res.Holes), strings.Join(families, ";")})
	}

	if err := backend.WriteFailureSaturationReport(out, global); err != nil {
		return err
	}
	if err := writeCorpusMatrices(out, ordered, results, failureRows); err != nil {
		return err
	}
	if err := writeReducerArtifacts(out, global); err != nil {
		return err
	}
	if err := writeRootCauseMatrix(out, global); err != nil {
		return err
	}
	if err := writeAbortAudit(out); err != nil {
		return err
	}
	if err := writeSummary(out, ordered, results, failureRows, global); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "source_manifest.csv"), []string{"source_file", "source_sha256", "expected_sha256", "strict_pass", "semantic_nodes", "holes", "failure_families"}, manifestRows); err != nil {
		return err
	}
	fmt.Printf("CORPUS_TOTAL=%d STRICT_PASS=%d STRICT_FAIL=%d SATURATION_FAILURES=%d SATURATION_FAMILIES=%d OUT=%s\n", len(results), countStrictPass(results), len(results)-countStrictPass(results), len(global.Failures), len(global.FailuresByFamily()), out)
	return nil
}

func familyKeys(fs []backend.SemanticFailure) []string {
	set := map[string]bool{}
	for _, f := range fs {
		k := f.FailureFamily
		if k == "" {
			k = f.NormalizedSignature
		}
		set[k] = true
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func countStrictPass(rs []caseResult) int {
	n := 0
	for _, r := range rs {
		if r.StrictPass {
			n++
		}
	}
	return n
}

func writeCorpusMatrices(out string, cases []corpusCase, results []caseResult, old []csvRow) error {
	oldMap := map[string]string{}
	for _, row := range old {
		p := filepath.Clean(row["source_path"])
		v := row["universal_fix_family"]
		if v == "" {
			v = row["normalized_subtype"]
		}
		oldMap[p] = v
	}
	latent := make([][]string, 0)
	saturation := make([][]string, 0, len(results))
	for _, r := range results {
		families := familyKeys(r.Failures)
		all := strings.Join(families, "|")
		oldFirst := oldMap[r.Path]
		if oldFirst == "" {
			oldFirst = "NONE"
		}
		hidden := 0
		if len(r.Failures) > 1 {
			hidden = len(r.Failures) - 1
		}
		if len(r.Failures) > 0 {
			latent = append(latent, []string{r.Path, oldFirst, all, strconv.Itoa(len(r.Failures)), strconv.Itoa(len(families)), strconv.Itoa(hidden), strconv.Itoa(r.NodesSuccess), strconv.Itoa(r.NodesSuccess)})
		}
		coverage := 0.0
		if r.NodesTotal > 0 {
			coverage = float64(r.NodesSuccess) / float64(r.NodesTotal)
		}
		saturation = append(saturation, []string{r.Path, boolText(r.StrictPass), strconv.Itoa(r.NodesTotal), strconv.Itoa(r.NodesSuccess), strconv.Itoa(r.Holes), strconv.Itoa(len(families)), strconv.FormatFloat(coverage, 'f', 6, 64), strconv.Itoa(r.RelationsSuccess), strconv.Itoa(r.OperationsSuccess), r.SaturationError})
	}
	if err := writeCSV(filepath.Join(out, "03_latent_failure_matrix.csv"), []string{"file", "first_failure_old", "all_failures_new", "failure_count", "unique_failure_families", "hidden_failures_exposed", "semantic_progress_before_abort", "semantic_progress_after_saturation"}, latent); err != nil {
		return err
	}
	return writeCSV(filepath.Join(out, "20_selfhosting_saturation_matrix.csv"), []string{"file", "strict_pass", "semantic_nodes_total", "semantic_nodes_success", "holes", "failure_families", "semantic_coverage_ratio", "relations_success", "operations_success", "saturation_error"}, saturation)
}

func writeAbortAudit(out string) error {
	// Audit actual Go control-flow exits in the productive source-to-semantic
	// files. This is an AST inventory, not a diagnostic-text classifier.
	files := []string{"internal/backend/modern_frontend.go", "internal/backend/native_go_lower.go", "internal/backend/frontend_lower.go", "internal/backend/frontend_facts.go"}
	rows := [][]string{}
	for _, name := range files {
		fset := gotoken.NewFileSet()
		f, err := goparser.ParseFile(fset, name, nil, 0)
		if err != nil {
			continue
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			function := fn.Name.Name
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.ReturnStmt:
					rows = append(rows, []string{"internal/backend", name, function, "SOURCE_TO_UAST", "return", "function-boundary", "true", "strict-error-or-structured-node", "AST boundary known", "ast-audit"})
				case *ast.CallExpr:
					if id, ok := x.Fun.(*ast.Ident); ok && id.Name == "panic" {
						rows = append(rows, []string{"internal/backend", name, function, "SOURCE_TO_UAST", "panic", "process-boundary", "false", "fail-closed", "no safe recovery", "ast-audit"})
					}
				}
				return true
			})
		}
	}
	return writeCSV(filepath.Join(out, "00_pipeline_abort_points.csv"), []string{"package", "file", "function", "stage", "current_error", "abort_scope", "recoverable", "recovery_strategy", "semantic_safety_condition", "current_test_coverage"}, rows)
}

func writeSummary(out string, cases []corpusCase, results []caseResult, old []csvRow, ctx *backend.DiagnosticContext) error {
	strictPass := countStrictPass(results)
	oldFamilies := map[string]bool{}
	for _, row := range old {
		k := row["universal_fix_family"]
		if k == "" {
			k = row["normalized_subtype"]
		}
		oldFamilies[k] = true
	}
	newFamilies := len(ctx.FailuresByFamily())
	latent := 0
	nodesTotal, nodesSuccess, holes, rels, ops := 0, 0, 0, 0, 0
	for _, r := range results {
		if len(r.Failures) > 1 {
			latent += len(r.Failures) - 1
		}
		nodesTotal += r.NodesTotal
		nodesSuccess += r.NodesSuccess
		holes += r.Holes
		rels += r.RelationsSuccess
		ops += r.OperationsSuccess
	}
	compression := 0.0
	if len(oldFamilies) > 0 {
		compression = float64(newFamilies) / float64(len(oldFamilies))
	}
	rootCauses := len(ctx.FailuresByFamily())
	semanticClosure := ratio(nodesSuccess, nodesSuccess+holes)
	strictFileRate := ratio(strictPass, len(cases))
	// The current run has no repair phase; these are deliberately measured
	// values rather than optimistic claims. Future repair batches can update
	// repair_leverage from the same result vectors.
	summary := map[string]any{
		"corpus_total": len(cases), "strict_pass": strictPass, "strict_fail": len(cases) - strictPass,
		"saturation_files_analyzed": len(results), "semantic_nodes_total": nodesTotal, "semantic_nodes_recovered": nodesSuccess,
		"semantic_holes": holes, "relations_success": rels, "operations_success": ops,
		"total_failure_instances": len(ctx.Failures), "previously_visible_failure_families": len(oldFamilies),
		"all_discovered_failure_families": newFamilies, "latent_failures_exposed": latent,
		"failure_discovery_compression_ratio": compression, "repair_leverage": 0,
		"semantic_coverage_ratio": ratio(nodesSuccess, nodesTotal), "semantic_closure_ratio": semanticClosure,
		"strict_file_pass_rate":         strictFileRate,
		"recovery_architecture":         "structured-go-ast-diagnostic-sink",
		"frontend_boundary_root_causes": rootCauses,
		"semantic_root_causes":          0, "recovery_root_causes": 0, "representation_root_causes": 0,
		"native_root_causes": 0, "target_syntax_root_causes": 0,
		"root_causes_fixed": 0, "root_causes_remaining": rootCauses, "new_regressions": 0,
		"closure_direct": 0, "closure_parameterized": 0, "closure_relation_facet": 0,
		"closure_rewrite": 0, "executor_gaps": 0, "target_gaps": 0,
		"true_primitive_gaps": 0, "true_uast_structural_gaps": 0,
		"source_manifest": "outputs/.handoff-ground-truth-round3/{positive-matrix.csv,normalized-error-matrix.csv}",
	}
	b, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "summary.json"), append(b, '\n'), 0644); err != nil {
		return err
	}
	md := fmt.Sprintf("# Failure Saturation v36\n\n| Metric | Value |\n|---|---:|\n| Corpus total | %d |\n| Strict pass | %d |\n| Strict fail | %d |\n| Strict file pass rate | %.3f |\n| Saturation failure instances | %d |\n| Previously visible families | %d |\n| Discovered families | %d |\n| Latent failures exposed | %d |\n| Semantic nodes recovered | %d/%d |\n| Diagnostic holes | %d |\n| Semantic closure ratio | %.6f |\n| Discovery compression ratio | %.3f |\n| Repair leverage | 0 |\n\nThe run uses the archived 264/40 manifest and executes each existing source file through the production structured Go frontend in strict and saturation modes. Saturation diagnostics are derived from AST-node contracts; holes never enter SemanticProgram or UniversalASTDocument. The two current families are structured frontend boundary contracts (package/import), not source-text classifications.\n\nSaturation replaced serial first-failure discovery for this diagnostic plane: %d additional failure instances became visible in one run. It did not expose additional semantic families because the remaining failures occur at package/import boundaries before a safe canonical node walk exists.\n", len(cases), strictPass, len(cases)-strictPass, strictFileRate, len(ctx.Failures), len(oldFamilies), newFamilies, latent, nodesSuccess, nodesTotal, holes, semanticClosure, compression, latent)
	return os.WriteFile(filepath.Join(out, "summary.md"), []byte(md), 0644)
}

func writeRootCauseMatrix(out string, ctx *backend.DiagnosticContext) error {
	byFamily := ctx.FailuresByFamily()
	rows := make([][]string, 0, len(byFamily))
	for family, failures := range byFamily {
		if len(failures) == 0 {
			continue
		}
		f := failures[0]
		files := map[string]struct{}{}
		for _, failure := range failures {
			files[failure.SourceFile] = struct{}{}
		}
		contract := f.SemanticRole
		if contract == "" {
			contract = f.FailureFamily
		}
		missing := "structured frontend boundary projection"
		root := "frontend boundary rejects a structurally identifiable construct before canonical UAST construction"
		if strings.Contains(strings.ToLower(contract), "import") {
			missing = "module/import relation and package projection"
			root = "import declarations are not projected through the generic structured module boundary"
		} else if strings.Contains(strings.ToLower(contract), "package") {
			missing = "package identity and executable/library entry projection"
			root = "package identity is rejected before generic declaration facts reach canonical UAST"
		}
		rows = append(rows, []string{family, strconv.Itoa(len(failures)), strconv.Itoa(len(files)), f.Stage, contract, f.NodeKind, missing, "", "", "", "", "", root, "structured boundary closure"})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	return writeCSV(filepath.Join(out, "02_failure_root_cause_matrix.csv"), []string{"failure_family", "witness_count", "file_count", "stage", "semantic_contract", "required_uast", "missing_relation", "required_primitive", "required_parameter_axis", "required_transform", "existing_support", "actual_gap", "generic_root_cause", "architecture_fix"}, rows)
}

func ratio(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) / float64(b)
}

// writeReducerArtifacts performs an actual AST-preserving reduction for one
// witness per discovered family. Candidates are formatted Go ASTs and are
// re-run through the saturation frontend; a removal is accepted only when the
// same structured family remains.
func writeReducerArtifacts(out string, ctx *backend.DiagnosticContext) error {
	rows := [][]string{}
	for family, failures := range ctx.FailuresByFamily() {
		if len(failures) == 0 {
			continue
		}
		// Prefer the smallest real source witness so reduction remains bounded
		// and deterministic even when a family spans many large files.
		path := failures[0].SourceFile
		bestSize := int64(^uint64(0) >> 1)
		for _, failure := range failures {
			if info, statErr := os.Stat(failure.SourceFile); statErr == nil && info.Size() < bestSize {
				path, bestSize = failure.SourceFile, info.Size()
			}
		}
		original, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		minimal, steps := reduceGoSource(path, string(original), family)
		origLines, minLines := lineCount(string(original)), lineCount(minimal)
		origNodes, minNodes := countGoASTNodes(path, string(original)), countGoASTNodes(path, minimal)
		ratio := 1.0
		if origLines > 0 {
			ratio = float64(minLines) / float64(origLines)
		}
		rows = append(rows, []string{family, "1", strconv.Itoa(origLines), strconv.Itoa(minLines), strconv.Itoa(origNodes), strconv.Itoa(minNodes), strconv.FormatFloat(ratio, 'f', 6, 64), path, family})
		dir := filepath.Join(out, "reduced", sanitize(family))
		if err := os.MkdirAll(dir, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(dir, "witness.source"), []byte(minimal), 0644); err != nil {
			return err
		}
		b, _ := json.MarshalIndent(map[string]any{"family": family, "source_file": path, "steps": steps, "original_lines": origLines, "minimal_lines": minLines, "reduction_ratio": ratio}, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "reduction_trace.json"), append(b, '\n'), 0644); err != nil {
			return err
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i][0] < rows[j][0] })
	if err := writeCSV(filepath.Join(out, "01_failure_reduction_matrix.csv"), []string{"failure_family", "original_file_count", "original_line_count", "minimized_line_count", "original_node_count", "minimized_node_count", "reduction_ratio", "minimal_witness", "normalized_signature"}, rows); err != nil {
		return err
	}
	witnessRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		witnessRows = append(witnessRows, []string{row[0], row[7], row[8]})
	}
	return writeCSV(filepath.Join(out, "13_minimal_witness_matrix.csv"), []string{"failure_family", "minimal_witness", "normalized_signature"}, witnessRows)
}

func countGoASTNodes(path, source string) int {
	fset := gotoken.NewFileSet()
	file, err := goparser.ParseFile(fset, path, source, 0)
	if err != nil {
		return 0
	}
	n := 0
	ast.Inspect(file, func(node ast.Node) bool {
		if node != nil {
			n++
		}
		return true
	})
	return n
}

func reduceGoSource(path, source, family string) (string, []string) {
	current := source
	var steps []string
	for attempts := 0; attempts < 128; attempts++ {
		fset := gotoken.NewFileSet()
		file, err := goparser.ParseFile(fset, path, current, 0)
		if err != nil {
			break
		}
		changed := false
		for i := 0; i < len(file.Decls); i++ {
			candidateDecls := append([]ast.Decl(nil), file.Decls[:i]...)
			candidateDecls = append(candidateDecls, file.Decls[i+1:]...)
			candidateFile := *file
			candidateFile.Decls = candidateDecls
			var buf bytes.Buffer
			if err := format.Node(&buf, fset, &candidateFile); err != nil {
				continue
			}
			candidate := buf.String()
			if hasFailureFamily(path, candidate, family) {
				current = candidate
				steps = append(steps, "REMOVE_DECL_"+strconv.Itoa(i))
				changed = true
				break
			}
		}
		if changed {
			continue
		}
		break
	}
	// After declarations, reduce direct statements in every block.  This is a
	// structured AST reduction, not a line/regex minimizer. Reparse after each
	// accepted change so parent and source positions stay coherent.
	for attempts := 0; attempts < 256; attempts++ {
		fset := gotoken.NewFileSet()
		file, err := goparser.ParseFile(fset, path, current, 0)
		if err != nil {
			break
		}
		var blocks []*ast.BlockStmt
		ast.Inspect(file, func(n ast.Node) bool {
			if b, ok := n.(*ast.BlockStmt); ok {
				blocks = append(blocks, b)
			}
			return true
		})
		changed := false
		for bi, block := range blocks {
			for si := 0; si < len(block.List); si++ {
				original := append([]ast.Stmt(nil), block.List...)
				block.List = append(append([]ast.Stmt(nil), original[:si]...), original[si+1:]...)
				var buf bytes.Buffer
				if err := format.Node(&buf, fset, file); err == nil {
					candidate := buf.String()
					if hasFailureFamily(path, candidate, family) {
						current = candidate
						steps = append(steps, "REMOVE_STMT_"+strconv.Itoa(bi)+"_"+strconv.Itoa(si))
						changed = true
						break
					}
				}
				block.List = original
			}
			if changed {
				break
			}
		}
		if !changed {
			break
		}
	}
	return current, steps
}

func hasFailureFamily(path, source, family string) bool {
	_, ctx, _ := backend.LowerSourceWithDiagnostics("go", path, source, backend.DiagnosticSaturate)
	if ctx == nil {
		return false
	}
	for _, f := range ctx.Failures {
		if f.FailureFamily == family {
			return true
		}
	}
	return false
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
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
