package backend

import (
	"fmt"
	"reflect"

	"strings"
)

// SemanticOperation is a typed core operation, independent of source spelling.
// Type is the integer domain; comparison/format operations return bool/string.
type SemanticOperation struct {
	Name string       `json:"name"`
	Type SemanticType `json:"type"`
	Text string       `json:"text,omitempty"`
}

type OperationExpr struct {
	Operation SemanticOperation
	Operands  []Expr
}

func (*OperationExpr) exprNode() {}

func exactIntegerCapability(feature string) bool {
	if feature == "integer.operations.v1" {
		return true
	}
	for _, bits := range []int{8, 16, 32, 64} {
		for _, signed := range []bool{false, true} {
			if feature == integerFeature(integerType(bits, signed)) {
				return true
			}
		}
	}
	return false
}

func validValueContract(model string, t SemanticTypeContract) bool {
	if model == "tagged_exact_scalars_v1" {
		if t.Numeric != "binary64_and_fixed_width_integer" || t.IntegerWidth != "per_operation" {
			return false
		}
		t.Numeric, t.IntegerWidth = "binary64", "unknown"
	} else if model != "tagged_dynamic_binary64" {
		return false
	}
	return t.valid()
}

type integerRule struct {
	arity  int
	result string
}

var integerRules = map[string]integerRule{
	"integer.literal": {0, "integer"}, "integer.value": {1, "integer"},
	"integer.convert": {1, "integer"}, "integer.format": {1, "string"},
	"integer.negate": {1, "integer"}, "integer.complement": {1, "integer"},
	"integer.add": {2, "integer"}, "integer.subtract": {2, "integer"},
	"integer.multiply": {2, "integer"}, "integer.and": {2, "integer"},
	"integer.or": {2, "integer"}, "integer.xor": {2, "integer"}, "integer.and_not": {2, "integer"},
	"integer.equal": {2, "boolean"}, "integer.not_equal": {2, "boolean"},
	"integer.less": {2, "boolean"}, "integer.less_equal": {2, "boolean"},
	"integer.greater": {2, "boolean"}, "integer.greater_equal": {2, "boolean"},
}

func integerType(bits int, signed bool) SemanticType {
	return SemanticType{Kind: "integer", Bits: bits, Signed: &signed, TypeOrigin: "explicit"}
}
func integerFeature(t SemanticType) string {
	prefix := "uint"
	if t.Signed != nil && *t.Signed {
		prefix = "int"
	}
	return fmt.Sprintf("integer.%s%d.exact", prefix, t.Bits)
}
func (o SemanticOperation) resultType() SemanticType {
	if rule, ok := integerRules[o.Name]; ok && rule.result != "integer" {
		return SemanticType{Kind: rule.result, TypeOrigin: "inferred"}
	}
	return o.Type
}
func (o SemanticOperation) semantics() SemanticSemantics {
	overflow := "not_applicable"
	switch o.Name {
	case "integer.add", "integer.subtract", "integer.multiply", "integer.negate", "integer.complement", "integer.and", "integer.or", "integer.xor", "integer.and_not", "integer.convert":
		overflow = "wrap_modulo_2n"
	}
	return SemanticSemantics{Operation: o.Name, Dispatch: "builtin", Overflow: overflow, EvaluationOrder: "left_to_right", Confidence: "exact", ErrorModel: "reject_invalid_operand"}
}
func (o SemanticOperation) validate(arity int) error {
	rule, ok := integerRules[o.Name]
	if !ok {
		return fmt.Errorf("unsupported typed operation %q", o.Name)
	}
	if arity != rule.arity {
		return fmt.Errorf("%s expects %d operands, got %d", o.Name, rule.arity, arity)
	}
	t := o.Type
	if t.Kind != "integer" || t.Signed == nil || (t.Bits != 8 && t.Bits != 16 && t.Bits != 32 && t.Bits != 64) {
		return fmt.Errorf("invalid fixed-width integer type")
	}
	// Reject semantic information that this integer domain cannot interpret.
	expected := integerType(t.Bits, *t.Signed)
	expected.TypeOrigin = t.TypeOrigin
	if t.TypeOrigin != "explicit" && t.TypeOrigin != "inferred" {
		return fmt.Errorf("integer type origin must be explicit or inferred")
	}
	if !reflect.DeepEqual(t, expected) {
		return fmt.Errorf("unsupported integer type annotations")
	}
	if o.Name == "integer.literal" {
		_, err := parseExactInteger(t, o.Text)
		return err
	}
	if o.Text != "" {
		return fmt.Errorf("unexpected text on %s", o.Name)
	}
	return nil
}

func parseExactInteger(t SemanticType, text string) (exactInteger, error) {
	return parseExactIntegerValue(t.Bits, *t.Signed, text)
}
func evaluateInteger(o SemanticOperation, values []any) (any, error) {
	if err := o.validate(len(values)); err != nil {
		return nil, err
	}
	return exactIntegerOperation(o.Name, o.Type.Bits, *o.Type.Signed, o.Text, values)
}
func (g *targetGen) lowerTypedOperation(x *OperationExpr) (string, error) {
	if err := x.Operation.validate(len(x.Operands)); err != nil {
		return "", err
	}
	if err := TypedImplementationMatrix().Check([]string{x.Operation.Name}, "target."+g.target); err != nil {
		return "", err
	}
	// Emission is ordered through shared value bindings, including nested calls.
	var lets []valueBinding
	var args []string
	for _, operand := range x.Operands {
		s, err := g.expr(operand)
		if err != nil {
			return "", err
		}
		name := g.freshName("integer")
		g.cValues[name] = true
		lets = append(lets, valueBinding{name, s})
		args = append(args, name)
	}
	o := x.Operation
	signed := "false"
	if *o.Type.Signed {
		signed = "true"
	}
	if g.target == "go" {
		result := fmt.Sprintf("rExact(%q, %d, %s, %q, []any{%s})", o.Name, o.Type.Bits, signed, o.Text, strings.Join(args, ", "))
		return g.letExpression(lets, result), nil
	}
	if g.target == "rust" {
		result := fmt.Sprintf("r_exact(%q, %d, %s, %q, vec![%s])", o.Name, o.Type.Bits, signed, o.Text, strings.Join(args, ", "))
		return g.letExpression(lets, result), nil
	}
	if g.target == "c" {
		flag := 0
		if *o.Type.Signed {
			flag = 1
		}
		values := "NULL"
		if len(args) > 0 {
			values = "(RValue[]){" + strings.Join(args, ", ") + "}"
		}
		result := fmt.Sprintf("r_exact(%q, %d, %d, %q, %s, %d)", o.Name, o.Type.Bits, flag, o.Text, values, len(args))
		return g.letExpression(lets, result), nil
	}
	if g.target == "cpp" {
		result := fmt.Sprintf("r_exact(%q, %d, %s, %q, {%s})", o.Name, o.Type.Bits, signed, o.Text, strings.Join(args, ", "))
		return g.letExpression(lets, result), nil
	}
	if g.target == "java" {
		result := fmt.Sprintf("RExact.apply(%q, %d, %s, %q, new Object[]{%s})", o.Name, o.Type.Bits, signed, o.Text, strings.Join(args, ", "))
		return g.letExpression(lets, result), nil
	}
	if g.target == "csharp" {
		result := fmt.Sprintf("RExact.Apply(%q, %d, %s, %q, new object[]{%s})", o.Name, o.Type.Bits, signed, o.Text, strings.Join(args, ", "))
		return g.letExpression(lets, result), nil
	}
	if adapter, ok := typedAdapterFor(g.target); ok {
		signed = adapter.False
		if *o.Type.Signed {
			signed = adapter.True
		}
		return g.letExpression(lets, adapter.Call(o.Name, o.Type.Bits, signed, o.Text, args)), nil
	}
	return "", fmt.Errorf("no typed operation adapter for target %q", g.target)
}
