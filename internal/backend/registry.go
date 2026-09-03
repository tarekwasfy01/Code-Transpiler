package backend

import "strings"

// FrontendSpec and BackendSpec are declarative registries. They keep language
// selection, aliases, file extensions, dialects and feature boundaries out of
// growing switch statements. Their implementation remains local Go code; no
// CrossTL source is included.
type FrontendSpec struct {
	ID           string
	Aliases      []string
	Extensions   []string
	Capabilities []string
	Dialects     []string
}

type BackendSpec struct {
	ID           string
	Aliases      []string
	Capabilities []string
	Dialects     []string
}

type CapabilityStatus string

const (
	CapabilityNative      CapabilityStatus = "native"
	CapabilityLowering    CapabilityStatus = "lowering"
	CapabilityEmulated    CapabilityStatus = "emulated"
	CapabilityUnsupported CapabilityStatus = "unsupported"
)

// CapabilityResult is a backend contract, not a success prediction. The
// caller can display it before emitting and refuse any non-exact policy.
type CapabilityResult struct {
	Feature string           `json:"feature"`
	Backend string           `json:"backend"`
	Status  CapabilityStatus `json:"status"`
	Reason  string           `json:"reason,omitempty"`
}

var frontendRegistry = []FrontendSpec{
	{"r", []string{"r"}, []string{".r", ".R"}, []string{"core", "lazy_evaluation", "named_arguments", "one_based_index"}, []string{"core", "r"}},
	{"go", []string{"go"}, []string{".go"}, []string{"core", "eager_evaluation"}, []string{"core"}},
	{"python", []string{"python", "py"}, []string{".py"}, []string{"core", "eager_evaluation", "named_arguments"}, []string{"core"}},
	{"rust", []string{"rust", "rs"}, []string{".rs"}, []string{"core", "eager_evaluation"}, []string{"core"}},
	{"c", []string{"c"}, []string{".c", ".h"}, []string{"core", "eager_evaluation"}, []string{"core"}},
	{"cpp", []string{"cpp", "c++"}, []string{".cpp", ".cc", ".cxx", ".hpp"}, []string{"core", "eager_evaluation"}, []string{"core"}},
	{"zig", []string{"zig"}, []string{".zig"}, []string{"core", "eager_evaluation"}, []string{"core"}},
	{"julia", []string{"julia"}, []string{".jl"}, []string{"core", "eager_evaluation"}, []string{"core"}},
	{"nim", []string{"nim"}, []string{".nim"}, []string{"core", "eager_evaluation"}, []string{"core"}},
	{"csharp", []string{"csharp", "c#"}, []string{".cs"}, []string{"core", "eager_evaluation"}, []string{"core"}},
	{"java", []string{"java"}, []string{".java"}, []string{"core", "eager_evaluation"}, []string{"core"}},
	{"kotlin", []string{"kotlin"}, []string{".kt"}, []string{"core", "eager_evaluation"}, []string{"core"}},
	{"swift", []string{"swift"}, []string{".swift"}, []string{"core", "eager_evaluation"}, []string{"core"}},
}

var backendRegistry = func() []BackendSpec {
	out := make([]BackendSpec, len(frontendRegistry))
	for i, spec := range frontendRegistry {
		out[i] = BackendSpec{ID: spec.ID, Aliases: spec.Aliases, Capabilities: []string{"core"}, Dialects: []string{"core"}}
	}
	return out
}()

func Frontends() []FrontendSpec { return append([]FrontendSpec(nil), frontendRegistry...) }
func Backends() []BackendSpec   { return append([]BackendSpec(nil), backendRegistry...) }

func NormalizeLanguage(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	for _, spec := range frontendRegistry {
		for _, alias := range spec.Aliases {
			if name == alias {
				return spec.ID
			}
		}
	}
	return name
}
func HasFrontend(name string) bool {
	name = NormalizeLanguage(name)
	for _, spec := range frontendRegistry {
		if spec.ID == name {
			return true
		}
	}
	return false
}
func HasBackend(name string) bool {
	name = NormalizeLanguage(name)
	for _, spec := range backendRegistry {
		if spec.ID == name {
			return true
		}
	}
	return false
}
func BackendCapabilities(name string) []string {
	name = NormalizeLanguage(name)
	for _, spec := range backendRegistry {
		if spec.ID == name {
			return append([]string(nil), spec.Capabilities...)
		}
	}
	return nil
}
func SupportsCapability(caps []string, capability string) bool {
	for _, cap := range caps {
		if cap == capability {
			return true
		}
	}
	return false
}

func BackendCapability(feature, backend string) CapabilityResult {
	backend = NormalizeLanguage(backend)
	if exactIntegerCapability(feature) && (backend == "go" || backend == "python" || backend == "c" || backend == "rust" || backend == "cpp" || backend == "java" || backend == "csharp") {
		return CapabilityResult{Feature: feature, Backend: backend, Status: CapabilityLowering, Reason: "fixed-width integer operations with exact values and explicit wrap semantics"}
	}
	if !HasBackend(backend) {
		return CapabilityResult{Feature: feature, Backend: backend, Status: CapabilityUnsupported, Reason: "unknown backend"}
	}
	if feature == "native.go.functions" && (backend == "go" || backend == "python" || backend == "rust" || backend == "c" || backend == "cpp" || backend == "java" || backend == "csharp") {
		return CapabilityResult{Feature: feature, Backend: backend, Status: CapabilityLowering, Reason: "native scalar helper functions"}
	}
	if feature == "core" {
		return CapabilityResult{Feature: feature, Backend: backend, Status: CapabilityLowering, Reason: "shared semantic core lowering"}
	}
	if feature == "native.go.scalar" {
		// The scalar UAST subset is emitted through the common NativeEmitter for
		// every registered TargetSpec.  This is a lowering contract, not a
		// runtime permission; unavailable native syntax still fails at the
		// canonical DIRECT_NATIVE_UNAVAILABLE boundary.
		return CapabilityResult{Feature: feature, Backend: backend, Status: CapabilityLowering, Reason: "shared native scalar UAST lowering"}
	}
	if SupportsCapability(BackendCapabilities(backend), feature) {
		return CapabilityResult{Feature: feature, Backend: backend, Status: CapabilityNative}
	}
	return CapabilityResult{Feature: feature, Backend: backend, Status: CapabilityUnsupported, Reason: "backend has no declared capability"}
}
