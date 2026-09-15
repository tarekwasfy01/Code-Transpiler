// Copyright (c) 2026 Tarek Wasfy
// binary-source-pairs creates source -> Canonical-UAST -> compiler-assembly
// witnesses. It deliberately accepts only snapshot files whose SHA-256 still
// equals the checked-out source. A PE/RVA association is left empty unless a
// linker map or PDB proves it; source semantics are never guessed from bytes.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type manifestRow map[string]string

type goPackage struct {
	Dir        string
	ImportPath string
	GoFiles    []string
}

type goListRecord struct {
	Dir        string
	ImportPath string
	GoFiles    []string
}

type functionDecl struct {
	Name string
	Line int
}

var textSymbol = regexp.MustCompile(`^\s*0x[0-9a-f]+\s+[0-9]+ \((.+\.go):([0-9]+)\)\s+TEXT\s+([^\s]+)`)

func main() {
	snapshot := flag.String("snapshot", `C:\Users\tarek\Desktop\Semantic\current-build-go`, "source snapshot root")
	semantic := flag.String("semantic", `C:\Users\tarek\Desktop\Semantic\current-build-semanticjson`, "semantic JSON export root")
	out := flag.String("out", "outputs/binary-source-pairs", "output directory")
	limit := flag.Int("limit", 0, "maximum packages, 0 means all")
	offset := flag.Int("offset", 0, "package offset in stable go-list order")
	flag.Parse()
	if err := run(*snapshot, *semantic, *out, *limit, *offset); err != nil {
		fmt.Fprintln(os.Stderr, "binary-source-pairs:", err)
		os.Exit(1)
	}
}

func run(snapshotRoot, semanticRoot, out string, limit, offset int) error {
	wd, err := os.Getwd()
	if err != nil {
		return err
	}
	manifest, err := readManifest(filepath.Join(semanticRoot, "manifest.csv"))
	if err != nil {
		return err
	}
	bySource := map[string]manifestRow{}
	for _, r := range manifest {
		bySource[normal(r["source_path"])] = r
	}

	packages, err := listPackages(wd)
	if err != nil {
		return err
	}
	sort.Slice(packages, func(i, j int) bool { return packages[i].ImportPath < packages[j].ImportPath })
	rows := [][]string{}
	packageRows := [][]string{}
	exactFiles, semanticFiles, assemblyFunctions, assemblyBytes, built, buildFailures := 0, 0, 0, 0, 0, 0
	processed := 0
	for ordinal, pkg := range packages {
		if ordinal < offset {
			continue
		}
		if limit > 0 && processed >= limit {
			break
		}
		processed++
		eligible := map[string]manifestRow{}
		for _, name := range pkg.GoFiles {
			full := filepath.Join(pkg.Dir, name)
			rel, e := filepath.Rel(wd, full)
			if e != nil {
				continue
			}
			r := bySource[normal(rel)]
			if r == nil || !sameSnapshot(filepath.Join(snapshotRoot, filepath.FromSlash(normal(rel))), full, r["source_sha256"]) {
				continue
			}
			exactFiles++
			eligible[normal(full)] = r
		}
		if len(eligible) == 0 {
			continue
		}
		assembly, e := compileAssembly(wd, pkg.ImportPath)
		if e != nil {
			buildFailures++
			packageRows = append(packageRows, []string{pkg.ImportPath, "FAIL", fmt.Sprint(len(eligible)), "0", compact(e.Error())})
			if err := checkpoint(out, rows, packageRows); err != nil {
				return err
			}
			continue
		}
		built++
		asmRel := filepath.ToSlash(filepath.Join("assembly", strings.NewReplacer("/", "_", "\\", "_", ".", "_").Replace(pkg.ImportPath)+".s"))
		asmPath := filepath.Join(out, filepath.FromSlash(asmRel))
		if err := os.MkdirAll(filepath.Dir(asmPath), 0755); err != nil {
			return err
		}
		if err := os.WriteFile(asmPath, assembly, 0644); err != nil {
			return err
		}
		assemblyBytes += len(assembly)
		decls := map[string][]functionDecl{}
		for full := range eligible {
			decls[full] = parseDecls(full)
			semanticFiles++
		}
		matched := 0
		for _, x := range parseTextSymbols(assembly) {
			full := normal(x.file)
			r, ok := eligible[full]
			if !ok {
				continue
			}
			fn := matchingDecl(decls[full], x.line)
			if fn == "" {
				continue
			}
			matched++
			assemblyFunctions++
			rows = append(rows, []string{normalPath(wd, full), r["source_sha256"], r["semantic_json_path"], r["semantic_json_sha256"], pkg.ImportPath, fn, fmt.Sprint(x.line), x.symbol, fmt.Sprint(x.asmLine), asmRel, "true", "", "", "source hash + compiler source location"})
		}
		packageRows = append(packageRows, []string{pkg.ImportPath, "PASS", fmt.Sprint(len(eligible)), fmt.Sprint(matched), ""})
		if err := checkpoint(out, rows, packageRows); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	if err := checkpoint(out, rows, packageRows); err != nil {
		return err
	}
	summary := map[string]any{"snapshot_go_files_with_semantic_manifest": len(bySource), "exact_source_files_considered": exactFiles, "semantic_files_used": semanticFiles, "packages_built": built, "package_build_failures": buildFailures, "compiler_assembly_bytes": assemblyBytes, "source_semantic_assembly_witnesses": assemblyFunctions, "pe_rva_witnesses": 0, "pe_rva_reason": "no linker map or PDB association supplied", "fail_closed_source_matching": true}
	b, _ := json.MarshalIndent(summary, "", "  ")
	return os.WriteFile(filepath.Join(out, "summary.json"), append(b, '\n'), 0644)
}

func checkpoint(out string, rows, packageRows [][]string) error {
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	if err := writeCSV(filepath.Join(out, "source_semantic_assembly_pairs.csv"), []string{"source_path", "source_sha256", "semantic_json_path", "semantic_json_sha256", "package", "function", "source_line", "compiler_symbol", "assembly_line", "assembly_path", "exact_snapshot", "pe_chunk_id", "rva", "proof"}, rows); err != nil {
		return err
	}
	return writeCSV(filepath.Join(out, "package_build_matrix.csv"), []string{"package", "status", "eligible_exact_source_files", "matched_assembly_functions", "diagnostic"}, packageRows)
}

func normal(s string) string { return strings.ToLower(filepath.ToSlash(filepath.Clean(s))) }
func normalPath(root, full string) string {
	r, _ := filepath.Rel(root, full)
	return filepath.ToSlash(r)
}
func hashFile(path string) string {
	b, e := os.ReadFile(path)
	if e != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return strings.ToUpper(hex.EncodeToString(h[:]))
}
func sameSnapshot(snapshot, current, expected string) bool {
	return expected != "" && hashFile(snapshot) == strings.ToUpper(expected) && hashFile(current) == strings.ToUpper(expected)
}
func compact(s string) string { return strings.Join(strings.Fields(s), " ") }

func readManifest(path string) ([]manifestRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	h, err := r.Read()
	if err != nil {
		return nil, err
	}
	for i := range h {
		h[i] = strings.TrimPrefix(strings.TrimSpace(h[i]), "\ufeff")
	}
	var out []manifestRow
	for {
		v, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		x := manifestRow{}
		for i, k := range h {
			if i < len(v) {
				x[k] = strings.TrimSpace(v[i])
			}
		}
		out = append(out, x)
	}
	return out, nil
}

func listPackages(wd string) ([]goPackage, error) {
	c := exec.Command("go", "list", "-json", "./...")
	c.Dir = wd
	b, err := c.Output()
	if err != nil {
		return nil, err
	}
	d := json.NewDecoder(bytes.NewReader(b))
	var out []goPackage
	for {
		var x goListRecord
		if e := d.Decode(&x); e == io.EOF {
			break
		} else if e != nil {
			return nil, e
		}
		if x.ImportPath != "" && len(x.GoFiles) > 0 {
			out = append(out, goPackage{Dir: x.Dir, ImportPath: x.ImportPath, GoFiles: x.GoFiles})
		}
	}
	return out, nil
}

func compileAssembly(wd, pkg string) ([]byte, error) {
	c := exec.Command("go", "build", "-gcflags="+pkg+"=-S", pkg)
	c.Dir = wd
	var stderr bytes.Buffer
	c.Stderr = &stderr
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("%w: %s", err, compact(stderr.String()))
	}
	return stderr.Bytes(), nil
}

type asmSymbol struct {
	file          string
	line, asmLine int
	symbol        string
}

func parseTextSymbols(b []byte) []asmSymbol {
	var out []asmSymbol
	sc := bufio.NewScanner(bytes.NewReader(b))
	n := 0
	for sc.Scan() {
		n++
		m := textSymbol.FindStringSubmatch(sc.Text())
		if len(m) == 4 {
			var line int
			fmt.Sscanf(m[2], "%d", &line)
			out = append(out, asmSymbol{file: normal(m[1]), line: line, asmLine: n, symbol: m[3]})
		}
	}
	return out
}
func parseDecls(path string) []functionDecl {
	f, e := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if e != nil {
		return nil
	}
	fs := token.NewFileSet()
	f, e = parser.ParseFile(fs, path, nil, 0)
	if e != nil {
		return nil
	}
	var out []functionDecl
	for _, d := range f.Decls {
		if x, ok := d.(*ast.FuncDecl); ok && x.Name != nil {
			out = append(out, functionDecl{Name: x.Name.Name, Line: fs.Position(x.Pos()).Line})
		}
	}
	return out
}
func matchingDecl(ds []functionDecl, line int) string {
	for _, d := range ds {
		if d.Line == line {
			return d.Name
		}
	}
	return ""
}
func writeCSV(path string, h []string, rows [][]string) error {
	f, e := os.Create(path)
	if e != nil {
		return e
	}
	defer f.Close()
	w := csv.NewWriter(f)
	if e = w.Write(h); e != nil {
		return e
	}
	if e = w.WriteAll(rows); e != nil {
		return e
	}
	w.Flush()
	return w.Error()
}
