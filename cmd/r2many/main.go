package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gioui.org/app"
	"r2many/internal/backend"
	"r2many/internal/manytomany"
	"r2many/internal/platform"
	"r2many/internal/runtimeassets"
	"r2many/internal/targetrun"
	"r2many/internal/ui"
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
		fmt.Println("Code Transpiler v0.4")
	case "targets":
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
			fmt.Printf("%s\t%d embedded runtime files\n", t, len(files))
		}
	case "run":
		if err := runR(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	case "transpile":
		if err := transpile(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "r2many:", err)
			os.Exit(1)
		}
	default:
		fmt.Print(helpText)
		os.Exit(2)
	}
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
func runR(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	target := fs.String("target", "embedded", "execution target: embedded, go, rust, cpp, c, python, zig, julia, nim, csharp, java, kotlin, swift")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: r2many run [-target go] input.R")
	}
	b, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	res, err := targetrun.RunRSource(*target, string(b))
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
func transpile(args []string) error {
	fs := flag.NewFlagSet("transpile", flag.ContinueOnError)
	source := fs.String("source", "r", "source language")
	target := fs.String("target", "go", "target language")
	out := fs.String("o", "", "output path")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("expected one source file")
	}
	in := fs.Arg(0)
	b, err := os.ReadFile(in)
	if err != nil {
		return err
	}
	code, err := manytomany.Transpile(*source, *target, string(b))
	if err != nil {
		return err
	}
	if *out == "" {
		l, ok := backend.ByID(*target)
		if !ok {
			return fmt.Errorf("unknown target %q", *target)
		}
		*out = strings.TrimSuffix(in, filepath.Ext(in)) + l.Extension
	}
	return os.WriteFile(*out, []byte(code), 0644)
}

const helpText = `R2Many - R to many languages transpiler

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

  CodeTranspiler.exe runtimes
      List the runtime bundles embedded directly in CodeTranspiler.exe.

RUN R WITH THE EMBEDDED R2MANY R RUNTIME
  CodeTranspiler.exe run input.R

RUN AN R SCRIPT THROUGH EACH TARGET LANGUAGE
  CodeTranspiler.exe run -target go input.R
  CodeTranspiler.exe run -target rust input.R
  CodeTranspiler.exe run -target cpp input.R
  CodeTranspiler.exe run -target c input.R
  CodeTranspiler.exe run -target python input.R
  CodeTranspiler.exe run -target zig input.R
  CodeTranspiler.exe run -target julia input.R
  CodeTranspiler.exe run -target nim input.R
  CodeTranspiler.exe run -target csharp input.R
  CodeTranspiler.exe run -target java input.R
  CodeTranspiler.exe run -target kotlin input.R
  CodeTranspiler.exe run -target swift input.R

TRANSPILATION - GO
  CodeTranspiler.exe transpile -source r -target go input.R
  CodeTranspiler.exe transpile -source r -target go input.R -o output.go

TRANSPILATION - RUST
  CodeTranspiler.exe transpile -target rust input.R
  CodeTranspiler.exe transpile -target rust input.R -o output.rs

TRANSPILATION - C++
  CodeTranspiler.exe transpile -target cpp input.R
  CodeTranspiler.exe transpile -target cpp input.R -o output.cpp

TRANSPILATION - C
  CodeTranspiler.exe transpile -target c input.R
  CodeTranspiler.exe transpile -target c input.R -o output.c

TRANSPILATION - PYTHON
  CodeTranspiler.exe transpile -target python input.R
  CodeTranspiler.exe transpile -target python input.R -o output.py

TRANSPILATION - ZIG
  CodeTranspiler.exe transpile -target zig input.R
  CodeTranspiler.exe transpile -target zig input.R -o output.zig

TRANSPILATION - JULIA
  CodeTranspiler.exe transpile -target julia input.R
  CodeTranspiler.exe transpile -target julia input.R -o output.jl

TRANSPILATION - NIM
  CodeTranspiler.exe transpile -target nim input.R
  CodeTranspiler.exe transpile -target nim input.R -o output.nim

TRANSPILATION - C#
  CodeTranspiler.exe transpile -target csharp input.R
  CodeTranspiler.exe transpile -target csharp input.R -o output.cs

TRANSPILATION - JAVA
  CodeTranspiler.exe transpile -target java input.R
  CodeTranspiler.exe transpile -target java input.R -o Main.java

TRANSPILATION - KOTLIN
  CodeTranspiler.exe transpile -target kotlin input.R
  CodeTranspiler.exe transpile -target kotlin input.R -o output.kt

TRANSPILATION - SWIFT
  CodeTranspiler.exe transpile -target swift input.R
  CodeTranspiler.exe transpile -target swift input.R -o output.swift

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
  run input.R
      Executes the R source directly with the R2Many compatibility runtime
      embedded in CodeTranspiler.exe.

  run -target <language> input.R
      Transpiles the R source to the selected target language and executes
      the generated program.

OUTPUT FILE BEHAVIOR
  If -o is omitted, R2Many creates an output filename beside the input file
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
  CodeTranspiler.exe run analysis.R
  CodeTranspiler.exe run -target go analysis.R
  CodeTranspiler.exe run -target rust analysis.R
  CodeTranspiler.exe run -target python analysis.R

  CodeTranspiler.exe transpile -target go analysis.R -o analysis.go
  CodeTranspiler.exe transpile -target rust analysis.R -o analysis.rs
  CodeTranspiler.exe transpile -target cpp analysis.R -o analysis.cpp
  CodeTranspiler.exe transpile -target c analysis.R -o analysis.c
  CodeTranspiler.exe transpile -target python analysis.R -o analysis.py
  CodeTranspiler.exe transpile -target zig analysis.R -o analysis.zig
  CodeTranspiler.exe transpile -target julia analysis.R -o analysis.jl
  CodeTranspiler.exe transpile -target nim analysis.R -o analysis.nim
  CodeTranspiler.exe transpile -target csharp analysis.R -o analysis.cs
  CodeTranspiler.exe transpile -target java analysis.R -o Main.java
  CodeTranspiler.exe transpile -target kotlin analysis.R -o analysis.kt
  CodeTranspiler.exe transpile -target swift analysis.R -o analysis.swift
`
