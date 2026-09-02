package backend

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGeneratedEmissionRecipesAreExactAndExecutable(t *testing.T) {
	registry, err := UniversalEmissionRecipeRegistry()
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := UniversalEmissionContractAnalysis()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(registry.Recipes), len(analysis.Combinators); got != want {
		t.Fatalf("recipes=%d want matrix-derived=%d", got, want)
	}
	if got, want := len(registry.HandlerClasses), 3; got != want {
		t.Fatalf("handler classes=%d want=%d", got, want)
	}
	for id, recipe := range registry.Recipes {
		children := []EmissionRecipeChild{}
		ordinal := 0
		for _, operation := range recipe.Operations {
			if operation.Handler != emissionHandlerChild {
				continue
			}
			children = append(children, EmissionRecipeChild{Role: operation.Slot, Ordinal: ordinal, Doc: DocText{Text: operation.Slot}})
			ordinal++
		}
		spec, ok := targetSpec("go")
		if !ok {
			t.Fatal("missing Go target spec")
		}
		first, err := ExecuteEmissionRecipe(recipe, EmissionRecipeInput{NodeID: 17, Children: children}, spec)
		if err != nil {
			t.Fatalf("recipe %s failed: %v", id, err)
		}
		second, err := ExecuteEmissionRecipe(recipe, EmissionRecipeInput{NodeID: 17, Children: children}, spec)
		if err != nil {
			t.Fatalf("recipe %s second execution: %v", id, err)
		}
		a, b := (UniversalFormatter{Indent: spec.Indent}).Format(first), (UniversalFormatter{Indent: spec.Indent}).Format(second)
		if a == "" || a != b {
			t.Fatalf("recipe %s output=%q repeat=%q", id, a, b)
		}
		for _, operation := range recipe.Operations {
			if operation.Handler == emissionHandlerChild && !strings.Contains(a, operation.Slot) {
				t.Fatalf("recipe %s omitted proven child slot %s from %q", id, operation.Slot, a)
			}
		}
	}
}

func TestProjectionPrimitiveCoverIsExactAndMinimized(t *testing.T) {
	analysis, err := UniversalProjectionObligationAnalysis()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(analysis.Primitives), 21; got != want {
		t.Fatalf("matrix quotient primitives=%d want minimized=%d", got, want)
	}
	if err := analysis.ValidateExact(); err != nil {
		t.Fatalf("minimum rectangle cover is not exact: %v", err)
	}
	for _, primitive := range analysis.Primitives {
		if len(primitive.Archetypes) == 0 || len(primitive.Obligations) == 0 {
			t.Fatalf("empty primitive in exact cover: %+v", primitive)
		}
	}
}

func TestEmissionRecipesUseOnlyTargetSyntaxParameters(t *testing.T) {
	registry, err := UniversalEmissionRecipeRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range Backends() {
		spec, ok := targetSpec(target.ID)
		if !ok || spec.Indent == "" || spec.ChildSeparator == "" {
			t.Fatalf("target syntax spec incomplete for %s", target.ID)
		}
		for _, recipe := range registry.Recipes {
			if _, err := ExecuteEmissionRecipe(recipe, EmissionRecipeInput{}, spec); err != nil {
				t.Fatalf("recipe %s target %s: %v", recipe.ID, target.ID, err)
			}
		}
	}
}

func TestTargetSyntaxTemplateQuotientIsExactAndConservative(t *testing.T) {
	analysis, err := UniversalTargetSyntaxTemplateAnalysis()
	if err != nil {
		t.Fatal(err)
	}
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(analysis.Cells), len(Backends())*len(registry.Classes); got != want {
		t.Fatalf("target syntax cells=%d want=%d", got, want)
	}
	if got, want := len(analysis.StructureCells), len(Backends())*len(registry.Contracts); got != want {
		t.Fatalf("target structure syntax cells=%d want=%d", got, want)
	}
	seen := map[string]bool{}
	complete, missing := 0, 0
	for _, cell := range analysis.Cells {
		key := cell.Target + "|" + cell.ProjectionClass
		if seen[key] {
			t.Fatalf("duplicate target syntax cell %q", key)
		}
		seen[key] = true
		if cell.Complete {
			complete++
			if cell.MissingReason != "" {
				t.Fatalf("complete cell has a missing reason: %+v", cell)
			}
		} else {
			missing++
			if cell.MissingReason != "MISSING_TARGET_TEMPLATE" && cell.MissingReason != "MISSING_TARGET_PARAMETER" {
				t.Fatalf("unclassified missing syntax cell: %+v", cell)
			}
		}
	}
	if complete == 0 {
		t.Fatalf("syntax quotient contains no complete projection classes: complete=%d missing=%d", complete, missing)
	}
	seenStructure := map[string]bool{}
	structureComplete, structureMissing := 0, 0
	for _, cell := range analysis.StructureCells {
		key := cell.Target + "|" + cell.StructureKind
		if seenStructure[key] {
			t.Fatalf("duplicate target structure syntax cell %q", key)
		}
		seenStructure[key] = true
		if cell.Complete {
			structureComplete++
		} else {
			structureMissing++
		}
	}
	if structureComplete == 0 {
		t.Fatalf("structure syntax matrix contains no complete contracts: complete=%d missing=%d", structureComplete, structureMissing)
	}
	// A missing template must be a hard refusal by the generic executor.  It
	// must never turn a matrix-derived layout recipe into target source merely
	// because the target has ordinary punctuation parameters.
	spec, ok := targetSpec("go")
	if !ok {
		t.Fatal("missing Go target spec")
	}
	recipeRegistry, err := UniversalEmissionRecipeRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, cell := range analysis.Cells {
		if cell.Complete || len(cell.RecipeIDs) == 0 {
			continue
		}
		recipe := recipeRegistry.Recipes[cell.RecipeIDs[0]]
		if _, err := ExecuteTargetSyntaxTemplate(recipe, cell, spec, EmissionRecipeInput{}); err == nil || !strings.Contains(err.Error(), "TARGET_SYNTAX_MISSING_TARGET_TEMPLATE") {
			t.Fatalf("missing template was not refused: cell=%+v err=%v", cell, err)
		}
		break
	}
	output := t.TempDir()
	if _, err := WriteTargetSyntaxTemplateAnalysis(output); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"target_syntax_templates.csv", "target_projection_syntax_matrix.csv", "target_structure_syntax_matrix.csv", "target_syntax_parameters_generated.csv", "target_syntax_signatures.csv", "target_syntax_template_equivalence_classes.csv", "existing_renderer_target_syntax_template_matrix.csv", "missing_target_template_obligation_matrix.csv"} {
		if _, err := os.Stat(filepath.Join(output, name)); err != nil {
			t.Fatalf("missing target syntax artifact %s: %v", name, err)
		}
	}
}
