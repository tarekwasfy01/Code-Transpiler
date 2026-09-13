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
	"sort"
	"strconv"
	"strings"
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
	info := nativeGoTypeInfo()
	conf := types.Config{Importer: nativeGoImporterFor(paths[0]), Sizes: types.SizesFor("gc", "amd64")}
	if _, err := conf.Check(packageName, fs, files, info); err != nil && !nativeGoBoundaryTypecheckError(err) {
		return fmt.Errorf("Go package typecheck: %w", err)
	}
	for _, file := range files {
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
		if visitErr := visit(filename, program, err); visitErr != nil {
			return visitErr
		}
		program = nil
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
