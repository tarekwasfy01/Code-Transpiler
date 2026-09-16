// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"errors"
	"fmt"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"go/types"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// LowerNativeGoPackage lowers explicitly selected files from one Go package.
// Parsing and go/types checking happen once; each returned SemanticProgram
// still owns only its source file's declarations and body.
func LowerNativeGoPackage(filenames []string) (map[string]*SemanticProgram, error) {
	out := make(map[string]*SemanticProgram, len(filenames))
	var lowerErrors []error
	err := LowerNativeGoPackageEach(filenames, func(filename string, program *SemanticProgram, lowerErr error) error {
		if lowerErr != nil {
			lowerErrors = append(lowerErrors, fmt.Errorf("%s: %w", filename, lowerErr))
			return nil
		}
		out[filename] = program
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(lowerErrors) != 0 {
		return out, errors.Join(lowerErrors...)
	}
	return out, nil
}

// LowerNativeGoPackageEach is the streaming counterpart to
// LowerNativeGoPackage. visit is called as soon as each file has been lowered;
// callers can serialize/write the result and release it before the next file.
func LowerNativeGoPackageEach(filenames []string, visit func(filename string, program *SemanticProgram, lowerErr error) error) error {
	if len(filenames) == 0 {
		return fmt.Errorf("native Go package export requires at least one source file")
	}
	if visit == nil {
		return fmt.Errorf("native Go package export requires a result visitor")
	}
	paths := append([]string(nil), filenames...)
	for i := range paths {
		abs, err := filepath.Abs(paths[i])
		if err != nil {
			return err
		}
		paths[i] = filepath.Clean(abs)
	}
	sort.Strings(paths)
	fs := gotoken.NewFileSet()
	files := make([]*ast.File, 0, len(paths))
	sources := make(map[string]string, len(paths))
	packageName := ""
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		file, err := goparser.ParseFile(fs, path, data, 0)
		if err != nil {
			return err
		}
		if err := validateNativeGoSurface(file); err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if packageName == "" {
			packageName = file.Name.Name
		} else if packageName != file.Name.Name {
			return fmt.Errorf("Go package export contains mixed package names %q and %q", packageName, file.Name.Name)
		}
		files = append(files, file)
		sources[path] = string(data)
	}
	totalBytes := 0
	for _, path := range paths {
		totalBytes += len(sources[path])
	}
	// A large package-wide go/types graph can be many orders of magnitude
	// larger than its source (notably the compiler backend package). In that
	// case use the same fail-closed structural frontend one file at a time and
	// release each SemanticProgram immediately. This preserves source/UAST
	// facts while avoiding an unbounded whole-package graph.
	if totalBytes >= 1_000_000 || os.Getenv("UAST_GO_PACKAGE_STREAMING") == "1" {
		prior := os.Getenv("UAST_GO_SKIP_TYPECHECK")
		_ = os.Setenv("UAST_GO_SKIP_TYPECHECK", "1")
		defer func() {
			if prior == "" {
				_ = os.Unsetenv("UAST_GO_SKIP_TYPECHECK")
			} else {
				_ = os.Setenv("UAST_GO_SKIP_TYPECHECK", prior)
			}
		}()
		for _, path := range paths {
			// Parse and lower this file in an isolated FileSet. Do not call
			// LowerNativeGo here: that convenience path discovers sibling files
			// for package type checking and defeats the streaming memory bound.
			unitSet := gotoken.NewFileSet()
			unitFile, parseErr := goparser.ParseFile(unitSet, path, sources[path], 0)
			var program *SemanticProgram
			lowerErr := parseErr
			if lowerErr == nil {
				lowerErr = validateNativeGoSurface(unitFile)
			}
			if lowerErr == nil {
				var modules []string
				for _, imp := range unitFile.Imports {
					if importPath, unquoteErr := strconv.Unquote(imp.Path.Value); unquoteErr == nil {
						modules = append(modules, importPath)
					}
				}
				program, lowerErr = lowerNativeGoPrepared(path, sources[path], nil, unitSet, unitFile, nativeGoTypeInfo(), []*ast.File{unitFile}, unitFile.Name.Name, modules, true)
			}
			if visitErr := visit(path, program, lowerErr); visitErr != nil {
				return visitErr
			}
			program = nil
		}
		return nil
	}
	info := nativeGoTypeInfo()
	conf := types.Config{Importer: nativeGoImporterFor(paths[0]), Sizes: types.SizesFor("gc", "amd64")}
	// Large compiler packages do not need a second full body walk: the
	// structural lowering below still preserves every function body, while
	// go/types body checking can retain an enormous temporary graph. Keep the
	// declaration/type contracts and make the expensive body phase explicit.
	if totalBytes >= 1_000_000 || os.Getenv("UAST_GO_IGNORE_FUNC_BODIES") == "1" {
		conf.IgnoreFuncBodies = true
	}
	if _, err := conf.Check(packageName, fs, files, info); err != nil && !nativeGoBoundaryTypecheckError(err) {
		return fmt.Errorf("Go package typecheck: %w", err)
	}
	// Lower independent file bodies concurrently.  The callback remains
	// serialized because exporters update shared registries/counters, while
	// the expensive AST-to-semantic work runs in parallel.  The budget follows
	// the project-wide policy: 80%% of CPUs, with a floor of 32 workers, capped
	// by the number of files in this package.
	workers := runtime.NumCPU() * 80 / 100
	if configured := strings.TrimSpace(os.Getenv("SEMANTIC_EXPORT_FILE_WORKERS")); configured != "" {
		if n, parseErr := strconv.Atoi(configured); parseErr == nil && n > 0 {
			workers = n
		}
	}
	if workers < 1 {
		workers = 1
	}
	if workers > len(files) {
		workers = len(files)
	}
	jobs := make(chan *ast.File)
	var visitMu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for file := range jobs {
				filename := fs.Position(file.Pos()).Filename
				var modules []string
				for _, imp := range file.Imports {
					path, err := strconv.Unquote(imp.Path.Value)
					if err != nil {
						continue
					}
					modules = append(modules, path)
				}
				program, err := lowerNativeGoPrepared(filename, sources[filename], nil, fs, file, info, files, packageName, modules, false)
				visitMu.Lock()
				if firstErr == nil {
					firstErr = visit(filename, program, err)
				}
				visitMu.Unlock()
				program = nil
			}
		}()
	}
	for _, file := range files {
		jobs <- file
	}
	close(jobs)
	wg.Wait()
	if firstErr != nil {
		return firstErr
	}
	return nil
}

// NativeGoProjectFiles applies the Go build constraints for the current
// GOOS/GOARCH to a package directory. Tests, dotfiles and generated artifacts
// are intentionally excluded, matching the normal production build set.
func NativeGoProjectFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	ctx := nativeGoBuildContext()
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || strings.HasPrefix(name, ".") || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") || strings.Contains(name, ".transpiled.") {
			continue
		}
		match, err := ctx.MatchFile(dir, name)
		if err != nil {
			return nil, fmt.Errorf("match Go source %s: %w", name, err)
		}
		if match {
			out = append(out, filepath.Join(dir, name))
		}
	}
	sort.Strings(out)
	return out, nil
}
