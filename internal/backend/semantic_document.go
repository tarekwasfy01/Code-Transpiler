package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"io"
	"reflect"
	"sort"
)

// SemanticDocument is the stable interchange format for SemanticProgram. It
// intentionally contains no Canonical R text and can therefore move between a
// source frontend, a route decoder and every target backend without R carrying
// semantic state. The source-specific parser remains an adapter at the edge.
const SemanticDocumentSchema = "r2many.semantic-program"
const SemanticDocumentVersion = 1

type SemanticDocument struct {
	SchemaVersion    int                      `json:"schema_version"`
	Schema           string                   `json:"schema"`
	Evaluation       string                   `json:"evaluation"`
	ValueModel       string                   `json:"value_model"`
	IndexBase        int                      `json:"index_base"`
	Types            SemanticTypeContract     `json:"type_contract"`
	Origin           SemanticOrigin           `json:"origin"`
	Metadata         map[string]string        `json:"metadata,omitempty"`
	Extensions       map[string]any           `json:"extensions,omitempty"`
	Contracts        SemanticContracts        `json:"contracts,omitempty"`
	Dialects         []SemanticDialect        `json:"dialects,omitempty"`
	SemanticFeatures *SemanticFeatureModel    `json:"semantic_features,omitempty"`
	UniversalAST     *UniversalASTDocument    `json:"universal_ast,omitempty"`
	TypeTable        []SemanticTypeDefinition `json:"type_table,omitempty"`
	TypeGraph        matrixir.SparseMatrix    `json:"type_graph,omitempty"`
	TypeRelations    *SemanticTypeRelations   `json:"type_relations,omitempty"`
	Root             SemanticStatement        `json:"root"`
	Evidence         SemanticEvidence         `json:"evidence"`
}

// SemanticTypeDefinition gives structural types a stable document-local ID.
// IDs are assigned from canonical JSON order, never from source traversal.
type SemanticTypeDefinition struct {
	ID   int          `json:"id"`
	Type SemanticType `json:"type"`
}

type SemanticStatement struct {
	ID         int                 `json:"id"`
	Kind       string              `json:"kind"`
	Scope      int                 `json:"scope"`
	Type       SemanticType        `json:"type,omitempty"`
	TypeOrigin string              `json:"type_origin,omitempty"`
	Semantics  SemanticSemantics   `json:"semantics,omitempty"`
	Effects    []string            `json:"effects,omitempty"`
	Source     *SemanticSourceSpan `json:"source,omitempty"`
	Attributes map[string]any      `json:"attributes,omitempty"`
	Extensions map[string]any      `json:"extensions,omitempty"`
	Name       string              `json:"name,omitempty"`
	AssignOp   string              `json:"assign_op,omitempty"`
	Expression *SemanticExpression `json:"expression,omitempty"`
	Condition  *SemanticExpression `json:"condition,omitempty"`
	Sequence   *SemanticExpression `json:"sequence,omitempty"`
	Then       *SemanticStatement  `json:"then,omitempty"`
	Else       *SemanticStatement  `json:"else,omitempty"`
	Body       *SemanticStatement  `json:"body,omitempty"`
	Statements []SemanticStatement `json:"statements,omitempty"`
}

type SemanticExpression struct {
	Operation   *SemanticOperation      `json:"typed_operation,omitempty"`
	ID          int                     `json:"id"`
	Kind        string                  `json:"kind"`
	Scope       int                     `json:"scope"`
	Type        SemanticType            `json:"type,omitempty"`
	TypeOrigin  string                  `json:"type_origin,omitempty"`
	Semantics   SemanticSemantics       `json:"semantics,omitempty"`
	Effects     []string                `json:"effects,omitempty"`
	Binding     *int                    `json:"binding,omitempty"`
	Source      *SemanticSourceSpan     `json:"source,omitempty"`
	Attributes  map[string]any          `json:"attributes,omitempty"`
	Extensions  map[string]any          `json:"extensions,omitempty"`
	Name        string                  `json:"name,omitempty"`
	Operator    string                  `json:"operator,omitempty"`
	LiteralKind string                  `json:"literal_kind,omitempty"`
	Text        string                  `json:"text,omitempty"`
	Left        *SemanticExpression     `json:"left,omitempty"`
	Right       *SemanticExpression     `json:"right,omitempty"`
	Value       *SemanticExpression     `json:"value,omitempty"`
	Function    *SemanticFunction       `json:"function,omitempty"`
	Arguments   []SemanticArgument      `json:"arguments,omitempty"`
	Resolution  *SemanticCallResolution `json:"call_resolution,omitempty"`
	DoubleIndex bool                    `json:"double_index,omitempty"`
}

// SemanticCallResolution records frontend overload/binder output as matrices.
// Rows are candidates; Required and Satisfied columns are obligations, while
// ConversionCost columns correspond to call arguments. Selected is accepted
// only when it is the unique lexicographic minimum derived from these planes.
type SemanticCallResolution struct {
	Candidates     []SemanticCallCandidate `json:"candidates"`
	Obligations    []string                `json:"obligations"`
	Required       matrixir.Matrix         `json:"required"`
	Satisfied      matrixir.Matrix         `json:"satisfied"`
	ConversionCost matrixir.Matrix         `json:"conversion_cost"`
	Priority       []float64               `json:"priority"`
	Selected       *int                    `json:"selected"`
}

type SemanticCallCandidate struct {
	Name        string       `json:"name"`
	Declaration string       `json:"declaration"`
	Type        SemanticType `json:"type,omitempty"`
}

type SemanticArgument struct {
	Name    string              `json:"name,omitempty"`
	Missing bool                `json:"missing,omitempty"`
	Value   *SemanticExpression `json:"value,omitempty"`
}

type SemanticFunction struct {
	Binding           string              `json:"binding,omitempty"`
	DefaultEvaluation string              `json:"default_evaluation,omitempty"`
	Parameters        []SemanticParameter `json:"parameters"`
	Body              SemanticStatement   `json:"body"`
}

type SemanticParameter struct {
	Mode    string              `json:"mode,omitempty"`
	ID      int                 `json:"id"`
	Name    string              `json:"name"`
	Type    SemanticType        `json:"type,omitempty"`
	Passing string              `json:"passing,omitempty"`
	Default *SemanticExpression `json:"default,omitempty"`
}

// SemanticType is recursive so no JSON number has to carry type information.
// The textual literal value remains exact even for values JSON cannot represent.
type SemanticType struct {
	Identity       string             `json:"identity,omitempty"`
	Reference      bool               `json:"reference,omitempty"`
	Kind           string             `json:"kind,omitempty"`
	Name           string             `json:"name,omitempty"`
	Bits           int                `json:"bits,omitempty"`
	Signed         *bool              `json:"signed,omitempty"`
	IEEE754        bool               `json:"ieee754,omitempty"`
	Element        *SemanticType      `json:"element,omitempty"`
	Key            *SemanticType      `json:"key,omitempty"`
	Value          *SemanticType      `json:"value,omitempty"`
	Parameters     []SemanticType     `json:"parameters,omitempty"`
	Result         *SemanticType      `json:"result,omitempty"`
	Fields         []SemanticField    `json:"fields,omitempty"`
	Length         int                `json:"length,omitempty"`
	Rows           int                `json:"rows,omitempty"`
	Columns        int                `json:"columns,omitempty"`
	Constraints    []string           `json:"constraints,omitempty"`
	Nullable       string             `json:"nullable,omitempty"`
	Ownership      string             `json:"ownership,omitempty"`
	Lifetime       string             `json:"lifetime,omitempty"`
	TypeOrigin     string             `json:"type_origin,omitempty"`
	TypeParameters []SemanticType     `json:"type_parameters,omitempty"`
	TypeArguments  []SemanticType     `json:"type_arguments,omitempty"`
	Constraint     *SemanticType      `json:"constraint,omitempty"`
	Embedded       []SemanticType     `json:"embedded,omitempty"`
	Methods        []SemanticField    `json:"methods,omitempty"`
	Terms          []SemanticTypeTerm `json:"terms,omitempty"`
}

// SemanticTypeTerm distinguishes an exact type from its underlying-type set.
type SemanticTypeTerm struct {
	Type       SemanticType `json:"type"`
	Underlying bool         `json:"underlying,omitempty"`
}

type SemanticField struct {
	Name string       `json:"name"`
	Type SemanticType `json:"type"`
}

// deriveTypeTable projects every type occurrence to a canonical structural
// table and records parent-to-child type edges. It is deliberately derived
// from HIR data, so a frontend cannot claim a type graph through Extensions.
func deriveTypeTable(root *SemanticStatement) ([]SemanticTypeDefinition, matrixir.SparseMatrix, error) {
	// Reject cyclic API input before recursive traversal. Recursive source types
	// use finite named references, not cyclic Go pointers.
	if _, err := json.Marshal(root); err != nil {
		return nil, matrixir.SparseMatrix{}, fmt.Errorf("invalid type projection input: %w", err)
	}
	types := map[string]SemanticType{}
	edges := map[[2]string]bool{}
	var addType func(SemanticType) (string, error)
	addType = func(t SemanticType) (string, error) {
		if t.Kind == "" {
			return "", nil
		}
		encoded, err := json.Marshal(t)
		if err != nil {
			return "", err
		}
		key := string(encoded)
		if _, exists := types[key]; exists {
			return key, nil
		}
		types[key] = t
		childKeys := []string{}
		for _, child := range semanticTypeChildren(&t) {
			childKey, err := addType(*child.Type)
			if err != nil {
				return "", err
			}
			if childKey != "" {
				childKeys = append(childKeys, childKey)
			}
		}
		for _, childKey := range childKeys {
			edges[[2]string{key, childKey}] = true
		}
		return key, nil
	}
	var expression func(*SemanticExpression) error
	var statement func(*SemanticStatement) error
	expression = func(e *SemanticExpression) error {
		if e == nil {
			return nil
		}
		if _, err := addType(e.Type); err != nil {
			return err
		}
		if e.Operation != nil {
			if _, err := addType(e.Operation.Type); err != nil {
				return err
			}
		}
		if e.Function != nil {
			for _, p := range e.Function.Parameters {
				if _, err := addType(p.Type); err != nil {
					return err
				}
				if err := expression(p.Default); err != nil {
					return err
				}
			}
			if err := statement(&e.Function.Body); err != nil {
				return err
			}
		}
		if e.Resolution != nil {
			for _, candidate := range e.Resolution.Candidates {
				if _, err := addType(candidate.Type); err != nil {
					return err
				}
			}
		}
		if err := expression(e.Left); err != nil {
			return err
		}
		if err := expression(e.Right); err != nil {
			return err
		}
		if err := expression(e.Value); err != nil {
			return err
		}
		for _, a := range e.Arguments {
			if err := expression(a.Value); err != nil {
				return err
			}
		}
		return nil
	}
	statement = func(s *SemanticStatement) error {
		if s == nil {
			return nil
		}
		if _, err := addType(s.Type); err != nil {
			return err
		}
		if err := expression(s.Expression); err != nil {
			return err
		}
		if err := expression(s.Condition); err != nil {
			return err
		}
		if err := expression(s.Sequence); err != nil {
			return err
		}
		if err := statement(s.Then); err != nil {
			return err
		}
		if err := statement(s.Else); err != nil {
			return err
		}
		if err := statement(s.Body); err != nil {
			return err
		}
		for i := range s.Statements {
			if err := statement(&s.Statements[i]); err != nil {
				return err
			}
		}
		return nil
	}
	if err := statement(root); err != nil {
		return nil, matrixir.SparseMatrix{}, err
	}
	keys := make([]string, 0, len(types))
	for key := range types {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	ids := map[string]int{}
	table := make([]SemanticTypeDefinition, len(keys))
	for i, key := range keys {
		ids[key] = i
		table[i] = SemanticTypeDefinition{ID: i, Type: types[key]}
	}
	graph := matrixir.NewSparseMatrix(len(table), len(table))
	for edge := range edges {
		if from, ok := ids[edge[0]]; ok {
			if to, ok := ids[edge[1]]; ok {
				graph.Set(from, to, 1)
			}
		}
	}
	return table, graph, nil
}

type SemanticSemantics struct {
	Operation       string `json:"operation,omitempty"`
	Dispatch        string `json:"dispatch,omitempty"`
	Overflow        string `json:"overflow,omitempty"`
	EvaluationOrder string `json:"evaluation_order,omitempty"`
	ShortCircuit    bool   `json:"short_circuit,omitempty"`
	IndexBase       int    `json:"index_base,omitempty"`
	NegativeIndex   string `json:"negative_index,omitempty"`
	OutOfBounds     string `json:"out_of_bounds,omitempty"`
	Slicing         string `json:"slicing,omitempty"`
	ErrorModel      string `json:"error_model,omitempty"`
	Confidence      string `json:"confidence,omitempty"`
}

type SemanticSourceSpan struct {
	File        string `json:"file,omitempty"`
	StartOffset int    `json:"start_offset,omitempty"`
	EndOffset   int    `json:"end_offset,omitempty"`
	StartLine   int    `json:"start_line,omitempty"`
	StartColumn int    `json:"start_column,omitempty"`
	EndLine     int    `json:"end_line,omitempty"`
	EndColumn   int    `json:"end_column,omitempty"`
}

func (p *SemanticProgram) Document() (SemanticDocument, error) {
	if p == nil {
		return SemanticDocument{}, fmt.Errorf("missing semantic program")
	}
	// Once a program owns a UAST, it is the sole semantic truth.  Body and the
	// old document fields are compatibility views and are refreshed from it;
	// legacy mutations are never promoted back into the UAST.
	if p.UniversalAST != nil {
		return p.documentFromCanonicalUniversalAST()
	}
	if p.Body == nil {
		return SemanticDocument{}, fmt.Errorf("missing semantic program body")
	}
	if p.Evaluation != "lazy_demand" && p.Evaluation != "eager_left_to_right" {
		return SemanticDocument{}, fmt.Errorf("unknown evaluation contract %q", p.Evaluation)
	}
	if !validValueContract(p.ValueModel, p.Types) || p.IndexBase != 1 {
		return SemanticDocument{}, fmt.Errorf("unmodeled semantic value contract")
	}
	root, err := documentStatement(p.Body)
	if err != nil {
		return SemanticDocument{}, err
	}
	assignDocumentIDs(&root)
	decorateDocument(&root, p.Evidence, p.IndexBase)
	if len(p.nodeSources) != 0 {
		shape, err := json.Marshal(root)
		if err != nil {
			return SemanticDocument{}, err
		}
		if !bytes.Equal(shape, p.sourceTree) {
			return SemanticDocument{}, fmt.Errorf("source mapping invalidated by tree mutation; remap source spans explicitly")
		}
		container := SemanticDocument{Root: root}
		if err := WalkSemanticDocument(&container, &sourceSpanVisitor{spans: p.nodeSources, restore: true}); err != nil {
			return SemanticDocument{}, err
		}
		root = container.Root
	}
	table, graph, err := deriveTypeTable(&root)
	if err != nil {
		return SemanticDocument{}, err
	}
	relations, err := deriveTypeRelations(&root, table)
	if err != nil {
		return SemanticDocument{}, err
	}
	if err := validateSemanticFeatureModel(p.SemanticFeatures, p.Origin.SourceLanguage); err != nil {
		return SemanticDocument{}, err
	}
	if err := validateUniversalASTDocument(p.UniversalAST); err != nil {
		return SemanticDocument{}, err
	}
	doc := SemanticDocument{SchemaVersion: SemanticDocumentVersion, Schema: SemanticDocumentSchema, Evaluation: p.Evaluation, ValueModel: p.ValueModel, IndexBase: p.IndexBase, Types: p.Types, Origin: p.Origin, Metadata: p.Metadata, Extensions: p.Extensions, Contracts: p.Contracts, Dialects: p.Dialects, SemanticFeatures: p.SemanticFeatures, UniversalAST: p.UniversalAST, TypeTable: table, TypeGraph: graph, TypeRelations: relations, Root: root, Evidence: p.Evidence}
	if err = reconcileUniversalAST(&doc); err != nil {
		return SemanticDocument{}, err
	}
	p.UniversalAST = doc.UniversalAST
	if err = installLegacyProgramView(p, doc); err != nil {
		return SemanticDocument{}, err
	}
	if _, err := validateSignatureContracts(&doc); err != nil {
		return SemanticDocument{}, err
	}
	if _, err := validateCallResolutions(&doc); err != nil {
		return SemanticDocument{}, err
	}
	return doc, nil
}

func (p *SemanticProgram) documentFromCanonicalUniversalAST() (SemanticDocument, error) {
	if err := validateUniversalASTDocument(p.UniversalAST); err != nil {
		return SemanticDocument{}, err
	}
	if p.UniversalAST.Projection != "semantic_document.v1" && p.UniversalAST.Projection != "frontend_facts.v1" {
		return SemanticDocument{}, fmt.Errorf("universal AST payload is canonical but has no SemanticDocument compatibility view")
	}
	// Dialect operations have canonical UAST contracts but no lossless legacy
	// executable-tree representation.  Returning a document here would invite
	// callers to treat the old tree as if it carried these operations.
	if len(p.UniversalAST.Dialects) != 0 {
		return SemanticDocument{}, fmt.Errorf("universal AST dialect contracts have no lossless SemanticDocument compatibility view")
	}
	if err := validateUniversalExecutionContracts(p.UniversalAST); err != nil {
		return SemanticDocument{}, err
	}
	doc, err := SemanticDocumentFromUniversalAST(p.UniversalAST)
	if err != nil {
		return SemanticDocument{}, err
	}
	if err = validateSemanticFeatureModel(doc.SemanticFeatures, doc.Origin.SourceLanguage); err != nil {
		return SemanticDocument{}, err
	}
	if err = validateDialects(doc.Dialects); err != nil {
		return SemanticDocument{}, err
	}
	if _, err = validateSignatureContracts(&doc); err != nil {
		return SemanticDocument{}, err
	}
	if _, err = validateCallResolutions(&doc); err != nil {
		return SemanticDocument{}, err
	}
	if err = installLegacyProgramView(p, doc); err != nil {
		return SemanticDocument{}, err
	}
	return doc, nil
}

// installLegacyProgramView materializes the old executable structs from UAST.
// It is an adapter for existing APIs/backends, never a synchronization path
// back into the canonical representation.
func installLegacyProgramView(p *SemanticProgram, doc SemanticDocument) error {
	root, err := documentStatementAST(doc.Root)
	if err != nil {
		return err
	}
	body, ok := root.(*BlockStmt)
	if !ok {
		return fmt.Errorf("universal AST root is not an executable block")
	}
	p.Body = body
	p.Evaluation, p.ValueModel, p.IndexBase, p.Types, p.Origin = doc.Evaluation, doc.ValueModel, doc.IndexBase, doc.Types, doc.Origin
	p.Metadata, p.Extensions, p.Contracts, p.Dialects = doc.Metadata, doc.Extensions, doc.Contracts, doc.Dialects
	p.SemanticFeatures, p.Evidence = doc.SemanticFeatures, doc.Evidence
	// Preserve source mapping owned by the canonical UAST importer.
	return nil
}

func (p *SemanticProgram) MarshalSemanticJSON() ([]byte, error) {
	// Once the canonical UAST exists, JSON transport must preserve it directly.
	// A SemanticDocument is only a read-only compatibility view and may not
	// discard UAST-only data such as dialect operations.
	if p != nil && p.UniversalAST != nil {
		return p.MarshalUniversalASTJSON()
	}
	doc, err := p.Document()
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

// MarshalUniversalASTJSON serializes the canonical representation directly.
// It also supports richer, currently non-executable UAST documents without
// inventing a misleading SemanticDocument tree beside them.
func (p *SemanticProgram) MarshalUniversalASTJSON() ([]byte, error) {
	if p == nil || p.UniversalAST == nil {
		return nil, fmt.Errorf("semantic program has no universal AST")
	}
	if err := validateUniversalASTDocument(p.UniversalAST); err != nil {
		return nil, err
	}
	return json.Marshal(p.UniversalAST)
}

type semanticDocumentWire SemanticDocument

func (doc SemanticDocument) MarshalJSON() ([]byte, error) {
	copy := doc
	if copy.SchemaVersion == SemanticDocumentVersion && copy.Schema == SemanticDocumentSchema {
		if err := reconcileUniversalAST(&copy); err != nil {
			return nil, err
		}
	}
	return json.Marshal(semanticDocumentWire(copy))
}

func ParseSemanticJSON(data []byte) (*SemanticProgram, error) {
	var shape struct {
		BasisSHA256 string `json:"basis_sha256"`
	}
	if err := json.Unmarshal(data, &shape); err == nil && shape.BasisSHA256 != "" {
		return ParseUniversalASTJSON(data)
	}
	var doc SemanticDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&doc); err != nil {
		return nil, fmt.Errorf("semantic document JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("semantic document JSON: expected exactly one document")
	}
	return ParseSemanticDocument(doc)
}

// ParseUniversalASTJSON imports a canonical UAST without fabricating a second
// legacy tree.  Compatibility views are materialized only when the projection
// proves that they are lossless.
func ParseUniversalASTJSON(data []byte) (*SemanticProgram, error) {
	var u UniversalASTDocument
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&u); err != nil {
		return nil, fmt.Errorf("universal AST JSON: %w", err)
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("universal AST JSON: expected exactly one document")
	}
	if _, err := NormalizeUniversalAST(&u); err != nil {
		return nil, err
	}
	p := &SemanticProgram{UniversalAST: &u}
	if u.Projection == "semantic_document.v1" && u.Metadata["frontend"] != "native-go-uast-v1" {
		// Validate and derive the public compatibility view exactly once. The
		// imported UAST remains the sole semantic representation afterwards.
		if _, err := p.documentFromCanonicalUniversalAST(); err != nil {
			// The compatibility view is optional.  A canonical UAST with data that
			// cannot be represented by SemanticDocument is still a valid direct
			// UAST import and must be retained for direct validation/rejection.
			p.Body = nil
		}
	} else {
		// Do not fabricate or alias any legacy view for a richer UAST.
		// Execution will reject it until a direct lowering exists.
	}
	return p, nil
}

func ParseSemanticDocument(doc SemanticDocument) (*SemanticProgram, error) {
	if _, err := typedRequirements(&doc); err != nil {
		return nil, err
	}
	if doc.SchemaVersion != SemanticDocumentVersion || doc.Schema != SemanticDocumentSchema {
		return nil, fmt.Errorf("unsupported semantic document schema %q version %d", doc.Schema, doc.SchemaVersion)
	}
	if doc.Evaluation != "lazy_demand" && doc.Evaluation != "eager_left_to_right" {
		return nil, fmt.Errorf("unknown semantic evaluation contract %q", doc.Evaluation)
	}
	if !validValueContract(doc.ValueModel, doc.Types) || doc.IndexBase != 1 {
		return nil, fmt.Errorf("unmodeled semantic value contract")
	}
	if err := reconcileUniversalAST(&doc); err != nil {
		return nil, err
	}
	root, err := documentStatementAST(doc.Root)
	if err != nil {
		return nil, err
	}
	body, ok := root.(*BlockStmt)
	if !ok {
		return nil, fmt.Errorf("semantic document root must be a block")
	}
	if doc.TypeRelations != nil || len(doc.TypeTable) != 0 || doc.TypeGraph.Rows != 0 || doc.TypeGraph.Cols != 0 || doc.TypeGraph.NonZeros() != 0 {
		table, graph, err := deriveTypeTable(&doc.Root)
		if err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(table, doc.TypeTable) || !reflect.DeepEqual(graph, doc.TypeGraph) {
			return nil, fmt.Errorf("semantic type table or type graph does not match HIR")
		}
		if doc.TypeRelations != nil {
			relations, err := deriveTypeRelations(&doc.Root, table)
			if err != nil {
				return nil, err
			}
			// Earlier v1 relation documents predate the nominal plane.
			if doc.TypeRelations.Nominal == nil {
				relations.Nominal = nil
			}
			if doc.TypeRelations.Equivalence == nil {
				relations.Equivalence = nil
			}
			if !reflect.DeepEqual(relations, doc.TypeRelations) {
				return nil, fmt.Errorf("semantic type relations do not match HIR")
			}
		}
	}
	p := NewSemanticProgram(body, doc.Evaluation)
	if p.ValueModel == "tagged_exact_scalars_v1" && doc.ValueModel != p.ValueModel {
		return nil, fmt.Errorf("typed operations require exact scalar value contract")
	}
	p.ValueModel, p.IndexBase, p.Types, p.Origin = doc.ValueModel, doc.IndexBase, doc.Types, doc.Origin
	p.Metadata, p.Extensions, p.Contracts, p.Dialects, p.SemanticFeatures = doc.Metadata, doc.Extensions, doc.Contracts, doc.Dialects, doc.SemanticFeatures
	if err := validateSemanticFeatureModel(p.SemanticFeatures, p.Origin.SourceLanguage); err != nil {
		return nil, err
	}
	if err := validateDialects(p.Dialects); err != nil {
		return nil, err
	}
	if p.Origin.SourceLanguage == "" || p.Origin.EntryPoint == "" {
		return nil, fmt.Errorf("semantic origin missing source language or entry point")
	}
	if err := validateSemanticEvidence(doc.Evidence, p.Evidence); err != nil {
		return nil, err
	}
	// Evidence alone cannot detect annotations that the executable AST drops.
	// Until the core can retain these semantics, reject instead of silently
	// replacing explicit types, IDs, binding references or operation contracts.
	reconstructed, err := p.Document()
	if err != nil {
		return nil, err
	}
	// Source spans are nonsemantic metadata, but must survive transport. Work
	// on a deep copy to avoid mutating the caller's document through pointers.
	copyBytes, err := json.Marshal(doc.Root)
	if err != nil {
		return nil, err
	}
	var sourceRoot SemanticStatement
	if err := json.Unmarshal(copyBytes, &sourceRoot); err != nil {
		return nil, err
	}
	container := SemanticDocument{Root: sourceRoot}
	sources := &sourceSpanVisitor{spans: map[int]SemanticSourceSpan{}}
	if err := WalkSemanticDocument(&container, sources); err != nil {
		return nil, err
	}
	inputTree, err := json.Marshal(container.Root)
	if err != nil {
		return nil, err
	}
	outputTree, err := json.Marshal(reconstructed.Root)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(inputTree, outputTree) {
		return nil, fmt.Errorf("semantic executable tree contains inconsistent or unsupported annotations; refusing information loss")
	}
	p.nodeSources, p.sourceTree = sources.spans, outputTree
	p.UniversalAST = doc.UniversalAST
	return p, nil
}

func validateDialects(dialects []SemanticDialect) error {
	for _, dialect := range dialects {
		if dialect.Name == "" {
			return fmt.Errorf("semantic dialect missing name")
		}
		for _, operation := range dialect.Operations {
			if operation.ID == "" || operation.Kind == "" {
				return fmt.Errorf("semantic dialect %q has operation without id or kind", dialect.Name)
			}
		}
	}
	return nil
}

func assignDocumentIDs(root *SemanticStatement) {
	next := 0
	var expr func(*SemanticExpression)
	var stmt func(*SemanticStatement)
	stmt = func(s *SemanticStatement) {
		if s == nil {
			return
		}
		s.ID, next = next, next+1
		expr(s.Expression)
		expr(s.Condition)
		expr(s.Sequence)
		stmt(s.Then)
		stmt(s.Else)
		stmt(s.Body)
		for i := range s.Statements {
			stmt(&s.Statements[i])
		}
	}
	expr = func(e *SemanticExpression) {
		if e == nil {
			return
		}
		e.ID, next = next, next+1
		expr(e.Left)
		expr(e.Right)
		expr(e.Value)
		for i := range e.Arguments {
			expr(e.Arguments[i].Value)
		}
		if e.Function != nil {
			for i := range e.Function.Parameters {
				e.Function.Parameters[i].ID, next = next, next+1
				expr(e.Function.Parameters[i].Default)
			}
			stmt(&e.Function.Body)
		}
	}
	stmt(root)
}

// decorateDocument projects the verified matrix analysis back onto the
// executable tree. A document therefore carries both the tree and the facts
// from which a backend may make a conservative lowering decision.
func decorateDocument(root *SemanticStatement, evidence SemanticEvidence, indexBase int) {
	nodeInfo := func(id int) (SemanticType, string, []string, int, *int) {
		if id < 0 || id >= len(evidence.Nodes) {
			return SemanticType{Kind: "unknown", TypeOrigin: "unknown"}, "unknown", nil, 0, nil
		}
		typ := SemanticType{Kind: "unknown", TypeOrigin: "unknown"}
		for col, axis := range evidence.TypeAxes {
			if evidence.Types.At(id, col) != 0 {
				typ = semanticTypeForAxis(axis)
				break
			}
		}
		var effects []string
		for col, axis := range evidence.EffectAxes {
			if evidence.Effects.At(id, col) != 0 {
				effects = append(effects, axis)
			}
		}
		var binding *int
		for col := range evidence.Bindings {
			if evidence.Binding.At(id, col) != 0 {
				v := col
				binding = &v
				break
			}
		}
		return typ, typ.TypeOrigin, effects, evidence.Nodes[id].Scope, binding
	}
	semantics := func(kind, op string) SemanticSemantics {
		s := SemanticSemantics{Confidence: "exact"}
		switch kind {
		case "binary":
			s.Operation = map[string]string{"+": "add", "-": "subtract", "*": "multiply", "/": "divide", "%%": "remainder", "==": "equal", "!=": "not_equal", "<": "less_than", "<=": "less_or_equal", ">": "greater_than", ">=": "greater_or_equal", "&&": "logical_and", "||": "logical_or"}[op]
			s.Dispatch, s.EvaluationOrder = "builtin", "left_to_right"
			s.ShortCircuit = op == "&&" || op == "||"
		case "unary":
			s.Operation, s.Dispatch = map[string]string{"-": "negate", "+": "identity", "!": "logical_not"}[op], "builtin"
		case "call":
			s.Operation, s.Dispatch, s.EvaluationOrder = "call", "unknown", "source_defined"
		case "index":
			s.Operation, s.IndexBase, s.NegativeIndex, s.OutOfBounds, s.Slicing = "index", indexBase, "unknown", "unknown", "unknown"
		}
		return s
	}
	var expr func(*SemanticExpression)
	var stmt func(*SemanticStatement)
	stmt = func(s *SemanticStatement) {
		if s == nil {
			return
		}
		s.Type, s.TypeOrigin, s.Effects, s.Scope, _ = nodeInfo(s.ID)
		s.Semantics = semantics(s.Kind, s.AssignOp)
		expr(s.Expression)
		if s.Kind == "assign" && s.Expression != nil && s.Expression.Operation != nil {
			s.Type = s.Expression.Operation.resultType()
			s.TypeOrigin = s.Type.TypeOrigin
		}
		expr(s.Condition)
		expr(s.Sequence)
		stmt(s.Then)
		stmt(s.Else)
		stmt(s.Body)
		for i := range s.Statements {
			stmt(&s.Statements[i])
		}
	}
	expr = func(e *SemanticExpression) {
		if e == nil {
			return
		}
		e.Type, e.TypeOrigin, e.Effects, e.Scope, e.Binding = nodeInfo(e.ID)
		e.Semantics = semantics(e.Kind, e.Operator)
		if e.Operation != nil {
			e.Type = e.Operation.resultType()
			e.TypeOrigin = e.Type.TypeOrigin
			e.Semantics = e.Operation.semantics()
		}
		expr(e.Left)
		expr(e.Right)
		expr(e.Value)
		for i := range e.Arguments {
			expr(e.Arguments[i].Value)
		}
		if e.Function != nil {
			for i := range e.Function.Parameters {
				q := &e.Function.Parameters[i]
				if q.Passing != "value" {
					q.Type, _, _, _, _ = nodeInfo(q.ID)
					q.Passing = "unknown"
				}
				expr(q.Default)
			}
			stmt(&e.Function.Body)
		}
	}
	stmt(root)
}

func semanticTypeForAxis(axis string) SemanticType {
	switch axis {
	case "binary64":
		return SemanticType{Kind: "float", Bits: 64, IEEE754: true, TypeOrigin: "inferred"}
	case "string":
		return SemanticType{Kind: "string", TypeOrigin: "inferred"}
	case "boolean":
		return SemanticType{Kind: "boolean", TypeOrigin: "inferred"}
	case "null":
		return SemanticType{Kind: "null", Nullable: "explicit", TypeOrigin: "inferred"}
	case "na":
		return SemanticType{Kind: "na", Nullable: "r_na", TypeOrigin: "inferred"}
	case "nan":
		return SemanticType{Kind: "float", Bits: 64, IEEE754: true, Nullable: "nan", TypeOrigin: "inferred"}
	case "function":
		return SemanticType{Kind: "function", TypeOrigin: "inferred"}
	default:
		return SemanticType{Kind: "unknown", TypeOrigin: "unknown"}
	}
}

func validateSemanticEvidence(got, want SemanticEvidence) error {
	if !reflect.DeepEqual(got.TypeAxes, want.TypeAxes) || !reflect.DeepEqual(got.EffectAxes, want.EffectAxes) || !reflect.DeepEqual(got.CallModeAxes, want.CallModeAxes) || !reflect.DeepEqual(got.ContractAxes, want.ContractAxes) || !reflect.DeepEqual(got.Contract, want.Contract) {
		return fmt.Errorf("semantic evidence axes or contract differ from executable tree")
	}
	if len(got.Nodes) != len(want.Nodes) || len(got.Scopes) != len(want.Scopes) || len(got.Bindings) != len(want.Bindings) {
		return fmt.Errorf("semantic evidence shape differs from executable tree")
	}
	for i := range want.Nodes {
		if got.Nodes[i] != want.Nodes[i] {
			return fmt.Errorf("semantic evidence node %d differs from executable tree", i)
		}
	}
	for i := range want.Scopes {
		if got.Scopes[i] != want.Scopes[i] {
			return fmt.Errorf("semantic evidence scope %d differs from executable tree", i)
		}
	}
	for i := range want.Bindings {
		if got.Bindings[i] != want.Bindings[i] {
			return fmt.Errorf("semantic evidence binding %d differs from executable tree", i)
		}
	}
	if !sameSparse(got.Types, want.Types) || !sameSparse(got.Effects, want.Effects) || !sameSparse(got.Syntax, want.Syntax) || !sameSparse(got.Control, want.Control) || !sameSparse(got.Data, want.Data) || !sameSparse(got.Binding, want.Binding) || !sameSparse(got.Order, want.Order) || !sameSparse(got.CallModes, want.CallModes) || !sameSparse(got.Scope, want.Scope) {
		return fmt.Errorf("semantic evidence relations differ from executable tree")
	}
	return nil
}
func sameSparse(a, b matrixir.SparseMatrix) bool {
	if a.Rows != b.Rows || a.Cols != b.Cols || a.NonZeros() != b.NonZeros() {
		return false
	}
	same := true
	a.Each(func(r, c int, v float64) {
		if b.At(r, c) != v {
			same = false
		}
	})
	return same
}

func (t SemanticTypeContract) valid() bool {
	return t.SchemaVersion == 1 && t.Numeric == "binary64" && t.IntegerWidth == "unknown" && t.Text == "utf8" && t.Truth == "r_compatible" && t.Null == "explicit" && t.Collection == "dynamic_vector" && t.Pointer == "unknown" && t.Ownership == "unknown" && t.ABI == "unknown"
}

func documentStatement(s Stmt) (SemanticStatement, error) {
	if s == nil {
		return SemanticStatement{}, fmt.Errorf("nil semantic statement")
	}
	d := SemanticStatement{}
	switch x := s.(type) {
	case *BlockStmt:
		d.Kind = "block"
		for _, child := range x.List {
			item, err := documentStatement(child)
			if err != nil {
				return d, err
			}
			d.Statements = append(d.Statements, item)
		}
	case *ExprStmt:
		d.Kind = "expression"
		v, err := documentExpression(x.X)
		if err != nil {
			return d, err
		}
		d.Expression = &v
	case *AssignStmt:
		d.Kind, d.Name, d.AssignOp = "assign", x.Name, x.Op
		v, err := documentExpression(x.Value)
		if err != nil {
			return d, err
		}
		d.Expression = &v
	case *IfStmt:
		d.Kind = "if"
		c, err := documentExpression(x.Cond)
		if err != nil {
			return d, err
		}
		d.Condition = &c
		then, err := documentStatement(x.Then)
		if err != nil {
			return d, err
		}
		d.Then = &then
		if x.Else != nil {
			other, err := documentStatement(x.Else)
			if err != nil {
				return d, err
			}
			d.Else = &other
		}
	case *WhileStmt:
		d.Kind = "while"
		c, err := documentExpression(x.Cond)
		if err != nil {
			return d, err
		}
		d.Condition = &c
		b, err := documentStatement(x.Body)
		if err != nil {
			return d, err
		}
		d.Body = &b
	case *ForStmt:
		d.Kind, d.Name = "for", x.Name
		seq, err := documentExpression(x.Seq)
		if err != nil {
			return d, err
		}
		d.Sequence = &seq
		b, err := documentStatement(x.Body)
		if err != nil {
			return d, err
		}
		d.Body = &b
	case *RepeatStmt:
		d.Kind = "repeat"
		b, err := documentStatement(x.Body)
		if err != nil {
			return d, err
		}
		d.Body = &b
	case *ReturnStmt:
		d.Kind = "return"
		if x.X != nil {
			v, err := documentExpression(x.X)
			if err != nil {
				return d, err
			}
			d.Expression = &v
		}
	case *BreakStmt:
		d.Kind = "break"
	case *NextStmt:
		d.Kind = "continue"
	default:
		return d, fmt.Errorf("cannot serialize semantic statement %T", s)
	}
	return d, nil
}

func documentExpression(e Expr) (SemanticExpression, error) {
	if e == nil {
		return SemanticExpression{}, fmt.Errorf("nil semantic expression")
	}
	d := SemanticExpression{}
	switch x := e.(type) {
	case *OperationExpr:
		if err := x.Operation.validate(len(x.Operands)); err != nil {
			return d, err
		}
		d.Kind = "typed_operation"
		op := x.Operation
		d.Operation = &op
		for _, operand := range x.Operands {
			v, err := documentExpression(operand)
			if err != nil {
				return d, err
			}
			d.Arguments = append(d.Arguments, SemanticArgument{Value: &v})
		}
	case *IdentExpr:
		switch x.Name {
		case "NULL":
			d.Kind, d.LiteralKind, d.Text = "literal", "null", x.Name
		case "NA", "NA_integer_", "NA_real_", "NA_character_", "NA_complex_":
			d.Kind, d.LiteralKind, d.Text = "literal", "na", x.Name
		case "NaN":
			d.Kind, d.LiteralKind, d.Text = "literal", "nan", x.Name
		case "TRUE", "FALSE", "T", "F":
			d.Kind, d.LiteralKind, d.Text = "literal", "boolean", x.Name
		default:
			d.Kind, d.Name = "identifier", x.Name
		}
	case *LiteralExpr:
		d.Kind, d.LiteralKind, d.Text = "literal", x.Kind, x.Text
	case *UnaryExpr:
		d.Kind, d.Operator = "unary", x.Op
		v, err := documentExpression(x.X)
		if err != nil {
			return d, err
		}
		d.Value = &v
	case *BinaryExpr:
		d.Kind, d.Operator = "binary", x.Op
		l, err := documentExpression(x.L)
		if err != nil {
			return d, err
		}
		r, err := documentExpression(x.R)
		if err != nil {
			return d, err
		}
		d.Left, d.Right = &l, &r
	case *CallExpr:
		d.Kind = "call"
		d.Resolution = x.Resolution
		if x.Eager {
			d.Operator = "eager_left_to_right"
		}
		f, err := documentExpression(x.Fun)
		if err != nil {
			return d, err
		}
		d.Value = &f
		args, err := documentArguments(x.Args)
		if err != nil {
			return d, err
		}
		d.Arguments = args
	case *IndexExpr:
		d.Kind, d.DoubleIndex = "index", x.Double
		v, err := documentExpression(x.X)
		if err != nil {
			return d, err
		}
		d.Value = &v
		args, err := documentArguments(x.Args)
		if err != nil {
			return d, err
		}
		d.Arguments = args
	case *FunctionExpr:
		d.Kind = "function"
		fn := SemanticFunction{Binding: x.Binding, DefaultEvaluation: x.DefaultEvaluation}
		for _, p := range x.Params {
			q := SemanticParameter{Name: p.Name, Mode: p.Mode}
			if p.Type != nil {
				q.Type = *p.Type
				q.Passing = "value"
			}
			if p.Default != nil {
				v, err := documentExpression(p.Default)
				if err != nil {
					return d, err
				}
				q.Default = &v
			}
			fn.Parameters = append(fn.Parameters, q)
		}
		body, err := documentStatement(x.Body)
		if err != nil {
			return d, err
		}
		fn.Body = body
		d.Function = &fn
	case *IterationExpr:
		d.Kind, d.Operator = "iteration", x.Kind
		v, err := documentExpression(x.Value)
		if err != nil {
			return d, err
		}
		d.Value = &v
	default:
		return d, fmt.Errorf("cannot serialize semantic expression %T", e)
	}
	return d, nil
}

func documentArguments(args []Arg) ([]SemanticArgument, error) {
	out := make([]SemanticArgument, len(args))
	for i, arg := range args {
		out[i].Name, out[i].Missing = arg.Name, arg.Missing
		if !arg.Missing {
			v, err := documentExpression(arg.Value)
			if err != nil {
				return nil, err
			}
			out[i].Value = &v
		}
	}
	return out, nil
}

func documentStatementAST(d SemanticStatement) (Stmt, error) {
	switch d.Kind {
	case "block":
		out := &BlockStmt{}
		for _, item := range d.Statements {
			s, err := documentStatementAST(item)
			if err != nil {
				return nil, err
			}
			out.List = append(out.List, s)
		}
		return out, nil
	case "expression":
		v, err := documentExpressionAST(d.Expression)
		return &ExprStmt{X: v}, err
	case "assign":
		if d.Name == "" {
			return nil, fmt.Errorf("semantic assignment missing name")
		}
		v, err := documentExpressionAST(d.Expression)
		return &AssignStmt{Name: d.Name, Op: d.AssignOp, Value: v}, err
	case "if":
		c, err := documentExpressionAST(d.Condition)
		if err != nil {
			return nil, err
		}
		then, err := documentStatementPointerAST(d.Then)
		if err != nil {
			return nil, err
		}
		var other Stmt
		if d.Else != nil {
			other, err = documentStatementPointerAST(d.Else)
			if err != nil {
				return nil, err
			}
		}
		return &IfStmt{Cond: c, Then: then, Else: other}, nil
	case "while":
		c, err := documentExpressionAST(d.Condition)
		if err != nil {
			return nil, err
		}
		b, err := documentStatementPointerAST(d.Body)
		if err != nil {
			return nil, err
		}
		return &WhileStmt{Cond: c, Body: b}, nil
	case "for":
		if d.Name == "" {
			return nil, fmt.Errorf("semantic for missing name")
		}
		seq, err := documentExpressionAST(d.Sequence)
		if err != nil {
			return nil, err
		}
		b, err := documentStatementPointerAST(d.Body)
		if err != nil {
			return nil, err
		}
		return &ForStmt{Name: d.Name, Seq: seq, Body: b}, nil
	case "repeat":
		b, err := documentStatementPointerAST(d.Body)
		return &RepeatStmt{Body: b}, err
	case "return":
		if d.Expression == nil {
			return &ReturnStmt{}, nil
		}
		v, err := documentExpressionAST(d.Expression)
		return &ReturnStmt{X: v}, err
	case "break":
		return &BreakStmt{}, nil
	case "continue":
		return &NextStmt{}, nil
	default:
		return nil, fmt.Errorf("unknown semantic statement kind %q", d.Kind)
	}
}

func documentStatementPointerAST(d *SemanticStatement) (Stmt, error) {
	if d == nil {
		return nil, fmt.Errorf("semantic statement missing body")
	}
	return documentStatementAST(*d)
}
func documentExpressionAST(d *SemanticExpression) (Expr, error) {
	if d == nil {
		return nil, fmt.Errorf("semantic expression missing")
	}
	switch d.Kind {
	case "typed_operation":
		if d.Operation == nil {
			return nil, fmt.Errorf("missing typed operation")
		}
		if err := d.Operation.validate(len(d.Arguments)); err != nil {
			return nil, err
		}
		x := &OperationExpr{Operation: *d.Operation}
		for _, arg := range d.Arguments {
			if arg.Missing || arg.Name != "" {
				return nil, fmt.Errorf("typed operands must be positional and present")
			}
			v, err := documentExpressionAST(arg.Value)
			if err != nil {
				return nil, err
			}
			x.Operands = append(x.Operands, v)
		}
		return x, nil
	case "identifier":
		if d.Name == "" {
			return nil, fmt.Errorf("semantic identifier missing name")
		}
		return &IdentExpr{Name: d.Name}, nil
	case "literal":
		if d.LiteralKind == "" {
			return nil, fmt.Errorf("semantic literal missing kind")
		}
		switch d.LiteralKind {
		case "null", "na", "nan", "boolean":
			if d.Text == "" {
				return nil, fmt.Errorf("semantic special literal missing exact text")
			}
			return &IdentExpr{Name: d.Text}, nil
		}
		return &LiteralExpr{Kind: d.LiteralKind, Text: d.Text}, nil
	case "unary":
		v, err := documentExpressionAST(d.Value)
		if err != nil {
			return nil, err
		}
		if d.Operator == "" {
			return nil, fmt.Errorf("semantic unary missing operator")
		}
		return &UnaryExpr{Op: d.Operator, X: v}, nil
	case "binary":
		l, err := documentExpressionAST(d.Left)
		if err != nil {
			return nil, err
		}
		r, err := documentExpressionAST(d.Right)
		if err != nil {
			return nil, err
		}
		if d.Operator == "" {
			return nil, fmt.Errorf("semantic binary missing operator")
		}
		return &BinaryExpr{Op: d.Operator, L: l, R: r}, nil
	case "call":
		f, err := documentExpressionAST(d.Value)
		if err != nil {
			return nil, err
		}
		args, err := documentArgumentsAST(d.Arguments)
		if err != nil {
			return nil, err
		}
		if d.Operator != "" && d.Operator != "eager_left_to_right" {
			return nil, fmt.Errorf("unknown semantic call mode %q", d.Operator)
		}
		return &CallExpr{Fun: f, Args: args, Eager: d.Operator == "eager_left_to_right", Resolution: d.Resolution}, nil
	case "index":
		v, err := documentExpressionAST(d.Value)
		if err != nil {
			return nil, err
		}
		args, err := documentArgumentsAST(d.Arguments)
		if err != nil {
			return nil, err
		}
		return &IndexExpr{X: v, Args: args, Double: d.DoubleIndex}, nil
	case "function":
		if d.Function == nil {
			return nil, fmt.Errorf("semantic function missing body")
		}
		fn := &FunctionExpr{Binding: d.Function.Binding, DefaultEvaluation: d.Function.DefaultEvaluation}
		for _, p := range d.Function.Parameters {
			if p.Name == "" {
				return nil, fmt.Errorf("semantic parameter missing name")
			}
			q := Param{Name: p.Name, Mode: p.Mode}
			if p.Passing == "value" {
				if err := (SemanticOperation{Name: "integer.value", Type: p.Type}).validate(1); err != nil {
					return nil, fmt.Errorf("parameter %s: %w", p.Name, err)
				}
				typ := p.Type
				q.Type = &typ
			}
			if p.Default != nil {
				v, err := documentExpressionAST(p.Default)
				if err != nil {
					return nil, err
				}
				q.Default = v
			}
			fn.Params = append(fn.Params, q)
		}
		body, err := documentStatementAST(d.Function.Body)
		if err != nil {
			return nil, err
		}
		b, ok := body.(*BlockStmt)
		if !ok {
			return nil, fmt.Errorf("semantic function body must be block")
		}
		fn.Body = b
		return fn, nil
	case "iteration":
		v, err := documentExpressionAST(d.Value)
		if err != nil {
			return nil, err
		}
		if d.Operator != "snapshot" && d.Operator != "size" {
			return nil, fmt.Errorf("unknown semantic iteration intrinsic %q", d.Operator)
		}
		return &IterationExpr{Kind: d.Operator, Value: v}, nil
	default:
		return nil, fmt.Errorf("unknown semantic expression kind %q", d.Kind)
	}
}
func documentArgumentsAST(args []SemanticArgument) ([]Arg, error) {
	out := make([]Arg, len(args))
	for i, arg := range args {
		out[i].Name, out[i].Missing = arg.Name, arg.Missing
		if arg.Missing {
			if arg.Value != nil {
				return nil, fmt.Errorf("missing semantic argument has value")
			}
			continue
		}
		v, err := documentExpressionAST(arg.Value)
		if err != nil {
			return nil, err
		}
		out[i].Value = v
	}
	return out, nil
}
