package backend

import "fmt"

type NativeGoSourceFact struct {
	ID   int
	Span SemanticSourceSpan
}
type NativeGoTypeFact struct {
	ID   int
	Type SemanticType
}
type NativeGoSymbolFact struct {
	ID         int
	Name, Kind string
}
type NativeGoRelationFact struct {
	Kind        string
	From, To    int
	Role        string
	Ordinal     int
	EvidenceRef int
}
type NativeGoEvidenceFact struct {
	NodeID       int
	Kind, Detail string
}
type NativeGoNodeFact struct {
	ID, Parent                                           int
	Kind, Name, Operator, LiteralKind, LiteralValue      string
	SourceID, TypeID, SymbolID                           int
	Children                                             []int
	Fields                                               map[string]string
	ParameterRefs, ArgumentRefs, OperandRefs, ReturnRefs []int
	CallTargetRef, FunctionRef, ScopeRef                 int
}

// NativeGoSemanticFacts are short-lived frontend facts shared by lowering
// adapters. They are not a second IR and are discarded after construction.
type NativeGoSemanticFacts struct {
	FrontendSemanticFacts
	Root                                                                       *SemanticStatement
	Structure, Fields, Sources, Types, Symbols, Calls, Control, Data, Evidence int
	SourceFacts                                                                []NativeGoSourceFact
	TypeFacts                                                                  []NativeGoTypeFact
	SymbolFacts                                                                []NativeGoSymbolFact
	BindingFacts                                                               []NativeGoRelationFact
	RelationFacts                                                              []NativeGoRelationFact
	EvidenceFacts                                                              []NativeGoEvidenceFact
	Nodes                                                                      []NativeGoNodeFact
	RawUAST                                                                    *UniversalASTDocument
}

func extractNativeGoSemanticFacts(root *SemanticStatement) NativeGoSemanticFacts {
	var f NativeGoSemanticFacts
	f.Root = root
	nextID := 0
	var walkExpr func(*SemanticExpression)
	var walkStmt func(*SemanticStatement)
	walkExpr = func(e *SemanticExpression) {
		if e == nil {
			return
		}
		id := nextID
		nextID++
		n := NativeGoNodeFact{ID: id, Kind: e.Kind, Name: e.Name, Operator: e.Operator, LiteralKind: e.LiteralKind, LiteralValue: e.Text}
		f.Nodes = append(f.Nodes, n)
		f.Fields++
		if e.Source != nil {
			f.Sources++
			f.SourceFacts = append(f.SourceFacts, NativeGoSourceFact{ID: id, Span: *e.Source})
		}
		if e.Type.Name != "" {
			f.Types++
			f.TypeFacts = append(f.TypeFacts, NativeGoTypeFact{ID: id, Type: e.Type})
		}
		if e.Kind == "identifier" {
			f.Symbols++
			f.SymbolFacts = append(f.SymbolFacts, NativeGoSymbolFact{ID: id, Name: e.Name, Kind: "reference"})
			f.BindingFacts = append(f.BindingFacts, NativeGoRelationFact{Kind: "symbol.reference", From: id, To: id})
		}
		if e.Kind == "call" {
			f.Calls++
			f.RelationFacts = append(f.RelationFacts, NativeGoRelationFact{Kind: "call.calls", From: id, To: id})
		}
		if e.Left != nil {
			f.Data++
		}
		if e.Right != nil {
			f.Data++
		}
		walkExpr(e.Left)
		walkExpr(e.Right)
		walkExpr(e.Value)
		for _, a := range e.Arguments {
			walkExpr(a.Value)
		}
		if e.Function != nil {
			for _, p := range e.Function.Parameters {
				f.Symbols++
				if p.Default != nil {
					walkExpr(p.Default)
				}
			}
			walkStmt(&e.Function.Body)
		}
	}
	walkStmt = func(s *SemanticStatement) {
		if s == nil {
			return
		}
		id := nextID
		nextID++
		n := NativeGoNodeFact{ID: id, Kind: s.Kind, Name: s.Name, Operator: s.AssignOp}
		f.Nodes = append(f.Nodes, n)
		f.Structure++
		if s.Source != nil {
			f.Sources++
			f.SourceFacts = append(f.SourceFacts, NativeGoSourceFact{ID: id, Span: *s.Source})
		}
		if s.Type.Name != "" {
			f.Types++
			f.TypeFacts = append(f.TypeFacts, NativeGoTypeFact{ID: id, Type: s.Type})
		}
		if s.Name != "" {
			f.Symbols++
			f.SymbolFacts = append(f.SymbolFacts, NativeGoSymbolFact{ID: id, Name: s.Name, Kind: "declaration"})
			f.BindingFacts = append(f.BindingFacts, NativeGoRelationFact{Kind: "symbol.declaration", From: id, To: id})
		}
		if s.Kind == "return" {
			f.RelationFacts = append(f.RelationFacts, NativeGoRelationFact{Kind: "control.return", From: id, To: id})
			f.EvidenceFacts = append(f.EvidenceFacts, NativeGoEvidenceFact{NodeID: id, Kind: "return", Detail: s.Name})
		}
		if s.Kind == "if" || s.Kind == "for" || s.Kind == "while" || s.Kind == "repeat" {
			f.Control++
			f.EvidenceFacts = append(f.EvidenceFacts, NativeGoEvidenceFact{NodeID: id, Kind: "control", Detail: s.Kind})
		}
		if s.Expression != nil {
			walkExpr(s.Expression)
		}
		for i := range s.Statements {
			walkStmt(&s.Statements[i])
		}
	}
	walkStmt(root)
	if root != nil {
		doc := SemanticDocument{SchemaVersion: SemanticDocumentVersion, Schema: SemanticDocumentSchema, Evaluation: "eager_left_to_right", ValueModel: "tagged_dynamic_binary64", IndexBase: 1, Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "go", EntryPoint: "main"}, Root: *root}
		if u, err := ProjectSemanticDocumentToUniversal(doc); err == nil {
			for i := range u.Nodes {
				if i < len(f.SourceFacts) {
					s := f.SourceFacts[i].Span
					u.Nodes[i].Source = &s
				}
			}
			f.RawUAST = u
			shared, err := frontendSemanticFactsFromUniversalAST(u, nil)
			if err == nil {
				f.FrontendSemanticFacts = shared
			}
		}
	}
	for i := range f.Nodes {
		if f.Nodes[i].Fields == nil {
			f.Nodes[i].Fields = map[string]string{}
		}
		if i > 0 {
			p := (i - 1) / 2
			f.Nodes[i].Parent = f.Nodes[p].ID
			f.Nodes[p].Children = append(f.Nodes[p].Children, f.Nodes[i].ID)
		}
	}
	for i := range f.Nodes {
		if i < len(f.SourceFacts) {
			f.Nodes[i].SourceID = f.SourceFacts[i].ID
		}
		if i < len(f.TypeFacts) {
			f.Nodes[i].TypeID = f.TypeFacts[i].ID
		}
		if i < len(f.SymbolFacts) {
			f.Nodes[i].SymbolID = f.SymbolFacts[i].ID
		}
	}
	f.Evidence = f.Structure + f.Fields + f.Sources + f.Types + f.Symbols + f.Calls + f.Control + f.Data
	return f
}

// materializeLegacyNativeGo exposes the already analysed facts to the legacy
// compatibility layer without re-running semantic analysis.
func materializeLegacyNativeGo(f NativeGoSemanticFacts) (*BlockStmt, error) {
	if f.RawUAST == nil {
		return nil, fmt.Errorf("native Go facts have no root")
	}
	doc, err := SemanticDocumentFromUniversalAST(f.RawUAST)
	if err != nil {
		return nil, err
	}
	v, err := documentStatementAST(doc.Root)
	if err != nil {
		return nil, err
	}
	b, ok := v.(*BlockStmt)
	if !ok {
		return nil, fmt.Errorf("native Go fact root is not a block")
	}
	return b, nil
}

func buildNativeGoUniversalAST(f NativeGoSemanticFacts, metadata map[string]string, extensions map[string]any, contracts SemanticContracts, features *SemanticFeatureModel, evidence SemanticEvidence) (*UniversalASTDocument, error) {
	if len(f.FrontendSemanticFacts.Nodes) == 0 {
		return nil, fmt.Errorf("native Go facts have no shared frontend contract")
	}
	// These arguments are retained for the internal call signature while the
	// shared contract is adopted. The contract header is the single source used
	// by the generic facts-to-UAST route.
	_ = metadata
	_ = extensions
	_ = contracts
	_ = features
	_ = evidence
	return BuildCanonicalUniversalASTFromFrontendFacts(f.FrontendSemanticFacts)
}
