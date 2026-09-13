// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"strconv"
	"strings"
)

// The extended contract kinds are semantic axes, not target instructions.
// They deliberately use closed, typed payloads so a producer cannot hide a
// second IR in Extensions or make a backend guess a missing fact.
const (
	SemanticMemoryContractKind        SemanticContractKind = "MEMORY"
	SemanticOwnershipContractKind     SemanticContractKind = "OWNERSHIP"
	SemanticReferenceContractKind     SemanticContractKind = "REFERENCE"
	SemanticPointerContractKind       SemanticContractKind = "POINTER"
	SemanticLifetimeContractKind      SemanticContractKind = "LIFETIME"
	SemanticStorageContractKind       SemanticContractKind = "STORAGE"
	SemanticMutabilityContractKind    SemanticContractKind = "MUTABILITY"
	SemanticAggregateContractKind     SemanticContractKind = "AGGREGATE"
	SemanticLayoutContractKind        SemanticContractKind = "LAYOUT"
	SemanticShapeContractKind         SemanticContractKind = "SHAPE"
	SemanticBoundsContractKind        SemanticContractKind = "BOUNDS"
	SemanticNullableContractKind      SemanticContractKind = "NULLABLE"
	SemanticValueCategoryContractKind SemanticContractKind = "VALUE_CATEGORY"
	SemanticTextContractKind          SemanticContractKind = "TEXT"
	SemanticControlContractKind       SemanticContractKind = "CONTROL"
	SemanticEffectContractKind        SemanticContractKind = "EFFECT"
	SemanticExceptionContractKind     SemanticContractKind = "EXCEPTION"
	SemanticClosureContractKind       SemanticContractKind = "CLOSURE"
	SemanticDispatchContractKind      SemanticContractKind = "DISPATCH"
	SemanticGenericContractKind       SemanticContractKind = "GENERIC"
	SemanticModuleContractKind        SemanticContractKind = "MODULE"
	SemanticCompileTimeContractKind   SemanticContractKind = "COMPILETIME"
	SemanticConcurrencyContractKind   SemanticContractKind = "CONCURRENCY"
	SemanticAsyncContractKind         SemanticContractKind = "ASYNC"
	SemanticMemoryOrderContractKind   SemanticContractKind = "MEMORY_ORDER"
	SemanticValidationContractKind    SemanticContractKind = "VALIDATION"
	SemanticHandleContractKind        SemanticContractKind = "HANDLE"
	SemanticIOContractKind            SemanticContractKind = "IO"
)

type SemanticMemoryContract struct {
	Access         string   `json:"access"`
	Aliasing       string   `json:"aliasing"`
	Addressability string   `json:"addressability"`
	Provenance     string   `json:"provenance"`
	Safety         string   `json:"safety"`
	Reads          []string `json:"reads,omitempty"`
	Writes         []string `json:"writes,omitempty"`
}

type SemanticOwnershipContract struct {
	Mode     string `json:"mode"`
	Passing  string `json:"passing"`
	Moved    string `json:"moved"`
	Borrowed string `json:"borrowed"`
	Copyable string `json:"copyable"`
	Drop     string `json:"drop"`
}

type SemanticReferenceContract struct {
	Kind       string       `json:"kind"`
	TargetType SemanticType `json:"target_type,omitempty"`
	Mutable    string       `json:"mutable"`
	Rebindable string       `json:"rebindable"`
}

type SemanticPointerContract struct {
	Nullable           string `json:"nullable"`
	Provenance         string `json:"provenance"`
	AddressSpace       string `json:"address_space"`
	Bounds             string `json:"bounds"`
	Arithmetic         string `json:"arithmetic"`
	Dereferenceability string `json:"dereferenceability"`
}

type SemanticLifetimeContract struct {
	Kind     string   `json:"kind"`
	State    string   `json:"state"`
	Region   string   `json:"region"`
	Outlives []string `json:"outlives,omitempty"`
	Cleanup  string   `json:"cleanup"`
}

type SemanticStorageContract struct {
	Kind        string `json:"kind"`
	Addressable string `json:"addressable"`
	Escapes     string `json:"escapes"`
	Allocation  string `json:"allocation"`
}

type SemanticMutabilityContract struct {
	Kind      string `json:"kind"`
	Rebinding string `json:"rebinding"`
}

type SemanticAggregateContract struct {
	Kind          string          `json:"kind"`
	ElementType   *SemanticType   `json:"element_type,omitempty"`
	Fields        []SemanticField `json:"fields,omitempty"`
	FieldOrder    []string        `json:"field_order,omitempty"`
	FixedLength   string          `json:"fixed_length"`
	DynamicLength string          `json:"dynamic_length"`
	Contiguous    string          `json:"contiguous"`
	Packed        string          `json:"packed"`
	Tagged        string          `json:"tagged"`
}

type SemanticLayoutContract struct {
	FieldOrder    []string `json:"field_order,omitempty"`
	SourceDefined string   `json:"source_defined"`
	Packed        string   `json:"packed"`
	Alignment     string   `json:"alignment"`
	Size          string   `json:"size"`
}

type SemanticShapeContract struct {
	LengthKind string `json:"length_kind"`
	Length     *int   `json:"length,omitempty"`
	Rows       *int   `json:"rows,omitempty"`
	Columns    *int   `json:"columns,omitempty"`
	Dimensions []int  `json:"dimensions,omitempty"`
	Strides    []int  `json:"strides,omitempty"`
	Order      string `json:"order"`
}

type SemanticBoundsContract struct {
	Kind                string `json:"kind"`
	Policy              string `json:"policy"`
	IndexDomain         string `json:"index_domain"`
	Checking            string `json:"checking"`
	NegativeIndex       string `json:"negative_index"`
	SliceStartInclusive string `json:"slice_start_inclusive"`
	SliceEndInclusive   string `json:"slice_end_inclusive"`
}

type SemanticNullableContract struct {
	Mode   string `json:"mode"`
	Null   string `json:"null"`
	Unwrap string `json:"unwrap"`
}

type SemanticValueCategoryContract struct {
	Kind            string `json:"kind"`
	Addressable     string `json:"addressable"`
	Temporary       string `json:"temporary"`
	Materialization string `json:"materialization"`
}

type SemanticTextContract struct {
	Encoding      string `json:"encoding"`
	StorageUnit   string `json:"storage_unit"`
	IndexUnit     string `json:"index_unit"`
	LengthUnit    string `json:"length_unit"`
	Normalization string `json:"normalization"`
}

type SemanticControlContract struct {
	EvaluationOrder string `json:"evaluation_order"`
	ShortCircuit    string `json:"short_circuit"`
	Termination     string `json:"termination"`
	Branching       string `json:"branching"`
}

type SemanticEffectContract struct {
	Effects []string `json:"effects,omitempty"`
	Purity  string   `json:"purity"`
	Reads   []string `json:"reads,omitempty"`
	Writes  []string `json:"writes,omitempty"`
}

type SemanticExceptionContract struct {
	Model           string `json:"model"`
	MayThrow        string `json:"may_throw"`
	Nothrow         string `json:"nothrow"`
	CleanupRequired string `json:"cleanup_required"`
	UnwindRequired  string `json:"unwind_required"`
}

type SemanticClosureContract struct {
	Captures            []int             `json:"captures,omitempty"`
	CaptureModes        map[string]string `json:"capture_modes,omitempty"`
	EnvironmentLifetime string            `json:"environment_lifetime"`
	Escaping            string            `json:"escaping"`
	Receiver            string            `json:"receiver"`
}

type SemanticDispatchContract struct {
	Kind            string   `json:"kind"`
	SelectedTarget  *int     `json:"selected_target,omitempty"`
	ResolutionStage string   `json:"resolution_stage"`
	Candidates      []string `json:"candidates,omitempty"`
}

type SemanticGenericContract struct {
	Parameters            []string `json:"parameters,omitempty"`
	Constraints           []string `json:"constraints,omitempty"`
	Variance              []string `json:"variance,omitempty"`
	Specialization        string   `json:"specialization"`
	InstantiatedArguments []string `json:"instantiated_arguments,omitempty"`
}

type SemanticModuleContract struct {
	Kind           string   `json:"kind"`
	Identity       string   `json:"identity"`
	Imports        []string `json:"imports,omitempty"`
	Exports        []string `json:"exports,omitempty"`
	Initialization []string `json:"initialization,omitempty"`
}

type SemanticCompileTimeContract struct {
	Required         string   `json:"required"`
	Allowed          string   `json:"allowed"`
	RuntimeForbidden string   `json:"runtime_forbidden"`
	Result           string   `json:"result"`
	Dependencies     []string `json:"dependencies,omitempty"`
}

type SemanticConcurrencyContract struct {
	Operation       string `json:"operation"`
	Synchronization string `json:"synchronization"`
	Communication   string `json:"communication"`
	Spawn           string `json:"spawn"`
}

type SemanticAsyncContract struct {
	Operation             string `json:"operation"`
	SuspendPoint          string `json:"suspend_point"`
	ResumeTarget          string `json:"resume_target"`
	ExceptionContinuation string `json:"exception_continuation"`
}

type SemanticMemoryOrderContract struct {
	Order string `json:"order"`
}

type SemanticValidationContract struct {
	Kind    string `json:"kind"`
	Policy  string `json:"policy"`
	Failure string `json:"failure"`
}

// SemanticHandleContract describes an opaque operating-system resource.
// Ownership and cleanup remain semantic facts; the target backend only
// supplies the representation and ABI implementation.
type SemanticHandleContract struct {
	Kind           string `json:"kind"`
	Representation string `json:"representation"`
	Nullable       string `json:"nullable"`
	Ownership      string `json:"ownership"`
	Close          string `json:"close"`
	Lifetime       string `json:"lifetime"`
}

// SemanticIOContract carries the complete reader/writer operation contract.
type SemanticIOContract struct {
	Operation    string       `json:"operation"`
	Handle       SemanticType `json:"handle"`
	Payload      SemanticType `json:"payload"`
	Encoding     string       `json:"encoding"`
	Result       SemanticType `json:"result"`
	Failure      string       `json:"failure"`
	PartialWrite string       `json:"partial_write"`
}

func extendedContractPayloadTarget(kind SemanticContractKind) any {
	switch kind {
	case SemanticMemoryContractKind:
		return &SemanticMemoryContract{}
	case SemanticOwnershipContractKind:
		return &SemanticOwnershipContract{}
	case SemanticReferenceContractKind:
		return &SemanticReferenceContract{}
	case SemanticPointerContractKind:
		return &SemanticPointerContract{}
	case SemanticLifetimeContractKind:
		return &SemanticLifetimeContract{}
	case SemanticStorageContractKind:
		return &SemanticStorageContract{}
	case SemanticMutabilityContractKind:
		return &SemanticMutabilityContract{}
	case SemanticAggregateContractKind:
		return &SemanticAggregateContract{}
	case SemanticLayoutContractKind:
		return &SemanticLayoutContract{}
	case SemanticShapeContractKind:
		return &SemanticShapeContract{}
	case SemanticBoundsContractKind:
		return &SemanticBoundsContract{}
	case SemanticNullableContractKind:
		return &SemanticNullableContract{}
	case SemanticValueCategoryContractKind:
		return &SemanticValueCategoryContract{}
	case SemanticTextContractKind:
		return &SemanticTextContract{}
	case SemanticControlContractKind:
		return &SemanticControlContract{}
	case SemanticEffectContractKind:
		return &SemanticEffectContract{}
	case SemanticExceptionContractKind:
		return &SemanticExceptionContract{}
	case SemanticClosureContractKind:
		return &SemanticClosureContract{}
	case SemanticDispatchContractKind:
		return &SemanticDispatchContract{}
	case SemanticGenericContractKind:
		return &SemanticGenericContract{}
	case SemanticModuleContractKind:
		return &SemanticModuleContract{}
	case SemanticCompileTimeContractKind:
		return &SemanticCompileTimeContract{}
	case SemanticConcurrencyContractKind:
		return &SemanticConcurrencyContract{}
	case SemanticAsyncContractKind:
		return &SemanticAsyncContract{}
	case SemanticMemoryOrderContractKind:
		return &SemanticMemoryOrderContract{}
	case SemanticValidationContractKind:
		return &SemanticValidationContract{}
	case SemanticHandleContractKind:
		return &SemanticHandleContract{}
	case SemanticIOContractKind:
		return &SemanticIOContract{}
	default:
		return nil
	}
}

func knownExtendedContractRole(role string) bool {
	switch role {
	case "memory", "ownership", "reference", "pointer", "lifetime", "storage", "mutability", "aggregate", "layout", "shape", "bounds", "nullable", "value_category", "text", "control", "effect", "exception", "closure", "dispatch", "generic", "module", "compiletime", "concurrency", "async", "memory_order", "validation", "handle", "io":
		return true
	default:
		return false
	}
}

func contractRoleKind(role string) SemanticContractKind {
	return SemanticContractKind(strings.ToUpper(role))
}

func fieldString(n *UniversalASTNode, name string) string {
	var value string
	if n == nil || len(n.Fields[name]) == 0 {
		return ""
	}
	if json.Unmarshal(n.Fields[name], &value) == nil {
		return strings.TrimSpace(value)
	}
	return ""
}

func fieldMapString(n *UniversalASTNode, name string) map[string]string {
	var value map[string]string
	if n == nil || len(n.Fields[name]) == 0 || json.Unmarshal(n.Fields[name], &value) != nil {
		return nil
	}
	return value
}

func stringOrUnknown(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unknown"
	}
	return value
}

func boolState(value bool, known bool) string {
	if !known {
		return "unknown"
	}
	if value {
		return "true"
	}
	return "false"
}

func intPointer(value int, known bool) *int {
	if !known {
		return nil
	}
	return &value
}

func typeHasAggregateShape(t SemanticType) bool {
	switch strings.ToLower(t.Kind) {
	case "array", "slice", "vector", "matrix", "map", "set", "tuple", "struct", "record", "object", "aggregate", "string":
		return true
	default:
		return false
	}
}

func aggregateKind(t SemanticType) string {
	if t.Kind == "" {
		return "unknown"
	}
	return strings.ToLower(t.Kind)
}

func semanticFieldNames(fields []SemanticField) []string {
	if len(fields) == 0 {
		return nil
	}
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.Name != "" {
			out = append(out, field.Name)
		}
	}
	return out
}

func integerStrings(values []int) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, strconv.Itoa(value))
	}
	return out
}

func hasUniversalField(n *UniversalASTNode, name string) bool {
	return n != nil && len(n.Fields[name]) != 0
}

func structuralOrSemanticKind(n *UniversalASTNode, c universalDecodedCommon) string {
	if n != nil && n.StructuralKind != "" {
		return strings.ToLower(n.StructuralKind)
	}
	return strings.ToLower(c.Kind)
}

func containsAny(value string, values ...string) bool {
	value = strings.ToLower(value)
	for _, item := range values {
		if value == strings.ToLower(item) {
			return true
		}
	}
	return false
}

func deriveExtendedUniversalContractCandidates(d *UniversalASTDocument, common map[int]universalDecodedCommon, children map[int]map[string][]universalChild) ([]contractCandidate, error) {
	if d == nil {
		return nil, nil
	}
	var out []contractCandidate
	add := func(nodeID int, role string, kind SemanticContractKind, value any) error {
		payload, err := contractPayload(kind, value)
		if err != nil {
			return err
		}
		out = append(out, contractCandidate{kind: kind, payload: payload, nodeID: nodeID, role: role})
		return nil
	}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		c, ok := common[n.ID]
		if !ok {
			continue
		}
		kind := structuralOrSemanticKind(n, c)
		roles := children[n.ID]

		var ownership universalOwnershipField
		_ = decodeUniversalField(n, "ownership", &ownership)
		ownershipMode := stringOrUnknown(c.Type.Ownership)
		passing := stringOrUnknown(c.Operation.ParameterPassing)
		if ownership.TypeOwnership != "" {
			ownershipMode = ownership.TypeOwnership
		}
		if ownership.ParameterPassing != "" {
			passing = ownership.ParameterPassing
		}
		if c.Semantics.Aliasing != "" || len(c.Effects) != 0 || c.Type.Reference || strings.EqualFold(c.Type.Kind, "pointer") || hasUniversalField(n, "provenance") || hasUniversalField(n, "storage") {
			access := "unknown"
			for _, effect := range c.Effects {
				switch strings.ToLower(effect) {
				case "read", "load":
					access = "read"
				case "write", "store", "mutation":
					access = "write"
				}
			}
			if err := add(n.ID, "memory", SemanticMemoryContractKind, SemanticMemoryContract{Access: access, Aliasing: stringOrUnknown(c.Semantics.Aliasing), Addressability: stringOrUnknown(fieldString(n, "addressable")), Provenance: stringOrUnknown(fieldString(n, "provenance")), Safety: stringOrUnknown(fieldString(n, "memory_safety")), Reads: nil, Writes: nil}); err != nil {
				return nil, err
			}
		}
		if c.Type.Ownership != "" || c.Operation.ParameterPassing != "" || hasUniversalField(n, "ownership") {
			if err := add(n.ID, "ownership", SemanticOwnershipContractKind, SemanticOwnershipContract{Mode: ownershipMode, Passing: passing, Moved: stringOrUnknown(fieldString(n, "moved")), Borrowed: stringOrUnknown(fieldString(n, "borrowed")), Copyable: stringOrUnknown(fieldString(n, "copyable")), Drop: stringOrUnknown(fieldString(n, "drop"))}); err != nil {
				return nil, err
			}
		}

		if c.Type.Reference || strings.EqualFold(c.Type.Kind, "reference") || containsAny(kind, "referenceexpr", "reference") {
			if err := add(n.ID, "reference", SemanticReferenceContractKind, SemanticReferenceContract{Kind: stringOrUnknown(c.Type.Kind), TargetType: typeOrUnknown(c.Type.Element), Mutable: stringOrUnknown(c.Semantics.Mutation), Rebindable: stringOrUnknown(fieldString(n, "rebindable"))}); err != nil {
				return nil, err
			}
		}
		if strings.EqualFold(c.Type.Kind, "pointer") || containsAny(kind, "pointerexpr", "pointer") || hasUniversalField(n, "provenance") || hasUniversalField(n, "address_space") {
			if err := add(n.ID, "pointer", SemanticPointerContractKind, SemanticPointerContract{Nullable: stringOrUnknown(c.Type.Nullable), Provenance: stringOrUnknown(fieldString(n, "provenance")), AddressSpace: stringOrUnknown(fieldString(n, "address_space")), Bounds: stringOrUnknown(fieldString(n, "bounds")), Arithmetic: stringOrUnknown(fieldString(n, "pointer_arithmetic")), Dereferenceability: stringOrUnknown(fieldString(n, "dereferenceability"))}); err != nil {
				return nil, err
			}
		}
		if c.Type.Lifetime != "" || hasUniversalField(n, "lifetime") || containsAny(kind, "lifetimeregion", "defer", "cleanup") {
			if err := add(n.ID, "lifetime", SemanticLifetimeContractKind, SemanticLifetimeContract{Kind: stringOrUnknown(c.Type.Lifetime), State: stringOrUnknown(fieldString(n, "lifetime_state")), Region: stringOrUnknown(fieldString(n, "region")), Cleanup: stringOrUnknown(fieldString(n, "cleanup"))}); err != nil {
				return nil, err
			}
		}
		if hasUniversalField(n, "storage") || containsAny(kind, "variabledecl", "parameterdecl", "storage", "allocation") {
			if err := add(n.ID, "storage", SemanticStorageContractKind, SemanticStorageContract{Kind: stringOrUnknown(fieldString(n, "storage")), Addressable: stringOrUnknown(fieldString(n, "addressable")), Escapes: stringOrUnknown(fieldString(n, "escapes")), Allocation: stringOrUnknown(fieldString(n, "allocation"))}); err != nil {
				return nil, err
			}
		}
		if c.Semantics.Mutation != "" || hasUniversalField(n, "mutability") || containsAny(kind, "mutable", "immutable") {
			if err := add(n.ID, "mutability", SemanticMutabilityContractKind, SemanticMutabilityContract{Kind: stringOrUnknown(c.Semantics.Mutation), Rebinding: stringOrUnknown(fieldString(n, "rebinding"))}); err != nil {
				return nil, err
			}
		}

		if typeHasAggregateShape(c.Type) {
			fixed := "unknown"
			dynamic := "unknown"
			if c.Type.Length != 0 || strings.EqualFold(c.Type.Kind, "array") {
				fixed, dynamic = "true", "false"
			}
			if strings.EqualFold(c.Type.Kind, "slice") || strings.EqualFold(c.Type.Kind, "vector") || strings.EqualFold(c.Type.Kind, "map") {
				fixed, dynamic = "false", "true"
			}
			agg := SemanticAggregateContract{Kind: aggregateKind(c.Type), FixedLength: fixed, DynamicLength: dynamic, Contiguous: stringOrUnknown(fieldString(n, "contiguous")), Packed: stringOrUnknown(fieldString(n, "packed")), Tagged: stringOrUnknown(fieldString(n, "tagged"))}
			if c.Type.Element != nil {
				agg.ElementType = c.Type.Element
			}
			agg.Fields = append(agg.Fields, c.Type.Fields...)
			agg.FieldOrder = semanticFieldNames(c.Type.Fields)
			if err := add(n.ID, "aggregate", SemanticAggregateContractKind, agg); err != nil {
				return nil, err
			}
			if err := add(n.ID, "layout", SemanticLayoutContractKind, SemanticLayoutContract{FieldOrder: agg.FieldOrder, SourceDefined: stringOrUnknown(fieldString(n, "layout_source_defined")), Packed: agg.Packed, Alignment: stringOrUnknown(fieldString(n, "alignment")), Size: stringOrUnknown(fieldString(n, "size"))}); err != nil {
				return nil, err
			}
			shape := SemanticShapeContract{LengthKind: "unknown", Order: stringOrUnknown(fieldString(n, "order"))}
			if c.Type.Length != 0 {
				shape.LengthKind, shape.Length = "fixed", intPointer(c.Type.Length, true)
			} else if strings.EqualFold(c.Type.Kind, "slice") || strings.EqualFold(c.Type.Kind, "vector") {
				shape.LengthKind = "dynamic"
			}
			if c.Type.Rows != 0 {
				shape.Rows = intPointer(c.Type.Rows, true)
			}
			if c.Type.Columns != 0 {
				shape.Columns = intPointer(c.Type.Columns, true)
			}
			if shape.Rows != nil || shape.Columns != nil {
				shape.Dimensions = []int{c.Type.Rows, c.Type.Columns}
			}
			if err := add(n.ID, "shape", SemanticShapeContractKind, shape); err != nil {
				return nil, err
			}
		}

		isIndex := c.Kind == "index" || containsAny(kind, "indexexpr", "sliceexpr") || c.Semantics.IndexDomain != "" || c.Semantics.OutOfBounds != ""
		if isIndex {
			if err := add(n.ID, "bounds", SemanticBoundsContractKind, SemanticBoundsContract{Kind: stringOrUnknown(c.Kind), Policy: stringOrUnknown(c.Semantics.OutOfBounds), IndexDomain: stringOrUnknown(c.Semantics.IndexDomain), Checking: stringOrUnknown(c.Semantics.ErrorModel), NegativeIndex: stringOrUnknown(c.Semantics.NegativeIndex), SliceStartInclusive: boolStringPointer(c.Semantics.SliceStartInclusive), SliceEndInclusive: boolStringPointer(c.Semantics.SliceEndInclusive)}); err != nil {
				return nil, err
			}
		}
		if c.Type.Nullable != "" || c.Kind == "literal" && containsAny(c.Operation.LiteralKind, "null", "na") {
			if err := add(n.ID, "nullable", SemanticNullableContractKind, SemanticNullableContract{Mode: stringOrUnknown(c.Type.Nullable), Null: stringOrUnknown(c.Operation.LiteralKind), Unwrap: stringOrUnknown(fieldString(n, "unwrap"))}); err != nil {
				return nil, err
			}
		}
		if c.Kind != "" && (c.Kind == "literal" || c.Kind == "identifier" || c.Kind == "call" || c.Kind == "conversion") {
			category := "rvalue"
			if c.Kind == "identifier" {
				category = "lvalue"
			}
			if c.Kind == "call" || c.Kind == "conversion" {
				category = "temporary"
			}
			if err := add(n.ID, "value_category", SemanticValueCategoryContractKind, SemanticValueCategoryContract{Kind: category, Addressable: stringOrUnknown(fieldString(n, "addressable")), Temporary: boolState(category == "temporary", true), Materialization: stringOrUnknown(fieldString(n, "materialization"))}); err != nil {
				return nil, err
			}
		}
		if strings.EqualFold(c.Type.Kind, "string") || strings.EqualFold(c.Type.Kind, "text") || c.Operation.LiteralKind == "string" {
			if err := add(n.ID, "text", SemanticTextContractKind, SemanticTextContract{Encoding: stringOrUnknown(d.Types.Text), StorageUnit: stringOrUnknown(fieldString(n, "storage_unit")), IndexUnit: stringOrUnknown(fieldString(n, "index_unit")), LengthUnit: stringOrUnknown(fieldString(n, "length_unit")), Normalization: stringOrUnknown(fieldString(n, "normalization"))}); err != nil {
				return nil, err
			}
		}

		isControl := c.Operation.SemanticID != "" || c.Operation.Operator != "" || c.Semantics.EvaluationOrder != "" || containsAny(kind, "ifstmt", "loopstmt", "returnstmt", "breakstmt", "continuestmt", "blockstmt")
		if isControl {
			shortKnown := c.Operation.Operator == "&&" || c.Operation.Operator == "||" || c.Semantics.ShortCircuit
			if err := add(n.ID, "control", SemanticControlContractKind, SemanticControlContract{EvaluationOrder: stringOrUnknown(c.Semantics.EvaluationOrder), ShortCircuit: boolState(c.Semantics.ShortCircuit, shortKnown), Termination: stringOrUnknown(fieldString(n, "termination")), Branching: stringOrUnknown(fieldString(n, "branching"))}); err != nil {
				return nil, err
			}
		}
		if len(c.Effects) != 0 || c.Semantics.Mutation != "" || hasUniversalField(n, "effects") {
			if err := add(n.ID, "effect", SemanticEffectContractKind, SemanticEffectContract{Effects: append([]string(nil), c.Effects...), Purity: stringOrUnknown(fieldString(n, "purity")), Reads: nil, Writes: nil}); err != nil {
				return nil, err
			}
		}
		if c.Semantics.ErrorModel != "" || hasUniversalField(n, "exception_model") || containsAny(kind, "trystmt", "throwstmt", "catchstmt", "exception") {
			model := c.Semantics.ErrorModel
			if model == "" {
				model = fieldString(n, "exception_model")
			}
			if err := add(n.ID, "exception", SemanticExceptionContractKind, SemanticExceptionContract{Model: stringOrUnknown(model), MayThrow: stringOrUnknown(fieldString(n, "may_throw")), Nothrow: stringOrUnknown(fieldString(n, "nothrow")), CleanupRequired: stringOrUnknown(fieldString(n, "cleanup_required")), UnwindRequired: stringOrUnknown(fieldString(n, "unwind_required"))}); err != nil {
				return nil, err
			}
		}

		isClosure := containsAny(kind, "closureexpr", "lambda", "closure") || c.Kind == "closure" || len(roles["capture"]) != 0
		if isClosure {
			captures := make([]int, 0, len(roles["capture"]))
			for _, child := range roles["capture"] {
				captures = append(captures, child.ID)
			}
			if err := add(n.ID, "closure", SemanticClosureContractKind, SemanticClosureContract{Captures: captures, CaptureModes: fieldMapString(n, "capture_modes"), EnvironmentLifetime: stringOrUnknown(c.Type.Lifetime), Escaping: stringOrUnknown(fieldString(n, "escaping")), Receiver: stringOrUnknown(fieldString(n, "receiver"))}); err != nil {
				return nil, err
			}
		}
		if c.Kind == "call" || c.Semantics.Dispatch != "" || c.Operation.FunctionBinding != "" {
			dispatch := SemanticDispatchContract{Kind: stringOrUnknown(c.Semantics.Dispatch), ResolutionStage: stringOrUnknown(fieldString(n, "resolution_stage"))}
			if c.Operation.CallResolution != nil {
				if c.Operation.CallResolution.Selected != nil {
					dispatch.SelectedTarget = c.Operation.CallResolution.Selected
				}
				for _, candidate := range c.Operation.CallResolution.Candidates {
					dispatch.Candidates = append(dispatch.Candidates, candidate.Declaration)
				}
			}
			if err := add(n.ID, "dispatch", SemanticDispatchContractKind, dispatch); err != nil {
				return nil, err
			}
		}
		if len(c.Type.TypeParameters) != 0 || len(c.Type.TypeArguments) != 0 || len(c.Type.Constraints) != 0 || containsAny(kind, "genericdecl", "template", "typeparameter") {
			params := make([]string, 0, len(c.Type.TypeParameters))
			for _, p := range c.Type.TypeParameters {
				params = append(params, p.Identity+":"+p.Name)
			}
			args := make([]string, 0, len(c.Type.TypeArguments))
			for _, a := range c.Type.TypeArguments {
				args = append(args, a.Identity+":"+a.Name)
			}
			if err := add(n.ID, "generic", SemanticGenericContractKind, SemanticGenericContract{Parameters: params, Constraints: append([]string(nil), c.Type.Constraints...), Specialization: stringOrUnknown(fieldString(n, "specialization")), InstantiatedArguments: args}); err != nil {
				return nil, err
			}
		}
		if containsAny(kind, "moduledecl", "importdecl", "exportdecl", "module") || (n.ID == 0 && len(d.Origin.Modules) != 0) {
			moduleID := c.Name
			if moduleID == "" {
				moduleID = fieldString(n, "module_identity")
			}
			if err := add(n.ID, "module", SemanticModuleContractKind, SemanticModuleContract{Kind: stringOrUnknown(c.Kind), Identity: stringOrUnknown(moduleID), Imports: append([]string(nil), d.Origin.Modules...), Initialization: []string{stringOrUnknown(fieldString(n, "initialization"))}}); err != nil {
				return nil, err
			}
		}
		if hasUniversalField(n, "compiletime_value") || containsAny(kind, "compiletime", "constdecl", "macro", "constexpr") {
			if err := add(n.ID, "compiletime", SemanticCompileTimeContractKind, SemanticCompileTimeContract{Required: stringOrUnknown(fieldString(n, "compiletime_required")), Allowed: stringOrUnknown(fieldString(n, "compiletime_allowed")), RuntimeForbidden: stringOrUnknown(fieldString(n, "runtime_forbidden")), Result: stringOrUnknown(fieldString(n, "compiletime_value")), Dependencies: nil}); err != nil {
				return nil, err
			}
		}
		if containsAny(kind, "concurrency", "concurrencyop", "spawn", "channel", "mutex", "atomic") || hasUniversalField(n, "synchronization") {
			if err := add(n.ID, "concurrency", SemanticConcurrencyContractKind, SemanticConcurrencyContract{Operation: stringOrUnknown(c.Operation.SemanticID), Synchronization: stringOrUnknown(fieldString(n, "synchronization")), Communication: stringOrUnknown(fieldString(n, "communication")), Spawn: stringOrUnknown(fieldString(n, "spawn"))}); err != nil {
				return nil, err
			}
		}
		if c.Semantics.Suspension != "" || c.Semantics.Resumption != "" || containsAny(kind, "async", "await", "coroutine", "suspend") {
			if err := add(n.ID, "async", SemanticAsyncContractKind, SemanticAsyncContract{Operation: stringOrUnknown(c.Operation.SemanticID), SuspendPoint: stringOrUnknown(c.Semantics.Suspension), ResumeTarget: stringOrUnknown(c.Semantics.Resumption), ExceptionContinuation: stringOrUnknown(c.Semantics.FailureContinuation)}); err != nil {
				return nil, err
			}
		}
		if hasUniversalField(n, "memory_order") || containsAny(kind, "atomic", "synchronization") {
			if err := add(n.ID, "memory_order", SemanticMemoryOrderContractKind, SemanticMemoryOrderContract{Order: stringOrUnknown(fieldString(n, "memory_order"))}); err != nil {
				return nil, err
			}
		}
		if c.Semantics.FailureCondition != "" || c.Semantics.FailureResult != "" || containsAny(kind, "validation", "assert", "check") {
			if err := add(n.ID, "validation", SemanticValidationContractKind, SemanticValidationContract{Kind: stringOrUnknown(c.Kind), Policy: stringOrUnknown(c.Semantics.ErrorModel), Failure: stringOrUnknown(c.Semantics.FailureCondition)}); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func typeOrUnknown(value *SemanticType) SemanticType {
	if value == nil {
		return SemanticType{Kind: "unknown", TypeOrigin: "derived"}
	}
	return *value
}

func boolStringPointer(value *bool) string {
	if value == nil {
		return "unknown"
	}
	if *value {
		return "true"
	}
	return "false"
}

func nodeSupportsExtendedContractRole(n *UniversalASTNode, c universalDecodedCommon, role string) bool {
	if n == nil {
		return false
	}
	kind := structuralOrSemanticKind(n, c)
	switch role {
	case "memory":
		return c.Semantics.Aliasing != "" || len(c.Effects) != 0 || c.Type.Reference || strings.EqualFold(c.Type.Kind, "pointer") || hasUniversalField(n, "provenance") || hasUniversalField(n, "storage")
	case "ownership":
		return c.Type.Ownership != "" || c.Operation.ParameterPassing != "" || hasUniversalField(n, "ownership")
	case "reference":
		return c.Type.Reference || strings.EqualFold(c.Type.Kind, "reference") || containsAny(kind, "referenceexpr", "reference")
	case "pointer":
		return strings.EqualFold(c.Type.Kind, "pointer") || containsAny(kind, "pointerexpr", "pointer") || hasUniversalField(n, "provenance") || hasUniversalField(n, "address_space")
	case "lifetime":
		return c.Type.Lifetime != "" || hasUniversalField(n, "lifetime") || containsAny(kind, "lifetimeregion", "defer", "cleanup")
	case "storage":
		return hasUniversalField(n, "storage") || containsAny(kind, "variabledecl", "parameterdecl", "storage", "allocation")
	case "mutability":
		return c.Semantics.Mutation != "" || hasUniversalField(n, "mutability") || containsAny(kind, "mutable", "immutable")
	case "aggregate", "layout", "shape":
		return typeHasAggregateShape(c.Type)
	case "bounds":
		return c.Kind == "index" || containsAny(kind, "indexexpr", "sliceexpr") || c.Semantics.IndexDomain != "" || c.Semantics.OutOfBounds != ""
	case "nullable":
		return c.Type.Nullable != "" || (c.Kind == "literal" && containsAny(c.Operation.LiteralKind, "null", "na"))
	case "value_category":
		return c.Kind == "literal" || c.Kind == "identifier" || c.Kind == "call" || c.Kind == "conversion"
	case "text":
		return strings.EqualFold(c.Type.Kind, "string") || strings.EqualFold(c.Type.Kind, "text") || c.Operation.LiteralKind == "string"
	case "control":
		return c.Operation.SemanticID != "" || c.Operation.Operator != "" || c.Semantics.EvaluationOrder != "" || containsAny(kind, "ifstmt", "loopstmt", "returnstmt", "breakstmt", "continuestmt", "blockstmt")
	case "effect":
		return len(c.Effects) != 0 || c.Semantics.Mutation != "" || hasUniversalField(n, "effects")
	case "exception":
		return c.Semantics.ErrorModel != "" || hasUniversalField(n, "exception_model") || containsAny(kind, "trystmt", "throwstmt", "catchstmt", "exception")
	case "closure":
		return containsAny(kind, "closureexpr", "lambda", "closure") || c.Kind == "closure"
	case "dispatch":
		return c.Kind == "call" || c.Semantics.Dispatch != "" || c.Operation.FunctionBinding != ""
	case "generic":
		return len(c.Type.TypeParameters) != 0 || len(c.Type.TypeArguments) != 0 || len(c.Type.Constraints) != 0 || containsAny(kind, "genericdecl", "template", "typeparameter")
	case "module":
		return containsAny(kind, "moduledecl", "importdecl", "exportdecl", "module")
	case "compiletime":
		return hasUniversalField(n, "compiletime_value") || containsAny(kind, "compiletime", "constdecl", "macro", "constexpr")
	case "concurrency":
		return containsAny(kind, "concurrency", "concurrencyop", "spawn", "channel", "mutex", "atomic") || hasUniversalField(n, "synchronization")
	case "async":
		return c.Semantics.Suspension != "" || c.Semantics.Resumption != "" || containsAny(kind, "async", "await", "coroutine", "suspend")
	case "memory_order":
		return hasUniversalField(n, "memory_order") || containsAny(kind, "atomic", "synchronization")
	case "validation":
		return c.Semantics.FailureCondition != "" || c.Semantics.FailureResult != "" || containsAny(kind, "validation", "assert", "check")
	default:
		return false
	}
}

// validateExtendedContractConsumers is deliberately a semantic gate. It
// validates that native/runtime consumers see the same facts as the graph;
// it never substitutes a target default when a contract says unknown.
func validateExtendedContractConsumers(d *UniversalASTDocument) error {
	if d == nil {
		return nil
	}
	for _, ref := range d.ContractRefs {
		var node *UniversalASTNode
		for i := range d.Nodes {
			if d.Nodes[i].ID == ref.NodeID {
				node = &d.Nodes[i]
				break
			}
		}
		if node == nil || ref.ContractID < 0 || ref.ContractID >= len(d.ContractTable) {
			continue
		}
		common, err := decodeUniversalCommon(node)
		if err != nil {
			continue
		}
		contract := d.ContractTable[ref.ContractID]
		switch contract.Kind {
		case SemanticOwnershipContractKind:
			var payload SemanticOwnershipContract
			if err := decodeStrictContractPayload(contract, &payload); err != nil {
				return err
			}
			if common.Type.Ownership != "" && payload.Mode != "unknown" && payload.Mode != common.Type.Ownership {
				return contractFactMismatch(ref, "ownership", common.Type.Ownership, payload.Mode)
			}
		case SemanticLifetimeContractKind:
			var payload SemanticLifetimeContract
			if err := decodeStrictContractPayload(contract, &payload); err != nil {
				return err
			}
			if common.Type.Lifetime != "" && payload.Kind != "unknown" && payload.Kind != common.Type.Lifetime {
				return contractFactMismatch(ref, "lifetime", common.Type.Lifetime, payload.Kind)
			}
		case SemanticNullableContractKind:
			var payload SemanticNullableContract
			if err := decodeStrictContractPayload(contract, &payload); err != nil {
				return err
			}
			if common.Type.Nullable != "" && payload.Mode != "unknown" && payload.Mode != common.Type.Nullable {
				return contractFactMismatch(ref, "nullable", common.Type.Nullable, payload.Mode)
			}
		case SemanticControlContractKind:
			var payload SemanticControlContract
			if err := decodeStrictContractPayload(contract, &payload); err != nil {
				return err
			}
			if common.Semantics.EvaluationOrder != "" && payload.EvaluationOrder != "unknown" && payload.EvaluationOrder != common.Semantics.EvaluationOrder {
				return contractFactMismatch(ref, "evaluation_order", common.Semantics.EvaluationOrder, payload.EvaluationOrder)
			}
		case SemanticEffectContractKind:
			var payload SemanticEffectContract
			if err := decodeStrictContractPayload(contract, &payload); err != nil {
				return err
			}
			if len(common.Effects) != 0 && len(payload.Effects) != 0 && !sameStringSetLocal(common.Effects, payload.Effects) {
				return contractFactMismatch(ref, "effects", strings.Join(common.Effects, "|"), strings.Join(payload.Effects, "|"))
			}
		case SemanticExceptionContractKind:
			var payload SemanticExceptionContract
			if err := decodeStrictContractPayload(contract, &payload); err != nil {
				return err
			}
			if common.Semantics.ErrorModel != "" && payload.Model != "unknown" && payload.Model != common.Semantics.ErrorModel {
				return contractFactMismatch(ref, "exception_model", common.Semantics.ErrorModel, payload.Model)
			}
		}
	}
	return nil
}

func contractFactMismatch(ref SemanticContractReference, fact, graph, contract string) error {
	return &semanticContractMismatchError{NodeID: ref.NodeID, Fact: fact, Graph: graph, Contract: contract}
}

type semanticContractMismatchError struct {
	NodeID   int
	Fact     string
	Graph    string
	Contract string
}

func (e *semanticContractMismatchError) Error() string {
	return "semantic contract mismatch node=" + strconv.Itoa(e.NodeID) + " fact=" + e.Fact + " graph=" + e.Graph + " contract=" + e.Contract
}

func sameStringSetLocal(left, right []string) bool {
	a, b := map[string]bool{}, map[string]bool{}
	for _, item := range left {
		a[item] = true
	}
	for _, item := range right {
		b[item] = true
	}
	if len(a) != len(b) {
		return false
	}
	for item := range a {
		if !b[item] {
			return false
		}
	}
	return true
}
