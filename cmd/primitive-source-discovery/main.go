// primitive-source-discovery analyzes an existing source corpus exactly once
// per file. It stops at Canonical UAST and never selects or invokes a target
// backend. All primitive evidence comes from backend.BuildSemanticTrace.
package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

type sourceCase struct {
	Language string
	Package  string
	Path     string
}

type csvOutput struct {
	file *os.File
	csv  *csv.Writer
}

func openCSV(path string, header []string) (*csvOutput, error) {
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		_ = f.Close()
		return nil, err
	}
	w.Flush()
	return &csvOutput{file: f, csv: w}, w.Error()
}

func (w *csvOutput) write(row []string) error {
	if err := w.csv.Write(row); err != nil {
		return err
	}
	w.csv.Flush()
	return w.csv.Error()
}

func (w *csvOutput) close() error {
	w.csv.Flush()
	if err := w.csv.Error(); err != nil {
		_ = w.file.Close()
		return err
	}
	return w.file.Close()
}

func sha(data []byte) string {
	s := sha256.Sum256(data)
	return hex.EncodeToString(s[:])
}

func shortHash(v any) string {
	b, _ := json.Marshal(v)
	return sha(b)[:24]
}

func loadManifest(path string) ([]sourceCase, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, 0, err
	}
	idx := map[string]int{}
	for i, name := range header {
		idx[strings.TrimSpace(name)] = i
	}
	get := func(row []string, name string) string {
		i, ok := idx[name]
		if !ok || i >= len(row) {
			return ""
		}
		return strings.TrimSpace(row[i])
	}
	seen := map[string]bool{}
	var out []sourceCase
	missing := 0
	for {
		row, e := r.Read()
		if e != nil {
			break
		}
		path := get(row, "source_file")
		if path == "" {
			continue
		}
		abs, e := filepath.Abs(path)
		if e != nil {
			continue
		}
		key := strings.ToLower(filepath.Clean(abs))
		if seen[key] {
			continue
		}
		seen[key] = true
		st, e := os.Stat(abs)
		if e != nil || st.IsDir() {
			missing++
			continue
		}
		lang := backend.NormalizeLanguage(get(row, "source_language"))
		if !backend.HasFrontend(lang) {
			continue
		}
		out = append(out, sourceCase{Language: lang, Package: get(row, "package"), Path: abs})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Language != out[j].Language {
			return out[i].Language < out[j].Language
		}
		return strings.ToLower(out[i].Path) < strings.ToLower(out[j].Path)
	})
	return out, missing, nil
}

func main() {
	manifest := flag.String("manifest", filepath.Join("outputs", "miner-cross-transpile", "cases.csv"), "existing source-corpus manifest")
	outDir := flag.String("out", filepath.Join("outputs", "primitive-full-discovery", "discovery"), "discovery matrix directory")
	flag.Parse()
	if err := run(*manifest, *outDir); err != nil {
		panic(err)
	}
}

func run(manifest, outDir string) error {
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	cases, missingFiles, err := loadManifest(manifest)
	if err != nil {
		return err
	}

	caseOut, err := openCSV(filepath.Join(outDir, "cases.csv"), []string{"case_id", "root_case_id", "parent_case_id", "generation", "source_id", "source_language", "ecosystem", "package", "version", "relative_input", "source_sha256", "uast_hash", "semantic_demand_hash", "semantic_residual_hash", "parse_success", "uast_success", "structured_demand_success", "semantic_evidence_status"})
	if err != nil {
		return err
	}
	defer caseOut.close()
	nodeOut, err := openCSV(filepath.Join(outDir, "uast_nodes.csv"), []string{"case_id", "uast_node_id", "parent_uast_node_id", "source_language", "relative_input", "source_start", "source_end", "node_kind", "semantic_operation", "semantic_family", "arity", "primitive_id", "primitive_family", "parameterization", "language_operation"})
	if err != nil {
		return err
	}
	defer nodeOut.close()
	featureOut, err := openCSV(filepath.Join(outDir, "case_semantic_features.csv"), []string{"case_id", "uast_node_id", "node_kind", "semantic_operation", "semantic_family", "arity", "operand_roles", "result_role", "type_model", "numeric_model", "effects", "evaluation_order", "binding", "scope", "ownership", "lifetime", "representation", "control_flow", "memory_behavior", "exception_behavior", "semantic_feature_hash"})
	if err != nil {
		return err
	}
	defer featureOut.close()
	primitiveOut, err := openCSV(filepath.Join(outDir, "case_primitive_matrix.csv"), []string{"case_id", "primitive_id", "primitive_family", "parameterization", "occurrence_count"})
	if err != nil {
		return err
	}
	defer primitiveOut.close()
	attemptOut, err := openCSV(filepath.Join(outDir, "attempts.csv"), []string{"attempt_id", "case_id", "root_case_id", "source_language", "target_language", "route_type", "projection_mode", "intermediate_language", "intermediate_route", "direct_success", "primitive_lowering_success", "intermediate_success", "runtime_used", "transpile_success", "target_parse_success", "compile_success", "run_success", "semantic_verify_success", "failure_stage", "status", "error_class", "diagnostic_sha256", "semantic_evidence_status"})
	if err != nil {
		return err
	}
	defer attemptOut.close()

	operations := map[string]int{}
	parsed, canonical, demandCases, missingUAST, missingDemand := 0, 0, 0, 0, 0
	nodeRows, featureRows, primitiveRows := 0, 0, 0
	primitiveSet, operationSet := map[string]bool{}, map[string]bool{}
	for i, item := range cases {
		data, readErr := os.ReadFile(item.Path)
		caseID := "CASE_" + sha([]byte(strings.ToLower(item.Path)))[:24]
		trace := backend.SemanticTrace{SchemaVersion: backend.SemanticTraceSchemaVersion, Route: backend.SemanticTraceRoute{RouteType: "DISCOVERY", ProjectionMode: "canonical-uast-only"}}
		status := "MISSING_ROOT_UAST"
		parseValue, uastValue, demandValue := "0", "0", "0"
		if readErr == nil {
			program, lowerErr := backend.LowerSource(item.Language, item.Path, string(data))
			if lowerErr == nil && program != nil && program.UniversalAST != nil {
				trace = backend.BuildSemanticTrace(true, program.UniversalAST, trace.Route)
				parsed++
				canonical++
				parseValue, uastValue, demandValue, status = "1", "1", "1", "STRUCTURED_DEMAND_OK"
				if len(trace.PrimitiveDemands) > 0 {
					demandCases++
				} else {
					status = "STRUCTURED_ZERO_DEMAND"
				}
			} else {
				missingUAST++
				missingDemand++
			}
		} else {
			missingUAST++
			missingDemand++
		}
		demandHash, residualHash := "", ""
		if trace.UASTSuccess {
			demandHash = shortHash(trace.PrimitiveDemands)
			residualHash = shortHash(trace.Nodes)
		}
		if err := caseOut.write([]string{caseID, caseID, "", "0", item.Path, item.Language, "existing-corpus", item.Package, "", item.Path, sha(data), trace.UASTHash, demandHash, residualHash, parseValue, uastValue, demandValue, status}); err != nil {
			return err
		}
		for _, n := range trace.Nodes {
			if err := nodeOut.write([]string{caseID, n.NodeID, n.ParentNodeID, item.Language, item.Path, n.SourceStart, n.SourceEnd, n.NodeKind, n.SemanticOperation, n.SemanticFamily, strconv.Itoa(n.Arity), n.PrimitiveID, n.PrimitiveFamily, n.Parameterization, n.LanguageOperation}); err != nil {
				return err
			}
			feature := []string{caseID, n.NodeID, n.NodeKind, n.SemanticOperation, n.SemanticFamily, strconv.Itoa(n.Arity), n.OperandRoles, n.ResultRole, n.TypeModel, n.NumericModel, n.Effects, n.EvaluationOrder, n.Binding, n.Scope, n.Ownership, n.Lifetime, n.Representation, n.ControlFlow, n.MemoryBehavior, n.ExceptionBehavior}
			if err := featureOut.write(append(feature, shortHash(feature))); err != nil {
				return err
			}
			nodeRows++
			featureRows++
			if n.SemanticOperation != "" {
				operationSet[n.SemanticOperation] = true
				operations[item.Language+"\x00"+n.LanguageOperation+"\x00"+n.SemanticOperation+"\x00"+n.PrimitiveID]++
			}
		}
		for _, p := range trace.PrimitiveDemands {
			if p.PrimitiveID == "" || p.PrimitiveID == "UNCLASSIFIED" || strings.HasPrefix(p.PrimitiveID, "UNSUPPORTED.") {
				continue
			}
			if err := primitiveOut.write([]string{caseID, p.PrimitiveID, p.PrimitiveFamily, p.Parameterization, strconv.Itoa(p.OccurrenceCount)}); err != nil {
				return err
			}
			primitiveSet[p.PrimitiveID] = true
			primitiveRows++
		}
		if (i+1)%100 == 0 || i+1 == len(cases) {
			fmt.Printf("DISCOVERY=%d/%d CANONICAL=%d PRIMITIVES=%d\n", i+1, len(cases), canonical, len(primitiveSet))
		}
	}

	opOut, err := openCSV(filepath.Join(outDir, "language_operation_semantic_matrix.csv"), []string{"language", "language_operation", "semantic_operation", "primitive_id", "semantic_feature_hash", "observed_count"})
	if err != nil {
		return err
	}
	keys := make([]string, 0, len(operations))
	for k := range operations {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		p := strings.Split(k, "\x00")
		if err := opOut.write([]string{p[0], p[1], p[2], p[3], shortHash(p), strconv.Itoa(operations[k])}); err != nil {
			return err
		}
	}
	if err := opOut.close(); err != nil {
		return err
	}

	summary := map[string]any{"source_files_analyzed": len(cases), "manifest_files_missing": missingFiles, "cases_parsed": parsed, "cases_with_canonical_uast": canonical, "cases_with_primitive_demand": demandCases, "cases_missing_uast": missingUAST, "cases_missing_demand": missingDemand, "uast_node_rows": nodeRows, "semantic_feature_rows": featureRows, "primitive_demand_rows": primitiveRows, "unique_semantic_operations": len(operationSet), "unique_observed_primitives": len(primitiveSet), "target_projection_attempts": 0, "semantic_source": "Canonical UAST only"}
	b, _ := json.MarshalIndent(summary, "", "  ")
	if err := os.WriteFile(filepath.Join(outDir, "summary.json"), b, 0644); err != nil {
		return err
	}
	return nil
}
