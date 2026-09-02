package main

// uast-source-verify records the evidence status of UAST cells.  An empirical
// proof is a proof when the corpus pipeline has established enough independent,
// deduplicated observations with no contradiction.  The proof is still kept
// separate from compiler/source provenance and cannot overwrite a conflict.

import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

type profile struct {
	SourceRoot       string `json:"source_root"`
	FilesScanned     int    `json:"files_scanned"`
	SemanticFeatures int    `json:"semantic_features"`
	ASTNodes         int    `json:"ast_nodes_extracted"`
}

type sourceContract struct {
	Language string
	Paths    []string
}

type empiricalProof struct {
	Proven   bool
	Conflict bool
}

func main() {
	project := flag.String("project", ".", "project root")
	out := flag.String("out", "matrices/uast_handoff/source_contract_verification.csv", "output CSV")
	flag.Parse()
	if err := run(*project, *out); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(root, out string) error {
	corpus := filepath.Join(root, "matrices", "uast_corpus_partial", "corpus_derived", "new_empirical_language_semantic_evidence.csv")
	languageFeatures := filepath.Join(root, "matrices", "uast_engine", "language_features.csv")
	scans := filepath.Join(root, "matrices", "language_source_scans")
	rows, err := readCSV(corpus)
	if err != nil {
		return fmt.Errorf("read corpus evidence: %w", err)
	}
	if len(rows) < 2 {
		return errors.New("corpus evidence has no rows")
	}
	idx := headerIndex(rows[0])
	for _, col := range []string{"language_id", "canonical_semantic_id", "corpus_observed"} {
		if _, ok := idx[col]; !ok {
			return fmt.Errorf("corpus evidence missing column %q", col)
		}
	}
	crosswalk, err := readCrosswalk(languageFeatures)
	if err != nil {
		return fmt.Errorf("read language crosswalk: %w", err)
	}
	sourceProof, err := readSourceProof(root, scans, filepath.Join(root, "matrices", "uast_handoff", "julia_semanticast_crosswalk.csv"))
	if err != nil {
		return fmt.Errorf("read source proof crosswalk: %w", err)
	}
	empirical, err := readEmpiricalProof(filepath.Join(root, "outputs", "uast-corpus-matrix-live", "empirical_proven_uast_matrix.csv"))
	if err != nil {
		return fmt.Errorf("read empirical proof matrix: %w", err)
	}
	contracts := map[string]sourceContract{
		"julia": {"julia", []string{"JuliaSyntax", "doc/src/devdocs/compiler.md", "base/Base.jl"}},
		"nim":   {"nim", []string{"compiler/nodekinds.nim", "compiler/astdef.nim", "compiler/options.nim", "lib/system.nim", "compiler/extccomp.nim"}},
		"swift": {"swift", []string{"include/swift/AST/DeclNodes.def", "include/swift/AST/ExprNodes.def", "include/swift/AST/StmtNodes.def", "include/swift/AST/PatternNodes.def", "docs/TypeChecker.md", "docs/Compiler.md"}},
		"zig":   {"zig", []string{"lib/std/zig/Ast.zig", "lib/std/builtin.zig", "src/Sema.zig", "doc/langref.html.in", "lib/std/zig.zig"}},
	}
	// The builder's feature projection is evidence only when it is language
	// specific. A byte-identical projection for all languages is a template,
	// not a compiler contract.
	scanHashes := map[string]string{}
	for lang := range contracts {
		b, err := os.ReadFile(filepath.Join(scans, lang, "03_feature_semantic_axis_matrix.csv"))
		if err == nil {
			scanHashes[lang] = fmt.Sprintf("%x", sha256.Sum256(b))
		}
	}
	uniqueHashes := map[string]bool{}
	for _, h := range scanHashes {
		uniqueHashes[h] = true
	}
	scanSpecific := len(uniqueHashes) > 1
	type result struct {
		language, semanticID, observed, scanPresent, sourceFiles, astNodes, scanFeatures, exactCrosswalk, compilerVerified, empiricalProven, empiricalConflict, decision, reason string
	}
	var results []result
	for _, row := range rows[1:] {
		lang, sid := row[idx["language_id"]], row[idx["canonical_semantic_id"]]
		p, scanOK := loadProfile(filepath.Join(scans, lang, "00_language_profile.json"))
		contract := contracts[lang]
		found := 0
		for _, rel := range contract.Paths {
			if p.SourceRoot != "" {
				if _, err := os.Stat(filepath.Join(p.SourceRoot, filepath.FromSlash(rel))); err == nil {
					found++
				}
			}
		}
		exact := crosswalk[lang+"\x00"+sid]
		proof := sourceProof[lang+"\x00"+sid]
		emp := empirical[lang+"\x00"+sid]
		verified := (proof || (scanOK && scanSpecific && found == len(contract.Paths) && exact))
		decision, reason := "REQUIRES_EXACT_CROSSWALK", "compiler/source archive and scan exist, but no exact language-to-UASF crosswalk is registered"
		if emp.Conflict {
			decision, reason = "EMPIRICAL_CONFLICT", "empirical observations contradict this language-to-UASF cell; promotion is blocked"
		} else if emp.Proven {
			decision, reason = "EMPIRICAL_PROVEN", "independent deduplicated corpus evidence meets the configured proof threshold without a contradiction"
		} else if proof {
			decision, reason = "VERIFIED_CANDIDATE", "exact source crosswalk and referenced AST evidence are present; promotion is limited to this explicitly documented cell"
		} else if !scanSpecific {
			decision, reason = "SOURCE_SCAN_NOT_LANGUAGE_SPECIFIC", "the source feature projection is byte-identical across scanned languages; it cannot prove a language-specific semantic cell"
		} else if exact && scanOK && found == len(contract.Paths) {
			decision, reason = "VERIFIED_CANDIDATE", "exact crosswalk and all declared compiler/source anchors present; promotion still requires explicit matrix review"
		} else if !scanOK {
			decision, reason = "SOURCE_SCAN_MISSING", "no source-derived scan profile for this language"
		} else if found != len(contract.Paths) {
			decision, reason = "SOURCE_ANCHOR_INCOMPLETE", fmt.Sprintf("%d/%d declared compiler/source anchors present", found, len(contract.Paths))
		}
		results = append(results, result{lang, sid, row[idx["corpus_observed"]], strconv.FormatBool(scanOK), strconv.Itoa(found), strconv.Itoa(p.ASTNodes), strconv.Itoa(p.SemanticFeatures), strconv.FormatBool(exact), strconv.FormatBool(verified), strconv.FormatBool(emp.Proven), strconv.FormatBool(emp.Conflict), decision, reason})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].language != results[j].language {
			return results[i].language < results[j].language
		}
		return results[i].semanticID < results[j].semanticID
	})
	if err := os.MkdirAll(filepath.Dir(filepath.Join(root, out)), 0o755); err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(root, out))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"language_id", "canonical_semantic_id", "corpus_observed", "source_scan_present", "source_anchors_found", "source_scan_ast_nodes", "source_scan_semantic_features", "exact_uast_crosswalk_present", "compiler_contract_verified", "empirical_proven", "empirical_conflict", "decision", "reason"}); err != nil {
		return err
	}
	for _, r := range results {
		if err := w.Write([]string{r.language, r.semanticID, r.observed, r.scanPresent, r.sourceFiles, r.astNodes, r.scanFeatures, r.exactCrosswalk, r.compilerVerified, r.empiricalProven, r.empiricalConflict, r.decision, r.reason}); err != nil {
			return err
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return err
	}
	if err := writeManifest(root, scans, contracts); err != nil {
		return fmt.Errorf("write source contract manifest: %w", err)
	}
	counts := map[string]int{}
	for _, r := range results {
		counts[r.decision]++
	}
	fmt.Printf("verified=%d rows=%d decisions=%v output=%s\n", len(results), len(results), counts, filepath.Join(root, out))
	return nil
}

func readSourceProof(root, scans, path string) (map[string]bool, error) {
	rows, err := readCSV(path)
	if err != nil {
		return map[string]bool{}, err
	}
	if len(rows) < 2 {
		return map[string]bool{}, nil
	}
	idx := headerIndex(rows[0])
	out := map[string]bool{}
	for _, row := range rows[1:] {
		if len(row) <= idx["language_id"] || len(row) <= idx["canonical_semantic_id"] || len(row) <= idx["evidence_file"] || len(row) <= idx["evidence_pattern"] {
			continue
		}
		lang, sid := row[idx["language_id"]], row[idx["canonical_semantic_id"]]
		file := filepath.Join(filepath.Dir(filepath.Dir(path)), filepath.FromSlash(row[idx["evidence_file"]]))
		if strings.HasPrefix(row[idx["evidence_file"]], "julia-master/") {
			p, ok := loadProfile(filepath.Join(scans, "julia", "00_language_profile.json"))
			if ok {
				file = filepath.Join(p.SourceRoot, filepath.FromSlash(strings.TrimPrefix(row[idx["evidence_file"]], "julia-master/")))
			}
		}
		content, readErr := os.ReadFile(file)
		if readErr != nil {
			continue
		}
		matched := true
		for _, pattern := range strings.Split(row[idx["evidence_pattern"]], ";") {
			if pattern != "" && !strings.Contains(string(content), pattern) {
				matched = false
			}
		}
		if matched {
			out[lang+"\x00"+sid] = true
		}
	}
	return out, nil
}

// readEmpiricalProof consumes only the corpus pipeline's boolean proof
// projection.  A plain corpus observation is deliberately insufficient: the
// pipeline has already checked the configured independence threshold and
// contradiction count before writing empirical_proven=true.
func readEmpiricalProof(path string) (map[string]empiricalProof, error) {
	if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
		return map[string]empiricalProof{}, nil
	} else if err != nil {
		return nil, err
	}
	rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return map[string]empiricalProof{}, nil
	}
	idx := headerIndex(rows[0])
	for _, column := range []string{"language_id", "canonical_semantic_id", "empirical_proven", "empirical_conflict"} {
		if _, ok := idx[column]; !ok {
			return nil, fmt.Errorf("empirical proof matrix missing column %q", column)
		}
	}
	out := map[string]empiricalProof{}
	for _, row := range rows[1:] {
		max := -1
		for _, column := range []string{"language_id", "canonical_semantic_id", "empirical_proven", "empirical_conflict"} {
			if idx[column] > max {
				max = idx[column]
			}
		}
		if len(row) <= max {
			continue
		}
		proven, err := strconv.ParseBool(strings.TrimSpace(row[idx["empirical_proven"]]))
		if err != nil {
			return nil, fmt.Errorf("invalid empirical_proven for %s/%s: %w", row[idx["language_id"]], row[idx["canonical_semantic_id"]], err)
		}
		conflict, err := strconv.ParseBool(strings.TrimSpace(row[idx["empirical_conflict"]]))
		if err != nil {
			return nil, fmt.Errorf("invalid empirical_conflict for %s/%s: %w", row[idx["language_id"]], row[idx["canonical_semantic_id"]], err)
		}
		key := strings.ToLower(strings.TrimSpace(row[idx["language_id"]])) + "\x00" + strings.TrimSpace(row[idx["canonical_semantic_id"]])
		prior := out[key]
		out[key] = empiricalProof{Proven: (prior.Proven || proven) && !(prior.Conflict || conflict), Conflict: prior.Conflict || conflict}
	}
	return out, nil
}

func writeManifest(root, scans string, contracts map[string]sourceContract) error {
	path := filepath.Join(root, "matrices", "uast_handoff", "source_contract_manifest.csv")
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"language_id", "relative_path", "present", "kind", "sha256"}); err != nil {
		return err
	}
	langs := make([]string, 0, len(contracts))
	for lang := range contracts {
		langs = append(langs, lang)
	}
	sort.Strings(langs)
	for _, lang := range langs {
		p, _ := loadProfile(filepath.Join(scans, lang, "00_language_profile.json"))
		for _, rel := range contracts[lang].Paths {
			full := filepath.Join(p.SourceRoot, filepath.FromSlash(rel))
			info, statErr := os.Stat(full)
			present := statErr == nil
			kind, hash := "missing", ""
			if present && info.IsDir() {
				kind = "directory"
			} else if present {
				kind = "file"
				b, readErr := os.ReadFile(full)
				if readErr == nil {
					hash = fmt.Sprintf("%x", sha256.Sum256(b))
				}
			}
			if err := w.Write([]string{lang, rel, strconv.FormatBool(present), kind, hash}); err != nil {
				return err
			}
		}
	}
	w.Flush()
	return w.Error()
}

func readCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	var rows [][]string
	for {
		row, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}
func headerIndex(h []string) map[string]int {
	m := map[string]int{}
	for i, v := range h {
		m[strings.TrimSpace(v)] = i
	}
	return m
}
func readCrosswalk(path string) (map[string]bool, error) {
	rows, err := readCSV(path)
	if err != nil {
		return nil, err
	}
	if len(rows) < 1 {
		return map[string]bool{}, nil
	}
	idx := headerIndex(rows[0])
	out := map[string]bool{}
	for _, row := range rows[1:] {
		if len(row) <= idx["canonical_semantic_id"] || len(row) <= idx["source_feature_id"] {
			continue
		}
		sf, sid := row[idx["source_feature_id"]], row[idx["canonical_semantic_id"]]
		lang := ""
		if strings.HasPrefix(sf, "uast.evidence.") {
			continue
		}
		if p := strings.IndexByte(sf, '.'); p > 0 {
			lang = strings.ToLower(sf[:p])
		}
		if lang != "" {
			out[lang+"\x00"+sid] = true
		}
	}
	return out, nil
}
func loadProfile(path string) (profile, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return profile{}, false
	}
	var p profile
	if json.Unmarshal(b, &p) != nil {
		return profile{}, false
	}
	return p, p.SourceRoot != "" && p.FilesScanned > 0
}
