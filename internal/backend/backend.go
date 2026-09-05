package backend

import (
	"fmt"
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
	// Runtime assets are the concrete support sources used by the compatibility
	// path.  TargetSpec.Prelude is an optional declarative override and is empty
	// for targets whose checked-in runtime is provided by the established target
	// prelude.  Do not expose an empty asset merely because that override is
	// absent.
	return targetPreludeExisting(target), nil
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
	// All source text enters through the modern structured frontend.  The
	// historical text parser is intentionally not selectable by source spelling
	// or target; callers that need the old compatibility representation must use
	// an explicitly named compatibility API.
	program, err := LowerSource(source, "", src)
	if err != nil {
		return "", err
	}
	return EmitSemantic(target, program)
}
