package backend

import (
	"encoding/json"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"reflect"
)

// SemanticDocument is the stable interchange format for SemanticProgram. It
// intentionally contains no Canonical R text and can therefore move between a
// source frontend, a route decoder and every target backend without R carrying
// semantic state. The source-specific parser remains an adapter at the edge.
const SemanticDocumentSchema = "r2many.semantic-program"
const SemanticDocumentVersion = 1

type SemanticDocument struct {
	SchemaVersion int                  `json:"schema_version"`
	Schema        string               `json:"schema"`
	Evaluation    string               `json:"evaluation"`
	ValueModel    string               `json:"value_model"`
	IndexBase     int                  `json:"index_base"`
	Types         SemanticTypeContract `json:"type_contract"`
	Origin        SemanticOrigin       `json:"origin"`
	Metadata      map[string]string    `json:"metadata,omitempty"`
	Extensions    map[string]any       `json:"extensions,omitempty"`
	Contracts     SemanticContracts    `json:"contracts,omitempty"`
	Dialects      []SemanticDialect    `json:"dialects,omitempty"`
	Root          SemanticStatement    `json:"root"`
	Evidence      SemanticEvidence     `json:"evidence"`
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
	ID          int                 `json:"id"`
	Kind        string              `json:"kind"`
	Scope       int                 `json:"scope"`
	Type        SemanticType        `json:"type,omitempty"`
	TypeOrigin  string              `json:"type_origin,omitempty"`
	Semantics   SemanticSemantics   `json:"semantics,omitempty"`
	Effects     []string            `json:"effects,omitempty"`
	Binding     *int                `json:"binding,omitempty"`
	Source      *SemanticSourceSpan `json:"source,omitempty"`
	Attributes  map[string]any      `json:"attributes,omitempty"`
	Extensions  map[string]any      `json:"extensions,omitempty"`
	Name        string              `json:"name,omitempty"`
	Operator    string              `json:"operator,omitempty"`
	LiteralKind string              `json:"literal_kind,omitempty"`
	Text        string              `json:"text,omitempty"`
	Left        *SemanticExpression `json:"left,omitempty"`
	Right       *SemanticExpression `json:"right,omitempty"`
	Value       *SemanticExpression `json:"value,omitempty"`
	Function    *SemanticFunction   `json:"function,omitempty"`
	Arguments   []SemanticArgument  `json:"arguments,omitempty"`
	DoubleIndex bool                `json:"double_index,omitempty"`
}

type SemanticArgument struct {
	Name    string              `json:"name,omitempty"`
	Missing bool                `json:"missing,omitempty"`
	Value   *SemanticExpression `json:"value,omitempty"`
}

type SemanticFunction struct {
	Parameters []SemanticParameter `json:"parameters"`
	Body       SemanticStatement   `json:"body"`
}

type SemanticParameter struct {
	ID      int                 `json:"id"`
	Name    string              `json:"name"`
	Type    SemanticType        `json:"type,omitempty"`
	Passing string              `json:"passing,omitempty"`
	Default *SemanticExpression `json:"default,omitempty"`
}

// SemanticType is recursive so no JSON number has to carry type information.
// The textual literal value remains exact even for values JSON cannot represent.
type SemanticType struct {
	Kind        string          `json:"kind,omitempty"`
	Name        string          `json:"name,omitempty"`
	Bits        int             `json:"bits,omitempty"`
	Signed      *bool           `json:"signed,omitempty"`
	IEEE754     bool            `json:"ieee754,omitempty"`
	Element     *SemanticType   `json:"element,omitempty"`
	Key         *SemanticType   `json:"key,omitempty"`
	Value       *SemanticType   `json:"value,omitempty"`
	Parameters  []SemanticType  `json:"parameters,omitempty"`
	Result      *SemanticType   `json:"result,omitempty"`
	Fields      []SemanticField `json:"fields,omitempty"`
	Length      int             `json:"length,omitempty"`
	Rows        int             `json:"rows,omitempty"`
	Columns     int             `json:"columns,omitempty"`
	Constraints []string        `json:"constraints,omitempty"`
	Nullable    string          `json:"nullable,omitempty"`
	Ownership   string          `json:"ownership,omitempty"`
	Lifetime    string          `json:"lifetime,omitempty"`
	TypeOrigin  string          `json:"type_origin,omitempty"`
}

type SemanticField struct {
	Name string       `json:"name"`
	Type SemanticType `json:"type"`
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
	if p == nil || p.Body == nil {
		return SemanticDocument{}, fmt.Errorf("missing semantic program body")
	}
	if p.Evaluation != "lazy_demand" && p.Evaluation != "eager_left_to_right" {
		return SemanticDocument{}, fmt.Errorf("unknown evaluation contract %q", p.Evaluation)
	}
	if p.ValueModel != "tagged_dynamic_binary64" || p.IndexBase != 1 || !p.Types.valid() {
		return SemanticDocument{}, fmt.Errorf("unmodeled semantic value contract")
	}
	root, err := documentStatement(p.Body)
	if err != nil {
		return SemanticDocument{}, err
	}
	assignDocumentIDs(&root)
	decorateDocument(&root, p.Evidence, p.IndexBase)
	return SemanticDocument{SchemaVersion: SemanticDocumentVersion, Schema: SemanticDocumentSchema, Evaluation: p.Evaluation, ValueModel: p.ValueModel, IndexBase: p.IndexBase, Types: p.Types, Origin: p.Origin, Metadata: p.Metadata, Extensions: p.Extensions, Contracts: p.Contracts, Dialects: p.Dialects, Root: root, Evidence: p.Evidence}, nil
}

func (p *SemanticProgram) MarshalSemanticJSON() ([]byte, error) {
	doc, err := p.Document()
	if err != nil {
		return nil, err
	}
	return json.Marshal(doc)
}

func ParseSemanticJSON(data []byte) (*SemanticProgram, error) {
	var doc SemanticDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("semantic document JSON: %w", err)
	}
	return ParseSemanticDocument(doc)
}

func ParseSemanticDocument(doc SemanticDocument) (*SemanticProgram, error) {
	if doc.SchemaVersion != SemanticDocumentVersion || doc.Schema != SemanticDocumentSchema {
		return nil, fmt.Errorf("unsupported semantic document schema %q version %d", doc.Schema, doc.SchemaVersion)
	}
	if doc.Evaluation != "lazy_demand" && doc.Evaluation != "eager_left_to_right" {
		return nil, fmt.Errorf("unknown semantic evaluation contract %q", doc.Evaluation)
	}
	if doc.ValueModel != "tagged_dynamic_binary64" || doc.IndexBase != 1 || !doc.Types.valid() {
		return nil, fmt.Errorf("unmodeled semantic value contract")
	}
	root, err := documentStatementAST(doc.Root)
	if err != nil {
		return nil, err
	}
	body, ok := root.(*BlockStmt)
	if !ok {
		return nil, fmt.Errorf("semantic document root must be a block")
	}
	p := NewSemanticProgram(body, doc.Evaluation)
	p.ValueModel, p.IndexBase, p.Types, p.Origin = doc.ValueModel, doc.IndexBase, doc.Types, doc.Origin
	p.Metadata, p.Extensions, p.Contracts, p.Dialects = doc.Metadata, doc.Extensions, doc.Contracts, doc.Dialects
	if err := validateDialects(p.Dialects); err != nil {
		return nil, err
	}
	if p.Origin.SourceLanguage == "" || p.Origin.EntryPoint == "" {
		return nil, fmt.Errorf("semantic origin missing source language or entry point")
	}
	if err := validateSemanticEvidence(doc.Evidence, p.Evidence); err != nil {
		return nil, err
	}
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
		expr(e.Left)
		expr(e.Right)
		expr(e.Value)
		for i := range e.Arguments {
			expr(e.Arguments[i].Value)
		}
		if e.Function != nil {
			for i := range e.Function.Parameters {
				q := &e.Function.Parameters[i]
				q.Type, _, _, _, _ = nodeInfo(q.ID)
				q.Passing = "unknown"
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
		fn := SemanticFunction{}
		for _, p := range x.Params {
			q := SemanticParameter{Name: p.Name}
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
		return &CallExpr{Fun: f, Args: args, Eager: d.Operator == "eager_left_to_right"}, nil
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
		fn := &FunctionExpr{}
		for _, p := range d.Function.Parameters {
			if p.Name == "" {
				return nil, fmt.Errorf("semantic parameter missing name")
			}
			q := Param{Name: p.Name}
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
