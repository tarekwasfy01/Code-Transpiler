package backend

import "sort"

type UASTDirectExecutionItem struct {
	Category                   string   `json:"category"`
	Name                       string   `json:"name"`
	Projected                  bool     `json:"projected"`
	CurrentlyDirect            bool     `json:"currently_direct"`
	CurrentlyViaLegacyAdapter  bool     `json:"currently_via_legacy_adapter"`
	RepresentableNotExecutable bool     `json:"representable_not_executable"`
	StoredOnly                 bool     `json:"stored_only,omitempty"`
	DirectTargets              []string `json:"direct_targets,omitempty"`
	LowerableTargets           []string `json:"lowerable_targets,omitempty"`
	RuntimeRequiredTargets     []string `json:"runtime_required_targets,omitempty"`
	UnsupportedTargets         []string `json:"unsupported_targets,omitempty"`
	UnknownTargets             []string `json:"unknown_targets,omitempty"`
}

type UASTBackendPath struct {
	Name     string `json:"name"`
	Mode     string `json:"mode"`
	Function string `json:"function"`
}

type UASTDirectExecutionReport struct {
	Schema             string                       `json:"schema"`
	BasisSHA256        string                       `json:"basis_sha256"`
	Summary            map[string]map[string]int    `json:"summary"`
	Items              []UASTDirectExecutionItem    `json:"items"`
	BackendPaths       []UASTBackendPath            `json:"backend_paths"`
	TargetCapabilities UASTTargetCapabilityMatrix   `json:"target_capabilities"`
	TargetPreservation UASTTargetPreservationMatrix `json:"target_preservation"`
	Execution          UASTExecutionAnalysis        `json:"execution"`
	EndToEnd           UASTEndToEndCapabilityReport `json:"end_to_end"`
	BackendAudit       UASTBackendAuditReport       `json:"backend_audit"`
}

func UniversalDirectExecutionReport() (UASTDirectExecutionReport, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return UASTDirectExecutionReport{}, err
	}
	capabilities, err := UniversalTargetCapabilityMatrix()
	if err != nil {
		return UASTDirectExecutionReport{}, err
	}
	endToEnd, err := UniversalEndToEndCapabilityReport()
	if err != nil {
		return UASTDirectExecutionReport{}, err
	}
	backendAudit, err := UniversalBackendAuditReport()
	if err != nil {
		return UASTDirectExecutionReport{}, err
	}
	execution, err := UniversalExecutionAnalysis()
	if err != nil {
		return UASTDirectExecutionReport{}, err
	}
	preservation, err := UniversalTargetPreservationMatrix()
	if err != nil {
		return UASTDirectExecutionReport{}, err
	}
	report := UASTDirectExecutionReport{
		Schema: "code-transpiler.uast-direct-execution.v1", BasisSHA256: uastEmbedded.BasisSHA256,
		Summary: map[string]map[string]int{}, TargetCapabilities: capabilities, TargetPreservation: preservation, Execution: execution, EndToEnd: endToEnd, BackendAudit: backendAudit,
		BackendPaths: []UASTBackendPath{
			{Name: "canonical-validation", Mode: "direct-uast", Function: "newUASTExecutionGraph"},
			{Name: "embedded-runtime", Mode: "direct-uast", Function: "runState.uastBlock"},
			{Name: "legacy-body-api-view", Mode: "derived-legacy-view-after-direct-execution", Function: "refreshLegacyExecutableBodyView"},
			{Name: "r-output", Mode: "direct-uast", Function: "universalRSource"},
			{Name: "signature-validation", Mode: "direct-uast", Function: "validateDirectSignatureContracts"},
			{Name: "call-resolution", Mode: "direct-uast", Function: "validateDirectCallResolutions"},
			{Name: "typed-operations", Mode: "direct-uast", Function: "directTypedRequirements"},
			{Name: "generic-target-codegen", Mode: "direct-uast", Function: "generateTargetFromUniversal"},
			{Name: "function-flow", Mode: "direct-uast", Function: "AnalyzeSemanticFunctionFlows"},
		},
	}
	projectedStructures := map[string]bool{}
	for _, structural := range []string{"AggregateExpr", "AssignStmt", "BreakStmt", "CallExpr", "ClosureExpr", "ContinueStmt", "ForEachStmt", "IfStmt", "IndexExpr", "LiteralExpr", "LoopStmt", "NilLiteral", "OperationExpr", "ParameterDecl", "ReturnStmt", "Scope", "SymbolRef"} {
		projectedStructures[structural] = true
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
	targets := func(plane UASTCapabilityPlane, row int, status UASTExecutionStatus) []string {
		out := []string{}
		for col, target := range plane.Targets {
			if plane.Status(row, col) == status {
				out = append(out, target)
			}
		}
		return out
	}
	for row, name := range uastEmbedded.Basis.StructuralKinds {
		direct := directStructures[name]
		report.Items = append(report.Items, UASTDirectExecutionItem{Category: "structure", Name: name, Projected: projectedStructures[name], CurrentlyDirect: direct, RepresentableNotExecutable: !direct, DirectTargets: targets(capabilities.Structures, row, UASTDirect), LowerableTargets: targets(capabilities.Structures, row, UASTLowering), RuntimeRequiredTargets: targets(capabilities.Structures, row, UASTRuntimeRequired), UnsupportedTargets: targets(capabilities.Structures, row, UASTUnsupported), UnknownTargets: targets(capabilities.Structures, row, UASTUnknown)})
	}
	for row, name := range uastEmbedded.Basis.Facets {
		direct := directFacets[name]
		report.Items = append(report.Items, UASTDirectExecutionItem{Category: "facet", Name: name, Projected: direct, CurrentlyDirect: direct, RepresentableNotExecutable: !direct, DirectTargets: targets(capabilities.Facets, row, UASTDirect), LowerableTargets: targets(capabilities.Facets, row, UASTLowering), RuntimeRequiredTargets: targets(capabilities.Facets, row, UASTRuntimeRequired), UnsupportedTargets: targets(capabilities.Facets, row, UASTUnsupported), UnknownTargets: targets(capabilities.Facets, row, UASTUnknown)})
	}
	for _, name := range uastEmbedded.Basis.Fields {
		direct := directUASTFields[name] || name == "source_span" || name == "semantic_facets"
		report.Items = append(report.Items, UASTDirectExecutionItem{Category: "field", Name: name, Projected: direct, CurrentlyDirect: direct, RepresentableNotExecutable: !direct})
	}
	for row, name := range uastEmbedded.Basis.ConcreteRelations {
		projected, direct := projectedUASTRelations[name], directlyConsumedUASTRelations[name]
		report.Items = append(report.Items, UASTDirectExecutionItem{Category: "relation", Name: name, Projected: projected, CurrentlyDirect: direct, RepresentableNotExecutable: !projected, StoredOnly: projected && !direct, DirectTargets: targets(capabilities.Relations, row, UASTDirect), LowerableTargets: targets(capabilities.Relations, row, UASTLowering), RuntimeRequiredTargets: targets(capabilities.Relations, row, UASTRuntimeRequired), UnsupportedTargets: targets(capabilities.Relations, row, UASTUnsupported), UnknownTargets: targets(capabilities.Relations, row, UASTUnknown)})
	}
	sort.Slice(report.Items, func(i, j int) bool {
		if report.Items[i].Category == report.Items[j].Category {
			return report.Items[i].Name < report.Items[j].Name
		}
		return report.Items[i].Category < report.Items[j].Category
	})
	for _, item := range report.Items {
		if report.Summary[item.Category] == nil {
			report.Summary[item.Category] = map[string]int{}
		}
		s := report.Summary[item.Category]
		s["total"]++
		if item.Projected {
			s["projected"]++
		}
		if item.CurrentlyDirect {
			s["direct"]++
		}
		if item.CurrentlyViaLegacyAdapter {
			s["via_legacy_adapter"]++
		}
		if item.RepresentableNotExecutable {
			s["representable_not_executable"]++
		}
		if item.StoredOnly {
			s["stored_only"]++
		}
		for metric, values := range map[string][]string{"direct_target_items": item.DirectTargets, "lowerable": item.LowerableTargets, "runtime_required": item.RuntimeRequiredTargets, "unsupported_any_target": item.UnsupportedTargets, "unknown_any_target": item.UnknownTargets} {
			if len(values) > 0 {
				s[metric]++
				s[metric+"_cells"] += len(values)
			}
		}
	}
	return report, nil
}
