package backend

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// TargetSyntaxTemplate is a declarative, syntax-only quotient class. It has
// neither UAST node data nor source-language meaning; the canonical UAST
// remains the only semantic input to the projector.
type TargetSyntaxTemplate struct {
	ID              string
	Signature       string
	ProjectionForms []string
	RecipeIDs       []string
	Available       bool
}

// TargetSyntaxTemplateCell is one Target × ProjectionClass decision. Target
// parameters are deliberately stored separately from the target-independent
// template signature.
type TargetSyntaxTemplateCell struct {
	Target          string
	ProjectionClass string
	TemplateID      string
	SyntaxSignature string
	ProjectionForm  string
	RecipeIDs       []string
	Complete        bool
	MissingReason   string
	Parameters      map[string]string
}

// TargetStructureSyntaxCell is the exact structure-granular expansion of a
// TargetSyntaxTemplateCell. The 13×73 class matrix is retained for quotient
// analysis; this table is used when a class contains multiple syntax forms.
// It is a checked contract row, never a program representation.
type TargetStructureSyntaxCell struct {
	Target          string
	StructureKind   string
	ProjectionClass string
	TemplateID      string
	ProjectionForm  string
	Complete        bool
	MissingReason   string
}

// TargetSyntaxTemplateAnalysis is the checked 13×73 syntax contract plane. It
// is a registry/report, not a target or semantic intermediate representation.
type TargetSyntaxTemplateAnalysis struct {
	Templates        []TargetSyntaxTemplate
	Cells            []TargetSyntaxTemplateCell
	StructureCells   []TargetStructureSyntaxCell
	ExistingMatches  int
	MissingTemplate  int
	MissingParameter int
}

var targetSyntaxTemplateOnce struct {
	sync.Once
	analysis TargetSyntaxTemplateAnalysis
	err      error
}

func knownTargetSyntaxForm(form string) (TargetSyntaxTemplateForm, bool) {
	for _, item := range generatedTargetSyntaxTemplateForms {
		if item.ProjectionForm == form {
			return item, true
		}
	}
	return TargetSyntaxTemplateForm{}, false
}

func syntaxTemplateParameters(spec TargetSpec) map[string]string {
	out := map[string]string{
		"operator_table":  fmt.Sprintf("%d", len(spec.Operators)),
		"child_separator": spec.ChildSeparator,
		"block_open":      spec.BlockOpen,
		"block_close":     spec.BlockClose,
		"indent":          spec.Indent,
		"terminator":      spec.StatementTerminator,
	}
	for key, value := range spec.SyntaxTokens {
		out["token."+key] = value
	}
	for key, value := range spec.Operators {
		out["operator."+key+".spelling"] = value.Spelling
		out["operator."+key+".precedence"] = fmt.Sprintf("%d", value.Precedence)
		out["operator."+key+".associativity"] = value.Associativity
		out["operator."+key+".fixity"] = value.Fixity
	}
	out["literal.true"] = spec.Literals.True
	out["literal.false"] = spec.Literals.False
	out["literal.null"] = spec.Literals.Null
	out["literal.string_quote"] = spec.Literals.StringQuote
	out["literal.string_wrap"] = spec.Literals.StringWrap
	out["literal.number_wrap"] = spec.Literals.NumberWrap
	out["literal.number_rule"] = spec.Literals.NumberRule
	out["type.operation.form"] = spec.TypedOperations.Form
	out["type.operation.runtime"] = spec.TypedOperations.Runtime
	out["type.operation.arguments_open"] = spec.TypedOperations.ArgumentsOpen
	out["type.operation.arguments_close"] = spec.TypedOperations.ArgumentsClose
	out["import.ordering"] = spec.Imports.Ordering
	out["import.runtime_requirement"] = spec.Imports.RuntimeRequirement
	for key, value := range spec.ProjectionForms {
		out["projection."+key+".mode"] = string(value.Mode)
		out["projection."+key+".requirements"] = strings.Join(uniqueSorted(value.Requirements), ";")
	}
	return out
}

func syntaxParameterSignature(parameters map[string]string) string {
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+parameters[key])
	}
	return strings.Join(parts, ";")
}

func projectionClassRecipeIDs(harvest ProjectionRendererHarvest, class string) ([]string, error) {
	archetype := harvest.Assignments[class]
	if archetype == "" {
		return nil, nil
	}
	compositions, err := GeneratedProjectionRendererCompositions()
	if err != nil {
		return nil, err
	}
	for _, item := range compositions {
		if item.ArchetypeID == archetype {
			return append([]string(nil), item.Primitives...), nil
		}
	}
	return nil, nil
}

func projectionClassTemplateSignature(contracts []StructureProjectionContract, recipeIDs []string) string {
	parts := make([]string, 0, len(contracts))
	for _, c := range contracts {
		parts = append(parts, strings.Join([]string{
			c.ProjectionForm, c.SyntacticCategory, strings.Join(c.ChildRelations, ";"), strings.Join(c.RequiredFields, ";"),
			strings.Join(c.ExecutionPrimitives, ";"), c.PrecedenceRole, c.BlockPolicy, c.TerminatorPolicy, c.EmissionPolicy,
		}, "|"))
	}
	sort.Strings(parts)
	sort.Strings(recipeIDs)
	return strings.Join(parts, "||") + "|recipes=" + strings.Join(recipeIDs, ";")
}

func targetTemplateParametersComplete(contract StructureProjectionContract, spec TargetSpec) bool {
	if spec.Indent == "" || spec.ChildSeparator == "" {
		return false
	}
	if contract.PrecedenceRole == "EXPRESSION" && len(spec.Operators) == 0 {
		return false
	}
	// Empty terminators and block closers are meaningful parameters for
	// indentation-sensitive targets, so their presence is represented by the
	// TargetSpec itself rather than by a non-empty-string test.
	return true
}

func buildTargetSyntaxTemplateAnalysis() (TargetSyntaxTemplateAnalysis, error) {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return TargetSyntaxTemplateAnalysis{}, err
	}
	harvest, err := UniversalProjectionRendererHarvest()
	if err != nil {
		return TargetSyntaxTemplateAnalysis{}, err
	}
	recipeRegistry, err := UniversalEmissionRecipeRegistry()
	if err != nil {
		return TargetSyntaxTemplateAnalysis{}, err
	}
	byClass := map[string][]StructureProjectionContract{}
	for _, c := range registry.Contracts {
		byClass[c.ProjectionClass] = append(byClass[c.ProjectionClass], c)
	}
	classes := make([]string, 0, len(byClass))
	for class := range byClass {
		classes = append(classes, class)
	}
	sort.Strings(classes)
	templateForSignature := map[string]string{}
	out := TargetSyntaxTemplateAnalysis{}
	for _, class := range classes {
		contracts := byClass[class]
		recipeIDs, err := projectionClassRecipeIDs(harvest, class)
		if err != nil {
			return out, err
		}
		// Classes with an existing direct renderer can have no residual recipe;
		// their direct UAST form is itself the proved syntax composition.
		forms := []string{}
		for _, c := range contracts {
			forms = append(forms, c.ProjectionForm)
		}
		forms = uniqueSorted(forms)
		signature := projectionClassTemplateSignature(contracts, append([]string(nil), recipeIDs...))
		id, exists := templateForSignature[signature]
		if !exists {
			id = fmt.Sprintf("TEMPLATE_%03d", len(templateForSignature)+1)
			templateForSignature[signature] = id
		}
		available := true
		for _, form := range forms {
			if _, ok := knownTargetSyntaxForm(form); !ok {
				available = false
				break
			}
		}
		if available {
			for _, recipeID := range recipeIDs {
				if _, ok := recipeRegistry.Recipes[recipeID]; !ok {
					available = false
					break
				}
			}
		}
		// Template data is target-independent. Record it once per exact signature.
		if !exists {
			out.Templates = append(out.Templates, TargetSyntaxTemplate{ID: id, Signature: signature, ProjectionForms: forms, RecipeIDs: uniqueSorted(recipeIDs), Available: available})
		}
		for _, target := range Backends() {
			spec, ok := targetSpec(target.ID)
			if !ok {
				return out, fmt.Errorf("missing target spec %q", target.ID)
			}
			parameters := syntaxTemplateParameters(spec)
			complete := available
			reason := ""
			if !complete {
				reason = "MISSING_TARGET_TEMPLATE"
			}
			if complete && !targetTemplateParametersComplete(contracts[0], spec) {
				complete, reason = false, "MISSING_TARGET_PARAMETER"
			}
			// The TargetSpec must also explicitly register every selected direct
			// form. This avoids accepting a syntactic shape merely by language name.
			if complete {
				for _, form := range forms {
					if _, ok := spec.ProjectionForms[form]; !ok {
						complete, reason = false, "MISSING_TARGET_TEMPLATE"
						break
					}
				}
			}
			if complete {
				out.ExistingMatches++
			} else if reason == "MISSING_TARGET_TEMPLATE" {
				out.MissingTemplate++
			} else {
				out.MissingParameter++
			}
			out.Cells = append(out.Cells, TargetSyntaxTemplateCell{Target: target.ID, ProjectionClass: class, TemplateID: id, SyntaxSignature: signature + "|target_parameters=" + syntaxParameterSignature(parameters), ProjectionForm: strings.Join(forms, ";"), RecipeIDs: uniqueSorted(recipeIDs), Complete: complete, MissingReason: reason, Parameters: parameters})
		}
	}
	// Expand the quotient only for executable decisions. This preserves the
	// exact 13×73 class plane above while preventing a class that groups two
	// different syntax forms from masking an otherwise established renderer.
	for _, target := range Backends() {
		spec, ok := targetSpec(target.ID)
		if !ok {
			return out, fmt.Errorf("missing target spec %q", target.ID)
		}
		for _, contract := range registry.Contracts {
			cell, found := out.Cell(target.ID, contract.ProjectionClass)
			if !found {
				return out, fmt.Errorf("missing target syntax quotient cell %s/%s", target.ID, contract.ProjectionClass)
			}
			complete, reason := false, "MISSING_TARGET_TEMPLATE"
			if _, known := knownTargetSyntaxForm(contract.ProjectionForm); known {
				if !targetTemplateParametersComplete(contract, spec) {
					reason = "MISSING_TARGET_PARAMETER"
				} else if _, registered := spec.ProjectionForms[contract.ProjectionForm]; registered {
					complete, reason = true, ""
				}
			}
			out.StructureCells = append(out.StructureCells, TargetStructureSyntaxCell{Target: target.ID, StructureKind: contract.StructureKind, ProjectionClass: contract.ProjectionClass, TemplateID: cell.TemplateID, ProjectionForm: contract.ProjectionForm, Complete: complete, MissingReason: reason})
		}
	}
	sort.Slice(out.Templates, func(i, j int) bool { return out.Templates[i].ID < out.Templates[j].ID })
	sort.Slice(out.Cells, func(i, j int) bool {
		if out.Cells[i].Target != out.Cells[j].Target {
			return out.Cells[i].Target < out.Cells[j].Target
		}
		return out.Cells[i].ProjectionClass < out.Cells[j].ProjectionClass
	})
	sort.Slice(out.StructureCells, func(i, j int) bool {
		if out.StructureCells[i].Target != out.StructureCells[j].Target {
			return out.StructureCells[i].Target < out.StructureCells[j].Target
		}
		return out.StructureCells[i].StructureKind < out.StructureCells[j].StructureKind
	})
	return out, nil
}

func UniversalTargetSyntaxTemplateAnalysis() (TargetSyntaxTemplateAnalysis, error) {
	targetSyntaxTemplateOnce.Do(func() {
		targetSyntaxTemplateOnce.analysis, targetSyntaxTemplateOnce.err = buildTargetSyntaxTemplateAnalysis()
	})
	return targetSyntaxTemplateOnce.analysis, targetSyntaxTemplateOnce.err
}

func (a TargetSyntaxTemplateAnalysis) Cell(target, projectionClass string) (TargetSyntaxTemplateCell, bool) {
	for _, cell := range a.Cells {
		if cell.Target == target && cell.ProjectionClass == projectionClass {
			return cell, true
		}
	}
	return TargetSyntaxTemplateCell{}, false
}

func (a TargetSyntaxTemplateAnalysis) StructureCell(target, structure string) (TargetStructureSyntaxCell, bool) {
	for _, cell := range a.StructureCells {
		if cell.Target == target && cell.StructureKind == structure {
			return cell, true
		}
	}
	return TargetStructureSyntaxCell{}, false
}

// SupportsContract resolves a single structural member of a projection class.
// Projection classes are an execution quotient and can contain more than one
// syntax form. A class-level Complete=false must therefore never suppress a
// separately proved direct renderer for another member of that class.
func (a TargetSyntaxTemplateAnalysis) SupportsContract(target string, contract StructureProjectionContract, spec TargetSpec) bool {
	cell, ok := a.StructureCell(target, contract.StructureKind)
	return ok && cell.Complete
}

// ExecuteTargetSyntaxTemplate applies only an already-registered template and
// delegates all layout to the three generic emission handlers. It cannot turn
// a missing target template into source text.
func ExecuteTargetSyntaxTemplate(recipe EmissionRecipe, template TargetSyntaxTemplateCell, spec TargetSpec, input EmissionRecipeInput) (Doc, error) {
	if !template.Complete {
		return nil, fmt.Errorf("TARGET_SYNTAX_%s: projection class %s for target %s", template.MissingReason, template.ProjectionClass, template.Target)
	}
	found := false
	for _, id := range template.RecipeIDs {
		if id == recipe.ID {
			found = true
			break
		}
	}
	if len(template.RecipeIDs) != 0 && !found {
		return nil, fmt.Errorf("template %s does not permit recipe %s", template.TemplateID, recipe.ID)
	}
	return ExecuteEmissionRecipe(recipe, input, spec)
}

func WriteTargetSyntaxTemplateAnalysis(dir string) (TargetSyntaxTemplateAnalysis, error) {
	a, err := UniversalTargetSyntaxTemplateAnalysis()
	if err != nil {
		return TargetSyntaxTemplateAnalysis{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return TargetSyntaxTemplateAnalysis{}, err
	}
	templateRows := [][]string{}
	for _, t := range a.Templates {
		templateRows = append(templateRows, []string{t.ID, t.Signature, strings.Join(t.ProjectionForms, ";"), strings.Join(t.RecipeIDs, ";"), fmt.Sprintf("%t", t.Available)})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "target_syntax_templates.csv"), []string{"template_id", "syntax_signature", "projection_forms", "emission_recipes", "existing_code_template"}, templateRows); err != nil {
		return a, err
	}
	cellRows, parameterRows := [][]string{}, [][]string{}
	for _, c := range a.Cells {
		cellRows = append(cellRows, []string{c.Target, c.ProjectionClass, c.TemplateID, c.SyntaxSignature, c.ProjectionForm, strings.Join(c.RecipeIDs, ";"), fmt.Sprintf("%t", c.Complete), c.MissingReason})
		keys := make([]string, 0, len(c.Parameters))
		for k := range c.Parameters {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			parameterRows = append(parameterRows, []string{c.Target, c.TemplateID, k, c.Parameters[k]})
		}
	}
	if err := writeProjectionCSV(filepath.Join(dir, "target_projection_syntax_matrix.csv"), []string{"target", "projection_class", "template_id", "target_syntax_signature", "projection_forms", "emission_recipes", "complete", "missing_reason"}, cellRows); err != nil {
		return a, err
	}
	structureRows := [][]string{}
	for _, cell := range a.StructureCells {
		structureRows = append(structureRows, []string{cell.Target, cell.StructureKind, cell.ProjectionClass, cell.TemplateID, cell.ProjectionForm, fmt.Sprintf("%t", cell.Complete), cell.MissingReason})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "target_structure_syntax_matrix.csv"), []string{"target", "structure_id", "projection_class", "template_id", "projection_form", "complete", "missing_reason"}, structureRows); err != nil {
		return a, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "target_syntax_parameters_generated.csv"), []string{"target", "template_id", "parameter", "value"}, parameterRows); err != nil {
		return a, err
	}
	if err := writeProjectionCSV(filepath.Join(dir, "target_syntax_signatures.csv"), []string{"target", "projection_class", "template_id", "target_syntax_signature", "projection_forms", "emission_recipes", "complete", "missing_reason"}, cellRows); err != nil {
		return a, err
	}
	classRows := [][]string{}
	byTemplate := map[string][]TargetSyntaxTemplateCell{}
	for _, cell := range a.Cells {
		byTemplate[cell.TemplateID] = append(byTemplate[cell.TemplateID], cell)
	}
	ids := make([]string, 0, len(byTemplate))
	for id := range byTemplate {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		cells := byTemplate[id]
		members := make([]string, 0, len(cells))
		for _, cell := range cells {
			members = append(members, cell.Target+":"+cell.ProjectionClass)
		}
		sort.Strings(members)
		classRows = append(classRows, []string{id, strings.Join(members, ";"), fmt.Sprintf("%d", len(cells))})
	}
	if err := writeProjectionCSV(filepath.Join(dir, "target_syntax_template_equivalence_classes.csv"), []string{"template_id", "target_projection_members", "cell_count"}, classRows); err != nil {
		return a, err
	}
	// This is M_RT: existing direct-UAST renderer form × target syntax
	// template. A positive cell requires an exact harvested form binding; it
	// never promotes a legacy semantic backend to a renderer.
	rendererRows := [][]string{}
	for _, template := range a.Templates {
		for _, form := range template.ProjectionForms {
			binding, bound := generatedProjectionRendererBinding(form)
			rendererRows = append(rendererRows, []string{template.ID, form, binding.RendererID, fmt.Sprintf("%t", bound && binding.Reusable)})
		}
	}
	if err := writeProjectionCSV(filepath.Join(dir, "existing_renderer_target_syntax_template_matrix.csv"), []string{"template_id", "projection_form", "renderer_id", "exact_existing_renderer"}, rendererRows); err != nil {
		return a, err
	}
	// Factor every still-missing Target×ProjectionClass cell through the
	// already generated emission recipes. The rows are atomic obligations, not
	// per-UASF code requests, and are the input to a future rewrite/helper/
	// runtime closure.
	recipeRegistry, err := UniversalEmissionRecipeRegistry()
	if err != nil {
		return a, err
	}
	missingRows := [][]string{}
	for _, cell := range a.Cells {
		if cell.Complete {
			continue
		}
		for _, recipeID := range cell.RecipeIDs {
			recipe, ok := recipeRegistry.Recipes[recipeID]
			if !ok {
				return a, fmt.Errorf("template %s refers to absent recipe %s", cell.TemplateID, recipeID)
			}
			for _, operation := range recipe.Operations {
				missingRows = append(missingRows, []string{cell.Target, cell.ProjectionClass, cell.TemplateID, recipeID, operation.Atomic, string(operation.Handler), cell.MissingReason})
			}
		}
	}
	if err := writeProjectionCSV(filepath.Join(dir, "missing_target_template_obligation_matrix.csv"), []string{"target", "projection_class", "template_id", "emission_recipe", "atomic_target_syntax_operation", "generic_handler", "missing_reason"}, missingRows); err != nil {
		return a, err
	}
	return a, nil
}
