// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"gioui.org/app"
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
	"github.com/tarekwasfy01/Code-Transpiler/internal/platform"
	"github.com/tarekwasfy01/Code-Transpiler/internal/runtimeassets"
	"github.com/tarekwasfy01/Code-Transpiler/internal/targetrun"
	"github.com/tarekwasfy01/Code-Transpiler/internal/thirdpartylicenses"
	"github.com/tarekwasfy01/Code-Transpiler/internal/ui"
)

// Set by the local onefile build. Defaults keep source-tree invocations
// inspectable while GUI and CLI always report the same engine identity.
var (
	version   = "dev"
	commit    = "local"
	buildDate = "unbuilt"
)

func main() {
	if len(os.Args) < 2 {
		launchGUI()
		return
	}
	// `sp <command>` is a compact alias for every CLI command.
	if strings.EqualFold(os.Args[1], "sp") {
		if len(os.Args) < 3 {
			_, _ = os.Stdout.WriteString(helpText)
			return
		}
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	}
	platform.EnsureCLIConsole()
	switch os.Args[1] {
	case "gui":
		launchGUI()
	case "help", "--help", "-h":
		_, _ = os.Stdout.WriteString(helpText)
	case "version", "--version":
		fmt.Printf("Semantic Programming Language %s\ncommit=%s\nbuild_date=%s\nengine=TranspileCore/UAST\n", version, commit, buildDate)
	case "licenses", "licences", "--licenses", "--licences":
		fmt.Print(thirdpartylicenses.FullText())
	case "targets", "languages":
		fmt.Println("r\tR\t.R")
		for _, l := range backend.Languages {
			fmt.Printf("%s\t%s\t%s\n", l.ID, l.Name, l.Extension)
		}
	case "runtimes":
		for _, t := range runtimeassets.Targets() {
			files, err := runtimeassets.List(t)
			if err != nil {
				fmt.Printf("%s\terror: %v\n", t, err)
				continue
			}
			fmt.Printf("%s\t%d embedded runtime source files (external compiler required)\n", t, len(files))
		}
	case "bundle-info", "bundle-verify":
		if err := semanticBundleInfoCommand(); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "bundle-extract":
		if err := semanticBundleExtractCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "run":
		if err := runSource(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "transpile":
		if err := transpile(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "compile":
		if err := compileNative(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "compile-llvm":
		if err := compileLLVM(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "compile-toolchain", "compile-gcc", "compile-msvc":
		args := os.Args[2:]
		if os.Args[1] == "compile-gcc" {
			args = append([]string{"-toolchain", "gcc"}, args...)
		} else if os.Args[1] == "compile-msvc" {
			args = append([]string{"-toolchain", "msvc"}, args...)
		}
		if err := compileExternal(args); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "transpile-batch":
		if err := transpileBatch(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "routes":
		for _, source := range manytomany.Languages {
			for _, target := range manytomany.Languages {
				if source != target {
					fmt.Printf("%s\t%s\n", source, target)
				}
			}
		}
	case "semantic-export":
		if err := semanticExport(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "semantic-csc":
		if err := semanticCSC(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "compile-csc":
		if err := compileCSC(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "compile-csc-project":
		if err := compileCSCProject(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "semantic-export-project":
		if err := semanticExportProject(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "machine-ir":
		if err := machineIRExport(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "native-compiler-evidence":
		if err := nativeCompilerEvidence(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "decompile":
		if err := decompileSemantic(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "native-analysis":
		if err := nativeAnalysis(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	case "semantic-transpile":
		if err := semanticTranspile(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "semantic-convert":
		if err := semanticConvert(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "semantic-merge":
		if err := semanticMergeCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "semantic-link":
		if err := semanticLinkCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "semantic-format":
		if err := semanticFormat(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "semantic-validate":
		if err := semanticValidate(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "semantic-info":
		if err := semanticInfo(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "capability":
		if len(os.Args) != 4 {
			fmt.Fprintln(os.Stderr, "usage: CodeTranspiler.exe capability <target> <feature>")
			os.Exit(2)
		}
		result := backend.BackendCapability(os.Args[3], os.Args[2])
		_ = json.NewEncoder(os.Stdout).Encode(result)
	case "capability-matrix":
		_ = json.NewEncoder(os.Stdout).Encode(backend.SemanticCapabilityMatrix(os.Args[2:]))
	case "implementation-matrix":
		_ = json.NewEncoder(os.Stdout).Encode(backend.TypedImplementationMatrix())
	case "setpath":
		if err := setPath(); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "module":
		if err := semanticModuleCommand(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "semantic":
		if len(os.Args) >= 3 && strings.EqualFold(os.Args[2], "module") {
			if err := semanticModuleCommand(os.Args[3:]); err != nil {
				fmt.Fprintln(os.Stderr, "r2many:", err)
				os.Exit(1)
			}
		} else {
			_, _ = os.Stdout.WriteString(helpText)
			os.Exit(2)
		}
	default:
		_, _ = os.Stdout.WriteString(helpText)
		os.Exit(2)
	}
}

func setPath() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	dir := filepath.Dir(exe)
	if runtime.GOOS != "windows" {
		return fmt.Errorf("setpath is supported on Windows only")
	}
	// Use an elevated PowerShell process so the installation directory is
	// available to all users. The existing PATH is preserved and duplicates are
	// avoided by the script itself.
	quoted := strings.ReplaceAll(dir, "'", "''")
	script := "$d='" + quoted + "'; $p=[Environment]::GetEnvironmentVariable('Path','Machine'); if(-not (($p -split ';') -contains $d)){ [Environment]::SetEnvironmentVariable('Path',(($p.TrimEnd(';')+';'+$d).Trim(';')),'Machine') }"
	cmd := exec.Command("powershell.exe", "-NoProfile", "-Command", "Start-Process powershell.exe -Verb RunAs -ArgumentList '-NoProfile','-Command',\""+strings.ReplaceAll(script, "\"", "\\\"")+"\"")
	return cmd.Run()
}

func semanticExport(args []string) error {
	fs := flag.NewFlagSet("semantic-export", flag.ContinueOnError)
	native := fs.Bool("native", false, "use strict native frontend without legacy fallback")
	source := fs.String("source", "r", "source language")
	inputKind := fs.String("input", "source", "source|assembly|machine|object|executable")
	out := fs.String("o", "", "SemanticProgram JSON output path")
	format := fs.String("format", "json", "json or sp")
	preserveSource := fs.Bool("preserve-source", false, "include original source provenance in .se output")
	moduleRoot := fs.String("module-root", "", "Semantic module store root")
	embedAll := fs.Bool("embed-all-modules", false, "embed all resolved semantic modules")
	moduleMode := fs.String("module-mode", "needed", "Semantic imports: needed, references, or all")
	license := fs.Bool("license", false, "copy licenses of imported modules beside output")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-source": true, "-input": true, "-o": true, "-format": true, "-native": false, "-preserve-source": false, "-module-root": true, "-module-mode": true, "-embed-all-modules": false, "--embed-all-modules": false, "-license": false})); err != nil {
		return err
	}
	if *embedAll {
		*moduleMode = string(backend.SemanticModulesAll)
	}
	if *moduleMode != "needed" && *moduleMode != "references" && *moduleMode != "all" {
		return fmt.Errorf("invalid -module-mode %q (expected needed, references, or all)", *moduleMode)
	}
	if fs.NArg() != 1 || *out == "" {
		return fmt.Errorf("usage: semantic-export -source <language> input -o program.semantic.json (or -input assembly|machine|object|executable)")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var semantic *backend.SemanticProgram
	if *inputKind != "source" {
		kind := map[string]backend.CompileInputKind{
			"assembly":     backend.CompileInputAssembly,
			"machine":      backend.CompileInputMachine,
			"machine_code": backend.CompileInputMachine,
			"object":       backend.CompileInputObject,
			"executable":   backend.CompileInputExecutable,
		}[*inputKind]
		if kind == "" {
			return fmt.Errorf("unsupported semantic-export input kind %q", *inputKind)
		}
		semantic, err = backend.LiftBinaryInput(data, backend.CompileOptions{InputKind: kind, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	} else if *native {
		if backend.NormalizeLanguage(*source) != "go" {
			return fmt.Errorf("native executable frontend supports go only")
		}
		semantic, err = backend.LowerNativeGo(fs.Arg(0), string(data))
	} else {
		var program manytomany.Program
		program, err = manytomany.Parse(*source, string(data))
		semantic = program.Semantic
	}
	if err != nil {
		return err
	}
	backend.NormalizeGoPackageModuleReferences(semantic, filepath.Dir(fs.Arg(0)))
	if len(semantic.Origin.Modules) > 0 {
		resolver, err := backend.NewUniversalModuleResolver()
		if err != nil {
			return fmt.Errorf("semantic module resolver: %w", err)
		}
		if *moduleRoot != "" {
			resolver.Store.Root = *moduleRoot
		}
		if err := resolver.LinkSemanticDependenciesWithOptions(semantic, backend.SemanticModuleLinkOptions{
			BaseDir: filepath.Dir(fs.Arg(0)), EmbedAll: *embedAll, Mode: backend.SemanticModuleEmbeddingMode(*moduleMode),
		}); err != nil {
			return fmt.Errorf("semantic module import: %w", err)
		}
		// Persist the resolved module boundary in the exported document. The
		// resolver establishes/link-checks dependencies; EmbedSemanticModules
		// is the existing deduplicating payload/reference pass that makes the
		// result self-contained for later target projection.
		if *moduleMode != "references" {
			if _, err := backend.EmbedSemanticModules(semantic, backend.SemanticModuleEmbeddingOptions{
				BaseDir: filepath.Dir(fs.Arg(0)), StoreRoot: resolver.Store.Root, UnitPath: *out,
				Language: semantic.Origin.SourceLanguage, NeededOnly: *moduleMode == "needed",
			}); err != nil {
				return fmt.Errorf("semantic module embedding: %w", err)
			}
		}
	}
	var encoded []byte
	if isSemanticCompressedFormat(*format) || isSemanticCompressedPath(*out) {
		encoded, err = semantic.MarshalSemanticSPZ()
	} else if isSemanticTextFormat(*format) || isSemanticTextPath(*out) {
		if strings.HasSuffix(strings.ToLower(*out), ".se") || strings.EqualFold(*format, "se") {
			if *preserveSource {
				encoded, err = semantic.MarshalSemanticSEWithSource()
			} else {
				encoded, err = semantic.MarshalSemanticSEReadable()
			}
		} else {
			encoded, err = semantic.MarshalSemanticSP()
		}
	} else {
		encoded, err = semantic.MarshalSemanticJSON()
	}
	if err != nil {
		return err
	}
	if err := os.WriteFile(*out, encoded, 0644); err != nil {
		return err
	}
	if *license {
		if _, err := backend.CopyImportedPackageLicenses(*moduleRoot, *out); err != nil {
			return fmt.Errorf("copy imported licenses: %w", err)
		}
	}
	return nil
}

// semanticExportProject consumes a newline-delimited source manifest and emits
// one readable .se file per Go source file. Each Go package is parsed and
// type-checked once, avoiding N repeated sibling-package typechecks.
func semanticExportProject(args []string) error {
	fs := flag.NewFlagSet("semantic-export-project", flag.ContinueOnError)
	root := fs.String("root", ".", "source tree root used for relative output paths")
	out := fs.String("o", "", "output directory for per-file .se units")
	manifest := fs.String("manifest", "", "newline-delimited list of production Go source files")
	moduleRoot := fs.String("module-root", "", "Semantic module store root")
	moduleMode := fs.String("module-mode", "needed", "module embedding: needed, references, or all")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" || *manifest == "" || fs.NArg() != 0 {
		return fmt.Errorf("usage: semantic-export-project -root <source-root> -manifest <go-files.txt> -o <se-output-dir> [-module-root DIR] [-module-mode needed|references|all]")
	}
	if *moduleMode != "needed" && *moduleMode != "references" && *moduleMode != "all" {
		return fmt.Errorf("invalid -module-mode %q (expected needed, references, or all)", *moduleMode)
	}
	resolver, err := backend.NewUniversalModuleResolver()
	if err != nil {
		return err
	}
	if *moduleRoot != "" {
		resolver.Store.Root = *moduleRoot
		if err := resolver.Store.Ensure(); err != nil {
			return err
		}
	}
	rootPath, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	manifestFile, err := os.Open(*manifest)
	if err != nil {
		return err
	}
	defer manifestFile.Close()
	groups := map[string][]string{}
	scanner := bufio.NewScanner(manifestFile)
	scanner.Buffer(make([]byte, 4096), 4*1024*1024)
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if name == "" {
			continue
		}
		abs, err := filepath.Abs(name)
		if err != nil {
			return err
		}
		groups[filepath.Dir(abs)] = append(groups[filepath.Dir(abs)], abs)
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	dirs := make([]string, 0, len(groups))
	for dir := range groups {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	base, err := filepath.Abs(*out)
	if err != nil {
		return err
	}
	embeddingRegistry := backend.NewSemanticModuleEmbeddingRegistry(base)
	if err := os.MkdirAll(base, 0755); err != nil {
		return err
	}
	var exported, failed int
	var failures []string
	for _, dir := range dirs {
		err := backend.LowerNativeGoPackageEach(groups[dir], func(source string, program *backend.SemanticProgram, lowerErr error) error {
			if lowerErr != nil {
				failed++
				failures = append(failures, fmt.Sprintf("%s: %v", source, lowerErr))
				return nil
			}
			rel, err := filepath.Rel(rootPath, source)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				failed++
				failures = append(failures, fmt.Sprintf("%s: source lies outside root %s", source, rootPath))
				return nil
			}
			target := filepath.Join(base, strings.TrimSuffix(rel, filepath.Ext(rel))+".se")
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				failed++
				failures = append(failures, fmt.Sprintf("%s: %v", source, err))
				return nil
			}
			// LowerNativeGo records source imports in the native frontend shape.
			// Normalize them to canonical pkg:* module identities before resolving
			// and embedding, otherwise the project exporter cannot observe external
			// Go modules even though the source contains imports.
			backend.NormalizeGoPackageModuleReferences(program, filepath.Dir(source))
			if *moduleMode == "references" {
				// Reference-only exports keep imports as metadata and do not copy
				// module bodies into any unit.
				if program.Metadata == nil {
					program.Metadata = map[string]string{}
				}
				program.Metadata["semantic_module_embedding_mode"] = "references"
			} else if _, err = backend.EmbedSemanticModules(program, backend.SemanticModuleEmbeddingOptions{BaseDir: filepath.Dir(source), StoreRoot: resolver.Store.Root, UnitPath: target, Registry: embeddingRegistry}); err != nil {
				failed++
				failures = append(failures, fmt.Sprintf("%s: %v", source, err))
				return nil
			}
			data, err := program.MarshalSemanticSEReadable()
			if err == nil {
				err = os.WriteFile(target, data, 0644)
			}
			if err != nil {
				failed++
				failures = append(failures, fmt.Sprintf("%s: %v", source, err))
				return nil
			}
			exported++
			return nil
		})
		if err != nil {
			failed += len(groups[dir])
			failures = append(failures, fmt.Sprintf("%s: %v", dir, err))
		}
	}
	fmt.Printf("GO_PACKAGE_GROUPS=%d\nGO_FILES_EXPORTED=%d\nGO_FILES_FAILED=%d\nOUTPUT=%s\n", len(dirs), exported, failed, base)
	for _, failure := range failures {
		fmt.Fprintln(os.Stderr, failure)
	}
	if failed != 0 {
		return fmt.Errorf("semantic project export had %d failed source files", failed)
	}
	return nil
}

// embedExportModules links each declared package root into the first .se unit
// that imports it. Later units retain a canonical reference to that unit and
// do not copy the module graph again. This is deliberately performed at the
// existing SemanticProgram boundary; no second module representation is made.
func embedExportModules(program *backend.SemanticProgram, source, target string, resolver backend.UniversalModuleResolver, mode backend.SemanticModuleEmbeddingMode, embedded map[string]string) error {
	if program == nil || len(program.Origin.Modules) == 0 {
		return nil
	}
	backend.NormalizeGoPackageModuleReferences(program, filepath.Dir(source))
	declared := append([]string(nil), program.Origin.Modules...)
	for _, dep := range declared {
		dep = strings.TrimSpace(dep)
		if dep == "" {
			continue
		}
		if first, ok := embedded[dep]; ok {
			addSemanticModuleReference(program, dep, first)
			continue
		}
		if mode == backend.SemanticModulesReference {
			addSemanticModuleReference(program, dep, "")
			// The resolver acquires automatic package references but deliberately
			// keeps their bodies out of this unit in reference-only mode.
		}
		program.Origin.Modules = []string{dep}
		if program.UniversalAST != nil {
			program.UniversalAST.Origin.Modules = []string{dep}
		}
		if program.Metadata != nil {
			delete(program.Metadata, "semantic_modules_linked")
		}
		if program.UniversalAST != nil && program.UniversalAST.Metadata != nil {
			delete(program.UniversalAST.Metadata, "semantic_modules_linked")
		}
		if err := resolver.LinkSemanticDependenciesWithOptions(program, backend.SemanticModuleLinkOptions{BaseDir: filepath.Dir(source), Mode: mode}); err != nil {
			return err
		}
		if mode != backend.SemanticModulesReference {
			embedded[dep] = filepath.ToSlash(target)
		}
		addSemanticModuleReference(program, dep, embedded[dep])
	}
	program.Origin.Modules = uniqueModuleStrings(append(declared, program.Origin.Modules...))
	if program.UniversalAST != nil {
		program.UniversalAST.Origin.Modules = append([]string(nil), program.Origin.Modules...)
	}
	return nil
}

func addSemanticModuleReference(program *backend.SemanticProgram, dep, target string) {
	if program.Metadata == nil {
		program.Metadata = map[string]string{}
	}
	key := "semantic_module_reference:" + dep
	if _, exists := program.Metadata[key]; !exists {
		program.Metadata[key] = target
	}
	if program.UniversalAST != nil {
		if program.UniversalAST.Metadata == nil {
			program.UniversalAST.Metadata = map[string]string{}
		}
		if _, exists := program.UniversalAST.Metadata[key]; !exists {
			program.UniversalAST.Metadata[key] = target
		}
	}
}

func uniqueModuleStrings(values []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func machineIRExport(args []string) error {
	fs := flag.NewFlagSet("machine-ir", flag.ContinueOnError)
	inputKind := fs.String("input", "machine", "assembly|machine|object|executable")
	out := fs.String("o", "", "MachineIR JSON output path")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-input": true, "-o": true})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: machine-ir -input assembly|machine|object|executable input [-o machine-ir.json]")
	}
	kind := map[string]backend.CompileInputKind{"assembly": backend.CompileInputAssembly, "machine": backend.CompileInputMachine, "machine_code": backend.CompileInputMachine, "object": backend.CompileInputObject, "executable": backend.CompileInputExecutable}[*inputKind]
	if kind == "" {
		return fmt.Errorf("unsupported machine-ir input kind %q", *inputKind)
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	ir, err := backend.DecodeMachineIR(data, backend.CompileOptions{InputKind: kind, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(ir, "", "  ")
	if err != nil {
		return err
	}
	if *out == "" {
		_, err = os.Stdout.Write(append(encoded, '\n'))
		return err
	}
	return os.WriteFile(*out, append(encoded, '\n'), 0644)
}

func nativeCompilerEvidence(args []string) error {
	fs := flag.NewFlagSet("native-compiler-evidence", flag.ContinueOnError)
	root := fs.String("root", ".", "project root containing internal/backend")
	out := fs.String("out", "compiler-evidence/native", "evidence output directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	report, err := backend.ExtractNativeCompilerEvidence(*root)
	if err != nil {
		return err
	}
	if err := backend.WriteNativeCompilerEvidence(report, *out); err != nil {
		return err
	}
	fmt.Printf("COMPILER=%s\nSOURCE_UNITS=%d\nFUNCTIONS=%d\nLOWERING_RULES=%d\nCALL_EDGES=%d\nENCODABLE_OPS=%d\nENCODER_GAPS=%d\nOUTPUT=%s\n", report.Compiler, report.Units, report.Functions, len(report.Rules), len(report.CallEdges), len(report.EncodableOps), len(report.Gaps), *out)
	return nil
}

func compilerSourceEvidence(args []string) error {
	fs := flag.NewFlagSet("compiler-source-evidence", flag.ContinueOnError)
	projectRoot := fs.String("project-root", ".", "transpiler root to index its native backend")
	roslynRoot := fs.String("roslyn-root", ".cache/compiler-sources/roslyn", "local Roslyn source checkout (sparse checkout accepted)")
	llvmRoot := fs.String("llvm-root", ".cache/compiler-sources/llvm-project", "local llvm-project source checkout (sparse checkout accepted)")
	crosswalkPath := fs.String("crosswalk", "", "optional explicit canonical semantic crosswalk JSON; conditions must match across compiler evidence")
	migrationMatrixPath := fs.String("migration-matrix", "", "optional structured compiler migration matrix CSV to include as evidence")
	out := fs.String("out", "compiler-evidence", "evidence output root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	roslyn, err := backend.ExtractCompilerRepositoryEvidence(backend.CompilerRepositorySpec{
		Compiler: "Roslyn C# compiler", Repository: "https://github.com/dotnet/roslyn", Ref: "main", Root: *roslynRoot,
		License: "MIT", Include: []string{"src/Compilers/CSharp/Portable/Lowering", "src/Compilers/CSharp/Portable/Binder/Semantics/Operators", "src/Compilers/CSharp/Portable/BoundTree", "src/Compilers/CSharp/Portable/Emitter"}, Extensions: []string{".cs"},
	})
	if err != nil {
		return err
	}
	llvm, err := backend.ExtractCompilerRepositoryEvidence(backend.CompilerRepositorySpec{
		Compiler: "LLVM", Repository: "https://github.com/llvm/llvm-project", Ref: "main", Root: *llvmRoot,
		License: "Apache-2.0 WITH LLVM-exception", TargetArchitecture: "x86_64",
		Include:    []string{"llvm/lib/CodeGen/SelectionDAG", "llvm/lib/CodeGen/GlobalISel", "llvm/lib/Target/X86", "llvm/include/llvm/CodeGen", "llvm/include/llvm/IR/Instruction.def", "utils/TableGen"},
		Extensions: []string{".cpp", ".h", ".td", ".def"},
	})
	if err != nil {
		return err
	}
	native, err := backend.ExtractNativeCompilerEvidence(*projectRoot)
	if err != nil {
		return err
	}
	for _, item := range []struct {
		name   string
		report *backend.CompilerRepositoryReport
	}{{"roslyn", roslyn}, {"llvm", llvm}} {
		if err := backend.WriteCompilerRepositoryEvidence(item.report, filepath.Join(*out, item.name)); err != nil {
			return err
		}
	}
	if err := backend.WriteNativeCompilerEvidence(native, filepath.Join(*out, "native")); err != nil {
		return err
	}
	matrix := backend.MergeCompilerEvidenceMatrix([]*backend.CompilerRepositoryReport{roslyn, llvm}, native)
	if *migrationMatrixPath != "" {
		if err := appendMigrationMatrixEvidence(matrix, *migrationMatrixPath); err != nil {
			return err
		}
	}
	if *crosswalkPath != "" {
		data, err := os.ReadFile(*crosswalkPath)
		if err != nil {
			return err
		}
		var rows []backend.CompilerSemanticCrosswalk
		if err := json.Unmarshal(data, &rows); err != nil {
			return fmt.Errorf("decode compiler crosswalk: %w", err)
		}
		if err := backend.ApplyExplicitCompilerCrosswalks(matrix, rows); err != nil {
			return err
		}
		// Keep the authored crosswalk beside the generated matrix so every
		// confirmed equivalence is reproducible and reviewable with its run.
		if err := os.MkdirAll(*out, 0755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(*out, "crosswalk.json"), data, 0644); err != nil {
			return err
		}
	}
	if err := backend.WriteCompilerEvidenceMatrix(matrix, filepath.Join(*out, "matrix")); err != nil {
		return err
	}
	audit := backend.BuildNativeCoverageAudit(matrix)
	// Persist the exact source roots/commits used for this run so the report is
	// reproducible without making those external compiler trees runtime inputs.
	metadata := map[string]any{
		"schema_version": "compiler-evidence-run/v1",
		"generated_utc":  time.Now().UTC().Format(time.RFC3339Nano),
		"roslyn":         map[string]any{"root": filepath.Clean(*roslynRoot), "repository": roslyn.Repository, "ref": roslyn.Ref, "commit": roslyn.Commit, "license": roslyn.License},
		"llvm":           map[string]any{"root": filepath.Clean(*llvmRoot), "repository": llvm.Repository, "ref": llvm.Ref, "commit": llvm.Commit, "license": llvm.License},
		"native":         map[string]any{"project_root": filepath.Clean(*projectRoot), "repository": native.Repository, "version": native.Version, "license": "project source"},
	}
	if err := os.MkdirAll(*out, 0755); err != nil {
		return err
	}
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(*out, "source-roots.json"), append(metadataBytes, '\n'), 0644); err != nil {
		return err
	}
	reportText := fmt.Sprintf("# Compiler evidence run\n\nRoslyn `%s` at `%s` (%s). LLVM `%s` at `%s` (%s). Native source `%s`.\n\nEvidence is provenance-preserving and analytical. No external compiler runtime or source is linked into the product. Canonical equivalence requires an explicit crosswalk with compatible contracts.\n", roslyn.Commit, filepath.Clean(*roslynRoot), roslyn.License, llvm.Commit, filepath.Clean(*llvmRoot), llvm.License, native.Version)
	if err := os.WriteFile(filepath.Join(*out, "compiler-evidence-report.md"), []byte(reportText), 0644); err != nil {
		return err
	}
	// Export the existing canonical primitive authority beside the external
	// evidence. This is a projection of the current compiler registry, not a
	// second registry and not an automatic promotion of external candidates.
	primitiveReport, err := backend.CompileUniversalPrimitiveSpecs()
	if err != nil {
		return fmt.Errorf("compile canonical primitive authority: %w", err)
	}
	canonicalDir := filepath.Join(*out, "canonical")
	if err := os.MkdirAll(canonicalDir, 0755); err != nil {
		return err
	}
	canonicalFile, err := os.Create(filepath.Join(canonicalDir, "contracts.jsonl"))
	if err != nil {
		return err
	}
	canonicalEncoder := json.NewEncoder(canonicalFile)
	for _, spec := range primitiveReport.Specs {
		if err := canonicalEncoder.Encode(spec); err != nil {
			_ = canonicalFile.Close()
			return err
		}
	}
	if err := canonicalFile.Close(); err != nil {
		return err
	}
	canonicalSummary := map[string]any{"schema_version": "canonical-primitive-contracts/v1", "primitive_count": len(primitiveReport.Specs), "recipe_count": len(primitiveReport.Recipes), "derived_count": primitiveReport.DerivedCount, "atomic_count": len(primitiveReport.AtomicPrimitives), "basis_hash": primitiveReport.BasisHash}
	canonicalBytes, err := json.MarshalIndent(canonicalSummary, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(canonicalDir, "summary.json"), append(canonicalBytes, '\n'), 0644); err != nil {
		return err
	}
	fmt.Printf("ROSLYN_UNITS=%d ROSLYN_RULES=%d ROSLYN_MISSING_PATHS=%d\n", len(roslyn.Units), len(roslyn.Rules), len(roslyn.Gaps))
	fmt.Printf("LLVM_UNITS=%d LLVM_RULES=%d LLVM_MISSING_PATHS=%d\n", len(llvm.Units), len(llvm.Rules), len(llvm.Gaps))
	fmt.Printf("NATIVE_UNITS=%d NATIVE_RULES=%d NATIVE_CALL_EDGES=%d\n", native.Units, len(native.Rules), len(native.CallEdges))
	fmt.Printf("MATRIX_NODES=%d MATRIX_EDGES=%d MATRIX_RULES=%d PRIMITIVE_CANDIDATES=%d SEMANTIC_EQUIVALENCES=%d NATIVE_COVERAGE_ROWS=%d CANONICAL_PRIMITIVES=%d OUTPUT=%s\n", len(matrix.Nodes), len(matrix.Edges), len(matrix.Rules), len(matrix.PrimitiveCandidates), len(matrix.Equivalences), len(audit.Rows), len(primitiveReport.Specs), *out)
	return nil
}

// appendMigrationMatrixEvidence imports the checked-in cross-source migration
// matrix as provenance-preserving compiler evidence. It deliberately does not
// promote operation names to canonical equivalences; the explicit crosswalk
// remains the only promotion mechanism.
func appendMigrationMatrixEvidence(matrix *backend.CompilerEvidenceMatrix, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open migration matrix: %w", err)
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	head, err := r.Read()
	if err != nil {
		return fmt.Errorf("read migration matrix header: %w", err)
	}
	for len(head) > 0 && strings.HasPrefix(strings.TrimSpace(head[0]), "#") {
		head, err = r.Read()
		if err != nil {
			return fmt.Errorf("read migration matrix header: %w", err)
		}
	}
	idx := map[string]int{}
	for i, h := range head {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	for rowNo := 2; ; rowNo++ {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return fmt.Errorf("read migration matrix row %d: %w", rowNo, e)
		}
		get := func(name string) string {
			i, ok := idx[name]
			if !ok || i >= len(row) {
				return ""
			}
			return strings.TrimSpace(row[i])
		}
		stage := backend.CompilerStage(strings.ToLower(get("stage")))
		switch stage {
		case backend.CompilerStageBinding, backend.CompilerStageLowering, backend.CompilerStageLinking,
			backend.CompilerStageSemanticSelection, backend.CompilerStageInstructionSelect,
			backend.CompilerStageEncoding, backend.CompilerStageLegalization, backend.CompilerStageCodeGeneration:
		default:
			stage = backend.CompilerStageLowering
		}
		operation := get("operation")
		if operation == "" {
			continue
		}
		conditions := []string{}
		for _, name := range []string{"contract_preserved", "source_evidence", "validation", "status"} {
			if v := get(name); v != "" {
				conditions = append(conditions, name+"="+v)
			}
		}
		matrix.Rules = append(matrix.Rules, backend.CompilerEvidence{
			ID: "migration.matrix." + fmt.Sprint(rowNo), Compiler: "compiler migration matrix", Version: "cross-source-evidence-2026-09-13",
			Stage: stage, SemanticIdentity: operation, InputPattern: get("input"), Conditions: conditions,
			OutputPattern: []string{get("output")}, Relation: "migration_contract", Confidence: backend.EvidenceDerived,
		})
	}
	return nil
}

func nativeAnalysis(args []string) error {
	fs := flag.NewFlagSet("native-analysis", flag.ContinueOnError)
	source := fs.String("source", "go", "native source language (currently go)")
	out := fs.String("o", "", "analysis JSON output path")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-source": true, "-o": true})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: native-analysis -source go input.go [-o analysis.json]")
	}
	if backend.NormalizeLanguage(*source) != "go" {
		return fmt.Errorf("native analysis for %q is not implemented", *source)
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	analysis, err := (backend.GoNativeFrontend{}).Analyze(fs.Arg(0), string(data))
	if err != nil {
		return err
	}
	if *out == "" {
		return json.NewEncoder(os.Stdout).Encode(analysis)
	}
	encoded, err := json.MarshalIndent(analysis, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(*out, encoded, 0644)
}

func semanticTranspile(args []string) error {
	fs := flag.NewFlagSet("semantic-transpile", flag.ContinueOnError)
	target := fs.String("target", "go", "target language")
	out := fs.String("o", "", "output path")
	moduleRoot := fs.String("module-root", "", "Semantic module store root")
	license := fs.Bool("license", false, "copy licenses of imported packages beside output")
	fs.BoolVar(license, "l", false, "alias for -license")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-target": true, "-o": true, "-module-root": true, "-license": false, "-l": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: semantic-transpile -target <language> program.semantic.json [-o output]")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var code string
	if isSemanticPath(fs.Arg(0)) {
		code, err = manytomany.TranspileSemanticSP(*target, data)
	} else {
		program, e := manytomany.ParseDocument(data)
		if e != nil {
			return e
		}
		code, err = manytomany.Emit(*target, program)
	}
	if err != nil {
		return err
	}
	if *out == "" {
		fmt.Print(code)
		return nil
	}
	if err := os.WriteFile(*out, []byte(code), 0644); err != nil {
		return err
	}
	if *license {
		_, err = backend.CopyImportedPackageLicenses(*moduleRoot, *out)
	}
	return err
}

func semanticConvert(args []string) error {
	fs := flag.NewFlagSet("semantic-convert", flag.ContinueOnError)
	out := fs.String("o", "", "output path (.sp or .json)")
	preserveSource := fs.Bool("preserve-source", false, "include original source provenance in .se output")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-o": true, "-preserve-source": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 || *out == "" {
		return fmt.Errorf("usage: semantic-convert input.semantic.json|input.sp -o output.sp|output.json")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var encoded []byte
	if isSemanticPath(fs.Arg(0)) {
		var p *backend.SemanticProgram
		var e error
		if isSemanticCompressedPath(fs.Arg(0)) {
			p, e = backend.ParseSemanticSPZ(data)
		} else if strings.HasSuffix(strings.ToLower(fs.Arg(0)), ".se") && len(data) >= 100*1024*1024 {
			// Large distribution graphs contain a redundant evidence plane. Import
			// the canonical UAST graph directly and derive that plane once instead
			// of materializing the complete native-SE reflection tree.
			var u *backend.UniversalASTDocument
			u, e = backend.ParseSemanticSEGraph(data)
			if e == nil {
				var evidence backend.SemanticEvidence
				evidence, e = backend.AnalyzeUniversalEvidence(u)
				u.Evidence = evidence
				p = &backend.SemanticProgram{UniversalAST: u}
			}
		} else {
			p, e = parseSemanticInput(fs.Arg(0), data)
		}
		if e != nil {
			return e
		}
		if isSemanticCompressedPath(*out) {
			encoded, err = p.MarshalSemanticSPZ()
		} else if isSemanticTextPath(*out) {
			if isSemanticTextPath(*out) && strings.HasSuffix(strings.ToLower(*out), ".se") {
				if *preserveSource {
					encoded, err = p.MarshalSemanticSEWithSource()
				} else {
					encoded, err = p.MarshalSemanticSEReadable()
				}
			} else {
				encoded, err = p.MarshalSemanticSP()
			}
		} else {
			encoded, err = p.MarshalSemanticJSON()
		}
	} else {
		p, e := backend.ParseSemanticJSON(data)
		if e != nil {
			return e
		}
		if isSemanticCompressedPath(*out) {
			encoded, err = p.MarshalSemanticSPZ()
		} else if isSemanticTextPath(*out) {
			if isSemanticTextPath(*out) && strings.HasSuffix(strings.ToLower(*out), ".se") {
				if *preserveSource {
					encoded, err = p.MarshalSemanticSEWithSource()
				} else {
					encoded, err = p.MarshalSemanticSEReadable()
				}
			} else {
				encoded, err = p.MarshalSemanticSP()
			}
		} else {
			encoded, err = p.MarshalSemanticJSON()
		}
	}
	if err != nil {
		return err
	}
	return os.WriteFile(*out, encoded, 0644)
}

func semanticFormat(args []string) error {
	fs := flag.NewFlagSet("semantic-format", flag.ContinueOnError)
	out := fs.String("o", "", "formatted SP output")
	preserveSource := fs.Bool("preserve-source", false, "include original source provenance in .se output")
	readable := fs.Bool("readable", false, "pretty readable SE with four-space indentation")
	compact := fs.Bool("compact", false, "minimal canonical SE")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-o": true, "-preserve-source": false, "--preserve-source": false, "-readable": false, "--readable": false, "-compact": false, "--compact": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: semantic-format input.sp [-o output.sp]")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	// All semantic transports share one formatter boundary.  Parse once by
	// content/extension, then choose the requested canonical representation.
	if *readable && *compact {
		return fmt.Errorf("-readable and -compact are mutually exclusive")
	}
	p, pe := parseSemanticInput(fs.Arg(0), data)
	if pe != nil {
		return pe
	}
	formatOut := strings.ToLower(filepath.Ext(*out))
	if *readable || *compact || formatOut == ".se" || (formatOut == "" && strings.HasSuffix(strings.ToLower(fs.Arg(0)), ".se")) {
		if *compact {
			formatted, e := p.MarshalSemanticSECompact()
			if e != nil {
				return e
			}
			if *out == "" {
				_, e = os.Stdout.Write(formatted)
				return e
			}
			return os.WriteFile(*out, formatted, 0644)
		}
		formatted, e := p.MarshalSemanticSEReadable()
		if e != nil {
			return e
		}
		if *out == "" {
			_, e = os.Stdout.Write(formatted)
			return e
		}
		return os.WriteFile(*out, formatted, 0644)
	}
	if formatOut == ".spz" || (formatOut == "" && strings.HasSuffix(strings.ToLower(fs.Arg(0)), ".spz")) {
		formatted, e := p.MarshalSemanticSPZ()
		if e != nil {
			return e
		}
		if *out == "" {
			_, e = os.Stdout.Write(formatted)
			return e
		}
		return os.WriteFile(*out, formatted, 0644)
	}
	if formatOut == ".json" {
		formatted, e := p.MarshalSemanticJSON()
		if e != nil {
			return e
		}
		return os.WriteFile(*out, formatted, 0644)
	}
	if formatOut == ".sp" || *out != "" {
		formatted, e := p.MarshalSemanticSP()
		if e != nil {
			return e
		}
		return os.WriteFile(*out, formatted, 0644)
	}
	var formatted []byte
	if isSemanticCompressedPath(fs.Arg(0)) {
		if isSemanticCompressedPath(*out) || *out == "" {
			formatted, err = backend.FormatSemanticSPZ(data)
		} else {
			p, e := backend.ParseSemanticSPZ(data)
			if e != nil {
				return e
			}
			if strings.HasSuffix(strings.ToLower(*out), ".se") {
				if *preserveSource {
					formatted, err = p.MarshalSemanticSEWithSource()
				} else {
					formatted, err = p.MarshalSemanticSEReadable()
				}
			} else {
				formatted, err = p.MarshalSemanticSP()
			}
		}
	} else {
		if isSemanticCompressedPath(*out) {
			p, e := backend.ParseSemanticSP(data)
			if e != nil {
				return e
			}
			formatted, err = p.MarshalSemanticSPZ()
		} else {
			if strings.HasSuffix(strings.ToLower(fs.Arg(0)), ".se") {
				formatted, err = backend.FormatSemanticSE(data)
			} else {
				formatted, err = backend.FormatSemanticSP(data)
			}
		}
	}
	if err != nil {
		return err
	}
	if *readable && *compact {
		return fmt.Errorf("-readable and -compact are mutually exclusive")
	}
	if strings.HasSuffix(strings.ToLower(fs.Arg(0)), ".se") && (*readable || *compact) {
		p, e := backend.ParseSemanticSE(data)
		if e != nil {
			return e
		}
		if *readable {
			formatted, err = p.MarshalSemanticSEReadable()
		} else {
			formatted, err = p.MarshalSemanticSECompact()
		}
	}
	if *out == "" {
		_, err = os.Stdout.Write(formatted)
		return err
	}
	return os.WriteFile(*out, formatted, 0644)
}

func semanticValidate(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: semantic-validate input.sp|input.json")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	if isSemanticCompressedPath(args[0]) {
		_, err = parseSemanticInput(args[0], data)
	} else if isSemanticTextPath(args[0]) {
		_, err = parseSemanticInput(args[0], data)
	} else {
		_, err = backend.ParseSemanticJSON(data)
	}
	if err != nil {
		return err
	}
	fmt.Println("VALID")
	return nil
}

func semanticInfo(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: semantic-info input.sp|input.json")
	}
	data, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	var p *backend.SemanticProgram
	if isSemanticCompressedPath(args[0]) {
		p, err = parseSemanticInput(args[0], data)
	} else if isSemanticTextPath(args[0]) {
		p, err = parseSemanticInput(args[0], data)
	} else {
		p, err = backend.ParseSemanticJSON(data)
	}
	if err != nil {
		return err
	}
	info := map[string]any{"schema_version": backend.SemanticSPVersion, "source_language": p.Origin.SourceLanguage, "evaluation": p.Evaluation, "uast": p.UniversalAST != nil, "semantic_nodes": len(p.Evidence.Nodes)}
	if p.UniversalAST != nil && p.UniversalAST.LanguageProfile != "" {
		info["source_language"] = p.UniversalAST.LanguageProfile
	}
	return json.NewEncoder(os.Stdout).Encode(info)
}
func launchGUI() {
	go func() {
		a := ui.New()
		if err := a.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}()
	app.Main()
}
func runSource(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	source := fs.String("source", "auto", "source language or auto")
	fs.StringVar(source, "from", "auto", "alias of -source")
	target := fs.String("target", "embedded", "execution target: embedded or any registered target")
	fs.StringVar(target, "to", "embedded", "alias of -target")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-source": true, "-from": true, "-target": true, "-to": true})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: run -source <language|auto> -target <embedded|language> input")
	}
	input := fs.Arg(0)
	if isSemanticPath(input) {
		data, err := os.ReadFile(input)
		if err != nil {
			return err
		}
		program, err := parseSemanticInput(input, data)
		if err != nil {
			return err
		}
		if strings.EqualFold(*target, "embedded") || strings.EqualFold(*target, "semantic") {
			out, err := backend.RunSemantic(program)
			if out != "" {
				fmt.Print(out)
			}
			return err
		}
		code, err := manytomany.Emit(*target, manytomany.Program{Source: "semantic", Semantic: program})
		if err != nil {
			return err
		}
		res, err := targetrun.RunSource(*target, *target, code)
		if res.Stdout != "" {
			fmt.Print(res.Stdout)
		}
		if res.Stderr != "" {
			fmt.Fprint(os.Stderr, res.Stderr)
		}
		return err
	}
	resolvedSource, err := sourceLanguage(*source, input)
	if err != nil {
		return err
	}
	b, err := os.ReadFile(input)
	if err != nil {
		return err
	}
	res, err := targetrun.RunSource(*target, resolvedSource, string(b))
	if res.Stdout != "" {
		fmt.Print(res.Stdout)
	}
	if res.Stderr != "" {
		fmt.Fprint(os.Stderr, res.Stderr)
	}
	if err != nil {
		return err
	}
	return nil
}

// reorderValueFlags accepts the documented CLI style where output flags may
// follow the input file, while retaining the standard flag package.
func reorderValueFlags(args []string, supported map[string]bool) []string {
	flags, positional := make([]string, 0, len(args)), make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if takesValue, known := supported[args[i]]; known && !takesValue {
			flags = append(flags, args[i])
			continue
		}
		if supported[args[i]] && i+1 < len(args) {
			flags = append(flags, args[i], args[i+1])
			i++
			continue
		}
		positional = append(positional, args[i])
	}
	return append(flags, positional...)
}

type batchRequest struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Code   string `json:"code"`
}
type batchResponse struct {
	ID    string `json:"id"`
	Code  string `json:"code,omitempty"`
	Error string `json:"error,omitempty"`
}

func translateRequest(r batchRequest) (out batchResponse) {
	out.ID = r.ID
	defer func() {
		if p := recover(); p != nil {
			out.Code = ""
			out.Error = fmt.Sprintf("translation panic: %v", p)
		}
	}()
	code, err := manytomany.Transpile(r.Source, r.Target, r.Code)
	if err != nil {
		out.Error = err.Error()
	} else {
		out.Code = code
	}
	return
}
func transpileBatch() error {
	var requests []batchRequest
	if err := json.NewDecoder(os.Stdin).Decode(&requests); err != nil {
		return err
	}
	responses := make([]batchResponse, len(requests))
	for i, r := range requests {
		responses[i] = translateRequest(r)
	}
	return json.NewEncoder(os.Stdout).Encode(responses)
}

const helpText = `Semantic Programming Language - SemanticProgram v1

COMMAND ALIASES (EXACTLY EQUIVALENT)
  sp <command> [options]     ==    CodeTranspiler.exe <command> [options]
  se files are readable SemanticProgram; sp remains a compatibility alias.
  The complete command set below is valid with either executable name.

Native compiler (direct machine encoder; assembly optional):
  sp compile input.sp -o input.exe
  sp compile input.json -o input.exe
	sp compile program.se --embed-all-modules -o whole-program.exe
	  By default, compile follows serialized semantic_module_link_roots; this
	  flag deliberately embeds every declared module.
	  Semantic imports can resolve from another store with -module-root DIR.
	  Native builds retain status, source-unit inventory and a real program.obj
	  below %TEMP%\\CodeTranspiler\\builds by default.
	  --direct-exe disables retained intermediates; --build-dir DIR selects them.
  CodeTranspiler.exe compile -source go -target native-x86_64-windows input.go -entry entry -o program.exe
  CodeTranspiler.exe compile -source go -target object-x86_64-windows input.go -entry entry -o program.obj
  CodeTranspiler.exe compile -source go -target machine-x86_64 input.go -entry entry -o program.bin
  CodeTranspiler.exe compile -source go -target asm-x86_64 input.go -entry entry -o program.asm
  Optional: --via-assembly (requires NASM; direct compilation does not).
  Experimental common-subset translation; not full semantic compatibility.
  Runtime support SOURCE is embedded. Native target compilers are not bundled.

Optional LLVM compiler route (canonical UAST -> LLVM IR -> LLVM toolchain):
  sp compile-llvm input.sp -o input.exe
  sp compile-llvm input.json -o input.exe
  sp compile-llvm input.se --emit-llvm -o input.ll
  sp compile-llvm input.se -output object -o input.obj
  Options: -target-triple, -O 0..3, -entry, -module-root, -llvm-path, -clang, -llc, -lld-link.
  LLVM tools are external and are not bundled; --emit-llvm needs no LLVM install.

External C toolchain route (canonical UAST -> target-legal C -> compiler):
  sp compile-toolchain input.se -toolchain auto -o input.exe
  sp compile-gcc input.se -o input.exe
  sp compile-msvc input.se -o input.exe
  sp compile-toolchain input.go -source go -toolchain gcc -output object -o input.obj
  Options: -source, -toolchain auto|gcc|mingw|msvc, -compiler, -msvc-setup, -output, -O.
  GCC/MinGW and MSVC are external and are discovered through PATH/vswhere.

BATCH TRANSLATION
  CodeTranspiler.exe transpile-batch
      Read a JSON array of {id,source,target,code} from stdin.
      Write a JSON array of {id,code,error} to stdout. No code is executed.

GENERAL
  CodeTranspiler.exe
      Start the graphical user interface.

  CodeTranspiler.exe gui
      Start the graphical user interface.

  CodeTranspiler.exe help
  CodeTranspiler.exe --help
  CodeTranspiler.exe -h
      Show this complete command reference.

  CodeTranspiler.exe version
  CodeTranspiler.exe --version
      Show the Semantic Programming Language version.

  CodeTranspiler.exe targets
      List all target-language IDs.

  CodeTranspiler.exe languages
      List all source/target language IDs and extensions.

  CodeTranspiler.exe routes
      List all 156 directed source-to-target routes.

  CodeTranspiler.exe semantic-export -source c input.c -o program.semantic.json
      Parse source and save the complete SemanticProgram JSON document.
  CodeTranspiler.exe semantic-csc input.cs -o program.semantic.json
  CodeTranspiler.exe compile-csc input.se -o program.exe
      Project a SemanticProgram through the canonical C# target and csc.exe.
      -module-root DIR selects the Semantic module store for imported units.
  CodeTranspiler.exe compile-csc-project semantic-directory -o program.exe
      Link independently projected Semantic units through one csc.exe invocation.
      Parse and bind C# with the Roslyn adapter, then emit canonical UAST JSON.
  CodeTranspiler.exe semantic-export -source go input.go -format sp -o program.sp
      Export the same canonical program in readable Semantic Programming form.

  CodeTranspiler.exe semantic-export -input executable program.exe -o program.semantic.json
      Lift a supported x86-64 PE/executable into the same SemanticProgram JSON.

  CodeTranspiler.exe machine-ir -input executable program.exe -o machine-ir.json
      Export decoded x86-64 addressing, CFG and primitive mapping facts.

  CodeTranspiler.exe native-compiler-evidence [-root .] [-out compiler-evidence/native]
      Extract provenance-linked lowering/encoding evidence from this native backend.
      Evidence is analytical and does not modify compiler registries or execution.

      Index local Roslyn/LLVM source checkouts and this project's native backend.
      C#, C++, and TableGen relations keep commit, file hash, symbol, and lines.
      Source evidence does not automatically create semantic equivalences.

  CodeTranspiler.exe decompile -input assembly program.asm -o program.semantic.json
  CodeTranspiler.exe decompile -input machine program.bin -o program.semantic.json
  CodeTranspiler.exe decompile -input executable program.exe -o program.semantic.json
      Lift binary/assembly/object/PE input directly to SemanticProgram JSON.

  CodeTranspiler.exe semantic-transpile -target rust program.semantic.json -o output.rs
      Load SemanticProgram JSON and emit a target without original source.
  CodeTranspiler.exe semantic-transpile -target rust program.sp -o output.rs
      Load Semantic Programming source and emit a target without reparsing source.

  CodeTranspiler.exe semantic-convert input.json -o output.sp

  CodeTranspiler.exe semantic-link program.se -o linked.se
  CodeTranspiler.exe semantic-link --embed-all program.se -o whole-program.se
      Materialize selected Semantic module roots, or explicitly embed every
      declared module into one self-contained SemanticProgram document.

  CodeTranspiler.exe semantic-format input.se --readable -o readable.se
  CodeTranspiler.exe semantic-format input.se --compact -o compact.se
  CodeTranspiler.exe semantic-validate input.sp
  CodeTranspiler.exe semantic-info input.sp
      Convert, canonicalize, validate, or inspect Semantic Programming documents.

  CodeTranspiler.exe module import <source|module.se|module.spz>
  CodeTranspiler.exe module import --language go <source.go>
  CodeTranspiler.exe module import --language python six
  CodeTranspiler.exe module import --language rust itoa
  CodeTranspiler.exe module import --language r jsonlite
  CodeTranspiler.exe module import --language java org.apache.commons:commons-lang3
  CodeTranspiler.exe module create a.se b.spz -o mymodule.smod
  CodeTranspiler.exe module merge a.se b.sp -o mymodule.smod
  CodeTranspiler.exe module path
  CodeTranspiler.exe module setpath <parent-folder>
  CodeTranspiler.exe module setpath --default
  CodeTranspiler.exe module list
  CodeTranspiler.exe module info <cache-key>
  CodeTranspiler.exe module verify <cache-key>
  CodeTranspiler.exe module remove <cache-key>
  CodeTranspiler.exe semantic module import <target>
  CodeTranspiler.exe semantic module import --language go <target>
      Download packages, preserve their source tree and licenses, transpile
      supported units to SPZ, and manage Semantic Modules below the configured
      <base>\\Semantic\\Modules directory.
      All commands are also available through the exact alias: sp <command>.

  CodeTranspiler.exe capability go core
      Print the backend capability contract as JSON.

  CodeTranspiler.exe native-analysis -source go input.go -o analysis.json
      Extract native types, source spans and symbol matrices (analysis only).

  CodeTranspiler.exe semantic-export -native -source go input.go -o program.json
      Direct executable Go scalar frontend; unsupported syntax is rejected.

  CodeTranspiler.exe capability-matrix [additional-feature ...]
      Print feature-by-target capability status matrices as JSON.

  CodeTranspiler.exe implementation-matrix
      Show typed operation implementation stages; not a test coverage claim.

  CodeTranspiler.exe transpile -from c -to rust input.c -o output.rs
  CodeTranspiler.exe transpile input.py -target all -o translated
      Auto-detect source by extension; all emits every registered target and
      translation-report.json. Failures return a nonzero exit status.
      Add -native for strict native semantics, without legacy fallback.

  CodeTranspiler.exe runtimes
      List the runtime bundles embedded directly in CodeTranspiler.exe.

  CodeTranspiler.exe bundle-info
  CodeTranspiler.exe bundle-verify
      Verify and report the embedded frontend, UAST, and backend Semantic bundles.

  CodeTranspiler.exe bundle-extract <directory>
      Verify and extract all three complete embedded Semantic bundles.

  CodeTranspiler.exe licenses
      Show embedded third-party license notices (including Tree-sitter).

UNIVERSAL TRANSPILATION
  CodeTranspiler.exe transpile -from c -to rust input.c -o output.rs
  CodeTranspiler.exe transpile -from python -to go input.py -o output.go
  CodeTranspiler.exe transpile input.swift -target all -o translated
      Every registered source language can be projected to every registered
      target language. Source is inferred from the extension when possible.

UNIVERSAL EXECUTION
  CodeTranspiler.exe run -source python -target embedded input.py
  CodeTranspiler.exe run -from c -to go input.c
      Parse the selected source through UAST, then execute with the embedded
      UAST runtime or the chosen target compiler/runtime.

TARGET IDS
  go
  rust
  cpp
  c
  python
  zig
  julia
  nim
  csharp
  java
  kotlin
  swift

RUN BEHAVIOR
  run -source <language|auto> -target <embedded|language> input
      Parses any registered source language through the canonical UAST,
      then executes it with the embedded UAST runtime or a target toolchain.

OUTPUT FILE BEHAVIOR
  If -o is omitted, CodeTranspiler creates an output filename beside the input file
  using the selected target extension.

TARGET TOOLCHAINS
  Go      : go
  Rust    : rustc
  C++     : g++ or clang++
  C       : gcc or clang
  Python  : python / python3 / py
  Zig     : zig
  Julia   : julia
  Nim     : nim
  C#      : csc or dotnet
  Java    : javac + java
  Kotlin  : kotlinc + java
  Swift   : swift or swiftc

EXAMPLES
  CodeTranspiler.exe run -source python -target embedded analysis.py
  CodeTranspiler.exe run -from c -to go program.c
  CodeTranspiler.exe transpile -from rust -to swift input.rs -o output.swift
  CodeTranspiler.exe transpile input.py -target all -o translated
  CodeTranspiler.exe transpile -target python analysis.R -o analysis.py
  CodeTranspiler.exe transpile -target zig analysis.R -o analysis.zig
  CodeTranspiler.exe transpile -target julia analysis.R -o analysis.jl
  CodeTranspiler.exe transpile -target nim analysis.R -o analysis.nim
  CodeTranspiler.exe transpile -target csharp analysis.R -o analysis.cs
  CodeTranspiler.exe transpile -target java analysis.R -o Main.java
  CodeTranspiler.exe transpile -target kotlin analysis.R -o analysis.kt
  CodeTranspiler.exe transpile -target swift analysis.R -o analysis.swift
`
