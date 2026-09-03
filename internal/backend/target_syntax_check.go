package backend

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// TargetSyntaxCheck is an observation of a parser/compiler in syntax-only
// mode. Checked=false means the local toolchain is unavailable; it is not a
// positive syntax proof and is kept separate in reports.
type TargetSyntaxCheck struct {
	Checked bool   `json:"checked"`
	Valid   bool   `json:"valid"`
	Tool    string `json:"tool,omitempty"`
	Failure string `json:"failure,omitempty"`
}

// SyntaxFailureSignature groups actual target diagnostics into the stable
// matrix vocabulary. It does not infer a cause; unrecognised diagnostics are
// preserved as "other" for review.
func SyntaxFailureSignature(diagnostic string) string {
	d := strings.ToLower(diagnostic)
	for _, item := range []struct{ needle, signature string }{
		{"expected expression", "expected expression"},
		{"expected statement", "expected statement"},
		{"unexpected token", "unexpected token"},
		{"expected ';'", "missing delimiter"},
		{"expected `;`", "missing delimiter"},
		{"invalid operator", "invalid operator"},
		{"invalid call", "invalid call"},
		{"invalid index", "invalid index"},
		{"invalid declaration", "invalid declaration"},
		{"invalid assignment", "invalid assignment target"},
	} {
		if strings.Contains(d, item.needle) {
			return item.signature
		}
	}
	if strings.TrimSpace(d) == "" {
		return ""
	}
	return "other"
}

// CheckTargetSyntax parses generated source without executing it. Native
// output that fails an available target parser is ineligible for DIRECT and
// will reach the central compatibility fallback.
func CheckTargetSyntax(target, source string) TargetSyntaxCheck {
	extension := map[string]string{"go": ".go", "python": ".py", "rust": ".rs", "c": ".c", "cpp": ".cpp", "zig": ".zig", "julia": ".jl", "nim": ".nim", "csharp": ".cs", "java": ".java", "kotlin": ".kt", "swift": ".swift", "r": ".R"}[target]
	if extension == "" {
		return TargetSyntaxCheck{Failure: "unknown target"}
	}
	dir, err := os.MkdirTemp("", "codetranspiler-syntax-")
	if err != nil {
		return TargetSyntaxCheck{Failure: err.Error()}
	}
	defer os.RemoveAll(dir)
	base := "program"
	if target == "java" {
		base = "Main"
	}
	file := filepath.Join(dir, base+extension)
	if err := os.WriteFile(file, []byte(source), 0o600); err != nil {
		return TargetSyntaxCheck{Failure: err.Error()}
	}
	tool, args := syntaxCommand(target, file)
	if tool == "" {
		return TargetSyntaxCheck{Failure: "no syntax checker configured"}
	}
	path, err := exec.LookPath(tool)
	if err != nil {
		return TargetSyntaxCheck{Tool: tool, Failure: "tool unavailable"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Dir = dir
	output, runErr := cmd.CombinedOutput()
	if runErr != nil {
		diagnostic := strings.TrimSpace(string(output))
		return TargetSyntaxCheck{Checked: true, Tool: tool, Failure: SyntaxFailureSignature(diagnostic) + ": " + diagnostic}
	}
	return TargetSyntaxCheck{Checked: true, Valid: true, Tool: tool}
}

func syntaxCommand(target, file string) (string, []string) {
	switch target {
	case "go":
		// gofmt -d writes a diff and is not a validator contract: an ordinary
		// formatting diff must not demote valid Go to the runtime fallback.
		// Formatting a private temporary file still parses the complete source
		// and returns non-zero on syntax errors.
		return "gofmt", []string{"-w", file}
	case "python":
		return "python", []string{"-m", "py_compile", file}
	case "rust":
		return "rustc", []string{"--emit=metadata", file}
	case "c":
		return "gcc", []string{"-std=c11", "-fsyntax-only", file}
	case "cpp":
		return "g++", []string{"-std=c++17", "-fsyntax-only", file}
	case "zig":
		return "zig", []string{"ast-check", file}
	case "julia":
		return "julia", []string{"-e", "Meta.parse(read(ARGS[1], String))", file}
	case "nim":
		return "nim", []string{"check", "--hints:off", "--verbosity:0", file}
	case "csharp":
		return "csc", []string{"/nologo", "/target:library", file}
	case "java":
		return "javac", []string{"-d", filepath.Dir(file), file}
	case "kotlin":
		return "kotlinc", []string{file, "-d", filepath.Join(filepath.Dir(file), "check.jar")}
	case "swift":
		return "swiftc", []string{"-parse", file}
	case "r":
		return "Rscript", []string{"--vanilla", "-e", "parse(file=commandArgs(trailingOnly=TRUE)[1])", file}
	}
	return "", nil
}
