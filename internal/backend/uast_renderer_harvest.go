package backend

import (
	"encoding/csv"
	"fmt"
	"go/ast"
	goParser "go/parser"
	goToken "go/token"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// ProjectionRendererCatalogEntry records a renderer's syntax interface. It is
// harvested from Go source and remains declarative: no AST node or semantic
// value is copied into the catalog.
type ProjectionRendererCatalogEntry struct {
	RendererID         string   `json:"renderer_id"`
	SourceFile         string   `json:"source_file"`
	SourceFunction     string   `json:"source_function"`
	SyntacticCategory  string   `json:"syntactic_category"`
	RequiredChildren   []string `json:"required_children"`
	RequiredFields     []string `json:"required_fields"`
	RequiredRelations  []string `json:"required_relations"`
	TargetScope        string   `json:"target_scope"`
	ReturnsDocOrSource string   `json:"returns_doc_or_source"`
	Reusable           bool     `json:"reusable"`
}

type ProjectionRendererArchetype struct {
	ID                string   `json:"id"`
	ProjectionClasses []string `json:"projection_classes"`
	Signature         string   `json:"signature"`
	ExistingRenderers []string `json:"existing_renderers"`
	Implemented       bool     `json:"implemented"`
}

// ProjectionRendererMatch is one cell of M_AR. A true cell means that the
// renderer's checked-in UAST syntax contract supplies every requirement of the
// archetype. Partial matches never authorize a projection.
type ProjectionRendererMatch struct {
	ArchetypeID string `json:"archetype_id"`
	RendererID  string `json:"renderer_id"`
	Compatible  bool   `json:"compatible"`
}

type ProjectionRendererHarvest struct {
	Catalog     []ProjectionRendererCatalogEntry `json:"catalog"`
	Archetypes  []ProjectionRendererArchetype    `json:"archetypes"`
	Assignments map[string]string                `json:"class_to_archetype"`
	Matches     []ProjectionRendererMatch        `json:"matches"`
}

func backendRendererSourceFiles() []string {
	_, current, _, ok := runtime.Caller(0)
	if !ok {
		return nil
	}
	dir := filepath.Dir(current)
	return []string{
		filepath.Join(dir, "codegen_generic.go"),
		filepath.Join(dir, "uast_target_codegen.go"),
		filepath.Join(dir, "uast_r_writer.go"),
		filepath.Join(dir, "universal_target_projector.go"),
		filepath.Join(dir, "targets.go"),
	}
}

func rendererFunctionName(decl *ast.FuncDecl) string {
	name := decl.Name.Name
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return name
	}
	switch receiver := decl.Recv.List[0].Type.(type) {
	case *ast.StarExpr:
		if ident, ok := receiver.X.(*ast.Ident); ok {
			return "(*" + ident.Name + ")." + name
		}
	case *ast.Ident:
		return "(" + receiver.Name + ")." + name
	}
	return name
}

func rendererSyntaxCategory(name string) string {
	switch {
	case strings.Contains(name, "Expression") || strings.HasSuffix(name, ".expr"):
		return "expression"
	case strings.Contains(name, "Statement") || strings.HasSuffix(name, ".stmt"):
		return "statement"
	case strings.Contains(name, "Project"):
		return "projector"
	case strings.Contains(name, "Prelude") || strings.Contains(name, "target"):
		return "target_support"
	default:
		return "syntax_support"
	}
}

func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != goToken.STRING {
		return "", false
	}
	return strings.Trim(literal.Value, "\""), true
}

func harvestedRendererRequirements(body *ast.BlockStmt) (children, fields, relations []string, targetScope string) {
	childSet, fieldSet, relationSet := map[string]bool{}, map[string]bool{}, map[string]bool{}
	targetScope = "shared"
	ast.Inspect(body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "one", "many", "firstChild":
			for _, arg := range call.Args {
				if value, ok := stringLiteral(arg); ok {
					childSet[value] = true
				}
			}
		case "oneRelationNode", "relationNodes":
			for _, arg := range call.Args {
				if value, ok := stringLiteral(arg); ok {
					relationSet[value] = true
				}
			}
		}
		return true
	})
	// Existing UAST renderer methods use decoded `operation`, `name` and node
	// fields rather than reconstructing a legacy expression. Their accesses are
	// represented by this common schema channel set.
	ast.Inspect(body, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "Name":
			fieldSet["name"] = true
		case "Operation":
			fieldSet["operation"] = true
		case "StructuralKind":
			fieldSet["kind"] = true
		case "target":
			targetScope = "all_registered_targets"
		}
		return true
	})
	for value := range childSet {
		children = append(children, value)
	}
	for value := range fieldSet {
		fields = append(fields, value)
	}
	for value := range relationSet {
		relations = append(relations, value)
	}
	sort.Strings(children)
	sort.Strings(fields)
	sort.Strings(relations)
	return children, fields, relations, targetScope
}

// HarvestProjectionRenderers parses the checked-in renderer sources. A legacy
// renderer is catalogued for auditability but is marked non-reusable unless it
// consumes the UAST execution graph directly.
func HarvestProjectionRenderers() ([]ProjectionRendererCatalogEntry, error) {
	entries := []ProjectionRendererCatalogEntry{}
	files := backendRendererSourceFiles()
	// Production/onefile execution must not depend on the checkout containing
	// Go source files. The generated UAST renderer bindings are the canonical
	// product registry; source harvesting remains an optional local audit.
	for _, filename := range files {
		if _, err := os.Stat(filename); err != nil {
			return generatedRendererCatalog(), nil
		}
	}
	for _, filename := range files {
		file, err := goParser.ParseFile(goToken.NewFileSet(), filename, nil, 0)
		if err != nil {
			return nil, err
		}
		base := filepath.Base(filename)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			name := rendererFunctionName(fn)
			lower := strings.ToLower(name)
			if !strings.Contains(lower, "stmt") && !strings.Contains(lower, "statement") && !strings.Contains(lower, "expr") && !strings.Contains(lower, "expression") && !strings.Contains(lower, "project") && !strings.Contains(lower, "target") && !strings.Contains(lower, "prelude") {
				continue
			}
			children, fields, relations, targetScope := harvestedRendererRequirements(fn.Body)
			reusable := (base == "uast_target_codegen.go" || base == "uast_r_writer.go" || base == "universal_target_projector.go") && strings.Contains(lower, "uast")
			returnKind := "source"
			if strings.Contains(lower, "project") {
				returnKind = "doc"
			}
			entries = append(entries, ProjectionRendererCatalogEntry{
				RendererID: "renderer." + strings.Trim(strings.ReplaceAll(strings.ReplaceAll(name, "(*", ""), ")", ""), "."),
				SourceFile: base, SourceFunction: name, SyntacticCategory: rendererSyntaxCategory(name),
				RequiredChildren: children, RequiredFields: fields, RequiredRelations: relations,
				TargetScope: targetScope, ReturnsDocOrSource: returnKind, Reusable: reusable,
			})
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].RendererID < entries[j].RendererID })
	return entries, nil
}

func generatedRendererCatalog() []ProjectionRendererCatalogEntry {
	entries := make([]ProjectionRendererCatalogEntry, 0, len(generatedProjectionRendererBindings))
	for _, binding := range generatedProjectionRendererBindings {
		entries = append(entries, ProjectionRendererCatalogEntry{
			RendererID: binding.RendererID, SourceFile: binding.SourceFile, SourceFunction: binding.Function,
			SyntacticCategory: "syntax_support", TargetScope: "shared", ReturnsDocOrSource: "doc", Reusable: binding.Reusable,
		})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].RendererID < entries[j].RendererID })
	return entries
}

func rendererArchetypeSignature(contract StructureProjectionContract, registry UASTStructureProjectionRegistry) string {
	fieldRoles, relationRoles, childRoles := []string{}, []string{}, []string{}
	for _, field := range contract.RequiredFields {
		if role := registry.FieldUse[field]; role != "" {
			fieldRoles = append(fieldRoles, role)
		}
		if targetProjectionSyntaxFields[field] {
			childRoles = append(childRoles, field)
		}
	}
	for _, relation := range contract.ChildRelations {
		if role := registry.RelationUse[relation]; role != "" {
			relationRoles = append(relationRoles, role)
		}
	}
	return strings.Join([]string{
		contract.SyntacticCategory, strings.Join(sortedUnique(childRoles), ";"), strings.Join(sortedUnique(fieldRoles), ";"),
		strings.Join(sortedUnique(relationRoles), ";"), contract.PrecedenceRole, contract.BlockPolicy, contract.TerminatorPolicy,
	}, "|")
}

func rendererCanSatisfyArchetype(entry ProjectionRendererCatalogEntry, contracts []StructureProjectionContract) bool {
	if !entry.Reusable || len(contracts) == 0 {
		return false
	}
	for _, contract := range contracts {
		// The fallback is a generated runtime contract, not an existing renderer
		// match. Keeping it out of the harvest preserves the residual matrix so
		// the one new primitive remains visible as the common fix.
		if contract.ProjectionForm == projectionFormFallback {
			return false
		}
		binding, ok := generatedProjectionRendererBinding(contract.ProjectionForm)
		if !ok || binding.RendererID == "" || binding.SourceFile != entry.SourceFile {
			return false
		}
	}
	return true
}

// UniversalProjectionRendererHarvest constructs the exact quotient of the
// remaining projection classes by renderer requirements.
func UniversalProjectionRendererHarvest() (ProjectionRendererHarvest, error) {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return ProjectionRendererHarvest{}, err
	}
	catalog, err := HarvestProjectionRenderers()
	if err != nil {
		return ProjectionRendererHarvest{}, err
	}
	byClass := map[string][]StructureProjectionContract{}
	for _, contract := range registry.Contracts {
		if !registry.ClassProjectable[contract.ProjectionClass] {
			byClass[contract.ProjectionClass] = append(byClass[contract.ProjectionClass], contract)
		}
	}
	signatures, assignments := map[string][]string{}, map[string]string{}
	for class, contracts := range byClass {
		parts := make([]string, 0, len(contracts))
		for _, contract := range contracts {
			parts = append(parts, rendererArchetypeSignature(contract, registry))
		}
		sort.Strings(parts)
		signatures[strings.Join(parts, "||")] = append(signatures[strings.Join(parts, "||")], class)
	}
	keys := make([]string, 0, len(signatures))
	for signature := range signatures {
		keys = append(keys, signature)
	}
	sort.Strings(keys)
	harvest := ProjectionRendererHarvest{Catalog: catalog, Assignments: assignments}
	for i, signature := range keys {
		id := fmt.Sprintf("ARCH_%03d", i+1)
		classes := sortedUnique(signatures[signature])
		allContracts := []StructureProjectionContract{}
		for _, class := range classes {
			allContracts = append(allContracts, byClass[class]...)
		}
		renderers := []string{}
		for _, entry := range catalog {
			compatible := rendererCanSatisfyArchetype(entry, allContracts)
			harvest.Matches = append(harvest.Matches, ProjectionRendererMatch{ArchetypeID: id, RendererID: entry.RendererID, Compatible: compatible})
			if compatible {
				renderers = append(renderers, entry.RendererID)
			}
		}
		for _, class := range classes {
			harvest.Assignments[class] = id
		}
		harvest.Archetypes = append(harvest.Archetypes, ProjectionRendererArchetype{ID: id, ProjectionClasses: classes, Signature: signature, ExistingRenderers: sortedUnique(renderers), Implemented: len(renderers) > 0})
	}
	return harvest, nil
}

func WriteProjectionRendererHarvest(dir string) (ProjectionRendererHarvest, error) {
	harvest, err := UniversalProjectionRendererHarvest()
	if err != nil {
		return ProjectionRendererHarvest{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ProjectionRendererHarvest{}, err
	}
	write := func(name string, header []string, rows [][]string) error {
		file, err := os.Create(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		writer := csv.NewWriter(file)
		if err := writer.Write(header); err != nil {
			file.Close()
			return err
		}
		for _, row := range rows {
			if err := writer.Write(row); err != nil {
				file.Close()
				return err
			}
		}
		writer.Flush()
		err = writer.Error()
		closeErr := file.Close()
		if err != nil {
			return err
		}
		return closeErr
	}
	catalogRows := [][]string{}
	for _, entry := range harvest.Catalog {
		catalogRows = append(catalogRows, []string{entry.RendererID, entry.SourceFile, entry.SourceFunction, entry.SyntacticCategory, strings.Join(entry.RequiredChildren, ";"), strings.Join(entry.RequiredFields, ";"), strings.Join(entry.RequiredRelations, ";"), entry.TargetScope, entry.ReturnsDocOrSource, fmt.Sprintf("%t", entry.Reusable)})
	}
	if err := write("projection_renderer_catalog.csv", []string{"renderer_id", "source_file", "source_function", "syntactic_category", "required_children", "required_fields", "required_relations", "target_scope", "returns_doc_or_source", "reusable"}, catalogRows); err != nil {
		return ProjectionRendererHarvest{}, err
	}
	archetypeRows := [][]string{}
	for _, archetype := range harvest.Archetypes {
		archetypeRows = append(archetypeRows, []string{archetype.ID, strings.Join(archetype.ProjectionClasses, ";"), archetype.Signature, strings.Join(archetype.ExistingRenderers, ";"), fmt.Sprintf("%t", archetype.Implemented)})
	}
	if err := write("renderer_equivalence_classes.csv", []string{"archetype_id", "projection_classes", "renderer_signature", "existing_renderers", "implemented"}, archetypeRows); err != nil {
		return ProjectionRendererHarvest{}, err
	}
	matchRows := make([][]string, 0, len(harvest.Matches))
	for _, match := range harvest.Matches {
		matchRows = append(matchRows, []string{match.ArchetypeID, match.RendererID, fmt.Sprintf("%t", match.Compatible)})
	}
	if err := write("projection_renderer_matrix.csv", []string{"archetype_id", "renderer_id", "compatible"}, matchRows); err != nil {
		return ProjectionRendererHarvest{}, err
	}
	return harvest, nil
}
