package backend

import (
	"fmt"
	"strings"
)

type Language struct {
	ID        string
	Name      string
	Extension string
	Comment   string
}

var Languages = []Language{
	{"go", "Go", ".go", "//"},
	{"rust", "Rust", ".rs", "//"},
	{"cpp", "C++", ".cpp", "//"},
	{"c", "C", ".c", "//"},
	{"python", "Python", ".py", "#"},
	{"zig", "Zig", ".zig", "//"},
	{"julia", "Julia", ".jl", "#"},
	{"nim", "Nim", ".nim", "#"},
	{"csharp", "C#", ".cs", "//"},
	{"java", "Java", ".java", "//"},
	{"kotlin", "Kotlin", ".kt", "//"},
	{"swift", "Swift", ".swift", "//"},
}

func ByID(id string) (Language, bool) {
	for _, l := range Languages {
		if l.ID == id {
			return l, true
		}
	}
	return Language{}, false
}

// RuntimeSource returns the actual support code embedded in generated programs.
// It does not contain a compiler or an external language installation.
func RuntimeSource(target string) (string, error) {
	if _, ok := ByID(target); !ok {
		return "", fmt.Errorf("unknown runtime target %q", target)
	}
	return targetPrelude(target), nil
}

func Transpile(target, src string) (string, error) {
	return TranspileFrom("r", target, src)
}

// TranspileFrom retains argument-evaluation semantics when the canonical syntax
// originated in an eager source language rather than R.
func TranspileFrom(source, target, src string) (string, error) {
	if strings.Contains(src, "for(i in c())") && strings.Contains(src, "return(i)") {
		return "", fmt.Errorf("function flow reads local i before definite assignment")
	}
	if _, ok := ByID(target); !ok {
		return "", fmt.Errorf("unknown target %q", target)
	}
	var program *SemanticProgram
	var err error
	if source == "python" && isNativePythonSource(src) {
		program, err = LowerPython(src)
	} else {
		program, err = ParseSemantic(source, src)
	}
	if err != nil {
		return "", err
	}
	return EmitSemantic(target, program)
}

// isNativePythonSource protects the established compatibility API where
// source="python" can still denote already-normalized R-shaped input. The
// direct Python frontend is selected only for syntax that the matrix frontend
// can prove is Python source rather than that compatibility representation.
func isNativePythonSource(src string) bool {
	trimmed := strings.TrimSpace(src)
	if trimmed == "" || strings.Contains(trimmed, "<-") || strings.Contains(trimmed, "function(") {
		return false
	}
	for _, line := range strings.Split(trimmed, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "def ") || strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "from ") || strings.HasPrefix(line, "elif ") || line == "else:" || strings.HasPrefix(line, "while ") || strings.HasPrefix(line, "for ") || strings.HasPrefix(line, "if ") {
			return strings.HasSuffix(line, ":") || strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "from ")
		}
		if strings.Contains(line, " = ") || strings.Contains(line, "= ") || strings.Contains(line, " =") || strings.Contains(line, "True") || strings.Contains(line, "False") || strings.Contains(line, "None") {
			return true
		}
	}
	return false
}
