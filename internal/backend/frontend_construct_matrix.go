package backend

import "sort"

// FrontendConstructMatrixEntry is the language-neutral join between a
// frontend construct and the UAST/execution contracts that already exist in
// this backend.  It is metadata, not a second IR: parser events still carry
// the actual operands and relations.
type FrontendConstructMatrixEntry struct {
	Construct           string   `json:"construct"`
	UASTStructure       string   `json:"uast_structure"`
	UASTKind            string   `json:"uast_kind"`
	ExecutionPrimitives []string `json:"execution_primitives"`
}

// frontendConstructExecution is the shared semantic factor for all
// frontends.  Language adapters select a construct row; they do not create a
// target- or language-specific lowering branch.
var frontendConstructExecution = map[string][]string{
	"literal":             {"expression", "data"},
	"identifier":          {"expression", "binding"},
	"binary":              {"expression", "evaluation"},
	"unary":               {"expression", "evaluation"},
	"call":                {"call", "expression", "evaluation"},
	"member":              {"expression", "binding"},
	"index":               {"expression", "data"},
	"slice":               {"expression", "data"},
	"aggregate":           {"data", "expression"},
	"comprehension":       {"data", "control", "expression"},
	"assign":              {"binding", "data", "expression"},
	"return":              {"control", "expression"},
	"function":            {"declaration", "binding", "call", "expression"},
	"closure":             {"binding", "capture", "call", "expression"},
	"lambda":              {"binding", "capture", "call", "expression"},
	"for":                 {"control", "binding", "expression"},
	"foreach":             {"control", "binding", "expression"},
	"iteration":           {"control", "binding", "expression"},
	"while":               {"control", "expression"},
	"if":                  {"control", "expression"},
	"switch":              {"control", "expression", "binding"},
	"exception":           {"exception", "control"},
	"concurrency":         {"concurrency", "control"},
	"reflection":          {"expression", "metadata"},
	"module":              {"module", "declaration"},
	"binding":             {"binding", "declaration"},
	"tuple":               {"data", "expression"},
	"block":               {"control"},
	"attribute":           {"expression", "binding"},
	"method":              {"expression", "binding", "call"},
	"subscript":           {"expression", "data"},
	"map_access":          {"expression", "data"},
	"object_construction": {"data", "expression"},
	"destructuring":       {"binding", "data"},
	"pattern":             {"binding", "control"},
	"conditional":         {"control", "expression"},
	"async":               {"concurrency", "control"},
	"await":               {"concurrency", "control", "expression"},
	"promise":             {"concurrency", "control", "expression"},
	"range_iteration":     {"control", "binding", "expression"},
	"channel_receive":     {"expression", "data"},
	"conversion":          {"expression", "type"},
	"receiver":            {"expression", "binding"},
}

// UniversalFrontendConstructMatrix returns a deterministic exact join of the
// generated frontend crosswalk and the existing execution primitive plane.
// Keeping this derived from frontendFactCrosswalk prevents a second list of
// language handlers from drifting away from the canonical UAST contracts.
func UniversalFrontendConstructMatrix() []FrontendConstructMatrixEntry {
	keys := make([]string, 0, len(frontendFactCrosswalk))
	for construct := range frontendFactCrosswalk {
		keys = append(keys, construct)
	}
	sort.Strings(keys)
	rows := make([]FrontendConstructMatrixEntry, 0, len(keys))
	for _, construct := range keys {
		crosswalk := frontendFactCrosswalk[construct]
		primitives := append([]string(nil), frontendConstructExecution[construct]...)
		if len(primitives) == 0 {
			// Every crosswalk row has at least the generic expression contract;
			// this fallback keeps newly generated rows representable without
			// inventing a primitive.
			primitives = []string{"expression"}
		}
		rows = append(rows, FrontendConstructMatrixEntry{
			Construct: construct, UASTStructure: crosswalk.StructuralKind,
			UASTKind: crosswalk.UASTKind, ExecutionPrimitives: primitives,
		})
	}
	return rows
}
