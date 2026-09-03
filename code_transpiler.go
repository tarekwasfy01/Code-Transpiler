// Package codetranspiler exposes the stable many-to-many Code Transpiler API.
//
// Import path: github.com/tarekwasfy01/Code-Transpiler
package codetranspiler

import (
	"encoding/json"
	"fmt"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

type Language struct {
	ID         string   `json:"id"`
	Aliases    []string `json:"aliases"`
	Extensions []string `json:"extensions"`
}

// TranspileRequest and TranspileTrace expose the same immutable diagnostic
// boundary used by the bundled GUI and CLI.
type TranspileRequest = manytomany.TranspileRequest
type TranspileTrace = manytomany.TranspileTrace

type Capability struct {
	Feature string `json:"feature"`
	Backend string `json:"backend"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
}

func Languages() []Language {
	specs := backend.Frontends()
	out := make([]Language, len(specs))
	for i, spec := range specs {
		out[i] = Language{ID: spec.ID, Aliases: append([]string(nil), spec.Aliases...), Extensions: append([]string(nil), spec.Extensions...)}
	}
	return out
}

func Transpile(source, target, code string) (string, error) {
	return manytomany.Transpile(source, target, code)
}

// TranspileWithTrace is useful for differential diagnostics: it returns the
// exact hashes and native/runtime decision made by the common TranspileCore.
func TranspileWithTrace(request TranspileRequest) (string, TranspileTrace, error) {
	result, err := manytomany.TranspileCore(request)
	return result.Code, result.Trace, err
}

func SemanticJSON(source, code string) ([]byte, error) {
	program, err := manytomany.Parse(source, code)
	if err != nil {
		return nil, err
	}
	return program.Semantic.MarshalSemanticJSON()
}

func TranspileSemanticJSON(target string, data []byte) (string, error) {
	program, err := manytomany.ParseDocument(data)
	if err != nil {
		return "", err
	}
	return manytomany.Emit(target, program)
}

func BackendCapability(feature, target string) Capability {
	result := backend.BackendCapability(feature, target)
	return Capability{Feature: result.Feature, Backend: result.Backend, Status: string(result.Status), Reason: result.Reason}
}

func LanguagesJSON() ([]byte, error) { return json.Marshal(Languages()) }

// CapabilityMatrixJSON returns status matrices for semantic requirements
// across all registered targets. Unknown requested features are unsupported.
func CapabilityMatrixJSON(features []string) ([]byte, error) {
	return json.Marshal(backend.SemanticCapabilityMatrix(features))
}

// ImplementationMatrixJSON returns typed operations by frontend, JSON, runtime
// and target implementation stages. Declarations are not execution test results.
func ImplementationMatrixJSON() ([]byte, error) {
	return json.Marshal(backend.TypedImplementationMatrix())
}

// NativeAnalysisJSON extracts native types, source spans and symbol relations.
// Currently supports import-free Go files. This is an analysis artifact, not
// an executable SemanticProgram; TranspileSemanticJSON intentionally rejects it.
func NativeAnalysisJSON(source, filename, code string) ([]byte, error) {
	if source != "go" {
		return nil, fmt.Errorf("native analysis for %q is not implemented", source)
	}
	analysis, err := (backend.GoNativeFrontend{}).Analyze(filename, code)
	if err != nil {
		return nil, err
	}
	return json.Marshal(analysis)
}

// NativeSemanticJSON converts a supported native source subset directly to an
// executable SemanticProgram. It never retries the legacy textual frontend.
// Accepts a bounded Go scalar/function subset, including fixed-width integers.
func NativeSemanticJSON(source, filename, code string) ([]byte, error) {
	if source != "go" {
		return nil, fmt.Errorf("native semantic frontend for %q is not implemented", source)
	}
	program, err := backend.LowerNativeGo(filename, code)
	if err != nil {
		return nil, err
	}
	return program.MarshalSemanticJSON()
}
