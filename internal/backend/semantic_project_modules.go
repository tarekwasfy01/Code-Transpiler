// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// expandSemanticProjectModules promotes persisted embedded module bodies and
// owner references to ordinary project units. It parses and releases one
// transport at a time, and never links module bodies into their importer.
func expandSemanticProjectModules(p *SemanticProject, moduleBaseDir, moduleStoreRoot string) error {
	if p == nil || len(p.Units) == 0 {
		return fmt.Errorf("cannot expand modules for an empty semantic project")
	}
	if moduleBaseDir == "" {
		moduleBaseDir = filepath.Dir(p.Units[0].Path)
	}
	materializedDir := filepath.Join(moduleBaseDir, ".semantic-cache", "embedded-units")
	if err := os.MkdirAll(materializedDir, 0755); err != nil {
		return fmt.Errorf("create embedded-unit cache: %w", err)
	}

	pathToID := make(map[string]string, len(p.Units))
	rootToID := map[string]string{}
	for _, unit := range p.Units {
		if unit != nil && unit.Path != "" {
			pathToID[filepath.Clean(unit.Path)] = unit.ID
		}
	}
	unitIDs := map[string]bool{}
	for _, unit := range p.Units {
		if unit != nil {
			unitIDs[unit.ID] = true
		}
	}
	for cursor := 0; cursor < len(p.Units); cursor++ {
		unit := p.Units[cursor]
		if unit == nil || unit.Path == "" {
			return fmt.Errorf("semantic project contains a unit without a source path")
		}
		data, err := os.ReadFile(unit.Path)
		if err != nil {
			return fmt.Errorf("read module summary unit %s: %w", unit.ID, err)
		}
		program, err := loadSemanticUnitBytes(unit.Path, data)
		if err != nil {
			return fmt.Errorf("parse module summary unit %s: %w", unit.ID, err)
		}
		unitUAST, err := canonicalUniversalAST(program)
		if err != nil {
			return fmt.Errorf("canonicalize project unit %s: %w", unit.ID, err)
		}
		unitRoot := stableBytesHash(mustJSONBytes(unitUAST))
		unit.SemanticRoot = unitRoot
		if rootToID[unitRoot] == "" {
			rootToID[unitRoot] = unit.ID
		}
		if len(program.Origin.Modules) == 0 && program.UniversalAST != nil {
			program.Origin.Modules = semanticImportsFromUAST(program.UniversalAST)
		} else if program.UniversalAST != nil {
			normalized := make([]string, 0, len(program.Origin.Modules))
			for _, dep := range program.Origin.Modules {
				if strings.HasPrefix(dep, "go:") {
					parts := strings.Split(dep, ":")
					if len(parts) >= 2 && parts[1] != "" {
						normalized = append(normalized, parts[1])
					}
				} else {
					normalized = append(normalized, dep)
				}
			}
			program.Origin.Modules = mergeModuleNames(normalized, semanticImportsFromUAST(program.UniversalAST))
			program.UniversalAST.Origin.Modules = append([]string(nil), program.Origin.Modules...)
		}
		rawEntries := ""
		if program.Metadata != nil {
			rawEntries = strings.TrimSpace(program.Metadata["semantic_module_embeddings"])
		}
		if len(program.Origin.Modules) > 0 && (rawEntries == "" || rawEntries == "[]" || rawEntries == "null") {
			entries, embedErr := EmbedSemanticModules(program, SemanticModuleEmbeddingOptions{
				BaseDir: moduleBaseDir, StoreRoot: moduleStoreRoot, UnitPath: unit.Path,
				Language: program.Origin.SourceLanguage, NeededOnly: true,
			})
			if embedErr != nil {
				return fmt.Errorf("resolve module dependencies for %s: %w", unit.ID, embedErr)
			}
			encoded, marshalErr := json.Marshal(entries)
			if marshalErr != nil {
				return marshalErr
			}
			rawEntries = string(encoded)
		}
		var entries []SemanticEmbeddedModule
		if rawEntries != "" {
			if err := json.Unmarshal([]byte(rawEntries), &entries); err != nil {
				return fmt.Errorf("decode module embedding metadata in %s: %w", unit.ID, err)
			}
		}
		for _, entry := range entries {
			if entry.Mode == "external" {
				// A reachable reference with an external marker will be resolved by
				// the closure only when its concrete call carries a complete import
				// ABI contract. It does not become a phantom compilation unit.
				continue
			}
			var moduleProgram *SemanticProgram
			modulePath := ""
			switch entry.Mode {
			case "inline":
				moduleProgram, err = DecodeSemanticEmbeddedModule(entry)
				if err != nil {
					return fmt.Errorf("decode inline module %q from %s: %w", entry.Identity, unit.ID, err)
				}
				modulePath, err = materializeSemanticProjectModule(materializedDir, entry, moduleProgram)
				if err != nil {
					return fmt.Errorf("materialize inline module %q: %w", entry.Identity, err)
				}
			case "reference":
				modulePath = entry.Owner
				if modulePath == "" {
					return fmt.Errorf("module reference %q has no owner", entry.Identity)
				}
				if !filepath.IsAbs(modulePath) {
					modulePath = filepath.Join(filepath.Dir(unit.Path), filepath.FromSlash(modulePath))
				}
				modulePath = filepath.Clean(modulePath)
				if prior := pathToID[modulePath]; prior != "" {
					continue
				}
				moduleBytes, readErr := os.ReadFile(modulePath)
				if readErr != nil {
					return fmt.Errorf("read module owner %q for %s: %w", modulePath, unit.ID, readErr)
				}
				moduleProgram, err = loadSemanticUnitBytes(modulePath, moduleBytes)
				if err != nil {
					return fmt.Errorf("parse module owner %q: %w", modulePath, err)
				}
			default:
				return fmt.Errorf("module %q has unsupported materialization mode %q", entry.Identity, entry.Mode)
			}
			moduleUAST, err := canonicalUniversalAST(moduleProgram)
			if err != nil {
				return fmt.Errorf("canonicalize module %q: %w", entry.Identity, err)
			}
			semanticRoot := stableBytesHash(mustJSONBytes(moduleUAST))
			if existing := rootToID[semanticRoot]; existing != "" {
				pathToID[modulePath] = existing
				continue
			}
			if modulePath == "" {
				return fmt.Errorf("module %q has no materialized transport path", entry.Identity)
			}
			id := "embedded/" + semanticRoot[:24] + ".se"
			if entry.Mode == "reference" {
				id = "external-owner/" + semanticRoot[:24] + ".se"
			}
			if unitIDs[id] {
				return fmt.Errorf("semantic module unit identity collision for %q", id)
			}
			unitIDs[id] = true
			pathToID[modulePath] = id
			rootToID[semanticRoot] = id
			p.Units = append(p.Units, &SemanticCompilationUnit{ID: id, Path: modulePath, SemanticRoot: semanticRoot})
		}
		program, data = nil, nil
	}
	return nil
}

func materializeSemanticProjectModule(dir string, entry SemanticEmbeddedModule, program *SemanticProgram) (string, error) {
	if program == nil {
		return "", fmt.Errorf("nil module program")
	}
	b, err := program.MarshalSemanticSESemanticOnly()
	if err != nil {
		return "", err
	}
	root := entry.SemanticRoot
	if root == "" {
		root = stableBytesHash(b)
	}
	key := stableBytesHash([]byte(root))
	path := filepath.Join(dir, key+".se")
	if existing, readErr := os.ReadFile(path); readErr == nil && stableBytesHash(existing) == stableBytesHash(b) {
		return path, nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return "", err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	return path, nil
}
