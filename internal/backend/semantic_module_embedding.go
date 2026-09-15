// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// SemanticEmbeddedModule is the persisted module boundary in a .se file.
// Inline entries contain the canonical module payload exactly once in a
// project. Reference entries point at that owner .se file and contain no
// duplicate body.
type SemanticEmbeddedModule struct {
	Identity     string `json:"identity"`
	SemanticRoot string `json:"semantic_root"`
	Mode         string `json:"mode"` // inline, reference, external
	Owner        string `json:"owner,omitempty"`
	Payload      string `json:"payload,omitempty"` // base64 canonical .se
	Reason       string `json:"reason,omitempty"`
}

type SemanticModuleEmbeddingRegistry struct {
	mu        sync.Mutex
	owners    map[string]string
	rootDir   string
	statePath string
}

func NewSemanticModuleEmbeddingRegistry(rootDir string) *SemanticModuleEmbeddingRegistry {
	r := &SemanticModuleEmbeddingRegistry{owners: map[string]string{}, rootDir: rootDir, statePath: os.Getenv("SEMANTIC_EMBED_REGISTRY")}
	if r.statePath != "" {
		if b, err := os.ReadFile(r.statePath); err == nil {
			_ = json.Unmarshal(b, &r.owners)
		}
	}
	return r
}

type SemanticModuleEmbeddingOptions struct {
	BaseDir       string
	StoreRoot     string
	UnitPath      string
	Language      string
	NeededOnly    bool
	NeededSymbols []string
	Registry      *SemanticModuleEmbeddingRegistry
}

// EmbedSemanticModules stores the resolved module bodies in the program's
// ordinary metadata field. It does not merge module UAST bodies into the
// owning program. This keeps each Go file an independent semantic unit while
// making the .se transport self-describing and deduplicated.
func EmbedSemanticModules(p *SemanticProgram, opts SemanticModuleEmbeddingOptions) ([]SemanticEmbeddedModule, error) {
	if p == nil {
		return nil, fmt.Errorf("nil semantic program")
	}
	if opts.Registry == nil {
		opts.Registry = NewSemanticModuleEmbeddingRegistry(opts.BaseDir)
	}
	if opts.Registry.rootDir == "" {
		opts.Registry.rootDir = opts.BaseDir
	}
	if opts.StoreRoot != "" {
		s := opts.StoreRoot
		store := SemanticModuleStore{Root: s}
		if err := store.Ensure(); err != nil {
			return nil, err
		}
	}
	seen := map[string]bool{}
	entries := make([]SemanticEmbeddedModule, 0, len(p.Origin.Modules))
	for _, dep := range p.Origin.Modules {
		dep = strings.TrimSpace(dep)
		if dep == "" || seen[dep] {
			continue
		}
		seen[dep] = true
		depOpts := opts
		if depOpts.NeededOnly && len(depOpts.NeededSymbols) == 0 {
			depOpts.NeededSymbols = semanticNeededSymbols(p, dep)
		}
		modules, err := resolveSemanticEmbeddingModules(dep, depOpts)
		if err != nil {
			entries = append(entries, SemanticEmbeddedModule{Identity: dep, Mode: "external", Reason: err.Error()})
			continue
		}
		if opts.NeededOnly {
			modules = selectNeededSemanticModules(p, dep, modules)
		}
		for _, module := range modules {
			if module == nil || module.Program == nil {
				continue
			}
			payload, err := module.Program.MarshalSemanticSESemanticOnly()
			if err != nil {
				return nil, fmt.Errorf("serialize embedded module %q: %w", dep, err)
			}
			root := module.SemanticRoot
			if root == "" {
				root, err = semanticModuleRoot(module.Program)
				if err != nil {
					return nil, fmt.Errorf("derive embedded module root %q: %w", dep, err)
				}
			}
			identity := module.Identity
			if identity == "" {
				identity = dep
			}
			entry := SemanticEmbeddedModule{Identity: identity, SemanticRoot: root}
			owner, first := opts.Registry.claim(root, opts.UnitPath)
			if first {
				entry.Mode = "inline"
				entry.Owner = owner
				entry.Payload = base64.StdEncoding.EncodeToString(payload)
			} else {
				entry.Mode = "reference"
				entry.Owner = relativeOwner(opts.UnitPath, owner)
			}
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].SemanticRoot != entries[j].SemanticRoot {
			return entries[i].SemanticRoot < entries[j].SemanticRoot
		}
		return entries[i].Identity < entries[j].Identity
	})
	if p.Metadata == nil {
		p.Metadata = map[string]string{}
	}
	b, err := json.Marshal(entries)
	if err != nil {
		return nil, err
	}
	p.Metadata["semantic_module_embeddings"] = string(b)
	p.Metadata["semantic_module_embedding_version"] = "v1"
	if p.UniversalAST != nil {
		if p.UniversalAST.Metadata == nil {
			p.UniversalAST.Metadata = map[string]string{}
		}
		p.UniversalAST.Metadata["semantic_module_embeddings"] = string(b)
		p.UniversalAST.Metadata["semantic_module_embedding_version"] = "v1"
	}
	return entries, nil
}

func selectNeededSemanticModules(importer *SemanticProgram, dep string, modules []*SemanticModule) []*SemanticModule {
	if importer == nil || len(modules) == 0 {
		return modules
	}
	b, err := importer.MarshalSemanticJSON()
	if err != nil {
		return modules
	}
	text := string(b)
	required := map[string]bool{}
	prefix := `"identity":"` + strings.TrimSpace(dep) + `.`
	for at := strings.Index(text, prefix); at >= 0; {
		start := at + len(prefix)
		end := start
		for end < len(text) && ((text[end] >= 'a' && text[end] <= 'z') || (text[end] >= 'A' && text[end] <= 'Z') || (text[end] >= '0' && text[end] <= '9') || text[end] == '_') {
			end++
		}
		if end > start {
			required[strings.ToLower(text[start:end])] = true
		}
		next := start
		rest := text[next:]
		offset := strings.Index(rest, prefix)
		if offset < 0 {
			break
		}
		at = next + offset
	}
	if len(required) == 0 {
		return modules
	}
	selected := make([]*SemanticModule, 0, len(modules))
	for _, module := range modules {
		identity := strings.ToLower(module.Identity)
		matched := false
		for name := range required {
			if strings.HasSuffix(identity, "/"+name+".go") || strings.HasSuffix(identity, "#"+name+".go") {
				matched = true
				break
			}
		}
		if matched {
			selected = append(selected, module)
		}
	}
	if len(selected) == 0 {
		return modules
	}
	return selected
}

func (r *SemanticModuleEmbeddingRegistry) claim(root, unitPath string) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if owner := r.owners[root]; owner != "" {
		return owner, false
	}
	owner := unitPath
	if owner == "" {
		owner = "<in-memory>"
	}
	// Keep the owner as an absolute path. A root-relative path is ambiguous
	// once a unit is compiled outside the export directory; absolute ownership
	// makes reference resolution deterministic and does not duplicate payloads.
	r.owners[root] = filepath.Clean(owner)
	if r.statePath != "" {
		if b, err := json.Marshal(r.owners); err == nil {
			_ = os.WriteFile(r.statePath, b, 0644)
		}
	}
	return filepath.Clean(owner), true
}

func relativeOwner(unitPath, owner string) string {
	if owner == "" || owner == "<in-memory>" {
		return owner
	}
	if filepath.IsAbs(unitPath) && filepath.IsAbs(owner) {
		if rel, err := filepath.Rel(filepath.Dir(unitPath), owner); err == nil {
			return filepath.ToSlash(rel)
		}
	}
	return filepath.ToSlash(owner)
}

func resolveSemanticEmbeddingModules(dep string, opts SemanticModuleEmbeddingOptions) ([]*SemanticModule, error) {
	resolver := UniversalModuleResolver{Store: SemanticModuleStore{Root: opts.StoreRoot}}
	language, name, automatic := semanticPackageReference(dep)
	if language == "" {
		language = opts.Language
	}
	if name == "" {
		name = dep
	}
	// The durable Semantic module store is authoritative for an already
	// imported module. This lookup intentionally precedes any source-directory
	// import so embedding never retranspiles an existing module.
	if strings.TrimSpace(opts.StoreRoot) != "" {
		if m, err := resolver.Store.FindModule(name); err == nil && m != nil && m.Program != nil {
			return []*SemanticModule{m}, nil
		}
		if modules, err := loadStoredPackageModules(opts.StoreRoot, name, dep, language, opts.NeededSymbols); err == nil && len(modules) > 0 {
			return modules, nil
		}
	}
	candidate := name
	if !filepath.IsAbs(candidate) && opts.BaseDir != "" {
		candidate = filepath.Join(opts.BaseDir, candidate)
	}
	if local := localGoModuleCandidate(name, opts.BaseDir); local != "" {
		candidate = local
	}
	if strings.EqualFold(language, "go") && !filepath.IsAbs(name) {
		if stdlib := filepath.Join(runtime.GOROOT(), "src", filepath.FromSlash(name)); isDirectory(stdlib) {
			candidate = stdlib
		}
	}
	if st, err := os.Stat(candidate); err == nil {
		if st.IsDir() {
			m, err := resolver.ImportTarget(candidate, ModuleImportOptions{Language: language})
			if err != nil {
				return nil, err
			}
			return []*SemanticModule{m}, nil
		}
		m, err := resolver.ImportTarget(candidate, ModuleImportOptions{Language: language})
		if err != nil {
			return nil, err
		}
		return []*SemanticModule{m}, nil
	}
	if automatic {
		manifest, err := resolver.ImportPackage(name, ModuleImportOptions{Language: language})
		if err != nil {
			return nil, err
		}
		if manifest == nil || manifest.Root == "" {
			return nil, fmt.Errorf("module import produced no manifest for %q", dep)
		}
		path := filepath.Join(manifest.Root, SafeModuleName(manifest.Name)+".smod")
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		var loaded SemanticPackageManifest
		if err := json.Unmarshal(data, &loaded); err != nil {
			return nil, err
		}
		var modules []*SemanticModule
		for _, file := range loaded.Files {
			if file.Status != "SEMANTIC_READY" || file.SemanticPath == "" {
				continue
			}
			payload, err := os.ReadFile(filepath.Join(manifest.Root, filepath.FromSlash(file.SemanticPath)))
			if err != nil {
				continue
			}
			p, err := ParseSemanticSPZ(payload)
			if err != nil {
				continue
			}
			m, err := NewSemanticModule(dep+"#"+file.Path, language, string(payload), p)
			if err == nil {
				modules = append(modules, m)
			}
		}
		if len(modules) > 0 {
			return modules, nil
		}
	}
	return nil, fmt.Errorf("module implementation not available for %q", dep)
}

func loadStoredPackageModules(storeRoot, name, identity, language string, needed []string) ([]*SemanticModule, error) {
	manifestPath := filepath.Join(storeRoot, SafeModuleName(name), SafeModuleName(name)+".smod")
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, err
	}
	var manifest SemanticPackageManifest
	if err := json.Unmarshal(b, &manifest); err != nil {
		return nil, err
	}
	root := filepath.Dir(manifestPath)
	var out []*SemanticModule
	for _, file := range manifest.Files {
		if file.Status != "SEMANTIC_READY" || file.SemanticPath == "" {
			continue
		}
		if len(needed) > 0 && len(file.Symbols) > 0 && !symbolListProvidesAny(file.Symbols, identity, needed) {
			continue
		}
		payload, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file.SemanticPath)))
		if err != nil {
			return nil, err
		}
		if len(needed) > 0 && !semanticPayloadProvidesAny(payload, identity, needed) {
			continue
		}
		p, err := ParseSemanticSPZ(payload)
		if err != nil {
			return nil, err
		}
		m, err := NewSemanticModule(identity+"#"+file.Path, language, string(payload), p)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("stored package %q has no semantic-ready units", name)
	}
	return out, nil
}

func semanticNeededSymbols(importer *SemanticProgram, dep string) []string {
	if importer == nil {
		return nil
	}
	b, err := importer.MarshalSemanticJSON()
	if err != nil {
		return nil
	}
	text := string(b)
	prefix := `"identity":"` + strings.TrimSpace(dep) + `.`
	seen := map[string]bool{}
	var out []string
	for at := strings.Index(text, prefix); at >= 0; {
		start := at + len(prefix)
		end := start
		for end < len(text) && ((text[end] >= 'a' && text[end] <= 'z') || (text[end] >= 'A' && text[end] <= 'Z') || (text[end] >= '0' && text[end] <= '9') || text[end] == '_') {
			end++
		}
		if end > start {
			name := strings.ToLower(text[start:end])
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
		rest := text[start:]
		next := strings.Index(rest, prefix)
		if next < 0 {
			break
		}
		at = start + next
	}
	return out
}

func semanticPayloadProvidesAny(payload []byte, identity string, needed []string) bool {
	text := strings.ToLower(string(payload))
	for _, name := range needed {
		if strings.Contains(text, `"identity":"`+strings.ToLower(identity)+`.`+strings.ToLower(name)) {
			return true
		}
	}
	return false
}

func semanticProgramSymbolIdentities(p *SemanticProgram) []string {
	if p == nil {
		return nil
	}
	b, err := p.MarshalSemanticJSON()
	if err != nil {
		return nil
	}
	text := string(b)
	const prefix = `"identity":"`
	seen := map[string]bool{}
	var out []string
	for at := strings.Index(text, prefix); at >= 0; {
		start := at + len(prefix)
		end := strings.IndexByte(text[start:], '"')
		if end < 0 {
			break
		}
		identity := text[start : start+end]
		if identity != "" && !seen[identity] {
			seen[identity] = true
			out = append(out, identity)
		}
		next := start + end + 1
		if next >= len(text) {
			break
		}
		at = strings.Index(text[next:], prefix)
		if at >= 0 {
			at += next
		}
	}
	return out
}

func symbolListProvidesAny(symbols []string, packageIdentity string, needed []string) bool {
	for _, symbol := range symbols {
		for _, name := range needed {
			if strings.EqualFold(symbol, packageIdentity+"."+name) {
				return true
			}
		}
	}
	return false
}

func isDirectory(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

func localGoModuleCandidate(importPath, baseDir string) string {
	if baseDir == "" || filepath.IsAbs(importPath) {
		return ""
	}
	root, err := filepath.Abs(baseDir)
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	modulePath := ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			modulePath = strings.TrimSpace(fields[1])
			break
		}
	}
	if modulePath == "" || (importPath != modulePath && !strings.HasPrefix(importPath, modulePath+"/")) {
		return ""
	}
	rel := strings.TrimPrefix(importPath, modulePath)
	rel = strings.TrimPrefix(rel, "/")
	candidate := filepath.Join(root, filepath.FromSlash(rel))
	if st, err := os.Stat(candidate); err == nil && st.IsDir() {
		return candidate
	}
	return ""
}

// DecodeSemanticEmbeddedModule is used by consumers that want to materialize
// an inline body. References deliberately remain references and are resolved
// by the project/link layer, avoiding duplicate payloads.
func DecodeSemanticEmbeddedModule(entry SemanticEmbeddedModule) (*SemanticProgram, error) {
	if entry.Mode != "inline" || entry.Payload == "" {
		return nil, fmt.Errorf("embedded module %q has no inline payload", entry.Identity)
	}
	b, err := base64.StdEncoding.DecodeString(entry.Payload)
	if err != nil {
		return nil, err
	}
	p, err := ParseSemanticSE(b)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256(b)
	if entry.SemanticRoot == "" || hex.EncodeToString(sum[:]) == "" {
		return p, nil
	}
	return p, nil
}

// LinkEmbeddedSemanticModules materializes inline module payloads and follows
// deduplicated owner references before native compilation. It is intentionally
// a linker operation over existing SemanticProgram values, not a new IR.
func LinkEmbeddedSemanticModules(p *SemanticProgram, baseDir string) error {
	return linkEmbeddedSemanticModules(p, baseDir, map[string]bool{})
}

func linkEmbeddedSemanticModules(p *SemanticProgram, baseDir string, seen map[string]bool) error {
	if p == nil || p.Metadata == nil {
		return nil
	}
	if p.Metadata["semantic_embedded_modules_linked"] == "true" {
		return nil
	}
	var entries []SemanticEmbeddedModule
	if raw := strings.TrimSpace(p.Metadata["semantic_module_embeddings"]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &entries); err != nil {
			return fmt.Errorf("invalid embedded module metadata: %w", err)
		}
	}
	for _, entry := range entries {
		if entry.Mode != "inline" && entry.Mode != "reference" {
			continue
		}
		key := entry.SemanticRoot + "|" + entry.Owner
		if seen[key] {
			continue
		}
		seen[key] = true
		var mod *SemanticProgram
		var err error
		if entry.Mode == "inline" {
			mod, err = DecodeSemanticEmbeddedModule(entry)
		} else {
			owner := entry.Owner
			if !filepath.IsAbs(owner) {
				owner = filepath.Join(baseDir, filepath.FromSlash(owner))
			}
			data, readErr := os.ReadFile(owner)
			if readErr != nil {
				return fmt.Errorf("read embedded module owner %q: %w", owner, readErr)
			}
			mod, err = ParseSemanticSE(data)
			if err == nil {
				err = linkEmbeddedSemanticModules(mod, filepath.Dir(owner), seen)
			}
		}
		if err != nil {
			return fmt.Errorf("materialize embedded module %q: %w", entry.Identity, err)
		}
		mergeSemanticPrograms(p, mod)
	}
	p.Metadata["semantic_embedded_modules_linked"] = "true"
	return nil
}
