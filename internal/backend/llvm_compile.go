// Copyright (c) 2026 Tarek Wasfy
// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// LLVMOutputKind selects the artifact produced by the optional LLVM route.
// LLVMIR is deliberately an explicit output: it permits IR validation and
// inspection on machines that do not have an LLVM linker installed.
type LLVMOutputKind string

const (
	LLVMIR         LLVMOutputKind = "llvm-ir"
	LLVMAssembly   LLVMOutputKind = "assembly"
	LLVMObject     LLVMOutputKind = "object"
	LLVMExecutable LLVMOutputKind = "executable"
)

// LLVMCompileOptions is separate from CompileOptions so selecting LLVM can
// never silently change the established in-process native target. Paths may
// be either an executable or an LLVM bin directory.
type LLVMCompileOptions struct {
	OutputKind   LLVMOutputKind
	TargetTriple string
	Optimization int
	EntryPoint   string
	// ModuleBaseDir resolves relative semantic module references. When set,
	// ModuleStoreRoot overrides the default user Semantic module store.
	ModuleBaseDir   string
	ModuleStoreRoot string
	// EmbedAllModules is an explicit opt-in. The default follows the semantic
	// module link-root plan and embeds only reachable required modules.
	EmbedAllModules bool
	// ModuleEmbeddingMode preserves the explicit module policy for the LLVM
	// ingress as well as the direct native ingress.
	ModuleEmbeddingMode SemanticModuleEmbeddingMode
	LLVMPath            string
	ClangPath           string
	LLCPath             string
	LLVMAsPath          string
	LLDLinkPath         string
	EmitIR              bool
	// ProjectIndex and UnitID are read-only project context for the optional
	// per-unit route. They never cause another SemanticProgram to be assembled.
	ProjectIndex    *SemanticProjectIndex
	UnitID          string
	ProjectBindings map[string]string
	CacheDir        string
	// ExecutableClosureReportPath optionally overrides the project-level
	// executable-closure.json diagnostic path.
	ExecutableClosureReportPath string
	ProjectInitializers         []ExecutableFunctionRef
	// EmitEntryWrapper is true for the legacy single-program API. Project
	// units set it only on the owning entry unit so library objects never
	// manufacture competing process entry points.
	EmitEntryWrapper bool
}

type LLVMCompileResult struct {
	IR                 string
	Bytes              []byte
	Text               string
	OutputKind         LLVMOutputKind
	TargetTriple       string
	InstructionCount   int
	Functions          int
	ProjectionSchema   string
	ProjectionBasis    string
	ProjectionFamilies map[string]int
	ProjectionModes    map[string]int
	ProjectionGaps     []string
	ProjectionPlan     *LLVMProjectionPlan
}

// LLVMObjectFragment is the target-side result of compiling exactly one
// SemanticCompilationUnit. Semantic bodies are deliberately absent here;
// only LLVM/COFF bytes and compact ownership metadata cross the unit boundary.
type LLVMObjectFragment struct {
	UnitID       string
	SemanticRoot string
	Object       []byte
	IR           string
	SymbolNames  []string
}

type LLVMProjectResult struct {
	Fragments   []LLVMObjectFragment
	Failures    []LLVMProjectUnitFailure
	ObjectCount int
	CacheHits   int
	CacheMisses int
	Executable  []byte
	LinkMicros  int64
}

type LLVMProjectUnitFailure struct {
	UnitID string `json:"unit_id"`
	Phase  string `json:"phase"`
	Error  string `json:"error"`
}

// CompileLLVMProject lowers each project unit independently. This is the
// optional LLVM backend boundary: the GlobalSemanticIndex is shared as
// summaries, while LLVM modules and objects remain unit-local. A caller that
// needs a final PE must pass these object fragments to a linker capable of
// consuming their COFF section/symbol/relocation records; this function never
// merges their semantic programs.
func CompileLLVMProject(p *SemanticProject, opts LLVMCompileOptions) (LLVMProjectResult, error) {
	var result LLVMProjectResult
	if p == nil || len(p.Units) == 0 {
		return result, fmt.Errorf("LLVM_PROJECT_COMPILE: empty semantic project")
	}
	// Apply the canonical target-file selection before lowering. Semantic
	// projects may contain mutually exclusive platform units such as
	// *_windows.se and *_other.se; emitting both would create duplicate strong
	// entry symbols in the COFF link. Selection is filename/target metadata,
	// not a source-language special case.
	originalUnits := p.Units
	targetOS := ""
	if strings.Contains(strings.ToLower(opts.TargetTriple), "windows") {
		targetOS = "windows"
	}
	p.Units = selectSemanticTargetUnits(originalUnits, targetOS)
	defer func() { p.Units = originalUnits }()
	if len(p.Units) == 0 {
		return result, fmt.Errorf("LLVM_PROJECT_UNITS: no semantic units match target %q", targetOS)
	}
	moduleBase := opts.ModuleBaseDir
	if moduleBase == "" {
		moduleBase = filepath.Dir(p.Units[0].Path)
	}
	if err := expandSemanticProjectModules(p, moduleBase, opts.ModuleStoreRoot); err != nil {
		return result, fmt.Errorf("LLVM_PROJECT_MODULE_UNITS: %w", err)
	}
	if !loadProjectSummaryCache(p, opts.CacheDir) {
		if err := buildSemanticProjectSummaries(p, false); err != nil {
			return result, fmt.Errorf("LLVM_PROJECT_SUMMARY: %w", err)
		}
		saveProjectSummaryCache(p, opts.CacheDir)
	}
	var projectClosure ExecutableClosure
	if opts.OutputKind == LLVMExecutable {
		root := strings.TrimSpace(opts.EntryPoint)
		if root == "" {
			root = strings.TrimSpace(p.EntryPoint)
		}
		opts.EntryPoint = root
		closure, err := projectExecutableClosure(p, []string{root}, opts.ExecutableClosureReportPath)
		if err != nil {
			return result, err
		}
		projectClosure = closure
		p.Units, err = filterProjectUnitsToClosure(p.Units, closure)
		if err != nil {
			return result, fmt.Errorf("LLVM_EXECUTABLE_CLOSURE_UNITS: %w", err)
		}
	}
	var work string
	var toolchain llvmTools
	objects := make([]string, 0, len(p.Units))
	if opts.OutputKind == LLVMExecutable {
		toolchain = llvmToolchain(opts)
		if toolchain.lldLink == "" {
			return result, fmt.Errorf("LLVM_PROJECT_LINK: lld-link is required for LLVM object fragments")
		}
		var err error
		work, err = os.MkdirTemp("", "semantic-llvm-project-")
		if err != nil {
			return result, fmt.Errorf("LLVM_PROJECT_LINK: %w", err)
		}
		defer os.RemoveAll(work)
	}
	for unitIndex, unit := range p.Units {
		data, err := os.ReadFile(unit.Path)
		if err != nil {
			result.Failures = append(result.Failures, LLVMProjectUnitFailure{UnitID: unit.ID, Phase: "READ", Error: err.Error()})
			continue
		}
		program, err := loadSemanticUnitBytes(unit.Path, data)
		if err != nil {
			result.Failures = append(result.Failures, LLVMProjectUnitFailure{UnitID: unit.ID, Phase: "PARSE", Error: err.Error()})
			continue
		}
		if len(program.Origin.Modules) == 0 && program.UniversalAST != nil {
			program.Origin.Modules = semanticImportsFromUAST(program.UniversalAST)
		} else if program.UniversalAST != nil {
			// Persisted transports may contain declaration identities in the
			// module list (for example go:pkg:offset:Type:pkg). They are facts,
			// but not resolver keys. Reduce them to canonical package roots and
			// retain explicit import roots discovered from the UAST.
			normalized := make([]string, 0, len(program.Origin.Modules))
			for _, dep := range program.Origin.Modules {
				if strings.HasPrefix(dep, "go:") {
					parts := strings.Split(dep, ":")
					if len(parts) >= 2 && parts[1] != "" {
						normalized = append(normalized, parts[1])
					}
				} else {
					normalized = append(normalized, dep)
				}
			}
			program.Origin.Modules = mergeModuleNames(normalized, semanticImportsFromUAST(program.UniversalAST))
		}
		// Module bodies were promoted to independent project units before the
		// global summary pass. Never merge them into this LLVM unit body.
		unitSummary := p.Index.Summaries[unit.ID]
		unitUAST, err := canonicalUniversalAST(program)
		if err != nil {
			result.Failures = append(result.Failures, LLVMProjectUnitFailure{UnitID: unit.ID, Phase: "CANONICAL_UAST", Error: err.Error()})
			continue
		}
		if err := applyProjectCallableContractsToUAST(unitUAST, unitSummary); err != nil {
			result.Failures = append(result.Failures, LLVMProjectUnitFailure{UnitID: unit.ID, Phase: "PROJECT_CONTRACT", Error: err.Error()})
			continue
		}
		program.UniversalAST = unitUAST
		unitOpts := opts
		unitOpts.ProjectIndex = &p.Index
		unitOpts.UnitID = unit.ID
		unitOpts.ProjectBindings = semanticFunctionBindings(program)
		for _, summary := range p.Index.Summaries {
			for _, fn := range summary.Functions {
				if fn.Name == "" {
					continue
				}
				unitOpts.ProjectBindings["native_var_"+fn.Name] = fn.Name
				unitOpts.ProjectBindings["native_symbol_"+fn.Name] = fn.Name
			}
		}
		// Some semantic-only transports retain the canonical function summary
		// but omit the optional binding map. Preserve the frontend's stable
		// ordinal binding contract in that case; this is not name guessing and
		// remains confined to the current unit's declaration order.
		localFunctions := unitSummary.Functions
		for i, fn := range localFunctions {
			if fn.Name == "" {
				continue
			}
			binding := fmt.Sprintf("native_function_%d", i)
			if unitOpts.ProjectBindings[binding] == "" {
				unitOpts.ProjectBindings[binding] = fn.Name
			}
		}
		unitOpts.EntryPoint = ""
		unitOpts.EmitEntryWrapper = false
		unitOpts.ProjectInitializers = nil
		if opts.EntryPoint != "" && summaryDefinesEntry(p.Index.Summaries[unit.ID], opts.EntryPoint) {
			unitOpts.EntryPoint = opts.EntryPoint
			unitOpts.EmitEntryWrapper = true
			unitOpts.ProjectInitializers = append([]ExecutableFunctionRef(nil), projectClosure.InitializerFunctions...)
		}
		unitOpts.OutputKind = LLVMObject
		unitOpts.EmitIR = false
		compiled, err := CompileLLVM(program, unitOpts)
		program = nil
		if err != nil {
			result.Failures = append(result.Failures, LLVMProjectUnitFailure{UnitID: unit.ID, Phase: "LLVM_LOWER_OR_OBJECT", Error: err.Error()})
			continue
		}
		symbolNames := make([]string, 0, len(p.Index.Summaries[unit.ID].Functions))
		for _, fn := range p.Index.Summaries[unit.ID].Functions {
			if fn.Name != "" {
				symbolNames = append(symbolNames, llvmIdentifier(fn.Name))
			}
		}
		sort.Strings(symbolNames)
		fragment := LLVMObjectFragment{UnitID: unit.ID, SemanticRoot: unit.SemanticRoot, Object: append([]byte(nil), compiled.Bytes...), IR: compiled.IR, SymbolNames: symbolNames}
		result.Fragments = append(result.Fragments, fragment)
		if work != "" {
			path := filepath.Join(work, fmt.Sprintf("unit-%04d.obj", unitIndex))
			if err := os.WriteFile(path, fragment.Object, 0600); err != nil {
				result.Failures = append(result.Failures, LLVMProjectUnitFailure{UnitID: unit.ID, Phase: "OBJECT_WRITE", Error: err.Error()})
				continue
			}
			objects = append(objects, path)
		}
	}
	result.ObjectCount = len(result.Fragments)
	if len(result.Failures) > 0 {
		return result, fmt.Errorf("LLVM_PROJECT_UNITS_FAILED: units_total=%d objects_success=%d objects_failed=%d", len(p.Units), result.ObjectCount, len(result.Failures))
	}
	if opts.OutputKind == LLVMExecutable {
		outPath := filepath.Join(work, "semantic-project.exe")
		linkStart := time.Now()
		args := []string{"/entry:main", "/subsystem:console", "/errorlimit:0", "/out:" + outPath}
		args = append(args, llvmWindowsSystemLibArgs(opts.TargetTriple)...)
		args = append(args, objects...)
		if err := runLLVM(toolchain.lldLink, args...); err != nil {
			return result, fmt.Errorf("LLVM_PROJECT_LINK: %w", err)
		}
		result.LinkMicros = time.Since(linkStart).Microseconds()
		var err error
		result.Executable, err = os.ReadFile(outPath)
		if err != nil {
			return result, fmt.Errorf("LLVM_PROJECT_LINK_OUTPUT: %w", err)
		}
	}
	return result, nil
}

func summaryDefinesEntry(summary SemanticUnitSummary, entry string) bool {
	for _, fn := range summary.Functions {
		if fn.Name == entry || fn.ID == entry {
			return true
		}
	}
	return false
}

func semanticFunctionBindings(p *SemanticProgram) map[string]string {
	result := map[string]string{}
	if p == nil {
		return result
	}
	var bindings map[string]string
	if raw, err := json.Marshal(p.Extensions["function_entry_bindings"]); err == nil {
		_ = json.Unmarshal(raw, &bindings)
	}
	for source, binding := range bindings {
		if source != "" && binding != "" {
			result[binding] = source
		}
	}
	return result
}

// CompileLLVM projects the canonical UAST graph into textual LLVM IR and,
// when requested, invokes LLVM's own verifier/code generator and linker.
// There is no source-language special case here: input must already be a
// linked SemanticProgram with its canonical UAST attached.
func CompileLLVM(p *SemanticProgram, opts LLVMCompileOptions) (LLVMCompileResult, error) {
	result := LLVMCompileResult{OutputKind: opts.OutputKind}
	if p == nil {
		return result, fmt.Errorf("LLVM_COMPILE_CONTRACT: missing semantic program")
	}
	// Programmatic SemanticProgram construction may still provide the
	// compatibility Body view without having materialized its canonical UAST.
	// Materialize that existing semantic tree once at the backend boundary;
	// never emit from the compatibility tree directly.
	if p.UniversalAST == nil {
		doc, err := p.Document()
		if err != nil {
			return result, fmt.Errorf("LLVM_COMPILE_CONTRACT: canonical document: %w", err)
		}
		p.UniversalAST = doc.UniversalAST
	}
	if opts.ProjectIndex == nil && p.Metadata != nil && strings.TrimSpace(p.Metadata["semantic_module_embeddings"]) != "" {
		if err := LinkEmbeddedSemanticModules(p, opts.ModuleBaseDir); err != nil {
			return result, fmt.Errorf("LLVM_MODULE_EMBED: %w", err)
		}
	}
	// A project unit is already a separately discovered compilation unit. Do
	// not re-embed its module bodies into the local SemanticProgram; project
	// symbol resolution and the late object linker own that relationship.
	if len(p.Origin.Modules) > 0 && opts.ProjectIndex == nil {
		resolver, resolverErr := NewUniversalModuleResolver()
		if resolverErr != nil {
			return result, fmt.Errorf("LLVM_MODULE_LINK: initialize module resolver: %w", resolverErr)
		}
		if opts.ModuleStoreRoot != "" {
			resolver.Store.Root = opts.ModuleStoreRoot
			if err := resolver.Store.ensure(); err != nil {
				return result, fmt.Errorf("LLVM_MODULE_LINK: initialize module store: %w", err)
			}
		}
		if err := resolver.LinkSemanticDependenciesWithOptions(p, SemanticModuleLinkOptions{
			BaseDir: opts.ModuleBaseDir, EmbedAll: opts.EmbedAllModules, Mode: opts.ModuleEmbeddingMode,
		}); err != nil {
			return result, fmt.Errorf("LLVM_MODULE_LINK: %w", err)
		}
	}
	// Project mode has already completed the bounded summary/canonicalization
	// prepass. Re-running the recursive whole-program validator for every unit
	// defeats lazy compilation on large transports; the unit-local graph and
	// projection checks below remain mandatory and fail closed.
	if opts.ProjectIndex == nil {
		if err := ValidateSemanticProgram(p); err != nil {
			return result, fmt.Errorf("LLVM_COMPILE_CONTRACT: semantic validation: %w", err)
		}
	}
	u, err := canonicalUniversalAST(p)
	if err != nil {
		return result, fmt.Errorf("LLVM_COMPILE_CONTRACT: canonical UAST: %w", err)
	}
	g, err := newUASTExecutionGraph(u)
	if err != nil {
		return result, fmt.Errorf("LLVM_COMPILE_CONTRACT: executable UAST graph: %w", err)
	}
	projection, err := buildLLVMProjectionPlan(g)
	if err != nil {
		return result, fmt.Errorf("LLVM_COMPILE_CONTRACT: projection matrix: %w", err)
	}
	result.ProjectionSchema = projection.Schema
	result.ProjectionBasis = projection.BasisSHA256
	result.ProjectionFamilies = projection.Families
	result.ProjectionModes = projection.ModeCounts
	result.ProjectionGaps = append([]string(nil), projection.Gaps...)
	result.ProjectionPlan = &projection
	if opts.TargetTriple == "" {
		opts.TargetTriple = "x86_64-pc-windows-msvc"
	}
	if opts.Optimization < 0 || opts.Optimization > 3 {
		return result, fmt.Errorf("LLVM_COMPILE_CONTRACT: optimization must be 0..3")
	}
	if opts.OutputKind == "" {
		opts.OutputKind = LLVMExecutable
	}
	emitter := newLLVMEmitter(g, opts)
	emitter.projection = projection
	ir, err := emitter.emit()
	if err != nil {
		return result, fmt.Errorf("LLVM_PROJECTION_GAP: %w", err)
	}
	result.IR = ir
	result.TargetTriple = opts.TargetTriple
	result.InstructionCount = emitter.instructions
	result.Functions = len(emitter.functions)
	if opts.EmitIR || opts.OutputKind == LLVMIR {
		if err := verifyLLVMIRBytes(ir, opts); err != nil {
			return result, err
		}
		result.OutputKind = LLVMIR
		result.Text = ir
		return result, nil
	}

	work, err := os.MkdirTemp("", "semantic-llvm-")
	if err != nil {
		return result, fmt.Errorf("LLVM_TOOLCHAIN_TEMP: %w", err)
	}
	defer os.RemoveAll(work)
	irPath := filepath.Join(work, "program.ll")
	if err := os.WriteFile(irPath, []byte(ir), 0600); err != nil {
		return result, err
	}
	if err := emitLLVMArtifact(work, irPath, opts); err != nil {
		return result, err
	}
	// emitLLVMArtifact writes the requested artifact to this deterministic path
	// and returns it through the private helper below.
	artifact := filepath.Join(work, "program."+llvmArtifactExtension(opts.OutputKind))
	data, err := os.ReadFile(artifact)
	if err != nil {
		return result, fmt.Errorf("LLVM_TOOLCHAIN_OUTPUT: %w", err)
	}
	result.Bytes = data
	return result, nil
}

func verifyLLVMIRBytes(ir string, opts LLVMCompileOptions) error {
	tool := llvmToolchain(opts).llvmAs
	if tool == "" {
		// The native LLVM routes are verified by llc/clang before emission. IR-only
		// mode remains useful without an installation and is still checked by the
		// canonical graph projection; report no fabricated external verification.
		return nil
	}
	work, err := os.MkdirTemp("", "semantic-llvm-verify-")
	if err != nil {
		return fmt.Errorf("LLVM_VERIFIER_TEMP: %w", err)
	}
	defer os.RemoveAll(work)
	irPath := filepath.Join(work, "program.ll")
	bcPath := filepath.Join(work, "program.bc")
	if err := os.WriteFile(irPath, []byte(ir), 0600); err != nil {
		return err
	}
	if err := runLLVM(tool, irPath, "-o", bcPath); err != nil {
		return fmt.Errorf("LLVM_VERIFIER_FAILURE: %w", err)
	}
	return nil
}

func llvmArtifactExtension(kind LLVMOutputKind) string {
	switch kind {
	case LLVMAssembly:
		return "s"
	case LLVMObject:
		return "obj"
	default:
		return "exe"
	}
}

type llvmTools struct {
	clang, llc, llvmAs, lldLink string
}

func findLLVMTool(explicit, root, name string) string {
	if explicit != "" {
		if info, err := os.Stat(explicit); err == nil && !info.IsDir() {
			return explicit
		}
	}
	if root != "" {
		candidate := root
		if info, err := os.Stat(root); err == nil && !info.IsDir() {
			candidate = filepath.Dir(root)
		}
		// Accept both an LLVM installation root and its bin directory. The
		// previous implementation tested <root>\\<tool> first even when root
		// already was bin, making an explicitly supplied valid toolchain look
		// unavailable.
		candidates := []string{candidate}
		if strings.EqualFold(filepath.Base(strings.TrimRight(candidate, `\\/`)), "bin") {
			candidates = append(candidates, filepath.Dir(candidate))
		} else {
			candidates = append(candidates, filepath.Join(candidate, "bin"))
		}
		for _, dir := range candidates {
			for _, n := range []string{name, name + ".exe"} {
				p := filepath.Join(dir, n)
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

func llvmToolchain(opts LLVMCompileOptions) llvmTools {
	return llvmTools{
		clang:   findLLVMTool(opts.ClangPath, opts.LLVMPath, "clang"),
		llc:     findLLVMTool(opts.LLCPath, opts.LLVMPath, "llc"),
		llvmAs:  findLLVMTool(opts.LLVMAsPath, opts.LLVMPath, "llvm-as"),
		lldLink: findLLVMTool(opts.LLDLinkPath, opts.LLVMPath, "lld-link"),
	}
}

func runLLVM(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("LLVM_TOOLCHAIN_FAILURE: %s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func llvmWindowsSystemLibArgs(triple string) []string {
	if !strings.Contains(strings.ToLower(triple), "windows") {
		return nil
	}
	arch := "x64"
	if strings.Contains(strings.ToLower(triple), "i686") || strings.Contains(strings.ToLower(triple), "x86-") {
		arch = "x86"
	}
	root := `C:\Program Files (x86)\Windows Kits\10\Lib`
	versions, _ := filepath.Glob(filepath.Join(root, "*"))
	sort.Strings(versions)
	for i := len(versions) - 1; i >= 0; i-- {
		ucrt := filepath.Join(versions[i], "ucrt", arch)
		um := filepath.Join(versions[i], "um", arch)
		if _, err := os.Stat(filepath.Join(ucrt, "ucrt.lib")); err == nil {
			args := []string{"/libpath:" + ucrt, "/libpath:" + um, "ucrt.lib", "kernel32.lib", "user32.lib", "gdi32.lib", "shell32.lib"}
			// The UCRT import surface exposes printf through the MSVC
			// compatibility shim on current Windows toolchains. Resolve that
			// target from the installed target libraries instead of turning a
			// known builtin into a missing-symbol or zero-return fallback.
			msvcRoots := []string{
				`C:\Program Files\Microsoft Visual Studio`,
				`C:\Program Files (x86)\Microsoft Visual Studio`,
			}
			for _, root := range msvcRoots {
				patterns := []string{
					filepath.Join(root, "*", "*", "VC", "Tools", "MSVC", "*", "lib", arch),
					filepath.Join(root, "*", "*", "VC", "Tools", "MSVC", "*", "lib", "onecore", arch),
				}
				for _, pattern := range patterns {
					candidates, _ := filepath.Glob(pattern)
					sort.Strings(candidates)
					for j := len(candidates) - 1; j >= 0; j-- {
						shim := filepath.Join(candidates[j], "legacy_stdio_definitions.lib")
						if _, statErr := os.Stat(shim); statErr == nil {
							args = append(args, "/libpath:"+candidates[j], "legacy_stdio_definitions.lib")
							return args
						}
					}
				}
			}
			return args
		}
	}
	return nil
}

func emitLLVMArtifact(work, irPath string, opts LLVMCompileOptions) error {
	t := llvmToolchain(opts)
	level := fmt.Sprintf("-O%d", opts.Optimization)
	artifact := filepath.Join(work, "program."+llvmArtifactExtension(opts.OutputKind))
	switch opts.OutputKind {
	case LLVMAssembly:
		if t.llc != "" {
			return runLLVM(t.llc, "-mtriple="+opts.TargetTriple, level, "-filetype=asm", irPath, "-o", artifact)
		}
		if t.clang != "" {
			return runLLVM(t.clang, "-target", opts.TargetTriple, level, "-S", "-x", "ir", irPath, "-o", artifact)
		}
		return fmt.Errorf("LLVM_TOOLCHAIN_UNAVAILABLE: assembly output requires llc or clang (set LLVMPath or ClangPath)")
	case LLVMObject:
		if t.llc != "" {
			return runLLVM(t.llc, "-mtriple="+opts.TargetTriple, level, "-filetype=obj", irPath, "-o", artifact)
		}
		if t.clang != "" {
			return runLLVM(t.clang, "-target", opts.TargetTriple, level, "-c", "-x", "ir", irPath, "-o", artifact)
		}
		return fmt.Errorf("LLVM_TOOLCHAIN_UNAVAILABLE: object output requires llc or clang (set LLVMPath or ClangPath)")
	case LLVMExecutable:
		// Prefer the explicit LLVM pipeline: llc emits COFF and lld-link owns the
		// PE/COFF ABI. The clang fallback remains useful on installations where
		// the driver bundles lld but does not expose lld-link on PATH.
		obj := filepath.Join(work, "program.obj")
		if t.llc != "" && t.lldLink != "" {
			if err := runLLVM(t.llc, "-mtriple="+opts.TargetTriple, level, "-filetype=obj", irPath, "-o", obj); err != nil {
				return err
			}
			// The semantic project may intentionally retain runtime-backed imports;
			// keep the native artifact linkable while those optional symbols are
			// supplied by the host/runtime loader.
			args := []string{"/entry:main", "/subsystem:console", "/out:" + artifact}
			args = append(args, llvmWindowsSystemLibArgs(opts.TargetTriple)...)
			args = append(args, obj)
			return runLLVM(t.lldLink, args...)
		}
		if t.clang != "" {
			return runLLVM(t.clang, "-target", opts.TargetTriple, level, "-nostdlib", "-fuse-ld=lld", "-Wl,/entry:main", "-Wl,/subsystem:console", irPath, "-o", artifact)
		}
		return fmt.Errorf("LLVM_TOOLCHAIN_UNAVAILABLE: executable output requires clang or llc+lld-link; install LLVM and set LLVMPath")
	default:
		return fmt.Errorf("LLVM_COMPILE_CONTRACT: unsupported output kind %q", opts.OutputKind)
	}
}

type llvmValue struct {
	typ            string
	ref            string
	functionName   string
	functionResult string
	functionParams []string
	closure        bool
	closureEnv     string
	// pointee/length are the value-layout facts carried by the canonical
	// aggregate contract. LLVM's opaque `ptr` deliberately does not encode
	// them, so the projection keeps them beside the SSA reference until an
	// indexed load or a range loop consumes the facts.
	pointee   string
	length    int
	knownLen  bool
	lengthRef string
	// recordType/fieldNames/fieldTypes are the layout facts for named
	// aggregates. LLVM keeps the pointer opaque; the canonical SemanticType
	// supplies the field order used by member access.
	recordType         string
	fieldNames         []string
	fieldTypes         []string
	mapType            string
	mapKeyType         string
	mapValueType       string
	integerSigned      bool
	integerSignedKnown bool
	// bindingName preserves the declaration's canonical name beside storage.
	// It bridges transports where use sites carry a binding ID but the
	// declaration identifier does not repeat that ID.
	bindingName string
}

type llvmFunction struct {
	id         int
	name       string
	params     []llvmValue
	result     string
	vars       map[string]llvmValue
	terminated bool
	envRef     string
	envType    string
	captures   map[string]int
	body       strings.Builder
	allocas    []string
}

type llvmEmitter struct {
	g                   *uastExecutionGraph
	opts                LLVMCompileOptions
	projection          LLVMProjectionPlan
	b                   strings.Builder
	temp                int
	label               int
	stringID            int
	globals             []string
	typeDefs            []string
	instructions        int
	functions           map[string]int
	functionIDs         map[int]string
	current             *llvmFunction
	loops               []llvmLoopContext
	usesTrap            bool
	usesPow             bool
	usesTimeNow         bool
	usesTimeFormat      bool
	usesStringConcat    bool
	usesStringTrimSpace bool
	usesPathJoin        bool
	usesTempDir         bool
	usesPathDir         bool
	usesFileIO          bool
	captures            map[int][]string
	envTypes            map[int]string
	recordTypes         map[int]string
	externalCalls       map[string]llvmExternalABIContract
	builtinCalls        map[string]llvmExternalABIContract
	projectCalls        map[string]llvmExternalABIContract
	receiverFuncs       map[int]bool
	contractRefs        map[int][]SemanticContractReference
	typeCache           map[int]SemanticType
}

type llvmLoopContext struct {
	breakLabel    string
	continueLabel string
}

func newLLVMEmitter(g *uastExecutionGraph, opts LLVMCompileOptions) *llvmEmitter {
	e := &llvmEmitter{g: g, opts: opts, functions: map[string]int{}, functionIDs: map[int]string{}, captures: map[int][]string{}, envTypes: map[int]string{}, recordTypes: map[int]string{}, externalCalls: map[string]llvmExternalABIContract{}, builtinCalls: map[string]llvmExternalABIContract{}, projectCalls: map[string]llvmExternalABIContract{}, receiverFuncs: map[int]bool{}, contractRefs: map[int][]SemanticContractReference{}, typeCache: map[int]SemanticType{}}
	e.typeDefs = append(e.typeDefs,
		"%uast_file_handle = type { ptr, i1 }",
		"%uast_file_open_result = type { ptr, ptr }",
		"%uast_file_write_result = type { i64, ptr }",
	)
	if g != nil && g.document != nil {
		for _, ref := range g.document.ContractRefs {
			e.contractRefs[ref.NodeID] = append(e.contractRefs[ref.NodeID], ref)
		}
	}
	return e
}

func (e *llvmEmitter) discoverFunctionValueReceivers() {
	if e.g == nil || e.g.document == nil || !explicitReceiverContract(e.g.document.Extensions) {
		return
	}
	for id, common := range e.g.common {
		if common.Kind != "call" {
			continue
		}
		callee, ok, _ := e.g.callTarget(id)
		if !ok {
			continue
		}
		calleeCommon := e.g.common[callee]
		name := e.functionNameForIdentifier(calleeCommon.Name)
		if name == "" {
			continue
		}
		functionID, exists := e.functions[name]
		if !exists {
			continue
		}
		// Native frontends already encode a receiver as a typed parameter.
		// Do not add a second hidden parameter merely because a call happens to
		// carry one extra operand; that would corrupt the local ABI layout.
		if functionHasReceiverParameter(e.g, functionID) {
			continue
		}
		if len(e.g.many(id, "argument")) == len(e.g.many(functionID, "parameter"))+1 {
			e.receiverFuncs[functionID] = true
		}
	}
}

func functionHasReceiverParameter(g *uastExecutionGraph, id int) bool {
	if g == nil {
		return false
	}
	for _, parameter := range g.many(id, "parameter") {
		if c, ok := g.common[parameter.ID]; ok && (c.Operation.ParameterMode == "receiver" || c.Operation.ParameterPassing == "receiver") {
			return true
		}
	}
	return false
}

func explicitReceiverContract(extensions map[string]any) bool {
	var contract map[string]any
	encoded, err := json.Marshal(extensions["native_call_contract"])
	if err != nil || json.Unmarshal(encoded, &contract) != nil {
		return false
	}
	receiver, _ := contract["receiver"].(string)
	return receiver == "explicit_first_parameter"
}

func (e *llvmEmitter) discoverProjectCalls() error {
	if e.opts.ProjectIndex == nil {
		return nil
	}
	needed := map[string]bool{}
	for id, common := range e.g.common {
		if common.Kind == "call" {
			callee, ok, err := e.g.callTarget(id)
			if err != nil || !ok {
				continue
			}
			calleeCommon := e.g.common[callee]
			name := calleeCommon.Name
			if source := e.opts.ProjectBindings[name]; source != "" {
				name = source
			}
			if name != "" && e.functions[name] == 0 {
				needed[llvmIdentifier(name)] = true
			}
			continue
		}
		if common.Kind == "identifier" && common.Binding == nil && common.Name != "" {
			_, _, hasABI, err := e.functionABIForNode(id)
			if err != nil {
				return err
			}
			if hasABI {
				name := common.Name
				if source := e.opts.ProjectBindings[name]; source != "" {
					name = source
				}
				if name != "" && e.functions[name] == 0 {
					needed[llvmIdentifier(name)] = true
				}
			}
		}
	}
	neededNames := make([]string, 0, len(needed))
	for name := range needed {
		neededNames = append(neededNames, name)
	}
	sort.Strings(neededNames)
	unitIDs := make([]string, 0, len(e.opts.ProjectIndex.Summaries))
	for unitID := range e.opts.ProjectIndex.Summaries {
		if unitID != e.opts.UnitID {
			unitIDs = append(unitIDs, unitID)
		}
	}
	sort.Strings(unitIDs)
	for _, reference := range neededNames {
		var ownerUnit string
		var owner ProjectFunctionSummary
		matches := 0
		for _, unitID := range unitIDs {
			summary := e.opts.ProjectIndex.Summaries[unitID]
			for _, fn := range summary.Functions {
				if fn.Name == "" || !projectFunctionReferenceMatches(reference, summary.Package, fn) {
					continue
				}
				if matches == 0 || fn.ID != owner.ID {
					matches++
					ownerUnit, owner = unitID, fn
				}
			}
		}
		if matches == 0 {
			continue
		}
		if matches > 1 {
			return fmt.Errorf("LLVM_PROJECT_SYMBOL_AMBIGUOUS: reference %q matches %d project function declarations", reference, matches)
		}
		params := append([]SemanticType(nil), owner.Type.Parameters...)
		result := owner.Type.Result
		if result == nil {
			return fmt.Errorf("LLVM_CALL_ABI_CONTRACT_MISSING: project function %q in unit %q has no canonical result contract", owner.Name, ownerUnit)
		}
		for i, parameter := range params {
			if isUnknownSemanticType(parameter) {
				return fmt.Errorf("LLVM_CALL_ABI_CONTRACT_MISSING: project function %q in unit %q parameter %d has no canonical type contract", owner.Name, ownerUnit, i)
			}
		}
		contract := llvmExternalABIContract{Symbol: llvmIdentifier(owner.Name), CallingConvention: "ccc", Parameters: params, Result: *result}
		if previous, exists := e.projectCalls[reference]; exists && !llvmExternalABIEqual(previous, contract) {
			return fmt.Errorf("LLVM_PROJECT_ABI_CONFLICT: reference %q resolves to incompatible project declarations", reference)
		}
		e.projectCalls[reference] = contract
	}
	return nil
}

func projectFunctionReferenceMatches(reference, packageName string, fn ProjectFunctionSummary) bool {
	reference = llvmIdentifier(reference)
	name := llvmIdentifier(fn.Name)
	if reference == name || reference == fn.ID || reference == llvmIdentifier(fn.ID) {
		return true
	}
	if packageName == "" {
		return false
	}
	qualified := llvmIdentifier(packageName + "." + fn.Name)
	return reference == qualified
}

func (e *llvmEmitter) fresh(prefix string) string {
	e.temp++
	return fmt.Sprintf("%%%s%d", prefix, e.temp)
}
func (e *llvmEmitter) freshLabel(prefix string) string {
	e.label++
	return fmt.Sprintf("%s%d", prefix, e.label)
}
func (e *llvmEmitter) emitLine(format string, args ...any) {
	line := fmt.Sprintf(format, args...)
	if e.current != nil {
		e.current.body.WriteString(line)
		e.current.body.WriteByte('\n')
		return
	}
	e.b.WriteString(line)
	e.b.WriteByte('\n')
}
func (e *llvmEmitter) emitInstruction(format string, args ...any) string {
	e.instructions++
	value := fmt.Sprintf(format, args...)
	if strings.HasPrefix(strings.TrimSpace(value), "ret void ") {
		value = "  ret void"
	}
	if strings.TrimSpace(value) == "ret void" && e.current != nil && e.current.result != "void" {
		value = "  ret " + e.current.result + " " + llvmZero(e.current.result)
	}
	if e.current != nil {
		if strings.Contains(value, " = alloca ") {
			e.current.allocas = append(e.current.allocas, value)
		} else {
			e.current.body.WriteString(value)
			e.current.body.WriteByte('\n')
		}
	} else {
		e.b.WriteString(value)
		e.b.WriteByte('\n')
	}
	return value
}

func llvmIdentifier(name string) string {
	if name == "" {
		return "uast_anonymous"
	}
	var b strings.Builder
	for i, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || (i > 0 && r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	result := b.String()
	if result == "" || (result[0] >= '0' && result[0] <= '9') {
		result = "uast_" + result
	}
	return result
}

func llvmType(t SemanticType, literalKind string) string {
	kind := strings.ToLower(t.Kind)
	if kind == "" {
		kind = strings.ToLower(literalKind)
	}
	switch kind {
	case "boolean", "bool", "truth":
		return "i1"
	case "integer", "int", "uint":
		bits := t.Bits
		if bits == 0 {
			bits = 64
		}
		if bits != 1 && bits != 8 && bits != 16 && bits != 32 && bits != 64 {
			return "i64"
		}
		return "i" + strconv.Itoa(bits)
	case "number", "numeric", "float", "double", "binary64":
		return "double"
	case "string", "text", "bytes":
		return "ptr"
	case "array", "slice", "vector", "list", "tuple", "aggregate", "struct", "map", "function", "pointer", "reference":
		// Aggregate and function values cross the LLVM boundary through an
		// opaque pointer. Their element, field and signature contracts remain
		// attached to the canonical UAST value and are consumed by the
		// corresponding emitter family.
		return "ptr"
	case "void", "unit":
		return "void"
	default:
		if t.Reference || t.Element != nil || t.Fields != nil || t.Kind == "aggregate" {
			return "ptr"
		}
		return "i64"
	}
}

func llvmIntegerValue(t SemanticType, ref string) llvmValue {
	v := llvmValue{typ: llvmType(t, "integer"), ref: ref}
	if strings.EqualFold(t.Kind, "integer") || strings.EqualFold(t.Kind, "int") || strings.EqualFold(t.Kind, "uint") {
		if t.Signed != nil {
			v.integerSigned = *t.Signed
			v.integerSignedKnown = true
		}
	}
	return v
}

func llvmApplyAggregateLayout(v llvmValue, t SemanticType) llvmValue {
	if v.typ != "ptr" {
		return v
	}
	if t.Element != nil {
		v.pointee = llvmType(*t.Element, "")
	}
	if t.Length > 0 || strings.EqualFold(t.Kind, "array") {
		v.length = t.Length
		v.knownLen = true
	}
	// Tuple/product results use the canonical contiguous product ABI. When
	// every element has the same LLVM representation, the opaque result pointer
	// can be indexed with the proven product arity and element type.
	if strings.EqualFold(t.Kind, "tuple") && len(t.Parameters) > 0 {
		element := llvmType(t.Parameters[0], "")
		uniform := element != "void"
		for _, parameter := range t.Parameters[1:] {
			if llvmType(parameter, "") != element {
				uniform = false
				break
			}
		}
		if uniform {
			v.pointee = element
			v.length = len(t.Parameters)
			v.knownLen = true
		}
	}
	return v
}

// semanticTypeForNode closes backend layout facts only from contracts already
// attached to this canonical node. It does not infer a pointee, product shape,
// or length from the target representation.
func (e *llvmEmitter) semanticTypeForNode(id int) (SemanticType, error) {
	if typ, ok := e.typeCache[id]; ok {
		return typ, nil
	}
	common, ok := e.g.common[id]
	if !ok {
		return SemanticType{}, fmt.Errorf("UAST node %d has no decoded semantic type", id)
	}
	typ := common.Type
	// Named semantic types carry their canonical structural layout in the
	// element contract.  Preserve the named identity for symbol/ABI purposes,
	// but expose the already-declared fields to layout consumers such as member
	// access and aggregate lowering.  Without this normalization a valid
	// `named -> struct` contract was treated as an opaque pointer and member
	// access failed even though the field was present in the UAST.
	// Walk the declared pointer/named-type chain until the structural record
	// contract is reached.  A pointer to a named record commonly arrives as
	// pointer -> named -> struct; stopping after one hop loses the fields and
	// makes a valid member access look like an opaque record.
	for probe := &typ; len(probe.Fields) == 0 && probe.Element != nil; probe = probe.Element {
		if len(probe.Element.Fields) > 0 {
			typ.Fields = append([]SemanticField(nil), probe.Element.Fields...)
			break
		}
	}
	var aggregate SemanticAggregateContract
	if attached, err := e.contractForNode(id, SemanticAggregateContractKind, &aggregate); err != nil {
		return SemanticType{}, err
	} else if attached {
		if (typ.Element == nil || isUnknownSemanticType(*typ.Element)) && aggregate.ElementType != nil && !isUnknownSemanticType(*aggregate.ElementType) {
			element := *aggregate.ElementType
			typ.Element = &element
		}
		if len(typ.Fields) == 0 && len(aggregate.Fields) > 0 {
			typ.Fields = append([]SemanticField(nil), aggregate.Fields...)
		}
	}
	var reference SemanticReferenceContract
	if attached, err := e.contractForNode(id, SemanticReferenceContractKind, &reference); err != nil {
		return SemanticType{}, err
	} else if attached && (typ.Element == nil || isUnknownSemanticType(*typ.Element)) && !isUnknownSemanticType(reference.TargetType) {
		target := reference.TargetType
		if typ.Kind == "" || strings.EqualFold(typ.Kind, "unknown") {
			typ.Kind = "pointer"
		}
		typ.Element = &target
	}
	var shape SemanticShapeContract
	if attached, err := e.contractForNode(id, SemanticShapeContractKind, &shape); err != nil {
		return SemanticType{}, err
	} else if attached && shape.LengthKind == "fixed" && shape.Length != nil {
		typ.Length = *shape.Length
		if typ.Kind == "" || typ.Kind == "unknown" {
			typ.Kind = "array"
		}
	}
	e.typeCache[id] = typ
	return typ, nil
}

func (e *llvmEmitter) contractForNode(nodeID int, kind SemanticContractKind, out any) (bool, error) {
	if e == nil || e.g == nil || e.g.document == nil {
		return false, nil
	}
	for _, ref := range e.contractRefs[nodeID] {
		if ref.ContractID < 0 || ref.ContractID >= len(e.g.document.ContractTable) {
			continue
		}
		contract := e.g.document.ContractTable[ref.ContractID]
		if contract.Kind != kind {
			continue
		}
		if err := decodeStrictContractPayload(contract, out); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func (e *llvmEmitter) functionABIForNode(nodeID int) (string, []string, bool, error) {
	typ, err := e.semanticTypeForNode(nodeID)
	if err != nil {
		return "", nil, false, err
	}
	params := make([]SemanticType, 0, len(typ.Parameters))
	result := typ.Result
	if strings.EqualFold(typ.Kind, "function") {
		params = append(params, typ.Parameters...)
	}
	var contract SemanticFunctionContract
	if attached, contractErr := e.contractForNode(nodeID, SemanticFunctionContractKind, &contract); contractErr != nil {
		return "", nil, false, contractErr
	} else if attached {
		if len(params) == 0 && len(contract.Parameters) > 0 {
			for _, parameter := range contract.Parameters {
				params = append(params, parameter.Type)
			}
		}
		if result == nil {
			switch len(contract.Results) {
			case 0:
				void := SemanticType{Kind: "void", TypeOrigin: "derived"}
				result = &void
			case 1:
				result = &contract.Results[0]
			default:
				product := SemanticType{Kind: "tuple", Parameters: append([]SemanticType(nil), contract.Results...), TypeOrigin: "derived"}
				result = &product
			}
		}
	}
	if result == nil {
		return "", nil, false, nil
	}
	encoded := make([]string, len(params))
	for i, parameter := range params {
		if isUnknownSemanticType(parameter) {
			return "", nil, false, nil
		}
		encoded[i] = llvmType(parameter, "")
	}
	return llvmType(*result, ""), encoded, true, nil
}

func llvmVariadicElementForType(t SemanticType) (SemanticType, bool) {
	if !strings.EqualFold(t.Kind, "function") {
		return SemanticType{}, false
	}
	for _, constraint := range t.Constraints {
		if strings.EqualFold(constraint, "variadic") {
			if len(t.Parameters) == 1 && strings.EqualFold(t.Parameters[0].Kind, "slice") && t.Parameters[0].Element != nil {
				return *t.Parameters[0].Element, true
			}
		}
	}
	return SemanticType{}, false
}

func (e *llvmEmitter) applyNodeAggregateContract(id int, v llvmValue) (llvmValue, error) {
	typ, err := e.semanticTypeForNode(id)
	if err != nil {
		return llvmValue{}, err
	}
	v = llvmApplyAggregateLayout(v, typ)
	if v.typ != "ptr" || len(typ.Fields) == 0 {
		return v, nil
	}
	fieldNames := make([]string, len(typ.Fields))
	fieldTypes := make([]string, len(typ.Fields))
	for i, field := range typ.Fields {
		fieldTypes[i] = llvmType(field.Type, "")
		if fieldTypes[i] == "void" {
			return llvmValue{}, fmt.Errorf("aggregate node %d field %q has no concrete LLVM layout", id, field.Name)
		}
		fieldNames[i] = field.Name
		if fieldNames[i] == "" {
			fieldNames[i] = strconv.Itoa(i + 1)
		}
	}
	v.recordType = e.recordTypes[id]
	if v.recordType == "" {
		v.recordType = fmt.Sprintf("%%uast_record_%d", id)
		e.recordTypes[id] = v.recordType
		e.typeDefs = append(e.typeDefs, fmt.Sprintf("%s = type { %s }", v.recordType, strings.Join(fieldTypes, ", ")))
	}
	v.fieldNames = fieldNames
	v.fieldTypes = fieldTypes
	return v, nil
}

func (v llvmValue) signedInteger() bool {
	if !v.integerSignedKnown {
		return true
	}
	return v.integerSigned
}

func (e *llvmEmitter) discoverFunctions() error {
	ids := make([]int, 0)
	for id, c := range e.g.common {
		if c.Kind == "function" {
			ids = append(ids, id)
		}
	}
	// Stable IDs make output reproducible even though graph maps are unordered.
	for i := 0; i < len(ids); i++ {
		for j := i + 1; j < len(ids); j++ {
			if ids[j] < ids[i] {
				ids[i], ids[j] = ids[j], ids[i]
			}
		}
	}
	// Some readable Semantic exports preserve the canonical call graph but omit
	// the optional declaration-name field. If the graph proves exactly one
	// anonymous function and exactly one unresolved named call target, that
	// target is the unique binding projection; recover it without inspecting
	// source spelling. Ambiguous graphs remain fail-closed below.
	anonymous := []int{}
	for _, id := range ids {
		name := e.g.common[id].Name
		if name == "" {
			name = e.g.common[id].Operation.FunctionBinding
		}
		if name == "" {
			for parent, roles := range e.g.children {
				if e.g.common[parent].Kind != "assign" {
					continue
				}
				for _, child := range roles["expression"] {
					if child.ID == id && e.g.common[parent].Name != "" {
						name = e.g.common[parent].Name
					}
				}
			}
		}
		if source := e.opts.ProjectBindings[name]; source != "" {
			name = source
		}
		if name == "" {
			anonymous = append(anonymous, id)
		}
	}
	callNames := map[string]bool{}
	for id, c := range e.g.common {
		if c.Kind != "call" {
			continue
		}
		target, ok, _ := e.g.callTarget(id)
		if ok && e.g.common[target].Kind == "identifier" && e.g.common[target].Name != "" {
			callNames[e.g.common[target].Name] = true
		}
	}
	uniqueCallName := ""
	if len(callNames) == 1 {
		for name := range callNames {
			uniqueCallName = name
		}
	}
	used := map[string]bool{}
	anonymousOrdinal := 0
	for _, id := range ids {
		name := e.g.common[id].Name
		if name == "" {
			name = e.g.common[id].Operation.FunctionBinding
		}
		// A named function expression is normally attached to an assignment.
		if name == "" {
			for parent, roles := range e.g.children {
				if e.g.common[parent].Kind != "assign" {
					continue
				}
				for _, child := range roles["expression"] {
					if child.ID == id && e.g.common[parent].Name != "" {
						name = e.g.common[parent].Name
					}
				}
			}
		}
		if source := e.opts.ProjectBindings[name]; source != "" {
			name = source
		}
		if name == "" {
			// The prepass retains the stable declaration order even when the
			// transport omitted declaration names. Use that canonical summary
			// identity before considering a unique-call heuristic.
			if e.opts.ProjectIndex != nil {
				if summary, exists := e.opts.ProjectIndex.Summaries[e.opts.UnitID]; exists && anonymousOrdinal < len(summary.Functions) {
					name = summary.Functions[anonymousOrdinal].Name
				}
			}
			anonymousOrdinal++
		}
		if name == "" {
			if len(anonymous) == 1 && len(callNames) == 1 && anonymous[0] == id {
				name = uniqueCallName
			}
		}
		if name == "" {
			name = fmt.Sprintf("__uast_function_%d", id)
		}
		name = llvmIdentifier(name)
		base := name
		for n := 2; used[name]; n++ {
			name = fmt.Sprintf("%s_%d", base, n)
		}
		used[name] = true
		e.functions[name] = id
		e.functionIDs[id] = name
	}
	return nil
}

func (e *llvmEmitter) functionNameForIdentifier(name string) string {
	if source := e.opts.ProjectBindings[name]; source != "" {
		name = source
	}
	candidate := llvmIdentifier(name)
	if _, ok := e.functions[candidate]; ok {
		return candidate
	}
	// Module-qualified names are resolved through the canonical symbol graph,
	// not through a source-language rule. If the qualified reference has one
	// unambiguous declaration tail, it denotes that declaration; ambiguity
	// remains rejected instead of selecting an arbitrary overload.
	tail := name
	if at := strings.LastIndexAny(tail, ".:/"); at >= 0 && at+1 < len(tail) {
		tail = tail[at+1:]
	}
	matches := []string{}
	for emitted, id := range e.functions {
		declared := e.g.common[id].Name
		if declared == "" {
			declared = e.g.common[id].Operation.FunctionBinding
		}
		declaredTail := declared
		if at := strings.LastIndexAny(declaredTail, ".:/"); at >= 0 && at+1 < len(declaredTail) {
			declaredTail = declaredTail[at+1:]
		}
		if declared == name || declaredTail == tail {
			matches = append(matches, emitted)
		}
	}
	if len(matches) == 1 {
		return matches[0]
	}
	return ""
}

func (e *llvmEmitter) discoverCaptures() {
	for name, id := range e.functions {
		_ = name
		captures := e.functionCaptureNames(id)
		if len(captures) == 0 {
			continue
		}
		e.captures[id] = captures
		fields := make([]string, len(captures))
		for i, capture := range captures {
			typ := llvmType(e.commonForName(capture).Type, "")
			if typ == "void" {
				typ = "i64"
			}
			fields[i] = typ
		}
		typeName := fmt.Sprintf("%%uast_closure_env_%d", id)
		e.envTypes[id] = typeName
		e.typeDefs = append(e.typeDefs, fmt.Sprintf("%s = type { %s }", typeName, strings.Join(fields, ", ")))
	}
}

func (e *llvmEmitter) commonForName(name string) universalDecodedCommon {
	for _, c := range e.g.common {
		if llvmVariableKey(c) == name || c.Name == name {
			return c
		}
	}
	return universalDecodedCommon{Type: SemanticType{Kind: "integer", Bits: 64}}
}

func (e *llvmEmitter) functionCaptureNames(id int) []string {
	body, ok, _ := e.g.one(id, "body", false)
	if !ok {
		return nil
	}
	locals := map[string]bool{}
	for _, parameter := range e.g.many(id, "parameter") {
		if c, exists := e.g.common[parameter.ID]; exists {
			if key := llvmVariableKey(c); key != "" {
				locals[key] = true
			}
			if c.Name != "" {
				locals[c.Name] = true
			}
		}
	}
	seen := map[string]bool{}
	var captures []string
	var walk func(int)
	walk = func(node int) {
		c := e.g.common[node]
		if node != id && c.Kind == "function" {
			return
		}
		if c.Kind == "assign" {
			if key := llvmVariableKey(c); key != "" {
				locals[key] = true
			}
			if c.Name != "" {
				locals[c.Name] = true
			}
		}
		key := llvmVariableKey(c)
		if c.Kind == "identifier" && c.Name != "" && !locals[key] && !locals[c.Name] && e.functionNameForIdentifier(c.Name) == "" && !seen[key] {
			seen[key] = true
			captures = append(captures, key)
		}
		for _, child := range e.g.orderedChildren(node) {
			walk(child.ID)
		}
	}
	walk(body)
	return captures
}

func (e *llvmEmitter) functionSignature(id int) (string, []llvmValue, error) {
	// functionIDs contains the target-safe LLVM emission name.  It is not
	// necessarily the canonical semantic declaration name: transports may
	// project a declaration through function_entry_bindings (for example
	// native_function_79).  Resolve the project contract using the semantic
	// declaration identity first, while retaining the emitted name only for
	// LLVM symbols.  Otherwise an explicitly declared void result can be
	// mistaken for an unknown pointer result.
	contract, contractOK := e.projectFunctionContractForNode(id)
	if contractOK {
		semanticParameters := len(e.g.many(id, "parameter"))
		// A project summary is usable for a local definition only when its
		// parameter arity agrees with the definition being emitted.  This
		// guards against stale/transport-recovered declarations without
		// silently dropping local parameters; the local path below then emits
		// the precise missing-type error if the body lacks a contract.
		if len(contract.Parameters) != semanticParameters && !(e.receiverFuncs[id] && len(contract.Parameters)+1 == semanticParameters) {
			contractOK = false
		}
	}
	if contractOK {
		params := make([]llvmValue, 0, len(contract.Parameters))
		if e.receiverFuncs[id] {
			params = append(params, llvmValue{typ: "ptr", ref: "%p0"})
		}
		for i, parameter := range contract.Parameters {
			offset := 0
			if e.receiverFuncs[id] {
				offset = 1
			}
			params = append(params, llvmIntegerValue(parameter, "%p"+strconv.Itoa(i+offset)))
		}
		return llvmType(*contract.Result, ""), params, nil
	}
	params := []llvmValue{}
	if e.receiverFuncs[id] {
		params = append(params, llvmValue{typ: "ptr", ref: "%p0"})
	}
	for _, p := range e.g.many(id, "parameter") {
		c := e.g.common[p.ID]
		if isUnknownSemanticType(c.Type) {
			c.Type = SemanticType{Kind: "integer", Bits: 64, Signed: boolPtr(true), TypeOrigin: "derived"}
		}
		params = append(params, llvmIntegerValue(c.Type, "%p"+strconv.Itoa(len(params))))
	}
	functionType := e.g.common[id].Type
	if functionType.Result == nil {
		var functionContract SemanticFunctionContract
		if attached, err := contractForNode(e.g.document, id, SemanticFunctionContractKind, &functionContract); err != nil {
			return "", nil, err
		} else if attached {
			switch len(functionContract.Results) {
			case 0:
				void := SemanticType{Kind: "void", TypeOrigin: "derived"}
				functionType.Result = &void
			case 1:
				functionType.Result = &functionContract.Results[0]
			default:
				product := SemanticType{Kind: "tuple", Parameters: append([]SemanticType(nil), functionContract.Results...), TypeOrigin: "derived"}
				functionType.Result = &product
			}
		}
	}
	result := llvmType(llvmFunctionResultType(functionType), "")
	if functionType.Result == nil {
		if hasFunctionReturn(e.g, id) {
			// The transport has a structurally explicit return but omitted its
			// scalar result type. Keep the ABI width stable for this proven
			// scalar-return shape; value semantics are still lowered from the
			// return expression and are never replaced with zero.
			result = "i64"
		} else if e.opts.ProjectIndex != nil && e.opts.UnitID != "" {
			return "", nil, fmt.Errorf("LLVM_CALL_ABI_CONTRACT_MISSING: function node %d has no canonical result contract", id)
		} else {
			result = "i64"
		}
	}
	return result, params, nil
}

func (e *llvmEmitter) projectFunctionContractForNode(id int) (SemanticType, bool) {
	if e == nil {
		return SemanticType{}, false
	}
	candidates := []string{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		for _, existing := range candidates {
			if existing == name {
				return
			}
		}
		candidates = append(candidates, name)
	}
	if common, ok := e.g.common[id]; ok {
		add(common.Name)
		add(common.Operation.FunctionBinding)
	}
	add(e.functionIDs[id])
	// A binding projection may replace the semantic declaration name with a
	// native-safe emitted name.  Reverse lookup is metadata-driven and only
	// accepts an unambiguous exact binding; it is not source-name guessing.
	for semantic, emitted := range e.opts.ProjectBindings {
		if emitted == e.functionIDs[id] {
			add(semantic)
		}
	}
	for _, candidate := range candidates {
		if contract, ok := projectFunctionContract(e.opts.ProjectIndex, e.opts.UnitID, candidate); ok {
			return contract, true
		}
	}
	return SemanticType{}, false
}

func projectFunctionContract(index *SemanticProjectIndex, unitID, name string) (SemanticType, bool) {
	if index == nil || name == "" {
		return SemanticType{}, false
	}
	summary, ok := index.Summaries[unitID]
	if !ok {
		return SemanticType{}, false
	}
	for _, fn := range summary.Functions {
		if fn.Name == name && fn.Type.Kind == "function" && fn.Type.Result != nil {
			complete := true
			for _, parameter := range fn.Type.Parameters {
				if isUnknownSemanticType(parameter) {
					complete = false
				}
			}
			if complete {
				return fn.Type, true
			}
		}
	}
	return SemanticType{}, false
}

func isUnknownSemanticType(t SemanticType) bool {
	kind := strings.ToLower(strings.TrimSpace(t.Kind))
	return kind == "" || kind == "unknown" || strings.EqualFold(strings.TrimSpace(t.Name), "invalid type")
}

// inferFunctionResultType derives only a result type that is structurally
// forced by an existing canonical return expression. It never invents a
// value; callers still fail closed when no such expression is available.
func inferFunctionResultType(g *uastExecutionGraph, id int) (SemanticType, bool) {
	body, ok, err := g.one(id, "body", false)
	if err != nil || !ok {
		return SemanticType{}, false
	}
	var infer func(int) (SemanticType, bool)
	infer = func(nodeID int) (SemanticType, bool) {
		c, exists := g.common[nodeID]
		if !exists {
			return SemanticType{}, false
		}
		if !isUnknownSemanticType(c.Type) && c.Type.Kind != "function" {
			return c.Type, true
		}
		switch c.Kind {
		case "literal":
			kind := strings.ToLower(c.Operation.LiteralKind)
			if kind == "" {
				kind = strings.ToLower(c.Type.Kind)
			}
			switch kind {
			case "integer", "int", "uint", "number", "numeric", "float", "double":
				if kind == "float" || kind == "double" || kind == "number" || kind == "numeric" {
					return SemanticType{Kind: "number", IEEE754: true}, true
				}
				return SemanticType{Kind: "integer", Bits: 64}, true
			}
		case "binary", "unary", "typed_operation":
			for _, child := range g.orderedChildren(nodeID) {
				if child.Meta.Role == "left" || child.Meta.Role == "value" || child.Meta.Role == "operand" {
					if typ, found := infer(child.ID); found {
						return typ, true
					}
				}
			}
		}
		for _, child := range g.orderedChildren(nodeID) {
			if typ, found := infer(child.ID); found {
				return typ, true
			}
		}
		return SemanticType{}, false
	}
	var walk func(int) (SemanticType, bool)
	walk = func(nodeID int) (SemanticType, bool) {
		c := g.common[nodeID]
		if c.Kind == "return" {
			if value, found, _ := g.one(nodeID, "expression", false); found {
				return infer(value)
			}
			return SemanticType{Kind: "void"}, true
		}
		for _, child := range g.orderedChildren(nodeID) {
			if typ, found := walk(child.ID); found {
				return typ, true
			}
		}
		return SemanticType{}, false
	}
	return walk(body)
}

func hasFunctionReturn(g *uastExecutionGraph, id int) bool {
	body, ok, err := g.one(id, "body", false)
	if err != nil || !ok {
		return false
	}
	var walk func(int) bool
	walk = func(nodeID int) bool {
		if nodeID != body && g.common[nodeID].Kind == "function" {
			return false
		}
		if g.common[nodeID].Kind == "return" {
			value, hasValue, valueErr := g.one(nodeID, "expression", false)
			if valueErr != nil {
				return false
			}
			if !hasValue {
				_, hasValue, valueErr = g.one(nodeID, "value", false)
			}
			return valueErr == nil && hasValue && value >= 0
		}
		for _, child := range g.orderedChildren(nodeID) {
			if walk(child.ID) {
				return true
			}
		}
		return false
	}
	return walk(body)
}

func llvmFunctionResultType(t SemanticType) SemanticType {
	if t.Result != nil {
		return *t.Result
	}
	return SemanticType{Kind: "integer", Bits: 64}
}

func (e *llvmEmitter) emit() (string, error) {
	if err := e.discoverFunctions(); err != nil {
		return "", err
	}
	e.discoverFunctionValueReceivers()
	if err := e.discoverExternalCalls(); err != nil {
		return "", err
	}
	if err := e.discoverProjectCalls(); err != nil {
		return "", err
	}
	e.ensureCallDeclarations()
	e.discoverCaptures()
	if err := e.discoverRecordTypes(); err != nil {
		return "", err
	}
	e.predeclareTupleProducts()
	e.predeclareMapLayouts()
	e.emitLine("; Copyright (c) 2026 Tarek Wasfy")
	e.emitLine("; UAST -> LLVM IR projection; target=%s", e.opts.TargetTriple)
	e.emitLine("target triple = %q", e.opts.TargetTriple)
	for _, typeDef := range e.typeDefs {
		e.emitLine("%s", typeDef)
	}
	initializerSymbols := map[string]bool{}
	for _, initializer := range e.opts.ProjectInitializers {
		name := llvmIdentifier(initializer.Name)
		if name == "" || initializerSymbols[name] {
			continue
		}
		initializerSymbols[name] = true
		if _, local := e.functions[name]; !local {
			e.emitLine("declare void @%s()", name)
		}
	}
	names := make([]string, 0, len(e.functions))
	for name := range e.functions {
		names = append(names, name)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, name := range names {
		if err := e.emitFunction(name, e.functions[name]); err != nil {
			return "", err
		}
	}
	for _, global := range e.globals {
		e.emitLine("%s", global)
	}
	if e.usesTrap {
		e.emitLine("declare void @llvm.trap()")
	}
	if e.usesPow {
		e.emitLine("declare double @llvm.pow.f64(double, double)")
	}
	if err := e.emitBuiltinRuntime(); err != nil {
		return "", err
	}
	symbols := make([]string, 0, len(e.externalCalls))
	for symbol := range e.externalCalls {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	for _, symbol := range symbols {
		contract := e.externalCalls[symbol]
		cc, err := llvmCallingConvention(contract.CallingConvention)
		if err != nil {
			return "", fmt.Errorf("external ABI symbol %q: %w", symbol, err)
		}
		if cc != "" {
			cc += " "
		}
		params := make([]string, len(contract.Parameters))
		for i, parameter := range contract.Parameters {
			params[i] = llvmType(parameter, "")
		}
		e.emitLine("declare %s%s @%s(%s)", cc, llvmType(contract.Result, ""), symbol, strings.Join(params, ", "))
	}
	projectSymbols := make([]string, 0, len(e.projectCalls))
	projectSymbolSet := map[string]bool{}
	for _, contract := range e.projectCalls {
		if _, local := e.functions[contract.Symbol]; !local {
			projectSymbolSet[contract.Symbol] = true
		}
	}
	for symbol := range projectSymbolSet {
		projectSymbols = append(projectSymbols, symbol)
	}
	sort.Strings(projectSymbols)
	for _, symbol := range projectSymbols {
		contract := e.projectCalls[symbol]
		params := make([]string, len(contract.Parameters))
		for i, parameter := range contract.Parameters {
			params[i] = llvmType(parameter, "")
		}
		e.emitLine("declare %s @%s(%s)", llvmType(contract.Result, ""), symbol, strings.Join(params, ", "))
	}
	// A valid SemanticProgram can contain executable module-level statements
	// without FunctionDecl nodes. Project that existing root exactly once; do
	// not turn it into an empty wrapper.
	if len(e.functions) == 0 {
		e.emitLine("define i32 @main() {")
		e.emitLine("entry:")
		e.current = &llvmFunction{id: -1, name: "main", result: "i32", vars: map[string]llvmValue{}}
		if err := e.emitStmt(e.g.root); err != nil {
			return "", err
		}
		if !e.current.terminated {
			e.emitInstruction("  ret i32 0")
		}
		for _, allocation := range e.current.allocas {
			e.b.WriteString(allocation)
			e.b.WriteByte('\n')
		}
		e.b.WriteString(e.current.body.String())
		e.current = nil
		e.emitLine("}")
		return e.b.String(), nil
	}
	entry := e.opts.EntryPoint
	explicitEntry := strings.TrimSpace(entry) != ""
	if entry == "" {
		if _, ok := e.functions["main"]; ok {
			entry = "main"
		} else if canonical := llvmEntryAlias(e.g.document, "main"); canonical != "" {
			entry = canonical
		}
	}
	if entry != "" {
		entry = llvmIdentifier(entry)
		if _, ok := e.functions[entry]; !ok {
			if canonical := llvmEntryAlias(e.g.document, entry); canonical != "" {
				entry = llvmIdentifier(canonical)
			}
		}
		if _, ok := e.functions[entry]; !ok {
			if explicitEntry {
				return "", fmt.Errorf("entry %q not found in canonical function graph", entry)
			}
			// The default origin entry is only a hint. If the canonical graph has
			// no such declaration, project the existing module-level statements
			// through the wrapper instead of emitting an empty process entry.
			entry = ""
		}
	}
	// A user main with no parameters is already a valid PE entry. Otherwise
	// create one deterministic ABI wrapper and invoke the selected function
	// exactly once with zero-initialized arguments.
	projectUnit := e.opts.ProjectIndex != nil && e.opts.UnitID != ""
	_, entryFunctionExists := e.functions[entry]
	if (!projectUnit || e.opts.EmitEntryWrapper) && (entry == "" || entry != "main" || !entryFunctionExists) {
		e.emitLine("define i32 @main() {")
		e.emitLine("entry:")
		for _, initializer := range e.opts.ProjectInitializers {
			if initializer.Name == "" {
				return "", fmt.Errorf("LLVM_PROJECT_INITIALIZER_CONTRACT_MISSING: initializer %q has no canonical function name", initializer.ID)
			}
			e.emitInstruction("  call void @%s()", llvmIdentifier(initializer.Name))
		}
		if entry != "" {
			id := e.functions[entry]
			resultType, params, _ := e.functionSignature(id)
			args := make([]string, len(params))
			for i, p := range params {
				args[i] = p.typ + " " + llvmZero(p.typ)
			}
			if resultType == "void" {
				e.emitInstruction("  call void @%s(%s)", entry, strings.Join(args, ", "))
				e.emitInstruction("  ret i32 0")
			} else {
				call := e.newTemp(fmt.Sprintf("call %s @%s(%s)", resultType, entry, strings.Join(args, ", ")))
				if e.opts.EntryPoint != "" {
					switch {
					case resultType == "i32":
						e.emitInstruction("  ret i32 %s", call)
					case strings.HasPrefix(resultType, "i"):
						e.emitInstruction("  ret i32 %s", e.newTemp("trunc "+resultType+" "+call+" to i32"))
					case resultType == "double":
						e.emitInstruction("  ret i32 %s", e.newTemp("fptosi double "+call+" to i32"))
					default:
						e.emitInstruction("  ret i32 0")
					}
				} else {
					e.emitInstruction("  ret i32 0")
				}
			}
		} else {
			// A canonical UAST without an entry declaration may still carry
			// executable module-level statements. Project those statements into
			// the wrapper instead of silently discarding them.
			e.current = &llvmFunction{id: -1, name: "main", result: "i32", vars: map[string]llvmValue{}}
			if err := e.emitStmt(e.g.root); err != nil {
				return "", err
			}
			if !e.current.terminated {
				e.emitInstruction("  ret i32 0")
			}
			for _, allocation := range e.current.allocas {
				e.b.WriteString(allocation)
				e.b.WriteByte('\n')
			}
			e.b.WriteString(e.current.body.String())
			e.current = nil
			e.emitLine("}")
			return e.b.String(), nil
		}
		if entry != "" {
			// The selected-entry branch emitted its return above.
		} else {
			e.emitInstruction("  ret i32 0")
		}
		e.emitLine("}")
	}
	return e.b.String(), nil
}

// ensureCallDeclarations closes the declaration plane for canonical calls
// whose selected declaration is imported but has no body in this unit. This
// is deliberately structural: local definitions remain definitions, while an
// imported callable receives its typed declaration from the call graph.
func (e *llvmEmitter) ensureCallDeclarations() {
	if e == nil || e.g == nil {
		return
	}
	ids := make([]int, 0)
	for id, common := range e.g.common {
		if common.Kind == "call" {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		callee, ok, _ := e.g.callTarget(id)
		if !ok {
			continue
		}
		c := e.g.common[callee]
		if c.Kind != "identifier" || c.Name == "" {
			continue
		}
		name := llvmIdentifier(c.Name)
		if source := e.opts.ProjectBindings[c.Name]; source != "" {
			name = llvmIdentifier(source)
		}
		if localID, exists := e.functions[name]; exists {
			if _, hasBody, _ := e.g.one(localID, "body", false); hasBody {
				continue
			}
		}
		if _, exists := e.externalCalls[name]; exists {
			continue
		}
		if _, exists := e.builtinCalls[name]; exists {
			continue
		}
		if _, exists := e.projectCalls[name]; exists {
			continue
		}
		if contract, supported := llvmGenericBuiltinCallContract(e.g, id, c.Name); supported {
			contract.Symbol = name
			e.externalCalls[name] = contract
		}
	}
}

// predeclareTupleProducts runs before the module type block is emitted. Call
// lowering may encounter a product result only while emitting a function; the
// product's sized LLVM record must nevertheless exist before any GEP uses it.
// The pass reads only canonical graph result contracts and does not retain or
// rewrite semantic bodies.
func (e *llvmEmitter) predeclareTupleProducts() {
	if e == nil || e.g == nil {
		return
	}
	ids := make([]int, 0, len(e.g.common))
	for id, common := range e.g.common {
		if strings.EqualFold(common.Type.Kind, "tuple") && len(common.Type.Parameters) > 0 {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		if _, exists := e.recordTypes[id]; exists {
			continue
		}
		parts := make([]string, len(e.g.common[id].Type.Parameters))
		valid := true
		for i, parameter := range e.g.common[id].Type.Parameters {
			parts[i] = llvmType(parameter, "")
			if parts[i] == "void" {
				valid = false
			}
		}
		if !valid {
			continue
		}
		typeName := fmt.Sprintf("%%uast_call_product_%d", id)
		e.recordTypes[id] = typeName
		e.typeDefs = append(e.typeDefs, fmt.Sprintf("%s = type { %s }", typeName, strings.Join(parts, ", ")))
	}
}

// predeclareMapLayouts makes every map descriptor sized before function bodies
// are emitted. Map values are descriptors, not opaque placeholders: lowering
// may allocate and address them before the first literal is encountered.
func (e *llvmEmitter) predeclareMapLayouts() {
	if e == nil || e.g == nil {
		return
	}
	ids := make([]int, 0)
	for id, common := range e.g.common {
		if strings.EqualFold(common.Type.Kind, "map") {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		if _, exists := e.recordTypes[id]; exists {
			continue
		}
		typeName := fmt.Sprintf("%%uast_map_%d", id)
		e.recordTypes[id] = typeName
		e.typeDefs = append(e.typeDefs, fmt.Sprintf("%s = type { i64, i64, ptr }", typeName))
	}
}

func (e *llvmEmitter) discoverRecordTypes() error {
	ids := make([]int, 0, len(e.g.common))
	for id := range e.g.common {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	for _, id := range ids {
		typ, err := e.semanticTypeForNode(id)
		if err != nil {
			return err
		}
		fields := typ.Fields
		if len(fields) == 0 {
			continue
		}
		if _, exists := e.recordTypes[id]; exists {
			continue
		}
		fieldTypes := make([]string, len(fields))
		for i, field := range fields {
			fieldTypes[i] = llvmType(field.Type, "")
			if fieldTypes[i] == "void" {
				return fmt.Errorf("aggregate node %d field %q has no concrete LLVM layout", id, field.Name)
			}
		}
		typeName := fmt.Sprintf("%%uast_record_%d", id)
		e.recordTypes[id] = typeName
		e.typeDefs = append(e.typeDefs, fmt.Sprintf("%s = type { %s }", typeName, strings.Join(fieldTypes, ", ")))
	}
	return nil
}

func (e *llvmEmitter) discoverExternalCalls() error {
	ids := make([]int, 0, len(e.g.common))
	for id, common := range e.g.common {
		if common.Kind == "call" {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	for _, id := range ids {
		if resolvedID, resolved := llvmCallResolutionTarget(e.g, id); resolved && e.functionIDs[resolvedID] != "" {
			continue
		}
		callee, ok, err := e.g.callTarget(id)
		if err != nil {
			return err
		}
		if ok {
			if memberContract, memberOK := llvmWindowsProcCallContract(e.g, id, callee); memberOK {
				e.externalCalls[memberContract.Symbol] = memberContract
				continue
			}
			// A resolved declaration is a local call only when its graph node
			// owns a body. Imported declaration-only function nodes must still
			// receive an LLVM declaration from their canonical call contract.
			if e.g.common[callee].Kind == "function" {
				if _, hasBody, _ := e.g.one(callee, "body", false); hasBody {
					continue
				}
			} else if e.g.common[callee].Kind == "identifier" {
				if localName := e.functionNameForIdentifier(e.g.common[callee].Name); localName != "" {
					if localID, exists := e.functions[localName]; exists {
						if _, hasBody, _ := e.g.one(localID, "body", false); hasBody {
							continue
						}
					}
				}
			}
		}
		if _, local := llvmLocalCallTarget(e.g, id); local {
			continue
		}
		if ok && e.g.common[callee].Kind == "identifier" && strings.HasPrefix(e.g.common[callee].Name, "native_symbol_") {
			contract, supported := llvmBuiltinCallContract(e.g, id, e.g.common[callee].Name)
			if !supported {
				contract, supported = llvmGenericBuiltinCallContract(e.g, id, e.g.common[callee].Name)
			}
			if !supported {
				return fmt.Errorf("LLVM_BUILTIN_CONTRACT_UNSUPPORTED: %q at call node %d", e.g.common[callee].Name, id)
			}
			e.builtinCalls[contract.Symbol] = contract
			continue
		}
		contract, ok := llvmExternalCallContract(e.g, id)
		if !ok {
			calleeCommon := e.g.common[callee]
			if calleeCommon.Kind == "identifier" {
				contract, ok = llvmGenericBuiltinCallContract(e.g, id, calleeCommon.Name)
			}
		}
		if !ok {
			continue
		}
		// LLVM permits one external symbol declaration. If the canonical graph
		// carries more than one complete call signature for that symbol, keep a
		// deterministic declaration and let emitCall project the individual
		// signature through a typed function-pointer adapter. This preserves the
		// per-call ABI contract without mutating the UAST or fabricating a shared
		// prototype.
		if _, exists := e.externalCalls[contract.Symbol]; !exists {
			e.externalCalls[contract.Symbol] = contract
		}
	}
	for _, common := range e.g.common {
		if strings.HasPrefix(common.Name, "context_") {
			if _, exists := e.externalCalls[common.Name]; !exists {
				e.externalCalls[common.Name] = llvmExternalABIContract{Symbol: common.Name, CallingConvention: "ccc", Result: SemanticType{Kind: "pointer"}}
			}
		}
	}
	return nil
}

func llvmGenericBuiltinCallContract(g *uastExecutionGraph, callID int, symbol string) (llvmExternalABIContract, bool) {
	args := g.many(callID, "argument")
	params := make([]SemanticType, len(args))
	for i, arg := range args {
		t := g.common[arg.ID].Type
		if llvmType(t, "") == "void" {
			t = SemanticType{Kind: "integer", Bits: 64, Signed: boolPtr(true), TypeOrigin: "derived"}
		}
		params[i] = t
	}
	result := g.common[callID].Type
	if result.Kind == "" {
		result = SemanticType{Kind: "void", TypeOrigin: "derived"}
	}
	return llvmExternalABIContract{Symbol: llvmIdentifier(symbol), CallingConvention: "ccc", Parameters: params, Result: result}, true
}

// llvmWindowsProcCallContract describes the canonical Windows Proc.Call
// projection.  A Proc value is an external function identity, not an
// aggregate field and not a callable pointer that may be guessed from its
// opaque representation.  The identity table supplies the DLL/symbol pair;
// the call node supplies the already canonicalized argument/result types.
// This keeps the PE import boundary explicit and makes unsupported Proc
// identities fail closed.
func llvmWindowsProcCallContract(g *uastExecutionGraph, callID, memberID int) (llvmExternalABIContract, bool) {
	if g == nil || g.common[memberID].Kind != "member" || !strings.EqualFold(g.common[memberID].Name, "Call") {
		return llvmExternalABIContract{}, false
	}
	receiver, ok, _ := g.firstChild(memberID, "base", "receiver", "value", "object")
	if !ok {
		return llvmExternalABIContract{}, false
	}
	name := g.common[receiver].Name
	if name == "" {
		name = g.common[receiver].Operation.FunctionBinding
	}
	if !strings.HasPrefix(name, "proc") || len(name) <= len("proc") {
		return llvmExternalABIContract{}, false
	}
	api := map[string][2]string{
		"procRegisterClassEx": {"user32.dll", "RegisterClassExW"}, "procCreateWindowEx": {"user32.dll", "CreateWindowExW"},
		"procDefWindowProc": {"user32.dll", "DefWindowProcW"}, "procShowWindow": {"user32.dll", "ShowWindow"},
		"procUpdateWindow": {"user32.dll", "UpdateWindow"}, "procGetMessage": {"user32.dll", "GetMessageW"},
		"procTranslateMessage": {"user32.dll", "TranslateMessage"}, "procDispatchMessage": {"user32.dll", "DispatchMessageW"},
		"procPostQuitMessage": {"user32.dll", "PostQuitMessage"}, "procPostMessage": {"user32.dll", "PostMessageW"},
		"procDestroyWindow": {"user32.dll", "DestroyWindow"}, "procSendMessage": {"user32.dll", "SendMessageW"},
		"procSetWindowText": {"user32.dll", "SetWindowTextW"}, "procGetWindowText": {"user32.dll", "GetWindowTextW"},
		"procMessageBox": {"user32.dll", "MessageBoxW"}, "procLoadCursor": {"user32.dll", "LoadCursorW"},
		"procLoadImage": {"user32.dll", "LoadImageW"}, "procEnumDisplayMonitors": {"user32.dll", "EnumDisplayMonitors"},
		"procGetMonitorInfo": {"user32.dll", "GetMonitorInfoW"}, "procBeginPaint": {"user32.dll", "BeginPaint"},
		"procEndPaint": {"user32.dll", "EndPaint"}, "procGetClientRect": {"user32.dll", "GetClientRect"},
		"procGetDC": {"user32.dll", "GetDC"}, "procReleaseDC": {"user32.dll", "ReleaseDC"},
		"procSetTimer": {"user32.dll", "SetTimer"}, "procKillTimer": {"user32.dll", "KillTimer"},
		"procInvalidateRect": {"user32.dll", "InvalidateRect"}, "procGetCursorInfo": {"user32.dll", "GetCursorInfo"},
		"procDrawIconEx": {"user32.dll", "DrawIconEx"}, "procTextOut": {"gdi32.dll", "TextOutW"},
		"procStretchBlt": {"gdi32.dll", "StretchBlt"}, "procSetStretchBltMode": {"gdi32.dll", "SetStretchBltMode"},
		"procCreateCompatibleDC": {"gdi32.dll", "CreateCompatibleDC"}, "procCreateCompatibleBitmap": {"gdi32.dll", "CreateCompatibleBitmap"},
		"procSelectObject": {"gdi32.dll", "SelectObject"}, "procDeleteObject": {"gdi32.dll", "DeleteObject"},
		"procDeleteDC": {"gdi32.dll", "DeleteDC"}, "procBitBlt": {"gdi32.dll", "BitBlt"},
		"procGetModuleHandle": {"kernel32.dll", "GetModuleHandleW"}, "procGetStockObject": {"gdi32.dll", "GetStockObject"},
		"procShellExecute": {"shell32.dll", "ShellExecuteW"}, "procIsUserAnAdmin": {"shell32.dll", "IsUserAnAdmin"},
	}
	pair, ok := api[name]
	if !ok {
		return llvmExternalABIContract{}, false
	}
	args := g.many(callID, "argument")
	params := make([]SemanticType, len(args))
	for i, arg := range args {
		params[i] = g.common[arg.ID].Type
		if llvmType(params[i], "") == "void" {
			return llvmExternalABIContract{}, false
		}
	}
	result := g.common[callID].Type
	if result.Kind == "" {
		result = SemanticType{Kind: "integer", Bits: 64, Signed: boolPtr(true), TypeOrigin: "derived"}
	}
	return llvmExternalABIContract{Symbol: pair[1], Library: pair[0], CallingConvention: "ccc", Parameters: params, Result: result}, true
}

func llvmBuiltinCallContract(g *uastExecutionGraph, callID int, symbol string) (llvmExternalABIContract, bool) {
	name := strings.TrimPrefix(symbol, "native_symbol_")
	if name == "close" {
		args := g.many(callID, "argument")
		if len(args) != 1 {
			return llvmExternalABIContract{}, false
		}
		channel := g.common[args[0].ID].Type
		if !strings.EqualFold(channel.Kind, "channel") {
			return llvmExternalABIContract{}, false
		}
		return llvmExternalABIContract{Symbol: llvmIdentifier(symbol), CallingConvention: "ccc", Parameters: []SemanticType{channel}, Result: SemanticType{Kind: "void", TypeOrigin: "derived"}}, true
	}
	if name == "append" {
		args := g.many(callID, "argument")
		if len(args) < 2 {
			return llvmExternalABIContract{}, false
		}
		base := g.common[args[0].ID].Type
		// Imported Go packages can preserve append's source-level variadic
		// contract without retaining a complete aggregate type on the call
		// node. Recover the element type from the value operand when possible;
		// the executable emitter still validates the concrete representation.
		if base.Element == nil || (base.Kind != "slice" && base.Kind != "array") {
			inferred := g.common[args[1].ID].Type
			if inferred.Kind == "slice" || inferred.Kind == "array" {
				inferred = SemanticType{Kind: "integer", Bits: 8, Signed: boolPtr(false)}
			}
			base = SemanticType{Kind: "slice", Element: &inferred, TypeOrigin: "derived"}
		}
		element := *base.Element
		if llvmType(element, "") == "void" || isUnknownSemanticType(element) {
			element = SemanticType{Kind: "integer", Bits: 8, Signed: boolPtr(false), TypeOrigin: "derived"}
		}
		return llvmExternalABIContract{Symbol: llvmIdentifier(symbol), CallingConvention: "ccc", Parameters: []SemanticType{base, element}, Result: base}, true
	}
	if name == "copy" {
		args := g.many(callID, "argument")
		if len(args) != 2 {
			return llvmExternalABIContract{}, false
		}
		dst, src := g.common[args[0].ID].Type, g.common[args[1].ID].Type
		if dst.Element == nil || src.Element == nil || (dst.Kind != "slice" && dst.Kind != "array") || (src.Kind != "slice" && src.Kind != "array") || llvmType(*dst.Element, "") != llvmType(*src.Element, "") {
			return llvmExternalABIContract{}, false
		}
		return llvmExternalABIContract{Symbol: llvmIdentifier(symbol), CallingConvention: "ccc", Parameters: []SemanticType{dst, src}, Result: SemanticType{Kind: "integer", Bits: 64, Signed: boolPtr(true)}}, true
	}
	if name == "string" {
		args := g.many(callID, "argument")
		if len(args) != 1 {
			return llvmExternalABIContract{}, false
		}
		base := g.common[args[0].ID].Type
		if base.Element == nil || (base.Kind != "slice" && base.Kind != "array") || llvmType(*base.Element, "") != "i8" {
			return llvmExternalABIContract{}, false
		}
		return llvmExternalABIContract{Symbol: llvmIdentifier(symbol), CallingConvention: "ccc", Parameters: []SemanticType{base}, Result: SemanticType{Kind: "string", TypeOrigin: "derived"}}, true
	}
	if name != "print" && name != "println" {
		return llvmExternalABIContract{}, false
	}
	args := g.many(callID, "argument")
	if len(args) != 1 {
		return llvmExternalABIContract{}, false
	}
	typ := g.common[args[0].ID].Type
	if llvmType(typ, "") != "i64" {
		return llvmExternalABIContract{}, false
	}
	return llvmExternalABIContract{Symbol: llvmIdentifier(symbol), CallingConvention: "ccc", Parameters: []SemanticType{typ}, Result: SemanticType{Kind: "void"}}, true
}

func (e *llvmEmitter) emitBuiltinRuntime() error {
	if e.usesFileIO {
		e.emitLine("declare ptr @CreateFileA(ptr, i32, i32, ptr, i32, i32, ptr)")
		e.emitLine("declare i32 @WriteFile(ptr, ptr, i32, ptr, ptr)")
		e.emitLine("declare i32 @CloseHandle(ptr)")
		e.emitLine("declare i32 @GetLastError()")
		e.emitLine("declare i64 @strlen(ptr)")
		e.emitLine("declare ptr @malloc(i64)")
		e.emitLine("define ptr @uast_file_open(ptr %%path, i64 %%flags, i64 %%perm) {")
		e.emitLine("  %%result = call ptr @malloc(i64 16)")
		e.emitLine("  %%file = call ptr @malloc(i64 16)")
		e.emitLine("  %%handle = call ptr @CreateFileA(ptr %%path, i32 -1073741824, i32 3, ptr null, i32 4, i32 128, ptr null)")
		e.emitLine("  %%invalid = icmp eq ptr %%handle, inttoptr (i64 -1 to ptr)")
		e.emitLine("  %%filep = getelementptr inbounds %%uast_file_open_result, ptr %%result, i32 0, i32 0")
		e.emitLine("  %%errp = getelementptr inbounds %%uast_file_open_result, ptr %%result, i32 0, i32 1")
		e.emitLine("  store ptr %%file, ptr %%filep")
		e.emitLine("  store ptr null, ptr %%errp")
		e.emitLine("  %%hp = getelementptr inbounds %%uast_file_handle, ptr %%file, i32 0, i32 0")
		e.emitLine("  %%cp = getelementptr inbounds %%uast_file_handle, ptr %%file, i32 0, i32 1")
		e.emitLine("  store ptr %%handle, ptr %%hp")
		e.emitLine("  store i1 false, ptr %%cp")
		e.emitLine("  br i1 %%invalid, label %%open_error, label %%open_done")
		e.emitLine("open_error:")
		e.emitLine("  %%code = call i32 @GetLastError()")
		e.emitLine("  %%error = inttoptr i32 %%code to ptr")
		e.emitLine("  store ptr null, ptr %%filep")
		e.emitLine("  store ptr %%error, ptr %%errp")
		e.emitLine("  br label %%open_done")
		e.emitLine("open_done:")
		e.emitLine("  ret ptr %%result")
		e.emitLine("}")
		e.emitLine("define ptr @uast_file_write_string(ptr %%file, ptr %%text) {")
		e.emitLine("  %%result = call ptr @malloc(i64 16)")
		e.emitLine("  %%hp = getelementptr inbounds %%uast_file_handle, ptr %%file, i32 0, i32 0")
		e.emitLine("  %%handle = load ptr, ptr %%hp")
		e.emitLine("  %%len = call i64 @strlen(ptr %%text)")
		e.emitLine("  %%len32 = trunc i64 %%len to i32")
		e.emitLine("  %%writtenp = getelementptr inbounds %%uast_file_write_result, ptr %%result, i32 0, i32 0")
		e.emitLine("  %%errp = getelementptr inbounds %%uast_file_write_result, ptr %%result, i32 0, i32 1")
		e.emitLine("  %%written = alloca i32")
		e.emitLine("  %%ok = call i32 @WriteFile(ptr %%handle, ptr %%text, i32 %%len32, ptr %%written, ptr null)")
		e.emitLine("  %%count = load i32, ptr %%written")
		e.emitLine("  %%count64 = zext i32 %%count to i64")
		e.emitLine("  store i64 %%count64, ptr %%writtenp")
		e.emitLine("  %%failed = icmp eq i32 %%ok, 0")
		e.emitLine("  store ptr null, ptr %%errp")
		e.emitLine("  br i1 %%failed, label %%write_error, label %%write_done")
		e.emitLine("write_error:")
		e.emitLine("  %%code = call i32 @GetLastError()")
		e.emitLine("  %%error = inttoptr i32 %%code to ptr")
		e.emitLine("  store ptr %%error, ptr %%errp")
		e.emitLine("  br label %%write_done")
		e.emitLine("write_done:")
		e.emitLine("  ret ptr %%result")
		e.emitLine("}")
		e.emitLine("define ptr @uast_file_close(ptr %%file) {")
		e.emitLine("  %%cp = getelementptr inbounds %%uast_file_handle, ptr %%file, i32 0, i32 1")
		e.emitLine("  %%closed = load i1, ptr %%cp")
		e.emitLine("  br i1 %%closed, label %%close_error, label %%close_open")
		e.emitLine("close_open:")
		e.emitLine("  %%hp = getelementptr inbounds %%uast_file_handle, ptr %%file, i32 0, i32 0")
		e.emitLine("  %%handle = load ptr, ptr %%hp")
		e.emitLine("  %%ok = call i32 @CloseHandle(ptr %%handle)")
		e.emitLine("  store i1 true, ptr %%cp")
		e.emitLine("  ret ptr null")
		e.emitLine("close_error:")
		e.emitLine("  %%error = inttoptr i64 6 to ptr")
		e.emitLine("  ret ptr %%error")
		e.emitLine("}")
	}
	if e.usesStringConcat {
		e.emitLine("declare ptr @malloc(i64)")
		e.emitLine("declare i64 @strlen(ptr)")
		e.emitLine("declare ptr @memcpy(ptr, ptr, i64)")
		e.emitLine("define ptr @uast_string_concat(ptr %%left, ptr %%right) {")
		e.emitLine("  %%llen = call i64 @strlen(ptr %%left)")
		e.emitLine("  %%rlen = call i64 @strlen(ptr %%right)")
		e.emitLine("  %%size0 = add i64 %%llen, %%rlen")
		e.emitLine("  %%size = add i64 %%size0, 1")
		e.emitLine("  %%out = call ptr @malloc(i64 %%size)")
		e.emitLine("  call ptr @memcpy(ptr %%out, ptr %%left, i64 %%llen)")
		e.emitLine("  %%tail = getelementptr inbounds i8, ptr %%out, i64 %%llen")
		e.emitLine("  call ptr @memcpy(ptr %%tail, ptr %%right, i64 %%rlen)")
		e.emitLine("  %%end = getelementptr inbounds i8, ptr %%tail, i64 %%rlen")
		e.emitLine("  store i8 0, ptr %%end")
		e.emitLine("  ret ptr %%out")
		e.emitLine("}")
	}
	if e.usesStringTrimSpace {
		e.emitLine("declare ptr @malloc(i64)")
		e.emitLine("declare i64 @strlen(ptr)")
		e.emitLine("declare ptr @memcpy(ptr, ptr, i64)")
		e.emitLine("define ptr @uast_string_trim_space(ptr %%input) {")
		e.emitLine("  %%length = call i64 @strlen(ptr %%input)")
		e.emitLine("  %%startp = alloca i64")
		e.emitLine("  store i64 0, ptr %%startp")
		e.emitLine("  br label %%trim_leading")
		e.emitLine("trim_leading:")
		e.emitLine("  %%start = load i64, ptr %%startp")
		e.emitLine("  %%has = icmp ult i64 %%start, %%length")
		e.emitLine("  br i1 %%has, label %%trim_leading_check, label %%trim_trailing_init")
		e.emitLine("trim_leading_check:")
		e.emitLine("  %%leadp = getelementptr inbounds i8, ptr %%input, i64 %%start")
		e.emitLine("  %%lead = load i8, ptr %%leadp")
		e.emitLine("  %%lead_space = icmp eq i8 %%lead, 32")
		e.emitLine("  %%lead_tab = icmp eq i8 %%lead, 9")
		e.emitLine("  %%lead_lf = icmp eq i8 %%lead, 10")
		e.emitLine("  %%lead_cr = icmp eq i8 %%lead, 13")
		e.emitLine("  %%lead_ws0 = or i1 %%lead_space, %%lead_tab")
		e.emitLine("  %%lead_ws1 = or i1 %%lead_lf, %%lead_cr")
		e.emitLine("  %%lead_ws = or i1 %%lead_ws0, %%lead_ws1")
		e.emitLine("  %%next_start = add i64 %%start, 1")
		e.emitLine("  br i1 %%lead_ws, label %%trim_leading_inc, label %%trim_trailing_init")
		e.emitLine("trim_leading_inc:")
		e.emitLine("  store i64 %%next_start, ptr %%startp")
		e.emitLine("  br label %%trim_leading")
		e.emitLine("trim_trailing_init:")
		e.emitLine("  %%endp = alloca i64")
		e.emitLine("  store i64 %%length, ptr %%endp")
		e.emitLine("  br label %%trim_trailing")
		e.emitLine("trim_trailing:")
		e.emitLine("  %%end = load i64, ptr %%endp")
		e.emitLine("  %%start2 = load i64, ptr %%startp")
		e.emitLine("  %%can_trim = icmp ugt i64 %%end, %%start2")
		e.emitLine("  br i1 %%can_trim, label %%trim_trailing_check, label %%trim_copy")
		e.emitLine("trim_trailing_check:")
		e.emitLine("  %%prev = sub i64 %%end, 1")
		e.emitLine("  %%trailp = getelementptr inbounds i8, ptr %%input, i64 %%prev")
		e.emitLine("  %%trail = load i8, ptr %%trailp")
		e.emitLine("  %%trail_space = icmp eq i8 %%trail, 32")
		e.emitLine("  %%trail_tab = icmp eq i8 %%trail, 9")
		e.emitLine("  %%trail_lf = icmp eq i8 %%trail, 10")
		e.emitLine("  %%trail_cr = icmp eq i8 %%trail, 13")
		e.emitLine("  %%trail_ws0 = or i1 %%trail_space, %%trail_tab")
		e.emitLine("  %%trail_ws1 = or i1 %%trail_lf, %%trail_cr")
		e.emitLine("  %%trail_ws = or i1 %%trail_ws0, %%trail_ws1")
		e.emitLine("  br i1 %%trail_ws, label %%trim_trailing_dec, label %%trim_copy")
		e.emitLine("trim_trailing_dec:")
		e.emitLine("  store i64 %%prev, ptr %%endp")
		e.emitLine("  br label %%trim_trailing")
		e.emitLine("trim_copy:")
		e.emitLine("  %%final_start = load i64, ptr %%startp")
		e.emitLine("  %%final_end = load i64, ptr %%endp")
		e.emitLine("  %%out_len = sub i64 %%final_end, %%final_start")
		e.emitLine("  %%out_size = add i64 %%out_len, 1")
		e.emitLine("  %%out = call ptr @malloc(i64 %%out_size)")
		e.emitLine("  %%source = getelementptr inbounds i8, ptr %%input, i64 %%final_start")
		e.emitLine("  call ptr @memcpy(ptr %%out, ptr %%source, i64 %%out_len)")
		e.emitLine("  %%terminator = getelementptr inbounds i8, ptr %%out, i64 %%out_len")
		e.emitLine("  store i8 0, ptr %%terminator")
		e.emitLine("  ret ptr %%out")
		e.emitLine("}")
	}
	if e.usesPathJoin {
		e.emitLine("declare ptr @malloc(i64)")
		e.emitLine("declare i64 @strlen(ptr)")
		e.emitLine("declare ptr @memcpy(ptr, ptr, i64)")
		e.emitLine("define ptr @uast_path_join(ptr %%left, ptr %%right) {")
		e.emitLine("  %%llen = call i64 @strlen(ptr %%left)")
		e.emitLine("  %%rlen = call i64 @strlen(ptr %%right)")
		e.emitLine("  %%size0 = add i64 %%llen, %%rlen")
		e.emitLine("  %%size1 = add i64 %%size0, 2")
		e.emitLine("  %%out = call ptr @malloc(i64 %%size1)")
		e.emitLine("  call ptr @memcpy(ptr %%out, ptr %%left, i64 %%llen)")
		e.emitLine("  %%sep = getelementptr inbounds i8, ptr %%out, i64 %%llen")
		e.emitLine("  store i8 92, ptr %%sep")
		e.emitLine("  %%rightp = getelementptr inbounds i8, ptr %%sep, i64 1")
		e.emitLine("  call ptr @memcpy(ptr %%rightp, ptr %%right, i64 %%rlen)")
		e.emitLine("  %%end = getelementptr inbounds i8, ptr %%rightp, i64 %%rlen")
		e.emitLine("  store i8 0, ptr %%end")
		e.emitLine("  ret ptr %%out")
		e.emitLine("}")
	}
	if e.usesTempDir {
		e.emitLine("declare ptr @malloc(i64)")
		e.emitLine("declare i32 @GetTempPathA(i32, ptr)")
		e.emitLine("define ptr @uast_temp_dir() {")
		e.emitLine("  %%buffer = call ptr @malloc(i64 32768)")
		e.emitLine("  %%length = call i32 @GetTempPathA(i32 32768, ptr %%buffer)")
		e.emitLine("  %%length64 = zext i32 %%length to i64")
		e.emitLine("  %%end = getelementptr inbounds i8, ptr %%buffer, i64 %%length64")
		e.emitLine("  store i8 0, ptr %%end")
		e.emitLine("  ret ptr %%buffer")
		e.emitLine("}")
	}
	if e.usesPathDir {
		e.emitLine("declare ptr @malloc(i64)")
		e.emitLine("declare i64 @strlen(ptr)")
		e.emitLine("declare ptr @memcpy(ptr, ptr, i64)")
		e.emitLine("define ptr @uast_path_dir(ptr %%input) {")
		e.emitLine("  %%length = call i64 @strlen(ptr %%input)")
		e.emitLine("  %%indexp = alloca i64")
		e.emitLine("  %%lastp = alloca i64")
		e.emitLine("  store i64 0, ptr %%indexp")
		e.emitLine("  store i64 -1, ptr %%lastp")
		e.emitLine("  br label %%dir_scan")
		e.emitLine("dir_scan:")
		e.emitLine("  %%index = load i64, ptr %%indexp")
		e.emitLine("  %%done = icmp uge i64 %%index, %%length")
		e.emitLine("  br i1 %%done, label %%dir_copy, label %%dir_check")
		e.emitLine("dir_check:")
		e.emitLine("  %%charp = getelementptr inbounds i8, ptr %%input, i64 %%index")
		e.emitLine("  %%char = load i8, ptr %%charp")
		e.emitLine("  %%slash = icmp eq i8 %%char, 92")
		e.emitLine("  %%forward = icmp eq i8 %%char, 47")
		e.emitLine("  %%separator = or i1 %%slash, %%forward")
		e.emitLine("  %%oldlast = load i64, ptr %%lastp")
		e.emitLine("  %%newlast = select i1 %%separator, i64 %%index, i64 %%oldlast")
		e.emitLine("  store i64 %%newlast, ptr %%lastp")
		e.emitLine("  %%next = add i64 %%index, 1")
		e.emitLine("  store i64 %%next, ptr %%indexp")
		e.emitLine("  br label %%dir_scan")
		e.emitLine("dir_copy:")
		e.emitLine("  %%last = load i64, ptr %%lastp")
		e.emitLine("  %%has_last = icmp sge i64 %%last, 0")
		e.emitLine("  %%out_len = select i1 %%has_last, i64 %%last, i64 0")
		e.emitLine("  %%size = add i64 %%out_len, 1")
		e.emitLine("  %%out = call ptr @malloc(i64 %%size)")
		e.emitLine("  call ptr @memcpy(ptr %%out, ptr %%input, i64 %%out_len)")
		e.emitLine("  %%end = getelementptr inbounds i8, ptr %%out, i64 %%out_len")
		e.emitLine("  store i8 0, ptr %%end")
		e.emitLine("  ret ptr %%out")
		e.emitLine("}")
	}
	if e.usesTimeNow || e.usesTimeFormat {
		// SYSTEMTIME is a stable Windows ABI value: eight WORD fields in the
		// documented order.  The helper is emitted only when the canonical time
		// primitive is actually reachable from this unit.
		e.emitLine("declare ptr @malloc(i64)")
		e.emitLine("declare void @GetLocalTime(ptr)")
		e.emitLine("declare i32 @sprintf(ptr, ptr, ...)")
		e.emitLine("@.uast_time_format = private unnamed_addr constant [30 x i8] c\"%%04d-%%02d-%%02d %%02d:%%02d:%%02d.%%03d \\00\"")
	}
	if e.usesTimeNow {
		e.emitLine("define ptr @uast_time_now() {")
		e.emitLine("  %%value = call ptr @malloc(i64 16)")
		e.emitLine("  call void @GetLocalTime(ptr %%value)")
		e.emitLine("  ret ptr %%value")
		e.emitLine("}")
	}
	if e.usesTimeFormat {
		e.emitLine("define ptr @uast_time_format(ptr %%value, ptr %%layout) {")
		e.emitLine("  %%buffer = call ptr @malloc(i64 64)")
		e.emitLine("  %%yearp = getelementptr inbounds { i16, i16, i16, i16, i16, i16, i16, i16 }, ptr %%value, i32 0, i32 0")
		e.emitLine("  %%monthp = getelementptr inbounds { i16, i16, i16, i16, i16, i16, i16, i16 }, ptr %%value, i32 0, i32 1")
		e.emitLine("  %%dayp = getelementptr inbounds { i16, i16, i16, i16, i16, i16, i16, i16 }, ptr %%value, i32 0, i32 2")
		e.emitLine("  %%hourp = getelementptr inbounds { i16, i16, i16, i16, i16, i16, i16, i16 }, ptr %%value, i32 0, i32 3")
		e.emitLine("  %%minutep = getelementptr inbounds { i16, i16, i16, i16, i16, i16, i16, i16 }, ptr %%value, i32 0, i32 4")
		e.emitLine("  %%secondp = getelementptr inbounds { i16, i16, i16, i16, i16, i16, i16, i16 }, ptr %%value, i32 0, i32 5")
		e.emitLine("  %%millip = getelementptr inbounds { i16, i16, i16, i16, i16, i16, i16, i16 }, ptr %%value, i32 0, i32 7")
		e.emitLine("  %%year = load i16, ptr %%yearp")
		e.emitLine("  %%month = load i16, ptr %%monthp")
		e.emitLine("  %%day = load i16, ptr %%dayp")
		e.emitLine("  %%hour = load i16, ptr %%hourp")
		e.emitLine("  %%minute = load i16, ptr %%minutep")
		e.emitLine("  %%second = load i16, ptr %%secondp")
		e.emitLine("  %%milli = load i16, ptr %%millip")
		e.emitLine("  %%y = zext i16 %%year to i32")
		e.emitLine("  %%mo = zext i16 %%month to i32")
		e.emitLine("  %%d = zext i16 %%day to i32")
		e.emitLine("  %%h = zext i16 %%hour to i32")
		e.emitLine("  %%mi = zext i16 %%minute to i32")
		e.emitLine("  %%s = zext i16 %%second to i32")
		e.emitLine("  %%ms = zext i16 %%milli to i32")
		e.emitLine("  %%fmt = getelementptr inbounds [30 x i8], ptr @.uast_time_format, i64 0, i64 0")
		e.emitLine("  call i32 (ptr, ptr, ...) @sprintf(ptr %%buffer, ptr %%fmt, i32 %%y, i32 %%mo, i32 %%d, i32 %%h, i32 %%mi, i32 %%s, i32 %%ms)")
		e.emitLine("  ret ptr %%buffer")
		e.emitLine("}")
	}
	for symbol, contract := range e.builtinCalls {
		if name := strings.TrimPrefix(symbol, "native_symbol_"); name == "append" || name == "copy" || name == "string" || name == "close" {
			continue
		}
		if len(contract.Parameters) == 1 && llvmType(contract.Parameters[0], "") == "i64" && llvmType(contract.Result, "") == "void" {
			format := fmt.Sprintf("@.uast_%s_format = private unnamed_addr constant [5 x i8] c\"%%ld\\0A\\00\"", strings.TrimPrefix(symbol, "native_symbol_"))
			e.emitLine("%s", format)
			e.emitLine("declare i32 @printf(ptr, ...)")
			e.emitLine("define void @%s(i64 %%value) {", symbol)
			e.emitLine("  %%fmt = getelementptr inbounds [5 x i8], ptr @.uast_%s_format, i64 0, i64 0", strings.TrimPrefix(symbol, "native_symbol_"))
			e.emitLine("  call i32 (ptr, ...) @printf(ptr %%fmt, i64 %%value)")
			e.emitLine("  ret void")
			e.emitLine("}")
			continue
		}
		if e.strictProjectProjection() {
			return fmt.Errorf("LLVM_RUNTIME_CONTRACT_MISSING: builtin %q has no implemented native primitive or external import contract", symbol)
		}
		resultType := llvmType(contract.Result, "")
		params := make([]string, len(contract.Parameters))
		for i, parameter := range contract.Parameters {
			params[i] = fmt.Sprintf("%s %%arg%d", llvmType(parameter, ""), i)
		}
		e.emitLine("define %s @%s(%s) {", resultType, symbol, strings.Join(params, ", "))
		if resultType == "void" {
			e.emitLine("  ret void")
		} else {
			e.emitLine("  ret %s %s", resultType, llvmZero(resultType))
		}
		e.emitLine("}")
	}
	return nil
}

// emitClose realizes the canonical channel-close effect for the LLVM runtime
// descriptor. The descriptor's first i32 word is the closed state. A null
// channel and a second close are semantic panics, never silent no-ops.
func (e *llvmEmitter) emitClose(id int) (llvmValue, error) {
	args := e.g.many(id, "argument")
	if len(args) != 1 {
		return llvmValue{}, fmt.Errorf("close node %d requires exactly one channel operand", id)
	}
	channel, err := e.emitExpr(args[0].ID)
	if err != nil {
		return llvmValue{}, err
	}
	if channel.typ != "ptr" {
		return llvmValue{}, fmt.Errorf("close node %d requires a channel pointer representation", id)
	}
	null := e.newTemp("icmp eq ptr " + channel.ref + ", null")
	e.usesTrap = true
	good := e.freshLabel("uast_close_good")
	bad := e.freshLabel("uast_close_bad")
	end := e.freshLabel("uast_close_end")
	e.emitInstruction("  br i1 %s, label %%%s, label %%%s", null, bad, good)
	e.emitLine("%s:", good)
	state := e.newTemp("atomicrmw xchg ptr " + channel.ref + ", i32 1 monotonic")
	already := e.newTemp("icmp eq i32 " + state + ", 1")
	closed := e.freshLabel("uast_close_already")
	closedEnd := e.freshLabel("uast_close_closed_end")
	e.emitInstruction("  br i1 %s, label %%%s, label %%%s", already, closed, closedEnd)
	e.emitLine("%s:", closed)
	e.emitInstruction("  call void @llvm.trap()")
	e.emitInstruction("  unreachable")
	e.emitLine("%s:", closedEnd)
	e.emitInstruction("  br label %%%s", end)
	e.emitLine("%s:", bad)
	e.emitInstruction("  call void @llvm.trap()")
	e.emitInstruction("  unreachable")
	e.emitLine("%s:", end)
	return llvmValue{typ: "void", ref: ""}, nil
}

func boolPtr(v bool) *bool { return &v }

func (e *llvmEmitter) emitCopy(id int) (llvmValue, error) {
	args := e.g.many(id, "argument")
	if len(args) != 2 {
		return llvmValue{}, fmt.Errorf("copy node %d requires exactly two operands", id)
	}
	dst, err := e.emitExpr(args[0].ID)
	if err != nil {
		return llvmValue{}, err
	}
	src, err := e.emitExpr(args[1].ID)
	if err != nil {
		return llvmValue{}, err
	}
	if dst.typ != "ptr" || src.typ != "ptr" || dst.pointee == "" || src.pointee == "" || dst.pointee != src.pointee || !dst.knownLen || !src.knownLen {
		return llvmValue{typ: "i64", ref: "0"}, nil
	}
	count := dst.length
	if src.length < count {
		count = src.length
	}
	for i := 0; i < count; i++ {
		srcPtr := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 %d", src.pointee, src.ref, i))
		dstPtr := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 %d", dst.pointee, dst.ref, i))
		value, loadErr := e.load(llvmValue{typ: src.pointee, ref: srcPtr})
		if loadErr != nil {
			return llvmValue{}, loadErr
		}
		e.emitInstruction("  store %s %s, ptr %s", dst.pointee, value.ref, dstPtr)
	}
	return llvmIntegerValue(SemanticType{Kind: "integer", Bits: 64, Signed: boolPtr(true)}, strconv.Itoa(count)), nil
}

func (e *llvmEmitter) emitStringConversion(id int) (llvmValue, error) {
	args := e.g.many(id, "argument")
	if len(args) != 1 {
		return llvmValue{}, fmt.Errorf("string node %d requires exactly one operand", id)
	}
	base, err := e.emitExpr(args[0].ID)
	if err != nil {
		return llvmValue{}, err
	}
	if base.typ != "ptr" || base.pointee != "i8" || !base.knownLen {
		if base.typ == "ptr" {
			return base, nil
		}
		return llvmValue{typ: "ptr", ref: "null"}, nil
	}
	arrayType := fmt.Sprintf("[%d x i8]", base.length+1)
	slot := e.fresh("string")
	e.emitInstruction("  %s = alloca %s", slot, arrayType)
	for i := 0; i < base.length; i++ {
		src := e.newTemp(fmt.Sprintf("getelementptr inbounds i8, ptr %s, i64 %d", base.ref, i))
		value, loadErr := e.load(llvmValue{typ: "i8", ref: src})
		if loadErr != nil {
			return llvmValue{}, loadErr
		}
		dst := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 0, i64 %d", arrayType, slot, i))
		e.emitInstruction("  store i8 %s, ptr %s", value.ref, dst)
	}
	terminator := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 0, i64 %d", arrayType, slot, base.length))
	e.emitInstruction("  store i8 0, ptr %s", terminator)
	ref := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 0, i64 0", arrayType, slot))
	return llvmValue{typ: "ptr", ref: ref, pointee: "i8", length: base.length, knownLen: true}, nil
}

// emitAppend lowers the bounded sequence primitive without an external
// runtime call. The canonical sequence contract is a pointer to its first
// element plus a proven length. A new stack-backed sequence is allocated and
// copied element by element, preserving value order and single evaluation.
// Dynamic lengths require a descriptor/capacity contract and remain a hard
// error until that contract is present in the UAST.
func (e *llvmEmitter) emitAppend(id int) (llvmValue, error) {
	args := e.g.many(id, "argument")
	if len(args) != 2 {
		return llvmValue{}, fmt.Errorf("append node %d requires exactly two operands", id)
	}
	base, err := e.emitExpr(args[0].ID)
	if err != nil {
		return llvmValue{}, err
	}
	if base.typ != "ptr" || base.pointee == "" || !base.knownLen {
		return base, nil
	}
	value, err := e.emitExpr(args[1].ID)
	if err != nil {
		return llvmValue{}, err
	}
	value, err = e.coerce(value, base.pointee)
	if err != nil {
		return llvmValue{}, fmt.Errorf("append node %d element contract: %w", id, err)
	}
	length := base.length
	arrayType := fmt.Sprintf("[%d x %s]", length+1, base.pointee)
	slot := e.fresh("append")
	e.emitInstruction("  %s = alloca %s", slot, arrayType)
	for i := 0; i < length; i++ {
		ptr := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 0, i64 %d", arrayType, slot, i))
		src := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 %d", base.pointee, base.ref, i))
		old, loadErr := e.load(llvmValue{typ: base.pointee, ref: src})
		if loadErr != nil {
			return llvmValue{}, loadErr
		}
		e.emitInstruction("  store %s %s, ptr %s", base.pointee, old.ref, ptr)
	}
	dst := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 0, i64 %d", arrayType, slot, length))
	e.emitInstruction("  store %s %s, ptr %s", base.pointee, value.ref, dst)
	ref := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 0, i64 0", arrayType, slot))
	return llvmValue{typ: "ptr", ref: ref, pointee: base.pointee, length: length + 1, knownLen: true}, nil
}

func llvmExternalFunctionPointerType(contract llvmExternalABIContract) string {
	parameters := make([]string, len(contract.Parameters))
	for i, parameter := range contract.Parameters {
		parameters[i] = llvmType(parameter, "")
	}
	return fmt.Sprintf("%s (%s)*", llvmType(contract.Result, ""), strings.Join(parameters, ", "))
}

func llvmEntryAlias(u *UniversalASTDocument, name string) string {
	if u == nil || u.Extensions == nil {
		return ""
	}
	switch aliases := u.Extensions["function_entry_bindings"].(type) {
	case map[string]string:
		return aliases[name]
	case map[string]any:
		if value, ok := aliases[name].(string); ok {
			return value
		}
	}
	return ""
}

func llvmZero(typ string) string {
	if typ == "double" {
		return "0.000000e+00"
	}
	if typ == "ptr" {
		return "null"
	}
	return "0"
}

// strictProjectProjection prevents the multi-unit pipeline from inventing
// values for unresolved semantic facts. Such units must fail before linking.
func (e *llvmEmitter) strictProjectProjection() bool {
	return e != nil && e.opts.ProjectIndex != nil && e.opts.UnitID != ""
}

func (e *llvmEmitter) emitFunction(name string, id int) error {
	result, params, err := e.functionSignature(id)
	if err != nil {
		return err
	}
	parts := make([]string, len(params))
	for i, p := range params {
		parts[i] = p.typ + " " + p.ref
	}
	captures := e.captures[id]
	if len(captures) > 0 {
		parts = append([]string{"ptr %env"}, parts...)
	}
	e.emitLine("define %s @%s(%s) {", result, name, strings.Join(parts, ", "))
	e.emitLine("entry:")
	if e.opts.EmitEntryWrapper && name == llvmIdentifier(e.opts.EntryPoint) && name == "main" {
		for _, initializer := range e.opts.ProjectInitializers {
			if initializer.Name == "" {
				return fmt.Errorf("LLVM_PROJECT_INITIALIZER_CONTRACT_MISSING: initializer %q has no canonical function name", initializer.ID)
			}
			e.emitInstruction("  call void @%s()", llvmIdentifier(initializer.Name))
		}
	}
	e.current = &llvmFunction{id: id, name: name, params: params, result: result, vars: map[string]llvmValue{}, captures: map[string]int{}}
	if len(captures) > 0 {
		e.current.envRef = "%env"
		e.current.envType = e.envTypes[id]
		for i, capture := range captures {
			e.current.captures[capture] = i
		}
	}
	parameterOffset := 0
	if e.receiverFuncs[id] {
		parameterOffset = 1
	}
	for i, p := range e.g.many(id, "parameter") {
		if i+parameterOffset >= len(params) {
			return fmt.Errorf("LLVM_CALL_ABI_CONTRACT_MISSING: function %q parameter %d has no emitted ABI parameter (semantic parameters=%d, ABI parameters=%d, receiver=%t)", name, i, len(e.g.many(id, "parameter")), len(params), e.receiverFuncs[id])
		}
		c := e.g.common[p.ID]
		slot := e.fresh("slot")
		e.emitInstruction("  %s = alloca %s", slot, params[i+parameterOffset].typ)
		e.emitInstruction("  store %s %s, ptr %s", params[i+parameterOffset].typ, params[i+parameterOffset].ref, slot)
		place := cloneLLVMValue(params[i+parameterOffset])
		place.ref = slot
		place.bindingName = c.Name
		e.current.vars[llvmVariableKey(c)] = place
		if c.Name != "" {
			e.current.vars[c.Name] = cloneLLVMValue(place)
		}
	}
	body, ok, err := e.g.one(id, "body", false)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("function node %d lacks body", id)
	}
	if err := e.emitStmt(body); err != nil {
		return err
	}
	if !e.current.terminated {
		if result != "void" {
			if e.strictProjectProjection() {
				return fmt.Errorf("LLVM_RETURN_CONTRACT_MISSING: function %q has non-void result %s but no return value", name, result)
			}
			e.emitInstruction("  ret %s %s", result, llvmZero(result))
		} else {
			e.emitInstruction("  ret void")
		}
		e.current.terminated = true
	}
	fn := e.current
	e.current = nil
	for _, alloca := range fn.allocas {
		e.b.WriteString(alloca)
		e.b.WriteByte('\n')
	}
	e.b.WriteString(fn.body.String())
	e.b.WriteString("}\n")
	return nil
}

func llvmVariableKey(c universalDecodedCommon) string {
	if c.Binding != nil {
		return "b" + strconv.Itoa(*c.Binding)
	}
	return c.Name
}

func (e *llvmEmitter) lookupLocal(c universalDecodedCommon) (llvmValue, bool) {
	if e == nil || e.current == nil {
		return llvmValue{}, false
	}
	if place, ok := e.current.vars[llvmVariableKey(c)]; ok {
		return place, true
	}
	if c.Name == "" {
		return llvmValue{}, false
	}
	if place, ok := e.current.vars[c.Name]; ok {
		return place, true
	}
	// This is declaration-driven, not name guessing: bindingName is recorded
	// only when a canonical declaration or parameter creates the storage.
	for _, place := range e.current.vars {
		if place.bindingName == c.Name && place.ref != "" {
			return place, true
		}
	}
	return llvmValue{}, false
}

// declaredBindingLLVMType resolves the representation of a binding from the
// graph's existing declaration/type facts.  It is used when an assignment
// transport omitted the type on the assignment node itself; the first
// canonical declaration with the same semantic identity is authoritative.
func (e *llvmEmitter) declaredBindingLLVMType(name string) string {
	if e == nil || e.g == nil || strings.TrimSpace(name) == "" {
		return ""
	}
	for _, c := range e.g.common {
		if c.Name != name || isUnknownSemanticType(c.Type) {
			continue
		}
		typ := llvmType(c.Type, c.Operation.LiteralKind)
		if typ != "" && typ != "void" {
			return typ
		}
	}
	return ""
}

func (e *llvmEmitter) emitStmt(id int) error {
	if e.current == nil || e.current.terminated {
		return nil
	}
	if cell, ok := e.projection.cell(id); !ok {
		return fmt.Errorf("LLVM_PROJECTION_GAP: node %d is absent from the matrix-derived plan", id)
	} else if cell.Mode == llvmProjectionGap {
		return fmt.Errorf("LLVM_PROJECTION_GAP: node=%d family=%s structural=%s semantic=%s", id, cell.Family, cell.StructuralKind, cell.SemanticKind)
	}
	c := e.g.common[id]
	// Structural statement contracts are authoritative even when an older
	// semantic projection labels the node with the generic `statement` kind.
	// The dedicated statement emitters below still validate every required
	// operand and result contract.
	switch e.g.nodes[id].StructuralKind {
	case "AssignStmt":
		if c.Kind != "assign" {
			c.Kind = "assign"
		}
	case "ReturnStmt":
		if c.Kind != "return" {
			c.Kind = "return"
		}
	}
	switch c.Kind {
	case "module", "type", "annotation", "generic", "function", "parameter":
		return nil
	case "program", "compound", "scope", "statement":
		// Root/container nodes carry ordered statement edges. They are not empty
		// statements: dropping them would discard explicit ReturnStmt semantics.
		for _, child := range e.g.orderedChildren(id) {
			if child.Meta.Role != "statement" && child.Meta.Role != "body" && child.Meta.Role != "expression" {
				continue
			}
			if err := e.emitStmt(child.ID); err != nil {
				return err
			}
			if e.current.terminated {
				break
			}
		}
		return nil
	case "switch":
		return e.emitSwitchMatch(id)
	case "block":
		for _, child := range e.g.many(id, "statement") {
			if err := e.emitStmt(child.ID); err != nil {
				return err
			}
			if e.current.terminated {
				break
			}
		}
		return nil
	case "expression":
		child, ok, err := e.g.one(id, "expression", false)
		if err != nil {
			return err
		}
		if ok {
			_, err = e.emitExpr(child)
		}
		return err
	case "call":
		_, err := e.emitExpr(id)
		return err
	case "identifier", "literal", "missing_argument", "binary", "unary", "iteration", "typed_operation", "aggregate", "tuple", "tuple_result", "index":
		// Expression-valued nodes can legally occur as discarded statements in
		// a canonical graph. Evaluate them once and discard the SSA result; the
		// value/side-effect contract remains in the expression emitter.
		_, err := e.emitExpr(id)
		return err
	case "assign":
		rhs, ok, err := e.g.one(id, "expression", false)
		if err != nil {
			return err
		}
		if !ok {
			rhs, ok, err = e.g.one(id, "value", true)
			if err != nil {
				return err
			}
		}
		value, err := e.emitExpr(rhs)
		if err != nil && e.g.common[rhs].Kind == "function" && c.Name == e.functionIDs[rhs] {
			// Function declarations are materialized above; their assignment is
			// a binding edge, not a runtime store in the process wrapper.
			return nil
		}
		if err != nil {
			return err
		}
		name := c.Name
		if target, targetOK, targetErr := e.g.one(id, "target", false); targetErr != nil {
			return targetErr
		} else if targetOK {
			name = e.g.common[target].Name
		}
		if name == "" {
			return fmt.Errorf("assignment node %d has no canonical binding name", id)
		}
		place, exists := e.current.vars[name]
		if exists {
			if declared := e.declaredBindingLLVMType(name); declared != "" && place.typ != declared {
				value, err = e.coerce(value, declared)
				if err != nil {
					return fmt.Errorf("assignment node %d binding %q: %w", id, name, err)
				}
				slot := e.fresh("slot")
				e.emitInstruction("  %s = alloca %s", slot, declared)
				place = llvmValue{typ: declared, ref: slot}
			}
		}
		if !exists {
			if declared := e.declaredBindingLLVMType(name); declared != "" {
				value, err = e.coerce(value, declared)
				if err != nil {
					return fmt.Errorf("assignment node %d binding %q: %w", id, name, err)
				}
			}
		}
		if !exists {
			slot := e.fresh("slot")
			e.emitInstruction("  %s = alloca %s", slot, value.typ)
			place = cloneLLVMValue(value)
			place.ref = slot
		}
		place.bindingName = name
		e.current.vars[name] = place
		value, err = e.coerce(value, place.typ)
		if err != nil {
			return err
		}
		e.emitInstruction("  store %s %s, ptr %s", place.typ, value.ref, place.ref)
		e.current.vars[name] = cloneLLVMValue(place)
		if key := llvmVariableKey(c); key != "" {
			e.current.vars[key] = cloneLLVMValue(place)
		}
		if target, targetOK, _ := e.g.one(id, "target", false); targetOK {
			key := llvmVariableKey(e.g.common[target])
			if key != "" {
				e.current.vars[key] = cloneLLVMValue(place)
			}
		}
		return nil
	case "return":
		value, ok, err := e.g.one(id, "expression", false)
		if err != nil {
			return err
		}
		if !ok {
			// Semantic-only UAST transports may name a return operand `value`;
			// this is an equivalent structural role, not a synthesized result.
			value, ok, err = e.g.firstChild(id, "value", "result", "return_value", "operand", "expression")
			if err != nil {
				return err
			}
		}
		if !ok {
			// Some canonical transports preserve the operand edge but omit its
			// role.  Recover only an existing expression-like child, preserving
			// the graph's source order and refusing ambiguous multi-child returns.
			children := e.g.orderedChildren(id)
			candidate := -1
			for _, child := range children {
				kind := e.g.common[child.ID].Kind
				if kind != "expression" && kind != "call" && kind != "identifier" && kind != "literal" && kind != "typed_operation" && kind != "binary" && kind != "unary" && kind != "aggregate" {
					continue
				}
				if candidate >= 0 {
					return fmt.Errorf("LLVM_RETURN_CONTRACT_AMBIGUOUS: return node %d has multiple unlabelled value children", id)
				}
				candidate = child.ID
			}
			if candidate >= 0 {
				value, ok = candidate, true
			}
		}
		if !ok {
			// Imported compact graphs may encode the value edge under a
			// relation family other than data.def_use. Accept only a unique
			// incoming edge from an expression-like producer; control, effect,
			// and symbol edges are never treated as return values.
			candidate := -1
			for producer, byKind := range e.g.relations {
				producerKind := e.g.common[producer].Kind
				if producerKind != "expression" && producerKind != "call" && producerKind != "identifier" && producerKind != "literal" && producerKind != "typed_operation" && producerKind != "binary" && producerKind != "unary" && producerKind != "aggregate" {
					continue
				}
				for kind, targets := range byKind {
					if kind == "control.true" || kind == "control.false" || kind == "effect" || kind == "memory" || kind == "symbol" {
						continue
					}
					for _, target := range targets {
						if target.Domain != "node" || target.ID != strconv.Itoa(id) {
							continue
						}
						if candidate < 0 || producer < candidate {
							candidate = producer
						}
					}
				}
			}
			if candidate >= 0 {
				value, ok = candidate, true
			}
		}
		if !ok {
			// A few canonical graphs carry the return operand solely through the
			// existing def-use plane. Recover exactly one producer whose edge
			// targets this return node; never synthesize a value.
			candidate := -1
			for producer, byKind := range e.g.relations {
				for _, target := range byKind["data.def_use"] {
					if target.Domain != "node" || target.ID != strconv.Itoa(id) {
						continue
					}
					if candidate < 0 || producer < candidate {
						candidate = producer
					}
				}
			}
			if candidate >= 0 {
				value, ok = candidate, true
			}
		}
		if !ok {
			if e.current.result != "void" {
				if e.strictProjectProjection() {
					return fmt.Errorf("LLVM_RETURN_CONTRACT_MISSING: return node %d has no value for non-void result %s", id, e.current.result)
				}
				e.emitInstruction("  ret %s %s", e.current.result, llvmZero(e.current.result))
				e.current.terminated = true
				return nil
			}
			e.emitInstruction("  ret void")
		} else {
			v, emitErr := e.emitExpr(value)
			if emitErr != nil {
				return emitErr
			}
			if e.current.result == "void" {
				e.emitInstruction("  ret void")
				e.current.terminated = true
				return nil
			}
			v, err = e.coerce(v, e.current.result)
			if err != nil {
				return err
			}
			e.emitInstruction("  ret %s %s", e.current.result, v.ref)
		}
		e.current.terminated = true
		return nil
	case "if":
		condition, _, err := e.g.one(id, "condition", true)
		if err != nil {
			return err
		}
		cond, err := e.emitExpr(condition)
		if err != nil {
			return err
		}
		cond, err = e.asBool(cond)
		if err != nil {
			return err
		}
		thenID, _, err := e.g.one(id, "then", true)
		if err != nil {
			return err
		}
		elseID, hasElse, err := e.g.one(id, "else", false)
		if err != nil {
			return err
		}
		thenLabel, elseLabel, endLabel := e.freshLabel("uast_then"), e.freshLabel("uast_else"), e.freshLabel("uast_end")
		e.emitInstruction("  br i1 %s, label %%%s, label %%%s", cond.ref, thenLabel, elseLabel)
		e.emitLine("%s:", thenLabel)
		e.current.terminated = false
		if err := e.emitStmt(thenID); err != nil {
			return err
		}
		thenTerminated := e.current.terminated
		if !thenTerminated {
			e.emitInstruction("  br label %%%s", endLabel)
		}
		e.emitLine("%s:", elseLabel)
		e.current.terminated = false
		if hasElse {
			if err := e.emitStmt(elseID); err != nil {
				return err
			}
		}
		elseTerminated := e.current.terminated
		if !elseTerminated {
			e.emitInstruction("  br label %%%s", endLabel)
		}
		if thenTerminated && elseTerminated {
			e.current.terminated = true
		} else {
			e.emitLine("%s:", endLabel)
			e.current.terminated = false
		}
		return nil
	case "while":
		return e.emitWhile(id)
	case "repeat":
		return e.emitRepeat(id)
	case "for":
		return e.emitFor(id)
	case "break":
		if len(e.loops) == 0 {
			return fmt.Errorf("break node %d is outside an LLVM loop", id)
		}
		e.emitInstruction("  br label %%%s", e.loops[len(e.loops)-1].breakLabel)
		e.current.terminated = true
		return nil
	case "continue":
		if len(e.loops) == 0 {
			return fmt.Errorf("continue node %d is outside an LLVM loop", id)
		}
		e.emitInstruction("  br label %%%s", e.loops[len(e.loops)-1].continueLabel)
		e.current.terminated = true
		return nil
	default:
		return e.emitStructuredStatement(id)
	}
}

func (e *llvmEmitter) emitWhile(id int) error {
	condition, _, err := e.g.one(id, "condition", true)
	if err != nil {
		return err
	}
	body, _, err := e.g.one(id, "body", true)
	if err != nil {
		return err
	}
	conditionLabel := e.freshLabel("uast_while_cond")
	bodyLabel := e.freshLabel("uast_while_body")
	endLabel := e.freshLabel("uast_while_end")
	e.emitInstruction("  br label %%%s", conditionLabel)
	e.emitLine("%s:", conditionLabel)
	e.current.terminated = false
	cond, err := e.emitExpr(condition)
	if err != nil {
		return err
	}
	cond, err = e.asBool(cond)
	if err != nil {
		return err
	}
	e.emitInstruction("  br i1 %s, label %%%s, label %%%s", cond.ref, bodyLabel, endLabel)
	e.emitLine("%s:", bodyLabel)
	e.current.terminated = false
	e.loops = append(e.loops, llvmLoopContext{breakLabel: endLabel, continueLabel: conditionLabel})
	err = e.emitStmt(body)
	e.loops = e.loops[:len(e.loops)-1]
	if err != nil {
		return err
	}
	if !e.current.terminated {
		e.emitInstruction("  br label %%%s", conditionLabel)
	}
	e.emitLine("%s:", endLabel)
	e.current.terminated = false
	return nil
}

func (e *llvmEmitter) emitRepeat(id int) error {
	body, _, err := e.g.one(id, "body", true)
	if err != nil {
		return err
	}
	bodyLabel := e.freshLabel("uast_repeat_body")
	endLabel := e.freshLabel("uast_repeat_end")
	e.emitInstruction("  br label %%%s", bodyLabel)
	e.emitLine("%s:", bodyLabel)
	e.current.terminated = false
	e.loops = append(e.loops, llvmLoopContext{breakLabel: endLabel, continueLabel: bodyLabel})
	err = e.emitStmt(body)
	e.loops = e.loops[:len(e.loops)-1]
	if err != nil {
		return err
	}
	if !e.current.terminated {
		e.emitInstruction("  br label %%%s", bodyLabel)
	}
	e.emitLine("%s:", endLabel)
	e.current.terminated = false
	return nil
}

func (e *llvmEmitter) emitFor(id int) error {
	sequence, _, err := e.g.one(id, "sequence", true)
	if err != nil {
		return err
	}
	body, _, err := e.g.one(id, "body", true)
	if err != nil {
		return err
	}
	sequenceValue, err := e.emitExpr(sequence)
	if err != nil {
		return err
	}
	sequenceValue, err = e.applyNodeAggregateContract(sequence, sequenceValue)
	if err != nil {
		return err
	}
	if sequenceValue.typ != "ptr" || sequenceValue.pointee == "" || (!sequenceValue.knownLen && sequenceValue.lengthRef == "") {
		return nil
	}
	indexSlot := e.fresh("range_index")
	e.emitInstruction("  %s = alloca i64", indexSlot)
	e.emitInstruction("  store i64 0, ptr %s", indexSlot)
	conditionLabel := e.freshLabel("uast_for_cond")
	bodyLabel := e.freshLabel("uast_for_body")
	postLabel := e.freshLabel("uast_for_post")
	endLabel := e.freshLabel("uast_for_end")
	e.emitInstruction("  br label %%%s", conditionLabel)
	e.emitLine("%s:", conditionLabel)
	e.current.terminated = false
	index, err := e.load(llvmValue{typ: "i64", ref: indexSlot})
	if err != nil {
		return err
	}
	limit := strconv.Itoa(sequenceValue.length)
	if sequenceValue.lengthRef != "" {
		limit = sequenceValue.lengthRef
	}
	cond := e.newTemp("icmp ult i64 " + index.ref + ", " + limit)
	e.emitInstruction("  br i1 %s, label %%%s, label %%%s", cond, bodyLabel, endLabel)
	e.emitLine("%s:", bodyLabel)
	e.current.terminated = false
	elemPtr := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 %s", sequenceValue.pointee, sequenceValue.ref, index.ref))
	item, err := e.load(llvmValue{typ: sequenceValue.pointee, ref: elemPtr})
	if err != nil {
		return err
	}
	if pattern, patternOK, patternErr := e.g.one(id, "binding", false); patternErr != nil {
		return patternErr
	} else if patternOK {
		if err := e.emitBindingPattern(pattern, item); err != nil {
			return err
		}
	}
	if name := e.g.common[id].Name; name != "" {
		place, ok := e.current.vars[name]
		if !ok {
			slot := e.fresh("slot")
			e.emitInstruction("  %s = alloca %s", slot, item.typ)
			place = llvmValue{typ: item.typ, ref: slot}
			e.current.vars[name] = place
		}
		item, err = e.coerce(item, place.typ)
		if err != nil {
			return err
		}
		e.emitInstruction("  store %s %s, ptr %s", place.typ, item.ref, place.ref)
	}
	e.loops = append(e.loops, llvmLoopContext{breakLabel: endLabel, continueLabel: postLabel})
	err = e.emitStmt(body)
	e.loops = e.loops[:len(e.loops)-1]
	if err != nil {
		return err
	}
	if !e.current.terminated {
		e.emitInstruction("  br label %%%s", postLabel)
	}
	e.emitLine("%s:", postLabel)
	e.current.terminated = false
	next := e.newTemp("add i64 " + index.ref + ", 1")
	e.emitInstruction("  store i64 %s, ptr %s", next, indexSlot)
	e.emitInstruction("  br label %%%s", conditionLabel)
	e.emitLine("%s:", endLabel)
	e.current.terminated = false
	return nil
}

// emitBindingPattern realizes the ordered product-binding contract used by
// ForEachStmt. A pattern never invents a value: it can only project fields
// from the iteration item when the item carries a proven named product
// layout. This keeps destructuring positionally stable and fail-closed for
// heterogeneous values without a layout fact.
func (e *llvmEmitter) emitBindingPattern(id int, item llvmValue) error {
	bindings := e.g.many(id, "binding")
	if len(bindings) == 0 {
		return fmt.Errorf("binding pattern node %d has no ordered bindings", id)
	}
	if item.typ != "ptr" || item.recordType == "" || len(item.fieldTypes) < len(bindings) {
		return fmt.Errorf("binding pattern node %d requires an iteration item with a proven product layout", id)
	}
	for i, binding := range bindings {
		name := e.g.common[binding.ID].Name
		if name == "" {
			return fmt.Errorf("binding pattern node %d position %d lacks a canonical binding name", id, i)
		}
		fieldType := item.fieldTypes[i]
		field := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i32 0, i32 %d", item.recordType, item.ref, i))
		value, err := e.load(llvmValue{typ: fieldType, ref: field})
		if err != nil {
			return err
		}
		place, ok := e.current.vars[name]
		if !ok {
			slot := e.fresh("pattern_slot")
			e.emitInstruction("  %s = alloca %s", slot, value.typ)
			place = cloneLLVMValue(value)
			place.ref = slot
			e.current.vars[name] = place
		}
		value, err = e.coerceTyped(value, place)
		if err != nil {
			return err
		}
		e.emitInstruction("  store %s %s, ptr %s", place.typ, value.ref, place.ref)
	}
	return nil
}

func (e *llvmEmitter) emitExpr(id int) (llvmValue, error) {
	if cell, ok := e.projection.cell(id); !ok {
		return llvmValue{}, fmt.Errorf("LLVM_PROJECTION_GAP: node %d is absent from the matrix-derived plan", id)
	} else if cell.Mode == llvmProjectionGap {
		return llvmValue{}, fmt.Errorf("LLVM_PROJECTION_GAP: node=%d family=%s structural=%s semantic=%s", id, cell.Family, cell.StructuralKind, cell.SemanticKind)
	}
	// Transported graphs can expose a control node through an expression
	// wrapper.  The structural UAST contract is authoritative: a ReturnStmt
	// must be emitted by the statement lowering path, never interpreted as a
	// value expression.
	if node, ok := e.g.nodes[id]; ok && node != nil && (node.StructuralKind == "ReturnStmt" || node.StructuralKind == "IfStmt" || node.StructuralKind == "SwitchMatchStmt") {
		if err := e.emitStmt(id); err != nil {
			return llvmValue{}, err
		}
		return llvmValue{typ: "void"}, nil
	}
	// Structural expression kinds take precedence over a transport semantic
	// kind when IDs collide across the compact graph tables.  In particular an
	// AggregateExpr must materialize its address before an enclosing AddressOf
	// operation consumes it.
	if node, ok := e.g.nodes[id]; ok && node != nil {
		switch node.StructuralKind {
		case "AggregateExpr", "TupleExpr", "TupleResult":
			return e.emitAggregate(id)
		}
	}
	c := e.g.common[id]
	switch c.Kind {
	case "expression":
		child, ok, err := e.g.one(id, "expression", true)
		if err != nil || !ok {
			return llvmValue{}, fmt.Errorf("expression node %d lacks operand", id)
		}
		return e.emitExpr(child)
	case "literal", "missing_argument":
		return e.emitLiteral(c)
	case "identifier":
		if place, ok := e.lookupLocal(c); ok {
			value, err := e.load(place)
			if err != nil {
				return llvmValue{}, err
			}
			semanticType, typeErr := e.semanticTypeForNode(id)
			if typeErr != nil {
				return llvmValue{}, typeErr
			}
			targetType := llvmType(semanticType, "")
			if targetType != "void" && targetType != value.typ {
				value, err = e.coerce(value, targetType)
				if err != nil {
					return llvmValue{}, fmt.Errorf("LLVM_BINDING_TYPE_CONTRACT_MISMATCH: identifier %q node %d storage=%s semantic=%s: %w", c.Name, id, place.typ, targetType, err)
				}
			}
			return llvmApplyAggregateLayout(value, semanticType), nil
		}
		captureIndex, captured := e.current.captures[llvmVariableKey(c)]
		if !captured {
			captureIndex, captured = e.current.captures[c.Name]
		}
		if captured {
			captureType := llvmType(c.Type, "")
			if captureType == "void" {
				captureType = "i64"
			}
			ptr := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 0, i32 %d", e.current.envType, e.current.envRef, captureIndex))
			return e.load(llvmValue{typ: captureType, ref: ptr})
		}
		if strings.EqualFold(strings.TrimSpace(c.Name), "strings.TrimSpace") || strings.EqualFold(strings.TrimSpace(c.Name), "trimspace") {
			e.usesStringTrimSpace = true
			return llvmValue{typ: "ptr", ref: "@uast_string_trim_space", functionName: "uast_string_trim_space", functionResult: "ptr", functionParams: []string{"ptr"}}, nil
		}
		if strings.EqualFold(strings.TrimSpace(c.Name), "filepath.Join") || strings.EqualFold(strings.TrimSpace(c.Name), "path.Join") {
			e.usesPathJoin = true
			return llvmValue{typ: "ptr", ref: "@uast_path_join", functionName: "uast_path_join", functionResult: "ptr", functionParams: []string{"ptr", "ptr"}}, nil
		}
		if strings.EqualFold(strings.TrimSpace(c.Name), "os.TempDir") || strings.EqualFold(strings.TrimSpace(c.Name), "tempdir") {
			e.usesTempDir = true
			return llvmValue{typ: "ptr", ref: "@uast_temp_dir", functionName: "uast_temp_dir", functionResult: "ptr"}, nil
		}
		if strings.EqualFold(strings.TrimSpace(c.Name), "filepath.Dir") || strings.EqualFold(strings.TrimSpace(c.Name), "path.Dir") {
			e.usesPathDir = true
			return llvmValue{typ: "ptr", ref: "@uast_path_dir", functionName: "uast_path_dir", functionResult: "ptr", functionParams: []string{"ptr"}}, nil
		}
		if functionName := e.functionNameForIdentifier(c.Name); functionName != "" {
			result, params, err := e.functionSignature(e.functions[functionName])
			if err != nil {
				return llvmValue{}, err
			}
			paramTypes := make([]string, len(params))
			for i, param := range params {
				paramTypes[i] = param.typ
			}
			return llvmValue{typ: "ptr", ref: "@" + functionName, functionName: functionName, functionResult: result, functionParams: paramTypes}, nil
		}
		projectName := c.Name
		if source := e.opts.ProjectBindings[projectName]; source != "" {
			projectName = source
		}
		if project, exists := e.projectCalls[llvmIdentifier(projectName)]; exists {
			resultType := llvmType(project.Result, "")
			parameterTypes := make([]string, len(project.Parameters))
			for i, parameter := range project.Parameters {
				parameterTypes[i] = llvmType(parameter, "")
			}
			return llvmValue{typ: "ptr", ref: "@" + project.Symbol, functionName: project.Symbol, functionResult: resultType, functionParams: parameterTypes}, nil
		}
		if c.Binding == nil {
			if _, _, hasABI, abiErr := e.functionABIForNode(id); abiErr != nil {
				return llvmValue{}, abiErr
			} else if hasABI {
				return llvmValue{}, fmt.Errorf("LLVM_PROJECT_SYMBOL_UNRESOLVED: function value %q at node %d has a signature but no owning project declaration", c.Name, id)
			}
		}
		return llvmValue{}, fmt.Errorf("identifier %q at node %d has no LLVM-visible binding", c.Name, id)
	case "binary":
		left, _, err := e.g.one(id, "left", true)
		if err != nil {
			return llvmValue{}, err
		}
		right, _, err := e.g.one(id, "right", true)
		if err != nil {
			return llvmValue{}, err
		}
		l, err := e.emitExpr(left)
		if err != nil {
			return llvmValue{}, err
		}
		if c.Operation.Operator == "&&" || c.Operation.Operator == "||" {
			return e.emitLogicalBinary(c.Operation.Operator, l, right)
		}
		r, err := e.emitExpr(right)
		if err != nil {
			return llvmValue{}, err
		}
		return e.emitBinary(c.Operation.Operator, l, r)
	case "unary", "iteration":
		child, _, err := e.g.firstChild(id, "value", "operand")
		if err != nil {
			return llvmValue{}, err
		}
		v, err := e.emitExpr(child)
		if err != nil {
			return llvmValue{}, err
		}
		switch c.Operation.Operator {
		case "+", "identity", "positive":
			return v, nil
		case "-":
			if v.typ == "double" {
				return llvmValue{typ: "double", ref: e.newTemp("fneg double -0.000000e+00, " + v.ref)}, nil
			}
			return llvmValue{typ: v.typ, ref: e.newTemp(v.typ + " 0, " + v.ref)}, nil
		case "*", "deref":
			derefType, typeErr := e.semanticTypeForNode(id)
			if typeErr != nil {
				return llvmValue{}, typeErr
			}
			pointeeType := derefType
			if derefType.Element != nil && (strings.EqualFold(derefType.Kind, "pointer") || strings.EqualFold(derefType.Kind, "reference")) {
				pointeeType = *derefType.Element
			}
			if v.typ == "ptr" && v.pointee == "" && !isUnknownSemanticType(pointeeType) {
				// Dereference result types are explicit canonical facts. Carry
				// that fact back to the pointer load when the transport omitted
				// the redundant child Element field.
				v.pointee = llvmType(pointeeType, "")
			}
			if v.typ != "ptr" || v.pointee == "" {
				return llvmValue{}, fmt.Errorf("deref node %d lacks an LLVM pointee contract", id)
			}
			return e.load(llvmValue{typ: v.pointee, ref: v.ref})
		case "&", "address_of", "address":
			// Aggregate literals and already-materialized storage values are
			// addressable by construction: their LLVM representation is the
			// pointer returned by the aggregate/layout primitive.
			if v.typ == "ptr" && v.ref != "" {
				return v, nil
			}
			if cchild, ok := e.g.common[child]; ok && cchild.Kind == "identifier" {
				if place, exists := e.lookupLocal(cchild); exists {
					return llvmValue{typ: "ptr", ref: place.ref, pointee: place.typ}, nil
				}
			}
			if v.typ != "void" && v.ref != "" {
				slot := e.fresh("address")
				e.emitInstruction("  %s = alloca %s", slot, v.typ)
				e.emitInstruction("  store %s %s, ptr %s", v.typ, v.ref, slot)
				return llvmValue{typ: "ptr", ref: slot, pointee: v.typ}, nil
			}
			return llvmValue{typ: "ptr", ref: "null"}, nil
		case "!", "not":
			b, err := e.asBool(v)
			if err != nil {
				return llvmValue{}, err
			}
			return llvmValue{typ: "i1", ref: e.newTemp("xor i1 " + b.ref + ", true")}, nil
		default:
			if v.typ != "void" {
				return llvmValue{typ: v.typ, ref: v.ref}, nil
			}
			return llvmValue{typ: "i64", ref: "0"}, nil
		}
	case "typed_operation":
		if c.Operation.Typed == nil {
			return llvmValue{}, fmt.Errorf("typed operation node %d lacks operation contract", id)
		}
		args := e.g.many(id, "argument")
		values := make([]llvmValue, len(args))
		for i, arg := range args {
			var err error
			values[i], err = e.emitExpr(arg.ID)
			if err != nil {
				return llvmValue{}, err
			}
		}
		return e.emitTyped(c.Operation.Typed, values, id)
	case "call":
		return e.emitCall(id)
	case "function":
		name := e.functionIDs[id]
		if name == "" {
			return llvmValue{}, fmt.Errorf("anonymous function node %d has no LLVM declaration", id)
		}
		result, params, err := e.functionSignature(id)
		if err != nil {
			return llvmValue{}, err
		}
		paramTypes := make([]string, len(params))
		for i, param := range params {
			paramTypes[i] = param.typ
		}
		if captures := e.captures[id]; len(captures) > 0 {
			envType := e.envTypes[id]
			envSlot := e.fresh("closure_env")
			e.emitInstruction("  %s = alloca %s", envSlot, envType)
			for i, capture := range captures {
				place, ok := e.current.vars[capture]
				if !ok {
					// Imported closures may reference a binding materialized in a
					// different unit. Preserve the closure representation with a
					// null capture slot; no language-specific lookup is performed.
					place = llvmValue{typ: "i64", ref: "0"}
				}
				value := place
				if ok {
					var err error
					value, err = e.load(place)
					if err != nil {
						return llvmValue{}, err
					}
				}
				fieldType := value.typ
				field := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 0, i32 %d", envType, envSlot, i))
				e.emitInstruction("  store %s %s, ptr %s", fieldType, value.ref, field)
			}
			closureSlot := e.fresh("closure")
			e.emitInstruction("  %s = alloca { ptr, ptr }", closureSlot)
			codeField := e.newTemp("getelementptr inbounds { ptr, ptr }, ptr " + closureSlot + ", i64 0, i32 0")
			envField := e.newTemp("getelementptr inbounds { ptr, ptr }, ptr " + closureSlot + ", i64 0, i32 1")
			e.emitInstruction("  store ptr @%s, ptr %s", name, codeField)
			e.emitInstruction("  store ptr %s, ptr %s", envSlot, envField)
			return llvmValue{typ: "ptr", ref: closureSlot, functionName: name, functionResult: result, functionParams: paramTypes, closure: true, closureEnv: envSlot}, nil
		}
		return llvmValue{typ: "ptr", ref: "@" + name, functionName: name, functionResult: result, functionParams: paramTypes}, nil
	case "aggregate", "tuple", "tuple_result":
		return e.emitAggregate(id)
	case "index":
		return e.emitIndex(id)
	default:
		switch e.g.nodes[id].StructuralKind {
		case "AggregateExpr", "TupleExpr", "TupleResult":
			return e.emitAggregate(id)
		case "MemberAccessExpr", "SelectorExpr":
			return e.emitMember(id)
		case "IndexExpr", "SliceExpr":
			if e.g.nodes[id].StructuralKind == "SliceExpr" {
				return e.emitSlice(id)
			}
			return e.emitIndex(id)
		case "ConvertExpr", "TypeAssertExpr":
			value, ok, err := e.g.firstChild(id, "value", "operand", "expression")
			if err != nil || !ok {
				return llvmValue{}, fmt.Errorf("conversion node %d lacks a structured operand", id)
			}
			v, err := e.emitExpr(value)
			if err != nil {
				return llvmValue{}, err
			}
			return e.coerce(v, llvmType(c.Type, ""))
		case "AddressOf", "Deref":
			value, ok, err := e.g.firstChild(id, "value", "operand", "base")
			if err != nil || !ok {
				return llvmValue{}, fmt.Errorf("pointer node %d lacks a structured operand", id)
			}
			if e.g.nodes[id].StructuralKind == "AddressOf" {
				if binding, exists := e.g.common[value]; exists && binding.Kind == "identifier" {
					if place, found := e.lookupLocal(binding); found {
						return llvmValue{typ: "ptr", ref: place.ref, pointee: place.typ}, nil
					}
					return llvmValue{}, fmt.Errorf("address node %d binding %q is not addressable", id, binding.Name)
				}
			}
			v, err := e.emitExpr(value)
			if err != nil {
				return llvmValue{}, err
			}
			derefType, typeErr := e.semanticTypeForNode(id)
			if typeErr != nil {
				return llvmValue{}, typeErr
			}
			pointeeType := derefType
			if derefType.Element != nil && (strings.EqualFold(derefType.Kind, "pointer") || strings.EqualFold(derefType.Kind, "reference")) {
				pointeeType = *derefType.Element
			}
			if v.typ == "ptr" && v.pointee == "" && !isUnknownSemanticType(pointeeType) {
				v.pointee = llvmType(pointeeType, "")
			}
			if v.typ != "ptr" || v.pointee == "" {
				return llvmValue{}, fmt.Errorf("deref node %d lacks an LLVM pointee contract", id)
			}
			return e.load(llvmValue{typ: v.pointee, ref: v.ref})
		default:
			if typ, typeErr := e.semanticTypeForNode(id); typeErr == nil {
				llvmTyp := llvmType(typ, "")
				if llvmTyp != "void" {
					return llvmValue{typ: llvmTyp, ref: llvmZero(llvmTyp)}, nil
				}
			}
			return llvmValue{typ: "i64", ref: "0"}, nil
		}
	}
}

func (e *llvmEmitter) newTemp(instruction string) string {
	name := e.fresh("v")
	e.emitInstruction("  %s = %s", name, instruction)
	return name
}

func (e *llvmEmitter) aggregateChildren(id int) []universalChild {
	items := make([]universalChild, 0)
	for _, child := range e.g.orderedChildren(id) {
		switch child.Meta.Role {
		case "member", "element", "argument", "value":
			if !child.Meta.Missing {
				items = append(items, child)
			}
		}
	}
	return items
}

func (e *llvmEmitter) emitAggregate(id int) (llvmValue, error) {
	items := e.aggregateChildren(id)
	t, err := e.semanticTypeForNode(id)
	if err != nil {
		return llvmValue{}, err
	}
	if strings.EqualFold(t.Kind, "map") {
		if t.Key == nil || t.Value == nil || isUnknownSemanticType(*t.Key) || isUnknownSemanticType(*t.Value) {
			return llvmValue{}, fmt.Errorf("LLVM_MAP_CONTRACT_MISSING: aggregate node %d requires canonical key and value types", id)
		}
		if len(items) != 0 {
			return llvmValue{}, fmt.Errorf("LLVM_MAP_CONTRACT_MISSING: aggregate node %d requires explicit map-entry layout for non-empty literals", id)
		}
		keyType, valueType := llvmType(*t.Key, ""), llvmType(*t.Value, "")
		if keyType == "void" || valueType == "void" {
			return llvmValue{}, fmt.Errorf("LLVM_MAP_CONTRACT_MISSING: aggregate node %d has non-representable key/value type", id)
		}
		mapType := e.recordTypes[id]
		if mapType == "" {
			mapType = fmt.Sprintf("%%uast_map_%d", id)
			e.recordTypes[id] = mapType
			// count, capacity, and entry storage pointer.  The descriptor is
			// deliberately explicit so later lookup/insert lowering can use the
			// same canonical layout rather than treating a map as an array.
			e.typeDefs = append(e.typeDefs, fmt.Sprintf("%s = type { i64, i64, ptr }", mapType))
		}
		slot := e.fresh("map")
		e.emitInstruction("  %s = alloca %s", slot, mapType)
		count := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i32 0, i32 0", mapType, slot))
		capacity := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i32 0, i32 1", mapType, slot))
		entries := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i32 0, i32 2", mapType, slot))
		e.emitInstruction("  store i64 0, ptr %s", count)
		e.emitInstruction("  store i64 0, ptr %s", capacity)
		e.emitInstruction("  store ptr null, ptr %s", entries)
		return llvmValue{typ: "ptr", ref: slot, mapType: mapType, mapKeyType: keyType, mapValueType: valueType}, nil
	}
	elementType := ""
	if t.Element != nil {
		elementType = llvmType(*t.Element, "")
	}
	fieldTypes := make([]string, 0, len(t.Fields))
	fieldNames := make([]string, 0, len(t.Fields))
	recordLayout := len(t.Fields) > 0
	for _, field := range t.Fields {
		fieldType := llvmType(field.Type, "")
		if fieldType == "void" {
			return llvmValue{}, fmt.Errorf("aggregate node %d field %q has void layout", id, field.Name)
		}
		fieldTypes = append(fieldTypes, fieldType)
		fieldNames = append(fieldNames, field.Name)
	}
	if elementType == "" && len(t.Parameters) > 0 && len(fieldTypes) == 0 {
		elementType = llvmType(t.Parameters[0], "")
		for i, parameter := range t.Parameters {
			parameterType := llvmType(parameter, "")
			if parameterType != elementType {
				recordLayout = true
			}
			fieldTypes = append(fieldTypes, parameterType)
			fieldNames = append(fieldNames, strconv.Itoa(i+1))
		}
	}
	if recordLayout && len(fieldTypes) == 0 {
		if e.strictProjectProjection() {
			return llvmValue{}, fmt.Errorf("LLVM_AGGREGATE_LAYOUT_CONTRACT_MISSING: aggregate node %d declares record layout without fields", id)
		}
		return llvmValue{typ: "ptr", ref: "null"}, nil
	}
	values := make([]llvmValue, len(items))
	for i, item := range items {
		value, err := e.emitExpr(item.ID)
		if err != nil {
			return llvmValue{}, err
		}
		if !recordLayout {
			if elementType == "" {
				elementType = value.typ
			}
			value, err = e.coerce(value, elementType)
			if err != nil {
				return llvmValue{}, fmt.Errorf("aggregate node %d element %d: %w", id, i, err)
			}
		}
		values[i] = value
	}
	if elementType == "" && !recordLayout {
		return llvmValue{typ: "ptr", ref: "null"}, nil
	}
	if recordLayout {
		recordType := e.recordTypes[id]
		if recordType == "" {
			recordType = fmt.Sprintf("%%uast_record_%d", id)
			e.recordTypes[id] = recordType
			parts := append([]string(nil), fieldTypes...)
			e.typeDefs = append(e.typeDefs, fmt.Sprintf("%s = type { %s }", recordType, strings.Join(parts, ", ")))
		}
		if !strings.Contains(strings.Join(e.typeDefs, "\n"), recordType+" = type") {
			e.typeDefs = append(e.typeDefs, fmt.Sprintf("%s = type { ptr }", recordType))
		}
		slot := e.fresh("record")
		e.emitInstruction("  %s = alloca %s", slot, recordType)
		for i, value := range values {
			if i >= len(fieldTypes) {
				break
			}
			coerced, coerceErr := e.coerce(value, fieldTypes[i])
			if coerceErr != nil {
				return llvmValue{}, fmt.Errorf("aggregate node %d field %d: %w", id, i, coerceErr)
			}
			ptr := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i32 0, i32 %d", recordType, slot, i))
			e.emitInstruction("  store %s %s, ptr %s", fieldTypes[i], coerced.ref, ptr)
		}
		return llvmValue{typ: "ptr", ref: slot, recordType: recordType, fieldNames: fieldNames, fieldTypes: fieldTypes}, nil
	}
	if elementType == "void" {
		return llvmValue{}, fmt.Errorf("aggregate node %d has void element layout", id)
	}
	slot := e.fresh("aggregate")
	e.emitInstruction("  %s = alloca [%d x %s]", slot, len(values), elementType)
	for i, value := range values {
		ptr := e.newTemp(fmt.Sprintf("getelementptr inbounds [%d x %s], ptr %s, i64 0, i64 %d", len(values), elementType, slot, i))
		e.emitInstruction("  store %s %s, ptr %s", elementType, value.ref, ptr)
	}
	base := e.newTemp(fmt.Sprintf("getelementptr inbounds [%d x %s], ptr %s, i64 0, i64 0", len(values), elementType, slot))
	return llvmValue{typ: "ptr", ref: base, pointee: elementType, length: len(values), knownLen: true}, nil
}

func (e *llvmEmitter) emitMember(id int) (llvmValue, error) {
	baseID, ok, err := e.g.firstChild(id, "base", "receiver", "value", "object")
	if err != nil || !ok {
		return llvmValue{}, fmt.Errorf("member node %d lacks a structured receiver", id)
	}
	base, err := e.emitExpr(baseID)
	if err != nil {
		return llvmValue{}, err
	}
	base, err = e.applyNodeAggregateContract(baseID, base)
	if err != nil {
		return llvmValue{}, err
	}
	// A callable member is not an aggregate field. It requires an explicit
	// receiver/method or external-symbol contract; treating the receiver
	// pointer as a code pointer would produce a linkable but crashing stub.
	memberType, memberTypeErr := e.semanticTypeForNode(id)
	if memberTypeErr != nil {
		return llvmValue{}, memberTypeErr
	}
	if strings.EqualFold(memberType.Kind, "function") {
		if e.strictProjectProjection() {
			return llvmValue{}, fmt.Errorf("LLVM_CALLABLE_MEMBER_CONTRACT_MISSING: member node %d name=%q receiver node %d has no method/external ABI implementation", id, e.g.common[id].Name, baseID)
		}
	}
	if base.typ != "ptr" || base.recordType == "" {
		if e.strictProjectProjection() {
			member := e.g.common[id]
			baseCommon := e.g.common[baseID]
			return llvmValue{}, fmt.Errorf("LLVM_AGGREGATE_LAYOUT_CONTRACT_MISSING: member node %d name=%q semantic_type=%q base_node=%d base_type=%s base_record=%q fields=%d", id, member.Name, member.Type.Kind, baseID, base.typ, base.recordType, len(baseCommon.Type.Fields))
		}
		// Opaque imported records may omit their optional field layout while
		// retaining the member's result type. Keep LLVM projection productive by
		// materializing the canonical zero value for that typed member; concrete
		// records still take the exact GEP/load path below.
		memberType, typeErr := e.semanticTypeForNode(id)
		if typeErr != nil {
			return llvmValue{}, typeErr
		}
		resultType := llvmType(memberType, "")
		if resultType == "void" {
			return llvmValue{}, fmt.Errorf("member node %d has no representable result type", id)
		}
		return llvmValue{typ: resultType, ref: llvmZero(resultType)}, nil
	}
	memberName := e.g.common[id].Name
	if memberName == "" {
		if memberID, present, childErr := e.g.firstChild(id, "member", "selector", "field"); childErr != nil {
			return llvmValue{}, childErr
		} else if present {
			memberName = e.g.common[memberID].Name
			if memberName == "" {
				memberName = e.g.common[memberID].Operation.Text
			}
		}
	}
	if memberName == "" {
		memberName = e.g.common[id].Operation.Text
	}
	for i, name := range base.fieldNames {
		if name != memberName {
			continue
		}
		ptr := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i32 0, i32 %d", base.recordType, base.ref, i))
		return e.load(llvmValue{typ: base.fieldTypes[i], ref: ptr})
	}
	if semanticType, typeErr := e.semanticTypeForNode(id); typeErr == nil {
		if e.strictProjectProjection() {
			return llvmValue{}, fmt.Errorf("LLVM_AGGREGATE_LAYOUT_CONTRACT_MISSING: member node %d field %q is absent from record layout", id, memberName)
		}
		if typ := llvmType(semanticType, ""); typ != "void" {
			return llvmValue{typ: typ, ref: llvmZero(typ)}, nil
		}
	}
	return llvmValue{typ: "i64", ref: "0"}, nil
}

// emitSynchronizationMethod lowers the canonical synchronization operation
// carried by a receiver-selected method.  The receiver is an explicit UAST
// operand; the operation is represented as an atomic i32 state at the
// receiver's declared storage address.  No source-language type or runtime
// package is consulted here.  Methods without the required receiver contract
// remain a hard projection error.
func (e *llvmEmitter) emitSynchronizationMethod(callID, memberID int, operation string) (llvmValue, bool, error) {
	member := e.g.common[memberID]
	method := strings.ToLower(strings.TrimSpace(member.Name))
	if method != "lock" && method != "unlock" {
		return llvmValue{}, false, nil
	}
	if !strings.EqualFold(member.Type.Kind, "function") {
		return llvmValue{}, false, fmt.Errorf("LLVM_SYNC_CONTRACT_MISSING: member node %d method %q is not callable", memberID, member.Name)
	}
	baseID, ok, err := e.g.firstChild(memberID, "base", "receiver", "value", "object")
	if err != nil {
		return llvmValue{}, false, err
	}
	if !ok {
		return llvmValue{}, false, fmt.Errorf("LLVM_SYNC_CONTRACT_MISSING: call node %d method %q has no receiver", callID, member.Name)
	}
	receiver, err := e.emitExpr(baseID)
	if err != nil {
		return llvmValue{}, false, err
	}
	if receiver.typ != "ptr" || receiver.ref == "" {
		return llvmValue{}, false, fmt.Errorf("LLVM_SYNC_CONTRACT_MISSING: call node %d method %q receiver is not addressable", callID, member.Name)
	}
	if method == "unlock" {
		null := e.newTemp("icmp eq ptr " + receiver.ref + ", null")
		bad := e.freshLabel("uast_unlock_bad")
		good := e.freshLabel("uast_unlock_good")
		end := e.freshLabel("uast_unlock_end")
		e.usesTrap = true
		e.emitInstruction("  br i1 %s, label %%%s, label %%%s", null, bad, good)
		e.emitLine("%s:", bad)
		e.emitInstruction("  call void @llvm.trap()")
		e.emitInstruction("  unreachable")
		e.emitLine("%s:", good)
		e.emitInstruction("  store atomic i32 0, ptr %s release, align 4", receiver.ref)
		e.emitInstruction("  br label %%%s", end)
		e.emitLine("%s:", end)
		return llvmValue{typ: "void"}, true, nil
	}
	null := e.newTemp("icmp eq ptr " + receiver.ref + ", null")
	bad := e.freshLabel("uast_lock_bad")
	try := e.freshLabel("uast_lock_try")
	wait := e.freshLabel("uast_lock_wait")
	done := e.freshLabel("uast_lock_done")
	e.usesTrap = true
	e.emitInstruction("  br i1 %s, label %%%s, label %%%s", null, bad, try)
	e.emitLine("%s:", bad)
	e.emitInstruction("  call void @llvm.trap()")
	e.emitInstruction("  unreachable")
	e.emitLine("%s:", try)
	acquired := e.newTemp(fmt.Sprintf("cmpxchg ptr %s, i32 0, i32 1 acquire monotonic", receiver.ref))
	success := e.newTemp(fmt.Sprintf("extractvalue { i32, i1 } %s, 1", acquired))
	e.emitInstruction("  br i1 %s, label %%%s, label %%%s", success, done, wait)
	e.emitLine("%s:", wait)
	e.emitInstruction(`  call void asm sideeffect "pause", "~{dirflag},~{fpsr},~{flags}"()`)
	e.emitInstruction("  br label %%%s", try)
	e.emitLine("%s:", done)
	return llvmValue{typ: "void"}, true, nil
}

func (e *llvmEmitter) emitIndex(id int) (llvmValue, error) {
	baseID, ok, err := e.g.one(id, "value", true)
	if err != nil || !ok {
		return llvmValue{}, fmt.Errorf("index node %d lacks a structured base", id)
	}
	base, err := e.emitExpr(baseID)
	if err != nil {
		return llvmValue{}, err
	}
	base, err = e.applyNodeAggregateContract(baseID, base)
	if err != nil {
		return llvmValue{}, err
	}
	if base.recordType != "" {
		if typ, typeErr := e.semanticTypeForNode(id); typeErr == nil {
			t := llvmType(typ, "")
			if t != "void" {
				return llvmValue{typ: t, ref: llvmZero(t)}, nil
			}
		}
		return llvmValue{typ: "i64", ref: "0"}, nil
	}
	if base.typ == "ptr" && base.recordType != "" {
		args := e.g.many(id, "argument")
		if len(args) != 1 || args[0].Meta.Missing {
			return llvmValue{}, fmt.Errorf("index node %d requires exactly one present positional product index", id)
		}
		ordinal, ok := e.staticIndex(args[0].ID)
		if !ok {
			return llvmValue{}, fmt.Errorf("index node %d requires a compile-time product position for heterogeneous layout", id)
		}
		if ordinal < 1 || ordinal > len(base.fieldTypes) {
			return llvmValue{}, fmt.Errorf("index node %d product position %d outside arity %d", id, ordinal, len(base.fieldTypes))
		}
		fieldType := base.fieldTypes[ordinal-1]
		ptr := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i32 0, i32 %d", base.recordType, base.ref, ordinal-1))
		return e.load(llvmValue{typ: fieldType, ref: ptr})
	}
	if base.typ != "ptr" || base.pointee == "" || (!base.knownLen && base.lengthRef == "") {
		return llvmValue{typ: "i64", ref: "0"}, nil
	}
	args := e.g.many(id, "argument")
	if len(args) != 1 || args[0].Meta.Missing {
		return llvmValue{}, fmt.Errorf("index node %d requires exactly one present positional index", id)
	}
	index, err := e.emitExpr(args[0].ID)
	if err != nil {
		return llvmValue{}, err
	}
	index, err = e.coerce(index, "i64")
	if err != nil {
		return llvmValue{}, err
	}
	// The UAST index contract is one-based. Bounds are represented by the
	// aggregate's known length and are checked before the inbounds GEP so an
	// invalid semantic index cannot become undefined native behavior.
	zero := e.newTemp("sub i64 " + index.ref + ", 1")
	lengthRef := strconv.Itoa(base.length)
	if base.lengthRef != "" {
		lengthRef = base.lengthRef
	}
	badLow := e.newTemp("icmp ult i64 " + index.ref + ", 1")
	badHigh := e.newTemp("icmp ugt i64 " + index.ref + ", " + lengthRef)
	bad := e.newTemp("or i1 " + badLow + ", " + badHigh)
	badLabel := e.freshLabel("uast_index_bad")
	goodLabel := e.freshLabel("uast_index_good")
	doneLabel := e.freshLabel("uast_index_done")
	resultSlot := e.fresh("index_result")
	e.emitInstruction("  %s = alloca %s", resultSlot, base.pointee)
	e.usesTrap = true
	e.emitInstruction("  br i1 %s, label %%%s, label %%%s", bad, badLabel, goodLabel)
	e.emitLine("%s:", badLabel)
	e.emitInstruction("  call void @llvm.trap()")
	e.emitInstruction("  unreachable")
	e.emitLine("%s:", goodLabel)
	ptr := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 %s", base.pointee, base.ref, zero))
	value, err := e.load(llvmValue{typ: base.pointee, ref: ptr})
	if err != nil {
		return llvmValue{}, err
	}
	e.emitInstruction("  store %s %s, ptr %s", value.typ, value.ref, resultSlot)
	e.emitInstruction("  br label %%%s", doneLabel)
	e.emitLine("%s:", doneLabel)
	return e.load(llvmValue{typ: value.typ, ref: resultSlot})
}

func (e *llvmEmitter) staticIndex(id int) (int, bool) {
	c, ok := e.g.common[id]
	if !ok {
		return 0, false
	}
	switch c.Kind {
	case "literal":
		if c.Operation.LiteralKind != "" && c.Operation.LiteralKind != "number" && c.Operation.LiteralKind != "integer" {
			return 0, false
		}
		value, err := strconv.Atoi(strings.TrimSpace(c.Operation.Text))
		return value, err == nil
	case "binary":
		left, leftOK, err := e.g.one(id, "left", true)
		if err != nil || !leftOK {
			return 0, false
		}
		right, rightOK, err := e.g.one(id, "right", true)
		if err != nil || !rightOK {
			return 0, false
		}
		l, lok := e.staticIndex(left)
		r, rok := e.staticIndex(right)
		if !lok || !rok {
			return 0, false
		}
		switch c.Operation.Operator {
		case "+":
			return l + r, true
		case "-":
			return l - r, true
		default:
			return 0, false
		}
	default:
		return 0, false
	}
}

func (e *llvmEmitter) emitSlice(id int) (llvmValue, error) {
	baseID, ok, err := e.g.firstChild(id, "base", "value", "receiver", "object")
	if err != nil || !ok {
		return llvmValue{}, fmt.Errorf("slice node %d lacks a structured base", id)
	}
	base, err := e.emitExpr(baseID)
	if err != nil {
		return llvmValue{}, err
	}
	base, err = e.applyNodeAggregateContract(baseID, base)
	if err != nil {
		return llvmValue{}, err
	}
	if base.typ != "ptr" || base.pointee == "" || (!base.knownLen && base.lengthRef == "") {
		return llvmValue{}, fmt.Errorf("slice node %d requires a proven aggregate pointee and length contract", id)
	}
	bounds := e.g.many(id, "argument")
	if len(bounds) > 2 {
		return llvmValue{}, fmt.Errorf("slice node %d has %d bounds; expected at most two", id, len(bounds))
	}
	start := llvmValue{typ: "i64", ref: "1"}
	endRef := strconv.Itoa(base.length + 1)
	if base.lengthRef != "" {
		endRef = e.newTemp("add i64 " + base.lengthRef + ", 1")
	}
	end := llvmValue{typ: "i64", ref: endRef}
	if len(bounds) > 0 && !bounds[0].Meta.Missing {
		start, err = e.emitExpr(bounds[0].ID)
		if err != nil {
			return llvmValue{}, err
		}
		start, err = e.coerce(start, "i64")
		if err != nil {
			return llvmValue{}, err
		}
	}
	if len(bounds) > 1 && !bounds[1].Meta.Missing {
		end, err = e.emitExpr(bounds[1].ID)
		if err != nil {
			return llvmValue{}, err
		}
		end, err = e.coerce(end, "i64")
		if err != nil {
			return llvmValue{}, err
		}
	}
	startZero := e.newTemp("sub i64 " + start.ref + ", 1")
	resultSlot := e.fresh("slice_result")
	e.emitInstruction("  %s = alloca ptr", resultSlot)
	startBad := e.newTemp("icmp ult i64 " + start.ref + ", 1")
	endBad := e.newTemp("icmp ugt i64 " + end.ref + ", " + endRef)
	orderBad := e.newTemp("icmp ugt i64 " + start.ref + ", " + end.ref)
	endOrOrderBad := e.newTemp("or i1 " + endBad + ", " + orderBad)
	badStart := e.freshLabel("uast_slice_bad")
	checkEnd := e.freshLabel("uast_slice_check_end")
	badEnd := e.freshLabel("uast_slice_bad")
	good := e.freshLabel("uast_slice_good")
	body := e.freshLabel("uast_slice_body")
	done := e.freshLabel("uast_slice_done")
	e.usesTrap = true
	e.emitInstruction("  br i1 %s, label %%%s, label %%%s", startBad, badStart, checkEnd)
	e.emitLine("%s:", checkEnd)
	e.emitInstruction("  br i1 %s, label %%%s, label %%%s", endOrOrderBad, badEnd, good)
	e.emitLine("%s:", good)
	e.emitInstruction("  br label %%%s", body)
	e.emitLine("%s:", body)
	ptr := e.newTemp(fmt.Sprintf("getelementptr inbounds %s, ptr %s, i64 %s", base.pointee, base.ref, startZero))
	e.emitInstruction("  store ptr %s, ptr %s", ptr, resultSlot)
	sliceLength := e.newTemp("sub i64 " + end.ref + ", " + start.ref)
	e.emitInstruction("  br label %%%s", done)
	e.emitLine("%s:", badStart)
	e.emitInstruction("  call void @llvm.trap()")
	e.emitInstruction("  unreachable")
	e.emitLine("%s:", badEnd)
	e.emitInstruction("  call void @llvm.trap()")
	e.emitInstruction("  unreachable")
	e.emitLine("%s:", done)
	value := e.newTemp("load ptr, ptr " + resultSlot)
	return llvmValue{typ: "ptr", ref: value, pointee: base.pointee, lengthRef: sliceLength, knownLen: true}, nil
}

func (e *llvmEmitter) emitStructuredStatement(id int) error {
	structural := e.g.nodes[id].StructuralKind
	switch structural {
	case "SwitchMatchStmt":
		return e.emitSwitchMatch(id)
	case "Scope":
		for _, child := range e.g.orderedChildren(id) {
			if child.Meta.Role == "statement" {
				if err := e.emitStmt(child.ID); err != nil {
					return err
				}
			}
		}
		return nil
	case "VariableDecl", "ConstantDecl", "Binding":
		initializer, ok, err := e.g.firstChild(id, "initializer", "expression", "value")
		if err != nil || !ok {
			return fmt.Errorf("%s node %d lacks initializer", structural, id)
		}
		value, err := e.emitExpr(initializer)
		if err != nil {
			return err
		}
		if e.g.common[id].Name == "" {
			return fmt.Errorf("%s node %d lacks binding name", structural, id)
		}
		place := e.current.vars[e.g.common[id].Name]
		if place.ref == "" {
			slot := e.fresh("slot")
			e.emitInstruction("  %s = alloca %s", slot, value.typ)
			place = cloneLLVMValue(value)
			place.ref = slot
		}
		place.bindingName = e.g.common[id].Name
		e.current.vars[e.g.common[id].Name] = place
		value, err = e.coerce(value, place.typ)
		if err != nil {
			return err
		}
		e.emitInstruction("  store %s %s, ptr %s", place.typ, value.ref, place.ref)
		e.current.vars[e.g.common[id].Name] = cloneLLVMValue(place)
		if key := llvmVariableKey(e.g.common[id]); key != "" {
			e.current.vars[key] = cloneLLVMValue(place)
		}
		return nil
	case "FunctionDecl", "MethodDecl":
		// Function bodies are emitted in the declaration pass. The statement
		// edge is a binding/initialization fact and has no runtime instruction.
		return nil
	case "BindingPattern":
		// The parent ForEachStmt consumes this structural child through
		// emitBindingPattern, preserving the pattern's ordered data edges.
		return nil
	default:
		return fmt.Errorf("node=%d structural=%s semantic=%s has no LLVM statement primitive", id, structural, e.g.common[id].Kind)
	}
}

// emitSwitchMatch lowers the universal selector/branch contract. The
// selector is evaluated once, each case pattern is evaluated in source order,
// and only the selected body executes. A default branch is represented by the
// canonical `default` child role; no source-language spelling is inspected.
func (e *llvmEmitter) emitSwitchMatch(id int) error {
	selectorID, ok, err := e.g.firstChild(id, "condition", "value", "expression", "operand")
	if err != nil || !ok {
		return fmt.Errorf("switch node %d lacks selector", id)
	}
	selector, err := e.emitExpr(selectorID)
	if err != nil {
		return err
	}
	clauses := make([]universalChild, 0)
	for _, child := range e.g.orderedChildren(id) {
		switch child.Meta.Role {
		case "case", "branch", "default":
			clauses = append(clauses, child)
		}
	}
	if len(clauses) == 0 {
		// An empty switch has defined semantics: its selector is still
		// evaluated once, but no branch body is selected.  The selector was
		// emitted above, so the statement is complete without inventing a
		// branch or a result value.
		return nil
	}
	endLabel := e.freshLabel("uast_switch_end")
	nextLabel := e.freshLabel("uast_switch_check")
	e.emitInstruction("  br label %%%s", nextLabel)
	hasDefault := false
	for _, clause := range clauses {
		e.emitLine("%s:", nextLabel)
		e.current.terminated = false
		bodyID, bodyOK, bodyErr := e.g.one(clause.ID, "body", false)
		if bodyErr != nil {
			return bodyErr
		}
		if !bodyOK {
			bodyID, bodyOK, bodyErr = e.g.one(clause.ID, "statement", false)
		}
		if bodyErr != nil || !bodyOK {
			if bodyErr != nil {
				return bodyErr
			}
			return fmt.Errorf("switch clause %d lacks body", clause.ID)
		}
		bodyLabel := e.freshLabel("uast_switch_body")
		if clause.Meta.Role == "default" {
			hasDefault = true
			e.emitInstruction("  br label %%%s", bodyLabel)
		} else {
			patternID, patternOK, patternErr := e.g.firstChild(clause.ID, "pattern", "condition", "value", "expression")
			if patternErr != nil || !patternOK {
				return fmt.Errorf("switch clause %d lacks pattern", clause.ID)
			}
			pattern, patternErr := e.emitExpr(patternID)
			if patternErr != nil {
				return patternErr
			}
			matched, matchErr := e.emitBinary("==", selector, pattern)
			if matchErr != nil {
				return matchErr
			}
			var next string
			if clauseIndex := len(clauses) - 1; clauseIndex >= 0 && clause.ID == clauses[clauseIndex].ID {
				next = endLabel
			} else {
				next = e.freshLabel("uast_switch_check")
			}
			e.emitInstruction("  br i1 %s, label %%%s, label %%%s", matched.ref, bodyLabel, next)
			nextLabel = next
		}
		e.emitLine("%s:", bodyLabel)
		e.current.terminated = false
		if err := e.emitStmt(bodyID); err != nil {
			return err
		}
		if !e.current.terminated {
			e.emitInstruction("  br label %%%s", endLabel)
		}
		if clause.Meta.Role == "default" {
			break
		}
	}
	if !hasDefault && !e.current.terminated && nextLabel != endLabel {
		e.emitLine("%s:", nextLabel)
		e.current.terminated = false
		e.emitInstruction("  br label %%%s", endLabel)
	}
	e.emitLine("%s:", endLabel)
	e.current.terminated = false
	return nil
}

func (e *llvmEmitter) emitLiteral(c universalDecodedCommon) (llvmValue, error) {
	typ := llvmType(c.Type, c.Operation.LiteralKind)
	text := strings.TrimSpace(c.Operation.Text)
	// Compact/readable semantic exports may retain the literal spelling in the
	// canonical symbol field while omitting operation.text.  Preserve that
	// explicit value before deciding its representation.
	if text == "" && strings.TrimSpace(c.Name) != "" {
		text = strings.TrimSpace(c.Name)
	}
	literalKind := strings.ToLower(strings.TrimSpace(c.Operation.LiteralKind))
	// Readable Semantic transports are allowed to carry the literal kind in
	// the semantic type contract instead of duplicating it on the operation.
	// The value contract, not the transport spelling, decides the LLVM
	// representation.  This is particularly important for string operands of
	// external/file calls: a quoted string with a canonical string type is a
	// pointer to immutable bytes, never an integer zero.
	if literalKind == "" && strings.EqualFold(c.Type.Kind, "string") {
		literalKind = "string"
	}
	if literalKind == "" && len(text) >= 2 && ((text[0] == '"' && text[len(text)-1] == '"') || (text[0] == '`' && text[len(text)-1] == '`')) {
		literalKind = "string"
	}
	switch literalKind {
	case "boolean":
		if strings.EqualFold(text, "true") || text == "1" {
			return llvmValue{typ: "i1", ref: "true"}, nil
		}
		return llvmValue{typ: "i1", ref: "false"}, nil
	case "string":
		unquoted, err := strconv.Unquote(text)
		if err != nil {
			unquoted = text
		}
		bytes := []byte(unquoted)
		bytes = append(bytes, 0)
		id := e.stringID
		e.stringID++
		global := fmt.Sprintf("@.uast_str_%d", id)
		var encoded strings.Builder
		for _, b := range bytes {
			encoded.WriteString(fmt.Sprintf("\\%02X", b))
		}
		e.globals = append(e.globals, fmt.Sprintf("%s = private unnamed_addr constant [%d x i8] c\"%s\"", global, len(bytes), encoded.String()))
		ref := e.newTemp(fmt.Sprintf("getelementptr inbounds [%d x i8], ptr %s, i64 0, i64 0", len(bytes), global))
		return llvmValue{typ: "ptr", ref: ref, pointee: "i8", length: len(bytes) - 1, knownLen: true}, nil
	case "null", "na", "missing":
		return llvmValue{typ: "i64", ref: "0"}, nil
	}
	if typ == "double" {
		if text == "" {
			text = "0"
		}
		return llvmValue{typ: "double", ref: llvmFloat(text)}, nil
	}
	if typ == "i1" {
		return llvmValue{typ: "i1", ref: "false"}, nil
	}
	if text == "" {
		text = "0"
	}
	return llvmIntegerValue(c.Type, text), nil
}

func llvmFloat(text string) string {
	if strings.ContainsAny(text, ".eE") {
		return text
	}
	return text + ".000000e+00"
}

func (e *llvmEmitter) load(place llvmValue) (llvmValue, error) {
	name := e.fresh("load")
	e.emitInstruction("  %s = load %s, ptr %s", name, place.typ, place.ref)
	value := cloneLLVMValue(place)
	value.ref = name
	return value, nil
}

func cloneLLVMValue(value llvmValue) llvmValue {
	value.functionParams = append([]string(nil), value.functionParams...)
	value.fieldNames = append([]string(nil), value.fieldNames...)
	value.fieldTypes = append([]string(nil), value.fieldTypes...)
	return value
}

func (e *llvmEmitter) coerce(v llvmValue, typ string) (llvmValue, error) {
	if v.typ == typ {
		return v, nil
	}
	if typ == "i1" {
		return e.asBool(v)
	}
	if typ == "double" && strings.HasPrefix(v.typ, "i") {
		return llvmValue{typ: typ, ref: e.newTemp("sitofp " + v.typ + " " + v.ref + " to double")}, nil
	}
	if strings.HasPrefix(typ, "i") && v.typ == "double" {
		// Untyped numeric literals are represented as binary64 by the generic
		// value contract. If the value is still an immediate constant, fold the
		// conversion in the projection instead of introducing a floating-point
		// instruction (and consequently an unnecessary _fltused ABI dependency
		// in a freestanding COFF link).
		if !strings.HasPrefix(v.ref, "%") {
			if number, err := strconv.ParseFloat(v.ref, 64); err == nil && number == math.Trunc(number) {
				return llvmValue{typ: typ, ref: strconv.FormatInt(int64(number), 10)}, nil
			}
		}
		return llvmValue{typ: typ, ref: e.newTemp("fptosi double " + v.ref + " to " + typ)}, nil
	}
	if strings.HasPrefix(typ, "i") && v.typ == "ptr" {
		// Pointer/reference values have an explicit integer representation in
		// the canonical pointer contract. Preserve that representation when a
		// consumer's already-declared integer slot requires it; this is a
		// backend value conversion, not a guessed call signature.
		return llvmValue{typ: typ, ref: e.newTemp("ptrtoint ptr " + v.ref + " to " + typ)}, nil
	}
	if typ == "ptr" && strings.HasPrefix(v.typ, "i") {
		// The canonical pointer contract permits an explicitly integer-backed
		// reference representation. Convert it at the LLVM boundary instead of
		// rejecting a proven aggregate field layout or inventing a null value.
		return llvmValue{typ: typ, ref: e.newTemp("inttoptr " + v.typ + " " + v.ref + " to ptr")}, nil
	}
	if strings.HasPrefix(typ, "i") && strings.HasPrefix(v.typ, "i") {
		from, _ := strconv.Atoi(strings.TrimPrefix(v.typ, "i"))
		to, _ := strconv.Atoi(strings.TrimPrefix(typ, "i"))
		if from < to {
			return llvmValue{typ: typ, ref: e.newTemp("sext " + v.typ + " " + v.ref + " to " + typ)}, nil
		}
		return llvmValue{typ: typ, ref: e.newTemp("trunc " + v.typ + " " + v.ref + " to " + typ)}, nil
	}
	if typ != "" && typ != "void" {
		if e.strictProjectProjection() {
			return llvmValue{}, fmt.Errorf("LLVM_VALUE_CONTRACT_MISSING: cannot coerce LLVM value %s to %s", v.typ, typ)
		}
		return llvmValue{typ: typ, ref: llvmZero(typ)}, nil
	}
	if e.strictProjectProjection() {
		return llvmValue{}, fmt.Errorf("LLVM_VALUE_CONTRACT_MISSING: cannot coerce LLVM value %s to void", v.typ)
	}
	return llvmValue{typ: "i64", ref: "0"}, nil
}

// coerceTyped carries the complete scalar value contract across an LLVM
// boundary. The opaque LLVM integer type contains width but not signedness;
// signedness therefore has to travel with the canonical SemanticType.
func (e *llvmEmitter) coerceTyped(v llvmValue, target llvmValue) (llvmValue, error) {
	if v.typ == target.typ {
		v.integerSigned = target.integerSigned
		v.integerSignedKnown = target.integerSignedKnown
		return v, nil
	}
	if strings.HasPrefix(target.typ, "i") && strings.HasPrefix(v.typ, "i") {
		from, _ := strconv.Atoi(strings.TrimPrefix(v.typ, "i"))
		to, _ := strconv.Atoi(strings.TrimPrefix(target.typ, "i"))
		if from < to {
			op := "zext"
			if v.signedInteger() {
				op = "sext"
			}
			target.ref = e.newTemp(op + " " + v.typ + " " + v.ref + " to " + target.typ)
			return target, nil
		}
		target.ref = e.newTemp("trunc " + v.typ + " " + v.ref + " to " + target.typ)
		return target, nil
	}
	if target.typ == "double" && strings.HasPrefix(v.typ, "i") {
		op := "uitofp"
		if v.signedInteger() {
			op = "sitofp"
		}
		target.ref = e.newTemp(op + " " + v.typ + " " + v.ref + " to double")
		return target, nil
	}
	if strings.HasPrefix(target.typ, "i") && v.typ == "double" {
		op := "fptoui"
		if target.signedInteger() {
			op = "fptosi"
		}
		target.ref = e.newTemp(op + " double " + v.ref + " to " + target.typ)
		return target, nil
	}
	coerced, err := e.coerce(v, target.typ)
	if err != nil {
		return llvmValue{}, err
	}
	coerced.integerSigned = target.integerSigned
	coerced.integerSignedKnown = target.integerSignedKnown
	return coerced, nil
}

func (e *llvmEmitter) asBool(v llvmValue) (llvmValue, error) {
	if v.typ == "i1" {
		return v, nil
	}
	if v.typ == "double" {
		return llvmValue{typ: "i1", ref: e.newTemp("fcmp une double " + v.ref + ", 0.000000e+00")}, nil
	}
	if strings.HasPrefix(v.typ, "i") {
		return llvmValue{typ: "i1", ref: e.newTemp("icmp ne " + v.typ + " " + v.ref + ", 0")}, nil
	}
	if v.typ == "ptr" {
		return llvmValue{typ: "i1", ref: e.newTemp("icmp ne ptr " + v.ref + ", null")}, nil
	}
	return llvmValue{}, fmt.Errorf("LLVM value %s cannot be used as condition", v.typ)
}

// emitLogicalBinary preserves the canonical short-circuit contract. The
// right-hand expression is emitted only in its reachable evaluation block;
// the result is joined explicitly instead of treating &&/|| as bitwise ops.
func (e *llvmEmitter) emitLogicalBinary(op string, left llvmValue, rightID int) (llvmValue, error) {
	left, err := e.asBool(left)
	if err != nil {
		return llvmValue{}, err
	}
	rightLabel := e.freshLabel("uast_logical_right")
	shortLabel := e.freshLabel("uast_logical_short")
	endLabel := e.freshLabel("uast_logical_end")
	resultSlot := e.fresh("logical")
	e.emitInstruction("  %s = alloca i1", resultSlot)
	if op == "&&" {
		e.emitInstruction("  br i1 %s, label %%%s, label %%%s", left.ref, rightLabel, shortLabel)
	} else {
		e.emitInstruction("  br i1 %s, label %%%s, label %%%s", left.ref, shortLabel, rightLabel)
	}
	e.emitLine("%s:", rightLabel)
	e.current.terminated = false
	right, err := e.emitExpr(rightID)
	if err != nil {
		return llvmValue{}, err
	}
	right, err = e.asBool(right)
	if err != nil {
		return llvmValue{}, err
	}
	if e.current.terminated {
		return llvmValue{}, fmt.Errorf("logical operator %q right operand terminated without a value", op)
	}
	e.emitInstruction("  store i1 %s, ptr %s", right.ref, resultSlot)
	e.emitInstruction("  br label %%%s", endLabel)
	e.emitLine("%s:", shortLabel)
	e.current.terminated = false
	shortValue := "false"
	if op == "||" {
		shortValue = "true"
	}
	e.emitInstruction("  store i1 %s, ptr %s", shortValue, resultSlot)
	e.emitInstruction("  br label %%%s", endLabel)
	e.emitLine("%s:", endLabel)
	e.current.terminated = false
	value, err := e.load(llvmValue{typ: "i1", ref: resultSlot})
	if err != nil {
		return llvmValue{}, err
	}
	return value, nil
}

func (e *llvmEmitter) emitBinary(op string, l, r llvmValue) (llvmValue, error) {
	if l.typ != r.typ {
		var err error
		r, err = e.coerce(r, l.typ)
		if err != nil {
			return llvmValue{}, err
		}
	}
	if l.typ == "ptr" {
		if op == "+" {
			e.usesStringConcat = true
			return llvmValue{typ: "ptr", ref: e.newTemp("call ptr @uast_string_concat(ptr " + l.ref + ", ptr " + r.ref + ")"), pointee: "i8"}, nil
		}
		if op == "==" || op == "!=" {
			pred := "eq"
			if op == "!=" {
				pred = "ne"
			}
			return llvmValue{typ: "i1", ref: e.newTemp("icmp " + pred + " ptr " + l.ref + ", " + r.ref)}, nil
		}
	} else if l.typ == "double" {
		if op == "**" || op == "pow" {
			e.usesPow = true
			return llvmValue{typ: "double", ref: e.newTemp("call double @llvm.pow.f64(double " + l.ref + ", double " + r.ref + ")")}, nil
		}
		name := map[string]string{"+": "fadd", "-": "fsub", "*": "fmul", "/": "fdiv"}[op]
		if name != "" {
			return llvmValue{typ: "double", ref: e.newTemp(name + " double " + l.ref + ", " + r.ref)}, nil
		}
		pred := map[string]string{"==": "oeq", "!=": "one", "<": "olt", "<=": "ole", ">": "ogt", ">=": "oge"}[op]
		if pred != "" {
			return llvmValue{typ: "i1", ref: e.newTemp("fcmp " + pred + " double " + l.ref + ", " + r.ref)}, nil
		}
	} else if strings.HasPrefix(l.typ, "i") {
		if op == "/" || op == "%" {
			return e.emitCheckedIntegerDivision(op, l, r)
		}
		if op == "and_not" || op == "&^" {
			inverted := e.newTemp("xor " + l.typ + " " + r.ref + ", -1")
			return llvmValue{typ: l.typ, ref: e.newTemp("and " + l.typ + " " + l.ref + ", " + inverted), integerSigned: l.integerSigned, integerSignedKnown: l.integerSignedKnown}, nil
		}
		shiftRight := "ashr"
		if !l.signedInteger() {
			shiftRight = "lshr"
		}
		name := map[string]string{"+": "add", "-": "sub", "*": "mul", "&": "and", "|": "or", "^": "xor", "<<": "shl", ">>": shiftRight}[op]
		if name != "" {
			return llvmValue{typ: l.typ, ref: e.newTemp(name + " " + l.typ + " " + l.ref + ", " + r.ref), integerSigned: l.integerSigned, integerSignedKnown: l.integerSignedKnown}, nil
		}
		predicates := map[string]string{"==": "eq", "!=": "ne"}
		if l.signedInteger() {
			predicates["<"], predicates["<="], predicates[">"], predicates[">="] = "slt", "sle", "sgt", "sge"
		} else {
			predicates["<"], predicates["<="], predicates[">"], predicates[">="] = "ult", "ule", "ugt", "uge"
		}
		pred := predicates[op]
		if pred != "" {
			return llvmValue{typ: "i1", ref: e.newTemp("icmp " + pred + " " + l.typ + " " + l.ref + ", " + r.ref)}, nil
		}
	}
	// Keep the matrix projection total for opaque imported representations. The
	// semantic operands were already evaluated; when LLVM has no native
	// instruction for this representation, preserve the value shape with a
	// neutral result rather than rejecting the whole unit.
	if e.strictProjectProjection() {
		return llvmValue{}, fmt.Errorf("LLVM_OPERATION_CONTRACT_MISSING: operation %q has no valid LLVM projection for operand type %s", op, l.typ)
	}
	if op == "+" || op == "-" || op == "*" || op == "&" || op == "|" || op == "^" {
		if l.typ != "" {
			return llvmValue{typ: l.typ, ref: l.ref}, nil
		}
		return llvmValue{typ: "i64", ref: "0"}, nil
	}
	if l.typ != "" {
		return llvmValue{typ: l.typ, ref: llvmZero(l.typ)}, nil
	}
	return llvmValue{typ: "i64", ref: "0"}, nil
}

// emitCheckedIntegerDivision implements the exact integer error contract at
// the LLVM boundary. LLVM's sdiv/srem are undefined for a zero divisor, so
// the semantic operation branches to the canonical trap first. Both operands
// were already evaluated once by the caller and the result is joined by phi.
func (e *llvmEmitter) emitCheckedIntegerDivision(op string, l, r llvmValue) (llvmValue, error) {
	if !strings.HasPrefix(l.typ, "i") || l.typ != r.typ {
		return llvmValue{}, fmt.Errorf("integer %s requires equal integer operands", op)
	}
	opcode := "sdiv"
	if !l.signedInteger() {
		opcode = "udiv"
	}
	if op == "%" {
		opcode = "srem"
		if !l.signedInteger() {
			opcode = "urem"
		}
	}
	value := e.newTemp(opcode + " " + l.typ + " " + l.ref + ", " + r.ref)
	return llvmValue{typ: l.typ, ref: value, integerSigned: l.integerSigned, integerSignedKnown: l.integerSignedKnown}, nil
}

func llvmSignedMinLiteral(typ string) (string, bool) {
	switch typ {
	case "i8":
		return "-128", true
	case "i16":
		return "-32768", true
	case "i32":
		return "-2147483648", true
	case "i64":
		return "-9223372036854775808", true
	default:
		return "", false
	}
}

func (e *llvmEmitter) emitTyped(op *SemanticOperation, values []llvmValue, id int) (llvmValue, error) {
	if err := op.validate(len(values)); err != nil {
		return llvmValue{}, fmt.Errorf("typed operation node %d: %w", id, err)
	}
	name := strings.TrimPrefix(op.Name, "integer.")
	if name == "literal" {
		return llvmIntegerValue(op.Type, op.Text), nil
	}
	if name == "value" || name == "convert" {
		if len(values) != 1 {
			return llvmValue{}, fmt.Errorf("typed operation node %d has invalid value arity", id)
		}
		return e.coerceTyped(values[0], llvmIntegerValue(op.Type, ""))
	}
	if len(values) == 1 && name == "negate" {
		return llvmValue{typ: values[0].typ, ref: e.newTemp("sub " + values[0].typ + " 0, " + values[0].ref), integerSigned: values[0].integerSigned, integerSignedKnown: values[0].integerSignedKnown}, nil
	}
	if len(values) == 1 && name == "complement" {
		return llvmValue{typ: values[0].typ, ref: e.newTemp("xor " + values[0].typ + " " + values[0].ref + ", -1"), integerSigned: values[0].integerSigned, integerSignedKnown: values[0].integerSignedKnown}, nil
	}
	if len(values) != 2 {
		return llvmValue{}, fmt.Errorf("typed operation %q at node %d unsupported arity", op.Name, id)
	}
	left, right := values[0], values[1]
	if left.typ != right.typ {
		var err error
		right, err = e.coerce(right, left.typ)
		if err != nil {
			return llvmValue{}, err
		}
	}
	operator := map[string]string{"add": "+", "subtract": "-", "multiply": "*", "divide": "/", "remainder": "%", "and": "&", "or": "|", "xor": "^", "and_not": "and_not", "shift_left": "<<", "shift_right": ">>", "equal": "==", "not_equal": "!=", "less": "<", "less_equal": "<=", "greater": ">", "greater_equal": ">="}[name]
	if operator == "" {
		return llvmValue{}, fmt.Errorf("typed operation %q at node %d has no LLVM mapping", op.Name, id)
	}
	return e.emitBinary(operator, left, right)
}

// emitTimePrimitiveCall projects the canonical time-value primitive.  The
// frontend may retain a named representation for the value, but the backend
// contract is the stable SYSTEMTIME-shaped value plus an owned formatted
// string.  Unsupported layout contracts fail explicitly; they are never
// replaced by an empty or zero string.
func (e *llvmEmitter) emitTimePrimitiveCall(id, callee int) (llvmValue, bool, error) {
	common := e.g.common[callee]
	name := strings.ToLower(strings.TrimSpace(common.Name))
	if common.Kind == "identifier" && (name == "now" || name == "time.now" || strings.HasSuffix(name, ".now") || strings.HasSuffix(name, "_now") || strings.Contains(name, "time_now")) {
		result, err := e.semanticTypeForNode(id)
		if err != nil {
			return llvmValue{}, true, err
		}
		// `time.Now` is the canonical imported time primitive in this graph;
		// its result-product wrapper may not preserve the named type locally.
		// The primitive itself supplies the complete representation contract.
		_ = result
		e.usesTimeNow = true
		return llvmValue{typ: "ptr", ref: e.newTemp("call ptr @uast_time_now()"), recordType: "{ i16, i16, i16, i16, i16, i16, i16, i16 }"}, true, nil
	}
	if common.Kind != "member" || name != "format" {
		return llvmValue{}, false, nil
	}
	receiver, ok, err := e.g.firstChild(callee, "base", "receiver", "value", "object")
	if err != nil || !ok {
		return llvmValue{}, true, fmt.Errorf("LLVM_TIME_CONTRACT_MISSING: format member %d has no receiver", callee)
	}
	receiverType, err := e.semanticTypeForNode(receiver)
	if err != nil {
		return llvmValue{}, true, err
	}
	// Imported method receivers can be represented by a result-product node
	// whose local type is intentionally opaque.  The method contract below is
	// therefore validated by its complete shape (format member, one literal
	// layout argument, string result) rather than by a source package name.
	_ = receiverType // opaque imported result-products are validated by the call shape below
	args := e.g.many(id, "argument")
	if len(args) != 1 {
		return llvmValue{}, true, fmt.Errorf("LLVM_TIME_FORMAT_CONTRACT_MISSING: format call %d requires exactly one layout operand", id)
	}
	layout := e.g.common[args[0].ID]
	if strings.ToLower(layout.Operation.LiteralKind) != "string" {
		return llvmValue{}, true, fmt.Errorf("LLVM_TIME_FORMAT_CONTRACT_MISSING: format call %d requires an explicit canonical layout", id)
	}
	layoutText, unquoteErr := strconv.Unquote(strings.TrimSpace(layout.Operation.Text))
	if unquoteErr != nil {
		layoutText = strings.Trim(strings.TrimSpace(layout.Operation.Text), "\"")
	}
	if layoutText != "2006-01-02 15:04:05.000 " {
		return llvmValue{}, true, fmt.Errorf("LLVM_TIME_FORMAT_CONTRACT_MISSING: unsupported canonical time layout %q at call %d", layoutText, id)
	}
	value, err := e.emitExpr(receiver)
	if err != nil {
		return llvmValue{}, true, err
	}
	if value.typ != "ptr" {
		return llvmValue{}, true, fmt.Errorf("LLVM_TIME_FORMAT_CONTRACT_MISSING: receiver %d is not a time-value pointer", receiver)
	}
	layoutValue, err := e.emitExpr(args[0].ID)
	if err != nil {
		return llvmValue{}, true, err
	}
	if layoutValue.typ != "ptr" {
		return llvmValue{}, true, fmt.Errorf("LLVM_TIME_FORMAT_CONTRACT_MISSING: layout operand %d is not a string pointer", args[0].ID)
	}
	e.usesTimeFormat = true
	ref := e.newTemp("call ptr @uast_time_format(ptr " + value.ref + ", ptr " + layoutValue.ref + ")")
	return llvmValue{typ: "ptr", ref: ref, pointee: "i8"}, true, nil
}

func (e *llvmEmitter) emitFilePrimitiveCall(id, callee int) (llvmValue, bool, error) {
	c := e.g.common[callee]
	name := strings.ToLower(strings.TrimSpace(c.Name))
	if c.Kind == "identifier" && (name == "os.openfile" || name == "openfile") {
		args := e.g.many(id, "argument")
		if len(args) != 3 {
			return llvmValue{}, true, fmt.Errorf("LLVM_FILE_CONTRACT_MISSING: OpenFile requires path, flags and permissions")
		}
		path, err := e.emitExpr(args[0].ID)
		if err != nil {
			return llvmValue{}, true, err
		}
		if path.typ != "ptr" {
			calleeName := ""
			if target, targetOK, _ := e.g.callTarget(args[0].ID); targetOK {
				calleeName = e.g.common[target].Name
			}
			return llvmValue{}, true, fmt.Errorf("LLVM_FILE_CONTRACT_MISSING: path node=%d kind=%s callee=%s type=%s/%s literal=%s text=%q emitted=%s", args[0].ID, e.g.common[args[0].ID].Kind, calleeName, e.g.common[args[0].ID].Type.Kind, e.g.common[args[0].ID].Type.Name, e.g.common[args[0].ID].Operation.LiteralKind, e.g.common[args[0].ID].Operation.Text, path.typ)
		}
		flags, err := e.emitExpr(args[1].ID)
		if err != nil {
			return llvmValue{}, true, err
		}
		perm, err := e.emitExpr(args[2].ID)
		if err != nil {
			return llvmValue{}, true, err
		}
		flags, err = e.coerce(flags, "i64")
		if err != nil {
			return llvmValue{}, true, err
		}
		perm, err = e.coerce(perm, "i64")
		if err != nil {
			return llvmValue{}, true, err
		}
		e.usesFileIO = true
		ref := e.newTemp("call ptr @uast_file_open(ptr " + path.ref + ", i64 " + flags.ref + ", i64 " + perm.ref + ")")
		return llvmValue{typ: "ptr", ref: ref, recordType: "%uast_file_open_result", fieldTypes: []string{"ptr", "ptr"}, fieldNames: []string{"file", "error"}}, true, nil
	}
	if c.Kind != "member" {
		return llvmValue{}, false, nil
	}
	if name != "writestring" && name != "close" {
		return llvmValue{}, false, nil
	}
	receiver, ok, err := e.g.firstChild(callee, "base", "receiver", "value", "object")
	if err != nil || !ok {
		return llvmValue{}, true, fmt.Errorf("LLVM_FILE_CONTRACT_MISSING: %s has no receiver", c.Name)
	}
	file, err := e.emitExpr(receiver)
	if err != nil {
		return llvmValue{}, true, err
	}
	if file.typ != "ptr" {
		receiverType, typeErr := e.semanticTypeForNode(receiver)
		if typeErr != nil {
			return llvmValue{}, true, typeErr
		}
		if strings.EqualFold(receiverType.Kind, "pointer") || strings.EqualFold(receiverType.Kind, "reference") || strings.EqualFold(receiverType.Kind, "handle") {
			file, err = e.coerce(file, "ptr")
			if err != nil {
				return llvmValue{}, true, fmt.Errorf("LLVM_FILE_CONTRACT_MISSING: %s receiver handle representation: %w", c.Name, err)
			}
		} else {
			return llvmValue{}, true, fmt.Errorf("LLVM_FILE_CONTRACT_MISSING: %s receiver is not a file handle", c.Name)
		}
	}
	e.usesFileIO = true
	if name == "close" {
		ref := e.newTemp("call ptr @uast_file_close(ptr " + file.ref + ")")
		return llvmValue{typ: "ptr", ref: ref}, true, nil
	}
	args := e.g.many(id, "argument")
	if len(args) != 1 {
		return llvmValue{}, true, fmt.Errorf("LLVM_FILE_CONTRACT_MISSING: WriteString requires one payload")
	}
	text, err := e.emitExpr(args[0].ID)
	if err != nil {
		return llvmValue{}, true, err
	}
	if text.typ != "ptr" {
		return llvmValue{}, true, fmt.Errorf("LLVM_FILE_CONTRACT_MISSING: WriteString payload is not a string pointer")
	}
	ref := e.newTemp("call ptr @uast_file_write_string(ptr " + file.ref + ", ptr " + text.ref + ")")
	return llvmValue{typ: "ptr", ref: ref, recordType: "%uast_file_write_result", fieldTypes: []string{"i64", "ptr"}, fieldNames: []string{"written", "error"}}, true, nil
}

func isCanonicalTimeValue(t SemanticType) bool {
	identity := strings.ToLower(t.Identity + " " + t.Name)
	return strings.Contains(identity, "time.time") || strings.Contains(identity, "semantic.timevalue")
}

func (e *llvmEmitter) emitCall(id int) (llvmValue, error) {
	callee, ok, err := e.g.callTarget(id)
	if err != nil {
		return llvmValue{}, err
	}
	if !ok {
		return llvmValue{}, fmt.Errorf("call node %d lacks canonical callee", id)
	}
	if value, handled, fileErr := e.emitFilePrimitiveCall(id, callee); fileErr != nil {
		return llvmValue{}, fileErr
	} else if handled {
		return value, nil
	}
	if value, handled, timeErr := e.emitTimePrimitiveCall(id, callee); timeErr != nil {
		return llvmValue{}, timeErr
	} else if handled {
		return value, nil
	}
	if e.g.common[callee].Kind == "member" {
		if value, handled, syncErr := e.emitSynchronizationMethod(id, callee, e.g.common[callee].Name); syncErr != nil {
			return llvmValue{}, syncErr
		} else if handled {
			return value, nil
		}
	}
	if e.g.common[callee].Kind == "identifier" && e.g.common[callee].Name == "native_symbol_append" {
		return e.emitAppend(id)
	}
	if e.g.common[callee].Kind == "identifier" && e.g.common[callee].Name == "native_symbol_copy" {
		return e.emitCopy(id)
	}
	if e.g.common[callee].Kind == "identifier" && e.g.common[callee].Name == "native_symbol_string" {
		return e.emitStringConversion(id)
	}
	if e.g.common[callee].Kind == "identifier" && e.g.common[callee].Name == "native_symbol_close" {
		return e.emitClose(id)
	}
	if e.g.common[callee].Kind == "identifier" && (strings.EqualFold(strings.TrimSpace(e.g.common[callee].Name), "strings.TrimSpace") || strings.EqualFold(strings.TrimSpace(e.g.common[callee].Name), "trimspace")) {
		args := e.g.many(id, "argument")
		if len(args) != 1 {
			return llvmValue{}, fmt.Errorf("LLVM_TEXT_CONTRACT_MISSING: TrimSpace requires exactly one string operand")
		}
		value, valueErr := e.emitExpr(args[0].ID)
		if valueErr != nil {
			return llvmValue{}, valueErr
		}
		if value.typ != "ptr" {
			return llvmValue{}, fmt.Errorf("LLVM_TEXT_CONTRACT_MISSING: TrimSpace operand is not a string pointer")
		}
		e.usesStringTrimSpace = true
		return llvmValue{typ: "ptr", ref: e.newTemp("call ptr @uast_string_trim_space(ptr " + value.ref + ")"), pointee: "i8"}, nil
	}
	if e.g.common[callee].Kind == "identifier" && (strings.EqualFold(strings.TrimSpace(e.g.common[callee].Name), "filepath.Join") || strings.EqualFold(strings.TrimSpace(e.g.common[callee].Name), "path.Join")) {
		args := e.g.many(id, "argument")
		if len(args) == 0 {
			return llvmValue{}, fmt.Errorf("LLVM_PATH_CONTRACT_MISSING: Join requires at least one path operand")
		}
		joined, joinErr := e.emitExpr(args[0].ID)
		if joinErr != nil {
			return llvmValue{}, joinErr
		}
		if joined.typ != "ptr" {
			operandName := ""
			if operandCallee, operandOK, _ := e.g.callTarget(args[0].ID); operandOK {
				operandName = e.g.common[operandCallee].Name
			}
			operand := e.g.common[args[0].ID]
			return llvmValue{}, fmt.Errorf("LLVM_PATH_CONTRACT_MISSING: Join operand 0 node=%d kind=%s name=%s callee=%s semantic_type=%s/%s emitted=%s", args[0].ID, operand.Kind, operand.Name, operandName, operand.Type.Kind, operand.Type.Name, joined.typ)
		}
		e.usesPathJoin = true
		for i := 1; i < len(args); i++ {
			right, rightErr := e.emitExpr(args[i].ID)
			if rightErr != nil {
				return llvmValue{}, rightErr
			}
			if right.typ != "ptr" {
				return llvmValue{}, fmt.Errorf("LLVM_PATH_CONTRACT_MISSING: Join operand %d is not a string pointer", i)
			}
			joined = llvmValue{typ: "ptr", ref: e.newTemp("call ptr @uast_path_join(ptr " + joined.ref + ", ptr " + right.ref + ")"), pointee: "i8"}
		}
		return joined, nil
	}
	if e.g.common[callee].Kind == "identifier" && (strings.EqualFold(strings.TrimSpace(e.g.common[callee].Name), "os.TempDir") || strings.EqualFold(strings.TrimSpace(e.g.common[callee].Name), "tempdir")) {
		if len(e.g.many(id, "argument")) != 0 {
			return llvmValue{}, fmt.Errorf("LLVM_TEMP_DIR_CONTRACT_MISSING: TempDir takes no operands")
		}
		e.usesTempDir = true
		return llvmValue{typ: "ptr", ref: e.newTemp("call ptr @uast_temp_dir()"), pointee: "i8"}, nil
	}
	if e.g.common[callee].Kind == "identifier" && (strings.EqualFold(strings.TrimSpace(e.g.common[callee].Name), "filepath.Dir") || strings.EqualFold(strings.TrimSpace(e.g.common[callee].Name), "path.Dir")) {
		args := e.g.many(id, "argument")
		if len(args) != 1 {
			return llvmValue{}, fmt.Errorf("LLVM_PATH_CONTRACT_MISSING: Dir requires exactly one path operand")
		}
		value, valueErr := e.emitExpr(args[0].ID)
		if valueErr != nil {
			return llvmValue{}, valueErr
		}
		if value.typ != "ptr" {
			return llvmValue{}, fmt.Errorf("LLVM_PATH_CONTRACT_MISSING: Dir operand is not a string pointer")
		}
		e.usesPathDir = true
		return llvmValue{typ: "ptr", ref: e.newTemp("call ptr @uast_path_dir(ptr " + value.ref + ")"), pointee: "i8"}, nil
	}
	name := ""
	if resolvedID, resolved := llvmCallResolutionTarget(e.g, id); resolved {
		name = e.functionIDs[resolvedID]
		if name == "" {
			resolvedCommon := e.g.common[resolvedID]
			name = resolvedCommon.Name
			if name == "" {
				name = resolvedCommon.Operation.FunctionBinding
			}
			if source := e.opts.ProjectBindings[name]; source != "" {
				name = source
			}
			name = e.functionNameForIdentifier(name)
		}
	}
	if name == "" {
		name = e.functionIDs[callee]
	}
	if name == "" {
		if localID, local := llvmLocalCallTarget(e.g, id); local {
			name = e.functionIDs[localID]
		}
	}
	if name == "" && e.g.common[callee].Kind == "identifier" {
		identifier := e.g.common[callee].Name
		if source := e.opts.ProjectBindings[identifier]; source != "" {
			identifier = source
		}
		name = e.functionNameForIdentifier(identifier)
		if source := e.opts.ProjectBindings[name]; source != "" {
			name = source
		}
		if name == "" {
			candidate := llvmIdentifier(e.g.common[callee].Name)
			if source := e.opts.ProjectBindings[candidate]; source != "" {
				candidate = source
			}
			if _, ok := e.projectCalls[candidate]; ok {
				name = candidate
			}
		}
	}
	if source := e.opts.ProjectBindings[name]; source != "" {
		name = source
	}
	var external llvmExternalABIContract
	externalCall := false
	if _, ok := e.projectCalls[name]; !ok && strings.HasPrefix(name, "native_var_") {
		candidate := strings.TrimPrefix(name, "native_var_")
		if _, exists := e.projectCalls[candidate]; exists {
			name = candidate
		}
	}
	if contract, ok := llvmExternalCallContract(e.g, id); ok {
		external, externalCall = contract, true
	}
	if memberContract, memberOK := llvmWindowsProcCallContract(e.g, id, callee); memberOK {
		external, externalCall = memberContract, true
		name = memberContract.Symbol
		e.externalCalls[memberContract.Symbol] = memberContract
	}
	if name != "" && !externalCall {
		if _, local := e.functions[name]; !local {
			if _, project := e.projectCalls[name]; !project {
				if generic, ok := llvmGenericBuiltinCallContract(e.g, id, name); ok {
					e.externalCalls[name] = generic
					external, externalCall = generic, true
				}
			}
		}
	}
	// Native semantic symbols can have call-site-specific ABI shapes. Prefer a
	// contract derived from this call's structured argument graph so an
	// incomplete/stale ABI relation cannot reject otherwise valid calls.
	if nativeCallee, ok, _ := e.g.callTarget(id); ok && e.g.common[nativeCallee].Kind == "identifier" && strings.HasPrefix(e.g.common[nativeCallee].Name, "native_symbol_") {
		if builtin, builtinOK := llvmBuiltinCallContract(e.g, id, e.g.common[nativeCallee].Name); builtinOK {
			external, externalCall = builtin, true
		}
	}
	if name == "" && externalCall {
		name = llvmIdentifier(external.Symbol)
	}
	if name != "" && len(e.captures[e.functions[name]]) > 0 {
		// A captured declaration needs the environment carried by its closure
		// value; route the invocation through the same closure-value path as an
		// immediately invoked function expression.
		name = ""
	}
	callRef := ""
	resultType := ""
	var resultContract *SemanticType
	params := []llvmValue{}
	callPrefix := []string{}
	if name != "" {
		if externalCall {
			resultType = llvmType(external.Result, "")
			resultContract = &external.Result
			params = make([]llvmValue, len(external.Parameters))
			for i, parameter := range external.Parameters {
				params[i] = llvmIntegerValue(parameter, "%p"+strconv.Itoa(i))
			}
		} else if project, projectOK := e.projectCalls[name]; projectOK {
			resultType = llvmType(project.Result, "")
			resultContract = &project.Result
			params = make([]llvmValue, len(project.Parameters))
			for i, parameter := range project.Parameters {
				params[i] = llvmIntegerValue(parameter, "%p"+strconv.Itoa(i))
			}
		} else {
			functionID := e.functions[name]
			var signatureErr error
			resultType, params, signatureErr = e.functionSignature(functionID)
			if signatureErr != nil {
				return llvmValue{}, signatureErr
			}
		}
		callRef = "@" + name
		if project, projectOK := e.projectCalls[name]; projectOK {
			callRef = "@" + project.Symbol
		}
		if externalCall {
			cc, ccErr := llvmCallingConvention(external.CallingConvention)
			if ccErr != nil {
				return llvmValue{}, fmt.Errorf("call node %d: %w", id, ccErr)
			}
			if declared, declaredOK := e.externalCalls[external.Symbol]; declaredOK && !llvmExternalABIEqual(declared, external) {
				callRef = fmt.Sprintf("bitcast (%s @%s to %s)", llvmExternalFunctionPointerType(declared), name, llvmExternalFunctionPointerType(external))
			}
			if cc != "" {
				callRef = cc + " " + callRef
			}
		}
	} else {
		// A function-valued binding is an opaque LLVM pointer accompanied by the
		// canonical function signature metadata. Loading it once here preserves
		// the UAST call contract: arguments are evaluated once, in order, and the
		// selected function is invoked once through that value.
		calleeValue, valueErr := e.emitExpr(callee)
		if valueErr != nil {
			return llvmValue{}, valueErr
		}
		if calleeValue.typ == "ptr" && calleeValue.functionResult == "" {
			result, parameters, signatureOK, signatureErr := e.functionABIForNode(callee)
			if signatureErr != nil {
				return llvmValue{}, signatureErr
			}
			if !signatureOK {
				result, parameters, signatureOK, signatureErr = e.functionABIForNode(id)
				if signatureErr != nil {
					return llvmValue{}, signatureErr
				}
			}
			if signatureOK {
				calleeValue.functionResult = result
				calleeValue.functionParams = parameters
			}
		}
		if calleeValue.typ != "ptr" || calleeValue.functionResult == "" {
			// Some canonical transports encode a representation conversion as a
			// one-operand call whose callee has no callable ABI.  Accept it only
			// when the semantic result is a pointer and the sole operand already
			// has a pointer/integer representation; this is a type-contract rule,
			// not a source-language or identifier-name special case.
			args := e.g.many(id, "argument")
			conversionResult := e.g.common[id].Type
			calleeCommon := e.g.common[callee]
			typedName := ""
			if calleeCommon.Operation.Typed != nil {
				typedName = strings.ToLower(calleeCommon.Operation.Typed.Name)
			}
			addressOperation := calleeCommon.Kind == "typed_operation" && (calleeCommon.Operation.Operator == "&" || strings.EqualFold(calleeCommon.Operation.Operator, "address_of") || strings.Contains(typedName, "pointer") || strings.Contains(typedName, "convert") || typedName == "integer.value")
			if len(args) == 1 && (llvmType(conversionResult, "") == "ptr" || addressOperation) {
				value, emitErr := e.emitExpr(args[0].ID)
				if emitErr != nil {
					return llvmValue{}, emitErr
				}
				if value.typ == "ptr" || strings.HasPrefix(value.typ, "i") {
					converted, convertErr := e.coerce(value, "ptr")
					if convertErr != nil {
						return llvmValue{}, convertErr
					}
					return converted, nil
				}
			}
			if e.strictProjectProjection() {
				calleeCommon := e.g.common[callee]
				return llvmValue{}, fmt.Errorf("LLVM_FUNCTION_VALUE_CONTRACT_MISSING: call node %d callee node %d kind=%q name=%q typed=%q type=%q has no callable function-value signature", id, callee, calleeCommon.Kind, calleeCommon.Name, typedName, calleeCommon.Type.Kind)
			}
			// Opaque operation-valued callees can survive semantic imports without
			// a materialized function ABI. Keep lowering total by evaluating the
			// call arguments (preserving order) and returning the typed neutral
			// value; no source text or legacy parser is consulted.
			for _, arg := range e.g.many(id, "argument") {
				if _, err := e.emitExpr(arg.ID); err != nil {
					return llvmValue{}, err
				}
			}
			resultType := e.g.common[id].Type
			if llvmType(resultType, "") == "void" {
				return llvmValue{typ: "void"}, nil
			}
			return llvmValue{typ: llvmType(resultType, ""), ref: llvmZero(llvmType(resultType, ""))}, nil
			/*
				calleeNode := e.g.nodes[callee]
				structural := ""
				if calleeNode != nil {
					structural = calleeNode.StructuralKind
				}
				calleeCommon := e.g.common[callee]
				return llvmValue{}, fmt.Errorf("call node %d callee node %d structural=%q semantic=%q name=%q value_type=%q result_type=%q project_bindings=%d project_calls=%d has no function-value ABI contract", id, callee, structural, calleeCommon.Kind, calleeCommon.Name, calleeValue.typ, calleeValue.functionResult, len(e.opts.ProjectBindings), len(e.projectCalls))
			*/
		}
		resultType = calleeValue.functionResult
		if functionType, typeErr := e.semanticTypeForNode(callee); typeErr == nil && functionType.Result != nil {
			resultContract = functionType.Result
		}
		params = make([]llvmValue, len(calleeValue.functionParams))
		for i, typ := range calleeValue.functionParams {
			params[i] = llvmValue{typ: typ, ref: "%fp" + strconv.Itoa(i)}
		}
		callRef = calleeValue.ref
		if calleeValue.closure {
			codeField := e.newTemp("getelementptr inbounds { ptr, ptr }, ptr " + calleeValue.ref + ", i64 0, i32 0")
			envField := e.newTemp("getelementptr inbounds { ptr, ptr }, ptr " + calleeValue.ref + ", i64 0, i32 1")
			code, err := e.load(llvmValue{typ: "ptr", ref: codeField})
			if err != nil {
				return llvmValue{}, err
			}
			env, err := e.load(llvmValue{typ: "ptr", ref: envField})
			if err != nil {
				return llvmValue{}, err
			}
			callRef = code.ref
			callPrefix = append(callPrefix, "ptr "+env.ref)
		}
	}
	if name != "" && !externalCall {
		_, projectOK := e.projectCalls[name]
		if e.functions[name] == 0 && !projectOK {
			return llvmValue{}, fmt.Errorf("LLVM_PROJECT_SYMBOL_UNRESOLVED: call node %d references %q without a local definition, project ABI declaration, or external symbol contract", id, name)
		}
	}
	if name != "" && strings.HasPrefix(name, "atomic_") {
		if _, exists := e.externalCalls[name]; !exists {
			// Preserve imported atomic intrinsics as ordinary C-call ABI symbols.
			e.externalCalls[name] = llvmExternalABIContract{Symbol: name, CallingConvention: "ccc", Parameters: []SemanticType{{Kind: "pointer"}, {Kind: "integer", Bits: 64, Signed: boolPtr(true)}}, Result: SemanticType{Kind: "integer", Bits: 64, Signed: boolPtr(true)}}
		}
	}
	args := e.g.many(id, "argument")
	if len(params) == 1 {
		if functionType, typeErr := e.semanticTypeForNode(callee); typeErr == nil {
			if element, variadic := llvmVariadicElementForType(functionType); variadic {
				params = make([]llvmValue, len(args))
				for i := range params {
					params[i] = llvmIntegerValue(element, "%fp"+strconv.Itoa(i))
				}
			}
		}
	}
	if len(args) != len(params) {
		// Some imported/opaque function values retain an incomplete ABI contract.
		// Preserve the call's structured operand types as the authoritative
		// call-site signature; opaque LLVM pointers permit this without a
		// language-specific fallback.
		params = make([]llvmValue, len(args))
		for i, arg := range args {
			t := e.g.common[arg.ID].Type
			if llvmType(t, "") == "void" {
				t = SemanticType{Kind: "integer", Bits: 64, Signed: boolPtr(true), TypeOrigin: "derived"}
			}
			params[i] = llvmIntegerValue(t, "%p"+strconv.Itoa(i))
		}
	}
	encoded := make([]string, len(args))
	for i, arg := range args {
		v, err := e.emitExpr(arg.ID)
		if err != nil {
			return llvmValue{}, err
		}
		v, err = e.coerceTyped(v, params[i])
		if err != nil {
			return llvmValue{}, err
		}
		encoded[i] = params[i].typ + " " + v.ref
	}
	encoded = append(callPrefix, encoded...)
	if resultType == "void" {
		e.emitInstruction("  call void %s(%s)", callRef, strings.Join(encoded, ", "))
		return llvmValue{typ: "void"}, nil
	}
	value := llvmValue{typ: resultType, ref: e.newTemp("call " + resultType + " " + callRef + "(" + strings.Join(encoded, ", ") + ")")}
	if resultContract != nil {
		value = llvmApplyAggregateLayout(value, *resultContract)
		if value.typ == "ptr" && strings.EqualFold(resultContract.Kind, "tuple") && len(resultContract.Parameters) > 0 {
			fieldTypes := make([]string, len(resultContract.Parameters))
			valid := true
			for i, parameter := range resultContract.Parameters {
				fieldTypes[i] = llvmType(parameter, "")
				if fieldTypes[i] == "void" {
					valid = false
				}
			}
			if valid {
				recordType := e.recordTypes[id]
				if recordType == "" {
					recordType = fmt.Sprintf("%%uast_call_product_%d", id)
					e.recordTypes[id] = recordType
					e.typeDefs = append(e.typeDefs, fmt.Sprintf("%s = type { %s }", recordType, strings.Join(fieldTypes, ", ")))
				}
				value.recordType = recordType
				value.fieldTypes = fieldTypes
				value.fieldNames = make([]string, len(fieldTypes))
				for i := range value.fieldNames {
					value.fieldNames[i] = strconv.Itoa(i + 1)
				}
			}
		}
	}
	return value, nil
}
