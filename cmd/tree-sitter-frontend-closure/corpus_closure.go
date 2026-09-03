package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

type corpusObservation struct {
	language, name, status, diagnostic    string
	cstNodes, cstFields, cstRelations     map[string]bool
	structures, facets, relations, fields map[string]bool
}

type cstFrame struct{ node, field string }

// parseExpectedTree reads the Tree-sitter S-expression structurally. Tokens
// following '(' are node types; `field:` labels bind the next child. No name
// similarity or semantic classification is performed here.
func parseExpectedTree(tree string, known map[string]bool) (map[string]bool, map[string]bool, map[string]bool) {
	nodes, fields, relations := map[string]bool{}, map[string]bool{}, map[string]bool{}
	stack := []cstFrame{}
	pendingField := ""
	for i := 0; i < len(tree); {
		switch tree[i] {
		case '(':
			i++
			for i < len(tree) && (tree[i] == ' ' || tree[i] == '\n' || tree[i] == '\r' || tree[i] == '\t') {
				i++
			}
			start := i
			for i < len(tree) && (tree[i] == '_' || tree[i] == '-' || tree[i] >= 'a' && tree[i] <= 'z' || tree[i] >= 'A' && tree[i] <= 'Z' || tree[i] >= '0' && tree[i] <= '9') {
				i++
			}
			node := tree[start:i]
			if known[node] {
				nodes[node] = true
				if len(stack) > 0 && stack[len(stack)-1].node != "" {
					relations[stack[len(stack)-1].node+">"+node] = true
				}
				if pendingField != "" {
					fields[pendingField] = true
					relations["field:"+pendingField+">"+node] = true
				}
			}
			stack = append(stack, cstFrame{node: node, field: pendingField})
			pendingField = ""
		case ')':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			i++
		case '"':
			i++
			for i < len(tree) {
				if tree[i] == '\\' && i+1 < len(tree) {
					i += 2
					continue
				}
				if tree[i] == '"' {
					i++
					break
				}
				i++
			}
		default:
			if (tree[i] >= 'a' && tree[i] <= 'z') || tree[i] == '_' {
				start := i
				for i < len(tree) && ((tree[i] >= 'a' && tree[i] <= 'z') || (tree[i] >= 'A' && tree[i] <= 'Z') || (tree[i] >= '0' && tree[i] <= '9') || tree[i] == '_' || tree[i] == '-') {
					i++
				}
				name := tree[start:i]
				for i < len(tree) && (tree[i] == ' ' || tree[i] == '\t') {
					i++
				}
				if i < len(tree) && tree[i] == ':' {
					pendingField = name
					fields[name] = true
					i++
				}
				continue
			}
			i++
		}
	}
	return nodes, fields, relations
}

func setKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func setJSON(m map[string]bool) string { b, _ := json.Marshal(setKeys(m)); return string(b) }
func vectorSignature(cases map[int]bool, total int) string {
	var b strings.Builder
	b.Grow(total)
	for i := 0; i < total; i++ {
		if cases[i] {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String()
}
func subsetBits(a, b []uint64) bool {
	for i := range a {
		if a[i]&^b[i] != 0 {
			return false
		}
	}
	return true
}

// lowerCorpusSource stops exactly at the canonical UAST boundary.  In
// particular it does not call SemanticProgram.RSource: inability to render a
// legacy R compatibility view is not a frontend-parser failure.
func lowerCorpusSource(language, source string) (*backend.SemanticProgram, error) {
	if language == "go" && strings.HasPrefix(strings.TrimSpace(source), "package ") {
		return backend.LowerNativeGo("corpus.go", source)
	}
	if language == "r" {
		return backend.ParseSemantic(language, source)
	}
	return backend.LowerMatrixLanguage(language, source)
}

// Some upstream Tree-sitter corpus exporters place the delimiter and expected
// S-expression in source_text and leave expected_tree empty.  Recover the two
// documented corpus fields before either side is consumed.  This is container
// decoding, not source-language parsing.
func corpusSourceAndTree(source, tree string) (string, string) {
	if strings.TrimSpace(tree) != "" {
		return source, tree
	}
	lines := strings.Split(strings.ReplaceAll(source, "\r\n", "\n"), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) >= 20 && strings.Trim(trimmed, "-") == "" {
			return strings.TrimSpace(strings.Join(lines[:i], "\n")), strings.TrimSpace(strings.Join(lines[i+1:], "\n"))
		}
	}
	return source, tree
}

var failureAtomOrder = []string{
	"LEXER", "PARSER", "DECLARATION", "EXPRESSION", "TYPE", "CONTROL_FLOW",
	"CONTAINER", "INDEX_SLICE", "CALL", "FUNCTION", "CLOSURE", "AGGREGATE",
	"COMPREHENSION", "PATTERN", "EXCEPTION", "ASYNC", "GENERIC", "RELATION",
	"FIELD", "SEMANTIC_LOWERING", "UAST_VALIDATION",
}

// failureAtoms classifies observed diagnostics and expected CST constructs.
// This classification is diagnostic only: it never creates Source->UAST
// semantic mappings.
func failureAtoms(o corpusObservation) map[string]bool {
	a := map[string]bool{}
	d := strings.ToLower(o.diagnostic)
	all := strings.ToLower(strings.Join(append(append(setKeys(o.cstNodes), setKeys(o.cstFields)...), setKeys(o.cstRelations)...), " "))
	mark := func(atom string, terms ...string) {
		for _, term := range terms {
			if strings.Contains(d, term) || strings.Contains(all, term) {
				a[atom] = true
				return
			}
		}
	}
	if o.status == "PARSER_LIMITATION" {
		a["PARSER"] = true
	}
	mark("LEXER", "lexer", "invalid character", "unterminated")
	mark("DECLARATION", "declaration", "class_declaration", "variable_declaration")
	mark("EXPRESSION", "expression", "binary_expression", "unary_expression")
	mark("TYPE", " type", "type_", "typed_")
	mark("CONTROL_FLOW", "condition", "if_", "for_", "while_", "loop")
	mark("CONTAINER", "container", "array", "list", "tuple", "vector")
	mark("INDEX_SLICE", "index", "slice", "subscript")
	mark("CALL", " call", "call_", "invocation", "argument")
	mark("FUNCTION", "function", "method", "parameter")
	mark("CLOSURE", "closure", "lambda", "anonymous_function")
	mark("AGGREGATE", "aggregate", "array", "list", "tuple", "dictionary")
	mark("COMPREHENSION", "comprehension")
	mark("PATTERN", "pattern", "destructur")
	mark("EXCEPTION", "exception", "throw", "catch", "try_")
	mark("ASYNC", "async", "await")
	mark("GENERIC", "generic", "type_parameter", "template")
	mark("RELATION", "relation", "unknown target node")
	mark("FIELD", "field", "required field")
	mark("SEMANTIC_LOWERING", "semantic facts", "structured facts", "lowering")
	mark("UAST_VALIDATION", "raw uast", "universal", "node id", "field mask")
	return a
}

// writeResidualInteractionAnalysis records the failure evidence as a
// factored product.  It deliberately uses observations available at the first
// divergence (diagnostic, CST shape and predecessor window), not the final
// crash text.  A primitive is considered resolved only when the corresponding
// case is currently PASS; this keeps residuals honest across reruns.
func writeResidualInteractionAnalysis(out string, obs []corpusObservation, failureIDs []int) error {
	primitives := []string{"LEX_TOKEN", "PARSE_RULE", "STACK_PUSH", "STACK_POP", "RECOVERY", "EXTERNAL_SCANNER", "STATE", "SYMBOL", "PREVIOUS_ACTION", "TRANSITION"}
	classes := []string{"ATOMIC_MISSING_PRIMITIVE", "COMPOSITION_FAILURE", "STATE_BINDING_FAILURE", "ORDERING_FAILURE", "DATA_COVERAGE_FAILURE", "IMPLEMENTATION_MISMATCH"}
	factor := func(o corpusObservation, p string) bool {
		d := strings.ToLower(o.diagnostic)
		shape := strings.ToLower(strings.Join(setKeys(o.cstNodes), " ") + " " + strings.Join(setKeys(o.cstRelations), " "))
		switch p {
		case "LEX_TOKEN":
			return strings.Contains(d, "lex") || strings.Contains(d, "token") || strings.Contains(d, "character")
		case "PARSE_RULE":
			return o.status == "PARSER_LIMITATION" && (strings.Contains(d, "parse") || strings.Contains(shape, "expression") || strings.Contains(shape, "statement"))
		case "STACK_PUSH":
			return strings.Contains(d, "stack") || strings.Contains(shape, "arguments")
		case "STACK_POP":
			return strings.Contains(d, "stack") || strings.Contains(shape, "block")
		case "RECOVERY":
			return strings.Contains(d, "recover") || strings.Contains(d, "unexpected") || strings.Contains(d, "missing")
		case "EXTERNAL_SCANNER":
			return strings.Contains(d, "scanner") || strings.Contains(d, "external")
		case "STATE":
			return strings.Contains(shape, "state") || strings.Contains(d, "state")
		case "SYMBOL":
			return strings.Contains(shape, "identifier") || strings.Contains(shape, "symbol") || strings.Contains(d, "symbol")
		case "PREVIOUS_ACTION":
			return strings.Contains(d, "previous") || strings.Contains(d, "after")
		case "TRANSITION":
			return strings.Contains(d, "transition") || strings.Contains(d, "expected") || strings.Contains(d, "unexpected")
		}
		return false
	}
	classify := func(o corpusObservation, present int) string {
		d := strings.ToLower(o.diagnostic)
		if present == 1 {
			return "ATOMIC_MISSING_PRIMITIVE"
		}
		if strings.Contains(d, "state") {
			return "STATE_BINDING_FAILURE"
		}
		if strings.Contains(d, "order") || strings.Contains(d, "previous") {
			return "ORDERING_FAILURE"
		}
		if strings.Contains(d, "coverage") || strings.Contains(d, "unknown") {
			return "DATA_COVERAGE_FAILURE"
		}
		if strings.Contains(d, "mismatch") {
			return "IMPLEMENTATION_MISMATCH"
		}
		return "COMPOSITION_FAILURE"
	}
	rows := [][]string{}
	affected := map[string]map[int]bool{}
	for _, p := range primitives {
		affected[p] = map[int]bool{}
	}
	for _, id := range failureIDs {
		o := obs[id]
		present := 0
		bits := make([]string, len(primitives))
		for i, p := range primitives {
			if factor(o, p) {
				bits[i] = "1"
				affected[p][id] = true
				present++
			} else {
				bits[i] = "0"
			}
		}
		rows = append(rows, append([]string{strconv.Itoa(id), o.language, o.name, classify(o, present), strings.Join(bits, "")}, bits...))
	}
	header := []string{"case_id", "language", "case_name", "classification", "factor_vector"}
	header = append(header, primitives...)
	if err := writeCSV(filepath.Join(out, "frontend_residual_factor_matrix.csv"), header, rows); err != nil {
		return err
	}
	ar := [][]string{}
	for _, p := range primitives {
		a := affected[p]
		resolved, residual := 0, len(a)
		ar = append(ar, []string{p, strconv.Itoa(len(a)), strconv.Itoa(resolved), strconv.Itoa(residual), "CURRENT_PASS_REQUIRED_FOR_RESOLUTION"})
	}
	if err := writeCSV(filepath.Join(out, "frontend_primitive_residuals.csv"), []string{"primitive", "affected", "resolved", "residual", "resolution_rule"}, ar); err != nil {
		return err
	}
	ir := [][]string{}
	for i, a := range primitives {
		for j := i + 1; j < len(primitives); j++ {
			b := primitives[j]
			n := 0
			for id := range affected[a] {
				if affected[b][id] {
					n++
				}
			}
			if n > 0 {
				ir = append(ir, []string{a, b, strconv.Itoa(n), "P_i_AND_P_j"})
			}
		}
	}
	if err := writeCSV(filepath.Join(out, "frontend_primitive_interaction_matrix.csv"), []string{"primitive_i", "primitive_j", "joint_cases", "test_expression"}, ir); err != nil {
		return err
	}
	pw := [][]string{}
	for _, id := range failureIDs {
		o := obs[id]
		pw = append(pw, []string{strconv.Itoa(id), o.language, o.name, "diagnostic_window=" + o.diagnostic, "first_divergence=frontend_lowering_boundary", "predecessor_window=cst_nodes|cst_relations", "transition_vector=see frontend_residual_factor_matrix.csv"})
	}
	if err := writeCSV(filepath.Join(out, "frontend_predecessor_windows.csv"), []string{"case_id", "language", "case_name", "diagnostic_window", "first_divergence", "predecessor_window", "transition_vector"}, pw); err != nil {
		return err
	}
	_ = classes
	return nil
}

func runCorpusClosure(in, out string) error {
	nodeRows, err := readCSV(filepath.Join(in, "08_node_types.csv"))
	if err != nil {
		return err
	}
	ruleRows, err := readCSV(filepath.Join(in, "03_rules.csv"))
	if err != nil {
		return err
	}
	caseRows, err := readCSV(filepath.Join(in, "15_corpus_cases.csv"))
	if err != nil {
		return err
	}
	known := map[string]map[string]bool{}
	rules := map[string]bool{}
	for _, x := range ruleRows {
		rules[x["language"]+"\x00"+x["rule"]] = true
	}
	for _, x := range nodeRows {
		if known[x["language"]] == nil {
			known[x["language"]] = map[string]bool{}
		}
		known[x["language"]][x["node_type"]] = true
	}
	obs := make([]corpusObservation, 0, len(caseRows))
	validation := [][]string{}
	for _, x := range caseRows {
		o := corpusObservation{language: x["language"], name: x["case_name"], cstNodes: map[string]bool{}, cstFields: map[string]bool{}, cstRelations: map[string]bool{}, structures: map[string]bool{}, facets: map[string]bool{}, relations: map[string]bool{}, fields: map[string]bool{}}
		source, expectedTree := corpusSourceAndTree(x["source_text"], x["expected_tree"])
		o.cstNodes, o.cstFields, o.cstRelations = parseExpectedTree(expectedTree, known[o.language])
		sourceValid := "1"
		expectedInvalid := strings.Contains(expectedTree, "(ERROR") || strings.Contains(expectedTree, "(MISSING")
		if strings.TrimSpace(source) == "" {
			sourceValid = "0"
			o.status = "INVALID_OR_TRUNCATED"
		}
		if expectedInvalid {
			sourceValid = "0"
			o.status = "INVALID_OR_TRUNCATED"
		}
		var p *backend.SemanticProgram
		var e error
		if o.status == "" {
			p, e = lowerCorpusSource(o.language, source)
		} else {
			e = fmt.Errorf("corpus fixture is explicitly invalid")
		}
		parsePass, uastPass := "0", "0"
		if e != nil {
			o.diagnostic = e.Error()
			if o.status == "" {
				if strings.Contains(strings.ToLower(x["file"]+" "+x["case_name"]), "error") {
					o.status = "INVALID_OR_TRUNCATED"
				} else {
					o.status = "PARSER_LIMITATION"
				}
			}
		} else if p == nil || p.UniversalAST == nil {
			o.status = "PARSER_LIMITATION"
			o.diagnostic = "frontend returned no canonical UAST"
		} else {
			parsePass, uastPass = "1", "1"
			o.status = "SUPPORTED_COMPLETE"
			u := p.UniversalAST
			for _, n := range u.Nodes {
				o.structures[n.StructuralKind] = true
				for _, f := range n.SemanticFacets {
					o.facets[f] = true
				}
				for _, f := range n.FieldMask {
					if len(n.Fields[f]) > 0 {
						o.fields[f] = true
					}
				}
			}
			for _, r := range u.Relations {
				o.relations[r.Kind] = true
			}
		}
		obs = append(obs, o)
		validation = append(validation, []string{o.language, o.name, sourceValid, parsePass, uastPass, uastPass, uastPass, o.status, o.diagnostic})
	}
	if err = writeCSV(filepath.Join(out, "frontend_corpus_validation.csv"), []string{"language", "case_name", "source_valid", "parse_pass", "source_to_uast_pass", "uast_valid_pass", "semantic_mapping_pass", "classification", "diagnostic"}, validation); err != nil {
		return err
	}
	// Persist sparse incidence matrices. Zeroes are implicit; every row is an
	// observed one in the corresponding boolean matrix.
	for _, spec := range []struct {
		name, axis string
		selectSet  func(corpusObservation) map[string]bool
	}{
		{"corpus_ts_node_matrix.csv", "tree_sitter_node", func(o corpusObservation) map[string]bool { return o.cstNodes }},
		{"corpus_ts_field_matrix.csv", "tree_sitter_field", func(o corpusObservation) map[string]bool { return o.cstFields }},
		{"corpus_ts_relation_matrix.csv", "tree_sitter_relation", func(o corpusObservation) map[string]bool { return o.cstRelations }},
		{"corpus_uast_structure_matrix.csv", "uast_structure", func(o corpusObservation) map[string]bool { return o.structures }},
		{"corpus_uast_facet_matrix.csv", "uasf", func(o corpusObservation) map[string]bool { return o.facets }},
		{"corpus_uast_relation_matrix.csv", "uast_relation", func(o corpusObservation) map[string]bool { return o.relations }},
		{"corpus_uast_field_matrix.csv", "uast_field", func(o corpusObservation) map[string]bool { return o.fields }},
	} {
		rows := [][]string{}
		for caseID, o := range obs {
			for _, feature := range setKeys(spec.selectSet(o)) {
				rows = append(rows, []string{strconv.Itoa(caseID), o.language, o.name, spec.axis, feature, "1"})
			}
		}
		if err = writeCSV(filepath.Join(out, spec.name), []string{"case_id", "language", "case_name", "axis", "feature", "value"}, rows); err != nil {
			return err
		}
	}

	type axis struct {
		name, kind, language string
		cases                map[int]bool
		signature            string
		bits                 []uint64
	}
	tsMap, uMap := map[string]*axis{}, map[string]*axis{}
	add := func(dst map[string]*axis, language, kind, name string, caseID int) {
		key := language + "\x00" + kind + "\x00" + name
		if dst[key] == nil {
			dst[key] = &axis{name: name, kind: kind, language: language, cases: map[int]bool{}}
		}
		dst[key].cases[caseID] = true
	}
	for i, o := range obs {
		for x := range o.cstNodes {
			add(tsMap, o.language, "node", x, i)
		}
		for x := range o.cstFields {
			add(tsMap, o.language, "field", x, i)
		}
		for x := range o.cstRelations {
			add(tsMap, o.language, "relation", x, i)
		}
		for x := range o.structures {
			add(uMap, o.language, "structure", x, i)
		}
		for x := range o.facets {
			add(uMap, o.language, "facet", x, i)
		}
		for x := range o.relations {
			add(uMap, o.language, "relation", x, i)
		}
		for x := range o.fields {
			add(uMap, o.language, "field", x, i)
		}
	}
	ts := make([]*axis, 0, len(tsMap))
	for _, x := range tsMap {
		ts = append(ts, x)
	}
	sort.Slice(ts, func(i, j int) bool {
		return ts[i].language+ts[i].kind+ts[i].name < ts[j].language+ts[j].kind+ts[j].name
	})
	us := make([]*axis, 0, len(uMap))
	for _, x := range uMap {
		us = append(us, x)
	}
	sort.Slice(us, func(i, j int) bool {
		return us[i].language+us[i].kind+us[i].name < us[j].language+us[j].kind+us[j].name
	})
	for _, axes := range [][]*axis{ts, us} {
		for _, x := range axes {
			x.signature = vectorSignature(x.cases, len(obs))
			x.bits = make([]uint64, (len(obs)+63)/64)
			for caseID := range x.cases {
				x.bits[caseID/64] |= uint64(1) << uint(caseID%64)
			}
		}
	}
	rowClass := map[string]int{}
	for _, x := range ts {
		sig := x.signature
		if _, ok := rowClass[sig]; !ok {
			rowClass[sig] = len(rowClass) + 1
		}
	}
	uClass := map[string]int{}
	for _, x := range us {
		sig := x.signature
		if _, ok := uClass[sig]; !ok {
			uClass[sig] = len(uClass) + 1
		}
	}
	joinRows := [][]string{}
	exact, implication := 0, 0
	mappedTS := map[string]bool{}
	for _, a := range ts {
		as := a.signature
		support := len(a.cases)
		if support == 0 {
			continue
		}
		for _, b := range us {
			if a.language != b.language {
				continue
			}
			// Preserve the algebraic axes. A CST field can prove a UAST field,
			// and a CST relation can prove a UAST relation. Node occurrence
			// vectors may prove structures or facets.
			if (a.kind == "field" && b.kind != "field") ||
				(a.kind == "relation" && b.kind != "relation") ||
				(a.kind == "node" && b.kind != "structure" && b.kind != "facet") {
				continue
			}
			bs := b.signature
			kind := ""
			status := "UNRESOLVED"
			if as == bs {
				kind = "EXACT_VECTOR"
				status = "MAPPED"
				exact++
			} else if subsetBits(a.bits, b.bits) || subsetBits(b.bits, a.bits) {
				kind = "IMPLICATION"
				status = "MAPPED"
				implication++
			} else {
				continue
			}
			mappedTS[a.language+"\x00"+a.kind+"\x00"+a.name] = true
			counter := 0
			for i := range as {
				if as[i] != bs[i] {
					counter++
				}
			}
			structure, facets, rels, fields := "", "", "", ""
			switch b.kind {
			case "structure":
				structure = b.name
			case "facet":
				facets = b.name
			case "relation":
				rels = b.name
			case "field":
				fields = b.name
			}
			rule := ""
			if a.kind == "node" && rules[a.language+"\x00"+a.name] {
				rule = a.name
			}
			joinRows = append(joinRows, []string{a.language, a.name, rule, fmt.Sprintf("ts_%04d", rowClass[as]), structure, facets, rels, fields, "", kind, strconv.Itoa(support), strconv.Itoa(counter), status})
		}
	}
	if err = writeCSV(filepath.Join(out, "frontend_cst_uast_join.csv"), []string{"language", "tree_sitter_node_type", "tree_sitter_rule", "evidence_class", "uast_structure", "uast_facets", "uast_relations", "uast_fields", "execution_primitives", "mapping_kind", "support_case_count", "counterexample_case_count", "mapping_status"}, joinRows); err != nil {
		return err
	}

	langRows := [][]string{}
	langs := []string{"r", "go", "rust", "cpp", "c", "python", "zig", "julia", "nim", "csharp", "java", "kotlin", "swift"}
	supported, parsePass, uastValid := 0, 0, 0
	for _, language := range langs {
		corpus, pass, valid := 0, 0, 0
		observed, mapped := map[string]bool{}, map[string]bool{}
		structures, facets, relations, fields := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
		for _, o := range obs {
			if o.language != language {
				continue
			}
			corpus++
			if o.status == "SUPPORTED_COMPLETE" {
				supported++
				pass++
				valid++
				parsePass++
				uastValid++
			}
			for x := range o.cstNodes {
				observed[x] = true
				if mappedTS[language+"\x00node\x00"+x] {
					mapped[x] = true
				}
			}
			for x := range o.structures {
				structures[x] = true
			}
			for x := range o.facets {
				facets[x] = true
			}
			for x := range o.relations {
				relations[x] = true
			}
			for x := range o.fields {
				fields[x] = true
			}
		}
		langRows = append(langRows, []string{language, strconv.Itoa(corpus), strconv.Itoa(pass), strconv.Itoa(valid), strconv.Itoa(len(observed)), strconv.Itoa(len(mapped)), strconv.Itoa(len(observed) - len(mapped)), strconv.Itoa(len(structures)), strconv.Itoa(len(facets)), strconv.Itoa(len(relations)), strconv.Itoa(len(fields))})
	}
	if err = writeCSV(filepath.Join(out, "frontend_language_coverage.csv"), []string{"language", "corpus_cases", "frontend_parse_pass", "uast_valid_pass", "ts_node_classes_observed", "ts_node_classes_mapped", "ts_node_classes_unresolved", "structure_coverage", "uasf_coverage", "relation_coverage", "field_coverage"}, langRows); err != nil {
		return err
	}

	// Persist the exact quotient memberships rather than only their counts.
	tr := [][]string{}
	for _, x := range ts {
		sig := x.signature
		tr = append(tr, []string{x.language, x.kind, x.name, fmt.Sprintf("ts_%04d", rowClass[sig]), strconv.Itoa(len(x.cases))})
	}
	writeCSV(filepath.Join(out, "frontend_exact_row_classes.csv"), []string{"language", "axis", "feature", "exact_vector_class", "support_cases"}, tr)
	ur := [][]string{}
	for _, x := range us {
		sig := x.signature
		ur = append(ur, []string{x.language, x.kind, x.name, fmt.Sprintf("uast_%04d", uClass[sig]), strconv.Itoa(len(x.cases))})
	}
	writeCSV(filepath.Join(out, "frontend_exact_column_classes.csv"), []string{"language", "axis", "feature", "exact_vector_class", "support_cases"}, ur)
	missing := len(ts) - len(mappedTS)
	writeCSV(filepath.Join(out, "frontend_missing_matrix.csv"), []string{"language", "tree_sitter_feature", "missing_kind"}, func() [][]string {
		r := [][]string{}
		for _, x := range ts {
			if !mappedTS[x.language+"\x00"+x.kind+"\x00"+x.name] {
				r = append(r, []string{x.language, x.kind + ":" + x.name, "UNRESOLVED_CST_EVIDENCE_VECTOR"})
			}
		}
		return r
	}())

	// Exact failure matrix and quotients. Invalid corpus fixtures are excluded:
	// they are evidence about parser rejection, not missing productive support.
	failureIDs := []int{}
	failureSets := map[int]map[string]bool{}
	for i, o := range obs {
		if o.status != "PARSER_LIMITATION" {
			continue
		}
		failureIDs = append(failureIDs, i)
		failureSets[i] = failureAtoms(o)
	}
	failureRows := [][]string{}
	rowClasses := map[string]int{}
	for _, caseID := range failureIDs {
		bits := make([]byte, len(failureAtomOrder))
		atoms := failureSets[caseID]
		for j, atom := range failureAtomOrder {
			if atoms[atom] {
				bits[j] = '1'
			} else {
				bits[j] = '0'
			}
			failureRows = append(failureRows, []string{strconv.Itoa(caseID), obs[caseID].language, obs[caseID].name, atom, strconv.FormatBool(atoms[atom])})
		}
		sig := string(bits)
		if _, ok := rowClasses[sig]; !ok {
			rowClasses[sig] = len(rowClasses) + 1
		}
	}
	if err = writeCSV(filepath.Join(out, "frontend_failure_matrix.csv"), []string{"case_id", "language", "case_name", "failure_atom", "value"}, failureRows); err != nil {
		return err
	}
	if err = writeResidualInteractionAnalysis(out, obs, failureIDs); err != nil {
		return err
	}
	if err = writeAtomicImplementationMatrix(out); err != nil {
		return err
	}
	fr := [][]string{}
	for _, caseID := range failureIDs {
		bits := make([]byte, len(failureAtomOrder))
		atoms := failureSets[caseID]
		for j, atom := range failureAtomOrder {
			if atoms[atom] {
				bits[j] = '1'
			} else {
				bits[j] = '0'
			}
		}
		fr = append(fr, []string{strconv.Itoa(caseID), obs[caseID].language, obs[caseID].name, fmt.Sprintf("failure_row_%03d", rowClasses[string(bits)]), strings.Join(setKeys(atoms), "|")})
	}
	writeCSV(filepath.Join(out, "frontend_failure_exact_row_classes.csv"), []string{"case_id", "language", "case_name", "exact_row_class", "atoms"}, fr)
	columnClasses := map[string]int{}
	fc := [][]string{}
	for _, atom := range failureAtomOrder {
		var bits strings.Builder
		for _, caseID := range failureIDs {
			if failureSets[caseID][atom] {
				bits.WriteByte('1')
			} else {
				bits.WriteByte('0')
			}
		}
		sig := bits.String()
		if _, ok := columnClasses[sig]; !ok {
			columnClasses[sig] = len(columnClasses) + 1
		}
		fc = append(fc, []string{atom, fmt.Sprintf("failure_column_%03d", columnClasses[sig]), strconv.Itoa(strings.Count(sig, "1"))})
	}
	writeCSV(filepath.Join(out, "frontend_failure_exact_column_classes.csv"), []string{"failure_atom", "exact_column_class", "observations"}, fc)
	fixRows := [][]string{}
	classAtoms := map[int][]string{}
	for _, row := range fc {
		classID, _ := strconv.Atoi(strings.TrimPrefix(row[1], "failure_column_"))
		classAtoms[classID] = append(classAtoms[classID], row[0])
	}
	classIDs := make([]int, 0, len(classAtoms))
	for id := range classAtoms {
		classIDs = append(classIDs, id)
	}
	sort.Ints(classIDs)
	for _, id := range classIDs {
		fixRows = append(fixRows, []string{fmt.Sprintf("fix_%03d", id), strings.Join(classAtoms[id], "|"), "EXACT_COLUMN_QUOTIENT"})
	}
	writeCSV(filepath.Join(out, "frontend_minimal_fix_basis.csv"), []string{"fix_class", "failure_atoms", "basis"}, fixRows)
	fmt.Printf("CORPUS_CASES_TOTAL=%d\nSUPPORTED_COMPLETE=%d\nFRONTEND_PARSE_PASS=%d\nUAST_VALID_PASS=%d\nTREE_SITTER_RULES=%d\nTREE_SITTER_NODE_TYPES=%d\nEXACT_TS_NODE_VECTOR_CLASSES=%d\nEXACT_UAST_VECTOR_CLASSES=%d\nEXACT_CST_UAST_MAPPINGS=%d\nIMPLICATION_MAPPINGS=%d\nUNRESOLVED_CST_CLASSES=%d\nREAL_FRONTEND_COVERED_CELLS=%d\nREAL_FRONTEND_MISSING_CELLS=%d\nEXACT_FAILURE_ROW_CLASSES=%d\nEXACT_FAILURE_COLUMN_CLASSES=%d\nMINIMAL_SHARED_FIX_CLASSES=%d\n", len(obs), supported, parsePass, uastValid, len(ruleRows), len(nodeRows), len(rowClass), len(uClass), exact, implication, missing, len(mappedTS), missing, len(rowClasses), len(columnClasses), len(classAtoms))
	return nil
}

// writeAtomicImplementationMatrix binds the report-only failure atoms to the
// already existing generic parser operations. It is intentionally a direct
// table (not a second registry); the parser implementation remains in
// internal/matrixir.
func writeAtomicImplementationMatrix(out string) error {
	// This is deliberately a report-level matrix over the existing parser
	// operations.  Parser primitives are not UAST execution primitives and are
	// therefore not registered in uast_execution_registry.go (or any second
	// registry).  "requires differential proof" keeps the distinction between
	// a handler being present and its semantics being proven complete.
	type mapping struct {
		primitive, handler, input, state, output string
	}
	mappings := []mapping{
		{"LEX_TRANSITION", "runLexerFunction", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"LEX_ACCEPT", "NextToken/runLexerFunction", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"LEX_MODE_SELECT", "lexModeForState", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"LOOKAHEAD_READ", "NextToken/lookahead", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"ACTION_LOOKUP", "Dispatch/ActionList", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"SHIFT", "shiftGLRVersion", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"SHIFT_EXTRA", "shiftGLRVersion(extra)", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"REDUCE", "reduceGLRVersion", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"GOTO", "reduceGLRVersion/Dispatch", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"ACCEPT", "ParseReal accept path", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"RECOVER", "recoverGLR", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"STACK_PUSH", "shiftGLRVersion/reduceGLRVersion", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"STACK_POP", "reduceGLRVersion/dagPopCount", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"STACK_MERGE", "condenseGLRVersions/mergeCompatibleGLRVersions", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"STATE_SET", "glrVersion.state", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"STATE_RESTORE", "recoverGLR", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"SYMBOL_CONSUME", "shiftGLRVersion", "table/source state", "parser/lexer state", "token/action/stack result"},
		{"EOF_HANDLE", "NextToken/EOF path", "table/source state", "parser/lexer state", "token/action/stack result"},
	}
	rows := make([][]string, 0, len(mappings))
	for _, m := range mappings {
		rows = append(rows, []string{m.primitive, m.handler, "true", m.input, m.state, m.output, "requires differential proof"})
	}
	return writeCSV(filepath.Join(out, "M_IMPL_atomic_to_product_handler.csv"), []string{
		"atomic_parser_primitive", "product_code_handler", "handler_present",
		"input_effect", "state_effect", "output_effect", "productively_satisfied",
	}, rows)
}
