package highlight

import (
	"context"
	"unicode"

	"github.com/oligo/gvcode/textstyle/syntax"
)

type Language string

const (
	R      Language = "R"
	Go     Language = "Go"
	Rust   Language = "Rust"
	Cpp    Language = "C++"
	C      Language = "C"
	Python Language = "Python"
	Zig    Language = "Zig"
	Julia  Language = "Julia"
	Nim    Language = "Nim"
	CSharp Language = "C#"
	Java   Language = "Java"
	Kotlin Language = "Kotlin"
	Swift  Language = "Swift"
)

var rKeywords = wordSet("if", "else", "repeat", "while", "function", "for", "in", "next", "break", "return", "TRUE", "FALSE", "T", "F", "NULL", "NA", "Inf", "NaN")
var rBuiltins = wordSet("c", "list", "matrix", "array", "data.frame", "print", "cat", "length", "lengths", "names", "dim", "nrow", "ncol", "sum", "prod", "mean", "min", "max", "range", "sort", "order", "rank", "match", "which", "which.min", "which.max", "unique", "rev", "seq", "seq.int", "seq_len", "seq_along", "rep", "paste", "paste0", "nchar", "toupper", "tolower", "substr", "substring", "grep", "grepl", "sub", "gsub", "any", "all", "abs", "sqrt", "exp", "log", "sin", "cos", "tan", "floor", "ceiling", "round", "runif", "rnorm", "sample", "set.seed", "getwd", "setwd", "file.exists", "dir.create", "Sys.getenv", "Sys.time", "Sys.Date", "typeof", "class", "stop", "warning")

var genericKeywords = map[Language]map[string]bool{
	Go:     wordSet("break", "default", "func", "interface", "select", "case", "defer", "go", "map", "struct", "chan", "else", "goto", "package", "switch", "const", "fallthrough", "if", "range", "type", "continue", "for", "import", "return", "var", "true", "false", "nil"),
	Rust:   wordSet("as", "break", "const", "continue", "crate", "else", "enum", "extern", "false", "fn", "for", "if", "impl", "in", "let", "loop", "match", "mod", "move", "mut", "pub", "ref", "return", "self", "Self", "static", "struct", "super", "trait", "true", "type", "unsafe", "use", "where", "while"),
	Cpp:    wordSet("alignas", "alignof", "asm", "auto", "bool", "break", "case", "catch", "char", "class", "concept", "const", "constexpr", "continue", "default", "delete", "do", "double", "else", "enum", "explicit", "extern", "false", "float", "for", "friend", "if", "inline", "int", "long", "namespace", "new", "noexcept", "nullptr", "operator", "private", "protected", "public", "return", "short", "signed", "sizeof", "static", "struct", "switch", "template", "this", "throw", "true", "try", "typedef", "typename", "union", "unsigned", "using", "virtual", "void", "volatile", "while"),
	C:      wordSet("auto", "break", "case", "char", "const", "continue", "default", "do", "double", "else", "enum", "extern", "float", "for", "goto", "if", "inline", "int", "long", "register", "restrict", "return", "short", "signed", "sizeof", "static", "struct", "switch", "typedef", "union", "unsigned", "void", "volatile", "while"),
	Python: wordSet("and", "as", "assert", "async", "await", "break", "class", "continue", "def", "del", "elif", "else", "except", "False", "finally", "for", "from", "global", "if", "import", "in", "is", "lambda", "None", "nonlocal", "not", "or", "pass", "raise", "return", "True", "try", "while", "with", "yield"),
	Zig:    wordSet("align", "allowzero", "and", "anyframe", "anytype", "asm", "async", "await", "break", "catch", "comptime", "const", "continue", "defer", "else", "enum", "errdefer", "error", "export", "extern", "false", "fn", "for", "if", "inline", "noalias", "nosuspend", "null", "opaque", "or", "orelse", "packed", "pub", "resume", "return", "struct", "suspend", "switch", "test", "threadlocal", "true", "try", "undefined", "union", "unreachable", "usingnamespace", "var", "volatile", "while"),
	Julia:  wordSet("baremodule", "begin", "break", "catch", "const", "continue", "do", "else", "elseif", "end", "export", "false", "finally", "for", "function", "global", "if", "import", "let", "local", "macro", "module", "quote", "return", "struct", "true", "try", "using", "while"),
	Nim:    wordSet("addr", "and", "as", "asm", "bind", "block", "break", "case", "cast", "concept", "const", "continue", "converter", "defer", "discard", "distinct", "div", "do", "elif", "else", "end", "enum", "except", "export", "finally", "for", "from", "func", "if", "import", "in", "include", "interface", "is", "isnot", "iterator", "let", "macro", "method", "mixin", "mod", "nil", "not", "notin", "object", "of", "or", "out", "proc", "ptr", "raise", "ref", "return", "shl", "shr", "static", "template", "try", "tuple", "type", "using", "var", "when", "while", "with", "without", "xor", "yield"),
	CSharp: wordSet("abstract", "as", "base", "bool", "break", "byte", "case", "catch", "char", "checked", "class", "const", "continue", "decimal", "default", "delegate", "do", "double", "else", "enum", "event", "explicit", "extern", "false", "finally", "fixed", "float", "for", "foreach", "goto", "if", "implicit", "in", "int", "interface", "internal", "is", "lock", "long", "namespace", "new", "null", "object", "operator", "out", "override", "params", "private", "protected", "public", "readonly", "ref", "return", "sbyte", "sealed", "short", "sizeof", "stackalloc", "static", "string", "struct", "switch", "this", "throw", "true", "try", "typeof", "uint", "ulong", "unchecked", "unsafe", "ushort", "using", "virtual", "void", "volatile", "while", "var"),
	Java:   wordSet("abstract", "assert", "boolean", "break", "byte", "case", "catch", "char", "class", "const", "continue", "default", "do", "double", "else", "enum", "extends", "final", "finally", "float", "for", "goto", "if", "implements", "import", "instanceof", "int", "interface", "long", "native", "new", "package", "private", "protected", "public", "return", "short", "static", "strictfp", "super", "switch", "synchronized", "this", "throw", "throws", "transient", "try", "void", "volatile", "while", "true", "false", "null"),
	Kotlin: wordSet("as", "break", "class", "continue", "do", "else", "false", "for", "fun", "if", "in", "interface", "is", "null", "object", "package", "return", "super", "this", "throw", "true", "try", "typealias", "typeof", "val", "var", "when", "while"),
	Swift:  wordSet("associatedtype", "class", "deinit", "enum", "extension", "fileprivate", "func", "import", "init", "inout", "internal", "let", "open", "operator", "private", "protocol", "public", "rethrows", "static", "struct", "subscript", "typealias", "var", "break", "case", "continue", "default", "defer", "do", "else", "fallthrough", "for", "guard", "if", "in", "repeat", "return", "switch", "where", "while", "as", "Any", "catch", "false", "is", "nil", "super", "self", "Self", "throw", "throws", "true", "try"),
}

func Tokens(ctx context.Context, language Language, text string) ([]syntax.Token, error) {
	runes := []rune(text)
	tokens := make([]syntax.Token, 0, max(32, len(runes)/8))
	for i := 0; i < len(runes); {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		start := i
		r := runes[i]
		if language == R && r == '#' || (language != R && (r == '/' && i+1 < len(runes) && runes[i+1] == '/')) || ((language == Python || language == Julia || language == Nim) && r == '#') {
			for i < len(runes) && runes[i] != '\n' {
				i++
			}
			tokens = appendToken(tokens, start, i, "comment")
			continue
		}
		if language != R && r == '/' && i+1 < len(runes) && runes[i+1] == '*' {
			i += 2
			for i+1 < len(runes) && !(runes[i] == '*' && runes[i+1] == '/') {
				i++
			}
			if i+1 < len(runes) {
				i += 2
			}
			tokens = appendToken(tokens, start, i, "comment")
			continue
		}
		if r == '\'' || r == '"' || r == '`' {
			q := r
			i++
			for i < len(runes) {
				if runes[i] == '\\' && q != '`' && i+1 < len(runes) {
					i += 2
					continue
				}
				c := runes[i]
				i++
				if c == q {
					break
				}
			}
			tokens = appendToken(tokens, start, i, "literal.string")
			continue
		}
		if unicode.IsDigit(r) || r == '.' && i+1 < len(runes) && unicode.IsDigit(runes[i+1]) {
			i = scanNumber(runes, i)
			tokens = appendToken(tokens, start, i, "literal.number")
			continue
		}
		if identStart(r, language) {
			i++
			for i < len(runes) && identContinue(runes[i], language) {
				i++
			}
			w := string(runes[start:i])
			scope := syntax.StyleScope("")
			if language == R {
				if rKeywords[w] {
					scope = "keyword"
				} else if nextNonSpace(runes, i) == '(' {
					scope = "name.function"
				}
			} else if genericKeywords[language][w] {
				scope = "keyword"
			} else if nextNonSpace(runes, i) == '(' {
				scope = "name.function"
			}
			tokens = appendToken(tokens, start, i, scope)
			continue
		}
		if containsRune("+-*/^:~!?<>=&|$@%", r) {
			i++
			for i < len(runes) && containsRune("+-*/^:~!?<>=&|$@%", runes[i]) {
				i++
			}
			tokens = appendToken(tokens, start, i, "operator")
			continue
		}
		if containsRune("(){}[],;.", r) {
			i++
			tokens = appendToken(tokens, start, i, "punctuation")
			continue
		}
		i++
	}
	return tokens, nil
}
func scanNumber(r []rune, i int) int {
	for i < len(r) && (unicode.IsDigit(r[i]) || containsRune(".eExXaAbBcCdDfF+-", r[i])) {
		i++
	}
	return i
}
func appendToken(t []syntax.Token, s, e int, scope syntax.StyleScope) []syntax.Token {
	if scope == "" || e <= s {
		return t
	}
	return append(t, syntax.Token{Start: s, End: e, Scope: scope})
}
func wordSet(w ...string) map[string]bool {
	m := map[string]bool{}
	for _, x := range w {
		m[x] = true
	}
	return m
}
func identStart(r rune, l Language) bool {
	return unicode.IsLetter(r) || r == '_' || (l == R && r == '.')
}
func identContinue(r rune, l Language) bool { return identStart(r, l) || unicode.IsDigit(r) }
func nextNonSpace(r []rune, i int) rune {
	for i < len(r) && unicode.IsSpace(r[i]) {
		i++
	}
	if i < len(r) {
		return r[i]
	}
	return 0
}
func containsRune(s string, w rune) bool {
	for _, r := range s {
		if r == w {
			return true
		}
	}
	return false
}
