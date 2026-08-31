package matrixir

import "fmt"

var GrammarNames = [...]string{
	"brace_blocks", "indent_blocks", "end_blocks", "semicolon", "newline", "typed_declaration", "main_wrapper", "one_based_index", "integer_slash_truncates", "exclusive_range_end",
}

const (
	GrammarBrace = iota
	GrammarIndent
	GrammarEnd
	GrammarSemicolon
	GrammarNewline
	GrammarTypedDeclaration
	GrammarMainWrapper
	GrammarOneBasedIndex
	GrammarIntegerSlashTruncates
	GrammarExclusiveRangeEnd
	GrammarDimensions
)

var ActionNames = [...]string{
	"skip", "block", "assign", "print", "return", "expression", "if", "else", "while", "for", "function", "call", "index", "module", "object", "exception", "concurrency", "reflection",
}

const (
	ActionSkip = iota
	ActionBlock
	ActionAssign
	ActionPrint
	ActionReturn
	ActionExpression
	ActionIf
	ActionElse
	ActionWhile
	ActionFor
	ActionFunction
	ActionCall
	ActionIndex
	ActionModule
	ActionObject
	ActionException
	ActionConcurrency
	ActionReflection
	ActionDimensions
)

// GrammarProfileMatrix is the V10 source-language grammar contract. Parser
// behavior is selected by multiplying a language basis vector by this matrix.
func GrammarProfileMatrix() Matrix {
	m := NewMatrix(len(Languages), GrammarDimensions)
	set := func(language string, axes ...int) {
		row, _ := LanguageIndex(language)
		for _, axis := range axes {
			m.Set(row, axis, 1)
		}
	}
	set("r", GrammarBrace, GrammarNewline, GrammarOneBasedIndex)
	set("go", GrammarBrace, GrammarSemicolon, GrammarNewline, GrammarTypedDeclaration, GrammarMainWrapper, GrammarIntegerSlashTruncates)
	set("rust", GrammarBrace, GrammarSemicolon, GrammarNewline, GrammarTypedDeclaration, GrammarMainWrapper, GrammarIntegerSlashTruncates)
	set("cpp", GrammarBrace, GrammarSemicolon, GrammarNewline, GrammarTypedDeclaration, GrammarMainWrapper, GrammarIntegerSlashTruncates)
	set("c", GrammarBrace, GrammarSemicolon, GrammarNewline, GrammarTypedDeclaration, GrammarMainWrapper, GrammarIntegerSlashTruncates)
	set("python", GrammarIndent, GrammarNewline, GrammarExclusiveRangeEnd)
	set("zig", GrammarBrace, GrammarSemicolon, GrammarNewline, GrammarTypedDeclaration, GrammarMainWrapper, GrammarExclusiveRangeEnd)
	set("julia", GrammarEnd, GrammarNewline, GrammarOneBasedIndex)
	set("nim", GrammarIndent, GrammarNewline)
	set("csharp", GrammarBrace, GrammarSemicolon, GrammarNewline, GrammarTypedDeclaration, GrammarMainWrapper, GrammarIntegerSlashTruncates)
	set("java", GrammarBrace, GrammarSemicolon, GrammarNewline, GrammarTypedDeclaration, GrammarMainWrapper, GrammarIntegerSlashTruncates)
	set("kotlin", GrammarBrace, GrammarNewline, GrammarTypedDeclaration, GrammarMainWrapper, GrammarIntegerSlashTruncates)
	set("swift", GrammarBrace, GrammarNewline, GrammarTypedDeclaration, GrammarIntegerSlashTruncates)
	return m
}

func GrammarProfile(language string) (Vector, error) {
	index, ok := LanguageIndex(language)
	if !ok {
		return nil, fmt.Errorf("unknown matrix language %q", language)
	}
	row := Basis(len(Languages), index)
	left, _ := MatrixFromRows([][]float64{row})
	result, err := left.Multiply(GrammarProfileMatrix())
	if err != nil {
		return nil, err
	}
	return result.Row(0), nil
}

// ActionSemanticProjection converts an action vector into the shared semantic
// basis. It is the executable counterpart of the V9 structure-action matrix.
func ActionSemanticProjection() Matrix {
	m := NewMatrix(ActionDimensions, SemanticDimensions)
	link := func(action, semantic int) { m.Set(action, semantic, 1) }
	link(ActionBlock, SemBlock)
	link(ActionAssign, SemAssign)
	link(ActionAssign, SemBinding)
	link(ActionPrint, SemPrint)
	link(ActionPrint, SemIO)
	link(ActionPrint, SemEffect)
	link(ActionReturn, SemReturn)
	link(ActionExpression, SemExpression)
	link(ActionIf, SemIf)
	link(ActionElse, SemIf)
	link(ActionWhile, SemWhile)
	link(ActionFor, SemFor)
	link(ActionFunction, SemFunction)
	link(ActionFunction, SemScope)
	link(ActionCall, SemCall)
	link(ActionIndex, SemIndex)
	link(ActionModule, SemModule)
	link(ActionObject, SemObject)
	link(ActionException, SemException)
	link(ActionConcurrency, SemConcurrency)
	link(ActionReflection, SemReflection)
	return m
}

func ActionSemantic(action Vector) (Vector, error) {
	if len(action) != ActionDimensions {
		return nil, fmt.Errorf("action vector has %d dimensions, want %d", len(action), ActionDimensions)
	}
	row, _ := MatrixFromRows([][]float64{action})
	result, err := row.Multiply(ActionSemanticProjection())
	if err != nil {
		return nil, err
	}
	return result.Threshold().Row(0), nil
}
