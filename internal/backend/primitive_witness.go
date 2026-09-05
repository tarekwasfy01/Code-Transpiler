package backend

import "fmt"

// CanonicalPrimitiveWitness is a generated, target-neutral UAST witness for a
// semantic primitive family. It deliberately starts below the source frontend:
// it exists to validate Primitive × Target backend cells which do not yet have
// a source-language occurrence. The resulting document is built by the normal
// SemanticProgram -> Canonical UAST projection and is then consumed by the
// unchanged primitive compiler, legalizer and emitters.
//
// For parameterized evidence entries the witness proves the selected generic
// kernel and its operand wiring. It never claims that a VM/ISA spelling is a
// distinct source-language operation.
type CanonicalPrimitiveWitness struct {
	Primitive string
	Kernel    string
	Program   *SemanticProgram
}

// BuildCanonicalPrimitiveWitness constructs the smallest executable canonical
// UAST shape for the already-registered primitive kernel. It is intentionally
// data-driven through GenericAtomicKernel; no source or target language selects
// a special path here.
func BuildCanonicalPrimitiveWitness(primitive string) (CanonicalPrimitiveWitness, error) {
	kernel, ok := GenericAtomicKernel(primitive)
	if !ok {
		return CanonicalPrimitiveWitness{}, fmt.Errorf("primitive %q has no registered generic kernel", primitive)
	}
	number := func(v string) Expr { return &LiteralExpr{Kind: "number", Text: v} }
	boolean := func(v string) Expr { return &IdentExpr{Name: v} }
	integer := func(v string) Expr { return &LiteralExpr{Kind: "integer", Text: v} }
	var root *BlockStmt
	switch kernel {
	case "BINARY":
		root = &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: &BinaryExpr{Op: "+", L: number("1"), R: number("2")}}}}
	case "SHIFT":
		root = &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: &BinaryExpr{Op: "<<", L: integer("1"), R: integer("2")}}}}
	case "COMPARE":
		root = &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: &BinaryExpr{Op: "==", L: number("1"), R: number("1")}}}}
	case "LOGICAL_BINARY":
		root = &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: &BinaryExpr{Op: "&&", L: boolean("TRUE"), R: boolean("FALSE")}}}}
	case "LOGICAL_UNARY":
		root = &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: &UnaryExpr{Op: "!", X: boolean("TRUE")}}}}
	case "CALL":
		root = &BlockStmt{List: []Stmt{&ExprStmt{X: &CallExpr{Fun: &IdentExpr{Name: "print"}, Args: []Arg{{Value: number("1")}}}}}}
	case "REDUCE":
		root = &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: &CallExpr{Fun: &IdentExpr{Name: "sum"}, Args: []Arg{{Value: &CallExpr{Fun: &IdentExpr{Name: "c"}, Args: []Arg{{Value: number("1")}, {Value: number("2")}}}}}}}}}
	case "COLLECTION", "ALLOCATION":
		root = &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: &CallExpr{Fun: &IdentExpr{Name: "c"}, Args: []Arg{{Value: number("1")}, {Value: number("2")}}}}}}
	case "INDEX":
		root = &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: &IndexExpr{X: &CallExpr{Fun: &IdentExpr{Name: "c"}, Args: []Arg{{Value: number("1")}, {Value: number("2")}}}, Args: []Arg{{Value: number("1")}}}}}}
	case "ITERATION":
		root = &BlockStmt{List: []Stmt{&ForStmt{Name: "item", Seq: &CallExpr{Fun: &IdentExpr{Name: "c"}, Args: []Arg{{Value: number("1")}}}, Body: &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: &IdentExpr{Name: "item"}}}}}}}
	case "CONTROL", "EXCEPTION":
		// The binding belongs to the enclosing scope.  This is the canonical
		// representation needed by block-scoped targets (C/C++/Rust/Java), not
		// a target-specific declaration workaround.
		root = &BlockStmt{List: []Stmt{
			&AssignStmt{Name: "result", Op: "<-", Value: number("0")},
			&IfStmt{Cond: boolean("TRUE"), Then: &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: number("1")}}}, Else: &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: number("0")}}}},
		}}
	case "BINDING", "MEMBER", "MODULE", "CONVERSION", "CONSTANT", "LITERAL":
		root = &BlockStmt{List: []Stmt{&AssignStmt{Name: "result", Op: "<-", Value: number("1")}}}
	default:
		return CanonicalPrimitiveWitness{}, fmt.Errorf("primitive %q has unsupported registered kernel %q", primitive, kernel)
	}
	p := NewSemanticProgram(root, "eager_left_to_right")
	p.Origin = SemanticOrigin{SourceLanguage: "go", SourceVersion: "canonical-witness", EntryPoint: "main"}
	p.Metadata = map[string]string{"witness.primitive": primitive, "witness.kernel": kernel, "witness.kind": "canonical.generated"}
	if _, err := p.Document(); err != nil {
		return CanonicalPrimitiveWitness{}, fmt.Errorf("canonical witness %s: %w", primitive, err)
	}
	return CanonicalPrimitiveWitness{Primitive: primitive, Kernel: kernel, Program: p}, nil
}
