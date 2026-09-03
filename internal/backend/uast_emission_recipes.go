package backend

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// EmissionHandlerClass groups atomic matrix operations by identical syntax-only
// execution behavior. It is deliberately independent of UASF, source language,
// structural kind, and target identity.
type EmissionHandlerClass string

const (
	emissionHandlerValidate EmissionHandlerClass = "validate_contract_input"
	emissionHandlerChild    EmissionHandlerClass = "emit_ordered_child"
	emissionHandlerTarget   EmissionHandlerClass = "apply_target_parameter"
)

// EmissionRecipeOperation is one generated entry of M_EO. Atomic is retained
// verbatim so its matrix provenance can be checked before any Doc is built.
type EmissionRecipeOperation struct {
	Atomic  string
	Handler EmissionHandlerClass
	Slot    string
}

// EmissionRecipe is an ephemeral syntax plan. It holds no program facts: nodes,
// types, symbols and relations remain in UniversalASTDocument.
type EmissionRecipe struct {
	ID         string
	Archetypes []string
	Operations []EmissionRecipeOperation
}

// EmissionRecipeRegistry is the cached result of compiling the generated data.
// Recipes are built once per process and execution is O(recipe length).
type EmissionRecipeRegistry struct {
	Recipes        map[string]EmissionRecipe
	HandlerClasses []EmissionHandlerClass
}

type emissionRecipeSeed struct {
	ID          string
	Archetypes  []string
	Obligations []string
}

var emissionRecipeOnce struct {
	sync.Once
	registry EmissionRecipeRegistry
	err      error
}

func emissionHandlerForAtomic(atomic string) (EmissionHandlerClass, string, error) {
	switch {
	case strings.HasPrefix(atomic, "child="):
		return emissionHandlerChild, strings.TrimPrefix(atomic, "child="), nil
	case strings.HasPrefix(atomic, "category="), strings.HasPrefix(atomic, "field_role="), strings.HasPrefix(atomic, "relation_role="):
		return emissionHandlerValidate, "", nil
	case strings.HasPrefix(atomic, "precedence="), strings.HasPrefix(atomic, "block="), strings.HasPrefix(atomic, "terminator="):
		return emissionHandlerTarget, atomic, nil
	default:
		return "", "", fmt.Errorf("unknown atomic emission operation %q", atomic)
	}
}

func sameStringSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	a, b := append([]string(nil), left...), append([]string(nil), right...)
	sort.Strings(a)
	sort.Strings(b)
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func compileGeneratedEmissionRecipes() (EmissionRecipeRegistry, error) {
	analysis, err := UniversalEmissionContractAnalysis()
	if err != nil {
		return EmissionRecipeRegistry{}, err
	}
	byID := map[string]ProjectionObligationPrimitive{}
	for _, primitive := range analysis.Combinators {
		byID[primitive.ID] = primitive
	}
	result := EmissionRecipeRegistry{Recipes: map[string]EmissionRecipe{}}
	handlerSet := map[EmissionHandlerClass]bool{}
	if len(generatedEmissionRecipeSeeds) != len(byID) {
		return result, fmt.Errorf("generated emission recipe count %d differs from exact contract count %d", len(generatedEmissionRecipeSeeds), len(byID))
	}
	for _, seed := range generatedEmissionRecipeSeeds {
		primitive, ok := byID[seed.ID]
		if !ok {
			return result, fmt.Errorf("generated emission recipe %q has no matrix primitive", seed.ID)
		}
		if !sameStringSet(seed.Archetypes, primitive.Archetypes) || !sameStringSet(seed.Obligations, primitive.Obligations) {
			return result, fmt.Errorf("generated emission recipe %q is stale against the exact factorization", seed.ID)
		}
		if _, exists := result.Recipes[seed.ID]; exists {
			return result, fmt.Errorf("duplicate generated emission recipe %q", seed.ID)
		}
		ops := make([]EmissionRecipeOperation, 0, len(seed.Obligations))
		for _, atomic := range seed.Obligations {
			handler, slot, err := emissionHandlerForAtomic(atomic)
			if err != nil {
				return result, err
			}
			handlerSet[handler] = true
			ops = append(ops, EmissionRecipeOperation{Atomic: atomic, Handler: handler, Slot: slot})
		}
		result.Recipes[seed.ID] = EmissionRecipe{ID: seed.ID, Archetypes: append([]string(nil), seed.Archetypes...), Operations: ops}
	}
	for id := range byID {
		if _, ok := result.Recipes[id]; !ok {
			return result, fmt.Errorf("exact matrix primitive %q has no generated recipe", id)
		}
	}
	for handler := range handlerSet {
		result.HandlerClasses = append(result.HandlerClasses, handler)
	}
	sort.Slice(result.HandlerClasses, func(i, j int) bool { return result.HandlerClasses[i] < result.HandlerClasses[j] })
	return result, nil
}

// UniversalEmissionRecipeRegistry returns the build-cached generated recipes.
func UniversalEmissionRecipeRegistry() (EmissionRecipeRegistry, error) {
	emissionRecipeOnce.Do(func() { emissionRecipeOnce.registry, emissionRecipeOnce.err = compileGeneratedEmissionRecipes() })
	return emissionRecipeOnce.registry, emissionRecipeOnce.err
}

// EmissionRecipeChild preserves the proven syntax.child role and ordinal while
// holding only an already-rendered ephemeral Doc.
type EmissionRecipeChild struct {
	Role    string
	Ordinal int
	Doc     Doc
}

// EmissionRecipeInput adapts a single canonical UAST node to the generic
// executor. It intentionally excludes all semantic values except the node ID
// for diagnostics; children are passed as already-rendered syntax fragments.
type EmissionRecipeInput struct {
	NodeID   int
	Children []EmissionRecipeChild
}

// ExecuteEmissionRecipe builds only a layout Doc. It never selects a semantic
// lowering, resolves symbols, or interprets a source language. An empty role
// remains empty; the executor never invents a child or a target token.
func ExecuteEmissionRecipe(recipe EmissionRecipe, input EmissionRecipeInput, spec TargetSpec) (Doc, error) {
	allowed := map[string]bool{}
	hasBlock, hasTerminator, expression := false, false, false
	for _, op := range recipe.Operations {
		switch op.Handler {
		case emissionHandlerValidate:
			// Matrix assertion only. Its semantic validation occurred before
			// projection; this handler must not create syntax.
		case emissionHandlerChild:
			allowed[op.Slot] = true
		case emissionHandlerTarget:
			switch {
			case strings.HasPrefix(op.Atomic, "block=CHILD_BLOCK"):
				hasBlock = true
			case strings.HasPrefix(op.Atomic, "terminator=TARGET_SPEC"):
				hasTerminator = true
			case strings.HasPrefix(op.Atomic, "precedence=EXPRESSION"):
				expression = true
			}
		default:
			return nil, fmt.Errorf("recipe %s has unsupported handler %q", recipe.ID, op.Handler)
		}
	}
	children := make([]EmissionRecipeChild, 0, len(input.Children))
	for _, child := range input.Children {
		if allowed[child.Role] && child.Doc != nil {
			children = append(children, child)
		}
	}
	sort.SliceStable(children, func(i, j int) bool {
		if children[i].Ordinal != children[j].Ordinal {
			return children[i].Ordinal < children[j].Ordinal
		}
		return children[i].Role < children[j].Role
	})
	parts := make([]Doc, 0, len(children)*2+4)
	for i, child := range children {
		if i != 0 {
			parts = append(parts, DocText{Text: spec.ChildSeparator})
		}
		parts = append(parts, child.Doc)
	}
	body := DocConcat{Parts: parts}
	if hasBlock && len(parts) != 0 {
		block := []Doc{}
		if spec.BlockOpen != "" {
			block = append(block, DocText{Text: spec.BlockOpen})
		}
		block = append(block, DocHardLine{}, DocIndent{By: spec.Indent, Body: body})
		if spec.BlockClose != "" {
			block = append(block, DocHardLine{}, DocText{Text: spec.BlockClose})
		}
		body = DocConcat{Parts: block}
	}
	if hasTerminator && !expression && spec.StatementTerminator != "" {
		body = DocConcat{Parts: []Doc{body, DocText{Text: spec.StatementTerminator}}}
	}
	return body, nil
}

// GeneratedEmissionRecipeForPrimitive is used by the direct projector's
// preflight to verify that every residual syntax contract has a compiled
// layout plan. It does not upgrade target preservation modes by itself.
func GeneratedEmissionRecipeForPrimitive(id string) (EmissionRecipe, bool, error) {
	registry, err := UniversalEmissionRecipeRegistry()
	if err != nil {
		return EmissionRecipe{}, false, err
	}
	recipe, ok := registry.Recipes[id]
	return recipe, ok, nil
}
