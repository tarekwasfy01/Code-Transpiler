package backend

import (
	"encoding/json"
	"fmt"
	"reflect"
)

// TypeConversionStatus is deliberately separate from renderer selection. A
// conversion is usable by UniversalLowering only when its structural contract
// is exact; unknown source information never gets coerced to a convenient
// target type.
type TypeConversionStatus string

const (
	TypeConversionExact      TypeConversionStatus = "EXACT"
	TypeConversionUnresolved TypeConversionStatus = "TYPE_CONVERSION_UNRESOLVED"
)

// TypeConversionRecipe describes one canonical type conversion witness. It
// contains no target AST or intermediate representation.
type TypeConversionRecipe struct {
	ID                string               `json:"id"`
	FromKind          string               `json:"from_kind"`
	ToKind            string               `json:"to_kind"`
	Target            string               `json:"target"`
	Guard             []string             `json:"guard,omitempty"`
	PreservationClass LoweringExactness    `json:"preservation_class"`
	Status            TypeConversionStatus `json:"status"`
}

// UniversalTypeConversionRegistry is the data-driven registry for exact
// structural conversions. Target names are metadata for emitters; validity is
// proven from the canonical source/target type pair, never guessed from source
// spelling.
func UniversalTypeConversionRegistry() []TypeConversionRecipe {
	return []TypeConversionRecipe{
		{ID: "type.identity", FromKind: "*", ToKind: "*", Target: "*", PreservationClass: LoweringExact, Status: TypeConversionExact},
		{ID: "type.integer.same_width", FromKind: "integer", ToKind: "integer", Target: "*", Guard: []string{"bits_equal", "signedness_equal", "overflow_equal"}, PreservationClass: LoweringExact, Status: TypeConversionExact},
		{ID: "type.float.same_precision", FromKind: "float", ToKind: "float", Target: "*", Guard: []string{"precision_equal", "rounding_equal", "nan_equal"}, PreservationClass: LoweringExact, Status: TypeConversionExact},
		{ID: "type.list.elementwise", FromKind: "list", ToKind: "list", Target: "*", Guard: []string{"element_conversion_exact", "order_equal", "nullability_equal", "mutability_equal"}, PreservationClass: LoweringExact, Status: TypeConversionExact},
		{ID: "type.map.elementwise", FromKind: "map", ToKind: "map", Target: "*", Guard: []string{"key_conversion_exact", "value_conversion_exact", "order_equal", "nullability_equal"}, PreservationClass: LoweringExact, Status: TypeConversionExact},
		{ID: "type.array.elementwise", FromKind: "array", ToKind: "array", Target: "*", Guard: []string{"length_equal", "element_conversion_exact", "layout_equal"}, PreservationClass: LoweringExact, Status: TypeConversionExact},
		{ID: "type.function.signature", FromKind: "function", ToKind: "function", Target: "*", Guard: []string{"parameter_count_equal", "parameter_conversion_exact", "result_conversion_exact", "calling_convention_equal"}, PreservationClass: LoweringExact, Status: TypeConversionExact},
	}
}

// ResolveUniversalTypeConversion proves an exact conversion recursively. It
// returns TYPE_CONVERSION_UNRESOLVED for incomplete, unknown, or lossy pairs.
func ResolveUniversalTypeConversion(from, to SemanticType, target string) (TypeConversionRecipe, error) {
	if target == "" {
		return TypeConversionRecipe{Status: TypeConversionUnresolved}, fmt.Errorf("TYPE_CONVERSION_UNRESOLVED: missing target")
	}
	if reflect.DeepEqual(from, to) {
		return TypeConversionRecipe{ID: "type.identity", FromKind: from.Kind, ToKind: to.Kind, Target: target, PreservationClass: LoweringExact, Status: TypeConversionExact}, nil
	}
	if from.Kind == "" || to.Kind == "" {
		return TypeConversionRecipe{Status: TypeConversionUnresolved}, fmt.Errorf("TYPE_CONVERSION_UNRESOLVED: incomplete type")
	}
	if from.Kind != to.Kind {
		return TypeConversionRecipe{Status: TypeConversionUnresolved}, fmt.Errorf("TYPE_CONVERSION_UNRESOLVED: %s -> %s", from.Kind, to.Kind)
	}
	guard := []string{}
	id := ""
	switch from.Kind {
	case "integer":
		if from.Bits != to.Bits || from.Signed == nil || to.Signed == nil || *from.Signed != *to.Signed {
			return TypeConversionRecipe{Status: TypeConversionUnresolved}, fmt.Errorf("TYPE_CONVERSION_UNRESOLVED: integer width/signedness")
		}
		id, guard = "type.integer.same_width", []string{"bits_equal", "signedness_equal", "overflow_equal"}
	case "float":
		if from.Bits != to.Bits || from.IEEE754 != to.IEEE754 {
			return TypeConversionRecipe{Status: TypeConversionUnresolved}, fmt.Errorf("TYPE_CONVERSION_UNRESOLVED: float precision")
		}
		id, guard = "type.float.same_precision", []string{"precision_equal", "rounding_equal", "nan_equal"}
	case "list", "array":
		if from.Element == nil || to.Element == nil {
			return TypeConversionRecipe{Status: TypeConversionUnresolved}, fmt.Errorf("TYPE_CONVERSION_UNRESOLVED: missing element type")
		}
		if _, err := ResolveUniversalTypeConversion(*from.Element, *to.Element, target); err != nil {
			return TypeConversionRecipe{Status: TypeConversionUnresolved}, err
		}
		if from.Kind == "array" && from.Length != to.Length {
			return TypeConversionRecipe{Status: TypeConversionUnresolved}, fmt.Errorf("TYPE_CONVERSION_UNRESOLVED: array length")
		}
		if from.Kind == "list" {
			id, guard = "type.list.elementwise", []string{"element_conversion_exact", "order_equal", "nullability_equal", "mutability_equal"}
		} else {
			id, guard = "type.array.elementwise", []string{"length_equal", "element_conversion_exact", "layout_equal"}
		}
	case "map":
		if from.Key == nil || from.Value == nil || to.Key == nil || to.Value == nil {
			return TypeConversionRecipe{Status: TypeConversionUnresolved}, fmt.Errorf("TYPE_CONVERSION_UNRESOLVED: missing map type")
		}
		if _, err := ResolveUniversalTypeConversion(*from.Key, *to.Key, target); err != nil {
			return TypeConversionRecipe{Status: TypeConversionUnresolved}, err
		}
		if _, err := ResolveUniversalTypeConversion(*from.Value, *to.Value, target); err != nil {
			return TypeConversionRecipe{Status: TypeConversionUnresolved}, err
		}
		id, guard = "type.map.elementwise", []string{"key_conversion_exact", "value_conversion_exact", "order_equal", "nullability_equal"}
	case "function":
		if len(from.Parameters) != len(to.Parameters) || from.Result == nil || to.Result == nil {
			return TypeConversionRecipe{Status: TypeConversionUnresolved}, fmt.Errorf("TYPE_CONVERSION_UNRESOLVED: function signature")
		}
		for i := range from.Parameters {
			if _, err := ResolveUniversalTypeConversion(from.Parameters[i], to.Parameters[i], target); err != nil {
				return TypeConversionRecipe{Status: TypeConversionUnresolved}, err
			}
		}
		if _, err := ResolveUniversalTypeConversion(*from.Result, *to.Result, target); err != nil {
			return TypeConversionRecipe{Status: TypeConversionUnresolved}, err
		}
		id, guard = "type.function.signature", []string{"parameter_count_equal", "parameter_conversion_exact", "result_conversion_exact", "calling_convention_equal"}
	default:
		return TypeConversionRecipe{Status: TypeConversionUnresolved}, fmt.Errorf("TYPE_CONVERSION_UNRESOLVED: unsupported kind %q", from.Kind)
	}
	return TypeConversionRecipe{ID: id, FromKind: from.Kind, ToKind: to.Kind, Target: target, Guard: guard, PreservationClass: LoweringExact, Status: TypeConversionExact}, nil
}

// TypeConversionFingerprint provides a stable key for matrix/report joins.
func TypeConversionFingerprint(t SemanticType) string {
	b, _ := json.Marshal(t)
	return string(b)
}
