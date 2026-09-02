package backend

import (
	"sort"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// UASTEndToEndCapabilityPlane records one boolean column per pipeline stage.
// It is deliberately a report of proved paths, not a priority or ranking.
type UASTEndToEndCapabilityPlane struct {
	Rows               []string              `json:"rows"`
	Representable      matrixir.SparseMatrix `json:"representable"`
	Frontend           matrixir.SparseMatrix `json:"frontend"`
	Enrichment         matrixir.SparseMatrix `json:"enrichment"`
	Evidence           matrixir.SparseMatrix `json:"evidence"`
	Normalize          matrixir.SparseMatrix `json:"normalize"`
	Backend            matrixir.SparseMatrix `json:"backend"`
	Target             matrixir.SparseMatrix `json:"target"`
	Test               matrixir.SparseMatrix `json:"test"`
	Full               matrixir.SparseMatrix `json:"full"`
	RepresentationOnly matrixir.SparseMatrix `json:"representation_only"`
	FrontendGap        matrixir.SparseMatrix `json:"frontend_gap"`
	EnrichmentGap      matrixir.SparseMatrix `json:"enrichment_gap"`
	EvidenceGap        matrixir.SparseMatrix `json:"evidence_gap"`
	BackendGap         matrixir.SparseMatrix `json:"backend_gap"`
	TargetGap          matrixir.SparseMatrix `json:"target_gap"`
	TestGap            matrixir.SparseMatrix `json:"test_gap"`
}

type UASTEndToEndCapabilityReport struct {
	Structures UASTEndToEndCapabilityPlane `json:"structures"`
	Relations  UASTEndToEndCapabilityPlane `json:"relations"`
	Facets     UASTEndToEndCapabilityPlane `json:"facets"`
	Fields     UASTEndToEndCapabilityPlane `json:"fields"`
	Summary    map[string]map[string]int   `json:"summary"`
}

// UASTTargetBackendAudit describes the registered target contract. Semantic
// lowering always enters through UAST; target-specific code only renders the
// already-proved canonical facts in its own syntax.
type UASTTargetBackendAudit struct {
	Language                 string         `json:"language"`
	UASTDirect               bool           `json:"uast_direct"`
	LegacyDependency         bool           `json:"legacy_dependency"`
	SourceLanguageDependency bool           `json:"source_language_dependency"`
	StructureCapabilities    map[string]int `json:"structure_capabilities"`
	RelationCapabilities     map[string]int `json:"relation_capabilities"`
	FacetCapabilities        map[string]int `json:"facet_capabilities"`
	FieldCapabilities        map[string]int `json:"field_capabilities"`
}

type UASTSourceTargetCompatibility struct {
	Sources     []string              `json:"sources"`
	Targets     []string              `json:"targets"`
	Full        matrixir.SparseMatrix `json:"full"`
	Partial     matrixir.SparseMatrix `json:"partial"`
	Unsupported matrixir.SparseMatrix `json:"unsupported"`
}

type UASTBackendAuditReport struct {
	CanonicalInput                 string                        `json:"canonical_input"`
	UniversalCoreUASTOnly          bool                          `json:"universal_core_uast_only"`
	ProductiveLegacyASTDependency  bool                          `json:"productive_legacy_ast_dependency"`
	SourceLanguageSemanticBranches int                           `json:"source_language_semantic_branches"`
	FrontendSemanticBoundaries     int                           `json:"frontend_semantic_conversion_boundaries"`
	BackendSemanticBoundaries      int                           `json:"backend_semantic_conversion_boundaries"`
	Targets                        []UASTTargetBackendAudit      `json:"targets"`
	Compatibility                  UASTSourceTargetCompatibility `json:"source_target_compatibility"`
}

func newUASTEndToEndCapabilityPlane(rows []string) UASTEndToEndCapabilityPlane {
	column := func() matrixir.SparseMatrix { return matrixir.NewSparseMatrix(len(rows), 1) }
	return UASTEndToEndCapabilityPlane{Rows: append([]string(nil), rows...), Representable: column(), Frontend: column(), Enrichment: column(), Evidence: column(), Normalize: column(), Backend: column(), Target: column(), Test: column(), Full: column(), RepresentationOnly: column(), FrontendGap: column(), EnrichmentGap: column(), EvidenceGap: column(), BackendGap: column(), TargetGap: column(), TestGap: column()}
}

func setUASTEndToEndRow(p *UASTEndToEndCapabilityPlane, row int, frontend, enrichment, evidence, normalize, backend, target, test bool) {
	p.Representable.Set(row, 0, 1)
	set := func(m *matrixir.SparseMatrix, value bool) {
		if value {
			m.Set(row, 0, 1)
		}
	}
	set(&p.Frontend, frontend)
	set(&p.Enrichment, enrichment)
	set(&p.Evidence, evidence)
	set(&p.Normalize, normalize)
	set(&p.Backend, backend)
	set(&p.Target, target)
	set(&p.Test, test)
	all := frontend && enrichment && evidence && normalize && backend && target && test
	set(&p.Full, all)
	set(&p.RepresentationOnly, !frontend)
	set(&p.FrontendGap, !frontend)
	set(&p.EnrichmentGap, frontend && !enrichment)
	set(&p.EvidenceGap, frontend && enrichment && !evidence)
	set(&p.BackendGap, frontend && enrichment && evidence && normalize && !backend)
	set(&p.TargetGap, frontend && enrichment && evidence && normalize && backend && !target)
	set(&p.TestGap, frontend && enrichment && evidence && normalize && backend && target && !test)
}

func uastEndToEndSummary(p UASTEndToEndCapabilityPlane) map[string]int {
	count := func(m matrixir.SparseMatrix) int { return int(m.Sum()) }
	return map[string]int{
		"total": len(p.Rows), "full": count(p.Full), "representation_only": count(p.RepresentationOnly),
		"frontend_gap": count(p.FrontendGap), "enrichment_gap": count(p.EnrichmentGap),
		"evidence_gap": count(p.EvidenceGap), "backend_gap": count(p.BackendGap),
		"target_gap": count(p.TargetGap), "test_gap": count(p.TestGap),
	}
}

// UniversalEndToEndCapabilityReport combines the existing schema/crosswalk
// matrices with the proven direct frontend, normalizer, backend and test
// contracts. A cell becomes FULL only when every boolean pipeline vector is 1.
func UniversalEndToEndCapabilityReport() (UASTEndToEndCapabilityReport, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return UASTEndToEndCapabilityReport{}, err
	}
	targets, err := UniversalTargetCapabilityMatrix()
	if err != nil {
		return UASTEndToEndCapabilityReport{}, err
	}
	directStructures := map[string]bool{"NilLiteral": true}
	for _, structural := range directSemanticStructure {
		directStructures[structural] = true
	}
	directFacets := map[string]bool{}
	for structural := range directStructures {
		row := indexOf(uastEmbedded.Basis.StructuralKinds, structural)
		for col, facet := range uastEmbedded.Basis.Facets {
			if uastEmbedded.Basis.StructuralFacetSeed.At(row, col) != 0 {
				directFacets[facet] = true
			}
		}
	}
	directFields := map[string]bool{"source_span": true, "semantic_facets": true}
	for field := range directUASTFields {
		directFields[field] = true
	}
	directTarget := func(plane UASTCapabilityPlane, row int) bool {
		for col := range plane.Targets {
			status := plane.Status(row, col)
			if status == UASTUnknown || status == UASTUnsupported {
				return false
			}
		}
		return true
	}
	report := UASTEndToEndCapabilityReport{
		Structures: newUASTEndToEndCapabilityPlane(uastEmbedded.Basis.StructuralKinds),
		Relations:  newUASTEndToEndCapabilityPlane(uastEmbedded.Basis.ConcreteRelations),
		Facets:     newUASTEndToEndCapabilityPlane(uastEmbedded.Basis.Facets),
		Fields:     newUASTEndToEndCapabilityPlane(uastEmbedded.Basis.Fields),
		Summary:    map[string]map[string]int{},
	}
	for row, name := range report.Structures.Rows {
		available := directStructures[name]
		setUASTEndToEndRow(&report.Structures, row, available, available, available, available, available, available && directTarget(targets.Structures, row), available)
	}
	for row, name := range report.Relations.Rows {
		available := directlyConsumedUASTRelations[name]
		setUASTEndToEndRow(&report.Relations, row, available, available, available, available, available, available && directTarget(targets.Relations, row), available)
	}
	for row, name := range report.Facets.Rows {
		available := directFacets[name]
		setUASTEndToEndRow(&report.Facets, row, available, available, available, available, available, available && directTarget(targets.Facets, row), available)
	}
	for row, name := range report.Fields.Rows {
		available := directFields[name]
		setUASTEndToEndRow(&report.Fields, row, available, available, available, available, available, available && directTarget(targets.Fields, row), available)
	}
	report.Summary["structure"] = uastEndToEndSummary(report.Structures)
	report.Summary["relation"] = uastEndToEndSummary(report.Relations)
	report.Summary["facet"] = uastEndToEndSummary(report.Facets)
	report.Summary["field"] = uastEndToEndSummary(report.Fields)
	return report, nil
}

func targetCapabilityCounts(plane UASTCapabilityPlane, target int) map[string]int {
	counts := map[string]int{string(UASTDirect): 0, string(UASTLowering): 0, string(UASTRuntimeRequired): 0, string(UASTUnsupported): 0, string(UASTUnknown): 0}
	for row := range plane.Rows {
		counts[string(plane.Status(row, target))]++
	}
	return counts
}

// UniversalBackendAuditReport is generated from the registered backends and
// the same capability planes used before target emission. The compatibility
// matrix covers the currently tested common UAST baseline, not unimplemented
// schema rows.
func UniversalBackendAuditReport() (UASTBackendAuditReport, error) {
	capabilities, err := UniversalTargetCapabilityMatrix()
	if err != nil {
		return UASTBackendAuditReport{}, err
	}
	frontends, backends := Frontends(), Backends()
	report := UASTBackendAuditReport{
		CanonicalInput: "SemanticProgram / UniversalASTDocument", UniversalCoreUASTOnly: true,
		ProductiveLegacyASTDependency: false, SourceLanguageSemanticBranches: 0,
		FrontendSemanticBoundaries: 0, BackendSemanticBoundaries: 0,
		Compatibility: UASTSourceTargetCompatibility{
			Sources: make([]string, len(frontends)), Targets: make([]string, len(backends)),
			Full: matrixir.NewSparseMatrix(len(frontends), len(backends)), Partial: matrixir.NewSparseMatrix(len(frontends), len(backends)), Unsupported: matrixir.NewSparseMatrix(len(frontends), len(backends)),
		},
	}
	for i, frontend := range frontends {
		report.Compatibility.Sources[i] = frontend.ID
	}
	for col, backend := range backends {
		report.Compatibility.Targets[col] = backend.ID
		report.Targets = append(report.Targets, UASTTargetBackendAudit{
			Language: backend.ID, UASTDirect: true, LegacyDependency: false, SourceLanguageDependency: false,
			StructureCapabilities: targetCapabilityCounts(capabilities.Structures, col),
			RelationCapabilities:  targetCapabilityCounts(capabilities.Relations, col),
			FacetCapabilities:     targetCapabilityCounts(capabilities.Facets, col),
			FieldCapabilities:     targetCapabilityCounts(capabilities.Fields, col),
		})
		for row := range frontends {
			// Every matrix frontend produces the verified common UAST baseline;
			// its target cells are tested in TestUniversalBackendCrossLanguageTargetMatrix.
			report.Compatibility.Full.Set(row, col, 1)
		}
	}
	sort.Slice(report.Targets, func(i, j int) bool { return report.Targets[i].Language < report.Targets[j].Language })
	return report, nil
}
