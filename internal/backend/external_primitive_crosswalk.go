package backend

// This file imports the external primitive workbook as evidence only.  It
// crosswalks that evidence against the existing lowering contracts and emits
// deterministic reports; it never creates a second IR or promotes a rule on
// names alone.

import (
	"crypto/sha256"
	_ "embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed external_primitive_evidence.json
var embeddedExternalPrimitiveEvidence []byte

type ExternalPrimitiveCrosswalkReport struct {
	InitialInsufficientEvidence int      `json:"initial_insufficient_evidence"`
	ExternalEvidenceRows        int      `json:"external_evidence_rows"`
	ExactCodeEvidenceRows       int      `json:"exact_code_evidence_rows"`
	CodeEvidenceRows            int      `json:"code_evidence_rows"`
	DocCandidateRows            int      `json:"doc_candidate_rows"`
	GapRowsWithExternalMatch    int      `json:"gap_rows_with_external_match"`
	GapRowsPromotedExact        int      `json:"gap_rows_promoted_exact"`
	NewDerivedPrimitives        int      `json:"new_derived_primitives"`
	NewGeneratedRecipes         int      `json:"new_generated_recipes"`
	NewParameterizedMappings    int      `json:"new_parameterized_kernel_mappings"`
	NewTargetTerminalProofs     int      `json:"new_target_terminal_proofs"`
	TransitivelyClosedGaps      int      `json:"transitively_closed_gaps"`
	RemainingInsufficient       int      `json:"remaining_insufficient_evidence"`
	TotalGeneratedRecipes       int      `json:"total_generated_recipes"`
	TotalClosureRules           int      `json:"total_closure_reachable_rules"`
	TotalExecutorRules          int      `json:"total_executor_reachable_rules"`
	ExternalGapClosureRate      float64  `json:"external_gap_closure_rate"`
	Promoted                    []string `json:"promoted_exact_contracts"`
	Remaining                   []string `json:"remaining_contracts"`
	EvidenceSHA256              string   `json:"evidence_sha256"`
}

type externalEvidenceBundle map[string][]map[string]any

func loadExternalPrimitiveEvidence() (externalEvidenceBundle, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(embeddedExternalPrimitiveEvidence, &raw); err != nil {
		return nil, err
	}
	bundle := externalEvidenceBundle{}
	for name, payload := range raw {
		if name == "Summary" {
			continue
		}
		var rows []map[string]any
		if err := json.Unmarshal(payload, &rows); err != nil {
			return nil, fmt.Errorf("sheet %s: %w", name, err)
		}
		bundle[name] = rows
	}
	return bundle, nil
}

func externalValue(row map[string]any, key string) string {
	v := row[key]
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func evidenceTokens(value string) map[string]bool {
	value = strings.ToLower(value)
	for _, r := range []rune{'(', ')', ',', '|', ';', ':', '-', '_', '/', '<', '>', '$'} {
		value = strings.ReplaceAll(value, string(r), " ")
	}
	result := map[string]bool{}
	for _, token := range strings.Fields(value) {
		if len(token) > 1 {
			result[token] = true
		}
	}
	return result
}

func tokenOverlap(left, right string) bool {
	a, b := evidenceTokens(left), evidenceTokens(right)
	for token := range a {
		if b[token] {
			return true
		}
	}
	return false
}

func externalProofCounts(rows []map[string]any) (exact, code, doc int) {
	for _, row := range rows {
		switch strings.ToUpper(externalValue(row, "proof_state")) {
		case "EXACT_CODE":
			exact++
		case "CODE_EVIDENCE":
			code++
		case "DOC_CANDIDATE":
			doc++
		}
	}
	return
}

func externalKernelStatus(internal, external string) (string, string) {
	internal, external = strings.ToUpper(strings.TrimSpace(internal)), strings.ToUpper(strings.TrimSpace(external))
	if internal == external && internal != "" {
		return "EXACT_EQUIVALENT", "identical canonical kernel"
	}
	// These mappings are semantic subsets, not name aliases.  They are kept
	// deliberately small and are parameterized by the existing kernel family.
	switch {
	case internal == "CONSTANT" && external == "LITERAL":
		return "PARAMETER_EQUIVALENT", "literal type/value parameters"
	case internal == "CONTROL" && external == "CONTROL_TRANSFER":
		return "SUBSET", "external kernel covers control-transfer subset"
	case internal == "COLLECTION" && external == "CONTAINER_MAP":
		return "SUBSET", "external kernel covers container mapping subset"
	default:
		return "UNKNOWN", "no exact semantic kernel proof"
	}
}

func externalGapMatch(gap PrimitiveInventoryRecord, rule UniversalLoweringRule, candidate map[string]any, eq map[string]any) (semantic, typ, effect, evaluation, representation, target bool, promoted bool) {
	right := strings.Join([]string{externalValue(candidate, "primitive_id"), externalValue(candidate, "family"), externalValue(candidate, "kernel_class"), externalValue(candidate, "rewrite_candidate"), externalValue(candidate, "guard_summary")}, " ")
	if eq != nil {
		right = strings.Join([]string{right, externalValue(eq, "lhs_primitive"), externalValue(eq, "rhs_recipe"), externalValue(eq, "required_kernel"), externalValue(eq, "guards")}, " ")
	}
	semantic = strings.EqualFold(externalValue(candidate, "primitive_id"), gap.ID) || strings.EqualFold(externalValue(eq, "lhs_primitive"), gap.ID) || tokenOverlap(rule.SourceSemantic, right)
	if !semantic {
		return
	}
	for _, req := range rule.RequiredTypes {
		if !tokenOverlap(req, right) {
			return semantic, false, false, false, false, false, false
		}
	}
	typ = true
	for _, req := range rule.RequiredEffects {
		if !tokenOverlap(req, right) {
			return semantic, typ, false, false, false, false, false
		}
	}
	effect = true
	for _, req := range rule.RequiredContracts {
		if !tokenOverlap(req, right) {
			return semantic, typ, effect, false, false, false, false
		}
	}
	evaluation = true
	representation = strings.Contains(strings.ToLower(right), "representation") || len(rule.RequiredContracts) == 0
	target = strings.TrimSpace(externalValue(candidate, "target_scope")) == "" || strings.EqualFold(externalValue(candidate, "target_scope"), "Universal") || strings.EqualFold(externalValue(candidate, "target_scope"), "Generic")
	proof := strings.ToUpper(externalValue(candidate, "proof_state"))
	if eq != nil && strings.ToUpper(externalValue(eq, "proof_state")) == "EXACT_CODE" {
		proof = "EXACT_CODE"
	}
	promoted = semantic && typ && effect && evaluation && representation && target && proof == "EXACT_CODE"
	return
}

// WriteExternalPrimitiveCrosswalkReport crosswalks the embedded workbook
// against the existing contract-gap and primitive registries.
func WriteExternalPrimitiveCrosswalkReport(out string) (*ExternalPrimitiveCrosswalkReport, error) {
	bundle, err := loadExternalPrimitiveEvidence()
	if err != nil {
		return nil, err
	}
	internal, err := CompileUniversalPrimitiveSpecs()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return nil, err
	}
	write := func(name string, header []string, rows [][]string) error {
		f, err := os.Create(filepath.Join(out, name))
		if err != nil {
			return err
		}
		defer f.Close()
		w := csv.NewWriter(f)
		if err := w.Write(header); err != nil {
			return err
		}
		if err := w.WriteAll(rows); err != nil {
			return err
		}
		w.Flush()
		return w.Error()
	}
	candidates := bundle["Primitive Candidates"]
	equivalences := bundle["Equivalence Rules"]
	conversions := bundle["Type Conversions"]
	guards := bundle["Guards"]
	targets := bundle["Target Basis Evidence"]
	exact, code, doc := externalProofCounts(candidates)
	gapRules := map[string]UniversalLoweringRule{}
	for _, rule := range UniversalLoweringRules() {
		gapRules[rule.ID] = rule
	}
	gaps := []PrimitiveInventoryRecord{}
	for _, record := range internal.InventoryRecords {
		if record.Status == "CONTRACT_GAP" {
			gaps = append(gaps, record)
		}
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].ID < gaps[j].ID })
	report := &ExternalPrimitiveCrosswalkReport{InitialInsufficientEvidence: len(gaps), ExternalEvidenceRows: len(candidates), ExactCodeEvidenceRows: exact, CodeEvidenceRows: code, DocCandidateRows: doc, TotalGeneratedRecipes: len(internal.Recipes), TotalClosureRules: len(internal.Recipes), TotalExecutorRules: len(internal.Recipes)}
	for _, gap := range gaps {
		rule := gapRules[gap.ID]
		matched := false
		promoted := false
		for _, candidate := range candidates {
			semantic, typ, effect, evaluation, representation, target, exactMatch := externalGapMatch(gap, rule, candidate, nil)
			if semantic {
				matched = true
				if exactMatch {
					promoted = true
				}
				_ = typ
				_ = effect
				_ = evaluation
				_ = representation
				_ = target
			}
		}
		for _, candidate := range candidates {
			for _, eq := range equivalences {
				semantic, typ, effect, evaluation, representation, target, exactMatch := externalGapMatch(gap, rule, candidate, eq)
				if semantic {
					matched = true
					if exactMatch {
						promoted = true
					}
					_ = typ
					_ = effect
					_ = evaluation
					_ = representation
					_ = target
				}
			}
		}
		if matched {
			report.GapRowsWithExternalMatch++
		}
		if promoted {
			report.GapRowsPromotedExact++
			report.Promoted = append(report.Promoted, gap.ID)
		} else {
			report.Remaining = append(report.Remaining, gap.ID)
		}
	}
	report.RemainingInsufficient = len(report.Remaining)
	if report.InitialInsufficientEvidence > 0 {
		report.ExternalGapClosureRate = float64(report.InitialInsufficientEvidence-report.RemainingInsufficient) / float64(report.InitialInsufficientEvidence)
	}
	// Gap crosswalk: retain every proof dimension, including rejected matches.
	gapRows := [][]string{}
	for _, gap := range gaps {
		rule := gapRules[gap.ID]
		best := map[string]any{}
		bestState := ""
		for _, candidate := range candidates {
			semantic, typ, effect, evaluation, representation, target, promoted := externalGapMatch(gap, rule, candidate, nil)
			if semantic {
				best = candidate
				bestState = externalValue(candidate, "proof_state")
				status := "CANDIDATE_UNVERIFIED"
				if promoted {
					status = "PROMOTE_TO_EXACT"
				}
				gapRows = append(gapRows, []string{gap.ID, gap.Reason, externalValue(candidate, "primitive_id"), externalValue(candidate, "rewrite_candidate"), bestState, fmt.Sprint(semantic), fmt.Sprint(typ), fmt.Sprint(effect), fmt.Sprint(evaluation), fmt.Sprint(representation), fmt.Sprint(target), status, ""})
				break
			}
		}
		if len(best) == 0 {
			gapRows = append(gapRows, []string{gap.ID, gap.Reason, "", "", "", "false", "false", "false", "false", "false", "false", "NO_MATCH", "no exact external semantic contract"})
		}
	}
	if err := write("gap_external_evidence_matrix.csv", []string{"gap_id", "internal_primitive", "external_matches", "best_external_match", "external_proof_state", "semantic_match", "type_match", "effect_match", "evaluation_match", "representation_match", "target_match", "promotion_status", "remaining_reason"}, gapRows); err != nil {
		return nil, err
	}
	// Kernel crosswalk uses only declared kernel families and explicit semantic
	// subset relationships; architecture-only rows are never promoted.
	kernelRows := [][]string{}
	for _, kernel := range internal.KernelClasses {
		seen := map[string]bool{}
		for _, candidate := range candidates {
			externalKernel := externalValue(candidate, "kernel_class")
			if externalKernel == "" || seen[externalKernel] {
				continue
			}
			status, reason := externalKernelStatus(kernel, externalKernel)
			seen[externalKernel] = true
			kernelRows = append(kernelRows, []string{kernel, externalKernel, status, reason, externalValue(candidate, "proof_state"), externalValue(candidate, "source_project"), externalValue(candidate, "source_url")})
		}
	}
	if err := write("internal_external_kernel_matrix.csv", []string{"internal_kernel", "external_kernel", "status", "reason", "proof_state", "source_project", "source_url"}, kernelRows); err != nil {
		return nil, err
	}
	conversionRows := [][]string{}
	for _, row := range conversions {
		conversionRows = append(conversionRows, []string{externalValue(row, "source_type"), externalValue(row, "target_type"), externalValue(row, "target"), externalValue(row, "conversion_family"), externalValue(row, "guard"), externalValue(row, "proof_state"), "CANDIDATE_UNVERIFIED", externalValue(row, "source_url")})
	}
	if err := write("external_type_conversion_crosswalk.csv", []string{"source_type", "target_type", "target", "conversion_family", "guard", "proof_state", "promotion_status", "source_url"}, conversionRows); err != nil {
		return nil, err
	}
	guardRows := [][]string{}
	for _, row := range guards {
		guardRows = append(guardRows, []string{externalValue(row, "kernel_or_primitive"), externalValue(row, "guard_axis"), externalValue(row, "required_fact"), externalValue(row, "meaning"), externalValue(row, "proof_state"), externalValue(row, "source_url")})
	}
	if err := write("external_guard_crosswalk.csv", []string{"kernel_or_primitive", "guard_axis", "required_fact", "meaning", "proof_state", "source_url"}, guardRows); err != nil {
		return nil, err
	}
	targetRows := [][]string{}
	for _, row := range targets {
		targetRows = append(targetRows, []string{externalValue(row, "target"), externalValue(row, "kernel_or_terminal"), externalValue(row, "parameters_or_shape"), externalValue(row, "proof_state"), "CANDIDATE_UNVERIFIED", externalValue(row, "source_url")})
	}
	if err := write("external_target_basis_crosswalk.csv", []string{"target", "kernel_or_terminal", "parameters_or_shape", "proof_state", "promotion_status", "source_url"}, targetRows); err != nil {
		return nil, err
	}
	promotedRows := [][]string{}
	for _, id := range report.Promoted {
		promotedRows = append(promotedRows, []string{id, "external exact contract", "generated from existing primitive graph", "PROMOTED_EXACT"})
	}
	if err := write("promoted_exact_contracts.csv", []string{"gap_id", "evidence", "recipe_basis", "status"}, promotedRows); err != nil {
		return nil, err
	}
	candidateRows := [][]string{}
	for _, row := range candidates {
		state := strings.ToUpper(externalValue(row, "proof_state"))
		if state != "EXACT_CODE" || len(report.Promoted) == 0 {
			candidateRows = append(candidateRows, []string{externalValue(row, "primitive_id"), externalValue(row, "kernel_class"), externalValue(row, "rewrite_candidate"), state, externalValue(row, "source_project"), externalValue(row, "source_url")})
		}
	}
	if err := write("candidate_unverified_contracts.csv", []string{"primitive_id", "kernel_class", "rewrite_candidate", "proof_state", "source_project", "source_url"}, candidateRows); err != nil {
		return nil, err
	}
	if err := write("new_generated_recipes.csv", []string{"gap_id", "recipe", "status"}, nil); err != nil {
		return nil, err
	}
	deltaRows := [][]string{{"initial_insufficient", fmt.Sprint(report.InitialInsufficientEvidence)}, {"promoted_exact", fmt.Sprint(report.GapRowsPromotedExact)}, {"remaining_insufficient", fmt.Sprint(report.RemainingInsufficient)}}
	if err := write("closure_delta.csv", []string{"metric", "value"}, deltaRows); err != nil {
		return nil, err
	}
	remainingRows := [][]string{}
	for _, id := range report.Remaining {
		remainingRows = append(remainingRows, []string{id, "no complete internal/external contract proof", "CANDIDATE_UNVERIFIED or NO_MATCH"})
	}
	if err := write("remaining_insufficient_evidence.csv", []string{"gap_id", "reason", "status"}, remainingRows); err != nil {
		return nil, err
	}
	sha := sha256.Sum256(embeddedExternalPrimitiveEvidence)
	report.EvidenceSHA256 = hex.EncodeToString(sha[:])
	data, _ := json.MarshalIndent(report, "", "  ")
	data = append([]byte("{\n  \"generated\": \"DO NOT EDIT\",\n"), data[1:]...)
	if err := os.WriteFile(filepath.Join(out, "summary.json"), data, 0644); err != nil {
		return nil, err
	}
	return report, nil
}
