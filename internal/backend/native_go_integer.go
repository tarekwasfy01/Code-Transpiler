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

// nativeGoIRIntegerOperation is the shared contract adapter for Go source
// operators and the corresponding cmd/compile/internal/ir.Op spellings. It
// only covers exact integer-domain operations; OADD, for example, needs the
// resolved type before it may be treated as integer.add rather than float or
// string addition. Unknown opcodes deliberately remain unmapped.
func nativeGoIRIntegerOperation(op string, unary bool) string {
	if unary {
		switch op {
		case "+", "OPLUS":
			return "integer.value"
		case "-", "ONEG":
			return "integer.negate"
		case "^", "OBITNOT":
			return "integer.complement"
		default:
			return ""
		}
	}
	switch op {
	case "+", "OADD":
		return "integer.add"
	case "-", "OSUB":
		return "integer.subtract"
	case "*", "OMUL":
		return "integer.multiply"
	case "/", "ODIV":
		return "integer.divide"
	case "%", "OMOD":
		return "integer.remainder"
	case "<<", "OLSH":
		return "integer.shift_left"
	case ">>", "ORSH":
		return "integer.shift_right"
	case "&", "OAND":
		return "integer.and"
	case "|", "OOR":
		return "integer.or"
	case "^", "OXOR":
		return "integer.xor"
	case "&^", "OANDNOT":
		return "integer.and_not"
	case "==", "OEQ":
		return "integer.equal"
	case "!=", "ONE":
		return "integer.not_equal"
	case "<", "OLT":
		return "integer.less"
	case "<=", "OLE":
		return "integer.less_equal"
	case ">", "OGT":
		return "integer.greater"
	case ">=", "OGE":
		return "integer.greater_equal"
	default:
		return ""
	}
}

func nativeGoIRIntegerComparison(op string) string {
	switch op {
	case "==", "OEQ":
		return "integer.equal"
	case "!=", "ONE":
		return "integer.not_equal"
	case "<", "OLT":
		return "integer.less"
	case "<=", "OLE":
		return "integer.less_equal"
	case ">", "OGT":
		return "integer.greater"
	case ">=", "OGE":
		return "integer.greater_equal"
	default:
		return ""
	}
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
	case *ast.IndexExpr:
		// Keep an integer-valued indexed read as a real canonical IndexExpr.
		// The old scalar fallback returned literal zero here, which discarded the
		// aggregate/index graph before any target backend could project it.
		// A map key is an arbitrary value contract (for example string), not a
		// positional sequence index. Canonical one-based projection applies only
		// to arrays, slices and strings; applying it to map keys turns a valid
		// string constant into the spurious "non-integer constant" diagnostic.
		index := l.expr(x.Index)
		if l.indexIsPositional(x.X) {
			index = l.canonicalGoIndex(x.Index)
		}
		indexed := &SemanticExpression{Kind: "index", Value: l.expr(x.X), Arguments: []SemanticArgument{{Value: index}}, Source: l.span(n)}
		return l.integerOperation(n, "integer.value", t, indexed)
	case *ast.UnaryExpr:
		operation := nativeGoIRIntegerOperation(x.Op.String(), true)
		if operation != "" {
			return l.integerOperation(n, operation, t, l.expr(x.X))
		}
	case *ast.BinaryExpr:
		operation := nativeGoIRIntegerOperation(x.Op.String(), false)
		if operation != "" {
			return l.integerOperation(n, operation, t, l.expr(x.X), l.expr(x.Y))
		}
	}
	// A typed integer expression must never silently become literal zero. The
	// caller routes structurally supported AST forms through expr; reaching this
	// point means the semantic shape has no exact lowering contract yet.
	if b, ok := n.(*ast.BinaryExpr); ok {
		return &SemanticExpression{Kind: "binary", Operator: b.Op.String(), Left: l.expr(b.X), Right: l.expr(b.Y), Source: l.span(n)}
	}
	l.fail(n, "integer expression shape")
	return nil
}

// nativeGoIntegerFastPathExpr reports which AST forms integerExpr lowers
// without losing structure. Other integer-valued expressions must continue
// through expr's structural cases (for example selector, dereference, and
// type assertion) instead of being coerced to the old scalar fallback.
func nativeGoIntegerFastPathExpr(n ast.Expr) bool {
	switch n.(type) {
	case *ast.BasicLit, *ast.ParenExpr, *ast.Ident, *ast.CallExpr, *ast.IndexExpr, *ast.UnaryExpr, *ast.BinaryExpr:
		return true
	default:
		return false
	}
}

func (l *goScalarLowerer) indexIsPositional(base ast.Expr) bool {
	if l == nil || l.info == nil || base == nil {
		return false
	}
	t := l.info.TypeOf(base)
	if t == nil {
		return false
	}
	switch u := t.Underlying().(type) {
	case *types.Array, *types.Slice:
		return true
	case *types.Basic:
		return u.Kind() == types.String
	default:
		return false
	}
}
