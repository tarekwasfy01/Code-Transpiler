// Copyright (c) 2026 Tarek Wasfy
package backend

// This file implements a compiler-neutral, provenance-preserving source
// indexer for C#, C++, and LLVM TableGen. It intentionally extracts structural
// dispatch, call, construction, and declarative-pattern relations only. It
// never promotes matching names to canonical semantic equivalence.
import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const RepositoryEvidenceSchema = "compiler-repository-evidence/v1"

type CompilerRepositorySpec struct {
	Compiler           string   `json:"compiler"`
	Repository         string   `json:"repository"`
	Ref                string   `json:"branch_or_tag,omitempty"`
	Root               string   `json:"root"`
	Commit             string   `json:"commit,omitempty"`
	Version            string   `json:"version,omitempty"`
	License            string   `json:"license,omitempty"`
	TargetArchitecture string   `json:"target_architecture,omitempty"`
	Include            []string `json:"include"`
	Extensions         []string `json:"extensions"`
}

type CompilerRepositoryUnit struct {
	File     string `json:"file"`
	SHA256   string `json:"sha256"`
	Bytes    int    `json:"bytes"`
	Language string `json:"language"`
}

type CompilerRepositoryNode struct {
	ID         string                   `json:"id"`
	Compiler   string                   `json:"compiler"`
	Kind       string                   `json:"kind"`
	Identity   string                   `json:"identity"`
	Stage      CompilerStage            `json:"stage"`
	Location   CompilerEvidenceLocation `json:"location"`
	Attributes map[string]string        `json:"attributes,omitempty"`
}

type CompilerRepositoryEdge struct {
	From       string                   `json:"from"`
	Relation   string                   `json:"relation"`
	To         string                   `json:"to"`
	Confidence EvidenceConfidence       `json:"confidence"`
	Location   CompilerEvidenceLocation `json:"location"`
}

type CompilerRepositoryReport struct {
	SchemaVersion      string                   `json:"schema_version"`
	Compiler           string                   `json:"compiler"`
	Repository         string                   `json:"repository"`
	Ref                string                   `json:"branch_or_tag,omitempty"`
	Commit             string                   `json:"commit,omitempty"`
	Version            string                   `json:"version,omitempty"`
	ExtractedAt        string                   `json:"extracted_at_utc"`
	License            string                   `json:"license,omitempty"`
	SourceRoot         string                   `json:"source_root"`
	TargetArchitecture string                   `json:"target_architecture,omitempty"`
	Units              []CompilerRepositoryUnit `json:"source_units"`
	Nodes              []CompilerRepositoryNode `json:"nodes"`
	Edges              []CompilerRepositoryEdge `json:"edges"`
	Rules              []CompilerEvidence       `json:"rules"`
	Equivalences       []SemanticEquivalence    `json:"semantic_equivalences"`
	Gaps               []CompilerEvidenceGap    `json:"gaps"`
	Summary            map[string]int           `json:"summary"`
}

type CompilerMatrixNode struct {
	ID         string                     `json:"id"`
	Compiler   string                     `json:"compiler"`
	Kind       string                     `json:"kind"`
	Identity   string                     `json:"identity"`
	Stage      CompilerStage              `json:"stage,omitempty"`
	Attributes map[string]string          `json:"attributes,omitempty"`
	Locations  []CompilerEvidenceLocation `json:"locations"`
}

type CompilerMatrixEdge struct {
	From       string                     `json:"from"`
	Relation   string                     `json:"relation"`
	To         string                     `json:"to"`
	Confidence EvidenceConfidence         `json:"confidence"`
	Conditions []string                   `json:"conditions,omitempty"`
	Locations  []CompilerEvidenceLocation `json:"locations"`
}

type CompilerPrimitiveCandidate struct {
	ID                string                     `json:"id"`
	Compiler          string                     `json:"compiler"`
	SourceIdentity    string                     `json:"source_identity"`
	CanonicalIdentity string                     `json:"canonical_identity,omitempty"`
	Status            string                     `json:"status"`
	Reason            string                     `json:"reason"`
	EvidenceIDs       []string                   `json:"evidence_ids"`
	Locations         []CompilerEvidenceLocation `json:"locations"`
}

type CompilerEvidenceMatrix struct {
	SchemaVersion       string                       `json:"schema_version"`
	EvidenceAuthority   string                       `json:"evidence_authority"`
	Nodes               []CompilerMatrixNode         `json:"nodes"`
	Edges               []CompilerMatrixEdge         `json:"edges"`
	Rules               []CompilerEvidence           `json:"rules"`
	Equivalences        []SemanticEquivalence        `json:"semantic_equivalences"`
	PrimitiveCandidates []CompilerPrimitiveCandidate `json:"primitive_candidates"`
	Gaps                []CompilerEvidenceGap        `json:"gaps"`
	CompilerSummaries   []map[string]any             `json:"compiler_summaries"`
}

// NativeCoverageRow is a contract-first audit row. External source identities
// are kept separate from canonical identities; a row becomes confirmed only
// when an explicit crosswalk supplies the canonical contract and the native
// implementation has a matching evidence chain.
type NativeCoverageRow struct {
	CanonicalSemantic string   `json:"canonical_semantic"`
	RoslynEvidence    []string `json:"roslyn_evidence,omitempty"`
	LLVMEvidence      []string `json:"llvm_evidence,omitempty"`
	NativeEvidence    []string `json:"native_evidence,omitempty"`
	MachineOperations []string `json:"machine_operations,omitempty"`
	Status            string   `json:"status"`
	Reason            string   `json:"reason"`
}

type NativeCoverageAudit struct {
	SchemaVersion string              `json:"schema_version"`
	Rows          []NativeCoverageRow `json:"rows"`
}

type CompilerSemanticCrosswalk struct {
	CanonicalIdentity   string              `json:"canonical_identity"`
	CanonicalContractID string              `json:"canonical_contract_id,omitempty"`
	Relation            string              `json:"relation"`
	EvidenceIDs         []string            `json:"evidence_ids"`
	EvidenceContracts   map[string][]string `json:"evidence_contracts"`
	Conditions          []string            `json:"conditions,omitempty"`
	Confidence          EvidenceConfidence  `json:"confidence"`
}

type sourceToken struct {
	text       string
	line       int
	start, end int
}
type sourceFunction struct {
	name                      string
	start, bodyStart, bodyEnd int
}

var nonFunctionTokens = map[string]bool{"if": true, "for": true, "while": true, "switch": true, "catch": true, "using": true, "lock": true, "return": true, "sizeof": true, "typeof": true, "nameof": true, "new": true, "throw": true, "case": true, "class": true, "struct": true, "record": true, "namespace": true, "delegate": true, "def": true, "Pat": true, "PatFrag": true}

// ExtractCompilerRepositoryEvidence scans only files selected by a manifest.
// The resulting graph is source evidence, not an executable rule registry.
func ExtractCompilerRepositoryEvidence(spec CompilerRepositorySpec) (*CompilerRepositoryReport, error) {
	root, err := filepath.Abs(spec.Root)
	if err != nil {
		return nil, err
	}
	if spec.Compiler == "" || spec.Repository == "" {
		return nil, fmt.Errorf("compiler and repository are required")
	}
	if spec.Commit == "" {
		spec.Commit = sourceRepositoryCommit(root)
	}
	if spec.Version == "" {
		if spec.Commit != "" {
			spec.Version = spec.Commit
		} else if spec.Ref != "" {
			spec.Version = "ref:" + spec.Ref + "/commit-unavailable"
		} else {
			spec.Version = "working-tree"
		}
	}
	r := &CompilerRepositoryReport{SchemaVersion: RepositoryEvidenceSchema, Compiler: spec.Compiler, Repository: spec.Repository, Ref: spec.Ref, Commit: spec.Commit, Version: spec.Version, ExtractedAt: time.Now().UTC().Format(time.RFC3339Nano), License: spec.License, SourceRoot: filepath.ToSlash(root), TargetArchitecture: spec.TargetArchitecture, Summary: map[string]int{}}
	exts := map[string]bool{}
	for _, x := range spec.Extensions {
		exts[strings.ToLower(x)] = true
	}
	var paths []string
	for _, include := range spec.Include {
		p := filepath.Join(root, filepath.FromSlash(include))
		st, e := os.Stat(p)
		if e != nil {
			r.Gaps = append(r.Gaps, CompilerEvidenceGap{ID: "missing-source-" + sanitizeEvidenceID(include), Compiler: spec.Compiler, Gap: "SOURCE_PATH_UNAVAILABLE", Detail: e.Error(), Location: CompilerEvidenceLocation{Repository: spec.Repository, Commit: spec.Commit, File: filepath.ToSlash(include), Symbol: "manifest", StartLine: 1, EndLine: 1}})
			continue
		}
		if !st.IsDir() {
			paths = append(paths, p)
			continue
		}
		e = filepath.WalkDir(p, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				if d.Name() == "bin" || d.Name() == "obj" || d.Name() == ".git" || d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if exts[strings.ToLower(filepath.Ext(path))] {
				paths = append(paths, path)
			}
			return nil
		})
		if e != nil {
			return nil, e
		}
	}
	sort.Strings(paths)
	functionSymbols := map[string][]string{}
	for _, path := range paths {
		data, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return nil, e
		}
		rel = filepath.ToSlash(rel)
		indexer := newSourceIndexer(data)
		for _, fn := range indexer.functions() {
			id := functionEvidenceNodeID(spec.Compiler, rel, fn.name, indexer.lineAt(fn.start))
			functionSymbols[fn.name] = append(functionSymbols[fn.name], id)
		}
	}
	for _, path := range paths {
		data, e := os.ReadFile(path)
		if e != nil {
			return nil, e
		}
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return nil, e
		}
		rel = filepath.ToSlash(rel)
		lang := sourceLanguageForPath(path)
		set := newSourceIndexer(data)
		funcs := set.functions()
		unit := CompilerRepositoryUnit{File: rel, SHA256: hash, Bytes: len(data), Language: lang}
		r.Units = append(r.Units, unit)
		loc := func(symbol string, start, end int) CompilerEvidenceLocation {
			return CompilerEvidenceLocation{Repository: spec.Repository, Commit: spec.Commit, File: rel, SHA256: hash, Symbol: symbol, StartLine: set.lineAt(start), EndLine: set.lineAt(end)}
		}
		stage := compilerStageForPath(rel)
		for _, fn := range funcs {
			id := functionEvidenceNodeID(spec.Compiler, rel, fn.name, set.lineAt(fn.start))
			r.Nodes = append(r.Nodes, CompilerRepositoryNode{ID: id, Compiler: spec.Compiler, Kind: "function", Identity: fn.name, Stage: stage, Location: loc(fn.name, fn.start, fn.bodyEnd)})
		}
		set.collectDispatchEvidence(spec, r, unit, loc, stage, funcs)
		set.collectTableGenEvidence(spec, r, unit, loc, stage)
		set.collectCalls(spec, r, unit, loc, funcs, functionSymbols)
	}
	// Stable order makes identical source snapshots produce byte-stable JSONL.
	sort.Slice(r.Rules, func(i, j int) bool { return r.Rules[i].ID < r.Rules[j].ID })
	sort.Slice(r.Edges, func(i, j int) bool {
		a, b := r.Edges[i], r.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.Relation != b.Relation {
			return a.Relation < b.Relation
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Location.StartLine < b.Location.StartLine
	})
	sort.Slice(r.Nodes, func(i, j int) bool { return r.Nodes[i].ID < r.Nodes[j].ID })
	r.Summary["source_units"] = len(r.Units)
	r.Summary["functions_indexed"] = countRepositoryNodes(r.Nodes, "function")
	r.Summary["structural_rules"] = len(r.Rules)
	r.Summary["graph_edges"] = len(r.Edges)
	r.Summary["semantic_equivalences"] = len(r.Equivalences)
	r.Summary["source_paths_missing"] = len(r.Gaps)
	return r, nil
}

func functionEvidenceNodeID(compiler, file, name string, line int) string {
	return "src:" + sanitizeEvidenceID(compiler) + ":" + sanitizeEvidenceID(name) + ":" + file + ":" + fmt.Sprint(line)
}

func countRepositoryNodes(n []CompilerRepositoryNode, kind string) int {
	c := 0
	for _, x := range n {
		if x.Kind == kind {
			c++
		}
	}
	return c
}
func sourceRepositoryCommit(root string) string {
	c := exec.Command("git", "-C", root, "rev-parse", "HEAD")
	b, e := c.Output()
	if e != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
func sourceLanguageForPath(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".cs":
		return "csharp"
	case ".cpp", ".cc", ".cxx", ".h", ".hpp", ".td":
		return "cpp"
	default:
		return "unknown"
	}
}
func compilerStageForPath(p string) CompilerStage {
	s := strings.ToLower(p)
	switch {
	case strings.HasSuffix(s, ".td"):
		return CompilerStageInstructionSelect
	case strings.Contains(s, "lowering"):
		return CompilerStageLowering
	case strings.Contains(s, "selectiondag"), strings.Contains(s, "globalisel"), strings.Contains(s, "isel"):
		return CompilerStageInstructionSelect
	case strings.Contains(s, "legaliz"):
		return CompilerStageLegalization
	case strings.Contains(s, "emit"), strings.Contains(s, "codegen"):
		return CompilerStageCodeGeneration
	case strings.Contains(s, "binder"), strings.Contains(s, "binding"):
		return CompilerStageBinding
	case strings.Contains(s, "parser"):
		return "parsing"
	default:
		return "compiler_source"
	}
}

// Tokenization is deliberately lexical, not regex-based: comments and quoted
// text cannot create false dispatches, calls, or TableGen declarations.
func newSourceIndexer(data []byte) *sourceIndexer {
	s := &sourceIndexer{data: data, tokens: lexSource(data), lineStarts: []int{0}, pairs: map[int]int{}}
	for i, b := range data {
		if b == '\n' {
			s.lineStarts = append(s.lineStarts, i+1)
		}
	}
	type delimiter struct {
		index int
		text  string
	}
	var stack []delimiter
	for i, tok := range s.tokens {
		if tok.text == "(" || tok.text == "{" || tok.text == "[" {
			stack = append(stack, delimiter{i, tok.text})
			continue
		}
		want := ""
		switch tok.text {
		case ")":
			want = "("
		case "}":
			want = "{"
		case "]":
			want = "["
		}
		if want == "" {
			continue
		}
		for j := len(stack) - 1; j >= 0; j-- {
			if stack[j].text == want {
				open := stack[j].index
				s.pairs[open] = i
				s.pairs[i] = open
				stack = stack[:j]
				break
			}
		}
	}
	return s
}

type sourceIndexer struct {
	data       []byte
	tokens     []sourceToken
	lineStarts []int
	pairs      map[int]int
}

func lexSource(b []byte) []sourceToken {
	var out []sourceToken
	line := 1
	for i := 0; i < len(b); {
		c := b[i]
		if c == '\n' {
			line++
			i++
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' {
			i++
			continue
		}
		if i+1 < len(b) && c == '/' && b[i+1] == '/' {
			for i < len(b) && b[i] != '\n' {
				i++
			}
			continue
		}
		if i+1 < len(b) && c == '/' && b[i+1] == '*' {
			i += 2
			for i < len(b) {
				if b[i] == '\n' {
					line++
				}
				if i+1 < len(b) && b[i] == '*' && b[i+1] == '/' {
					i += 2
					break
				}
				i++
			}
			continue
		}
		start := i
		ln := line
		if c == '"' || c == '\'' {
			quote := c
			i++
			for i < len(b) {
				if b[i] == '\\' {
					i += 2
					continue
				}
				if b[i] == '\n' {
					line++
				}
				if b[i] == quote {
					i++
					break
				}
				i++
			}
			out = append(out, sourceToken{text: string(b[start:i]), line: ln, start: start, end: i})
			continue
		}
		if isSourceIdent(c) {
			i++
			for i < len(b) && isSourceIdentContinue(b[i]) {
				i++
			}
			out = append(out, sourceToken{text: string(b[start:i]), line: ln, start: start, end: i})
			continue
		}
		if i+1 < len(b) && ((c == ':' && b[i+1] == ':') || (c == '=' && b[i+1] == '>') || (c == '=' && b[i+1] == '=') || (c == '!' && b[i+1] == '=') || (c == '<' && b[i+1] == '=') || (c == '>' && b[i+1] == '=') || (c == '&' && b[i+1] == '&') || (c == '|' && b[i+1] == '|')) {
			i += 2
		} else {
			i++
		}
		out = append(out, sourceToken{text: string(b[start:i]), line: ln, start: start, end: i})
	}
	return out
}
func isSourceIdent(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_' || c == '$'
}
func isSourceIdentContinue(c byte) bool        { return isSourceIdent(c) || c >= '0' && c <= '9' }
func tokenStartsIdentifier(t sourceToken) bool { return len(t.text) > 0 && isSourceIdent(t.text[0]) }
func (s *sourceIndexer) lineAt(offset int) int {
	if offset < 0 {
		return 1
	}
	if offset > len(s.data) {
		offset = len(s.data)
	}
	return sort.Search(len(s.lineStarts), func(i int) bool { return s.lineStarts[i] > offset })
}
func (s *sourceIndexer) closeToken(i int) int {
	if j, ok := s.pairs[i]; ok {
		return j
	}
	return -1
}
func (s *sourceIndexer) functions() []sourceFunction {
	t := s.tokens
	var out []sourceFunction
	seen := map[int]bool{}
	for i := 0; i < len(t); i++ {
		if t[i].text != "{" {
			continue
		}
		paren := -1
		for j := i - 1; j >= 0 && i-j < 100; j-- {
			if t[j].text == ")" {
				if open, ok := s.pairs[j]; ok {
					paren = open
				}
				break
			}
			if t[j].text == ";" || t[j].text == "}" {
				break
			}
		}
		if paren < 1 || t[paren-1].text == "." || nonFunctionTokens[t[paren-1].text] {
			continue
		}
		name := t[paren-1].text
		if !isSourceIdent(name[0]) {
			continue
		}
		if seen[paren-1] {
			continue
		}
		close := s.closeToken(i)
		if close < 0 {
			continue
		}
		seen[paren-1] = true
		out = append(out, sourceFunction{name: name, start: t[paren-1].start, bodyStart: t[i].start, bodyEnd: t[close].end})
	}
	return out
}

func (s *sourceIndexer) collectDispatchEvidence(spec CompilerRepositorySpec, r *CompilerRepositoryReport, u CompilerRepositoryUnit, loc func(string, int, int) CompilerEvidenceLocation, stage CompilerStage, funcs []sourceFunction) {
	t := s.tokens
	for i := 0; i < len(t); i++ {
		if t[i].text != "switch" {
			continue
		}
		p := i + 1
		if p >= len(t) || t[p].text != "(" {
			continue
		}
		pe := s.closeToken(p)
		if pe < 0 || pe+1 >= len(t) || t[pe+1].text != "{" {
			continue
		}
		be := s.closeToken(pe + 1)
		if be < 0 {
			continue
		}
		owner := "<dispatch>"
		for _, fn := range funcs {
			if t[i].start >= fn.bodyStart && t[i].end <= fn.bodyEnd {
				owner = fn.name
			}
		}
		var tag []string
		for j := p + 1; j < pe; j++ {
			tag = append(tag, t[j].text)
		}
		tagText := strings.Join(tag, "")
		for j := pe + 2; j < be; j++ {
			if t[j].text != "case" {
				continue
			}
			k := j + 1
			var labels []string
			for k < be && t[k].text != ":" {
				labels = append(labels, t[k].text)
				k++
			}
			if len(labels) == 0 || k >= be {
				continue
			}
			bodyStart := k + 1
			bodyEnd := be
			depth := 0
			for q := bodyStart; q < be; q++ {
				switch t[q].text {
				case "{":
					depth++
				case "}":
					depth--
				}
				if depth == 0 && t[q].text == "case" || depth == 0 && t[q].text == "default" {
					bodyEnd = q
					break
				}
			}
			outs := map[string]bool{}
			for q := bodyStart; q < bodyEnd; q++ {
				if q+1 < bodyEnd && t[q].text == "new" && tokenStartsIdentifier(t[q+1]) {
					outs["construct:"+t[q+1].text] = true
				}
				if tokenStartsIdentifier(t[q]) && q+1 < bodyEnd && t[q+1].text == "(" && !nonFunctionTokens[t[q].text] {
					outs["call:"+t[q].text] = true
				}
			}
			if len(outs) > 0 {
				output := make([]string, 0, len(outs))
				for x := range outs {
					output = append(output, x)
				}
				sort.Strings(output)
				label := strings.Join(labels, "")
				id := "repo." + sanitizeEvidenceID(spec.Compiler) + ".dispatch." + sanitizeEvidenceID(owner) + "." + sanitizeEvidenceID(label) + "." + fmt.Sprint(t[j].line)
				where := loc(owner, t[j].start, t[bodyEnd-1].end)
				identity := "source.dispatch:" + tagText + "=" + label
				r.Rules = append(r.Rules, CompilerEvidence{ID: id, Compiler: spec.Compiler, Version: r.Version, Stage: stage, TargetArchitecture: spec.TargetArchitecture, SemanticIdentity: identity, InputPattern: identity, Conditions: []string{"dispatch_owner=" + owner, "dispatch_expression=" + tagText}, OutputPattern: output, ReferencedOperations: output, Relation: "dispatch_to_implementation_component", Confidence: EvidenceDirect, Locations: []CompilerEvidenceLocation{where}})
				for _, o := range output {
					r.Edges = append(r.Edges, CompilerRepositoryEdge{From: id, Relation: "dispatch_to", To: o, Confidence: EvidenceDirect, Location: where})
				}
			} else {
				endPos := t[k].end
				if bodyEnd > bodyStart {
					endPos = t[bodyEnd-1].end
				}
				label := strings.Join(labels, "")
				where := loc(owner, t[j].start, endPos)
				r.Gaps = append(r.Gaps, CompilerEvidenceGap{ID: "gap.dispatch." + sanitizeEvidenceID(spec.Compiler) + "." + sanitizeEvidenceID(owner) + "." + sanitizeEvidenceID(label) + "." + fmt.Sprint(t[j].line), Compiler: spec.Compiler, SemanticIdentity: "source.dispatch:" + tagText + "=" + label, Gap: "DISPATCH_OUTPUT_UNRESOLVED", Detail: "dispatch branch was indexed, but no construction or call output was structurally resolved", Location: where})
			}
			j = bodyStart
		}
	}
}

func (s *sourceIndexer) collectTableGenEvidence(spec CompilerRepositorySpec, r *CompilerRepositoryReport, u CompilerRepositoryUnit, loc func(string, int, int) CompilerEvidenceLocation, stage CompilerStage) {
	if !strings.EqualFold(filepath.Ext(u.File), ".td") {
		return
	}
	t := s.tokens
	for i := 0; i < len(t); i++ {
		kind := t[i].text
		if kind != "def" && kind != "defm" && kind != "class" && kind != "multiclass" {
			continue
		}
		if i+1 >= len(t) {
			continue
		}
		name := ""
		j := i + 1
		if tokenStartsIdentifier(t[i+1]) {
			name = t[i+1].text
			j = i + 2
		} else if kind == "def" && t[i+1].text == ":" {
			name = fmt.Sprintf("anonymous_at_%d", t[i].line)
		} else {
			continue
		}
		end := j
		for end < len(t) && t[end].text != ";" && t[end].text != "{" && end-j < 120 {
			end++
		}
		if end < len(t) && t[end].text == "{" {
			x := s.closeToken(end)
			if x >= 0 {
				end = x
			}
		}
		if end >= len(t) {
			end = len(t) - 1
		}
		rawTokens := t[i : end+1]
		attrs := map[string]string{}
		contains := map[string]bool{}
		for _, x := range rawTokens {
			if x.text == "Pat" || x.text == "PatFrag" || x.text == "Instruction" || x.text == "RegisterClass" || x.text == "Predicate" || x.text == "SDNode" || x.text == "bits" {
				contains[x.text] = true
			}
		}
		id := "td." + sanitizeEvidenceID(spec.Compiler) + "." + sanitizeEvidenceID(kind) + "." + sanitizeEvidenceID(name) + "." + fmt.Sprint(t[i].line)
		where := loc(name, t[i].start, t[end].end)
		attrs["declaration_kind"] = kind
		attrs["source_body"] = string(s.data[t[i].start:t[end].end])
		for k := range contains {
			attrs[k] = "true"
		}
		r.Nodes = append(r.Nodes, CompilerRepositoryNode{ID: id, Compiler: spec.Compiler, Kind: "tablegen_" + kind, Identity: name, Stage: stage, Location: where, Attributes: attrs})
		if contains["Pat"] || contains["PatFrag"] {
			r.Rules = append(r.Rules, CompilerEvidence{ID: id, Compiler: spec.Compiler, Version: r.Version, Stage: stage, TargetArchitecture: spec.TargetArchitecture, SemanticIdentity: "tablegen.declaration:" + name, InputPattern: "TableGen declaration:" + name, Conditions: []string{"declaration_kind=" + kind, "pattern_construct=" + strings.Join(sortedKeys(contains), ",")}, OutputPattern: []string{"source_body=" + string(s.data[t[i].start:t[end].end])}, Relation: "declarative_target_pattern", Confidence: EvidenceDirect, Locations: []CompilerEvidenceLocation{where}})
		}
		for typ := range contains {
			r.Edges = append(r.Edges, CompilerRepositoryEdge{From: id, Relation: "declares_technique", To: typ, Confidence: EvidenceDirect, Location: where})
		}
		i = end
	}
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *sourceIndexer) collectCalls(spec CompilerRepositorySpec, r *CompilerRepositoryReport, u CompilerRepositoryUnit, loc func(string, int, int) CompilerEvidenceLocation, funcs []sourceFunction, functionSymbols map[string][]string) {
	t := s.tokens
	seen := map[string]bool{}
	for _, fn := range funcs {
		for i := 0; i+1 < len(t); i++ {
			if t[i].start < fn.bodyStart || t[i].end > fn.bodyEnd || len(functionSymbols[t[i].text]) == 0 || t[i+1].text != "(" {
				continue
			}
			if nonFunctionTokens[t[i].text] {
				continue
			}
			key := fn.name + "\x00" + t[i].text + "\x00" + fmt.Sprint(t[i].line)
			if seen[key] {
				continue
			}
			seen[key] = true
			from := functionEvidenceNodeID(spec.Compiler, u.File, fn.name, s.lineAt(fn.start))
			where := loc(fn.name+"->"+t[i].text, t[i].start, t[i+1].end)
			targets := functionSymbols[t[i].text]
			confidence := EvidenceDirect
			relation := "calls"
			if len(targets) > 1 {
				confidence = EvidenceCandidate
				relation = "ambiguous_call_candidate"
			}
			for _, to := range targets {
				r.Edges = append(r.Edges, CompilerRepositoryEdge{From: from, Relation: relation, To: to, Confidence: confidence, Location: where})
			}
		}
	}
}

// WriteCompilerRepositoryEvidence persists both the graph and its matrix
// projection. Empty equivalence output is intentional until an explicit,
// condition-aware crosswalk establishes semantic equivalence.
func WriteCompilerRepositoryEvidence(r *CompilerRepositoryReport, outDir string) error {
	if r == nil {
		return fmt.Errorf("nil compiler source evidence")
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	write := func(name string, v any) error {
		b, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			return e
		}
		return os.WriteFile(filepath.Join(outDir, name), append(b, '\n'), 0644)
	}
	idx := map[string]any{"schema_version": r.SchemaVersion, "compiler": r.Compiler, "repository": r.Repository, "branch_or_tag": r.Ref, "commit": r.Commit, "version": r.Version, "extracted_at_utc": r.ExtractedAt, "license": r.License, "source_root": r.SourceRoot, "target_architecture": r.TargetArchitecture, "source_units": r.Units}
	if e := write("compiler-evidence-index.json", idx); e != nil {
		return e
	}
	if e := write("evidence-summary.json", map[string]any{"schema_version": r.SchemaVersion, "compiler": r.Compiler, "repository": r.Repository, "branch_or_tag": r.Ref, "commit": r.Commit, "version": r.Version, "extracted_at_utc": r.ExtractedAt, "license": r.License, "source_units": len(r.Units), "functions_indexed": r.Summary["functions_indexed"], "structural_rules": len(r.Rules), "semantic_equivalences": len(r.Equivalences), "missing_source_paths": len(r.Gaps)}); e != nil {
		return e
	}
	if e := writeJSONL(filepath.Join(outDir, "translation-rules.jsonl"), r.Rules); e != nil {
		return e
	}
	if e := writeJSONL(filepath.Join(outDir, "semantic-equivalences.jsonl"), r.Equivalences); e != nil {
		return e
	}
	if e := writeJSONL(filepath.Join(outDir, "semantic-gaps.jsonl"), r.Gaps); e != nil {
		return e
	}
	if e := writeJSONL(filepath.Join(outDir, "compiler-graph.jsonl"), r.Edges); e != nil {
		return e
	}
	if e := writeJSONL(filepath.Join(outDir, "source-nodes.jsonl"), r.Nodes); e != nil {
		return e
	}
	md := fmt.Sprintf("# Compiler source evidence\n\n- Compiler: `%s`\n- Repository: `%s`\n- Branch/tag: `%s`\n- Commit/version: `%s` / `%s`\n- Extracted UTC: `%s`\n- License: `%s`\n- Source units: %d\n- Functions indexed: %d\n- Structural rules: %d\n- Semantic equivalences: %d\n- Manifest paths unavailable: %d\n\nAll rows retain source path, line range, commit and per-file SHA-256. Extracted dispatches and TableGen declarations are implementation evidence only. No source names were promoted to canonical semantic equivalence. Missing source roots remain explicit gaps.\n", r.Compiler, r.Repository, r.Ref, r.Commit, r.Version, r.ExtractedAt, r.License, len(r.Units), r.Summary["functions_indexed"], len(r.Rules), len(r.Equivalences), len(r.Gaps))
	return os.WriteFile(filepath.Join(outDir, "extraction-report.md"), []byte(md), 0644)
}

// MergeCompilerEvidenceMatrix joins independently extracted compiler views.
// Raw compiler identities remain namespaced; only a future explicit,
// condition-aware crosswalk may create cross-compiler equivalence edges.
func MergeCompilerEvidenceMatrix(repositories []*CompilerRepositoryReport, native *NativeCompilerEvidenceReport) *CompilerEvidenceMatrix {
	m := &CompilerEvidenceMatrix{SchemaVersion: "compiler-semantic-matrix/v1", EvidenceAuthority: "empirical_authoritative_structured_source"}
	nodeSeen := map[string]bool{}
	addNode := func(n CompilerMatrixNode) {
		if !nodeSeen[n.ID] {
			nodeSeen[n.ID] = true
			m.Nodes = append(m.Nodes, n)
		}
	}
	for _, r := range repositories {
		if r == nil {
			continue
		}
		for _, n := range r.Nodes {
			addNode(CompilerMatrixNode{ID: n.ID, Compiler: n.Compiler, Kind: n.Kind, Identity: n.Identity, Stage: n.Stage, Attributes: n.Attributes, Locations: []CompilerEvidenceLocation{n.Location}})
		}
		for _, rule := range r.Rules {
			addNode(CompilerMatrixNode{ID: "evidence:" + rule.ID, Compiler: rule.Compiler, Kind: "evidence_rule", Identity: rule.SemanticIdentity, Stage: rule.Stage, Attributes: map[string]string{"relation": rule.Relation, "confidence": string(rule.Confidence)}, Locations: rule.Locations})
		}
		for _, e := range r.Edges {
			m.Edges = append(m.Edges, CompilerMatrixEdge{From: e.From, Relation: e.Relation, To: e.To, Confidence: e.Confidence, Locations: []CompilerEvidenceLocation{e.Location}})
		}
		m.Rules = append(m.Rules, r.Rules...)
		m.Equivalences = append(m.Equivalences, r.Equivalences...)
		m.Gaps = append(m.Gaps, r.Gaps...)
		m.CompilerSummaries = append(m.CompilerSummaries, map[string]any{"compiler": r.Compiler, "repository": r.Repository, "branch_or_tag": r.Ref, "commit": r.Commit, "version": r.Version, "extracted_at_utc": r.ExtractedAt, "license": r.License, "source_units": len(r.Units), "functions_indexed": r.Summary["functions_indexed"], "structural_rules": len(r.Rules), "graph_edges": len(r.Edges), "semantic_equivalences": len(r.Equivalences), "missing_source_paths": len(r.Gaps)})
	}
	if native != nil {
		for _, rule := range native.Rules {
			id := "evidence:" + rule.ID
			addNode(CompilerMatrixNode{ID: id, Compiler: rule.Compiler, Kind: "evidence_rule", Identity: rule.SemanticIdentity, Stage: rule.Stage, Attributes: map[string]string{"relation": rule.Relation, "confidence": string(rule.Confidence)}, Locations: rule.Locations})
			for _, out := range rule.OutputPattern {
				to := "native-operation:" + out
				addNode(CompilerMatrixNode{ID: to, Compiler: rule.Compiler, Kind: "implementation_operation", Identity: out, Stage: rule.Stage, Locations: rule.Locations})
				m.Edges = append(m.Edges, CompilerMatrixEdge{From: id, Relation: rule.Relation, To: to, Confidence: rule.Confidence, Conditions: rule.Conditions, Locations: rule.Locations})
			}
		}
		for _, e := range native.CallEdges {
			m.Edges = append(m.Edges, CompilerMatrixEdge{From: "native-function:" + e.From, Relation: "calls", To: "native-function:" + e.To, Confidence: EvidenceDirect, Locations: []CompilerEvidenceLocation{e.Location}})
		}
		m.Rules = append(m.Rules, native.Rules...)
		m.Equivalences = append(m.Equivalences, native.Equivalences...)
		m.Gaps = append(m.Gaps, native.Gaps...)
		m.CompilerSummaries = append(m.CompilerSummaries, map[string]any{"compiler": native.Compiler, "repository": native.Repository, "version": native.Version, "source_units": native.Units, "functions_indexed": native.Functions, "structural_rules": len(native.Rules), "graph_edges": len(native.CallEdges), "semantic_equivalences": len(native.Equivalences), "license": "project source"})
	}
	// Candidate rows preserve observed source identities without asserting that
	// they are canonical primitives or equivalent across compilers.
	candidateSeen := map[string]bool{}
	for _, rule := range m.Rules {
		if rule.SemanticIdentity == "" || len(rule.Locations) == 0 {
			continue
		}
		if rule.Relation != "lowering_component" && rule.Relation != "dispatch_to_implementation_component" && rule.Relation != "declarative_target_pattern" {
			continue
		}
		key := rule.Compiler + "\x00" + rule.SemanticIdentity
		if candidateSeen[key] {
			continue
		}
		candidateSeen[key] = true
		m.PrimitiveCandidates = append(m.PrimitiveCandidates, CompilerPrimitiveCandidate{ID: "candidate:" + sanitizeEvidenceID(rule.Compiler) + ":" + sanitizeEvidenceID(rule.SemanticIdentity), Compiler: rule.Compiler, SourceIdentity: rule.SemanticIdentity, Status: "candidate", Reason: "source evidence exists, but no explicit canonical semantic contract and compatible cross-source proof is attached", EvidenceIDs: []string{rule.ID}, Locations: rule.Locations})
	}
	sort.Slice(m.Nodes, func(i, j int) bool { return m.Nodes[i].ID < m.Nodes[j].ID })
	sort.Slice(m.Edges, func(i, j int) bool {
		a, b := m.Edges[i], m.Edges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.Relation != b.Relation {
			return a.Relation < b.Relation
		}
		return a.To < b.To
	})
	sort.Slice(m.Rules, func(i, j int) bool { return m.Rules[i].ID < m.Rules[j].ID })
	sort.Slice(m.PrimitiveCandidates, func(i, j int) bool { return m.PrimitiveCandidates[i].ID < m.PrimitiveCandidates[j].ID })
	return m
}

// ApplyExplicitCompilerCrosswalks adds cross-compiler claims only from an
// authored crosswalk that names existing evidence and compatible conditions.
// Raw identifier similarity is never consulted.
func ApplyExplicitCompilerCrosswalks(m *CompilerEvidenceMatrix, rows []CompilerSemanticCrosswalk) error {
	if m == nil {
		return fmt.Errorf("nil compiler matrix")
	}
	rules := map[string]CompilerEvidence{}
	for _, r := range m.Rules {
		rules[r.ID] = r
	}
	for _, row := range rows {
		if strings.TrimSpace(row.CanonicalIdentity) == "" || (row.Relation != "exact_equivalent" && row.Relation != "equivalent_under_conditions") {
			return fmt.Errorf("invalid explicit compiler crosswalk for %q", row.CanonicalIdentity)
		}
		if row.Relation == "equivalent_under_conditions" && len(row.Conditions) == 0 {
			return fmt.Errorf("conditional equivalence %q has no conditions", row.CanonicalIdentity)
		}
		compilers := map[string]bool{}
		var contractSignature string
		for _, id := range row.EvidenceIDs {
			e, ok := rules[id]
			if !ok {
				return fmt.Errorf("crosswalk %q references unknown evidence %q", row.CanonicalIdentity, id)
			}
			compilers[e.Compiler] = true
			if len(e.Locations) == 0 || e.Locations[0].File == "" || e.Locations[0].SHA256 == "" {
				return fmt.Errorf("crosswalk evidence %q has no source provenance", id)
			}
			contract, ok := row.EvidenceContracts[id]
			if !ok {
				return fmt.Errorf("crosswalk %q has no normalized contract for evidence %q", row.CanonicalIdentity, id)
			}
			contract = sortedStrings(contract)
			signature := strings.Join(contract, "\x00")
			if contractSignature == "" {
				contractSignature = signature
			} else if contractSignature != signature {
				return fmt.Errorf("crosswalk %q has incompatible semantic preconditions across sources", row.CanonicalIdentity)
			}
		}
		if len(row.EvidenceIDs) < 2 || len(compilers) < 2 {
			return fmt.Errorf("crosswalk %q must cite evidence from at least two compilers", row.CanonicalIdentity)
		}
		if row.Confidence != EvidenceDirect && row.Confidence != EvidenceDerived && row.Confidence != EvidenceInferred && row.Confidence != EvidenceCandidate {
			return fmt.Errorf("crosswalk %q has invalid confidence", row.CanonicalIdentity)
		}
		id := "equivalence:" + sanitizeEvidenceID(row.CanonicalIdentity) + ":" + strings.Join(sortedStrings(row.EvidenceIDs), "+")
		m.Equivalences = append(m.Equivalences, SemanticEquivalence{ID: id, Concept: row.CanonicalIdentity, Relation: row.Relation, Conditions: append([]string(nil), row.Conditions...), EvidenceIDs: sortedStrings(row.EvidenceIDs), Confidence: row.Confidence})
		for _, ev := range row.EvidenceIDs {
			m.Edges = append(m.Edges, CompilerMatrixEdge{From: "evidence:" + ev, Relation: row.Relation, To: "canonical:" + row.CanonicalIdentity, Confidence: row.Confidence, Conditions: append([]string(nil), row.Conditions...), Locations: rules[ev].Locations})
		}
	}
	sort.Slice(m.Equivalences, func(i, j int) bool { return m.Equivalences[i].ID < m.Equivalences[j].ID })
	return nil
}

// BuildNativeCoverageAudit produces a conservative comparison of external
// evidence and the native source evidence. No canonical operation is inferred
// from spelling. Explicit crosswalk equivalences are the sole authority for a
// confirmed row; all other rows remain candidates or insufficient evidence.
func BuildNativeCoverageAudit(m *CompilerEvidenceMatrix) *NativeCoverageAudit {
	audit := &NativeCoverageAudit{SchemaVersion: "native-coverage-matrix/v1"}
	if m == nil {
		return audit
	}
	byCanonical := map[string]*NativeCoverageRow{}
	for _, eq := range m.Equivalences {
		row := byCanonical[eq.Concept]
		if row == nil {
			row = &NativeCoverageRow{CanonicalSemantic: eq.Concept, Status: "insufficient_evidence"}
			byCanonical[eq.Concept] = row
		}
		for _, id := range eq.EvidenceIDs {
			for _, rule := range m.Rules {
				if rule.ID != id {
					continue
				}
				switch rule.Compiler {
				case "Roslyn C# compiler":
					row.RoslynEvidence = append(row.RoslynEvidence, id)
				case "LLVM":
					row.LLVMEvidence = append(row.LLVMEvidence, id)
				default:
					row.NativeEvidence = append(row.NativeEvidence, id)
					row.MachineOperations = append(row.MachineOperations, rule.OutputPattern...)
				}
			}
		}
		// An explicit crosswalk is the contract authority.  Once it connects
		// external compiler evidence to a native implementation rule, retain the
		// claim as confirmed (or conditional) in the audit; un-crosswalked
		// candidates below remain insufficient evidence.
		if len(row.NativeEvidence) > 0 && (len(row.LLVMEvidence) > 0 || len(row.RoslynEvidence) > 0) {
			if eq.Relation == "equivalent_under_conditions" {
				row.Status = "confirmed_under_conditions"
				row.Reason = "explicit crosswalk joins compiler evidence and native implementation under declared conditions"
			} else {
				row.Status = "confirmed"
				row.Reason = "explicit crosswalk joins compiler evidence and native implementation"
			}
		} else {
			row.Status = "insufficient_evidence"
			row.Reason = "explicit canonical equivalence exists, but native compiler evidence is incomplete"
		}
	}
	for _, candidate := range m.PrimitiveCandidates {
		key := "candidate:" + candidate.ID
		if _, ok := byCanonical[key]; ok {
			continue
		}
		byCanonical[key] = &NativeCoverageRow{CanonicalSemantic: key, Status: "insufficient_evidence", Reason: candidate.Reason}
	}
	for _, row := range byCanonical {
		sort.Strings(row.RoslynEvidence)
		sort.Strings(row.LLVMEvidence)
		sort.Strings(row.NativeEvidence)
		sort.Strings(row.MachineOperations)
		audit.Rows = append(audit.Rows, *row)
	}
	sort.Slice(audit.Rows, func(i, j int) bool { return audit.Rows[i].CanonicalSemantic < audit.Rows[j].CanonicalSemantic })
	return audit
}

func sortedStrings(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

func WriteCompilerEvidenceMatrix(m *CompilerEvidenceMatrix, outDir string) error {
	if m == nil {
		return fmt.Errorf("nil compiler evidence matrix")
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	write := func(name string, v any) error {
		b, e := json.MarshalIndent(v, "", "  ")
		if e != nil {
			return e
		}
		return os.WriteFile(filepath.Join(outDir, name), append(b, '\n'), 0644)
	}
	if err := write("compiler-evidence-index.json", map[string]any{"schema_version": m.SchemaVersion, "evidence_authority": m.EvidenceAuthority, "compiler_summaries": m.CompilerSummaries, "nodes": len(m.Nodes), "edges": len(m.Edges), "rules": len(m.Rules), "primitive_candidates": len(m.PrimitiveCandidates), "semantic_equivalences": len(m.Equivalences), "gaps": len(m.Gaps)}); err != nil {
		return err
	}
	if err := write("evidence-summary.json", map[string]any{"schema_version": m.SchemaVersion, "evidence_authority": m.EvidenceAuthority, "compiler_count": len(m.CompilerSummaries), "nodes": len(m.Nodes), "graph_edges": len(m.Edges), "rules": len(m.Rules), "primitive_candidates": len(m.PrimitiveCandidates), "semantic_equivalences": len(m.Equivalences), "gaps": len(m.Gaps), "cross_compiler_equivalence_policy": "explicit canonical identity plus compatible preconditions only"}); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "source-nodes.jsonl"), m.Nodes); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "compiler-graph.jsonl"), m.Edges); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "translation-rules.jsonl"), m.Rules); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "semantic-equivalences.jsonl"), m.Equivalences); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "semantic-gaps.jsonl"), m.Gaps); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "primitive-candidates.jsonl"), m.PrimitiveCandidates); err != nil {
		return err
	}
	audit := BuildNativeCoverageAudit(m)
	if err := write("native-coverage-matrix.json", audit); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "native-rule-candidates.jsonl"), audit.Rows); err != nil {
		return err
	}
	if err := writeNativeCoverageTargetForms(filepath.Join(outDir, "crosswalk-target-forms.csv"), audit.Rows); err != nil {
		return err
	}
	if err := writeCanonicalOperationAudit(filepath.Join(outDir, "canonical-operation-audit.jsonl"), filepath.Join(outDir, "native-gaps.jsonl"), m); err != nil {
		return err
	}
	md := fmt.Sprintf("# Compiler semantic evidence matrix\n\n- Compiler inputs: %d\n- Source and evidence nodes: %d\n- Graph edges: %d\n- Translation/evidence rules: %d\n- Primitive candidates (not promoted): %d\n- Semantic equivalences: %d\n- Gaps: %d\n\nCompiler-native identities remain namespaced. No semantic equivalence is inferred from spelling or structural similarity; equivalences require an explicit canonical mapping with compatible preconditions. Source code is indexed with repository, commit, file hash, symbol and line provenance.\n", len(m.CompilerSummaries), len(m.Nodes), len(m.Edges), len(m.Rules), len(m.PrimitiveCandidates), len(m.Equivalences), len(m.Gaps))
	return os.WriteFile(filepath.Join(outDir, "extraction-report.md"), []byte(md), 0644)
}

func writeCanonicalOperationAudit(auditPath, gapsPath string, m *CompilerEvidenceMatrix) error {
	report, err := CompileUniversalPrimitiveSpecs()
	if err != nil {
		return err
	}
	confirmed := map[string]SemanticEquivalence{}
	for _, eq := range m.Equivalences {
		confirmed[eq.Concept] = eq
	}
	af, err := os.Create(auditPath)
	if err != nil {
		return err
	}
	defer af.Close()
	gf, err := os.Create(gapsPath)
	if err != nil {
		return err
	}
	defer gf.Close()
	ae, ge := json.NewEncoder(af), json.NewEncoder(gf)
	for _, spec := range report.Specs {
		status := "insufficient_evidence"
		evidence := []string(nil)
		eq, ok := confirmed[spec.ID]
		if !ok {
			machineID := map[string]string{"BINARY:ADD": "add", "BINARY:SUB": "sub", "BINARY:MUL": "mul", "AND": "and", "BIT_AND": "and", "BIT_OR": "or", "SHIFT:SHL": "shl", "SHIFT:SHR": "shr", "SHR_LOGICAL": "shr", "BINARY:REM": "rem"}[spec.ID]
			if machineID != "" {
				eq, ok = confirmed["machine.realization.x86_64."+machineID]
			}
		}
		if ok {
			status, evidence = eq.Relation, eq.EvidenceIDs
		}
		row := map[string]any{"canonical_identity": spec.ID, "category": spec.Family, "operand_contract": map[string]any{"arity": spec.Arity}, "current_native_support": status, "evidence_status": status, "crosswalk_evidence_ids": evidence}
		if err := ae.Encode(row); err != nil {
			return err
		}
		if status == "insufficient_evidence" {
			if err := ge.Encode(map[string]any{"canonical_identity": spec.ID, "contract": map[string]any{"arity": spec.Arity}, "gap_layer": "evidence", "evidence_ids": []string{}, "reason": "no explicit contract-validated crosswalk", "suggested_fix": "provide provenance-complete independent evidence", "confidence": "candidate"}); err != nil {
				return err
			}
		}
	}
	return nil
}

// writeNativeCoverageTargetForms materializes only confirmed crosswalk rows
// into the existing matrix output. It is a projection, not a second registry.
func writeNativeCoverageTargetForms(path string, rows []NativeCoverageRow) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if err := w.Write([]string{"canonical_identity", "target_architecture", "target_native_operation", "native_evidence_id", "external_evidence_ids", "conditions", "status"}); err != nil {
		return err
	}
	for _, row := range rows {
		if row.Status != "confirmed" && row.Status != "confirmed_under_conditions" {
			continue
		}
		parts := strings.Split(row.CanonicalSemantic, ".")
		op := parts[len(parts)-1]
		arch := ""
		if len(parts) >= 2 {
			arch = parts[len(parts)-2]
		}
		if err := w.Write([]string{row.CanonicalSemantic, arch, op, strings.Join(row.NativeEvidence, ";"), strings.Join(append(append([]string{}, row.RoslynEvidence...), row.LLVMEvidence...), ";"), "", row.Status}); err != nil {
			return err
		}
	}
	w.Flush()
	return w.Error()
}
