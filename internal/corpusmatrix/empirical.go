package corpusmatrix

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

type empiricalObservation struct {
	Hashes, Repositories, Sources map[string]bool
	Positive, Contradictions      int
}

// writeEmpiricalProof applies the boolean proof rule from the proof-
// compression contract. It counts only deduplicated, UAST_FULL observations;
// copied snippets therefore cannot inflate independence. Repository identity
// is optional and is never fabricated.
func writeEmpiricalProof(out string, results []FileResult, cfg Config) (int, int, error) {
	byFeature := map[string][]FileResult{}
	allCaps := map[string]map[string]bool{}
	for _, x := range results {
		if x.State != ResultFull {
			continue
		}
		for _, c := range x.Capabilities {
			if allCaps[x.Record.LanguageID] == nil {
				allCaps[x.Record.LanguageID] = map[string]bool{}
			}
			allCaps[x.Record.LanguageID][c] = true
		}
		for _, f := range x.UsedFeatures {
			key := x.Record.LanguageID + "\x00" + f
			byFeature[key] = append(byFeature[key], x)
		}
	}
	rows := [][]string{}
	provenCaps := map[string]bool{}
	conflictCaps := map[string]bool{}
	proofs, contradictions := 0, 0
	for key, files := range byFeature {
		sep := -1
		for i, r := range key {
			if r == '\x00' {
				sep = i
				break
			}
		}
		if sep < 0 {
			continue
		}
		lang, feature := key[:sep], key[sep+1:]
		for cap := range allCaps[lang] {
			o := empiricalObservation{Hashes: map[string]bool{}, Repositories: map[string]bool{}, Sources: map[string]bool{}}
			for _, x := range files {
				o.Hashes[x.Record.NormalizedSourceHash] = true
				if x.Record.PackageOrRepo != "" {
					o.Repositories[x.Record.PackageOrRepo] = true
				}
				o.Sources[x.Record.CorpusSource] = true
				if contains(x.Capabilities, cap) {
					o.Positive++
				} else {
					o.Contradictions++
				}
			}
			enough := len(o.Hashes) >= cfg.MinDistinctHashes && len(o.Sources) >= cfg.MinDistinctCorpusSources && (cfg.MinDistinctRepositories == 0 || len(o.Repositories) >= cfg.MinDistinctRepositories)
			proven := enough && o.Positive > 0 && o.Contradictions == 0
			classification := "INSUFFICIENT_EVIDENCE"
			if o.Contradictions > 0 {
				classification = "EMPIRICAL_CONTRADICTION"
				contradictions++
			} else if proven {
				classification = "NEW_EMPIRICAL_PROOF"
				proofs++
				provenCaps[lang+"\x00"+cap] = true
			}
			if o.Contradictions > 0 {
				conflictCaps[lang+"\x00"+cap] = true
			}
			rows = append(rows, []string{lang, feature, cap, strconv.Itoa(len(o.Hashes)), strconv.Itoa(len(o.Repositories)), strconv.Itoa(len(o.Sources)), strconv.Itoa(o.Positive), strconv.Itoa(o.Contradictions), strconv.FormatBool(enough), strconv.FormatBool(proven), classification, "deduplicated UAST_FULL observations"})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		for k := range rows[i] {
			if rows[i][k] != rows[j][k] {
				return rows[i][k] < rows[j][k]
			}
		}
		return false
	})
	f, err := os.Create(filepath.Join(out, "empirical_proof_matrix.csv"))
	if err != nil {
		return 0, 0, err
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"language_id", "source_feature_id", "canonical_semantic_id", "distinct_hashes", "distinct_repositories", "distinct_corpus_sources", "positive_observations", "contradiction_count", "enough_independent_evidence", "empirical_proven", "classification", "evidence_rule"})
	for _, r := range rows {
		_ = w.Write(r)
	}
	w.Flush()
	e := w.Error()
	_ = f.Close()
	// Capability-level projection keeps the boolean proof separate from the
	// feature-level observations and makes promotion auditable by language.
	cf, ce := os.Create(filepath.Join(out, "empirical_proven_uast_matrix.csv"))
	if ce != nil {
		return 0, 0, ce
	}
	cw := csv.NewWriter(cf)
	_ = cw.Write([]string{"language_id", "canonical_semantic_id", "empirical_proven", "empirical_conflict", "rule"})
	keys := map[string]bool{}
	for k := range provenCaps {
		keys[k] = true
	}
	for k := range conflictCaps {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	for _, k := range ordered {
		sep := -1
		for i, r := range k {
			if r == '\x00' {
				sep = i
				break
			}
		}
		if sep < 0 {
			continue
		}
		lang, cap := k[:sep], k[sep+1:]
		_ = cw.Write([]string{lang, cap, strconv.FormatBool(provenCaps[k] && !conflictCaps[k]), strconv.FormatBool(conflictCaps[k]), "ENOUGH_INDEPENDENT_EVIDENCE AND ZERO_OBSERVED_CONTRADICTIONS AND UAST_TRANSLATION_CONSISTENT"})
	}
	cw.Flush()
	ce = cw.Error()
	_ = cf.Close()
	if ce != nil {
		return 0, 0, ce
	}
	return proofs, contradictions, e
}
