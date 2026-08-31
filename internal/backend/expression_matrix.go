package backend

import (
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// Rows are unary +, unary -, logical not, conjunction, disjunction. Columns
// encode numeric sign, logical complement, and the short-circuit gate polarity.
func expressionRuleMatrix() matrixir.Matrix {
	m, _ := matrixir.MatrixFromRows([][]float64{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, 0, 1}, {0, 0, -1}})
	return m
}
func (g *targetGen) lowerUnary(op, a string) (string, error) {
	row := -1
	switch op {
	case "+":
		row = 0
	case "-":
		row = 1
	case "!":
		row = 2
	}
	if row < 0 {
		return "", fmt.Errorf("unmodeled unary operator %q", op)
	}
	basis, _ := matrixir.MatrixFromRows([][]float64{matrixir.Basis(5, row)})
	rule, _ := basis.Multiply(expressionRuleMatrix())
	if rule.At(0, 0) == 1 {
		return a, nil
	}
	if rule.At(0, 0) == -1 {
		return emitDispatch(g.target, "__binary_-", []string{targetNumber(g.target, "0"), a}), nil
	}
	not := "!"
	if g.target == "python" || g.target == "nim" || g.target == "zig" {
		not = "not "
	}
	return g.boxBoolean(not + "(" + truthCall(g.target, a) + ")"), nil
}
func (g *targetGen) lowerLogical(op, a, b string) string {
	row := 3
	if op == "||" {
		row = 4
	}
	basis, _ := matrixir.MatrixFromRows([][]float64{matrixir.Basis(5, row)})
	rule, _ := basis.Multiply(expressionRuleMatrix())
	native := "&&"
	if rule.At(0, 2) < 0 {
		native = "||"
	}
	if g.target == "python" || g.target == "nim" || g.target == "zig" {
		native = "and"
		if rule.At(0, 2) < 0 {
			native = "or"
		}
	}
	return g.boxBoolean("(" + truthCall(g.target, a) + ") " + native + " (" + truthCall(g.target, b) + ")")
}
func (g *targetGen) boxBoolean(e string) string {
	switch g.target {
	case "nim":
		return "rBool(" + e + ")"
	case "rust":
		return "RValue::Bool(" + e + ")"
	case "cpp":
		return "RValue(" + e + ")"
	case "c":
		return "r_bool(" + e + ")"
	case "zig":
		return "RValue{ .boolean = " + e + " }"
	}
	return "(" + e + ")"
}
