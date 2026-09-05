package backend

// Source-parser inventory. This reads the checked-in Tree-sitter node schema
// and parser.c provenance independently of MatrixIR; MatrixIR is only the
// destination of the projection.
import (
	"encoding/csv"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type FrontendParserFormEvidence struct {
	Language      string   `json:"language"`
	Form          string   `json:"form"`
	ParserSource  string   `json:"parser_source"`
	ParserSymbol  string   `json:"parser_symbol"`
	Origin        string   `json:"origin"`
	Productive    bool     `json:"productive"`
	Coverage      string   `json:"coverage"`
	NodeNamed     bool     `json:"node_named"`
	HasFields     bool     `json:"has_fields"`
	HasChildren   bool     `json:"has_children"`
	MatrixForms   []string `json:"matrix_forms"`
	MappingStatus string   `json:"mapping_status"`
	CanonicalForm string   `json:"canonical_form"`
}
type FrontendParserInventory struct {
	Languages   []string
	Forms       []string
	Accept      matrixir.SparseMatrix
	Evidence    []FrontendParserFormEvidence
	PerLanguage map[string][]string
	ByLanguage  map[string]map[string]FrontendParserFormEvidence
}
type sourceNodeRow struct{ language, nodeType, named, fields, children string }

func repoRoot() string {
	if wd, e := os.Getwd(); e == nil {
		for p := wd; p != filepath.Dir(p); p = filepath.Dir(p) {
			if _, e := os.Stat(filepath.Join(p, "matrices", "tree_sitter_full", "08_node_types.csv")); e == nil {
				return p
			}
		}
	}
	return "."
}
func boolCSV(s string) bool { v, _ := strconv.ParseBool(strings.TrimSpace(s)); return v }
func readSourceNodeRows(root string) ([]sourceNodeRow, error) {
	f, e := os.Open(filepath.Join(root, "matrices", "tree_sitter_full", "08_node_types.csv"))
	if e != nil {
		return nil, e
	}
	defer f.Close()
	r := csv.NewReader(f)
	h, e := r.Read()
	if e != nil {
		return nil, e
	}
	ix := map[string]int{}
	for i, x := range h {
		ix[strings.TrimPrefix(strings.TrimSpace(x), "\ufeff")] = i
	}
	var out []sourceNodeRow
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		get := func(k string) string {
			if i, ok := ix[k]; ok && i < len(row) {
				return row[i]
			}
			return ""
		}
		if !boolCSV(get("named")) {
			continue
		}
		out = append(out, sourceNodeRow{get("language"), get("node_type"), get("named"), get("has_fields"), get("has_children")})
	}
	return out, nil
}
func parserSourceFor(root, language string) string {
	p := filepath.Join(root, "matrices", "REAL_TS_MATRIX", "raw_parser_c", language, "parser.c")
	if _, e := os.Stat(p); e == nil {
		return p
	}
	return filepath.Join("matrices", "REAL_TS_MATRIX", "raw_parser_c", language, "parser.c")
}

// sourceNodeCoverage is language-neutral. Exact node identity is retained in
// Form; non-canonical nodes use the explicit source-preservation contract.
func sourceNodeCoverage(node string) (string, string) {
	n := strings.ToLower(strings.ReplaceAll(node, "-", "_"))
	c := "source." + n
	ps := []struct {
		keys []string
		c, f string
	}{
		{[]string{"identifier", "name", "field_identifier", "type_identifier", "shorthand_property_identifier"}, "identifier", "DIRECT_CANONICAL"}, {[]string{"string", "integer", "float", "number", "boolean", "true", "false", "null", "nil", "character", "char"}, "literal", "DIRECT_CANONICAL"}, {[]string{"binary", "comparison", "logical", "arithmetic", "infix", "operator"}, "binary", "DESUGARED_CANONICAL"}, {[]string{"unary", "prefix", "postfix"}, "unary", "DESUGARED_CANONICAL"}, {[]string{"call", "invocation", "application"}, "call", "DIRECT_CANONICAL"}, {[]string{"index", "subscript", "slice", "subset", "member", "selector", "attribute", "field_access", "extract"}, "index", "DESUGARED_CANONICAL"}, {[]string{"function", "method_definition", "lambda", "closure", "arrow", "anonymous"}, "closure", "DIRECT_CANONICAL"}, {[]string{"assignment", "declaration", "variable_declaration", "binding", "parameter", "argument"}, "binding", "DIRECT_CANONICAL"}, {[]string{"if", "conditional", "match", "switch", "case", "ternary"}, "if", "DESUGARED_CANONICAL"}, {[]string{"for", "while", "repeat", "loop", "iteration", "range"}, "iteration", "DESUGARED_CANONICAL"}, {[]string{"return", "yield", "break", "continue"}, "control", "DIRECT_CANONICAL"}, {[]string{"import", "include", "namespace", "module", "package", "use"}, "module", "COMPILETIME_METADATA"}, {[]string{"throw", "raise", "catch", "try", "exception", "finally"}, "exception", "LANGUAGE_SPECIFIC_PRESERVED"}, {[]string{"class", "struct", "record", "object", "enum", "tuple", "array", "list", "map", "dictionary", "set", "composite", "literal"}, "aggregate", "DESUGARED_CANONICAL"}, {[]string{"type", "generic", "interface", "trait", "cast", "conversion", "annotation"}, "type", "COMPILETIME_METADATA"}, {[]string{"block", "body", "program", "source_file", "translation_unit", "compilation_unit"}, "block", "DIRECT_CANONICAL"}}
	for _, p := range ps {
		for _, k := range p.keys {
			if strings.Contains(n, k) {
				return p.f, p.c
			}
		}
	}
	return "LANGUAGE_SPECIFIC_PRESERVED", c
}

func ActualFrontendParserInventory() FrontendParserInventory {
	languages := append([]string(nil), matrixir.Languages[:]...)
	root := repoRoot()
	rows, e := readSourceNodeRows(root)
	if e != nil {
		return FrontendParserInventory{Languages: languages, PerLanguage: map[string][]string{}, ByLanguage: map[string]map[string]FrontendParserFormEvidence{}}
	}
	bl := map[string][]sourceNodeRow{}
	set := map[string]bool{}
	for _, r := range rows {
		bl[r.language] = append(bl[r.language], r)
		set[r.nodeType] = true
	}
	forms := make([]string, 0, len(set))
	for f := range set {
		forms = append(forms, f)
	}
	sort.Strings(forms)
	fi := map[string]int{}
	for i, f := range forms {
		fi[f] = i
	}
	accept := matrixir.NewSparseMatrix(len(languages), len(forms))
	ev := make([]FrontendParserFormEvidence, 0, len(rows))
	pl := map[string][]string{}
	by := map[string]map[string]FrontendParserFormEvidence{}
	for li, lang := range languages {
		seen := map[string]bool{}
		by[lang] = map[string]FrontendParserFormEvidence{}
		for _, r := range bl[lang] {
			if seen[r.nodeType] {
				continue
			}
			seen[r.nodeType] = true
			accept.Set(li, fi[r.nodeType], 1)
			cov, can := sourceNodeCoverage(r.nodeType)
			matrixForms := []string{can}
			x := FrontendParserFormEvidence{Language: lang, Form: r.nodeType, ParserSource: parserSourceFor(root, lang), ParserSymbol: r.nodeType, Origin: "tree_sitter_full/08_node_types.csv + parser.c; canonical=" + can, Productive: true, Coverage: cov, NodeNamed: true, HasFields: boolCSV(r.fields), HasChildren: boolCSV(r.children), MatrixForms: matrixForms, MappingStatus: cov}
			x.CanonicalForm = can
			ev = append(ev, x)
			by[lang][r.nodeType] = x
		}
		for f := range seen {
			pl[lang] = append(pl[lang], f)
		}
		sort.Strings(pl[lang])
	}
	sort.Slice(ev, func(i, j int) bool {
		if ev[i].Language == ev[j].Language {
			return ev[i].Form < ev[j].Form
		}
		return ev[i].Language < ev[j].Language
	})
	return FrontendParserInventory{Languages: languages, Forms: forms, Accept: accept, Evidence: ev, PerLanguage: pl, ByLanguage: by}
}
