// Copyright (c) 2026 Tarek Wasfy
// index-go-ssa.go inventories generated SSA operation metadata; it does not infer semantics.
package main

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type Row struct {
	Name, Kind, File        string
	Ordinal                 int
	Mapping, Classification string
}

func main() {
	root := `C:\Program Files\Go\src\cmd\compile\internal\ssa`
	out := `outputs\go-compiler-semantic-migration-2026-09-13\compiler-evidence\go\index`
	rows := []Row{}
	fs := token.NewFileSet()
	files := []string{filepath.Join(root, "opGen.go"), filepath.Join(root, "block.go")}
	for _, path := range files {
		f, e := parser.ParseFile(fs, path, nil, 0)
		if e != nil {
			continue
		}
		ast.Inspect(f, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, el := range cl.Elts {
				kv, ok := el.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				id, ok := kv.Key.(*ast.Ident)
				if !ok || id.Name != "name" {
					continue
				}
				lit, ok := kv.Value.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				name, _ := strconv.Unquote(lit.Value)
				kind := "SSA_OPERATION"
				if strings.Contains(filepath.Base(path), "block") {
					kind = "SSA_BLOCK"
				}
				rows = append(rows, Row{Name: name, Kind: kind, File: path, Ordinal: len(rows), Classification: "UNMAPPED_REQUIRES_SEMANTIC_WITNESS"})
			}
			return true
		})
	}
	os.MkdirAll(out, 0755)
	h, _ := os.Create(filepath.Join(out, "go-compiler-ssa-map.jsonl"))
	defer h.Close()
	for _, r := range rows {
		b, _ := json.Marshal(r)
		h.Write(append(b, '\n'))
	}
	fmt.Printf("ssa_records=%d\n", len(rows))
}
