package backend

import "fmt"

const (
	nativeCapabilityException = "native:exception"
	nativeCapabilitySyntax    = "native:syntax"
	nativeCapabilityTemplate  = "native:template"
)

// NativeEmitterCapabilityDoc provides the shared backend representation for
// the three previously runtime-terminal capabilities. It is layout-only and
// contains no UAST or program semantics.
func NativeEmitterCapabilityDoc(target, capability string, spec TargetSpec, body Doc) (Doc, error) {
	if body == nil {
		return nil, fmt.Errorf("native capability %s requires body", capability)
	}
	switch capability {
	case nativeCapabilitySyntax, nativeCapabilityTemplate:
		return body, nil
	case nativeCapabilityException:
		// Exception lowering is represented by the target's declarative token;
		// the surrounding control/body remains supplied by the recipe.
		token := spec.SyntaxTokens["keyword.panic"]
		if token == "" {
			token = spec.SyntaxTokens["keyword.throw"]
		}
		if token == "" {
			return nil, fmt.Errorf("target %s has no native exception token", target)
		}
		return DocConcat{Parts: []Doc{DocText{Text: token + " "}, body}}, nil
	default:
		return nil, fmt.Errorf("unknown native emitter capability %s", capability)
	}
}
