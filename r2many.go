// Package r2many exposes the stable many-to-many transpiler API.
//
// The package translates the supported common semantic subset between every
// registered language. SemanticProgram JSON is the versioned interchange
// format for callers that need to store, inspect or route programs.
package r2many

import (
	"encoding/json"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

type Language struct {
	ID         string   `json:"id"`
	Aliases    []string `json:"aliases"`
	Extensions []string `json:"extensions"`
}

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
