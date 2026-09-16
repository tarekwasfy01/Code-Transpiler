// Copyright (c) 2026 Tarek Wasfy
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	codetranspiler "github.com/tarekwasfy01/Code-Transpiler/v2"
	"github.com/tarekwasfy01/Code-Transpiler/v2/internal/backend"
)

func compileLLVM(args []string) error {
	fs := flag.NewFlagSet("compile-llvm", flag.ContinueOnError)
	out := fs.String("o", "", "output file")
	output := fs.String("output", "executable", "llvm-ir|assembly|object|executable")
	triple := fs.String("target-triple", "x86_64-pc-windows-msvc", "LLVM target triple")
	optimization := fs.Int("O", 0, "LLVM optimization level 0..3")
	entry := fs.String("entry", "", "entry function")
	moduleRoot := fs.String("module-root", "", "semantic module store root")
	embedAll := fs.Bool("embed-all-modules", false, "embed all declared semantic modules; default follows link roots")
	llvmPath := fs.String("llvm-path", "", "LLVM bin directory or tool executable")
	clang := fs.String("clang", "", "path to clang executable")
	llc := fs.String("llc", "", "path to llc executable")
	llvmAs := fs.String("llvm-as", "", "path to llvm-as executable")
	lldLink := fs.String("lld-link", "", "path to lld-link executable")
	emitIR := fs.Bool("emit-llvm", false, "write textual LLVM IR")
	projectionReport := fs.String("projection-report", "", "write the matrix-derived LLVM projection plan as JSON")
	if err := fs.Parse(reorderValueFlags(args, map[string]bool{
		"-o": true, "-output": true, "-target-triple": true, "-O": true,
		"-entry": true, "-llvm-path": true, "-clang": true, "-llc": true,
		"-llvm-as": true, "-lld-link": true, "-emit-llvm": false,
		"-projection-report": true, "-module-root": true, "-embed-all-modules": false,
		"--o": true, "--output": true, "--target-triple": true, "--O": true,
		"--entry": true, "--llvm-path": true, "--clang": true, "--llc": true,
		"--llvm-as": true, "--lld-link": true, "--emit-llvm": false,
		"--projection-report": true, "--module-root": true, "--embed-all-modules": false,
	})); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: compile-llvm input.sp|input.se|input.json -o program.exe")
	}
	kind := backend.LLVMOutputKind(strings.ToLower(*output))
	if *emitIR {
		kind = backend.LLVMIR
	}
	switch kind {
	case backend.LLVMIR, backend.LLVMAssembly, backend.LLVMObject, backend.LLVMExecutable:
	default:
		return fmt.Errorf("unsupported LLVM output kind %q", *output)
	}
	inputPath := fs.Arg(0)
	if info, statErr := os.Stat(inputPath); statErr == nil && info.IsDir() {
		if kind != backend.LLVMExecutable {
			return fmt.Errorf("compile-llvm directory input requires -output executable")
		}
		projectEntry := *entry
		if projectEntry == "" {
			projectEntry = "main"
		}
		project, loadErr := backend.LoadSemanticProject(inputPath, projectEntry)
		if loadErr != nil {
			return loadErr
		}
		projectResult, compileErr := backend.CompileLLVMProject(project, backend.LLVMCompileOptions{
			OutputKind: backend.LLVMExecutable, TargetTriple: *triple, Optimization: *optimization,
			EntryPoint: projectEntry, ModuleBaseDir: inputPath, ModuleStoreRoot: *moduleRoot, EmbedAllModules: *embedAll,
			CacheDir: filepath.Join(inputPath, ".semantic-cache"),
			LLVMPath: *llvmPath, ClangPath: *clang, LLCPath: *llc,
			LLVMAsPath: *llvmAs, LLDLinkPath: *lldLink,
		})
		if compileErr != nil {
			matrixPath := *out + ".failure-matrix.json"
			if *out == "" {
				matrixPath = filepath.Join(inputPath, "llvm-project-failure-matrix.json")
			}
			message := compileErr.Error()
			unresolved := make([]string, 0)
			for _, line := range strings.Split(message, "\n") {
				line = strings.TrimSpace(line)
				const marker = "undefined symbol:"
				if i := strings.Index(line, marker); i >= 0 {
					name := strings.TrimSpace(strings.TrimPrefix(line[i+len(marker):], ">>>"))
					if name != "" {
						seen := false
						for _, existing := range unresolved {
							if existing == name {
								seen = true
								break
							}
						}
						if !seen {
							unresolved = append(unresolved, name)
						}
					}
				}
			}
			matrix := map[string]any{
				"units_total":        len(project.Units),
				"objects_success":    projectResult.ObjectCount,
				"objects_failed":     len(projectResult.Failures),
				"failures":           projectResult.Failures,
				"compile_error":      message,
				"unresolved_symbols": unresolved,
				"output_kind":        "executable",
				"failure_semantics":  "fail-closed",
			}
			if b, marshalErr := json.MarshalIndent(matrix, "", "  "); marshalErr == nil {
				_ = os.WriteFile(matrixPath, append(b, '\n'), 0644)
			}
			return fmt.Errorf("LLVM project build failed: %w", compileErr)
		}
		if *out == "" {
			return fmt.Errorf("-o is required for compile-llvm directory output")
		}
		if err := os.WriteFile(*out, projectResult.Executable, 0644); err != nil {
			return err
		}
		functions, instructions, objectBytes := 0, 0, 0
		for _, fragment := range projectResult.Fragments {
			functions += len(fragment.SymbolNames)
			objectBytes += len(fragment.Object)
			instructions += strings.Count(fragment.IR, "\n  ")
		}
		fmt.Fprintf(os.Stdout, "LLVM project units_total=%d objects_success=%d objects_failed=0 llvm_functions=%d llvm_instructions=%d total_object_bytes=%d link_input_objects=%d exe_bytes=%d unresolved_symbols=0 link_time_us=%d output=%s\n",
			len(project.Units), projectResult.ObjectCount, functions, instructions, objectBytes,
			len(projectResult.Fragments), len(projectResult.Executable), projectResult.LinkMicros, *out)
		return nil
	}
	program, err := loadSemanticProgramForLLVM(inputPath)
	if err != nil {
		return err
	}
	result, err := codetranspiler.CompileLLVM(program, codetranspiler.LLVMCompileOptions{
		OutputKind: kind, TargetTriple: *triple, Optimization: *optimization,
		EntryPoint: *entry, ModuleBaseDir: filepath.Dir(fs.Arg(0)), ModuleStoreRoot: *moduleRoot, EmbedAllModules: *embedAll,
		LLVMPath: *llvmPath, ClangPath: *clang,
		LLCPath: *llc, LLVMAsPath: *llvmAs, LLDLinkPath: *lldLink,
		EmitIR: *emitIR,
	})
	if err != nil {
		// Preserve the matrix-derived failure evidence even when emission is
		// rejected. This keeps all family/primitive/contract gaps on disk for
		// the next batch, instead of losing them behind the first CLI error.
		if result.ProjectionPlan != nil {
			for _, gap := range result.ProjectionGaps {
				fmt.Fprintf(os.Stdout, "LLVM projection gap: %s\n", gap)
			}
			if *projectionReport != "" {
				report, reportErr := json.MarshalIndent(result.ProjectionPlan, "", "  ")
				if reportErr == nil {
					if writeErr := os.WriteFile(*projectionReport, report, 0644); writeErr != nil {
						return fmt.Errorf("projection report after LLVM failure: %w (original: %v)", writeErr, err)
					}
					fmt.Fprintf(os.Stdout, "LLVM projection failure report=%s bytes=%d\n", *projectionReport, len(report))
					evidencePath := *projectionReport + ".evidence.jsonl"
					if writeErr := backend.WriteLLVMProjectionEvidence(result.ProjectionPlan, evidencePath); writeErr != nil {
						return fmt.Errorf("LLVM projection evidence after failure: %w (original: %v)", writeErr, err)
					}
					fmt.Fprintf(os.Stdout, "LLVM projection failure evidence=%s\n", evidencePath)
				}
			}
		}
		return err
	}
	if *out == "" {
		return fmt.Errorf("-o is required for compile-llvm output")
	}
	data := result.Bytes
	if kind == backend.LLVMIR || *emitIR {
		data = []byte(result.Text)
	}
	if err := os.WriteFile(*out, data, 0644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "LLVM output=%s target=%s functions=%d instructions=%d bytes=%d\n", *out, result.TargetTriple, result.Functions, result.InstructionCount, len(data))
	fmt.Fprintf(os.Stdout, "LLVM projection schema=%s basis=%s families=%s modes=%s gaps=%d\n", result.ProjectionSchema, result.ProjectionBasis, formatProjectionCounts(result.ProjectionFamilies), formatProjectionCounts(result.ProjectionModes), len(result.ProjectionGaps))
	if *projectionReport != "" && result.ProjectionPlan != nil {
		report, err := json.MarshalIndent(result.ProjectionPlan, "", "  ")
		if err != nil {
			return fmt.Errorf("projection report: %w", err)
		}
		if err := os.WriteFile(*projectionReport, report, 0644); err != nil {
			return fmt.Errorf("projection report: %w", err)
		}
		fmt.Fprintf(os.Stdout, "LLVM projection report=%s bytes=%d\n", *projectionReport, len(report))
		evidencePath := *projectionReport + ".evidence.jsonl"
		if err := backend.WriteLLVMProjectionEvidence(result.ProjectionPlan, evidencePath); err != nil {
			return fmt.Errorf("LLVM projection evidence: %w", err)
		}
		fmt.Fprintf(os.Stdout, "LLVM projection evidence=%s\n", evidencePath)
	}
	for _, gap := range result.ProjectionGaps {
		fmt.Fprintf(os.Stdout, "LLVM projection gap: %s\n", gap)
	}
	return nil
}

func formatProjectionCounts(counts map[string]int) string {
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, key+"="+fmt.Sprint(counts[key]))
	}
	return strings.Join(parts, ",")
}

func loadSemanticProgramForLLVM(inputPath string) (*backend.SemanticProgram, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		info, statErr := os.Stat(inputPath)
		if statErr != nil || !info.IsDir() {
			return nil, err
		}
		resolver, resolverErr := backend.NewUniversalModuleResolver()
		if resolverErr != nil {
			return nil, resolverErr
		}
		resolver.Store.Root = filepath.Join(inputPath, ".semantic-cache")
		mod, importErr := resolver.ImportTarget(inputPath, backend.ModuleImportOptions{Language: "sp"})
		if importErr != nil {
			return nil, importErr
		}
		return mod.Program, nil
	}
	lowerPath := strings.ToLower(inputPath)
	if strings.HasPrefix(string(data), "SPZ2") {
		return backend.ParseSemanticSPZ(data)
	}
	if strings.HasSuffix(lowerPath, ".se") || strings.HasSuffix(lowerPath, ".sp") {
		if len(data) >= 100*1024*1024 {
			u, graphErr := backend.ParseSemanticSEGraph(data)
			if graphErr != nil {
				return nil, graphErr
			}
			if strings.HasSuffix(lowerPath, ".se") {
				u.Evidence, graphErr = backend.AnalyzeUniversalEvidence(u)
			}
			if graphErr != nil {
				return nil, graphErr
			}
			return &backend.SemanticProgram{UniversalAST: u}, nil
		}
		return backend.ParseSemanticSE(data)
	}
	if strings.HasSuffix(lowerPath, ".json") || strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
		return backend.ParseSemanticJSON(data)
	}
	// LLVM accepts regular source files as well: import them through the
	// existing ModernFrontend/package lowering boundary first, then compile the
	// resulting canonical SemanticProgram. This keeps LLVM independent of
	// source-language syntax and avoids a second parser path.
	language := ""
	ext := strings.ToLower(filepath.Ext(inputPath))
	for _, spec := range backend.Frontends() {
		for _, candidate := range spec.Extensions {
			if strings.EqualFold(candidate, ext) {
				language = spec.ID
				break
			}
		}
		if language != "" {
			break
		}
	}
	if language != "" {
		program, lowerErr := backend.LowerSource(language, inputPath, string(data))
		if lowerErr != nil {
			return nil, fmt.Errorf("import source as %s for LLVM: %w", language, lowerErr)
		}
		return program, nil
	}
	return nil, fmt.Errorf("LLVM input must be semantic (.sp/.se/.spz/.json) or a registered source-language file")
}
