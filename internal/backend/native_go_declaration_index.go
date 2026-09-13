// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"fmt"
	"go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"sort"
)

// NativeGoDeclarationIndex is the lightweight project/package index used by
// streaming exports. It contains only declaration identities and signatures;
// function bodies are never retained here.
type NativeGoDeclarationIndex struct {
	Package string
	Files   map[string][]string
	Symbols map[string]string
}

// BuildNativeGoDeclarationIndex parses declarations only and releases each
// AST immediately. The index is safe to keep while units are lowered one by
// one and provides deterministic cross-file symbol identities.
func BuildNativeGoDeclarationIndex(filenames []string) (*NativeGoDeclarationIndex, error) {
	idx := &NativeGoDeclarationIndex{Files: map[string][]string{}, Symbols: map[string]string{}}
	fs := gotoken.NewFileSet()
	paths := append([]string(nil), filenames...)
	sort.Strings(paths)
	for _, name := range paths {
		path, err := filepath.Abs(name)
		if err != nil {
			return nil, err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, err := goparser.ParseFile(fs, path, data, 0)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		if idx.Package == "" {
			idx.Package = f.Name.Name
		}
		for _, d := range f.Decls {
			switch x := d.(type) {
			case *ast.FuncDecl:
				if x.Name != nil {
					key := filepath.ToSlash(path) + "::" + x.Name.Name
					idx.Symbols[key] = x.Name.Name
					idx.Files[path] = append(idx.Files[path], x.Name.Name)
				}
			case *ast.GenDecl:
				for _, s := range x.Specs {
					if ts, ok := s.(*ast.TypeSpec); ok && ts.Name != nil {
						key := filepath.ToSlash(path) + "::" + ts.Name.Name
						idx.Symbols[key] = ts.Name.Name
						idx.Files[path] = append(idx.Files[path], ts.Name.Name)
					}
				}
			}
		}
	}
	return idx, nil
}
