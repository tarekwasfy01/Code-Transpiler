package backend

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// FrontendSemanticFacts is the short-lived, language-neutral hand-off from a
// frontend analyser to the Universal AST builder. It is deliberately not an
// intermediate representation: it contains only facts proved by one frontend
// and is discarded once the canonical UniversalASTDocument is built.
//
// Nodes and relations use the public UAST schema types so that the schema,
// field-mask and relation-matrix validators remain the single authority.
type FrontendSemanticFacts struct {
	SchemaVersion          int
	BasisSHA256            string
	SemanticDocumentSHA256 string
	LanguageProfile        string
	LanguageFacet          []float64
	Projection             string
	Evaluation             string
	ValueModel             string
	IndexBase              int
	Types                  SemanticTypeContract
	Origin                 SemanticOrigin
	Metadata               map[string]string
	Extensions             map[string]any
	Contracts              SemanticContracts
	Dialects               []SemanticDialect
	SemanticFeatures       *SemanticFeatureModel
	TypeTable              []SemanticTypeDefinition
	TypeGraph              matrixir.SparseMatrix
	TypeRelations          *SemanticTypeRelations
	Surface                *UniversalASTSurface

	Nodes     []UniversalASTNode
	Fields    []FrontendFieldFact
	Sources   []FrontendSourceFact
	TypesFact []FrontendTypeFact
	Symbols   []FrontendSymbolFact
	Bindings  []FrontendRelationFact
	Relations []FrontendRelationFact
	Returns   []FrontendReturnFact
	Evidence  SemanticEvidence

	// LanguageFacts is reserved for frontend-local, proved data. It is never
	// emitted unless it is represented by a permitted UAST field, facet or
	// relation above.
	LanguageFacts map[string]json.RawMessage
}

type FrontendFieldFact struct {
	NodeID int
	Name   string
	Value  json.RawMessage
}

type FrontendSourceFact struct {
	NodeID int
	Span   SemanticSourceSpan
}

type FrontendTypeFact struct {
	NodeID int
	Type   SemanticType
}

type FrontendSymbolFact struct {
	NodeID int
	Name   string
	Kind   string
}

type FrontendRelationFact struct {
	Kind        string
	From        int
	To          UniversalASTReference
	Role        string
	Ordinal     int
	EvidenceRef int
	Attributes  map[string]json.RawMessage
}

type FrontendReturnFact struct {
	ReturnNodeID   int
	FunctionNodeID int
	ValueNodeID    int
	Type           SemanticType
}

// FrontendFactSink is the language-neutral output boundary for a parser core.
// It records only proved frontend facts; it never owns a second AST.
type FrontendFactSink interface {
	AddNode(UniversalASTNode)
	AddField(FrontendFieldFact)
	AddSource(FrontendSourceFact)
	AddRole(FrontendRelationFact)
	AddOperand(FrontendRelationFact)
	AddSymbol(FrontendSymbolFact)
	AddBinding(FrontendRelationFact)
	AddRelation(FrontendRelationFact)
}

// FrontendFactsBuilder is the default in-memory sink used only until the
// shared facts-to-UAST builder consumes the collected frontend output.
type FrontendFactsBuilder struct{ Facts FrontendSemanticFacts }

func (b *FrontendFactsBuilder) AddNode(v UniversalASTNode) { b.Facts.Nodes = append(b.Facts.Nodes, v) }
func (b *FrontendFactsBuilder) AddField(v FrontendFieldFact) {
	b.Facts.Fields = append(b.Facts.Fields, v)
}
func (b *FrontendFactsBuilder) AddSource(v FrontendSourceFact) {
	b.Facts.Sources = append(b.Facts.Sources, v)
}
func (b *FrontendFactsBuilder) AddRole(v FrontendRelationFact) {
	b.Facts.Relations = append(b.Facts.Relations, v)
}
func (b *FrontendFactsBuilder) AddOperand(v FrontendRelationFact) {
	b.Facts.Relations = append(b.Facts.Relations, v)
}
func (b *FrontendFactsBuilder) AddSymbol(v FrontendSymbolFact) {
	b.Facts.Symbols = append(b.Facts.Symbols, v)
}
func (b *FrontendFactsBuilder) AddBinding(v FrontendRelationFact) {
	b.Facts.Bindings = append(b.Facts.Bindings, v)
	b.Facts.Relations = append(b.Facts.Relations, v)
}
func (b *FrontendFactsBuilder) AddRelation(v FrontendRelationFact) {
	b.Facts.Relations = append(b.Facts.Relations, v)
}

// BuildRawUniversalASTFromFrontendFacts materializes exactly the facts that a
// frontend proved. It does not infer fields, facets, relations or evidence.
func BuildRawUniversalASTFromFrontendFacts(f FrontendSemanticFacts) (*UniversalASTDocument, error) {
	if len(f.Nodes) == 0 {
		return nil, fmt.Errorf("frontend semantic facts contain no nodes")
	}
	if f.LanguageProfile == "" {
		return nil, fmt.Errorf("frontend semantic facts have no language profile")
	}
	if err := validateFrontendFactIDs(f); err != nil {
		return nil, err
	}
	u := &UniversalASTDocument{
		SchemaVersion: f.SchemaVersion, BasisSHA256: f.BasisSHA256, SemanticDocumentSHA256: f.SemanticDocumentSHA256,
		LanguageProfile: f.LanguageProfile, LanguageFacet: append([]float64(nil), f.LanguageFacet...),
		Projection: f.Projection, Evaluation: f.Evaluation, ValueModel: f.ValueModel,
		IndexBase: f.IndexBase, Types: f.Types, Origin: f.Origin,
		Metadata: cloneStringMap(f.Metadata), Extensions: cloneAnyMap(f.Extensions),
		Contracts: f.Contracts, Dialects: append([]SemanticDialect(nil), f.Dialects...),
		SemanticFeatures: f.SemanticFeatures, TypeTable: append([]SemanticTypeDefinition(nil), f.TypeTable...),
		TypeGraph: f.TypeGraph, TypeRelations: f.TypeRelations, Evidence: f.Evidence,
		Surface: cloneUniversalASTSurface(f.Surface),
	}
	if err := cloneFrontendFactValue(f.Nodes, &u.Nodes); err != nil {
		return nil, err
	}
	if err := cloneFrontendFactValue(f.Relations, &u.Relations); err != nil {
		return nil, err
	}
	return u, nil
}

// BuildCanonicalUniversalASTFromFrontendFacts is the single generic frontend
// route. Evidence is always derived by AnalyzeUniversalEvidence; the frontend
// cannot add guessed evidence through this builder.
func BuildCanonicalUniversalASTFromFrontendFacts(f FrontendSemanticFacts) (*UniversalASTDocument, error) {
	raw, err := BuildRawUniversalASTFromFrontendFacts(f)
	if err != nil {
		return nil, fmt.Errorf("raw UAST materialization: %w", err)
	}
	if err := EnrichUniversalAST(raw); err != nil {
		return nil, fmt.Errorf("UAST enrichment: %w", err)
	}
	evidence, err := AnalyzeUniversalEvidence(raw)
	if err != nil {
		return nil, fmt.Errorf("UAST evidence: %w", err)
	}
	raw.Evidence = evidence
	if err := materializeUniversalEvidenceFields(raw, evidence); err != nil {
		return nil, fmt.Errorf("UAST evidence fields: %w", err)
	}
	// Relations outside syntax.child are canonical projections of evidence and
	// matrix facts. Rebuild them here so a frontend cannot retain a stale
	// compatibility relation view beside the shared evidence result.
	syntaxRelations := raw.Relations[:0]
	for _, relation := range raw.Relations {
		if relation.Kind == "syntax.child" {
			syntaxRelations = append(syntaxRelations, relation)
		}
	}
	raw.Relations = syntaxRelations
	semanticIDs := make(map[int]int, len(raw.Nodes))
	for i := range raw.Nodes {
		common, err := decodeUniversalCommon(&raw.Nodes[i])
		if err != nil {
			return nil, err
		}
		if common.ID >= 0 {
			semanticIDs[common.ID] = raw.Nodes[i].ID
		}
	}
	appendUniversalEvidenceRelations(raw, semanticIDs, evidence)
	// Apply the cached language/relation/composition/dependency closure on the
	// canonical document itself.  This is a validation/metadata pass over the
	// existing UAST graph; it does not introduce another IR or registry.
	if err := ApplySemanticClosure(raw); err != nil {
		return nil, fmt.Errorf("semantic closure: %w", err)
	}
	if err := deriveUniversalTypeTable(raw); err != nil {
		return nil, fmt.Errorf("UAST type derivation: %w", err)
	}
	u, err := NormalizeUniversalAST(raw)
	if err != nil {
		return nil, fmt.Errorf("UAST normalization: %w", err)
	}
	return u, nil
}

// EnrichUniversalAST is the shared, UAST-only semantic enrichment pass. It
// materializes only facts that follow from schema crosswalks and explicit
// syntax roles; frontend-specific parsers and SemanticDocument are absent.
func EnrichUniversalAST(u *UniversalASTDocument) error {
	if err := materializeUniversalCrosswalkFields(u); err != nil {
		return err
	}
	// Derive scope, definition/reference, ordered-sequence and control facts
	// directly from the structured UAST roles. This is the common frontend
	// closure for every language and runs before evidence analysis so the
	// resulting matrices and executable runtime observe the same graph.
	if err := appendFrontendStructuralClosure(u); err != nil {
		return err
	}
	// Operand edges are structural facts consumed by the single evidence pass.
	materializeUniversalOperandFacts(u)
	// Apply the empirically mined presence implications.  This only projects
	// already explicit syntax children into the existing operand relation; it
	// never reconstructs unknown semantic payload.
	ApplyUniversalTruthClosure(u)
	// Apply the generated residual-repair closure after operand projection. The
	// closure only promotes an existing, unambiguous data.operand edge to the
	// canonical syntax role required by the executor; it does not infer values
	// or parse source text.
	ApplyAutomaticSemanticRepairClosure(u)
	// The legacy decoration oracle proves that executable blocks and control
	// constructs transfer control to each of their syntax children. Materialize
	// that exact contract on UAST so the shared evidence pass has no dependency
	// on a Legacy Stmt tree.
	materializeUniversalControlFacts(u)
	return nil
}

func materializeUniversalControlFacts(u *UniversalASTDocument) {
	// Exact mapping table extracted by the Legacy decorator oracle. This is not
	// a structural-kind ranking: absent kinds do not acquire control semantics.
	legacyControlTransferKinds := map[string]bool{
		"block": true, "if": true, "while": true, "for": true, "repeat": true,
	}
	seen := map[string]bool{}
	for _, relation := range u.Relations {
		seen[relation.Kind+":"+strconv.Itoa(relation.From)+":"+relation.To.Domain+":"+relation.To.ID] = true
	}
	controlParent := map[int]*UniversalASTNode{}
	for i := range u.Nodes {
		common, err := decodeUniversalCommon(&u.Nodes[i])
		if err != nil {
			continue
		}
		if legacyControlTransferKinds[common.Kind] {
			controlParent[u.Nodes[i].ID] = &u.Nodes[i]
		}
	}
	for _, relation := range append([]UniversalASTRelation(nil), u.Relations...) {
		parent := controlParent[relation.From]
		if relation.Kind != "syntax.child" || relation.To.Domain != "node" || parent == nil || !universalRelationAllowed(parent, "control.next") {
			continue
		}
		key := "control.next:" + strconv.Itoa(relation.From) + ":node:" + relation.To.ID
		if seen[key] {
			continue
		}
		seen[key] = true
		u.Relations = append(u.Relations, UniversalASTRelation{
			Kind: "control.next", From: relation.From, To: relation.To,
		})
	}
}

func materializeUniversalOperandFacts(u *UniversalASTDocument) {
	children, err := universalChildrenByRole(u)
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, r := range u.Relations {
		seen[r.Kind+":"+strconv.Itoa(r.From)+":"+r.To.ID] = true
	}
	for _, n := range u.Nodes {
		common, err := decodeUniversalCommon(&n)
		if err != nil {
			continue
		}
		roles := []string{}
		switch common.Kind {
		case "assign":
			roles = []string{"expression", "value"}
		case "expression":
			roles = []string{"expression", "operand", "argument", "value"}
		case "for":
			roles = []string{"sequence"}
		case "binary":
			roles = []string{"left", "right"}
		case "unary":
			roles = []string{"value"}
		case "index", "slice":
			roles = []string{"value", "argument"}
		case "call":
			roles = []string{"value", "argument"}
		case "aggregate", "tuple":
			roles = []string{"argument"}
		case "return":
			roles = []string{"expression"}
		}
		for _, role := range roles {
			for _, child := range children[n.ID][role] {
				key := "data.operand:" + strconv.Itoa(n.ID) + ":" + strconv.Itoa(child.ID)
				if !seen[key] {
					seen[key] = true
					u.Relations = append(u.Relations, UniversalASTRelation{Kind: "data.operand", From: n.ID, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(child.ID)}})
				}
			}
		}
	}
}

func materializeUniversalEvidenceFields(u *UniversalASTDocument, e SemanticEvidence) error {
	semanticIDs := map[int]int{}
	for i := range u.Nodes {
		c, err := decodeUniversalCommon(&u.Nodes[i])
		if err != nil {
			return err
		}
		semanticIDs[c.ID] = i
	}
	put := func(n *UniversalASTNode, key string, value any) error {
		if !containsString(n.FieldMask, key) {
			return nil
		}
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		if n.Fields == nil {
			n.Fields = map[string]json.RawMessage{}
		}
		n.Fields[key] = data
		return nil
	}
	for semanticID, nodeIndex := range semanticIDs {
		if semanticID < 0 || semanticID >= len(e.Nodes) {
			continue
		}
		n := &u.Nodes[nodeIndex]
		c, err := decodeUniversalCommon(n)
		if err != nil {
			return err
		}
		// Frontend-proved types are stronger than the common evidence axes. The
		// latter fill an absent/unknown type but must never erase an exact native
		// integer, parameter, return or aggregate contract.
		typ := c.Type
		if typ.Kind == "" || typ.Kind == "unknown" {
			typ = SemanticType{Kind: "unknown", TypeOrigin: "unknown"}
			for col, axis := range e.TypeAxes {
				if e.Types.At(semanticID, col) != 0 {
					typ = semanticTypeForAxis(axis)
					break
				}
			}
		}
		if c.Kind == "literal" {
			switch c.Operation.LiteralKind {
			case "number":
				typ = SemanticType{Kind: "float", Bits: 64, IEEE754: true, TypeOrigin: "inferred"}
			case "string":
				typ = SemanticType{Kind: "string", TypeOrigin: "inferred"}
			case "boolean":
				typ = SemanticType{Kind: "boolean", TypeOrigin: "inferred"}
			}
		}
		if err = put(n, "type_ref", typ); err != nil {
			return err
		}
		if err = put(n, "type_origin", typ.TypeOrigin); err != nil {
			return err
		}
		effects := []string{}
		for col, axis := range e.EffectAxes {
			if e.Effects.At(semanticID, col) != 0 {
				effects = append(effects, axis)
			}
		}
		if len(effects) > 0 {
			if err = put(n, "effects", effects); err != nil {
				return err
			}
		}
		refs := []int{}
		for col := range e.Bindings {
			if e.Binding.At(semanticID, col) != 0 {
				refs = append(refs, col)
			}
		}
		if len(refs) > 0 {
			if err = put(n, "binding_refs", refs); err != nil {
				return err
			}
		}
		op := c.Operation
		op.Semantics.Confidence = "exact"
		if c.Kind == "parameter" && op.ParameterPassing == "" {
			op.ParameterPassing = "unknown"
		}
		if err = put(n, "operation", op); err != nil {
			return err
		}
		if c.Kind == "parameter" && op.ParameterPassing != "" {
			if err = put(n, "ownership", universalOwnershipField{ParameterPassing: op.ParameterPassing, TypeOwnership: typ.Ownership}); err != nil {
				return err
			}
		}
	}
	return nil
}

func deriveUniversalTypeTable(u *UniversalASTDocument) error {
	types := map[string]SemanticType{}
	for i := range u.Nodes {
		c, err := decodeUniversalCommon(&u.Nodes[i])
		if err != nil {
			return err
		}
		if c.Type.Kind == "" {
			continue
		}
		raw, err := json.Marshal(c.Type)
		if err != nil {
			return err
		}
		types[string(raw)] = c.Type
	}
	keys := make([]string, 0, len(types))
	for key := range types {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	u.TypeTable = make([]SemanticTypeDefinition, len(keys))
	for i, key := range keys {
		u.TypeTable[i] = SemanticTypeDefinition{ID: i, Type: types[key]}
	}
	u.TypeGraph = matrixir.NewSparseMatrix(len(keys), len(keys))
	ids := map[string]int{}
	for i, key := range keys {
		ids[key] = i
	}
	// SemanticTypeRelations' JSON occurrence paths are a legacy document view
	// (class C). The parent/child type incidence is canonical (class A) and is
	// represented once in the UAST TypeGraph.
	typeEdges := []SemanticTypeEdge{}
	typeParents, typeChildren := []int{}, []int{}
	for parent, entry := range u.TypeTable {
		for _, child := range semanticTypeChildren(&entry.Type) {
			encoded, err := json.Marshal(*child.Type)
			if err != nil {
				return err
			}
			childID, ok := ids[string(encoded)]
			if !ok {
				return fmt.Errorf("UAST child type missing from type table")
			}
			u.TypeGraph.Set(parent, childID, 1)
			typeEdges = append(typeEdges, child.SemanticTypeEdge)
			typeParents = append(typeParents, parent)
			typeChildren = append(typeChildren, childID)
		}
	}
	occurrences, useIDs := []string{}, []int{}
	for _, n := range u.Nodes {
		c, err := decodeUniversalCommon(&n)
		if err != nil {
			return err
		}
		if c.Type.Kind == "" {
			continue
		}
		encoded, err := json.Marshal(c.Type)
		if err != nil {
			return err
		}
		id, ok := ids[string(encoded)]
		if !ok {
			return fmt.Errorf("UAST node type missing from type table")
		}
		occurrences = append(occurrences, "/nodes/"+strconv.Itoa(n.ID)+"/type")
		useIDs = append(useIDs, id)
	}
	relations := &SemanticTypeRelations{Occurrences: occurrences, Edges: typeEdges, UsageCounts: make([]int, len(keys))}
	relations.Uses = matrixir.NewSparseMatrix(len(useIDs), len(keys))
	for row, id := range useIDs {
		relations.Uses.Set(row, id, 1)
		relations.UsageCounts[id]++
	}
	relations.Parents = matrixir.NewSparseMatrix(len(typeParents), len(keys))
	relations.Children = matrixir.NewSparseMatrix(len(typeChildren), len(keys))
	for row := range typeParents {
		relations.Parents.Set(row, typeParents[row], 1)
		relations.Children.Set(row, typeChildren[row], 1)
	}
	var err error
	relations.Nominal, err = deriveNominalRelations(u.TypeTable)
	if err != nil {
		return err
	}
	relations.Equivalence, err = deriveTypeEquivalence(u.TypeTable)
	if err != nil {
		return err
	}
	u.TypeRelations = relations
	return nil
}

// frontendSemanticFactsFromUniversalAST converts a frontend's proved raw UAST
// payload to the shared transient contract. This is an adapter at the
// frontend boundary, not a second semantic representation.
func frontendSemanticFactsFromUniversalAST(u *UniversalASTDocument, languageFacts map[string]json.RawMessage) (FrontendSemanticFacts, error) {
	if u == nil {
		return FrontendSemanticFacts{}, fmt.Errorf("frontend raw UAST is nil")
	}
	f := FrontendSemanticFacts{
		SchemaVersion: u.SchemaVersion, BasisSHA256: u.BasisSHA256, SemanticDocumentSHA256: u.SemanticDocumentSHA256,
		LanguageProfile: u.LanguageProfile, LanguageFacet: append([]float64(nil), u.LanguageFacet...),
		Projection: u.Projection, Evaluation: u.Evaluation, ValueModel: u.ValueModel,
		IndexBase: u.IndexBase, Types: u.Types, Origin: u.Origin,
		Metadata: cloneStringMap(u.Metadata), Extensions: cloneAnyMap(u.Extensions),
		Contracts: u.Contracts, Dialects: append([]SemanticDialect(nil), u.Dialects...),
		SemanticFeatures: u.SemanticFeatures, TypeTable: append([]SemanticTypeDefinition(nil), u.TypeTable...),
		TypeGraph: u.TypeGraph, TypeRelations: u.TypeRelations, Evidence: u.Evidence,
		Surface:       cloneUniversalASTSurface(u.Surface),
		LanguageFacts: cloneRawMessageMap(languageFacts),
	}
	if err := cloneFrontendFactValue(u.Nodes, &f.Nodes); err != nil {
		return FrontendSemanticFacts{}, err
	}
	if err := cloneFrontendFactValue(u.Relations, &f.Relations); err != nil {
		return FrontendSemanticFacts{}, err
	}
	for _, node := range f.Nodes {
		if node.Source != nil {
			f.Sources = append(f.Sources, FrontendSourceFact{NodeID: node.ID, Span: *node.Source})
		}
		keys := make([]string, 0, len(node.Fields))
		for name := range node.Fields {
			keys = append(keys, name)
		}
		sort.Strings(keys)
		for _, name := range keys {
			f.Fields = append(f.Fields, FrontendFieldFact{NodeID: node.ID, Name: name, Value: append(json.RawMessage(nil), node.Fields[name]...)})
		}
		common, err := decodeUniversalCommon(&node)
		if err != nil {
			return FrontendSemanticFacts{}, err
		}
		if common.Type.Kind != "" {
			f.TypesFact = append(f.TypesFact, FrontendTypeFact{NodeID: node.ID, Type: common.Type})
		}
		if common.Name != "" {
			f.Symbols = append(f.Symbols, FrontendSymbolFact{NodeID: node.ID, Name: common.Name, Kind: common.Kind})
		}
		if common.Kind == "return" {
			f.Returns = append(f.Returns, FrontendReturnFact{ReturnNodeID: node.ID, Type: common.Type})
		}
	}
	for _, relation := range f.Relations {
		fact := FrontendRelationFact{Kind: relation.Kind, From: relation.From, To: relation.To, Attributes: cloneRawMessageMap(relation.Attributes)}
		if relation.Kind == "binding.declares" || relation.Kind == "binding.refers" || relation.Kind == "name.resolves" {
			f.Bindings = append(f.Bindings, fact)
		}
	}
	return f, nil
}

func validateFrontendFactIDs(f FrontendSemanticFacts) error {
	seen := make(map[int]bool, len(f.Nodes))
	for _, node := range f.Nodes {
		if node.ID < 0 || seen[node.ID] {
			return fmt.Errorf("frontend semantic facts have an invalid or duplicate node ID %d", node.ID)
		}
		seen[node.ID] = true
	}
	for _, relation := range f.Relations {
		if !seen[relation.From] {
			return fmt.Errorf("frontend relation %q has unknown source node %d", relation.Kind, relation.From)
		}
		if relation.To.Domain == "node" {
			target, err := strconv.Atoi(relation.To.ID)
			if err != nil || !seen[target] {
				return fmt.Errorf("frontend relation %q has unknown target node %q", relation.Kind, relation.To.ID)
			}
		}
	}
	return nil
}

func cloneFrontendFactValue(in, out any) error {
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func cloneRawMessageMap(in map[string]json.RawMessage) map[string]json.RawMessage {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]json.RawMessage, len(in))
	for key, value := range in {
		out[key] = append(json.RawMessage(nil), value...)
	}
	return out
}

func cloneUniversalASTSurface(in *UniversalASTSurface) *UniversalASTSurface {
	if in == nil {
		return nil
	}
	out := *in
	return &out
}
