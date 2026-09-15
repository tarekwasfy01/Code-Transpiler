// Copyright (c) 2026 Tarek Wasfy
package backend

// GoIROperationContract is the small, source-independent contract exposed by
// the staged Go compiler migration. The opcode is provenance from
// cmd/compile/internal/ir; the semantic operation is an existing canonical
// SemanticOperation name. No new execution registry is introduced here.
type GoIROperationContract struct {
	Opcode          string
	SemanticName    string
	Arity           int
	SemanticFamily  string
	RequiresWitness bool
}

// GoIRNodeContract describes the structural canonical node for an IR opcode.
// It is deliberately separate from the typed-operation adapter because many
// Go IR operators (notably OADD) are overloaded by operand type.
type GoIRNodeContract struct {
	Opcode          string
	StructuralKind  string
	SemanticKind    string
	RequiresWitness bool
}

// GoIRNodeContractFor maps source-observable IR shapes to existing canonical
// UAST structures. Lowered/compiler bookkeeping nodes fail closed and are not
// promoted to source semantics.
func GoIRNodeContractFor(opcode string) (GoIRNodeContract, bool) {
	var c GoIRNodeContract
	switch opcode {
	case "ONAME":
		c = GoIRNodeContract{opcode, "SymbolRef", "identifier", true}
	case "OTYPE":
		c = GoIRNodeContract{opcode, "TypeRef", "type", true}
	case "OLITERAL":
		c = GoIRNodeContract{opcode, "LiteralExpr", "literal", true}
	case "ONIL":
		c = GoIRNodeContract{opcode, "NilLiteral", "literal", true}
	case "OADD", "OSUB", "OOR", "OXOR", "OADDSTR", "OANDAND", "OOROR", "OMUL", "ODIV", "OMOD", "OLSH", "ORSH", "OAND", "OANDNOT", "OEQ", "ONE", "OLT", "OLE", "OGE", "OGT", "ONOT", "OBITNOT", "OPLUS", "ONEG":
		c = GoIRNodeContract{opcode, "OperationExpr", "operation", true}
	case "OCALL", "OCALLFUNC", "OCALLMETH", "OCALLINTER":
		c = GoIRNodeContract{opcode, "CallExpr", "call", true}
	case "OAS", "OAS2", "OAS2DOTTYPE", "OAS2FUNC", "OAS2MAPR", "OAS2RECV", "OASOP":
		c = GoIRNodeContract{opcode, "AssignStmt", "assignment", true}
	case "OCOMPLIT", "OMAPLIT", "OSTRUCTLIT", "OARRAYLIT", "OSLICELIT", "OPTRLIT":
		c = GoIRNodeContract{opcode, "AggregateExpr", "aggregate", true}
	case "OINDEX", "OINDEXMAP":
		c = GoIRNodeContract{opcode, "IndexExpr", "index", true}
	case "OSLICE", "OSLICEARR", "OSLICESTR", "OSLICE3", "OSLICE3ARR":
		c = GoIRNodeContract{opcode, "SliceExpr", "slice", true}
	case "OCLOSURE":
		c = GoIRNodeContract{opcode, "ClosureExpr", "function", true}
	case "OIF":
		c = GoIRNodeContract{opcode, "IfStmt", "if", true}
	case "OFOR":
		c = GoIRNodeContract{opcode, "LoopStmt", "for", true}
	case "ORANGE":
		c = GoIRNodeContract{opcode, "ForEachStmt", "range", true}
	case "OBLOCK":
		c = GoIRNodeContract{opcode, "Scope", "block", true}
	case "ORETURN":
		c = GoIRNodeContract{opcode, "ReturnStmt", "return", true}
	case "OBREAK":
		c = GoIRNodeContract{opcode, "BreakStmt", "break", true}
	case "OCONTINUE":
		c = GoIRNodeContract{opcode, "ContinueStmt", "continue", true}
	case "ODEREF":
		c = GoIRNodeContract{opcode, "Deref", "deref", true}
	case "OADDR":
		c = GoIRNodeContract{opcode, "AddressOf", "address", true}
	default:
		return GoIRNodeContract{}, false
	}
	return c, true
}

// GoIROperationContractFor returns only mappings for operations whose current
// canonical typed contracts already exist. Unknown or compiler-only opcodes
// fail closed. The type is supplied by Go type resolution, so OADD is not
// incorrectly classified as integer addition for string/float operands.
func GoIROperationContractFor(opcode string, typ SemanticType, unary bool) (GoIROperationContract, bool) {
	name := nativeGoIRIntegerOperation(opcode, unary)
	if name == "" {
		name = nativeGoIRIntegerComparison(opcode)
	}
	if name == "" {
		return GoIROperationContract{}, false
	}
	arity := 2
	if unary {
		arity = 1
	}
	family := "integer"
	if name == "integer.equal" || name == "integer.not_equal" || name == "integer.less" || name == "integer.less_equal" || name == "integer.greater" || name == "integer.greater_equal" {
		family = "integer.compare"
	}
	return GoIROperationContract{Opcode: opcode, SemanticName: name, Arity: arity, SemanticFamily: family, RequiresWitness: true}, true
}

// SemanticOperationFromGoIR is the executable adapter boundary used by a
// future phase producer. It intentionally reuses the existing typed
// SemanticOperation carrier instead of creating a parallel IR or registry.
func SemanticOperationFromGoIR(opcode string, typ SemanticType, unary bool) (SemanticOperation, bool) {
	c, ok := GoIROperationContractFor(opcode, typ, unary)
	if !ok {
		return SemanticOperation{}, false
	}
	return SemanticOperation{Name: c.SemanticName, Type: typ}, true
}
