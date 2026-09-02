// Code generated from the checked-in direct-UAST renderer catalog; DO NOT EDIT.
package backend

// TargetSyntaxTemplateForm identifies a syntax form already emitted directly
// from uastExecutionGraph. The table contains no program semantics and is
// deliberately smaller than the target×projection matrix.
type TargetSyntaxTemplateForm struct {
	ProjectionForm string
	RendererID     string
}

var generatedTargetSyntaxTemplateForms = []TargetSyntaxTemplateForm{
	{ProjectionForm: projectionFormCore, RendererID: "uast.core"},
	{ProjectionForm: projectionFormAggregate, RendererID: "uast.aggregate"},
	{ProjectionForm: projectionFormVariable, RendererID: "uast.variable"},
	{ProjectionForm: projectionFormDeclGroup, RendererID: "uast.declaration_group"},
	{ProjectionForm: projectionFormMetadata, RendererID: "uast.metadata"},
	{ProjectionForm: projectionFormAtomic, RendererID: "uast.atomic.direct"},
	{ProjectionForm: projectionFormStatement, RendererID: "uast.statement.direct"},
	{ProjectionForm: projectionFormFallback, RendererID: "uast.fallback.runtime"},
}
