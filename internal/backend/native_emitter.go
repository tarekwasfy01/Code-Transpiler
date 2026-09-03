package backend

import "fmt"

// NativeEmissionInput contains already-rendered UAST children. It is a
// transient layout input; semantic values remain in UniversalASTDocument.
type NativeEmissionInput struct {
	NodeID   int
	Children []EmissionRecipeChild
}

// EmitNativeExecution is the single data-driven native emission entry point.
// It composes an existing emission recipe with an existing target template and
// TargetSpec. No target×primitive dispatch or runtime helper is introduced.
func EmitNativeExecution(spec TargetSpec, recipe EmissionRecipe, template TargetSyntaxTemplateCell, input NativeEmissionInput) (Doc, error) {
	if spec.ID == "" {
		return nil, fmt.Errorf("native emitter requires target spec")
	}
	return ExecuteTargetSyntaxTemplate(recipe, template, spec, EmissionRecipeInput{NodeID: input.NodeID, Children: input.Children})
}

// EmitNativeDocument is the central DIRECT document boundary.  The semantic
// projector has already lowered every UAST node through the native target
// renderer; this final recipe carries the resulting syntax document through
// the same NativeEmitterContract without reintroducing a runtime or a second
// semantic representation.  Keeping this operation here makes it impossible
// for a DIRECT projection to bypass the native emitter dispatch point.
func EmitNativeDocument(spec TargetSpec, source string) (Doc, error) {
	recipe := EmissionRecipe{
		ID: "DOCUMENT_DIRECT",
		Operations: []EmissionRecipeOperation{{
			Atomic: "child=document", Handler: emissionHandlerChild, Slot: "document",
		}},
	}
	template := TargetSyntaxTemplateCell{
		Target: spec.ID, ProjectionClass: "DOCUMENT_DIRECT", TemplateID: "document-direct",
		ProjectionForm: projectionFormCore, RecipeIDs: []string{"DOCUMENT_DIRECT"}, Complete: true,
	}
	return EmitNativeExecution(spec, recipe, template, NativeEmissionInput{
		NodeID: -1, Children: []EmissionRecipeChild{{Role: "document", Ordinal: 0, Doc: DocText{Text: source}}},
	})
}

// NativeExpressionEmitter, NativeStatementEmitter and
// NativeDeclarationEmitter are deliberately thin category wrappers around the
// same generic emitter. Their category is selected by the already registered
// projection form, never by a target-specific switch.
func NativeExpressionEmitter(spec TargetSpec, recipe EmissionRecipe, template TargetSyntaxTemplateCell, input NativeEmissionInput) (Doc, error) {
	return EmitNativeExecution(spec, recipe, template, input)
}

func NativeStatementEmitter(spec TargetSpec, recipe EmissionRecipe, template TargetSyntaxTemplateCell, input NativeEmissionInput) (Doc, error) {
	return EmitNativeExecution(spec, recipe, template, input)
}

func NativeDeclarationEmitter(spec TargetSpec, recipe EmissionRecipe, template TargetSyntaxTemplateCell, input NativeEmissionInput) (Doc, error) {
	return EmitNativeExecution(spec, recipe, template, input)
}
