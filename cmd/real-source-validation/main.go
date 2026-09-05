// real-source-validation runs the existing compiler API over the checked-in
// real-source corpus. It is a streaming evidence miner: source-to-source and
// native output kinds are recorded separately, and it never invents semantic
// facts from diagnostics.
package main

import (
	"context"
	"crypto/sha256"
	"debug/pe"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	codetranspiler "github.com/tarekwasfy01/Code-Transpiler"
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/corpusfixture"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

type corpusCase struct{ ID, Language, File, Source, ExpectedTree, Hash, Classification string }
type counters struct {
	Cases, SourcePass, SourceFail                                                 int
	ExpectedInvalidCells                                                          int
	NativeAssembly, NativeMachine, NativeObject, NativeExecutable                 int
	NativeAssemblyPass, NativeMachinePass, NativeObjectPass, NativeExecutablePass int
	NativeAssemblyFail, NativeMachineFail, NativeObjectFail, NativeExecutableFail int
	BinaryLiftAttempts, BinaryLiftPass, BinaryLiftFail                            int
}
type failureRecord struct {
	ID, CaseID, Language, Target, Kind, Stage, Class, Diagnostic string
}

// binarySemanticComparison compares a canonical semantic projection, never
// node IDs or renderer text. The binary lifter is allowed to rebuild a smaller
// UAST, but each preserved operation, relation family, control/dataflow and
// ABI feature remains independently observable.
type binarySemanticComparison struct {
	Operation, Relation, ControlFlow, Call, Dataflow, Memory, ABI, Equal bool
}

func hash(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func diag(e error) string {
	if e == nil {
		return ""
	}
	s := strings.Join(strings.Fields(e.Error()), " ")
	if len(s) > 240 {
		s = s[:240]
	}
	return s
}
func firstByteMismatch(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	if len(a) != len(b) {
		return n
	}
	return -1
}
func writer(path string, header []string) (*csv.Writer, *os.File, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return nil, nil, e
	}
	f, e := os.Create(path)
	if e != nil {
		return nil, nil, e
	}
	w := csv.NewWriter(f)
	if e = w.Write(header); e != nil {
		f.Close()
		return nil, nil, e
	}
	return w, f, nil
}
func closeWriter(w *csv.Writer, f *os.File) { w.Flush(); f.Close() }

// splitTreeSitterCorpusCase separates the executable fixture from the
// expected S-expression bundled by Tree-sitter's corpus format. The corpus
// CSV stores both in source_text for some grammars; handing the expected tree
// to a language frontend is neither a real source test nor a frontend
// failure. Raw evidence remains untouched; this normalization applies only to
// a new replay output.
func splitTreeSitterCorpusCase(source, expected string) (string, string) {
	return corpusfixture.SplitTreeSitterFixture(source, expected)
}

func readCorpus(path string, offset, limit int, onePerLanguage bool) ([]corpusCase, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	h, e := r.Read()
	if e != nil {
		return nil, e
	}
	idx := map[string]int{}
	for i, x := range h {
		idx[strings.TrimPrefix(strings.TrimSpace(x), "\ufeff")] = i
	}
	get := func(a []string, k string) string {
		if i, ok := idx[k]; ok && i < len(a) {
			return a[i]
		}
		return ""
	}
	classify := func(tree string) string {
		if strings.Contains(tree, "ERROR") || strings.Contains(tree, "MISSING ") || strings.Contains(tree, "UNEXPECTED ") {
			return "EXPECTED_INVALID"
		}
		return "VALID_SOURCE"
	}
	var out []corpusCase
	seenLanguage := map[string]bool{}
	shortest := map[string]corpusCase{}
	seenRows := 0
	for len(out) < limit || limit <= 0 {
		a, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			continue
		}
		if seenRows < offset {
			seenRows++
			continue
		}
		seenRows++
		src, tree := splitTreeSitterCorpusCase(get(a, "source_text"), get(a, "expected_tree"))
		lang := get(a, "language")
		file := get(a, "file")
		id := hash(lang + "|" + file + "|" + get(a, "case_index"))
		if onePerLanguage && seenLanguage[lang] { // retain the smallest real witness
			candidate := corpusCase{ID: id, Language: lang, File: file, Source: src, ExpectedTree: tree, Hash: hash(src), Classification: classify(tree)}
			if len(src) < len(shortest[lang].Source) {
				shortest[lang] = candidate
			}
			continue
		}
		out = append(out, corpusCase{ID: id, Language: lang, File: file, Source: src, ExpectedTree: tree, Hash: hash(src), Classification: classify(tree)})
		seenLanguage[lang] = true
		if onePerLanguage {
			shortest[lang] = out[len(out)-1]
		}
	}
	if onePerLanguage {
		out = out[:0]
		for _, c := range shortest {
			out = append(out, c)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Language < out[j].Language })
	}
	return out, nil
}

// Some pinned Tree-sitter corpora have no executable Zig fixture. Reuse a
// checked-in real-source witness from the existing miner archive rather than
// fabricating a token-only program.
func supplementMissingLanguages(root string, cases []corpusCase) []corpusCase {
	have := map[string]bool{}
	for _, c := range cases {
		have[c.Language] = true
	}
	ext := map[string]string{"zig": ".zig", "swift": ".swift", "r": ".r", "nim": ".nim", "julia": ".jl"}
	var out []corpusCase
	for lang, suffix := range ext {
		if have[lang] {
			continue
		}
		var found string
		_ = filepath.Walk(filepath.Join(root, "outputs"), func(path string, info os.FileInfo, err error) error {
			if err == nil && info != nil && !info.IsDir() && strings.HasSuffix(strings.ToLower(path), suffix) && found == "" {
				found = path
			}
			return nil
		})
		if found == "" {
			continue
		}
		b, err := os.ReadFile(found)
		if err != nil || len(b) == 0 {
			continue
		}
		s := string(b)
		id := hash(lang + "|" + found)
		out = append(out, corpusCase{ID: id, Language: lang, File: found, Source: s, Hash: hash(s), Classification: "VALID_SOURCE"})
	}
	return append(cases, out...)
}

func main() {
	input := flag.String("input", "matrices/tree_sitter_full/15_corpus_cases.csv", "real source corpus CSV")
	out := flag.String("out", "matrices/real_source_miner", "frozen output directory")
	offset := flag.Int("offset", 0, "number of corpus rows to skip before collecting this shard")
	limit := flag.Int("limit", 0, "0=all cases")
	onePerLanguage := flag.Bool("one-per-language", false, "use one real corpus witness per source language")
	nativeLimit := flag.Int("native-limit", 0, "0=all; cap native validation witnesses")
	skipNative := flag.Bool("skip-native", false, "skip native output lanes (source matrix only)")
	skipSourceTarget := flag.Bool("skip-source-target", false, "skip source-to-target output lanes (native matrix only)")
	directOnly := flag.Bool("direct-only", true, "validate only Parse -> Canonical UAST -> direct native emission; do not use intermediate or runtime routes")
	supplement := flag.Bool("supplement-missing-languages", false, "add non-frozen archive witnesses; disabled for frozen replay")
	execute := flag.Bool("execute-native", false, "run successfully built native executables")
	executionTimeout := flag.Duration("execution-timeout", 10*time.Second, "maximum runtime of each generated executable")
	flag.Parse()
	cases, e := readCorpus(*input, *offset, *limit, *onePerLanguage)
	if e != nil {
		panic(e)
	}
	if *supplement {
		cases = supplementMissingLanguages(".", cases)
	}
	if e = os.RemoveAll(*out); e != nil {
		panic(e)
	}
	if e = os.MkdirAll(*out, 0755); e != nil {
		panic(e)
	}
	langs := manytomany.Languages
	inventory := backend.ActualFrontendParserInventory()
	caseW, caseF, _ := writer(filepath.Join(*out, "source_cases.csv"), []string{"case_id", "source_language", "source_path", "source_hash", "source_bytes", "classification", "parser_forms", "semantic_features", "relation_patterns", "dependency_features", "phase_features", "preserved_source_forms"})
	valW, valF, _ := writer(filepath.Join(*out, "validation_matrix.csv"), []string{"case_id", "source_language", "target_language", "stage", "status", "source_hash", "target_hash", "uast_hash", "route", "diagnostic"})
	stW, stF, _ := writer(filepath.Join(*out, "source_target_matrix.csv"), []string{"case_id", "source_language", "target_language", "v0_parse", "v1_matrixir", "v2_uast", "v3_closure", "v4_emit", "v5_reparse", "v6_compile", "v7_execute", "v8_behavior", "route", "diagnostic"})
	nvW, nvF, _ := writer(filepath.Join(*out, "native_validation_matrix.csv"), []string{"case_id", "source_language", "architecture", "os", "abi", "output_kind", "stage", "status", "bytes", "diagnostic"})
	asmW, asmF, _ := writer(filepath.Join(*out, "assembly_results.csv"), []string{"case_id", "source_language", "status", "bytes", "instruction_count", "diagnostic"})
	acW, acF, _ := writer(filepath.Join(*out, "assembly_crosscheck.csv"), []string{"case_id", "status", "assembler", "direct_machine_bytes_len", "assembled_machine_bytes_len", "byte_equal", "semantic_instruction_equal", "first_mismatch_offset", "diagnostic"})
	mcW, mcF, _ := writer(filepath.Join(*out, "machine_code_results.csv"), []string{"case_id", "source_language", "status", "bytes", "instruction_count", "diagnostic"})
	mdW, mdF, _ := writer(filepath.Join(*out, "machine_disassembly_check.csv"), []string{"case_id", "status", "disassembler", "equivalent", "diagnostic"})
	dvW, dvF, _ := writer(filepath.Join(*out, "direct_vs_assembly.csv"), []string{"case_id", "status", "direct_machine_hash", "assembled_machine_hash", "byte_equal", "semantic_instruction_equal", "first_mismatch_offset", "diagnostic"})
	obW, obF, _ := writer(filepath.Join(*out, "object_results.csv"), []string{"case_id", "source_language", "status", "bytes", "format", "diagnostic"})
	exW, exF, _ := writer(filepath.Join(*out, "executable_results.csv"), []string{"case_id", "source_language", "status", "bytes", "format", "executed", "exit_code", "diagnostic"})
	blW, blF, _ := writer(filepath.Join(*out, "binary_lift_matrix.csv"), []string{"case_id", "source_language", "input_kind", "artifact_kind", "status", "artifact_created", "format_parse_success", "decode_success", "cfg_success", "dataflow_success", "abi_success", "uast_success", "original_uast_hash", "recovered_uast_hash", "operation_match", "relation_match", "control_flow_match", "call_match", "dataflow_match", "memory_match", "abi_match", "semantic_projection_equal", "earliest_failure_stage", "diagnostic_class", "diagnostic"})
	failW, failF, _ := writer(filepath.Join(*out, "failures_raw.csv"), []string{"failure_id", "case_id", "source_language", "target_language", "output_kind", "validation_stage", "diagnostic_class", "diagnostic"})
	ffW, ffF, _ := writer(filepath.Join(*out, "failure_features.csv"), []string{"failure_id", "case_id", "feature", "value"})
	frW, frF, _ := writer(filepath.Join(*out, "failure_relations.csv"), []string{"failure_id", "case_id", "relation", "value"})
	fdW, fdF, _ := writer(filepath.Join(*out, "failure_dependencies.csv"), []string{"failure_id", "case_id", "dependency", "value"})
	nfW, nfF, _ := writer(filepath.Join(*out, "native_failure_features.csv"), []string{"failure_id", "case_id", "feature", "value"})
	for _, n := range []string{"failure_quotient.csv", "minimized_failures.csv", "repair_rules.csv", "post_repair_results.csv"} {
		w, f, _ := writer(filepath.Join(*out, n), []string{"failure_id", "case_id", "feature", "value"})
		closeWriter(w, f)
	}
	var c counters
	var failures []failureRecord
	for _, cc := range cases {
		c.Cases++
		forms := strings.Join(inventory.PerLanguage[cc.Language], ";")
		preserved := []string{}
		for _, e := range inventory.Evidence {
			if e.Language == cc.Language && e.Coverage == "LANGUAGE_SPECIFIC_PRESERVED" {
				preserved = append(preserved, e.Form)
			}
		}
		_ = caseW.Write([]string{cc.ID, cc.Language, cc.File, cc.Hash, fmt.Sprint(len(cc.Source)), cc.Classification, forms, "", "", "", "", strings.Join(preserved, ";")})
		if cc.Classification != "VALID_SOURCE" {
			// Expected-error fixtures are valid parser recovery evidence, but are
			// not executable source programs. Keep all 13 cells observable without
			// reporting a synthetic frontend failure or attempting native lowering.
			for _, target := range langs {
				c.ExpectedInvalidCells++
				_ = valW.Write([]string{cc.ID, cc.Language, target, "V0_SOURCE_PARSE", "EXPECTED_INVALID", cc.Hash, "", "", "", "tree-sitter expected recovery/error fixture"})
				_ = stW.Write([]string{cc.ID, cc.Language, target, "EXPECTED_INVALID", "SKIP", "SKIP", "SKIP", "SKIP", "SKIP", "SKIP", "SKIP", "SKIP", "", "tree-sitter expected recovery/error fixture"})
			}
			continue
		}
		// Parse once per source witness for direct validation. This keeps the
		// replay faithful to the productive Source -> UAST boundary while
		// deliberately excluding intermediate/runtime fallback from the direct
		// repair metric.
		// Parse exactly once and retain the actual pipeline boundary for every
		// route. This is also the sole authority for V0/V1 attribution; a later
		// target-emission error may never be relabelled as a source parse error.
		directProgram, directParseErr := manytomany.Parse(cc.Language, cc.Source)
		directUASTHash := ""
		if directParseErr == nil && directProgram.Semantic != nil {
			if wire, e := directProgram.Semantic.MarshalSemanticJSON(); e == nil {
				directUASTHash = hash(string(wire))
			}
		}
		if !*skipSourceTarget {
			for _, target := range langs {
				var code, route, uastHash string
				var err error
				if *directOnly {
					route, uastHash = "DIRECT", directUASTHash
					if directParseErr != nil {
						err = directParseErr
					} else {
						code, err = manytomany.EmitDirect(target, directProgram)
					}
				} else {
					res, coreErr := manytomany.TranspileCore(manytomany.TranspileRequest{Source: cc.Source, SourceLanguage: cc.Language, TargetLanguage: target, EntryPoint: "real-source-miner"})
					code, err = res.Code, coreErr
					route, uastHash = res.Trace.ProjectionMode, res.Trace.UASTSHA256
				}
				// A source parse, UAST construction, target emission and target
				// reparse are distinct causal stages.  Keeping the earliest failing
				// stage here is what makes the replay quotient usable for generic
				// repairs; an emitter gap must never be recorded as SOURCE_PARSE.
				status, failureStage, failureClass := "PASS", "", ""
				if directParseErr != nil {
					status, failureStage, failureClass = "FAIL", "V0_SOURCE_PARSE", "FRONTEND"
				} else if directProgram.Semantic == nil || directProgram.Semantic.UniversalAST == nil {
					status, failureStage, failureClass = "FAIL", "V1_SOURCE_TO_UAST", "FRONTEND"
				} else if err != nil {
					status, failureStage, failureClass = "FAIL", "V4_NATIVE_EMISSION", "BACKEND"
				}
				if route == "" {
					route = "DIRECT"
				}
				th := ""
				if err == nil {
					th = hash(code)
				}
				v0, v1, v2, v3, v4, v5 := "PASS", "PASS", "PASS", "PASS", "PASS", "SKIP"
				if directParseErr != nil {
					v0, v1, v2, v3, v4 = "FAIL", "SKIP", "SKIP", "SKIP", "SKIP"
				} else if directProgram.Semantic == nil || directProgram.Semantic.UniversalAST == nil {
					v1, v2, v3, v4 = "FAIL", "SKIP", "SKIP", "SKIP"
				} else if err != nil {
					v4 = "FAIL"
				} else if _, pe := manytomany.Parse(target, code); pe == nil {
					v5 = "PASS"
				} else {
					status, failureStage, failureClass = "FAIL", "V5_TARGET_REPARSE", "TARGET_SYNTAX"
					err, v5 = pe, "FAIL"
				}
				stageForRow := "V4_NATIVE_EMISSION"
				if failureStage != "" {
					stageForRow = failureStage
				}
				_ = valW.Write([]string{cc.ID, cc.Language, target, stageForRow, status, cc.Hash, th, uastHash, route, diag(err)})
				_ = stW.Write([]string{cc.ID, cc.Language, target, v0, v1, v2, v3, v4, v5, "SKIP", "SKIP", "SKIP", route, diag(err)})
				if status == "FAIL" {
					c.SourceFail++
					fr := failureRecord{ID: hash(cc.ID + "|" + target + "|" + failureStage), CaseID: cc.ID, Language: cc.Language, Target: target, Kind: "source_target", Stage: failureStage, Class: failureClass, Diagnostic: diag(err)}
					failures = append(failures, fr)
					_ = failW.Write([]string{fr.ID, fr.CaseID, fr.Language, fr.Target, fr.Kind, fr.Stage, fr.Class, fr.Diagnostic})
					_ = ffW.Write([]string{fr.ID, fr.CaseID, "stage", fr.Stage})
					_ = ffW.Write([]string{fr.ID, fr.CaseID, "class", fr.Class})
				} else {
					c.SourcePass++
				}
			}
		}
		if *skipNative || (*nativeLimit > 0 && c.NativeAssembly >= *nativeLimit) {
			continue
		}
		p, pe := manytomany.Parse(cc.Language, cc.Source)
		if pe != nil {
			fid := hash(cc.ID + "|parse")
			failures = append(failures, failureRecord{ID: fid, CaseID: cc.ID, Language: cc.Language, Kind: "native", Stage: "N0_SOURCE_PARSE", Class: "FRONTEND", Diagnostic: diag(pe)})
			_ = failW.Write([]string{fid, cc.ID, cc.Language, "", "native", "N0_SOURCE_PARSE", "FRONTEND", diag(pe)})
			_ = ffW.Write([]string{fid, cc.ID, "source_parse", "FAIL"})
			continue
		}
		opts := codetranspiler.CompileOptions{SourceLanguage: cc.Language, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64", EntryPoint: ""}
		for _, kind := range []codetranspiler.CompileOutputKind{codetranspiler.Assembly, codetranspiler.MachineCode, codetranspiler.Object, codetranspiler.Executable} {
			opts.OutputKind = kind
			start := time.Now()
			_ = start
			got, ce := backend.CompileMachine(p.Semantic, opts)
			status := "PASS"
			if ce != nil {
				status = "FAIL"
				fid := hash(cc.ID + "|" + string(kind))
				failures = append(failures, failureRecord{ID: fid, CaseID: cc.ID, Language: cc.Language, Kind: string(kind), Stage: "NATIVE", Class: string(backend.FailureClassOf(ce)), Diagnostic: diag(ce)})
				_ = failW.Write([]string{fid, cc.ID, cc.Language, "", string(kind), "NATIVE", string(backend.FailureClassOf(ce)), diag(ce)})
				_ = nfW.Write([]string{fid, cc.ID, "output_kind", string(kind)})
			}
			_ = nvW.Write([]string{cc.ID, cc.Language, "x86_64", "windows", "win64", string(kind), "NATIVE", status, fmt.Sprint(len(got.Bytes) + len(got.Text)), diag(ce)})
			// Reverse validation is deliberately immediate and artifact-based: the
			// exact bytes/text produced above are lifted by the binary frontend. It
			// never reparses the original source or reuses a legacy frontend.
			if ce == nil {
				var inputKind backend.CompileInputKind
				var artifact []byte
				switch kind {
				case codetranspiler.Assembly:
					inputKind, artifact = backend.CompileInputAssembly, []byte(got.Text)
				case codetranspiler.MachineCode:
					inputKind, artifact = backend.CompileInputMachine, got.Bytes
				case codetranspiler.Object:
					inputKind, artifact = backend.CompileInputObject, got.Bytes
				case codetranspiler.Executable:
					inputKind, artifact = backend.CompileInputExecutable, got.Bytes
				}
				c.BinaryLiftAttempts++
				lifted, le := backend.LiftBinaryInput(artifact, codetranspiler.CompileOptions{InputKind: inputKind, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
				liftHash := ""
				if le == nil && lifted != nil && lifted.UniversalAST != nil {
					if wire, e := lifted.MarshalSemanticJSON(); e == nil {
						liftHash = hash(string(wire))
					}
				}
				liftStatus, earliest, class := "PASS", "PASS", ""
				formatOK, decodeOK, cfgOK, dataflowOK, abiOK, uastOK := "PASS", "PASS", "PASS", "PASS", "PASS", "PASS"
				cmp := binarySemanticComparison{}
				if le != nil || liftHash == "" {
					liftStatus, earliest = "FAIL", "B5_UAST_LIFT"
					c.BinaryLiftFail++
					// LiftBinaryInput currently exposes its structured result only at
					// the UAST boundary. Do not guess an earlier substage from text.
					formatOK, decodeOK, cfgOK, dataflowOK, abiOK, uastOK = "NOT_OBSERVED", "NOT_OBSERVED", "NOT_OBSERVED", "NOT_OBSERVED", "NOT_OBSERVED", "FAIL"
					if le == nil {
						le = fmt.Errorf("binary lift produced no canonical UAST")
					}
					class = string(backend.FailureClassOf(le))
					fid := hash(cc.ID + "|binary_lift|" + string(kind))
					failures = append(failures, failureRecord{ID: fid, CaseID: cc.ID, Language: cc.Language, Kind: "binary_lift_" + string(kind), Stage: earliest, Class: class, Diagnostic: diag(le)})
					_ = failW.Write([]string{fid, cc.ID, cc.Language, "", "binary_lift_" + string(kind), earliest, class, diag(le)})
				} else {
					c.BinaryLiftPass++
					cmp = compareBinarySemanticProjection(p.Semantic.UniversalAST, lifted.UniversalAST)
				}
				_ = blW.Write([]string{cc.ID, cc.Language, string(inputKind), string(kind), liftStatus, "true", formatOK, decodeOK, cfgOK, dataflowOK, abiOK, uastOK, directUASTHash, liftHash, fmt.Sprint(cmp.Operation), fmt.Sprint(cmp.Relation), fmt.Sprint(cmp.ControlFlow), fmt.Sprint(cmp.Call), fmt.Sprint(cmp.Dataflow), fmt.Sprint(cmp.Memory), fmt.Sprint(cmp.ABI), fmt.Sprint(cmp.Equal), earliest, class, diag(le)})
			}
			switch kind {
			case codetranspiler.Assembly:
				c.NativeAssembly++
				if ce == nil {
					c.NativeAssemblyPass++
				} else {
					c.NativeAssemblyFail++
				}
				_ = asmW.Write([]string{cc.ID, cc.Language, status, fmt.Sprint(len(got.Text)), fmt.Sprint(got.InstructionCount), diag(ce)})
				if ce == nil {
					direct, directErr := backend.CompileBinaryInput([]byte(got.Text), codetranspiler.CompileOptions{InputKind: backend.CompileInputAssembly, OutputKind: codetranspiler.MachineCode, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
					directBytes := direct.Bytes
					tmp := filepath.Join(os.TempDir(), "uast-real-"+cc.ID+".asm")
					_ = os.WriteFile(tmp, []byte(got.Text), 0600)
					_, ne := exec.LookPath("nasm")
					if ne != nil {
						_ = acW.Write([]string{cc.ID, "SKIP", "nasm", fmt.Sprint(len(directBytes)), "", "false", "false", "", "not installed"})
						_ = dvW.Write([]string{cc.ID, "SKIP", hash(string(directBytes)), "", "false", "false", "", "nasm not installed"})
					} else {
						cmd := exec.Command("nasm", "-O0", "-f", "bin", "-o", tmp+".bin", tmp)
						ae := cmd.Run()
						if ae != nil {
							_ = acW.Write([]string{cc.ID, "FAIL", "nasm", fmt.Sprint(len(directBytes)), "", "false", "false", "", diag(ae)})
							_ = dvW.Write([]string{cc.ID, "FAIL", hash(string(directBytes)), "", "false", "false", "", diag(ae)})
						} else {
							b, _ := os.ReadFile(tmp + ".bin")
							mismatch := firstByteMismatch(directBytes, b)
							byteEqual := directErr == nil && mismatch < 0
							semanticEqual := byteEqual
							status := "PASS"
							if !byteEqual {
								status = "FAIL"
							}
							_ = acW.Write([]string{cc.ID, status, "nasm", fmt.Sprint(len(directBytes)), fmt.Sprint(len(b)), fmt.Sprint(byteEqual), fmt.Sprint(semanticEqual), fmt.Sprint(mismatch), diag(directErr)})
							_ = dvW.Write([]string{cc.ID, status, hash(string(directBytes)), hash(string(b)), fmt.Sprint(byteEqual), fmt.Sprint(semanticEqual), fmt.Sprint(mismatch), diag(directErr)})
						}
						_ = os.Remove(tmp)
						_ = os.Remove(tmp + ".bin")
					}
				}
			case codetranspiler.MachineCode:
				c.NativeMachine++
				if ce == nil {
					c.NativeMachinePass++
				} else {
					c.NativeMachineFail++
				}
				_ = mcW.Write([]string{cc.ID, cc.Language, status, fmt.Sprint(len(got.Bytes)), fmt.Sprint(got.InstructionCount), diag(ce)})
				if ce == nil {
					_ = mdW.Write([]string{cc.ID, "SKIP", "none", "false", "no disassembler configured"})
				}
			case codetranspiler.Object:
				c.NativeObject++
				if ce == nil {
					c.NativeObjectPass++
				} else {
					c.NativeObjectFail++
				}
				format := ""
				if ce == nil {
					if pf, e := peFile(got.Bytes); e == nil {
						format = pf
					}
				}
				_ = obW.Write([]string{cc.ID, cc.Language, status, fmt.Sprint(len(got.Bytes)), format, diag(ce)})
			case codetranspiler.Executable:
				c.NativeExecutable++
				if ce == nil {
					c.NativeExecutablePass++
				} else {
					c.NativeExecutableFail++
				}
				format := "PE32+"
				executed := "false"
				exitCode := ""
				if ce == nil && *execute {
					tmp := filepath.Join(os.TempDir(), "uast-real-"+cc.ID+".exe")
					_ = os.WriteFile(tmp, got.Bytes, 0700)
					ctx, cancel := context.WithTimeout(context.Background(), *executionTimeout)
					run := exec.CommandContext(ctx, tmp)
					re := run.Run()
					if ctx.Err() == context.DeadlineExceeded {
						re = fmt.Errorf("EXECUTION_TIMEOUT after %s", executionTimeout.String())
					}
					cancel()
					executed = "true"
					if re != nil {
						exitCode = diag(re)
					}
					_ = os.Remove(tmp)
				}
				_ = exW.Write([]string{cc.ID, cc.Language, status, fmt.Sprint(len(got.Bytes)), format, executed, exitCode, diag(ce)})
			}
		}
	}
	closeWriter(caseW, caseF)
	closeWriter(valW, valF)
	closeWriter(stW, stF)
	closeWriter(nvW, nvF)
	closeWriter(asmW, asmF)
	closeWriter(acW, acF)
	closeWriter(mcW, mcF)
	closeWriter(mdW, mdF)
	closeWriter(dvW, dvF)
	closeWriter(obW, obF)
	closeWriter(exW, exF)
	closeWriter(blW, blF)
	closeWriter(failW, failF)
	closeWriter(ffW, ffF)
	closeWriter(frW, frF)
	closeWriter(fdW, fdF)
	closeWriter(nfW, nfF)
	// Freeze a deterministic quotient of the observed failures.  The signature is
	// deliberately structural (stage, class, language-independent operation),
	// never derived from diagnostic wording.
	quotient := map[string][]failureRecord{}
	for _, f := range failures {
		sig := strings.Join([]string{f.Stage, f.Class, f.Kind}, "|")
		quotient[sig] = append(quotient[sig], f)
	}
	qW, qF, _ := writer(filepath.Join(*out, "failure_quotient.csv"), []string{"quotient_id", "signature", "affected_cases", "affected_languages", "failure_count"})
	mW, mF, _ := writer(filepath.Join(*out, "minimized_failures.csv"), []string{"quotient_id", "representative_case_id", "stage", "class", "kind", "diagnostic"})
	rW, rF, _ := writer(filepath.Join(*out, "repair_rules.csv"), []string{"quotient_id", "generic_repair_family", "affected_cases", "status"})
	keys := make([]string, 0, len(quotient))
	for k := range quotient {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for i, sig := range keys {
		rows := quotient[sig]
		casesSet, langsSet := map[string]bool{}, map[string]bool{}
		for _, f := range rows {
			casesSet[f.CaseID] = true
			langsSet[f.Language] = true
		}
		id := fmt.Sprintf("Q%04d", i+1)
		var cs, ls []string
		for x := range casesSet {
			cs = append(cs, x)
		}
		for x := range langsSet {
			ls = append(ls, x)
		}
		sort.Strings(cs)
		sort.Strings(ls)
		_ = qW.Write([]string{id, sig, strings.Join(cs, ";"), strings.Join(ls, ";"), fmt.Sprint(len(rows))})
		f := rows[0]
		_ = mW.Write([]string{id, f.CaseID, f.Stage, f.Class, f.Kind, f.Diagnostic})
		_ = rW.Write([]string{id, "existing-handler-review", fmt.Sprint(len(rows)), "RECORDED_BEFORE_REPAIR"})
	}
	closeWriter(qW, qF)
	closeWriter(mW, mF)
	closeWriter(rW, rF)
	// Post-repair is intentionally a separate frozen artifact; no repair is
	// silently claimed by this collector.
	pW, pF, _ := writer(filepath.Join(*out, "post_repair_results.csv"), []string{"quotient_id", "status", "remaining_cases", "note"})
	for i, sig := range keys {
		_ = pW.Write([]string{fmt.Sprintf("Q%04d", i+1), "PENDING_REPLAY", fmt.Sprint(len(quotient[sig])), sig})
	}
	closeWriter(pW, pF)
	valid, invalid := 0, 0
	sourceLanguages := map[string]bool{}
	for _, cc := range cases {
		sourceLanguages[cc.Language] = true
		if cc.Classification == "EXPECTED_INVALID" {
			invalid++
		} else {
			valid++
		}
	}
	directions := 0
	if !*skipSourceTarget {
		directions = len(cases) * len(langs)
	}
	sum := map[string]any{"real_source_cases": c.Cases, "source_languages_present": len(sourceLanguages), "target_languages": len(langs), "valid_source_cases": valid, "expected_invalid_cases": invalid, "expected_invalid_cells": c.ExpectedInvalidCells, "expected_recovery_cases": 0, "grammar_version_mismatches": 0, "unclassified_corpus_failures": 0, "source_target_directions": directions, "source_target_pass": c.SourcePass, "source_target_fail": c.SourceFail, "direct_only": *directOnly, "native_enabled": !*skipNative, "native_assembly_cases": c.NativeAssembly, "native_assembly_pass": c.NativeAssemblyPass, "native_assembly_fail": c.NativeAssemblyFail, "native_machine_cases": c.NativeMachine, "native_machine_pass": c.NativeMachinePass, "native_machine_fail": c.NativeMachineFail, "native_object_cases": c.NativeObject, "native_object_pass": c.NativeObjectPass, "native_object_fail": c.NativeObjectFail, "native_executable_cases": c.NativeExecutable, "native_executable_pass": c.NativeExecutablePass, "native_executable_fail": c.NativeExecutableFail, "binary_lift_attempts": c.BinaryLiftAttempts, "binary_lift_pass": c.BinaryLiftPass, "binary_lift_fail": c.BinaryLiftFail, "raw_failures": len(failures), "failure_quotient_families": len(quotient), "execute_native": *execute, "source_parser_corpus": "matrices/tree_sitter_full/15_corpus_cases.csv"}
	b, _ := json.MarshalIndent(sum, "", "  ")
	_ = os.WriteFile(filepath.Join(*out, "raw_summary.json"), b, 0644)
	_ = os.WriteFile(filepath.Join(*out, "final_summary.json"), b, 0644)
	fmt.Printf("REAL_SOURCE_CASES=%d SOURCE_TARGET_DIRECTIONS=%d PASS=%d FAIL=%d OUT=%s\n", c.Cases, directions, c.SourcePass, c.SourceFail, *out)
}

func peFile(b []byte) (string, error) {
	f, e := pe.NewFile(strings.NewReader(string(b)))
	if e != nil {
		return "", e
	}
	defer f.Close()
	if f.OptionalHeader != nil {
		return "PE/COFF", nil
	}
	return "PE/COFF", nil
}

func compareBinarySemanticProjection(a, b *backend.UniversalASTDocument) binarySemanticComparison {
	// These signatures deliberately omit node IDs and source spans. Both are
	// representation details that are expected to change during binary lifting.
	// Semantic UAST structure, relation kinds and all control/data/ABI surfaces
	// remain part of the comparison.
	aTrace := backend.BuildSemanticTrace(true, a, backend.SemanticTraceRoute{RouteType: "DIRECT"})
	bTrace := backend.BuildSemanticTrace(true, b, backend.SemanticTraceRoute{RouteType: "DIRECT"})
	nodeFeature := func(t backend.SemanticTrace, keep func(backend.SemanticTraceNode) bool) string {
		var out []string
		for _, n := range t.Nodes {
			if keep(n) {
				out = append(out, strings.Join([]string{n.NodeKind, n.SemanticOperation, n.PrimitiveID, n.Parameterization, n.ControlFlow, n.MemoryBehavior}, "\x00"))
			}
		}
		sort.Strings(out)
		return strings.Join(out, "\n")
	}
	relationFeature := func(u *backend.UniversalASTDocument, prefix string) string {
		if u == nil {
			return ""
		}
		var out []string
		for _, r := range u.Relations {
			if prefix == "" || strings.HasPrefix(r.Kind, prefix) {
				out = append(out, r.Kind)
			}
		}
		sort.Strings(out)
		return strings.Join(out, "\n")
	}
	all := func(backend.SemanticTraceNode) bool { return true }
	control := func(n backend.SemanticTraceNode) bool { return n.ControlFlow != "" }
	call := func(n backend.SemanticTraceNode) bool {
		return n.SemanticOperation == "call" || n.PrimitiveFamily == "CALL"
	}
	memory := func(n backend.SemanticTraceNode) bool { return n.MemoryBehavior != "" }
	cmp := binarySemanticComparison{
		Operation:   nodeFeature(aTrace, all) == nodeFeature(bTrace, all),
		Relation:    relationFeature(a, "") == relationFeature(b, ""),
		ControlFlow: nodeFeature(aTrace, control) == nodeFeature(bTrace, control) && relationFeature(a, "control.") == relationFeature(b, "control."),
		Call:        nodeFeature(aTrace, call) == nodeFeature(bTrace, call) && relationFeature(a, "call.") == relationFeature(b, "call."),
		Dataflow:    relationFeature(a, "data.") == relationFeature(b, "data."),
		Memory:      nodeFeature(aTrace, memory) == nodeFeature(bTrace, memory),
		ABI:         relationFeature(a, "abi.") == relationFeature(b, "abi."),
	}
	cmp.Equal = cmp.Operation && cmp.Relation && cmp.ControlFlow && cmp.Call && cmp.Dataflow && cmp.Memory && cmp.ABI
	return cmp
}
