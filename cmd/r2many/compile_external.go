// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	codetranspiler "github.com/tarekwasfy01/Code-Transpiler"
	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
)

func compileExternal(args []string) error {
	fs := flag.NewFlagSet("compile-toolchain", flag.ContinueOnError)
	out := fs.String("o", "", "output file")
	sourceLanguage := fs.String("source", "semantic", "source language")
	targetLanguage := fs.String("target", "c", "native source target c|cpp")
	family := fs.String("toolchain", "auto", "auto|gcc|mingw|msvc")
	compiler := fs.String("compiler", "", "explicit gcc/cl executable")
	msvcSetup := fs.String("msvc-setup", "", "explicit vcvars64.bat or VsDevCmd.bat")
	output := fs.String("output", "executable", "source|object|executable")
	optimization := fs.Int("O", 2, "optimization level 0..3")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{
		"-o": true, "-source": true, "-target": true, "-toolchain": true, "-compiler": true,
		"-msvc-setup": true, "-output": true, "-O": true,
	})); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: compile-toolchain input.go|input.se|input.json -source <language> -toolchain auto|gcc|msvc -o program.exe")
	}
	input := fs.Arg(0)
	program, err := loadProgramForExternalCompiler(input, *sourceLanguage)
	if err != nil {
		return err
	}
	kind := codetranspiler.Executable
	switch strings.ToLower(*output) {
	case "source":
		kind = codetranspiler.Source
	case "object":
		kind = codetranspiler.Object
	case "executable", "exe":
	default:
		return fmt.Errorf("unsupported external compiler output %q", *output)
	}
	selected := strings.ToLower(strings.TrimSpace(*family))
	if selected == "mingw" {
		selected = "gcc"
	}
	result, err := codetranspiler.CompileExternalC(program, codetranspiler.ExternalCCompileOptions{
		Family: codetranspiler.ExternalCompilerFamily(selected), TargetLanguage: *targetLanguage, OutputKind: kind,
		CompilerPath: *compiler, MSVCSetupPath: *msvcSetup, Optimization: *optimization,
	})
	if err != nil {
		return err
	}
	if *out == "" {
		return fmt.Errorf("-o is required")
	}
	data := result.Bytes
	if kind == codetranspiler.Source {
		data = []byte(result.Source)
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		return err
	}
	fmt.Printf("external compiler family=%s compiler=%s projection=%s output=%s bytes=%d source_sha256=%s artifact_sha256=%s\n",
		result.Family, result.CompilerPath, result.ProjectionMode, *out, len(data), result.SourceSHA256, result.ArtifactSHA256)
	return nil
}

func loadProgramForExternalCompiler(path, language string) (*backend.SemanticProgram, error) {
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".se" || ext == ".sp" || ext == ".spz" || ext == ".json" || strings.EqualFold(language, "semantic") || strings.EqualFold(language, "se") || strings.EqualFold(language, "sp") {
		return loadSemanticProgramForLLVM(path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(language, "go") {
		if program, nativeErr := backend.LowerNativeGo(path, string(data)); nativeErr == nil {
			return program, nil
		}
	}
	return backend.LowerSource(language, path, string(data))
}
