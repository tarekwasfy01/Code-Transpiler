package backend

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// StructureProjectionContract is a declarative, schema-derived syntax
// contract. It is not an IR and never stores program data: the projector reads
// program semantics only from UniversalASTDocument.
type StructureProjectionContract struct {
	StructureKind       string   `json:"structure_kind"`
	ProjectionClass     string   `json:"projection_class"`
	ProjectionForm      string   `json:"projection_form"`
	SyntacticCategory   string   `json:"syntactic_category"`
	ChildRelations      []string `json:"child_relations"`
	RequiredFields      []string `json:"required_fields"`
	ExecutionPrimitives []string `json:"execution_primitives"`
	PrecedenceRole      string   `json:"precedence_role"`
	BlockPolicy         string   `json:"block_policy"`
	TerminatorPolicy    string   `json:"terminator_policy"`
	EmissionPolicy      string   `json:"emission_policy"`
	Implemented         bool     `json:"implemented"`
}

const (
	projectionFormCore      = "uast.core.syntax"
	projectionFormAggregate = "uast.aggregate.runtime_list"
	projectionFormVariable  = "uast.declaration.mutable"
	projectionFormDeclGroup = "uast.declaration.group"
	projectionFormMetadata  = "uast.metadata.none"
	projectionFormAtomic    = "uast.atomic.direct"
	projectionFormStatement = "uast.statement.direct"
	projectionFormFallback  = "uast.fallback.runtime"
	projectionFormMissing   = "uast.unimplemented"
)

// UASTStructureProjectionRegistry is the exact quotient of the canonical
// structural matrix by syntax requirements. Contracts sharing a class have
// the same layers, child relations, fields and execution requirements.
type UASTStructureProjectionRegistry struct {
	Schema           string                        `json:"schema"`
	BasisSHA256      string                        `json:"basis_sha256"`
	Contracts        []StructureProjectionContract `json:"contracts"`
	Classes          map[string][]string           `json:"classes"`
	ClassProjectable map[string]bool               `json:"class_projectable"`
	FieldUse         map[string]string             `json:"field_use"`
	RelationUse      map[string]string             `json:"relation_use"`
}

// targetProjectionSyntaxFields are the channels actually read while forming
// target tokens.  Other UAST fields remain semantic, validation or
// requirement inputs and must never create a syntax gap just by existing.
var targetProjectionSyntaxFields = map[string]bool{
	"name": true, "operation": true, "value": true, "operands": true,
	"callee": true, "arguments": true, "parameters": true, "receiver": true,
	"body": true, "condition": true, "branches": true, "pattern": true,
	"members": true, "conversion": true, "dispatch": true, "candidates": true,
}

// genericProjectionStructures are structural forms for which the shared
// target emitter has one proved implementation. The key is a structure
// contract, not a facet or target-language exception. Every target prelude
// already exposes the `list` runtime operation used by this aggregate form.
var genericProjectionStructures = map[string]bool{
	"AggregateExpr":     true,
	"TupleExpr":         true,
	"TupleResult":       true,
	"ComprehensionExpr": true,
}

var genericMutableDeclarationStructures = map[string]bool{
	"VariableDecl": true,
}

var genericDeclarationGroupStructures = map[string]bool{
	"VariableDeclGroup": true,
}

// These schema structures are semantic contracts/facts.  Their fields and
// relations are consumed by validation, evidence, type and requirement
// matrices, but they have no independent target token.  Keeping this
// declarative matrix separate from operational structures prevents metadata
// from becoming a projection gap while still rejecting unregistered source
// operations.
var genericMetadataProjectionStructures = map[string]bool{
	"ABIContract":         true,
	"Annotation":          true,
	"BindingResolution":   true,
	"CaptureRelation":     true,
	"DispatchResolution":  true,
	"DispatchSemantics":   true,
	"Effect":              true,
	"ExecutionModel":      true,
	"IRFact":              true,
	"LayoutContract":      true,
	"LifetimeRegion":      true,
	"LoweringFact":        true,
	"MemoryModelContract": true,
	"MethodSet":           true,
	"OptimizationFact":    true,
	"OwnershipSemantics":  true,
	"SafetyRegion":        true,
	"TypeInferenceFact":   true,
	"TypeRelation":        true,
	"Visibility":          true,
}

// Atomic expression nodes are emitted entirely from their proved UAST value
// and target literal contract. They do not call the target runtime dispatcher.
var directAtomicProjectionStructures = map[string]bool{
	"LiteralExpr": true,
	"NilLiteral":  true,
	"SymbolRef":   true,
}

// These statement forms only emit target punctuation/keywords. Their child
// expressions retain their own matrix-derived mode, so a runtime expression
// cannot be mistaken for a direct statement primitive.
var directStatementProjectionStructures = map[string]bool{
	"Scope":             true,
	"AssignStmt":        true,
	"ReturnStmt":        true,
	"BreakStmt":         true,
	"ContinueStmt":      true,
	"VariableDecl":      true,
	"VariableDeclGroup": true,
}

func projectionNames(values []string, at func(int) float64) []string {
	out := []string{}
	for i, value := range values {
		if at(i) != 0 {
			out = append(out, value)
		}
	}
	return out
}

func projectionImplementedStructure(kind string) bool {
	if genericProjectionStructures[kind] || genericMutableDeclarationStructures[kind] || genericDeclarationGroupStructures[kind] || genericMetadataProjectionStructures[kind] {
		return true
	}
	if kind == "NilLiteral" {
		return true
	}
	for _, structural := range directSemanticStructure {
		if structural == kind {
			return true
		}
	}
	return false
}

func projectionFormForStructure(kind string, metadataOnly bool) string {
	if genericProjectionStructures[kind] {
		return projectionFormAggregate
	}
	if genericMutableDeclarationStructures[kind] {
		return projectionFormVariable
	}
	if genericDeclarationGroupStructures[kind] {
		return projectionFormStatement
	}
	if directStatementProjectionStructures[kind] {
		return projectionFormStatement
	}
	if directAtomicProjectionStructures[kind] {
		return projectionFormAtomic
	}
	if genericMetadataProjectionStructures[kind] || metadataOnly {
		return projectionFormMetadata
	}
	if projectionImplementedStructure(kind) {
		return projectionFormCore
	}
	// Every canonical structure remains representable.  Unknown operational
	// forms use one shared runtime-preserving syntax contract; the dispatcher
	// reports the unsupported semantic operation explicitly at execution time.
	return projectionFormFallback
}

// metadataOnlyProjection is deliberately stricter than the old
// "no syntax.child" rule.  A graph fact may have no syntactic child but still
// require a runtime, control-flow, or target-projection primitive (for
// example AtomicOp or ForeignDeclCall).  Such a node must be rejected until a
// real projection is registered; treating it as metadata would make the
// direct generator silently omit semantics.
func metadataOnlyProjection(layers, fields, relations, primitives []string, fieldUse, relationUse map[string]string) bool {
	for _, field := range fields {
		if structureSyntaxFieldRequired(field, layers, fieldUse) {
			return false
		}
	}
	for _, relation := range relations {
		if structureSyntaxRelationRequired(relation, layers, relationUse) {
			return false
		}
	}
	if len(primitives) == 0 {
		return false
	}
	// Type-only structures are descriptions of a type contract. Their global
	// execution requirements can mention runtime-consumed primitives (for
	// example data/lifetime), but those requirements describe representation,
	// not an independent source token. The layer matrix proves that no
	// executable syntax node is being hidden here.
	if standaloneTypeValueLayers(layers) {
		return true
	}
	for _, primitive := range primitives {
		for _, consumer := range executionPrimitiveConsumers(UASTExecutionPrimitive(primitive)) {
			switch consumer {
			case UASTValidationConsumed, UASTRequirementConsumed, UASTTypeSystemConsumed, UASTCompileTimeConsumed:
				// These consumers validate or describe the surrounding syntax;
				// they do not execute an independent target-language construct.
			default:
				return false
			}
		}
	}
	return true
}

// structureSyntaxFieldRequired is the structure-local projection of the
// field-role matrix. A field name alone does not prove target syntax: the same
// operands, members or conversion channel is also used by type descriptions.
// Pure type.value families therefore keep those channels semantic/validation
// data and must not create a target syntax gap merely because they are set.
func structureSyntaxFieldRequired(field string, layers []string, fieldUse map[string]string) bool {
	if standaloneTypeValueLayers(layers) {
		return false
	}
	return fieldUse[field] == "SYNTAX_REQUIRED" || fieldUse[field] == "TARGET_DEPENDENT"
}

// structureSyntaxRelationRequired applies the same structure-local rule to
// relations.  Data/type relations can be globally consumed by the runtime,
// yet remain purely descriptive on a type.value node.  Only a structure that
// proves a source-emitting layer may turn such a relation into a syntax
// obligation.
func structureSyntaxRelationRequired(relation string, layers []string, relationUse map[string]string) bool {
	if standaloneTypeValueLayers(layers) {
		return false
	}
	return relationUse[relation] == "SYNTAX_STRUCTURAL"
}

// standaloneTypeValueLayers identifies the schema-level type-description
// family. These nodes have no declaration, expression, control, binding or
// concurrency layer; all target renderers use their information through the
// universal type contract.  They therefore have an exact NO_SYNTAX contract.
func standaloneTypeValueLayers(layers []string) bool {
	if len(layers) == 0 {
		return false
	}
	hasType := false
	for _, layer := range layers {
		switch layer {
		case "type.value":
			hasType = true
		case "data.flow", "effect", "evaluation", "memory.lifetime":
			// These are semantic consumers of a type description, not a syntax
			// role by themselves.
		default:
			return false
		}
	}
	return hasType
}

func projectionFieldUse(field string, execution UASTExecutionAnalysis) string {
	if targetProjectionSyntaxFields[field] {
		return "SYNTAX_REQUIRED"
	}
	row := indexOf(execution.Fields, field)
	if row < 0 {
		return "SEMANTIC_ONLY"
	}
	required := executionRequirementNames(execution.MDE, row, DefaultUASTExecutionRegistry())
	if len(required) == 0 {
		return "SEMANTIC_ONLY"
	}
	if len(required) == 1 && (required[0] == string(execValidation) || required[0] == string(execMetadata)) {
		return "VALIDATION_ONLY"
	}
	requirementOnly := true
	for _, primitive := range required {
		for _, consumer := range executionPrimitiveConsumers(UASTExecutionPrimitive(primitive)) {
			if consumer != UASTRequirementConsumed && consumer != UASTValidationConsumed {
				requirementOnly = false
			}
		}
	}
	if requirementOnly {
		return "REQUIREMENT_ONLY"
	}
	for _, primitive := range required {
		if primitive == string(execLowering) || primitive == string(execRuntime) {
			return "TARGET_DEPENDENT"
		}
	}
	return "SEMANTIC_ONLY"
}

func projectionRelationUse(relation string, execution UASTExecutionAnalysis) string {
	if relation == "syntax.child" {
		return "SYNTAX_STRUCTURAL"
	}
	row := indexOf(execution.Relations, relation)
	if row >= 0 {
		required := executionRequirementNames(execution.MRE, row, DefaultUASTExecutionRegistry())
		for _, primitive := range required {
			switch UASTExecutionPrimitive(primitive) {
			case execEvaluation:
				return "EVALUATION_ORDER"
			case execBinding, execCapture:
				return "BINDING"
			case execControl, execEffects:
				return "CONTROL"
			case execTypes, execConversion:
				return "TYPE"
			case execLifetime, execMemory:
				return "OWNERSHIP_LIFETIME"
			}
		}
	}
	if directlyConsumedUASTRelations[relation] {
		return "SYNTAX_STRUCTURAL"
	}
	return "METADATA_ONLY"
}

func structureProjectionSignature(layers, relations, fields, primitives []string) string {
	return strings.Join([]string{strings.Join(layers, ";"), strings.Join(relations, ";"), strings.Join(fields, ";"), strings.Join(primitives, ";")}, "|")
}

// UniversalStructureProjectionRegistry derives all contracts from the UAST
// schema matrices. No UASF IDs, language IDs or target-specific rankings are
// used here.
var structureProjectionRegistryOnce struct {
	sync.Once
	registry UASTStructureProjectionRegistry
	err      error
}

// UniversalStructureProjectionRegistry returns the immutable schema quotient
// cached for this process. The registry is recomputed only across processes
// from the embedded matrices; callers must treat the returned data as
// read-only.
func UniversalStructureProjectionRegistry() (UASTStructureProjectionRegistry, error) {
	structureProjectionRegistryOnce.Do(func() {
		structureProjectionRegistryOnce.registry, structureProjectionRegistryOnce.err = buildUniversalStructureProjectionRegistry()
	})
	return structureProjectionRegistryOnce.registry, structureProjectionRegistryOnce.err
}

func buildUniversalStructureProjectionRegistry() (UASTStructureProjectionRegistry, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	execution, err := UniversalExecutionAnalysis()
	if err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	b := uastEmbedded.Basis
	registry := UASTStructureProjectionRegistry{
		Schema: "code-transpiler.uast-structure-projection.v1", BasisSHA256: uastEmbedded.BasisSHA256,
		Contracts: []StructureProjectionContract{}, Classes: map[string][]string{}, ClassProjectable: map[string]bool{}, FieldUse: map[string]string{}, RelationUse: map[string]string{},
	}
	for _, field := range b.Fields {
		registry.FieldUse[field] = projectionFieldUse(field, execution)
	}
	for _, relation := range b.ConcreteRelations {
		registry.RelationUse[relation] = projectionRelationUse(relation, execution)
	}
	signatureClass := map[string]string{}
	for row, structure := range b.StructuralKinds {
		layers := projectionNames(b.Layers, func(col int) float64 { return b.StructuralLayer.At(row, col) })
		relations := projectionNames(b.ConcreteRelations, func(col int) float64 { return b.StructuralConcreteRelation.At(row, col) })
		fields := projectionNames(b.Fields, func(col int) float64 { return b.StructuralField.At(row, col) })
		primitives := executionRequirementNames(execution.MSE, row, DefaultUASTExecutionRegistry())
		signature := structureProjectionSignature(layers, relations, fields, primitives)
		class, ok := signatureClass[signature]
		if !ok {
			class = fmt.Sprintf("PROJ_%03d", len(signatureClass)+1)
			signatureClass[signature] = class
		}
		block := "NONE"
		for _, field := range fields {
			if field == "body" || field == "branches" {
				block = "CHILD_BLOCK"
				break
			}
		}
		precedence := "TARGET_SPEC"
		for _, layer := range layers {
			if layer == "structure.expression" {
				precedence = "EXPRESSION"
				break
			}
		}
		metadataOnly := metadataOnlyProjection(layers, fields, relations, primitives, registry.FieldUse, registry.RelationUse) || genericMetadataProjectionStructures[structure]
		emission := "SYNTAX"
		if metadataOnly {
			emission = "METADATA_ONLY"
		}
		form := projectionFormForStructure(structure, metadataOnly)
		registry.Contracts = append(registry.Contracts, StructureProjectionContract{
			StructureKind: structure, ProjectionClass: class, SyntacticCategory: strings.Join(layers, "+"), ChildRelations: relations,
			RequiredFields: fields, ExecutionPrimitives: primitives, PrecedenceRole: precedence, BlockPolicy: block,
			ProjectionForm: form, TerminatorPolicy: "TARGET_SPEC", EmissionPolicy: emission, Implemented: form != projectionFormMissing,
		})
		registry.Classes[class] = append(registry.Classes[class], structure)
	}
	for class := range registry.Classes {
		sort.Strings(registry.Classes[class])
		projectable := true
		for _, structure := range registry.Classes[class] {
			if !projectionImplementedStructure(structure) {
				projectable = false
				break
			}
		}
		registry.ClassProjectable[class] = projectable
	}
	return registry, nil
}

// UASTTargetStructureProjectionCapabilities is the exact target-side
// availability of every structure contract. It is derived from declarative
// TargetSpec forms and the generated template quotient, never from a guess
// based on a target-language name. It remains structure-granular because an
// execution-equivalent projection class can contain multiple syntax forms.
func UASTTargetStructureProjectionCapabilities() (map[string]map[string]PreservationMode, error) {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return nil, err
	}
	// The target syntax quotient is the productive contract boundary.  A
	// TargetSpec form alone only says that the target names a family; the
	// quotient additionally proves that the exact class has a checked syntax
	// template and all required target parameters.  This cannot upgrade a
	// missing class to support, but it prevents the preservation matrix from
	// bypassing the generated template registry.
	templates, err := UniversalTargetSyntaxTemplateAnalysis()
	if err != nil {
		return nil, err
	}
	paths := DefaultPreservationRegistry()
	out := map[string]map[string]PreservationMode{}
	for _, target := range Backends() {
		spec, ok := targetSpec(target.ID)
		if !ok {
			return nil, fmt.Errorf("missing target specification for %q", target.ID)
		}
		out[target.ID] = map[string]PreservationMode{}
		for _, contract := range registry.Contracts {
			mode := PreservationError
			if !templates.SupportsContract(target.ID, contract, spec) {
				out[target.ID][contract.StructureKind] = PreservationError
				continue
			}
			candidate, supported := spec.ProjectionForms[contract.ProjectionForm]
			if !supported {
				out[target.ID][contract.StructureKind] = PreservationError
				continue
			}
			if mode == PreservationError || projectionModeRank(candidate.Mode) > projectionModeRank(mode) {
				mode = candidate.Mode
			}
			// A generated direct-lowering contract is the only path that may
			// replace the installed runtime semantic core. Syntax completeness
			// alone remains insufficient. Contracts are proof-gated and read
			// here at the productive capability boundary.
			if _, direct := DirectLoweringContractFor(target.ID, contract.ProjectionClass); direct {
				mode = PreservationDirect
			}
			if mode != PreservationError && mode == PreservationRuntime {
				if _, ok := paths.Solve(target.ID, "uast.core"); !ok {
					mode = PreservationError
				}
			}
			out[target.ID][contract.StructureKind] = mode
		}
	}
	return out, nil
}

// UASTTargetProjectionCapabilities folds the exact structure decisions back
// to the historical 13×73 class matrix. A class is only marked supported when
// all members are supported; the per-structure matrix above is used for
// actual executable capability decisions and never loses a proved member.
func UASTTargetProjectionCapabilities() (map[string]map[string]PreservationMode, error) {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return nil, err
	}
	structures, err := UASTTargetStructureProjectionCapabilities()
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]PreservationMode{}
	for _, target := range Backends() {
		out[target.ID] = map[string]PreservationMode{}
		for class, members := range registry.Classes {
			mode := PreservationDirect
			for _, structure := range members {
				candidate := structures[target.ID][structure]
				if candidate == PreservationError {
					mode = PreservationError
					break
				}
				if projectionModeRank(candidate) > projectionModeRank(mode) {
					mode = candidate
				}
			}
			out[target.ID][class] = mode
		}
	}
	return out, nil
}

func structureProjectionContract(registry UASTStructureProjectionRegistry, structure string) (StructureProjectionContract, bool) {
	for _, contract := range registry.Contracts {
		if contract.StructureKind == structure {
			return contract, true
		}
	}
	return StructureProjectionContract{}, false
}

func projectionModeRank(mode PreservationMode) int {
	switch mode {
	case PreservationDirect:
		return 0
	case PreservationRewrite:
		return 1
	case PreservationHelper:
		return 2
	case PreservationEmulate:
		return 3
	case PreservationRuntime:
		return 4
	default:
		return 5
	}
}

// validateUASTStructureProjectionContracts is the projector's shared guard.
// It makes the generated structure contract productive: a canonical node is
// either handled by a generic syntax class already present in the emitter or
// is rejected as a concrete projection gap. No semantic node is discarded.
func validateUASTStructureProjectionContracts(u *UniversalASTDocument) error {
	if u == nil {
		return fmt.Errorf("missing universal AST")
	}
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return err
	}
	byKind := map[string]StructureProjectionContract{}
	for _, contract := range registry.Contracts {
		byKind[contract.StructureKind] = contract
	}
	for _, node := range u.Nodes {
		contract, ok := byKind[node.StructuralKind]
		if !ok {
			return fmt.Errorf("PROJECTION_GAP: structural kind %q has no schema-derived projection contract", node.StructuralKind)
		}
		if !contract.Implemented {
			return fmt.Errorf("PROJECTION_GAP: structural kind %q requires projection class %s", node.StructuralKind, contract.ProjectionClass)
		}
		binding, bound := generatedProjectionRendererBinding(contract.ProjectionForm)
		if !bound || !binding.Reusable {
			return fmt.Errorf("PROJECTION_GAP: structural kind %q has no direct UAST renderer binding for form %s", node.StructuralKind, contract.ProjectionForm)
		}
	}
	return nil
}

// validateUASTTargetSyntaxTemplates makes the generated TargetSpec/template
// contract a productive projector guard. It reads only canonical UAST node
// kinds and schema-derived contracts; it never creates a compatibility AST or
// infers a target lowering from a language name.
func validateUASTTargetSyntaxTemplates(u *UniversalASTDocument, spec TargetSpec) error {
	if u == nil {
		return fmt.Errorf("missing universal AST")
	}
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return err
	}
	templates, err := UniversalTargetSyntaxTemplateAnalysis()
	if err != nil {
		return err
	}
	byKind := map[string]StructureProjectionContract{}
	for _, contract := range registry.Contracts {
		byKind[contract.StructureKind] = contract
	}
	for _, node := range u.Nodes {
		contract, ok := byKind[node.StructuralKind]
		if !ok {
			return fmt.Errorf("TARGET_SYNTAX_MISSING_TARGET_TEMPLATE: structural kind %q has no projection contract", node.StructuralKind)
		}
		if !templates.SupportsContract(spec.ID, contract, spec) {
			return fmt.Errorf("TARGET_SYNTAX_MISSING_TARGET_TEMPLATE: structure %q requires projection class %s for target %s", node.StructuralKind, contract.ProjectionClass, spec.ID)
		}
	}
	return nil
}

func isMetadataOnlyProjectionStructure(kind string) bool {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return false
	}
	for _, contract := range registry.Contracts {
		if contract.StructureKind == kind {
			return contract.EmissionPolicy == "METADATA_ONLY"
		}
	}
	return false
}

// WriteUASTStructureProjectionRegistry produces the machine-readable
// projection quotient used to identify a shared projector gap before any
// target-specific code is written.
func WriteUASTStructureProjectionRegistry(dir string) (UASTStructureProjectionRegistry, error) {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	path := filepath.Join(dir, "structure_projection_contracts.csv")
	f, err := os.Create(path)
	if err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	w := csv.NewWriter(f)
	if err := w.Write([]string{"structure_id", "projection_class", "projection_form", "syntactic_category", "child_relations", "required_fields", "execution_primitives", "precedence_role", "block_policy", "terminator_policy", "emission_policy", "implemented"}); err != nil {
		f.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	for _, c := range registry.Contracts {
		if err := w.Write([]string{c.StructureKind, c.ProjectionClass, c.ProjectionForm, c.SyntacticCategory, strings.Join(c.ChildRelations, ";"), strings.Join(c.RequiredFields, ";"), strings.Join(c.ExecutionPrimitives, ";"), c.PrecedenceRole, c.BlockPolicy, c.TerminatorPolicy, c.EmissionPolicy, fmt.Sprintf("%t", c.Implemented)}); err != nil {
			f.Close()
			return UASTStructureProjectionRegistry{}, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	if err := f.Close(); err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	capabilities, err := UASTTargetProjectionCapabilities()
	if err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	structureCapabilities, err := UASTTargetStructureProjectionCapabilities()
	if err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	structureMatrix, err := os.Create(filepath.Join(dir, "structure_projection_matrix.csv"))
	if err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	structureWriter := csv.NewWriter(structureMatrix)
	if err := structureWriter.Write([]string{"structure_id", "target", "projection_class", "projectable", "preservation", "class_preservation"}); err != nil {
		structureMatrix.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	for _, contract := range registry.Contracts {
		for _, target := range Backends() {
			mode := structureCapabilities[target.ID][contract.StructureKind]
			classMode := capabilities[target.ID][contract.ProjectionClass]
			if err := structureWriter.Write([]string{contract.StructureKind, target.ID, contract.ProjectionClass, fmt.Sprintf("%t", contract.Implemented), string(mode), string(classMode)}); err != nil {
				structureMatrix.Close()
				return UASTStructureProjectionRegistry{}, err
			}
		}
	}
	structureWriter.Flush()
	if err := structureWriter.Error(); err != nil {
		structureMatrix.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	if err := structureMatrix.Close(); err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	roles, err := os.Create(filepath.Join(dir, "field_projection_roles.csv"))
	if err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	rw := csv.NewWriter(roles)
	if err := rw.Write([]string{"field", "projection_role"}); err != nil {
		roles.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	fields := make([]string, 0, len(registry.FieldUse))
	for field := range registry.FieldUse {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	for _, field := range fields {
		if err := rw.Write([]string{field, registry.FieldUse[field]}); err != nil {
			roles.Close()
			return UASTStructureProjectionRegistry{}, err
		}
	}
	rw.Flush()
	if err := rw.Error(); err != nil {
		roles.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	if err := roles.Close(); err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	relationRoles, err := os.Create(filepath.Join(dir, "relation_projection_roles.csv"))
	if err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	relw := csv.NewWriter(relationRoles)
	if err := relw.Write([]string{"relation", "projection_role"}); err != nil {
		relationRoles.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	relations := make([]string, 0, len(registry.RelationUse))
	for relation := range registry.RelationUse {
		relations = append(relations, relation)
	}
	sort.Strings(relations)
	for _, relation := range relations {
		if err := relw.Write([]string{relation, registry.RelationUse[relation]}); err != nil {
			relationRoles.Close()
			return UASTStructureProjectionRegistry{}, err
		}
	}
	relw.Flush()
	if err := relw.Error(); err != nil {
		relationRoles.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	if err := relationRoles.Close(); err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	classes, err := os.Create(filepath.Join(dir, "projection_equivalence_classes.csv"))
	if err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	cw := csv.NewWriter(classes)
	if err := cw.Write([]string{"projection_class", "structure_members", "projectable"}); err != nil {
		classes.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	keys := make([]string, 0, len(registry.Classes))
	for key := range registry.Classes {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := cw.Write([]string{key, strings.Join(registry.Classes[key], ";"), fmt.Sprintf("%t", registry.ClassProjectable[key])}); err != nil {
			classes.Close()
			return UASTStructureProjectionRegistry{}, err
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		classes.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	if err := classes.Close(); err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	capabilityFile, err := os.Create(filepath.Join(dir, "target_projection_capabilities.csv"))
	if err != nil {
		return UASTStructureProjectionRegistry{}, err
	}
	capabilityWriter := csv.NewWriter(capabilityFile)
	if err := capabilityWriter.Write([]string{"target", "projection_class", "preservation"}); err != nil {
		capabilityFile.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	for _, target := range Backends() {
		for _, key := range keys {
			if err := capabilityWriter.Write([]string{target.ID, key, string(capabilities[target.ID][key])}); err != nil {
				capabilityFile.Close()
				return UASTStructureProjectionRegistry{}, err
			}
		}
	}
	capabilityWriter.Flush()
	if err := capabilityWriter.Error(); err != nil {
		capabilityFile.Close()
		return UASTStructureProjectionRegistry{}, err
	}
	return registry, capabilityFile.Close()
}
