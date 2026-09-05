package backend

import "strings"

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
	"print": {"CallExpr", "call"}, "object": {"AggregateExpr", "aggregate"},
	// Compiler evidence aliases are mapped only to already-existing canonical
	// UAST forms. They carry structured parser facts through the common
	// frontend boundary; no spelling-specific parser or target branch exists.
	"attribute": {"MemberAccessExpr", "member"}, "method": {"MemberAccessExpr", "member"},
	"subscript": {"IndexExpr", "index"}, "map_access": {"IndexExpr", "index"},
	"object_construction": {"AggregateExpr", "aggregate"}, "destructuring": {"BindingPattern", "binding"},
	"pattern": {"BindingPattern", "binding"}, "conditional": {"IfStmt", "if"},
	"switch": {"SwitchMatchStmt", "switch"}, "async": {"ConcurrencyOp", "concurrency"},
	"await": {"ConcurrencyOp", "concurrency"}, "promise": {"ConcurrencyOp", "concurrency"},
	"range_iteration": {"ForEachStmt", "for"}, "channel_receive": {"OperationExpr", "expression"},
	"conversion": {"OperationExpr", "expression"}, "receiver": {"MemberAccessExpr", "member"},
}

func matrixUASTKind(kind string) (string, string, bool) {
	// Harvested language operations are normalized to the existing canonical
	// construct contracts.  This is a data-driven semantic join: operation
	// spelling never creates a language-specific frontend or a second IR.
	kind = canonicalHarvestConstruct(kind)
	e, ok := frontendFactCrosswalk[kind]
	return e.StructuralKind, e.UASTKind, ok
}

func canonicalHarvestConstruct(kind string) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	switch k {
	case "assignment", "assign":
		return "assign"
	case "function", "closure", "lambda":
		return "function"
	case "load", "symbol_ref", "symbol-reference", "identifier":
		return "identifier"
	case "literal", "bool", "float64", "int64", "string":
		return "literal"
	case "call", "invoke":
		return "call"
	case "print", "println", "printf":
		return "call"
	case "iteration", "for", "foreach", "range", "range_iteration":
		return "iteration"
	case "attribute", "method", "receiver":
		return "member"
	case "subscript", "map_access":
		return "index"
	case "channel_receive", "conversion":
		return "expression"
	case "object_construction", "object", "objects":
		return "aggregate"
	case "destructuring", "pattern":
		return "binding"
	case "exceptions", "try":
		return "exception"
	case "import", "include":
		return "module"
	case "conditional":
		return "if"
	case "switch":
		return "switch"
	case "async", "await", "promise":
		return "concurrency"
	case "return":
		return "return"
	case "not":
		return "unary"
	case "and", "or", "bit_and", "bit_or", "eq", "ne", "lt", "le", "gt", "ge", "add", "sub", "mul", "div":
		return "binary"
	default:
		return k
	}
}
