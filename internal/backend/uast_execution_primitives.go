package backend

import (
	"encoding/json"
	"fmt"
)

// uastExecutionPrimitiveValidator consumes one semantic execution contract
// directly from the canonical UAST.  Validators are deliberately stateless:
// they neither construct an ExecutionIR nor copy semantic nodes.
type uastExecutionPrimitiveValidator func(*UniversalASTDocument, UASTExecutionPrimitive) error

// executionPrimitiveHandlers is the productive primitive registry.  A
// primitive is reported as implemented only when a concrete consumer is
// registered here.  Runtime value operations remain in runtime_uast.go;
// language/compiler contracts are consumed by the validators below and by the
// target capability projector.
func executionPrimitiveHandlers() map[UASTExecutionPrimitive]struct {
	name     string
	validate uastExecutionPrimitiveValidator
} {
	return map[UASTExecutionPrimitive]struct {
		name     string
		validate uastExecutionPrimitiveValidator
	}{
		execABI:              {"validateUASTABIExecutionContract", validateUASTABIExecutionContract},
		execAnnotation:       {"validateUASTAnnotationExecutionContract", validateUASTAnnotationExecutionContract},
		execBinding:          {"runEnv.get/runEnv.set", validateUASTBindingExecutionContract},
		execCall:             {"runState.uastCall", validateUASTCallExecutionContract},
		execCapture:          {"validateUASTCaptureExecutionContract", validateUASTCaptureExecutionContract},
		execCompileTime:      {"runState.uastPrimitiveExpression/validateUASTCompileTimeExecutionContract", validateUASTCompileTimeExecutionContract},
		execConcurrency:      {"runState.uastPrimitiveStatement/runState.uastPrimitiveExpression", validateUASTConcurrencyExecutionContract},
		execControl:          {"runState.uastStmt", validateUASTControlExecutionContract},
		execConversion:       {"runState.uastConversion", validateUASTConversionExecutionContract},
		execData:             {"runState.uastExpr/runBinary/runSubset", validateUASTDataExecutionContract},
		execDeclaration:      {"runState.uastPrimitiveStatement", validateUASTDeclarationExecutionContract},
		execDialect:          {"validateUASTDialectExecutionContract", validateUASTDialectExecutionContract},
		execEffects:          {"runState.uastStmt", validateUASTEffectExecutionContract},
		execEvaluation:       {"runState.uastBlock", validateUASTEvaluationExecutionContract},
		execException:        {"runState.uastPrimitiveStatement", validateUASTExceptionExecutionContract},
		execExpression:       {"runState.uastExpr", validateUASTExpressionExecutionContract},
		execLanguageContract: {"validateUASTLanguageExecutionContract", validateUASTLanguageExecutionContract},
		execLifetime:         {"validateUASTLifetimeExecutionContract", validateUASTLifetimeExecutionContract},
		execLowering:         {"UniversalTargetProjector.Analyze", validateUASTLoweringExecutionContract},
		execMemory:           {"runState.uastPrimitiveExpression", validateUASTMemoryExecutionContract},
		execMetadata:         {"decodeUniversalCommon", validateUASTMetadataExecutionContract},
		execModule:           {"runState.uastPrimitiveStatement", validateUASTModuleExecutionContract},
		execPreprocessor:     {"validateUASTPreprocessorExecutionContract", validateUASTPreprocessorExecutionContract},
		execRuntime:          {"runState.uastBlock", validateUASTRuntimeExecutionContract},
		execSyntax:           {"newUASTExecutionGraph", validateUASTSyntaxExecutionContract},
		execTemplate:         {"validateUASTTemplateExecutionContract", validateUASTTemplateExecutionContract},
		execTypes:            {"deriveUniversalTypeTable/directTypedRequirements", validateUASTTypeExecutionContract},
		execValidation:       {"validateUniversalASTDocument", validateUASTValidationExecutionContract},
	}
}

// validateUniversalExecutionContracts evaluates the boolean requirement
// product for the concrete document and invokes every required primitive once.
// The calculation uses the same M_SE/M_RE/M_DE/M_CE tables as the generated
// execution report; therefore adding a field or relation cannot bypass its
// semantic consumer.
func validateUniversalExecutionContracts(u *UniversalASTDocument) error {
	analysis, err := UniversalExecutionAnalysis()
	if err != nil {
		return err
	}
	handlers := executionPrimitiveHandlers()
	required := map[UASTExecutionPrimitive]bool{}
	addRow := func(matrixRows []string, matrixCols []UASTExecutionPrimitiveSpec, matrixAt func(int, int) float64, id string) {
		row := indexOf(matrixRows, id)
		if row < 0 {
			return
		}
		for col, primitive := range matrixCols {
			if matrixAt(row, col) != 0 {
				required[primitive.ID] = true
			}
		}
	}
	for i := range u.Nodes {
		n := &u.Nodes[i]
		addRow(analysis.Structures, analysis.Primitives, analysis.MSE.At, n.StructuralKind)
		for _, facet := range n.SemanticFacets {
			addRow(analysis.Capabilities, analysis.Primitives, analysis.MCE.At, facet)
		}
		for field := range n.Fields {
			addRow(analysis.Fields, analysis.Primitives, analysis.MDE.At, field)
		}
	}
	for _, relation := range u.Relations {
		addRow(analysis.Relations, analysis.Primitives, analysis.MRE.At, relation.Kind)
	}
	for _, primitive := range analysis.Primitives {
		if !required[primitive.ID] {
			continue
		}
		handler, ok := handlers[primitive.ID]
		if !ok || handler.validate == nil {
			return fmt.Errorf("EXECUTION_SEMANTIC_GAP: primitive %q has no UAST consumer", primitive.ID)
		}
		if err := handler.validate(u, primitive.ID); err != nil {
			return fmt.Errorf("execution primitive %q: %w", primitive.ID, err)
		}
	}
	return nil
}

func universalExecutionStructureImplemented(kind string) bool {
	analysis, err := UniversalExecutionAnalysis()
	if err != nil {
		return false
	}
	row := indexOf(analysis.Structures, kind)
	return row >= 0 && analysis.ExecutableStructures[row] != 0
}

func universalExecutionFieldImplemented(field string) bool {
	analysis, err := UniversalExecutionAnalysis()
	if err != nil {
		return false
	}
	row := indexOf(analysis.Fields, field)
	return row >= 0 && analysis.ExecutableFields[row] != 0
}

func validatePrimitivePayloads(u *UniversalASTDocument, fields, relations map[string]bool) error {
	for i := range u.Nodes {
		for name, raw := range u.Nodes[i].Fields {
			if !fields[name] {
				continue
			}
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return fmt.Errorf("node %d field %q: %w", u.Nodes[i].ID, name, err)
			}
		}
	}
	for _, relation := range u.Relations {
		if !relations[relation.Kind] {
			continue
		}
		if relation.To.Domain == "" || relation.To.ID == "" {
			return fmt.Errorf("relation %q has incomplete target", relation.Kind)
		}
		for name, raw := range relation.Attributes {
			var value any
			if name == "" || json.Unmarshal(raw, &value) != nil {
				return fmt.Errorf("relation %q has invalid attribute %q", relation.Kind, name)
			}
		}
	}
	return nil
}

func fieldSet(names ...string) map[string]bool {
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out
}

func relationSet(names ...string) map[string]bool {
	return fieldSet(names...)
}

func validateUASTABIExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("abi_contract", "calling_convention", "layout", "linkage"), relationSet("abi.calls", "layout.field", "linkage.links"))
}
func validateUASTAnnotationExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("attributes"), relationSet("annotation.applies"))
}
func validateUASTBindingExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("scope_id", "binding_refs", "name", "symbol"), relationSet("binding.declares", "binding.refers", "binding.shadows", "name.resolves", "scope.parent"))
}
func validateUASTCallExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	if err := validatePrimitivePayloads(u, fieldSet("callee", "arguments", "parameters", "receiver", "dispatch", "candidates"), relationSet("call.calls", "dispatch.resolves", "overload.candidate")); err != nil {
		return err
	}
	return validateCallArgumentContractDimensions(u)
}

// validateCallArgumentContractDimensions is the shared partial-implementation
// contract for calls.  A call is not scalar-only: every argument is a
// structured expression value, including aggregates, indexing results,
// nested calls and function values.  The dimension is derived from the
// canonical node shape so the same contract applies to every frontend and
// target; no source-language or callee-name special case is involved.
func validateCallArgumentContractDimensions(u *UniversalASTDocument) error {
	children, err := universalChildrenByRole(u)
	if err != nil {
		return err
	}
	nodes := make(map[int]*UniversalASTNode, len(u.Nodes))
	common := make(map[int]universalDecodedCommon, len(u.Nodes))
	for i := range u.Nodes {
		n := &u.Nodes[i]
		nodes[n.ID] = n
		c, decodeErr := decodeUniversalCommon(n)
		if decodeErr != nil {
			return decodeErr
		}
		common[n.ID] = c
	}
	for callID, roles := range children {
		call, ok := common[callID]
		if !ok || call.Kind != "call" {
			continue
		}
		for _, arg := range roles["argument"] {
			if arg.Meta.Missing {
				continue
			}
			value, exists := nodes[arg.ID]
			if !exists {
				return fmt.Errorf("CALL_ARGUMENT: node %d references missing argument node %d", callID, arg.ID)
			}
			argCommon := common[arg.ID]
			if !isStructuredCallValue(argCommon, value.StructuralKind) {
				return fmt.Errorf("CALL_ARGUMENT: node %d argument %d has no structured value contract", callID, arg.ID)
			}
		}
	}
	return nil
}

func isStructuredCallValue(c universalDecodedCommon, structuralKind string) bool {
	// These are all canonical expression/value forms already consumed by the
	// execution graph.  AggregateExpr is deliberately included as its own
	// contract dimension rather than being coerced to a scalar.
	switch c.Kind {
	case "literal", "identifier", "aggregate", "binary", "unary", "typed_operation", "call", "index", "function", "missing_argument":
		return structuralKind != ""
	default:
		return structuralKind == "AggregateExpr" || structuralKind == "TupleExpr" || structuralKind == "TupleResult"
	}
}
func validateUASTCaptureExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("binding_refs", "ownership", "lifetime"), relationSet("capture.captures", "binding.shadows"))
}
func validateUASTCompileTimeExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("compiletime_contract", "compiletime_value"), relationSet("compiletime.depends"))
}
func validateUASTConcurrencyExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("synchronization", "concurrency_contract", "coroutine_state", "memory_order"), relationSet("coroutine.suspends", "concurrency.atomic_order", "concurrency.communicates", "concurrency.spawns", "concurrency.synchronizes"))
}
func validateUASTControlExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("body", "condition", "branches", "pattern"), relationSet("control.next", "control.true", "control.false", "control.loop_back"))
}
func validateUASTConversionExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("conversion", "type_ref", "value_category"), relationSet("conversion.converts", "type.convert"))
}
func validateUASTDataExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("operands", "results", "value", "members"), relationSet("data.def_use", "data.operand", "data.result"))
}
func validateUASTDeclarationExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("name", "symbol", "parameters", "body", "initialization_order", "members"), relationSet("binding.declares", "initialization.before"))
}
func validateUASTDialectExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("dialect"), relationSet("dialect.requires"))
}
func validateUASTEffectExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("effects"), relationSet("effect.has"))
}
func validateUASTEvaluationExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("evaluation_order"), relationSet("evaluation.before"))
}
func validateUASTExceptionExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("exception_model", "unwind"), relationSet("exception.unwinds_to"))
}
func validateUASTExpressionExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("operation", "value", "operands", "members"), relationSet("operation.kind", "data.operand", "data.result"))
}
func validateUASTLanguageExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("language_contract"), relationSet("language.contract", "contract.requires"))
}
func validateUASTLifetimeExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("ownership", "lifetime", "storage"), relationSet("memory.owns", "memory.borrows", "lifetime.outlives", "storage.resides_in"))
}
func validateUASTLoweringExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("lowering", "runtime_contract"), relationSet("lowering.requires", "runtime.requires"))
}
func validateUASTMemoryExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("provenance", "storage", "memory_model", "memory_order", "ownership", "lifetime"), relationSet("memory.aliases", "memory.borrows", "pointer.provenance", "storage.resides_in"))
}
func validateUASTMetadataExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("id", "kind", "source_span", "attributes", "extensions"), nil)
}
func validateUASTModuleExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("module", "name", "linkage"), relationSet("module.imports", "module.exports"))
}
func validateUASTPreprocessorExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("compiletime_contract", "compiletime_value", "module"), relationSet("preprocessor.expands", "compiletime.depends"))
}
func validateUASTRuntimeExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("runtime_contract"), relationSet("runtime.requires"))
}
func validateUASTSyntaxExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("body", "condition", "branches", "members", "arguments", "parameters", "value", "callee"), relationSet("syntax.child"))
}
func validateUASTTemplateExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("parameters", "constraints", "compiletime_contract", "compiletime_value"), relationSet("template.instantiates", "type.parameter", "type.constraint"))
}
func validateUASTTypeExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("type_ref", "type_origin", "type_shape", "value_category", "constraints"), relationSet("type.has", "type.origin", "type.constraint", "type.parameter", "value.category"))
}
func validateUASTValidationExecutionContract(u *UniversalASTDocument, _ UASTExecutionPrimitive) error {
	return validatePrimitivePayloads(u, fieldSet("validation", "semantic_facets"), relationSet("validation.proves", "contract.requires"))
}
