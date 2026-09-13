// Command go-compiler-closure extracts a conservative, symbol-based callgraph
// from the pinned Go compiler source. It is migration evidence, not a second IR.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type edge struct {
	Caller, Callee, CallKind, Package, Resolution, SourceFile string
	Line                                                      int
	Confidence                                                string
}
type fn struct {
	ID, Package, Name, File string
	Line                    int
}
type sourceFile struct {
	AST     *ast.File
	Pkg     string
	Imports map[string]string
}

type closureReport struct {
	SchemaVersion     string   `json:"schema_version"`
	SourceRoot        string   `json:"source_root"`
	Roots             []string `json:"roots"`
	Reachable         int      `json:"reachable_functions"`
	ResolvedEdges     int      `json:"resolved_edges"`
	UnresolvedEdges   int      `json:"unresolved_edges"`
	UnresolvedCallees []string `json:"unresolved_callees"`
	Status            string   `json:"status"`
}

func main() {
	root := flag.String("root", `C:\Program Files\Go\src\cmd\compile`, "Go compiler source root")
	out := flag.String("out", "compiler-migration/go/index/go-compiler-resolved-callgraph.jsonl", "output JSONL")
	closureOut := flag.String("closure-out", "compiler-migration/go/closure/go-compiler-executable-closure.json", "closure report")
	flag.Parse()
	fset := token.NewFileSet()
	var files []sourceFile
	var names []fn
	funcs := map[string]fn{}
	err := filepath.Walk(*root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if strings.EqualFold(info.Name(), "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, e := parser.ParseFile(fset, path, nil, 0)
		if e != nil {
			return nil
		}
		imports := map[string]string{}
		for _, spec := range f.Imports {
			path := strings.Trim(spec.Path.Value, `"`)
			alias := filepath.Base(path)
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			if alias != "_" && alias != "." {
				imports[alias] = path
			}
		}
		pkg := packageID(*root, path, f.Name.Name)
		files = append(files, sourceFile{AST: f, Pkg: pkg, Imports: imports})
		rel, _ := filepath.Rel(*root, path)
		for _, d := range f.Decls {
			if fd, ok := d.(*ast.FuncDecl); ok {
				id := pkg + "." + fd.Name.Name
				if fd.Recv != nil {
					id = pkg + ".(method)." + fd.Name.Name
				}
				p := fset.Position(fd.Pos())
				x := fn{ID: id, Package: pkg, Name: fd.Name.Name, File: filepath.ToSlash(rel), Line: p.Line}
				funcs[id] = x
				names = append(names, x)
			}
		}
		return nil
	})
	if err != nil {
		panic(err)
	}
	var edges []edge
	packagePaths := map[string]string{}
	for _, sf := range files {
		filePath := fset.Position(sf.AST.Pos()).Filename
		relDir, _ := filepath.Rel(*root, filepath.Dir(filePath))
		packagePaths[filepath.ToSlash(relDir)] = sf.Pkg
	}
	for _, sf := range files {
		f := sf.AST
		pkg := sf.Pkg
		rel, _ := filepath.Rel(*root, fset.Position(f.Pos()).Filename)
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			caller := pkg + "." + fd.Name.Name
			if fd.Recv != nil {
				caller = pkg + ".(method)." + fd.Name.Name
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				ce, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				callee, kind, res := resolve(ce.Fun, pkg, sf.Imports, packagePaths, funcs)
				p := fset.Position(ce.Pos())
				edges = append(edges, edge{caller, callee, kind, pkg, res, filepath.ToSlash(rel), p.Line, map[bool]string{true: "direct", false: "unresolved"}[res == "local_symbol"]})
				return true
			})
		}
	}
	sort.Slice(edges, func(i, j int) bool {
		return edges[i].Caller+edges[i].SourceFile+fmt.Sprint(edges[i].Line) < edges[j].Caller+edges[j].SourceFile+fmt.Sprint(edges[j].Line)
	})
	if e := os.MkdirAll(filepath.Dir(*out), 0755); e != nil {
		panic(e)
	}
	w, e := os.Create(*out)
	if e != nil {
		panic(e)
	}
	defer w.Close()
	enc := json.NewEncoder(w)
	for _, x := range edges {
		if e := enc.Encode(x); e != nil {
			panic(e)
		}
	}
	adj := map[string][]string{}
	for _, e := range edges {
		if e.Resolution == "local_symbol" {
			adj[e.Caller] = append(adj[e.Caller], e.Callee)
		}
	}
	roots := []string{}
	for _, candidate := range []string{"root#main.main", "root#main.Main"} {
		if _, ok := funcs[candidate]; ok {
			roots = append(roots, candidate)
		}
	}
	seen := map[string]bool{}
	queue := append([]string(nil), roots...)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		for _, next := range adj[id] {
			if !seen[next] {
				queue = append(queue, next)
			}
		}
	}
	unknown := map[string]bool{}
	for _, e := range edges {
		if seen[e.Caller] && e.Resolution != "local_symbol" {
			unknown[e.Callee] = true
		}
	}
	unknownList := make([]string, 0, len(unknown))
	for x := range unknown {
		unknownList = append(unknownList, x)
	}
	sort.Strings(unknownList)
	cr := closureReport{SchemaVersion: "go-compiler-closure-v1", SourceRoot: *root, Roots: roots, Reachable: len(seen), ResolvedEdges: len(edges) - countUnresolved(edges), UnresolvedEdges: countUnresolved(edges), UnresolvedCallees: unknownList, Status: "CONSERVATIVE_SOURCE_CLOSURE"}
	if e := os.MkdirAll(filepath.Dir(*closureOut), 0755); e != nil {
		panic(e)
	}
	cb, e := json.MarshalIndent(cr, "", "  ")
	if e != nil {
		panic(e)
	}
	if e = os.WriteFile(*closureOut, append(cb, '\n'), 0644); e != nil {
		panic(e)
	}
	fmt.Printf("FUNCTIONS=%d CALL_EDGES=%d OUTPUT=%s\n", len(names), len(edges), *out)
}

func countUnresolved(edges []edge) int {
	n := 0
	for _, e := range edges {
		if e.Resolution != "local_symbol" {
			n++
		}
	}
	return n
}

func resolve(x ast.Expr, pkg string, imports map[string]string, packagePaths map[string]string, funcs map[string]fn) (string, string, string) {
	switch v := x.(type) {
	case *ast.Ident:
		id := pkg + "." + v.Name
		if _, ok := funcs[id]; ok {
			return id, "function_call", "local_symbol"
		}
		return "GO_COMPILER_CALL_UNRESOLVED:" + v.Name, "function_call", "unresolved"
	case *ast.SelectorExpr:
		if id, ok := v.X.(*ast.Ident); ok {
			if importPath, imported := imports[id.Name]; imported {
				prefix := strings.TrimPrefix(importPath, "cmd/compile/")
				if targetPkg, found := packagePaths[prefix]; found {
					candidate := targetPkg + "." + v.Sel.Name
					if _, exists := funcs[candidate]; exists {
						return candidate, "selector_call", "local_symbol"
					}
				}
			}
		}
		return "GO_COMPILER_CALL_UNRESOLVED:" + v.Sel.Name, "selector_call", "unresolved"
	default:
		return "GO_COMPILER_CALL_UNRESOLVED:dynamic", "dynamic_call", "unresolved"
	}
}

func packageID(root, file, declared string) string {
	rel, err := filepath.Rel(root, filepath.Dir(file))
	if err != nil || rel == "." {
		rel = "root"
	}
	return filepath.ToSlash(rel) + "#" + declared
}
