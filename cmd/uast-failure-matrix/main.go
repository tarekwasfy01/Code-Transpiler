// uast-failure-matrix consolidates source-free corpus results into one
// normalized failure quotient. It stores evidence about failures, not program
// semantics, and therefore is not an intermediate representation.
package main

import (
	"encoding/csv"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var supported = []string{"r", "go", "rust", "cpp", "c", "python", "zig", "julia", "nim", "csharp", "java", "kotlin", "swift"}

type row struct{ language, state, diagnostic, source string }

func normalized(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

func classify(language, state, diagnostic string) string {
	if state == "UAST_FULL" {
		return "VALID_SUPPORTED"
	}
	if state == "FOREIGN_DIALECT" || state == "VALID_UNSUPPORTED_SEMANTICS" || state == "PARSER_LIMITATION" || state == "INVALID_SOURCE" || state == "TRUNCATED_FRAGMENT" || state == "INTERNAL_ERROR" {
		return state
	}
	d := normalized(diagnostic)
	if strings.Contains(d, "cython") {
		return "FOREIGN_DIALECT"
	}
	if strings.Contains(d, "range binding") || strings.Contains(d, "range step") || strings.Contains(d, "near \"@\"") || strings.Contains(d, "near \"is\"") {
		return "VALID_UNSUPPORTED_SEMANTICS"
	}
	if strings.Contains(d, "unexpected eof") || strings.Contains(d, "unterminated") {
		return "TRUNCATED_FRAGMENT"
	}
	if strings.Contains(d, "internal") || strings.Contains(d, "panic") {
		return "INTERNAL_ERROR"
	}
	return "PARSER_LIMITATION"
}

func profile(diagnostic string) (stage, parser, contract, structure, relation, fix string) {
	d := normalized(diagnostic)
	switch {
	case strings.Contains(d, "range binding"):
		return "frontend_semantic", "binding_pattern", "ForEachStmt + BindingPattern", "ForEachStmt|BindingPattern", "syntax.child|binding.declares", "ordered binding-pattern materializer"
	case strings.Contains(d, "range step"):
		return "frontend_semantic", "symbolic_range", "ForEachStmt + data.operand", "ForEachStmt", "control.loop_back|data.operand", "signed-step contract"
	case strings.Contains(d, "near \"@\""):
		return "frontend_semantic", "decorator_attribute", "Annotation + annotation.applies", "Annotation", "annotation.applies", "proven decorator/attribute mapping"
	case strings.Contains(d, "near \"is\""):
		return "frontend_semantic", "identity_operator", "OperationExpr + operation.kind", "OperationExpr", "operation.kind", "identity operator contract"
	case strings.Contains(d, "near \";\""):
		return "parser", "statement_separator", "Scope + syntax.child", "Scope", "syntax.child", "statement-segment parser binding"
	case strings.Contains(d, "near \":\"") || strings.Contains(d, "near \",\"") || strings.Contains(d, "near \"=\""):
		return "parser", "typed_or_aggregate_form", "TypeRef/ParameterDecl + type.has", "TypeRef|ParameterDecl", "syntax.child|type.has", "typed source-form parser binding"
	default:
		return "parser", "normalized_frontend_syntax", "not proven", "", "", "source-to-facts crosswalk"
	}
}

func main() {
	inputs := flag.String("input", "", "comma-separated corpus output directories; later directories override earlier records")
	out := flag.String("out", "outputs/uast-global-failure-matrix", "output directory")
	flag.Parse()
	if strings.TrimSpace(*inputs) == "" {
		panic("missing -input")
	}
	byID := map[string]row{}
	for _, dir := range strings.Split(*inputs, ",") {
		path := filepath.Join(strings.TrimSpace(dir), "uast_translation_result_matrix.csv")
		f, err := os.Open(path)
		if err != nil {
			panic(err)
		}
		r := csv.NewReader(f)
		header, err := r.Read()
		if err != nil {
			panic(err)
		}
		idx := map[string]int{}
		for i, name := range header {
			idx[name] = i
		}
		for {
			record, err := r.Read()
			if err != nil {
				break
			}
			get := func(name string) string {
				if i, ok := idx[name]; ok && i < len(record) {
					return record[i]
				}
				return ""
			}
			id, language := get("corpus_record_id"), get("language_id")
			if id == "" || language == "" {
				continue
			}
			entry := row{language: language, state: classify(language, get("result_state"), get("diagnostic")), diagnostic: normalized(get("diagnostic")), source: id}
			byID[language+"\x00"+id] = entry
		}
		f.Close()
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		panic(err)
	}
	classes := map[string][]row{}
	covered := map[string]int{}
	for _, r := range byID {
		covered[r.language]++
		if r.state == "VALID_SUPPORTED" {
			continue
		}
		stage, parser, contract, structure, relation, fix := profile(r.diagnostic)
		key := strings.Join([]string{r.language, r.state, stage, parser, contract, structure, relation, r.diagnostic, fix}, "\x00")
		classes[key] = append(classes[key], r)
	}
	f, err := os.Create(filepath.Join(*out, "global_failure_equivalence_classes.csv"))
	if err != nil {
		panic(err)
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"failure_class_id", "source_language", "failure_stage", "parser_class", "frontend_contract", "structure", "uasf", "field", "relation", "projection_class", "normalized_diagnostic", "existing_semantic_contract", "candidate_fix_contract", "result_state", "member_count", "files_unlocked_if_fixed"})
	keys := make([]string, 0, len(classes))
	for k := range classes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, k := range keys {
		p := strings.Split(k, "\x00")
		rs := classes[k]
		_ = w.Write([]string{fmt.Sprintf("global_failure_%04d", i+1), p[0], p[2], p[3], p[4], p[5], "not_proven", "not_proven", p[6], "not_proven", p[7], p[4], p[8], p[1], fmt.Sprint(len(rs)), fmt.Sprint(len(rs))})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		panic(err)
	}
	_ = f.Close()
	c, err := os.Create(filepath.Join(*out, "frontend_evidence_coverage.csv"))
	if err != nil {
		panic(err)
	}
	cw := csv.NewWriter(c)
	_ = cw.Write([]string{"source_language", "records_available", "evidence_state"})
	for _, language := range supported {
		state := "SEMANTIC_EVIDENCE_INSUFFICIENT"
		if covered[language] > 0 {
			state = "EVIDENCE_AVAILABLE"
		}
		_ = cw.Write([]string{language, fmt.Sprint(covered[language]), state})
	}
	cw.Flush()
	_ = c.Close()
	fmt.Printf("Global failure matrix: records=%d classes=%d output=%s\n", len(byID), len(classes), *out)
}
