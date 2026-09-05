package backend

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func directVectorBinary(target, left, right, op string) (string, bool) {
	switch target {
	case "go":
		return "func() []float64 { out := make([]float64, len(" + left + ")); for i, v := range " + left + " { out[i] = v " + op + " " + right + " }; return out }()", true
	case "python":
		return "[v " + op + " " + right + " for v in " + left + "]", true
	case "rust":
		return left + ".iter().map(|v| *v " + op + " " + right + ").collect::<Vec<f64>>()", true
	case "cpp":
		return "[&](){ std::vector<double> out = " + left + "; for (double& v : out) v = v " + op + " " + right + "; return out; }()", true
	case "csharp":
		return left + ".Select(v => v " + op + " " + right + ").ToArray()", true
	case "java":
		return "java.util.Arrays.stream(" + left + ").map(v -> v " + op + " " + right + ").toArray()", true
	case "kotlin":
		return left + ".map { it " + op + " " + right + " }", true
	case "swift":
		return left + ".map { $0 " + op + " " + right + " }", true
	default:
		return "", false
	}
}

// nativeAssignment is the UAST-only declaration edge.  It deliberately does
// not call assignSyntax because that helper is the compatibility renderer and
// still carries runtime value types for several targets.
func (g *targetGen) nativeAssignment(name, expression string) string {
	for i := len(g.declared) - 1; i >= 0; i-- {
		if g.declared[i][name] {
			return reassignSyntax(g.target, name, expression)
		}
	}
	g.declared[len(g.declared)-1][name] = true
	switch g.target {
	case "python", "julia":
		return name + " = " + expression
	case "nim":
		return "var " + name + " = " + expression
	case "go":
		return name + " := " + expression
	case "rust":
		return "let mut " + name + " = " + expression + ";"
	case "cpp":
		return "auto " + name + " = " + expression + ";"
	case "c":
		return "double " + name + " = " + expression + ";"
	case "zig":
		return "var " + name + " = " + expression + ";"
	case "csharp", "java":
		return "var " + name + " = " + expression + ";"
	case "kotlin", "swift":
		return "var " + name + " = " + expression
	}
	return name + " = " + expression
}

// Conditions proven as boolean by the UAST can be consumed directly by all
// TargetSpecs.  The normalizer remains responsible for rejecting an unproved
// truth conversion before this emitter is reached.
func (g *targetGen) nativeCondition(expression string) string { return expression }

func (g *targetGen) nativeExpressionStatement(expression string) string {
	switch g.target {
	case "go":
		if strings.HasPrefix(expression, "fmt.Print") {
			return expression + ";"
		}
		return "_ = " + expression
	case "rust", "csharp", "java", "cpp", "c":
		if strings.HasSuffix(expression, ";") {
			return expression
		}
		return expression + ";"
	case "nim":
		return expression
	case "zig":
		if strings.HasPrefix(expression, "std.debug.print") {
			return expression + ";"
		}
		return "_ = " + expression + ";"
	default:
		return expression
	}
}

// nativeLiteral bypasses TargetSpec's compatibility value wrappers.  These
// are target grammar tokens, not an alternate semantic model.
func nativeLiteral(target string, kind string, text string) string {
	switch kind {
	case "string":
		q := strconv.Quote(unquote(text))
		switch target {
		case "rust":
			return q + ".to_string()"
		case "swift":
			return q
		default:
			return q
		}
	case "boolean":
		if text == "TRUE" || text == "T" || strings.EqualFold(text, "true") {
			return "true"
		}
		return "false"
	case "null":
		switch target {
		case "python":
			return "None"
		case "julia":
			return "nothing"
		case "rust":
			return "None"
		case "swift":
			return "nil"
		case "kotlin":
			return "null"
		default:
			return "null"
		}
	}
	number := strings.TrimSuffix(text, "L")
	// Integer literals are a distinct canonical representation. They must not
	// be promoted to binary64 merely because a target normally defaults bare
	// numeric literals to floating point; shifts, bitwise operations and index
	// contracts require the integral representation on every target.
	if kind == "integer" {
		return number
	}
	if !strings.ContainsAny(number, ".eE") && (target == "rust" || target == "kotlin") {
		number += ".0"
	}
	return number
}

// nativeDispatch is the single residual-dispatch boundary.  A program that
// entered the DIRECT projection mode is never allowed to cross from here into
// a target runtime: either a target-native form is available or the projector
// returns an explicit diagnostic.  Compatibility/oracle paths retain the
// existing dispatcher below this boundary.
func (g *targetGen) nativeDispatch(name string, args []string) (string, error) {
	if !g.nativeDirect {
		return emitDispatch(g.target, name, args), nil
	}
	if strings.HasPrefix(name, "__binary_") && len(args) == 2 {
		op := strings.TrimPrefix(name, "__binary_")
		// R's remainder and floor-division spellings are canonical binary
		// operations, not target tokens.  Legalize them through the same
		// parameterized native form table used by every frontend before falling
		// back to the generic infix renderer.  The operands are already UAST
		// expressions and are each evaluated exactly once by the emitted form.
		if rendered, ok := nativeBinaryExpression(g.target, op, args[0], args[1]); ok {
			return rendered, nil
		}
		if op == "^" || op == "**" {
			return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: exponentiation requires a target math contract")
		}
		return "(" + args[0] + " " + op + " " + args[1] + ")", nil
	}
	if name == "length" && len(args) == 1 {
		if rendered, ok := nativeLengthExpression(g.target, args[0]); ok {
			return rendered, nil
		}
	}
	if name == "sum" && len(args) == 1 {
		if expression, helper := nativeSumExpression(g.target, args[0]); expression != "" {
			if helper != "" {
				g.requireHelper("helper.native.sum", helper)
			}
			return expression, nil
		}
	}
	if name == "list" || name == "c" {
		return directNativeCall(g.target, "c", args), nil
	}
	if name == "[" || name == "[[" {
		if len(args) != 2 {
			return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: index arity")
		}
		index := args[1]
		switch g.target {
		case "python", "nim":
			return args[0] + "[int(" + index + ") - 1]", nil
		case "rust":
			return args[0] + "[" + index + " as usize - 1]", nil
		case "cpp":
			return args[0] + "[static_cast<size_t>(" + index + " - 1)]", nil
		case "csharp":
			return args[0] + "[(int)" + index + " - 1]", nil
		case "java":
			return args[0] + "[(int)(" + index + " - 1)]", nil
		case "c":
			return args[0] + "[(size_t)((int)" + index + " - 1)]", nil
		case "kotlin", "swift", "zig":
			return args[0] + "[int(" + index + ") - 1]", nil
		case "go":
			return args[0] + "[int(" + index + ") - 1]", nil
		case "julia":
			return args[0] + "[Int(" + index + ")]", nil
		}
	}
	if direct := directNativeCall(g.target, name, args); direct != "" {
		return direct, nil
	}
	return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: call %q has no native contract for target %q (arguments: %s)", name, g.target, strings.Join(args, ", "))
}

// nativeBinaryExpression is the target-legalization relation for canonical
// binary forms whose source spelling is not portable.  It is deliberately
// keyed only by the semantic operator and target representation; frontends
// never take part in this decision.
func nativeBinaryExpression(target, op, left, right string) (string, bool) {
	switch op {
	case "%%":
		switch target {
		case "go":
			return "math.Mod(" + left + ", " + right + ")", true
		case "rust", "python", "csharp", "java", "kotlin":
			return "(" + left + " % " + right + ")", true
		case "cpp":
			return "std::fmod(" + left + ", " + right + ")", true
		case "c":
			return "fmod(" + left + ", " + right + ")", true
		case "julia":
			return "mod(" + left + ", " + right + ")", true
		case "nim":
			return "(" + left + " mod " + right + ")", true
		case "swift":
			return left + ".truncatingRemainder(dividingBy: " + right + ")", true
		case "zig":
			return "@mod(" + left + ", " + right + ")", true
		}
	case "%/%":
		switch target {
		case "go":
			return "math.Floor(" + left + " / " + right + ")", true
		case "rust":
			return "((" + left + " as f64) / (" + right + " as f64)).floor()", true
		case "python":
			return "(" + left + " // " + right + ")", true
		case "cpp":
			return "std::floor(" + left + " / " + right + ")", true
		case "c":
			return "floor(" + left + " / " + right + ")", true
		case "julia":
			return "floor(" + left + " / " + right + ")", true
		case "nim":
			return "floor(" + left + " / " + right + ")", true
		case "csharp":
			return "Math.Floor(" + left + " / " + right + ")", true
		case "java":
			return "Math.floor(" + left + " / " + right + ")", true
		case "kotlin":
			return "kotlin.math.floor(" + left + " / " + right + ")", true
		case "swift":
			return "(" + left + " / " + right + ").rounded(.down)", true
		case "zig":
			return "std.math.floor(" + left + " / " + right + ")", true
		}
	}
	return "", false
}

// nativeSumExpression is a target-parameterized lowering contract. It emits
// either a target builtin or a small generated source helper; it never routes
// through the semantic runtime dispatcher.
func nativeSumExpression(target, value string) (string, string) {
	switch target {
	case "python":
		return "sum(" + value + ")", ""
	case "julia":
		return "sum(" + value + ")", ""
	case "nim":
		return "sum(" + value + ")", ""
	case "kotlin":
		return value + ".sum()", ""
	case "swift":
		return value + ".reduce(0, +)", ""
	case "rust":
		return "uast_sum_f64(&" + value + ")", "fn uast_sum_f64(v: &Vec<f64>) -> f64 { v.iter().copied().sum() }\n"
	case "go":
		return "uastSumF64(" + value + ")", "func uastSumF64(v []float64) float64 { var s float64; for _, x := range v { s += x }; return s }\n"
	case "cpp":
		return "uast_sum_f64(" + value + ")", "static double uast_sum_f64(const std::vector<double>& v) { double s=0; for (double x : v) s += x; return s; }\n"
	case "c":
		return "uast_sum_f64(" + value + ", sizeof(" + value + ")/sizeof(double))", "static double uast_sum_f64(const double* v, size_t n) { double s=0; for(size_t i=0;i<n;++i) s+=v[i]; return s; }\n"
	case "csharp":
		return "uast_sum_f64(" + value + ")", "static double uast_sum_f64(double[] v) { double s=0; foreach(var x in v) s+=x; return s; }\n"
	case "java":
		return "UastHelpers.uast_sum_f64(" + value + ")", "final class UastHelpers { static double uast_sum_f64(double[] v) { double s=0; for(double x:v) s+=x; return s; } }\n"
	case "zig":
		return "uastSumF64(" + value + "[0..])", "fn uastSumF64(v: []const f64) f64 { var s:f64=0; for(v)|x|{s+=x;} return s; }\n"
	}
	return "", ""
}

// nativeLengthExpression is the shared target-parameterized length contract.
// The spelling is derived from the empirical emitter contracts (py2many and
// the existing target specifications); callers remain on the common UAST call
// path and do not need language-specific lowering.
func nativeLengthExpression(target, value string) (string, bool) {
	switch target {
	case "go":
		return "float64(len(" + value + "))", true
	case "python":
		return "len(" + value + ")", true
	case "julia":
		return "length(" + value + ")", true
	case "nim":
		return "len(" + value + ")", true
	case "kotlin":
		return value + ".size.toDouble()", true
	case "swift":
		return "Double(" + value + ".count)", true
	case "rust":
		return value + ".len() as f64", true
	case "cpp":
		return "static_cast<double>(" + value + ".size())", true
	case "c":
		if strings.Contains(value, "(double*)0") {
			return "0", true
		}
		return "(double)(sizeof(" + value + ") / sizeof(double))", true
	case "csharp":
		return "(double)" + value + ".Length", true
	case "java":
		return "(double)" + value + ".length", true
	case "zig":
		return "@as(f64, @floatFromInt(" + value + ".len))", true
	default:
		return "", false
	}
}

func isFallbackProjectionStructure(kind string) bool {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return false
	}
	for _, contract := range registry.Contracts {
		if contract.StructureKind == kind {
			return contract.ProjectionForm == projectionFormFallback
		}
	}
	return false
}

// directStructuredExpression lists the canonical expression forms whose
// renderer is shared across all frontends.  It is deliberately keyed by UAST
// structure, never by a source language or a parser node name.  Target syntax
// differences remain isolated in the rendering helpers below.
func directStructuredExpression(kind string) bool {
	switch kind {
	case "OperationExpr", "MemberAccessExpr", "SliceExpr", "AddressOf", "Deref":
		return true
	default:
		return false
	}
}

func (g *targetGen) uastTransparentExpression(graph *uastExecutionGraph, id int) (string, bool, error) {
	// A generic OperationExpr is only transparent when the structured graph
	// proves that it wraps exactly one expression and carries no operation.
	// This preserves grouping/adapter nodes without converting unknown source
	// syntax into target text.
	c := graph.common[id]
	if c.Kind != "expression" || c.Operation.Operator != "" || c.Operation.Typed != nil || c.Semantics.Operation != "" {
		return "", false, nil
	}
	children := graph.orderedChildren(id)
	if len(children) != 1 || children[0].Meta.Missing {
		return "", false, nil
	}
	value, err := g.uastExpression(graph, children[0].ID)
	return value, err == nil, err
}

func (g *targetGen) uastMemberExpression(graph *uastExecutionGraph, id int) (string, error) {
	base, found, err := graph.firstChild(id, "base", "receiver", "object", "value")
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: member node %d lacks a structured base", id)
	}
	member := strings.TrimSpace(graph.common[id].Name)
	if member == "" {
		member = strings.TrimSpace(graph.common[id].Operation.Text)
	}
	if member == "" {
		return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: member node %d lacks a structured member name", id)
	}
	value, err := g.uastExpression(graph, base)
	if err != nil {
		return "", err
	}
	return value + "." + g.name(member), nil
}

func (g *targetGen) uastSliceExpression(graph *uastExecutionGraph, id int) (string, error) {
	base, found, err := graph.firstChild(id, "base", "value", "object")
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: slice node %d lacks a structured base", id)
	}
	value, err := g.uastExpression(graph, base)
	if err != nil {
		return "", err
	}
	part := func(roles ...string) (string, bool, error) {
		child, ok, e := graph.firstChild(id, roles...)
		if e != nil || !ok {
			return "", ok, e
		}
		text, e := g.uastExpression(graph, child)
		return text, true, e
	}
	start, hasStart, err := part("start", "low")
	if err != nil {
		return "", err
	}
	end, hasEnd, err := part("end", "high")
	if err != nil {
		return "", err
	}
	_, hasStep, err := part("step")
	if err != nil {
		return "", err
	}
	if hasStep {
		return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: slice step requires a target slice adapter")
	}
	// The UAST slice relation is zero-based once it reaches this target form;
	// target spelling alone differs.  Omitted bounds remain omitted rather than
	// being guessed from the source language.
	switch g.target {
	case "python", "julia", "go", "zig":
		return value + "[" + start + ":" + end + "]", nil
	case "nim":
		if !hasStart || !hasEnd {
			return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: target nim requires explicit slice bounds")
		}
		return value + "[" + start + " ..< " + end + "]", nil
	case "kotlin":
		if !hasStart || !hasEnd {
			return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: target kotlin requires explicit slice bounds")
		}
		return value + ".slice(" + start + " until " + end + ")", nil
	case "swift":
		if !hasStart || !hasEnd {
			return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: target swift requires explicit slice bounds")
		}
		return value + "[" + start + "..<" + end + "]", nil
	case "rust":
		return "&" + value + "[" + start + ".." + end + "]", nil
	default:
		return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: target %s requires a slice representation adapter", g.target)
	}
}

func (g *targetGen) uastPointerExpression(graph *uastExecutionGraph, id int, address bool) (string, error) {
	value, found, err := graph.firstChild(id, "value", "operand", "base")
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: pointer node %d lacks a structured operand", id)
	}
	text, err := g.uastExpression(graph, value)
	if err != nil {
		return "", err
	}
	if g.target != "c" && g.target != "cpp" && g.target != "go" && g.target != "rust" && g.target != "zig" {
		return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: target %s lacks a native pointer form", g.target)
	}
	if address {
		return "(&" + text + ")", nil
	}
	return "(*" + text + ")", nil
}

// uastFallbackExpression is the shared canonical residual boundary. Unknown
// UAST semantics cannot enter a target runtime from DIRECT emission; they are
// reported to the caller as a hard, target-independent diagnostic.
func (g *targetGen) uastFallbackExpression(graph *uastExecutionGraph, id int) (string, error) {
	node := graph.nodes[id]
	if node == nil {
		return "", fmt.Errorf("missing fallback UAST node %d", id)
	}
	// Compatibility mode is the explicit semantic-runtime last resort.  Keep
	// the direct path strict, but preserve an unsupported-yet-structured node
	// by forwarding its already decoded operation and child values through the
	// existing runtime dispatcher.  No source text is reparsed here.
	if !g.nativeDirect {
		c := graph.common[id]
		name := c.Name
		if name == "" {
			name = c.Operation.Operator
		}
		if name == "" {
			name = "uast." + c.Kind
		}
		childIDs := make([]int, 0)
		for role, items := range graph.children[id] {
			// Body/scope edges are statement ownership, not expression
			// operands. Passing them to the runtime dispatcher would recurse
			// through a Scope as an expression and produce a misleading
			// "block has no expression lowering" error.
			if role == "body" || role == "statement" || role == "then" || role == "else" {
				continue
			}
			for _, child := range items {
				childIDs = append(childIDs, child.ID)
			}
		}
		sort.Ints(childIDs)
		args := make([]string, 0, len(childIDs))
		for _, childID := range childIDs {
			value, childErr := g.uastExpression(graph, childID)
			if childErr != nil {
				return "", childErr
			}
			args = append(args, value)
		}
		return emitDispatch(g.target, name, args), nil
	}
	return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: unsupported UAST node %s/%d", node.StructuralKind, node.ID)
}

func (g *targetGen) uastFallbackStatement(graph *uastExecutionGraph, id int) error {
	expression, err := g.uastFallbackExpression(graph, id)
	if err != nil {
		return err
	}
	g.line(exprStmt(g.target, expression))
	return nil
}

// uastStatement and uastExpression are the generic backend's direct UAST
// emitter. They read only graph nodes, crosswalk fields and proved relations.
// The only temporary compatibility object is an isolated function view used by
// the existing flow-safe inline lowering; it is never a full document and is
// never written back to the UAST.
func (g *targetGen) uastStatement(graph *uastExecutionGraph, id int) error {
	if g.hybridFallback && g.nativeDirect {
		return g.uastHybridStatement(graph, id)
	}
	return g.uastStatementCore(graph, id)
}

type targetGenCheckpoint struct {
	body               string
	indent             int
	temp               int
	declared           []map[string]bool
	bindings           []map[string]string
	helperRequirements []string
	helperSources      map[string]string
	usedNames          map[string]bool
	cValues            map[string]bool
	directVectors      map[string]bool
	uastActiveInline   map[int]bool
	runtimeUsed        bool
}

func cloneBoolMaps(in []map[string]bool) []map[string]bool {
	out := make([]map[string]bool, len(in))
	for i, m := range in {
		out[i] = map[string]bool{}
		for k, v := range m {
			out[i][k] = v
		}
	}
	return out
}

func cloneStringMaps(in []map[string]string) []map[string]string {
	out := make([]map[string]string, len(in))
	for i, m := range in {
		out[i] = map[string]string{}
		for k, v := range m {
			out[i][k] = v
		}
	}
	return out
}

func cloneBoolMap(in map[string]bool) map[string]bool {
	out := map[string]bool{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (g *targetGen) checkpoint() targetGenCheckpoint {
	return targetGenCheckpoint{
		body: g.b.String(), indent: g.indent, temp: g.temp,
		declared: cloneBoolMaps(g.declared), bindings: cloneStringMaps(g.bindings),
		helperRequirements: append([]string(nil), g.helperRequirements...),
		helperSources:      cloneStringMapCopy(g.helperSources), usedNames: cloneBoolMap(g.usedNames),
		cValues: cloneBoolMap(g.cValues), directVectors: cloneBoolMap(g.directVectors),
		uastActiveInline: cloneIntBoolMap(g.uastActiveInline), runtimeUsed: g.runtimeUsed,
	}
}

func cloneStringMapCopy(in map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneIntBoolMap(in map[int]bool) map[int]bool {
	out := map[int]bool{}
	for k, v := range in {
		out[k] = v
	}
	return out
}

func (g *targetGen) restore(cp targetGenCheckpoint) {
	g.b.Reset()
	g.b.WriteString(cp.body)
	g.indent, g.temp = cp.indent, cp.temp
	g.declared, g.bindings = cloneBoolMaps(cp.declared), cloneStringMaps(cp.bindings)
	g.helperRequirements = append([]string(nil), cp.helperRequirements...)
	g.helperSources, g.usedNames = cloneStringMapCopy(cp.helperSources), cloneBoolMap(cp.usedNames)
	g.cValues, g.directVectors = cloneBoolMap(cp.cValues), cloneBoolMap(cp.directVectors)
	g.uastActiveInline, g.runtimeUsed = cloneIntBoolMap(cp.uastActiveInline), cp.runtimeUsed
}

func hybridFallbackEligible(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "DIRECT_NATIVE_UNAVAILABLE") ||
		strings.Contains(message, "unsupported UAST") ||
		strings.Contains(message, "has no direct") ||
		strings.Contains(message, "no native")
}

func (g *targetGen) uastHybridStatement(graph *uastExecutionGraph, id int) error {
	cp := g.checkpoint()
	// Disable the wrapper while trying the strict direct implementation. Any
	// nested failure is handled by this same outer statement transaction.
	g.hybridFallback = false
	err := g.uastStatementCore(graph, id)
	g.hybridFallback = true
	if err == nil {
		return nil
	}
	g.restore(cp)
	if !hybridFallbackEligible(err) {
		return err
	}
	// Re-run only this statement through the compatibility renderer. Existing
	// native statements emitted before it remain in the builder untouched.
	g.nativeDirect = false
	g.runtimeUsed = true
	compatErr := g.uastStatementCore(graph, id)
	g.nativeDirect = true
	if compatErr == nil {
		return nil
	}
	g.restore(cp)
	return compatErr
}

// uastStatementCore performs one ordinary lowering attempt.  The hybrid
// wrapper above snapshots the generator around this function so a failed
// native statement cannot leak partial declarations or helper requirements
// into its runtime replacement.
func (g *targetGen) uastStatementCore(graph *uastExecutionGraph, id int) error {
	// A module is a lexical container, not an executable target expression.
	// Frontends use it both for source-file headers and for a module that owns
	// statements.  Preserve the latter by emitting its ordered statements and
	// omit the former when it is empty.  This is one UAST container contract,
	// independent of the source or target language.
	if node := graph.nodes[id]; node != nil && node.StructuralKind == "ModuleDecl" {
		for _, child := range graph.orderedChildren(id) {
			if child.Meta.Missing {
				return fmt.Errorf("module node %d has a missing child", id)
			}
			if err := g.uastStatement(graph, child.ID); err != nil {
				return err
			}
		}
		return nil
	}
	if node := graph.nodes[id]; node != nil && isMetadataOnlyProjectionStructure(node.StructuralKind) {
		// The schema-derived contract proves that this node has no target
		// syntax channel. Its facts were already consumed by validation and
		// requirements, so emitting no token preserves the program syntax
		// without fabricating a target construct.
		return nil
	}
	if node := graph.nodes[id]; node != nil && isFallbackProjectionStructure(node.StructuralKind) && !directStructuredExpression(node.StructuralKind) {
		return g.uastFallbackStatement(graph, id)
	}
	c := graph.common[id]
	one := func(role string, required bool) (int, bool, error) { return graph.one(id, role, required) }
	if strings.HasPrefix(c.Operation.Operator, "unsupported.") {
		// The frontend supplied a structured construct marker but not enough
		// proven children for a concrete direct statement shape.  Keep the
		// native-first contract strict; the caller may select the documented
		// compatibility/runtime fallback for this node.
		return g.uastFallbackStatement(graph, id)
	}
	if genericMutableDeclarationStructures[graph.nodes[id].StructuralKind] {
		if c.Name == "" {
			return fmt.Errorf("variable declaration node %d lacks a binding name", id)
		}
		initializer, found, err := graph.firstChild(id, "initializer", "expression", "value")
		if err != nil {
			return err
		}
		value := targetNull(g.target)
		if found {
			value, err = g.uastExpression(graph, initializer)
			if err != nil {
				return err
			}
		}
		if g.nativeDirect {
			g.line(g.nativeAssignment(g.name(c.Name), value))
		} else {
			g.line(g.assignment(g.name(c.Name), value))
		}
		return nil
	}
	if genericDeclarationGroupStructures[graph.nodes[id].StructuralKind] {
		for _, child := range graph.orderedChildren(id) {
			if child.Meta.Missing {
				return fmt.Errorf("declaration group node %d has a missing child", id)
			}
			if err := g.uastStatement(graph, child.ID); err != nil {
				return err
			}
		}
		return nil
	}
	switch c.Kind {
	case "block":
		if g.target == "python" {
			for _, item := range graph.many(id, "statement") {
				if err := g.uastStatement(graph, item.ID); err != nil {
					return err
				}
			}
			return nil
		}
		g.line("{")
		g.indent++
		for _, item := range graph.many(id, "statement") {
			if err := g.uastStatement(graph, item.ID); err != nil {
				return err
			}
		}
		g.indent--
		g.line("}")
	case "assign":
		expression, ok, err := one("expression", false)
		if err != nil {
			return err
		}
		// Frontend facts may encode the RHS using the canonical `value`
		// relation (the same relation used by call arguments). Accept both
		// spellings at this shared boundary; no source-language reparsing is
		// involved.
		if !ok {
			expression, _, err = one("value", true)
			if err != nil {
				return err
			}
		}
		n := g.name(c.Name)
		if graph.common[expression].Kind == "function" {
			return g.uastFunctionAssign(graph, n, expression)
		}
		if g.nativeDirect && graph.common[expression].Kind == "call" {
			callee, _, _ := graph.oneRelationNode(expression, "call.calls", false)
			if callee >= 0 && graph.common[callee].Kind == "identifier" && graph.common[callee].Name == "c" {
				g.directVectors[n] = true
			}
		}
		value, err := g.uastExpression(graph, expression)
		if err != nil {
			return err
		}
		// A structured frontend may prove an assignment operation while the
		// target binding pattern is not yet representable as one canonical name
		// (attribute/destructuring targets are examples). The compatibility
		// runtime can still evaluate the RHS, but emitting an empty assignment
		// target produces invalid code in every target. Preserve evaluation as a
		// discard expression until a binding adapter is available.
		if strings.TrimSpace(c.Name) == "" {
			if g.nativeDirect {
				g.line(g.nativeExpressionStatement(value))
			} else {
				g.line(exprStmt(g.target, value))
			}
			return nil
		}
		if g.nativeDirect && g.directVectors[n] {
			g.declared[len(g.declared)-1][n] = true
			switch g.target {
			case "go":
				g.line("var " + n + " []float64 = " + value)
			case "rust":
				g.line("let mut " + n + " = " + value + ";")
			case "cpp":
				g.line("std::vector<double> " + n + " = " + value + ";")
			case "java":
				g.line("var " + n + " = " + value + ";")
			case "c":
				// C array declarations require an initializer list rather than
				// assigning a compound-literal array expression.
				initializer := strings.TrimPrefix(value, "(double[]){")
				if initializer != value && strings.HasSuffix(initializer, "}") {
					initializer = "{" + strings.TrimSuffix(initializer, "}") + "}"
				}
				g.line("double " + n + "[] = " + initializer + ";")
			default:
				g.line(g.assignment(n, value))
			}
		} else if g.nativeDirect {
			g.line(g.nativeAssignment(n, value))
		} else {
			g.line(g.assignment(n, value))
		}
	case "expression":
		expression, found, err := graph.firstChild(id, "expression", "value", "operand", "base", "receiver", "object")
		if err != nil {
			return err
		}
		if !found && len(graph.orderedChildren(id)) == 1 {
			expression, found = graph.orderedChildren(id)[0].ID, true
		}
		if !found {
			return fmt.Errorf("universal node %d lacks a structured expression child", id)
		}
		value, err := g.uastExpression(graph, expression)
		if err != nil {
			return err
		}
		if g.nativeDirect {
			g.line(g.nativeExpressionStatement(value))
		} else {
			g.line(exprStmt(g.target, value))
		}
	case "if":
		condition, _, err := one("condition", true)
		if err != nil {
			return err
		}
		then, _, err := graph.oneRelationNode(id, "control.true", true)
		if err != nil {
			return err
		}
		other, hasElse, err := graph.oneRelationNode(id, "control.false", false)
		if err != nil {
			return err
		}
		conditionText, err := g.uastExpression(graph, condition)
		if err != nil {
			return err
		}
		if g.nativeDirect {
			conditionText = g.nativeCondition(conditionText)
		}
		switch g.target {
		case "python":
			g.line("if " + conditionText + ":")
			g.indent++
			err = g.uastStatementBody(graph, then)
			g.indent--
			if hasElse {
				g.line("else:")
				g.indent++
				err = g.uastStatementBody(graph, other)
				g.indent--
			}
			return err
		case "julia":
			g.line("if " + conditionText)
			g.indent++
			if err = g.uastStatementBody(graph, then); err != nil {
				return err
			}
			g.indent--
			if hasElse {
				g.line("else")
				g.indent++
				if err = g.uastStatementBody(graph, other); err != nil {
					return err
				}
				g.indent--
			}
			g.line("end")
		case "nim":
			g.line("if " + conditionText + ":")
			g.indent++
			if err = g.uastStatementBody(graph, then); err != nil {
				return err
			}
			g.indent--
			if hasElse {
				g.line("else:")
				g.indent++
				if err = g.uastStatementBody(graph, other); err != nil {
					return err
				}
				g.indent--
			}
		default:
			g.line("if (" + g.nativeCondition(conditionText) + ") {")
			g.indent++
			if err = g.uastStatementBody(graph, then); err != nil {
				return err
			}
			g.indent--
			if hasElse {
				if g.target == "go" {
					g.line("} else {")
				} else {
					g.line("}")
					g.line("else {")
				}
				g.indent++
				if err = g.uastStatementBody(graph, other); err != nil {
					return err
				}
				g.indent--
			}
			g.line("}")
		}
	case "while":
		condition, _, err := one("condition", true)
		if err != nil {
			return err
		}
		body, _, err := one("body", true)
		if err != nil {
			return err
		}
		conditionText, err := g.uastExpression(graph, condition)
		if err != nil {
			return err
		}
		if g.target == "python" {
			g.line("while " + g.nativeCondition(conditionText) + ":")
			g.indent++
			err = g.uastStatementBody(graph, body)
			g.indent--
			return err
		}
		if g.target == "julia" {
			g.line("while " + g.nativeCondition(conditionText))
			g.indent++
			err = g.uastStatementBody(graph, body)
			g.indent--
			g.line("end")
			return err
		}
		if g.target == "nim" {
			g.line("while " + g.nativeCondition(conditionText) + ":")
			g.indent++
			err = g.uastStatementBody(graph, body)
			g.indent--
			return err
		}
		if g.target == "go" {
			g.line("for " + g.nativeCondition(conditionText) + " {")
		} else {
			g.line("while (" + g.nativeCondition(conditionText) + ") {")
		}
		g.indent++
		err = g.uastStatementBody(graph, body)
		g.indent--
		g.line("}")
		return err
	case "for":
		sequence, _, err := one("sequence", true)
		if err != nil {
			return err
		}
		body, _, err := one("body", true)
		if err != nil {
			return err
		}
		sequenceText, err := g.uastExpression(graph, sequence)
		if err != nil {
			return err
		}
		n := g.name(c.Name)
		indexBinding := ""
		if raw, ok := c.Attributes["iteration.index_binding"]; ok {
			if value, ok := raw.(string); ok {
				indexBinding = g.name(value)
			}
		}
		switch g.target {
		case "python":
			g.line("for " + n + " in " + sequenceText + ":")
		case "julia":
			g.line("for " + n + " in " + sequenceText)
		case "nim":
			g.line("for " + n + " in " + sequenceText + ":")
		case "go":
			if indexBinding != "" {
				g.line("for " + indexBinding + ", " + n + " := range " + sequenceText + " {")
			} else {
				g.line("for _, " + n + " := range " + sequenceText + " {")
			}
		case "rust":
			g.line("for " + n + " in " + sequenceText + " {")
		case "cpp":
			if indexBinding != "" {
				g.line("for (size_t " + indexBinding + " = 0; " + indexBinding + " < " + sequenceText + ".size(); ++" + indexBinding + ") {")
				g.indent++
				g.line("const auto& " + n + " = " + sequenceText + "[" + indexBinding + "];")
				if err := g.uastStatementBody(graph, body); err != nil {
					g.indent--
					return err
				}
				g.indent--
				g.line("}")
				return nil
			}
			g.line("for (const auto& " + n + " : " + sequenceText + ") {")
		case "csharp":
			g.line("foreach (var " + n + " in " + sequenceText + ") {")
		case "java":
			g.line("for (double " + n + " : " + sequenceText + ") {")
		case "c":
			g.temp++
			sequenceName, index := fmt.Sprintf("__sequence_%d", g.temp), fmt.Sprintf("__index_%d", g.temp)
			if g.nativeDirect {
				g.line("double *" + sequenceName + " = " + sequenceText + ";")
				g.line("size_t " + sequenceName + "_len = sizeof(" + sequenceText + ") / sizeof(double);")
				g.line("for (size_t " + index + " = 0; " + index + " < " + sequenceName + "_len; ++" + index + ") {")
				g.indent++
				g.line("double " + n + " = " + sequenceName + "[" + index + "];")
			} else {
				g.line("RValue " + sequenceName + " = " + sequenceText + ";")
				g.line("for (size_t " + index + " = 0; " + index + " < " + sequenceName + ".len; ++" + index + ") {")
				g.indent++
				g.line("RValue " + n + " = " + sequenceName + ".v[" + index + "];")
			}
			err = g.uastStatementBody(graph, body)
			g.indent--
			g.line("}")
			return err
		case "kotlin":
			g.line("for (" + n + " in " + sequenceText + ") {")
		case "swift":
			g.line("for " + n + " in " + sequenceText + " {")
		case "zig":
			g.line("for (" + sequenceText + ") |" + n + "| {")
		default:
			return fmt.Errorf("target %s has no iterable-loop lowering; refusing to omit its body", g.target)
		}
		g.indent++
		err = g.uastStatementBody(graph, body)
		g.indent--
		if g.target == "julia" {
			g.line("end")
		} else if g.target != "python" && g.target != "nim" {
			g.line("}")
		}
		return err
	case "repeat":
		body, _, err := one("body", true)
		if err != nil {
			return err
		}
		switch g.target {
		case "python":
			g.line("while True:")
		case "julia":
			g.line("while true")
		case "nim":
			g.line("while true:")
		default:
			g.line("for (;;)")
			return g.uastStatement(graph, body)
		}
		g.indent++
		err = g.uastStatementBody(graph, body)
		g.indent--
		if g.target == "julia" {
			g.line("end")
		}
		return err
	case "return":
		if expression, ok, err := one("expression", false); err != nil {
			return err
		} else if !ok {
			g.line(returnNull(g.target))
		} else {
			value, err := g.uastExpression(graph, expression)
			if err != nil {
				return err
			}
			g.line(returnExpr(g.target, value))
		}
	case "break":
		g.line("break" + stmtEnd(g.target))
	case "continue":
		if g.target == "python" || g.target == "julia" || g.target == "nim" {
			g.line("continue")
		} else {
			g.line("continue;")
		}
	case "identifier", "literal", "function", "tuple", "binding", "parameter", "module":
		// Shared UAST graphs may expose a declaration/assignment target as a
		// statement child as well as through its target relation. It carries no
		// standalone executable statement, so consume it without emitting code.
		return nil
	case "aggregate":
		// Aggregate nodes can be shared as assignment values and appear in the
		// enclosing statement relation. They have no standalone statement form.
		return nil
	case "call", "binary", "unary", "index", "slice", "typed_operation", "iteration", "member", "deref", "address":
		if expressionOwnedByStructuredParent(graph, id) {
			return nil
		}
		expr, err := g.uastExpression(graph, id)
		if err != nil {
			return err
		}
		g.line(expr + stmtEnd(g.target))
		return nil
	default:
		return fmt.Errorf("universal node %d kind %q has no direct statement lowering", id, c.Kind)
	}
	return nil
}

func (g *targetGen) uastStatementBody(graph *uastExecutionGraph, id int) error {
	if graph.common[id].Kind == "block" {
		for _, item := range graph.many(id, "statement") {
			if err := g.uastStatement(graph, item.ID); err != nil {
				return err
			}
		}
		return nil
	}
	return g.uastStatement(graph, id)
}

func (g *targetGen) uastExpression(graph *uastExecutionGraph, id int) (string, error) {
	c := graph.common[id]
	one := func(role string) (int, error) { value, _, err := graph.one(id, role, true); return value, err }
	if strings.HasPrefix(c.Operation.Operator, "unsupported.") {
		return g.uastFallbackExpression(graph, id)
	}
	if node := graph.nodes[id]; node != nil && isFallbackProjectionStructure(node.StructuralKind) && !directStructuredExpression(node.StructuralKind) {
		return g.uastFallbackExpression(graph, id)
	}
	if node := graph.nodes[id]; node != nil {
		switch node.StructuralKind {
		case "MemberAccessExpr":
			return g.uastMemberExpression(graph, id)
		case "SliceExpr":
			return g.uastSliceExpression(graph, id)
		case "AddressOf":
			return g.uastPointerExpression(graph, id, true)
		case "Deref":
			return g.uastPointerExpression(graph, id, false)
		}
	}
	// AggregateExpr has one shared runtime-preserving lowering: its ordered
	// syntax children form a target runtime list.  It deliberately uses only
	// the UAST child relation and preserves the same value contract as the
	// direct UAST executor; no source-language aggregate syntax is assumed.
	if genericProjectionStructures[graph.nodes[id].StructuralKind] {
		return g.uastAggregateExpression(graph, id)
	}
	switch c.Kind {
	case "expression":
		// OperationExpr is a canonical envelope, not an opaque target syntax
		// form. When its operation and ordered operands are structurally proved,
		// consume that semantic contract through the same binary/unary rendering
		// kernels as their specialized UAST counterparts.
		if value, ok, err := g.uastCanonicalOperationExpression(graph, id); err != nil || ok {
			return value, err
		}
		if value, ok, err := g.uastTransparentExpression(graph, id); err != nil {
			return "", err
		} else if ok {
			return value, nil
		}
		return "", fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: generic operation node %d has no proved renderer contract", id)
	case "identifier":
		for i := len(g.bindings) - 1; i >= 0; i-- {
			if value, ok := g.bindings[i][g.name(c.Name)]; ok {
				return value, nil
			}
		}
		if strings.HasPrefix(c.Name, "\x00") {
			return "", fmt.Errorf("unbound internal state slot %q", c.Name)
		}
		switch c.Name {
		case "TRUE", "T":
			if g.nativeDirect {
				return nativeLiteral(g.target, "boolean", "true"), nil
			}
			return targetBool(g.target, true), nil
		case "FALSE", "F":
			if g.nativeDirect {
				return nativeLiteral(g.target, "boolean", "false"), nil
			}
			return targetBool(g.target, false), nil
		case "NULL":
			return targetNull(g.target), nil
		case "NA", "NA_real_", "NA_integer_", "NA_character_", "NA_complex_", "NaN":
			return targetNA(g.target), nil
		case "Inf":
			return targetInf(g.target), nil
		case "pi":
			return targetNumber(g.target, "3.14159265358979323846"), nil
		}
		name := g.name(c.Name)
		g.cValues[name] = true
		if g.target == "rust" {
			return name + ".clone()", nil
		}
		return name, nil
	case "literal":
		if g.nativeDirect {
			return nativeLiteral(g.target, c.Operation.LiteralKind, c.Operation.Text), nil
		}
		switch c.Operation.LiteralKind {
		case "string":
			return targetString(g.target, unquote(c.Operation.Text)), nil
		case "null":
			return targetNull(g.target), nil
		case "na", "nan":
			return targetNA(g.target), nil
		case "boolean":
			return targetBool(g.target, c.Operation.Text == "TRUE" || c.Operation.Text == "T"), nil
		}
		return targetNumber(g.target, strings.TrimSuffix(c.Operation.Text, "L")), nil
	case "unary":
		value, ok, err := graph.one(id, "value", false)
		if err != nil {
			return "", err
		}
		if !ok {
			value, err = one("operand")
		}
		if err != nil {
			return "", err
		}
		text, err := g.uastExpression(graph, value)
		if err != nil {
			return "", err
		}
		if g.nativeDirect {
			switch c.Operation.Operator {
			case "+":
				return text, nil
			case "-":
				return "(-" + text + ")", nil
			case "!":
				if g.target == "python" || g.target == "nim" || g.target == "zig" {
					return "(not " + text + ")", nil
				}
				return "(!" + text + ")", nil
			}
		}
		return g.lowerUnary(c.Operation.Operator, text)
	case "binary":
		operands, err := graph.relationNodes(id, "data.operand")
		if err != nil || len(operands) != 2 {
			if err != nil {
				return "", err
			}
			return "", fmt.Errorf("binary node %d lacks two proved data.operand relations", id)
		}
		left, err := one("left")
		if err != nil {
			return "", err
		}
		right, err := one("right")
		if err != nil {
			return "", err
		}
		if operands[0] != left || operands[1] != right {
			return "", fmt.Errorf("binary node %d operand relation disagrees with syntax fields", id)
		}
		a, err := g.uastExpression(graph, left)
		if err != nil {
			return "", err
		}
		b, err := g.uastExpression(graph, right)
		if err != nil {
			return "", err
		}
		if g.nativeDirect && (c.Operation.Operator == "&&" || c.Operation.Operator == "||") {
			op := c.Operation.Operator
			if g.target == "python" || g.target == "nim" || g.target == "zig" {
				if op == "&&" {
					op = "and"
				} else {
					op = "or"
				}
			}
			return "(" + a + " " + op + " " + b + ")", nil
		}
		if g.nativeDirect {
			if g.target == "cpp" || g.target == "c" || g.target == "go" || g.target == "rust" {
				switch c.Operation.Operator {
				case "+", "-", "*", "/", "%", "==", "!=", "<", "<=", ">", ">=":
					return "(" + a + " " + c.Operation.Operator + " " + b + ")", nil
				}
			}
		}
		if c.Operation.Operator == "&&" || c.Operation.Operator == "||" {
			return g.lowerLogical(c.Operation.Operator, a, b), nil
		}
		if g.nativeDirect && g.directVectors[g.name(graph.common[left].Name)] {
			if direct, ok := directVectorBinary(g.target, a, b, c.Operation.Operator); ok {
				return direct, nil
			}
		}
		if !g.uastEffectFree(graph, left) || !g.uastEffectFree(graph, right) {
			leftName, rightName := g.freshName("left"), g.freshName("right")
			g.cValues[leftName], g.cValues[rightName] = true, true
			value, err := g.nativeDispatch("__binary_"+c.Operation.Operator, []string{leftName, rightName})
			if err != nil {
				return "", err
			}
			return g.letExpression([]valueBinding{{leftName, a}, {rightName, b}}, value), nil
		}
		return g.nativeDispatch("__binary_"+c.Operation.Operator, []string{a, b})
	case "typed_operation":
		return g.uastTypedOperation(graph, id)
	case "index":
		value, err := one("value")
		if err != nil {
			return "", err
		}
		container, err := g.uastExpression(graph, value)
		if err != nil {
			return "", err
		}
		args, err := g.uastArguments(graph, id)
		if err != nil {
			return "", err
		}
		name := "["
		if c.Operation.DoubleIndex {
			name = "[["
		}
		if g.nativeDirect && (g.target == "cpp" || g.target == "c") && len(args) == 1 && name == "[" {
			return container + "[" + args[0] + "]", nil
		}
		return g.nativeDispatch(name, append([]string{container}, args...))
	case "call":
		callee, _, err := graph.oneRelationNode(id, "call.calls", true)
		if err != nil {
			return "", err
		}
		args, err := g.uastArguments(graph, id)
		if err != nil {
			return "", err
		}
		// Function values, including immediately invoked closures, are already
		// structured UAST nodes. Inline them through the shared function engine
		// instead of routing the value through a runtime dispatcher.
		if graph.common[callee].Kind == "function" {
			return g.uastInlineCall(graph, callee, id, args)
		}
		if graph.common[callee].Kind == "identifier" {
			name := g.name(graph.common[callee].Name)
			if g.nativeDirect {
				if direct := directNativeCall(g.target, graph.common[callee].Name, args); direct != "" {
					return direct, nil
				}
			}
			if fn, ok := g.uastFunctions[name]; ok {
				if g.uastInline[name] {
					if c.Operation.Operator == "eager_left_to_right" {
						previous := g.evaluation
						g.evaluation = "eager_left_to_right"
						result, err := g.uastInlineCall(graph, fn, id, args)
						g.evaluation = previous
						return result, err
					}
					return g.uastInlineCall(graph, fn, id, args)
				}
				if g.evaluation == "lazy_demand" && g.uastEffectfulCall(graph, id) {
					return "", fmt.Errorf("lazy effectful argument requires a promise in this function body")
				}
				return callUser(g.target, name, args), nil
			}
			return g.nativeDispatch(graph.common[callee].Name, args)
		}
		value, err := g.uastExpression(graph, callee)
		if err != nil {
			return "", err
		}
		return g.nativeDispatch("__call_value", append([]string{value}, args...))
	case "iteration":
		value, err := one("value")
		if err != nil {
			return "", err
		}
		text, err := g.uastExpression(graph, value)
		if err != nil {
			return "", err
		}
		switch c.Operation.Operator {
		case "snapshot":
			return g.snapshotIteration(text)
		case "size":
			if g.nativeDirect && (g.target == "cpp" || g.target == "c") {
				return text + ".size()", nil
			}
			return g.nativeDispatch("length", []string{text})
		default:
			return "", fmt.Errorf("unknown iteration intrinsic %q", c.Operation.Operator)
		}
	case "missing_argument":
		return "", nil
	case "function":
		return "", fmt.Errorf("anonymous function must be assigned to a name")
	default:
		return "", fmt.Errorf("universal node %d kind %q has no direct expression lowering", id, c.Kind)
	}
}

// uastCanonicalOperationExpression lowers a structurally identified
// OperationExpr. It deliberately accepts only the finite canonical operation
// vocabulary and data.operand order already present in the UAST graph; it
// neither reads source text nor guesses an operand from a target renderer.
func (g *targetGen) uastCanonicalOperationExpression(graph *uastExecutionGraph, id int) (string, bool, error) {
	c := graph.common[id]
	op := canonicalOperationOperator(c.Operation.Operator, c.Semantics.Operation)
	if op == "" {
		return "", false, nil
	}
	operands, err := graph.relationNodes(id, "data.operand")
	if err != nil {
		return "", true, err
	}
	switch len(operands) {
	case 1:
		if op != "+" && op != "-" && op != "!" {
			return "", false, nil
		}
		value, err := g.uastExpression(graph, operands[0])
		if err != nil {
			return "", true, err
		}
		if op == "+" {
			return value, true, nil
		}
		if op == "!" && (g.target == "python" || g.target == "nim" || g.target == "zig") {
			return "(not " + value + ")", true, nil
		}
		return "(" + op + value + ")", true, nil
	case 2:
		left, err := g.uastExpression(graph, operands[0])
		if err != nil {
			return "", true, err
		}
		right, err := g.uastExpression(graph, operands[1])
		if err != nil {
			return "", true, err
		}
		if op == "&&" || op == "||" {
			rendered := op
			if g.target == "python" || g.target == "nim" || g.target == "zig" {
				if op == "&&" {
					rendered = "and"
				} else {
					rendered = "or"
				}
			}
			return "(" + left + " " + rendered + " " + right + ")", true, nil
		}
		if !g.uastEffectFree(graph, operands[0]) || !g.uastEffectFree(graph, operands[1]) {
			ln, rn := g.freshName("left"), g.freshName("right")
			g.cValues[ln], g.cValues[rn] = true, true
			value, err := g.nativeDispatch("__binary_"+op, []string{ln, rn})
			if err != nil {
				return "", true, err
			}
			return g.letExpression([]valueBinding{{ln, left}, {rn, right}}, value), true, nil
		}
		value, err := g.nativeDispatch("__binary_"+op, []string{left, right})
		return value, true, err
	}
	return "", false, nil
}

func canonicalOperationOperator(operator, semantic string) string {
	if operator != "" {
		return operator
	}
	return map[string]string{
		"add": "+", "subtract": "-", "multiply": "*", "divide": "/",
		"remainder": "%%", "floor_divide": "%/%", "equal": "==",
		"not_equal": "!=", "less_than": "<", "less_or_equal": "<=",
		"greater_than": ">", "greater_or_equal": ">=", "logical_and": "&&",
		"logical_or": "||", "logical_not": "!", "negate": "-", "identity": "+",
	}[strings.ToLower(strings.TrimSpace(semantic))]
}

func directNativeCall(target, name string, args []string) string {
	// Frontends preserve source spelling in the UAST symbol field.  Native
	// contracts are keyed by the common call operation, so qualified standard
	// library spellings are normalized once at this shared boundary.
	if dot := strings.LastIndex(name, "."); dot >= 0 {
		name = name[dot+1:]
	}
	name = strings.ToLower(name)
	if name == "println" || name == "printf" {
		name = "print"
	}
	joined := strings.Join(args, ", ")
	// Index writes are a data-operation contract, not a frontend-specific
	// helper. Their index has already been normalized to the canonical one-based
	// UAST contract by the source adapter; each native target consumes its own
	// indexing representation here.
	if name == "__index_set" && len(args) == 3 {
		index := "int(" + args[1] + ") - 1"
		switch target {
		case "rust":
			index = "(" + args[1] + " as usize) - 1"
		case "kotlin":
			index = "(" + args[1] + ").toInt() - 1"
		case "swift":
			index = "Int(" + args[1] + ") - 1"
		}
		slot := args[0] + "[" + index + "]"
		switch target {
		case "go", "cpp", "csharp", "java", "kotlin", "swift", "zig", "nim", "python", "rust":
			return "(" + slot + " = " + args[2] + ")"
		case "julia":
			return "(" + args[0] + "[Int(" + args[1] + ")] = " + args[2] + ")"
		}
	}
	if name == "__make_float64" && len(args) == 1 {
		switch target {
		case "go":
			return "make([]float64, int(" + args[0] + "))"
		case "cpp":
			return "std::vector<double>(" + args[0] + ")"
		case "rust":
			return "vec![0.0; " + args[0] + "]"
		case "python":
			return "[0.0] * int(" + args[0] + ")"
		case "julia":
			return "zeros(Float64, " + args[0] + ")"
		case "nim":
			return "newSeq[float64](" + args[0] + ")"
		case "swift":
			return "Array(repeating: 0.0, count: " + args[0] + ")"
		case "kotlin":
			return "MutableList(" + args[0] + ") { 0.0 }"
		case "csharp":
			return "new double[" + args[0] + "]"
		case "java":
			return "new double[" + args[0] + "]"
		}
	}
	if name == "length" && len(args) == 1 {
		if rendered, ok := nativeLengthExpression(target, args[0]); ok {
			return rendered
		}
	}
	// A native call is valid only when its already-rendered arguments contain
	// no runtime value or dispatcher.  This keeps the decision data-driven at
	// the common call boundary: proven native expressions use target syntax,
	// mixed expressions continue through the explicit compatibility fallback.
	if strings.Contains(joined, "rCall(") || strings.Contains(joined, "r_call(") || strings.Contains(joined, "RValue") || strings.Contains(joined, "r_call") {
		return ""
	}
	switch target {
	case "go":
		if name == "c" {
			// Aggregate element type is a representation contract, not a
			// scalar-only assumption.  Keep the compact numeric form for the
			// proven numeric witness path, but use []any when structured UAST
			// elements include strings/booleans.  This preserves the generic
			// call/aggregate projection for real modules such as Python __all__.
			elementType := "float64"
			for _, arg := range args {
				trimmed := strings.TrimSpace(arg)
				if strings.HasPrefix(trimmed, "\"") || strings.HasPrefix(trimmed, "'") ||
					trimmed == "true" || trimmed == "false" || trimmed == "nil" {
					elementType = "any"
					break
				}
			}
			return "[]" + elementType + "{" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "fmt.Println(" + joined + ")"
		}
	case "python":
		if name == "c" {
			return "[" + joined + "]"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "print(" + joined + ")"
		}
	case "julia":
		if name == "c" {
			return "[" + joined + "]"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "println(" + joined + ")"
		}
	case "nim":
		if name == "c" {
			return "@[" + joined + "]"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "echo " + joined
		}
	case "swift":
		if name == "c" {
			return "[" + joined + "]"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "print(" + joined + ")"
		}
	case "rust":
		if name == "c" {
			return "vec![" + joined + "]"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "println!(\"{:?}\", " + joined + ");"
		}
	case "cpp":
		if name == "c" {
			return "std::vector<double>{" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			// Print is a semantic operation. Its arguments may be aggregates
			// produced by any structured expression, so do not assume operator<<.
			return "uast_print(" + joined + ")"
		}
	case "c":
		if name == "c" {
			if len(args) == 0 {
				return "((double*)0)"
			}
			return "(double[]){" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "printf(\"%g\\n\", (double)(" + joined + "))"
		}
	case "zig":
		if name == "c" {
			return "[_]f64{" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "std.debug.print(\"{any}\\n\", .{" + joined + "})"
		}
	case "csharp":
		if name == "c" {
			return "new double[]{" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "Console.WriteLine(" + joined + ")"
		}
	case "java":
		if name == "c" {
			return "new double[]{" + joined + "}"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "System.out.println(" + joined + ")"
		}
	case "kotlin":
		if name == "c" {
			return "listOf(" + joined + ")"
		}
		if name == "print" || name == "show" || name == "cat" {
			return "println(" + joined + ")"
		}
	}
	return ""
}

func (g *targetGen) uastAggregateExpression(graph *uastExecutionGraph, id int) (string, error) {
	items := graph.orderedChildren(id)
	values := make([]string, 0, len(items))
	for _, item := range items {
		if item.Meta.Missing {
			values = append(values, targetNull(g.target))
			continue
		}
		value, err := g.uastExpression(graph, item.ID)
		if err != nil {
			return "", err
		}
		values = append(values, value)
	}
	return g.nativeDispatch("list", values)
}

func (g *targetGen) uastArguments(graph *uastExecutionGraph, id int) ([]string, error) {
	items := graph.many(id, "argument")
	out := make([]string, 0, len(items))
	for _, item := range items {
		if item.Meta.Missing {
			out = append(out, targetNull(g.target))
			continue
		}
		value, err := g.uastExpression(graph, item.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, nil
}

func (g *targetGen) uastTypedOperation(graph *uastExecutionGraph, id int) (string, error) {
	c := graph.common[id]
	operation := c.Operation.Typed
	args := graph.many(id, "argument")
	if operation == nil {
		return "", fmt.Errorf("typed operation node %d lacks operation", id)
	}
	if err := operation.validate(len(args)); err != nil {
		return "", err
	}
	if err := TypedImplementationMatrix().Check([]string{operation.Name}, "target."+g.target); err != nil {
		return "", err
	}
	bindings, values := []valueBinding{}, []string{}
	for _, arg := range args {
		if arg.Meta.Missing || arg.Meta.Name != "" {
			return "", fmt.Errorf("typed operation node %d has non-positional or missing operand", id)
		}
		value, err := g.uastExpression(graph, arg.ID)
		if err != nil {
			return "", err
		}
		name := g.freshName("integer")
		g.cValues[name] = true
		bindings, values = append(bindings, valueBinding{name, value}), append(values, name)
	}
	signed := "false"
	if *operation.Type.Signed {
		signed = "true"
	}
	spec, ok := targetSpec(g.target)
	if !ok || spec.TypedOperations.Form == "unsupported" {
		return "", fmt.Errorf("no typed operation specification for target %q", g.target)
	}
	adapter := spec.TypedOperations
	var result string
	switch adapter.Form {
	case "c":
		flag := 0
		if *operation.Type.Signed {
			flag = 1
		}
		payload := "NULL"
		if len(values) > 0 {
			payload = "(RValue[]){" + strings.Join(values, ", ") + "}"
		}
		result = fmt.Sprintf("%s(%q, %d, %d, %q, %s, %d)", adapter.Runtime, operation.Name, operation.Type.Bits, flag, operation.Text, payload, len(values))
	default:
		if *operation.Type.Signed {
			signed = adapter.SignedTrue
		} else {
			signed = adapter.SignedFalse
		}
		result = fmt.Sprintf("%s(%q, %d, %s, %q, %s%s%s)", adapter.Runtime, operation.Name, operation.Type.Bits, signed, operation.Text, adapter.ArgumentsOpen, strings.Join(values, ", "), adapter.ArgumentsClose)
	}
	return g.letExpression(bindings, result), nil
}

func (g *targetGen) uastEffectFree(graph *uastExecutionGraph, id int) bool {
	c := graph.common[id]
	if genericProjectionStructures[graph.nodes[id].StructuralKind] {
		for _, child := range graph.orderedChildren(id) {
			if !child.Meta.Missing && !g.uastEffectFree(graph, child.ID) {
				return false
			}
		}
		return true
	}
	switch c.Kind {
	case "literal", "identifier":
		return true
	case "typed_operation":
		return c.Operation.Typed != nil && c.Operation.Typed.Name == "integer.literal"
	case "unary", "iteration":
		value, ok, _ := graph.one(id, "value", false)
		return ok && g.uastEffectFree(graph, value)
	case "binary":
		left, lok, _ := graph.one(id, "left", false)
		right, rok, _ := graph.one(id, "right", false)
		return lok && rok && g.uastEffectFree(graph, left) && g.uastEffectFree(graph, right)
	case "index":
		value, ok, _ := graph.one(id, "value", false)
		if !ok || !g.uastEffectFree(graph, value) {
			return false
		}
		for _, item := range graph.many(id, "argument") {
			if !item.Meta.Missing && !g.uastEffectFree(graph, item.ID) {
				return false
			}
		}
		return true
	case "call":
		callee, ok, _ := graph.oneRelationNode(id, "call.calls", false)
		if !ok || graph.common[callee].Kind != "identifier" {
			return false
		}
		for _, item := range graph.many(id, "argument") {
			if !item.Meta.Missing && !g.uastEffectFree(graph, item.ID) {
				return false
			}
		}
		switch graph.common[callee].Name {
		case "c", "list", "length":
			return true
		}
	}
	return false
}
