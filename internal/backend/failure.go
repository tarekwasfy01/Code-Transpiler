package backend

import "errors"

// FailureClass is the stable, machine-readable classification exposed at the
// public transpilation boundary.  Internal parser/compiler text is retained
// only as Cause and never used to choose a route.
type FailureClass string

const (
	FailureSourceSyntax        FailureClass = "SOURCE_SYNTAX_ERROR"
	FailureFrontendParse       FailureClass = "FRONTEND_PARSE_BUG"
	FailureFrontendSemantic    FailureClass = "FRONTEND_SEMANTIC_GAP"
	FailureLegacyParserEscape  FailureClass = "LEGACY_PARSER_ESCAPE"
	FailureLegacyBackendEscape FailureClass = "LEGACY_BACKEND_ESCAPE"
	FailureUASTValidation      FailureClass = "UAST_VALIDATION_FAILED"
	FailureDirectUnavailable   FailureClass = "DIRECT_NATIVE_UNAVAILABLE"
	FailureLowering            FailureClass = "LOWERING_INVALID_UAST"
	FailureIntermediate        FailureClass = "INTERMEDIATE_ROUTE_FAILED"
	FailureRuntime             FailureClass = "RUNTIME_ROUTE_FAILED"
	FailureRuntimeDisabled     FailureClass = "RUNTIME_DISABLED"
	FailureTargetEmit          FailureClass = "TARGET_RENDER_FAILED"
	FailureTargetReparse       FailureClass = "TARGET_REPARSE_FAILED"
	FailureRegexParserEscape   FailureClass = "REGEX_PARSER_ESCAPE"
	FailureTextParserEscape    FailureClass = "TEXT_PARSER_ESCAPE"
	FailurePostFrontendReparse FailureClass = "POST_FRONTEND_REPARSE_ESCAPE"
	FailureInternal            FailureClass = "INTERNAL_BUG"
	FailureUnknown             FailureClass = "UNKNOWN_FAILURE"
)

// TranspileFailure is the typed error crossing the public source-to-target
// boundary.  Callers can inspect Class without parsing diagnostic strings.
type TranspileFailure struct {
	Class  FailureClass
	Stage  string
	Source string
	Target string
	Reason string
	Cause  error
}

func (f *TranspileFailure) Error() string {
	if f == nil {
		return string(FailureUnknown)
	}
	if f.Stage != "" {
		return string(f.Class) + " (stage=" + f.Stage + ")"
	}
	return string(f.Class)
}

func (f *TranspileFailure) Unwrap() error {
	if f == nil {
		return nil
	}
	return f.Cause
}

// NewTranspileFailure wraps an internal cause without making the raw message
// part of routing or classification.  The cause remains available to
// diagnostics and tests via errors.Unwrap/As.
func NewTranspileFailure(class FailureClass, stage, source, target string, cause error) error {
	if cause == nil {
		return &TranspileFailure{Class: class, Stage: stage, Source: source, Target: target}
	}
	return &TranspileFailure{Class: class, Stage: stage, Source: source, Target: target, Reason: cause.Error(), Cause: cause}
}

func FailureClassOf(err error) FailureClass {
	var failure *TranspileFailure
	if errors.As(err, &failure) && failure != nil && failure.Class != "" {
		return failure.Class
	}
	return FailureUnknown
}
