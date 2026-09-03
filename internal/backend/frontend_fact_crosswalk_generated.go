package backend

// frontendFactCrosswalk is the generated, language-neutral projection table
// for semantic kinds observed in local FrontendSemanticFacts captures. The
// table deliberately maps semantic kinds to existing UAST nodes; it does not
// introduce a second IR or language-specific lowering rules.
type frontendFactCrosswalkEntry struct {
	StructuralKind string
	UASTKind       string
}

var frontendFactCrosswalk = map[string]frontendFactCrosswalkEntry{
	"literal": {"LiteralExpr", "literal"}, "identifier": {"SymbolRef", "identifier"},
	"binary": {"OperationExpr", "binary"}, "unary": {"OperationExpr", "unary"},
	"deref": {"Deref", "deref"}, "address": {"AddressOf", "address"},
	"member": {"MemberAccessExpr", "member"}, "call": {"CallExpr", "call"},
	"index": {"IndexExpr", "index"}, "slice": {"SliceExpr", "slice"},
	"assign": {"AssignStmt", "assign"}, "return": {"ReturnStmt", "return"},
	"function": {"ClosureExpr", "function"}, "closure": {"ClosureExpr", "function"}, "lambda": {"ClosureExpr", "function"},
	"for": {"ForEachStmt", "for"}, "foreach": {"ForEachStmt", "for"}, "loop": {"ForEachStmt", "for"}, "iteration": {"ForEachStmt", "for"},
	"binding": {"BindingPattern", "binding"}, "aggregate": {"AggregateExpr", "aggregate"}, "comprehension": {"AggregateExpr", "aggregate"},
	"tuple": {"TupleExpr", "tuple"}, "block": {"Scope", "block"}, "if": {"IfStmt", "if"}, "while": {"LoopStmt", "while"},
	"module": {"ModuleDecl", "module"}, "exception": {"TryStmt", "exception"}, "concurrency": {"ConcurrencyOp", "concurrency"},
	"reflection": {"ReflectionOp", "reflection"}, "expression": {"OperationExpr", "expression"}, "unknown": {"OperationExpr", "expression"},
}

func matrixUASTKind(kind string) (string, string, bool) {
	e, ok := frontendFactCrosswalk[kind]
	return e.StructuralKind, e.UASTKind, ok
}
