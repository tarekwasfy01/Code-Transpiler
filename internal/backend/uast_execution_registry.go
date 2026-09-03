package backend

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

var universalExecutionAnalysisOnce sync.Once
var universalExecutionAnalysisCache UASTExecutionAnalysis
var universalExecutionAnalysisErr error

// UASTExecutionPrimitive is a behavior already consumed by the direct UAST
// runtime, validator, or target projector. It is not another semantic IR: the
// UAST graph remains the sole input and the registry only describes which
// existing execution paths a graph requires.
type UASTExecutionPrimitive string

const (
	execABI              UASTExecutionPrimitive = "abi"
	execAnnotation       UASTExecutionPrimitive = "annotation"
	execBinding          UASTExecutionPrimitive = "binding"
	execCall             UASTExecutionPrimitive = "call"
	execCapture          UASTExecutionPrimitive = "capture"
	execCompileTime      UASTExecutionPrimitive = "compiletime"
	execConcurrency      UASTExecutionPrimitive = "concurrency"
	execControl          UASTExecutionPrimitive = "control"
	execConversion       UASTExecutionPrimitive = "conversion"
	execData             UASTExecutionPrimitive = "data"
	execDeclaration      UASTExecutionPrimitive = "declaration"
	execDialect          UASTExecutionPrimitive = "dialect"
	execEffects          UASTExecutionPrimitive = "effects"
	execEvaluation       UASTExecutionPrimitive = "evaluation"
	execException        UASTExecutionPrimitive = "exception"
	execExpression       UASTExecutionPrimitive = "expression"
	execLanguageContract UASTExecutionPrimitive = "language_contract"
	execLifetime         UASTExecutionPrimitive = "lifetime"
	execLowering         UASTExecutionPrimitive = "lowering"
	execMemory           UASTExecutionPrimitive = "memory"
	execMetadata         UASTExecutionPrimitive = "metadata"
	execModule           UASTExecutionPrimitive = "module"
	execPreprocessor     UASTExecutionPrimitive = "preprocessor"
	execRuntime          UASTExecutionPrimitive = "runtime"
	execSyntax           UASTExecutionPrimitive = "syntax"
	execTemplate         UASTExecutionPrimitive = "template"
	execTypes            UASTExecutionPrimitive = "types"
	execValidation       UASTExecutionPrimitive = "validation"
)

type UASTExecutionPrimitiveSpec struct {
	ID          UASTExecutionPrimitive `json:"id"`
	Implemented bool                   `json:"implemented"`
	Handler     string                 `json:"handler,omitempty"`
	Consumers   []UASTConsumerKind     `json:"consumers,omitempty"`
}

// UASTConsumerKind identifies the productive direct-UAST consumer that owns
// a contract.  It classifies consumption without creating a second IR.
type UASTConsumerKind string

const (
	UASTRuntimeConsumed          UASTConsumerKind = "RUNTIME_CONSUMED"
	UASTValidationConsumed       UASTConsumerKind = "VALIDATION_CONSUMED"
	UASTTypeSystemConsumed       UASTConsumerKind = "TYPE_SYSTEM_CONSUMED"
	UASTControlFlowConsumed      UASTConsumerKind = "CONTROL_FLOW_CONSUMED"
	UASTRequirementConsumed      UASTConsumerKind = "REQUIREMENT_CONSUMED"
	UASTTargetProjectionConsumed UASTConsumerKind = "TARGET_PROJECTION_CONSUMED"
	UASTCompileTimeConsumed      UASTConsumerKind = "COMPILETIME_CONSUMED"
)

// UASTExecutionRegistry maps canonical schema axes to execution primitives.
// All rows are boolean and are derived from the checked-in axis labels and the
// actual direct UAST entry points named in Handler.
type UASTExecutionRegistry struct {
	Primitives      []UASTExecutionPrimitiveSpec
	SemanticAxis    map[string][]UASTExecutionPrimitive
	RelationAxis    map[string][]UASTExecutionPrimitive
	StructuralLayer map[string][]UASTExecutionPrimitive
	Relation        map[string][]UASTExecutionPrimitive
	Field           map[string][]UASTExecutionPrimitive
}

type UASTExecutionClass struct {
	ID                 string   `json:"id"`
	UASFMembers        []string `json:"uasf_members"`
	ExecutionSignature string   `json:"execution_signature"`
	RequiredPrimitives []string `json:"required_primitives"`
}

// UASTExecutionAnalysis contains the four required boolean matrices and the
// exact UASF quotient. Executable is one only when each primitive in the UASF
// row has a real product handler.
type UASTExecutionAnalysis struct {
	Schema                    string                        `json:"schema"`
	BasisSHA256               string                        `json:"basis_sha256"`
	Primitives                []UASTExecutionPrimitiveSpec  `json:"primitives"`
	Capabilities              []string                      `json:"capabilities"`
	Structures                []string                      `json:"structures"`
	Relations                 []string                      `json:"relations"`
	Fields                    []string                      `json:"fields"`
	MCE                       matrixir.SparseMatrix         `json:"m_ce"`
	MSE                       matrixir.SparseMatrix         `json:"m_se"`
	MRE                       matrixir.SparseMatrix         `json:"m_re"`
	MDE                       matrixir.SparseMatrix         `json:"m_de"`
	Implemented               matrixir.Vector               `json:"exec_primitive_implemented"`
	Executable                matrixir.Vector               `json:"executable_uasf"`
	ExecutableStructures      matrixir.Vector               `json:"executable_structures"`
	ExecutableRelations       matrixir.Vector               `json:"executable_relations"`
	ExecutableFields          matrixir.Vector               `json:"executable_fields"`
	ProductivelyConsumed      matrixir.Vector               `json:"productively_consumed_uasf"`
	ConsumerCoverage          map[string][]UASTConsumerKind `json:"consumer_coverage"`
	Missing                   matrixir.SparseMatrix         `json:"missing_primitives"`
	GlobalMissing             map[string]int                `json:"global_missing_primitives"`
	EquivalenceClasses        []UASTExecutionClass          `json:"execution_equivalence_classes"`
	ProductiveLegacyRelations int                           `json:"productive_legacy_semantic_dependencies"`
}

func DefaultUASTExecutionRegistry() UASTExecutionRegistry {
	implemented := executionPrimitiveHandlers()
	ids := []UASTExecutionPrimitive{execABI, execAnnotation, execBinding, execCall, execCapture, execCompileTime, execConcurrency, execControl, execConversion, execData, execDeclaration, execDialect, execEffects, execEvaluation, execException, execExpression, execLanguageContract, execLifetime, execLowering, execMemory, execMetadata, execModule, execPreprocessor, execRuntime, execSyntax, execTemplate, execTypes, execValidation}
	primitives := make([]UASTExecutionPrimitiveSpec, 0, len(ids))
	for _, id := range ids {
		handler, ok := implemented[id]
		name := ""
		if ok {
			name = handler.name
		}
		primitives = append(primitives, UASTExecutionPrimitiveSpec{ID: id, Implemented: ok && handler.validate != nil, Handler: name, Consumers: executionPrimitiveConsumers(id)})
	}
	return UASTExecutionRegistry{
		Primitives: primitives,
		SemanticAxis: map[string][]UASTExecutionPrimitive{
			"abi.ffi": {execABI}, "abi.layout": {execABI}, "binding.graph": {execBinding}, "capture.shadowing": {execCapture},
			"compiletime.evaluation": {execCompileTime}, "compiletime.semantics": {execCompileTime}, "concurrency.model": {execConcurrency},
			"control.flow": {execControl}, "conversion.semantics": {execConversion}, "coroutine.state_machine": {execConcurrency},
			"data.flow": {execData}, "dialect.extension": {execDialect}, "effect.graph": {execEffects}, "effects.control": {execControl},
			"evaluation.order": {execEvaluation}, "exception.unwind": {execException}, "initialization.order": {execDeclaration},
			"language.implementation_defined": {execLanguageContract}, "language.undefined_behavior": {execLanguageContract}, "language.unspecified_behavior": {execLanguageContract},
			"linkage.odr": {execABI}, "lowering.pipeline": {execLowering}, "memory.model": {execMemory}, "memory.ordering": {execMemory},
			"module.semantics": {execModule}, "name.resolution": {execBinding}, "nodes.declaration": {execDeclaration}, "nodes.expression": {execExpression},
			"object.lifetime": {execLifetime}, "operations": {execExpression}, "overload.resolution": {execCall}, "ownership.lifetime": {execLifetime},
			"pointer.provenance": {execMemory}, "preprocessor.semantics": {execPreprocessor}, "purity": {execEffects}, "runtime.contract": {execRuntime},
			"scope.binding": {execBinding}, "storage.duration": {execMemory}, "synchronization": {execConcurrency}, "template.instantiation": {execTemplate},
			"types.origin": {execTypes}, "types.structure": {execTypes}, "validation.contract": {execValidation}, "value.category": {execTypes},
		},
		RelationAxis: map[string][]UASTExecutionPrimitive{
			"abi.graph": {execABI}, "annotation.graph": {execAnnotation}, "binding.graph": {execBinding}, "call.graph": {execCall}, "capture.graph": {execCapture},
			"compiletime.graph": {execCompileTime}, "concurrency.graph": {execConcurrency}, "constraint.graph": {execTypes}, "contract.graph": {execValidation},
			"control.flow": {execControl}, "data.flow": {execData}, "dialect.extension": {execDialect}, "dispatch.graph": {execCall}, "effect.graph": {execEffects},
			"layout.graph": {execABI}, "lifetime.graph": {execLifetime}, "lowering.graph": {execLowering}, "memory.graph": {execMemory},
			"ownership.graph": {execLifetime}, "runtime.graph": {execRuntime}, "scope.binding": {execBinding}, "scope.graph": {execBinding}, "types.graph": {execTypes},
		},
		StructuralLayer: map[string][]UASTExecutionPrimitive{
			"structure.declaration": {execDeclaration}, "structure.expression": {execExpression}, "type.value": {execTypes}, "binding.scope": {execBinding},
			"control.flow": {execControl}, "data.flow": {execData}, "effect": {execEffects}, "evaluation": {execEvaluation},
			"exception": {execException}, "memory.lifetime": {execLifetime}, "concurrency": {execConcurrency}, "compiletime.meta": {execCompileTime},
			"module.dispatch": {execModule}, "abi.layout": {execABI}, "lowering.runtime.validation": {execLowering, execRuntime, execValidation},
			"language.contract": {execLanguageContract}, "dialect.extension": {execDialect},
		},
		Relation: executionRelationPrimitives(),
		Field:    executionFieldPrimitives(),
	}
}

func executionPrimitiveConsumers(id UASTExecutionPrimitive) []UASTConsumerKind {
	switch id {
	case execControl:
		return []UASTConsumerKind{UASTRuntimeConsumed, UASTControlFlowConsumed}
	case execTypes:
		return []UASTConsumerKind{UASTTypeSystemConsumed, UASTValidationConsumed}
	case execCompileTime:
		return []UASTConsumerKind{UASTCompileTimeConsumed, UASTValidationConsumed}
	case execABI, execAnnotation, execCapture, execDialect, execLanguageContract, execLifetime, execMemory, execMetadata, execModule, execPreprocessor, execTemplate, execValidation:
		return []UASTConsumerKind{UASTValidationConsumed, UASTRequirementConsumed}
	case execLowering:
		return []UASTConsumerKind{UASTTargetProjectionConsumed, UASTRequirementConsumed}
	case execRuntime:
		return []UASTConsumerKind{UASTRuntimeConsumed, UASTRequirementConsumed}
	default:
		return []UASTConsumerKind{UASTRuntimeConsumed, UASTValidationConsumed}
	}
}

func executionRelationPrimitives() map[string][]UASTExecutionPrimitive {
	return map[string][]UASTExecutionPrimitive{
		"syntax.child": {execSyntax}, "control.next": {execControl}, "control.true": {execControl}, "control.false": {execControl}, "control.loop_back": {execControl},
		"call.calls": {execCall}, "dispatch.resolves": {execCall}, "overload.candidate": {execCall},
		"binding.declares": {execBinding}, "binding.refers": {execBinding}, "binding.shadows": {execBinding}, "name.resolves": {execBinding}, "scope.parent": {execBinding},
		"data.def_use": {execData}, "data.operand": {execData}, "data.result": {execData}, "evaluation.before": {execEvaluation}, "operation.kind": {execExpression},
		"type.has": {execTypes}, "type.origin": {execTypes}, "type.constraint": {execTypes}, "type.parameter": {execTypes}, "type.convert": {execConversion}, "conversion.converts": {execConversion},
		"effect.has": {execEffects}, "exception.unwinds_to": {execException}, "coroutine.suspends": {execConcurrency}, "concurrency.atomic_order": {execConcurrency}, "concurrency.communicates": {execConcurrency}, "concurrency.spawns": {execConcurrency}, "concurrency.synchronizes": {execConcurrency},
		"capture.captures": {execCapture}, "memory.aliases": {execMemory}, "memory.borrows": {execMemory}, "memory.owns": {execLifetime}, "pointer.provenance": {execMemory}, "lifetime.outlives": {execLifetime}, "storage.resides_in": {execMemory},
		"module.exports": {execModule}, "module.imports": {execModule}, "compiletime.depends": {execCompileTime}, "preprocessor.expands": {execPreprocessor}, "template.instantiates": {execTemplate},
		"abi.calls": {execABI}, "layout.field": {execABI}, "linkage.links": {execABI}, "runtime.requires": {execRuntime}, "lowering.requires": {execLowering}, "validation.proves": {execValidation}, "contract.requires": {execValidation}, "language.contract": {execLanguageContract}, "dialect.requires": {execDialect}, "annotation.applies": {execAnnotation}, "value.category": {execTypes}, "initialization.before": {execDeclaration},
	}
}

func executionFieldPrimitives() map[string][]UASTExecutionPrimitive {
	return map[string][]UASTExecutionPrimitive{
		"id": {execMetadata}, "kind": {execMetadata}, "source_span": {execMetadata}, "semantic_facets": {execValidation}, "attributes": {execMetadata}, "extensions": {execMetadata},
		"scope_id": {execBinding}, "binding_refs": {execBinding}, "name": {execBinding}, "symbol": {execBinding},
		"type_ref": {execTypes}, "type_origin": {execTypes}, "type_shape": {execTypes}, "value_category": {execTypes}, "constraints": {execTypes},
		"operation": {execExpression}, "value": {execExpression}, "operands": {execData}, "results": {execData}, "callee": {execCall}, "arguments": {execCall}, "parameters": {execCall}, "receiver": {execCall},
		"body": {execControl}, "condition": {execControl}, "branches": {execControl}, "pattern": {execControl}, "members": {execExpression},
		"conversion": {execConversion}, "dispatch": {execCall}, "candidates": {execCall}, "effects": {execEffects}, "evaluation_order": {execEvaluation}, "initialization_order": {execDeclaration},
		"exception_model": {execException}, "unwind": {execException}, "ownership": {execLifetime}, "lifetime": {execLifetime}, "provenance": {execMemory}, "storage": {execMemory}, "memory_model": {execMemory}, "memory_order": {execMemory},
		"synchronization": {execConcurrency}, "concurrency_contract": {execConcurrency}, "coroutine_state": {execConcurrency}, "compiletime_contract": {execCompileTime}, "compiletime_value": {execCompileTime},
		"module": {execModule}, "linkage": {execABI}, "layout": {execABI}, "abi_contract": {execABI}, "calling_convention": {execABI}, "lowering": {execLowering}, "runtime_contract": {execRuntime}, "validation": {execValidation}, "language_contract": {execLanguageContract}, "dialect": {execDialect},
	}
}

func executionPrimitiveIndex(registry UASTExecutionRegistry) map[UASTExecutionPrimitive]int {
	out := make(map[UASTExecutionPrimitive]int, len(registry.Primitives))
	for i, primitive := range registry.Primitives {
		out[primitive.ID] = i
	}
	return out
}

func addExecutionRequirements(m *matrixir.SparseMatrix, row int, ids []UASTExecutionPrimitive, index map[UASTExecutionPrimitive]int) {
	for _, id := range ids {
		if col, ok := index[id]; ok {
			m.Set(row, col, 1)
		}
	}
}

func capabilityExecutionSignature(m matrixir.SparseMatrix, row int) string {
	var b strings.Builder
	for col := 0; col < m.Cols; col++ {
		if m.At(row, col) != 0 {
			b.WriteByte('1')
		} else {
			b.WriteByte('0')
		}
	}
	return b.String()
}

func executionRequirementNames(m matrixir.SparseMatrix, row int, registry UASTExecutionRegistry) []string {
	out := []string{}
	for col, primitive := range registry.Primitives {
		if m.At(row, col) != 0 {
			out = append(out, string(primitive.ID))
		}
	}
	return out
}

// UniversalExecutionAnalysis derives M_CE, M_SE, M_RE and M_DE directly from
// the canonical UAST basis. It uses exact row equality for the quotient.
func UniversalExecutionAnalysis() (UASTExecutionAnalysis, error) {
	universalExecutionAnalysisOnce.Do(func() {
		universalExecutionAnalysisCache, universalExecutionAnalysisErr = computeUniversalExecutionAnalysis()
	})
	return universalExecutionAnalysisCache, universalExecutionAnalysisErr
}

func computeUniversalExecutionAnalysis() (UASTExecutionAnalysis, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	b := uastEmbedded.Basis
	registry := DefaultUASTExecutionRegistry()
	index := executionPrimitiveIndex(registry)
	analysis := UASTExecutionAnalysis{
		Schema: "code-transpiler.uast-execution-matrix.v1", BasisSHA256: uastEmbedded.BasisSHA256,
		Primitives: append([]UASTExecutionPrimitiveSpec(nil), registry.Primitives...), Capabilities: append([]string(nil), b.Facets...), Structures: append([]string(nil), b.StructuralKinds...), Relations: append([]string(nil), b.ConcreteRelations...), Fields: append([]string(nil), b.Fields...),
		MCE: matrixir.NewSparseMatrix(len(b.Facets), len(registry.Primitives)), MSE: matrixir.NewSparseMatrix(len(b.StructuralKinds), len(registry.Primitives)), MRE: matrixir.NewSparseMatrix(len(b.ConcreteRelations), len(registry.Primitives)), MDE: matrixir.NewSparseMatrix(len(b.Fields), len(registry.Primitives)),
		Implemented: make(matrixir.Vector, len(registry.Primitives)), Executable: make(matrixir.Vector, len(b.Facets)), ExecutableStructures: make(matrixir.Vector, len(b.StructuralKinds)), ExecutableRelations: make(matrixir.Vector, len(b.ConcreteRelations)), ExecutableFields: make(matrixir.Vector, len(b.Fields)), ProductivelyConsumed: make(matrixir.Vector, len(b.Facets)), ConsumerCoverage: map[string][]UASTConsumerKind{}, Missing: matrixir.NewSparseMatrix(len(b.Facets), len(registry.Primitives)), GlobalMissing: map[string]int{}, ProductiveLegacyRelations: 0,
	}
	for col, primitive := range registry.Primitives {
		if primitive.Implemented {
			analysis.Implemented[col] = 1
		}
	}
	for row, structural := range b.StructuralKinds {
		for layer, primitives := range registry.StructuralLayer {
			layerColumn := indexOf(b.Layers, layer)
			if layerColumn >= 0 && b.StructuralLayer.At(row, layerColumn) != 0 {
				addExecutionRequirements(&analysis.MSE, row, primitives, index)
			}
		}
		_ = structural
	}
	for row, relation := range b.ConcreteRelations {
		addExecutionRequirements(&analysis.MRE, row, registry.Relation[relation], index)
	}
	for row, field := range b.Fields {
		addExecutionRequirements(&analysis.MDE, row, registry.Field[field], index)
	}
	for row := range b.Facets {
		for axis, primitives := range registry.SemanticAxis {
			axisColumn := indexOf(b.SemanticAxes, axis)
			if axisColumn >= 0 && b.FacetAxis.At(row, axisColumn) != 0 {
				addExecutionRequirements(&analysis.MCE, row, primitives, index)
			}
		}
		for axis, primitives := range registry.RelationAxis {
			axisColumn := indexOf(b.RelationAxes, axis)
			if axisColumn >= 0 && b.FacetRelationAxis.At(row, axisColumn) != 0 {
				addExecutionRequirements(&analysis.MCE, row, primitives, index)
			}
		}
		for structuralRow := range b.StructuralKinds {
			if b.StructuralFacetSeed.At(structuralRow, row) == 0 {
				continue
			}
			for primitive := range registry.Primitives {
				if analysis.MSE.At(structuralRow, primitive) != 0 {
					analysis.MCE.Set(row, primitive, 1)
				}
			}
		}
		for relationRow := range b.ConcreteRelations {
			if b.FacetConcreteRelation.At(row, relationRow) != 0 {
				for primitive := range registry.Primitives {
					if analysis.MRE.At(relationRow, primitive) != 0 {
						analysis.MCE.Set(row, primitive, 1)
					}
				}
			}
		}
		for fieldRow := range b.Fields {
			if b.FacetField.At(row, fieldRow) != 0 {
				for primitive := range registry.Primitives {
					if analysis.MDE.At(fieldRow, primitive) != 0 {
						analysis.MCE.Set(row, primitive, 1)
					}
				}
			}
		}
	}
	classes := map[string][]int{}
	for row := range b.Facets {
		signature := capabilityExecutionSignature(analysis.MCE, row)
		classes[signature] = append(classes[signature], row)
		complete := true
		for col, primitive := range registry.Primitives {
			if analysis.MCE.At(row, col) == 0 || primitive.Implemented {
				continue
			}
			complete = false
			analysis.Missing.Set(row, col, 1)
			analysis.GlobalMissing[string(primitive.ID)]++
		}
		if complete {
			analysis.Executable[row] = 1
			consumers := map[UASTConsumerKind]bool{}
			for col, primitive := range registry.Primitives {
				if analysis.MCE.At(row, col) == 0 {
					continue
				}
				for _, consumer := range primitive.Consumers {
					consumers[consumer] = true
				}
			}
			ordered := make([]UASTConsumerKind, 0, len(consumers))
			for consumer := range consumers {
				ordered = append(ordered, consumer)
			}
			sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
			if len(ordered) > 0 {
				analysis.ProductivelyConsumed[row] = 1
				analysis.ConsumerCoverage[b.Facets[row]] = ordered
			}
		}
	}
	markExecutablePlane := func(plane matrixir.SparseMatrix, out matrixir.Vector) {
		for row := range out {
			complete := true
			for col := range registry.Primitives {
				if plane.At(row, col) != 0 && analysis.Implemented[col] == 0 {
					complete = false
					break
				}
			}
			if complete {
				out[row] = 1
			}
		}
	}
	markExecutablePlane(analysis.MSE, analysis.ExecutableStructures)
	markExecutablePlane(analysis.MRE, analysis.ExecutableRelations)
	markExecutablePlane(analysis.MDE, analysis.ExecutableFields)
	signatures := make([]string, 0, len(classes))
	for signature := range classes {
		signatures = append(signatures, signature)
	}
	sort.Strings(signatures)
	for i, signature := range signatures {
		rows := classes[signature]
		members := make([]string, len(rows))
		for j, row := range rows {
			members[j] = b.Facets[row]
		}
		analysis.EquivalenceClasses = append(analysis.EquivalenceClasses, UASTExecutionClass{ID: fmt.Sprintf("EXEC_%03d", i+1), UASFMembers: members, ExecutionSignature: signature, RequiredPrimitives: executionRequirementNames(analysis.MCE, rows[0], registry)})
	}
	return analysis, nil
}

// WriteUniversalExecutionAnalysis writes the quotient and missing primitive
// matrices. The files are reproducible reports of the canonical UAST basis.
func WriteUniversalExecutionAnalysis(dir string) (UASTExecutionAnalysis, error) {
	analysis, err := UniversalExecutionAnalysis()
	if err != nil {
		return UASTExecutionAnalysis{}, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	encoded, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return UASTExecutionAnalysis{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, "execution_analysis.json"), encoded, 0o644); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	if err := writeExecutionPlane(filepath.Join(dir, "capability_execution_matrix.csv"), "canonical_semantic_id", analysis.Capabilities, analysis.MCE, analysis.Primitives, analysis.Executable); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	if err := writeExecutionPlane(filepath.Join(dir, "structure_execution_matrix.csv"), "structure_kind", analysis.Structures, analysis.MSE, analysis.Primitives, analysis.ExecutableStructures); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	if err := writeExecutionPlane(filepath.Join(dir, "relation_execution_matrix.csv"), "relation_kind", analysis.Relations, analysis.MRE, analysis.Primitives, analysis.ExecutableRelations); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	if err := writeExecutionPlane(filepath.Join(dir, "field_execution_matrix.csv"), "field", analysis.Fields, analysis.MDE, analysis.Primitives, analysis.ExecutableFields); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	if err := writeExecutionPrimitiveState(filepath.Join(dir, "execution_primitives.csv"), analysis); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	if err := writeExecutionCapabilityFeatures(filepath.Join(dir, "capability_features.csv")); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	if err := writeExecutionConsumerCoverage(filepath.Join(dir, "productive_consumer_coverage.csv"), analysis); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	f, err := os.Create(filepath.Join(dir, "execution_equivalence_classes.csv"))
	if err != nil {
		return UASTExecutionAnalysis{}, err
	}
	w := csv.NewWriter(f)
	if err := w.Write([]string{"class_id", "uasf_members", "execution_signature", "required_primitives"}); err != nil {
		f.Close()
		return UASTExecutionAnalysis{}, err
	}
	for _, class := range analysis.EquivalenceClasses {
		if err := w.Write([]string{class.ID, strings.Join(class.UASFMembers, ";"), class.ExecutionSignature, strings.Join(class.RequiredPrimitives, ";")}); err != nil {
			f.Close()
			return UASTExecutionAnalysis{}, err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		f.Close()
		return UASTExecutionAnalysis{}, err
	}
	if err := f.Close(); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	missing, err := os.Create(filepath.Join(dir, "missing_primitives.csv"))
	if err != nil {
		return UASTExecutionAnalysis{}, err
	}
	mw := csv.NewWriter(missing)
	if err := mw.Write([]string{"canonical_semantic_id", "execution_primitive"}); err != nil {
		missing.Close()
		return UASTExecutionAnalysis{}, err
	}
	for row, capability := range analysis.Capabilities {
		for col, primitive := range analysis.Primitives {
			if analysis.Missing.At(row, col) != 0 {
				if err := mw.Write([]string{capability, string(primitive.ID)}); err != nil {
					missing.Close()
					return UASTExecutionAnalysis{}, err
				}
			}
		}
	}
	mw.Flush()
	if err := mw.Error(); err != nil {
		missing.Close()
		return UASTExecutionAnalysis{}, err
	}
	if err := missing.Close(); err != nil {
		return UASTExecutionAnalysis{}, err
	}
	return analysis, nil
}

func writeExecutionConsumerCoverage(path string, analysis UASTExecutionAnalysis) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"canonical_semantic_id", "required_primitives", "productive_consumers", "productively_consumed"}); err != nil {
		return err
	}
	registry := DefaultUASTExecutionRegistry()
	for row, facet := range analysis.Capabilities {
		consumers := make([]string, 0, len(analysis.ConsumerCoverage[facet]))
		for _, consumer := range analysis.ConsumerCoverage[facet] {
			consumers = append(consumers, string(consumer))
		}
		consumed := "0"
		if analysis.ProductivelyConsumed[row] != 0 {
			consumed = "1"
		}
		if err := w.Write([]string{facet, strings.Join(executionRequirementNames(analysis.MCE, row, registry), ";"), strings.Join(consumers, ";"), consumed}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// writeExecutionCapabilityFeatures preserves the semantic names behind the
// exact 553→334 quotient.  Execution remains keyed by UASF IDs, while this
// table makes every class auditable without relying on placeholder feature
// rows in an external report.
func writeExecutionCapabilityFeatures(path string) error {
	if err := loadUniversalASTBasis(); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"canonical_semantic_id", "source_feature_id"}); err != nil {
		return err
	}
	for featureRow, feature := range uastEmbedded.Basis.Features {
		for facetCol, facet := range uastEmbedded.Basis.Facets {
			if uastEmbedded.Basis.FeatureFacet.At(featureRow, facetCol) == 0 {
				continue
			}
			if err := w.Write([]string{facet, feature}); err != nil {
				return err
			}
		}
	}
	w.Flush()
	return w.Error()
}

func writeExecutionPlane(path, idHeader string, ids []string, plane matrixir.SparseMatrix, primitives []UASTExecutionPrimitiveSpec, executable matrixir.Vector) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	header := []string{idHeader}
	for _, primitive := range primitives {
		header = append(header, string(primitive.ID))
	}
	if executable != nil {
		header = append(header, "executable")
	}
	if err := w.Write(header); err != nil {
		return err
	}
	for row, id := range ids {
		out := []string{id}
		for col := range primitives {
			if plane.At(row, col) != 0 {
				out = append(out, "1")
			} else {
				out = append(out, "0")
			}
		}
		if executable != nil {
			if executable[row] != 0 {
				out = append(out, "1")
			} else {
				out = append(out, "0")
			}
		}
		if err := w.Write(out); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

func writeExecutionPrimitiveState(path string, analysis UASTExecutionAnalysis) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"execution_primitive", "implemented", "handler", "global_missing_uasf"}); err != nil {
		return err
	}
	for _, primitive := range analysis.Primitives {
		if err := w.Write([]string{string(primitive.ID), fmt.Sprintf("%t", primitive.Implemented), primitive.Handler, fmt.Sprintf("%d", analysis.GlobalMissing[string(primitive.ID)])}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
