// Copyright (c) 2026 Tarek Wasfy
package backend

// Semantic modules are the package boundary around the canonical
// SemanticProgram.  SFPC/SPZ is only their storage encoding; no second IR is
// introduced here.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type SemanticModule struct {
	Identity            string           `json:"identity"`
	Origin              SemanticOrigin   `json:"origin"`
	Version             string           `json:"version,omitempty"`
	Exports             []string         `json:"exports,omitempty"`
	Imports             []string         `json:"imports,omitempty"`
	Dependencies        []string         `json:"dependencies,omitempty"`
	SemanticRoot        string           `json:"semantic_root"`
	SourceHash          string           `json:"source_hash,omitempty"`
	Frontend            string           `json:"frontend,omitempty"`
	FrontendVersion     string           `json:"frontend_version,omitempty"`
	CacheKey            string           `json:"cache_key"`
	Kind                ArtifactKind     `json:"kind,omitempty"`
	Capabilities        []string         `json:"capabilities,omitempty"`
	ABI                 string           `json:"abi,omitempty"`
	RuntimeRequirements []string         `json:"runtime_requirements,omitempty"`
	Types               []string         `json:"types,omitempty"`
	Functions           []string         `json:"functions,omitempty"`
	Globals             []string         `json:"globals,omitempty"`
	Constants           []string         `json:"constants,omitempty"`
	Bindings            []string         `json:"bindings,omitempty"`
	Effects             []string         `json:"effects,omitempty"`
	Contracts           []string         `json:"contracts,omitempty"`
	Capsules            []string         `json:"capsules,omitempty"`
	Program             *SemanticProgram `json:"-"`
}

type SemanticModuleStore struct{ Root string }

func DefaultSemanticModuleStore() (SemanticModuleStore, error) {
	root := os.Getenv("LOCALAPPDATA")
	if root == "" {
		root, _ = os.UserCacheDir()
	}
	if root == "" {
		return SemanticModuleStore{}, fmt.Errorf("cannot resolve user module store")
	}
	return SemanticModuleStore{Root: filepath.Join(root, "Semantic", "Modules")}, nil
}

func (s SemanticModuleStore) ensure() error {
	for _, d := range []string{"index", "cache", "modules", "locks"} {
		if err := os.MkdirAll(filepath.Join(s.Root, d), 0755); err != nil {
			return err
		}
	}
	return nil
}

func semanticModuleRoot(p *SemanticProgram) (string, error) {
	if p == nil || p.UniversalAST == nil {
		return "", fmt.Errorf("missing canonical UAST")
	}
	q := &SFPCQuery{program: p}
	roots, err := q.MerkleRoots()
	if err != nil {
		return "", err
	}
	return roots.ProgramRoot, nil
}

// semanticModuleStableRoot hashes the exact canonical payload that is stored
// in module.spz. Some in-memory frontend views contain derived nil/empty
// distinctions that are normalized by the SP/SE serializer; module identity
// must be based on the persisted semantic payload.
func semanticModuleStableRoot(p *SemanticProgram) (string, error) {
	if p == nil {
		return "", fmt.Errorf("missing canonical UAST")
	}
	b, err := p.MarshalSemanticSPZ()
	if err != nil {
		return "", err
	}
	q, err := ParseSemanticSPZ(b)
	if err != nil {
		return "", err
	}
	return semanticModuleRoot(q)
}

func NewSemanticModule(identity, language, source string, p *SemanticProgram) (*SemanticModule, error) {
	root, err := semanticModuleStableRoot(p)
	if err != nil {
		return nil, err
	}
	s := sha256.Sum256([]byte(source))
	keyInput := strings.Join([]string{hex.EncodeToString(s[:]), fmt.Sprint(SemanticSPVersion), root}, "|")
	k := sha256.Sum256([]byte(keyInput))
	m := &SemanticModule{Identity: identity, Origin: p.Origin, SemanticRoot: root, SourceHash: hex.EncodeToString(s[:]), Frontend: NormalizeLanguage(language), FrontendVersion: "modern", CacheKey: hex.EncodeToString(k[:]), Program: p, Kind: SourceModule}
	m.Imports = append([]string(nil), p.Origin.Modules...)
	m.Dependencies = append([]string(nil), p.Origin.Modules...)
	if m.Origin.SourceLanguage == "" {
		m.Origin.SourceLanguage = NormalizeLanguage(language)
	}
	enrichSemanticModule(m)
	return m, nil
}

func enrichSemanticModule(m *SemanticModule) {
	if m == nil || m.Program == nil || m.Program.UniversalAST == nil {
		return
	}
	seen := func(dst *[]string, value string) {
		if value == "" {
			return
		}
		for _, x := range *dst {
			if x == value {
				return
			}
		}
		*dst = append(*dst, value)
	}
	for _, n := range m.Program.UniversalAST.Nodes {
		name := ""
		if raw, ok := n.Fields["name"]; ok {
			_ = json.Unmarshal(raw, &name)
		}
		switch strings.ToLower(n.StructuralKind) {
		case "functiondecl", "methoddecl":
			seen(&m.Functions, name)
		case "variabledecl", "binding":
			seen(&m.Bindings, name)
		case "constantdecl":
			seen(&m.Constants, name)
		case "globaldecl":
			seen(&m.Globals, name)
		case "nominaltypedecl", "recordtypedecl", "interfacetypedecl", "typealiasdecl":
			seen(&m.Types, name)
		case "exportdecl":
			seen(&m.Exports, name)
		}
	}
	sort.Strings(m.Functions)
	sort.Strings(m.Bindings)
	sort.Strings(m.Constants)
	sort.Strings(m.Globals)
	sort.Strings(m.Types)
	sort.Strings(m.Exports)
}

func moduleCacheKey(m *SemanticModule) (string, error) {
	if m == nil {
		return "", fmt.Errorf("missing module")
	}
	root := m.SemanticRoot
	if root == "" && m.Program != nil {
		var err error
		root, err = semanticModuleStableRoot(m.Program)
		if err != nil {
			return "", err
		}
	}
	deps := append([]string(nil), m.Dependencies...)
	sort.Strings(deps)
	contracts := append([]string(nil), m.Contracts...)
	sort.Strings(contracts)
	seed := strings.Join([]string{m.SourceHash, m.FrontendVersion, fmt.Sprint(SemanticSPVersion), root, strings.Join(deps, ","), strings.Join(contracts, ",")}, "|")
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:]), nil
}

// ModuleStoreRoot exposes the platform-resolved store location without
// exposing any machine-specific user path in the API.
func ModuleStoreRoot() (string, error)                                       { s, err := DefaultSemanticModuleStore(); return s.Root, err }
func (s SemanticModuleStore) SaveModule(m *SemanticModule) error             { return s.Put(m) }
func (s SemanticModuleStore) OpenModule(key string) (*SemanticModule, error) { return s.Open(key) }
func (s SemanticModuleStore) VerifyModule(key string) error {
	m, err := s.Open(key)
	if err != nil {
		return err
	}
	return VerifySemanticModule(m)
}
func (s SemanticModuleStore) RemoveModule(key string) error {
	return os.RemoveAll(filepath.Join(s.Root, "modules", filepath.Base(key)))
}
func (s SemanticModuleStore) ListModules() ([]string, error) {
	if err := s.ensure(); err != nil {
		return nil, err
	}
	ents, err := os.ReadDir(filepath.Join(s.Root, "modules"))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(ents))
	for _, e := range ents {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}
func (s SemanticModuleStore) FindModule(identity string) (*SemanticModule, error) {
	ents, err := s.ListModules()
	if err != nil {
		return nil, err
	}
	for _, key := range ents {
		b, e := os.ReadFile(filepath.Join(s.Root, "modules", key, "module.meta"))
		if e != nil {
			continue
		}
		var m SemanticModule
		if json.Unmarshal(b, &m) == nil && m.Identity == identity {
			return s.Open(key)
		}
	}
	return nil, os.ErrNotExist
}

func ImportModule(identity, language, filename, source string) (*SemanticModule, error) {
	p, err := LowerSource(language, filename, source)
	if err != nil {
		return nil, err
	}
	return NewSemanticModule(identity, language, source, p)
}

func (s SemanticModuleStore) Put(m *SemanticModule) error {
	if m == nil || m.Program == nil {
		return fmt.Errorf("module has no program")
	}
	if err := s.ensure(); err != nil {
		return err
	}
	enrichSemanticModule(m)
	if key, err := moduleCacheKey(m); err != nil {
		return err
	} else {
		m.CacheKey = key
	}
	if err := VerifySemanticModule(m); err != nil {
		return err
	}
	dir := filepath.Join(s.Root, "modules", m.CacheKey)
	tmp := dir + ".tmp-" + fmt.Sprint(os.Getpid())
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0755); err != nil {
		return err
	}
	se, err := m.Program.MarshalSemanticSESemanticOnly()
	if err != nil {
		return err
	}
	spz, err := m.Program.MarshalSemanticSPZ()
	if err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(tmp, "module.se"), se, 0644); err != nil {
		return err
	}
	if err = os.WriteFile(filepath.Join(tmp, "module.spz"), spz, 0644); err != nil {
		return err
	}
	meta, _ := json.MarshalIndent(struct {
		*SemanticModule
		Created string `json:"created"`
	}{m, time.Now().UTC().Format(time.RFC3339)}, "", "  ")
	if err = os.WriteFile(filepath.Join(tmp, "module.meta"), meta, 0644); err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		_ = os.RemoveAll(dir)
	}
	return os.Rename(tmp, dir)
}

func (s SemanticModuleStore) Open(cacheKey string) (*SemanticModule, error) {
	b, err := os.ReadFile(filepath.Join(s.Root, "modules", cacheKey, "module.spz"))
	if err != nil {
		return nil, err
	}
	p, err := ParseSemanticSPZ(b)
	if err != nil {
		return nil, err
	}
	m := &SemanticModule{CacheKey: cacheKey, Program: p}
	if meta, e := os.ReadFile(filepath.Join(s.Root, "modules", cacheKey, "module.meta")); e == nil {
		var saved SemanticModule
		if json.Unmarshal(meta, &saved) == nil {
			*m = saved
			m.Program = p
			m.CacheKey = cacheKey
		}
	}
	expectedRoot := m.SemanticRoot
	computedRoot, rootErr := semanticModuleStableRoot(p)
	if rootErr != nil {
		return nil, rootErr
	}
	if expectedRoot == "" {
		m.SemanticRoot = computedRoot
	} else {
		m.SemanticRoot = expectedRoot
	}
	if err = VerifySemanticModule(m); err != nil {
		return nil, err
	}
	return m, nil
}

func VerifySemanticModule(m *SemanticModule) error {
	if m == nil || m.Program == nil {
		return fmt.Errorf("missing module")
	}
	root, err := semanticModuleStableRoot(m.Program)
	if err != nil {
		return err
	}
	if m.SemanticRoot != "" && root != m.SemanticRoot {
		return fmt.Errorf("semantic root mismatch")
	}
	if m.CacheKey != "" {
		key, err := moduleCacheKey(m)
		if err != nil {
			return err
		}
		if key != m.CacheKey {
			return fmt.Errorf("module cache key mismatch")
		}
	}
	return nil
}

type SemanticModuleGraph struct {
	Modules map[string]*SemanticModule
	Edges   map[string][]string
}

type SemanticManifest struct {
	Version  int
	Module   string
	Requires []SemanticRequirement
}
type SemanticRequirement struct{ Language, Target string }

func ParseSemanticManifest(data []byte) (SemanticManifest, error) {
	var m SemanticManifest
	m.Version = 1
	for _, raw := range strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n") {
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "#") {
			continue
		}
		f := strings.Fields(s)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "semantic":
			if len(f) != 2 || f[1] != "1" {
				return m, fmt.Errorf("unsupported semantic manifest")
			}
		case "module":
			if len(f) != 2 {
				return m, fmt.Errorf("invalid module declaration")
			}
			m.Module = f[1]
		case "require":
			if len(f) != 2 && len(f) != 3 {
				return m, fmt.Errorf("invalid require declaration")
			}
			r := SemanticRequirement{Target: f[len(f)-1]}
			if len(f) == 3 {
				r.Language = f[1]
			}
			m.Requires = append(m.Requires, r)
		default:
			return m, fmt.Errorf("unknown manifest directive %q", f[0])
		}
	}
	if m.Module == "" {
		return m, fmt.Errorf("manifest has no module")
	}
	return m, nil
}

type SemanticLock struct {
	Version int                 `json:"version"`
	Module  string              `json:"module"`
	Entries []SemanticLockEntry `json:"entries"`
}
type SemanticLockEntry struct {
	Identity     string `json:"identity,omitempty"`
	Language     string `json:"language,omitempty"`
	SourceHash   string `json:"source_hash,omitempty"`
	SemanticRoot string `json:"semantic_root,omitempty"`
	CacheKey     string `json:"cache_key,omitempty"`
}

func (l SemanticLock) MarshalDeterministic() ([]byte, error) {
	sort.Slice(l.Entries, func(i, j int) bool { return l.Entries[i].Identity < l.Entries[j].Identity })
	return json.MarshalIndent(l, "", "  ")
}

type ArtifactKind string

const (
	SourceModule           ArtifactKind = "SOURCE_MODULE"
	SemanticModuleArtifact ArtifactKind = "SEMANTIC_MODULE"
	AssemblyModule         ArtifactKind = "ASSEMBLY_MODULE"
	BinaryModule           ArtifactKind = "BINARY_MODULE"
	NativeExternal         ArtifactKind = "NATIVE_EXTERNAL"
)

type DetectionResult struct {
	Kind       ArtifactKind   `json:"kind"`
	Language   string         `json:"language,omitempty"`
	Confidence string         `json:"confidence"`
	Scores     map[string]int `json:"scores"`
	Languages  []string       `json:"languages,omitempty"`
	Mixed      bool           `json:"mixed,omitempty"`
}

func DetectArtifact(path string) DetectionResult {
	if st, err := os.Stat(path); err == nil && st.IsDir() {
		return detectDirectory(path)
	}
	e := strings.ToLower(filepath.Ext(path))
	scores := map[string]int{}
	lang := ""
	switch e {
	case ".go":
		scores["go"] += 5
		lang = "go"
	case ".py":
		scores["python"] += 5
		lang = "python"
	case ".rs":
		scores["rust"] += 5
		lang = "rust"
	case ".c", ".h":
		scores["c"] += 5
		lang = "c"
	case ".cpp", ".cc", ".hpp":
		scores["cpp"] += 5
		lang = "cpp"
	case ".java":
		scores["java"] += 5
		lang = "java"
	case ".asm", ".s":
		scores["assembly"] += 5
		lang = "assembly"
	case ".se":
		return DetectionResult{Kind: SemanticModuleArtifact, Language: "se", Confidence: "DETECTED", Scores: scores}
	case ".spz":
		return DetectionResult{Kind: SemanticModuleArtifact, Language: "spz", Confidence: "DETECTED", Scores: scores}
	case ".dll", ".exe", ".obj":
		return DetectionResult{Kind: BinaryModule, Confidence: "DETECTED", Scores: scores}
	}
	if len(scores) == 0 {
		return DetectionResult{Kind: SourceModule, Confidence: "UNKNOWN", Scores: scores}
	}
	return DetectionResult{Kind: SourceModule, Language: lang, Confidence: "DETECTED", Scores: scores}
}

func detectDirectory(path string) DetectionResult {
	scores := map[string]int{}
	kinds := map[ArtifactKind]bool{}
	langs := map[string]bool{}
	_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		name := strings.ToLower(filepath.Base(p))
		e := strings.ToLower(filepath.Ext(p))
		switch name {
		case "go.mod":
			scores["go"] += 8
			langs["go"] = true
			kinds[SourceModule] = true
		case "cargo.toml":
			scores["rust"] += 8
			langs["rust"] = true
			kinds[SourceModule] = true
		case "pyproject.toml", "setup.py":
			scores["python"] += 8
			langs["python"] = true
			kinds[SourceModule] = true
		case "package.json":
			scores["javascript"] += 8
			langs["javascript"] = true
			kinds[SourceModule] = true
		case "pom.xml":
			scores["java"] += 8
			langs["java"] = true
			kinds[SourceModule] = true
		}
		switch e {
		case ".go":
			scores["go"] += 5
			langs["go"] = true
			kinds[SourceModule] = true
		case ".py":
			scores["python"] += 5
			langs["python"] = true
			kinds[SourceModule] = true
		case ".rs":
			scores["rust"] += 5
			langs["rust"] = true
			kinds[SourceModule] = true
		case ".c", ".h":
			scores["c"] += 3
			langs["c"] = true
			kinds[SourceModule] = true
		case ".cc", ".cpp", ".hpp":
			scores["cpp"] += 3
			langs["cpp"] = true
			kinds[SourceModule] = true
		case ".java":
			scores["java"] += 5
			langs["java"] = true
			kinds[SourceModule] = true
		case ".asm", ".s":
			scores["assembly"] += 5
			langs["assembly"] = true
			kinds[AssemblyModule] = true
		case ".dll", ".exe", ".obj":
			kinds[BinaryModule] = true
		case ".se", ".spz":
			kinds[SemanticModuleArtifact] = true
		}
		return nil
	})
	var ls []string
	for l := range langs {
		ls = append(ls, l)
	}
	sort.Strings(ls)
	if len(ls) == 0 && len(kinds) == 0 {
		return DetectionResult{Kind: SourceModule, Confidence: "UNKNOWN", Scores: scores}
	}
	if len(ls) > 1 {
		return DetectionResult{Kind: SourceModule, Confidence: "AMBIGUOUS", Scores: scores, Languages: ls, Mixed: true}
	}
	best, second, lang := 0, 0, ""
	for l, n := range scores {
		if n > best {
			second = best
			best = n
			lang = l
		} else if n > second {
			second = n
		}
	}
	if best == 0 {
		if kinds[AssemblyModule] {
			return DetectionResult{Kind: AssemblyModule, Language: "assembly", Confidence: "DETECTED", Scores: scores, Languages: ls}
		}
		if kinds[SemanticModuleArtifact] {
			return DetectionResult{Kind: SemanticModuleArtifact, Language: "semantic", Confidence: "DETECTED", Scores: scores, Languages: ls}
		}
		if kinds[BinaryModule] {
			return DetectionResult{Kind: BinaryModule, Confidence: "DETECTED", Scores: scores, Languages: ls}
		}
		return DetectionResult{Kind: SourceModule, Confidence: "UNKNOWN", Scores: scores, Languages: ls}
	}
	if best == second {
		return DetectionResult{Kind: SourceModule, Confidence: "AMBIGUOUS", Scores: scores, Languages: ls}
	}
	return DetectionResult{Kind: SourceModule, Language: lang, Confidence: "DETECTED", Scores: scores, Languages: ls, Mixed: len(kinds) > 1}
}

func BuildModuleGraph(mods []*SemanticModule) *SemanticModuleGraph {
	g := &SemanticModuleGraph{Modules: map[string]*SemanticModule{}, Edges: map[string][]string{}}
	for _, m := range mods {
		if m != nil {
			g.Modules[m.Identity] = m
			g.Edges[m.Identity] = append([]string(nil), m.Dependencies...)
		}
	}
	return g
}
func (g *SemanticModuleGraph) Dependencies(id string) []string {
	out := append([]string(nil), g.Edges[id]...)
	sort.Strings(out)
	return out
}
