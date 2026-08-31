package matrixir

import (
	"fmt"

	"strings"
	"unicode"
)

type CanonicalNode struct {
	Action Vector
	Text   string
	Source int
	// Close and Post are structural action payload, not R transport text.
	// They make block boundaries available to direct semantic lowerers.
	Close int
	Post  string
}

type CanonicalProgram struct {
	Source  string
	R       string
	Nodes   []CanonicalNode
	Graph   *Graph
	Actions Matrix
	Grammar Vector
	Roles   Matrix
	Lexemes []Lexeme
	Events  []CanonicalEvent
}

// CanonicalEvent preserves structural action order for direct lowerers. It is
// not a source string field and is deliberately separate from the legacy R
// diagnostic view.
type CanonicalEvent struct {
	Text   string
	Source int
}

type blockFrame struct {
	indent   int
	semantic bool
	function bool
	entry    bool
	loop     bool
	post     string
	closes   int
	node     int
}

type sourceLine struct {
	text   string
	trim   string
	indent int
	start  int
	tokens []Lexeme
}

// Canonicalize lowers the supported common subset through token structure and
// action matrices into R-shaped syntax. It is not a full parser for all source
// languages; the R expression parser and some header recognizers remain.
func Canonicalize(source, code string) (CanonicalProgram, error) {
	profile, err := GrammarProfile(source)
	if err != nil {
		return CanonicalProgram{}, err
	}
	lexicalGraph, lexemes, err := BuildLexicalGraph(source, code)
	if err != nil {
		return CanonicalProgram{}, err
	}
	structure, err := AnalyzeTokenStructure(lexemes)
	if err != nil {
		return CanonicalProgram{}, err
	}
	lines := statementSegments(source, code, lexemes, structure)
	var output []string
	var nodes []CanonicalNode
	var events []CanonicalEvent
	stack := []blockFrame{}
	rangePrefix := "__matrix_range_"
	for strings.Contains(code, rangePrefix) {
		rangePrefix = "_" + rangePrefix
	}
	closeFrame := func(frame blockFrame) {
		n := frame.closes
		if n == 0 {
			n = 1
		}
		if frame.node >= 0 && frame.node < len(nodes) {
			nodes[frame.node].Close += n
			nodes[frame.node].Post = frame.post
		}
		if frame.post != "" {
			output = append(output, frame.post)
			events = append(events, CanonicalEvent{Text: frame.post})
		}
		if frame.semantic {
			for i := 0; i < n; i++ {
				output = append(output, "}")
				events = append(events, CanonicalEvent{Text: "}"})
			}
		}
	}
	appendAction := func(action int, text string, sourceAt int) (int, error) {
		vector := Basis(ActionDimensions, action)
		nodes = append(nodes, CanonicalNode{Action: vector, Text: text, Source: sourceAt})
		if text != "" {
			output = append(output, text)
			events = append(events, CanonicalEvent{Text: text, Source: sourceAt})
		}
		return len(nodes) - 1, nil
	}
	closeIndent := func(indent int) {
		for len(stack) > 0 && profile[GrammarIndent] != 0 && indent <= stack[len(stack)-1].indent {
			frame := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			closeFrame(frame)
		}
	}
	for lineIndex, line := range lines {
		trim := strings.TrimSpace(line.trim)
		if trim == "" {
			continue
		}
		lower := strings.ToLower(trim)
		if profile[GrammarIndent] != 0 && !strings.HasPrefix(lower, "else") {
			closeIndent(line.indent)
		}
		leadingClose := 0
		for strings.HasPrefix(trim, "}") {
			leadingClose++
			trim = strings.TrimSpace(strings.TrimPrefix(trim, "}"))
		}
		if lower == "end" || strings.HasPrefix(lower, "end;") {
			leadingClose++
			trim = ""
		}
		hadLeadingClose := leadingClose > 0
		for ; leadingClose > 0 && len(stack) > 0; leadingClose-- {
			frame := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			closeFrame(frame)
		}
		if trim == "" {
			continue
		}
		lower = strings.ToLower(trim)
		if isModuleScaffold(line.tokens, trim) {
			_, _ = appendAction(ActionModule, "", line.start)
			continue
		}
		if isObjectWrapper(line.tokens, trim) {
			_, _ = appendAction(ActionObject, "", line.start)
			if opensBlock(trim, profile) {
				stack = append(stack, blockFrame{indent: line.indent, semantic: false})
			}
			continue
		}
		if strings.HasPrefix(lower, "else") {
			// An outer else must first close all deeper indentation frames.
			// Keep its own if frame for the matching close below. This applies
			// to every indentation-based grammar profile, not one language.
			if profile[GrammarIndent] != 0 {
				closeIndent(line.indent + 1)
			}
			if !hadLeadingClose && len(stack) > 0 {
				frame := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				closeFrame(frame)
			}
			id, _ := appendAction(ActionElse, "else {", line.start)
			stack = append(stack, blockFrame{indent: line.indent, semantic: true, node: id})
			continue
		}
		if isFunctionHeader(line.tokens, trim) {
			name, params := functionSignature(source, line.tokens, trim, profile)
			if strings.EqualFold(name, "main") {
				_, _ = appendAction(ActionSkip, "", line.start)
				stack = append(stack, blockFrame{indent: line.indent, semantic: false, function: true, entry: true, node: -1})
				continue
			}
			if name == "" {
				return CanonicalProgram{}, fmt.Errorf("%s matrix parser: function name missing near %q", source, trim)
			}
			text := fmt.Sprintf("%s <- function(%s) {", name, strings.Join(params, ", "))
			id, _ := appendAction(ActionFunction, text, line.start)
			stack = append(stack, blockFrame{indent: line.indent, semantic: true, function: true, node: id})
			continue
		}
		if startsKeyword(line.tokens, "if") {
			condition := headerExpression(trim, "if")
			id, _ := appendAction(ActionIf, "if ("+normalizeExpression(source, condition, profile)+") {", line.start)
			stack = append(stack, blockFrame{indent: line.indent, semantic: true, node: id})
			continue
		}
		if startsKeyword(line.tokens, "while") || startsKeyword(line.tokens, "repeat") {
			condition := headerExpression(trim, firstTokenText(line.tokens))
			id, _ := appendAction(ActionWhile, "while ("+normalizeExpression(source, condition, profile)+") {", line.start)
			stack = append(stack, blockFrame{indent: line.indent, semantic: true, loop: true, node: id})
			continue
		}
		if startsKeyword(line.tokens, "for") {
			if !forHasRange(line.tokens, trim) {
				condition := headerExpression(trim, "for")
				id, _ := appendAction(ActionWhile, "while ("+normalizeExpression(source, condition, profile)+") {", line.start)
				stack = append(stack, blockFrame{indent: line.indent, semantic: true, loop: true, node: id})
				continue
			}
			plan, rangeErr := planRange(source, trim, profile)
			if rangeErr != nil {
				return CanonicalProgram{}, fmt.Errorf("%s range matrix: %w", source, rangeErr)
			}
			if plan.Counting {
				_, _ = appendAction(ActionAssign, plan.Name+" <- "+plan.Begin, line.start)
				id, _ := appendAction(ActionWhile, "while ("+plan.Condition+") {", line.start)
				stack = append(stack, blockFrame{indent: line.indent, semantic: true, loop: true, post: plan.Advance, node: id})
			} else {
				// Snapshot range endpoints once, then guard empty ascending ranges.
				begin := fmt.Sprintf("%s%d_start", rangePrefix, line.start)
				end := fmt.Sprintf("%s%d_end", rangePrefix, line.start)
				_, _ = appendAction(ActionAssign, begin+" <- "+plan.Begin, line.start)
				_, _ = appendAction(ActionAssign, end+" <- "+plan.End, line.start)
				_, _ = appendAction(ActionIf, "if ("+begin+" <= "+end+") {", line.start)
				id, _ := appendAction(ActionFor, fmt.Sprintf("for (%s in %s:%s) {", plan.Name, begin, end), line.start)
				stack = append(stack, blockFrame{indent: line.indent, semantic: true, loop: true, closes: 2, node: id})
			}
			continue
		}
		if startsKeyword(line.tokens, "continue") {
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].loop {
					if stack[i].post != "" {
						_, _ = appendAction(ActionAssign, stack[i].post, line.start)
					}
					break
				}
			}
			_, _ = appendAction(ActionExpression, "next", line.start)
			continue
		}
		if startsKeyword(line.tokens, "return") {
			expression := strings.TrimSpace(trim[len(firstTokenText(line.tokens)):])
			expression = strings.TrimSpace(strings.TrimSuffix(expression, ";"))
			expression = trimOuterCall(expression)
			entry := false
			for i := len(stack) - 1; i >= 0; i-- {
				if stack[i].function {
					entry = stack[i].entry
					break
				}
			}
			if entry {
				terminal := true
				for _, remaining := range lines[lineIndex+1:] {
					if strings.Trim(remaining.trim, " };\t\r\n") != "" {
						terminal = false
						break
					}
				}
				if terminal && (expression == "0" || expression == "EXIT_SUCCESS") {
					continue
				}
				return CanonicalProgram{}, fmt.Errorf("entry-point early return requires explicit exit semantics")
			}
			_, _ = appendAction(ActionReturn, "return("+normalizeExpression(source, expression, profile)+")", line.start)
			continue
		}
		if expression, ok := printExpression(trim); ok {
			_, _ = appendAction(ActionPrint, "print("+normalizeExpression(source, expression, profile)+")", line.start)
			continue
		}
		if name, expression, ok := assignmentExpression(line.tokens, trim); ok {
			_, _ = appendAction(ActionAssign, name+" <- "+normalizeExpression(source, expression, profile), line.start)
			continue
		}
		if trim == "{" || trim == ";" || trim == "return 0;" {
			continue
		}
		if strings.Contains(lower, "++") || strings.Contains(lower, "--") {
			continue
		}
		_, _ = appendAction(ActionExpression, normalizeExpression(source, trim, profile), line.start)
	}
	for len(stack) > 0 {
		frame := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		closeFrame(frame)
	}
	actions := NewMatrix(len(nodes), ActionDimensions)
	graph, _ := NewGraph(source)
	root := graph.AddNode(Basis(SemanticDimensions, SemProgram), "", "", 0)
	previous := -1
	for i, node := range nodes {
		copy(actions.Data[i*actions.Cols:(i+1)*actions.Cols], node.Action)
		semantic, _ := ActionSemantic(node.Action)
		id := graph.AddNode(semantic, node.Text, "", node.Source)
		_ = graph.Connect(Syntax, root, id)
		if previous >= 0 {
			_ = graph.Connect(Control, previous, id)
		}
		previous = id
	}
	roles := tokenRoleMatrix(lexemes)
	_ = lexicalGraph
	return CanonicalProgram{Source: source, R: strings.Join(output, "\n") + "\n", Nodes: nodes, Graph: graph, Actions: actions, Grammar: profile, Roles: roles, Lexemes: lexemes, Events: events}, nil
}

func significant(tokens []Lexeme) []Lexeme {
	out := tokens[:0]
	for _, token := range tokens {
		if token.Class != TokenNewline && token.Class != TokenComment {
			out = append(out, token)
		}
	}
	return out
}

func indentation(line string) int {
	n := 0
	for _, r := range line {
		if r == ' ' {
			n++
		} else if r == '\t' {
			n += 4
		} else {
			break
		}
	}
	return n
}

func tokenRoleMatrix(tokens []Lexeme) Matrix {
	roles := NewMatrix(len(tokens), 3)
	for i, token := range tokens {
		role := 0
		if token.Axes[LexModule] != 0 || token.Axes[LexObject] != 0 {
			role = 1
		}
		if token.Class == TokenComment {
			role = 2
		}
		roles.Set(i, role, 1)
	}
	return roles
}

func isModuleScaffold(tokens []Lexeme, text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if strings.HasPrefix(lower, "#include") || strings.HasPrefix(lower, "using namespace") || strings.HasPrefix(lower, "extern crate") {
		return true
	}
	if len(tokens) == 0 {
		return false
	}
	first := strings.ToLower(tokens[0].Text)
	switch first {
	case "package", "import", "use", "using", "namespace", "module":
		return true
	}
	for _, token := range tokens {
		if token.Axes[LexModule] != 0 && strings.Contains(lower, "import") {
			return true
		}
	}
	return false
}

func isObjectWrapper(tokens []Lexeme, text string) bool {
	if len(tokens) == 0 {
		return false
	}
	hasObject, wrapperName := false, false
	for _, token := range tokens {
		if token.Axes[LexObject] != 0 {
			hasObject = true
		}
		if strings.EqualFold(token.Text, "program") || strings.EqualFold(token.Text, "main") {
			wrapperName = true
		}
	}
	return hasObject && wrapperName && strings.Contains(text, "{")
}

func isFunctionHeader(tokens []Lexeme, text string) bool {
	if _, ok := printExpression(text); ok {
		return false
	}
	for _, token := range tokens {
		if token.Axes[LexFunction] != 0 {
			return true
		}
	}
	// C-family function definitions carry a typed name followed by parameters.
	hasBlockDelimiter := false
	for _, token := range tokens {
		if token.Class == TokenDelimiter && token.Text == "{" {
			hasBlockDelimiter = true
		}
	}
	return strings.Contains(text, "(") && strings.Contains(text, ")") && hasBlockDelimiter && !strings.HasPrefix(strings.TrimSpace(text), "if") && !strings.HasPrefix(strings.TrimSpace(text), "while") && !strings.HasPrefix(strings.TrimSpace(text), "for")
}

func functionSignature(source string, tokens []Lexeme, text string, profile Vector) (string, []string) {
	open, close := strings.Index(text, "("), strings.Index(text, ")")
	if open < 0 || close < open {
		return "", nil
	}
	before := significant(Tokenize("r", text[:open]))
	name := ""
	for i := len(before) - 1; i >= 0; i-- {
		candidate := before[i].Text
		if before[i].Class == TokenIdentifier && !isFunctionWord(candidate) && !isTypeWord(candidate) {
			name = candidate
			break
		}
	}
	if strings.Contains(text[:open], "<-") || strings.Contains(text[:open], "=") {
		for i, token := range before {
			if token.Class == TokenOperator && isBindingOperator(token.Text) && i > 0 {
				name = before[i-1].Text
				break
			}
		}
	}
	var params []string
	for _, part := range splitTopLevel(text[open+1:close], ',') {
		defaultValue := ""
		parts := splitTopLevel(part, '=')
		if len(parts) == 2 {
			part = parts[0]
			defaultValue = normalizeExpression(source, parts[1], profile)
		}
		pt := significant(Tokenize("r", part))
		var names []string
		for _, token := range pt {
			if token.Class == TokenIdentifier && !isTypeWord(token.Text) && token.Text != "_" {
				names = append(names, token.Text)
			}
		}
		if len(names) > 0 {
			parameter := names[0]
			if defaultValue != "" {
				parameter += " = " + defaultValue
			}
			params = append(params, parameter)
		} else if strings.Contains(part, "_") {
			params = append(params, fmt.Sprintf("__unused_%d", len(params)))
		}
	}
	return name, params
}

func isFunctionWord(text string) bool {
	switch strings.ToLower(text) {
	case "function", "func", "fn", "def", "fun", "proc", "pub", "static":
		return true
	}
	return false
}

func isTypeWord(text string) bool {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "int", "i32", "i64", "u32", "u64", "double", "float", "f32", "f64", "bool", "boolean", "string", "void", "auto", "any", "num":
		return true
	}
	return false
}

func opensBlock(text string, profile Vector) bool {
	trim := strings.TrimSpace(text)
	return strings.Contains(trim, "{") || (profile[GrammarIndent] != 0 && strings.HasSuffix(trim, ":")) || profile[GrammarEnd] != 0
}

func startsKeyword(tokens []Lexeme, keyword string) bool {
	for _, token := range tokens {
		if token.Class == TokenIdentifier {
			return strings.EqualFold(token.Text, keyword)
		}
	}
	return false
}

func firstTokenText(tokens []Lexeme) string {
	for _, token := range tokens {
		if token.Class == TokenIdentifier {
			return token.Text
		}
	}
	return ""
}

func headerExpression(text, keyword string) string {
	trim := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(text), keyword))
	trim = strings.TrimSpace(strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(trim, "{"), ":"), ";"))
	if strings.HasPrefix(trim, "(") && strings.HasSuffix(trim, ")") {
		trim = strings.TrimSpace(trim[1 : len(trim)-1])
	}
	return trim
}

func forHasRange(tokens []Lexeme, text string) bool {
	lower := strings.ToLower(text)
	if strings.Contains(lower, " in ") || strings.Contains(lower, "range(") || strings.Contains(text, "..") || strings.Contains(text, ":") {
		return true
	}
	// C/Go counting loops have semicolon-separated header clauses.
	return strings.Count(text, ";") >= 2
}

func assignmentExpression(tokens []Lexeme, text string) (string, string, bool) {
	depth := 0
	for i, token := range tokens {
		if token.Class == TokenDelimiter {
			switch token.Text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				if depth > 0 {
					depth--
				}
			}
		}
		if token.Class != TokenOperator || !isBindingOperator(token.Text) || depth != 0 {
			continue
		}
		name := ""
		for j := i - 1; j >= 0; j-- {
			if tokens[j].Class == TokenIdentifier && !isTypeWord(tokens[j].Text) && !strings.EqualFold(tokens[j].Text, "global") {
				name = tokens[j].Text
				break
			}
		}
		if name == "" {
			return "", "", false
		}
		at := strings.Index(text, token.Text)
		if at < 0 {
			return "", "", false
		}
		return name, strings.TrimSpace(text[at+len(token.Text):]), true
	}
	return "", "", false
}

func printExpression(text string) (string, bool) {
	trim := strings.TrimSpace(strings.TrimSuffix(text, ";"))
	lower := strings.ToLower(trim)
	if strings.Contains(lower, "cout") && strings.Contains(trim, "<<") {
		parts := strings.Split(trim, "<<")
		if len(parts) >= 2 {
			return strings.TrimSpace(parts[1]), true
		}
	}
	if strings.HasPrefix(lower, "echo ") {
		return strings.TrimSpace(trim[5:]), true
	}
	open := strings.Index(trim, "(")
	close := strings.LastIndex(trim, ")")
	if open < 0 || close <= open {
		return "", false
	}
	rawHead := strings.ToLower(strings.TrimSpace(trim[:open]))
	head := strings.ReplaceAll(rawHead, "!", "")
	if !(strings.Contains(head, "print") || strings.HasSuffix(head, "writeline")) {
		return "", false
	}
	inside := trim[open+1 : close]
	parts := splitTopLevel(inside, ',')
	if strings.HasPrefix(head, "printf") && len(parts) > 1 {
		return strings.TrimSpace(parts[len(parts)-1]), true
	}
	if strings.Contains(rawHead, "println!") && len(parts) > 1 {
		return strings.TrimSpace(parts[len(parts)-1]), true
	}
	if strings.Contains(strings.ToLower(trim), "std.debug.print") && len(parts) > 1 {
		x := strings.TrimSpace(parts[len(parts)-1])
		x = strings.TrimPrefix(x, ".{")
		x = strings.TrimSuffix(x, "}")
		return x, true
	}
	return strings.TrimSpace(inside), true
}

func normalizeExpression(source, expression string, profile Vector) string {
	e := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(expression), ";"))
	e, restore := protectLiterals(source, e)
	for strings.HasPrefix(e, "(") && strings.HasSuffix(e, ")") && balancedOuter(e) {
		e = strings.TrimSpace(e[1 : len(e)-1])
	}
	e = replaceWord(e, "true", "TRUE")
	e = replaceWord(e, "false", "FALSE")
	e = replaceWord(e, "None", "NULL")
	e = replaceWord(e, "null", "NULL")
	e = replaceWord(e, "nil", "NULL")
	e = replaceWord(e, "not", "!")
	e = strings.ReplaceAll(e, " and ", " && ")
	e = strings.ReplaceAll(e, " or ", " || ")
	e = strings.ReplaceAll(e, " div ", " %/% ")
	if strings.Contains(e, "@divTrunc") {
		numbers := numericTexts(Tokenize(source, e))
		if len(numbers) >= 2 {
			e = numbers[len(numbers)-2] + " %/% " + numbers[len(numbers)-1]
		}
	}
	for _, cast := range []string{"(double)", "(float)", "(int)"} {
		e = strings.ReplaceAll(e, cast, "")
	}
	if strings.Contains(e, "@as(") || strings.Contains(e, "@intCast(") {
		e = stripZigCasts(e)
	}
	if profile[GrammarIntegerSlashTruncates] != 0 {
		tokens := significant(Tokenize(source, e))
		for i := 1; i+1 < len(tokens); i++ {
			if tokens[i].Class == TokenOperator && tokens[i].Text == "/" && integerToken(tokens[i-1]) && integerToken(tokens[i+1]) {
				e = strings.Replace(e, "/", "%/%", 1)
				break
			}
		}
	}
	e = normalizeCollection(e)
	e = normalizeIndexes(source, e, profile)
	return restore(strings.TrimSpace(e))
}

func stripZigCasts(expression string) string {
	for {
		start := strings.LastIndex(expression, "@intCast(")
		nameLen := len("@intCast")
		if start < 0 {
			start = strings.LastIndex(expression, "@as(")
			nameLen = len("@as")
		}
		if start < 0 {
			return expression
		}
		open := start + nameLen
		close := matchingClose(expression, open)
		if close < 0 {
			return expression
		}
		inside := expression[open+1 : close]
		parts := splitTopLevel(inside, ',')
		value := strings.TrimSpace(parts[len(parts)-1])
		expression = expression[:start] + value + expression[close+1:]
	}
}

func matchingClose(text string, open int) int {
	depth := 0
	for i := open; i < len(text); i++ {
		switch text[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func numericTexts(tokens []Lexeme) []string {
	var out []string
	for _, token := range tokens {
		if token.Class == TokenNumber {
			out = append(out, token.Text)
		}
	}
	return out
}

func integerToken(token Lexeme) bool {
	return token.Class == TokenNumber && !strings.ContainsAny(token.Text, ".eE")
}

func normalizeCollection(e string) string {
	for _, head := range []string{"intArrayOf(", "arrayOf(", "listOf(", "vec!["} {
		if i := strings.Index(e, head); i >= 0 {
			body := e[i+len(head):]
			body = strings.TrimSuffix(strings.TrimSuffix(body, ")"), "]")
			return "c(" + body + ")"
		}
	}
	if open := strings.Index(e, "{"); open >= 0 && strings.HasSuffix(e, "}") {
		return "c(" + e[open+1:len(e)-1] + ")"
	}
	return e
}

func trimOuterCall(e string) string {
	e = strings.TrimSpace(e)
	if strings.HasPrefix(e, "(") && strings.HasSuffix(e, ")") && balancedOuter(e) {
		return strings.TrimSpace(e[1 : len(e)-1])
	}
	return e
}

func balancedOuter(e string) bool {
	depth := 0
	for i, r := range e {
		switch r {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && i != len([]rune(e))-1 {
				return false
			}
		}
	}
	return depth == 0
}

func splitTopLevel(text string, separator rune) []string {
	var out []string
	depth, start := 0, 0
	quote := rune(0)
	runes := []rune(text)
	for i, r := range runes {
		if quote != 0 {
			if r == quote && (i == 0 || runes[i-1] != '\\') {
				quote = 0
			}
			continue
		}
		if r == '\'' || r == '"' {
			quote = r
			continue
		}
		switch r {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		default:
			if r == separator && depth == 0 {
				out = append(out, string(runes[start:i]))
				start = i + 1
			}
		}
	}
	out = append(out, string(runes[start:]))
	return out
}

func replaceWord(text, old, replacement string) string {
	var out strings.Builder
	runes := []rune(text)
	quote := rune(0)
	for i := 0; i < len(runes); {
		if quote != 0 {
			out.WriteRune(runes[i])
			if runes[i] == quote && (i == 0 || runes[i-1] != '\\') {
				quote = 0
			}
			i++
			continue
		}
		if runes[i] == '\'' || runes[i] == '"' || runes[i] == '`' {
			quote = runes[i]
			out.WriteRune(runes[i])
			i++
			continue
		}
		if unicode.IsLetter(runes[i]) || runes[i] == '_' {
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			word := string(runes[start:i])
			if strings.EqualFold(word, old) {
				out.WriteString(replacement)
			} else {
				out.WriteString(word)
			}
			continue
		}
		out.WriteRune(runes[i])
		i++
	}
	return out.String()
}
