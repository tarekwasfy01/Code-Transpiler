package backend

// UniversalASTCanonicalCoverage describes the representability contract of
// the embedded UAST schema. It is deliberately separate from execution and
// target-preservation reports: a canonical facet can be represented before a
// frontend mapping is proven or a target lowering exists.
type UniversalASTCanonicalCoverage struct {
	Structures int `json:"structures"`
	Relations  int `json:"relations"`
	Facets     int `json:"facets"`
	Fields     int `json:"fields"`
	UASFReady  int `json:"uast_ready"`
}

// CanonicalUniversalASTCoverage returns the complete structural coverage of
// the canonical UAST basis. It fails if the embedded schema is malformed.
func CanonicalUniversalASTCoverage() (UniversalASTCanonicalCoverage, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return UniversalASTCanonicalCoverage{}, err
	}
	return UniversalASTCanonicalCoverage{
		Structures: len(uastEmbedded.Basis.StructuralKinds),
		Relations:  len(uastEmbedded.Basis.ConcreteRelations),
		Facets:     len(uastEmbedded.Basis.Facets),
		Fields:     len(uastEmbedded.Basis.Fields),
		UASFReady:  len(uastEmbedded.Basis.Facets),
	}, nil
}
