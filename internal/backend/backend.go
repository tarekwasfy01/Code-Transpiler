package backend

import "fmt"

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
	if _, ok := ByID(target); !ok {
		return "", fmt.Errorf("unknown target %q", target)
	}
	program, err := ParseSemantic(source, src)
	if err != nil {
		return "", err
	}
	return EmitSemantic(target, program)
}
