// Copyright (c) 2026 Tarek Wasfy

package main

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

type row struct {
	Corpus, File, Phase, Status, Error, ErrorFamily, NodeKinds, Operations, Digest string
}

type residual struct {
	Family, PrimitiveClass, ContractFamily, Phase, Representative, SuggestedHandler string
	Count                                                                           int
}

func main() {
	first := flag.String("retranspile", "outputs/semantic-go-retranspile-current/json", "first Semantic-JSON corpus")
	second := flag.String("failure", "outputs/semantic-go-failure-matrix/json", "second Semantic-JSON corpus")
	out := flag.String("out", "outputs/semantic-native-batch-current", "batch evidence output")
	flag.Parse()
	var rows []row
	for _, input := range []struct{ name, root string }{{"retranspile", *first}, {"failure-matrix", *second}} {
		files, err := jsonFiles(input.root)
		if err != nil {
			panic(err)
		}
		for _, file := range files {
			rows = append(rows, check(input.name, file))
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Corpus != rows[j].Corpus {
			return rows[i].Corpus < rows[j].Corpus
		}
		return rows[i].File < rows[j].File
	})
	if err := os.MkdirAll(*out, 0755); err != nil {
		panic(err)
	}
	f, err := os.Create(filepath.Join(*out, "semantic_native_batch.csv"))
	if err != nil {
		panic(err)
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"corpus", "file", "phase", "status", "error", "error_family", "node_kinds", "operations", "sha256"})
	for _, r := range rows {
		_ = w.Write([]string{r.Corpus, r.File, r.Phase, r.Status, r.Error, r.ErrorFamily, r.NodeKinds, r.Operations, r.Digest})
	}
	w.Flush()
	_ = f.Close()
	family := map[string]int{}
	phase := map[string]int{}
	status := map[string]int{}
	for _, r := range rows {
		family[r.ErrorFamily]++
		phase[r.Phase]++
		status[r.Status]++
	}
	residuals := reduceResiduals(rows)
	rf, err := os.Create(filepath.Join(*out, "semantic_native_error_families.csv"))
	if err != nil {
		panic(err)
	}
	rw := csv.NewWriter(rf)
	_ = rw.Write([]string{"error_family", "count", "representative_error", "primitive_class", "contract_family", "phase", "suggested_handler"})
	for _, x := range residuals {
		_ = rw.Write([]string{x.Family, fmt.Sprint(x.Count), x.Representative, x.PrimitiveClass, x.ContractFamily, x.Phase, x.SuggestedHandler})
	}
	rw.Flush()
	_ = rf.Close()
	summary := map[string]any{"files": len(rows), "families": family, "phases": phase, "statuses": status}
	data, _ := json.MarshalIndent(summary, "", "  ")
	if err := os.WriteFile(filepath.Join(*out, "summary.json"), data, 0644); err != nil {
		panic(err)
	}
	fmt.Printf("SEMANTIC_NATIVE_FILES=%d\nOUTPUT=%s\n", len(rows), *out)
	for _, key := range sortedKeys(family) {
		fmt.Printf("FAMILY_%s=%d\n", key, family[key])
	}
}

func jsonFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(filepath.Ext(path), ".json") {
			files = append(files, path)
		}
		return nil
	})
	sort.Strings(files)
	return files, err
}

func check(corpus, file string) row {
	r := row{Corpus: corpus, File: file, Status: "FAIL"}
	data, err := os.ReadFile(file)
	if err != nil {
		r.Phase = "read"
		r.Error = err.Error()
		r.ErrorFamily = "io"
		return r
	}
	sum := sha256.Sum256(data)
	r.Digest = fmt.Sprintf("%x", sum[:])
	p, err := backend.ParseSemanticJSON(data)
	if err != nil {
		r.Phase = "parse_validate"
		r.Error = compact(err.Error())
		r.ErrorFamily = family(r.Error)
		return r
	}
	r.Phase = "native_emit"
	if p.UniversalAST != nil {
		kinds, ops := uastFacts(p.UniversalAST)
		r.NodeKinds, r.Operations = kinds, ops
	}
	if _, err = backend.EmitNativeExecutable(p, "native-x86_64-windows", ""); err != nil {
		r.Error = compact(err.Error())
		r.ErrorFamily = family(r.Error)
		return r
	}
	r.Status = "PASS"
	r.Phase = "native_emit"
	r.ErrorFamily = "none"
	return r
}

func compact(err string) string { return strings.Join(strings.Fields(err), " ") }
func family(err string) string {
	s := strings.ToLower(err)
	switch {
	case strings.Contains(s, "layout") || strings.Contains(s, "representation"):
		return "value_layout_representation"
	case strings.Contains(s, "call") || strings.Contains(s, "abi") || strings.Contains(s, "callee"):
		return "call_abi"
	case strings.Contains(s, "relation") || strings.Contains(s, "operand") || strings.Contains(s, "graph"):
		return "graph_wiring"
	case strings.Contains(s, "type") || strings.Contains(s, "integer") || strings.Contains(s, "float"):
		return "type_integer_float"
	case strings.Contains(s, "closure") || strings.Contains(s, "capture"):
		return "closure_lifetime"
	case strings.Contains(s, "unimplemented") || strings.Contains(s, "native_"):
		return "native_target_gap"
	default:
		return "other_contract"
	}
}

func reduceResiduals(rows []row) []residual {
	byKey := map[string]*residual{}
	for _, r := range rows {
		if r.Status == "PASS" {
			continue
		}
		pc, cf, handler := residualContract(r.Error)
		key := strings.Join([]string{r.ErrorFamily, pc, cf, r.Phase, handler}, "\x00")
		x := byKey[key]
		if x == nil {
			x = &residual{Family: r.ErrorFamily, PrimitiveClass: pc, ContractFamily: cf, Phase: r.Phase, Representative: r.Error, SuggestedHandler: handler}
			byKey[key] = x
		}
		x.Count++
	}
	out := make([]residual, 0, len(byKey))
	for _, x := range byKey {
		out = append(out, *x)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Family < out[j].Family
	})
	return out
}

func residualContract(err string) (primitive, contract, handler string) {
	s := strings.ToUpper(err)
	switch {
	case strings.Contains(s, "UNSUPPORTED.ASSIGN.TARGET"):
		return "ASSIGN_TARGET", "mutable_place_write", "implement universal write-place target lowering"
	case strings.Contains(s, "UNSUPPORTED.EXPRESSION.OPERAND"):
		return "EXPRESSION_OPERAND", "typed_value_operand", "complete operand/result value contract"
	case strings.Contains(s, "NUMERIC.REM.TRUNC"):
		return "REM", "integer_remainder", "bind remainder to native integer kernel"
	case strings.Contains(s, "NO NATIVE SEMANTIC FAMILY"):
		return "UNCLASSIFIED_NODE", "native_legality_family", "classify canonical metadata/executable node"
	case strings.Contains(s, "CALL_ARGUMENT") || strings.Contains(s, "CALL TARGET") || strings.Contains(s, "PARAMETER MODES"):
		return "CALL", "call_abi_value_product", "complete typed call/return ABI projection"
	case strings.Contains(s, "UNRESOLVED BINDING"):
		return "BINDING", "closure_environment_lifetime", "complete binding environment materialization"
	case strings.Contains(s, "CLOSURE") || strings.Contains(s, "BINDING"):
		return "BINDING", "closure_environment_lifetime", "complete binding/capture lifetime lowering"
	case strings.Contains(s, "NATIVE_LEGALITY_UNRESOLVED"):
		return "NATIVE_LEGALITY", "native_legality_contract", "complete native legality classifier"
	default:
		return "UNCLASSIFIED", "residual_contract", "inspect representative contract and add verified handler"
	}
}
func uastFacts(u *backend.UniversalASTDocument) (string, string) {
	kinds := map[string]bool{}
	ops := map[string]bool{}
	for _, n := range u.Nodes {
		var f struct {
			Kind      string `json:"kind"`
			Operation struct {
				Operator   string `json:"operator"`
				SemanticID string `json:"semantic_id"`
			} `json:"operation"`
		}
		b, _ := json.Marshal(n.Fields)
		_ = json.Unmarshal(b, &f)
		if f.Kind != "" {
			kinds[f.Kind] = true
		}
		if f.Operation.Operator != "" {
			ops[f.Operation.Operator] = true
		}
		if f.Operation.SemanticID != "" {
			ops[f.Operation.SemanticID] = true
		}
	}
	return joinSorted(kinds), joinSorted(ops)
}
func joinSorted(m map[string]bool) string {
	a := make([]string, 0, len(m))
	for x := range m {
		a = append(a, x)
	}
	sort.Strings(a)
	return strings.Join(a, "|")
}
func sortedKeys(m map[string]int) []string {
	a := make([]string, 0, len(m))
	for x := range m {
		a = append(a, x)
	}
	sort.Strings(a)
	return a
}
