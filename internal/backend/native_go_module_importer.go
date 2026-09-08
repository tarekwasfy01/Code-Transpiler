// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"fmt"
	"go/ast"
	"go/importer"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// nativeGoModuleImporter resolves packages belonging to the current module
// directly from their Go source.  The native frontend previously used the
// export-data importer only, which cannot locate sibling module packages in a
// normal module checkout.  This importer is intentionally limited to the
// current module; all other imports keep using the standard Go importer.
type nativeGoModuleImporter struct {
	root       string
	modulePath string
	fallback   types.Importer
	mu         sync.Mutex
	loaded     map[string]*types.Package
	loading    map[string]bool
}

func nativeGoImporterFor(filename string) types.Importer {
	root, modulePath, ok := nativeGoModuleRoot(filename)
	if !ok {
		return nativeGoFmtImporter{delegate: importer.Default()}
	}
	// Retain the portable, structured declaration for fmt.Println in front of
	// the module-aware importer. Virtual source names (the public API's normal
	// mode) must not depend on local GOROOT export data merely to establish the
	// type facts for a supported output builtin.
	// Each type-check operation receives its own importer. go/importer and the
	// gc export-data reader are not safe to call concurrently; sharing one cache
	// across the corpus workers corrupts package scopes and causes fatal
	// "concurrent map writes" panics. Package resolution stays structured and
	// deterministic within this check while the corpus runner provides its own
	// case-level parallelism.
	return nativeGoFmtImporter{delegate: &nativeGoModuleImporter{
		root: root, modulePath: modulePath, fallback: importer.Default(),
		loaded: map[string]*types.Package{}, loading: map[string]bool{},
	}}
}

func nativeGoModuleRoot(filename string) (string, string, bool) {
	path := nativeGoResolveSourcePath(filename)
	for dir := filepath.Dir(path); ; dir = filepath.Dir(dir) {
		goMod := filepath.Join(dir, "go.mod")
		if data, err := os.ReadFile(goMod); err == nil {
			for _, line := range strings.Split(string(data), "\n") {
				fields := strings.Fields(line)
				if len(fields) == 2 && fields[0] == "module" {
					return dir, fields[1], true
				}
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
	}
	return "", "", false
}

func nativeGoResolveSourcePath(filename string) string {
	if filepath.IsAbs(filename) {
		return filepath.Clean(filename)
	}
	if direct, err := filepath.Abs(filename); err == nil {
		if _, statErr := os.Stat(direct); statErr == nil {
			return direct
		}
	}
	if cwd, err := os.Getwd(); err == nil {
		for dir := cwd; ; dir = filepath.Dir(dir) {
			candidate := filepath.Join(dir, filename)
			if _, statErr := os.Stat(candidate); statErr == nil {
				return candidate
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	if abs, err := filepath.Abs(filename); err == nil {
		return abs
	}
	return filename
}

func (i *nativeGoModuleImporter) Import(path string) (*types.Package, error) {
	if path != i.modulePath && !strings.HasPrefix(path, i.modulePath+"/") {
		return i.fallback.Import(path)
	}
	// Source-file export is a structural frontend operation. Recursively
	// type-checking the complete compiler module for every individual file can
	// re-enter the same package graph and turn a valid file into a timeout. Full
	// module checking remains available for explicit project compilation; the
	// ordinary frontend uses a completed package shell and preserves unresolved
	// selectors as structured boundary facts.
	if os.Getenv("CODE_TRANSPILER_FULL_GO_TYPECHECK") != "1" {
		return i.loadShallow(path)
	}
	i.mu.Lock()
	if pkg := i.loaded[path]; pkg != nil {
		i.mu.Unlock()
		return pkg, nil
	}
	if i.loading[path] {
		i.mu.Unlock()
		return nil, fmt.Errorf("native Go module import cycle at %q", path)
	}
	i.loading[path] = true
	i.mu.Unlock()

	pkg, err := i.load(path)
	i.mu.Lock()
	delete(i.loading, path)
	if err == nil {
		i.loaded[path] = pkg
	}
	i.mu.Unlock()
	return pkg, err
}

func (i *nativeGoModuleImporter) loadShallow(path string) (*types.Package, error) {
	rel := strings.TrimPrefix(strings.TrimPrefix(path, i.modulePath), "/")
	dir := filepath.Join(i.root, filepath.FromSlash(rel))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	packageName := filepath.Base(dir)
	fs := gotoken.NewFileSet()
	for _, entry := range entries {
		if entry.IsDir() || strings.HasPrefix(entry.Name(), ".") || filepath.Ext(entry.Name()) != ".go" || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, parseErr := goparser.ParseFile(fs, filepath.Join(dir, entry.Name()), nil, 0)
		if parseErr == nil && file.Name != nil {
			packageName = file.Name.Name
			break
		}
	}
	pkg := types.NewPackage(path, packageName)
	pkg.MarkComplete()
	return pkg, nil
}

func (i *nativeGoModuleImporter) load(path string) (*types.Package, error) {
	rel := strings.TrimPrefix(strings.TrimPrefix(path, i.modulePath), "/")
	dir := filepath.Join(i.root, filepath.FromSlash(rel))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	fs := gotoken.NewFileSet()
	var files []*ast.File
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") || strings.Contains(name, ".transpiled.") {
			continue
		}
		filePath := filepath.Join(dir, name)
		file, parseErr := goparser.ParseFile(fs, filePath, nil, 0)
		if parseErr != nil {
			return nil, parseErr
		}
		if nativeGoFileUsesC(file) {
			// cgo bindings are host implementation detail, not a semantic
			// dependency of source-level type facts. They cannot be loaded by
			// go/types without the cgo toolchain, so exclude their file-local
			// declarations from this portable module importer.
			continue
		}
		files = append(files, file)
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("native Go module import %q has no source files", path)
	}
	info := nativeGoTypeInfo()
	conf := types.Config{Importer: i, Sizes: types.SizesFor("gc", "amd64")}
	pkg, err := conf.Check(path, fs, files, info)
	if err != nil {
		return nil, err
	}
	return pkg, nil
}

func nativeGoFileUsesC(file *ast.File) bool {
	for _, imp := range file.Imports {
		if imp.Path != nil && imp.Path.Value == `"C"` {
			return true
		}
	}
	return false
}
