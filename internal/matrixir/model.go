package matrixir

import (
	"fmt"
	"strings"
)

var Languages = [...]string{"r", "go", "rust", "cpp", "c", "python", "zig", "julia", "nim", "csharp", "java", "kotlin", "swift"}

var Features = [...]string{
	"arithmetic", "binding", "reassignment", "division", "boolean", "string_comment", "string_keywords", "comparison",
	"multiline", "grouping", "if_else", "while", "for", "function", "index", "integer_division",
	"scope", "closure", "named_args", "lazy_eval", "short_circuit", "overflow", "null_na", "objects",
	"exceptions", "generics", "ownership", "modules", "concurrency", "ffi", "reflection", "serialization",
}

var SemanticNames = [...]string{
	"program", "block", "assign", "print", "return", "expression", "identifier", "number", "string", "boolean", "null",
	"unary", "binary", "call", "index", "if", "while", "for", "function", "arithmetic", "division", "integer_division",
	"comparison", "logical", "binding", "reassignment", "scope", "closure", "named_argument", "lazy_evaluation", "short_circuit",
	"overflow", "object", "exception", "generic", "ownership", "module", "concurrency", "ffi", "reflection", "serialization",
	"io", "effect", "string_comment", "string_keyword", "multiline", "grouping", "unknown",
}

var RelationNames = [...]string{"syntax", "control", "data", "binding", "effect"}

const (
	SemProgram = iota
	SemBlock
	SemAssign
	SemPrint
	SemReturn
	SemExpression
	SemIdentifier
	SemNumber
	SemString
	SemBoolean
	SemNull
	SemUnary
	SemBinary
	SemCall
	SemIndex
	SemIf
	SemWhile
	SemFor
	SemFunction
	SemArithmetic
	SemDivision
	SemIntegerDivision
	SemComparison
	SemLogical
	SemBinding
	SemReassignment
	SemScope
	SemClosure
	SemNamedArgument
	SemLazyEvaluation
	SemShortCircuit
	SemOverflow
	SemObject
	SemException
	SemGeneric
	SemOwnership
	SemModule
	SemConcurrency
	SemFFI
	SemReflection
	SemSerialization
	SemIO
	SemEffect
	SemStringComment
	SemStringKeyword
	SemMultiline
	SemGrouping
	SemUnknown
	SemanticDimensions
)

type Relation int

const (
	Syntax Relation = iota
	Control
	Data
	Binding
	Effect
	RelationCount
)

type Node struct {
	ID       int
	Vector   Vector
	Text     string
	Symbol   string
	SourceAt int
}

type Graph struct {
	Language string
	Nodes    []Node
	Edges    [RelationCount]SparseMatrix
}

func NewGraph(language string) (*Graph, error) {
	if _, ok := LanguageIndex(language); !ok {
		return nil, fmt.Errorf("unknown matrix language %q", language)
	}
	return &Graph{Language: language}, nil
}

func (g *Graph) AddNode(vector Vector, text, symbol string, sourceAt int) int {
	return g.AddNodes([]Node{{Vector: vector, Text: text, Symbol: symbol, SourceAt: sourceAt}})[0]
}

// AddNodes appends a batch with consecutive graph-owned IDs and copied semantic
// vectors. Relation matrices grow their dimensions without copying any edges.
// Invalid vectors are rejected before the graph is changed; incoming IDs are
// ignored because IDs are positions in this graph's relation matrices.
func (g *Graph) AddNodes(nodes []Node) []int {
	for _, node := range nodes {
		if len(node.Vector) != SemanticDimensions {
			panic(fmt.Sprintf("semantic vector has %d dimensions, want %d", len(node.Vector), SemanticDimensions))
		}
	}
	ids := make([]int, len(nodes))
	if len(nodes) == 0 {
		return ids
	}
	start, size := len(g.Nodes), len(g.Nodes)+len(nodes)
	appended := make([]Node, len(nodes))
	for i, node := range nodes {
		ids[i] = start + i
		node.ID = ids[i]
		node.Vector = append(Vector(nil), node.Vector...)
		appended[i] = node
	}
	for relation := Relation(0); relation < RelationCount; relation++ {
		g.Edges[relation].Grow(size, size)
	}
	g.Nodes = append(g.Nodes, appended...)
	return ids
}

func (g *Graph) Connect(relation Relation, from, to int) error {
	if relation < 0 || relation >= RelationCount {
		return fmt.Errorf("unknown relation %d", relation)
	}
	if from < 0 || from >= len(g.Nodes) || to < 0 || to >= len(g.Nodes) {
		return fmt.Errorf("edge %d -> %d outside graph with %d nodes", from, to, len(g.Nodes))
	}
	g.Edges[relation].Set(from, to, 1)
	return nil
}

func (g *Graph) NodeMatrix() Matrix {
	out := NewMatrix(len(g.Nodes), SemanticDimensions)
	for i, node := range g.Nodes {
		copy(out.Data[i*out.Cols:(i+1)*out.Cols], node.Vector)
	}
	return out
}

// Requirements performs X*W, where X is the node matrix and W projects
// semantic dimensions onto the shared 32-feature contract. Counts are reduced
// to a binary program requirement vector after multiplication.
func (g *Graph) Requirements() Vector {
	projection := SemanticFeatureProjection()
	product, err := g.NodeMatrix().Multiply(projection)
	if err != nil {
		panic(err)
	}
	out := make(Vector, len(Features))
	for row := 0; row < product.Rows; row++ {
		for feature := 0; feature < product.Cols; feature++ {
			if product.At(row, feature) != 0 {
				out[feature] = 1
			}
		}
	}
	return out
}

func FeatureRequirements(semantic Vector) (Vector, error) {
	row, err := MatrixFromRows([][]float64{semantic})
	if err != nil {
		return nil, err
	}
	product, err := row.Multiply(SemanticFeatureProjection())
	if err != nil {
		return nil, err
	}
	return product.Threshold().Row(0), nil
}

func (g *Graph) SemanticRequirements() Vector {
	out := make(Vector, SemanticDimensions)
	for _, node := range g.Nodes {
		for dimension, value := range node.Vector {
			if value != 0 {
				out[dimension] = 1
			}
		}
	}
	return out
}

func (g *Graph) RelationCounts() Vector {
	out := make(Vector, RelationCount)
	for relation := Relation(0); relation < RelationCount; relation++ {
		out[relation] = g.Edges[relation].Sum()
	}
	return out
}

func SemanticFeatureProjection() Matrix {
	m := NewMatrix(SemanticDimensions, len(Features))
	link := func(semantic int, feature string) { m.Set(semantic, MustFeatureIndex(feature), 1) }
	link(SemArithmetic, "arithmetic")
	link(SemBinding, "binding")
	link(SemReassignment, "reassignment")
	link(SemDivision, "division")
	link(SemBoolean, "boolean")
	link(SemComparison, "comparison")
	link(SemIf, "if_else")
	link(SemWhile, "while")
	link(SemFor, "for")
	link(SemFunction, "function")
	link(SemIndex, "index")
	link(SemIntegerDivision, "integer_division")
	link(SemScope, "scope")
	link(SemClosure, "closure")
	link(SemNamedArgument, "named_args")
	link(SemLazyEvaluation, "lazy_eval")
	link(SemShortCircuit, "short_circuit")
	link(SemOverflow, "overflow")
	link(SemNull, "null_na")
	link(SemObject, "objects")
	link(SemException, "exceptions")
	link(SemGeneric, "generics")
	link(SemOwnership, "ownership")
	link(SemModule, "modules")
	link(SemConcurrency, "concurrency")
	link(SemFFI, "ffi")
	link(SemReflection, "reflection")
	link(SemSerialization, "serialization")
	link(SemStringComment, "string_comment")
	link(SemStringKeyword, "string_keywords")
	link(SemMultiline, "multiline")
	link(SemGrouping, "grouping")
	return m
}

func LanguageIndex(language string) (int, bool) {
	language = strings.ToLower(strings.TrimSpace(language))
	for i, value := range Languages {
		if value == language {
			return i, true
		}
	}
	return -1, false
}

func FeatureIndex(feature string) (int, bool) {
	for i, value := range Features {
		if value == feature {
			return i, true
		}
	}
	return -1, false
}

func MustFeatureIndex(feature string) int {
	i, ok := FeatureIndex(feature)
	if !ok {
		panic("unknown matrix feature " + feature)
	}
	return i
}
