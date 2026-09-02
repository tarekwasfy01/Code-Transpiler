package backend

// Code generated from the checked-in UAST renderer contracts; DO NOT EDIT.
//
// This is a declarative binding table. It does not contain AST data or any
// source-language semantics. The implementations named here operate directly
// on uastExecutionGraph and are selected by StructureProjectionContract.

type ProjectionRendererBinding struct {
	Form       string
	RendererID string
	SourceFile string
	Function   string
	Reusable   bool
}

var generatedProjectionRendererBindings = map[string]ProjectionRendererBinding{
	projectionFormCore: {
		Form: projectionFormCore, RendererID: "uast.core", SourceFile: "uast_target_codegen.go", Function: "(*targetGen).uastStatement/uastExpression", Reusable: true,
	},
	projectionFormAggregate: {
		Form: projectionFormAggregate, RendererID: "uast.aggregate", SourceFile: "uast_target_codegen.go", Function: "(*targetGen).uastAggregateExpression", Reusable: true,
	},
	projectionFormVariable: {
		Form: projectionFormVariable, RendererID: "uast.variable", SourceFile: "uast_target_codegen.go", Function: "(*targetGen).uastStatement", Reusable: true,
	},
	projectionFormDeclGroup: {
		Form: projectionFormDeclGroup, RendererID: "uast.declaration_group", SourceFile: "uast_target_codegen.go", Function: "(*targetGen).uastStatement", Reusable: true,
	},
	projectionFormAtomic: {
		Form: projectionFormAtomic, RendererID: "uast.atomic.direct", SourceFile: "uast_target_codegen.go", Function: "(*targetGen).uastStatement/uastExpression", Reusable: true,
	},
	projectionFormStatement: {
		Form: projectionFormStatement, RendererID: "uast.statement.direct", SourceFile: "uast_target_codegen.go", Function: "(*targetGen).uastStatement", Reusable: true,
	},
	projectionFormMetadata: {
		Form: projectionFormMetadata, RendererID: "uast.metadata", SourceFile: "uast_target_codegen.go", Function: "(*targetGen).uastStatement", Reusable: true,
	},
	projectionFormFallback: {
		Form: projectionFormFallback, RendererID: "uast.fallback.runtime", SourceFile: "uast_target_codegen.go", Function: "(*targetGen).uastFallbackStatement/uastFallbackExpression", Reusable: true,
	},
}

func generatedProjectionRendererBinding(form string) (ProjectionRendererBinding, bool) {
	binding, ok := generatedProjectionRendererBindings[form]
	return binding, ok
}
