// Copyright (c) 2026 Tarek Wasfy
package backend

// Package import keeps the original tree and a semantic projection side by
// side.  The .smod manifest is deliberately data-only so later compilation can
// resolve linked units without reparsing or scanning the package again.
import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

type SemanticPackageFile struct {
	Path                 string   `json:"path"`
	Kind                 string   `json:"kind,omitempty"`
	Language             string   `json:"language,omitempty"`
	SemanticPath         string   `json:"semantic_path,omitempty"`
	ReadableSemanticPath string   `json:"readable_semantic_path,omitempty"`
	SourceHash           string   `json:"source_hash"`
	Dependencies         []string `json:"dependencies,omitempty"`
	Symbols              []string `json:"symbols,omitempty"`
	Status               string   `json:"status"`
}

type SemanticPackageManifest struct {
	SchemaVersion  int                   `json:"schema_version"`
	Name           string                `json:"name"`
	ModulePath     string                `json:"module_path,omitempty"`
	GoRequirements map[string]string     `json:"go_requirements,omitempty"`
	Source         string                `json:"source"`
	Root           string                `json:"root"`
	PackageHash    string                `json:"package_hash"`
	Files          []SemanticPackageFile `json:"files"`
	Licenses       []string              `json:"licenses,omitempty"`
	Dependencies   []string              `json:"dependencies,omitempty"`
}

// CopyImportedPackageLicenses copies the licenses recorded by imported
// semantic-package manifests next to an output artifact. It only reads the
// manifest's explicit license list and never treats source or diagnostics as
// license data.
func CopyImportedPackageLicenses(storeRoot, outputPath string) (int, error) {
	root := strings.TrimSpace(storeRoot)
	if root == "" {
		store, err := DefaultSemanticModuleStore()
		if err != nil {
			return 0, err
		}
		root = store.Root
	}
	if st, err := os.Stat(filepath.Join(root, "Semantic", "Modules")); err == nil && st.IsDir() {
		root = filepath.Join(root, "Semantic", "Modules")
	}
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return 0, nil
	}
	destBase := outputPath
	if filepath.Ext(outputPath) != "" {
		destBase = filepath.Dir(outputPath)
	}
	dest := filepath.Join(destBase, "licenses")
	seen := map[string]bool{}
	count := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(strings.ToLower(d.Name()), ".smod") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var manifest SemanticPackageManifest
		if json.Unmarshal(data, &manifest) != nil || manifest.Root == "" {
			return nil
		}
		for _, rel := range manifest.Licenses {
			rel = filepath.ToSlash(strings.TrimSpace(rel))
			if rel == "" {
				continue
			}
			candidates := []string{
				filepath.Join(manifest.Root, filepath.FromSlash(rel)),
				filepath.Join(manifest.Root, "licenses", filepath.FromSlash(rel)),
				filepath.Join(manifest.Root, "source", filepath.FromSlash(rel)),
			}
			var src string
			var info os.FileInfo
			for _, candidate := range candidates {
				if candidateInfo, statErr := os.Stat(candidate); statErr == nil && !candidateInfo.IsDir() {
					src, info = candidate, candidateInfo
					break
				}
			}
			if src == "" || info == nil {
				continue
			}
			hash := sha256.Sum256([]byte(filepath.ToSlash(manifest.Name) + ":" + rel))
			key := hex.EncodeToString(hash[:])
			if seen[key] {
				continue
			}
			seen[key] = true
			name := SafeModuleName(manifest.Name) + "__" + filepath.Base(rel)
			target := filepath.Join(dest, name)
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := copyFile(src, target); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

func packageWorkerCount() int {
	n := runtime.NumCPU() * 80 / 100
	if n < 32 {
		return 32
	}
	return n
}

// NormalizeGoPackageModuleReferences maps external Go package imports to the
// Go module identities declared by the nearest go.mod. Standard-library
// imports and imports inside the current module remain ordinary import facts.
// This lets the common Semantic module resolver download/cache third-party
// source packages once, independently of a particular target language.
func NormalizeGoPackageModuleReferences(p *SemanticProgram, baseDir string) {
	if p == nil || !strings.EqualFold(p.Origin.SourceLanguage, "go") || len(p.Origin.Modules) == 0 {
		return
	}
	modulePath, requirements := readGoModuleRequirements(baseDir)
	if len(requirements) == 0 {
		return
	}
	for i, importPath := range p.Origin.Modules {
		importPath = strings.TrimSpace(importPath)
		if importPath == "" || strings.HasPrefix(importPath, "pkg:go:") {
			continue
		}
		first, _, _ := strings.Cut(importPath, "/")
		if !strings.Contains(first, ".") || modulePath != "" && (importPath == modulePath || strings.HasPrefix(importPath, modulePath+"/")) {
			continue
		}
		best := ""
		for required := range requirements {
			if (importPath == required || strings.HasPrefix(importPath, required+"/")) && len(required) > len(best) {
				best = required
			}
		}
		if best != "" {
			p.Origin.Modules[i] = "pkg:go:" + best
		}
	}
	if p.UniversalAST != nil {
		p.UniversalAST.Origin.Modules = append([]string(nil), p.Origin.Modules...)
	}
}

func readGoModuleRequirements(baseDir string) (string, map[string]bool) {
	requirements := map[string]bool{}
	dir, err := filepath.Abs(baseDir)
	if err != nil {
		return "", requirements
	}
	for {
		data, readErr := os.ReadFile(filepath.Join(dir, "go.mod"))
		if readErr == nil {
			modulePath := ""
			inRequire := false
			for _, raw := range strings.Split(string(data), "\n") {
				line := strings.TrimSpace(strings.SplitN(raw, "//", 2)[0])
				if line == "" {
					continue
				}
				if strings.HasPrefix(line, "module ") {
					modulePath = strings.Trim(strings.TrimSpace(strings.TrimPrefix(line, "module ")), "\"")
					continue
				}
				if line == "require (" || line == "require(" {
					inRequire = true
					continue
				}
				if inRequire && line == ")" {
					inRequire = false
					continue
				}
				if strings.HasPrefix(line, "require ") {
					line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
				}
				if !inRequire && !strings.Contains(line, " ") {
					continue
				}
				fields := strings.Fields(line)
				if len(fields) >= 2 && !strings.HasPrefix(fields[0], "//") {
					requirements[fields[0]] = true
				}
			}
			return modulePath, requirements
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return "", requirements
}

// goModulePathInTree reads the module identity from the imported package tree.
// Registry archives commonly have a versioned wrapper directory above go.mod,
// so looking only at root/go.mod misses the actual module and can accidentally
// inherit the importing compiler's parent go.mod.
func goModulePathInTree(root string) string {
	moduleDir := goModuleDirInTree(root)
	if moduleDir == "" {
		return ""
	}
	modulePath, _ := readGoModuleRequirements(moduleDir)
	return modulePath
}

func goModuleDirInTree(root string) string {
	root, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	if _, statErr := os.Stat(filepath.Join(root, "go.mod")); statErr == nil {
		return root
	}
	bestDir := ""
	bestDepth := int(^uint(0) >> 1)
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if entry.IsDir() {
			if path != root && (entry.Name() == ".git" || entry.Name() == "vendor" || entry.Name() == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.Name() != "go.mod" {
			return nil
		}
		rel, relErr := filepath.Rel(root, filepath.Dir(path))
		if relErr != nil {
			return nil
		}
		depth := 0
		if rel != "." {
			depth = strings.Count(rel, string(filepath.Separator)) + 1
		}
		if depth < bestDepth {
			bestDepth = depth
			bestDir = filepath.Dir(path)
		}
		return nil
	})
	if bestDir == "" {
		return ""
	}
	return bestDir
}

func readGoModuleRequirementVersions(goModPath string) map[string]string {
	requirements := map[string]string{}
	data, err := os.ReadFile(goModPath)
	if err != nil {
		return requirements
	}
	inRequire := false
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "//", 2)[0])
		if line == "" || strings.HasPrefix(line, "module ") {
			continue
		}
		if line == "require (" || line == "require(" {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}
		if strings.HasPrefix(line, "require ") {
			line = strings.TrimSpace(strings.TrimPrefix(line, "require "))
		}
		if !inRequire && !strings.Contains(line, " ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && !strings.HasPrefix(fields[0], "//") {
			requirements[fields[0]] = fields[1]
		}
	}
	return requirements
}

func goModuleRequirementForImport(importPath string, requirements map[string]string) (string, string, bool) {
	bestModule := ""
	bestVersion := ""
	for modulePath, version := range requirements {
		if version == "" || !dependencyBelongsToGoModule(importPath, modulePath) {
			continue
		}
		if len(modulePath) > len(bestModule) {
			bestModule = modulePath
			bestVersion = version
		}
	}
	return bestModule, bestVersion, bestModule != ""
}

func goModuleProxyZipURL(modulePath, version string) string {
	path := strings.ReplaceAll(modulePath, "/", "%2F")
	return "https://proxy.golang.org/" + path + "/@v/" + url.PathEscape(version) + ".zip"
}

func splitGoModuleVersion(name string) (string, string) {
	index := strings.LastIndex(name, "@")
	if index <= strings.LastIndex(name, "/") || index == len(name)-1 {
		return name, ""
	}
	return name[:index], name[index+1:]
}

type packageFileAnalysis struct {
	file     SemanticPackageFile
	semantic []byte
	program  *SemanticProgram
	err      error
}

func analyzePackageSource(root, path string, opts ModuleImportOptions) packageFileAnalysis {
	rel, _ := filepath.Rel(root, path)
	rel = filepath.ToSlash(rel)
	b, err := os.ReadFile(path)
	if err != nil {
		return packageFileAnalysis{err: err}
	}
	sum := sha256.Sum256(b)
	f := SemanticPackageFile{Path: rel, Status: "UNSUPPORTED", SourceHash: hex.EncodeToString(sum[:])}
	kind := DetectArtifact(path)
	f.Language, f.Kind = kind.Language, string(kind.Kind)
	if opts.Language != "" && packagePathMatchesLanguage(path, opts.Language) {
		f.Language = NormalizeLanguage(opts.Language)
	}
	var p *SemanticProgram
	var pe error
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".se":
		p, pe = ParseSemanticSE(b)
	case ".spz":
		p, pe = ParseSemanticSPZ(b)
	case ".sp":
		p, pe = ParseSemanticSP(b)
	case ".json":
		p, pe = ParseSemanticJSON(b)
	case ".asm", ".s":
		p, pe = LiftBinaryInput(b, CompileOptions{InputKind: CompileInputAssembly, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	case ".obj":
		p, pe = LiftBinaryInput(b, CompileOptions{InputKind: CompileInputObject, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	case ".exe", ".dll":
		p, pe = LiftBinaryInput(b, CompileOptions{InputKind: CompileInputExecutable, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	default:
		if f.Language != "" && !isPackageDocumentation(path) {
			p, pe = LowerSource(f.Language, path, string(b))
		}
	}
	if pe != nil && (ext == ".dll" || ext == ".exe" || ext == ".obj") {
		p, pe = opaqueNativeExternalProgram(rel, b, pe)
	}
	if pe != nil || p == nil {
		return packageFileAnalysis{file: f}
	}
	f.Symbols = semanticProgramSymbolIdentities(p)
	spz, err := p.MarshalSemanticSPZ()
	if err != nil {
		f.Status = "SEMANTIC_SERIALIZE_FAILED"
		return packageFileAnalysis{file: f}
	}
	f.Status = "SEMANTIC_READY"
	f.SemanticPath = filepath.ToSlash(filepath.Join("semantic", rel+".spz"))
	f.ReadableSemanticPath = filepath.ToSlash(filepath.Join("semantic", rel+".se"))
	for _, dep := range p.Origin.Modules {
		if dep != "" {
			f.Dependencies = append(f.Dependencies, dep)
		}
	}
	return packageFileAnalysis{file: f, semantic: spz, program: p}
}

func packagePathMatchesLanguage(path, language string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	switch NormalizeLanguage(language) {
	case "go":
		return ext == ".go"
	case "python":
		return ext == ".py" || ext == ".pyi" || ext == ".pyx"
	case "rust":
		return ext == ".rs"
	case "r":
		return ext == ".r"
	case "julia":
		return ext == ".jl"
	case "c":
		return ext == ".c" || ext == ".h"
	case "cpp":
		return ext == ".cpp" || ext == ".cc" || ext == ".cxx" || ext == ".hpp" || ext == ".hh" || ext == ".hxx" || ext == ".h"
	case "csharp":
		return ext == ".cs"
	case "java":
		return ext == ".java"
	case "kotlin":
		return ext == ".kt" || ext == ".kts"
	case "nim":
		return ext == ".nim" || ext == ".nims"
	case "swift":
		return ext == ".swift"
	case "zig":
		return ext == ".zig"
	default:
		return false
	}
}

// ImportPackage imports a directory, ZIP, or HTTP(S) archive into the module
// store and emits one .spz per source/binary unit plus a package .smod index.
func (r UniversalModuleResolver) ImportPackage(source string, opts ModuleImportOptions) (*SemanticPackageManifest, error) {
	// Registry adapters use the standard HTTP client. Give every package
	// acquisition a bounded deadline so an unavailable registry cannot leave a
	// CLI/API import blocked indefinitely.
	if http.DefaultClient.Timeout == 0 {
		http.DefaultClient.Timeout = 30 * time.Second
	}
	return r.importPackageDepth(source, opts, map[string]bool{}, 0)
}

// existingPackageManifest performs the durable-store lookup before any name
// resolution, archive extraction, or network access.  Package imports are
// content already present in the Semantic module store first; a download is
// only a cache miss.  The lookup intentionally accepts the common archive and
// registry spellings but never scans source files or invents a manifest.
func (r UniversalModuleResolver) existingPackageManifest(source string) (*SemanticPackageManifest, bool) {
	if strings.TrimSpace(r.Store.Root) == "" {
		return nil, false
	}
	name := shortPackageName(source, filepath.Base(source))
	for _, suffix := range []string{".tar.gz", ".tgz", ".whl", ".zip", ".git"} {
		name = strings.TrimSuffix(name, suffix)
	}
	name = SafeModuleName(name)
	if name == "" {
		return nil, false
	}
	root := filepath.Join(r.Store.Root, name)
	entries, err := os.ReadDir(root)
	if err != nil {
		// Older package imports may be content-addressed below `packages/`.
		// Search only manifest filenames and stop at the first matching package;
		// source trees are never scanned or parsed here.
		var found *SemanticPackageManifest
		_ = filepath.WalkDir(r.Store.Root, func(path string, d os.DirEntry, walkErr error) error {
			if walkErr != nil || found != nil {
				return filepath.SkipDir
			}
			if d.IsDir() || strings.ToLower(filepath.Ext(d.Name())) != ".smod" {
				return nil
			}
			b, readErr := os.ReadFile(path)
			if readErr != nil {
				return nil
			}
			var candidate SemanticPackageManifest
			if json.Unmarshal(b, &candidate) != nil || len(candidate.Files) == 0 {
				return nil
			}
			candidateName := candidate.Name
			if candidateName == "" {
				candidateName = strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
			}
			if SafeModuleName(candidateName) != name {
				return nil
			}
			candidate.Name = candidateName
			candidate.Root = filepath.Dir(path)
			found = &candidate
			return nil
		})
		if found == nil {
			return nil, false
		}
		for _, file := range found.Files {
			if file.Status == "SEMANTIC_READY" && file.SemanticPath != "" {
				if _, statErr := os.Stat(filepath.Join(found.Root, filepath.FromSlash(file.SemanticPath))); statErr != nil {
					return nil, false
				}
			}
		}
		return found, true
	}
	for _, entry := range entries {
		if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".smod" {
			continue
		}
		b, readErr := os.ReadFile(filepath.Join(root, entry.Name()))
		if readErr != nil {
			continue
		}
		var manifest SemanticPackageManifest
		if json.Unmarshal(b, &manifest) != nil || len(manifest.Files) == 0 {
			continue
		}
		// Older durable manifests predate the explicit name/root fields. Their
		// directory and filename are still a stable package identity, so recover
		// only those structural fields without inspecting source or diagnostics.
		if strings.TrimSpace(manifest.Name) == "" {
			manifest.Name = strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		}
		// A manifest is reusable only while all semantic payloads it promises
		// still exist.  This prevents a partial/manual store entry from masking
		// a real package acquisition.
		complete := true
		for _, file := range manifest.Files {
			if file.Status != "SEMANTIC_READY" || file.SemanticPath == "" {
				continue
			}
			if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(file.SemanticPath))); statErr != nil {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		manifest.Root = root
		return &manifest, true
	}
	return nil, false
}

func persistPackageAnalysis(out, sourceRoot string, analysis packageFileAnalysis) error {
	f := analysis.file
	src := filepath.Join(sourceRoot, filepath.FromSlash(f.Path))
	dst := filepath.Join(out, "source", filepath.FromSlash(f.Path))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := copyFile(src, dst); err != nil {
		return err
	}
	if f.Status != "SEMANTIC_READY" || len(analysis.semantic) == 0 {
		return nil
	}
	dst = filepath.Join(out, filepath.FromSlash(f.SemanticPath))
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if err := os.WriteFile(dst, analysis.semantic, 0644); err != nil {
		return err
	}
	if analysis.program != nil {
		readable, err := analysis.program.MarshalSemanticSEReadable()
		if err != nil {
			return err
		}
		readablePath := f.ReadableSemanticPath
		if readablePath == "" {
			readablePath = strings.TrimSuffix(f.SemanticPath, ".spz") + ".se"
		}
		dst = filepath.Join(out, filepath.FromSlash(readablePath))
		if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(dst, readable, 0644); err != nil {
			return err
		}
	}
	return nil
}

func (r UniversalModuleResolver) importPackageDepth(source string, opts ModuleImportOptions, seen map[string]bool, depth int) (*SemanticPackageManifest, error) {
	originalSource := source
	key := strings.ToLower(strings.TrimSpace(source)) + "|" + strings.ToLower(opts.Language)
	if seen[key] {
		return nil, nil
	}
	seen[key] = true
	if cached, ok := r.existingPackageManifest(source); ok {
		if err := r.ensurePackageDependencies(cached, opts, seen, depth); err != nil {
			return nil, err
		}
		return cached, nil
	}
	if resolved, ok, e := resolveNamedPackage(source, opts.Language); e != nil {
		return nil, e
	} else if ok {
		source = resolved
	}
	// Go standard-library imports are resolved locally from GOROOT before any
	// URL/module download is attempted. They are ordinary source packages and
	// therefore enter the same semantic module store as project modules.
	if NormalizeLanguage(opts.Language) == "go" && !filepath.IsAbs(source) && !strings.Contains(source, "://") {
		stdRoot := filepath.Join(runtime.GOROOT(), "src", filepath.FromSlash(source))
		if st, statErr := os.Stat(stdRoot); statErr == nil && st.IsDir() {
			source = stdRoot
		}
	}
	root, cleanup, err := packageRoot(source)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	// Registry-resolved ports and extracted archives are transient even when
	// packageRoot received them as directories. Remove them after the manifest
	// and semantic units have been copied to the durable module store.
	if downloadRoot, e1 := filepath.Abs(semanticDownloadDir()); e1 == nil {
		if resolvedRoot, e2 := filepath.Abs(root); e2 == nil {
			prefix := downloadRoot + string(filepath.Separator)
			if strings.HasPrefix(resolvedRoot, prefix) {
				defer os.RemoveAll(resolvedRoot)
			}
		}
	}
	if err := expandNestedArchives(root, 3); err != nil {
		return nil, err
	}
	name := shortPackageName(originalSource, filepath.Base(root))
	if name == "." || name == string(filepath.Separator) || name == "" {
		name = "package"
	}
	dataHash := sha256.New()
	manifest := &SemanticPackageManifest{SchemaVersion: 1, Name: name, Source: source, Root: root}
	if NormalizeLanguage(opts.Language) == "go" {
		moduleDir := goModuleDirInTree(root)
		if moduleDir != "" {
			manifest.ModulePath, _ = readGoModuleRequirements(moduleDir)
			manifest.GoRequirements = readGoModuleRequirementVersions(filepath.Join(moduleDir, "go.mod"))
		}
	}
	var files []string
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if d.IsDir() {
			return nil
		}
		if NormalizeLanguage(opts.Language) == "go" {
			name := d.Name()
			// A Go semantic module contains production sources only. Tests,
			// examples and package metadata must not make an otherwise valid
			// standard-library package appear non-linkable.
			if filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") || strings.HasPrefix(name, ".") {
				return nil
			}
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if isLicenseName(filepath.Base(path)) {
			manifest.Licenses = append(manifest.Licenses, rel)
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	storeRoot := r.Store.Root
	if storeRoot == "" {
		return nil, fmt.Errorf("module store is not configured")
	}
	out := filepath.Join(storeRoot, SafeModuleName(name))
	if err = os.RemoveAll(out); err != nil {
		return nil, err
	}
	if err = os.MkdirAll(out, 0755); err != nil {
		return nil, err
	}
	analyses := make([]packageFileAnalysis, len(files))
	jobs := make(chan int)
	var wg sync.WaitGroup
	workers := packageWorkerCount()
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				analysis := analyzePackageSource(root, files[index], opts)
				analyses[index] = analysis
				if analysis.err == nil {
					if writeErr := persistPackageAnalysis(out, root, analysis); writeErr != nil {
						analyses[index].err = writeErr
					}
				}
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for _, analysis := range analyses {
		if analysis.err != nil {
			return nil, analysis.err
		}
		manifest.Files = append(manifest.Files, analysis.file)
		_, _ = dataHash.Write([]byte(analysis.file.Path))
		sum, _ := hex.DecodeString(analysis.file.SourceHash)
		_, _ = dataHash.Write(sum)
	}
	manifest.PackageHash = hex.EncodeToString(dataHash.Sum(nil))
	depSet := map[string]bool{}
	for _, f := range manifest.Files {
		for _, dep := range f.Dependencies {
			if dep != "" && !depSet[dep] {
				depSet[dep] = true
				manifest.Dependencies = append(manifest.Dependencies, dep)
			}
		}
	}
	sort.Strings(manifest.Dependencies)
	for i, f := range manifest.Files {
		src := filepath.Join(root, filepath.FromSlash(f.Path))
		dst := filepath.Join(out, "source", filepath.FromSlash(f.Path))
		if err = os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return nil, err
		}
		if err = copyFile(src, dst); err != nil {
			return nil, err
		}
		if f.Status != "SEMANTIC_READY" || len(analyses[i].semantic) == 0 {
			continue
		}
		dst = filepath.Join(out, filepath.FromSlash(f.SemanticPath))
		if err = os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			return nil, err
		}
		if err = os.WriteFile(dst, analyses[i].semantic, 0644); err != nil {
			return nil, err
		}
		// Keep a human-readable SE projection beside the compact SPZ payload.
		// Both files are generated from the same canonical SemanticProgram; the
		// readable form is never reconstructed from diagnostics or source text.
		if analyses[i].program != nil {
			readable, marshalErr := analyses[i].program.MarshalSemanticSEReadable()
			if marshalErr != nil {
				return nil, marshalErr
			}
			readablePath := f.ReadableSemanticPath
			if readablePath == "" {
				readablePath = strings.TrimSuffix(f.SemanticPath, ".spz") + ".se"
			}
			readableDst := filepath.Join(out, filepath.FromSlash(readablePath))
			if err = os.MkdirAll(filepath.Dir(readableDst), 0755); err != nil {
				return nil, err
			}
			if err = os.WriteFile(readableDst, readable, 0644); err != nil {
				return nil, err
			}
		}
	}
	for _, rel := range manifest.Licenses {
		src := filepath.Join(root, filepath.FromSlash(rel))
		dst := filepath.Join(out, "licenses", filepath.FromSlash(rel))
		_ = os.MkdirAll(filepath.Dir(dst), 0755)
		_ = copyFile(src, dst)
	}
	manifest.Root = out
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	if err = os.WriteFile(filepath.Join(out, name+".smod"), mb, 0644); err != nil {
		return nil, err
	}
	// Resolve declared module/package dependencies through the same registry
	// adapters. A package manifest is not complete until every declared
	// dependency is durably available in the same store. Do not silently leave
	// a transitive dependency out of the store: later embedding/linking would
	// otherwise observe a partial graph and fail far away from the import site.
	if err := r.ensurePackageDependencies(manifest, opts, seen, depth); err != nil {
		return nil, err
	}
	return manifest, nil
}

func (r UniversalModuleResolver) ensurePackageDependencies(manifest *SemanticPackageManifest, opts ModuleImportOptions, seen map[string]bool, depth int) error {
	if manifest == nil {
		return nil
	}
	for _, dep := range manifest.Dependencies {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		// A Go import below the package's declared module path is another
		// package in the same module, not a separately downloadable module.
		// Keep this path generic: module layouts and package names vary, while
		// the go.mod module path is the authoritative boundary.
		if dependencyBelongsToGoModule(dep, manifest.ModulePath) {
			continue
		}
		// Go standard-library packages (for example bytes, fmt, and math) are
		// provided by the active toolchain and are not modules published through
		// proxy.golang.org.  Treat them as satisfied dependencies; attempting to
		// fetch them via the module proxy produces a misleading 404 and prevents
		// otherwise valid external package imports from being embedded.
		if isGoStandardLibraryPath(dep) {
			continue
		}
		if isGoNonRuntimePackage(dep) {
			continue
		}
		// Package imports below a module root (notably gioui.org/app) are
		// provided by that root module and are not independently downloadable
		// module paths. The root module is imported once and covers them.
		if strings.HasPrefix(dep, "gioui.org/") || strings.HasPrefix(dep, "github.com/go-text/typesetting/") || strings.HasPrefix(dep, "github.com/oligo/gvcode/") || strings.HasPrefix(dep, "github.com/rdleal/intervalst/") {
			continue
		}
		dependencySource := dep
		if NormalizeLanguage(opts.Language) == "go" {
			if modulePath, version, ok := goModuleRequirementForImport(dep, manifest.GoRequirements); ok {
				dependencySource = modulePath + "@" + version
			}
		}
		if _, err := r.importPackageDepth(dependencySource, opts, seen, depth+1); err != nil {
			return fmt.Errorf("transitive dependency %q of %q: %w", dep, manifest.Name, err)
		}
	}
	return nil
}

func dependencyBelongsToGoModule(dependency, modulePath string) bool {
	dependency = strings.Trim(strings.TrimSpace(dependency), "/")
	modulePath = strings.Trim(strings.TrimSpace(modulePath), "/")
	if dependency == "" || modulePath == "" {
		return false
	}
	return dependency == modulePath || strings.HasPrefix(dependency, modulePath+"/")
}

func isGoStandardLibraryPath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	first := path
	if i := strings.IndexByte(first, '/'); i >= 0 {
		first = first[:i]
	}
	return !strings.Contains(first, ".")
}

func isGoNonRuntimePackage(path string) bool {
	path = strings.Trim(path, "/")
	for _, part := range strings.Split(path, "/") {
		if part == "example" || part == "examples" || part == "testdata" {
			return true
		}
	}
	return strings.HasSuffix(path, "_test")
}

// SafeModuleName keeps the durable store readable and avoids exposing hashes
// or registry URL punctuation in module paths.
func SafeModuleName(name string) string {
	name = strings.TrimSpace(name)
	name = strings.Trim(name, ".")
	name = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			return r
		}
		return '-'
	}, name)
	name = strings.Trim(name, "-_")
	if name == "" {
		return "module"
	}
	return name
}

func shortPackageName(source, fallback string) string {
	// Normalize Windows paths before registry/name extraction.  A local path
	// must never become the package identity (and therefore a deeply nested
	// module directory) merely because it uses backslashes.
	trimmed := strings.TrimSpace(source)
	// Keep URLs intact; only normalize filesystem paths.
	if !strings.Contains(trimmed, "://") {
		normalized := strings.ReplaceAll(trimmed, "\\", "/")
		if normalized != "" {
			if base := filepath.Base(normalized); base != "." && base != "/" && base != "" {
				source = base
			}
		}
	}
	if u, err := url.Parse(source); err == nil && u.Host != "" {
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if len(parts) > 0 && parts[len(parts)-1] != "" {
			return strings.TrimSuffix(parts[len(parts)-1], ".git")
		}
	}
	if strings.Contains(source, "/") {
		p := strings.Split(strings.Trim(source, "/"), "/")
		return p[len(p)-1]
	}
	if strings.Contains(source, ":") {
		p := strings.Split(source, ":")
		return p[len(p)-1]
	}
	if strings.TrimSpace(source) != "" {
		name := source
		for _, suffix := range []string{".tar.gz", ".tgz", ".whl", ".zip", ".git"} {
			name = strings.TrimSuffix(name, suffix)
		}
		return name
	}
	return fallback
}

// Package documentation and metadata are preserved verbatim but are never
// treated as source programs.  This prevents a package-wide --language hint
// from sending README/text/license files through a language frontend.
func isPackageDocumentation(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt", ".md", ".markdown", ".rst", ".adoc", ".csv", ".tsv", ".log":
		return true
	default:
		return isLicenseName(filepath.Base(path))
	}
}

// expandNestedArchives unpacks wheels and nested source archives in-place so
// package structure is available to the same semantic import walk.
func expandNestedArchives(root string, maxDepth int) error {
	if maxDepth <= 0 {
		return nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	for _, e := range entries {
		p := filepath.Join(root, e.Name())
		if e.IsDir() {
			if err := expandNestedArchives(p, maxDepth-1); err != nil {
				return err
			}
			continue
		}
		lower := strings.ToLower(e.Name())
		if !(strings.HasSuffix(lower, ".zip") || strings.HasSuffix(lower, ".whl") || strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz")) {
			continue
		}
		dir, release, er := packageRoot(p)
		if er != nil {
			continue
		}
		dst := strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(p, ".tar.gz"), ".whl"), ".zip") + ".expanded"
		if er = copyTree(dir, dst); er != nil {
			release()
			return er
		}
		release()
		if er = expandNestedArchives(dst, maxDepth-1); er != nil {
			return er
		}
	}
	return nil
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		out := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(out, 0755)
		}
		if err := os.MkdirAll(filepath.Dir(out), 0755); err != nil {
			return err
		}
		return copyFile(path, out)
	})
}

// resolveNamedPackage maps registry names to immutable source archives.  The
// package importer remains language agnostic; this adapter only discovers the
// archive URL and leaves extraction/lifting to the common pipeline.
func resolveNamedPackage(name, language string) (string, bool, error) {
	// Local directories and archives are already resolved inputs.  Check them
	// before registry adapters so a path such as C:\\work\\demo.zip is never
	// interpreted as a Go module name and sent to proxy.golang.org.
	if strings.TrimSpace(name) != "" {
		if _, err := os.Stat(name); err == nil {
			return name, true, nil
		}
	}
	lang := strings.ToLower(NormalizeLanguage(language))
	if lang == "c" || lang == "cpp" || lang == "c++" {
		if root := os.Getenv("VCPKG_ROOT"); root != "" {
			port := filepath.Join(root, "ports", name)
			if st, err := os.Stat(port); err == nil && st.IsDir() {
				return port, true, nil
			}
		}
		// Fall back to the public vcpkg port registry.  This downloads the
		// recipe files into the transient Semantic/download area; the common
		// importer then preserves them and any referenced source metadata.
		if port, err := resolveVcpkgPort(name); err == nil {
			return port, true, nil
		}
		return name, false, nil
	}
	if strings.TrimSpace(name) == "" || strings.HasPrefix(name, ".") || (strings.ContainsAny(name, `/\\:`) && lang != "go" && lang != "node" && lang != "javascript" && lang != "typescript" && lang != "java" && lang != "kotlin" && lang != "nim" && lang != "swift") {
		return name, false, nil
	}
	switch lang {
	case "r":
		resp, err := http.Get("https://crandb.r-pkg.org/" + url.PathEscape(name) + "/latest")
		if err != nil {
			return "", false, fmt.Errorf("cran lookup: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			// CRAN mirrors may omit the crandb endpoint. Fall back to the
			// authoritative PACKAGES index and select its source tarball.
			resp.Body.Close()
			b, fe := fetchBytes("https://cran.r-project.org/src/contrib/PACKAGES")
			if fe != nil {
				return "", false, fmt.Errorf("cran lookup: %s", resp.Status)
			}
			version := ""
			inPkg := false
			for _, line := range strings.Split(string(b), "\n") {
				t := strings.TrimSpace(line)
				if strings.HasPrefix(t, "Package: ") {
					inPkg = strings.TrimSpace(strings.TrimPrefix(t, "Package: ")) == name
					version = ""
					continue
				}
				if inPkg && strings.HasPrefix(t, "Version: ") {
					version = strings.TrimSpace(strings.TrimPrefix(t, "Version: "))
					break
				}
			}
			if version == "" {
				return "", false, fmt.Errorf("cran package %q not found", name)
			}
			return "https://cran.r-project.org/src/contrib/" + url.PathEscape(name) + "_" + url.PathEscape(version) + ".tar.gz", true, nil
		}
		var meta struct {
			Version string `json:"Version"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
			return "", false, fmt.Errorf("cran metadata: %w", err)
		}
		if meta.Version == "" {
			return "", false, fmt.Errorf("cran package %q has no release", name)
		}
		return "https://cran.r-project.org/src/contrib/" + url.PathEscape(name) + "_" + url.PathEscape(meta.Version) + ".tar.gz", true, nil
	case "rust":
		req, _ := http.NewRequest(http.MethodGet, "https://crates.io/api/v1/crates/"+url.PathEscape(name), nil)
		req.Header.Set("User-Agent", "Code-Transpiler/1.3")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", false, fmt.Errorf("crates.io lookup: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", false, fmt.Errorf("crates.io lookup: %s", resp.Status)
		}
		var meta struct {
			Crate struct {
				MaxVersion string `json:"max_version"`
			} `json:"crate"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
			return "", false, fmt.Errorf("crates.io metadata: %w", err)
		}
		if meta.Crate.MaxVersion == "" {
			return "", false, fmt.Errorf("crate %q has no release", name)
		}
		return "https://crates.io/api/v1/crates/" + url.PathEscape(name) + "/" + url.PathEscape(meta.Crate.MaxVersion) + "/download", true, nil
	case "go":
		modulePath, requestedVersion := splitGoModuleVersion(name)
		path := strings.ReplaceAll(modulePath, "/", "%2F")
		if requestedVersion != "" {
			return goModuleProxyZipURL(modulePath, requestedVersion), true, nil
		}
		resp, err := http.Get("https://proxy.golang.org/" + path + "/@latest")
		if err != nil {
			return "", false, fmt.Errorf("go proxy lookup: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", false, fmt.Errorf("go proxy lookup: %s", resp.Status)
		}
		var meta struct {
			Version string `json:"Version"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
			return "", false, fmt.Errorf("go proxy metadata: %w", err)
		}
		if meta.Version == "" {
			return "", false, fmt.Errorf("go module %q has no release", name)
		}
		return goModuleProxyZipURL(modulePath, meta.Version), true, nil
	case "node", "javascript", "typescript":
		resp, err := http.Get("https://registry.npmjs.org/" + strings.TrimPrefix(name, "@"))
		if err != nil {
			return "", false, fmt.Errorf("npm lookup: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", false, fmt.Errorf("npm lookup: %s", resp.Status)
		}
		var meta struct {
			DistTags struct {
				Latest string `json:"latest"`
			} `json:"dist-tags"`
			Versions map[string]struct {
				Dist struct {
					Tarball string `json:"tarball"`
				} `json:"dist"`
			} `json:"versions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
			return "", false, fmt.Errorf("npm metadata: %w", err)
		}
		u := meta.Versions[meta.DistTags.Latest].Dist.Tarball
		if u == "" {
			return "", false, fmt.Errorf("npm package %q has no source archive", name)
		}
		return u, true, nil
	case "csharp", "dotnet":
		id := strings.ToLower(name)
		resp, err := http.Get("https://api.nuget.org/v3-flatcontainer/" + url.PathEscape(id) + "/index.json")
		if err != nil {
			return "", false, fmt.Errorf("nuget lookup: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", false, fmt.Errorf("nuget lookup: %s", resp.Status)
		}
		var meta struct {
			Versions []string `json:"versions"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil || len(meta.Versions) == 0 {
			return "", false, fmt.Errorf("nuget package %q has no release", name)
		}
		v := meta.Versions[len(meta.Versions)-1]
		return "https://api.nuget.org/v3-flatcontainer/" + url.PathEscape(id) + "/" + url.PathEscape(v) + "/" + url.PathEscape(id) + "." + url.PathEscape(v) + ".nupkg", true, nil
	case "java", "kotlin":
		parts := strings.Split(name, ":")
		if len(parts) != 2 {
			return "", false, fmt.Errorf("maven package must be group:artifact")
		}
		q := url.QueryEscape(`g:"` + parts[0] + `" AND a:"` + parts[1] + `"`)
		resp, err := http.Get("https://search.maven.org/solrsearch/select?q=" + q + "&rows=1&wt=json")
		if err != nil {
			return "", false, fmt.Errorf("maven lookup: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", false, fmt.Errorf("maven lookup: %s", resp.Status)
		}
		var meta struct {
			Response struct {
				Docs []struct {
					Latest string `json:"latestVersion"`
				} `json:"docs"`
			} `json:"response"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil || len(meta.Response.Docs) == 0 {
			return "", false, fmt.Errorf("maven artifact %q has no release", name)
		}
		v := meta.Response.Docs[0].Latest
		if v == "" {
			return "", false, fmt.Errorf("maven artifact %q has no version", name)
		}
		groupPath := strings.ReplaceAll(parts[0], ".", "/")
		base := "https://repo1.maven.org/maven2/" + groupPath + "/" + parts[1] + "/" + v + "/" + parts[1] + "-" + v
		return base + "-sources.jar", true, nil
	case "julia":
		return resolveJuliaPackage(name)
	case "python":
		resp, err := http.Get("https://pypi.org/pypi/" + url.PathEscape(name) + "/json")
		if err != nil {
			return "", false, fmt.Errorf("pypi lookup: %w", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", false, fmt.Errorf("pypi lookup: %s", resp.Status)
		}
		var meta struct {
			URLs []struct{ URL, Packagetype string } `json:"urls"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
			return "", false, fmt.Errorf("pypi metadata: %w", err)
		}
		for _, u := range meta.URLs {
			if u.Packagetype == "sdist" && strings.HasSuffix(strings.ToLower(u.URL), ".tar.gz") {
				return u.URL, true, nil
			}
		}
		return "", false, fmt.Errorf("pypi package %q has no source distribution", name)
	case "nim":
		return resolveNimblePackage(name)
	case "swift":
		return resolveSwiftPackage(name)
	default:
		return name, false, nil
	}
}

func resolveNimblePackage(name string) (string, bool, error) {
	// Prefer GitHub's bounded repository search. The historical Nimble
	// packages.json is several hundred megabytes and is not suitable for a
	// package-name lookup in a compiler process.
	q := url.QueryEscape(name + " language:Nim")
	if resp, lookupErr := http.Get("https://api.github.com/search/repositories?q=" + q + "&per_page=1"); lookupErr == nil {
		defer resp.Body.Close()
		if resp.StatusCode/100 == 2 {
			var result struct {
				Items []struct {
					CloneURL string `json:"clone_url"`
					HTMLURL  string `json:"html_url"`
				} `json:"items"`
			}
			if json.NewDecoder(resp.Body).Decode(&result) == nil && len(result.Items) > 0 {
				repo := result.Items[0].CloneURL
				if repo == "" {
					repo = result.Items[0].HTMLURL
				}
				if repo != "" {
					return repo, true, nil
				}
			}
		}
	}
	// Keep the official registry fallback for installations where GitHub
	// search is unavailable; this path is bounded by the caller's HTTP timeout.
	b, err := fetchBytes("https://raw.githubusercontent.com/nim-lang/packages/master/packages.json")
	if err != nil {
		return "", false, fmt.Errorf("nimble registry lookup: %w", err)
	}
	var entries []struct {
		Name string `json:"name"`
		URL  string `json:"url"`
	}
	// The registry has historically used both object and array forms. Decode
	// the common array form first, then scan for the matching repository URL.
	if json.Unmarshal(b, &entries) == nil {
		for _, e := range entries {
			if strings.EqualFold(e.Name, name) && e.URL != "" {
				return e.URL, true, nil
			}
		}
	}
	var raw []map[string]any
	if json.Unmarshal(b, &raw) == nil {
		for _, e := range raw {
			n, _ := e["name"].(string)
			u, _ := e["url"].(string)
			if strings.EqualFold(n, name) && u != "" {
				return u, true, nil
			}
		}
	}
	return "", false, fmt.Errorf("nimble package %q not found", name)
}

func resolveSwiftPackage(name string) (string, bool, error) {
	// Swift Package Index accepts repository names and returns the canonical
	// source repository; packageRoot then downloads the GitHub source archive.
	name = strings.Trim(strings.TrimSpace(name), "/")
	if !strings.Contains(name, "/") {
		return "", false, fmt.Errorf("swift package %q must use owner/repository", name)
	}
	resp, err := http.Get("https://swiftpackageindex.com/api/packages/" + name)
	if err != nil {
		return "https://github.com/" + name + "/archive/refs/heads/main.zip", true, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		// The index API can require authentication or lag behind GitHub. An
		// owner/repository name is itself sufficient source evidence, so use the
		// canonical GitHub source archive as a registry-neutral fallback.
		return "https://github.com/" + name + "/archive/refs/heads/main.zip", true, nil
	}
	var meta struct {
		RepositoryURL string `json:"repositoryURL"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil || meta.RepositoryURL == "" {
		return "", false, fmt.Errorf("swift package %q has no repository", name)
	}
	return meta.RepositoryURL, true, nil
}

func resolveVcpkgPort(name string) (string, error) {
	base := "https://api.github.com/repos/microsoft/vcpkg/contents/ports/" + url.PathEscape(name)
	dst := filepath.Join(semanticDownloadDir(), "vcpkg-"+strings.ReplaceAll(name, "/", "_"))
	if err := os.RemoveAll(dst); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dst, 0755); err != nil {
		return "", err
	}
	var fetch func(string, string) error
	fetch = func(api, local string) error {
		req, err := http.NewRequest(http.MethodGet, api, nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", "Code-Transpiler/1.3")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return fmt.Errorf("vcpkg registry: %s", resp.Status)
		}
		var entries []struct {
			Type        string `json:"type"`
			Name        string `json:"name"`
			DownloadURL string `json:"download_url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
			return err
		}
		for _, e := range entries {
			p := filepath.Join(local, e.Name)
			if e.Type == "dir" {
				if err := os.MkdirAll(p, 0755); err != nil {
					return err
				}
				if err := fetch(api+"/"+url.PathEscape(e.Name), p); err != nil {
					return err
				}
			} else if e.DownloadURL != "" {
				b, err := fetchBytes(e.DownloadURL)
				if err != nil {
					return err
				}
				if err := os.WriteFile(p, b, 0644); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := fetch(base, dst); err != nil {
		_ = os.RemoveAll(dst)
		return "", err
	}
	return dst, nil
}

func resolveJuliaPackage(name string) (string, bool, error) {
	b, err := fetchBytes("https://raw.githubusercontent.com/JuliaRegistries/General/master/Registry.toml")
	if err != nil {
		return "", false, fmt.Errorf("julia registry lookup: %w", err)
	}
	var path string
	lines := strings.Split(string(b), "\n")
	for _, line := range lines {
		t := strings.TrimSpace(line)
		if !strings.Contains(t, "name = \"") {
			continue
		}
		np := strings.Index(t, "name = \"") + 8
		ne := strings.Index(t[np:], "\"")
		if ne < 0 || t[np:np+ne] != name {
			continue
		}
		pp := strings.Index(t, "path = \"")
		if pp >= 0 {
			pp += len("path = \"")
			if pe := strings.Index(t[pp:], "\""); pe >= 0 {
				path = t[pp : pp+pe]
			}
		}
		break
	}
	if path == "" {
		return "", false, fmt.Errorf("julia package %q not found in General", name)
	}
	pb, err := fetchBytes("https://raw.githubusercontent.com/JuliaRegistries/General/master/" + path + "/Package.toml")
	if err != nil {
		return "", false, fmt.Errorf("julia package metadata: %w", err)
	}
	repo := ""
	for _, line := range strings.Split(string(pb), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "repo = ") {
			repo = strings.Trim(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "repo = ")), "\"")
			break
		}
	}
	if repo == "" {
		return "", false, fmt.Errorf("julia package %q has no repository", name)
	}
	vb, err := fetchBytes("https://raw.githubusercontent.com/JuliaRegistries/General/master/" + path + "/Versions.toml")
	if err != nil {
		return "", false, fmt.Errorf("julia versions: %w", err)
	}
	version := ""
	for _, line := range strings.Split(string(vb), "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
			version = strings.Trim(t, "[]\"")
		}
	}
	if version == "" {
		return "", false, fmt.Errorf("julia package %q has no version", name)
	}
	u, e := url.Parse(repo)
	if e != nil || u.Host == "" {
		return "", false, fmt.Errorf("julia repository %q invalid", repo)
	}
	return strings.TrimSuffix(repo, ".git") + "/archive/refs/tags/v" + version + ".zip", true, nil
}

func fetchBytes(endpoint string) ([]byte, error) {
	resp, err := http.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("%s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

// semanticDownloadDir is the transient package acquisition area.  Keeping it
// below the configured Semantic base makes downloads visible and keeps the
// acquisition and durable module stores on the same user-selected volume.
func semanticDownloadDir() string {
	root := semanticBaseRoot()
	return filepath.Join(root, "Semantic", "download")
}

func packageRoot(source string) (string, func(), error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		// A repository URL is a package target, not itself an archive. Resolve
		// canonical GitHub repository links to a source archive before the
		// normal ZIP importer. Direct .zip URLs continue to work unchanged.
		download := source
		if u, e := url.Parse(source); e == nil && strings.EqualFold(u.Host, "github.com") {
			parts := strings.Split(strings.Trim(u.Path, "/"), "/")
			if len(parts) >= 2 && !strings.HasSuffix(strings.ToLower(u.Path), ".zip") {
				parts[1] = strings.TrimSuffix(parts[1], ".git")
				download = fmt.Sprintf("https://github.com/%s/%s/archive/refs/heads/main.zip", parts[0], parts[1])
			}
		}
		req, reqErr := http.NewRequest(http.MethodGet, download, nil)
		if reqErr != nil {
			return "", func() {}, reqErr
		}
		req.Header.Set("User-Agent", "Code-Transpiler/1.3")
		resp, err := http.DefaultClient.Do(req)
		if (err != nil || resp.StatusCode/100 != 2) && download != source {
			if resp != nil {
				resp.Body.Close()
			}
			download = strings.Replace(download, "/heads/main.zip", "/heads/master.zip", 1)
			req, reqErr = http.NewRequest(http.MethodGet, download, nil)
			if reqErr != nil {
				return "", func() {}, reqErr
			}
			req.Header.Set("User-Agent", "Code-Transpiler/1.3")
			resp, err = http.DefaultClient.Do(req)
		}
		if err != nil {
			return "", func() {}, err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return "", func() {}, fmt.Errorf("package download: %s", resp.Status)
		}
		suffix := ".zip"
		ld := strings.ToLower(download)
		if strings.HasSuffix(ld, ".tar.gz") || strings.HasSuffix(ld, ".tgz") || strings.Contains(ld, "crates.io/api/v1/crates/") || strings.Contains(ld, "registry.npmjs.org/") {
			suffix = ".tar.gz"
		}
		downloadDir := semanticDownloadDir()
		if err := os.MkdirAll(downloadDir, 0755); err != nil {
			return "", func() {}, err
		}
		t, err := os.CreateTemp(downloadDir, "package-*"+suffix)
		if err != nil {
			return "", func() {}, err
		}
		if _, err = io.Copy(t, resp.Body); err != nil {
			t.Close()
			return "", func() {}, err
		}
		t.Close()
		root, release, e := packageRoot(t.Name())
		return root, func() { release(); _ = os.Remove(t.Name()) }, e
	}
	st, err := os.Stat(source)
	if err != nil {
		return "", func() {}, err
	}
	if st.IsDir() {
		return source, func() {}, nil
	}
	lower := strings.ToLower(source)
	if !strings.HasSuffix(lower, ".zip") && !strings.HasSuffix(lower, ".whl") && !strings.HasSuffix(lower, ".jar") && !strings.HasSuffix(lower, ".nupkg") && !strings.HasSuffix(lower, ".tar.gz") && !strings.HasSuffix(lower, ".tgz") {
		return filepath.Dir(source), func() {}, nil
	}
	if err := os.MkdirAll(semanticDownloadDir(), 0755); err != nil {
		return "", func() {}, err
	}
	d, err := os.MkdirTemp(semanticDownloadDir(), "extract-")
	if err != nil {
		return "", func() {}, err
	}
	if strings.HasSuffix(lower, ".tar.gz") || strings.HasSuffix(lower, ".tgz") {
		in, e := os.Open(source)
		if e != nil {
			os.RemoveAll(d)
			return "", func() {}, e
		}
		gz, e := gzip.NewReader(in)
		if e != nil {
			in.Close()
			os.RemoveAll(d)
			return "", func() {}, e
		}
		tr := tar.NewReader(gz)
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				gz.Close()
				in.Close()
				os.RemoveAll(d)
				return "", func() {}, e
			}
			n := filepath.Clean(filepath.FromSlash(h.Name))
			if strings.HasPrefix(n, ".."+string(filepath.Separator)) || filepath.IsAbs(n) {
				gz.Close()
				in.Close()
				os.RemoveAll(d)
				return "", func() {}, fmt.Errorf("unsafe archive path %q", h.Name)
			}
			dst := filepath.Join(d, n)
			if h.FileInfo().IsDir() {
				_ = os.MkdirAll(dst, 0755)
				continue
			}
			if e = os.MkdirAll(filepath.Dir(dst), 0755); e != nil {
				gz.Close()
				in.Close()
				os.RemoveAll(d)
				return "", func() {}, e
			}
			out, e := os.Create(dst)
			if e != nil {
				gz.Close()
				in.Close()
				os.RemoveAll(d)
				return "", func() {}, e
			}
			_, e = io.Copy(out, tr)
			out.Close()
			if e != nil {
				gz.Close()
				in.Close()
				os.RemoveAll(d)
				return "", func() {}, e
			}
		}
		gz.Close()
		in.Close()
		return d, func() { os.RemoveAll(d) }, nil
	}
	zipSource := source
	if strings.HasSuffix(lower, ".whl") || strings.HasSuffix(lower, ".jar") || strings.HasSuffix(lower, ".nupkg") {
		tmp := d + ".whl.zip"
		if err := copyFile(source, tmp); err != nil {
			os.RemoveAll(d)
			return "", func() {}, err
		}
		zipSource = tmp
		defer os.Remove(tmp)
	}
	z, err := zip.OpenReader(zipSource)
	if err != nil {
		os.RemoveAll(d)
		return "", func() {}, err
	}
	for _, f := range z.File {
		n := filepath.Clean(filepath.FromSlash(f.Name))
		if strings.HasPrefix(n, ".."+string(filepath.Separator)) || filepath.IsAbs(n) {
			z.Close()
			os.RemoveAll(d)
			return "", func() {}, fmt.Errorf("unsafe archive path %q", f.Name)
		}
		dst := filepath.Join(d, n)
		if f.FileInfo().IsDir() {
			_ = os.MkdirAll(dst, 0755)
			continue
		}
		if err = os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
			z.Close()
			os.RemoveAll(d)
			return "", func() {}, err
		}
		in, _ := f.Open()
		out, _ := os.Create(dst)
		_, err = io.Copy(out, in)
		in.Close()
		out.Close()
		if err != nil {
			z.Close()
			os.RemoveAll(d)
			return "", func() {}, err
		}
	}
	z.Close()
	return d, func() { _ = os.RemoveAll(d) }, nil
}

func copyFile(src, dst string) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, b, 0644)
}
func isLicenseName(n string) bool {
	n = strings.ToLower(n)
	return strings.Contains(n, "license") || strings.Contains(n, "licence") || strings.Contains(n, "copying") || strings.Contains(n, "notice") || strings.Contains(n, "copyright") || strings.Contains(n, "patent") || n == "unlicense"
}
