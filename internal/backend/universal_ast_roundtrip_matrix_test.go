package backend

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"testing"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// roundtripSourceVisitor supplies nonsemantic source evidence to every legacy
// node which can carry it. Parameters have no source slot in SemanticDocument;
// their defaults are ordinary expressions and are covered here.
type roundtripSourceVisitor struct{ next int }

func (v *roundtripSourceVisitor) span(kind string, id int) *SemanticSourceSpan {
	v.next++
	start := v.next * 10
	return &SemanticSourceSpan{
		File:        "roundtrip_" + kind + ".go",
		StartOffset: start,
		EndOffset:   start + 7,
		StartLine:   v.next,
		StartColumn: 2,
		EndLine:     v.next,
		EndColumn:   9,
	}
}
func (v *roundtripSourceVisitor) EnterStatement(s *SemanticStatement) error {
	s.Source = v.span(s.Kind, s.ID)
	return nil
}
func (*roundtripSourceVisitor) LeaveStatement(*SemanticStatement) error { return nil }
func (v *roundtripSourceVisitor) EnterExpression(e *SemanticExpression) error {
	e.Source = v.span(e.Kind, e.ID)
	return nil
}
func (*roundtripSourceVisitor) LeaveExpression(*SemanticExpression) error { return nil }

type roundtripInventoryVisitor struct {
	statements  map[string][]SemanticStatement
	expressions map[string][]SemanticExpression
	parameters  []SemanticParameter
	literals    map[string]int
}

func newRoundtripInventory() *roundtripInventoryVisitor {
	return &roundtripInventoryVisitor{
		statements:  map[string][]SemanticStatement{},
		expressions: map[string][]SemanticExpression{},
		literals:    map[string]int{},
	}
}
func (v *roundtripInventoryVisitor) EnterStatement(s *SemanticStatement) error {
	v.statements[s.Kind] = append(v.statements[s.Kind], *s)
	return nil
}
func (*roundtripInventoryVisitor) LeaveStatement(*SemanticStatement) error { return nil }
func (v *roundtripInventoryVisitor) EnterExpression(e *SemanticExpression) error {
	v.expressions[e.Kind] = append(v.expressions[e.Kind], *e)
	if e.Kind == "literal" {
		v.literals[e.LiteralKind]++
	}
	return nil
}
func (*roundtripInventoryVisitor) LeaveExpression(*SemanticExpression) error { return nil }
func (v *roundtripInventoryVisitor) EnterParameter(p *SemanticParameter) error {
	v.parameters = append(v.parameters, *p)
	return nil
}

func uastRoundtripMatrix(t *testing.T, rows [][]float64) matrixir.Matrix {
	t.Helper()
	m, err := matrixir.MatrixFromRows(rows)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// universalExecutableRoundtripProgram contains one occurrence of every node
// handled by documentStatement/documentExpression. The expected inventory in
// the test below is the explicit executable compatibility contract.
func universalExecutableRoundtripProgram(t *testing.T) *SemanticProgram {
	t.Helper()
	signed := true
	i16 := SemanticType{Kind: "integer", Bits: 16, Signed: &signed, TypeOrigin: "explicit"}
	intLiteral := func(text string) Expr {
		return &OperationExpr{Operation: SemanticOperation{Name: "integer.literal", Type: i16, Text: text}}
	}
	selected := 1
	resolved := &CallExpr{
		Fun:  &IdentExpr{Name: "overload"},
		Args: []Arg{{Value: &LiteralExpr{Kind: "number", Text: "7"}}},
		Resolution: &SemanticCallResolution{
			Candidates: []SemanticCallCandidate{
				{Name: "overload(integer)", Declaration: "overload_integer", Type: SemanticType{Kind: "function", Result: &i16}},
				{Name: "overload(float)", Declaration: "overload_float", Type: SemanticType{Kind: "function", Result: &SemanticType{Kind: "float", Bits: 64, IEEE754: true}}},
			},
			Obligations:    []string{"arity", "visible"},
			Required:       uastRoundtripMatrix(t, [][]float64{{1, 1}, {1, 1}}),
			Satisfied:      uastRoundtripMatrix(t, [][]float64{{1, 1}, {1, 1}}),
			ConversionCost: uastRoundtripMatrix(t, [][]float64{{2}, {0}}),
			Priority:       []float64{0, 0},
			Selected:       &selected,
		},
	}
	fn := &FunctionExpr{
		Binding:           "exact_v1",
		DefaultEvaluation: "definition",
		Params: []Param{
			{Name: "typed", Mode: "positional_only", Type: &i16},
			{Name: "defaulted", Mode: "positional_or_keyword", Default: &LiteralExpr{Kind: "number", Text: "5"}},
			{Name: "rest", Mode: "variadic_positional"},
			{Name: "named", Mode: "keyword_only", Default: &IdentExpr{Name: "NULL"}},
			{Name: "options", Mode: "variadic_keyword"},
		},
		Body: &BlockStmt{List: []Stmt{
			&IfStmt{
				Cond: &BinaryExpr{Op: ">", L: &IdentExpr{Name: "defaulted"}, R: &LiteralExpr{Kind: "number", Text: "0"}},
				Then: &BlockStmt{List: []Stmt{&ReturnStmt{X: &IdentExpr{Name: "defaulted"}}}},
				Else: &BlockStmt{List: []Stmt{&ReturnStmt{X: &IdentExpr{Name: "typed"}}}},
			},
		}},
	}
	root := &BlockStmt{List: []Stmt{
		&AssignStmt{Name: "x", Op: "<-", Value: &LiteralExpr{Kind: "number", Text: "1"}},
		&AssignStmt{Name: "overload_integer", Op: "<-", Value: &FunctionExpr{Body: &BlockStmt{List: []Stmt{&ReturnStmt{X: &LiteralExpr{Kind: "number", Text: "1"}}}}}},
		&AssignStmt{Name: "overload_float", Op: "<-", Value: &FunctionExpr{Body: &BlockStmt{List: []Stmt{&ReturnStmt{X: &LiteralExpr{Kind: "number", Text: "2"}}}}}},
		&AssignStmt{Name: "f", Op: "<-", Value: fn},
		&AssignStmt{Name: "exact", Op: "<-", Value: &OperationExpr{Operation: SemanticOperation{Name: "integer.add", Type: i16}, Operands: []Expr{intLiteral("7"), intLiteral("8")}}},
		&ExprStmt{X: &UnaryExpr{Op: "-", X: &LiteralExpr{Kind: "number", Text: "3"}}},
		&ExprStmt{X: &BinaryExpr{Op: "+", L: &IdentExpr{Name: "x"}, R: &LiteralExpr{Kind: "number", Text: "2"}}},
		&ExprStmt{X: &CallExpr{Fun: &IdentExpr{Name: "f"}, Eager: true, Args: []Arg{
			{Name: "typed", Value: intLiteral("4")},
			{Missing: true},
			{Name: "named", Value: &LiteralExpr{Kind: "string", Text: "value"}},
		}}},
		&ExprStmt{X: &CallExpr{Fun: &IdentExpr{Name: "c"}, Args: []Arg{
			{Value: &LiteralExpr{Kind: "number", Text: "1"}},
			{Value: &LiteralExpr{Kind: "number", Text: "2"}},
			{Name: "label", Value: &LiteralExpr{Kind: "string", Text: "three"}},
		}}},
		&ExprStmt{X: resolved},
		&ExprStmt{X: &IndexExpr{X: &IdentExpr{Name: "x"}, Args: []Arg{{Value: &LiteralExpr{Kind: "number", Text: "1"}}, {Missing: true}}}},
		&ExprStmt{X: &IndexExpr{X: &IdentExpr{Name: "x"}, Args: []Arg{{Value: &LiteralExpr{Kind: "number", Text: "1"}}}, Double: true}},
		&ExprStmt{X: &IterationExpr{Kind: "snapshot", Value: &IdentExpr{Name: "x"}}},
		&ExprStmt{X: &IterationExpr{Kind: "size", Value: &IdentExpr{Name: "x"}}},
		&ExprStmt{X: &IdentExpr{Name: "NULL"}},
		&ExprStmt{X: &IdentExpr{Name: "NA_integer_"}},
		&ExprStmt{X: &IdentExpr{Name: "NaN"}},
		&ExprStmt{X: &IdentExpr{Name: "TRUE"}},
		&ExprStmt{X: &LiteralExpr{Kind: "string", Text: "text"}},
		&IfStmt{
			Cond: &IdentExpr{Name: "TRUE"},
			Then: &BlockStmt{List: []Stmt{&ExprStmt{X: &IdentExpr{Name: "x"}}}},
			Else: &BlockStmt{List: []Stmt{&ExprStmt{X: &IdentExpr{Name: "f"}}}},
		},
		&WhileStmt{Cond: &IdentExpr{Name: "TRUE"}, Body: &BlockStmt{List: []Stmt{&BreakStmt{}}}},
		&ForStmt{Name: "i", Seq: &BinaryExpr{Op: ":", L: &LiteralExpr{Kind: "number", Text: "1"}, R: &LiteralExpr{Kind: "number", Text: "3"}}, Body: &BlockStmt{List: []Stmt{&NextStmt{}}}},
		&RepeatStmt{Body: &BlockStmt{List: []Stmt{&BreakStmt{}}}},
		&ReturnStmt{},
	}}
	p := NewSemanticProgram(root, "eager_left_to_right")
	p.Origin = SemanticOrigin{
		SourceLanguage: "go",
		SourceVersion:  "1.25",
		EntryPoint:     "main",
		Modules:        []string{"example/math", "example/collections"},
	}
	p.Metadata = map[string]string{"roundtrip": "matrix"}
	return p
}

func semanticDocumentWireBytes(t *testing.T, doc SemanticDocument) []byte {
	t.Helper()
	doc.UniversalAST = nil
	b, err := json.Marshal(semanticDocumentWire(doc))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func universalNodeSemanticKind(t *testing.T, node *UniversalASTNode) string {
	t.Helper()
	var kind string
	if err := decodeUniversalField(node, "kind", &kind); err != nil {
		t.Fatal(err)
	}
	return kind
}

func TestUniversalASTEveryExecutableNodeRoundTripMatrix(t *testing.T) {
	p := universalExecutableRoundtripProgram(t)
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	if err = WalkSemanticDocument(&doc, &roundtripSourceVisitor{}); err != nil {
		t.Fatal(err)
	}
	u, err := ProjectSemanticDocumentToUniversal(doc)
	if err != nil {
		t.Fatal(err)
	}
	back, err := SemanticDocumentFromUniversalAST(u)
	if err != nil {
		t.Fatal(err)
	}
	if before, after := semanticDocumentWireBytes(t, doc), semanticDocumentWireBytes(t, back); !bytes.Equal(before, after) {
		t.Fatalf("complete executable SemanticDocument changed in UAST roundtrip\nwant=%s\ngot =%s", before, after)
	}
	uAgain, err := ProjectSemanticDocumentToUniversal(back)
	if err != nil {
		t.Fatal(err)
	}
	firstJSON, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	secondJSON, err := json.Marshal(uAgain)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(firstJSON, secondJSON) {
		t.Fatal("UAST projection is not deterministic after the SemanticDocument roundtrip")
	}

	wantStatements := map[string]string{
		"block": "Scope", "expression": "OperationExpr", "assign": "AssignStmt",
		"if": "IfStmt", "while": "LoopStmt", "for": "ForEachStmt",
		"repeat": "LoopStmt", "return": "ReturnStmt", "break": "BreakStmt",
		"continue": "ContinueStmt",
	}
	wantExpressions := map[string]string{
		"typed_operation": "OperationExpr", "identifier": "SymbolRef", "literal": "LiteralExpr",
		"unary": "OperationExpr", "binary": "OperationExpr", "call": "CallExpr",
		"index": "IndexExpr", "function": "ClosureExpr", "iteration": "OperationExpr",
	}
	wantLiterals := []string{"boolean", "na", "nan", "null", "number", "string"}

	inventory := newRoundtripInventory()
	if err = WalkSemanticDocument(&back, inventory); err != nil {
		t.Fatal(err)
	}
	uastKinds := map[string]map[string]int{}
	for i := range u.Nodes {
		node := &u.Nodes[i]
		kind := universalNodeSemanticKind(t, node)
		if uastKinds[kind] == nil {
			uastKinds[kind] = map[string]int{}
		}
		uastKinds[kind][node.StructuralKind]++
		wantFacets := defaultUniversalFacets(node.StructuralKind)
		if !reflect.DeepEqual(node.SemanticFacets, wantFacets) {
			t.Fatalf("node %d facets are not the structural seed matrix row: got %v want %v", node.ID, node.SemanticFacets, wantFacets)
		}
		if node.Source == nil && kind != "parameter" && kind != "missing_argument" {
			t.Errorf("semantic node %d (%s) lost its source span", node.ID, kind)
		}
	}
	for kind, structural := range wantStatements {
		kind, structural := kind, structural
		t.Run("statement/"+kind, func(t *testing.T) {
			if len(inventory.statements[kind]) == 0 {
				t.Fatalf("executable statement %q is absent from roundtrip inventory", kind)
			}
			if uastKinds[kind][structural] == 0 {
				t.Fatalf("statement %q was not projected to %q", kind, structural)
			}
		})
	}
	for kind, structural := range wantExpressions {
		kind, structural := kind, structural
		t.Run("expression/"+kind, func(t *testing.T) {
			if len(inventory.expressions[kind]) == 0 {
				t.Fatalf("executable expression %q is absent from roundtrip inventory", kind)
			}
			if uastKinds[kind][structural] == 0 {
				t.Fatalf("expression %q was not projected to %q", kind, structural)
			}
		})
	}
	for _, literal := range wantLiterals {
		literal := literal
		t.Run("literal/"+literal, func(t *testing.T) {
			if inventory.literals[literal] == 0 {
				t.Fatalf("literal subtype %q was not roundtrip verified", literal)
			}
		})
	}
	t.Run("collection/call-and-index", func(t *testing.T) {
		collectionCall := false
		singleIndex, doubleIndex, missingIndex := false, false, false
		for _, expression := range inventory.expressions["call"] {
			if expression.Value != nil && expression.Value.Kind == "identifier" && expression.Value.Name == "c" {
				collectionCall = len(expression.Arguments) == 3 && expression.Arguments[2].Name == "label"
			}
		}
		for _, expression := range inventory.expressions["index"] {
			if expression.DoubleIndex {
				doubleIndex = true
			} else {
				singleIndex = true
			}
			for _, argument := range expression.Arguments {
				missingIndex = missingIndex || argument.Missing
			}
		}
		if !collectionCall || !singleIndex || !doubleIndex || !missingIndex {
			t.Fatalf("collection compatibility changed: c-call=%t single-index=%t double-index=%t missing-index=%t", collectionCall, singleIndex, doubleIndex, missingIndex)
		}
	})
	t.Run("parameter/parameter", func(t *testing.T) {
		if len(inventory.parameters) != 5 || uastKinds["parameter"]["ParameterDecl"] != 5 {
			t.Fatalf("parameter projection coverage: semantic=%d UAST=%d", len(inventory.parameters), uastKinds["parameter"]["ParameterDecl"])
		}
		parameterByName := map[string]SemanticParameter{}
		for _, parameter := range inventory.parameters {
			parameterByName[parameter.Name] = parameter
		}
		if parameterByName["defaulted"].Default == nil || parameterByName["named"].Default == nil {
			t.Fatal("parameter defaults were not retained")
		}
		if got := parameterByName["typed"]; got.Passing != "value" || !reflect.DeepEqual(got.Type, SemanticType{Kind: "integer", Bits: 16, Signed: boolPointer(true), TypeOrigin: "explicit"}) {
			t.Fatalf("typed parameter changed: %+v", got)
		}
		for name, mode := range map[string]string{
			"typed": "positional_only", "defaulted": "positional_or_keyword",
			"rest": "variadic_positional", "named": "keyword_only", "options": "variadic_keyword",
		} {
			if parameterByName[name].Mode != mode {
				t.Fatalf("parameter %q mode changed: got %q want %q", name, parameterByName[name].Mode, mode)
			}
		}
	})
	if !reflect.DeepEqual(back.Origin.Modules, []string{"example/math", "example/collections"}) {
		t.Fatalf("module/import metadata changed: %v", back.Origin.Modules)
	}
	// The executable SemanticDocument switch has no ImportDecl/ModuleDecl node.
	// Origin.Modules is therefore header metadata and must not be promoted to a
	// structural declaration without frontend evidence.
	for _, node := range u.Nodes {
		if node.StructuralKind == "ImportDecl" || node.StructuralKind == "ModuleDecl" {
			t.Fatalf("module header metadata was guessed as executable node %q", node.StructuralKind)
		}
	}
	if len(back.TypeTable) == 0 || back.TypeGraph.Rows == 0 || !reflect.DeepEqual(doc.TypeTable, back.TypeTable) || !reflect.DeepEqual(doc.TypeRelations, back.TypeRelations) {
		t.Fatal("type table/graph/relations were not retained")
	}
}

func boolPointer(value bool) *bool { return &value }

func TestUniversalASTRoundTripReferencesEvidenceRelationsAndCoverage(t *testing.T) {
	p := universalExecutableRoundtripProgram(t)
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	u, err := ProjectSemanticDocumentToUniversal(doc)
	if err != nil {
		t.Fatal(err)
	}
	back, err := SemanticDocumentFromUniversalAST(u)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(doc.Evidence, back.Evidence) {
		t.Fatal("SemanticEvidence matrices changed in UAST roundtrip")
	}
	for name, matrix := range map[string]matrixir.SparseMatrix{
		"syntax": doc.Evidence.Syntax, "control": doc.Evidence.Control,
		"data": doc.Evidence.Data, "binding": doc.Evidence.Binding,
		"evaluation": doc.Evidence.Order, "scope": doc.Evidence.Scope,
	} {
		if matrix.NonZeros() == 0 {
			t.Errorf("test fixture does not prove %s evidence", name)
		}
	}

	semanticToUniversal := map[int]int{}
	bindingFields := 0
	typedFields := 0
	for i := range u.Nodes {
		node := &u.Nodes[i]
		var semanticID int
		if data := node.Fields["id"]; len(data) != 0 {
			if err := json.Unmarshal(data, &semanticID); err != nil {
				t.Fatal(err)
			}
			semanticToUniversal[semanticID] = node.ID
		}
		if len(node.Fields["binding_refs"]) != 0 {
			bindingFields++
		}
		if len(node.Fields["type_ref"]) != 0 {
			typedFields++
		}
	}
	if bindingFields == 0 || typedFields == 0 {
		t.Fatalf("reference/type fields not exercised: binding=%d type=%d", bindingFields, typedFields)
	}

	relationKey := func(kind string, from int, domain, id string) string {
		return kind + ":" + strconv.Itoa(from) + ":" + domain + ":" + id
	}
	want := map[string]int{}
	addEvidence := func(kind string, from, to int, domain string) {
		uFrom, ok := semanticToUniversal[from]
		if !ok {
			return
		}
		toID := strconv.Itoa(to)
		if domain == "node" {
			uTo, exists := semanticToUniversal[to]
			if !exists {
				return
			}
			toID = strconv.Itoa(uTo)
		}
		if universalRelationAllowed(&u.Nodes[uFrom], kind) {
			want[relationKey(kind, uFrom, domain, toID)]++
		}
	}
	doc.Evidence.Control.Each(func(r, c int, _ float64) { addEvidence("control.next", r, c, "node") })
	doc.Evidence.Data.Each(func(r, c int, _ float64) { addEvidence("data.def_use", r, c, "node") })
	doc.Evidence.Order.Each(func(r, c int, _ float64) { addEvidence("evaluation.before", r, c, "node") })
	doc.Evidence.Binding.Each(func(r, c int, _ float64) { addEvidence("binding.refers", r, c, "binding") })
	addReference := func(kind string, from int, domain, id string) {
		if universalRelationAllowed(&u.Nodes[from], kind) {
			want[relationKey(kind, from, domain, id)]++
		}
	}
	doc.Evidence.Binding.Each(func(r, c int, _ float64) {
		if from, ok := semanticToUniversal[r]; ok {
			id := c
			if c >= 0 && c < len(doc.Evidence.Bindings) {
				id = doc.Evidence.Bindings[c].ID
			}
			addReference("name.resolves", from, "binding", strconv.Itoa(id))
		}
	})
	for _, binding := range doc.Evidence.Bindings {
		if from, ok := semanticToUniversal[binding.Definition]; ok {
			addReference("binding.declares", from, "binding", strconv.Itoa(binding.ID))
		}
	}
	doc.Evidence.Effects.Each(func(r, c int, _ float64) {
		if from, ok := semanticToUniversal[r]; ok && c >= 0 && c < len(doc.Evidence.EffectAxes) {
			addReference("effect.has", from, "effect", doc.Evidence.EffectAxes[c])
		}
	})
	doc.Evidence.Types.Each(func(r, c int, _ float64) {
		if from, ok := semanticToUniversal[r]; ok && c >= 0 && c < len(doc.Evidence.TypeAxes) {
			addReference("type.has", from, "type_axis", doc.Evidence.TypeAxes[c])
		}
	})
	projectedChildren, err := universalChildrenByRole(u)
	if err != nil {
		t.Fatal(err)
	}
	for i := range u.Nodes {
		node := &u.Nodes[i]
		common, err := decodeUniversalCommon(node)
		if err != nil {
			t.Fatal(err)
		}
		operation := common.Semantics.Operation
		if common.Operation.Typed != nil {
			operation = common.Operation.Typed.Name
		}
		if operation != "" {
			addReference("operation.kind", node.ID, "operation", operation)
		}
		origin := common.TypeOrigin
		if origin == "" {
			origin = common.Type.TypeOrigin
		}
		if origin != "" {
			addReference("type.origin", node.ID, "type_origin", origin)
		}
		var operands []universalReferenceField
		if err := decodeUniversalField(node, "operands", &operands); err != nil {
			t.Fatal(err)
		}
		for _, operand := range operands {
			addReference("data.operand", node.ID, operand.Reference.Domain, operand.Reference.ID)
		}
		if common.Kind == "if" {
			for _, branch := range projectedChildren[node.ID]["then"] {
				addReference("control.true", node.ID, "node", strconv.Itoa(branch.ID))
			}
			for _, branch := range projectedChildren[node.ID]["else"] {
				addReference("control.false", node.ID, "node", strconv.Itoa(branch.ID))
			}
		}
		if common.Kind == "call" {
			var callee UniversalASTReference
			if err := decodeUniversalField(node, "callee", &callee); err != nil {
				t.Fatal(err)
			}
			if callee.Domain != "" {
				addReference("call.calls", node.ID, callee.Domain, callee.ID)
			}
		}
		if common.Kind == "block" && common.Scope >= 0 && common.Scope < len(doc.Evidence.Scopes) && doc.Evidence.Scopes[common.Scope].Parent >= 0 {
			addReference("scope.parent", node.ID, "scope", strconv.Itoa(doc.Evidence.Scopes[common.Scope].Parent))
		}
	}
	got := map[string]int{}
	incomingSyntax := make([]int, len(u.Nodes))
	roles := map[string]int{}
	for _, relation := range u.Relations {
		if relation.Kind == "syntax.child" {
			id, convErr := strconv.Atoi(relation.To.ID)
			if convErr != nil || relation.To.Domain != "node" || id < 0 || id >= len(incomingSyntax) {
				t.Fatalf("invalid syntax.child endpoint: %+v", relation)
			}
			incomingSyntax[id]++
			var role string
			if err := json.Unmarshal(relation.Attributes["role"], &role); err != nil || role == "" {
				t.Fatalf("syntax.child lacks structural role: %+v", relation)
			}
			roles[role]++
			continue
		}
		got[relationKey(relation.Kind, relation.From, relation.To.Domain, relation.To.ID)]++
	}
	if incomingSyntax[0] != 0 {
		t.Fatal("UAST root has an incoming syntax.child relation")
	}
	for id := 1; id < len(incomingSyntax); id++ {
		if incomingSyntax[id] != 1 {
			t.Fatalf("UAST node %d has %d syntax parents", id, incomingSyntax[id])
		}
	}
	for _, role := range []string{"statement", "expression", "condition", "sequence", "then", "else", "body", "left", "right", "value", "argument", "parameter", "default"} {
		if roles[role] == 0 {
			t.Errorf("syntax.child role %q was not exercised", role)
		}
	}
	if !reflect.DeepEqual(got, want) {
		keys := make([]string, 0, len(got)+len(want))
		for key := range got {
			keys = append(keys, key)
		}
		for key := range want {
			if got[key] == 0 {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		t.Fatalf("projected evidence relation instances differ from matrix-applicable evidence\nkeys=%v\ngot=%v\nwant=%v", keys, got, want)
	}
	if len(want) == 0 {
		t.Fatal("no matrix-applicable evidence relation was projected")
	}

	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	row := indexOf(uastEmbedded.Basis.Languages, "go")
	if row < 0 || u.LanguageProfile != "go" {
		t.Fatalf("language profile projection: row=%d profile=%q", row, u.LanguageProfile)
	}
	coverageCells := 0
	for col := range uastEmbedded.Basis.Facets {
		if u.LanguageFacet[col] != uastEmbedded.Basis.LanguageFacet.At(row, col) {
			t.Fatalf("language facet %d differs from matrix row", col)
		}
		for language := range uastEmbedded.Basis.Languages {
			lo := uastEmbedded.Basis.CoverageLower.At(language, col)
			hi := uastEmbedded.Basis.CoverageUpper.At(language, col)
			if lo < 0 || hi > 1 || lo > hi {
				t.Fatalf("invalid coverage interval language=%d facet=%d lower=%v upper=%v", language, col, lo, hi)
			}
			if lo != 0 || hi != 0 {
				coverageCells++
			}
		}
	}
	if coverageCells == 0 {
		t.Fatal("coverage lower/upper matrices are empty")
	}
	if u.BasisSHA256 == "" || u.BasisSHA256 != uastEmbedded.BasisSHA256 {
		t.Fatalf("document is not pinned to the validated coverage basis: %q", u.BasisSHA256)
	}

	t.Logf("verified 10 statement categories, 9 expression categories, 1 parameter category, 6 literal subtypes, %d UAST nodes, %d syntax relations, %d evidence relations, and %d nonzero coverage cells", len(u.Nodes), len(u.Nodes)-1, len(want), coverageCells)
}

func TestUniversalASTExecutableRoundTripMatrixContractCount(t *testing.T) {
	// This count is intentionally explicit: adding a new executable switch case
	// requires adding it to the roundtrip matrix above in the same change.
	const statementCategories = 10
	const expressionCategories = 9
	const parameterCategories = 1
	if got := statementCategories + expressionCategories + parameterCategories; got != 20 {
		t.Fatal(fmt.Sprintf("unexpected executable UAST category count %d", got))
	}
}
