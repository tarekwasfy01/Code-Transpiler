package extsemmatrix

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ProofSummary describes the independently supplied 67-dimensional proof
// compression package.  It is kept beside (and never merged into) the UAST
// schema so the provenance of formal/source evidence remains auditable.
type ProofSummary struct {
	PackageSHA256        string `json:"package_sha256"`
	SHA256Verified       bool   `json:"sha256_verified"`
	SourceFeatures       int    `json:"source_features"`
	UASFCapabilities     int    `json:"uasf_capabilities"`
	BasisDimensions      int    `json:"basis_dimensions"`
	ExactCrosswalkRows   int    `json:"exact_crosswalk_rows"`
	PresentLanguageCells int    `json:"present_language_cells"`
	BasisSupportedCells  int    `json:"basis_supported_cells"`
	DirectBasisCells     int    `json:"direct_basis_cells"`
}

// ImportProof copies and validates the proof-compression package and derives a
// transparent basis-level EXTSEM→UASF matrix from M_EXTSEM_B × M_UASF_B^T.
// A basis overlap is labelled corroborating; it is not silently promoted to a
// canonical UASF mapping.
func ImportProof(opts Options, input string) (ProofSummary, error) {
	stage, cleanup, err := stageInput(input)
	if err != nil {
		return ProofSummary{}, err
	}
	defer cleanup()
	dst := filepath.Join(opts.Out, "proof_compression_v1")
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return ProofSummary{}, err
	}
	if err := copyDirFiles(stage, dst); err != nil {
		return ProofSummary{}, err
	}
	verified, err := verifyChecksums(input, stage)
	if err != nil {
		return ProofSummary{}, err
	}
	basis, err := readCSV(filepath.Join(stage, "01_semantic_basis_67.csv"))
	if err != nil {
		return ProofSummary{}, err
	}
	uasf, err := readCSV(filepath.Join(stage, "02_uasf_basis_signature_matrix.csv"))
	if err != nil {
		return ProofSummary{}, err
	}
	atomBasis, err := readCSV(filepath.Join(stage, "07_external_atom_to_basis_crosswalk.csv"))
	if err != nil {
		return ProofSummary{}, err
	}
	langPresence, err := readCSV(filepath.Join(stage, "05_language_uasf_presence_13.csv"))
	if err != nil {
		return ProofSummary{}, err
	}
	uh := index(uasf[0])
	bh := index(atomBasis[0])
	uasfIDs := make([]string, 0, len(uasf)-1)
	uBits := map[string]map[string]bool{}
	for _, row := range uasf[1:] {
		id := row[uh["canonical_semantic_id"]]
		if id == "" {
			continue
		}
		uasfIDs = append(uasfIDs, id)
		bits := map[string]bool{}
		for _, col := range uasf[0] {
			if strings.HasPrefix(col, "BASIS_") && uh[col] < len(row) && row[uh[col]] == "1" {
				bits[col] = true
			}
		}
		uBits[id] = bits
	}
	atomAll := map[string]map[string]bool{}
	atomDirect := map[string]map[string]bool{}
	for _, row := range atomBasis[1:] {
		a := row[bh["external_atom_id"]]
		b := row[bh["basis_id"]]
		if a == "" || b == "" {
			continue
		}
		if atomAll[a] == nil {
			atomAll[a] = map[string]bool{}
		}
		atomAll[a][b] = true
		if row[bh["mapping_strength"]] == "DIRECT" {
			if atomDirect[a] == nil {
				atomDirect[a] = map[string]bool{}
			}
			atomDirect[a][b] = true
		}
	}
	atoms := make([]string, 0, len(atomAll))
	for a := range atomAll {
		atoms = append(atoms, a)
	}
	sort.Strings(atoms)
	sort.Strings(uasfIDs)
	f, err := os.Create(filepath.Join(opts.Out, "M_EXTSEM_UASF_PROOF.csv"))
	if err != nil {
		return ProofSummary{}, err
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"atom_id", "canonical_semantic_id", "direct_basis_overlap", "all_basis_overlap", "atom_direct_basis", "atom_all_basis", "uasf_basis", "mapping_status", "operation"})
	directCells, supported := 0, 0
	for _, a := range atoms {
		for _, id := range uasfIDs {
			d, aN := 0, len(atomDirect[a])
			all := 0
			for b := range atomAll[a] {
				if uBits[id][b] {
					all++
				}
				if atomDirect[a][b] && uBits[id][b] {
					d++
				}
			}
			st := "UNRESOLVED"
			if d == aN && aN > 0 {
				st = "DIRECT_BASIS_SUPPORTED"
				directCells++
			} else if all > 0 {
				st = "CORROBORATING_BASIS_OVERLAP"
				supported++
			}
			_ = w.Write([]string{a, id, strconv.Itoa(d), strconv.Itoa(all), strconv.Itoa(aN), strconv.Itoa(len(atomAll[a])), strconv.Itoa(len(uBits[id])), st, "M_EXTSEM_B × M_UASF_B^T"})
		}
	}
	w.Flush()
	f.Close()
	if err := w.Error(); err != nil {
		return ProofSummary{}, err
	}
	if err := writeProofLanguagePresence(opts.Out, langPresence); err != nil {
		return ProofSummary{}, err
	}
	packageSHA, _ := sha256Path(input)
	s := ProofSummary{PackageSHA256: packageSHA, SHA256Verified: verified, SourceFeatures: countCSVRows(filepath.Join(stage, "03_source_feature_to_uasf_exact_proof.csv")), UASFCapabilities: len(uasfIDs), BasisDimensions: len(basis) - 1, ExactCrosswalkRows: countCSVRows(filepath.Join(stage, "03_source_feature_to_uasf_exact_proof.csv")), PresentLanguageCells: countOnes(langPresence), BasisSupportedCells: supported, DirectBasisCells: directCells}
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(filepath.Join(opts.Out, "proof_compression_summary.json"), append(b, '\n'), 0644); err != nil {
		return ProofSummary{}, err
	}
	return s, nil
}

func writeProofLanguagePresence(out string, rows [][]string) error {
	if len(rows) < 2 {
		return fmt.Errorf("language presence matrix is empty")
	}
	h := index(rows[0])
	f, err := os.Create(filepath.Join(out, "M_LANGUAGE_UASF_PROOF.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"language_id", "canonical_semantic_id", "present", "evidence_basis"})
	for _, r := range rows[1:] {
		l := r[h["language_id"]]
		for _, id := range rows[0][1:] {
			if !strings.HasPrefix(id, "UASF_") {
				continue
			}
			v := "0"
			if i, ok := h[id]; ok && i < len(r) {
				v = r[i]
			}
			_ = w.Write([]string{l, id, v, "proof_compression_67bit"})
		}
	}
	w.Flush()
	return w.Error()
}
func countOnes(rows [][]string) int {
	if len(rows) < 2 {
		return 0
	}
	h := index(rows[0])
	n := 0
	for _, r := range rows[1:] {
		for k, i := range h {
			if strings.HasPrefix(k, "UASF_") && i < len(r) && r[i] == "1" {
				n++
			}
		}
	}
	return n
}
