package matrixir

import (
	"fmt"
	"strings"
)

const (
	StatementAssign = iota
	StatementPrint
	StatementReturn
	StatementExpression
	StatementClasses
)

func ClassifyStatement(vector Vector) (int, error) {
	row, err := MatrixFromRows([][]float64{vector})
	if err != nil {
		return 0, err
	}
	projection := NewMatrix(SemanticDimensions, StatementClasses)
	projection.Set(SemAssign, StatementAssign, 1)
	projection.Set(SemPrint, StatementPrint, 1)
	projection.Set(SemReturn, StatementReturn, 1)
	projection.Set(SemExpression, StatementExpression, 1)
	scores, err := row.Multiply(projection)
	if err != nil {
		return 0, err
	}
	best, score := StatementExpression, float64(0)
	for class := 0; class < StatementClasses; class++ {
		if scores.At(0, class) > score {
			best, score = class, scores.At(0, class)
		}
	}
	return best, nil
}

// LoweringMatrix is target-language x semantic statement class. It is the
// shared structural rule table used before any target-specific renderer runs.
func LoweringMatrix() Matrix {
	m := NewMatrix(len(Languages), StatementClasses)
	for language := range Languages {
		for class := 0; class < StatementClasses; class++ {
			m.Set(language, class, 1)
		}
	}
	return m
}

func StatementRequirements(vectors []Vector) (Vector, error) {
	required := make(Vector, StatementClasses)
	for _, vector := range vectors {
		class, err := ClassifyStatement(vector)
		if err != nil {
			return nil, err
		}
		required[class] = 1
	}
	return required, nil
}

func MissingLowerings(target string, required Vector) (Vector, error) {
	if len(required) != StatementClasses {
		return nil, fmt.Errorf("lowering requirement vector has %d dimensions, want %d", len(required), StatementClasses)
	}
	targetIndex, ok := LanguageIndex(target)
	if !ok {
		return nil, fmt.Errorf("unknown target %q", target)
	}
	rules := LoweringMatrix()
	out := make(Vector, StatementClasses)
	for class := range out {
		out[class] = required[class] * (1 - rules.At(targetIndex, class))
	}
	return out, nil
}

// AnalyzeExpression creates a shared semantic vector without rewriting text.
// It is quote-aware and language-neutral; source-specific syntax is mapped to
// the same semantic axes. It is a structural scanner, not the translation
// engine and not a claim of full parsing.
func AnalyzeExpression(source, text string) Vector {
	v := Basis(SemanticDimensions, SemExpression)
	if strings.Contains(text, "\n") {
		v[SemMultiline] = 1
	}
	var quote rune
	escaped := false
	runes := []rune(text)
	outside := make([]rune, len(runes))
	stringRunes := make([]rune, 0, len(runes)/4)
	for i := 0; i < len(runes); i++ {
		c := runes[i]
		if quote != 0 {
			v[SemString] = 1
			stringRunes = append(stringRunes, c)
			if c == '#' || (c == '/' && i+1 < len(runes) && runes[i+1] == '/') {
				v[SemStringComment] = 1
			}
			if escaped {
				escaped = false
				continue
			}
			if c == '\\' {
				escaped = true
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		outside[i] = c
		if c == '\'' || c == '"' || c == '`' {
			quote = c
			outside[i] = ' '
			v[SemString] = 1
			continue
		}
		if c >= '0' && c <= '9' {
			v[SemNumber] = 1
		}
		switch c {
		case '+', '-', '*', '^':
			v[SemBinary], v[SemArithmetic] = 1, 1
		case '/':
			v[SemBinary], v[SemDivision] = 1, 1
			if i+1 < len(runes) && runes[i+1] == '/' && source == "python" {
				v[SemIntegerDivision] = 1
			}
		case '<', '>', '=':
			v[SemBinary], v[SemComparison] = 1, 1
		case '&', '|':
			v[SemBinary], v[SemLogical] = 1, 1
		case '[':
			v[SemIndex] = 1
		case '(':
			v[SemCall], v[SemGrouping] = 1, 1
		}
	}
	lower := strings.ToLower(string(outside))
	stringLower := strings.ToLower(string(stringRunes))
	if strings.Contains(lower, "true") || strings.Contains(lower, "false") {
		v[SemBoolean] = 1
	}
	if strings.Contains(stringLower, "true") || strings.Contains(stringLower, "false") {
		v[SemStringKeyword] = 1
	}
	if strings.Contains(lower, "null") || strings.Contains(lower, "none") || strings.Contains(lower, "nil") || strings.Contains(text, "NA") {
		v[SemNull] = 1
	}
	if strings.Contains(text, "%/%") || strings.Contains(lower, " div ") {
		v[SemIntegerDivision] = 1
	}
	words := strings.FieldsFunc(lower, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
	})
	for _, word := range words {
		switch word {
		case "if":
			v[SemIf] = 1
		case "while", "repeat":
			v[SemWhile] = 1
		case "for":
			v[SemFor] = 1
		case "function", "func", "fn", "def", "fun", "proc":
			v[SemFunction], v[SemScope] = 1, 1
		case "class", "struct", "enum", "interface":
			v[SemObject] = 1
		case "try", "catch", "except", "throw", "raise":
			v[SemException] = 1
		case "import", "package", "module", "namespace", "using", "use":
			v[SemModule] = 1
		case "async", "await", "thread", "goroutine":
			v[SemConcurrency] = 1
		case "reflect", "eval":
			v[SemReflection] = 1
		}
	}
	if strings.Contains(lower, "<-") || strings.Contains(lower, ":=") || strings.Contains(lower, " = ") {
		v[SemBinding] = 1
	}
	if strings.Contains(lower, "&&") || strings.Contains(lower, "||") || strings.Contains(lower, " and ") || strings.Contains(lower, " or ") {
		v[SemShortCircuit] = 1
	}
	return v
}
