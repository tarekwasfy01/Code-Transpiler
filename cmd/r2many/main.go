// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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
			fmt.Print(helpText)
			return
		}
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	}
	platform.EnsureCLIConsole()
	switch os.Args[1] {
	case "gui":
		launchGUI()
	case "help", "--help", "-h":
		fmt.Print(helpText)
	case "version", "--version":
		fmt.Printf("Semantic Programming Language %s\ncommit=%s\nbuild_date=%s\nengine=TranspileCore/UAST\n", version, commit, buildDate)
	case "licenses", "licences", "--licenses", "--licences":
		fmt.Println("Tree-sitter (MIT License)")
		fmt.Println(thirdpartylicenses.TreeSitter)
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
	case "machine-ir":
		if err := machineIRExport(os.Args[2:]); err != nil {
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
			fmt.Print(helpText)
			os.Exit(2)
		}
	default:
		fmt.Print(helpText)
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
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-source": true, "-input": true, "-o": true, "-format": true, "-native": false, "-preserve-source": false})); err != nil {
		return err
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
	return os.WriteFile(*out, encoded, 0644)
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
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-target": true, "-o": true})); err != nil {
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
	return os.WriteFile(*out, []byte(code), 0644)
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
		} else {
			p, e = backend.ParseSemanticSP(data)
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
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-o": true, "-preserve-source": false, "-readable": false, "-compact": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: semantic-format input.sp [-o output.sp]")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
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
		_, err = backend.ParseSemanticSPZ(data)
	} else if isSemanticTextPath(args[0]) {
		_, err = backend.ParseSemanticSP(data)
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
		p, err = backend.ParseSemanticSPZ(data)
	} else if isSemanticTextPath(args[0]) {
		p, err = backend.ParseSemanticSP(data)
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
  CodeTranspiler.exe compile -source go -target native-x86_64-windows input.go -entry entry -o program.exe
  CodeTranspiler.exe compile -source go -target object-x86_64-windows input.go -entry entry -o program.obj
  CodeTranspiler.exe compile -source go -target machine-x86_64 input.go -entry entry -o program.bin
  CodeTranspiler.exe compile -source go -target asm-x86_64 input.go -entry entry -o program.asm
  Optional: --via-assembly (requires NASM; direct compilation does not).
Experimental common-subset translation; not full semantic compatibility.
Runtime support SOURCE is embedded. Native target compilers are not bundled.

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
  CodeTranspiler.exe semantic-export -source go input.go -format sp -o program.sp
      Export the same canonical program in readable Semantic Programming form.

  CodeTranspiler.exe semantic-export -input executable program.exe -o program.semantic.json
      Lift a supported x86-64 PE/executable into the same SemanticProgram JSON.

  CodeTranspiler.exe machine-ir -input executable program.exe -o machine-ir.json
      Export decoded x86-64 addressing, CFG and primitive mapping facts.

  CodeTranspiler.exe decompile -input assembly program.asm -o program.semantic.json
  CodeTranspiler.exe decompile -input machine program.bin -o program.semantic.json
  CodeTranspiler.exe decompile -input executable program.exe -o program.semantic.json
      Lift binary/assembly/object/PE input directly to SemanticProgram JSON.

  CodeTranspiler.exe semantic-transpile -target rust program.semantic.json -o output.rs
      Load SemanticProgram JSON and emit a target without original source.
  CodeTranspiler.exe semantic-transpile -target rust program.sp -o output.rs
      Load Semantic Programming source and emit a target without reparsing source.

  CodeTranspiler.exe semantic-convert input.json -o output.sp
  CodeTranspiler.exe semantic-format input.se --readable -o readable.se
  CodeTranspiler.exe semantic-format input.se --compact -o compact.se
  CodeTranspiler.exe semantic-validate input.sp
  CodeTranspiler.exe semantic-info input.sp
      Convert, canonicalize, validate, or inspect Semantic Programming documents.

  CodeTranspiler.exe module import <source|module.se|module.spz>
  CodeTranspiler.exe module import --language go <source.go>
  CodeTranspiler.exe module list
  CodeTranspiler.exe module info <cache-key>
  CodeTranspiler.exe module verify <cache-key>
  CodeTranspiler.exe module remove <cache-key>
  CodeTranspiler.exe semantic module import <target>
  CodeTranspiler.exe semantic module import --language go <target>
      Manage Semantic Modules in %LOCALAPPDATA%\\Semantic\\Modules.
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
