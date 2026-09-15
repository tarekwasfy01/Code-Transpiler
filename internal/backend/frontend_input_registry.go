// Copyright (c) 2026 Tarek Wasfy
package backend

// FrontendInputKind separates evidence extractors at the input boundary.
// All successful extractors still return the existing SemanticProgram/UAST;
// this registry is not a second intermediate representation.
type FrontendInputKind string

const (
	FrontendInputSource     FrontendInputKind = "source"
	FrontendInputAssembly   FrontendInputKind = "assembly"
	FrontendInputMachine    FrontendInputKind = "machine_code"
	FrontendInputObject     FrontendInputKind = "object"
	FrontendInputExecutable FrontendInputKind = "executable"
)

func SupportedFrontendInputKinds() []FrontendInputKind {
	return []FrontendInputKind{FrontendInputSource, FrontendInputAssembly, FrontendInputMachine, FrontendInputObject, FrontendInputExecutable}
}

// FrontendInputUsesSemanticLift identifies non-source inputs that must pass
// through the existing binary/assembly semantic lift before UAST projection.
func FrontendInputUsesSemanticLift(kind FrontendInputKind) bool {
	return kind != FrontendInputSource
}
