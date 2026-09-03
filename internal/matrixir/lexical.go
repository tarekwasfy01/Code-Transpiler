package matrixir

import (
	"strings"
	"unicode"
)

type TokenClass int

const (
	TokenIdentifier TokenClass = iota
	TokenNumber
	TokenString
	TokenOperator
	TokenDelimiter
	TokenNewline
	TokenComment
	TokenClassCount
)

type Lexeme struct {
	Class    TokenClass
	Text     string
	Start    int
	End      int
	Depth    int
	Axes     Vector
	Semantic Vector
}

const (
	LexIdentifier = iota
	LexNumber
	LexString
	LexStringComment
	LexStringKeyword
	LexBoolean
	LexReturn
	LexPrint
	LexIf
	LexWhile
	LexFor
	LexFunction
	LexObject
	LexException
	LexModule
	LexConcurrency
	LexReflection
	LexArithmetic
	LexDivision
	LexIntegerDivision
	LexComparison
	LexLogical
	LexShortCircuit
	LexBinding
	LexNamedArgument
	LexIndex
	LexGrouping
	LexCall
	LexBlock
	LexComment
	LexicalAxes
)

func LexicalRuleMatrix() Matrix {
	m := NewMatrix(LexicalAxes, SemanticDimensions)
	link := func(axis, semantic int) { m.Set(axis, semantic, 1) }
	link(LexIdentifier, SemIdentifier)
	link(LexNumber, SemNumber)
	link(LexString, SemString)
	link(LexStringComment, SemStringComment)
	link(LexStringKeyword, SemStringKeyword)
	link(LexBoolean, SemBoolean)
	link(LexReturn, SemReturn)
	link(LexPrint, SemPrint)
	link(LexPrint, SemIO)
	link(LexPrint, SemEffect)
	link(LexIf, SemIf)
	link(LexWhile, SemWhile)
	link(LexFor, SemFor)
	link(LexFunction, SemFunction)
	link(LexFunction, SemScope)
	link(LexObject, SemObject)
	link(LexException, SemException)
	link(LexModule, SemModule)
	link(LexConcurrency, SemConcurrency)
	link(LexReflection, SemReflection)
	link(LexArithmetic, SemBinary)
	link(LexArithmetic, SemArithmetic)
	link(LexDivision, SemBinary)
	link(LexDivision, SemDivision)
	link(LexIntegerDivision, SemIntegerDivision)
	link(LexComparison, SemBinary)
	link(LexComparison, SemComparison)
	link(LexLogical, SemBinary)
	link(LexLogical, SemLogical)
	link(LexShortCircuit, SemShortCircuit)
	link(LexBinding, SemAssign)
	link(LexBinding, SemBinding)
	link(LexNamedArgument, SemNamedArgument)
	link(LexIndex, SemIndex)
	link(LexGrouping, SemGrouping)
	link(LexCall, SemCall)
	link(LexBlock, SemBlock)
	return m
}

func LexicalAxesFor(source string, class TokenClass, text string) Vector {
	axes := make(Vector, LexicalAxes)
	lower := strings.ToLower(text)
	switch class {
	case TokenIdentifier:
		axes[LexIdentifier] = 1
		if lower == "true" || lower == "false" || lower == "t" || lower == "f" {
			axes[LexBoolean] = 1
		}
		if lower == "and" || lower == "or" {
			axes[LexLogical], axes[LexShortCircuit] = 1, 1
		}
		switch lower {
		case "return":
			axes[LexReturn] = 1
		case "print", "println", "echo", "printf", "fmt.println", "console.writeline", "system.out.println", "cout":
			axes[LexPrint] = 1
		case "if":
			axes[LexIf] = 1
		case "while", "repeat":
			axes[LexWhile] = 1
		case "for":
			axes[LexFor] = 1
		case "function", "func", "fn", "def", "fun", "proc":
			axes[LexFunction] = 1
		case "class", "struct", "enum", "interface":
			axes[LexObject] = 1
		case "try", "catch", "except", "throw", "raise":
			axes[LexException] = 1
		case "import", "package", "module", "namespace", "using", "use":
			axes[LexModule] = 1
		case "async", "await", "thread", "goroutine":
			axes[LexConcurrency] = 1
		case "reflect", "eval":
			axes[LexReflection] = 1
		}
	case TokenNumber:
		axes[LexNumber] = 1
	case TokenString:
		axes[LexString] = 1
		if strings.Contains(text, "#") || strings.Contains(text, "//") {
			axes[LexStringComment] = 1
		}
		if strings.Contains(lower, "true") || strings.Contains(lower, "false") {
			axes[LexStringKeyword] = 1
		}
	case TokenOperator:
		switch text {
		case "+", "-", "*", "^", "**", "%%":
			axes[LexArithmetic] = 1
		case "/":
			axes[LexDivision] = 1
		case "//", "%/%":
			axes[LexDivision], axes[LexIntegerDivision] = 1, 1
		case "<", ">", "<=", ">=", "==", "!=", "===", "!==":
			axes[LexComparison] = 1
		case "&&", "||", "&", "|":
			axes[LexLogical] = 1
			if text == "&&" || text == "||" {
				axes[LexShortCircuit] = 1
			}
		case "=", "<-", "<<-", ":=":
			axes[LexBinding] = 1
		}
	case TokenDelimiter:
		if text == "[" || text == "]" {
			axes[LexIndex] = 1
		}
		if text == "(" || text == ")" {
			axes[LexGrouping] = 1
		}
		if text == "{" || text == "}" {
			axes[LexBlock] = 1
		}
	case TokenComment:
		axes[LexComment] = 1
	}
	return axes
}

func lexicalSemantic(source string, class TokenClass, text string) (Vector, Vector) {
	axes := LexicalAxesFor(source, class, text)
	row, _ := MatrixFromRows([][]float64{axes})
	semantic, _ := row.Multiply(LexicalRuleMatrix())
	return axes, semantic.Row(0)
}

func Tokenize(source, code string) []Lexeme {
	runes := []rune(code)
	lexemes := make([]Lexeme, 0, len(runes)/3)
	parenDepth := 0
	add := func(class TokenClass, start, end int) {
		text := string(runes[start:end])
		axes, semantic := lexicalSemantic(source, class, text)
		if class == TokenNewline {
			semantic[SemMultiline] = 0
			if parenDepth > 0 {
				semantic[SemMultiline] = 1
			}
		}
		lexemes = append(lexemes, Lexeme{Class: class, Text: text, Start: start, End: end, Depth: parenDepth, Axes: axes, Semantic: semantic})
	}
	for i := 0; i < len(runes); {
		c := runes[i]
		if c == '\r' {
			i++
			continue
		}
		if c == '\n' {
			add(TokenNewline, i, i+1)
			i++
			continue
		}
		if unicode.IsSpace(c) {
			i++
			continue
		}
		if c == '#' && hashStartsComment(source) {
			start := i
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			add(TokenComment, start, i)
			continue
		}
		// C-family block comments are lexically inert. Handling them here keeps
		// the same source-independent token contract for C, C++, Go, Rust,
		// Java, C#, Swift, Zig and Kotlin; the parser must never interpret the
		// opening slash as a division expression.
		if c == '/' && i+1 < len(runes) && runes[i+1] == '*' && source != "python" && source != "r" && source != "julia" {
			start := i
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			if i+1 < len(runes) {
				i += 2
			} else {
				i = len(runes)
			}
			add(TokenComment, start, i)
			continue
		}
		if c == '/' && i+1 < len(runes) && runes[i+1] == '/' && source != "python" {
			start := i
			i += 2
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			add(TokenComment, start, i)
			continue
		}
		if c == '"' || c == '\'' || c == '`' {
			start, quote := i, c
			i++
			for i < len(runes) {
				if runes[i] == '\\' && i+1 < len(runes) {
					i += 2
					continue
				}
				i++
				if runes[i-1] == quote {
					break
				}
			}
			add(TokenString, start, i)
			continue
		}
		if unicode.IsLetter(c) || c == '_' {
			start := i
			i++
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_' || runes[i] == '.') {
				i++
			}
			add(TokenIdentifier, start, i)
			continue
		}
		if unicode.IsDigit(c) {
			start := i
			i++
			for i < len(runes) && (unicode.IsDigit(runes[i]) || strings.ContainsRune(".eExXaAbBcCdDfF_+-", runes[i])) {
				if (runes[i] == '+' || runes[i] == '-') && !(runes[i-1] == 'e' || runes[i-1] == 'E') {
					break
				}
				i++
			}
			add(TokenNumber, start, i)
			continue
		}
		matched := ""
		for _, operator := range []string{"<<-", "->>", ":::", "===", "!==", "=>", "::", "<-", "->", ":=", "<=", ">=", "==", "!=", "&&", "||", "%%", "%/%", "//", "**", "++", "--", "+=", "-=", "*=", "/="} {
			candidate := []rune(operator)
			if i+len(candidate) <= len(runes) && string(runes[i:i+len(candidate)]) == operator {
				matched = operator
				break
			}
		}
		if matched != "" {
			start := i
			i += len([]rune(matched))
			add(TokenOperator, start, i)
			continue
		}
		if strings.ContainsRune("+-*/^~!?<>=&|$@%:", c) {
			add(TokenOperator, i, i+1)
			i++
			continue
		}
		if strings.ContainsRune("(){}[],;", c) {
			if c == '(' || c == '[' {
				parenDepth++
			}
			if (c == ')' || c == ']') && parenDepth > 0 {
				parenDepth--
			}
			add(TokenDelimiter, i, i+1)
			i++
			continue
		}
		add(TokenDelimiter, i, i+1)
		i++
	}
	return lexemes
}

func hashStartsComment(source string) bool {
	switch source {
	case "r", "python", "julia", "nim":
		return true
	}
	return false
}

func BuildLexicalGraph(source, code string) (*Graph, []Lexeme, error) {
	graph, err := NewGraph(source)
	if err != nil {
		return nil, nil, err
	}
	lexemes := Tokenize(source, code)
	payloads := make([]Node, len(lexemes)+1)
	payloads[0] = Node{Vector: Basis(SemanticDimensions, SemProgram)}
	for i, lexeme := range lexemes {
		payloads[i+1] = Node{Vector: lexeme.Semantic, Text: lexeme.Text, SourceAt: lexeme.Start}
	}
	ids := graph.AddNodes(payloads)
	root, nodes := ids[0], ids[1:]
	previous := -1
	for i := range lexemes {
		_ = graph.Connect(Syntax, root, nodes[i])
		if previous >= 0 {
			_ = graph.Connect(Control, previous, nodes[i])
		}
		previous = nodes[i]
	}
	bound := map[string]bool{}
	for i, lexeme := range lexemes {
		if lexeme.Class == TokenDelimiter && lexeme.Text == "(" {
			left := previousSignificant(lexemes, i-1)
			if left >= 0 && lexemes[left].Class == TokenIdentifier && !isStructuralHead(lexemes[left].Text) {
				lexemes[i].Axes[LexCall] = 1
				graph.Nodes[nodes[i]].Vector[SemCall] = 1
				_ = graph.Connect(Syntax, nodes[left], nodes[i])
			}
		}
		if lexeme.Class != TokenOperator || !isBindingOperator(lexeme.Text) {
			if lexeme.Semantic[SemEffect] != 0 {
				right := nextSignificant(lexemes, i+1)
				if right >= 0 {
					_ = graph.Connect(Effect, nodes[i], nodes[right])
				}
			}
			continue
		}
		left := previousSignificant(lexemes, i-1)
		right := nextSignificant(lexemes, i+1)
		if left >= 0 && lexemes[left].Class == TokenIdentifier {
			_ = graph.Connect(Binding, nodes[i], nodes[left])
			name := lexemes[left].Text
			if bound[name] {
				graph.Nodes[nodes[i]].Vector[SemReassignment] = 1
				graph.Nodes[nodes[left]].Vector[SemReassignment] = 1
			}
			bound[name] = true
			if lexeme.Depth > 0 {
				lexemes[i].Axes[LexNamedArgument] = 1
				graph.Nodes[nodes[i]].Vector[SemNamedArgument] = 1
			}
		}
		if left >= 0 && right >= 0 {
			_ = graph.Connect(Data, nodes[right], nodes[left])
		}
	}
	return graph, lexemes, nil
}

func isStructuralHead(text string) bool {
	switch strings.ToLower(text) {
	case "if", "while", "for", "switch", "function", "func", "fn", "def", "fun", "proc":
		return true
	}
	return false
}

func isBindingOperator(operator string) bool {
	switch operator {
	case "=", "<-", "<<-", ":=":
		return true
	}
	return false
}

func previousSignificant(lexemes []Lexeme, i int) int {
	for ; i >= 0; i-- {
		if lexemes[i].Class != TokenNewline && lexemes[i].Class != TokenComment {
			return i
		}
	}
	return -1
}

func nextSignificant(lexemes []Lexeme, i int) int {
	for ; i < len(lexemes); i++ {
		if lexemes[i].Class != TokenNewline && lexemes[i].Class != TokenComment {
			return i
		}
	}
	return -1
}
