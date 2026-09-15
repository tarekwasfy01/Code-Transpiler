// Copyright (c) 2026 Tarek Wasfy
package backend

// Compiler-rule evidence is a source-provenance view over compiler stages.
// It records lowering components and implementation strategies; it does not
// promote names or similar-looking machine operations to semantic equivalence.
import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	goParser "go/parser"
	goToken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const CompilerRuleEvidenceSchema = "compiler-rule-evidence/v1"

type CompilerStage string

const (
	CompilerStageSemanticSelection CompilerStage = "semantic_selection"
	CompilerStageInstructionSelect CompilerStage = "instruction_selection"
	CompilerStageEncoding          CompilerStage = "encoding"
	CompilerStageLinking           CompilerStage = "linking"
	CompilerStageLowering          CompilerStage = "lowering"
	CompilerStageBinding           CompilerStage = "binding"
	CompilerStageLegalization      CompilerStage = "legalization"
	CompilerStageCodeGeneration    CompilerStage = "code_generation"
)

type EvidenceConfidence string

const (
	EvidenceDirect    EvidenceConfidence = "direct"
	EvidenceDerived   EvidenceConfidence = "derived"
	EvidenceInferred  EvidenceConfidence = "inferred"
	EvidenceCandidate EvidenceConfidence = "candidate"
)

type CompilerEvidenceLocation struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit,omitempty"`
	File       string `json:"file"`
	SHA256     string `json:"sha256"`
	Symbol     string `json:"symbol"`
	StartLine  int    `json:"start_line"`
	EndLine    int    `json:"end_line"`
}

type CompilerSourceUnit struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type CompilerEvidence struct {
	ID                   string                     `json:"id"`
	Compiler             string                     `json:"compiler"`
	Version              string                     `json:"version"`
	Stage                CompilerStage              `json:"stage"`
	TargetArchitecture   string                     `json:"target_architecture,omitempty"`
	SemanticIdentity     string                     `json:"semantic_identity,omitempty"`
	InputPattern         string                     `json:"input_pattern"`
	Conditions           []string                   `json:"conditions,omitempty"`
	OutputPattern        []string                   `json:"output_pattern,omitempty"`
	ReferencedOperations []string                   `json:"referenced_operations,omitempty"`
	Relation             string                     `json:"relation"`
	Confidence           EvidenceConfidence         `json:"confidence"`
	Locations            []CompilerEvidenceLocation `json:"locations"`
}

type SemanticEquivalence struct {
	ID          string             `json:"id"`
	Concept     string             `json:"concept"`
	Relation    string             `json:"relation"`
	Conditions  []string           `json:"conditions,omitempty"`
	EvidenceIDs []string           `json:"evidence_ids"`
	Confidence  EvidenceConfidence `json:"confidence"`
}

type CompilerCallEdge struct {
	From     string                   `json:"from"`
	To       string                   `json:"to"`
	Location CompilerEvidenceLocation `json:"location"`
}

type CompilerEvidenceGap struct {
	ID               string                   `json:"id"`
	Compiler         string                   `json:"compiler"`
	SemanticIdentity string                   `json:"semantic_identity,omitempty"`
	Gap              string                   `json:"gap"`
	Detail           string                   `json:"detail"`
	Location         CompilerEvidenceLocation `json:"location"`
}

type NativeCompilerEvidenceReport struct {
	SchemaVersion string                `json:"schema_version"`
	Compiler      string                `json:"compiler"`
	Version       string                `json:"version"`
	Repository    string                `json:"repository"`
	SourceRoot    string                `json:"source_root"`
	Units         int                   `json:"source_units_analyzed"`
	Functions     int                   `json:"functions_indexed"`
	SourceFiles   []CompilerSourceUnit  `json:"source_files"`
	CallEdges     []CompilerCallEdge    `json:"call_edges"`
	Rules         []CompilerEvidence    `json:"rules"`
	Equivalences  []SemanticEquivalence `json:"semantic_equivalences"`
	Gaps          []CompilerEvidenceGap `json:"gaps"`
	EncodableOps  []string              `json:"encodable_machine_operations"`
	Summary       map[string]int        `json:"summary"`
}

type nativeEvidenceFile struct {
	path string
	data []byte
	ast  *ast.File
	set  *goToken.FileSet
}

// ExtractNativeCompilerEvidence indexes the current compiler's semantic
// dispatch, emitted x64 operations, encoder coverage, and Go call edges. It
// reads source only: no compiler process is run and no productive registry is
// changed. Every extracted edge points to source line(s) and a file digest.
func ExtractNativeCompilerEvidence(root string) (*NativeCompilerEvidenceReport, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	backendRoot := filepath.Join(root, "internal", "backend")
	set := goToken.NewFileSet()
	var files []nativeEvidenceFile
	funcIndex := map[string]goToken.Position{}
	err = filepath.WalkDir(backendRoot, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		file, e := goParser.ParseFile(set, path, data, goParser.AllErrors|goParser.ParseComments)
		if e != nil {
			return fmt.Errorf("parse %s: %w", path, e)
		}
		rel, _ := filepath.Rel(root, path)
		f := nativeEvidenceFile{path: filepath.ToSlash(rel), data: data, ast: file, set: set}
		files = append(files, f)
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Body != nil {
				funcIndex[functionIdentity(fn)] = set.Position(fn.Pos())
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no backend Go source units under %s", backendRoot)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	hashes := map[string]string{}
	for _, f := range files {
		sum := sha256.Sum256(f.data)
		hashes[f.path] = hex.EncodeToString(sum[:])
	}
	report := &NativeCompilerEvidenceReport{SchemaVersion: CompilerRuleEvidenceSchema, Compiler: "Universal-Code-Transpiler native x64 compiler", Version: "working-tree", Repository: "local-working-tree", SourceRoot: filepath.ToSlash(backendRoot), Units: len(files), Functions: len(funcIndex), Summary: map[string]int{}}
	report.Summary["canonical_compiler_truth_families"] = len(CompilerSourceTruths())
	report.Summary["csharp_projection_primitives"] = len(CSharpProjectionPrimitives())
	for _, f := range files {
		report.SourceFiles = append(report.SourceFiles, CompilerSourceUnit{File: f.path, SHA256: hashes[f.path]})
	}
	encodable := map[string]CompilerEvidenceLocation{}
	for _, f := range files {
		collectEncoderEvidence(f, hashes[f.path], encodable)
	}
	for op := range encodable {
		report.EncodableOps = append(report.EncodableOps, op)
	}
	sort.Strings(report.EncodableOps)
	for _, f := range files {
		collectNativeCallEdges(f, hashes[f.path], funcIndex, &report.CallEdges)
		collectNativeRuleEvidence(f, hashes[f.path], encodable, report)
	}
	collectEncoderRules(report, encodable)
	collectMachineClassRules(files, hashes, report)
	sort.Slice(report.Rules, func(i, j int) bool { return report.Rules[i].ID < report.Rules[j].ID })
	sort.Slice(report.Gaps, func(i, j int) bool { return report.Gaps[i].ID < report.Gaps[j].ID })
	sort.Slice(report.CallEdges, func(i, j int) bool {
		a, b := report.CallEdges[i], report.CallEdges[j]
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Location.StartLine < b.Location.StartLine
	})
	report.Summary["lowering_component_rules"] = countRelation(report.Rules, "lowering_component")
	report.Summary["direct_evidence_rules"] = countEvidenceConfidence(report.Rules, EvidenceDirect)
	report.Summary["derived_evidence_rules"] = countEvidenceConfidence(report.Rules, EvidenceDerived)
	report.Summary["semantic_equivalences"] = len(report.Equivalences)
	report.Summary["encoder_coverage_gaps"] = len(report.Gaps)
	report.Summary["call_edges"] = len(report.CallEdges)
	report.Summary["encodable_machine_operations"] = len(report.EncodableOps)
	report.Summary["machine_classification_rules"] = countRelation(report.Rules, "machine_ir_classification")
	report.Summary["encoding_strategy_rules"] = countRelation(report.Rules, "encoding_strategy")
	return report, nil
}

func functionIdentity(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) == 0 {
		return fn.Name.Name
	}
	return exprString(fn.Recv.List[0].Type) + "." + fn.Name.Name
}

func exprString(e ast.Expr) string {
	switch x := e.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return "*" + exprString(x.X)
	case *ast.SelectorExpr:
		return exprString(x.X) + "." + x.Sel.Name
	case *ast.IndexExpr:
		return exprString(x.X) + "[" + exprString(x.Index) + "]"
	case *ast.ParenExpr:
		return exprString(x.X)
	case *ast.CallExpr:
		return exprString(x.Fun)
	default:
		return fmt.Sprintf("%T", e)
	}
}

func compilerRuleStringLiteral(e ast.Expr) (string, bool) {
	lit, ok := e.(*ast.BasicLit)
	if !ok || lit.Kind != goToken.STRING {
		return "", false
	}
	v, err := strconv.Unquote(lit.Value)
	return v, err == nil
}

func evidenceLocation(f nativeEvidenceFile, hash, symbol string, start, end goToken.Pos) CompilerEvidenceLocation {
	return CompilerEvidenceLocation{Repository: "local-working-tree", File: f.path, SHA256: hash, Symbol: symbol, StartLine: f.set.Position(start).Line, EndLine: f.set.Position(end).Line}
}

func collectEncoderEvidence(f nativeEvidenceFile, hash string, out map[string]CompilerEvidenceLocation) {
	ast.Inspect(f.ast, func(n ast.Node) bool {
		sw, ok := n.(*ast.SwitchStmt)
		if !ok || exprString(sw.Tag) != "in.Op" {
			return true
		}
		owner := ""
		for _, d := range f.ast.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if ok && fn.Body != nil && fn.Body.Pos() <= sw.Pos() && fn.Body.End() >= sw.End() {
				owner = functionIdentity(fn)
				break
			}
		}
		if owner != "encodeX64" {
			return true
		}
		for _, stmt := range sw.Body.List {
			cl, ok := stmt.(*ast.CaseClause)
			if !ok {
				continue
			}
			for _, e := range cl.List {
				if op, ok := compilerRuleStringLiteral(e); ok {
					out[op] = evidenceLocation(f, hash, owner, e.Pos(), e.End())
				}
			}
		}
		return true
	})
	// Direct string comparisons are alternate encoding arms in encodeX64.
	for _, decl := range f.ast.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "encodeX64" || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			b, ok := n.(*ast.BinaryExpr)
			if !ok || (b.Op != goToken.EQL && b.Op != goToken.NEQ) {
				return true
			}
			if exprString(b.X) == "in.Op" {
				if op, ok := compilerRuleStringLiteral(b.Y); ok {
					out[op] = evidenceLocation(f, hash, "encodeX64", b.Pos(), b.End())
				}
			}
			if exprString(b.Y) == "in.Op" {
				if op, ok := compilerRuleStringLiteral(b.X); ok {
					out[op] = evidenceLocation(f, hash, "encodeX64", b.Pos(), b.End())
				}
			}
			return true
		})
	}
	// Encoder opcode families are declared in tables consumed by the encoder.
	for _, decl := range f.ast.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok {
			continue
		}
		for _, spec := range gen.Specs {
			vs, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for i, name := range vs.Names {
				lower := strings.ToLower(name.Name)
				if !strings.Contains(lower, "opcode") && !strings.Contains(lower, "condition") && !strings.Contains(lower, "stencil") {
					continue
				}
				if i >= len(vs.Values) {
					continue
				}
				lit, ok := vs.Values[i].(*ast.CompositeLit)
				if !ok {
					continue
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					if op, ok := compilerRuleStringLiteral(kv.Key); ok {
						out[op] = evidenceLocation(f, hash, name.Name, kv.Pos(), kv.End())
					}
				}
			}
		}
	}
}

func collectNativeCallEdges(f nativeEvidenceFile, hash string, index map[string]goToken.Position, out *[]CompilerCallEdge) {
	seen := map[string]bool{}
	byName := map[string][]string{}
	for identity := range index {
		if i := strings.LastIndexByte(identity, '.'); i >= 0 {
			byName[identity[i+1:]] = append(byName[identity[i+1:]], identity)
		} else {
			byName[identity] = append(byName[identity], identity)
		}
	}
	for _, decl := range f.ast.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		from := functionIdentity(fn)
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			to := ""
			switch x := call.Fun.(type) {
			case *ast.Ident:
				to = x.Name
			case *ast.SelectorExpr:
				to = x.Sel.Name
			}
			candidates := byName[to]
			if len(candidates) != 1 {
				return true
			}
			identity := candidates[0]
			loc := evidenceLocation(f, hash, from+"->"+identity, call.Pos(), call.End())
			key := loc.Symbol + "@" + strconv.Itoa(loc.StartLine)
			if !seen[key] {
				seen[key] = true
				*out = append(*out, CompilerCallEdge{From: from, To: identity, Location: loc})
			}
			return true
		})
	}
}

func collectNativeRuleEvidence(f nativeEvidenceFile, hash string, encodable map[string]CompilerEvidenceLocation, report *NativeCompilerEvidenceReport) {
	for _, decl := range f.ast.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		owner := functionIdentity(fn)
		if !strings.HasPrefix(owner, "*x64Selector.") {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sw, ok := n.(*ast.SwitchStmt)
			if !ok {
				return true
			}
			tag := exprString(sw.Tag)
			prefix := ""
			stage := CompilerStageInstructionSelect
			switch tag {
			case "c.Kind":
				prefix = "uast.kind:"
			case "c.Operation.Operator":
				prefix = "semantic.operator:"
			case "c.Operation.Name":
				prefix = "semantic.operation:"
			default:
				return true
			}
			for _, stmt := range sw.Body.List {
				cl, ok := stmt.(*ast.CaseClause)
				if !ok || len(cl.List) == 0 {
					continue
				}
				var cases []string
				for _, e := range cl.List {
					if v, ok := compilerRuleStringLiteral(e); ok {
						cases = append(cases, v)
					}
				}
				if len(cases) == 0 {
					continue
				}
				ops := map[string]bool{}
				nested := false
				ast.Inspect(cl, func(child ast.Node) bool {
					if child != cl {
						switch child.(type) {
						case *ast.IfStmt, *ast.SwitchStmt, *ast.ForStmt:
							nested = true
						}
					}
					call, ok := child.(*ast.CallExpr)
					if !ok {
						return true
					}
					sel, ok := call.Fun.(*ast.SelectorExpr)
					if !ok || sel.Sel.Name != "emit" || len(call.Args) == 0 {
						return true
					}
					if op, ok := compilerRuleStringLiteral(call.Args[0]); ok {
						ops[op] = true
					}
					return true
				})
				if len(ops) == 0 {
					continue
				}
				outputs := make([]string, 0, len(ops))
				for op := range ops {
					outputs = append(outputs, op)
				}
				sort.Strings(outputs)
				for _, semanticCase := range cases {
					identity := prefix + semanticCase
					id := fmt.Sprintf("native.%s.%s.%s.%d", sanitizeEvidenceID(owner), sanitizeEvidenceID(tag), sanitizeEvidenceID(semanticCase), f.set.Position(cl.Pos()).Line)
					loc := evidenceLocation(f, hash, owner, cl.Pos(), cl.End())
					confidence := EvidenceDirect
					if nested || len(outputs) > 1 {
						confidence = EvidenceDerived
					}
					unencoded := []string{}
					for _, op := range outputs {
						if _, ok := encodable[op]; !ok {
							unencoded = append(unencoded, op)
						}
					}
					conditions := []string{"target_architecture=x86_64", "dispatch=" + tag + ":" + semanticCase}
					e := CompilerEvidence{ID: id, Compiler: report.Compiler, Version: report.Version, Stage: stage, TargetArchitecture: "x86_64", SemanticIdentity: identity, InputPattern: identity, Conditions: conditions, OutputPattern: outputs, ReferencedOperations: outputs, Relation: "lowering_component", Confidence: confidence, Locations: []CompilerEvidenceLocation{loc}}
					report.Rules = append(report.Rules, e)
					if len(unencoded) > 0 {
						report.Gaps = append(report.Gaps, CompilerEvidenceGap{ID: "gap." + id, Compiler: report.Compiler, SemanticIdentity: identity, Gap: "ENCODER_COVERAGE_UNCONFIRMED", Detail: "selector emits operations not found in the static encoder index: " + strings.Join(unencoded, ","), Location: loc})
					}
				}
			}
			return true
		})
	}
}

func collectEncoderRules(report *NativeCompilerEvidenceReport, encodable map[string]CompilerEvidenceLocation) {
	for _, op := range report.EncodableOps {
		loc := encodable[op]
		id := "native.encoding." + sanitizeEvidenceID(op)
		report.Rules = append(report.Rules, CompilerEvidence{
			ID: id, Compiler: report.Compiler, Version: report.Version,
			Stage: CompilerStageEncoding, TargetArchitecture: "x86_64",
			SemanticIdentity:     "machine.operation:" + op,
			InputPattern:         "x64.operation:" + op,
			Conditions:           []string{"operand_shape=validated_by_encodeX64"},
			OutputPattern:        []string{"x64.encoded_form:" + op},
			ReferencedOperations: []string{op}, Relation: "encoding_strategy",
			Confidence: EvidenceDirect, Locations: []CompilerEvidenceLocation{loc},
		})
	}
}

// collectMachineClassRules reads the product's explicit x64PrimitiveFor
// dispatch. This is an implementation classification used by MachineIR, not
// a claim that an x64 opcode is a canonical source-language primitive.
func collectMachineClassRules(files []nativeEvidenceFile, hashes map[string]string, report *NativeCompilerEvidenceReport) {
	for _, f := range files {
		for _, decl := range f.ast.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Name.Name != "x64PrimitiveFor" || fn.Body == nil {
				continue
			}
			for _, stmt := range fn.Body.List {
				sw, ok := stmt.(*ast.SwitchStmt)
				if !ok || exprString(sw.Tag) != "op" {
					continue
				}
				for _, branch := range sw.Body.List {
					cl, ok := branch.(*ast.CaseClause)
					if !ok || len(cl.List) == 0 {
						continue
					}
					var inputs []string
					for _, label := range cl.List {
						if op, ok := stringLiteral(label); ok {
							inputs = append(inputs, op)
						}
					}
					if len(inputs) == 0 {
						continue
					}
					outputs := map[string]bool{}
					ast.Inspect(cl, func(n ast.Node) bool {
						r, ok := n.(*ast.ReturnStmt)
						if !ok {
							return true
						}
						if len(r.Results) != 0 {
							if v, ok := stringLiteral(r.Results[0]); ok && v != "" {
								outputs[v] = true
							}
						}
						return true
					})
					if len(outputs) == 0 {
						continue
					}
					out := make([]string, 0, len(outputs))
					for v := range outputs {
						out = append(out, v)
					}
					sort.Strings(out)
					confidence := EvidenceDirect
					for _, input := range inputs {
						id := "native.machine-class." + sanitizeEvidenceID(input) + "." + strconv.Itoa(f.set.Position(cl.Pos()).Line)
						loc := evidenceLocation(f, hashes[f.path], "x64PrimitiveFor", cl.Pos(), cl.End())
						if len(out) > 1 {
							confidence = EvidenceDerived
						}
						report.Rules = append(report.Rules, CompilerEvidence{ID: id, Compiler: report.Compiler, Version: report.Version, Stage: CompilerStageInstructionSelect, TargetArchitecture: "x86_64", SemanticIdentity: "machine_ir.classification:" + strings.Join(out, "|"), InputPattern: "x64.operation:" + input, Conditions: []string{"classification=as implemented by x64PrimitiveFor"}, OutputPattern: out, Relation: "machine_ir_classification", Confidence: confidence, Locations: []CompilerEvidenceLocation{loc}})
					}
				}
			}
		}
	}
}

func countRelation(rows []CompilerEvidence, relation string) int {
	n := 0
	for _, row := range rows {
		if row.Relation == relation {
			n++
		}
	}
	return n
}

func sanitizeEvidenceID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

// WriteNativeCompilerEvidence emits stable evidence artifacts. These files
// are reports and do not constitute an executable rule registry.
func WriteNativeCompilerEvidence(report *NativeCompilerEvidenceReport, outDir string) error {
	if report == nil {
		return fmt.Errorf("nil native compiler evidence report")
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		return err
	}
	writeJSON := func(name string, v any) error {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(outDir, name), append(b, '\n'), 0644)
	}
	if err := writeJSON("compiler-evidence-index.json", map[string]any{"schema_version": report.SchemaVersion, "compiler": report.Compiler, "version": report.Version, "repository": report.Repository, "source_root": report.SourceRoot, "source_units_analyzed": report.Units, "functions_indexed": report.Functions, "source_files": report.SourceFiles}); err != nil {
		return err
	}
	if err := writeJSON("evidence-summary.json", map[string]any{"schema_version": report.SchemaVersion, "compiler": report.Compiler, "version": report.Version, "source_units_analyzed": report.Units, "functions_indexed": report.Functions, "rules_extracted": len(report.Rules), "direct_evidence_rules": countEvidenceConfidence(report.Rules, EvidenceDirect), "derived_evidence_rules": countEvidenceConfidence(report.Rules, EvidenceDerived), "semantic_equivalences": len(report.Equivalences), "semantic_gaps": len(report.Gaps), "call_edges": len(report.CallEdges), "encodable_machine_operations": len(report.EncodableOps)}); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "semantic-equivalences.jsonl"), report.Equivalences); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "semantic-gaps.jsonl"), report.Gaps); err != nil {
		return err
	}
	if err := writeJSONL(filepath.Join(outDir, "compiler-graph.jsonl"), report.CallEdges); err != nil {
		return err
	}
	file, err := os.Create(filepath.Join(outDir, "translation-rules.jsonl"))
	if err != nil {
		return err
	}
	enc := json.NewEncoder(file)
	for _, rule := range report.Rules {
		if err = enc.Encode(rule); err != nil {
			_ = file.Close()
			return err
		}
	}
	if err = file.Close(); err != nil {
		return err
	}
	md := fmt.Sprintf("# Native compiler evidence extraction\n\n- Compiler: `%s`\n- Source version: `%s` (per-file SHA-256 is recorded in every evidence row)\n- Source units analyzed: %d\n- Functions indexed: %d\n- Lowering-component rules: %d\n- Semantic equivalences: %d\n- Encoder coverage gaps: %d\n- Call edges: %d\n\nRules are evidence-only. They do not alter the native compiler, primitive registry, or LLVM path. Similar names are not semantic equivalence.\n", report.Compiler, report.Version, report.Units, report.Functions, len(report.Rules), len(report.Equivalences), len(report.Gaps), len(report.CallEdges))
	return os.WriteFile(filepath.Join(outDir, "extraction-report.md"), []byte(md), 0644)
}

func writeJSONL[T any](path string, rows []T) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(file)
	for _, row := range rows {
		if err := enc.Encode(row); err != nil {
			_ = file.Close()
			return err
		}
	}
	return file.Close()
}

func countEvidenceConfidence(rows []CompilerEvidence, c EvidenceConfidence) int {
	n := 0
	for _, r := range rows {
		if r.Confidence == c {
			n++
		}
	}
	return n
}

// CompileSourceFingerprint returns a stable digest of exact source bytes.
func CompileSourceFingerprint(source []byte) string {
	sum := sha256.Sum256(source)
	return hex.EncodeToString(sum[:])
}
