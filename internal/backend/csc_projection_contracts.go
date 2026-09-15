// Copyright (c) 2026 Tarek Wasfy
package backend

// CSharpProjectionPrimitive describes a compiler-independent lowering step
// required before csc.exe can consume a SemanticProgram. It records an
// obligation and its fail-closed boundary; it is not a fake C# implementation.
type CSharpProjectionPrimitive struct {
	ID             string
	InputFamily    string
	RequiredFacts  []string
	EmittedSurface []string
	FailureIfUnset string
}

var csharpProjectionPrimitives = []CSharpProjectionPrimitive{
	{ID: "semantic_header_to_csharp_unit", InputFamily: "syntax_boundary", RequiredFacts: []string{"semantic schema", "unit identity", "namespace/module identity"}, EmittedSurface: []string{"C# compilation unit", "namespace", "typed declarations"}, FailureIfUnset: "CSHARP_PROJECTION_UNIT_CONTRACT_MISSING"},
	{ID: "semantic_graph_to_namespace_type_member", InputFamily: "namespace_member_model", RequiredFacts: []string{"UAST graph", "declaration identity", "member ownership"}, EmittedSurface: []string{"namespace", "class/struct/interface", "member declarations"}, FailureIfUnset: "CSHARP_PROJECTION_MEMBER_CONTRACT_MISSING"},
	{ID: "identifier_escaping_and_object_shape", InputFamily: "keyword_identifier_collision", RequiredFacts: []string{"canonical identifier", "keyword table", "object field types"}, EmittedSurface: []string{"escaped C# identifiers", "typed object/record shape"}, FailureIfUnset: "CSHARP_PROJECTION_IDENTIFIER_CONTRACT_MISSING"},
	{ID: "typed_member_signature_emitter", InputFamily: "member_declaration", RequiredFacts: []string{"callable signature", "parameter/result contracts", "receiver/ABI contract"}, EmittedSurface: []string{"typed methods", "constructors", "properties"}, FailureIfUnset: "CSHARP_PROJECTION_SIGNATURE_CONTRACT_MISSING"},
	{ID: "diagnostic_specific_projection", InputFamily: "other_csc", RequiredFacts: []string{"canonical node and operation contract"}, EmittedSurface: []string{"valid C# expression/statement"}, FailureIfUnset: "CSHARP_PROJECTION_NODE_CONTRACT_MISSING"},
}

// CSharpProjectionPrimitives returns defensive copies for frontend/backend
// tooling and evidence reports.
func CSharpProjectionPrimitives() []CSharpProjectionPrimitive {
	out := make([]CSharpProjectionPrimitive, len(csharpProjectionPrimitives))
	for i, primitive := range csharpProjectionPrimitives {
		out[i] = primitive
		out[i].RequiredFacts = append([]string(nil), primitive.RequiredFacts...)
		out[i].EmittedSurface = append([]string(nil), primitive.EmittedSurface...)
	}
	return out
}
