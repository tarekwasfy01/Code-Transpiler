package backend

import (
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"strconv"
)

// IterationExpr is an internal intrinsic, never resolved as a user function.
// Snapshot normalizes null/scalar/vector to a private outer vector; size then
// operates on that vector. Element gathers use the existing IndexExpr node.
type IterationExpr struct {
	Kind  string
	Value Expr
}

func (*IterationExpr) exprNode() {}

// A homogeneous coordinate keeps the cursor update auditable as v' = M v,
// where v = [position, length, 1]. The guard and gather remain nonlinear
// operations on that state; arbitrary program semantics are not linearized.
func iterationAdvanceMatrix() matrixir.Matrix {
	m, _ := matrixir.MatrixFromRows([][]float64{{1, 0, 1}, {0, 1, 0}, {0, 0, 1}})
	return m
}

type IterationEvidence struct {
	Sequence        string          `json:"sequence_slot"`
	Position        string          `json:"position_slot"`
	Length          string          `json:"length_slot"`
	Element         string          `json:"element_slot"`
	Advance         matrixir.Matrix `json:"advance"`
	InitialPosition float64         `json:"initial_position"`
}

// normalizeFunctionIterations creates fresh nodes, leaving the parsed function
// unchanged for demand analysis and subsequent calls. Both loop syntax families
// ultimately use the same A/T/F state graph and definite-assignment matrices.
func normalizeFunctionIterations(fn *FunctionExpr) (*BlockStmt, []IterationEvidence) {
	serial := 0
	fresh := func(kind string) string {
		serial++
		// NUL is rejected by the source lexer. Private slot identities therefore
		// cannot capture user names, including free names in called functions.
		// These keys are resolved to vector positions, never emitted identifiers.
		return fmt.Sprintf("\x00iteration:%s:%d", kind, serial)
	}
	id := func(name string) Expr { return &IdentExpr{Name: name} }
	number := func(value float64) Expr {
		return &LiteralExpr{Kind: "number", Text: strconv.FormatFloat(value, 'g', -1, 64)}
	}
	assign := func(name string, value Expr) Stmt { return &AssignStmt{Name: name, Op: "<-", Value: value} }
	var evidence []IterationEvidence
	var lower func(Stmt) Stmt
	lower = func(stmt Stmt) Stmt {
		switch x := stmt.(type) {
		case *BlockStmt:
			out := &BlockStmt{}
			for _, child := range x.List {
				out.List = append(out.List, lower(child))
			}
			return out
		case *IfStmt:
			return &IfStmt{Cond: x.Cond, Then: lower(x.Then), Else: lower(x.Else)}
		case *WhileStmt:
			return &WhileStmt{Cond: x.Cond, Body: lower(x.Body)}
		case *ForStmt:
			sequence, position, length := fresh("seq"), fresh("pos"), fresh("len")
			advance := iterationAdvanceMatrix()
			// Project the cursor row, then apply its coefficients to the AST state
			// vector. This expression is consumed by every target's common generator.
			selector, _ := matrixir.MatrixFromRows([][]float64{matrixir.Basis(3, 0)})
			row, _ := selector.Multiply(advance)
			values := []Expr{id(position), id(length), number(1)}
			var increment Expr
			for j, coefficient := range row.Data {
				if coefficient == 0 {
					continue
				}
				term := values[j]
				if coefficient != 1 {
					term = &BinaryExpr{Op: "*", L: number(coefficient), R: term}
				}
				if increment == nil {
					increment = term
				} else {
					increment = &BinaryExpr{Op: "+", L: increment, R: term}
				}
			}
			evidence = append(evidence, IterationEvidence{sequence, position, length, x.Name, advance, 1})
			body := &BlockStmt{List: []Stmt{
				assign(x.Name, &IndexExpr{X: id(sequence), Args: []Arg{{Value: id(position)}}, Double: true}),
				assign(position, increment), lower(x.Body),
			}}
			return &BlockStmt{List: []Stmt{
				assign(sequence, &IterationExpr{Kind: "snapshot", Value: x.Seq}),
				assign(length, &IterationExpr{Kind: "size", Value: id(sequence)}),
				assign(position, number(1)),
				&WhileStmt{Cond: &BinaryExpr{Op: "<=", L: id(position), R: id(length)}, Body: body},
			}}
		default:
			return stmt
		}
	}
	return lower(fn.Body).(*BlockStmt), evidence
}
