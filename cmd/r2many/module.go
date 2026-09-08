// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"encoding/json"
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"os"
	"path/filepath"
	"strings"
)

func semanticModuleCommand(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: module import|list|info|remove|verify <target>")
	}
	store, err := backend.DefaultSemanticModuleStore()
	if err != nil {
		return err
	}
	switch strings.ToLower(args[0]) {
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
