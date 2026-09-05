package backend

import "strings"

// RepositoryPrimitiveResolution is the terminal, data-driven classification
// used by the repository-wide primitive handoff. It binds candidate semantics
// to existing kernels or shared helper families without adding UAST nodes.
type RepositoryPrimitiveResolution struct {
	ID, Status, Kernel string
}

var repositoryPrimitiveAliases = map[string]string{
	"CONST": "LITERAL", "LOAD_LOCAL": "LOAD", "STORE_LOCAL": "ASSIGNMENT",
	"MOD": "REM", "SHR_LOGICAL": "SHR", "LOGICAL_NOT": "NOT",
}

var repositoryExisting28 = map[string]bool{
	"ADD": true, "SUB": true, "MUL": true, "DIV": true, "REM": true, "POW": true,
	"BIT_AND": true, "BIT_OR": true, "BIT_XOR": true, "SHL": true, "SHR": true,
	"EQ": true, "NE": true, "LT": true, "LE": true, "GT": true, "GE": true,
	"AND": true, "OR": true, "NOT": true, "LITERAL": true, "LOAD": true,
	"ASSIGNMENT": true, "RETURN": true, "ITERATION": true, "CALL": true, "APPEND": true,
}

var repositoryRecipeIDs = map[string]bool{"ALL": true, "AVERAGE2": true, "DOUBLE": true, "FILTER": true, "MEAN": true, "RMS": true}

// ResolveRepositoryPrimitive returns one of the handoff's five terminal
// states. The decision uses structured candidate metadata only.
func ResolveRepositoryPrimitive(id, family, scope, handler string) RepositoryPrimitiveResolution {
	id, family, scope, handler = strings.TrimSpace(id), strings.TrimSpace(family), strings.TrimSpace(scope), strings.TrimSpace(handler)
	if scope == "COMPILER_INTERNAL" {
		return RepositoryPrimitiveResolution{ID: id, Status: "FILTERED_COMPILER_INTERNAL"}
	}
	if repositoryExisting28[id] || repositoryPrimitiveAliases[id] != "" {
		return RepositoryPrimitiveResolution{ID: id, Status: "EXISTING_28_MAP", Kernel: "EXISTING_28"}
	}
	if repositoryRecipeIDs[id] {
		return RepositoryPrimitiveResolution{ID: id, Status: "GENERATED_RECIPE", Kernel: "PRIMITIVE_COMPILER_RECIPE"}
	}
	if scope == "V1_NORMALIZED" {
		if handler == "" {
			handler = "GENERIC_SEMANTIC_KERNEL"
		}
		return RepositoryPrimitiveResolution{ID: id, Status: "GENERIC_HANDLER", Kernel: handler}
	}
	return RepositoryPrimitiveResolution{ID: id, Status: "GENERATED_NATIVE_HELPER", Kernel: handler}
}
