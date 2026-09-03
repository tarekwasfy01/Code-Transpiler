package backend

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

type parseProducerRegistry struct {
	byState       map[string]string
	bySignature   map[string]string
	byStateSymbol map[string]string
	structure     map[string]string
}

func loadParseProducerRegistry() parseProducerRegistry {
	r := parseProducerRegistry{byState: map[string]string{}, bySignature: map[string]string{}, byStateSymbol: map[string]string{}, structure: map[string]string{}}
	kernel := filepath.Join("matrices", "REAL_TS_MATRIX", "15_construct_true_parser_kernel.csv")
	spec := filepath.Join("matrices", "frontend_closure", "producer_spec", "producer_spec.csv")
	if f, e := os.Open(kernel); e == nil {
		cr := csv.NewReader(f)
		cr.FieldsPerRecord = -1
		cr.Read()
		for {
			row, e := cr.Read()
			if e != nil {
				break
			}
			if len(row) < 17 {
				continue
			}
			for _, s := range strings.Split(row[12], ";") {
				if s != "" {
					r.byState[row[0]+":"+s] = row[16]
				}
			}
			// parser_symbols is the generated symbol-name vector for the same
			// kernel states. Resolve names through symbols.csv once and retain a
			// state+symbol index; production remains part of the exact lookup key
			// when available, with this table as a deterministic fallback.
			symbols := map[string]string{}
			if sf, se := os.Open(filepath.Join("matrices", "REAL_TS_MATRIX", "execution_ready", "symbols.csv")); se == nil {
				sr := csv.NewReader(sf)
				sr.FieldsPerRecord = -1
				_, _ = sr.Read()
				for {
					x, re := sr.Read()
					if re != nil {
						break
					}
					if len(x) >= 3 && x[0] == row[0] {
						symbols[x[2]] = x[1]
					}
				}
				sf.Close()
			}
			for _, st := range strings.Split(row[12], ";") {
				if st == "" {
					continue
				}
				for _, name := range strings.Split(row[13], ";") {
					if sid, ok := symbols[name]; ok {
						r.byStateSymbol[row[0]+":"+st+":"+sid] = row[16]
					}
				}
			}
		}
		f.Close()
	}
	if f, e := os.Open(spec); e == nil {
		cr := csv.NewReader(f)
		cr.FieldsPerRecord = -1
		cr.Read()
		for {
			row, e := cr.Read()
			if e != nil {
				break
			}
			if len(row) >= 8 {
				r.structure[row[0]+":"+row[7]] = strings.Split(row[3], ";")[0]
			}
		}
		f.Close()
	}
	return r
}

// BuildCanonicalUniversalASTFromParseNode is the common, language-neutral
// bridge from the table parser's neutral reduction tree to the existing UAST
// facts pipeline. It deliberately uses only node shape, fields and source
// spans; language-specific parser branches are not involved.
func BuildCanonicalUniversalASTFromParseNode(language string, root *matrixir.ParseNode) (*UniversalASTDocument, error) {
	if root == nil {
		return nil, fmt.Errorf("nil parse root")
	}
	b := &FrontendFactsBuilder{}
	registry := loadParseProducerRegistry()
	ids := map[*matrixir.ParseNode]int{}
	next := 0
	var visit func(*matrixir.ParseNode, int) error
	visit = func(n *matrixir.ParseNode, parent int) error {
		id := next
		next++
		ids[n] = id
		kind := "OperationExpr"
		cls := registry.bySignature[language+":"+fmt.Sprint(n.ParserState)+":"+fmt.Sprint(n.SymbolID)+":"+fmt.Sprint(n.ProductionID)]
		if cls == "" {
			cls = registry.byStateSymbol[language+":"+fmt.Sprint(n.ParserState)+":"+fmt.Sprint(n.SymbolID)]
		}
		if cls == "" {
			cls = registry.byState[language+":"+fmt.Sprint(n.ParserState)]
		}
		if cls != "" {
			if k := registry.structure[language+":"+cls]; k != "" {
				kind = k
			}
		}
		if parent < 0 {
			kind = "Scope"
		}
		node := UniversalASTNode{ID: id, StructuralKind: kind, SemanticFacets: defaultUniversalFacets(kind), Fields: map[string]json.RawMessage{}}
		mask, err := universalFieldMask(&node)
		if err != nil {
			return err
		}
		node.FieldMask = mask
		for k, v := range map[string]any{"id": id, "symbol_id": n.SymbolID, "production_id": n.ProductionID} {
			if containsString(mask, k) {
				raw, _ := json.Marshal(v)
				node.Fields[k] = raw
			}
		}
		if n.Start >= 0 {
			node.Source = &SemanticSourceSpan{File: language, StartOffset: n.Start, EndOffset: n.End}
			b.AddSource(FrontendSourceFact{NodeID: id, Span: *node.Source})
		}
		b.AddNode(node)
		for ordinal, c := range n.Children {
			if err := visit(c, id); err != nil {
				return err
			}
			cid := ids[c]
			b.AddRole(FrontendRelationFact{Kind: "syntax.child", From: id, To: UniversalASTReference{Domain: "node", ID: fmt.Sprint(cid)}, Role: "child", Ordinal: ordinal})
		}
		return nil
	}
	if err := visit(root, -1); err != nil {
		return nil, err
	}
	f := b.Facts
	f.SchemaVersion = 1
	f.LanguageProfile = language
	f.Projection = "uast"
	f.Evaluation = "eager"
	f.ValueModel = "native"
	f.IndexBase = 0
	return BuildCanonicalUniversalASTFromFrontendFacts(f)
}
