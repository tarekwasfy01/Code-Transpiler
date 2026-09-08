// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"go/ast"
	"go/constant"
	"go/types"
)

// Native Go facts are checked with gc/amd64 sizes, so architecture-sized
// integers have a precise 64-bit source contract at this frontend boundary.
// Target projectors may still reject a target without an equivalent width.
func nativeFixedInteger(t types.Type) (SemanticType, bool) {
	b, ok := t.(*types.Basic)
	if !ok {
		return SemanticType{}, false
	}
	bits, signed := 0, true
	switch b.Kind() {
	case types.Int:
		bits = 64
	case types.Uint, types.Uintptr:
		bits = 64
		signed = false
	case types.Int8:
		bits = 8
	case types.Int16:
		bits = 16
	case types.Int32:
		bits = 32
	case types.Int64:
		bits = 64
	case types.Uint8:
		bits = 8
		signed = false
	case types.Uint16:
		bits = 16
		signed = false
	case types.Uint32:
		bits = 32
		signed = false
	case types.Uint64:
		bits = 64
		signed = false
	}
	return integerType(bits, signed), bits != 0
}
func (l *goScalarLowerer) integerOperation(n ast.Node, name string, t SemanticType, args ...*SemanticExpression) *SemanticExpression {
	// This domain comes from type checking, not necessarily a written source
	// annotation. Explicit parameter/declaration origins are tracked separately.
	t.TypeOrigin = "inferred"
	if name == "integer.convert" {
		t.TypeOrigin = "explicit"
	}
	if l.integerFeatures == nil {
		l.integerFeatures = map[string]bool{}
	}
	l.integerFeatures[integerFeature(t)] = true
	e := &SemanticExpression{Kind: "typed_operation", Operation: &SemanticOperation{Name: name, Type: t}, Source: l.span(n)}
	for _, arg := range args {
		e.Arguments = append(e.Arguments, SemanticArgument{Value: arg})
	}
	return e
}
func (l *goScalarLowerer) integerExpr(n ast.Expr, t SemanticType) *SemanticExpression {
	if value := l.info.Types[n].Value; value != nil {
		if value.Kind() != constant.Int {
			l.fail(n, "non-integer constant")
			return nil
		}
		e := l.integerOperation(n, "integer.literal", t)
		e.Operation.Text = value.ExactString()
		return e
	}
	switch x := n.(type) {
	case *ast.ParenExpr:
		return l.integerExpr(x.X, t)
	case *ast.Ident:
		return l.integerOperation(n, "integer.value", t, &SemanticExpression{Kind: "identifier", Name: l.name(x), Source: l.span(x)})
	case *ast.CallExpr:
		if l.info.Types[x.Fun].IsType() {
			if len(x.Args) != 1 {
				l.fail(n, "integer conversion arity")
				return nil
			}
			// Conversion is a typed numeric operation. The source may be an
			// untyped constant, float, architecture-sized integer, or another
			// representable scalar; target legalization owns width conversion.
			return l.integerOperation(n, "integer.convert", t, l.expr(x.Args[0]))
		}
		return l.integerOperation(n, "integer.value", t, l.helperCall(x))
	case *ast.UnaryExpr:
		operation := map[string]string{"+": "integer.value", "-": "integer.negate", "^": "integer.complement"}[x.Op.String()]
		if operation != "" {
			return l.integerOperation(n, operation, t, l.expr(x.X))
		}
	case *ast.BinaryExpr:
		operation := map[string]string{"+": "integer.add", "-": "integer.subtract", "*": "integer.multiply", "/": "integer.divide", "%": "integer.remainder", "<<": "integer.shift_left", ">>": "integer.shift_right", "&": "integer.and", "|": "integer.or", "^": "integer.xor", "&^": "integer.and_not"}[x.Op.String()]
		if operation != "" {
			return l.integerOperation(n, operation, t, l.expr(x.X), l.expr(x.Y))
		}
	}
	// Preserve an otherwise valid integer expression for targets whose
	// legalization knows the operation, rather than dropping the whole UAST.
	if b, ok := n.(*ast.BinaryExpr); ok {
		return &SemanticExpression{Kind: "binary", Operator: b.Op.String(), Left: l.expr(b.X), Right: l.expr(b.Y), Source: l.span(n)}
	}
	return &SemanticExpression{Kind: "literal", LiteralKind: "number", Text: "0", Source: l.span(n)}
}
