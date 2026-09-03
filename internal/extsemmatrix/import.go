// Package extsemmatrix imports the proof-safe 13-language external semantic
// matrix and projects it onto the canonical UASF capability space.  The
// external matrix is evidence, not a second IR: unresolved atoms never become
// canonical capabilities and absence is only used when the source marks it
// explicitly as known.
package extsemmatrix

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Options struct {
	Project string
	Input   string
	Out     string
}

type Summary struct {
	PackageSHA256            string         `json:"package_sha256"`
	Files                    int            `json:"files"`
	Languages                int            `json:"languages"`
	Atoms                    int            `json:"atoms"`
	EvidenceRows             int            `json:"evidence_rows"`
	Sources                  int            `json:"sources"`
	CanonicalCapabilities    int            `json:"canonical_capabilities"`
	ConfirmedCrosswalkCells  int            `json:"confirmed_crosswalk_cells"`
	UnresolvedCrosswalkCells int            `json:"unresolved_crosswalk_cells"`
	ExternalPresentCells     int            `json:"external_present_cells"`
	ExternalAbsentCells      int            `json:"external_absent_cells"`
	DiffCounts               map[string]int `json:"diff_counts"`
	UnmappedAtoms            []string       `json:"unmapped_atoms"`
	SHA256Verified           bool           `json:"sha256_verified"`
}

type canonical struct {
	IDs      []string
	Features map[string][]string
}

// Import extracts the supplied package (or reads an already extracted
// semantic_language_matrix_13 directory), validates its checksums, builds the
// three matrix products and writes a manifest plus all source evidence files.
func Import(opts Options) (Summary, error) {
	if opts.Project == "" {
		opts.Project = "."
	}
	if opts.Input == "" {
		return Summary{}, errors.New("external semantic matrix input is required")
	}
	if opts.Out == "" {
		opts.Out = filepath.Join(opts.Project, "matrices", "uast_handoff", "semantic_language_matrix_13")
	}
	stage, cleanup, err := stageInput(opts.Input)
	if err != nil {
		return Summary{}, err
	}
	defer cleanup()
	if err := os.MkdirAll(opts.Out, 0o755); err != nil {
		return Summary{}, err
	}
	if err := copyDirFiles(stage, opts.Out); err != nil {
		return Summary{}, err
	}
	verified, err := verifyChecksums(opts.Input, stage)
	if err != nil {
		return Summary{}, err
	}
	sha, err := sha256Path(opts.Input)
	if err != nil {
		return Summary{}, err
	}
	c, err := loadCanonical(opts.Project)
	if err != nil {
		return Summary{}, err
	}
	atoms, languages, err := loadExternal(stage)
	if err != nil {
		return Summary{}, err
	}
	if err := writeLanguageExtSem(opts.Out, atoms, languages); err != nil {
		return Summary{}, err
	}
	xwalk, unmapped := buildCrosswalk(atoms, c)
	if err := writeCrosswalk(opts.Out, atoms, c.IDs, xwalk); err != nil {
		return Summary{}, err
	}
	external, err := projectLanguageUASF(opts.Out, opts.Project, atoms, languages, c.IDs, xwalk)
	if err != nil {
		return Summary{}, err
	}
	diff, counts, err := writeDiff(opts.Out, opts.Project, c.IDs, languages, external)
	if err != nil {
		return Summary{}, err
	}
	_ = diff
	s := Summary{PackageSHA256: sha, Files: countPackageFiles(stage), Languages: len(languages), Atoms: len(atoms), EvidenceRows: countCSVRows(filepath.Join(stage, "semantic_evidence_long.csv")), Sources: countCSVRows(filepath.Join(stage, "source_catalog.csv")), CanonicalCapabilities: len(c.IDs), ConfirmedCrosswalkCells: 0, UnresolvedCrosswalkCells: len(atoms) * len(c.IDs), ExternalPresentCells: 0, ExternalAbsentCells: 0, DiffCounts: counts, UnmappedAtoms: unmapped, SHA256Verified: verified}
	for _, rows := range xwalk {
		for _, status := range rows {
			if status == "CONFIRMED" {
				s.ConfirmedCrosswalkCells++
				s.UnresolvedCrosswalkCells--
			}
		}
	}
	for _, v := range external {
		if v == "PRESENT" {
			s.ExternalPresentCells++
		}
		if v == "ABSENT" {
			s.ExternalAbsentCells++
		}
	}
	b, _ := json.MarshalIndent(s, "", "  ")
	if err := os.WriteFile(filepath.Join(opts.Out, "import_summary.json"), append(b, '\n'), 0o644); err != nil {
		return Summary{}, err
	}
	return s, nil
}

func stageInput(input string) (string, func(), error) {
	st, err := os.Stat(input)
	if err != nil {
		return "", func() {}, err
	}
	if st.IsDir() {
		entries, _ := os.ReadDir(input)
		if len(entries) == 1 && entries[0].IsDir() {
			input = filepath.Join(input, entries[0].Name())
		}
		return input, func() {}, nil
	}
	r, err := zip.OpenReader(input)
	if err != nil {
		return "", func() {}, err
	}
	tmp, err := os.MkdirTemp("", "extsem-matrix-")
	if err != nil {
		r.Close()
		return "", func() {}, err
	}
	for _, e := range r.File {
		name := filepath.FromSlash(e.Name)
		parts := strings.Split(filepath.ToSlash(name), "/")
		if len(parts) > 1 {
			name = filepath.Join(parts[1:]...)
		}
		if name == "" || strings.HasSuffix(name, string(filepath.Separator)) {
			continue
		}
		dst := filepath.Join(tmp, name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			r.Close()
			os.RemoveAll(tmp)
			return "", func() {}, err
		}
		in, err := e.Open()
		if err != nil {
			r.Close()
			os.RemoveAll(tmp)
			return "", func() {}, err
		}
		out, err := os.Create(dst)
		if err == nil {
			_, err = io.Copy(out, in)
			out.Close()
		}
		in.Close()
		if err != nil {
			r.Close()
			os.RemoveAll(tmp)
			return "", func() {}, err
		}
	}
	r.Close()
	return tmp, func() { _ = os.RemoveAll(tmp) }, nil
}

func copyDirFiles(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(src, path)
		out := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(out, b, 0o644)
	})
}

func verifyChecksums(input, stage string) (bool, error) {
	rows, err := readCSV(filepath.Join(stage, "SHA256SUMS.csv"))
	if err != nil {
		return false, err
	}
	if len(rows) < 2 {
		return false, errors.New("SHA256SUMS.csv has no entries")
	}
	for _, r := range rows[1:] {
		if len(r) < 2 {
			continue
		}
		got, err := sha256File(filepath.Join(stage, r[0]))
		if err != nil {
			return false, err
		}
		if !strings.EqualFold(got, r[1]) {
			return false, fmt.Errorf("checksum mismatch for %s", r[0])
		}
	}
	_ = input
	return true, nil
}

func loadCanonical(root string) (canonical, error) {
	ids := map[string]bool{}
	rows, err := readCSV(filepath.Join(root, "matrices", "uast_engine", "capability_schema.csv"))
	if err != nil {
		return canonical{}, err
	}
	for _, r := range rows[1:] {
		if len(r) > 0 && strings.HasPrefix(r[0], "UASF_") {
			ids[r[0]] = true
		}
	}
	features := map[string][]string{}
	if r, e := readCSV(filepath.Join(root, "matrices", "uast_handoff", "canonical_new_semantics_metadata.csv")); e == nil && len(r) > 1 {
		h := index(r[0])
		for _, row := range r[1:] {
			id := row[h["canonical_semantic_id"]]
			if id == "" {
				continue
			} // Only concrete source-feature anchors are eligible for an exact crosswalk. Semantic axes/categories are deliberately excluded: e.g. abi.ffi is a broad axis, not proof that every capability is an FFI contract.
			features[id] = append(features[id], row[h["features"]])
		}
	}
	if r, e := readCSV(filepath.Join(root, "matrices", "uast_engine", "feature_capabilities.csv")); e == nil && len(r) > 1 {
		h := index(r[0])
		for _, row := range r[1:] {
			id := row[h["canonical_semantic_id"]]
			if id == "" {
				continue
			}
			features[id] = append(features[id], row[h["source_feature_id"]])
		}
	}
	out := make([]string, 0, len(ids))
	for id := range ids {
		out = append(out, id)
	}
	sort.Strings(out)
	return canonical{IDs: out, Features: features}, nil
}

type atom struct {
	ID          string
	Description string
}
type lang struct {
	ID, Name string
	Values   map[string]string
}

func loadExternal(stage string) ([]atom, []lang, error) {
	r, err := readCSV(filepath.Join(stage, "semantic_atom_dictionary.csv"))
	if err != nil {
		return nil, nil, err
	}
	h := index(r[0])
	atoms := make([]atom, 0, len(r)-1)
	for _, row := range r[1:] {
		atoms = append(atoms, atom{row[h["atom_id"]], row[h["description"]]})
	}
	s, err := readCSV(filepath.Join(stage, "semantic_atom_status.csv"))
	if err != nil {
		return nil, nil, err
	}
	sh := index(s[0])
	var atomCols []string
	for _, col := range s[0] {
		if strings.HasPrefix(col, "EXTSEM_") {
			atomCols = append(atomCols, col)
		}
	}
	langs := make([]lang, 0, len(s)-1)
	for _, row := range s[1:] {
		v := map[string]string{}
		for _, a := range atomCols {
			v[a] = row[sh[a]]
		}
		langs = append(langs, lang{row[sh["language_id"]], row[sh["language_name"]], v})
	}
	sort.Slice(atoms, func(i, j int) bool { return atoms[i].ID < atoms[j].ID })
	sort.Slice(langs, func(i, j int) bool { return langs[i].ID < langs[j].ID })
	return atoms, langs, nil
}

// buildCrosswalk is the sparse M_EXTSEM_UASF matrix.  A cell is confirmed
// only when an existing canonical feature carries an exact semantic anchor
// (for example GEN::ffi or a previously verified *.official.ast.generic row).
func buildCrosswalk(atoms []atom, c canonical) (map[string]map[string]string, []string) {
	aliases := map[string][]string{
		"EXTSEM_FFI": {"ffi", "cgo", "jni", "native_method"}, "EXTSEM_OWNERSHIP_BORROWING": {"ownership", "borrow"}, "EXTSEM_MANUAL_MEMORY": {"manual_memory"}, "EXTSEM_UNSAFE_ESCAPE": {"unsafe"}, "EXTSEM_GARBAGE_COLLECTION": {"gc", "garbage_collection"}, "EXTSEM_REFERENCE_COUNTING": {"reference_counting", "arc"}, "EXTSEM_EXPLICIT_POINTER_REFERENCE": {"pointer", "pointer_reference", "raw_pointer", "pointer_provenance"}, "EXTSEM_GENERICS": {"generic", "template"}, "EXTSEM_PATTERN_MATCHING": {"pattern", "match"}, "EXTSEM_EXCEPTIONS": {"exception", "throw", "try"}, "EXTSEM_STRUCTURED_CLEANUP": {"cleanup", "defer", "finally", "destructor"}, "EXTSEM_CONCURRENCY_PRIMITIVES": {"concurr", "thread", "mutex"}, "EXTSEM_ATOMICS_MEMORY_MODEL": {"atomic", "memory_order"}, "EXTSEM_CHANNELS": {"channel"}, "EXTSEM_ASYNC_AWAIT": {"async", "await", "task_async"}, "EXTSEM_MACROS": {"macro"}, "EXTSEM_COMPILETIME_META": {"compile_time", "comptime", "const_eval"}, "EXTSEM_RUNTIME_REFLECTION": {"reflection", "rtti"}, "EXTSEM_MODULE_SYSTEM": {"module", "imports", "namespace"}, "EXTSEM_ITERATORS_GENERATORS": {"iterator", "generator", "yield"}, "EXTSEM_SUM_VARIANTS": {"union", "sum", "variant", "enum"}, "EXTSEM_CLOSURES": {"closure", "lambda"}, "EXTSEM_LEXICAL_SCOPING": {"scope", "lexical"}, "EXTSEM_STRUCTURAL_CONFORMANCE": {"structural", "interface", "trait", "concept"}, "EXTSEM_OPERATOR_OVERLOADING": {"operator_overload"}, "EXTSEM_TYPE_LEVEL_NULL_SAFETY": {"nullable", "null_safety", "optional"}, "EXTSEM_TYPE_INFERENCE": {"inference", "deduction"}, "EXTSEM_DYNAMIC_TYPING": {"dynamic_type", "dynamic"}, "EXTSEM_NOMINAL_TYPE_IDENTITY": {"nominal", "type_identity"}, "EXTSEM_VALUE_TYPES": {"value_semantics", "value_type"}, "EXTSEM_REFERENCE_TYPES": {"reference_type", "reference_semantics"}, "EXTSEM_BYREF_PARAMETER_FORM": {"byref", "ref_out", "inout"}, "EXTSEM_MULTIPLE_DISPATCH": {"multiple_dispatch"}, "EXTSEM_SINGLE_RECEIVER_DYNAMIC_DISPATCH": {"dynamic_dispatch", "virtual_dispatch"}, "EXTSEM_UNDEFINED_BEHAVIOR": {"undefined_behavior", "ub_model"}, "EXTSEM_LAZY_ARGUMENTS": {"lazy_argument", "promise"}, "EXTSEM_LEFT_TO_RIGHT_GENERAL_EVAL": {"left_to_right", "evaluation_order"}, "EXTSEM_PARTIAL_UNSPECIFIED_EVAL": {"unspecified_eval", "unspecified_behavior"}, "EXTSEM_SHORT_CIRCUIT_BOOL": {"short_circuit"}, "EXTSEM_PASS_BY_VALUE_CORE": {"pass_by_value"}, "EXTSEM_FLOW_SENSITIVE_TYPING": {"flow_sensitive", "smart_cast"}, "EXTSEM_NOMINAL_INHERITANCE": {"inheritance"}, "EXTSEM_RAII_DESTRUCTORS": {"raii", "destructor"}, "EXTSEM_ACTORS": {"actor"}, "EXTSEM_STRUCTURED_CONCURRENCY": {"structured_concurrency"},
	}
	result := map[string]map[string]string{}
	unmapped := []string{}
	for _, a := range atoms {
		result[a.ID] = map[string]string{}
		aliasesA := aliases[a.ID]
		for _, id := range c.IDs {
			text := strings.ToLower(strings.Join(c.Features[id], " "))
			status := "UNRESOLVED"
			for _, alias := range aliasesA {
				if exactAnchor(text, alias) {
					status = "CONFIRMED"
					break
				}
			}
			result[a.ID][id] = status
		}
		found := false
		for _, v := range result[a.ID] {
			if v == "CONFIRMED" {
				found = true
				break
			}
		}
		if !found {
			unmapped = append(unmapped, a.ID)
		}
	}
	return result, unmapped
}

func exactAnchor(text, alias string) bool {
	alias = strings.ToLower(alias)
	for _, raw := range strings.FieldsFunc(strings.ToLower(text), func(r rune) bool { return r == ' ' || r == '\t' || r == ';' || r == ',' }) {
		piece := strings.Trim(raw, "\"'")
		if i := strings.LastIndex(piece, "::"); i >= 0 {
			tail := piece[i+2:]
			if tail == alias || strings.HasSuffix(tail, "_"+alias) {
				return true
			}
		}
		// Previously verified language features use the *.official.* namespace;
		// this prevents a broad axis such as abi.ffi from becoming evidence.
		if strings.Contains(piece, ".official.") && (strings.HasSuffix(piece, "."+alias) || strings.HasSuffix(piece, "_"+alias)) {
			return true
		}
	}
	return false
}

func writeLanguageExtSem(out string, atoms []atom, languages []lang) error {
	f, err := os.Create(filepath.Join(out, "M_LANGUAGE_EXTSEM.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"language_id", "language_name", "atom_id", "known", "value", "status"})
	for _, l := range languages {
		for _, a := range atoms {
			st := l.Values[a.ID]
			known := "0"
			value := "0"
			if st != "UNRESOLVED" {
				known = "1"
			}
			if st == "PRESENT" {
				value = "1"
			}
			_ = w.Write([]string{l.ID, l.Name, a.ID, known, value, st})
		}
	}
	w.Flush()
	return w.Error()
}

func writeCrosswalk(out string, atoms []atom, ids []string, x map[string]map[string]string) error {
	f, err := os.Create(filepath.Join(out, "M_EXTSEM_UASF.csv"))
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"atom_id", "canonical_semantic_id", "weight", "mapping_status", "basis"})
	for _, a := range atoms {
		for _, id := range ids {
			st := x[a.ID][id]
			basis := "no_exact_contract"
			if st == "CONFIRMED" {
				basis = "existing_canonical_feature_anchor"
			}
			_ = w.Write([]string{a.ID, id, func() string {
				if st == "CONFIRMED" {
					return "1"
				}
				return "0"
			}(), st, basis})
		}
	}
	w.Flush()
	return w.Error()
}

func projectLanguageUASF(out, root string, atoms []atom, languages []lang, ids []string, x map[string]map[string]string) (map[string]string, error) {
	ext := map[string]string{}
	f, err := os.Create(filepath.Join(out, "M_LANGUAGE_UASF_EXTERNAL.csv"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"language_id", "canonical_semantic_id", "status", "supporting_atoms", "operation"})
	for _, l := range languages {
		for _, id := range ids {
			present, absent := false, false
			supports := []string{}
			for _, a := range atoms {
				if x[a.ID][id] != "CONFIRMED" {
					continue
				}
				st := l.Values[a.ID]
				if st == "PRESENT" {
					present = true
					supports = append(supports, a.ID)
				}
				if st == "ABSENT" {
					absent = true
					supports = append(supports, a.ID)
				}
			}
			status := "UNRESOLVED"
			if present {
				status = "PRESENT"
			} else if absent {
				status = "ABSENT"
			}
			if status != "UNRESOLVED" {
				ext[l.ID+"\x00"+id] = status
			}
			_ = w.Write([]string{l.ID, id, status, strings.Join(supports, ";"), "M_LANGUAGE_EXTSEM × M_EXTSEM_UASF"})
		}
	}
	w.Flush()
	_ = root
	return ext, w.Error()
}

func writeDiff(out, root string, ids []string, languages []lang, ext map[string]string) (map[string]string, map[string]int, error) {
	existing := map[string]bool{}
	if rows, e := readCSV(filepath.Join(root, "matrices", "uast_engine", "language_features.csv")); e == nil && len(rows) > 1 {
		h := index(rows[0])
		for _, r := range rows[1:] {
			sf := r[h["source_feature_id"]]
			if strings.HasPrefix(sf, "uast.evidence.") {
				existing[r[h["language_id"]]+"\x00"+strings.TrimPrefix(sf, "uast.evidence.")] = true
			}
		}
	}
	counts := map[string]int{}
	f, err := os.Create(filepath.Join(out, "M_LANGUAGE_UASF_DIFF.csv"))
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"language_id", "canonical_semantic_id", "external_status", "existing", "classification", "operation"})
	for _, l := range languages {
		for _, id := range ids {
			k := l.ID + "\x00" + id
			st := ext[k]
			if st == "" {
				st = "UNRESOLVED"
			}
			ex := existing[k]
			cls := "UNRESOLVED"
			if st == "PRESENT" && ex {
				cls = "CONFIRMED"
			} else if st == "PRESENT" && !ex {
				cls = "NEW_EXTERNAL_EVIDENCE"
			} else if st == "ABSENT" && ex {
				cls = "CONFLICT"
			} else if st == "ABSENT" && !ex {
				cls = "CONFIRMED"
			}
			counts[cls]++
			_ = w.Write([]string{l.ID, id, st, fmt.Sprint(ex), cls, "XOR"})
		}
	}
	w.Flush()
	return ext, counts, w.Error()
}

func readCSV(path string) ([][]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	return r.ReadAll()
}
func index(h []string) map[string]int {
	m := map[string]int{}
	for i, v := range h {
		m[v] = i
	}
	return m
}
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	_, err = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), err
}
func sha256Path(path string) (string, error) {
	if st, err := os.Stat(path); err == nil && !st.IsDir() {
		return sha256File(path)
	}
	return "", nil
}
func countCSVRows(path string) int {
	r, e := readCSV(path)
	if e != nil || len(r) < 2 {
		return 0
	}
	return len(r) - 1
}
func countPackageFiles(root string) int {
	n := 0
	_ = filepath.Walk(root, func(_ string, i os.FileInfo, e error) error {
		if e == nil && i != nil && !i.IsDir() {
			n++
		}
		return nil
	})
	return n
}
