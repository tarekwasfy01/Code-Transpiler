// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"encoding/json"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/v2/internal/backend"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func semanticModuleCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: module import|list|info|remove|verify|path|setpath <target>")
	}
	store, err := backend.DefaultSemanticModuleStore()
	if err != nil {
		return err
	}
	switch strings.ToLower(args[0]) {
	case "setpath":
		if len(args) != 2 {
			return fmt.Errorf("usage: module setpath <parent-folder> (or module setpath --default)")
		}
		if args[1] == "--default" {
			if err := backend.ResetSemanticModuleBase(); err != nil {
				return err
			}
			fmt.Println("Semantic module path reset to LOCALAPPDATA")
		} else {
			if err := backend.SetSemanticModuleBase(args[1]); err != nil {
				return err
			}
			s, _ := backend.DefaultSemanticModuleStore()
			fmt.Println(s.Root)
		}
		return nil
	case "path":
		s, err := backend.DefaultSemanticModuleStore()
		if err != nil {
			return err
		}
		fmt.Println(s.Root)
		return nil
	case "create", "merge":
		return createSemanticModule(args[1:])
	case "list":
		ents, err := os.ReadDir(filepath.Join(store.Root, "modules"))
		if err != nil && !os.IsNotExist(err) {
			return err
		}
		for _, e := range ents {
			if e.IsDir() {
				fmt.Println(e.Name())
			}
		}
		return nil
	case "import":
		if len(args) < 2 {
			return fmt.Errorf("usage: module import [--language lang] <source>")
		}
		lang := ""
		path := ""
		for i := 1; i < len(args); i++ {
			if args[i] == "--language" && i+1 < len(args) {
				lang = args[i+1]
				i++
				continue
			}
			path = args[i]
		}
		if path == "" {
			return fmt.Errorf("missing module path")
		}
		// Package imports (directory, ZIP, or HTTP(S) archive) preserve the
		// original tree and emit linked semantic units plus a .smod manifest.
		isDir := false
		if st, se := os.Stat(path); se == nil {
			isDir = st.IsDir()
		}
		registryLang := backend.NormalizeLanguage(lang)
		isMavenCoordinate := (registryLang == "java" || registryLang == "kotlin") && strings.Contains(path, ":")
		isNamedRegistryPackage := ((registryLang == "python" || registryLang == "rust" || registryLang == "r" || registryLang == "go" || registryLang == "node" || registryLang == "javascript" || registryLang == "typescript" || registryLang == "csharp" || registryLang == "dotnet" || registryLang == "c" || registryLang == "cpp" || registryLang == "c++" || registryLang == "julia" || registryLang == "nim" || registryLang == "swift") && filepath.Ext(path) == "") || isMavenCoordinate
		if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") || strings.EqualFold(filepath.Ext(path), ".zip") || isDir || isNamedRegistryPackage {
			var stop = make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(1)
			go func() {
				defer wg.Done()
				ticker := time.NewTicker(180 * time.Millisecond)
				defer ticker.Stop()
				for {
					select {
					case <-ticker.C:
						fmt.Print(".")
					case <-stop:
						return
					}
				}
			}()
			pkg, pe := (backend.UniversalModuleResolver{Store: store}).ImportPackage(path, backend.ModuleImportOptions{Language: lang})
			close(stop)
			wg.Wait()
			fmt.Println()
			if pe != nil {
				return pe
			}
			fmt.Printf("Package import complete: %s/%s.smod\n", filepath.Join(store.Root, backend.SafeModuleName(pkg.Name)), pkg.Name)
			return nil
		}
		resolver := backend.UniversalModuleResolver{Store: store}
		m, err := resolver.ImportTarget(path, backend.ModuleImportOptions{Language: lang})
		if err != nil {
			return err
		}
		fmt.Println(m.CacheKey)
		return nil
	case "verify":
		if len(args) < 2 {
			return fmt.Errorf("usage: module verify <cache-key>")
		}
		m, err := store.Open(args[1])
		if err != nil {
			return err
		}
		return backend.VerifySemanticModule(m)
	case "info":
		if len(args) < 2 {
			return fmt.Errorf("usage: module info <cache-key>")
		}
		b, err := os.ReadFile(filepath.Join(store.Root, "modules", args[1], "module.meta"))
		if err != nil {
			return err
		}
		var v any
		if json.Unmarshal(b, &v) != nil {
			return fmt.Errorf("invalid module metadata")
		}
		out, _ := json.MarshalIndent(v, "", "  ")
		fmt.Println(string(out))
		return nil
	case "remove":
		if len(args) < 2 {
			return fmt.Errorf("usage: module remove <cache-key>")
		}
		if !filepath.IsAbs(args[1]) && args[1] != filepath.Base(args[1]) {
			return fmt.Errorf("invalid cache key")
		}
		return os.RemoveAll(filepath.Join(store.Root, "modules", filepath.Base(args[1])))
	default:
		return fmt.Errorf("unknown module command %q", args[0])
	}
}

func createSemanticModule(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: module create <inputs...> -o module.smod")
	}
	out := ""
	inputs := []string{}
	for i := 0; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			out = args[i+1]
			i++
			continue
		}
		inputs = append(inputs, args[i])
	}
	if out == "" || len(inputs) == 0 {
		return fmt.Errorf("usage: module create <inputs...> -o module.smod")
	}
	// Accept package sources as merge inputs too; import them once through the
	// resolver so ZIPs, directories and URLs are handled identically.
	store, err := backend.DefaultSemanticModuleStore()
	if err != nil {
		return err
	}
	resolved := make([]string, 0, len(inputs))
	for _, in := range inputs {
		isDir := false
		if st, se := os.Stat(in); se == nil {
			isDir = st.IsDir()
		}
		if isDir || strings.HasPrefix(in, "http://") || strings.HasPrefix(in, "https://") || strings.EqualFold(filepath.Ext(in), ".zip") {
			pkg, pe := (backend.UniversalModuleResolver{Store: store}).ImportPackage(in, backend.ModuleImportOptions{})
			if pe != nil {
				return pe
			}
			resolved = append(resolved, filepath.Join(store.Root, backend.SafeModuleName(pkg.Name), pkg.Name+".smod"))
		} else {
			resolved = append(resolved, in)
		}
	}
	p, err := backend.MergeSemanticFiles(resolved)
	if err != nil {
		return err
	}
	spz, err := p.MarshalSemanticSPZ()
	if err != nil {
		return err
	}
	root := strings.TrimSuffix(out, filepath.Ext(out))
	if root == "" {
		root = out
	}
	if err = os.MkdirAll(filepath.Join(root, "semantic"), 0755); err != nil {
		return err
	}
	semPath := filepath.Join(root, "semantic", "merged.spz")
	if err = os.WriteFile(semPath, spz, 0644); err != nil {
		return err
	}
	manifest := backend.SemanticPackageManifest{SchemaVersion: 1, Name: filepath.Base(root), Source: "semantic-merge", Root: root, Files: []backend.SemanticPackageFile{{Path: "merged.spz", Kind: "SEMANTIC_MODULE", Language: "semantic", SemanticPath: "semantic/merged.spz", SourceHash: "", Status: "SEMANTIC_READY"}}}
	mb, _ := json.MarshalIndent(manifest, "", "  ")
	manifestPath := out
	if strings.ToLower(filepath.Ext(manifestPath)) != ".smod" {
		manifestPath += ".smod"
	}
	if err = os.WriteFile(manifestPath, mb, 0644); err != nil {
		return err
	}
	fmt.Println(manifestPath)
	return nil
}
