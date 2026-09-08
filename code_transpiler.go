// Copyright (c) 2026 Tarek Wasfy
// Package codetranspiler exposes the stable many-to-many Code Transpiler API.
//
// Import path: github.com/tarekwasfy01/Code-Transpiler
package codetranspiler

import (
	"encoding/json"
	"fmt"
	"strings"

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

type CompileOptions = backend.CompileOptions
type CompileResult = backend.CompileResult
type CompileOutputKind = backend.CompileOutputKind
type CompileInputKind = backend.CompileInputKind
type SemanticProgram = backend.SemanticProgram

const (
	InputSource     CompileInputKind = backend.CompileInputSource
	InputAssembly   CompileInputKind = backend.CompileInputAssembly
	InputMachine    CompileInputKind = backend.CompileInputMachine
	InputObject     CompileInputKind = backend.CompileInputObject
	InputExecutable CompileInputKind = backend.CompileInputExecutable
)

const (
	Source      CompileOutputKind = backend.CompileSource
	Assembly    CompileOutputKind = backend.CompileAssembly
	MachineCode CompileOutputKind = backend.CompileMachineCode
	Object      CompileOutputKind = backend.CompileObject
	Executable  CompileOutputKind = backend.CompileExecutable
)

// Compile uses the same ModernFrontend and canonical UAST as Transpile.
// Native outputs are encoded in-process; Assembly is an optional rendering.
func Compile(source string, options CompileOptions) (CompileResult, error) {
	if options.InputKind != "" && options.InputKind != InputSource {
		return backend.CompileBinaryInput([]byte(source), options)
	}
	// SemanticProgram JSON is an official source representation.  Reuse the
	// same parser/validator/native pipeline as in-memory semantic programs;
	// never route it through a textual language frontend.
	if strings.EqualFold(options.SourceLanguage, "semantic") || strings.EqualFold(options.SourceLanguage, "uast") || strings.EqualFold(options.SourceLanguage, "sp") {
		var program *backend.SemanticProgram
		var err error
		if strings.EqualFold(options.SourceLanguage, "sp") {
			if len(source) >= 4 && source[:4] == "SPZ2" {
				program, err = backend.ParseSemanticSPZ([]byte(source))
			} else {
				program, err = backend.ParseSemanticSP([]byte(source))
			}
		} else {
			program, err = backend.ParseSemanticJSON([]byte(source))
		}
		if err != nil {
			return CompileResult{}, err
		}
		if options.OutputKind == Source {
			text, err := backend.EmitSemanticCompatibility(options.TargetLanguage, program)
			return CompileResult{Text: text, OutputKind: Source}, err
		}
		return backend.CompileMachine(program, options)
	}
	// Go has a structured native frontend for the executable subset.  Prefer it
	// here so the public compiler API does not silently route Go source through
	// the generic textual frontend (which cannot preserve native declarations,
	// fixed-width types, or entry-point metadata).  Unsupported native constructs
	// still use the established ModernFrontend as a compatibility path.
	if strings.EqualFold(options.SourceLanguage, "go") {
		if native, nativeErr := backend.LowerNativeGo("input.go", source); nativeErr == nil {
			if options.OutputKind == Source {
				text, err := backend.EmitSemanticCompatibility(options.TargetLanguage, native)
				return CompileResult{Text: text, OutputKind: Source}, err
			}
			return backend.CompileMachine(native, options)
		}
	}
	p, err := manytomany.Parse(options.SourceLanguage, source)
	if err != nil {
		return CompileResult{}, err
	}
	if options.OutputKind == Source {
		text, err := manytomany.Emit(options.TargetLanguage, p)
		return CompileResult{Text: text, OutputKind: Source}, err
	}
	return backend.CompileMachine(p.Semantic, options)
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

// SemanticSP exports a source program as the readable, lossless Semantic
// Programming transport format.
func SemanticSP(source, code string) ([]byte, error) {
	return manytomany.SemanticSP(source, code)
}

// TranspileSemanticSP imports the canonical SP transport and emits a target
// through the same UAST backend used by JSON and source APIs.
func TranspileSemanticSP(target string, data []byte) (string, error) {
	return manytomany.TranspileSemanticSP(target, data)
}

// ParseSemanticSP imports a readable SP document into the canonical program.
func ParseSemanticSP(data []byte) (*SemanticProgram, error) { return backend.ParseSemanticSP(data) }

// MarshalSemanticSP serializes a canonical program as readable SP.
func MarshalSemanticSP(program *SemanticProgram) ([]byte, error) {
	if program == nil {
		return nil, fmt.Errorf("nil semantic program")
	}
	return program.MarshalSemanticSP()
}

// MarshalSemanticSPZ returns the optional compressed SP transport.
func MarshalSemanticSPZ(program *SemanticProgram) ([]byte, error) {
	if program == nil {
		return nil, fmt.Errorf("nil semantic program")
	}
	return program.MarshalSemanticSPZ()
}

// ParseSemanticSPZ imports the optional compressed SP transport.
func ParseSemanticSPZ(data []byte) (*SemanticProgram, error) { return backend.ParseSemanticSPZ(data) }

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

// BinarySemanticJSON lifts an x86-64 assembly, machine-code, COFF, or PE32+
// input through the structured machine frontend and returns the same
// SemanticProgram JSON used by source-language inputs. Unsupported or
// ambiguous instructions fail closed instead of being guessed as source.
func BinarySemanticJSON(data []byte, options CompileOptions) ([]byte, error) {
	if options.InputKind == InputSource || options.InputKind == "" {
		return nil, fmt.Errorf("binary semantic JSON requires assembly, machine, object, or executable input")
	}
	program, err := backend.LiftBinaryInput(data, options)
	if err != nil {
		return nil, err
	}
	return program.MarshalSemanticJSON()
}

// DecompileSemanticJSON is the public decompilation entry point for binary
// and assembly inputs. It returns the canonical SemanticProgram JSON emitted
// by the structured machine frontend, without routing through a source parser.
func DecompileSemanticJSON(data []byte, options CompileOptions) ([]byte, error) {
	return BinarySemanticJSON(data, options)
}
