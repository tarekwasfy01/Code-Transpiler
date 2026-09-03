package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

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
	platform.EnsureCLIConsole()
	switch os.Args[1] {
	case "gui":
		launchGUI()
	case "help", "--help", "-h":
		fmt.Print(helpText)
	case "version", "--version":
		fmt.Printf("Code Transpiler %s\ncommit=%s\nbuild_date=%s\nengine=TranspileCore/UAST\n", version, commit, buildDate)
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
	default:
		fmt.Print(helpText)
		os.Exit(2)
	}
}

func semanticExport(args []string) error {
	fs := flag.NewFlagSet("semantic-export", flag.ContinueOnError)
	native := fs.Bool("native", false, "use strict native frontend without legacy fallback")
	source := fs.String("source", "r", "source language")
	out := fs.String("o", "", "SemanticProgram JSON output path")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{"-source": true, "-o": true, "-native": false})); err != nil {
		return err
	}
	if fs.NArg() != 1 || *out == "" {
		return fmt.Errorf("usage: semantic-export -source <language> input -o program.semantic.json")
	}
	data, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	var semantic *backend.SemanticProgram
	if *native {
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
	encoded, err := semantic.MarshalSemanticJSON()
	if err != nil {
		return err
	}
	return os.WriteFile(*out, encoded, 0644)
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
	program, err := manytomany.ParseDocument(data)
	if err != nil {
		return err
	}
	code, err := manytomany.Emit(*target, program)
	if err != nil {
		return err
	}
	if *out == "" {
		fmt.Print(code)
		return nil
	}
	return os.WriteFile(*out, []byte(code), 0644)
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

const helpText = `Code Transpiler v1.0 - SemanticProgram v1
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
      Show the Code Transpiler version.

  CodeTranspiler.exe targets
      List all target-language IDs.

  CodeTranspiler.exe languages
      List all source/target language IDs and extensions.

  CodeTranspiler.exe routes
      List all 156 directed source-to-target routes.

  CodeTranspiler.exe semantic-export -source c input.c -o program.semantic.json
      Parse source and save the complete SemanticProgram JSON document.

  CodeTranspiler.exe semantic-transpile -target rust program.semantic.json -o output.rs
      Load SemanticProgram JSON and emit a target without original source.

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
