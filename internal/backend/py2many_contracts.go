package backend

// EmpiricalLoweringContract describes a target-independent UAST operation
// observed in py2many's emitters. It is deliberately a contract table, not a
// second IR or a parser.
type EmpiricalLoweringContract struct {
	Target       string
	ASTNode      string
	Primitive    string
	UASTContract string
}

var empiricalLoweringPrimitives = map[string]string{
	"List": "AGGREGATE", "Tuple": "AGGREGATE", "Set": "AGGREGATE", "Dict": "AGGREGATE",
	"Subscript": "INDEX_READ", "Slice": "INDEX_SLICE", "For": "ITERATION", "ListComp": "ITERATION",
	"Lambda": "FUNCTION_VALUE", "FunctionDef": "FUNCTION_DECL", "Call": "CALL", "Return": "RETURN",
	"If": "CONDITIONAL", "While": "LOOP", "BinOp": "BINARY_OPERATOR", "UnaryOp": "UNARY_OPERATOR",
	"Compare": "COMPARISON", "Assign": "ASSIGNMENT", "AnnAssign": "ASSIGNMENT", "Constant": "LITERAL",
	"Name": "SYMBOL", "Attribute": "SELECTOR",
}

var empiricalPrimitiveExecution = map[string]UASTExecutionPrimitive{
	"AGGREGATE": execData, "INDEX_READ": execData, "INDEX_SLICE": execData,
	"ITERATION": execControl, "LOOP": execControl, "CONDITIONAL": execControl,
	"FUNCTION_VALUE": execCapture, "FUNCTION_DECL": execDeclaration,
	"CALL": execCall, "RETURN": execControl, "BINARY_OPERATOR": execExpression,
	"COMPARISON": execExpression, "UNARY_OPERATOR": execExpression,
	"ASSIGNMENT": execBinding, "LITERAL": execExpression, "SYMBOL": execBinding,
	"SELECTOR":              execData,
	"BINARY_UNARY_OPERATOR": execExpression, "CONTROL_TRANSFER": execControl,
	"TYPE": execTypes, "CONCURRENCY": execConcurrency, "ANNOTATION": execAnnotation,
	"CLOSURE_FUNCTION": execCapture, "CONVERSION": execConversion,
	"SWITCH_MATCH": execControl, "EXCEPTION": execException,
	"DEALLOCATION": execLifetime, "MEMBER_ACCESS": execData,
	"ALLOCATION": execMemory, "BINDING_DECLARATION": execDeclaration,
	"BINDING_REFERENCE": execBinding, "LIFETIME": execLifetime,
	"COMPILETIME": execCompileTime, "MODULE": execModule, "FFI_ABI": execABI,
	"MEMORY": execMemory, "ASSERTION": execControl,
	"IR_BINARY_OPERATION": execExpression, "IR_CONVERSION": execConversion,
	"IR_CONTROL_TRANSFER": execControl,
}

// observedPrimitiveAliases covers handler names emitted by the empirical
// miners (for example "execControl") and resolves them to the same existing
// execution contracts. Parser machine atoms are deliberately absent here:
// they are implemented by the lexer/parser engine, not UAST semantics.
var observedPrimitiveAliases = map[string]UASTExecutionPrimitive{
	"execABI": execABI, "execAnnotation": execAnnotation, "execBinding": execBinding,
	"execCall": execCall, "execCapture": execCapture, "execCompileTime": execCompileTime,
	"execConcurrency": execConcurrency, "execControl": execControl, "execConversion": execConversion,
	"execData": execData, "execDeclaration": execDeclaration, "execException": execException,
	"execExpression": execExpression, "execLifetime": execLifetime, "execMemory": execMemory,
	"execModule": execModule, "execTypes": execTypes, "execLowering": execLowering,
	"execValidation":          execValidation,
	"SEMANTIC_FACT_PRIMITIVE": execValidation, "UAST_PRIMITIVE": execSyntax,
	"TYPE_PRIMITIVE": execTypes, "CONTROL_FLOW_PRIMITIVE": execControl,
	"BACKEND_PROJECTION_PRIMITIVE": execLowering, "CODEGEN_PRIMITIVE": execLowering,
	"TARGET_TOOLCHAIN_PRIMITIVE": execValidation, "COMPILER_IMPLEMENTATION_DETAIL": execValidation,
}

// EmpiricalPy2ManyPrimitive returns the canonical primitive for a known AST
// operation. Unknown operations remain unavailable and are handled by the
// normal compatibility path.
func EmpiricalPy2ManyPrimitive(astNode string) (string, bool) {
	p, ok := empiricalLoweringPrimitives[astNode]
	return p, ok
}

// EmpiricalPy2ManyExecution links an observed py2many primitive to the
// existing productive UAST execution handler.
func EmpiricalPy2ManyExecution(primitive string) (UASTExecutionPrimitive, bool) {
	p, ok := empiricalPrimitiveExecution[primitive]
	if !ok {
		return "", false
	}
	_, registered := executionPrimitiveHandlers()[p]
	return p, registered
}

// EmpiricalCompilerDetailExecution keeps compiler/bytecode-only observations
// on the existing validation path. They are useful contracts but do not imply
// a new executable UAST semantic.
func EmpiricalCompilerDetailExecution(primitive string) (UASTExecutionPrimitive, bool) {
	if primitive != "COMPILER_IMPLEMENTATION_DETAIL" {
		return "", false
	}
	_, ok := executionPrimitiveHandlers()[execValidation]
	return execValidation, ok
}

// ResolveEmpiricalPrimitive is the common bridge used by evidence consumers
// for both compiler and py2many primitive names.
func ResolveEmpiricalPrimitive(primitive string) (UASTExecutionPrimitive, bool) {
	p, ok := empiricalPrimitiveExecution[primitive]
	if !ok {
		p, ok = observedPrimitiveAliases[primitive]
	}
	if !ok {
		return "", false
	}
	_, registered := executionPrimitiveHandlers()[p]
	return p, registered
}

// ResolveObservedPrimitive is the shared evidence bridge used by the
// all-to-all matrix. It links observed semantic names to productive handlers
// while leaving parser-only operations to their existing machine handlers.
func ResolveObservedPrimitive(primitive string) (UASTExecutionPrimitive, bool) {
	return ResolveEmpiricalPrimitive(primitive)
}

// ResolveEmpiricalPrimitiveVector resolves a composed evidence signature to
// the unique set of existing productive handlers. Ambiguous source evidence
// therefore remains a composition of normal UAST consumers rather than a new
// primitive or language-specific path.
func ResolveEmpiricalPrimitiveVector(primitives []string) []UASTExecutionPrimitive {
	seen := map[UASTExecutionPrimitive]bool{}
	result := make([]UASTExecutionPrimitive, 0, len(primitives))
	for _, name := range primitives {
		if p, ok := ResolveEmpiricalPrimitive(name); ok && !seen[p] {
			seen[p] = true
			result = append(result, p)
		}
	}
	return result
}
