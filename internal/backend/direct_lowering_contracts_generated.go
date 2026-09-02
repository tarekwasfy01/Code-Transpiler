package backend

// DirectLoweringContract is a checked product-path registration. It stores no
// program data and is deliberately narrower than a projection form: a target
// may use the generic syntax recipe only after the named native semantic
// handler and its proof gate are registered.
type DirectLoweringContract struct {
	Target          string
	ProjectionClass string
	Primitive       string
	Handler         string
	RecipeIDs       []string
	TemplateID      string
	ProofStatus     string
}

// generatedDirectLoweringContracts is generated from the proven direct
// lowering matrix. It is intentionally empty until a native semantic handler
// passes the roundtrip and runtime-differential gates; target syntax alone is
// never a direct-lowering proof.
var generatedDirectLoweringContracts = []DirectLoweringContract{}

func DirectLoweringContractFor(target, projectionClass string) (DirectLoweringContract, bool) {
	for _, contract := range generatedDirectLoweringContracts {
		if contract.Target == target && contract.ProjectionClass == projectionClass && contract.ProofStatus == "PROVEN" && contract.Handler != "" {
			return contract, true
		}
	}
	return DirectLoweringContract{}, false
}
