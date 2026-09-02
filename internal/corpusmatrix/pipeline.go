package corpusmatrix

// Package corpusmatrix implements the bounded, evidence-only corpus pipeline.
// Corpus records and failures are ephemeral analysis rows; the UAST remains
// the only semantic source of truth.

import (
	"archive/zip"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

const (
	ResultFull   = "UAST_FULL"
	ResultGap    = "UAST_REJECTED_GAP"
	ResultInput  = "INPUT_ERROR"
	ResultParser = "PARSER_ERROR"
	ResultInfra  = "INFRA_ERROR"
	// Result classes separate source eligibility from UAST coverage.  They are
	// analysis labels only: no source fact is promoted merely because a corpus
	// file has been classified.
	ResultForeign      = "FOREIGN_DIALECT"
	ResultPreprocessor = "PREPROCESSOR_FRAGMENT"
	ResultUnsupported  = "VALID_UNSUPPORTED_SEMANTICS"
	ResultParserLimit  = "PARSER_LIMITATION"
	ResultInvalid      = "INVALID_SOURCE"
	ResultTruncated    = "TRUNCATED_FRAGMENT"
)

var gapCategories = []string{
	"UNKNOWN_SOURCE_CONSTRUCT", "MISSING_SOURCE_CROSSWALK", "MISSING_UAST_STRUCTURE",
	"MISSING_UAST_RELATION", "MISSING_UAST_FACET", "MISSING_UAST_FIELD",
	"UNPROVEN_SEMANTIC_MAPPING", "CONFLICTING_SEMANTICS", "FRONTEND_REJECTION",
	"TYPE_SEMANTIC_GAP", "CONTROL_FLOW_GAP", "EFFECT_GAP", "BINDING_GAP",
	"OWNERSHIP_GAP", "ERROR_PROPAGATION_GAP", "CONCURRENCY_GAP", "OTHER_EXPLICIT_GAP",
}

type CorpusRecord struct {
	CorpusSource                string   `json:"corpus_source"`
	CorpusRecordID              string   `json:"corpus_record_id"`
	LanguageID                  string   `json:"language_id"`
	SourceHash                  string   `json:"source_hash"`
	NormalizedSourceHash        string   `json:"normalized_source_hash"`
	PackageOrRepo               string   `json:"package_or_repo"`
	SourcePath                  string   `json:"source_path"`
	SourceCode                  string   `json:"source_code"`
	StructuralEvidenceAvailable bool     `json:"structural_evidence_available"`
	StructuralEvidenceKind      string   `json:"structural_evidence_kind"`
	ParserErrorCount            int      `json:"parser_error_count"`
	Provenance                  string   `json:"provenance"`
	Features                    []string `json:"features,omitempty"`
	MLCPDNodes                  []string `json:"mlcpd_nodes,omitempty"`
}

type FileResult struct {
	Record       CorpusRecord
	State        string
	InputClass   string
	Gaps         []string
	Diagnostic   string
	Capabilities []string
	UsedFeatures []string
}

type Config struct {
	Project                  string
	Out                      string
	MinerZip                 string
	MLCPD                    string
	Folder                   string
	FolderLanguage           string
	MinOccurrences           int
	MinDistinctHashes        int
	MinDistinctRepositories  int
	MinDistinctCorpusSources int
	Workers                  int
	Iteration                int
	Checkpoint               string
}

type Summary struct {
	FilesTotal                  int `json:"files_total"`
	FilesUASTFull               int `json:"files_uast_full"`
	FilesRejectedGap            int `json:"files_rejected_gap"`
	UniqueGapClasses            int `json:"unique_gap_classes"`
	CapabilitiesUsed            int `json:"capabilities_used"`
	CapabilitiesCorpusValidated int `json:"capabilities_corpus_validated"`
	NewEmpiricalCells           int `json:"new_empirical_cells"`
	BaselineConfirmedCells      int `json:"baseline_confirmed_cells"`
	DeduplicatedRecords         int `json:"deduplicated_records"`
	SourceHashDuplicates        int `json:"source_hash_duplicates"`
	MinOccurrences              int `json:"min_occurrences"`
	Iteration                   int `json:"iteration"`
	EmpiricalProofCells         int `json:"empirical_proof_cells"`
	EmpiricalContradictions     int `json:"empirical_contradictions"`
	EligiblePythonFiles         int `json:"eligible_python_files"`
	ValidUnsupported            int `json:"valid_unsupported"`
	ForeignDialect              int `json:"foreign_dialect"`
	InvalidOrTruncated          int `json:"invalid_or_truncated"`
	ParserLimitations           int `json:"parser_limitations"`
}

type mlcpdRow struct {
	ID       string
	Language string
	Code     string
	Package  string
	Path     string
	Parse    string
	Schema   string
	Errors   int
}

func NormalizeSource(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], " \t")
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n") + "\n"
}
func hashText(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }

func normalizeLanguage(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "c++" {
		return "cpp"
	}
	if s == "c#" {
		return "csharp"
	}
	return backend.NormalizeLanguage(s)
}

func extensionLanguage(path string) string {
	ext := strings.ToLower(filepath.Ext(path))
	for _, f := range backend.Frontends() {
		for _, e := range f.Extensions {
			if strings.EqualFold(e, ext) {
				return f.ID
			}
		}
	}
	return ""
}

func makeRecord(source, id, lang, code, pkg, path, evidence string, errors int, provenance string) CorpusRecord {
	norm := NormalizeSource(code)
	return CorpusRecord{CorpusSource: source, CorpusRecordID: id, LanguageID: normalizeLanguage(lang), SourceHash: hashText(code), NormalizedSourceHash: hashText(norm), PackageOrRepo: pkg, SourcePath: path, SourceCode: code, StructuralEvidenceAvailable: evidence != "", StructuralEvidenceKind: evidence, ParserErrorCount: errors, Provenance: provenance}
}

func LoadFolder(root, language string) ([]CorpusRecord, error) {
	var out []CorpusRecord
	requestedLanguage := normalizeLanguage(language)
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		// An explicit folder language is a source selection constraint, not a
		// request to reinterpret arbitrary binaries or metadata as source text.
		// This keeps empirical evidence tied to files the registered frontend can
		// actually parse.
		detectedLanguage := extensionLanguage(path)
		if requestedLanguage != "" && detectedLanguage != requestedLanguage {
			return nil
		}
		lang := requestedLanguage
		if lang == "" {
			lang = detectedLanguage
		}
		if lang == "" {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		id := "folder:" + filepath.ToSlash(path)
		out = append(out, makeRecord("FOLDER", id, lang, string(data), "", filepath.ToSlash(path), "", 0, path))
		return nil
	})
	return out, err
}

func LoadMLCPD(path string) ([]CorpusRecord, error) {
	var rows []mlcpdRow
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		err = filepath.Walk(path, func(p string, i os.FileInfo, e error) error {
			if e != nil || i.IsDir() {
				return e
			}
			rel, _ := filepath.Rel(path, p)
			first := strings.Split(filepath.ToSlash(rel), "/")[0]
			// The streaming runner writes shards atomically through .partial
			// files.  They are incomplete inputs and must never be parsed as CSV
			// or JSONL while a shard is still being downloaded.
			if strings.HasSuffix(strings.ToLower(p), ".partial") {
				return nil
			}
			// A streaming MLCPD result directory may also contain parquet
			// staging files, structural matrices, and a contract. Only the
			// records/ subtree is a source-record input.
			if !strings.EqualFold(first, "records") {
				return nil
			}
			// Parquet shards are intentionally left to the streaming runner or
			// a future reader implementation; never interpret them as CSV.
			if strings.EqualFold(filepath.Ext(p), ".parquet") {
				return nil
			}
			if strings.Contains(strings.ToLower(filepath.Base(p)), "contract") {
				return nil
			}
			rs, x := loadMLCPDFile(p)
			if x == nil {
				rows = append(rows, rs...)
			}
			return x
		})
	} else {
		rows, err = loadMLCPDFile(path)
	}
	if err != nil {
		return nil, err
	}
	out := make([]CorpusRecord, 0, len(rows))
	for i, r := range rows {
		lang := normalizeLanguage(r.Language)
		if lang == "" {
			lang = strings.ToLower(strings.TrimSpace(r.Language))
		}
		id := r.ID
		if id == "" {
			id = fmt.Sprintf("mlcpd:%d:%s", i, hashText(r.Code)[:16])
		}
		evidence := ""
		if r.Parse != "" || r.Schema != "" {
			evidence = "MLCPD_STRUCTURAL"
		}
		rec := makeRecord("MLCPD", id, lang, r.Code, r.Package, r.Path, evidence, r.Errors, path)
		rec.MLCPDNodes = extractMLCPDNodes(r.Parse, r.Schema)
		out = append(out, rec)
	}
	return out, nil
}

func loadMLCPDFile(path string) ([]mlcpdRow, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".gz" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return nil, err
		}
		defer gz.Close()
		return loadMLCPDJSONL(gz)
	}
	if ext == ".json" || ext == ".jsonl" || ext == ".ndjson" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		return loadMLCPDJSONL(f)
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	hdr, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i, h := range hdr {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	var out []mlcpdRow
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		get := func(names ...string) string {
			for _, n := range names {
				if i, ok := idx[n]; ok && i < len(row) {
					return row[i]
				}
			}
			return ""
		}
		n, _ := strconv.Atoi(get("num_errors", "parser_error_count", "errors"))
		out = append(out, mlcpdRow{ID: get("id", "record_id", "file_id"), Language: get("language", "lang"), Code: get("code", "source_code"), Package: get("package", "package_or_repo", "repository"), Path: get("path", "source_path", "file"), Parse: get("lang_specific_parse", "parse"), Schema: get("universal_schema", "schema"), Errors: n})
	}
	return out, nil
}

func loadMLCPDJSONL(r io.Reader) ([]mlcpdRow, error) {
	var out []mlcpdRow
	sc := bufio.NewScanner(r)
	// MLCPD records can contain large source strings; retain streaming while
	// allowing records larger than Scanner's default 64 KiB token size.
	sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var v map[string]any
		if json.Unmarshal([]byte(line), &v) != nil {
			continue
		}
		out = append(out, mlcpdMap(v))
	}
	return out, sc.Err()
}
func mlcpdMap(v map[string]any) mlcpdRow {
	get := func(keys ...string) string {
		for _, k := range keys {
			if x, ok := v[k]; ok {
				// Current MLCPD shards encode node arrays as JSON values. Preserve
				// their machine-readable form so extractMLCPDNodes can retain the
				// source-derived structural evidence instead of flattening it with
				// fmt.Sprint.
				if a, ok := x.([]any); ok {
					b, _ := json.Marshal(a)
					return string(b)
				}
				if a, ok := x.([]string); ok {
					b, _ := json.Marshal(a)
					return string(b)
				}
				return fmt.Sprint(x)
			}
		}
		return ""
	}
	n, _ := strconv.Atoi(get("num_errors", "parser_error_count", "errors"))
	return mlcpdRow{
		ID:       get("id", "record_id", "file_id", "corpus_record_id"),
		Language: get("language", "lang", "language_id"),
		Code:     get("code", "source_code"),
		Package:  get("package_or_repo", "package", "repository"),
		Path:     get("source_path", "path", "file"),
		Parse:    get("lang_specific_parse", "parse", "mlcpd_node_types"),
		Schema:   get("universal_schema", "schema", "structural_evidence_kind"),
		Errors:   n,
	}
}

func LoadMinerZip(path string) ([]CorpusRecord, error) {
	a, err := zip.OpenReader(path)
	if err != nil {
		return nil, err
	}
	defer a.Close()
	var out []CorpusRecord
	featureByLang := map[string]map[string]bool{}
	// Miner archives contain one language_feature_matrix per ecosystem.  It is
	// structural empirical evidence, not semantic truth; retain it as record
	// features so the unified matrices can account for its provenance.
	for _, e := range a.File {
		if !strings.HasSuffix(filepath.ToSlash(e.Name), "language_feature_matrix.csv") {
			continue
		}
		f, x := e.Open()
		if x != nil {
			return nil, x
		}
		r := csv.NewReader(f)
		hdr, x := r.Read()
		if x != nil {
			f.Close()
			continue
		}
		idx := map[string]int{}
		for i, h := range hdr {
			idx[strings.ToLower(strings.TrimSpace(h))] = i
		}
		for {
			row, er := r.Read()
			if er == io.EOF {
				break
			}
			if er != nil {
				f.Close()
				return nil, er
			}
			li, lok := idx["language_id"]
			fi, fok := idx["source_feature_id"]
			if !lok || !fok || li >= len(row) || fi >= len(row) {
				continue
			}
			lang := normalizeLanguage(row[li])
			feat := row[fi]
			if lang == "" || feat == "" {
				continue
			}
			if featureByLang[lang] == nil {
				featureByLang[lang] = map[string]bool{}
			}
			featureByLang[lang][feat] = true
		}
		f.Close()
	}
	for _, e := range a.File {
		if !strings.HasSuffix(filepath.ToSlash(e.Name), "artifact_summary.csv") {
			continue
		}
		f, x := e.Open()
		if x != nil {
			return nil, x
		}
		r := csv.NewReader(f)
		hdr, x := r.Read()
		if x != nil {
			f.Close()
			continue
		}
		idx := map[string]int{}
		for i, h := range hdr {
			idx[strings.ToLower(strings.TrimSpace(h))] = i
		}
		for {
			row, er := r.Read()
			if er == io.EOF {
				break
			}
			if er != nil {
				f.Close()
				return nil, er
			}
			get := func(n string) string {
				if i, ok := idx[n]; ok && i < len(row) {
					return row[i]
				}
				return ""
			}
			lang := get("language")
			sid := get("source_id")
			if lang == "" || sid == "" {
				continue
			}
			pkg := get("name")
			if pkg == "" {
				pkg = sid
			}
			code := ""
			evidence := "MINER_MATRIX"
			status := get("status")
			parserErr := 0
			if status != "ok" {
				parserErr = 1
			}
			rec := makeRecord("MINER", "miner:"+sid, lang, code, pkg, sid, evidence, parserErr, e.Name)
			rec.SourceHash = get("archive_sha256")
			if rec.SourceHash == "" {
				rec.SourceHash = hashText(sid)
			}
			rec.NormalizedSourceHash = rec.SourceHash
			for feat := range featureByLang[rec.LanguageID] {
				rec.Features = append(rec.Features, feat)
			}
			sort.Strings(rec.Features)
			out = append(out, rec)
		}
		f.Close()
	}
	return out, nil
}

// LoadMiner accepts either the verified ZIP or an extracted live miner run.
// The extracted form's artifact_records.jsonl is preferred because it retains
// per-artifact feature/provenance data while the miner is still downloading.
func LoadMiner(path string) ([]CorpusRecord, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return LoadMinerZip(path)
	}
	var out []CorpusRecord
	err = filepath.Walk(path, func(p string, i os.FileInfo, e error) error {
		if e != nil || i.IsDir() {
			return e
		}
		if filepath.Base(p) != "artifact_records.jsonl" {
			return nil
		}
		f, er := os.Open(p)
		if er != nil {
			return er
		}
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 64*1024*1024)
		for sc.Scan() {
			var v struct {
				Task   struct{ ID, Language, Ecosystem, Name, Version string }
				Result struct {
					Status, Error, ArchiveSHA256 string
					Features                     map[string]int `json:"Features"`
					SourceCode                   string         `json:"source_code"`
				}
			}
			if json.Unmarshal(sc.Bytes(), &v) != nil || v.Task.ID == "" {
				continue
			}
			id := "miner:" + v.Task.ID
			pkg := v.Task.Name
			if pkg == "" {
				pkg = v.Task.ID
			}
			rec := makeRecord("MINER", id, v.Task.Language, v.Result.SourceCode, pkg, v.Task.ID, "MINER_MATRIX", 0, filepath.ToSlash(p))
			if v.Result.Status != "" && v.Result.Status != "ok" {
				rec.ParserErrorCount = 1
			}
			if v.Result.ArchiveSHA256 != "" {
				rec.SourceHash = v.Result.ArchiveSHA256
				rec.NormalizedSourceHash = rec.SourceHash
			}
			for feat := range v.Result.Features {
				rec.Features = append(rec.Features, feat)
			}
			sort.Strings(rec.Features)
			out = append(out, rec)
		}
		return sc.Err()
	})
	return out, err
}

func extractMLCPDNodes(parse, schema string) []string {
	set := map[string]bool{}
	for _, raw := range []string{parse, schema} {
		var v any
		if json.Unmarshal([]byte(raw), &v) != nil {
			continue
		}
		collectNodeStrings(v, set)
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func collectNodeStrings(v any, set map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, y := range x {
			lk := strings.ToLower(k)
			if strings.Contains(lk, "node") || strings.Contains(lk, "kind") || strings.Contains(lk, "type") {
				if s, ok := y.(string); ok && s != "" {
					set[s] = true
				}
			}
			collectNodeStrings(y, set)
		}
	case []any:
		for _, y := range x {
			collectNodeStrings(y, set)
		}
	case string:
		// Current MLCPD shards expose tree-sitter node kinds directly as a
		// JSON string array. At this point the value is already bounded to a
		// parse/schema payload, so retaining the scalar is structural evidence.
		if x != "" {
			set[x] = true
		}
	}
}

func Dedupe(records []CorpusRecord) ([]CorpusRecord, int) {
	sort.SliceStable(records, func(i, j int) bool {
		if records[i].NormalizedSourceHash != records[j].NormalizedSourceHash {
			return records[i].NormalizedSourceHash < records[j].NormalizedSourceHash
		}
		return records[i].CorpusRecordID < records[j].CorpusRecordID
	})
	seen := map[string]int{}
	out := make([]CorpusRecord, 0, len(records))
	dup := 0
	for _, r := range records {
		if at, ok := seen[r.NormalizedSourceHash]; ok {
			dup++
			out[at].Provenance += "|" + r.CorpusSource + ":" + r.CorpusRecordID
			if out[at].PackageOrRepo == "" {
				out[at].PackageOrRepo = r.PackageOrRepo
			}
			continue
		}
		seen[r.NormalizedSourceHash] = len(out)
		out = append(out, r)
	}
	return out, dup
}

func Run(cfg Config, records []CorpusRecord) (Summary, error) {
	if cfg.MinOccurrences <= 0 {
		cfg.MinOccurrences = 1
	}
	if cfg.MinDistinctHashes <= 0 {
		cfg.MinDistinctHashes = cfg.MinOccurrences
	}
	if cfg.MinDistinctCorpusSources <= 0 {
		cfg.MinDistinctCorpusSources = 1
	}
	if cfg.Workers <= 0 {
		cfg.Workers = runtime.NumCPU()
	}
	if cfg.Workers > 8 {
		cfg.Workers = 8
	}
	if cfg.Iteration <= 0 {
		cfg.Iteration = 1
	}
	dedup, dup := Dedupe(records)
	results := make([]FileResult, len(dedup))
	cache := map[string]FileResult{}
	if cfg.Checkpoint != "" {
		if data, err := os.ReadFile(cfg.Checkpoint); err == nil {
			var saved []FileResult
			if json.Unmarshal(data, &saved) == nil {
				for _, x := range saved {
					cache[x.Record.NormalizedSourceHash] = x
				}
			}
		}
	}
	jobs := make(chan int)
	for i := range dedup {
		if x, ok := cache[dedup[i].NormalizedSourceHash]; ok {
			results[i] = x
		}
	}
	var wg sync.WaitGroup
	var resultMu sync.Mutex
	completed := 0
	for w := 0; w < cfg.Workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				resultMu.Lock()
				already := results[i].State != ""
				resultMu.Unlock()
				if already {
					continue
				}
				x := translate(dedup[i])
				resultMu.Lock()
				results[i] = x
				completed++
				checkpointNow := cfg.Checkpoint != "" && completed%64 == 0
				var snapshot []FileResult
				if checkpointNow {
					snapshot = append([]FileResult(nil), results...)
				}
				resultMu.Unlock()
				if checkpointNow {
					_ = writeCheckpoint(cfg.Checkpoint, snapshot)
				}
			}
		}()
	}
	for i := range dedup {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if cfg.Checkpoint != "" {
		_ = writeCheckpoint(cfg.Checkpoint, results)
	}
	return writeOutputs(cfg, records, dedup, results, dup)
}

func translate(r CorpusRecord) FileResult {
	res := FileResult{Record: r, State: ResultFull, InputClass: sourceInputClass(r), UsedFeatures: append([]string(nil), r.Features...)}
	if r.SourceCode == "" {
		res.State = ResultInput
		res.InputClass = "SOURCE_UNAVAILABLE"
		res.Diagnostic = "source code unavailable in corpus record"
		res.Gaps = []string{"OTHER_EXPLICIT_GAP"}
		return res
	}
	// Strong Cython markers describe a different frontend dialect.  Do not run
	// those sources through the Python grammar and then count the resulting
	// rejection as missing Python/UAST coverage.  Ordinary Python that imports
	// Cython is deliberately not classified as foreign.
	if res.InputClass == ResultForeign || res.InputClass == ResultPreprocessor {
		res.State = res.InputClass
		if res.InputClass == ResultPreprocessor {
			res.Diagnostic = "preprocessor fragment requires preprocessing"
		} else {
			res.Diagnostic = "foreign source dialect"
		}
		return res
	}
	p, err := backend.LowerMatrixLanguage(r.LanguageID, r.SourceCode)
	if err != nil {
		res.Diagnostic = normalizeDiagnostic(err.Error())
		res.State = classifyResultState(r.LanguageID, res.InputClass, res.Diagnostic)
		res.Gaps = classifyDiagnostic(res.Diagnostic)
		if len(res.Gaps) == 0 {
			res.Gaps = []string{"FRONTEND_REJECTION"}
		}
		return res
	}
	set := map[string]bool{}
	featureSet := map[string]bool{}
	for _, n := range p.UniversalAST.Nodes {
		for _, f := range n.SemanticFacets {
			set[f] = true
			featureSet[f] = true
		}
		if n.StructuralKind != "" {
			featureSet["structure:"+n.StructuralKind] = true
		}
	}
	for f := range set {
		res.Capabilities = append(res.Capabilities, f)
	}
	sort.Strings(res.Capabilities)
	for f := range featureSet {
		if !contains(res.UsedFeatures, f) {
			res.UsedFeatures = append(res.UsedFeatures, f)
		}
	}
	sort.Strings(res.UsedFeatures)
	return res
}

// sourceInputClass makes only source-level distinctions that are independently
// observable.  In particular, an unsupported Python construct is not called
// invalid and an absent construct is never used as negative evidence.
func sourceInputClass(r CorpusRecord) string {
	code := strings.ToLower(r.SourceCode)
	if strings.IndexByte(r.SourceCode, 0) >= 0 {
		return ResultInvalid
	}
	if r.LanguageID == "c" || r.LanguageID == "cpp" {
		// A preprocessor-only fragment is not a translation unit. It cannot
		// establish a frontend/UAST gap before preprocessing has occurred.
		withoutComments := strings.TrimSpace(stripCComments(code))
		if strings.HasPrefix(withoutComments, "#") && !hasCTranslationUnitBody(withoutComments) {
			return ResultPreprocessor
		}
		for _, marker := range []string{"@interface", "@implementation", "@protocol", "\n<%", "\n<%="} {
			if strings.Contains(code, marker) {
				return ResultForeign
			}
		}
		if r.LanguageID == "c" {
			for _, marker := range []string{"<-", "\nnamespace ", "\ntemplate<", "\ntemplate <", "\nusing namespace", "\nfun ", "\ndef ", "\nproc ", "\nfn "} {
				if strings.Contains(code, marker) {
					return ResultForeign
				}
			}
		}
		return "VALID_" + strings.ToUpper(r.LanguageID)
	}
	if r.LanguageID != "python" {
		return "SOURCE_CANDIDATE"
	}
	for _, marker := range []string{"cdef ", "cpdef ", "cimport ", "# cython:"} {
		if strings.HasPrefix(code, marker) || strings.Contains(code, "\n"+marker) {
			return ResultForeign
		}
	}
	return "VALID_PYTHON_CANDIDATE"
}

func stripCComments(code string) string {
	for {
		start := strings.Index(code, "/*")
		if start < 0 {
			break
		}
		end := strings.Index(code[start+2:], "*/")
		if end < 0 {
			return code[:start]
		}
		code = code[:start] + code[start+2+end+2:]
	}
	lines := strings.Split(code, "\n")
	for i, line := range lines {
		if at := strings.Index(line, "//"); at >= 0 {
			lines[i] = line[:at]
		}
	}
	return strings.Join(lines, "\n")
}

func hasCTranslationUnitBody(code string) bool {
	for _, line := range strings.Split(code, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			return true
		}
	}
	return false
}

func classifyResultState(language, inputClass, diagnostic string) string {
	if inputClass == ResultInvalid {
		return ResultInvalid
	}
	if inputClass == ResultForeign {
		return ResultForeign
	}
	if inputClass == ResultPreprocessor {
		return ResultPreprocessor
	}
	if language != "python" {
		return ResultGap
	}
	// These gaps name a semantic contract, rather than merely a token the
	// frontend has not yet accepted.  Retain them as explicit valid-source
	// unsupported cases until an existing UAST contract proves an exact mapping.
	if strings.Contains(diagnostic, "range binding") || strings.Contains(diagnostic, "range step") || strings.Contains(diagnostic, " near \"is\"") {
		return ResultUnsupported
	}
	// Decorators can transform the callable and must not be erased just to make
	// parsing continue.  They stay a semantic unsupported case, not invalid
	// Python and not a direct mapping.
	if strings.Contains(diagnostic, " near \"@\"") {
		return ResultUnsupported
	}
	if strings.Contains(diagnostic, "unexpected eof") || strings.Contains(diagnostic, "unterminated") {
		return ResultTruncated
	}
	return ResultParserLimit
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// sourceEligible is deliberately evidence-only. Excluded records have not
// reached the language frontend, so they cannot count as a frontend failure.
func sourceEligible(inputClass string) bool {
	switch inputClass {
	case ResultForeign, ResultPreprocessor, ResultInvalid, ResultTruncated, "SOURCE_UNAVAILABLE":
		return false
	}
	return true
}

func writeCheckpoint(path string, results []FileResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	// Checkpoints must be resumable without retaining third-party source text.
	// A cached result is keyed by the normalized source hash; output generation
	// needs only the metadata and the analysis result, never SourceCode itself.
	sanitized := append([]FileResult(nil), results...)
	for i := range sanitized {
		sanitized[i].Record.SourceCode = ""
	}
	data, err := json.Marshal(sanitized)
	if err != nil {
		return err
	}
	if err = os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func normalizeDiagnostic(s string) string {
	s = strings.ToLower(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}
func classifyDiagnostic(s string) []string {
	set := map[string]bool{"FRONTEND_REJECTION": true}
	if strings.Contains(s, "unknown") || strings.Contains(s, "expected") {
		set["UNKNOWN_SOURCE_CONSTRUCT"] = true
	}
	if strings.Contains(s, "type") {
		set["TYPE_SEMANTIC_GAP"] = true
	}
	if strings.Contains(s, "control") || strings.Contains(s, "loop") {
		set["CONTROL_FLOW_GAP"] = true
	}
	if strings.Contains(s, "binding") || strings.Contains(s, "scope") {
		set["BINDING_GAP"] = true
	}
	if strings.Contains(s, "relation") {
		set["MISSING_UAST_RELATION"] = true
	}
	out := make([]string, 0, len(set))
	for _, c := range gapCategories {
		if set[c] {
			out = append(out, c)
		}
	}
	return out
}

// failureContractProfile is a report-only quotient over normalized frontend
// diagnostics.  It records the existing UAST contract that was considered and
// the precise missing frontend mapping.  It never creates a UAST node, facet,
// relation, or proof row.
func failureContractProfile(diagnostic string) (constructs, contract, missing string) {
	switch {
	case strings.Contains(diagnostic, "range binding is not a single identifier"):
		return "loop pattern binding", "ForEachStmt + syntax.child + binding.declares", "pattern binding materializer"
	case strings.Contains(diagnostic, "range step requires"):
		return "symbolic stepped range", "ForEachStmt + control.loop_back + data.operand", "signed-step direction and zero contract"
	case strings.Contains(diagnostic, " near \"@\""):
		return "decorator", "Annotation + annotation.applies", "statically proven decorator transformation"
	case strings.Contains(diagnostic, " near \"is\""):
		return "identity operator", "OperationExpr + operation.kind", "identity/reference-equality operator contract"
	case strings.Contains(diagnostic, " near \":\"") || strings.Contains(diagnostic, " near \",\"") || strings.Contains(diagnostic, " near \"=\""):
		return "annotation or aggregate syntax", "TypeRef/ParameterDecl + type.has + syntax.child", "typed Python source-form binding"
	case strings.Contains(diagnostic, " near \";\""):
		return "semicolon-separated statements", "Scope + syntax.child", "statement-segment frontend binding"
	default:
		return "unclassified frontend syntax", "existing contracts not yet proven applicable", "matrix/frontend crosswalk"
	}
}

type baselineData struct{ lang map[string]map[string]bool }

func loadBaseline(root string) baselineData {
	b := baselineData{lang: map[string]map[string]bool{}}
	lf, err := os.Open(filepath.Join(root, "matrices", "uast_engine", "language_features.csv"))
	if err != nil {
		return b
	}
	defer lf.Close()
	r := csv.NewReader(lf)
	h, _ := r.Read()
	idx := map[string]int{}
	for i, v := range h {
		idx[v] = i
	}
	fc := map[string]string{}
	f, _ := os.Open(filepath.Join(root, "matrices", "uast_engine", "feature_capabilities.csv"))
	if f != nil {
		rr := csv.NewReader(f)
		hh, _ := rr.Read()
		ii := map[string]int{}
		for i, v := range hh {
			ii[v] = i
		}
		for {
			x, e := rr.Read()
			if e == io.EOF {
				break
			}
			if e == nil && len(x) > 1 {
				fc[x[ii["source_feature_id"]]] = x[ii["canonical_semantic_id"]]
			}
		}
		f.Close()
	}
	for {
		x, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			continue
		}
		l := x[idx["language_id"]]
		c := fc[x[idx["source_feature_id"]]]
		if c != "" {
			if b.lang[l] == nil {
				b.lang[l] = map[string]bool{}
			}
			b.lang[l][c] = true
		}
	}
	return b
}

func writeOutputs(cfg Config, allRecords []CorpusRecord, records []CorpusRecord, results []FileResult, dup int) (Summary, error) {
	if cfg.Out == "" {
		return Summary{}, fmt.Errorf("missing output directory")
	}
	if err := os.MkdirAll(cfg.Out, 0755); err != nil {
		return Summary{}, err
	}
	base := cfg.Project
	if base == "" {
		base = "."
	}
	baseline := loadBaseline(base)
	write := func(name string, header []string, rows [][]string) error {
		sort.Slice(rows, func(i, j int) bool {
			for k := range rows[i] {
				if rows[i][k] != rows[j][k] {
					return rows[i][k] < rows[j][k]
				}
			}
			return false
		})
		f, e := os.Create(filepath.Join(cfg.Out, name))
		if e != nil {
			return e
		}
		w := csv.NewWriter(f)
		if e = w.Write(header); e != nil {
			f.Close()
			return e
		}
		for _, row := range rows {
			if e = w.Write(row); e != nil {
				f.Close()
				return e
			}
		}
		w.Flush()
		e = w.Error()
		ce := f.Close()
		if e != nil {
			return e
		}
		return ce
	}
	manifest := [][]string{}
	fileMatrix := [][]string{}
	sourceHashRows := [][]string{}
	resultRows := [][]string{}
	eligibilityRows := [][]string{}
	gapRows := [][]string{}
	featureRows := [][]string{}
	languageGapRows := [][]string{}
	capabilityGapRows := [][]string{}
	languageGapSet := map[string]bool{}
	capabilityGapSet := map[string]bool{}
	langFeat := map[string]map[string]map[string]bool{}
	langPackages := map[string]map[string]map[string]bool{}
	pkgFeat := map[string]map[string]map[string]bool{}
	fileCaps := [][]string{}
	langCaps := map[string]map[string]bool{}
	success := map[string]bool{}
	capGap := map[string]bool{}
	classFiles := map[string][]FileResult{}
	failureFiles := map[string][]FileResult{}
	for _, r := range allRecords {
		manifest = append(manifest, []string{r.CorpusSource, r.CorpusRecordID, r.LanguageID, r.SourceHash, r.NormalizedSourceHash, r.PackageOrRepo, r.SourcePath, strconv.FormatBool(r.StructuralEvidenceAvailable), r.StructuralEvidenceKind, strconv.Itoa(r.ParserErrorCount), r.Provenance})
		sourceHashRows = append(sourceHashRows, []string{r.CorpusRecordID, r.NormalizedSourceHash})
	}
	for _, r := range records {
		fileMatrix = append(fileMatrix, []string{r.CorpusRecordID, r.LanguageID, r.NormalizedSourceHash, r.PackageOrRepo, r.SourcePath})
	}
	for _, x := range results {
		resultRows = append(resultRows, []string{x.Record.CorpusRecordID, x.Record.LanguageID, x.InputClass, x.State, x.Diagnostic})
		eligible := sourceEligible(x.InputClass)
		eligibilityRows = append(eligibilityRows, []string{x.Record.CorpusRecordID, x.Record.LanguageID, x.InputClass, x.State, strconv.FormatBool(eligible)})
		if x.State != ResultFull && x.Diagnostic != "" {
			failureFiles[x.State+"\x00"+x.Diagnostic] = append(failureFiles[x.State+"\x00"+x.Diagnostic], x)
		}
		if len(x.Gaps) > 0 {
			vec := append([]string(nil), x.Gaps...)
			sort.Strings(vec)
			vh := hashText(strings.Join(vec, "\x1f"))
			classFiles[vh] = append(classFiles[vh], x)
		}
		for _, g := range x.Gaps {
			gapRows = append(gapRows, []string{x.Record.CorpusRecordID, g, "true"})
			lg := x.Record.LanguageID + "\x00" + g
			if !languageGapSet[lg] {
				languageGapSet[lg] = true
				languageGapRows = append(languageGapRows, []string{x.Record.LanguageID, g, "true"})
			}
			for _, c := range x.Capabilities {
				cg := c + "\x00" + g
				if !capabilityGapSet[cg] {
					capabilityGapSet[cg] = true
					capabilityGapRows = append(capabilityGapRows, []string{c, g, "true"})
				}
			}
		}
		for _, f := range x.UsedFeatures {
			featureRows = append(featureRows, []string{x.Record.CorpusRecordID, x.Record.LanguageID, x.Record.PackageOrRepo, f, "true"})
			if langFeat[x.Record.LanguageID] == nil {
				langFeat[x.Record.LanguageID] = map[string]map[string]bool{}
			}
			if langFeat[x.Record.LanguageID][f] == nil {
				langFeat[x.Record.LanguageID][f] = map[string]bool{}
			}
			langFeat[x.Record.LanguageID][f][x.Record.NormalizedSourceHash] = true
			if langPackages[x.Record.LanguageID] == nil {
				langPackages[x.Record.LanguageID] = map[string]map[string]bool{}
			}
			if langPackages[x.Record.LanguageID][f] == nil {
				langPackages[x.Record.LanguageID][f] = map[string]bool{}
			}
			if x.Record.PackageOrRepo != "" {
				langPackages[x.Record.LanguageID][f][x.Record.PackageOrRepo] = true
			}
			if x.Record.PackageOrRepo != "" {
				if pkgFeat[x.Record.PackageOrRepo] == nil {
					pkgFeat[x.Record.PackageOrRepo] = map[string]map[string]bool{}
				}
				if pkgFeat[x.Record.PackageOrRepo][f] == nil {
					pkgFeat[x.Record.PackageOrRepo][f] = map[string]bool{}
				}
				pkgFeat[x.Record.PackageOrRepo][f][x.Record.NormalizedSourceHash] = true
			}
		}
		for _, c := range x.Capabilities {
			fileCaps = append(fileCaps, []string{x.Record.CorpusRecordID, c, "true"})
			if langCaps[x.Record.LanguageID] == nil {
				langCaps[x.Record.LanguageID] = map[string]bool{}
			}
			langCaps[x.Record.LanguageID][c] = true
			if x.State == ResultFull {
				success[c] = true
			} else {
				capGap[c] = true
			}
		}
	}
	if e := write("corpus_source_manifest.csv", []string{"corpus_source", "corpus_record_id", "language_id", "source_hash", "normalized_source_hash", "package_or_repo", "source_path", "structural_evidence_available", "structural_evidence_kind", "parser_error_count", "provenance"}, manifest); e != nil {
		return Summary{}, e
	}
	if e := write("corpus_file_matrix.csv", []string{"corpus_record_id", "language_id", "normalized_source_hash", "package_or_repo", "source_path"}, fileMatrix); e != nil {
		return Summary{}, e
	}
	if e := write("source_hash_matrix.csv", []string{"corpus_record_id", "normalized_source_hash"}, sourceHashRows); e != nil {
		return Summary{}, e
	}
	if e := write("file_feature_matrix.csv", []string{"corpus_record_id", "language_id", "package_or_repo", "source_feature_id", "observed"}, featureRows); e != nil {
		return Summary{}, e
	}
	lfRows := [][]string{}
	for l, fs := range langFeat {
		for f, hs := range fs {
			// Repository identity is intentionally left unknown unless the input
			// contract supplies it explicitly; corpus source is not a repository.
			lfRows = append(lfRows, []string{l, f, strconv.Itoa(len(hs)), strconv.Itoa(len(hs)), strconv.Itoa(len(langPackages[l][f])), "0", "true", strconv.FormatBool(len(hs) >= cfg.MinOccurrences)})
		}
	}
	if e := write("language_feature_matrix.csv", []string{"language_id", "source_feature_id", "distinct_files", "distinct_source_hashes", "distinct_packages", "distinct_repositories", "observed", "empirical_accepted"}, lfRows); e != nil {
		return Summary{}, e
	}
	pfRows := [][]string{}
	for p, fs := range pkgFeat {
		for f, hs := range fs {
			pfRows = append(pfRows, []string{p, f, strconv.Itoa(len(hs)), strconv.Itoa(len(hs))})
		}
	}
	if e := write("package_feature_matrix.csv", []string{"package_or_repo", "source_feature_id", "distinct_files", "distinct_source_hashes"}, pfRows); e != nil {
		return Summary{}, e
	}
	if e := write("file_capability_matrix.csv", []string{"corpus_record_id", "canonical_semantic_id", "used"}, fileCaps); e != nil {
		return Summary{}, e
	}
	lcRows := [][]string{}
	baseRows := [][]string{}
	newRows := [][]string{}
	for l, cs := range langCaps {
		for c := range cs {
			b := baseline.lang[l][c]
			g := capGap[c]
			lcRows = append(lcRows, []string{l, c, "true", strconv.FormatBool(success[c]), strconv.FormatBool(g)})
			baseRows = append(baseRows, []string{l, c, strconv.FormatBool(b)})
			newRows = append(newRows, []string{l, c, strconv.FormatBool(!b)})
		}
	}
	if e := write("language_capability_matrix.csv", []string{"language_id", "canonical_semantic_id", "used", "corpus_uast_validated", "has_gap"}, lcRows); e != nil {
		return Summary{}, e
	}
	if e := write("baseline_confirmation_matrix.csv", []string{"language_id", "canonical_semantic_id", "baseline_confirmed"}, baseRows); e != nil {
		return Summary{}, e
	}
	if e := write("new_empirical_matrix.csv", []string{"language_id", "canonical_semantic_id", "new_empirical"}, newRows); e != nil {
		return Summary{}, e
	}
	if e := write("uast_translation_result_matrix.csv", []string{"corpus_record_id", "language_id", "input_class", "result_state", "diagnostic"}, resultRows); e != nil {
		return Summary{}, e
	}
	if e := write("source_eligibility_matrix.csv", []string{"corpus_record_id", "language_id", "input_class", "result_state", "eligible_source"}, eligibilityRows); e != nil {
		return Summary{}, e
	}
	if e := write("uast_gap_matrix.csv", []string{"corpus_record_id", "gap_id", "present"}, gapRows); e != nil {
		return Summary{}, e
	}
	if e := write("language_gap_matrix.csv", []string{"language_id", "gap_id", "present"}, languageGapRows); e != nil {
		return Summary{}, e
	}
	if e := write("capability_gap_matrix.csv", []string{"canonical_semantic_id", "gap_id", "present"}, capabilityGapRows); e != nil {
		return Summary{}, e
	}
	gapSummary := [][]string{}
	classes := [][]string{}
	for vh, fs := range classFiles {
		ids := map[string]bool{}
		langs := map[string]bool{}
		pkgs := map[string]bool{}
		srcs := map[string]bool{}
		for _, x := range fs {
			for _, g := range x.Gaps {
				ids[g] = true
			}
			langs[x.Record.LanguageID] = true
			pkgs[x.Record.PackageOrRepo] = true
			srcs[x.Record.CorpusSource] = true
		}
		gids := []string{}
		for g := range ids {
			gids = append(gids, g)
		}
		sort.Strings(gids)
		cls := "gap_" + vh[:16]
		classes = append(classes, []string{cls, vh, strconv.Itoa(len(fs)), strconv.Itoa(len(fs)), joinKeys(langs), joinKeys(pkgs), joinKeys(srcs), strings.Join(gids, "|")})
		gapSummary = append(gapSummary, []string{cls, strings.Join(gids, "|"), strconv.Itoa(len(fs))})
	}
	if e := write("uast_gap_summary.csv", []string{"gap_class_id", "gap_ids", "file_count"}, gapSummary); e != nil {
		return Summary{}, e
	}
	if e := write("gap_equivalence_classes.csv", []string{"gap_class_id", "gap_vector_hash", "file_count", "source_hash_count", "languages", "packages", "corpus_sources", "gap_ids"}, classes); e != nil {
		return Summary{}, e
	}
	// The gap-vector quotient is intentionally coarse.  The failure-signature
	// quotient below supplies the exact unit for a shared frontend fix, together
	// with its exact corpus effect: one signature mapping unlocks precisely the
	// rows in its class, not an inferred score.
	failureClasses := [][]string{}
	for signature, fs := range failureFiles {
		parts := strings.SplitN(signature, "\x00", 2)
		state, diagnostic := parts[0], parts[1]
		constructs, contract, missing := failureContractProfile(diagnostic)
		hashes := map[string]bool{}
		paths := make([]string, 0, 3)
		for _, x := range fs {
			hashes[x.Record.NormalizedSourceHash] = true
			if len(paths) < 3 {
				paths = append(paths, x.Record.SourcePath)
			}
		}
		h := hashText(signature)
		failureClasses = append(failureClasses, []string{"failure_" + h[:16], state, diagnostic, strconv.Itoa(len(fs)), strconv.Itoa(len(hashes)), strings.Join(paths, "|"), constructs, contract, missing, strconv.Itoa(len(fs))})
	}
	if e := write("failure_equivalence_classes.csv", []string{"failure_class_id", "result_state", "normalized_failure_signature", "member_count", "source_hash_count", "representative_paths", "involved_syntax_constructs", "existing_uast_contract_considered", "exact_missing_mapping", "files_unlocked_if_fixed"}, failureClasses); e != nil {
		return Summary{}, e
	}
	capRows := [][]string{}
	capSet := map[string]bool{}
	for c := range success {
		capSet[c] = true
		capRows = append(capRows, []string{c, "true", strconv.FormatBool(!capGap[c])})
	}
	if e := write("capability_corpus_coverage.csv", []string{"canonical_semantic_id", "used", "corpus_uast_validated"}, capRows); e != nil {
		return Summary{}, e
	}
	empiricalProofCells, empiricalContradictions, e := writeEmpiricalProof(cfg.Out, results, cfg)
	if e != nil {
		return Summary{}, e
	}
	mlRows := [][]string{}
	mlGap := [][]string{}
	for _, r := range records {
		for _, n := range r.MLCPDNodes {
			mlRows = append(mlRows, []string{r.CorpusRecordID, r.LanguageID, n, "MLCPD_STRUCTURAL"})
			for _, x := range results {
				if x.Record.CorpusRecordID == r.CorpusRecordID {
					for _, g := range x.Gaps {
						mlGap = append(mlGap, []string{r.CorpusRecordID, n, g})
					}
				}
			}
		}
	}
	if e := write("mlcpd_node_matrix.csv", []string{"corpus_record_id", "language_id", "mlcpd_node", "evidence_kind"}, mlRows); e != nil {
		return Summary{}, e
	}
	if e := write("mlcpd_node_uast_gap_matrix.csv", []string{"corpus_record_id", "mlcpd_node", "gap_id"}, mlGap); e != nil {
		return Summary{}, e
	}
	full, gaps := 0, 0
	eligiblePython, validUnsupported, foreign, invalidOrTruncated, parserLimitations := 0, 0, 0, 0, 0
	for _, x := range results {
		if x.State == ResultFull {
			full++
		}
		if x.State == ResultGap {
			gaps++
		}
		if x.Record.LanguageID == "python" && x.InputClass == "VALID_PYTHON_CANDIDATE" {
			eligiblePython++
		}
		switch x.State {
		case ResultUnsupported:
			validUnsupported++
		case ResultForeign:
			foreign++
		case ResultInvalid, ResultTruncated:
			invalidOrTruncated++
		case ResultParserLimit:
			parserLimitations++
		}
	}
	newCount, confirmedCount := 0, 0
	for _, row := range newRows {
		if len(row) > 2 && row[2] == "true" {
			newCount++
		}
	}
	for _, row := range baseRows {
		if len(row) > 2 && row[2] == "true" {
			confirmedCount++
		}
	}
	sum := Summary{FilesTotal: len(results), FilesUASTFull: full, FilesRejectedGap: gaps, UniqueGapClasses: len(classFiles), CapabilitiesUsed: len(capSet), CapabilitiesCorpusValidated: len(success) - len(capGap), NewEmpiricalCells: newCount, BaselineConfirmedCells: confirmedCount, DeduplicatedRecords: len(results), SourceHashDuplicates: dup, MinOccurrences: cfg.MinOccurrences, Iteration: cfg.Iteration, EmpiricalProofCells: empiricalProofCells, EmpiricalContradictions: empiricalContradictions, EligiblePythonFiles: eligiblePython, ValidUnsupported: validUnsupported, ForeignDialect: foreign, InvalidOrTruncated: invalidOrTruncated, ParserLimitations: parserLimitations}
	sumBytes, _ := json.MarshalIndent(sum, "", "  ")
	if e := os.WriteFile(filepath.Join(cfg.Out, "corpus_matrix_summary.json"), sumBytes, 0644); e != nil {
		return sum, e
	}
	iter := [][]string{{strconv.Itoa(cfg.Iteration), strconv.Itoa(sum.FilesTotal), strconv.Itoa(sum.FilesUASTFull), strconv.Itoa(sum.FilesRejectedGap), strconv.Itoa(sum.UniqueGapClasses), strconv.Itoa(sum.CapabilitiesUsed), strconv.Itoa(sum.CapabilitiesCorpusValidated), strconv.Itoa(sum.NewEmpiricalCells), strconv.Itoa(sum.BaselineConfirmedCells)}}
	if e := write("fixpoint_iteration_summary.csv", []string{"iteration", "files_total", "files_uast_full", "files_rejected_gap", "unique_gap_classes", "capabilities_used", "capabilities_corpus_validated", "new_empirical_cells", "baseline_confirmed_cells"}, iter); e != nil {
		return sum, e
	}
	if e := writeChecksums(cfg.Out); e != nil {
		return sum, e
	}
	return sum, nil
}

func joinKeys(m map[string]bool) string {
	a := make([]string, 0, len(m))
	for k := range m {
		if k != "" {
			a = append(a, k)
		}
	}
	sort.Strings(a)
	return strings.Join(a, "|")
}
func writeChecksums(dir string) error {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	f, err := os.Create(filepath.Join(dir, "SHA256SUMS.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"file", "sha256"})
	for _, e := range ents {
		if e.IsDir() || e.Name() == "SHA256SUMS.csv" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		d, er := os.ReadFile(p)
		if er != nil {
			return er
		}
		h := sha256.Sum256(d)
		_ = w.Write([]string{e.Name(), hex.EncodeToString(h[:])})
	}
	w.Flush()
	return w.Error()
}
