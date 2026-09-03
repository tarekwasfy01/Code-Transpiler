package backend

import "github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"

// StructuredConstructInput is a short-lived parser DTO. It deliberately has
// no target, runtime, UAST facet, or execution information.
type StructuredConstructInput struct {
	Family       string
	NodeKind     string
	Roles        []matrixir.CanonicalRoleFact
	Operands     []matrixir.CanonicalOperandFact
	Bindings     []matrixir.CanonicalBindingFact
	Types        []matrixir.CanonicalSymbolFact
	Fields       map[string]string
	SourceOffset int
}

// MatrixStructuredAdapter transfers only already-proven MatrixIR structure.
// Event.Text is intentionally not read on this path.
func MatrixStructuredAdapter(event matrixir.CanonicalSemanticEvent) (StructuredConstructInput, bool) {
	if event.FactFamily == "" {
		return StructuredConstructInput{}, false
	}
	fields := make(map[string]string, len(event.Fields))
	for key, value := range event.Fields {
		fields[key] = value
	}
	return StructuredConstructInput{Family: string(event.FactFamily), NodeKind: event.StructureKind, Roles: append([]matrixir.CanonicalRoleFact(nil), event.Roles...), Operands: append([]matrixir.CanonicalOperandFact(nil), event.Operands...), Bindings: append([]matrixir.CanonicalBindingFact(nil), event.Bindings...), Types: append([]matrixir.CanonicalSymbolFact(nil), event.Symbols...), Fields: fields, SourceOffset: event.SourceOffset}, true
}

// StructuredFieldCoverage is the exact field basis used by the four shared
// extractor families. A missing cell is diagnostic evidence, never a request
// to reconstruct source spelling.
var StructuredFieldCoverage = map[string][]string{
	"CONTAINER":              {"node kind", "child", "operand", "type", "binding", "allocation size"},
	"ITERATION":              {"node kind", "iterable", "binding", "child", "control/body reference", "filter/condition"},
	"CLOSURE_FUNCTION_VALUE": {"node kind", "parameter", "capture", "binding", "child", "control/body reference"},
	"INDEX_SLICE":            {"node kind", "operand", "index", "slice bounds", "binding"},
}

func StructuredInputFields(input StructuredConstructInput) map[string]bool {
	out := map[string]bool{"node kind": input.NodeKind != ""}
	for _, r := range input.Roles {
		out["child"] = true
		switch r.Role {
		case "base":
			out["operand"] = true
		case "index":
			out["index"] = true
		case "start", "end", "step":
			out["slice bounds"] = true
		case "parameter":
			out["parameter"] = true
		case "binding":
			out["binding"] = true
		case "body":
			out["control/body reference"] = true
		case "iterable":
			out["iterable"] = true
		case "condition", "filter":
			out["filter/condition"] = true
		}
	}
	if input.Family == "CONTAINER" && len(input.Roles) > 0 {
		out["child"] = true
	}
	if len(input.Operands) > 0 {
		out["operand"] = true
	}
	if len(input.Bindings) > 0 {
		out["binding"] = true
	}
	if len(input.Types) > 0 {
		out["type"] = true
	}
	return out
}

func MissingStructuredFields(input StructuredConstructInput) []string {
	available := StructuredInputFields(input)
	required := StructuredFieldCoverage[input.Family]
	missing := []string{}
	for _, field := range required {
		if !available[field] {
			missing = append(missing, field)
		}
	}
	return missing
}
