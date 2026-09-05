package matrixir

import (
	"fmt"
	"strconv"
	"strings"
)

// TokenStructure keeps class selection and delimiter incidence separate from
// semantic guesses. Strings/comments have zero weight in the code projection.
// Pairing is a syntax operation; multiplication alone cannot infer a grammar.
type TokenStructure struct {
	Classes Matrix
	Pairs   SparseMatrix
	Code    Vector
}

func AnalyzeTokenStructure(tokens []Lexeme) (TokenStructure, error) {
	s := TokenStructure{Classes: NewMatrix(len(tokens), int(TokenClassCount)), Pairs: NewSparseMatrix(len(tokens), len(tokens))}
	selector := NewMatrix(int(TokenClassCount), 1)
	for c := 0; c < int(TokenClassCount); c++ {
		if c != int(TokenString) && c != int(TokenComment) {
			selector.Set(c, 0, 1)
		}
	}
	stack := []int{}
	closes := map[string]string{")": "(", "]": "[", "}": "{"}
	for i, t := range tokens {
		s.Classes.Set(i, int(t.Class), 1)
		if t.Class == TokenString {
			r := []rune(t.Text)
			if len(r) < 2 || r[len(r)-1] != r[0] {
				return s, fmt.Errorf("unterminated string at %d", t.Start)
			}
			continue
		}
		if t.Class != TokenDelimiter {
			continue
		}
		if t.Text == "(" || t.Text == "[" || t.Text == "{" {
			stack = append(stack, i)
		}
		if open, ok := closes[t.Text]; ok {
			if len(stack) == 0 || tokens[stack[len(stack)-1]].Text != open {
				return s, fmt.Errorf("unmatched delimiter %q at %d", t.Text, t.Start)
			}
			left := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			s.Pairs.Set(left, i, 1)
		}
	}
	if len(stack) > 0 {
		t := tokens[stack[len(stack)-1]]
		return s, fmt.Errorf("unclosed delimiter %q at %d", t.Text, t.Start)
	}
	projection, err := s.Classes.Multiply(selector)
	if err != nil {
		return s, err
	}
	s.Code = Vector(projection.Data)
	return s, nil
}

// protectLiterals derives its selection from the class matrix. Replacements
// operate on code tokens only, even when literal text looks like source code.
func protectLiterals(source, text string) (string, func(string) string) {
	tokens := Tokenize(source, text)
	classes := NewMatrix(len(tokens), int(TokenClassCount))
	selector := NewMatrix(int(TokenClassCount), 1)
	selector.Set(int(TokenString), 0, 1)
	for i, t := range tokens {
		classes.Set(i, int(t.Class), 1)
	}
	mask, _ := classes.Multiply(selector)
	prefix := "__matrix_literal_"
	for strings.Contains(text, prefix) {
		prefix = "_" + prefix
	}
	literals := map[string]string{}
	runes := []rune(text)
	at := 0
	var b strings.Builder
	for i, t := range tokens {
		if mask.At(i, 0) == 0 && t.Class != TokenComment {
			continue
		}
		b.WriteString(string(runes[at:t.Start]))
		if mask.At(i, 0) != 0 {
			name := fmt.Sprintf("%s%d", prefix, i)
			literals[name] = t.Text
			b.WriteString(name)
		}
		at = t.End
	}
	b.WriteString(string(runes[at:]))
	restore := func(code string) string {
		r := []rune(code)
		pos := 0
		var out strings.Builder
		for _, t := range Tokenize("r", code) {
			if value, ok := literals[t.Text]; ok {
				out.WriteString(string(r[pos:t.Start]))
				out.WriteString(value)
				pos = t.End
			}
		}
		out.WriteString(string(r[pos:]))
		return out.String()
	}
	return b.String(), restore
}

// statementSegments uses paired delimiters, not physical lines, to keep
// expressions intact while separating semicolons and brace-delimited bodies.
// Indentation is retained as a source grammar input for Python/Nim.
func statementSegments(source, code string, tokens []Lexeme, structure TokenStructure) []sourceLine {
	var out []sourceLine
	var current []Lexeme
	var b strings.Builder
	runes := []rune(code)
	start, indent, previousEnd := 0, 0, 0
	depth := 0
	blockOpen := map[int]bool{}
	blockClose := map[int]bool{}
	flush := func() {
		text := strings.TrimSpace(b.String())
		if text != "" {
			out = append(out, sourceLine{text: text, trim: text, indent: indent, start: start, tokens: significant(Tokenize(source, text))})
		}
		b.Reset()
		current = nil
	}
	appendToken := func(t Lexeme) {
		if b.Len() == 0 {
			start = t.Start
			line := t.Start
			for line > 0 && runes[line-1] != '\n' {
				line--
			}
			indent = indentation(string(runes[line:t.Start]))
			previousEnd = t.Start
		}
		if t.Start > previousEnd {
			b.WriteByte(' ')
		}
		b.WriteString(t.Text)
		previousEnd = t.End
		current = append(current, t)
	}
	// Conditional expressions are a single grammar construct even when their
	// arms are placed on separate physical lines.  Keep only a proven `?`/`:`
	// token pair together; indentation colons without `?` retain their normal
	// statement/block meaning.
	conditionalContinuation := func(tokens []Lexeme, next int) bool {
		hasQuestion := false
		for _, token := range tokens {
			if token.Class == TokenOperator && token.Text == "?" {
				hasQuestion = true
				break
			}
		}
		if len(tokens) > 0 {
			last := tokens[len(tokens)-1]
			if last.Class == TokenOperator && (last.Text == "?" || (last.Text == ":" && hasQuestion)) {
				return true
			}
		}
		for j := next; j < len(tokens); j++ {
			if tokens[j].Class == TokenNewline || tokens[j].Class == TokenComment {
				continue
			}
			return tokens[j].Class == TokenOperator && (tokens[j].Text == "?" || tokens[j].Text == ":")
		}
		return false
	}
	// Function values may split their introducer from the parameter list across
	// physical lines (R's `function\n() value`, Go's `func\n(...)`, and the
	// compact anonymous-function marker used by several grammars).  Pair the
	// already-tokenized header with its opening delimiter before deciding a
	// statement boundary. This is a grammar-token continuation rule, not a
	// source-text semantic reconstruction.
	functionHeaderContinuation := func(tokens []Lexeme, next int) bool {
		if len(tokens) == 0 {
			return false
		}
		last := tokens[len(tokens)-1]
		if !(last.Class == TokenIdentifier && isFunctionWord(last.Text)) && last.Text != "\\" {
			return false
		}
		for j := next; j < len(tokens); j++ {
			if tokens[j].Class == TokenNewline || tokens[j].Class == TokenComment {
				continue
			}
			return tokens[j].Class == TokenDelimiter && tokens[j].Text == "("
		}
		return false
	}
	for i, t := range tokens {
		if t.Class == TokenComment {
			continue
		}
		if t.Class == TokenNewline {
			if depth == 0 {
				if conditionalContinuation(current, i+1) || functionHeaderContinuation(current, i+1) {
					b.WriteByte(' ')
				} else {
					flush()
				}
			} else if b.Len() > 0 {
				b.WriteByte(' ')
			}
			continue
		}
		if t.Class == TokenDelimiter && t.Text == ";" && depth == 0 && !startsKeyword(current, "for") {
			flush()
			continue
		}
		if t.Class == TokenDelimiter && t.Text == "{" && depth == 0 {
			candidate := strings.TrimSpace(b.String()) + " {"
			headerTokens := append(append([]Lexeme(nil), current...), t)
			block := isFunctionHeader(headerTokens, candidate) || isObjectWrapper(headerTokens, candidate)
			for _, kw := range []string{"if", "else", "while", "for", "repeat"} {
				block = block || startsKeyword(current, kw)
			}
			if block {
				blockOpen[i] = true
				for j := i + 1; j < len(tokens); j++ {
					if structure.Pairs.At(i, j) != 0 {
						blockClose[j] = true
						break
					}
				}
			}
		}
		if blockClose[i] {
			flush()
			appendToken(t)
			flush()
			continue
		}
		appendToken(t)
		if blockOpen[i] {
			flush()
			continue
		}
		if t.Class == TokenDelimiter {
			switch t.Text {
			case "(", "[", "{":
				depth++
			case ")", "]", "}":
				depth--
			}
		}
	}
	flush()
	// Keep closing braces attached to else so the control stack knows the then
	// branch has already ended, including when the source used a newline.
	for i := 1; i < len(out); i++ {
		if strings.HasPrefix(out[i].trim, "else") && out[i-1].trim == "}" {
			out[i].trim = "} " + out[i].trim
			out[i].text = out[i].trim
			out[i].tokens = significant(Tokenize(source, out[i].trim))
			out[i-1].trim = ""
		}
	}
	return out
}

// normalizeIndexes applies the source base vector to every paired index,
// innermost first. Strings are already protected by the class projection.
func normalizeIndexes(source, text string, profile Vector) string {
	tokens := Tokenize(source, text)
	structure, err := AnalyzeTokenStructure(tokens)
	if err != nil {
		return text
	}
	runes := []rune(text)
	var rewrite func(int, int, int, int) string
	rewrite = func(first, last, left, right int) string {
		var b strings.Builder
		at := left
		for i := first; i < last; i++ {
			if tokens[i].Text != "[" || tokens[i].Class != TokenDelimiter {
				continue
			}
			close := -1
			for j := i + 1; j < last; j++ {
				if structure.Pairs.At(i, j) != 0 {
					close = j
					break
				}
			}
			if close < 0 {
				continue
			}
			b.WriteString(string(runes[at:tokens[i].Start]))
			inside := rewrite(i+1, close, tokens[i].End, tokens[close].Start)
			previous := previousSignificant(tokens, i-1)
			index := previous >= first && (tokens[previous].Class == TokenIdentifier || tokens[previous].Text == "]" || tokens[previous].Text == ")")
			if index {
				b.WriteByte('[')
				if profile[GrammarOneBasedIndex] == 0 {
					if n, err := strconv.Atoi(strings.TrimSpace(inside)); err == nil {
						b.WriteString(strconv.Itoa(n + 1))
					} else {
						b.WriteString("(" + inside + ") + 1")
					}
				} else {
					b.WriteString(inside)
				}
				b.WriteByte(']')
			} else {
				b.WriteString("c(" + inside + ")")
			}
			at = tokens[close].End
			i = close
		}
		b.WriteString(string(runes[at:right]))
		return b.String()
	}
	return rewrite(0, len(tokens), 0, len(runes))
}
