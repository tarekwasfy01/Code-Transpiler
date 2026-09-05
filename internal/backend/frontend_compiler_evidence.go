package backend

import (
	_ "embed"
	"encoding/csv"
	"fmt"
	"sort"
	"strings"
)

// These are facts extracted from the user supplied compiler matrix pack. They
// contain no donor implementation code. Keeping them embedded makes releases
// reproducible and lets every frontend use the same evidence at runtime.
//
//go:embed frontend_compiler_evidence.csv
var embeddedCompilerFrontendEvidence []byte

//go:embed frontend_compiler_m_lang.csv
var embeddedCompilerFrontendMLang []byte

//go:embed frontend_compiler_m_phase.csv
var embeddedCompilerFrontendMPhase []byte

//go:embed frontend_compiler_m_rel.csv
var embeddedCompilerFrontendMRel []byte

//go:embed frontend_compiler_sources.csv
var embeddedCompilerFrontendSources []byte

type CompilerFrontendEvidence struct {
	ID, Language, Compiler, Stage, SourceArea, Representation string
	Features, Relations                                       []string
	Phase, EvidenceClass, Guidance, SourceURL                 string
}
type FrontendFeatureEvidence struct {
	Language, Feature, State, Proof, IDs string
	Count                                int
}
type FrontendPhaseEvidence struct{ Language, Compiler, Area, Phase, Features, ID string }
type FrontendRelationEvidence struct{ ID, Left, Relation, Right, Language, Proof, EvidenceID string }

// CompilerSourceEvidence records provenance and licence constraints for facts.
// It is deliberately metadata only; no third-party implementation is shipped.
type CompilerSourceEvidence struct{ Language, Compiler, Repository, Area, URL, LicensePolicy string }

type frontendCompilerEvidencePack struct {
	Rows      []CompilerFrontendEvidence
	Features  []FrontendFeatureEvidence
	Phases    []FrontendPhaseEvidence
	Relations []FrontendRelationEvidence
	Sources   []CompilerSourceEvidence
}

func evidenceList(s string) []string {
	var out []string
	for _, v := range strings.Split(s, ";") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
func csvRows(data []byte) ([]map[string]string, error) {
	r := csv.NewReader(strings.NewReader(string(data)))
	records, e := r.ReadAll()
	if e != nil {
		return nil, e
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("empty compiler frontend evidence")
	}
	out := make([]map[string]string, 0, len(records)-1)
	for _, record := range records[1:] {
		if len(record) != len(records[0]) {
			return nil, fmt.Errorf("invalid evidence row")
		}
		row := map[string]string{}
		for i, h := range records[0] {
			row[h] = record[i]
		}
		out = append(out, row)
	}
	return out, nil
}
func loadCompilerFrontendEvidence() (frontendCompilerEvidencePack, error) {
	var p frontendCompilerEvidencePack
	rows, e := csvRows(embeddedCompilerFrontendEvidence)
	if e != nil {
		return p, e
	}
	for _, r := range rows {
		p.Rows = append(p.Rows, CompilerFrontendEvidence{r["EvidenceID"], r["Language"], r["Compiler"], r["Stage"], r["SourceArea"], r["CompilerRepresentation"], evidenceList(r["SemanticFeatures"]), evidenceList(r["RelationFeatures"]), r["PhaseClass"], r["EvidenceClass"], r["MappingGuidance"], r["SourceURL"]})
	}
	rows, e = csvRows(embeddedCompilerFrontendMLang)
	if e != nil {
		return p, e
	}
	for _, r := range rows {
		var n int
		fmt.Sscan(r["EvidenceCount"], &n)
		p.Features = append(p.Features, FrontendFeatureEvidence{r["Language"], r["SemanticFeature"], r["EvidenceState"], r["ProofClass"], r["EvidenceIDs"], n})
	}
	rows, e = csvRows(embeddedCompilerFrontendMPhase)
	if e != nil {
		return p, e
	}
	for _, r := range rows {
		p.Phases = append(p.Phases, FrontendPhaseEvidence{r["Language"], r["Compiler"], r["FrontendArea"], r["PhaseClass"], r["ObservedSemanticFeatures"], r["EvidenceID"]})
	}
	rows, e = csvRows(embeddedCompilerFrontendMRel)
	if e != nil {
		return p, e
	}
	for _, r := range rows {
		p.Relations = append(p.Relations, FrontendRelationEvidence{r["RelationID"], r["LeftFeature"], r["Relation"], r["RightFeature"], r["Language"], r["EvidenceClass"], r["EvidenceID"]})
	}
	rows, e = csvRows(embeddedCompilerFrontendSources)
	if e != nil {
		return p, e
	}
	for _, r := range rows {
		p.Sources = append(p.Sources, CompilerSourceEvidence{r["Language"], r["Compiler"], r["Repository"], r["SourceArea"], r["SourceURL"], r["LicensePolicy"]})
	}
	return p, nil
}
func compilerEvidenceFeatureSet(p frontendCompilerEvidencePack) []string {
	m := map[string]bool{}
	for _, r := range p.Features {
		m[r.Feature] = true
	}
	for _, r := range p.Rows {
		for _, f := range r.Features {
			m[f] = true
		}
	}
	out := make([]string, 0, len(m))
	for x := range m {
		out = append(out, x)
	}
	sort.Strings(out)
	return out
}
