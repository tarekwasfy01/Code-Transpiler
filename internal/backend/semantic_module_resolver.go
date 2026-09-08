// Copyright (c) 2026 Tarek Wasfy
package backend

// UniversalModuleResolver is the single import boundary for source, semantic,
// assembly and binary artifacts. It never invokes an external compiler.
import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ModuleImportOptions struct{ Language string }
type UniversalModuleResolver struct{ Store SemanticModuleStore }

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
		return r.importDirectory(target, lang, det)
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

func (r UniversalModuleResolver) importDirectory(dir, lang string, det DetectionResult) (*SemanticModule, error) {
	var files []string
	_ = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("directory contains no importable artifacts")
	}
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
	if primary == "" {
		return nil, fmt.Errorf("directory has no source for selected language")
	}
	b, err := os.ReadFile(primary)
	if err != nil {
		return nil, err
	}
	m, err := ImportModule(filepath.Base(dir), lang, primary, string(b))
	if err != nil {
		return nil, err
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
	if det.Mixed {
		m.Capabilities = append(m.Capabilities, "mixed_components")
	}
	return r.persist(m)
}
func mustRead(path string) []byte { b, _ := os.ReadFile(path); return b }
