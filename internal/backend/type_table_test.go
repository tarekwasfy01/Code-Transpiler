package backend

import (
	"encoding/json"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"reflect"
	"testing"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

func TestTypeProjectionCapturesDefaultsAndOperationDomains(t *testing.T) {
	root := &SemanticStatement{Expression: &SemanticExpression{Function: &SemanticFunction{
		Parameters: []SemanticParameter{{Default: &SemanticExpression{Type: SemanticType{Kind: "string"}, Operation: &SemanticOperation{Type: SemanticType{Kind: "integer", Bits: 16}}}}},
	}}}
	table, _, err := deriveTypeTable(root)
	if err != nil || len(table) != 2 {
		t.Fatalf("table=%v err=%v", table, err)
	}
	cyclic := &SemanticType{Kind: "pointer"}
	cyclic.Element = cyclic
	root.Type = *cyclic
	if _, _, err := deriveTypeTable(root); err == nil {
		t.Fatal("cyclic input accepted")
	}
}

func TestTypeGraphWithoutTableRejected(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{}, "eager_left_to_right")
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	doc.TypeTable = nil
	doc.TypeGraph = matrixir.NewSparseMatrix(1, 1)
	if _, err := ParseSemanticDocument(doc); err == nil {
		t.Fatal("orphan graph accepted")
	}
	doc.TypeGraph = matrixir.SparseMatrix{}
	doc.TypeRelations = nil
	doc.UniversalAST = nil // explicit one-time import of the legacy v1 shape
	if _, err := ParseSemanticDocument(doc); err != nil {
		t.Fatalf("legacy document: %v", err)
	}
}

func TestNativeGenericTypeProjection(t *testing.T) {
	fset := gotoken.NewFileSet()
	file, err := goparser.ParseFile(fset, "types.go", `package example
 type Number interface { ~int | ~int64 }
 type Reader interface { Read() string }
 type Box[T Number] struct { Value T; Next *Box[T] }
 var B Box[int64]
 `, 0)
	if err != nil {
		t.Fatal(err)
	}
	pkg, err := (&types.Config{}).Check("example", fset, []*ast.File{file}, nil)
	if err != nil {
		t.Fatal(err)
	}
	box := nativeGoType(pkg.Scope().Lookup("B").Type(), map[types.Type]bool{})
	if len(box.TypeArguments) != 1 || box.TypeArguments[0].Bits != 64 || len(box.TypeParameters) != 1 {
		t.Fatalf("generic type: %+v", box)
	}
	constraint := box.TypeParameters[0].Constraint
	if constraint == nil || constraint.Element == nil || len(constraint.Element.Embedded) != 1 {
		t.Fatalf("constraint: %+v", constraint)
	}
	union := constraint.Element.Embedded[0]
	if union.Kind != "union" || len(union.Terms) != 2 || !union.Terms[0].Underlying {
		t.Fatalf("union: %+v", union)
	}
	reader := nativeGoType(pkg.Scope().Lookup("Reader").Type(), map[types.Type]bool{})
	if len(reader.Element.Methods) != 1 || reader.Element.Methods[0].Name != "Read" {
		t.Fatalf("methods: %+v", reader)
	}
	root := &SemanticStatement{Type: box, Statements: []SemanticStatement{{Type: reader}}}
	table, graph, err := deriveTypeTable(root)
	if err != nil || graph.NonZeros() == 0 {
		t.Fatalf("projection: %v", err)
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	var restored SemanticStatement
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	again, edges, err := deriveTypeTable(&restored)
	if err != nil || !reflect.DeepEqual(table, again) || !reflect.DeepEqual(graph, edges) {
		t.Fatal("type projection changed after JSON roundtrip")
	}
}

func TestSemanticTypeTableAndGraphAreDerived(t *testing.T) {
	signed := true
	tuple := SemanticType{Kind: "tuple", Parameters: []SemanticType{{Kind: "integer", Bits: 64, Signed: &signed}, {Kind: "string"}}, TypeOrigin: "explicit"}
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{&AssignStmt{Name: "pair", Op: "<-", Value: &LiteralExpr{Kind: "number", Text: "1"}}}}, "eager_left_to_right")
	p.Origin.SourceLanguage, p.Origin.EntryPoint = "go", "main"
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	root := doc.Root
	root.Type = tuple
	table, graph, err := deriveTypeTable(&root)
	if err != nil {
		t.Fatal(err)
	}
	if len(table) < 5 || graph.NonZeros() != 2 {
		t.Fatalf("table=%v graph=%v", table, graph)
	}
	if _, err := ParseSemanticDocument(doc); err != nil {
		t.Fatal(err)
	}
	doc.TypeTable[0].Type.Kind = "corrupt"
	if _, err := ParseSemanticDocument(doc); err == nil {
		t.Fatal("modified type table accepted")
	}
}
