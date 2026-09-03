package manytomany

import (
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"sort"
)

func finalizeProgram(source string, statements []Statement) (Program, error) {
	graph, err := matrixir.NewGraph(source)
	if err != nil {
		return Program{}, err
	}
	root := graph.AddNode(matrixir.Basis(matrixir.SemanticDimensions, matrixir.SemProgram), "", "", 0)
	previous := -1
	symbols := map[string]int{}
	for i := range statements {
		s := &statements[i]
		semantic := make(matrixir.Vector, matrixir.SemanticDimensions)
		switch s.Kind {
		case IRAssign:
			semantic[matrixir.SemAssign], semantic[matrixir.SemBinding] = 1, 1
			if _, exists := symbols[s.Name]; exists {
				semantic[matrixir.SemReassignment] = 1
			}
		case IRPrint:
			semantic[matrixir.SemPrint], semantic[matrixir.SemIO], semantic[matrixir.SemEffect] = 1, 1, 1
		case IRReturn:
			semantic[matrixir.SemReturn] = 1
		default:
			semantic[matrixir.SemExpression] = 1
		}
		s.Semantic = semantic
		s.MatrixNode = graph.AddNode(semantic, s.Expr, s.Name, i)
		if err := graph.Connect(matrixir.Syntax, root, s.MatrixNode); err != nil {
			return Program{}, err
		}
		if previous >= 0 {
			if err := graph.Connect(matrixir.Control, previous, s.MatrixNode); err != nil {
				return Program{}, err
			}
		}
		previous = s.MatrixNode
		expressionVector := matrixir.AnalyzeExpression(source, s.Expr)
		expression := graph.AddNode(expressionVector, s.Expr, "", i)
		if err := graph.Connect(matrixir.Syntax, s.MatrixNode, expression); err != nil {
			return Program{}, err
		}
		if s.Kind == IRAssign {
			symbol, exists := symbols[s.Name]
			if !exists {
				symbolVector := matrixir.Basis(matrixir.SemanticDimensions, matrixir.SemIdentifier)
				symbol = graph.AddNode(symbolVector, "", s.Name, i)
				symbols[s.Name] = symbol
			}
			if err := graph.Connect(matrixir.Binding, s.MatrixNode, symbol); err != nil {
				return Program{}, err
			}
			if err := graph.Connect(matrixir.Data, expression, symbol); err != nil {
				return Program{}, err
			}
		}
	}
	return Program{Source: source, Statements: statements, Graph: graph, Requirements: graph.Requirements()}, nil
}

func vectorKind(s Statement) (IRKind, error) {
	class, err := matrixir.ClassifyStatement(s.Semantic)
	if err != nil {
		return "", err
	}
	switch class {
	case matrixir.StatementAssign:
		return IRAssign, nil
	case matrixir.StatementPrint:
		return IRPrint, nil
	case matrixir.StatementReturn:
		return IRReturn, nil
	case matrixir.StatementExpression:
		return IRExpr, nil
	default:
		return "", fmt.Errorf("unknown statement class %d", class)
	}
}

func orderedStatements(p Program) ([]Statement, error) {
	if p.Graph == nil {
		return nil, fmt.Errorf("program has no matrix graph")
	}
	closure, err := p.Graph.Edges[matrixir.Control].BooleanClosure()
	if err != nil {
		return nil, err
	}
	type ranked struct {
		statement Statement
		reach     float64
		sourceAt  int
	}
	rankedStatements := make([]ranked, 0, len(p.Statements))
	for _, statement := range p.Statements {
		if statement.MatrixNode < 0 || statement.MatrixNode >= closure.Rows {
			return nil, fmt.Errorf("statement matrix node %d outside graph", statement.MatrixNode)
		}
		var reach float64
		for column := 0; column < closure.Cols; column++ {
			reach += closure.At(statement.MatrixNode, column)
		}
		rankedStatements = append(rankedStatements, ranked{statement: statement, reach: reach, sourceAt: p.Graph.Nodes[statement.MatrixNode].SourceAt})
	}
	sort.SliceStable(rankedStatements, func(i, j int) bool {
		if rankedStatements[i].reach != rankedStatements[j].reach {
			return rankedStatements[i].reach > rankedStatements[j].reach
		}
		return rankedStatements[i].sourceAt < rankedStatements[j].sourceAt
	})
	out := make([]Statement, len(rankedStatements))
	for i, ranked := range rankedStatements {
		out[i] = ranked.statement
	}
	return out, nil
}

func validateLoweringMatrix(p Program, target string) error {
	vectors := make([]matrixir.Vector, len(p.Statements))
	for i, statement := range p.Statements {
		vectors[i] = statement.Semantic
	}
	required, err := matrixir.StatementRequirements(vectors)
	if err != nil {
		return err
	}
	missing, err := matrixir.MissingLowerings(target, required)
	if err != nil {
		return err
	}
	for class, value := range missing {
		if value != 0 {
			return fmt.Errorf("target %s has no matrix lowering for statement class %d", target, class)
		}
	}
	return nil
}
