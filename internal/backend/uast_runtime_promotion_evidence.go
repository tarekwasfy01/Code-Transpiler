package backend

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RuntimePromotionEvidence is an observational record. It is deliberately
// separate from UAST semantics and cannot promote a target capability by
// itself. Proof rows are added only by the existing roundtrip/compiler/runtime
// test gates.
type RuntimePromotionEvidence struct {
	Source, Target, ProjectionClass, UASF, SourceID, Package, SourceHash string
}

type RuntimePromotionEvidenceSummary struct {
	Schema                   string `json:"schema"`
	SourceRecordsRead        int    `json:"source_records_read"`
	RelevantRuntimeMatches   int    `json:"relevant_runtime_matches"`
	DirectCandidates         int    `json:"direct_candidates"`
	NoDirectCandidate        int    `json:"no_direct_candidate"`
	NewProvenDirectContracts int    `json:"new_proven_direct_contracts"`
	RuntimeCellsPromoted     int    `json:"runtime_cells_promoted"`
	MLCPDRows                int    `json:"mlcpd_rows"`
	EcosystemPackages        int    `json:"ecosystem_packages"`
	CrossSourceConfirmed     int    `json:"cross_source_confirmed"`
	EmpiricalSemanticProven  int    `json:"empirical_semantic_proven"`
	Rejected                 int    `json:"rejected"`
	Conflicts                int    `json:"conflicts"`
	DirectBefore             int    `json:"direct_before"`
	RuntimeBefore            int    `json:"runtime_before"`
	DirectAfter              int    `json:"direct_after"`
	RuntimeAfter             int    `json:"runtime_after"`
	MLCPDInputState          string `json:"mlcpd_input_state"`
}

// empiricalSemanticProofThreshold is deliberately expressed in independent
// package archive hashes. Repeated observations from one artifact count once.
// This confirms source semantics; a target is still marked DIRECT only after
// its actual projection contract is present and tested.
const empiricalSemanticProofThreshold = 3

func runtimePromotionClassesForFacet(registry UASTStructureProjectionRegistry, facet string) []string {
	classes := map[string]bool{}
	for _, contract := range registry.Contracts {
		row := indexOf(uastEmbedded.Basis.StructuralKinds, contract.StructureKind)
		col := indexOf(uastEmbedded.Basis.Facets, facet)
		if row >= 0 && col >= 0 && uastEmbedded.Basis.StructuralFacetSeed.At(row, col) != 0 {
			classes[contract.ProjectionClass] = true
		}
	}
	out := make([]string, 0, len(classes))
	for class := range classes {
		out = append(out, class)
	}
	sort.Strings(out)
	return out
}

// runtimePromotionFacetsForClass is the inverse matrix lookup used to state
// the exact impact of proving one target/projection-class contract. It does
// not infer semantics: a facet is included only when the existing structural
// facet seed maps it to that class and its current target cell is RUNTIME.
func runtimePromotionFacetsForClass(registry UASTStructureProjectionRegistry, target, class string, preservation UASTTargetPreservationMatrix) []string {
	col := indexOf(preservation.Targets, target)
	if col < 0 {
		return nil
	}
	set := map[string]bool{}
	for row, facet := range preservation.Capabilities {
		if preservation.Status(row, col) != PreservationRuntime {
			continue
		}
		for _, candidate := range runtimePromotionClassesForFacet(registry, facet) {
			if candidate == class {
				set[facet] = true
			}
		}
	}
	out := make([]string, 0, len(set))
	for facet := range set {
		out = append(out, facet)
	}
	sort.Strings(out)
	return out
}

func readPromotionCSV(path string) ([]map[string]string, error) {
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	header, err := r.Read()
	if err != nil {
		return nil, err
	}
	rows := []map[string]string{}
	for {
		values, err := r.Read()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		row := map[string]string{}
		for i, name := range header {
			if i < len(values) {
				row[name] = values[i]
			}
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func writePromotionCSV(path string, header []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}

// WriteRuntimePromotionEvidence imports only source-free evidence emitted by
// the ecosystem miner. Native observations are filtered through the current
// runtime matrix and projected with the existing UAST crosswalk. They remain
// candidates until a semantic roundtrip and/or runtime differential test
// proves the direct contract.
func WriteRuntimePromotionEvidence(out, ecosystemProvenance, ecosystemArtifacts string) (RuntimePromotionEvidenceSummary, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return RuntimePromotionEvidenceSummary{}, err
	}
	preservation, err := UniversalTargetPreservationMatrix()
	if err != nil {
		return RuntimePromotionEvidenceSummary{}, err
	}
	promotion, err := UniversalRuntimePromotionAnalysis()
	if err != nil {
		return RuntimePromotionEvidenceSummary{}, err
	}
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return RuntimePromotionEvidenceSummary{}, err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return RuntimePromotionEvidenceSummary{}, err
	}
	summary := RuntimePromotionEvidenceSummary{Schema: "code-transpiler.uast-runtime-promotion-evidence.v1", MLCPDInputState: "NOT_AVAILABLE"}
	for _, counts := range preservation.StatusCounts {
		summary.DirectBefore += counts[string(PreservationDirect)]
		summary.RuntimeBefore += counts[string(PreservationRuntime)]
	}
	summary.DirectAfter, summary.RuntimeAfter = summary.DirectBefore, summary.RuntimeBefore

	artifacts, err := readPromotionCSV(ecosystemArtifacts)
	if err != nil {
		return summary, err
	}
	artifactByID := map[string]map[string]string{}
	packages := map[string]bool{}
	for _, row := range artifacts {
		artifactByID[row["source_id"]] = row
		if row["status"] == "ok" {
			packages[row["source_id"]] = true
		}
	}
	summary.EcosystemPackages = len(packages)
	provenance, err := readPromotionCSV(ecosystemProvenance)
	if err != nil {
		return summary, err
	}
	evidence := []RuntimePromotionEvidence{}
	seen := map[string]bool{}
	for _, row := range provenance {
		summary.SourceRecordsRead++
		target, facet := strings.TrimSpace(row["language"]), strings.TrimSpace(row["canonical_semantic_id"])
		col, facetRow := indexOf(preservation.Targets, target), indexOf(preservation.Capabilities, facet)
		if col < 0 || facetRow < 0 || preservation.Status(facetRow, col) != PreservationRuntime {
			continue
		}
		for _, class := range runtimePromotionClassesForFacet(registry, facet) {
			if !promotion.RuntimeProjectionClass[target][class] {
				continue
			}
			artifact := artifactByID[row["source_id"]]
			entry := RuntimePromotionEvidence{Source: "ECOSYSTEM_PACKAGE", Target: target, ProjectionClass: class, UASF: facet, SourceID: row["source_id"], Package: artifact["name"], SourceHash: artifact["archive_sha256"]}
			key := strings.Join([]string{entry.Source, entry.Target, entry.ProjectionClass, entry.UASF, entry.SourceID, entry.SourceHash}, "|")
			if seen[key] {
				continue
			}
			seen[key] = true
			evidence = append(evidence, entry)
		}
	}
	sort.Slice(evidence, func(i, j int) bool {
		a, b := evidence[i], evidence[j]
		return strings.Join([]string{a.Target, a.ProjectionClass, a.UASF, a.SourceID}, "|") < strings.Join([]string{b.Target, b.ProjectionClass, b.UASF, b.SourceID}, "|")
	})
	summary.RelevantRuntimeMatches = len(evidence)
	evidenceRows := make([][]string, 0, len(evidence))
	for _, e := range evidence {
		evidenceRows = append(evidenceRows, []string{e.Source, e.Target, e.ProjectionClass, e.UASF, e.SourceID, e.Package, e.SourceHash, "NATIVE_OBSERVATION", "false", "false", "false", "false"})
	}
	header := []string{"source", "target", "projection_class", "canonical_semantic_id", "source_id", "package", "source_hash", "proof_level", "roundtrip_pass", "compiler_pass", "runtime_behavior_match", "conflict"}
	if err := writePromotionCSV(filepath.Join(out, "native_evidence.csv"), header, evidenceRows); err != nil {
		return summary, err
	}
	if err := writePromotionCSV(filepath.Join(out, "ecosystem_evidence.csv"), header, evidenceRows); err != nil {
		return summary, err
	}
	if err := writePromotionCSV(filepath.Join(out, "mlcpd_evidence.csv"), header, nil); err != nil {
		return summary, err
	}

	byCandidate := map[string][]RuntimePromotionEvidence{}
	for _, e := range evidence {
		byCandidate[e.Target+"|"+e.ProjectionClass] = append(byCandidate[e.Target+"|"+e.ProjectionClass], e)
	}
	keys := make([]string, 0, len(byCandidate))
	for key := range byCandidate {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	candidateRows, classRows, empiricalRows := [][]string{}, [][]string{}, [][]string{}
	for i, key := range keys {
		members := byCandidate[key]
		facets, hashes := map[string]bool{}, map[string]bool{}
		for _, e := range members {
			facets[e.UASF] = true
			if e.SourceHash != "" {
				hashes[e.SourceHash] = true
			}
		}
		facetList := make([]string, 0, len(facets))
		for facet := range facets {
			facetList = append(facetList, facet)
		}
		sort.Strings(facetList)
		parts := strings.SplitN(key, "|", 2)
		candidateID := fmt.Sprintf("PROM_%03d", i+1)
		empiricalProven := len(hashes) >= empiricalSemanticProofThreshold
		if empiricalProven {
			summary.EmpiricalSemanticProven++
			empiricalRows = append(empiricalRows, []string{candidateID, parts[0], parts[1], strings.Join(facetList, ";"), fmt.Sprintf("%d", len(members)), fmt.Sprintf("%d", len(hashes)), "EMPIRICAL_PROVEN_SOURCE_SEMANTICS"})
		}
		classFacets := runtimePromotionFacetsForClass(registry, parts[0], parts[1], preservation)
		directContract := promotion.DirectProjectionClass[parts[0]][parts[1]]
		candidateState := "NO_DIRECT_CANDIDATE"
		if directContract {
			candidateState = "DIRECT_CANDIDATE"
			summary.DirectCandidates++
		} else {
			summary.NoDirectCandidate++
		}
		// The candidate observation can cover a subset of a projection class;
		// the impact column deliberately states the full existing matrix class
		// that a successful shared proof would promote.
		row := []string{candidateID, parts[0], parts[1], strings.Join(facetList, ";"), fmt.Sprintf("%d", len(members)), fmt.Sprintf("%d", len(hashes)), candidateState, "SEMANTIC_ROUNDTRIP+RUNTIME_DIFFERENTIAL", fmt.Sprintf("%t", empiricalProven), "false", fmt.Sprintf("%d", len(classFacets))}
		candidateRows = append(candidateRows, row)
		classRows = append(classRows, []string{candidateID, parts[0], parts[1], "target_runtime_contract", candidateState, "SEMANTIC_ROUNDTRIP+RUNTIME_DIFFERENTIAL", fmt.Sprintf("%t", empiricalProven), strings.Join(facetList, ";")})
	}
	if err := writePromotionCSV(filepath.Join(out, "promotion_candidates.csv"), []string{"candidate_id", "target", "projection_class", "uasf_set", "native_observations", "independent_hashes", "candidate_state", "direct_proof_requirement", "empirical_semantics_proven", "promotion_allowed", "cells_promoted_if_proven"}, candidateRows); err != nil {
		return summary, err
	}
	if err := writePromotionCSV(filepath.Join(out, "promotion_equivalence_classes.csv"), []string{"candidate_id", "target", "projection_class", "runtime_contract", "candidate_state", "direct_proof_requirement", "empirical_semantics_proven", "uasf_set"}, classRows); err != nil {
		return summary, err
	}
	if err := writePromotionCSV(filepath.Join(out, "empirical_proven_semantics.csv"), []string{"candidate_id", "target", "projection_class", "uasf_set", "native_observations", "independent_hashes", "classification"}, empiricalRows); err != nil {
		return summary, err
	}
	// This is a proof-gap matrix, not a semantic-error matrix.  A native package
	// observation can prioritize a candidate, but it cannot turn a runtime
	// contract into a direct one without the controlled proof gates below.
	errorRows := make([][]string, 0, len(candidateRows)+1)
	for _, row := range candidateRows {
		gap := row[6]
		if row[6] == "DIRECT_CANDIDATE" && row[8] == "true" {
			gap = "TARGET_DIRECT_CONTRACT_NOT_IMPLEMENTED"
		}
		errorRows = append(errorRows, []string{row[0], row[1], row[2], gap, "ECOSYSTEM_PACKAGE", row[4]})
	}
	if summary.MLCPDRows == 0 {
		errorRows = append(errorRows, []string{"", "cpp", "", "MLCPD_INPUT_NOT_READY", "MLCPD", "0"})
	}
	if err := writePromotionCSV(filepath.Join(out, "promotion_error_matrix.csv"), []string{"candidate_id", "target", "projection_class", "proof_gap", "evidence_source", "observation_count"}, errorRows); err != nil {
		return summary, err
	}

	emptyProofHeader := []string{"candidate_id", "target", "projection_class", "status", "diagnostic"}
	for _, name := range []string{"direct_roundtrip_results.csv", "runtime_differential_results.csv", "compiler_results.csv", "proven_direct_contracts.csv", "rejected_direct_contracts.csv"} {
		if err := writePromotionCSV(filepath.Join(out, name), emptyProofHeader, nil); err != nil {
			return summary, err
		}
	}
	matrixRows := [][]string{}
	for col, target := range preservation.Targets {
		for row, facet := range preservation.Capabilities {
			matrixRows = append(matrixRows, []string{target, facet, string(preservation.Status(row, col))})
		}
	}
	if err := writePromotionCSV(filepath.Join(out, "promotion_matrix_before.csv"), []string{"target", "canonical_semantic_id", "preservation"}, matrixRows); err != nil {
		return summary, err
	}
	if err := writePromotionCSV(filepath.Join(out, "promotion_matrix_after.csv"), []string{"target", "canonical_semantic_id", "preservation"}, matrixRows); err != nil {
		return summary, err
	}
	encoded, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return summary, err
	}
	if err := os.WriteFile(filepath.Join(out, "promotion_summary.json"), encoded, 0o644); err != nil {
		return summary, err
	}
	return summary, nil
}
