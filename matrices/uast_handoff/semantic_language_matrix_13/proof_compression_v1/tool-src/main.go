package main

import (
    "encoding/csv"
    "encoding/json"
    "fmt"
    "os"
    "path/filepath"
    "strconv"
)

type Summary struct {
    SourceFeaturesTotal int `json:"source_features_total"`
    UASFCapabilities int `json:"uasf_capabilities"`
    SemanticBasisDimensions int `json:"semantic_basis_dimensions"`
    ExactCrosswalkRows int `json:"exact_crosswalk_rows"`
    UniqueUASFSignatures int `json:"unique_uasf_signatures"`
}

func readCSV(path string) ([][]string, error) {
    f, err := os.Open(path); if err != nil { return nil, err }; defer f.Close()
    r := csv.NewReader(f); r.FieldsPerRecord = -1
    return r.ReadAll()
}

func main() {
    root := "."
    if len(os.Args) > 1 { root = os.Args[1] }
    b, err := os.ReadFile(filepath.Join(root, "SUMMARY.json")); if err != nil { panic(err) }
    var s Summary
    if err := json.Unmarshal(b, &s); err != nil { panic(err) }
    proof, err := readCSV(filepath.Join(root, "03_source_feature_to_uasf_exact_proof.csv")); if err != nil { panic(err) }
    sig, err := readCSV(filepath.Join(root, "02_uasf_basis_signature_matrix.csv")); if err != nil { panic(err) }
    if len(proof)-1 != s.SourceFeaturesTotal { panic(fmt.Sprintf("feature rows %d != summary %d", len(proof)-1, s.SourceFeaturesTotal)) }
    exact := 0
    seen := map[string]bool{}
    for _, row := range proof[1:] {
        if len(row) < 8 { panic("malformed proof row") }
        v, _ := strconv.Atoi(row[7]); exact += v
        if row[4] != row[5] { panic(fmt.Sprintf("signature mismatch: %s -> %s", row[0], row[3])) }
    }
    for _, row := range sig[1:] {
        if len(row) < 2 { panic("malformed UASF signature row") }
        if seen[row[1]] { panic("duplicate UASF signature hash: " + row[1]) }
        seen[row[1]] = true
    }
    if exact != s.ExactCrosswalkRows { panic(fmt.Sprintf("exact rows %d != summary %d", exact, s.ExactCrosswalkRows)) }
    if len(seen) != s.UniqueUASFSignatures || len(sig)-1 != s.UASFCapabilities { panic("UASF signature count mismatch") }
    fmt.Printf("SEMANTIC PROOF COMPRESSION: PASS\n")
    fmt.Printf("SOURCE_FEATURES=%d\nUASF=%d\nBASIS=%d\nEXACT_ROWS=%d\nUNIQUE_UASF_SIGNATURES=%d\n", s.SourceFeaturesTotal, s.UASFCapabilities, s.SemanticBasisDimensions, exact, len(seen))
}
