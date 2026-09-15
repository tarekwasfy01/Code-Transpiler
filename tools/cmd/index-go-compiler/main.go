// Copyright (c) 2026 Tarek Wasfy
// index-go-compiler parses Go source for migration inventory only.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
)

type Package struct{ ImportPath, SourceDir string }
type Function struct {
	ID, Package, File, Name, Receiver, Kind string
	Start, End                              int
	Params, Results                         int
	Calls                                   []string
}
type Call struct {
	Caller, Callee, Package, File string
	Start                         int
	Selector                      bool
	Resolved                      bool
}

func main() {
	root := `C:\Program Files\Go`
	out := `outputs\go-compiler-semantic-migration-2026-09-13\compiler-evidence\go\index`
	pkgs := []Package{}
	f, _ := os.Open(`outputs\go-compiler-semantic-migration-2026-09-13\compiler-package-inventory.csv`)
	defer f.Close()
	s := bufio.NewScanner(f)
	if s.Scan() {
	}
	for s.Scan() {
		var fields []string
		line := s.Text()
		fields = parseCSV(line)
		if len(fields) >= 5 {
			pkgs = append(pkgs, Package{ImportPath: fields[0], SourceDir: fields[4]})
		}
	}
	_ = root
	funcs := []Function{}
	calls := []Call{}
	fs := token.NewFileSet()
	for _, p := range pkgs {
		entries, _ := os.ReadDir(p.SourceDir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), `.go`) || strings.HasSuffix(e.Name(), `_test.go`) {
				continue
			}
			path := filepath.Join(p.SourceDir, e.Name())
			file, err := parser.ParseFile(fs, path, nil, parser.ParseComments)
			if err != nil {
				continue
			}
			ast.Inspect(file, func(n ast.Node) bool {
				fd, ok := n.(*ast.FuncDecl)
				if !ok {
					return true
				}
				fn := Function{Package: p.ImportPath, File: path, Name: fd.Name.Name, Kind: `function`, Start: fs.Position(fd.Pos()).Offset, End: fs.Position(fd.End()).Offset}
				if fd.Recv != nil && len(fd.Recv.List) > 0 {
					fn.Kind = `method`
					fn.Receiver = exprString(fd.Recv.List[0].Type)
				}
				fn.ID = p.ImportPath + `::` + fn.Receiver + `::` + fn.Name
				if fd.Type.Params != nil {
					fn.Params = countFields(fd.Type.Params)
				}
				if fd.Type.Results != nil {
					fn.Results = countFields(fd.Type.Results)
				}
				ast.Inspect(fd.Body, func(x ast.Node) bool {
					ce, ok := x.(*ast.CallExpr)
					if !ok {
						return true
					}
					callee := exprString(ce.Fun)
					fn.Calls = append(fn.Calls, callee)
					calls = append(calls, Call{Caller: fn.ID, Callee: callee, Package: p.ImportPath, File: path, Start: fs.Position(ce.Pos()).Offset, Selector: strings.Contains(callee, `.`)})
					return true
				})
				funcs = append(funcs, fn)
				return false
			})
		}
	}
	os.MkdirAll(out, 0755)
	writeJSONL(filepath.Join(out, `go-compiler-functions.jsonl`), funcs)
	writeJSONL(filepath.Join(out, `go-compiler-callgraph.jsonl`), calls)
	stages := map[string]string{"syntax": "Parsing", "types2": "TypeChecking", "noder": "IRConstruction", "ir": "IRConstruction", "typecheck": "TypeChecking", "ssa": "SSAConstruction", "ssagen": "MachineLowering", "walk": "Walk", "escape": "MiddleEnd", "inline": "MiddleEnd", "devirtualize": "MiddleEnd"}
	sm := map[string]any{"schema": "go-compiler-stage-map.v1", "stages": stages, "package_count": len(pkgs), "function_count": len(funcs), "call_edge_count": len(calls), "resolution": "syntactic edges only; target resolution is not inferred"}
	b, _ := json.MarshalIndent(sm, "", "  ")
	os.WriteFile(filepath.Join(out, `go-compiler-stage-map.json`), append(b, '\n'), 0644)
	fmt.Printf("packages=%d functions=%d calls=%d\n", len(pkgs), len(funcs), len(calls))
}
func countFields(f *ast.FieldList) int {
	n := 0
	for _, x := range f.List {
		if len(x.Names) == 0 {
			n++
		} else {
			n += len(x.Names)
		}
	}
	return n
}
func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return exprString(x.X) + `.` + x.Sel.Name
	case *ast.StarExpr:
		return `*` + exprString(x.X)
	case *ast.IndexExpr:
		return exprString(x.X) + `[...]`
	case *ast.IndexListExpr:
		return exprString(x.X) + `[...]`
	default:
		return fmt.Sprintf(`%T`, e)
	}
}
func writeJSONL(path string, v any) {
	f, _ := os.Create(path)
	defer f.Close()
	switch xs := v.(type) {
	case []Function:
		for _, x := range xs {
			b, _ := json.Marshal(x)
			f.Write(append(b, '\n'))
		}
	case []Call:
		for _, x := range xs {
			b, _ := json.Marshal(x)
			f.Write(append(b, '\n'))
		}
	}
}
func parseCSV(line string) []string {
	var out []string
	var cur strings.Builder
	q := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '"' {
			q = !q
			continue
		}
		if c == ',' && !q {
			out = append(out, cur.String())
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	out = append(out, cur.String())
	return out
}
