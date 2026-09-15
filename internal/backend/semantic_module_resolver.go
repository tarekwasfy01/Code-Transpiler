// Copyright (c) 2026 Tarek Wasfy
package backend

// UniversalModuleResolver is the single import boundary for source, semantic,
// assembly and binary artifacts. It never invokes an external compiler.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ModuleImportProgress reports real resolver work. It is diagnostic-only and
// deliberately carries no semantic state, so callers can persist progress
// without introducing another module representation.
type ModuleImportProgress struct {
	Stage string
	Path  string
	Index int
	Total int
}

type ModuleImportOptions struct {
	Language string
	// SkipPersist keeps an import entirely in memory. It is used by the
	// executable directory fast path; callers that need the module cache leave
	// this false.
	SkipPersist bool
	Progress    func(ModuleImportProgress)
}

func (o ModuleImportOptions) report(stage, path string, index, total int) {
	if o.Progress != nil {
		o.Progress(ModuleImportProgress{Stage: stage, Path: path, Index: index, Total: total})
	}
}

type UniversalModuleResolver struct{ Store SemanticModuleStore }

// SemanticModuleLinkOptions controls how declared Semantic modules become
// implementation units of a program. By default an explicit
// semantic_module_link_roots plan is respected; EmbedAll deliberately ignores
// that plan and links every declared module. It is intended for reproducible
// whole-program bundles and diagnostics, not as the normal executable path.
type SemanticModuleLinkOptions struct {
	BaseDir  string
	EmbedAll bool
	// Mode is the requested serialization/link policy. Empty preserves the
	// historical selected-roots behavior. "references" keeps imports as
	// canonical module references, "needed" links the declared demand roots,
	// and "all" materializes every declared module.
	Mode SemanticModuleEmbeddingMode
}

// SemanticModuleEmbeddingMode selects how imported Semantic modules cross the
// source-to-UAST boundary. It is shared by CLI, GUI and API callers.
type SemanticModuleEmbeddingMode string

const (
	SemanticModulesNeeded    SemanticModuleEmbeddingMode = "needed"
	SemanticModulesReference SemanticModuleEmbeddingMode = "references"
	SemanticModulesAll       SemanticModuleEmbeddingMode = "all"
)

// LinkSemanticDependencies resolves module identities recorded by a semantic
// program and merges the reachable units into the program before compilation.
// Unresolved external names remain metadata (they may be provided by a target
// runtime), while local paths and cached .smod modules are linked eagerly.
func (r UniversalModuleResolver) LinkSemanticDependencies(p *SemanticProgram, baseDir string) error {
	return r.LinkSemanticDependenciesWithOptions(p, SemanticModuleLinkOptions{BaseDir: baseDir})
}

// LinkSemanticDependenciesWithOptions resolves declared dependencies using an
// explicit link policy. A link-root plan is a sound opt-in pruning boundary:
// it is serialized with the SemanticProgram and therefore does not infer use
// from source text or diagnostics. EmbedAll is the explicit whole-program
// override used by CLI/API callers that require every declared module.
func (r UniversalModuleResolver) LinkSemanticDependenciesWithOptions(p *SemanticProgram, opts SemanticModuleLinkOptions) error {
	if p == nil {
		return fmt.Errorf("nil semantic program")
	}
	selected, err := semanticModuleLinkRoots(p)
	if err != nil {
		return err
	}
	units, err := semanticModuleUnitRoots(p)
	if err != nil {
		return err
	}
	mode := opts.Mode
	if opts.EmbedAll {
		mode = SemanticModulesAll
	}
	if mode != "" && mode != SemanticModulesNeeded && mode != SemanticModulesReference && mode != SemanticModulesAll {
		return fmt.Errorf("unknown semantic module embedding mode %q (expected needed, references, or all)", mode)
	}
	if mode == SemanticModulesAll {
		selected = nil
		units = nil
	}
	if mode == SemanticModulesReference {
		// Preserve the declared import list in the UAST without copying module
		// bodies into it. Explicit registry references are still acquired so a
		// later compile can resolve them from the durable module store.
		for _, dep := range p.Origin.Modules {
			language, name, automatic := semanticPackageReference(dep)
			if !automatic {
				continue
			}
			if _, err := r.ImportPackage(name, ModuleImportOptions{Language: language}); err != nil {
				return fmt.Errorf("acquire Semantic module %q: %w", dep, err)
			}
		}
		markSemanticModulesLinked(p, selected, units, false)
		if p.Metadata == nil {
			p.Metadata = map[string]string{}
		}
		p.Metadata["semantic_modules_linked"] = "references-only"
		if p.UniversalAST != nil {
			if p.UniversalAST.Metadata == nil {
				p.UniversalAST.Metadata = map[string]string{}
			}
			p.UniversalAST.Metadata["semantic_modules_linked"] = "references-only"
		}
		return nil
	}
	if semanticModulesAlreadyLinked(p, selected, units, opts.EmbedAll) {
		return nil
	}
	for _, dep := range p.Origin.Modules {
		if strings.TrimSpace(dep) == "" {
			continue
		}
		if selected != nil && !selected[dep] {
			continue
		}
		language, packageName, automatic := semanticPackageReference(dep)
		candidate := packageName
		if !filepath.IsAbs(candidate) && opts.BaseDir != "" {
			candidate = filepath.Join(opts.BaseDir, candidate)
		}
		var m *SemanticModule
		var err error
		if _, se := os.Stat(candidate); se == nil {
			if strings.EqualFold(filepath.Ext(candidate), ".smod") {
				if err = r.linkSMODUnits(p, candidate, units[dep]); err != nil {
					return fmt.Errorf("link semantic module %q: %w", dep, err)
				}
				continue
			}
			m, err = r.ImportTarget(candidate, ModuleImportOptions{})
		} else if r.Store.Root != "" {
			m, err = r.Store.FindModule(packageName)
			if (err != nil || m == nil) && r.Store.Root != "" {
				if linkErr := r.linkPackageName(p, packageName, units[dep]); linkErr == nil {
					continue
				} else if !errors.Is(linkErr, os.ErrNotExist) {
					return fmt.Errorf("link semantic package %q: %w", packageName, linkErr)
				}
			}
		}
		if (err != nil || m == nil || m.Program == nil) && automatic {
			manifest, importErr := r.ImportPackage(packageName, ModuleImportOptions{Language: language})
			if importErr != nil {
				return fmt.Errorf("MODULE_NOT_INSTALLED %q: automatic %s package import failed: %w", dep, language, importErr)
			}
			if manifest == nil || manifest.Root == "" {
				return fmt.Errorf("MODULE_NOT_INSTALLED %q: automatic %s package import produced no manifest", dep, language)
			}
			if linkErr := r.linkSMODUnits(p, filepath.Join(manifest.Root, SafeModuleName(manifest.Name)+".smod"), units[dep]); linkErr != nil {
				return fmt.Errorf("MODULE_NOT_INSTALLED %q: imported package cannot link: %w", dep, linkErr)
			}
			continue
		}
		if err != nil || m == nil || m.Program == nil {
			if automatic {
				return fmt.Errorf("MODULE_NOT_INSTALLED %q", dep)
			}
			continue
		}
		mergeSemanticPrograms(p, m.Program)
	}
	markSemanticModulesLinked(p, selected, units, mode == SemanticModulesAll)
	return nil
}

func semanticModulesAlreadyLinked(p *SemanticProgram, selected map[string]bool, units map[string]map[string]bool, embedAll bool) bool {
	if p == nil || p.Metadata == nil {
		return false
	}
	completed := strings.TrimSpace(p.Metadata["semantic_modules_linked"])
	if completed == "embed-all" {
		return true
	}
	return !embedAll && completed == "selected-roots:"+semanticModuleRootKey(selected)+"|units:"+semanticModuleUnitRootKey(units)
}

func markSemanticModulesLinked(p *SemanticProgram, selected map[string]bool, units map[string]map[string]bool, embedAll bool) {
	if p.Metadata == nil {
		p.Metadata = map[string]string{}
	}
	mode := "selected-roots:" + semanticModuleRootKey(selected) + "|units:" + semanticModuleUnitRootKey(units)
	if embedAll {
		mode = "embed-all"
	}
	p.Metadata["semantic_modules_linked"] = mode
	if p.UniversalAST != nil {
		if p.UniversalAST.Metadata == nil {
			p.UniversalAST.Metadata = map[string]string{}
		}
		p.UniversalAST.Metadata["semantic_modules_linked"] = mode
	}
}

// semanticModuleUnitRoots optionally narrows each selected package to exact
// persisted Semantic units. Values use `package-reference|semantic/path.spz`
// separated by semicolons. Paths are manifest-relative and validated before a
// module is read, so this is a data plan rather than a filename heuristic.
func semanticModuleUnitRoots(p *SemanticProgram) (map[string]map[string]bool, error) {
	if p == nil || p.Metadata == nil {
		return nil, nil
	}
	raw := strings.TrimSpace(p.Metadata["semantic_module_unit_roots"])
	if raw == "" {
		return nil, nil
	}
	declared := map[string]bool{}
	for _, dep := range p.Origin.Modules {
		declared[dep] = true
	}
	out := map[string]map[string]bool{}
	for _, entry := range strings.Split(raw, ";") {
		ref, unit, ok := strings.Cut(strings.TrimSpace(entry), "|")
		ref, unit = strings.TrimSpace(ref), filepath.ToSlash(strings.TrimSpace(unit))
		if !ok || ref == "" || unit == "" || !declared[ref] {
			return nil, fmt.Errorf("invalid semantic module unit root %q", entry)
		}
		if strings.HasPrefix(unit, "/") || strings.Contains(unit, "../") || !strings.HasSuffix(unit, ".spz") {
			return nil, fmt.Errorf("invalid semantic module unit path %q", unit)
		}
		if out[ref] == nil {
			out[ref] = map[string]bool{}
		}
		out[ref][unit] = true
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("semantic module unit plan has no units")
	}
	// A unit can only narrow an explicitly selected dependency. This prevents
	// metadata from causing an undeclared or accidentally unused module load.
	if selected, err := semanticModuleLinkRoots(p); err == nil && selected != nil {
		for ref := range out {
			if !selected[ref] {
				return nil, fmt.Errorf("semantic module unit root %q lacks a matching link root", ref)
			}
		}
	}
	return out, nil
}

func semanticModuleRootKey(selected map[string]bool) string {
	if selected == nil {
		return "all"
	}
	roots := make([]string, 0, len(selected))
	for root := range selected {
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return strings.Join(roots, ";")
}

func semanticModuleUnitRootKey(units map[string]map[string]bool) string {
	if units == nil {
		return "all"
	}
	entries := make([]string, 0)
	for ref, paths := range units {
		for path := range paths {
			entries = append(entries, ref+"|"+path)
		}
	}
	sort.Strings(entries)
	return strings.Join(entries, ";")
}

// semanticModuleLinkRoots reads an explicit, canonical link plan from the
// SemanticProgram. Origin.Modules remains the complete declared dependency
// set; this optional field names the demand roots whose implementation is
// needed by this executable. The plan is validated as a subset, so metadata
// cannot introduce an undeclared package during compilation.
func semanticModuleLinkRoots(p *SemanticProgram) (map[string]bool, error) {
	if p == nil || p.Metadata == nil {
		return nil, nil
	}
	raw := strings.TrimSpace(p.Metadata["semantic_module_link_roots"])
	if raw == "" {
		return nil, nil
	}
	declared := map[string]bool{}
	for _, dep := range p.Origin.Modules {
		declared[dep] = true
	}
	roots := map[string]bool{}
	for _, root := range strings.Split(raw, ";") {
		root = strings.TrimSpace(root)
		if root == "" {
			continue
		}
		if !declared[root] {
			return nil, fmt.Errorf("semantic module link root %q is not declared in origin modules", root)
		}
		roots[root] = true
	}
	if len(roots) == 0 {
		return nil, fmt.Errorf("semantic module link plan has no roots")
	}
	return roots, nil
}

// semanticPackageReference marks a module as a registry-resolvable Semantic
// package rather than an ordinary language import. The explicit language makes
// downloading deterministic across registries (for example python:requests
// and rust:requests are distinct packages). Existing unqualified imports keep
// their compatibility behavior.
func semanticPackageReference(reference string) (language, name string, automatic bool) {
	parts := strings.SplitN(strings.TrimSpace(reference), ":", 3)
	if len(parts) == 3 && strings.EqualFold(parts[0], "pkg") && NormalizeLanguage(parts[1]) != "" && strings.TrimSpace(parts[2]) != "" {
		return NormalizeLanguage(parts[1]), strings.TrimSpace(parts[2]), true
	}
	return "", strings.TrimSpace(reference), false
}

func (r UniversalModuleResolver) linkPackageName(dst *SemanticProgram, name string, units map[string]bool) error {
	root := r.Store.Root
	ents, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		matches, _ := filepath.Glob(filepath.Join(root, e.Name(), name+".smod"))
		for _, m := range matches {
			return r.linkSMODUnits(dst, m, units)
		}
		for _, f := range []string{"*.smod"} {
			paths, _ := filepath.Glob(filepath.Join(root, e.Name(), f))
			for _, p := range paths {
				b, _ := os.ReadFile(p)
				var meta SemanticPackageManifest
				if json.Unmarshal(b, &meta) == nil && strings.EqualFold(meta.Name, name) {
					return r.linkSMODUnits(dst, p, units)
				}
			}
		}
	}
	return os.ErrNotExist
}

func (r UniversalModuleResolver) linkSMOD(dst *SemanticProgram, manifestPath string) error {
	return r.linkSMODUnits(dst, manifestPath, nil)
}

// linkSMODUnits links either every ready unit (nil selection) or exactly the
// manifest-relative SPZ units selected by semantic_module_unit_roots.
func (r UniversalModuleResolver) linkSMODUnits(dst *SemanticProgram, manifestPath string, selectedUnits map[string]bool) error {
	b, err := os.ReadFile(manifestPath)
	if err != nil {
		return err
	}
	var manifest SemanticPackageManifest
	if err = json.Unmarshal(b, &manifest); err != nil {
		return err
	}
	root := filepath.Dir(manifestPath)
	linked := 0
	batchCanonical := false
	var incompatible []string
	matchedUnits := map[string]bool{}
	for _, f := range manifest.Files {
		if f.Status != "SEMANTIC_READY" || f.SemanticPath == "" {
			continue
		}
		semanticPath := filepath.ToSlash(f.SemanticPath)
		if selectedUnits != nil && !selectedUnits[semanticPath] {
			continue
		}
		matchedUnits[semanticPath] = true
		data, re := os.ReadFile(filepath.Join(root, filepath.FromSlash(f.SemanticPath)))
		if re != nil {
			continue
		}
		part, pe := ParseSemanticSPZ(data)
		if pe == nil {
			// Package archives often contain hundreds of canonical SPZ units.
			// Recomputing the whole type/evidence closure after every append is
			// quadratic in the linked graph.  Canonical links are independent at
			// this boundary, so append all compatible graphs first and derive the
			// shared closure exactly once below.
			if dst.Body == nil && part.Body == nil && dst.UniversalAST != nil && part.UniversalAST != nil {
				before := len(dst.UniversalAST.Nodes)
				if mergeCanonicalUniversalASTRaw(dst.UniversalAST, part.UniversalAST) == nil {
					if len(dst.UniversalAST.Nodes) > before {
						linked++
						batchCanonical = true
					}
					continue
				}
			}
			if part.Body == nil && part.UniversalAST != nil {
				if _, ce := part.documentFromCanonicalUniversalAST(); ce != nil && len(incompatible) < 3 {
					incompatible = append(incompatible, f.Path+": "+ce.Error())
				}
			}
			before := 0
			if dst.UniversalAST != nil {
				before = len(dst.UniversalAST.Nodes)
			}
			mergeSemanticPrograms(dst, part)
			if dst.UniversalAST != nil && len(dst.UniversalAST.Nodes) > before {
				linked++
			}
		}
	}
	if selectedUnits != nil {
		missing := make([]string, 0)
		for path := range selectedUnits {
			if !matchedUnits[path] {
				missing = append(missing, path)
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			return fmt.Errorf("module unit roots not found in manifest: %s", strings.Join(missing, ", "))
		}
	}
	if batchCanonical {
		if err := finalizeCanonicalModuleMergeProject(dst.UniversalAST); err != nil {
			return err
		}
		dst.Evidence = dst.UniversalAST.Evidence
		dst.Origin = dst.UniversalAST.Origin
	}
	if linked == 0 {
		if len(incompatible) > 0 {
			return fmt.Errorf("module contains no linkable semantic units: %s", strings.Join(incompatible, "; "))
		}
		return fmt.Errorf("module contains no linkable semantic units")
	}
	return nil
}

// ImportGraph resolves the root and locally addressable transitive
// dependencies through this same resolver. External package names remain
// graph edges and never cause an external toolchain invocation.
func (r UniversalModuleResolver) ImportGraph(target string, opts ModuleImportOptions) (*SemanticModuleGraph, error) {
	root, err := r.ImportTarget(target, opts)
	if err != nil {
		return nil, err
	}
	g := &SemanticModuleGraph{Modules: map[string]*SemanticModule{}, Edges: map[string][]string{}}
	seen := map[string]bool{}
	var visit func(*SemanticModule, string) error
	visit = func(m *SemanticModule, base string) error {
		if m == nil || seen[m.Identity] {
			return nil
		}
		seen[m.Identity] = true
		g.Modules[m.Identity] = m
		g.Edges[m.Identity] = append([]string(nil), m.Dependencies...)
		for _, dep := range m.Dependencies {
			candidate := dep
			if !filepath.IsAbs(candidate) {
				candidate = filepath.Join(base, candidate)
			}
			if _, e := os.Stat(candidate); e != nil {
				continue
			}
			d, e := r.ImportTarget(candidate, ModuleImportOptions{})
			if e != nil {
				return e
			}
			if e = visit(d, filepath.Dir(candidate)); e != nil {
				return e
			}
		}
		return nil
	}
	if err = visit(root, filepath.Dir(target)); err != nil {
		return nil, err
	}
	return g, nil
}

func NewUniversalModuleResolver() (UniversalModuleResolver, error) {
	s, err := DefaultSemanticModuleStore()
	return UniversalModuleResolver{Store: s}, err
}

func (r UniversalModuleResolver) ImportTarget(target string, opts ModuleImportOptions) (*SemanticModule, error) {
	if target == "" {
		return nil, fmt.Errorf("empty module target")
	}
	det := DetectArtifact(target)
	lang := NormalizeLanguage(opts.Language)
	if lang == "" {
		lang = det.Language
	}
	if det.Confidence == "AMBIGUOUS" && opts.Language == "" {
		return nil, fmt.Errorf("ambiguous artifact language; use --language")
	}
	if det.Confidence == "UNKNOWN" && lang == "" {
		return nil, fmt.Errorf("unknown artifact; use --language")
	}
	if st, err := os.Stat(target); err == nil && st.IsDir() {
		return r.importDirectory(target, lang, det, opts)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(filepath.Ext(target))
	if lower == ".se" || lower == ".spz" {
		var p *SemanticProgram
		if lower == ".spz" {
			p, err = ParseSemanticSPZ(data)
		} else {
			p, err = ParseSemanticSE(data)
		}
		if err != nil {
			return nil, err
		}
		m, err := NewSemanticModule(filepath.Base(target), "semantic", string(data), p)
		if err != nil {
			return nil, err
		}
		m.Kind = SemanticModuleArtifact
		if err = VerifySemanticModule(m); err != nil {
			return nil, err
		}
		return r.persist(m)
	}
	if lower == ".asm" || lower == ".s" {
		p, e := LiftBinaryInput(data, CompileOptions{InputKind: CompileInputAssembly, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
		if e != nil {
			return nil, e
		}
		m, e := NewSemanticModule(filepath.Base(target), "assembly", string(data), p)
		if e != nil {
			return nil, e
		}
		m.Kind = AssemblyModule
		m.ABI = "win64"
		return r.persist(m)
	}
	if lower == ".dll" || lower == ".exe" || lower == ".obj" {
		kind := CompileInputExecutable
		if lower == ".obj" {
			kind = CompileInputObject
		}
		p, e := LiftBinaryInput(data, CompileOptions{InputKind: kind, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
		if e == nil {
			m, e := NewSemanticModule(filepath.Base(target), "binary", string(data), p)
			if e != nil {
				return nil, e
			}
			m.Kind = BinaryModule
			m.ABI = "win64"
			return r.persist(m)
		}
		// A binary that cannot be reconstructed remains a first-class external
		// module. The opaque capsule is represented by metadata, never guessed AST.
		p, pe := opaqueNativeExternalProgram(filepath.Base(target), data, e)
		if pe != nil {
			return nil, fmt.Errorf("binary analysis failed: %w", e)
		}
		m, pe := NewSemanticModule(filepath.Base(target), "binary", string(data), p)
		if pe != nil {
			return nil, pe
		}
		m.Kind = NativeExternal
		m.ABI = "unknown"
		m.Capabilities = []string{"symbols", "abi", "runtime_external"}
		m.RuntimeRequirements = []string{"external_native"}
		m.MetadataSet("binary_analysis", e.Error())
		return r.persist(m)
	}
	if lang == "" {
		return nil, fmt.Errorf("language detection failed; use --language")
	}
	m, err := ImportModule(filepath.Base(target), lang, target, string(data))
	if err != nil {
		return nil, err
	}
	if opts.SkipPersist {
		return m, nil
	}
	return r.persist(m)
}

func opaqueNativeExternalProgram(identity string, data []byte, cause error) (*SemanticProgram, error) {
	p := NewSemanticProgram(&BlockStmt{}, "eager_left_to_right")
	sum := sha256.Sum256(data)
	p.Origin = SemanticOrigin{SourceLanguage: "binary", EntryPoint: identity}
	p.Metadata = map[string]string{"artifact_kind": "NATIVE_EXTERNAL", "binary_sha256": hex.EncodeToString(sum[:]), "analysis_status": "UNKNOWN"}
	p.UniversalAST = &UniversalASTDocument{SchemaVersion: 1, Projection: "external_binary.v1", LanguageProfile: "binary", Origin: p.Origin, Metadata: p.Metadata, Nodes: []UniversalASTNode{{ID: 0, StructuralKind: "ModuleDecl", FieldMask: []string{"id", "structural_kind"}}}}
	if cause != nil {
		p.Metadata["analysis_error"] = cause.Error()
	}
	return p, nil
}

func (r UniversalModuleResolver) persist(m *SemanticModule) (*SemanticModule, error) {
	if m != nil {
		root, err := semanticModuleStableRoot(m.Program)
		if err != nil {
			return nil, err
		}
		m.SemanticRoot = root
		deps := append([]string(nil), m.Dependencies...)
		sort.Strings(deps)
		contracts := append([]string(nil), m.Contracts...)
		sort.Strings(contracts)
		seed := strings.Join([]string{m.SourceHash, m.FrontendVersion, fmt.Sprint(SemanticSPVersion), root, strings.Join(deps, ","), strings.Join(contracts, ",")}, "|")
		sum := sha256.Sum256([]byte(seed))
		m.CacheKey = hex.EncodeToString(sum[:])
	}
	if r.Store.Root != "" {
		if err := r.Store.Put(m); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *SemanticModule) MetadataSet(k, v string) {
	if m.Program != nil {
		if m.Program.Metadata == nil {
			m.Program.Metadata = map[string]string{}
		}
		m.Program.Metadata[k] = v
	}
}

func (r UniversalModuleResolver) importDirectory(dir, lang string, det DetectionResult, opts ModuleImportOptions) (*SemanticModule, error) {
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		// Generated resolver metadata is an output cache, not a module source.
		// Including it makes a directory import recursively ingest its own
		// previous merge and can select module.se as the primary entrypoint.
		if err == nil && d.IsDir() && d.Name() == ".semantic-cache" {
			return filepath.SkipDir
		}
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	// A Semantic project directory is a transport tree. Build journals, native
	// outputs and editor files beside it are not source units and must never be
	// handed to a language frontend merely because they share the directory.
	if strings.EqualFold(opts.Language, "sp") || strings.EqualFold(opts.Language, "semantic") || strings.EqualFold(lang, "sp") || strings.EqualFold(lang, "semantic") {
		semanticFiles := files[:0]
		for _, file := range files {
			switch strings.ToLower(filepath.Ext(file)) {
			case ".se", ".sp", ".spz", ".smod":
				semanticFiles = append(semanticFiles, file)
			case ".json":
				// JSON is a supported Semantic transport, but a Semantic project
				// directory can also contain build journals, manifests and editor
				// metadata. Include JSON only when it parses as the actual
				// SemanticProgram transport; never treat arbitrary JSON as source.
				data, readErr := os.ReadFile(file)
				if readErr == nil {
					if _, parseErr := ParseSemanticJSON(data); parseErr == nil {
						semanticFiles = append(semanticFiles, file)
					}
				}
			}
		}
		files = semanticFiles
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("directory contains no importable artifacts")
	}
	opts.report("discovered", dir, 0, len(files))
	// A single language directory is lowered as one package boundary. Mixed
	// components are retained as deterministic provenance/dependency metadata;
	// each component can subsequently be resolved through this same resolver.
	var primary string
	for _, f := range files {
		d := DetectArtifact(f)
		if lang != "" && d.Language == lang {
			primary = f
			break
		}
		if primary == "" && d.Language != "" {
			primary = f
		}
	}
	// Prefer an explicit main.se entrypoint when present.  Otherwise retain
	// deterministic discovery order for ordinary semantic packages.
	for _, f := range files {
		if strings.EqualFold(filepath.Base(f), "main.se") {
			primary = f
			break
		}
	}
	if primary == "" {
		return nil, fmt.Errorf("directory has no source for selected language")
	}
	b, err := os.ReadFile(primary)
	if err != nil {
		return nil, err
	}
	// Reuse a previously persisted linked module when the complete directory
	// fingerprint is unchanged. This is deliberately checked before parsing
	// any sibling unit: the cache is keyed by the same primary-plus-component
	// hash used by persist(), so stale semantic data can never be reused.
	if cached, ok := r.openDirectoryCache(dir, files, primary, b); ok {
		opts.report("cache-hit", dir, len(files), len(files))
		if cached.Program != nil && cached.Program.UniversalAST != nil {
			if cached.Program.UniversalAST.Metadata == nil {
				cached.Program.UniversalAST.Metadata = map[string]string{}
			}
			cached.Program.UniversalAST.Metadata["graph_merge"] = "uast-disjoint-namespace-v1"
			cached.Program.UniversalAST.Metadata["graph_merge_inputs"] = strconv.Itoa(len(files))
			if cached.Program.Metadata == nil {
				cached.Program.Metadata = map[string]string{}
			}
			cached.Program.Metadata["graph_merge"] = "uast-disjoint-namespace-v1"
			cached.Program.Metadata["graph_merge_inputs"] = strconv.Itoa(len(files))
		}
		return cached, nil
	}
	opts.report("primary", primary, 1, len(files))
	var m *SemanticModule
	primaryExt := strings.ToLower(filepath.Ext(primary))
	if primaryExt == ".se" || primaryExt == ".sp" || primaryExt == ".spz" || primaryExt == ".json" {
		var p *SemanticProgram
		switch primaryExt {
		case ".se":
			p, err = ParseSemanticSE(b)
		case ".sp":
			p, err = ParseSemanticSP(b)
		case ".spz":
			p, err = ParseSemanticSPZ(b)
		case ".json":
			p, err = ParseSemanticJSON(b)
		}
		if err == nil {
			m, err = NewSemanticModule(filepath.Base(dir), "semantic", primary, p)
		}
	} else {
		m, err = ImportModule(filepath.Base(dir), lang, primary, string(b))
	}
	if err != nil {
		return nil, err
	}
	// A directory is one module boundary: lower every source unit and merge
	// their executable declarations into the same SemanticProgram.  Keeping
	// the merge here makes compile/link/import agree and avoids treating sibling
	// files as metadata-only dependencies.
	// Persisted SE/SP/SPZ units are already canonical UAST documents. Appending
	// each one through mergeSemanticPrograms would call the whole-document
	// type/evidence closure after every unit, making a 174-unit project
	// quadratic. Keep structural appends batch-local and close the canonical
	// graph exactly once below. Any non-canonical/legacy unit still takes the
	// existing compatibility merge path unchanged.
	batchCanonical := false
	for index, f := range files {
		if f == primary || filepath.Ext(f) == "" {
			continue
		}
		opts.report("unit", f, index+1, len(files))
		fd, re := os.ReadFile(f)
		if re != nil {
			return nil, re
		}
		var part *SemanticProgram
		var pe error
		ext := strings.ToLower(filepath.Ext(f))
		switch ext {
		case ".se":
			part, pe = ParseSemanticSE(fd)
		case ".sp":
			part, pe = ParseSemanticSP(fd)
		case ".spz":
			part, pe = ParseSemanticSPZ(fd)
		case ".json":
			part, pe = ParseSemanticJSON(fd)
		default:
			part, pe = LowerSource(lang, f, string(fd))
		}
		if pe != nil {
			continue // non-source assets remain provenance dependencies
		}
		if m.Program != nil && part != nil && m.Program.Body == nil && part.Body == nil && m.Program.UniversalAST != nil && part.UniversalAST != nil {
			before := len(m.Program.UniversalAST.Nodes)
			if mergeCanonicalUniversalASTRaw(m.Program.UniversalAST, part.UniversalAST) == nil {
				if len(m.Program.UniversalAST.Nodes) > before {
					batchCanonical = true
				}
				m.Program.Origin.Modules = mergeModuleNames(m.Program.Origin.Modules, part.Origin.Modules)
				m.Program.UniversalAST.Origin.Modules = append([]string(nil), m.Program.Origin.Modules...)
				continue
			}
		}
		mergeSemanticPrograms(m.Program, part)
	}
	m.Dependencies = nil
	depDigest := sha256.New()
	_, _ = depDigest.Write(b)
	for _, f := range files {
		if f == primary {
			continue
		}
		sum := sha256.Sum256(mustRead(f))
		_, _ = depDigest.Write(sum[:])
		m.Dependencies = append(m.Dependencies, filepath.ToSlash(f))
		if m.Program != nil {
			if m.Program.Metadata == nil {
				m.Program.Metadata = map[string]string{}
			}
			m.Program.Metadata["component_hash:"+filepath.ToSlash(f)] = hex.EncodeToString(sum[:])
		}
	}
	m.SourceHash = hex.EncodeToString(depDigest.Sum(nil))
	sort.Strings(m.Dependencies)
	// Individual semantic transports can arrive through either the legacy-view
	// merge or the canonical-only merge. Establish one final canonical closure
	// for the whole directory so derived type/evidence data and, where valid,
	// the compatibility digest describe the linked project rather than an
	// arbitrary intermediate unit.
	if m.Program != nil && m.Program.UniversalAST != nil {
		// A raw batch links several independent canonical documents below one
		// project root. Its executable authority is the combined UAST, not a
		// fabricated single legacy SemanticDocument. Reclassify before closure so
		// finalization does not serialize/clone the complete project merely to
		// discover that the compatibility view is inapplicable.
		if batchCanonical && m.Program.UniversalAST.Projection == "semantic_document.v1" {
			m.Program.UniversalAST.Projection = "frontend_facts.v1"
			m.Program.UniversalAST.SemanticDocumentSHA256 = ""
		}
		var closeErr error
		if batchCanonical {
			closeErr = finalizeCanonicalModuleMergeProject(m.Program.UniversalAST)
		} else {
			closeErr = finalizeCanonicalModuleMerge(m.Program.UniversalAST)
		}
		if closeErr != nil {
			return nil, closeErr
		}
		if batchCanonical && m.Program.Metadata != nil {
			m.Program.Metadata["canonical_merge_mode"] = "batch_raw_then_single_closure"
			// Every member is already structurally closed before its disjoint
			// namespace is appended.  This explicit boundary lets subsequent
			// lowering stages avoid rebuilding global adjacency/binding caches.
			m.Program.Metadata["graph_merge"] = "uast-disjoint-namespace-v1"
			m.Program.Metadata["graph_merge_inputs"] = strconv.Itoa(len(files))
		}
		m.Program.Evidence = m.Program.UniversalAST.Evidence
		m.Program.Origin = m.Program.UniversalAST.Origin
	}
	if det.Mixed {
		m.Capabilities = append(m.Capabilities, "mixed_components")
	}
	if opts.SkipPersist {
		return m, nil
	}
	return r.persist(m)
}

// openDirectoryCache returns the persisted linked module for dir when its
// exact source-unit set and source hash match. Metadata is intentionally read
// before module.spz so cache misses never allocate the large canonical graph.
func (r UniversalModuleResolver) openDirectoryCache(dir string, files []string, primary string, primaryBytes []byte) (*SemanticModule, bool) {
	if strings.TrimSpace(r.Store.Root) == "" {
		return nil, false
	}
	modulesDir := filepath.Join(r.Store.Root, "modules")
	entries, err := os.ReadDir(modulesDir)
	if err != nil {
		return nil, false
	}
	hash := directorySourceHash(primaryBytes, files, primary)
	wantDeps := make([]string, 0, len(files)-1)
	for _, file := range files {
		if file != primary {
			wantDeps = append(wantDeps, filepath.ToSlash(file))
		}
	}
	sort.Strings(wantDeps)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		metaBytes, readErr := os.ReadFile(filepath.Join(modulesDir, entry.Name(), "module.meta"))
		if readErr != nil {
			continue
		}
		var meta struct {
			SourceHash   string   `json:"source_hash"`
			Dependencies []string `json:"dependencies"`
		}
		if json.Unmarshal(metaBytes, &meta) != nil || meta.SourceHash != hash {
			continue
		}
		deps := append([]string(nil), meta.Dependencies...)
		for i := range deps {
			deps[i] = filepath.ToSlash(deps[i])
		}
		sort.Strings(deps)
		if len(deps) != len(wantDeps) {
			continue
		}
		match := true
		for i := range deps {
			if deps[i] != wantDeps[i] {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		// A historical directory cache can be a monolithic hundreds-of-MB
		// semantic graph. Loading it defeats incremental compilation by
		// expanding the complete JSON/SPZ payload in RAM. Such caches are
		// intentionally ignored; per-unit caches are safe to add below.
		spzInfo, statErr := os.Stat(filepath.Join(modulesDir, entry.Name(), "module.spz"))
		if statErr != nil || spzInfo.Size() > 16*1024*1024 {
			continue
		}
		cached, openErr := r.Store.Open(entry.Name())
		if openErr == nil && cached != nil && cached.Program != nil {
			return cached, true
		}
	}
	return nil, false
}

func directorySourceHash(primaryBytes []byte, files []string, primary string) string {
	digest := sha256.New()
	_, _ = digest.Write(primaryBytes)
	for _, file := range files {
		if file == primary {
			continue
		}
		if data, err := os.ReadFile(file); err == nil {
			sum := sha256.Sum256(data)
			_, _ = digest.Write(sum[:])
		}
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// mergeSemanticPrograms links source units at the canonical program boundary.
// It intentionally reuses the existing statement/UAST structures; no second
// module IR is introduced.
func mergeSemanticPrograms(dst, src *SemanticProgram) {
	if dst == nil || src == nil {
		return
	}
	// SPZ/SE modules intentionally persist the canonical UAST without a second
	// executable tree. Reconstruct the existing compatibility view only when
	// the canonical projection proves that it is lossless, then use the same
	// deterministic body merge as source-directory compilation.
	if dst.Body == nil && dst.UniversalAST != nil {
		_, _ = dst.documentFromCanonicalUniversalAST()
	}
	if src.Body == nil && src.UniversalAST != nil {
		_, _ = src.documentFromCanonicalUniversalAST()
	}
	if src.Body == nil && dst.UniversalAST != nil && src.UniversalAST != nil {
		if err := mergeCanonicalUniversalAST(dst.UniversalAST, src.UniversalAST); err == nil {
			// Canonical-only transports carry imports in Origin rather than in a
			// compatibility statement tree. Preserve the ordered union here so a
			// semantic merge of several .se files links every declared module on
			// the later compile path, not only the first input's module.
			dst.Origin.Modules = mergeModuleNames(dst.Origin.Modules, src.Origin.Modules)
			dst.UniversalAST.Origin.Modules = append([]string(nil), dst.Origin.Modules...)
			dst.Evidence = dst.UniversalAST.Evidence
			dst.Origin = dst.UniversalAST.Origin
			return
		} else {
			// The raw append mutates dst before closure. Preserve the immediate
			// structural reason for the fallback so callers never only see a
			// later, misleading missing-digest validation failure.
			if dst.Metadata == nil {
				dst.Metadata = map[string]string{}
			}
			dst.Metadata["canonical_merge_fallback"] = err.Error()
		}
	}
	hadBody := dst.Body != nil || src.Body != nil
	if dst.Body == nil {
		dst.Body = &BlockStmt{}
	}
	// Semantic graph imports may carry their contract on UniversalAST rather
	// than on the compatibility SemanticProgram view. Promote it before the
	// merge so rebuilding the linked graph cannot erase value/type contracts.
	if dst.Evaluation == "" && dst.UniversalAST != nil {
		dst.Evaluation = dst.UniversalAST.Evaluation
	}
	if dst.Evaluation == "" {
		dst.Evaluation = src.Evaluation
		if dst.Evaluation == "" && src.UniversalAST != nil {
			dst.Evaluation = src.UniversalAST.Evaluation
		}
		if dst.Evaluation == "" {
			dst.Evaluation = "eager_left_to_right"
		}
	}
	if dst.ValueModel == "" && dst.UniversalAST != nil {
		dst.ValueModel = dst.UniversalAST.ValueModel
	}
	if dst.ValueModel == "" {
		dst.ValueModel = src.ValueModel
		if dst.ValueModel == "" && src.UniversalAST != nil {
			dst.ValueModel = src.UniversalAST.ValueModel
		}
	}
	if dst.IndexBase == 0 && dst.UniversalAST != nil {
		dst.IndexBase = dst.UniversalAST.IndexBase
	}
	if dst.IndexBase == 0 && src.IndexBase != 0 {
		dst.IndexBase = src.IndexBase
	}
	if dst.Types.SchemaVersion == 0 {
		if dst.UniversalAST != nil && dst.UniversalAST.Types.SchemaVersion != 0 {
			dst.Types = dst.UniversalAST.Types
		} else if src.Types.SchemaVersion != 0 {
			dst.Types = src.Types
		} else if src.UniversalAST != nil {
			dst.Types = src.UniversalAST.Types
		}
	}
	if src.Body != nil {
		dst.Body.List = append(dst.Body.List, src.Body.List...)
	}
	if dst.Origin.Modules == nil {
		dst.Origin.Modules = append([]string(nil), src.Origin.Modules...)
	} else {
		dst.Origin.Modules = append(dst.Origin.Modules, src.Origin.Modules...)
	}
	// Re-project the linked body so persisted .se/.spz representations retain
	// every merged unit instead of only the primary file's UAST snapshot.
	if hadBody {
		if rebuilt := NewSemanticProgram(dst.Body, dst.Evaluation); rebuilt != nil && rebuilt.UniversalAST != nil {
			dst.UniversalAST = rebuilt.UniversalAST
			dst.Evidence = rebuilt.Evidence
			// NewSemanticProgram supplies a default contract, but preserve explicit
			// contracts from the imported graph when they are richer.
			dst.UniversalAST.ValueModel = dst.ValueModel
			dst.UniversalAST.IndexBase = dst.IndexBase
			dst.UniversalAST.Types = dst.Types
		}
	}
}

func mergeModuleNames(left, right []string) []string {
	seen := make(map[string]bool, len(left)+len(right))
	out := make([]string, 0, len(left)+len(right))
	for _, names := range [][]string{left, right} {
		for _, name := range names {
			name = strings.TrimSpace(name)
			if name != "" && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}

// mergeCanonicalUniversalAST links a UAST-only module without inventing a
// second statement IR. Node and scope identifiers are document-local, so the
// source graph is rebased before its root is attached to the destination root.
func mergeCanonicalUniversalAST(dst, src *UniversalASTDocument) error {
	if err := mergeCanonicalUniversalASTRaw(dst, src); err != nil {
		return err
	}
	return finalizeCanonicalModuleMerge(dst)
}

// mergeCanonicalUniversalASTRaw is the structural half of a module merge.
// Callers importing many independent SPZ units batch raw merges and invoke
// finalizeCanonicalModuleMerge once, preserving the identical canonical
// result without repeatedly deriving an increasingly large evidence graph.
func mergeCanonicalUniversalASTRaw(dst, src *UniversalASTDocument) error {
	if dst == nil || src == nil || len(dst.Nodes) == 0 || len(src.Nodes) == 0 {
		return fmt.Errorf("canonical module graph is empty")
	}
	if dst.ContractSchema != "" && src.ContractSchema != "" && dst.ContractSchema != src.ContractSchema {
		return fmt.Errorf("cannot merge semantic contract schemas %q and %q", dst.ContractSchema, src.ContractSchema)
	}
	if dst.ContractSchema == "" {
		dst.ContractSchema = src.ContractSchema
	}
	if (len(dst.ContractTable) != 0 || len(dst.ContractRefs) != 0) && dst.ContractSchema != semanticContractSchema {
		return fmt.Errorf("unsupported destination semantic contract schema %q", dst.ContractSchema)
	}
	if (len(src.ContractTable) != 0 || len(src.ContractRefs) != 0) && src.ContractSchema != semanticContractSchema {
		return fmt.Errorf("unsupported source semantic contract schema %q", src.ContractSchema)
	}
	maxNode, maxScope := -1, -1
	for _, n := range dst.Nodes {
		if n.ID > maxNode {
			maxNode = n.ID
		}
		if raw := n.Fields["scope_id"]; len(raw) != 0 {
			var scope int
			if json.Unmarshal(raw, &scope) == nil && scope > maxScope {
				maxScope = scope
			}
		}
	}
	nodeMap, scopeMap := map[int]int{}, map[int]int{}
	for _, n := range src.Nodes {
		maxNode++
		nodeMap[n.ID] = maxNode
		if raw := n.Fields["scope_id"]; len(raw) != 0 {
			var scope int
			if json.Unmarshal(raw, &scope) == nil {
				if _, ok := scopeMap[scope]; !ok {
					maxScope++
					scopeMap[scope] = maxScope
				}
			}
		}
	}
	rebased := make([]UniversalASTNode, 0, len(src.Nodes))
	for _, n := range src.Nodes {
		n.ID = nodeMap[n.ID]
		if n.Fields == nil {
			n.Fields = map[string]json.RawMessage{}
		}
		n.Fields["id"], _ = json.Marshal(n.ID)
		if raw := n.Fields["scope_id"]; len(raw) != 0 {
			var old int
			if json.Unmarshal(raw, &old) == nil {
				n.Fields["scope_id"], _ = json.Marshal(scopeMap[old])
			}
		}
		rebased = append(rebased, n)
	}
	// Contract IDs are local to each serialized UAST unit. Rebase and intern
	// the source table before attaching its references; copying node IDs alone
	// leaves imported SymbolRef/CallExpr/etc. nodes pointing into the importer's
	// unrelated contract table (or beyond its end).
	contractIDMap := make([]int, len(src.ContractTable))
	contractsByHash := make(map[string]int, len(dst.ContractTable)+len(src.ContractTable))
	contractKey := func(contract *SemanticContract) (string, error) {
		canonical, err := canonicalJSONBytes(contract.Payload)
		if err != nil {
			return "", err
		}
		contract.Payload = canonical
		key := semanticContractHash(contract.Kind, canonical)
		if contract.Hash != "" && contract.Hash != key {
			return "", fmt.Errorf("semantic contract %d hash does not match its canonical payload", contract.ID)
		}
		contract.Hash = key
		return key, nil
	}
	for i := range dst.ContractTable {
		contract := &dst.ContractTable[i]
		if contract.ID != i {
			return fmt.Errorf("destination semantic contract IDs are not contiguous at %d", i)
		}
		key, err := contractKey(contract)
		if err != nil {
			return fmt.Errorf("destination semantic contract %d: %w", i, err)
		}
		if prior, ok := contractsByHash[key]; ok {
			return fmt.Errorf("destination semantic contract hash %q is duplicated at %d and %d", key, prior, i)
		}
		contractsByHash[key] = i
	}
	for i, contract := range src.ContractTable {
		if contract.ID != i {
			return fmt.Errorf("source semantic contract IDs are not contiguous at %d", i)
		}
		key, err := contractKey(&contract)
		if err != nil {
			return fmt.Errorf("source semantic contract %d: %w", i, err)
		}
		if existing, ok := contractsByHash[key]; ok {
			contractIDMap[i] = existing
			continue
		}
		contract.ID = len(dst.ContractTable)
		contractIDMap[i] = contract.ID
		dst.ContractTable = append(dst.ContractTable, contract)
		contractsByHash[key] = contract.ID
	}
	for _, ref := range src.ContractRefs {
		mappedNode, ok := nodeMap[ref.NodeID]
		if !ok {
			return fmt.Errorf("module contract reference node %d is missing", ref.NodeID)
		}
		if ref.ContractID < 0 || ref.ContractID >= len(contractIDMap) {
			return fmt.Errorf("module node %d references absent contract %d", ref.NodeID, ref.ContractID)
		}
		ref.NodeID = mappedNode
		ref.ContractID = contractIDMap[ref.ContractID]
		dst.ContractRefs = append(dst.ContractRefs, ref)
	}
	relations := make([]UniversalASTRelation, 0, len(src.Relations)+1)
	for _, relation := range src.Relations {
		mapped, ok := nodeMap[relation.From]
		if !ok {
			return fmt.Errorf("module relation source %d is missing", relation.From)
		}
		relation.From = mapped
		var old int
		if _, err := fmt.Sscan(relation.To.ID, &old); err == nil {
			switch relation.To.Domain {
			case "node":
				if target, ok := nodeMap[old]; ok {
					relation.To.ID = strconv.Itoa(target)
				}
			case "scope":
				if target, ok := scopeMap[old]; ok {
					relation.To.ID = strconv.Itoa(target)
				}
			}
		}
		relations = append(relations, relation)
	}
	dstRoot, srcRoot := dst.Nodes[0].ID, nodeMap[src.Nodes[0].ID]
	ordinal := 0
	for _, relation := range dst.Relations {
		if relation.Kind == "syntax.child" && relation.From == dstRoot {
			ordinal++
		}
	}
	ord, _ := json.Marshal(ordinal)
	// A linked module root is a top-level semantic child.  Keep the canonical
	// block contract (statement children); module provenance remains available
	// through the node's structural kind and origin metadata.
	role, _ := json.Marshal("statement")
	relations = append(relations, UniversalASTRelation{Kind: "syntax.child", From: dstRoot, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(srcRoot)}, Attributes: map[string]json.RawMessage{"ordinal": ord, "role": role}})
	dst.Nodes = append(dst.Nodes, rebased...)
	dst.Relations = append(dst.Relations, relations...)
	dst.TypeTable = append(dst.TypeTable, src.TypeTable...)
	return nil
}

func finalizeCanonicalModuleMerge(dst *UniversalASTDocument) error {
	return finalizeCanonicalModuleMergeMode(dst, false)
}

// finalizeCanonicalModuleMergeProject closes a linked canonical project using
// only the production facts consumed by lowering.  Evidence matrices are a
// derived audit projection: materializing their node-sorted and adjacency
// copies for every linked source unit made large project builds quadratic in
// both RAM and work.  They remain available through AnalyzeUniversalEvidence
// for reports and exports, but are intentionally lazy on this hot path.
//
// Canonical nodes, relations, TypeTable and TypeGraph remain the complete
// executable authority; no source or semantic fact is discarded.
func finalizeCanonicalModuleMergeProject(dst *UniversalASTDocument) error {
	return finalizeCanonicalModuleMergeMode(dst, true)
}

func finalizeCanonicalModuleMergeMode(dst *UniversalASTDocument, linkedProject bool) error {
	if dst == nil {
		return fmt.Errorf("canonical module graph is empty")
	}
	// deriveUniversalTypeTable validates the canonical document before it
	// derives relations. A semantic_document.v1 digest is derived *after* that
	// closure, but the validator still requires a syntactically valid digest
	// while the closure is running. Install a temporary value first; it is
	// replaced with the digest of the finished compatibility view below, or the
	// document is explicitly reclassified as frontend_facts.v1 when no such
	// view exists.
	if dst.Projection == "semantic_document.v1" {
		dst.SemanticDocumentSHA256 = strings.Repeat("0", 64)
	} else {
		dst.SemanticDocumentSHA256 = ""
	}
	dst.TypeRelations = nil
	dst.Evidence = SemanticEvidence{}
	var typeErr error
	if linkedProject {
		typeErr = deriveUniversalTypeTableForLinkedProject(dst)
	} else {
		typeErr = deriveUniversalTypeTable(dst)
	}
	if typeErr != nil {
		return typeErr
	}
	if linkedProject {
		// SemanticEvidence is an analysis cache, not an input to generic
		// lowering. Avoid a second full node index plus several sparse
		// N-by-N relation matrices while a project is linked.
		dst.Evidence = SemanticEvidence{}
		if dst.Metadata == nil {
			dst.Metadata = map[string]string{}
		}
		dst.Metadata["derived.evidence"] = "lazy"
	} else {
		evidence, err := AnalyzeUniversalEvidence(dst)
		if err != nil {
			return err
		}
		dst.Evidence = evidence
	}
	// Raw canonical appends invalidate the compatibility-document digest. The
	// digest is a derived identity, not optional source data: rebuild it from
	// the exact compatibility view once closure has produced the final graph.
	if dst.Projection == "semantic_document.v1" {
		doc, err := SemanticDocumentFromUniversalAST(dst)
		if err != nil {
			// A linked project can retain canonical facts that no longer have one
			// lossless legacy SemanticDocument view (for example several document
			// roots with project-level bindings). This is not a semantic failure:
			// the canonical UAST remains the executable authority. Mark it as the
			// existing frontend-facts projection rather than publishing a forged
			// or stale compatibility digest.
			dst.Projection = "frontend_facts.v1"
			dst.SemanticDocumentSHA256 = ""
			return nil
		}
		dst.SemanticDocumentSHA256 = semanticDocumentDigest(doc)
	}
	return nil
}
func mustRead(path string) []byte { b, _ := os.ReadFile(path); return b }
