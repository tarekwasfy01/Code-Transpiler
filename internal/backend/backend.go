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

func Transpile(target, src string) (string, error) {
	if _, ok := ByID(target); !ok {
		return "", fmt.Errorf("unknown target %q", target)
	}
	ast, err := parse(src)
	if err != nil {
		return "", err
	}
	return generateTarget(target, ast)
}
