// Copyright (c) 2026 Tarek Wasfy
package manytomany

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// TranspileRequest is the single immutable input boundary shared by the GUI
// and CLI. Source is intentionally kept byte-for-byte: the core never strips
// BOMs, normalizes line endings, or rewrites editor text before parsing.
type TranspileRequest struct {
	Source, SourceLanguage, TargetLanguage, EntryPoint string
	// DisableRuntimeFallback is opt-in so existing CLI/API callers retain the
	// established NATIVE -> semantic-runtime -> ERROR order.
	DisableRuntimeFallback bool
}

// TranspileTrace makes a GUI/CLI divergence observable without exposing a
// second semantic representation.
type TranspileTrace struct {
	SourceLength                int      `json:"source_length"`
	SourceSHA256                string   `json:"source_sha256"`
	NormalizedSourceSHA256      string   `json:"normalized_source_sha256"`
	SourceLanguage              string   `json:"source_language"`
	TargetLanguage              string   `json:"target_language"`
	EntryPoint                  string   `json:"entrypoint"`
	UASTSHA256                  string   `json:"uast_hash,omitempty"`
	ProjectionMode              string   `json:"projection_mode,omitempty"`
	FinalSourceSHA256           string   `json:"final_source_sha256,omitempty"`
	ErrorClass                  string   `json:"error_class,omitempty"`
	NativeAttempt               bool     `json:"native_attempt"`
	RuntimeFallback             bool     `json:"runtime_fallback"`
	IntermediateRoute           string   `json:"intermediate_route,omitempty"`
	UniversalLoweringAttempt    bool     `json:"universal_lowering_attempt"`
	UniversalLoweringSuccess    bool     `json:"universal_lowering_success"`
	UniversalLoweringIterations int      `json:"universal_lowering_iterations,omitempty"`
	UniversalLoweringRules      []string `json:"universal_lowering_rules,omitempty"`
	UniversalLoweringResiduals  []string `json:"universal_lowering_residuals,omitempty"`
}

type TranspileResult struct {
	Code  string
	Trace TranspileTrace
}

func typedFailure(class backend.FailureClass, stage, source, target string, err error) error {
	return backend.NewTranspileFailure(class, stage, source, target, err)
}

// targetSyntaxError validates the emitted representation only. Parse(target,
// code) is intentionally not used here: it lowers the output as a new source
// program and can therefore reject valid target syntax merely because a small
// semantic witness contains an unresolved free symbol. Scope/type resolution
// belongs to the separately reported target-compile stage.
func targetSyntaxError(target, code string) error {
	check := backend.CheckTargetSyntax(target, code)
	if check.Checked && !check.Valid {
		return fmt.Errorf("target syntax failed: %s", check.Failure)
	}
	return nil
}

func sha256Text(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// TranspileCore is the only productive source-to-target path. Frontends call
// it with a copied request; Parse -> canonical UAST -> projector is therefore
// identical for GUI and CLI invocations.
func TranspileCore(request TranspileRequest) (result TranspileResult, retErr error) {
	source, target := normalize(request.SourceLanguage), normalize(request.TargetLanguage)
	trace := TranspileTrace{SourceLength: len(request.Source), SourceSHA256: sha256Text(request.Source), NormalizedSourceSHA256: sha256Text(request.Source), SourceLanguage: source, TargetLanguage: target, EntryPoint: request.EntryPoint, NativeAttempt: true}
	var semantic *backend.SemanticProgram
	var intermediate semanticIntermediateRoute
	// The miner supplies this path through the environment. It is intentionally
	// optional for normal API/GUI calls, and the sidecar is always derived from
	// the exact UAST that this productive invocation created.
	defer func() {
		path := os.Getenv("UAST_SEMANTIC_TRACE_OUT")
		if path == "" {
			return
		}
		route := semanticTraceRoute(trace, retErr, intermediate)
		_ = backend.WriteSemanticTrace(path, semantic != nil, universalASTOf(semantic), route)
	}()
	p, err := Parse(source, request.Source)
	if err != nil {
		wrapped := typedFailure(backend.FailureFrontendParse, "frontend", source, target, err)
		trace.ErrorClass = string(backend.FailureClassOf(wrapped))
		return TranspileResult{Trace: trace}, wrapped
	}
	semantic = p.Semantic
	if p.Semantic != nil {
		if wire, e := p.Semantic.MarshalSemanticJSON(); e == nil {
			trace.UASTSHA256 = sha256Text(string(wire))
		}
	}
	code, err := EmitDirect(target, p)
	// Direct projection is only a successful route when its target source can
	// enter the same modern frontend again.  This is a target-independent
	// legality guard: a renderer must not be allowed to commit a syntactically
	// invalid direct form merely because it produced non-empty text.  Mark it
	// unavailable so the existing universal lowering/intermediate/runtime
	// sequence can choose another representation of the *same* canonical UAST.
	if err == nil {
		if reparseErr := targetSyntaxError(target, code); reparseErr != nil {
			err = typedFailure(backend.FailureDirectUnavailable, "direct-reparse", source, target,
				fmt.Errorf("DIRECT_NATIVE_UNAVAILABLE: target reparse failed: %w", reparseErr))
		}
	}
	if err != nil {
		err = typedFailure(backend.FailureDirectUnavailable, "direct", source, target, err)
		trace.UniversalLoweringAttempt = true
		// C's ISO emitter cannot represent the private expression-binding
		// helpers used by the lowered direct path. Let the canonical compatibility
		// projector handle those documents instead of accepting a syntactically
		// invalid mixed RValue/double translation unit.
		if target != "c" {
			if loweredCode, loweringTrace, loweringErr := backend.EmitSemanticLoweredDirect(target, p.Semantic); loweringErr == nil {
				// Generated lowering recipes are productive only when their target
				// representation is legal. Keep the failed direct form out of the
				// result and let the existing later routes handle an invalid lowered
				// representation as well.
				if reparseErr := targetSyntaxError(target, loweredCode); reparseErr == nil {
					code = loweredCode
					err = nil
					trace.UniversalLoweringSuccess = true
					trace.UniversalLoweringIterations = loweringTrace.Iterations
					trace.UniversalLoweringRules = append([]string(nil), loweringTrace.Rules...)
					trace.UniversalLoweringResiduals = append([]string(nil), loweringTrace.Residuals...)
					trace.ProjectionMode = "lowered-native"
				}
			} else {
				trace.UniversalLoweringIterations = loweringTrace.Iterations
				trace.UniversalLoweringRules = append([]string(nil), loweringTrace.Rules...)
				trace.UniversalLoweringResiduals = append([]string(nil), loweringTrace.Residuals...)
			}
		}
		if err != nil && request.DisableRuntimeFallback {
			trace.ErrorClass = string(backend.FailureClassOf(err))
			return TranspileResult{Trace: trace}, err
		}
	}
	if err != nil {
		// Native and semantic-runtime emission are exhausted at this point.
		// Use the matrix-defined language set as a bounded, cycle-free
		// compatibility route: emit one intermediate target, parse that output
		// through the same frontend, then emit the requested target.  This is a
		// last resort and never replaces a successful direct/runtime result.
		viaCode, via, routeInfo, routeErr := transpileViaIntermediate(p, target)
		if routeErr != nil {
			// Only after all strict matrix routes are exhausted use the explicit
			// canonical-UAST compatibility projector as the last resort. Emit is
			// intentionally strict-native, so calling it here would make runtime
			// fallback unreachable for the CLI and GUI.
			code, err = backend.EmitSemanticCompatibility(target, p.Semantic)
			if err != nil {
				wrapped := typedFailure(backend.FailureRuntime, "runtime", source, target, err)
				trace.ErrorClass = string(backend.FailureClassOf(wrapped))
				return TranspileResult{Trace: trace}, wrapped
			}
			if reparseErr := targetSyntaxError(target, code); reparseErr != nil {
				wrapped := typedFailure(backend.FailureRuntime, "runtime-reparse", source, target,
					fmt.Errorf("compatibility target reparse failed: %w", reparseErr))
				trace.ErrorClass = string(backend.FailureClassOf(wrapped))
				return TranspileResult{Trace: trace}, wrapped
			}
		} else {
			code = viaCode
			trace.IntermediateRoute = via
			trace.ProjectionMode = "matrix-route"
			intermediate = routeInfo
		}
	}
	if request.DisableRuntimeFallback && backend.AnalyzeRuntimeTaint(code, nil).Tainted() {
		err = fmt.Errorf("RUNTIME_DISABLED: native target emission unavailable for %s", target)
		err = typedFailure(backend.FailureRuntimeDisabled, "runtime", source, target, err)
		trace.ErrorClass = string(backend.FailureClassOf(err))
		return TranspileResult{Trace: trace}, err
	}
	trace.FinalSourceSHA256 = sha256Text(code)
	trace.RuntimeFallback = backend.AnalyzeRuntimeTaint(code, nil).Tainted()
	if trace.RuntimeFallback {
		trace.ProjectionMode = "compatibility-runtime"
	} else if !trace.UniversalLoweringSuccess {
		trace.ProjectionMode = "native-first"
	}
	return TranspileResult{Code: code, Trace: trace}, nil
}

type semanticIntermediateRoute struct {
	Language, Route                                              string
	Leg1InputHash, Leg1OutputHash, Leg2InputHash, Leg2OutputHash string
	RootUASTHash, IntermediateUASTHash, FinalUASTHash            string
}

func universalASTOf(p *backend.SemanticProgram) *backend.UniversalASTDocument {
	if p == nil {
		return nil
	}
	return p.UniversalAST
}

func semanticTraceRoute(trace TranspileTrace, err error, via semanticIntermediateRoute) backend.SemanticTraceRoute {
	r := backend.SemanticTraceRoute{ProjectionMode: trace.ProjectionMode, RuntimeFallbackUsed: trace.RuntimeFallback, DirectSuccess: err == nil && !trace.UniversalLoweringSuccess && trace.IntermediateRoute == "" && !trace.RuntimeFallback, PrimitiveLoweringSuccess: trace.UniversalLoweringSuccess, IntermediateSuccess: trace.IntermediateRoute != "", IntermediateRoute: trace.IntermediateRoute, IntermediateLanguage: via.Language, Leg1InputHash: via.Leg1InputHash, Leg1OutputHash: via.Leg1OutputHash, Leg2InputHash: via.Leg2InputHash, Leg2OutputHash: via.Leg2OutputHash, RootUASTHash: via.RootUASTHash, IntermediateUASTHash: via.IntermediateUASTHash, FinalUASTHash: via.FinalUASTHash}
	switch {
	case err != nil:
		r.RouteType = "FAIL"
	case r.RuntimeFallbackUsed:
		r.RouteType = "RUNTIME"
	case r.IntermediateSuccess:
		r.RouteType = "INTERMEDIATE"
	case r.PrimitiveLoweringSuccess:
		r.RouteType = "PRIMITIVE_LOWERING"
	default:
		r.RouteType = "DIRECT"
	}
	caseID := os.Getenv("UAST_MINER_CASE_ID")
	if caseID != "" && r.IntermediateSuccess {
		r.Leg1CaseID, r.Leg2CaseID = caseID+":leg1", caseID+":leg2"
	}
	return r
}

// transpileViaIntermediate tries each available language exactly once as an
// intermediate representation.  It deliberately works on Program/Semantic
// values and never calls TranspileCore recursively, so routes cannot cycle or
// multiply indefinitely.
func transpileViaIntermediate(program Program, target string) (string, string, semanticIntermediateRoute, error) {
	for _, intermediate := range backend.IntermediateRouteCandidates(program.Source, target) {
		if intermediate == target || intermediate == program.Source {
			continue
		}
		// Prefer a fully native intermediate. If that target lacks one
		// operation, its existing compatibility rendering is still useful as a
		// bounded bridge: the generated target source is parsed once, then the
		// requested target is attempted natively. This is strictly before the
		// document-level runtime fallback and never recurses through TranspileCore.
		middle, err := EmitDirect(intermediate, program)
		routeKind := "native"
		if err != nil || middle == "" {
			middle, err = Emit(intermediate, program)
			routeKind = "compat"
		}
		if err != nil || middle == "" {
			continue
		}
		reparsed, err := Parse(intermediate, middle)
		if err != nil {
			continue
		}
		final, err := EmitDirect(target, reparsed)
		if err == nil && final != "" {
			if reparseErr := targetSyntaxError(target, final); reparseErr != nil {
				continue
			}
			info := semanticIntermediateRoute{Language: intermediate, Route: program.Source + "->" + intermediate + "->" + target + "(" + routeKind + ")", Leg1OutputHash: sha256Text(middle), Leg2InputHash: sha256Text(middle), Leg2OutputHash: sha256Text(final)}
			if program.Semantic != nil {
				if b, e := program.Semantic.MarshalSemanticJSON(); e == nil {
					info.RootUASTHash = sha256Text(string(b))
					info.Leg1InputHash = info.RootUASTHash
				}
			}
			if reparsed.Semantic != nil {
				if b, e := reparsed.Semantic.MarshalSemanticJSON(); e == nil {
					info.IntermediateUASTHash = sha256Text(string(b))
					info.FinalUASTHash = info.IntermediateUASTHash
				}
			}
			return final, info.Route, info, nil
		}
	}
	return "", "", semanticIntermediateRoute{}, fmt.Errorf("no intermediate route from %s to %s", program.Source, target)
}

// Language is both a supported source and target language.
var Languages = []string{"r", "go", "rust", "cpp", "c", "python", "zig", "julia", "nim", "csharp", "java", "kotlin", "swift"}

type IRKind string

const (
	IRAssign IRKind = "assign"
	IRPrint  IRKind = "print"
	IRReturn IRKind = "return"
	IRExpr   IRKind = "expr"
)

type Statement struct {
	Kind       IRKind // retained for diagnostics; emission uses Semantic
	Name       string
	Expr       string
	Semantic   matrixir.Vector
	MatrixNode int
}

type Program struct {
	Semantic     *backend.SemanticProgram
	Source       string
	Statements   []Statement
	Graph        *matrixir.Graph
	Requirements matrixir.Vector
	// CanonicalR is retained only as a compatibility diagnostic serialization.
	// Emit, function-flow and route fanout use Semantic exclusively.
	CanonicalR string
	Actions    matrixir.Matrix
	Grammar    matrixir.Vector
}

// Document returns the neutral interchange representation. No caller needs to
// read CanonicalR in order to pass a parsed program to another target.
func (p Program) Document() (backend.SemanticDocument, error) {
	if p.Semantic == nil {
		return backend.SemanticDocument{}, fmt.Errorf("program has no semantic representation")
	}
	return p.Semantic.Document()
}

// ParseDocument accepts only the versioned semantic interchange format. It is
// intentionally separate from Parse(source, code): source frontends are free
// to evolve, while transport between them is language-neutral.
func ParseDocument(data []byte) (Program, error) {
	semantic, err := backend.ParseSemanticJSON(data)
	if err != nil {
		return Program{}, err
	}
	// The verified semantic relation matrices already describe the program.
	// Importing JSON must not require an R serialization or reparsed R graph.
	return Program{Source: "semantic", Semantic: semantic}, nil
}

// Parse is the single source frontend boundary. Every supported source is
// lowered to a canonical UAST before any target renderer is selected.
func Parse(source, code string) (Program, error) {
	source = normalize(source)
	if !supported(source) {
		return Program{}, fmt.Errorf("unsupported source language %q", source)
	}
	// Every source, including generated intermediate source, enters the same
	// modern structured frontend.  DecodeGenerated is an explicit inverse
	// compatibility API and is intentionally not part of this product path.
	semantic, err := backend.LowerSource(source, "", code)
	if err != nil {
		return Program{}, err
	}
	// The canonical UAST is the productive representation for matrix
	// frontends.  RSource is an optional compatibility view and may not be
	// losslessly represent a structured construct (for example a closure with
	// an as-yet-unlowered body).  Do not reject an otherwise valid UAST merely
	// because that legacy view cannot be serialized.
	// Keep the MatrixIR compatibility serialization as the diagnostic view when
	// available. It is deliberately outside the productive UAST path, but it
	// preserves legacy range/control semantics for callers that still execute
	// Program.CanonicalR. Fall back to the UAST writer only for imported
	// semantic documents that have no compatibility payload.
	view := semantic.CompatibilityR
	if view == "" {
		view, _ = semantic.RSource(false)
	}
	return Program{Source: source, Semantic: semantic, CanonicalR: view}, nil
}

func Emit(target string, p Program) (string, error) {
	target = normalize(target)
	if !supported(target) {
		return "", fmt.Errorf("unsupported target %q", target)
	}
	if p.Semantic == nil || p.Semantic.UniversalAST == nil {
		return "", typedFailure(backend.FailureLegacyBackendEscape, "backend", p.Source, target,
			fmt.Errorf("canonical UAST is required; compatibility text emission is explicit-only"))
	}
	return backend.EmitSemantic(target, p.Semantic)
}

// EmitDirect requests one strict native projection without changing the
// normal Emit API, whose compatibility runtime fallback remains preserved.
func EmitDirect(target string, p Program) (string, error) {
	target = normalize(target)
	if !supported(target) {
		return "", fmt.Errorf("unsupported target %q", target)
	}
	if p.Semantic != nil {
		return backend.EmitSemanticDirect(target, p.Semantic)
	}
	return Emit(target, p)
}

func Transpile(source, target, code string) (string, error) {
	result, err := TranspileCore(TranspileRequest{Source: code, SourceLanguage: source, TargetLanguage: target, EntryPoint: "api"})
	return result.Code, err
}

// SemanticSP parses source through the normal frontend and serializes the
// resulting canonical SemanticProgram as readable SP.
func SemanticSP(source, code string) ([]byte, error) {
	p, err := Parse(source, code)
	if err != nil {
		return nil, err
	}
	return p.Semantic.MarshalSemanticSP()
}

// TranspileSemanticSP imports SP and emits the requested target using the
// existing SemanticProgram/UAST backend.
func TranspileSemanticSP(target string, data []byte) (string, error) {
	var p *backend.SemanticProgram
	var err error
	if len(data) >= 4 && string(data[:4]) == "SPZ2" {
		p, err = backend.ParseSemanticSPZ(data)
	} else {
		p, err = backend.ParseSemanticSP(data)
	}
	if err != nil {
		return "", err
	}
	return Emit(target, Program{Source: "semantic", Semantic: p})
}

func normalize(s string) string {
	return backend.NormalizeLanguage(s)
}
func supported(s string) bool {
	return backend.HasFrontend(s)
}
