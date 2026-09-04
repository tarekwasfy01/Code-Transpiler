package manytomany

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// TranspileRequest is the single immutable input boundary shared by the GUI
// and CLI. Source is intentionally kept byte-for-byte: the core never strips
// BOMs, normalizes line endings, or rewrites editor text before parsing.
type TranspileRequest struct {
	Source, SourceLanguage, TargetLanguage, EntryPoint string
	// DisableRuntimeFallback is opt-in so existing CLI/API callers retain the
	// established NATIVE -> semantic-runtime -> ERROR order.
	DisableRuntimeFallback bool
}

// TranspileTrace makes a GUI/CLI divergence observable without exposing a
// second semantic representation.
type TranspileTrace struct {
	SourceLength           int    `json:"source_length"`
	SourceSHA256           string `json:"source_sha256"`
	NormalizedSourceSHA256 string `json:"normalized_source_sha256"`
	SourceLanguage         string `json:"source_language"`
	TargetLanguage         string `json:"target_language"`
	EntryPoint             string `json:"entrypoint"`
	UASTSHA256             string `json:"uast_hash,omitempty"`
	ProjectionMode         string `json:"projection_mode,omitempty"`
	FinalSourceSHA256      string `json:"final_source_sha256,omitempty"`
	ErrorClass             string `json:"error_class,omitempty"`
	NativeAttempt          bool   `json:"native_attempt"`
	RuntimeFallback        bool   `json:"runtime_fallback"`
	IntermediateRoute      string `json:"intermediate_route,omitempty"`
}

type TranspileResult struct {
	Code  string
	Trace TranspileTrace
}

func sha256Text(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// TranspileCore is the only productive source-to-target path. Frontends call
// it with a copied request; Parse -> canonical UAST -> projector is therefore
// identical for GUI and CLI invocations.
func TranspileCore(request TranspileRequest) (TranspileResult, error) {
	source, target := normalize(request.SourceLanguage), normalize(request.TargetLanguage)
	trace := TranspileTrace{SourceLength: len(request.Source), SourceSHA256: sha256Text(request.Source), NormalizedSourceSHA256: sha256Text(request.Source), SourceLanguage: source, TargetLanguage: target, EntryPoint: request.EntryPoint, NativeAttempt: true}
	if source == target {
		trace.FinalSourceSHA256 = trace.SourceSHA256
		return TranspileResult{Code: request.Source, Trace: trace}, nil
	}
	p, err := Parse(source, request.Source)
	if err != nil {
		trace.ErrorClass = err.Error()
		return TranspileResult{Trace: trace}, err
	}
	if p.Semantic != nil {
		if wire, e := p.Semantic.MarshalSemanticJSON(); e == nil {
			trace.UASTSHA256 = sha256Text(string(wire))
		}
	}
	code, err := EmitDirect(target, p)
	if err != nil {
		if request.DisableRuntimeFallback {
			trace.ErrorClass = err.Error()
			return TranspileResult{Trace: trace}, err
		}
		// Native and semantic-runtime emission are exhausted at this point.
		// Use the matrix-defined language set as a bounded, cycle-free
		// compatibility route: emit one intermediate target, parse that output
		// through the same frontend, then emit the requested target.  This is a
		// last resort and never replaces a successful direct/runtime result.
		viaCode, via, routeErr := transpileViaIntermediate(p, target)
		if routeErr != nil {
			// Only after all strict matrix routes are exhausted use the explicit
			// semantic-runtime projector as the last resort.
			code, err = Emit(target, p)
			if err != nil {
				trace.ErrorClass = err.Error()
				return TranspileResult{Trace: trace}, err
			}
		} else {
			code = viaCode
			trace.IntermediateRoute = via
			trace.ProjectionMode = "matrix-route"
		}
	}
	if request.DisableRuntimeFallback && backend.AnalyzeRuntimeTaint(code, nil).Tainted() {
		err = fmt.Errorf("RUNTIME_DISABLED: native target emission unavailable for %s", target)
		trace.ErrorClass = err.Error()
		return TranspileResult{Trace: trace}, err
	}
	trace.FinalSourceSHA256 = sha256Text(code)
	trace.RuntimeFallback = backend.AnalyzeRuntimeTaint(code, nil).Tainted()
	if trace.RuntimeFallback {
		trace.ProjectionMode = "compatibility-runtime"
	} else {
		trace.ProjectionMode = "native-first"
	}
	return TranspileResult{Code: code, Trace: trace}, nil
}

// transpileViaIntermediate tries each available language exactly once as an
// intermediate representation.  It deliberately works on Program/Semantic
// values and never calls TranspileCore recursively, so routes cannot cycle or
// multiply indefinitely.
func transpileViaIntermediate(program Program, target string) (string, string, error) {
	for _, intermediate := range Languages {
		if intermediate == target || intermediate == program.Source {
			continue
		}
		// Prefer a fully native intermediate. If that target lacks one
		// operation, its existing compatibility rendering is still useful as a
		// bounded bridge: the generated target source is parsed once, then the
		// requested target is attempted natively. This is strictly before the
		// document-level runtime fallback and never recurses through TranspileCore.
		middle, err := EmitDirect(intermediate, program)
		routeKind := "native"
		if err != nil || middle == "" {
			middle, err = Emit(intermediate, program)
			routeKind = "compat"
		}
		if err != nil || middle == "" {
			continue
		}
		reparsed, err := Parse(intermediate, middle)
		if err != nil {
			continue
		}
		final, err := EmitDirect(target, reparsed)
		if err == nil && final != "" {
			return final, program.Source + "->" + intermediate + "->" + target + "(" + routeKind + ")", nil
		}
	}
	return "", "", fmt.Errorf("no intermediate route from %s to %s", program.Source, target)
}

// Language is both a supported source and target language.
var Languages = []string{"r", "go", "rust", "cpp", "c", "python", "zig", "julia", "nim", "csharp", "java", "kotlin", "swift"}

type IRKind string

const (
	IRAssign IRKind = "assign"
	IRPrint  IRKind = "print"
	IRReturn IRKind = "return"
	IRExpr   IRKind = "expr"
)

type Statement struct {
	Kind       IRKind // retained for diagnostics; emission uses Semantic
	Name       string
	Expr       string
	Semantic   matrixir.Vector
	MatrixNode int
}

type Program struct {
	Semantic     *backend.SemanticProgram
	Source       string
	Statements   []Statement
	Graph        *matrixir.Graph
	Requirements matrixir.Vector
	// CanonicalR is retained only as a compatibility diagnostic serialization.
	// Emit, function-flow and route fanout use Semantic exclusively.
	CanonicalR string
	Actions    matrixir.Matrix
	Grammar    matrixir.Vector
}

// Document returns the neutral interchange representation. No caller needs to
// read CanonicalR in order to pass a parsed program to another target.
func (p Program) Document() (backend.SemanticDocument, error) {
	if p.Semantic == nil {
		return backend.SemanticDocument{}, fmt.Errorf("program has no semantic representation")
	}
	return p.Semantic.Document()
}

// ParseDocument accepts only the versioned semantic interchange format. It is
// intentionally separate from Parse(source, code): source frontends are free
// to evolve, while transport between them is language-neutral.
func ParseDocument(data []byte) (Program, error) {
	semantic, err := backend.ParseSemanticJSON(data)
	if err != nil {
		return Program{}, err
	}
	// The verified semantic relation matrices already describe the program.
	// Importing JSON must not require an R serialization or reparsed R graph.
	return Program{Source: "semantic", Semantic: semantic}, nil
}

// Parse lowers a conservative common subset of each supported source language
// into R2Many CIR. It deliberately rejects constructs it cannot lower safely.
func Parse(source, code string) (Program, error) {
	source = normalize(source)
	if !supported(source) {
		return Program{}, fmt.Errorf("unsupported source language %q", source)
	}
	// Go source has a real Go-AST frontend. Never feed Go tokens (closures,
	// slices, braces or semicolons) to the generic text canonicalizer: that
	// path produced misleading target-looking errors such as "expected
	// expression near )" before any UAST was built.
	if source == "go" && strings.HasPrefix(strings.TrimSpace(code), "package ") {
		semantic, err := backend.LowerNativeGo("input.go", code)
		if err != nil {
			return Program{}, fmt.Errorf("go frontend: %w", err)
		}
		return Program{Source: source, Semantic: semantic}, nil
	}
	if semantic, recognized, err := backend.DecodeGenerated(source, code); recognized {
		if err != nil {
			return Program{}, err
		}
		graph, _, err := matrixir.BuildLexicalGraph(source, code)
		if err != nil {
			return Program{}, err
		}
		view, err := semantic.RSource(false)
		if err != nil {
			return Program{}, err
		}
		return Program{Source: source, Semantic: semantic, Graph: graph, Requirements: graph.Requirements(), CanonicalR: view}, nil
	}
	if source == "r" {
		graph, _, err := matrixir.BuildLexicalGraph(source, code)
		if err != nil {
			return Program{}, err
		}
		semantic, err := backend.ParseSemantic(source, code)
		if err != nil {
			return Program{}, err
		}
		view, err := semantic.RSource(false)
		if err != nil {
			return Program{}, err
		}
		return Program{Source: source, Semantic: semantic, Graph: graph, Requirements: graph.Requirements(), CanonicalR: view}, nil
	}
	canonical, err := matrixir.Canonicalize(source, code)
	if err != nil {
		return Program{}, err
	}
	// Every non-native frontend uses the same typed-event -> FrontendFacts ->
	// UAST producer.  Do not pass event text into the legacy SemanticDocument
	// parser here: that would make brackets, closures and delimiters depend on
	// source spelling rather than the matrix grammar decision.
	semantic, err := backend.LowerMatrixLanguage(source, code)
	if err != nil {
		return Program{}, err
	}
	view, err := semantic.RSource(false)
	if err != nil {
		return Program{}, err
	}
	return Program{Source: source, Semantic: semantic, Graph: canonical.Graph, Requirements: canonical.Graph.Requirements(), CanonicalR: view, Actions: canonical.Actions, Grammar: canonical.Grammar}, nil
}

func Emit(target string, p Program) (string, error) {
	target = normalize(target)
	if !supported(target) {
		return "", fmt.Errorf("unsupported target %q", target)
	}
	if p.Semantic != nil {
		return backend.EmitSemantic(target, p.Semantic)
	}
	if p.CanonicalR != "" {
		if target == "r" {
			return p.CanonicalR, nil
		}
		return backend.TranspileFrom(p.Source, target, p.CanonicalR)
	}
	if err := validateLoweringMatrix(p, target); err != nil {
		return "", err
	}
	statements, err := orderedStatements(p)
	if err != nil {
		return "", err
	}
	// From here onward the control relation matrix is the source of statement
	// order; the original slice is retained only as node payload storage.
	p.Statements = statements
	if p.Source == "r" {
		return backend.Transpile(target, p.Statements[0].Expr)
	}
	if target == "r" {
		return emitR(p), nil
	}
	var b strings.Builder
	switch target {
	case "go":
		b.WriteString("package main\n\nimport \"fmt\"\n\nfunc main() {\n")
	case "rust":
		b.WriteString("fn main() {\n")
	case "cpp":
		b.WriteString("#include <iostream>\nint main() {\n")
	case "c":
		b.WriteString("#include <stdio.h>\nint main(void) {\n")
	case "python": // no wrapper
	case "java":
		b.WriteString("public class Main { public static void main(String[] args) {\n")
	case "csharp":
		b.WriteString("using System;\nclass Program { static void Main() {\n")
	case "kotlin":
		b.WriteString("fun main() {\n")
	case "swift": // top-level
	case "zig":
		b.WriteString("const std = @import(\"std\");\npub fn main() !void {\n")
	case "julia": // top-level
	case "nim": // top-level
	}
	for _, s := range p.Statements {
		b.WriteString(emitStmt(target, s))
	}
	switch target {
	case "go", "rust", "cpp", "c", "kotlin", "zig":
		b.WriteString("}\n")
	case "java", "csharp":
		b.WriteString("}}\n")
	}
	return b.String(), nil
}

// EmitDirect requests one strict native projection without changing the
// normal Emit API, whose compatibility runtime fallback remains preserved.
func EmitDirect(target string, p Program) (string, error) {
	target = normalize(target)
	if !supported(target) {
		return "", fmt.Errorf("unsupported target %q", target)
	}
	if p.Semantic != nil {
		return backend.EmitSemanticDirect(target, p.Semantic)
	}
	return Emit(target, p)
}

func Transpile(source, target, code string) (string, error) {
	result, err := TranspileCore(TranspileRequest{Source: code, SourceLanguage: source, TargetLanguage: target, EntryPoint: "api"})
	return result.Code, err
}

func emitR(p Program) string {
	var b strings.Builder
	for _, s := range p.Statements {
		kind, err := vectorKind(s)
		if err != nil {
			continue
		}
		switch kind {
		case IRAssign:
			fmt.Fprintf(&b, "%s <- %s\n", s.Name, toRExpr(p.Source, s.Expr))
		case IRPrint:
			fmt.Fprintf(&b, "print(%s)\n", toRExpr(p.Source, s.Expr))
		case IRReturn:
			fmt.Fprintf(&b, "return(%s)\n", toRExpr(p.Source, s.Expr))
		default:
			b.WriteString(toRExpr(p.Source, s.Expr) + "\n")
		}
	}
	return b.String()
}

func emitStmt(t string, s Statement) string {
	e := translateExpr(s.Expr)
	ind := "    "
	if t == "python" || t == "swift" || t == "julia" || t == "nim" {
		ind = ""
	}
	kind, err := vectorKind(s)
	if err != nil {
		return ""
	}
	switch kind {
	case IRAssign:
		switch t {
		case "go":
			return ind + s.Name + " := " + e + "\n"
		case "rust":
			return ind + "let mut " + s.Name + " = " + e + ";\n"
		case "cpp":
			return ind + "auto " + s.Name + " = " + e + ";\n"
		case "c":
			return ind + "double " + s.Name + " = " + e + ";\n"
		case "java":
			return ind + "var " + s.Name + " = " + e + ";\n"
		case "csharp":
			return ind + "var " + s.Name + " = " + e + ";\n"
		case "kotlin":
			return ind + "var " + s.Name + " = " + e + "\n"
		case "swift":
			return "var " + s.Name + " = " + e + "\n"
		case "zig":
			return ind + "var " + s.Name + " = " + e + ";\n"
		case "julia", "python", "nim":
			return s.Name + " = " + e + "\n"
		}
	case IRPrint:
		switch t {
		case "go":
			return ind + "fmt.Println(" + e + ")\n"
		case "rust":
			return ind + "println!(\"{:?}\", " + e + ");\n"
		case "cpp":
			// Parenthesize the whole translated expression. C++ stream insertion
			// binds tighter than relational operators, so an unwrapped `a < b`
			// would otherwise be parsed as `(std::cout << a) < b`.
			return ind + "std::cout << (" + e + ") << std::endl;\n"
		case "c":
			return ind + "printf(\"%g\\n\", (double)(" + e + "));\n"
		case "python":
			return "print(" + e + ")\n"
		case "java":
			return ind + "System.out.println(" + e + ");\n"
		case "csharp":
			return ind + "Console.WriteLine(" + e + ");\n"
		case "kotlin":
			return ind + "println(" + e + ")\n"
		case "swift":
			return "print(" + e + ")\n"
		case "julia":
			return "println(" + e + ")\n"
		case "nim":
			return "echo " + e + "\n"
		case "zig":
			return ind + "std.debug.print(\"{}\\n\", .{" + e + "});\n"
		}
	case IRReturn:
		return ind + "return " + e + terminator(t)
	default:
		return ind + e + terminator(t)
	}
	return ""
}
func terminator(t string) string {
	switch t {
	case "go", "rust", "cpp", "c", "java", "csharp", "zig":
		return ";\n"
	default:
		return "\n"
	}
}

var assignRE = regexp.MustCompile(`^(?:(?:const\s+)?(?:unsigned\s+|signed\s+)?(?:long\s+long|long|short|int|double|float|bool|char|size_t|auto|std::string|string|String)\s+|let\s+mut\s+|let\s+|var\s+|val\s+|const\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(?::[^=]+)?=\s*(.+)$`)

func parseAssign(src, line string) (string, string, bool) {
	m := assignRE.FindStringSubmatch(line)
	if m == nil {
		return "", "", false
	}
	return m[1], strings.TrimSpace(m[2]), true
}
func parsePrint(src, line string) (string, bool) {
	prefixes := []string{"print(", "println(", "fmt.Println(", "System.out.println(", "Console.WriteLine("}
	for _, p := range prefixes {
		if strings.HasPrefix(line, p) {
			x := strings.TrimPrefix(line, p)
			x = strings.TrimSuffix(x, ")")
			return strings.TrimSpace(x), true
		}
	}

	// C++ stream output: std::cout << expr << std::endl  or cout << expr << endl
	if strings.HasPrefix(line, "std::cout << ") || strings.HasPrefix(line, "cout << ") {
		x := line
		if strings.HasPrefix(x, "std::cout << ") {
			x = strings.TrimPrefix(x, "std::cout << ")
		} else {
			x = strings.TrimPrefix(x, "cout << ")
		}
		for _, tail := range []string{"<< std::endl", "<< endl", `<< "\n"`, `<< '\n'`} {
			if i := strings.Index(x, tail); i >= 0 {
				x = x[:i]
				break
			}
		}
		return strings.TrimSpace(x), true
	}

	// Basic C printf("%g\n", expr) / printf("%d\n", expr) lowering.
	if strings.HasPrefix(line, "printf(") {
		if comma := strings.Index(line, ","); comma >= 0 {
			x := strings.TrimSpace(strings.TrimSuffix(line[comma+1:], ")"))
			return x, true
		}
	}

	if strings.HasPrefix(line, "echo ") {
		return strings.TrimSpace(strings.TrimPrefix(line, "echo ")), true
	}
	if strings.HasPrefix(line, "println!(") {
		x := strings.TrimSuffix(strings.TrimPrefix(line, "println!("), ")")
		if i := strings.LastIndex(x, ","); i >= 0 {
			x = x[i+1:]
		}
		return strings.TrimSpace(x), true
	}
	return "", false
}

func isBoilerplate(src, line string) bool {
	switch src {
	case "cpp", "c":
		if strings.HasPrefix(line, "#include ") || strings.HasPrefix(line, "#define ") ||
			strings.HasPrefix(line, "#pragma ") || line == "using namespace std;" ||
			strings.HasPrefix(line, "using std::") {
			return true
		}
	case "go":
		if strings.HasPrefix(line, "package ") || strings.HasPrefix(line, "import ") {
			return true
		}
	case "rust":
		if strings.HasPrefix(line, "use ") || strings.HasPrefix(line, "extern crate ") {
			return true
		}
	case "java":
		if strings.HasPrefix(line, "import ") || strings.HasPrefix(line, "package ") {
			return true
		}
	case "csharp":
		if strings.HasPrefix(line, "using ") || strings.HasPrefix(line, "namespace ") {
			return true
		}
	}
	return false
}

func isMainWrapper(src, line string) bool {
	compact := strings.Join(strings.Fields(line), " ")
	switch src {
	case "cpp":
		return strings.HasSuffix(compact, "{") &&
			(strings.HasPrefix(compact, "int main(") || strings.HasPrefix(compact, "auto main("))
	case "c":
		return strings.HasSuffix(compact, "{") && strings.HasPrefix(compact, "int main(")
	case "go":
		return compact == "func main() {" || compact == "func main(){"
	case "rust":
		return compact == "fn main() {" || compact == "fn main(){"
	case "java":
		return strings.Contains(compact, "static void main(") && strings.HasSuffix(compact, "{")
	case "csharp":
		return strings.Contains(compact, "static void Main(") && strings.HasSuffix(compact, "{")
	case "kotlin":
		return strings.HasPrefix(compact, "fun main(") && strings.HasSuffix(compact, "{")
	case "zig":
		return strings.HasPrefix(compact, "pub fn main(") && strings.HasSuffix(compact, "{")
	}
	return false
}

func translateExpr(e string) string {
	e = strings.TrimSpace(strings.TrimSuffix(e, ";"))
	e = strings.ReplaceAll(e, "true", "true")
	e = strings.ReplaceAll(e, "false", "false")
	return e
}
func toRExpr(src, e string) string {
	e = strings.TrimSpace(strings.TrimSuffix(e, ";"))
	e = strings.ReplaceAll(e, "true", "TRUE")
	e = strings.ReplaceAll(e, "false", "FALSE")
	// common collection literals
	if strings.HasPrefix(e, "[]") {
		if i := strings.Index(e, "{"); i >= 0 {
			e = "c(" + strings.TrimSuffix(e[i+1:], "}") + ")"
		}
	}
	if strings.HasPrefix(e, "vec![") {
		e = "c(" + strings.TrimSuffix(strings.TrimPrefix(e, "vec!["), "]") + ")"
	}
	return e
}
func stripComment(src, s string) string {
	if src == "python" || src == "julia" || src == "nim" {
		if i := strings.Index(s, "#"); i >= 0 {
			return s[:i]
		}
	}
	if i := strings.Index(s, "//"); i >= 0 {
		return s[:i]
	}
	return s
}
func normalize(s string) string {
	return backend.NormalizeLanguage(s)
}
func supported(s string) bool {
	return backend.HasFrontend(s)
}

var _ = strconv.Itoa
