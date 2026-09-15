// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	codetranspiler "github.com/tarekwasfy01/Code-Transpiler"
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
)

func compileNative(args []string) error {
	fs := flag.NewFlagSet("compile", flag.ContinueOnError)
	source := fs.String("source", "go", "source language")
	target := fs.String("target", "native-x86_64-windows", "native-x86_64-windows, machine-x86_64, object-x86_64-windows, asm-x86_64")
	out := fs.String("o", "", "output file (required for binary output)")
	inputKind := fs.String("input", "source", "source|assembly|machine|object|executable")
	outputKind := fs.String("output", "", "source|assembly|machine|object|executable")
	arch := fs.String("arch", "x86_64", "target architecture")
	osName := fs.String("os", "windows", "target operating system")
	abi := fs.String("abi", "win64", "target ABI")
	hexOutput := fs.Bool("hex", false, "print machine code as hexadecimal")
	entry := fs.String("entry", "", "entry function (default main, otherwise module)")
	via := fs.Bool("via-assembly", false, "explicitly use NASM instead of the internal encoder")
	embedAll := fs.Bool("embed-all-modules", false, "embed every declared Semantic module instead of the program's selected link roots")
	moduleMode := fs.String("module-mode", "needed", "Semantic imports: needed, references, or all")
	moduleRoot := fs.String("module-root", "", "Semantic module store root")
	directEXE := fs.Bool("direct-exe", false, "write only the final output; do not retain native build intermediates")
	buildDir := fs.String("build-dir", "", "directory for persistent native build status and object intermediates")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-source": true, "-target": true, "-o": true, "-entry": true, "-input": true, "-output": true, "-arch": true, "-os": true, "-abi": true, "-via-assembly": false, "--via-assembly": false, "--hex": false, "-module-mode": true, "-module-root": true, "-embed-all-modules": false, "--embed-all-modules": false, "-direct-exe": false, "--direct-exe": false, "-build-dir": true, "--build-dir": true})); err != nil {
		return err
	}
	if *embedAll {
		*moduleMode = string(backend.SemanticModulesAll)
	}
	if *moduleMode != "needed" && *moduleMode != "references" && *moduleMode != "all" {
		return fmt.Errorf("invalid -module-mode %q (expected needed, references, or all)", *moduleMode)
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: compile [ -source <language> ] input...|module-directory -o program.exe")
	}
	// Semantic transport files are self-describing.  Keep the compact public
	// command independent of frontend flags while preserving explicit support
	// for ordinary source languages and all existing binary input modes.
	inputPath := fs.Arg(0)
	journal, err := newNativeBuildJournal(*directEXE, *buildDir, inputPath)
	if err != nil {
		return err
	}
	if journal != nil {
		defer func() { journal.close() }()
		fmt.Fprintf(os.Stderr, "native build artifacts: %s\n", journal.dir)
		journal.stage("input_discovery", "input accepted", inputPath)
	}
	cacheDir := ""
	if journal != nil {
		cacheDir = filepath.Join(journal.dir, "cache")
	}
	// Multiple files form one linked module.  Stage them in a private
	// directory so the normal resolver can preserve relative structure and
	// merge all source units before machine compilation.
	var staged string
	if fs.NArg() > 1 {
		var se error
		staged, se = os.MkdirTemp("", "codetranspiler-module-")
		if se != nil {
			return se
		}
		defer os.RemoveAll(staged)
		for i := 0; i < fs.NArg(); i++ {
			in := fs.Arg(i)
			b, re := os.ReadFile(in)
			if re != nil {
				return re
			}
			name := filepath.Base(in)
			if name == "." || name == string(filepath.Separator) || name == "" {
				return fmt.Errorf("invalid input file %q", in)
			}
			if re = os.WriteFile(filepath.Join(staged, name), b, 0644); re != nil {
				return re
			}
		}
		firstExt := strings.ToLower(filepath.Ext(fs.Arg(0)))
		if firstExt == ".se" || firstExt == ".sp" || firstExt == ".spz" {
			*source = "sp"
		}
		inputPath = staged
	}
	switch strings.ToLower(filepath.Ext(inputPath)) {
	case ".sp", ".spz", ".se":
		*source = "sp"
	case ".json":
		*source = "semantic"
	}
	// A semantic project directory is self-describing through its conventional
	// root transport. The command's historical default is Go, but applying that
	// default to a directory containing main.se would also make the resolver
	// ingest adjacent logs and build scripts as if they were Go source.
	if info, statErr := os.Stat(inputPath); statErr == nil && info.IsDir() && (strings.EqualFold(*source, "go") || strings.EqualFold(*source, "se") || strings.EqualFold(*source, "semantic") || strings.EqualFold(*source, "sp")) {
		for _, name := range []string{"main.se", "main.sp", "main.spz", "main.json"} {
			if _, probeErr := os.Stat(filepath.Join(inputPath, name)); probeErr == nil {
				*source = "sp"
				break
			}
		}
		if strings.EqualFold(*source, "go") && containsSemanticTransport(inputPath) {
			*source = "sp"
		}
	}
	kinds := map[string]codetranspiler.CompileOutputKind{"native-x86_64-windows": codetranspiler.Executable, "object-x86_64-windows": codetranspiler.Object, "machine-x86_64": codetranspiler.MachineCode, "asm-x86_64": codetranspiler.Assembly}
	kind, ok := kinds[*target]
	if *outputKind != "" {
		for alias, candidate := range map[string]codetranspiler.CompileOutputKind{"source": codetranspiler.Source, "assembly": codetranspiler.Assembly, "machine": codetranspiler.MachineCode, "object": codetranspiler.Object, "executable": codetranspiler.Executable} {
			if *outputKind == alias {
				kind, ok = candidate, true
				break
			}
		}
	}
	if !ok {
		if *outputKind != "" {
			for alias, candidate := range map[string]codetranspiler.CompileOutputKind{"source": codetranspiler.Source, "assembly": codetranspiler.Assembly, "machine": codetranspiler.MachineCode, "object": codetranspiler.Object, "executable": codetranspiler.Executable} {
				if *outputKind == alias {
					kind, ok = candidate, true
					break
				}
			}
		}
	}
	if !ok {
		return fmt.Errorf("unsupported compile target %q", *target)
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		if info, statErr := os.Stat(inputPath); statErr == nil && info.IsDir() {
			if journal != nil {
				journal.recordUnits(inputPath)
				journal.stage("semantic_import", "merging semantic source units", inputPath)
			}
			project, re := backend.LoadSemanticProject(inputPath, *entry)
			if re != nil {
				if journal != nil {
					journal.fail(re)
				}
				return re
			}
			if journal != nil {
				journal.stage("project_index", "global semantic index ready", inputPath)
				journal.stage("partition", "compilation units partitioned", inputPath)
				journal.stage("unit_compile", "local unit lowering and fragment generation", inputPath)
			}
			result, re := codetranspiler.CompileSemanticProject(project, codetranspiler.CompileOptions{TargetArch: *arch, TargetOS: *osName, ABI: *abi, OutputKind: kind, EntryPoint: *entry, ViaAssembly: *via, ModuleBaseDir: inputPath, ModuleStoreRoot: *moduleRoot, EmbedAllModules: *embedAll, ModuleEmbeddingMode: backend.SemanticModuleEmbeddingMode(*moduleMode), CacheDir: cacheDir})
			if re != nil {
				if journal != nil {
					journal.fail(re)
				}
				return re
			}
			if kind == codetranspiler.Executable && result.InstructionCount == 0 {
				re = fmt.Errorf("native executable refused: no lowered instructions were produced (incomplete semantic closure)")
				if journal != nil {
					journal.fail(re)
				}
				return re
			}
			if journal != nil {
				journal.stage("fragment_finalize", "machine fragments finalized", inputPath)
				journal.stage("symbol_layout", "symbols laid out deterministically", inputPath)
				journal.stage("relocation", "relocations resolved", inputPath)
				journal.stage("pe_write", "writing final PE image", inputPath)
			}
			if journal != nil {
				journal.streamingPlan(result.Plan)
				journal.streamingMetrics(result.Metrics)
				journal.encodedRegions(result.Regions)
				journal.object(result.ObjectBytes)
				journal.functionObjects(result.FunctionObjects)
				journal.stage("pe_link", "native selection complete", inputPath)
			}
			output := result.Bytes
			if kind == codetranspiler.Assembly {
				output = []byte(result.Text)
			}
			if kind == codetranspiler.Executable && len(output) <= 4608 {
				re = fmt.Errorf("native executable refused: linker produced only the empty PE image (%d bytes)", len(output))
				if journal != nil {
					journal.fail(re)
				}
				return re
			}
			if *out == "" {
				return fmt.Errorf("-o is required for binary output")
			}
			re = os.WriteFile(*out, output, 0644)
			if re != nil && journal != nil {
				journal.fail(re)
			}
			if re == nil && journal != nil {
				journal.complete(*out, len(output))
			}
			return re
		}
		return err
	}
	input := codetranspiler.InputSource
	switch *inputKind {
	case "assembly":
		input = codetranspiler.InputAssembly
	case "machine", "machine_code":
		input = codetranspiler.InputMachine
	case "object":
		input = codetranspiler.InputObject
	case "executable":
		input = codetranspiler.InputExecutable
	case "source":
	default:
		return fmt.Errorf("unsupported input kind %q", *inputKind)
	}
	var result codetranspiler.CompileResult
	// Semantic-SE is the readable transport format used by the GUI and by
	// embedded modules. Route it through the canonical parser/UAST path rather
	// than treating it as ordinary source text.
	if input == codetranspiler.InputSource && (*source == "sp" || *source == "se" || *source == "semantic") {
		var p *backend.SemanticProgram
		var pe error
		if strings.HasPrefix(string(data), "SPZ2") {
			p, pe = backend.ParseSemanticSPZ(data)
		} else if (strings.HasSuffix(strings.ToLower(inputPath), ".se") || strings.HasSuffix(strings.ToLower(inputPath), ".sp")) && len(data) >= 16*1024*1024 {
			// Distribution graphs carry a large derived evidence projection. The
			// native compiler needs the canonical UAST graph, not a second legacy
			// statement tree and its reflection-sized evidence copy. Recompute the
			// same evidence contract once after graph import, as semantic-convert
			// does, and feed that program into CompileMachine.
			var u *backend.UniversalASTDocument
			u, pe = backend.ParseSemanticSEGraph(data)
			if pe == nil {
				// Legacy SP graph exports already carry their derived evidence
				// plane. Reuse it directly; deriving bindings over a merged graph
				// is quadratic in node/binding count. Native SE graph exports omit
				// that optional plane, so derive it once for those files.
				if !strings.HasSuffix(strings.ToLower(inputPath), ".sp") {
					u.Evidence, pe = backend.AnalyzeUniversalEvidence(u)
				}
				if pe == nil {
					p = &backend.SemanticProgram{UniversalAST: u}
				}
			}
		} else if strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
			p, pe = backend.ParseSemanticJSON(data)
		} else {
			p, pe = backend.ParseSemanticSE(data)
		}
		if pe == nil {
			result, pe = codetranspiler.CompileSemanticProgram(p, codetranspiler.CompileOptions{TargetArch: *arch, TargetOS: *osName, ABI: *abi, OutputKind: kind, EntryPoint: *entry, ViaAssembly: *via, ModuleBaseDir: filepath.Dir(inputPath), ModuleStoreRoot: *moduleRoot, EmbedAllModules: *embedAll, ModuleEmbeddingMode: backend.SemanticModuleEmbeddingMode(*moduleMode), CacheDir: cacheDir})
		}
		err = pe
	} else {
		// Source compilation crosses the canonical SemanticProgram boundary
		// before selecting the native backend. This applies uniformly to Go and
		// every registered frontend; the legacy direct source compiler remains
		// only for binary/assembly input kinds.
		if input == codetranspiler.InputSource {
			program, parseErr := manytomany.Parse(*source, string(data))
			if parseErr == nil && program.Semantic != nil {
				result, err = codetranspiler.CompileSemanticProgram(program.Semantic, codetranspiler.CompileOptions{TargetArch: *arch, TargetOS: *osName, ABI: *abi, OutputKind: kind, EntryPoint: *entry, ViaAssembly: *via, ModuleBaseDir: filepath.Dir(inputPath), ModuleStoreRoot: *moduleRoot, EmbedAllModules: *embedAll, ModuleEmbeddingMode: backend.SemanticModuleEmbeddingMode(*moduleMode), CacheDir: cacheDir})
			} else if parseErr != nil {
				err = parseErr
			} else {
				err = fmt.Errorf("frontend produced no SemanticProgram")
			}
		} else {
			result, err = codetranspiler.Compile(string(data), codetranspiler.CompileOptions{InputKind: input, SourceLanguage: *source, SourceArch: *arch, SourceAsmSyntax: "intel", TargetArch: *arch, TargetOS: *osName, ABI: *abi, OutputKind: kind, EntryPoint: *entry, ViaAssembly: *via})
		}
	}
	if err != nil {
		if journal != nil {
			journal.fail(err)
		}
		return err
	}
	if kind == codetranspiler.Executable && result.InstructionCount == 0 {
		err = fmt.Errorf("native executable refused: no lowered instructions were produced (incomplete semantic closure)")
		if journal != nil {
			journal.fail(err)
		}
		return err
	}
	output := result.Bytes
	if kind == codetranspiler.Assembly {
		output = []byte(result.Text)
	}
	if kind == codetranspiler.Executable && len(output) <= 4608 {
		err = fmt.Errorf("native executable refused: linker produced only the empty PE image (%d bytes)", len(output))
		if journal != nil {
			journal.fail(err)
		}
		return err
	}
	if *hexOutput && kind == codetranspiler.MachineCode {
		if *out == "" {
			_, _ = fmt.Fprintln(os.Stdout, hex.EncodeToString(result.Bytes))
			return nil
		}
	}
	if *out == "" {
		return fmt.Errorf("-o is required for binary output")
	}
	if journal != nil {
		journal.streamingPlan(result.Plan)
		journal.streamingMetrics(result.Metrics)
		journal.encodedRegions(result.Regions)
		journal.object(result.ObjectBytes)
		journal.functionObjects(result.FunctionObjects)
		journal.stage("pe_link", "native selection complete", inputPath)
	}
	err = os.WriteFile(*out, output, 0644)
	if err != nil && journal != nil {
		journal.fail(err)
	}
	if err == nil && journal != nil {
		journal.complete(*out, len(output))
	}
	return err
}

func containsSemanticTransport(root string) bool {
	found := false
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found {
			return nil
		}
		if entry.IsDir() && entry.Name() == ".semantic-cache" {
			return filepath.SkipDir
		}
		if !entry.IsDir() {
			switch strings.ToLower(filepath.Ext(path)) {
			case ".se", ".sp", ".spz", ".smod":
				found = true
			}
		}
		return nil
	})
	return found
}

// nativeBuildJournal is deliberately a build artifact, not a second compiler
// IR. It records the existing pipeline's real phase boundaries and retains the
// COFF object generated from the same x64 instruction stream as the final PE.
// A PE image itself cannot be incrementally executable: its section layout,
// relocations and import table are only known after selection has finished.
type nativeBuildJournal struct {
	dir   string
	state nativeBuildState
}

type nativeBuildState struct {
	Schema      string `json:"schema"`
	Input       string `json:"input"`
	Stage       string `json:"stage"`
	Message     string `json:"message"`
	UpdatedAt   string `json:"updated_at"`
	Output      string `json:"output,omitempty"`
	OutputBytes int    `json:"output_bytes,omitempty"`
	Error       string `json:"error,omitempty"`
	CurrentUnit string `json:"current_unit,omitempty"`
	UnitsDone   int    `json:"units_done,omitempty"`
	UnitsTotal  int    `json:"units_total,omitempty"`
}

func newNativeBuildJournal(direct bool, requested, input string) (*nativeBuildJournal, error) {
	if direct {
		return nil, nil
	}
	dir := requested
	if dir == "" {
		base := filepath.Join(os.TempDir(), "CodeTranspiler", "builds")
		if err := os.MkdirAll(base, 0755); err != nil {
			return nil, err
		}
		// Keep the journal/cache content-addressed by the input project instead
		// of allocating a fresh directory for every invocation.  This makes
		// fragment and summary caches reusable across CLI runs while the
		// content keys still invalidate entries when a unit changes.
		abs, err := filepath.Abs(input)
		if err != nil {
			return nil, err
		}
		digest := sha256.Sum256([]byte(filepath.Clean(abs)))
		dir = filepath.Join(base, "native-"+hex.EncodeToString(digest[:8]))
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	j := &nativeBuildJournal{dir: dir, state: nativeBuildState{Schema: "code-transpiler.native-build.v1", Input: input}}
	j.stage("created", "native build journal created", input)
	return j, nil
}

func (j *nativeBuildJournal) write() {
	j.state.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	b, err := json.MarshalIndent(j.state, "", "  ")
	if err == nil {
		_ = os.WriteFile(filepath.Join(j.dir, "build-status.json"), append(b, '\n'), 0644)
	}
}
func (j *nativeBuildJournal) stage(stage, message, _ string) {
	j.state.Stage, j.state.Message = stage, message
	j.write()
}
func (j *nativeBuildJournal) fail(err error) {
	j.state.Stage, j.state.Message, j.state.Error = "failed", "native build failed", err.Error()
	j.write()
}
func (j *nativeBuildJournal) complete(output string, bytes int) {
	j.state.Stage, j.state.Message, j.state.Output, j.state.OutputBytes = "complete", "PE image written", output, bytes
	j.write()
}
func (j *nativeBuildJournal) close() {}
func (j *nativeBuildJournal) object(data []byte) {
	if len(data) > 0 {
		_ = os.WriteFile(filepath.Join(j.dir, "program.obj"), data, 0644)
		j.stage("object_written", "COFF object written", "")
	}
}
func (j *nativeBuildJournal) functionObjects(objects map[string][]byte) {
	if len(objects) == 0 {
		return
	}
	dir := filepath.Join(j.dir, "objects")
	if os.MkdirAll(dir, 0755) != nil {
		return
	}
	cacheDir := filepath.Join(j.dir, "fragments")
	if os.MkdirAll(cacheDir, 0755) != nil {
		return
	}
	cacheManifest := map[string]string{}
	names := make([]string, 0, len(objects))
	for name := range objects {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := objects[name]
		if len(data) == 0 {
			continue
		}
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		cacheManifest[name] = hash
		cached := filepath.Join(cacheDir, hash+".obj")
		if old, err := os.ReadFile(cached); err != nil || !bytes.Equal(old, data) {
			tmp := cached + ".tmp"
			if os.WriteFile(tmp, data, 0644) == nil {
				_ = os.Rename(tmp, cached)
			}
		}
		// Keep the human-readable per-function name as a staging view while the
		// content-addressed copy is the stable cache record.
		_ = os.WriteFile(filepath.Join(dir, safeObjectName(name)+".obj"), data, 0644)
	}
	if b, err := json.MarshalIndent(cacheManifest, "", "  "); err == nil {
		_ = os.WriteFile(filepath.Join(j.dir, "fragment-cache.json"), append(b, '\n'), 0644)
	}
	j.stage("function_objects_written", "per-function COFF staging objects written", "")
}
func (j *nativeBuildJournal) streamingPlan(plan backend.StreamingPlan) {
	b, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(j.dir, "streaming-plan.json"), append(b, '\n'), 0644)
}
func (j *nativeBuildJournal) streamingMetrics(metrics backend.StreamingMetrics) {
	b, err := json.MarshalIndent(metrics, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(j.dir, "streaming-metrics.json"), append(b, '\n'), 0644)
}
func (j *nativeBuildJournal) encodedRegions(regions []backend.EncodedRegion) {
	b, err := json.MarshalIndent(regions, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(filepath.Join(j.dir, "encoded-regions.json"), append(b, '\n'), 0644)
}
func safeObjectName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "function"
	}
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return b.String()
}
func (j *nativeBuildJournal) progress(p backend.ModuleImportProgress) {
	j.state.Stage = "semantic_import_" + p.Stage
	j.state.Message = "importing semantic source unit"
	j.state.CurrentUnit = filepath.ToSlash(p.Path)
	j.state.UnitsDone, j.state.UnitsTotal = p.Index, p.Total
	j.write()
	entry := struct {
		Stage string `json:"stage"`
		Path  string `json:"path"`
		Index int    `json:"index"`
		Total int    `json:"total"`
		At    string `json:"at"`
	}{p.Stage, filepath.ToSlash(p.Path), p.Index, p.Total, time.Now().UTC().Format(time.RFC3339Nano)}
	if b, err := json.Marshal(entry); err == nil {
		f, err := os.OpenFile(filepath.Join(j.dir, "unit-progress.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
		if err == nil {
			_, _ = f.Write(append(b, '\n'))
			_ = f.Close()
		}
	}
}
func (j *nativeBuildJournal) recordUnits(root string) {
	f, err := os.Create(filepath.Join(j.dir, "source-units.jsonl"))
	if err != nil {
		return
	}
	defer f.Close()
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err == nil && entry.IsDir() && entry.Name() == ".semantic-cache" {
			return filepath.SkipDir
		}
		if err != nil || entry.IsDir() {
			return nil
		}
		info, e := entry.Info()
		if e == nil {
			_, _ = fmt.Fprintf(f, "{\"path\":%q,\"bytes\":%d}\n", filepath.ToSlash(path), info.Size())
		}
		return nil
	})
}
