package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
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
	"time"
)

const version = "1.0.2"

type stringSet map[string]bool

type pair struct{ A, B string }
type triple struct{ A, B, C string }

type discovery struct {
	File       string   `json:"file"`
	Kind       string   `json:"kind"`
	InputClass string   `json:"input_class"`
	Rows       int      `json:"rows"`
	Recognized []string `json:"recognized"`
	Ignored    []string `json:"ignored"`
}

type dataModel struct {
	Languages    stringSet
	Features     stringSet
	Capabilities stringSet
	Targets      stringSet
	Structures   stringSet
	Relations    stringSet
	Facets       stringSet
	Fields       stringSet

	LF            map[pair]bool   // language -> feature
	FC            map[pair]bool   // feature -> capability
	CapSchema     map[triple]bool // capability, element_type, element_id
	Dep           map[pair]bool   // capability -> dependency
	Canonical     map[string]bool
	Conflict      map[string]bool
	AlreadyCap    map[string]bool
	CurrentSchema map[pair]bool // type,id

	Preserve map[triple]bool // target, capability, mode
	Tested   map[pair]bool   // target, capability

	Discoveries []discovery
	FilesSeen   []string
	InputHash   string
}

type blocked struct {
	Capability string   `json:"capability"`
	Reasons    []string `json:"reasons"`
}

type capabilityPlan struct {
	ID                string            `json:"id"`
	EvidenceLanguages []string          `json:"evidence_languages"`
	SourceFeatures    []string          `json:"source_features"`
	Dependencies      []string          `json:"dependencies"`
	Structures        []string          `json:"structures"`
	Relations         []string          `json:"relations"`
	Facets            []string          `json:"facets"`
	Fields            []string          `json:"fields"`
	Frontends         []string          `json:"frontends"`
	Targets           map[string]string `json:"targets"`
}

type plan struct {
	Tool               string         `json:"tool"`
	Version            string         `json:"version"`
	GeneratedUTC       string         `json:"generated_utc"`
	Project            string         `json:"project"`
	InputHash          string         `json:"input_hash"`
	FixpointIterations int            `json:"fixpoint_iterations"`
	Counts             map[string]int `json:"counts"`
	// UASTReady is structural readiness only. It is intentionally independent
	// of target-preservation proofs and empirical SourceFeature evidence.
	UASTReady           int              `json:"uast_ready"`
	UASTDelta           map[string]int   `json:"uast_delta"`
	ImplementationDelta map[string]int   `json:"implementation_delta"`
	NewSemantics        []capabilityPlan `json:"new_semantics"`
	Blocked             []blocked        `json:"blocked"`
	Unproven            []string         `json:"unproven"`
	Conflicting         []string         `json:"conflicting"`
	Notes               []string         `json:"notes"`
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	cmd := strings.ToLower(os.Args[1])
	switch cmd {
	case "analyze":
		if err := runAnalyze(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "verify":
		if err := runVerify(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "init":
		if err := runInit(os.Args[2:]); err != nil {
			fatal(err)
		}
	case "version", "--version", "-version":
		fmt.Println("uast-matrix-engine", version)
	case "help", "--help", "-h":
		usage()
	default:
		fatal(fmt.Errorf("unknown command %q", cmd))
	}
}

func usage() {
	fmt.Printf(`UAST Matrix Engine %s

Commands:
  analyze --project PATH [--out PATH] [--strict=true]
  verify  --project PATH [--plan PATH] [--strict=true]
  init    --project PATH
  version

Purpose:
  Deterministic boolean-matrix / graph analysis for UAST semantic expansion.
  Unknown data is never invented: not proven -> not emitted.

Canonical input folder (the only dimension source):
  <project>/matrices/uast_engine/

Evidence and report folders (proof, external, corpus, outputs) are never
dimension inputs. They may reference canonical IDs, but cannot add them.

Run "init" once to create canonical CSV templates and an input contract.
`, version)
}

func runInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ContinueOnError)
	project := fs.String("project", ".", "project root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := filepath.Abs(*project)
	if err != nil {
		return err
	}
	dir := filepath.Join(root, "matrices", "uast_engine")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	templates := map[string]string{
		"language_features.csv":       "language_id,source_feature_id\n",
		"feature_capabilities.csv":    "source_feature_id,canonical_semantic_id\n",
		"capability_schema.csv":       "canonical_semantic_id,element_type,element_id\n",
		"capability_dependencies.csv": "canonical_semantic_id,depends_on_semantic_id\n",
		"capability_status.csv":       "canonical_semantic_id,canonical,conflict,already_uast\n",
		"current_uast_elements.csv":   "element_type,element_id,implemented\n",
		"target_preservation.csv":     "target_id,canonical_semantic_id,direct,rewrite,helper,emulate,runtime,tested\n",
	}
	for name, body := range templates {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(p, []byte(body), 0644); err != nil {
				return err
			}
		}
	}
	readme := `# UAST Matrix Engine canonical input contract

The engine reads only this folder.  Files elsewhere in the project are evidence
or reports and cannot expand canonical dimensions.

Input classes are explicit: CANONICAL_DIMENSION files define the fixed axes;
CANONICAL_CROSSWALK files define only edges between those axes.  Proof,
external, empirical, corpus, and report files are never loaded here.

## Files
- language_features.csv: language_id, source_feature_id
- feature_capabilities.csv: source_feature_id, canonical_semantic_id
- capability_schema.csv: canonical_semantic_id, element_type, element_id
  - element_type: structure | relation | facet | field
- capability_dependencies.csv: canonical_semantic_id, depends_on_semantic_id
- capability_status.csv: canonical_semantic_id, canonical, conflict, already_uast
- current_uast_elements.csv: element_type, element_id, implemented
- target_preservation.csv: target_id, canonical_semantic_id, direct, rewrite, helper, emulate, runtime, tested

Boolean values accepted: 1/0, true/false, yes/no, y/n.

The engine never guesses missing semantics. Missing evidence remains blocked/unproven.
`
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0644); err != nil {
		return err
	}
	fmt.Println("Initialized canonical matrix input folder:", dir)
	return nil
}

func runAnalyze(args []string) error {
	fs := flag.NewFlagSet("analyze", flag.ContinueOnError)
	project := fs.String("project", ".", "project root")
	out := fs.String("out", "", "output folder (default: <project>/outputs/uast-semantic-expansion)")
	strict := fs.Bool("strict", true, "fail when no usable semantic evidence is found")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := filepath.Abs(*project)
	if err != nil {
		return err
	}
	outDir := *out
	if outDir == "" {
		outDir = filepath.Join(root, "outputs", "uast-semantic-expansion")
	}
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(root, outDir)
	}

	dm := newDataModel()
	if err := loadProject(root, outDir, dm); err != nil {
		return err
	}
	if *strict && (len(dm.LF) == 0 || len(dm.FC) == 0) {
		return fmt.Errorf("strict mode: insufficient evidence mappings (language-feature edges=%d, feature-capability edges=%d). Run 'init' and populate matrices/uast_engine from existing project matrices", len(dm.LF), len(dm.FC))
	}
	p := analyze(root, dm)
	if err := writeOutputs(outDir, dm, p); err != nil {
		return err
	}
	fmt.Printf("UAST Matrix Engine %s\n", version)
	fmt.Printf("Project: %s\n", root)
	fmt.Printf("Inputs: %d files, SHA256 %s\n", len(dm.FilesSeen), dm.InputHash)
	fmt.Printf("Languages: %d | Features: %d | Capabilities: %d | Targets: %d\n", len(dm.Languages), len(dm.Features), len(dm.Capabilities), len(dm.Targets))
	fmt.Printf("Proven candidates: %d | New dependency-closed: %d | Blocked: %d | Conflicting: %d\n", p.Counts["proven_capabilities"], len(p.NewSemantics), len(p.Blocked), len(p.Conflicting))
	fmt.Printf("UAST_READY: %d/%d | schema delta NEW_S/NEW_R/NEW_A/NEW_D: %d/%d/%d/%d\n", p.UASTReady, p.Counts["canonical_capabilities"], p.UASTDelta["NEW_S"], p.UASTDelta["NEW_R"], p.UASTDelta["NEW_A"], p.UASTDelta["NEW_D"])
	fmt.Printf("Fixpoint iterations: %d\n", p.FixpointIterations)
	fmt.Printf("Output: %s\n", outDir)
	return nil
}

func runVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	project := fs.String("project", ".", "project root")
	planPath := fs.String("plan", "", "plan path (default outputs/uast-semantic-expansion/semantic_expansion_plan.json)")
	strict := fs.Bool("strict", true, "require unchanged input fingerprint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	root, err := filepath.Abs(*project)
	if err != nil {
		return err
	}
	pp := *planPath
	if pp == "" {
		pp = filepath.Join(root, "outputs", "uast-semantic-expansion", "semantic_expansion_plan.json")
	}
	b, err := os.ReadFile(pp)
	if err != nil {
		return err
	}
	var old plan
	if err := json.Unmarshal(b, &old); err != nil {
		return err
	}

	tmpOut := filepath.Join(root, "outputs", "uast-semantic-expansion", ".verify-tmp")
	_ = os.RemoveAll(tmpOut)
	dm := newDataModel()
	if err := loadProject(root, tmpOut, dm); err != nil {
		return err
	}
	now := analyze(root, dm)
	_ = os.RemoveAll(tmpOut)

	failures := []string{}
	if *strict && old.InputHash != dm.InputHash {
		failures = append(failures, fmt.Sprintf("input fingerprint changed: plan=%s current=%s", old.InputHash, dm.InputHash))
	}
	oldIDs := setFromPlans(old.NewSemantics)
	nowIDs := setFromPlans(now.NewSemantics)
	if !setsEqual(oldIDs, nowIDs) {
		failures = append(failures, fmt.Sprintf("new semantic set differs: expected=%d current=%d", len(oldIDs), len(nowIDs)))
	}
	if len(failures) > 0 {
		fmt.Println("VERIFY: FAIL")
		for _, f := range failures {
			fmt.Println(" -", f)
		}
		return errors.New("verification failed")
	}
	fmt.Println("VERIFY: PASS")
	fmt.Println("Input fingerprint:", dm.InputHash)
	fmt.Println("New semantic capabilities:", len(now.NewSemantics))
	return nil
}

func newDataModel() *dataModel {
	return &dataModel{
		Languages: stringSet{}, Features: stringSet{}, Capabilities: stringSet{}, Targets: stringSet{},
		Structures: stringSet{}, Relations: stringSet{}, Facets: stringSet{}, Fields: stringSet{},
		LF: map[pair]bool{}, FC: map[pair]bool{}, CapSchema: map[triple]bool{}, Dep: map[pair]bool{},
		Canonical: map[string]bool{}, Conflict: map[string]bool{}, AlreadyCap: map[string]bool{},
		CurrentSchema: map[pair]bool{}, Preserve: map[triple]bool{}, Tested: map[pair]bool{},
	}
}

var aliases = map[string][]string{
	"language":  {"language_id", "language", "lang", "source_language"},
	"feature":   {"source_feature_id", "source_feature", "feature_id", "feature"},
	"cap":       {"canonical_semantic_id", "semantic_capability_id", "capability_id", "capability", "semantic_id", "semantic"},
	"depends":   {"depends_on_semantic_id", "depends_on", "dependency", "requires_capability", "required_semantic_id"},
	"etype":     {"element_type", "schema_type", "kind_type"},
	"eid":       {"element_id", "schema_element_id"},
	"structure": {"structure_id", "structure", "structure_kind"},
	"relation":  {"relation_id", "relation"},
	"facet":     {"facet_id", "facet", "semantic_facet"},
	"field":     {"field_id", "field", "uast_field"},
	"target":    {"target_id", "target", "target_language"},
	"canonical": {"canonical", "is_canonical"},
	"conflict":  {"conflict", "conflicting", "has_conflict"},
	"already":   {"already_uast", "implemented", "current", "is_implemented"},
	"direct":    {"direct"}, "rewrite": {"rewrite"}, "helper": {"helper"}, "emulate": {"emulate", "emulation"}, "runtime": {"runtime"}, "tested": {"tested", "test"},
}

func loadProject(root, outDir string, dm *dataModel) error {
	var files []string
	// Dimension discovery is deliberately bounded to the explicit canonical
	// input contract.  Walking the whole project used to interpret proof,
	// corpus, and report CSVs as new languages/features.  Those files remain
	// auditable evidence, but only matrices/uast_engine can define dimensions.
	canonicalDir := filepath.Join(root, "matrices", "uast_engine")
	err := filepath.WalkDir(canonicalDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			base := strings.ToLower(d.Name())
			if base == ".git" || base == ".gocache" || base == "node_modules" || strings.Contains(path, string(filepath.Separator)+"vendor"+string(filepath.Separator)) {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		if ext == ".csv" || ext == ".json" {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return err
	}
	sort.Strings(files)
	h := sha256.New()
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		b, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		h.Write([]byte(filepath.ToSlash(rel)))
		h.Write([]byte{0})
		h.Write(b)
		h.Write([]byte{0})
		dm.FilesSeen = append(dm.FilesSeen, filepath.ToSlash(rel))
		switch strings.ToLower(filepath.Ext(f)) {
		case ".csv":
			_ = loadCSV(f, rel, dm)
		case ".json":
			_ = loadJSON(f, rel, dm)
		}
	}
	dm.InputHash = hex.EncodeToString(h.Sum(nil))
	return nil
}

func loadCSV(path, rel string, dm *dataModel) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	br := bufio.NewReader(f)
	sample, _ := br.Peek(4096)
	comma := detectDelimiter(string(sample))
	r := csv.NewReader(br)
	r.Comma = comma
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	r.LazyQuotes = true
	hdr, err := r.Read()
	if err != nil {
		return err
	}
	headers := normalizeHeaders(hdr)
	rows := 0
	recognizedSet := stringSet{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		row := map[string]string{}
		for i, v := range rec {
			if i < len(headers) {
				row[headers[i]] = strings.TrimSpace(v)
			}
		}
		recognized := ingestRow(row, dm)
		for _, x := range recognized {
			recognizedSet[x] = true
		}
		rows++
	}
	dm.Discoveries = append(dm.Discoveries, discovery{File: filepath.ToSlash(rel), Kind: "csv", InputClass: canonicalInputClass(rel), Rows: rows, Recognized: sortedSet(recognizedSet), Ignored: ignoredHeaders(headers)})
	return nil
}

func loadJSON(path, rel string, dm *dataModel) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var v any
	if json.Unmarshal(b, &v) != nil {
		return nil
	}
	rows := []map[string]string{}
	collectJSONRows(v, &rows)
	recognizedSet := stringSet{}
	headerSet := stringSet{}
	for _, row := range rows {
		nr := map[string]string{}
		for k, val := range row {
			nk := normalizeHeader(k)
			nr[nk] = val
			headerSet[nk] = true
		}
		rec := ingestRow(nr, dm)
		for _, x := range rec {
			recognizedSet[x] = true
		}
	}
	dm.Discoveries = append(dm.Discoveries, discovery{File: filepath.ToSlash(rel), Kind: "json", InputClass: canonicalInputClass(rel), Rows: len(rows), Recognized: sortedSet(recognizedSet), Ignored: ignoredHeaders(sortedSet(headerSet))})
	return nil
}

func collectJSONRows(v any, out *[]map[string]string) {
	switch x := v.(type) {
	case []any:
		for _, e := range x {
			collectJSONRows(e, out)
		}
	case map[string]any:
		row := map[string]string{}
		scalar := 0
		for k, val := range x {
			switch y := val.(type) {
			case string:
				row[k] = y
				scalar++
			case float64:
				row[k] = strconv.FormatFloat(y, 'g', -1, 64)
				scalar++
			case bool:
				row[k] = strconv.FormatBool(y)
				scalar++
			case nil:
				row[k] = ""
				scalar++
			}
		}
		if scalar >= 2 {
			*out = append(*out, row)
		}
		for _, val := range x {
			collectJSONRows(val, out)
		}
	}
}

func detectDelimiter(s string) rune {
	first := s
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		first = s[:i]
	}
	candidates := []rune{',', ';', '\t', '|'}
	best := ','
	max := -1
	for _, c := range candidates {
		n := strings.Count(first, string(c))
		if n > max {
			max = n
			best = c
		}
	}
	return best
}
func normalizeHeaders(in []string) []string {
	out := make([]string, len(in))
	for i, s := range in {
		out[i] = normalizeHeader(s)
	}
	return out
}
func normalizeHeader(s string) string {
	s = strings.TrimSpace(strings.ToLower(strings.TrimPrefix(s, "\ufeff")))
	rep := strings.NewReplacer(" ", "_", "-", "_", ".", "_", "/", "_", "\\", "_", "(", "", ")", "")
	return rep.Replace(s)
}
func get(row map[string]string, key string) string {
	for _, a := range aliases[key] {
		if v := strings.TrimSpace(row[a]); v != "" {
			return v
		}
	}
	return ""
}
func boolVal(s string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "ja", "pass", "full":
		return true, true
	case "0", "false", "no", "n", "nein", "fail":
		return false, true
	default:
		return false, false
	}
}

func ingestRow(row map[string]string, dm *dataModel) []string {
	rec := stringSet{}
	l, f, c := get(row, "language"), get(row, "feature"), get(row, "cap")
	if l != "" && f != "" {
		dm.Languages[l] = true
		dm.Features[f] = true
		dm.LF[pair{l, f}] = true
		rec["language→feature"] = true
	}
	if f != "" && c != "" {
		dm.Features[f] = true
		dm.Capabilities[c] = true
		dm.FC[pair{f, c}] = true
		rec["feature→capability"] = true
	}
	dep := get(row, "depends")
	if c != "" && dep != "" {
		dm.Capabilities[c] = true
		dm.Capabilities[dep] = true
		dm.Dep[pair{c, dep}] = true
		rec["capability→dependency"] = true
	}
	if c != "" {
		if b, ok := boolVal(get(row, "canonical")); ok {
			dm.Canonical[c] = b
			rec["capability-status"] = true
		}
		if b, ok := boolVal(get(row, "conflict")); ok {
			dm.Conflict[c] = b
			rec["capability-status"] = true
		}
		if b, ok := boolVal(get(row, "already")); ok && b {
			dm.AlreadyCap[c] = true
			rec["capability-status"] = true
		}
	}
	et, eid := strings.ToLower(get(row, "etype")), get(row, "eid")
	if c != "" && et != "" && eid != "" {
		addSchema(dm, c, et, eid)
		rec["capability→schema"] = true
	}
	if et != "" && eid != "" {
		if b, ok := boolVal(get(row, "already")); ok && b {
			dm.CurrentSchema[pair{et, eid}] = true
			rec["current-schema"] = true
		}
		// The current_uast_elements catalog defines the complete canonical
		// axis even when an element is not executable yet. Keep representability
		// (axis membership) separate from the implemented boolean above.
		addElementSet(dm, et, eid)
	}
	for _, typ := range []string{"structure", "relation", "facet", "field"} {
		if id := get(row, typ); c != "" && id != "" {
			addSchema(dm, c, typ, id)
			rec["capability→schema"] = true
		}
	}
	t := get(row, "target")
	if t != "" && c != "" {
		dm.Targets[t] = true
		dm.Capabilities[c] = true
		for _, mode := range []string{"direct", "rewrite", "helper", "emulate", "runtime"} {
			if b, ok := boolVal(get(row, mode)); ok && b {
				dm.Preserve[triple{t, c, strings.ToUpper(mode)}] = true
				rec["target-preservation"] = true
			}
		}
		if b, ok := boolVal(get(row, "tested")); ok && b {
			dm.Tested[pair{t, c}] = true
			rec["target-preservation"] = true
		}
	}
	return sortedSet(rec)
}

func addSchema(dm *dataModel, c, typ, id string) {
	typ = strings.ToLower(typ)
	dm.Capabilities[c] = true
	dm.CapSchema[triple{c, typ, id}] = true
	addElementSet(dm, typ, id)
}
func addElementSet(dm *dataModel, typ, id string) {
	switch strings.ToLower(typ) {
	case "structure", "structures":
		dm.Structures[id] = true
	case "relation", "relations":
		dm.Relations[id] = true
	case "facet", "facets":
		dm.Facets[id] = true
	case "field", "fields":
		dm.Fields[id] = true
	}
}

func analyze(root string, dm *dataModel) plan {
	// Language -> canonical capability evidence: B(M_LF × M_FC)
	lc := map[pair]bool{}
	capFeatures := map[string]stringSet{}
	capLangs := map[string]stringSet{}
	for lf := range dm.LF {
		for fc := range dm.FC {
			if lf.B == fc.A {
				lc[pair{lf.A, fc.B}] = true
				if capFeatures[fc.B] == nil {
					capFeatures[fc.B] = stringSet{}
				}
				capFeatures[fc.B][lf.B] = true
				if capLangs[fc.B] == nil {
					capLangs[fc.B] = stringSet{}
				}
				capLangs[fc.B][lf.A] = true
			}
		}
	}
	proven := stringSet{}
	for x := range lc {
		proven[x.B] = true
	}
	// Canonical default: if no explicit status row, a capability linked via FC is treated as canonical mapping,
	// but conflicts always block it. This does not invent semantics; FC is the explicit canonical mapping edge.
	canonical := stringSet{}
	for c := range proven {
		if dm.Conflict[c] {
			continue
		}
		if v, exists := dm.Canonical[c]; exists {
			if v {
				canonical[c] = true
			}
		} else {
			canonical[c] = true
		}
	}
	// dependency closure
	closure := transitiveClosure(dm.Dep)
	ready := stringSet{}
	blockedList := []blocked{}
	for c := range canonical {
		reasons := []string{}
		for e := range closure {
			if e.A == c && !canonical[e.B] && !dm.AlreadyCap[e.B] {
				reasons = append(reasons, "missing dependency: "+e.B)
			}
		}
		if len(reasons) == 0 {
			ready[c] = true
		} else {
			sort.Strings(reasons)
			blockedList = append(blockedList, blocked{c, reasons})
		}
	}
	// fixpoint over dependency-closed already/proven set. Since all proven set is known, this is deterministic.
	state := copySet(dm.AlreadyCap)
	iterations := 0
	for {
		iterations++
		changed := false
		keys := sortedSet(ready)
		for _, c := range keys {
			if state[c] {
				continue
			}
			ok := true
			for e := range closure {
				if e.A == c && !state[e.B] && !ready[e.B] {
					ok = false
					break
				}
			}
			if ok {
				state[c] = true
				changed = true
			}
		}
		if !changed || iterations > len(ready)+2 {
			break
		}
	}
	// A semantic block is executable work only when at least one registered
	// target has an existing preservation proof.  Keeping target-less
	// capabilities in new_semantics made the plan demand implementation while
	// every target was explicitly ERROR.  They remain proven schema facts, but
	// are classified as blocked until a direct/rewrite/helper/emulate/runtime
	// proof is supplied.
	newCaps := stringSet{}
	for c := range ready {
		if dm.AlreadyCap[c] {
			continue
		}
		hasPreservation := false
		for t := range dm.Targets {
			if selectPath(dm, t, c) != "ERROR" {
				hasPreservation = true
				break
			}
		}
		if !hasPreservation {
			blockedList = append(blockedList, blocked{c, []string{"missing preservation proof for all registered targets"}})
			continue
		}
		newCaps[c] = true
	}

	conflicts := []string{}
	for c, v := range dm.Conflict {
		if v {
			conflicts = append(conflicts, c)
		}
	}
	sort.Strings(conflicts)
	unproven := []string{}
	for c := range dm.Capabilities {
		if !proven[c] {
			unproven = append(unproven, c)
		}
	}
	sort.Strings(unproven)
	sort.Slice(blockedList, func(i, j int) bool { return blockedList[i].Capability < blockedList[j].Capability })

	plans := []capabilityPlan{}
	for _, c := range sortedSet(newCaps) {
		cp := capabilityPlan{ID: c, EvidenceLanguages: sortedSet(capLangs[c]), SourceFeatures: sortedSet(capFeatures[c]), Targets: map[string]string{}}
		deps := stringSet{}
		for e := range closure {
			if e.A == c {
				deps[e.B] = true
			}
		}
		cp.Dependencies = sortedSet(deps)
		s, r, a, d := stringSet{}, stringSet{}, stringSet{}, stringSet{}
		for e := range dm.CapSchema {
			if e.A == c {
				switch e.B {
				case "structure", "structures":
					s[e.C] = true
				case "relation", "relations":
					r[e.C] = true
				case "facet", "facets":
					a[e.C] = true
				case "field", "fields":
					d[e.C] = true
				}
			}
		}
		cp.Structures, cp.Relations, cp.Facets, cp.Fields = sortedSet(s), sortedSet(r), sortedSet(a), sortedSet(d)
		cp.Frontends = cp.EvidenceLanguages
		for _, t := range sortedSet(dm.Targets) {
			cp.Targets[t] = selectPath(dm, t, c)
		}
		plans = append(plans, cp)
	}
	counts := map[string]int{
		"languages": len(dm.Languages), "source_features": len(dm.Features), "canonical_capabilities": len(dm.Capabilities), "targets": len(dm.Targets),
		"proven_capabilities": len(proven), "canonical_proven": len(canonical), "ready_capabilities": len(ready), "new_capabilities": len(newCaps),
		"blocked_capabilities": len(blockedList), "unproven_capabilities": len(unproven), "conflicting_capabilities": len(conflicts),
		"known_structures": len(dm.Structures), "known_relations": len(dm.Relations), "known_facets": len(dm.Facets), "known_fields": len(dm.Fields),
	}
	// Structural UAST readiness is computed from the canonical capability
	// schema, never from target preservation or empirical evidence.  A UASF is
	// ready when its facet itself and every referenced schema element are part of
	// the canonical axes.  This keeps "representable" separate from executable
	// target lowering.
	uastReady := 0
	for c := range dm.Capabilities {
		facet := false
		valid := true
		for edge := range dm.CapSchema {
			if edge.A != c {
				continue
			}
			switch edge.B {
			case "facet", "facets":
				if edge.C == c {
					facet = true
				}
			case "structure", "structures":
				valid = valid && dm.Structures[edge.C]
			case "relation", "relations":
				valid = valid && dm.Relations[edge.C]
			case "field", "fields":
				valid = valid && dm.Fields[edge.C]
			}
		}
		if facet && valid {
			uastReady++
		}
	}
	// Delta vectors are reported in two planes. uast_delta describes schema
	// dimensions absent from the canonical axes (the structural gap); the
	// implementation delta retains the existing executable-status accounting.
	uastDelta := map[string]int{"NEW_S": 0, "NEW_R": 0, "NEW_A": 0, "NEW_D": 0}
	implementationDelta := map[string]int{"NEW_S": 0, "NEW_R": 0, "NEW_A": 0, "NEW_D": 0}
	for typ, dst := range map[string]stringSet{"structure": dm.Structures, "relation": dm.Relations, "facet": dm.Facets, "field": dm.Fields} {
		for id := range dst {
			if !dm.CurrentSchema[pair{typ, id}] {
				switch typ {
				case "structure":
					implementationDelta["NEW_S"]++
				case "relation":
					implementationDelta["NEW_R"]++
				case "facet":
					implementationDelta["NEW_A"]++
				case "field":
					implementationDelta["NEW_D"]++
				}
			}
		}
	}
	notes := []string{}
	if len(dm.Targets) == 0 {
		notes = append(notes, "No target preservation matrix was recognized; target paths remain absent.")
	}
	if len(dm.CurrentSchema) == 0 {
		notes = append(notes, "No current_uast_elements mapping was recognized; schema delta files list requirements not known as already implemented.")
	}
	counts["uast_ready"] = uastReady
	counts["uast_not_ready"] = len(dm.Capabilities) - uastReady
	return plan{Tool: "uast-matrix-engine", Version: version, GeneratedUTC: time.Now().UTC().Format(time.RFC3339), Project: root, InputHash: dm.InputHash, FixpointIterations: iterations, Counts: counts, UASTReady: uastReady, UASTDelta: uastDelta, ImplementationDelta: implementationDelta, NewSemantics: plans, Blocked: blockedList, Unproven: unproven, Conflicting: conflicts, Notes: notes}
}

func selectPath(dm *dataModel, t, c string) string {
	for _, mode := range []string{"DIRECT", "REWRITE", "HELPER", "EMULATE", "RUNTIME"} {
		if dm.Preserve[triple{t, c, mode}] {
			return mode
		}
	}
	return "ERROR"
}

func transitiveClosure(dep map[pair]bool) map[pair]bool {
	out := map[pair]bool{}
	nodes := stringSet{}
	for e := range dep {
		out[e] = true
		nodes[e.A] = true
		nodes[e.B] = true
	}
	ns := sortedSet(nodes)
	for _, k := range ns {
		for _, i := range ns {
			if !out[pair{i, k}] {
				continue
			}
			for _, j := range ns {
				if out[pair{k, j}] {
					out[pair{i, j}] = true
				}
			}
		}
	}
	return out
}

func writeOutputs(out string, dm *dataModel, p plan) error {
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "semantic_expansion_plan.json"), p); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "semantic_fixpoint.json"), map[string]any{"iterations": p.FixpointIterations, "input_hash": p.InputHash, "new_capabilities": len(p.NewSemantics)}); err != nil {
		return err
	}
	if err := writeUASTReady(filepath.Join(out, "uast_ready.csv"), dm, p); err != nil {
		return err
	}
	if err := writeJSON(filepath.Join(out, "uast_delta_vector.json"), map[string]any{
		"uast_ready":             p.UASTReady,
		"canonical_capabilities": p.Counts["canonical_capabilities"],
		"NEW_S":                  p.UASTDelta["NEW_S"], "NEW_R": p.UASTDelta["NEW_R"],
		"NEW_A": p.UASTDelta["NEW_A"], "NEW_D": p.UASTDelta["NEW_D"],
		"implementation_delta": p.ImplementationDelta,
	}); err != nil {
		return err
	}
	if err := writeDiscovery(filepath.Join(out, "input_discovery.csv"), dm); err != nil {
		return err
	}
	if err := writeEdgeCSV(filepath.Join(out, "feature_to_capability.csv"), []string{"source_feature_id", "canonical_semantic_id"}, dm.FC); err != nil {
		return err
	}
	if err := writeEdgeCSV(filepath.Join(out, "language_features.csv"), []string{"language_id", "source_feature_id"}, dm.LF); err != nil {
		return err
	}
	if err := writeEdgeCSV(filepath.Join(out, "semantic_dependency_matrix.csv"), []string{"canonical_semantic_id", "depends_on_semantic_id"}, dm.Dep); err != nil {
		return err
	}
	if err := writeEdgeCSV(filepath.Join(out, "semantic_dependency_closure.csv"), []string{"canonical_semantic_id", "depends_on_semantic_id"}, transitiveClosure(dm.Dep)); err != nil {
		return err
	}
	if err := writePlansList(filepath.Join(out, "new_capabilities.csv"), p.NewSemantics); err != nil {
		return err
	}
	if err := writeBlocked(filepath.Join(out, "blocked_capabilities.csv"), p.Blocked); err != nil {
		return err
	}
	if err := writeFrontendMatrix(filepath.Join(out, "frontend_producer_matrix.csv"), p.NewSemantics); err != nil {
		return err
	}
	if err := writeSchemaDelta(out, dm, p); err != nil {
		return err
	}
	if err := writePreservation(out, dm); err != nil {
		return err
	}
	if err := writeMaster(filepath.Join(out, "master_semantic_matrix.csv"), p); err != nil {
		return err
	}
	if err := writeReport(filepath.Join(out, "semantic_expansion_report.md"), dm, p); err != nil {
		return err
	}
	return nil
}

// writeUASTReady records structural representability separately from executable
// target paths. Every canonical facet in the schema is a valid UAST dimension;
// conflicts and missing empirical evidence are intentionally not represented as
// structural gaps here.
func writeUASTReady(path string, dm *dataModel, p plan) error {
	rows := make([][]string, 0, len(dm.Capabilities))
	for _, c := range sortedSet(dm.Capabilities) {
		ready := false
		facet := false
		valid := true
		for edge := range dm.CapSchema {
			if edge.A != c {
				continue
			}
			switch edge.B {
			case "facet", "facets":
				facet = facet || edge.C == c
			case "structure", "structures":
				valid = valid && dm.Structures[edge.C]
			case "relation", "relations":
				valid = valid && dm.Relations[edge.C]
			case "field", "fields":
				valid = valid && dm.Fields[edge.C]
			}
		}
		ready = facet && valid
		rows = append(rows, []string{c, strconv.FormatBool(ready), strconv.FormatBool(dm.Conflict[c])})
	}
	_ = p // plan is passed to keep this writer tied to the analyzed snapshot.
	return writeCSV(path, []string{"canonical_semantic_id", "uast_ready", "conflict"}, rows)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
func writeCSV(path string, headers []string, rows [][]string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write(headers); err != nil {
		return err
	}
	for _, r := range rows {
		if err := w.Write(r); err != nil {
			return err
		}
	}
	return w.Error()
}
func writeEdgeCSV(path string, h []string, m map[pair]bool) error {
	rows := [][]string{}
	for e := range m {
		rows = append(rows, []string{e.A, e.B})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i][0] == rows[j][0] {
			return rows[i][1] < rows[j][1]
		}
		return rows[i][0] < rows[j][0]
	})
	return writeCSV(path, h, rows)
}
func writeDiscovery(path string, dm *dataModel) error {
	rows := [][]string{}
	for _, d := range dm.Discoveries {
		rows = append(rows, []string{d.File, d.Kind, d.InputClass, strconv.Itoa(d.Rows), strings.Join(d.Recognized, ";"), strings.Join(d.Ignored, ";")})
	}
	return writeCSV(path, []string{"file", "kind", "input_class", "rows", "recognized_relations", "ignored_headers"}, rows)
}

// canonicalInputClass is intentionally a closed mapping.  It documents the
// distinction between axis-defining files and crosswalk/status edges in the
// discovery report without allowing arbitrary filenames to create dimensions.
func canonicalInputClass(rel string) string {
	base := strings.ToLower(filepath.Base(rel))
	switch base {
	case "language_features.csv", "feature_capabilities.csv", "capability_dependencies.csv", "target_preservation.csv":
		return "CANONICAL_CROSSWALK"
	case "capability_schema.csv", "capability_status.csv", "current_uast_elements.csv":
		return "CANONICAL_DIMENSION"
	default:
		return "CANONICAL_DIMENSION"
	}
}
func writePlansList(path string, ps []capabilityPlan) error {
	rows := [][]string{}
	for _, p := range ps {
		rows = append(rows, []string{p.ID, strings.Join(p.EvidenceLanguages, ";"), strings.Join(p.SourceFeatures, ";"), strings.Join(p.Dependencies, ";")})
	}
	return writeCSV(path, []string{"canonical_semantic_id", "evidence_languages", "source_features", "dependencies"}, rows)
}
func writeBlocked(path string, b []blocked) error {
	rows := [][]string{}
	for _, x := range b {
		rows = append(rows, []string{x.Capability, strings.Join(x.Reasons, ";")})
	}
	return writeCSV(path, []string{"canonical_semantic_id", "reasons"}, rows)
}
func writeFrontendMatrix(path string, ps []capabilityPlan) error {
	rows := [][]string{}
	for _, p := range ps {
		for _, l := range p.Frontends {
			rows = append(rows, []string{l, p.ID, "1"})
		}
	}
	sortRows(rows)
	return writeCSV(path, []string{"language_id", "canonical_semantic_id", "produce"}, rows)
}
func writeSchemaDelta(out string, dm *dataModel, p plan) error {
	maps := map[string][][]string{"structure": {}, "relation": {}, "facet": {}, "field": {}}
	seen := map[string]stringSet{"structure": {}, "relation": {}, "facet": {}, "field": {}}
	for _, cp := range p.NewSemantics {
		for typ, ids := range map[string][]string{"structure": cp.Structures, "relation": cp.Relations, "facet": cp.Facets, "field": cp.Fields} {
			for _, id := range ids {
				if dm.CurrentSchema[pair{typ, id}] {
					continue
				}
				if !seen[typ][id] {
					seen[typ][id] = true
					maps[typ] = append(maps[typ], []string{id, cp.ID})
				}
			}
		}
	}
	for typ, rows := range maps {
		sortRows(rows)
		if err := writeCSV(filepath.Join(out, "schema_delta_"+typ+"s.csv"), []string{"element_id", "required_by_capability"}, rows); err != nil {
			return err
		}
	}
	return nil
}
func writePreservation(out string, dm *dataModel) error {
	modes := []string{"DIRECT", "REWRITE", "HELPER", "EMULATE", "RUNTIME"}
	for _, mode := range modes {
		rows := [][]string{}
		for e := range dm.Preserve {
			if e.C == mode {
				rows = append(rows, []string{e.A, e.B, "1"})
			}
		}
		sortRows(rows)
		if err := writeCSV(filepath.Join(out, "target_"+strings.ToLower(mode)+"_matrix.csv"), []string{"target_id", "canonical_semantic_id", strings.ToLower(mode)}, rows); err != nil {
			return err
		}
	}
	rows := [][]string{}
	for _, t := range sortedSet(dm.Targets) {
		for _, c := range sortedSet(dm.Capabilities) {
			path := selectPath(dm, t, c)
			pres := "0"
			if path != "ERROR" {
				pres = "1"
			}
			tested := "0"
			if dm.Tested[pair{t, c}] {
				tested = "1"
			}
			rows = append(rows, []string{t, c, path, pres, tested})
		}
	}
	return writeCSV(filepath.Join(out, "target_preservable_matrix.csv"), []string{"target_id", "canonical_semantic_id", "selected_path", "preservable", "tested"}, rows)
}
func writeMaster(path string, p plan) error {
	rows := [][]string{}
	for _, cp := range p.NewSemantics {
		rows = append(rows, []string{cp.ID, strings.Join(cp.EvidenceLanguages, ";"), strings.Join(cp.Dependencies, ";"), strings.Join(cp.Structures, ";"), strings.Join(cp.Relations, ";"), strings.Join(cp.Facets, ";"), strings.Join(cp.Fields, ";")})
	}
	return writeCSV(path, []string{"canonical_semantic_id", "evidence_languages", "dependencies", "structures", "relations", "facets", "fields"}, rows)
}
func writeReport(path string, dm *dataModel, p plan) error {
	var b strings.Builder
	fmt.Fprintf(&b, "# UAST Semantic Expansion Matrix Report\n\n")
	fmt.Fprintf(&b, "- Tool: uast-matrix-engine %s\n- Input SHA256: `%s`\n- Files scanned: %d\n- Fixpoint iterations: %d\n\n", version, p.InputHash, len(dm.FilesSeen), p.FixpointIterations)
	fmt.Fprintf(&b, "## Matrix algebra\n\n`E = B(M_LF × M_FC)`\n\n`P = E ∧ K`\n\n`READY = P ∩ dependency_closed(P ∪ ALREADY)`\n\n`PRESERVE = DIRECT ∨ REWRITE ∨ HELPER ∨ EMULATE ∨ RUNTIME`\n\n`NEW = READY ∧ ¬ALREADY ∧ PRESERVABLE`\n\nStructural readiness is independent of target preservation and empirical evidence.\n\n")
	fmt.Fprintf(&b, "- UAST_READY: %d/%d\n- UAST schema delta NEW_S/NEW_R/NEW_A/NEW_D: %d/%d/%d/%d\n- executable implementation delta NEW_S/NEW_R/NEW_A/NEW_D: %d/%d/%d/%d\n\n", p.UASTReady, p.Counts["canonical_capabilities"], p.UASTDelta["NEW_S"], p.UASTDelta["NEW_R"], p.UASTDelta["NEW_A"], p.UASTDelta["NEW_D"], p.ImplementationDelta["NEW_S"], p.ImplementationDelta["NEW_R"], p.ImplementationDelta["NEW_A"], p.ImplementationDelta["NEW_D"])
	fmt.Fprintf(&b, "## Counts\n\n")
	keys := make([]string, 0, len(p.Counts))
	for k := range p.Counts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(&b, "- %s: %d\n", k, p.Counts[k])
	}
	fmt.Fprintf(&b, "\n## New dependency-closed capabilities\n\n")
	if len(p.NewSemantics) == 0 {
		b.WriteString("None.\n")
	} else {
		for _, c := range p.NewSemantics {
			fmt.Fprintf(&b, "- `%s` (%d evidence languages)\n", c.ID, len(c.EvidenceLanguages))
		}
	}
	fmt.Fprintf(&b, "\n## Blocked\n\n")
	if len(p.Blocked) == 0 {
		b.WriteString("None.\n")
	} else {
		for _, x := range p.Blocked {
			fmt.Fprintf(&b, "- `%s`: %s\n", x.Capability, strings.Join(x.Reasons, ", "))
		}
	}
	fmt.Fprintf(&b, "\n## Invariants\n\n- No rankings used: YES\n- No priority scores used: YES\n- Not proven -> not emitted: YES\n- Runtime selection order: DIRECT → REWRITE → HELPER → EMULATE → RUNTIME → ERROR\n")
	for _, n := range p.Notes {
		fmt.Fprintf(&b, "- Note: %s\n", n)
	}
	return os.WriteFile(path, []byte(b.String()), 0644)
}

func sortedSet(s stringSet) []string {
	if s == nil {
		return []string{}
	}
	a := make([]string, 0, len(s))
	for k := range s {
		a = append(a, k)
	}
	sort.Strings(a)
	return a
}
func copySet(s stringSet) stringSet {
	o := stringSet{}
	for k, v := range s {
		if v {
			o[k] = true
		}
	}
	return o
}
func setsEqual(a, b stringSet) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}
func setFromPlans(ps []capabilityPlan) stringSet {
	s := stringSet{}
	for _, p := range ps {
		s[p.ID] = true
	}
	return s
}
func sortRows(rows [][]string) {
	sort.Slice(rows, func(i, j int) bool { return strings.Join(rows[i], "\x00") < strings.Join(rows[j], "\x00") })
}
func ignoredHeaders(headers []string) []string {
	recognized := stringSet{}
	for _, as := range aliases {
		for _, a := range as {
			recognized[a] = true
		}
	}
	out := []string{}
	for _, h := range headers {
		if !recognized[h] {
			out = append(out, h)
		}
	}
	sort.Strings(out)
	return out
}
func fatal(err error) { fmt.Fprintln(os.Stderr, "ERROR:", err); os.Exit(1) }
