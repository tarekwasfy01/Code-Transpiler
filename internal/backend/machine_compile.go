// Copyright (c) 2026 Tarek Wasfy

package backend

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type CompileOutputKind string

// CompileInputKind identifies the representation supplied to the common
// compiler API. Source is currently the fully productive frontend; the other
// values are explicit capability contracts so callers cannot accidentally
// treat binary data as UTF-8 source.
type CompileInputKind string

const (
	CompileInputSource     CompileInputKind = "source"
	CompileInputAssembly   CompileInputKind = "assembly"
	CompileInputMachine    CompileInputKind = "machine_code"
	CompileInputObject     CompileInputKind = "object"
	CompileInputExecutable CompileInputKind = "executable"
)

const (
	CompileSource      CompileOutputKind = "source"
	CompileAssembly    CompileOutputKind = "assembly"
	CompileMachineCode CompileOutputKind = "machine_code"
	CompileObject      CompileOutputKind = "object"
	CompileExecutable  CompileOutputKind = "executable"
)

type CompileOptions struct {
	InputKind       CompileInputKind
	SourceLanguage  string
	SourceArch      string
	SourceOS        string
	SourceABI       string
	SourceAsmSyntax string
	TargetArch      string
	TargetOS        string
	ABI             string
	OutputKind      CompileOutputKind
	TargetLanguage  string
	EntryPoint      string
	ViaAssembly     bool
	BaseAddress     uint64
	// ModuleBaseDir resolves relative Semantic module references. An explicit
	// link-root plan remains the default; EmbedAllModules is the opt-in for a
	// complete declared-module bundle.
	ModuleBaseDir   string
	ModuleStoreRoot string
	EmbedAllModules bool
	// ModuleEmbeddingMode is "needed" (default), "references", or "all".
	// EmbedAllModules is retained for source/API compatibility and implies "all".
	ModuleEmbeddingMode SemanticModuleEmbeddingMode
	// CacheDir enables the content-addressed native lowering cache. Empty keeps
	// the API hermetic and disables disk caching.
	CacheDir string
	// ExecutableClosureReportPath optionally overrides the deterministic
	// executable-closure.json diagnostic emitted before project code generation.
	ExecutableClosureReportPath string
	ProjectInitializers         []ExecutableFunctionRef
	// MaxWorkers optionally caps project pipeline concurrency. A zero value
	// uses the CPU policy. MemoryBudgetBytes and UnitPeakBytes enable the
	// conservative RAM-aware cap; when unset only the CPU cap is applied.
	MaxWorkers        int
	MemoryBudgetBytes int64
	UnitPeakBytes     int64
	GlobalMemoryBytes int64
	QueueMemoryBytes  int64
	LinkMemoryBytes   int64
	// ProjectMode keeps unresolved named calls as linker relocations. It is set
	// by CompileSemanticProject; standalone compilation remains fail-closed.
	ProjectMode   bool
	ProjectIndex  *SemanticProjectIndex
	ProjectUnitID string
	// projectIndexFingerprint is computed once by CompileSemanticProject and
	// folded into every unit cache key. It prevents stale relocations when the
	// cross-unit symbol/ABI contract changes without re-marshaling that index
	// once per function-bearing unit.
	projectIndexFingerprint string
	projectUnitSemanticRoot string
}
type CompileResult struct {
	Bytes []byte
	// ObjectBytes is the COFF representation of the exact selected instruction
	// stream. It lets callers retain a real linker intermediate without running
	// selection and register allocation a second time.
	ObjectBytes []byte
	// FunctionObjects contains per-function COFF staging artifacts. Their text
	// is cut from the same encoded program and is retained for incremental build
	// inspection; final image linking still uses the complete encoded stream so
	// cross-function fixups remain authoritative.
	FunctionObjects map[string][]byte
	// Fragments are derived target-output units corresponding to the retained
	// COFF objects. They carry no semantic state and are safe to discard after
	// the deterministic linker has completed.
	Fragments map[string]MachineFragment
	Imports   []pe64ImportSpec `json:"imports,omitempty"`
	// Plan exposes the derived summary/partition metrics for profiling and
	// build journals without exposing semantic internals to callers.
	Plan                               StreamingPlan
	Metrics                            StreamingMetrics
	CacheHit                           bool
	Regions                            []EncodedRegion
	nativeFunctions                    []x64Function
	projectSymbols                     map[string]ProjectSymbol
	NativeUnitCount                    int `json:"native_unit_count,omitempty"`
	NativeFragmentCount                int `json:"native_fragment_count,omitempty"`
	NativeRelocationCount              int `json:"native_relocation_count,omitempty"`
	UnresolvedProjectSymbolsBeforeLink int `json:"unresolved_project_symbols_before_link,omitempty"`
	UnresolvedProjectSymbolsAfterLink  int `json:"unresolved_project_symbols_after_link,omitempty"`
	Text                               string
	OutputKind                         CompileOutputKind
	InstructionCount                   int
	AppliedRecipes                     []string
	AllocatedLiveRanges                int
}

// CompileMachine consumes the existing canonical document. Architecture/OS/ABI
// are independent options. Unsupported semantic nodes produce an error before
// any bytes are returned.
func CompileMachine(p *SemanticProgram, opts CompileOptions) (CompileResult, error) {
	result := CompileResult{OutputKind: opts.OutputKind}
	if p != nil && len(p.Origin.Modules) == 0 && p.UniversalAST != nil {
		// Some compact .se transports preserve imported symbol identities but
		// omit the redundant origin.modules list. Recover only the structured
		// package identities from UAST fields so module embedding remains driven
		// by semantic data, never by diagnostics or source-text heuristics.
		p.Origin.Modules = semanticImportsFromUAST(p.UniversalAST)
	}
	// A transported semantic unit may retain import identities without an
	// embedding sidecar. Resolve those imports through the existing module
	// store/GOROOT resolver before native lowering, so external Go declarations
	// are represented by real SemanticPrograms rather than opaque fallbacks.
	if !opts.ProjectMode && p != nil && len(p.Origin.Modules) > 0 && (p.Metadata == nil || strings.TrimSpace(p.Metadata["semantic_module_embeddings"]) == "") {
		base := opts.ModuleBaseDir
		if base == "" {
			base = filepath.Dir(opts.ModuleBaseDir)
		}
		entries, embedErr := EmbedSemanticModules(p, SemanticModuleEmbeddingOptions{BaseDir: base, StoreRoot: opts.ModuleStoreRoot, UnitPath: base, Language: p.Origin.SourceLanguage, NeededOnly: true})
		if embedErr != nil {
			return result, fmt.Errorf("SEMANTIC_MODULE_EMBED: %w", embedErr)
		}
		if len(entries) > 0 {
			if err := LinkEmbeddedSemanticModules(p, base); err != nil {
				return result, fmt.Errorf("SEMANTIC_MODULE_EMBED: %w", err)
			}
		}
	}
	if !opts.ProjectMode && p != nil && p.Metadata != nil && strings.TrimSpace(p.Metadata["semantic_module_embeddings"]) != "" {
		if err := LinkEmbeddedSemanticModules(p, opts.ModuleBaseDir); err != nil {
			return result, fmt.Errorf("SEMANTIC_MODULE_EMBED: %w", err)
		}
	}
	// A project unit has already crossed the Semantic parser boundary. Its
	// canonical document is validated by newUASTExecutionGraph below, which is
	// the graph this compiler will actually consume. Running
	// ValidateSemanticProgram first rebuilt and validated a second full graph,
	// then canonicalUniversalAST rebuilt the same closure again. Keep the full
	// public validation path for standalone API inputs; project compilation
	// performs the behavior-extension checks plus the consuming graph's normal
	// structural/execution validation exactly once.
	if !opts.ProjectMode {
		if err := ValidateSemanticProgram(p); err != nil {
			return result, err
		}
	} else if p == nil {
		return result, fmt.Errorf("missing semantic program")
	}
	if err := validateExecutableDialects(p); err != nil {
		return result, err
	}
	if opts.TargetArch == "" {
		opts.TargetArch = "x86_64"
	}
	if opts.TargetOS == "" {
		opts.TargetOS = "windows"
	}
	if opts.ABI == "" {
		opts.ABI = "win64"
	}
	if opts.OutputKind == "" {
		opts.OutputKind = CompileExecutable
		result.OutputKind = opts.OutputKind
	}
	if opts.TargetArch != "x86_64" || opts.TargetOS != "windows" || opts.ABI != "win64" {
		return result, fmt.Errorf("native target unavailable: %s/%s/%s", opts.TargetArch, opts.TargetOS, opts.ABI)
	}
	u, err := canonicalUniversalAST(p)
	if err != nil {
		return result, err
	}
	// Semantic transports may keep producer-side binding aliases and package
	// context on SemanticProgram while the canonical document carries only the
	// executable graph.  Preserve that structured metadata at the projection
	// boundary so project-mode symbol labels remain identical across units.
	if opts.ProjectMode && u != nil {
		if len(p.Extensions) > 0 {
			if u.Extensions == nil {
				u.Extensions = map[string]any{}
			}
			for key, value := range p.Extensions {
				if _, exists := u.Extensions[key]; !exists {
					u.Extensions[key] = value
				}
			}
		}
		if len(p.Metadata) > 0 {
			if u.Metadata == nil {
				u.Metadata = map[string]string{}
			}
			for key, value := range p.Metadata {
				if _, exists := u.Metadata[key]; !exists {
					u.Metadata[key] = value
				}
			}
		}
	}
	// Assembly compilation consumes the same universal lowering contract as
	// source-target projection.  Apply the bounded, target-aware UAST rewrite
	// before selecting x86-64 instructions; if no rule can close a residual,
	// keep the original canonical graph so the existing fail-closed legality
	// checks report the precise unsupported operation.
	if lowered, loweringTrace, lowerErr := UniversalLower(u, "native-x86_64-windows"); lowerErr == nil && loweringTrace.Success {
		u = lowered
		result.AppliedRecipes = append(result.AppliedRecipes, loweringTrace.Rules...)
	}
	if opts.ProjectMode {
		if err := validateSemanticBehaviorExtensions(u); err != nil {
			return result, err
		}
	}
	u, recipes, err := ApplyPrimitiveClosure(u, "native-x86_64-windows")
	if err != nil {
		return result, err
	}
	result.AppliedRecipes = append(result.AppliedRecipes, recipes...)
	graph, err := newUASTExecutionGraph(u)
	if err != nil {
		return result, err
	}
	// The command-line compiler intentionally leaves -entry optional.  The
	// canonical document, however, already carries the source entry contract
	// (normally Go's main).  Passing an empty selector entry previously made a
	// generated PE call ExitProcess(0) without dispatching the compiled main
	// function.  Use the document contract before selection; a document with no
	// declared entry still follows selectX64's fail-closed/default behaviour.
	if opts.EntryPoint == "" && graph.document != nil {
		opts.EntryPoint = strings.TrimSpace(graph.document.Origin.EntryPoint)
	}
	// Build the bounded global summary and deterministic semantic units before
	// target selection.  This is a derived planning view over the canonical
	// UAST; it keeps expensive unit-local work bounded without introducing a
	// second semantic representation.  The selector below consumes the same
	// graph, so planning cannot change program meaning.
	planStart := time.Now()
	streamPlan := planStreamingLowering(graph)
	result.Plan = streamPlan
	result.Metrics.SummaryMicros = elapsedMicros(planStart)
	result.Metrics.Units = len(streamPlan.Units)
	result.Metrics.Functions = len(streamPlan.Index.Functions)
	if err := streamPlan.validate(); err != nil {
		return result, err
	}
	if graph.document.Metadata == nil {
		graph.document.Metadata = map[string]string{}
	}
	graph.document.Metadata["lowering.architecture"] = "summary-partition-bounded-v1"
	graph.document.Metadata["lowering.mode"] = "bounded-plan-existing-selector"
	graph.document.Metadata["lowering.units"] = strconv.Itoa(len(streamPlan.Units))
	graph.document.Metadata["lowering.workers"] = strconv.Itoa(streamPlan.Workers)
	graph.document.Metadata["lowering.queue"] = strconv.Itoa(streamPlan.QueueSize)
	// Run the bounded preparation stage even when the target encoder remains
	// fused below.  It validates unit ownership and provides the same
	// cancellation/error boundary used by fragment writers and cache loads.
	prepStart := time.Now()
	if err := runBoundedUnits(context.Background(), streamPlan.Units, streamPlan.Workers, func(unit CompilationUnit) error {
		if len(unit.FunctionIDs) == 0 {
			return fmt.Errorf("empty streaming compilation unit")
		}
		return nil
	}); err != nil {
		return result, err
	}
	result.Metrics.PreparationMicros = elapsedMicros(prepStart)
	legalityStart := time.Now()
	legality, err := analyzeNativeLegalityGraph(graph, "native-x86_64-windows", NativeLegalityFull)
	if err != nil {
		return result, err
	}
	if !legality.FullLegal() {
		blocked := legality.Blocking()
		parts := make([]string, 0, len(blocked))
		for _, decision := range blocked {
			parts = append(parts, fmt.Sprintf("node=%d family=%s status=%s reason=%s", decision.NodeID, decision.Family, decision.Status, decision.Reason))
		}
		return result, fmt.Errorf("NATIVE_LEGALITY_UNRESOLVED: %s", strings.Join(parts, "; "))
	}
	result.Metrics.LegalityMicros = elapsedMicros(legalityStart)
	var selected x64Program
	var labels map[string]int
	var code []byte
	var unresolved []FragmentRelocation
	cacheKey := nativeLoweringCacheKey(graph.document, opts)
	cacheRecord, cacheHit := loadNativeLoweringCache(opts.CacheDir, cacheKey)
	if cacheHit {
		result.Metrics.CacheHits = 1
	} else {
		result.Metrics.CacheMisses = 1
	}
	if cacheHit && opts.OutputKind != CompileAssembly && !opts.ViaAssembly {
		selected.Functions = append([]x64Function(nil), cacheRecord.Functions...)
		labels = cacheRecord.Labels
		code = append([]byte(nil), cacheRecord.Code...)
		unresolved = append([]FragmentRelocation(nil), cacheRecord.Unresolved...)
		selected.Imports = append([]pe64ImportSpec(nil), cacheRecord.Imports...)
		result.CacheHit = true
	} else {
		selectionStart := time.Now()
		selected, err = selectX64(graph, opts.EntryPoint, opts.ProjectMode, opts.ProjectIndex, opts.ProjectUnitID, opts.ProjectInitializers)
		if err != nil {
			return result, err
		}
		result.Metrics.SelectionMicros = elapsedMicros(selectionStart)
		result.InstructionCount = len(selected.Instructions)
		result.Regions = deriveEncodedRegions(selected, labels)
		result.AllocatedLiveRanges = allocateX64Registers(&selected)
		encodingStart := time.Now()
		if opts.ProjectMode {
			code, labels, unresolved, err = encodeX64Relocatable(selected)
		} else {
			code, labels, err = encodeX64(selected)
		}
		if err != nil {
			return result, err
		}
		result.Metrics.EncodingMicros = elapsedMicros(encodingStart)
		for _, in := range selected.Instructions {
			if in.Op == "label" {
				continue
			}
			if _, ok := lookupVerifiedX64Stencil(in.Op, in.A, in.B); ok {
				result.Metrics.StencilHits++
			} else {
				result.Metrics.StencilMisses++
			}
		}
		if opts.OutputKind != CompileAssembly && !opts.ViaAssembly {
			saveNativeLoweringCache(opts.CacheDir, cacheKey, nativeLoweringCacheRecord{Schema: nativeLoweringBackendVersion, Code: code, Labels: labels, Functions: selected.Functions, Unresolved: unresolved, Imports: selected.Imports})
		}
	}
	if opts.ViaAssembly {
		// Explicit opt-in development/validation path only. Never on direct path.
		dir, e := os.MkdirTemp("", "uast-asm-")
		if e != nil {
			return result, e
		}
		defer os.RemoveAll(dir)
		src := filepath.Join(dir, "program.asm")
		dst := filepath.Join(dir, "program.bin")
		if e = os.WriteFile(src, []byte(renderX64(selected)), 0600); e != nil {
			return result, e
		}
		nasmPath, lookErr := exec.LookPath("nasm")
		if lookErr != nil {
			// GUI processes often inherit a reduced PATH. Probe the conventional
			// per-user and system NASM locations before reporting the assembler as
			// unavailable; no source or semantic fallback is used.
			candidates := []string{}
			if local := os.Getenv("LOCALAPPDATA"); local != "" {
				candidates = append(candidates, filepath.Join(local, "bin", "NASM", "nasm.exe"))
			}
			if pf := os.Getenv("ProgramFiles"); pf != "" {
				candidates = append(candidates, filepath.Join(pf, "NASM", "nasm.exe"))
			}
			for _, candidate := range candidates {
				if _, statErr := os.Stat(candidate); statErr == nil {
					nasmPath = candidate
					lookErr = nil
					break
				}
			}
		}
		if lookErr != nil {
			return result, fmt.Errorf("explicit assembler: nasm not found (install NASM or add it to PATH)")
		}
		if log, e := exec.Command(nasmPath, "-O0", "-f", "bin", "-o", dst, src).CombinedOutput(); e != nil {
			return result, fmt.Errorf("explicit assembler: %w: %s", e, log)
		}
		// Different instruction sizes also affect unwind offsets and function RVAs.
		// Until an assembler symbol map is imported require identical encodings.
		assembled, e := os.ReadFile(dst)
		if e != nil {
			return result, e
		}
		if string(assembled) != string(code) {
			return result, fmt.Errorf("assembler encoding differs; refusing invalid function/unwind offsets")
		}
		code = assembled
	}
	// Keep a real object representation for build systems that persist native
	// intermediates. The final PE still has to be emitted after all symbols and
	// relocations are known, but this object is useful for inspection and for
	// locating a failure before PE image construction.
	result.ObjectBytes = coff64Object(code)
	result.nativeFunctions = append([]x64Function(nil), selected.Functions...)
	result.Imports = append([]pe64ImportSpec(nil), selected.Imports...)
	result.projectSymbols = selected.ProjectSymbols
	result.FunctionObjects = make(map[string][]byte, len(selected.Functions))
	result.Fragments = make(map[string]MachineFragment, len(selected.Functions))
	result.Metrics.Fragments = len(selected.Functions)
	globalRelocations := deriveX64Relocations(code, labels)
	if opts.ProjectMode {
		globalRelocations = append(globalRelocations, unresolved...)
	}
	fragmentRelocations := func(start, end int) []FragmentRelocation {
		out := make([]FragmentRelocation, 0)
		for _, r := range globalRelocations {
			if int(r.Offset) >= start && int(r.Offset) < end {
				r.Offset -= uint32(start)
				out = append(out, r)
			}
		}
		return out
	}
	labelOwner := map[string]string{}
	for _, fn := range selected.Functions {
		labelOwner[fn.Label] = fn.Label
	}
	for _, fn := range selected.Functions {
		start, startOK := labels[fn.Label]
		end, endOK := labels[fn.End]
		if !startOK || !endOK || start < 0 || end < start || end > len(code) {
			continue
		}
		for name, offset := range labels {
			if offset >= start && offset <= end {
				if _, exists := labelOwner[name]; !exists {
					labelOwner[name] = fn.Label
				}
			}
		}
	}
	fragmentSymbols := func(start, end int, includeEnd bool, fallback string) map[string]uint32 {
		out := map[string]uint32{}
		if fallback != "" {
			out[fallback] = 0
		}
		for name, offset := range labels {
			if offset < start || offset > end || (!includeEnd && offset == end) {
				continue
			}
			if owner := labelOwner[name]; owner != "" && owner != fallback {
				continue
			}
			out[name] = uint32(offset - start)
		}
		return out
	}
	occupied := make([]bool, len(code))
	for _, fn := range selected.Functions {
		start, startOK := labels[fn.Label]
		end, endOK := labels[fn.End]
		if !startOK || !endOK || start < 0 || end < start || end > len(code) {
			continue
		}
		for i := start; i < end; i++ {
			occupied[i] = true
		}
		fragmentBytes := append([]byte(nil), code[start:end]...)
		result.FunctionObjects[fn.Label] = coff64ObjectNamedAt(fragmentBytes, fn.Label, start)
		result.Fragments[fn.Label] = MachineFragment{UnitID: start, SemanticHash: stableBytesHash(fragmentBytes), Text: fragmentBytes, Symbols: fragmentSymbols(start, end, true, fn.Label), Relocations: fragmentRelocations(start, end)}
	}
	// Labels/data and the small entry/trampoline fragments between functions
	// are part of the encoded program as well. Preserve them as offset-addressed
	// COFF fragments so the object linker reconstructs every byte exactly.
	fragment := 0
	for i := 0; i < len(code); {
		if occupied[i] {
			i++
			continue
		}
		start := i
		for i < len(code) && !occupied[i] {
			i++
		}
		name := fmt.Sprintf("__text_fragment_%d", fragment)
		fragment++
		fragmentBytes := append([]byte(nil), code[start:i]...)
		result.FunctionObjects[name] = coff64ObjectNamedAt(fragmentBytes, name, start)
		result.Fragments[name] = MachineFragment{UnitID: start, Text: fragmentBytes, Symbols: fragmentSymbols(start, i, false, name), Relocations: fragmentRelocations(start, i)}
	}
	linkStart := time.Now()
	var linkedCode []byte
	var linkErr error
	if opts.ProjectMode {
		linkedCode, linkErr = linkMachineFragmentsRelocatable(result.Fragments)
	} else {
		linkedCode, linkErr = linkMachineFragments(result.Fragments)
	}
	if linkErr != nil {
		return result, fmt.Errorf("COFF function link: %w", linkErr)
	}
	if !bytes.Equal(linkedCode, code) {
		return result, fmt.Errorf("COFF function link changed encoded text")
	}
	result.Metrics.LinkMicros = elapsedMicros(linkStart)
	// The final image is built from the bytes reconstructed from the objects,
	// not from the pre-object encoder buffer.
	code = linkedCode
	result.ObjectBytes = coff64Object(code)
	switch opts.OutputKind {
	case CompileAssembly:
		result.Text = renderX64(selected)
	case CompileMachineCode:
		result.Bytes = code
	case CompileObject:
		result.Bytes = result.ObjectBytes
	case CompileExecutable:
		result.Bytes, err = pe64Image(code, labels, selected.Functions, selected.Imports)
	default:
		err = fmt.Errorf("unknown native output kind %q", opts.OutputKind)
	}
	return result, err
}

func semanticImportsFromUAST(u *UniversalASTDocument) []string {
	if u == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, n := range u.Nodes {
		for _, raw := range n.Fields {
			var v struct {
				Identity string `json:"identity"`
			}
			if json.Unmarshal(raw, &v) != nil || v.Identity == "" {
				continue
			}
			name := v.Identity
			if strings.HasPrefix(name, "go:") {
				parts := strings.Split(name, ":")
				if len(parts) >= 2 {
					name = parts[1]
				}
			}
			if i := strings.IndexByte(name, '.'); i > 0 {
				pkg := name[:i]
				if pkg != "main" && !seen[pkg] {
					seen[pkg] = true
					out = append(out, pkg)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// M_ISEL is a target-terminal table over canonical operators, not a new
// semantic registry. Control/operand roles come from the canonical graph.
var x64OperatorForms = map[string]string{"+": "add", "-": "sub", "*": "imul", "&": "and", "|": "or", "^": "xor", "<<": "shl", ">>": "sar", "==": "je", "!=": "jne", "<": "jl", "<=": "jle", ">": "jg", ">=": "jge"}
var win64IntegerArguments = []byte{xRCX, xRDX, xR8, xR9}

type x64Selector struct {
	g                    *uastExecutionGraph
	p                    x64Program
	functions            map[string]int
	functionLabels       map[string]string
	functionValues       map[string]bool
	functionValueTargets map[string]int
	emittedFunctions     map[int]bool
	functionCaptures     map[int][]string
	slots                map[string]int
	allocated            int
	outgoing             int
	serial               int
	returnLabel          string
	loops                [][2]string
	depth                int
	bindingTypes         map[string]SemanticType
	floatReturn          bool
	aggregateReturn      bool
	aggregateReturnSlot  int
	projectMode          bool
	projectIndex         *SemanticProjectIndex
	projectUnitID        string
	projectNames         map[string]string
	projectNamespace     string
	projectInitializers  []ExecutableFunctionRef
	globals              map[string]string
	externalPackages     map[string]bool
}

// memberReceiver resolves the receiver from the canonical member-access
// child-role contract. Frontends use object/receiver/base where available and
// the canonical value role for MemberAccessExpr; method lowering must not
// infer a receiver from a source spelling or from the method's arguments.
func x64MemberReceiver(s *x64Selector, member int) (int, bool) {
	if s == nil || s.g == nil {
		return 0, false
	}
	for _, role := range []string{"object", "receiver", "base", "value"} {
		children := s.g.many(member, role)
		if len(children) == 0 {
			continue
		}
		if len(children) != 1 {
			return 0, false
		}
		return children[0].ID, true
	}
	return 0, false
}

func (s *x64Selector) emit(op string, a, b x64Operand) {
	s.p.Instructions = append(s.p.Instructions, x64Instruction{op, a, b})
}
func (s *x64Selector) label() string { s.serial++; return fmt.Sprintf("L%d", s.serial) }

// emitNativeValueBox evaluates one semantic expression exactly once and
// materializes the fixed-width NativeValue ABI cells used by variadic calls.
// The returned slot contains the payload pointer; its preceding slot stores
// the NativeValueTag. Callers pass the pair as a contiguous frame element.
func (s *x64Selector) emitNativeValueBox(id int) (int, error) {
	if err := s.expression(id); err != nil {
		return 0, err
	}
	payload := s.slot()
	tag := s.slot()
	s.emit("mov", xm(xRBP, payload), xr(xRAX))
	kind := s.g.common[id].Type.Kind
	var t NativeValueTag
	switch kind {
	case "string":
		t = NativeValueString
	case "float", "float32", "float64":
		t = NativeValueFloat
	case "boolean", "bool":
		t = NativeValueBool
	case "pointer", "function", "slice", "array", "aggregate", "tuple", "struct":
		t = NativeValuePointer
	case "unsigned", "uint", "uint8", "uint16", "uint32", "uint64", "uintptr":
		t = NativeValueUnsigned
	default:
		t = NativeValueInteger
	}
	s.emit("mov", xm(xRBP, tag), xi(int64(t)))
	return tag, nil
}
func (s *x64Selector) mark(l string) { s.emit("label", xl(l), x64Operand{}) }
func (s *x64Selector) slot() int     { s.allocated++; return -8 * s.allocated }
func projectFunctionLabel(name string) string {
	h := stableBytesHash([]byte(name))
	return "__project_fn_" + h[:24]
}

func projectGlobalLabel(name string) string {
	h := stableBytesHash([]byte(name))
	return "__project_data_" + h[:24]
}

func (s *x64Selector) projectSymbolName(name string) string {
	if sourceName := s.projectNames[name]; sourceName != "" {
		name = sourceName
	}
	if s.projectNamespace != "" && !strings.Contains(name, ".") {
		return s.projectNamespace + "." + name
	}
	return name
}

// resolveProjectFunctionName closes the common transport case where a unit
// carries an unqualified reference but the declaration plane stores the
// canonical package-qualified identity.  Only an unambiguous index match is
// accepted; ambiguity remains fail-closed rather than selecting arbitrarily.
func (s *x64Selector) resolveProjectFunctionName(name string) string {
	name = s.projectSymbolName(name)
	if s.projectIndex == nil || name == "" {
		return name
	}
	if _, ok := s.projectIndex.Symbols[projectFunctionLabel(name)]; ok {
		return name
	}
	var match string
	for _, symbol := range s.projectIndex.Symbols {
		if symbol.Name != name && !strings.HasSuffix(symbol.QualifiedName, "."+name) {
			continue
		}
		if match != "" && match != symbol.QualifiedName {
			return name
		}
		match = symbol.QualifiedName
	}
	if match != "" {
		return match
	}
	return name
}

// resolveProjectGlobalName applies the same canonical identity rule to data
// symbols as to functions.  A semantic transport may qualify a global at the
// use site while its defining unit retains only the source spelling; an
// unambiguous project-index match is the sole allowed repair.
func (s *x64Selector) resolveProjectGlobalName(name string) string {
	name = s.projectSymbolName(name)
	if s.projectIndex == nil || name == "" {
		return name
	}
	if _, ok := s.projectIndex.Symbols[projectGlobalLabel(name)]; ok {
		return name
	}
	var match string
	for _, symbol := range s.projectIndex.Symbols {
		if !strings.HasPrefix(symbol.ID, "__project_data_") || symbol.Name == "" {
			continue
		}
		if symbol.Name != name && !strings.HasSuffix(symbol.QualifiedName, "."+name) {
			continue
		}
		if match != "" && match != symbol.QualifiedName {
			return name
		}
		match = symbol.QualifiedName
	}
	if match != "" {
		return match
	}
	return name
}

func (s *x64Selector) uniqueProjectFunctionName(exclude string) string {
	if s.projectIndex == nil {
		return ""
	}
	candidate := ""
	for _, symbol := range s.projectIndex.Symbols {
		if s.projectUnitID != "" && symbol.UnitID != s.projectUnitID {
			continue
		}
		if symbol.Name == "" || symbol.Name == exclude || symbol.Linkage == "data" {
			continue
		}
		if candidate != "" && candidate != symbol.Name {
			return ""
		}
		candidate = symbol.Name
	}
	return candidate
}

func (s *x64Selector) isExternalCallName(name string) bool {
	if name == "" {
		return false
	}
	// In project mode a call may become a project relocation only when the
	// canonical index proves the declaration.  Every other unresolved name
	// (qualified imports and partially recovered/unqualified references alike)
	// follows the opaque external-call contract.  This prevents dangling
	// __project_fn_* relocations while keeping genuine cross-unit calls intact.
	if s.projectIndex != nil {
		resolved := s.resolveProjectFunctionName(name)
		if _, exists := s.projectIndex.Symbols[projectFunctionLabel(resolved)]; exists {
			return false
		}
	}
	return s.projectMode
}

// isExternalQualifiedValue identifies a qualified value whose declaring
// package is outside the semantic project.  It deliberately shares the
// package/import identity table with external calls; no package name or
// library constant is hard-coded here.  The native selector can therefore
// apply the same explicit opaque-value contract to imported constants and
// variables instead of treating every dotted identifier as a lexical binding.
func (s *x64Selector) isExternalQualifiedValue(name string) bool {
	// At project compile time standard-library/package declarations are not
	// materialized as units. A qualified value therefore remains an external
	// import identity even when the import index has no executable package
	// record. Member accesses are resolved immediately before this check.
	return strings.Contains(name, ".")
}

func (s *x64Selector) functionLabel(name string, nodeID int) string {
	if s.projectMode && name != "" {
		// Frontend-generated native_var_* names are lexical temporaries, not
		// project declarations. Keep their labels unit-local so identically
		// numbered temporaries from different units cannot collide in the global
		// symbol index.
		// A native_function_* name is local only when the project index has no
		// declaration for it. Semantic transports use this generated spelling for
		// real exported functions as well; in that case the project index is the
		// canonical identity and calls from another unit must hash the same name.
		declaredProjectName := false
		// function_entry_bindings is the canonical transport alias map.  Its
		// reverse direction (generated native_function_N -> source declaration)
		// identifies exported functions even when the node itself carries only
		// the generated binding spelling.
		if _, ok := s.projectNames[name]; ok {
			declaredProjectName = true
		}
		if s.projectIndex != nil {
			for _, symbol := range s.projectIndex.Symbols {
				if symbol.Name == name || symbol.QualifiedName == name || strings.HasSuffix(symbol.QualifiedName, "."+name) {
					declaredProjectName = true
					break
				}
			}
		}
		if (strings.HasPrefix(name, "native_") && !declaredProjectName) || strings.HasPrefix(name, "__uast_function_") {
			return s.label()
		}
		sourceName := name
		if resolved := s.projectNames[name]; resolved != "" {
			sourceName = resolved
		}
		qualifiedName := s.projectSymbolName(name)
		label := projectFunctionLabel(qualifiedName)
		if s.p.ProjectSymbols == nil {
			s.p.ProjectSymbols = map[string]ProjectSymbol{}
		}
		symbol := ProjectSymbol{ID: label, NodeID: nodeID, Name: sourceName, QualifiedName: qualifiedName, Visibility: "project", Linkage: "internal"}
		if nodeID >= 0 && nodeID < len(s.g.common) {
			symbol.Type = s.g.common[nodeID].Type
		}
		s.p.ProjectSymbols[label] = symbol
		return label
	}
	return s.label()
}

func (s *x64Selector) registerGlobal(name string, nodeID int, valueType SemanticType) string {
	qualifiedName := s.projectSymbolName(name)
	if s.projectMode {
		qualifiedName = s.resolveProjectGlobalName(name)
	}
	label := "uast_global_" + stableBytesHash([]byte(name))[:24]
	generatedLocal := strings.HasPrefix(name, "native_")
	if s.projectMode && !generatedLocal {
		label = projectGlobalLabel(qualifiedName)
	}
	if s.globals == nil {
		s.globals = map[string]string{}
	}
	s.globals[name] = label
	s.globals[qualifiedName] = label
	if _, ok := s.p.Data[label]; !ok {
		s.p.Data[label] = make([]byte, 8)
	}
	if s.projectMode && !generatedLocal {
		s.p.ProjectSymbols[label] = ProjectSymbol{ID: label, NodeID: nodeID, Name: name, QualifiedName: qualifiedName, Visibility: "project", Linkage: "internal", Type: valueType}
	}
	return label
}

// filterGlobalCaptures keeps project/module globals on their canonical data
// symbol.  The source-level name is visible inside every function, but it is
// not a lexical closure capture and must never consume a hidden ABI parameter
// slot.  Treating a global as a capture makes a zero-argument function read
// the caller's RCX instead of the linked global cell.
func (s *x64Selector) filterGlobalCaptures(captures []string) []string {
	if len(captures) == 0 {
		return captures
	}
	out := captures[:0]
	for _, name := range captures {
		if _, global := s.globals[name]; global {
			continue
		}
		// native_var_* identifiers are frontend-generated temporaries. They are
		// scoped to the source function/closure and are not stable project
		// symbols; retaining one as a cross-function capture would manufacture a
		// hidden ABI parameter when the canonical graph has already lost its
		// lexical declaration edge.
		if strings.HasPrefix(name, "native_") {
			continue
		}
		// Double-underscore names are canonical runtime/builtin operations
		// (for example __index_set and __make_float64), not lexical values.
		if strings.HasPrefix(name, "__") {
			continue
		}
		// Qualified identifiers belonging to an imported package are call-site
		// symbols, not lexical captures.  Keeping them in the hidden capture
		// list turns a normal external reference such as strings.HasSuffix into a
		// bogus closure ABI parameter and makes otherwise valid project units
		// fail before their external/import contract is checked.
		if s.isExternalCallName(name) || strings.Contains(name, ".") {
			continue
		}
		out = append(out, name)
	}
	return out
}

func (s *x64Selector) globalLabel(name string) (string, bool) {
	label, ok := s.globals[name]
	if !ok && s.projectMode && name != "" {
		qualified := s.resolveProjectGlobalName(name)
		candidate := projectGlobalLabel(qualified)
		// Unknown identifiers are not implicit project globals.  Only an
		// explicit data symbol in the project index may create a relocation;
		// otherwise the caller must use its normal lexical/external resolution.
		if s.projectIndex != nil {
			if symbol, exists := s.projectIndex.Symbols[candidate]; exists && strings.HasPrefix(symbol.ID, "__project_data_") {
				label, ok = candidate, true
			}
		}
	}
	return label, ok
}
func nativeBoolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func nativeAggregateType(t SemanticType) bool {
	switch t.Kind {
	case "slice", "array", "tuple", "product", "struct", "aggregate":
		return true
	default:
		return false
	}
}

// semanticTypeKnownForABI reports whether a transported function type carries
// enough information to constrain a call.  Frontends may preserve a function
// declaration while type checking was unavailable; those declarations use an
// "unknown" kind or the sentinel name "invalid type".  Such metadata is
// evidence, but it is not a complete ABI contract and must not reject an
// otherwise valid project call on an invented arity.
func semanticTypeKnownForABI(t SemanticType) bool {
	if strings.EqualFold(strings.TrimSpace(t.Name), "invalid type") {
		return false
	}
	if t.Kind == "" || t.Kind == "unknown" {
		return false
	}
	if t.Element != nil && !semanticTypeKnownForABI(*t.Element) {
		return false
	}
	if t.Key != nil && !semanticTypeKnownForABI(*t.Key) {
		return false
	}
	if t.Value != nil && !semanticTypeKnownForABI(*t.Value) {
		return false
	}
	for _, p := range t.Parameters {
		if !semanticTypeKnownForABI(p) {
			return false
		}
	}
	if t.Result != nil && !semanticTypeKnownForABI(*t.Result) {
		return false
	}
	return true
}

// The canonical graph selects one product ABI for all aggregate results.
func (s *x64Selector) functionReturnsAggregate(id int) bool {
	if id < 0 {
		return false
	}
	t := s.g.common[id].Type
	if nativeAggregateType(t) || t.Result != nil && nativeAggregateType(*t.Result) {
		return true
	}
	seen := map[int]bool{}
	var scan func(int) bool
	scan = func(n int) bool {
		if seen[n] {
			return false
		}
		seen[n] = true
		c := s.g.common[n]
		if c.Kind == "function" && n != id {
			return false
		}
		if c.Kind == "return" {
			v, ok, err := s.g.one(n, "expression", false)
			if err == nil && ok {
				vc := s.g.common[v]
				return nativeAggregateType(vc.Type) || vc.Kind == "aggregate" || vc.Kind == "tuple"
			}
		}
		for _, roles := range s.g.children[n] {
			for _, child := range roles {
				if scan(child.ID) {
					return true
				}
			}
		}
		return false
	}
	return scan(id)
}

func (s *x64Selector) functionVariadic(id int) bool {
	if id < 0 || s.g == nil {
		return false
	}
	if value, ok := s.g.common[id].Attributes["variadic"].(bool); ok && value {
		return true
	}
	// Function declaration attributes are not part of every canonical
	// projection. The parameter operation is the shared structural contract
	// and survives JSON/SE graph transport, so use it as the authoritative
	// variadic marker when the declaration attribute is absent.
	for _, parameter := range s.g.many(id, "parameter") {
		mode := s.g.common[parameter.ID].Operation.ParameterMode
		if mode == "variadic" || mode == "variadic_positional" || mode == "variadic_keyword" {
			return true
		}
	}
	return false
}

func (s *x64Selector) functionAggregateLength(id int) (int, bool) {
	if id < 0 {
		return 0, false
	}
	seen := map[int]bool{}
	var scan func(int) (int, bool)
	scan = func(n int) (int, bool) {
		if seen[n] {
			return 0, false
		}
		seen[n] = true
		c := s.g.common[n]
		if c.Kind == "function" && n != id {
			return 0, false
		}
		if c.Kind == "return" {
			v, ok, err := s.g.one(n, "expression", false)
			if err != nil || !ok {
				return 0, false
			}
			vc := s.g.common[v]
			if vc.Kind != "aggregate" && vc.Kind != "tuple" {
				return 0, false
			}
			return len(s.g.many(v, "member")) + len(s.g.many(v, "element")) + len(s.g.many(v, "argument")), true
		}
		for _, roles := range s.g.children[n] {
			for _, child := range roles {
				if length, ok := scan(child.ID); ok {
					return length, true
				}
			}
		}
		return 0, false
	}
	return scan(id)
}
func (s *x64Selector) child(id int, roles ...string) (int, error) {
	for _, role := range roles {
		n, ok, err := s.g.one(id, role, false)
		if err != nil {
			return 0, err
		}
		if ok {
			return n, nil
		}
	}
	return 0, fmt.Errorf("native node %d missing operand %v", id, roles)
}
func (s *x64Selector) binding(id int) string {
	c := s.g.common[id]
	if c.Binding != nil {
		return fmt.Sprintf("b%d", *c.Binding)
	}
	return c.Name
}

func selectX64(g *uastExecutionGraph, entry string, projectMode bool, projectIndex *SemanticProjectIndex, projectUnitID string, projectInitializers []ExecutableFunctionRef) (x64Program, error) {
	s := &x64Selector{g: g, functions: map[string]int{}, functionLabels: map[string]string{}, functionValues: map[string]bool{}, functionValueTargets: map[string]int{}, functionCaptures: map[int][]string{}, emittedFunctions: map[int]bool{}, projectMode: projectMode, projectIndex: projectIndex, projectUnitID: projectUnitID, projectNames: map[string]string{}, globals: map[string]string{}, externalPackages: map[string]bool{}, projectInitializers: append([]ExecutableFunctionRef(nil), projectInitializers...)}
	s.p.Data = map[string][]byte{}
	s.p.ProjectSymbols = map[string]ProjectSymbol{}
	for _, module := range g.document.Origin.Modules {
		if module != "" {
			base := module
			if slash := strings.LastIndexByte(base, '/'); slash >= 0 {
				base = base[slash+1:]
			}
			s.externalPackages[base] = true
			s.externalPackages[module] = true
		}
	}
	if projectMode {
		var aliases map[string]string
		encoded, _ := json.Marshal(g.document.Extensions["function_entry_bindings"])
		_ = json.Unmarshal(encoded, &aliases)
		for sourceName, canonicalName := range aliases {
			if canonicalName != "" && sourceName != "" {
				s.projectNames[canonicalName] = sourceName
			}
		}
		var packageContext map[string]any
		encoded, _ = json.Marshal(g.document.Extensions["native_package_context"])
		_ = json.Unmarshal(encoded, &packageContext)
		if packageName, ok := packageContext["package"].(string); ok {
			s.projectNamespace = packageName
		}
		// Semantic-only SE imports retain the package identity in document
		// metadata even when the optional extension plane is compacted away.
		// Project labels must use the same qualified identity in both transport
		// forms or cross-unit relocations address different symbols.
		if s.projectNamespace == "" && g.document != nil && g.document.Metadata != nil {
			s.projectNamespace = g.document.Metadata["package"]
		}
	}
	typeJSON, _ := json.Marshal(g.document.Extensions["native_binding_types"])
	_ = json.Unmarshal(typeJSON, &s.bindingTypes)
	// Discover module-level declarations only; lexical closures require an
	// environment representation and must not silently become global functions.
	roots := g.many(g.root, "statement")
	for _, item := range roots {
		c := g.common[item.ID]
		kind := strings.ToLower(c.Kind)
		isGlobal := kind == "assign" || kind == "globaldecl" || kind == "variabledecl" || kind == "vardecl" || kind == "constdecl" || kind == "constantdecl"
		if !isGlobal || c.Name == "" {
			continue
		}
		var rhs int
		var ok bool
		if kind == "assign" {
			var childErr error
			rhs, ok, childErr = g.one(item.ID, "expression", false)
			if childErr != nil {
				continue
			}
			if ok && g.common[rhs].Kind == "function" {
				continue
			}
		}
		valueType := c.Type
		if (valueType.Kind == "" || valueType.Kind == "unknown") && ok {
			valueType = g.common[rhs].Type
		}
		s.registerGlobal(c.Name, item.ID, valueType)
	}
	for _, item := range roots {
		c := g.common[item.ID]
		if c.Kind == "function" && expressionOwnedByStructuredParent(g, item.ID) {
			continue
		}
		id := item.ID
		name := c.Name
		if c.Kind == "assign" {
			v, e := s.child(id, "expression", "value")
			if e != nil {
				return s.p, e
			}
			id = v
			c = g.common[id]
		}
		if c.Kind == "function" {
			if name == "" {
				name = c.Operation.FunctionBinding
			}
			// Semantic-only transports may preserve a generated binding such as
			// native_function_0 instead of the source declaration name.  At the
			// project boundary recover the declaration from the compact index so
			// the body receives its canonical cross-unit label.
			if projectMode && strings.HasPrefix(name, "native_function_") {
				if entry != "" && s.projectIndex != nil {
					for _, symbol := range s.projectIndex.Symbols {
						if symbol.Name == entry {
							name = entry
							break
						}
					}
				}
				if strings.HasPrefix(name, "native_function_") {
					if recovered := s.uniqueProjectFunctionName(entry); recovered != "" {
						name = recovered
					}
				}
			}
			// Native frontends represent a named function declaration as an
			// assignment whose binding is carried by the function expression.
			// The assignment node itself may therefore have no `name` field after
			// canonical projection.  Resolve that binding before rejecting the
			// declaration; otherwise every non-entry helper function is reported
			// as an implementation gap even though its canonical call edges are
			// complete.
			if name == "" {
				name = c.Operation.FunctionBinding
			}
			if name == "" {
				name = c.Name
			}
			if name == "" {
				// Root-level anonymous functions use the same stable UAST identity
				// as functions discovered in nested expression positions. This keeps
				// both discovery passes on one closure/ABI naming contract.
				name = s.uniqueProjectFunctionName(entry)
				if name == "" {
					name = fmt.Sprintf("__uast_function_%d", id)
				}
			}
			s.functions[name] = id
			s.functionLabels[name] = s.functionLabel(name, id)
			s.functionCaptures[id] = s.filterGlobalCaptures(uastFunctionCaptureNames(g, id, s.functions))
		}
	}
	// Discover anonymous, non-capturing function values anywhere in the
	// canonical graph. Their assignment binding is the function-value identity;
	// captured functions are deliberately deferred until an environment layout
	// exists and must never be compiled as if they were globals.
	// Build the parent-name index once. Walking the complete child graph for
	// every function is quadratic on merged member graphs and can turn a valid
	// native compilation into an apparent hang.
	functionValueNames := map[int]string{}
	for parent, roles := range g.children {
		if g.common[parent].Kind != "assign" {
			continue
		}
		for _, child := range roles["expression"] {
			if g.common[child.ID].Kind == "function" && g.common[parent].Name != "" {
				functionValueNames[child.ID] = g.common[parent].Name
			}
		}
	}
	functionIDs := make([]int, 0)
	knownFunctionIDs := map[int]bool{}
	for _, id := range s.functions {
		knownFunctionIDs[id] = true
	}
	for id, c := range g.common {
		if c.Kind == "function" {
			functionIDs = append(functionIDs, id)
		}
	}
	sort.Ints(functionIDs)
	for _, id := range functionIDs {
		if knownFunctionIDs[id] {
			continue
		}
		name := functionValueNames[id]
		if name == "" {
			// Anonymous function values still need a native label. The stable
			// canonical node ID is the universal binding identity and avoids
			// inventing source-language names.
			name = fmt.Sprintf("__uast_function_%d", id)
		}
		params := make([]string, 0)
		for _, parameter := range g.many(id, "parameter") {
			params = append(params, g.common[parameter.ID].Name)
		}
		s.functions[name] = id
		knownFunctionIDs[id] = true
		s.functionLabels[name] = s.functionLabel(name, id)
		s.functionCaptures[id] = s.filterGlobalCaptures(uastFunctionCaptureNames(g, id, s.functions))
	}
	if entry == "" {
		if _, ok := s.functions["main"]; ok {
			entry = "main"
		}
	}
	processEntryName := entry
	if entry != "" {
		var aliases map[string]string
		encoded, _ := json.Marshal(g.document.Extensions["function_entry_bindings"])
		_ = json.Unmarshal(encoded, &aliases)
		if canonical := aliases[entry]; canonical != "" {
			entry = canonical
		}
		// Generated bindings are transport identities, not project entry names.
		// Resolve them back to the explicit source-level entry recorded in the
		// project index before selecting the function body.
		if strings.HasPrefix(entry, "native_function_") && s.projectIndex != nil {
			for _, symbol := range s.projectIndex.Symbols {
				if symbol.Name == processEntryName {
					entry = processEntryName
					break
				}
			}
		}
	}
	if entry != "" {
		if _, ok := s.functions[entry]; !ok {
			return s.p, fmt.Errorf("native entry %q not found", entry)
		}
	}
	// Entry and every selected function use the same frame builder.
	if err := s.function("native_entry", -1, func() error {
		if err := s.statement(g.root); err != nil {
			return err
		}
		for _, initializer := range s.projectInitializers {
			if initializer.ID == "" {
				return fmt.Errorf("project initializer has no canonical symbol id")
			}
			s.emit("call", xl(initializer.ID), x64Operand{})
		}
		if entry != "" {
			entryID := s.functions[entry]
			parameters := g.many(entryID, "parameter")
			// A library function selected as a process entry still needs a
			// deterministic ABI invocation. Materialize zero values in the Win64
			// argument registers/stack slots; never call with uninitialized inputs.
			if s.functionReturnsAggregate(entryID) {
				s.emit("mov", xr(xRCX), xr(xRSP))
			}
			for i, parameter := range parameters {
				argumentIndex := i + nativeBoolInt(s.functionReturnsAggregate(entryID))
				if argumentIndex < 4 {
					if s.isFloat(parameter.ID) {
						s.emit("xor", xr(xRAX), xr(xRAX))
						s.emit("mov_to_xmm", xr(byte(argumentIndex)), xr(xRAX))
					} else {
						s.emit("mov", xr(win64IntegerArguments[argumentIndex]), xi(0))
					}
				} else {
					s.emit("mov", xm(xRSP, 48+(argumentIndex-4)*8), xi(0))
				}
			}
			s.emit("call", xl(s.functionLabels[entry]), x64Operand{})
			// A language-defined main has a void process contract and therefore
			// returns success. An explicitly selected library entry exposes its
			// scalar return as the process result, which also provides an executable
			// cross-unit linkage witness without target-specific output calls.
			// Preserve a value returned by an explicitly selected semantic entry.
			// Void language mains already leave the canonical zero initialized by
			// their function frame, while typed entries (for example a Semantic
			// witness returning int32) must propagate RAX to ExitProcess.  The
			// previous unconditional reset silently discarded every non-void result
			// after a successful internal call.
		} else {
			s.emit("mov", xr(xRAX), xi(0))
		}
		// A PE entry point has no caller to return to.  Terminate through the
		// Win32 process contract so the generated image cannot fall through into
		// padding or attempt an undefined return.  RCX is the Win64 first integer
		// argument and carries the normalized process exit code.
		s.emit("mov", xr(xRCX), xr(xRAX))
		s.p.Imports = append(s.p.Imports, pe64ImportSpec{DLL: "kernel32.dll", Name: "ExitProcess"})
		s.emit("call_iat", xl("__iat_kernel32_ExitProcess"), x64Operand{})
		return nil
	}); err != nil {
		return s.p, err
	}
	names := make([]string, 0, len(s.functions))
	for n := range s.functions {
		names = append(names, n)
	}
	sort.Strings(names)
	// Native emission only needs functions reachable through canonical call
	// edges. Keeping dead declarations out of the image is semantics-preserving
	// and prevents an unused helper's foreign call from blocking an otherwise
	// executable program. The function registry remains complete so any live
	// direct or function-value call still resolves through the same ABI map.
	referenced := map[int]bool{}
	for id, c := range g.common {
		if c.Kind != "call" {
			continue
		}
		callee, ok, _ := g.one(id, "value", false)
		if !ok {
			callee, ok, _ = g.one(id, "callee", false)
		}
		if !ok {
			continue
		}
		if g.common[callee].Kind == "function" {
			referenced[callee] = true
			continue
		}
		if g.common[callee].Kind == "identifier" {
			if target, exists := s.functions[g.common[callee].Name]; exists {
				referenced[target] = true
				continue
			}
			if target := s.functionValueTargets[s.binding(callee)]; target != 0 {
				referenced[target] = true
				continue
			}
			if target := s.functionValueTargets[g.common[callee].Name]; target != 0 {
				referenced[target] = true
			}
		}
	}
	// An explicitly selected library entry may call a helper through a
	// canonical function-value edge that is not represented by a direct call
	// relation. Emit the complete module function set in that mode so every
	// allocated function label has a concrete body and fixups cannot dangle.
	if entry != "" {
		for _, id := range s.functions {
			referenced[id] = true
		}
	}
	// A linked distribution is a complete program graph, not a single source
	// file with dead declarations. Function values may be materialized through
	// identifier/assignment edges that are not represented as direct call
	// nodes. Emit the complete discovered function closure so every relocatable
	// function label has a concrete body and no lea/call fixup can dangle.
	if isLinkedUASTGraph(g.document) {
		for _, id := range s.functions {
			referenced[id] = true
		}
	}
	for _, name := range names {
		id := s.functions[name]
		// Emit every discovered function body.  Canonical semantic modules may
		// reference function values through metadata/aggregate edges that are not
		// represented by a direct call node; retaining all bodies guarantees that
		// every generated label has a concrete target and keeps linked .se modules
		// hermetic.  Dead code elimination belongs to a later image pass.
		s.emittedFunctions[id] = true
		if err := s.function(s.functionLabels[name], id, func() error {
			body, e := s.child(id, "body")
			if e != nil {
				return e
			}
			return s.statement(body)
		}); err != nil {
			return s.p, fmt.Errorf("native function %s: %w", name, err)
		}
	}
	return s.p, nil
}

// uastFunctionCaptureNames derives the lexical environment from canonical
// identifier/binding structure. It does not inspect source spelling. The
// native ABI passes these values as hidden leading parameters; lifetime
// promotion is handled separately by the closure-value contract.
func uastFunctionCaptureNames(g *uastExecutionGraph, functionID int, functions map[string]int) []string {
	allowed := map[string]bool{"TRUE": true, "FALSE": true, "T": true, "F": true, "NULL": true, "NA": true, "NaN": true, "Inf": true, "pi": true, "length": true}
	for _, p := range g.many(functionID, "parameter") {
		allowed[g.common[p.ID].Name] = true
	}
	body, ok, _ := g.one(functionID, "body", false)
	if !ok {
		return nil
	}
	var collect func(int)
	collect = func(id int) {
		c := g.common[id]
		// Names introduced by a function body are lexical locals too.  The
		// canonical graph represents range/iteration bindings as a named `for`
		// node (and some frontends use an explicit binding/decl node), so they
		// must be in the local environment before identifier capture analysis.
		// Treating them as captures produces a false hidden ABI parameter and
		// later PROJECT_SEMANTIC_FACT_MISSING errors when the closure is emitted.
		if (c.Kind == "assign" || c.Kind == "for" || c.Kind == "range" ||
			c.Kind == "binding" || c.Kind == "var" || c.Kind == "let" ||
			c.Kind == "const" || c.Kind == "decl") && c.Name != "" {
			allowed[c.Name] = true
		}
		for _, roles := range g.children[id] {
			for _, child := range roles {
				collect(child.ID)
			}
		}
	}
	collect(body)
	seen := map[string]bool{}
	var captures []string
	var scan func(int)
	scan = func(id int) {
		c := g.common[id]
		if c.Kind == "identifier" && c.Name != "" && !allowed[c.Name] {
			if _, isFunctionName := functions[c.Name]; isFunctionName {
				return
			}
			if seen[c.Name] {
				return
			}
			seen[c.Name] = true
			captures = append(captures, c.Name)
		}
		for _, roles := range g.children[id] {
			for _, child := range roles {
				scan(child.ID)
			}
		}
	}
	scan(body)
	sort.Strings(captures)
	return captures
}

func (s *x64Selector) function(label string, id int, body func() error) error {
	s.slots = map[string]int{}
	s.allocated = 0
	s.outgoing = 32
	s.returnLabel = s.label()
	s.loops = nil
	s.aggregateReturn = id >= 0 && s.functionReturnsAggregate(id)
	s.aggregateReturnSlot = 0
	s.floatReturn = id >= 0 && !s.aggregateReturn && s.functionFloat(id)
	s.mark(label)
	s.emit("push", xr(xRBP), x64Operand{})
	s.emit("mov", xr(xRBP), xr(xRSP))
	frameAt := len(s.p.Instructions)
	s.emit("sub_sp", xi(0), x64Operand{})
	if id >= 0 {
		if s.aggregateReturn {
			s.aggregateReturnSlot = s.slot()
			s.emit("mov", xm(xRBP, s.aggregateReturnSlot), xr(xRCX))
		}
		captures := s.functionCaptures[id]
		for i, name := range captures {
			argumentIndex := i + nativeBoolInt(s.aggregateReturn)
			slot := s.slot()
			s.slots[name] = slot
			if argumentIndex < 4 {
				s.emit("mov", xm(xRBP, slot), xr(win64IntegerArguments[argumentIndex]))
			} else {
				s.emit("mov", xr(xRAX), xm(xRBP, 48+(argumentIndex-4)*8))
				s.emit("mov", xm(xRBP, slot), xr(xRAX))
			}
		}
		for i, p := range s.g.many(id, "parameter") {
			argumentIndex := i + len(captures) + nativeBoolInt(s.aggregateReturn)
			slot := s.slot()
			bindingName := s.binding(p.ID)
			parameterName := s.g.common[p.ID].Name
			s.slots[bindingName] = slot
			s.slots[parameterName] = slot
			// Keep the structured parameter type available to the generic member
			// projection below.  Qualified field references such as value.Field
			// are represented by the canonical frontend as one identifier; the
			// type contract, rather than source spelling, determines the field
			// offset and aggregate representation.
			if s.bindingTypes == nil {
				s.bindingTypes = map[string]SemanticType{}
			}
			s.bindingTypes[bindingName] = s.g.common[p.ID].Type
			if parameterName != "" {
				s.bindingTypes[parameterName] = s.g.common[p.ID].Type
			}
			if argumentIndex < 4 {
				if s.isFloat(p.ID) {
					s.emit("mov_from_xmm", xr(xRAX), xr(byte(argumentIndex)))
					s.emit("mov", xm(xRBP, slot), xr(xRAX))
				} else {
					s.emit("mov", xm(xRBP, slot), xr(win64IntegerArguments[argumentIndex]))
				}
			} else {
				s.emit("mov", xr(xRAX), xm(xRBP, 48+(argumentIndex-4)*8))
				s.emit("mov", xm(xRBP, slot), xr(xRAX))
			}
		}
	}
	s.emit("mov", xr(xRAX), xi(0))
	if err := body(); err != nil {
		return err
	}
	s.mark(s.returnLabel)
	if s.floatReturn {
		s.emit("mov_to_xmm", xr(0), xr(xRAX))
	}
	s.emit("mov", xr(xRSP), xr(xRBP))
	s.emit("pop", xr(xRBP), x64Operand{})
	s.emit("ret", x64Operand{}, x64Operand{})
	end := s.label()
	s.mark(end)
	frame := machineAlign(s.allocated*8+s.outgoing, 16)
	// Windows commits stack pages lazily. Probe every 4096-byte decrement so
	// large, valid UAST activation records preserve the same stack contract as
	// small frames instead of being rejected at an arbitrary size threshold.
	probe := make([]x64Instruction, 0, frame/4096*2+1)
	remaining := frame
	for remaining >= 4096 {
		probe = append(probe,
			x64Instruction{"sub_sp", xi(4096), x64Operand{}},
			x64Instruction{"mov", xm(xRSP, 0), xr(xRAX)},
		)
		remaining -= 4096
	}
	if remaining > 0 {
		probe = append(probe, x64Instruction{"sub_sp", xi(int64(remaining)), x64Operand{}})
	}
	if frame < 4096 {
		probe = []x64Instruction{{"sub_sp", xi(int64(frame)), x64Operand{}}}
	}
	prefix := append([]x64Instruction(nil), s.p.Instructions[:frameAt]...)
	suffix := append([]x64Instruction(nil), s.p.Instructions[frameAt+1:]...)
	s.p.Instructions = append(prefix, probe...)
	s.p.Instructions = append(s.p.Instructions, suffix...)
	s.p.Functions = append(s.p.Functions, x64Function{label, end, frame})
	return nil
}

func (s *x64Selector) statement(id int) error {
	c := s.g.common[id]
	switch c.Kind {
	case "module", "type", "annotation", "generic":
		// Module/type/annotation declarations are compile-time metadata. Their
		// canonical facts have already been validated and linked before native
		// selection; they do not produce runtime instructions.
		return nil
	case "block":
		for _, item := range s.g.many(id, "statement") {
			if err := s.statement(item.ID); err != nil {
				return err
			}
		}
		return nil
	case "assign":
		rhs, e := s.child(id, "expression", "value")
		if e != nil {
			return e
		}
		if target, ok, e := s.g.one(id, "target", false); e != nil {
			return e
		} else if ok {
			return s.writePlace(target, rhs)
		}
		if s.g.common[rhs].Kind == "function" {
			if _, ok := s.functions[c.Name]; ok {
				return nil
			}
			if s.projectMode {
				// Semantic-only transports may retain the generated assignment
				// binding while the declaration has already been canonicalized under
				// its project name. The function body is emitted from that canonical
				// registry entry; the assignment itself has no machine side effect.
				return nil
			}
			return fmt.Errorf("native closure environment unavailable")
		}
		if c.Name == "" {
			return fmt.Errorf("native assignment binding unavailable at %d", id)
		}
		if e = s.expression(rhs); e != nil {
			return e
		}
		if s.g.common[rhs].Kind == "aggregate" || s.g.common[rhs].Kind == "tuple" {
			if e = s.materializeAggregate(rhs); e != nil {
				return e
			}
		}
		if label, global := s.globals[c.Name]; global {
			s.emit("lea", xr(xR11), xl(label))
			s.emit("mov", xm(xR11, 0), xr(xRAX))
			return nil
		}
		key := s.binding(id)
		if rc := s.g.common[rhs]; rc.Kind == "identifier" {
			if target, ok := s.functions[rc.Name]; ok {
				s.functionValues[key] = true
				s.functionValues[c.Name] = true
				s.functionValueTargets[key] = target
				s.functionValueTargets[c.Name] = target
			}
		}
		slot, ok := s.slots[key]
		if !ok {
			slot = s.slot()
			s.slots[key] = slot
			s.slots[c.Name] = slot
		}
		s.emit("mov", xm(xRBP, slot), xr(xRAX))
		return nil
	case "function":
		return nil
	case "expression":
		if async, ok := c.Attributes["go_async"].(bool); ok && async {
			// The minimal native task contract is cooperative: arguments and the
			// callable are lowered through the normal exactly-once path, and the
			// task is completed before the enclosing native activation continues.
			// This keeps the native image executable without silently inventing an
			// OS thread ABI; true parallel scheduling is a separate target contract.
			v, e := s.child(id, "expression")
			if e != nil {
				return e
			}
			if e = s.expression(v); e != nil {
				return e
			}
			s.emit("mov", xr(xRAX), xi(0))
			return nil
		}
		if strings.HasPrefix(strings.ToLower(c.Operation.Operator), "unsupported.") {
			if _, ok, _ := s.g.one(id, "expression", false); !ok {
				// Unsupported markers without a canonical operand are evidence
				// nodes, not executable expressions. Preserve the marker in the
				// UAST while keeping it out of machine instruction selection.
				return nil
			}
		}
		if s.g.document != nil && s.g.document.Metadata != nil && s.g.document.Metadata["frontend_route"] == "CANONICALIZE_ONLY" && c.Operation.Operator == "" {
			if _, ok, _ := s.g.one(id, "expression", false); !ok {
				return nil
			}
		}
		v, e := s.child(id, "expression")
		if e != nil {
			return e
		}
		return s.expression(v)
	case "return":
		v, ok, e := s.g.one(id, "expression", false)
		if e != nil {
			return e
		}
		if ok {
			if e = s.expression(v); e != nil {
				return e
			}
			if s.aggregateReturn {
				// The aggregate expression is a temporary in this frame. Copy it
				// into the caller-owned result buffer before returning.
				sourceSlot := s.slot()
				s.emit("mov", xm(xRBP, sourceSlot), xr(xRAX))
				s.emit("mov", xr(xR9), xm(xRBP, s.aggregateReturnSlot))
				s.emit("mov", xr(xRAX), xm(xRBP, sourceSlot))
				s.emit("mov", xr(xRDX), xm(xRAX, 0))
				s.emit("mov", xm(xR9, 0), xr(xRDX))
				loop, done := s.label(), s.label()
				s.emit("mov", xr(xR10), xi(0))
				s.mark(loop)
				s.emit("cmp", xr(xR10), xr(xRDX))
				s.emit("jae", xl(done), x64Operand{})
				s.emit("mov", xr(xR11), xmIndexed(xRAX, xR10, 8, 8))
				s.emit("mov", xmIndexed(xR9, xR10, 8, 8), xr(xR11))
				s.emit("add", xr(xR10), xi(1))
				s.emit("jmp", xl(loop), x64Operand{})
				s.mark(done)
				s.emit("mov", xr(xRAX), xr(xR9))
			}
		} else {
			if s.aggregateReturn {
				// A bare aggregate return denotes the current zero product when no
				// named result expression is present. Materialize an empty product
				// in the caller-owned result buffer instead of rejecting the ABI.
				s.emit("mov", xr(xR9), xm(xRBP, s.aggregateReturnSlot))
				s.emit("mov", xm(xR9, 0), xi(0))
				s.emit("mov", xr(xRAX), xr(xR9))
			} else {
				s.emit("mov", xr(xRAX), xi(0))
			}
		}
		s.emit("jmp", xl(s.returnLabel), x64Operand{})
		return nil
	case "if", "ifstmt":
		cond, e := s.child(id, "condition")
		if e != nil {
			if s.projectMode || s.g.document == nil {
				return e
			}
			// Compatibility projections may omit an optional condition edge.
			// Use the contract's deterministic false default so the branch is
			// executable without inventing an operand evaluation.
			s.emit("mov", xr(xRAX), xi(0))
		} else if e = s.expression(cond); e != nil {
			return e
		}
		other, end := s.label(), s.label()
		s.emit("test", xr(xRAX), xr(xRAX))
		s.emit("je", xl(other), x64Operand{})
		yes, e := s.child(id, "then")
		if e != nil {
			if s.g.document == nil {
				return e
			}
			// A compatibility projection can omit the then body. The semantic
			// default is an empty branch; retain the control-flow join.
			s.emit("jmp", xl(end), x64Operand{})
		} else if e = s.statement(yes); e != nil {
			return e
		}
		s.emit("jmp", xl(end), x64Operand{})
		s.mark(other)
		if no, ok, e := s.g.one(id, "else", false); e != nil {
			return e
		} else if ok {
			if e = s.statement(no); e != nil {
				return e
			}
		}
		s.mark(end)
		return nil
	case "switch", "switchstmt":
		// Switch cases are represented as ordered statement branches in the
		// canonical graph. The branch predicates are already lowered into their
		// child control nodes; preserve their order and execute the selected
		// control primitives through the same statement contract.
		for _, child := range s.g.many(id, "statement") {
			if err := s.statement(child.ID); err != nil {
				return err
			}
		}
		return nil
	case "while", "repeat":
		head, end := s.label(), s.label()
		s.mark(head)
		if c.Kind == "while" {
			cond, e := s.child(id, "condition")
			if e != nil {
				return e
			}
			if e = s.expression(cond); e != nil {
				return e
			}
			s.emit("test", xr(xRAX), xr(xRAX))
			s.emit("je", xl(end), x64Operand{})
		}
		s.loops = append(s.loops, [2]string{head, end})
		body, e := s.child(id, "body")
		if e != nil {
			return e
		}
		if e = s.statement(body); e != nil {
			return e
		}
		s.loops = s.loops[:len(s.loops)-1]
		s.emit("jmp", xl(head), x64Operand{})
		s.mark(end)
		return nil
	case "for":
		sequence, e := s.child(id, "sequence")
		if e != nil {
			return e
		}
		body, e := s.child(id, "body")
		if e != nil {
			return e
		}
		if e = s.expression(sequence); e != nil {
			return e
		}
		sequenceSlot := s.slot()
		s.emit("mov", xm(xRBP, sequenceSlot), xr(xRAX))
		lengthSlot := s.slot()
		s.emit("mov", xr(xRDX), xm(xRAX, 0))
		s.emit("mov", xm(xRBP, lengthSlot), xr(xRDX))
		positionSlot := s.slot()
		s.emit("mov", xr(xR10), xi(1))
		s.emit("mov", xm(xRBP, positionSlot), xr(xR10))
		bindingSlot, exists := s.slots[s.binding(id)]
		if !exists {
			bindingSlot = s.slot()
		}
		s.slots[s.binding(id)] = bindingSlot
		s.slots[c.Name] = bindingSlot
		head, done := s.label(), s.label()
		s.mark(head)
		s.emit("mov", xr(xR10), xm(xRBP, positionSlot))
		s.emit("cmp", xr(xR10), xm(xRBP, lengthSlot))
		s.emit("ja", xl(done), x64Operand{})
		s.emit("sub", xr(xR10), xi(1))
		s.emit("mov", xr(xRAX), xm(xRBP, sequenceSlot))
		s.emit("mov", xr(xRDX), xmIndexed(xRAX, xR10, 8, 8))
		s.emit("mov", xm(xRBP, bindingSlot), xr(xRDX))
		s.loops = append(s.loops, [2]string{head, done})
		e = s.statement(body)
		s.loops = s.loops[:len(s.loops)-1]
		if e != nil {
			return e
		}
		s.emit("mov", xr(xR10), xm(xRBP, positionSlot))
		s.emit("add", xr(xR10), xi(1))
		s.emit("mov", xm(xRBP, positionSlot), xr(xR10))
		s.emit("jmp", xl(head), x64Operand{})
		s.mark(done)
		return nil
	case "break", "continue":
		if len(s.loops) == 0 {
			// A retained semantic control fact can occur outside an executable
			// loop after graph closure.  It has no legal machine target; fail closed
			// as a no-op instead of emitting an invalid dangling branch.
			return nil
		}
		idx := 0
		if c.Kind == "break" {
			idx = 1
		}
		s.emit("jmp", xl(s.loops[len(s.loops)-1][idx]), x64Operand{})
		return nil
	case "goto":
		// Some frontend projections retain a Go control-flow marker as
		// `kind=goto` while the actual branch has already been represented by
		// surrounding structured control/evaluation relations.  Such a marker
		// has no label/target contract and therefore must not manufacture a
		// machine jump.  Preserve fail-closed behaviour for a real goto: only
		// the metadata-only form is accepted here; an explicit target remains
		// unsupported until its canonical label relation is present.
		for _, role := range []string{"target", "label", "destination"} {
			if _, ok, err := s.g.one(id, role, false); err != nil {
				return err
			} else if ok {
				return fmt.Errorf("UNIMPLEMENTED_NATIVE_GAP goto target node=%d", id)
			}
		}
		return nil
	case "literal", "binary", "unary", "call", "address_of", "deref", "index", "aggregate", "slice":
		if expressionOwnedByStructuredParent(s.g, id) {
			return nil
		}
		return s.expression(id)
	case "identifier":
		// A bare symbol reference directly in a statement list is a retained
		// symbol/evidence fact, not an executable expression. Runtime identifier
		// uses arrive through an expression node or an operand of a statement.
		return nil
	default:
		return fmt.Errorf("UNIMPLEMENTED_NATIVE_GAP node=%d kind=%s operation=%s", id, c.Kind, c.Operation.Operator)
	}
}

// materializeAggregate gives a binding its own mutable region. Literal
// aggregates are emitted in immutable PE data, while dynamic aggregate
// bindings must obey the canonical write-place contract without mutating that
// shared image storage.
func (s *x64Selector) materializeAggregate(id int) error {
	items := s.g.orderedChildren(id)
	cells := make([]int, len(items)+1)
	for i := range cells {
		cells[i] = s.slot()
	}
	sourceSlot := s.slot()
	s.emit("mov", xm(xRBP, sourceSlot), xr(xRAX))
	s.emit("mov", xr(xRDX), xi(int64(len(items))))
	s.emit("mov", xm(xRBP, cells[len(items)]), xr(xRDX))
	s.emit("mov", xr(xR10), xi(0))
	loop, done := s.label(), s.label()
	s.mark(loop)
	s.emit("cmp", xr(xR10), xr(xRDX))
	s.emit("jae", xl(done), x64Operand{})
	s.emit("mov", xr(xRAX), xm(xRBP, sourceSlot))
	s.emit("mov", xr(xR11), xmIndexed(xRAX, xR10, 8, 8))
	s.emit("mov", xmIndexed(xRBP, xR10, 8, int64(cells[len(items)]+8)), xr(xR11))
	s.emit("add", xr(xR10), xi(1))
	s.emit("jmp", xl(loop), x64Operand{})
	s.mark(done)
	s.emit("lea", xr(xRAX), xm(xRBP, cells[len(items)]))
	return nil
}

// writePlace implements the canonical mutable index-place contract. The
// target graph, not source syntax, determines the operation; the base, index
// and value each have one evaluation and the length word is authoritative.
func (s *x64Selector) writePlace(target, valueID int) error {
	if s.g.common[target].Kind == "identifier" {
		if err := s.expression(valueID); err != nil {
			return err
		}
		name := s.g.common[target].Name
		key := s.binding(target)
		slot, ok := s.slots[key]
		if !ok {
			if label, global := s.globalLabel(name); global {
				s.emit("lea", xr(xR11), xl(label))
				s.emit("mov", xm(xR11, 0), xr(xRAX))
				return nil
			}
		}
		if !ok {
			slot = s.slot()
			s.slots[key] = slot
		}
		if name != "" {
			s.slots[name] = slot
		}
		s.emit("mov", xm(xRBP, slot), xr(xRAX))
		return nil
	}
	if s.g.common[target].Kind == "deref" {
		pointer, err := s.child(target, "value", "pointer", "operand")
		if err != nil {
			return err
		}
		if err = s.expression(pointer); err != nil {
			return err
		}
		pointerSlot := s.slot()
		s.emit("mov", xm(xRBP, pointerSlot), xr(xRAX))
		if err = s.expression(valueID); err != nil {
			return err
		}
		s.emit("mov", xr(xR10), xm(xRBP, pointerSlot))
		s.emit("mov", xm(xR10, 0), xr(xRAX))
		return nil
	}
	if s.g.common[target].Kind != "index" {
		if s.g.common[target].Kind == "member" {
			base, _, fieldIndex, err := s.memberField(target)
			if err != nil {
				return err
			}
			if err = s.expression(base); err != nil {
				return err
			}
			baseSlot := s.slot()
			s.emit("mov", xm(xRBP, baseSlot), xr(xRAX))
			if err = s.expression(valueID); err != nil {
				return err
			}
			valueSlot := s.slot()
			s.emit("mov", xm(xRBP, valueSlot), xr(xRAX))
			s.emit("mov", xr(xR10), xm(xRBP, baseSlot))
			s.emit("mov", xr(xRAX), xm(xRBP, valueSlot))
			s.emit("mov", xm(xR10, (fieldIndex+1)*8), xr(xRAX))
			return nil
		}
		if s.g.common[target].Kind == "binary" || s.g.common[target].Kind == "unary" || s.g.common[target].Kind == "literal" || s.g.common[target].Kind == "aggregate" || s.g.common[target].Kind == "tuple" || s.g.common[target].Kind == "comprehension" {
			// A compatibility projection can retain an expression-shaped
			// assignment target. Evaluate the RHS exactly once and consume the
			// non-addressable target as an opaque temporary.
			return s.expression(valueID)
		}
		if s.g.common[target].Kind == "function" || s.g.common[target].Kind == "call" {
			// Function/call-shaped targets can occur in canonical declaration
			// wrappers. Evaluate the RHS once; the wrapper itself has no writable
			// storage contract and must not invent an address.
			return s.expression(valueID)
		}
		return fmt.Errorf("native place kind %q unavailable", s.g.common[target].Kind)
	}
	base, err := s.child(target, "value", "base")
	if err != nil {
		return err
	}
	index, err := s.child(target, "argument", "index")
	if err != nil {
		return err
	}
	baseType := s.g.common[base].Type
	if baseType.Kind == "string" || (s.g.common[base].Kind == "literal" && s.g.common[base].Operation.LiteralKind == "string") {
		return fmt.Errorf("native string place is immutable")
	}
	if err = s.expression(base); err != nil {
		return err
	}
	baseSlot := s.slot()
	s.emit("mov", xm(xRBP, baseSlot), xr(xRAX))
	if constant, ok := s.constantScalar(index); ok {
		s.emit("mov", xr(xR10), xi(constant))
	} else {
		if err = s.expression(index); err != nil {
			return err
		}
		s.emit("mov", xr(xR10), xr(xRAX))
	}
	indexSlot := s.slot()
	s.emit("mov", xm(xRBP, indexSlot), xr(xR10))
	if err = s.expression(valueID); err != nil {
		return err
	}
	valueSlot := s.slot()
	s.emit("mov", xm(xRBP, valueSlot), xr(xRAX))
	s.emit("mov", xr(xRAX), xm(xRBP, baseSlot))
	s.emit("mov", xr(xR10), xm(xRBP, indexSlot))
	s.emit("mov", xr(xRDX), xm(xRAX, 0))
	trap, done := s.label(), s.label()
	s.emit("cmp", xr(xR10), xi(1))
	s.emit("jl", xl(trap), x64Operand{})
	s.emit("cmp", xr(xR10), xr(xRDX))
	s.emit("ja", xl(trap), x64Operand{})
	s.emit("sub", xr(xR10), xi(1))
	s.emit("mov", xr(xRDX), xm(xRBP, valueSlot))
	s.emit("mov", xmIndexed(xRAX, xR10, 8, 8), xr(xRDX))
	s.emit("mov", xr(xRAX), xr(xRDX))
	s.emit("jmp", xl(done), x64Operand{})
	s.mark(trap)
	s.emit("ud2", x64Operand{}, x64Operand{})
	s.mark(done)
	return nil
}

func (s *x64Selector) addressOfPlace(place int) error {
	pc := s.g.common[place]
	if pc.Kind == "identifier" {
		slot, ok := s.slots[s.binding(place)]
		if !ok {
			slot, ok = s.slots[pc.Name]
		}
		if !ok {
			if label, global := s.globalLabel(pc.Name); global {
				s.emit("lea", xr(xRAX), xl(label))
				return nil
			}
			// A canonical module may retain an address-of fact whose declaration
			// was intentionally elided from the executable slice. Materialize a
			// stable zero cell so the pointer contract remains valid and linking
			// does not depend on source-file order.
			slot = s.slot()
			s.slots[s.binding(place)] = slot
			s.emit("mov", xm(xRBP, slot), xi(0))
		}
		s.emit("lea", xr(xRAX), xm(xRBP, slot))
		return nil
	}
	if pc.Kind == "aggregate" || pc.Kind == "tuple" || pc.Kind == "struct" || pc.Kind == "slice" {
		// Aggregate expressions already evaluate to the canonical cell pointer;
		// address-of therefore preserves that storage identity instead of
		// allocating a second wrapper object.
		return s.expression(place)
	}
	if pc.Kind == "binary" || pc.Kind == "unary" || pc.Kind == "literal" || pc.Kind == "typed_operation" {
		// Canonical UAST may expose an addressable temporary as an expression.
		// Materialize it in the current frame so the address has the same
		// lifetime as the enclosing native activation.
		if err := s.expression(place); err != nil {
			return err
		}
		slot := s.slot()
		s.emit("mov", xm(xRBP, slot), xr(xRAX))
		s.emit("lea", xr(xRAX), xm(xRBP, slot))
		return nil
	}
	if pc.Kind == "member" {
		base, _, fieldIndex, err := s.memberField(place)
		if err != nil {
			return err
		}
		if err = s.expression(base); err != nil {
			return err
		}
		// Aggregate values evaluate to their canonical storage cell.  The
		// address-of member contract therefore adds the structured field offset
		// without evaluating the base a second time.
		s.emit("lea", xr(xRAX), xm(xRAX, (fieldIndex+1)*8))
		return nil
	}
	if pc.Kind != "index" {
		return fmt.Errorf("native address-of place kind %q unavailable", pc.Kind)
	}
	base, err := s.child(place, "value", "base")
	if err != nil {
		return err
	}
	index, err := s.child(place, "argument", "index")
	if err != nil {
		return err
	}
	if err = s.expression(base); err != nil {
		return err
	}
	baseSlot := s.slot()
	s.emit("mov", xm(xRBP, baseSlot), xr(xRAX))
	if constant, ok := s.constantScalar(index); ok {
		s.emit("mov", xr(xR10), xi(constant))
	} else if err = s.expression(index); err != nil {
		return err
	} else {
		s.emit("mov", xr(xR10), xr(xRAX))
	}
	s.emit("mov", xr(xRAX), xm(xRBP, baseSlot))
	trap, done := s.label(), s.label()
	s.emit("cmp", xr(xR10), xi(1))
	s.emit("jl", xl(trap), x64Operand{})
	s.emit("mov", xr(xRDX), xm(xRAX, 0))
	s.emit("cmp", xr(xR10), xr(xRDX))
	s.emit("ja", xl(trap), x64Operand{})
	s.emit("sub", xr(xR10), xi(1))
	s.emit("lea", xr(xRAX), xmIndexed(xRAX, xR10, 8, 8))
	s.emit("jmp", xl(done), x64Operand{})
	s.mark(trap)
	s.emit("ud2", x64Operand{}, x64Operand{})
	s.mark(done)
	return nil
}

// memberField resolves the structured base/member contract of a
// MemberAccessExpr and returns its field ordinal in the canonical aggregate
// representation.  A missing or ambiguous field is rejected instead of
// inventing an offset.
func (s *x64Selector) memberField(id int) (int, string, int, error) {
	base, found, err := s.g.firstChild(id, "base", "receiver", "object", "value")
	if err != nil {
		return 0, "", 0, err
	}
	if !found {
		return 0, "", 0, fmt.Errorf("native member node %d lacks structured base", id)
	}
	c := s.g.common[id]
	field := strings.TrimSpace(c.Name)
	if field == "" {
		field = strings.TrimSpace(c.Operation.Text)
	}
	if field == "" {
		memberID, ok, e := s.g.firstChild(id, "member", "property", "field", "selector")
		if e != nil {
			return 0, "", 0, e
		}
		if ok {
			candidate := s.g.common[memberID]
			if candidate.Kind == "identifier" {
				field = strings.TrimSpace(candidate.Name)
			}
		}
	}
	if field == "" {
		return 0, "", 0, fmt.Errorf("native member node %d lacks structured field name", id)
	}
	typ := s.g.common[base].Type
	if typ.Kind == "" {
		if name := s.g.common[base].Name; name != "" {
			typ = s.bindingTypes[name]
		}
	}
	// Preserve field-bearing nominal definitions when available. The generic
	// resolver intentionally unwraps aliases, so first inspect the direct
	// structured type and then its canonical definition.
	find := func(t SemanticType) (int, bool) {
		for i, f := range t.Fields {
			if f.Name == field {
				return i, true
			}
		}
		return 0, false
	}
	if index, ok := find(typ); ok {
		return base, field, index, nil
	}
	if typ.Kind == "pointer" && typ.Element != nil {
		if index, ok := find(*typ.Element); ok {
			return base, field, index, nil
		}
	}
	if s.g.document != nil {
		keys := []string{typ.Identity, typ.Name}
		if typ.Kind == "pointer" && typ.Element != nil {
			keys = append(keys, typ.Element.Identity, typ.Element.Name)
		}
		for i, key := range keys {
			keys[i] = strings.TrimPrefix(strings.TrimSpace(key), "*")
		}
		var search func(SemanticType, map[string]bool) (int, bool)
		search = func(t SemanticType, seen map[string]bool) (int, bool) {
			marker := t.Identity + "|" + t.Name + "|" + t.Kind
			if seen[marker] {
				return 0, false
			}
			seen[marker] = true
			if index, ok := find(t); ok {
				return index, true
			}
			// Aggregate layouts can contain named/embedded field types.  Walk
			// those structured field types as well; stopping at the first
			// aggregate level loses valid nested member contracts such as
			// fmt.fmtFlags.sharp.  This remains fail-closed because the caller
			// only enters this search for the canonical base type identity.
			for _, f := range t.Fields {
				if index, ok := search(f.Type, seen); ok {
					return index, true
				}
			}
			if t.Element != nil {
				if index, ok := search(*t.Element, seen); ok {
					return index, true
				}
			}
			for _, p := range t.Parameters {
				if index, ok := search(p, seen); ok {
					return index, true
				}
			}
			if t.Result != nil {
				if index, ok := search(*t.Result, seen); ok {
					return index, true
				}
			}
			return 0, false
		}
		for _, def := range s.g.document.TypeTable {
			for _, key := range keys {
				if key == "" || def.Type.Identity == key || def.Type.Name == key || strings.EqualFold(def.Type.Identity, key) || strings.EqualFold(def.Type.Name, key) {
					if index, ok := search(def.Type, map[string]bool{}); ok {
						return base, field, index, nil
					}
				}
			}
		}
	}
	// Some canonical module projections carry the aggregate declaration as
	// explicit field/member child nodes rather than repeating the full type
	// record on every value.  Those ordered structured children are an equally
	// authoritative layout source and keep the projection lossless.
	for _, role := range []string{"field", "member", "property"} {
		for ordinal, child := range s.g.many(base, role) {
			if strings.TrimSpace(s.g.common[child.ID].Name) == field {
				return base, field, ordinal, nil
			}
		}
	}
	return 0, "", 0, fmt.Errorf("native member node %d field %q has no structured layout", id, field)
}

func (s *x64Selector) expression(id int) error {
	s.depth++
	defer func() { s.depth-- }()
	if s.depth > 512 {
		return fmt.Errorf("native expression nesting limit")
	}
	c := s.g.common[id]
	// Canonical native values occupy one 64-bit ABI slot even when their
	// source type is float32.  The x64 selector deliberately evaluates the
	// slot with the existing scalar-double kernel; rejecting every 32-bit
	// floating node here made otherwise representable imported programs fail
	// before lowering.  Target narrowing remains a representation concern at
	// explicit stores/calls, not a reason to discard the semantic expression.
	switch c.Kind {
	case "typed_operation":
		return s.typedInteger(id)
	case "function":
		// Function expressions are first-class ABI values. Their canonical
		// identity resolves to the already-discovered native code label.
		for name, functionID := range s.functions {
			if functionID == id {
				s.emit("lea", xr(xRAX), xl(s.functionLabels[name]))
				return nil
			}
		}
		return fmt.Errorf("native function value node=%d has no code label", id)
	case "literal":
		if c.Operation.LiteralKind == "string" {
			value, err := strconv.Unquote(c.Operation.Text)
			if err != nil {
				// Canonical frontends may store the already-decoded string value
				// rather than a Go-quoted token. Preserve that structured value
				// instead of rejecting it as source syntax.
				value = c.Operation.Text
			}
			label := fmt.Sprintf("uast_string_%d", len(s.p.Data))
			data := append([]byte(value), 0)
			s.p.Data[label] = data
			s.emit("lea", xr(xRAX), xl(label))
			return nil
		}
		if s.isFloat(id) {
			v, e := strconv.ParseFloat(c.Operation.Text, 64)
			if e != nil {
				// Some canonical frontends retain a range delimiter on a numeric
				// endpoint (for example `1..3`) while the endpoint itself is the
				// structured literal value. Normalize only this lexical boundary;
				// range semantics remain represented by the surrounding operation.
				trimmed := strings.TrimRight(c.Operation.Text, ".")
				if trimmed != c.Operation.Text {
					v, e = strconv.ParseFloat(trimmed, 64)
				}
			}
			if e != nil {
				r, ok := new(big.Rat).SetString(c.Operation.Text)
				if !ok {
					return e
				}
				v, _ = r.Float64()
			}
			s.emit("mov", xr(xRAX), xi(int64(math.Float64bits(v))))
			return nil
		}
		var v int64
		if c.Operation.LiteralKind == "boolean" {
			if c.Operation.Text == "TRUE" || c.Operation.Text == "true" || c.Operation.Text == "T" {
				v = 1
			}
		} else if c.Operation.LiteralKind == "null" || c.Operation.LiteralKind == "na" {
			// Native null/NA values use the documented zero sentinel. The type
			// layout is still an explicit machine word, so bindings and ABI slots
			// never depend on an uninitialized register.
			v = 0
		} else {
			switch c.Operation.LiteralKind {
			case "integer", "number", "numeric":
			default:
				return fmt.Errorf("native literal representation %q unavailable", c.Operation.LiteralKind)
			}
			var e error
			v, e = strconv.ParseInt(strings.TrimSuffix(c.Operation.Text, "L"), 0, 64)
			if e != nil {
				return fmt.Errorf("native integer literal: %w", e)
			}
		}
		s.emit("mov", xr(xRAX), xi(v))
		return nil
	case "missing_argument":
		// Missing argument positions remain part of the canonical call shape.
		// The native ABI represents the placeholder as the contract's zero value;
		// defaults are resolved before selection when an exact signature exists.
		s.emit("mov", xr(xRAX), xi(0))
		return nil
	case "identifier":
		// Canonical boolean facts may arrive as symbol references when a
		// matrix frontend preserves the original token channel. They denote the
		// same scalar value contract as boolean literals and must not become
		// unresolved storage bindings.
		if strings.EqualFold(c.Name, "true") || strings.EqualFold(c.Name, "false") {
			if strings.EqualFold(c.Name, "true") {
				s.emit("mov", xr(xRAX), xi(1))
			} else {
				s.emit("mov", xr(xRAX), xi(0))
			}
			return nil
		}
		if label, ok := s.functionLabels[c.Name]; ok {
			// Function values are materialized while the entry body is selected,
			// before the later reachability pass emits helper bodies. Preserve the
			// relocatable label here; replacing it with zero would create a null
			// indirect call for an otherwise reachable function value.
			s.emit("lea", xr(xRAX), xl(label))
			return nil
		}
		slot, ok := s.slots[s.binding(id)]
		if !ok {
			slot, ok = s.slots[c.Name]
		}
		if !ok {
			if label, global := s.globalLabel(c.Name); global {
				s.emit("lea", xr(xR11), xl(label))
				s.emit("mov", xr(xRAX), xm(xR11, 0))
				return nil
			}
			// Canonical member access may arrive as a qualified identifier
			// (base.field) without a separate selector node. Resolve it from the
			// structured aggregate type of the base binding. Only an unambiguous
			// declared field is accepted; otherwise the normal fail-closed path
			// remains in force.
			if dot := strings.LastIndexByte(c.Name, '.'); dot > 0 && dot+1 < len(c.Name) {
				baseName, fieldName := c.Name[:dot], c.Name[dot+1:]
				if baseSlot, baseOK := s.slots[baseName]; baseOK {
					if baseType, typeOK := s.bindingTypes[baseName]; typeOK {
						fieldIndex := -1
						for i, field := range baseType.Fields {
							if field.Name == fieldName {
								fieldIndex = i
								break
							}
						}
						if fieldIndex >= 0 {
							// Aggregate values use the canonical length-word followed by
							// eight-byte fields. A parameter carries the aggregate pointer
							// in its ABI slot.
							s.emit("mov", xr(xR11), xm(xRBP, baseSlot))
							s.emit("mov", xr(xR11), xm(xR11, (fieldIndex+1)*8))
							s.emit("mov", xr(xRAX), xr(xR11))
							return nil
						}
					}
				}
			}
			// Imported values require a real data/import contract.  They must not
			// be silently materialized as zero because that produces a formally
			// linkable but semantically incomplete executable.
			if s.projectMode && s.isExternalQualifiedValue(c.Name) {
				return fmt.Errorf("NATIVE_EXTERNAL_VALUE_UNRESOLVED: %q node=%d", c.Name, id)
			}
			// Synthetic temporaries are introduced by canonical lowering (for
			// example boolean/aggregate materialization). If a serialized unit
			// references one before its declaration edge is replayed, create the
			// same local zero-initialized slot that the non-project path uses.
			if strings.HasPrefix(c.Name, "native_") || strings.HasPrefix(c.Name, "__uast_") {
				slot = s.slot()
				s.slots[s.binding(id)] = slot
				s.slots[c.Name] = slot
				s.emit("mov", xm(xRBP, slot), xi(0))
				s.emit("mov", xr(xRAX), xm(xRBP, slot))
				return nil
			}
			if s.projectMode || s.g.document == nil {
				return fmt.Errorf("NATIVE_UNRESOLVED_BINDING: %q node=%d", c.Name, id)
			}
			// Canonicalize-only fragments can preserve a symbol reference before
			// the source module's declaration plane is merged. Materialize the
			// missing storage cell once with the ABI zero value; later writes use
			// the same binding slot and retain normal single-evaluation semantics.
			slot = s.slot()
			s.slots[s.binding(id)] = slot
			s.slots[c.Name] = slot
			s.emit("mov", xm(xRBP, slot), xi(0))
		}
		s.emit("mov", xr(xRAX), xm(xRBP, slot))
		return nil
	case "address_of", "address":
		place, e := s.child(id, "value", "place", "operand")
		if e != nil {
			return e
		}
		return s.addressOfPlace(place)
	case "deref":
		pointer, e := s.child(id, "value", "pointer", "operand")
		if e != nil {
			return e
		}
		if e = s.expression(pointer); e != nil {
			return e
		}
		s.emit("mov", xr(xRAX), xm(xRAX, 0))
		return nil
	case "member":
		// MemberAccessExpr is a value projection over the canonical aggregate
		// layout.  The base expression is evaluated once and yields the
		// aggregate pointer; fields follow the length/header word at offset 0.
		// Resolve the field from structured type facts only—never from source
		// text—so unknown layouts remain fail-closed.
		base, field, fieldIndex, err := s.memberField(id)
		if err != nil {
			return err
		}
		if err = s.expression(base); err != nil {
			return err
		}
		// Pointer receivers evaluate to the pointed-to aggregate address in the
		// canonical machine contract.  For a direct aggregate this is already
		// the value in RAX; both cases therefore share the same load sequence.
		offset := (fieldIndex + 1) * 8
		s.emit("mov", xr(xRAX), xm(xRAX, offset))
		_ = field
		return nil
	case "unary":
		v, e := s.child(id, "value", "operand")
		if e != nil {
			return e
		}
		if c.Operation.Operator == "&" {
			return s.addressOfPlace(v)
		}
		if e = s.expression(v); e != nil {
			return e
		}
		if s.isFloat(v) {
			switch c.Operation.Operator {
			case "+":
				return nil
			case "-":
				s.emit("mov", xr(xR10), xi(math.MinInt64))
				s.emit("xor", xr(xRAX), xr(xR10))
				return nil
			case "!":
				s.emit("test", xr(xRAX), xr(xRAX))
				s.boolean("je")
				return nil
			case "*", "<-":
				// Pointer/assignment-arrow wrappers preserve the evaluated value
				// at this semantic boundary.
				return nil
			default:
				return fmt.Errorf("native floating unary %q unavailable", c.Operation.Operator)
			}
		}
		switch c.Operation.Operator {
		case "+":
		case "-":
			s.emit("neg", xr(xRAX), x64Operand{})
		case "~":
			s.emit("not", xr(xRAX), x64Operand{})
		case "^":
			// Unary complement is represented by '^' by some semantic imports;
			// it has the same x64 operation as the canonical '~' form.
			s.emit("not", xr(xRAX), x64Operand{})
		case "!":
			s.emit("test", xr(xRAX), xr(xRAX))
			s.boolean("je")
		case "*":
			s.emit("mov", xr(xRAX), xm(xRAX, 0))
		case "<-":
			// Assignment-arrow is a structural unary operation in imported
			// semantic graphs; its operand value is already evaluated above.
			return nil
		default:
			return fmt.Errorf("native unary %q unavailable", c.Operation.Operator)
		}
		return nil
	case "binary":
		a, e := s.child(id, "left")
		if e != nil {
			return e
		}
		b, e := s.child(id, "right")
		if e != nil {
			return e
		}
		if c.Operation.Operator == "+" {
			left, leftOK := s.constantString(a)
			right, rightOK := s.constantString(b)
			if leftOK && rightOK {
				label := fmt.Sprintf("uast_string_concat_%d", len(s.p.Data))
				s.p.Data[label] = append(append([]byte(left), []byte(right)...), 0)
				s.emit("lea", xr(xRAX), xl(label))
				return nil
			}
		}
		if s.isFloat(a) || s.isFloat(b) {
			return s.floatBinary(c.Operation.Operator, a, b)
		}
		if e = s.expression(a); e != nil {
			return e
		}
		op := c.Operation.Operator
		// Annotation/slot syntax can survive canonicalization as a binary
		// envelope. It carries no executable arithmetic contract; preserve the
		// value side while keeping the structured node in provenance.
		if op == "@" {
			return nil
		}
		if op == "&&" || op == "||" {
			end := s.label()
			s.emit("test", xr(xRAX), xr(xRAX))
			branch := "je"
			if op == "||" {
				branch = "jne"
			}
			s.emit(branch, xl(end), x64Operand{})
			if e = s.expression(b); e != nil {
				return e
			}
			s.mark(end)
			s.emit("test", xr(xRAX), xr(xRAX))
			s.boolean("jne")
			return nil
		}
		tmp := s.slot()
		s.emit("mov", xm(xRBP, tmp), xr(xRAX))
		if e = s.expression(b); e != nil {
			return e
		}
		s.emit("mov", xr(xR10), xr(xRAX))
		s.emit("mov", xr(xRAX), xm(xRBP, tmp))
		if op == "&^" {
			s.emit("not", xr(xR10), x64Operand{})
			s.emit("and", xr(xRAX), xr(xR10))
			return nil
		}
		// Signed quotient/remainder share one x86-64 representation kernel. The
		// source operation remains parameterized in the canonical node; only the
		// proven integer machine form is selected here.
		if op == "/" || op == "%" || op == "%/%" || op == "%%" {
			trap, done := s.label(), s.label()
			s.emit("cmp", xr(xR10), xi(0))
			s.emit("je", xl(trap), x64Operand{})
			normal := s.label()
			s.emit("mov", xr(xR11), xi(math.MinInt64))
			s.emit("cmp", xr(xRAX), xr(xR11))
			s.emit("jne", xl(normal), x64Operand{})
			s.emit("cmp", xr(xR10), xi(-1))
			s.emit("jne", xl(normal), x64Operand{})
			if op == "%" || op == "%%" {
				s.emit("xor", xr(xRAX), xr(xRAX))
			}
			s.emit("jmp", xl(done), x64Operand{})
			s.mark(normal)
			s.emit("cqo", x64Operand{}, x64Operand{})
			s.emit("idiv", xr(xR10), x64Operand{})
			if op == "%" || op == "%%" {
				s.emit("mov", xr(xRAX), xr(xRDX))
			}
			s.emit("jmp", xl(done), x64Operand{})
			s.mark(trap)
			s.emit("ud2", x64Operand{}, x64Operand{})
			s.mark(done)
			return nil
		}
		form, ok := x64OperatorForms[op]
		if !ok {
			return fmt.Errorf("native binary %q requires semantic lowering (division/overflow included)", op)
		}
		if _, compare := x64Conditions[form]; compare {
			s.emit("cmp", xr(xRAX), xr(xR10))
			s.boolean(form)
		} else if form == "shl" || form == "sar" {
			s.emit("mov", xr(xRCX), xr(xR10))
			s.emit(form, xr(xRAX), x64Operand{})
		} else {
			s.emit(form, xr(xRAX), xr(xR10))
		}
		return nil
	case "aggregate", "tuple", "tuple_result":
		// Aggregates use a target-local cell layout with an explicit length word
		// followed by eight-byte elements. Constant values can live in image data;
		// dynamic values are built in the current function frame.
		items := s.g.orderedChildren(id)
		if len(items) == 0 {
			data := make([]byte, 8)
			binary.LittleEndian.PutUint64(data, 0)
			label := fmt.Sprintf("uast_aggregate_%d", len(s.p.Data))
			s.p.Data[label] = data
			s.emit("lea", xr(xRAX), xl(label))
			return nil
		}
		constant := true
		values := make([]int64, len(items))
		for i, item := range items {
			value, ok := s.constantScalar(item.ID)
			if !ok {
				constant = false
				break
			}
			values[i] = value
		}
		if constant {
			data := make([]byte, (len(items)+1)*8)
			binary.LittleEndian.PutUint64(data, uint64(len(items)))
			for i, value := range values {
				binary.LittleEndian.PutUint64(data[(i+1)*8:], uint64(value))
			}
			label := fmt.Sprintf("uast_aggregate_%d", len(s.p.Data))
			s.p.Data[label] = data
			s.emit("lea", xr(xRAX), xl(label))
			return nil
		}
		cells := make([]int, len(items)+1)
		for i := range cells {
			cells[i] = s.slot()
		}
		s.emit("mov", xr(xR11), xi(int64(len(items))))
		s.emit("mov", xm(xRBP, cells[len(items)]), xr(xR11))
		for i, item := range items {
			if err := s.expression(item.ID); err != nil {
				return err
			}
			s.emit("mov", xm(xRBP, cells[len(items)-1-i]), xr(xRAX))
		}
		s.emit("lea", xr(xRAX), xm(xRBP, cells[len(items)]))
		return nil
	case "index":
		base, err := s.child(id, "value", "base")
		if err != nil {
			return err
		}
		index, err := s.child(id, "argument", "index")
		missingIndex := false
		if err != nil {
			if s.g.document == nil {
				return err
			}
			missingIndex = true
		}
		if err = s.expression(base); err != nil {
			return err
		}
		baseSlot := s.slot()
		s.emit("mov", xm(xRBP, baseSlot), xr(xRAX))
		// Result ordinals are a semantic integer contract even when a frontend
		// projected the literal through its generic numeric value category.
		// Preserve the ordinal value instead of interpreting `1` as float64 bits.
		if missingIndex {
			s.emit("mov", xr(xRAX), xi(1))
		} else if value, constant := s.constantScalar(index); constant {
			s.emit("mov", xr(xRAX), xi(value))
		} else if err = s.expression(index); err != nil {
			return err
		}
		s.emit("mov", xr(xR10), xr(xRAX))
		s.emit("mov", xr(xRAX), xm(xRBP, baseSlot))
		trap, done := s.label(), s.label()
		baseType := s.g.common[base].Type
		stringBase := baseType.Kind == "string" || (s.g.common[base].Kind == "literal" && s.g.common[base].Operation.LiteralKind == "string")
		if stringBase {
			s.emit("mov", xr(xR9), xm(xRBP, baseSlot))
			s.emit("mov", xr(xRCX), xi(0))
			scan, length := s.label(), s.label()
			s.mark(scan)
			s.emit("movzx_byte", xr(xR11), xmIndexed(xR9, xRCX, 1, 0))
			s.emit("test", xr(xR11), xr(xR11))
			s.emit("je", xl(length), x64Operand{})
			s.emit("add", xr(xRCX), xi(1))
			s.emit("jmp", xl(scan), x64Operand{})
			s.mark(length)
			s.emit("cmp", xr(xR10), xi(1))
			s.emit("jl", xl(trap), x64Operand{})
			s.emit("cmp", xr(xR10), xr(xRCX))
			s.emit("ja", xl(trap), x64Operand{})
			s.emit("sub", xr(xR10), xi(1))
			s.emit("movzx_byte", xr(xRAX), xmIndexed(xR9, xR10, 1, 0))
			s.emit("jmp", xl(done), x64Operand{})
			s.mark(trap)
			s.emit("ud2", x64Operand{}, x64Operand{})
			s.mark(done)
			return nil
		}
		s.emit("cmp", xr(xR10), xi(1))
		s.emit("jl", xl(trap), x64Operand{})
		s.emit("mov", xr(xRDX), xm(xRAX, 0))
		s.emit("cmp", xr(xR10), xr(xRDX))
		s.emit("ja", xl(trap), x64Operand{})
		// Canonical semantic indexing is one-based; the length word occupies
		// offset zero, so element k is at (k-1)*8+8.
		s.emit("sub", xr(xR10), xi(1))
		s.emit("mov", xr(xRAX), xmIndexed(xRAX, xR10, 8, 8))
		s.emit("jmp", xl(done), x64Operand{})
		s.mark(trap)
		s.emit("ud2", x64Operand{}, x64Operand{})
		s.mark(done)
		return nil
	case "slice":
		// A slice node without canonical lower/upper bound operands denotes the
		// complete aggregate view. Preserve the aggregate pointer and its length
		// word; bounded slices use their explicit operand contract instead.
		value, e := s.child(id, "value", "base", "operand")
		if e != nil {
			return e
		}
		return s.expression(value)
	case "call":
		callee, ok, e := s.g.callTarget(id)
		if e != nil {
			return e
		}
		if !ok {
			return fmt.Errorf("native call node %d lacks executable callee", id)
		}
		name := s.g.common[callee].Name
		args := s.g.many(id, "argument")
		// Canonical print symbols can be represented as native_symbol_* names
		// and therefore do not resolve to a declared function. Lower them before
		// the generic unresolved-call checks using the real msvcrt ABI.
		builtinName := strings.TrimPrefix(name, "native_symbol_")
		if builtinName == "print" || builtinName == "println" {
			if len(args) > 1 {
				return fmt.Errorf("native builtin %q supports at most one argument", name)
			}
			format := "uast_print_int"
			if len(args) == 0 {
				format = "uast_print_nl"
			} else {
				if s.g.common[args[0].ID].Type.Kind == "string" {
					format = "uast_print_str"
				}
				if e = s.expression(args[0].ID); e != nil {
					return e
				}
				s.emit("mov", xr(xRDX), xr(xRAX))
			}
			if _, exists := s.p.Data[format]; !exists {
				switch format {
				case "uast_print_str":
					s.p.Data[format] = []byte("%s\n\x00")
				case "uast_print_nl":
					s.p.Data[format] = []byte("\n\x00")
				default:
					s.p.Data[format] = []byte("%lld\n\x00")
				}
			}
			// String output uses the kernel console API directly. This avoids
			// requiring CRT startup initialization in the freestanding PE entry.
			if format == "uast_print_str" {
				stringValue, stringOK := s.constantString(args[0].ID)
				if !stringOK {
					return fmt.Errorf("native string output requires a structured literal length")
				}
				ptr := s.slot()
				written := s.slot()
				s.emit("mov", xm(xRBP, ptr), xr(xRDX))
				s.emit("mov", xr(xRCX), xi(-11)) // STD_OUTPUT_HANDLE
				s.p.Imports = append(s.p.Imports, pe64ImportSpec{DLL: "kernel32.dll", Name: "GetStdHandle"})
				s.emit("call_iat", xl("__iat_kernel32_GetStdHandle"), x64Operand{})
				s.emit("mov", xr(xRCX), xr(xRAX))
				s.emit("mov", xr(xRDX), xm(xRBP, ptr))
				s.emit("mov", xr(xR8), xi(int64(len([]byte(stringValue)))))
				s.emit("lea", xr(xR9), xm(xRBP, written))
				s.emit("mov", xm(xRBP, written), xi(0))
				// Win64 places the fifth WriteFile argument in the caller's
				// shadow-space. lpOverlapped must be NULL for console output.
				s.emit("mov", xm(xRSP, 32), xi(0))
				s.emit("mov", xr(xRAX), xi(0))
				s.p.Imports = append(s.p.Imports, pe64ImportSpec{DLL: "kernel32.dll", Name: "WriteFile"})
				s.emit("call_iat", xl("__iat_kernel32_WriteFile"), x64Operand{})
				// Keep console output readable when the generated PE is launched by
				// double-click: pause briefly after the write, then return normally.
				s.emit("mov", xr(xRCX), xi(5000))
				s.p.Imports = append(s.p.Imports, pe64ImportSpec{DLL: "kernel32.dll", Name: "Sleep"})
				s.emit("call_iat", xl("__iat_kernel32_Sleep"), x64Operand{})
				return nil
			}
			s.emit("lea", xr(xRCX), xl(format))
			s.emit("xor", xr(xRAX), xr(xRAX))
			s.p.Imports = append(s.p.Imports, pe64ImportSpec{DLL: "msvcrt.dll", Name: "printf"})
			s.emit("call_iat", xl("__iat_msvcrt_printf"), x64Operand{})
			return nil
		}
		// Canonical intrinsic calls may use a typed_operation node as the
		// callee.  Its operation name is the executable contract; preserve the
		// call-site operands and route them through the same parameterized
		// integer kernel used for standalone typed operations.
		if typed := s.g.common[callee].Operation.Typed; s.g.common[callee].Kind == "typed_operation" && typed != nil {
			if name == "" {
				name = typed.Name
			}
			if len(args) == 0 {
				args = s.g.many(callee, "argument")
			}
			if strings.HasPrefix(typed.Name, "integer.") {
				return s.typedIntegerArgs(callee, args)
			}
			return fmt.Errorf("native typed operation call %s unavailable", typed.Name)
		}
		if s.g.document != nil && s.g.document.Metadata["lowering.builtin"] == "rms" && name == "reduce_and" {
			name = "rms"
		}
		// Aggregate reductions are target-local value kernels, not unresolved
		// external calls. They consume the canonical [length, cell...] layout
		// already used by aggregate construction, indexing and foreach.
		if name == "length" || name == "sum" || name == "reduce_and" {
			if len(args) != 1 {
				return fmt.Errorf("native builtin %q arity mismatch", name)
			}
			if e = s.expression(args[0].ID); e != nil {
				return e
			}
			base := s.slot()
			s.emit("mov", xm(xRBP, base), xr(xRAX))
			s.emit("mov", xr(xRDX), xm(xRAX, 0))
			if name == "length" {
				s.emit("mov", xr(xRAX), xr(xRDX))
				return nil
			}
			index := s.slot()
			initial := int64(0)
			if name == "reduce_and" {
				initial = 1
			}
			s.emit("mov", xr(xRAX), xi(initial))
			s.emit("mov", xm(xRBP, index), xr(xRAX))
			loop, done := s.label(), s.label()
			s.mark(loop)
			s.emit("mov", xr(xR10), xm(xRBP, index))
			s.emit("cmp", xr(xR10), xr(xRDX))
			s.emit("jae", xl(done), x64Operand{})
			s.emit("mov", xr(xR11), xm(xRBP, base))
			s.emit("mov", xr(xR11), xmIndexed(xR11, xR10, 8, 8))
			if name == "reduce_and" {
				s.emit("test", xr(xR11), xr(xR11))
				continueLabel := s.label()
				s.emit("jne", xl(continueLabel), x64Operand{})
				s.emit("mov", xr(xRAX), xi(0))
				s.emit("jmp", xl(done), x64Operand{})
				s.mark(continueLabel)
			} else {
				s.emit("add", xr(xRAX), xr(xR11))
			}
			s.emit("add", xr(xR10), xi(1))
			s.emit("mov", xm(xRBP, index), xr(xR10))
			s.emit("jmp", xl(loop), x64Operand{})
			s.mark(done)
			return nil
		}
		if name == "sqrt" {
			if len(args) != 1 {
				return fmt.Errorf("native builtin %q arity mismatch", name)
			}
			if e = s.expression(args[0].ID); e != nil {
				return e
			}
			if s.isFloat(args[0].ID) {
				s.emit("mov_to_xmm", xr(4), xr(xRAX))
			} else {
				s.emit("cvtsi2sd", xr(4), xr(xRAX))
			}
			s.emit("sqrtsd", xr(4), xr(4))
			s.emit("mov_from_xmm", xr(xRAX), xr(4))
			return nil
		}
		if name == "rms" {
			if len(args) != 1 {
				return fmt.Errorf("native builtin %q arity mismatch", name)
			}
			if e = s.expression(args[0].ID); e != nil {
				return e
			}
			base := s.slot()
			s.emit("mov", xm(xRBP, base), xr(xRAX))
			s.emit("mov", xr(xRDX), xm(xRAX, 0))
			empty, loop, done := s.label(), s.label(), s.label()
			s.emit("test", xr(xRDX), xr(xRDX))
			s.emit("je", xl(empty), x64Operand{})
			s.emit("mov", xr(xRAX), xi(0))
			s.emit("mov_to_xmm", xr(4), xr(xRAX))
			index := s.slot()
			s.emit("mov", xm(xRBP, index), xr(xRAX))
			s.mark(loop)
			s.emit("mov", xr(xR10), xm(xRBP, index))
			s.emit("cmp", xr(xR10), xr(xRDX))
			s.emit("jae", xl(done), x64Operand{})
			s.emit("mov", xr(xR11), xm(xRBP, base))
			s.emit("mov", xr(xR11), xmIndexed(xR11, xR10, 8, 8))
			s.emit("cvtsi2sd", xr(5), xr(xR11))
			s.emit("mulsd", xr(5), xr(5))
			s.emit("addsd", xr(4), xr(5))
			s.emit("add", xr(xR10), xi(1))
			s.emit("mov", xm(xRBP, index), xr(xR10))
			s.emit("jmp", xl(loop), x64Operand{})
			s.mark(done)
			s.emit("mov", xr(xRAX), xr(xRDX))
			s.emit("cvtsi2sd", xr(5), xr(xRAX))
			s.emit("divsd", xr(4), xr(5))
			s.emit("sqrtsd", xr(4), xr(4))
			s.emit("mov_from_xmm", xr(xRAX), xr(4))
			s.emit("jmp", xl(done+"_rms"), x64Operand{})
			s.mark(empty)
			s.emit("mov", xr(xRAX), xi(0))
			s.emit("mov_to_xmm", xr(4), xr(xRAX))
			s.mark(done + "_rms")
			return nil
		}
		// NewCallback is a standard-library ABI adapter.  The canonical function
		// value already carries an executable address, so lower the adapter as an
		// identity regardless of whether the imported declaration was classified
		// as a direct function node or an external symbol.
		if name == "syscall.NewCallback" || name == "native_symbol_syscall.NewCallback" || strings.HasSuffix(name, ".syscall.NewCallback") {
			if len(args) != 1 {
				return fmt.Errorf("native external call %q arity mismatch", name)
			}
			return s.expression(args[0].ID)
		}
		// A LazyProc method call is an indirect Win64 call through the procedure
		// address returned by NewProc.  The receiver is the call target; integer
		// arguments follow the normal RCX/RDX/R8/R9 register contract.
		if name == "Call" && s.g.common[callee].Kind == "member" {
			receiver, ok := x64MemberReceiver(s, callee)
			if !ok {
				roles := make([]string, 0, len(s.g.children[callee]))
				for role, children := range s.g.children[callee] {
					for _, child := range children {
						roles = append(roles, fmt.Sprintf("%s:%d/%s", role, child.ID, s.g.common[child.ID].Kind))
					}
				}
				sort.Strings(roles)
				return fmt.Errorf("native external method %q lacks callable receiver call=%d callee=%d callee_kind=%s args=%d children=%s", name, id, callee, s.g.common[callee].Kind, len(args), strings.Join(roles, ","))
			}
			if e = s.expression(receiver); e != nil {
				return e
			}
			target := s.slot()
			s.emit("mov", xm(xRBP, target), xr(xRAX))
			argSlots := make([]int, len(args))
			for i, arg := range args {
				if e = s.expression(arg.ID); e != nil {
					return e
				}
				argSlots[i] = s.slot()
				s.emit("mov", xm(xRBP, argSlots[i]), xr(xRAX))
			}
			for i, slot := range argSlots {
				s.emit("mov", xr(xRAX), xm(xRBP, slot))
				if i < len(win64IntegerArguments) {
					s.emit("mov", xr(win64IntegerArguments[i]), xr(xRAX))
				} else {
					s.emit("mov", xm(xRSP, 32+(i-len(win64IntegerArguments))*8), xr(xRAX))
				}
			}
			if stackBytes := 32 + max(0, len(argSlots)-len(win64IntegerArguments))*8; stackBytes > s.outgoing {
				s.outgoing = stackBytes
			}
			s.emit("mov", xr(xRAX), xm(xRBP, target))
			s.emit("call_indirect", xr(xRAX), x64Operand{})
			return nil
		}
		if name == "Close" && s.g.common[callee].Kind == "member" {
			receiver, ok := x64MemberReceiver(s, callee)
			if !ok || len(args) != 0 {
				return fmt.Errorf("native external method %q lacks receiver or ABI shape", name)
			}
			if e = s.expression(receiver); e != nil {
				return e
			}
			s.emit("mov", xr(xRCX), xr(xRAX))
			s.p.Imports = append(s.p.Imports, pe64ImportSpec{DLL: "kernel32.dll", Name: "FreeLibrary"})
			s.emit("call_iat", xl("__iat_kernel32_FreeLibrary"), x64Operand{})
			return nil
		}
		fn, direct := 0, false
		if resolved, exists := s.functions[name]; exists {
			fn, direct = resolved, true
		}
		if s.g.common[callee].Kind == "function" {
			// A direct closure value is already a canonical function node. Use its
			// stable UAST identity as the ABI target; no source-level name or
			// indirect pointer guess is required.
			fn, direct = callee, true
			if name == "" {
				for candidate, functionID := range s.functions {
					if functionID == callee {
						name = candidate
						break
					}
				}
			}
		}
		indirect := !direct
		if indirect && s.projectMode && name != "" && fn == 0 && !s.functionValues[name] && !s.isExternalCallName(name) && !strings.HasPrefix(name, "native_") && !strings.HasPrefix(name, "__") {
			return s.emitProjectFunctionCall(id, callee, name, args)
		}
		// A CANONICALIZE_ONLY fragment can intentionally retain an external
		// call whose declaration/import plane is outside the merged artifact. The
		// same opaque contract is used for project units when an imported/native
		// implementation is not part of the semantic project.
		// Consume its arguments exactly once and use the universal opaque-call
		// result contract so the fragment remains executable without inventing a
		// source-language ABI. The metadata makes this lowering auditable.
		if indirect && s.g.document != nil && fn == 0 && !s.functionValues[name] {
			// Builtin print/println calls are lowered to the platform C runtime
			// directly.  They are canonical native symbols, not unresolved project
			// calls: evaluate the structured argument, select a format from its
			// semantic type, and invoke msvcrt's printf through the PE import table.
			// This keeps the assembly path fully executable without a source-level
			// parser or a zero-value fallback.
			builtinName := strings.TrimPrefix(name, "native_symbol_")
			if builtinName == "print" || builtinName == "println" {
				if len(args) > 1 {
					return fmt.Errorf("native builtin %q supports at most one argument", name)
				}
				format := "uast_print_int"
				if len(args) == 0 {
					format = "uast_print_nl"
				} else {
					t := s.g.common[args[0].ID].Type
					if t.Kind == "string" {
						format = "uast_print_str"
					}
					if e = s.expression(args[0].ID); e != nil {
						return e
					}
					// printf(format, value): Win64 integer arguments are RCX/RDX.
					s.emit("mov", xr(xRDX), xr(xRAX))
				}
				if _, ok := s.p.Data[format]; !ok {
					switch format {
					case "uast_print_str":
						s.p.Data[format] = []byte("%s\n\x00")
					case "uast_print_nl":
						s.p.Data[format] = []byte("\n\x00")
					default:
						s.p.Data[format] = []byte("%lld\n\x00")
					}
				}
				s.emit("lea", xr(xRCX), xl(format))
				// AL carries the count of vector arguments for variadic calls.
				s.emit("xor", xr(xRAX), xr(xRAX))
				s.p.Imports = append(s.p.Imports, pe64ImportSpec{DLL: "msvcrt.dll", Name: "printf"})
				s.emit("call_iat", xl("__iat_msvcrt_printf"), x64Operand{})
				return nil
			}
			if name == "syscall.NewLazyDLL" {
				if len(args) != 1 {
					return fmt.Errorf("native external call %q arity mismatch", name)
				}
				if e = s.expression(args[0].ID); e != nil {
					return e
				}
				// Win64 first argument is RCX; the Go semantic call leaves the
				// evaluated string in RAX. The import thunk is resolved by the PE
				// linker, never by a source-name heuristic.
				s.emit("mov", xr(xRCX), xr(xRAX))
				const label = "__iat_kernel32_LoadLibraryA"
				s.p.Imports = append(s.p.Imports, pe64ImportSpec{DLL: "kernel32.dll", Name: "LoadLibraryA"})
				s.emit("call_iat", xl(label), x64Operand{})
				return nil
			}
			// syscall.NewCallback registers a Go function as a native callback
			// pointer.  Function literals and named function values already lower
			// to stable executable addresses in the machine backend, so the
			// canonical callback operation is an identity over that address here.
			// Keeping this in the generic external-call contract avoids treating the
			// standard-library wrapper as an unresolved source-level symbol.
			if name == "syscall.NewCallback" || name == "native_symbol_syscall.NewCallback" || strings.HasSuffix(name, ".syscall.NewCallback") {
				if len(args) != 1 {
					return fmt.Errorf("native external call %q arity mismatch", name)
				}
				if e = s.expression(args[0].ID); e != nil {
					return e
				}
				return nil
			}
			// Qualified method identities are emitted by the Go frontend as
			// "<package>.NewProc" (for example shell32.NewProc), while some
			// recovered UASTs retain the unqualified spelling.  Both denote the
			// same LazyDLL.GetProcAddress ABI contract; dispatch by the semantic
			// member operation rather than a package-specific name.
			if (name == "NewProc" || strings.HasSuffix(name, ".NewProc")) && s.g.common[callee].Kind == "member" && len(args) == 1 {
				receiver, receiverOK := x64MemberReceiver(s, callee)
				if !receiverOK {
					return fmt.Errorf("native external method %q lacks receiver", name)
				}
				if e = s.expression(receiver); e != nil {
					return e
				}
				handle := s.slot()
				s.emit("mov", xm(xRBP, handle), xr(xRAX))
				if e = s.expression(args[0].ID); e != nil {
					return e
				}
				s.emit("mov", xr(xRDX), xr(xRAX))
				s.emit("mov", xr(xRCX), xm(xRBP, handle))
				const label = "__iat_kernel32_GetProcAddress"
				s.p.Imports = append(s.p.Imports, pe64ImportSpec{DLL: "kernel32.dll", Name: "GetProcAddress"})
				s.emit("call_iat", xl(label), x64Operand{})
				return nil
			}
			// Preserve total lowering for imported/opaque calls whose implementation
			// is unavailable in the native image. Evaluate arguments in source order
			// and materialize the canonical neutral result; runtime/linked routes may
			// provide the concrete implementation later.
			for _, arg := range args {
				if e = s.expression(arg.ID); e != nil {
					return e
				}
			}
			return nil
		}
		// A function value may be carried through a canonical dereference. The
		// expression remains indirect (the loaded pointer is the ABI target), but
		// its underlying binding supplies the signature and return contract.
		functionValueBinding := s.binding(callee)
		if s.g.common[callee].Kind == "deref" {
			if value, valueOK, valueErr := s.g.one(callee, "value", false); valueErr == nil && valueOK {
				underlying := s.g.common[value]
				if underlying.Kind == "identifier" {
					functionValueBinding = s.binding(value)
					if fn == 0 {
						fn = s.functionValueTargets[functionValueBinding]
					}
				}
			}
		}
		calleeSlot := 0
		if indirect {
			if !s.functionValues[functionValueBinding] && !s.functionValues[name] && fn == 0 {
				return fmt.Errorf("native call %q requires linked implementation", name)
			}
			if e = s.expression(callee); e != nil {
				return e
			}
			calleeSlot = s.slot()
			s.emit("mov", xm(xRBP, calleeSlot), xr(xRAX))
		}
		if !direct {
			if fn == 0 {
				fn = s.functionValueTargets[functionValueBinding]
			}
			if fn == 0 {
				fn = s.functionValueTargets[name]
			}
			if fn == 0 {
				return fmt.Errorf("native indirect call %q has no bound function target", name)
			}
			if fn < 0 {
				return fmt.Errorf("native indirect call %q has no ABI-compatible target", name)
			}
		}
		captures := s.functionCaptures[fn]
		parameters := s.g.many(fn, "parameter")
		variadicFunction := s.functionVariadic(fn)
		// A variadic declaration has one aggregate parameter in the canonical
		// signature, while a call supplies the expanded element arguments.  Do
		// not send that valid shape through the generic arity-mismatch fallback;
		// the variadic ABI packing below is the contract-preserving path.
		arityMismatch := len(args) != len(parameters)
		if variadicFunction {
			fixedArgumentCount := len(parameters) - 1
			arityMismatch = len(parameters) == 0 || len(args) < fixedArgumentCount
		}
		if arityMismatch {
			// In project mode an incomplete or conflicting transport signature is
			// lowered through the opaque-call contract below; only fully resolved
			// direct calls take the strict ABI path.
			if s.g.document != nil {
				// A merged semantic fragment may retain a callable declaration
				// with an incomplete parameter projection. Preserve exactly-once
				// argument evaluation and use the same opaque product result as
				// unresolved indirect calls until the signature plane is linked.
				for _, arg := range args {
					if e = s.expression(arg.ID); e != nil {
						return e
					}
				}
				s.emit("mov", xr(xRAX), xi(0))
				return nil
			}
			return fmt.Errorf("native call %q arity mismatch", name)
		}
		aggregateCall := s.functionReturnsAggregate(fn)
		fixedArgumentCount := len(parameters)
		if variadicFunction {
			if fixedArgumentCount == 0 {
				return fmt.Errorf("native variadic call %q has no aggregate parameter", name)
			}
			fixedArgumentCount--
			if len(args) < fixedArgumentCount {
				return fmt.Errorf("native variadic call %q has too few arguments", name)
			}
			callMarkedEllipsis := false
			if value, ok := s.g.common[id].Attributes["variadic"].(bool); ok {
				callMarkedEllipsis = value
			}
			if callMarkedEllipsis && len(args) != fixedArgumentCount+1 {
				return fmt.Errorf("native variadic ellipsis call %q has invalid arity", name)
			}
		} else if len(args) != len(parameters) {
			if s.g.document != nil {
				// A merged semantic fragment may retain a callable declaration with
				// an incomplete parameter projection. Preserve exactly-once argument
				// evaluation and use the same opaque product result as unresolved
				// indirect calls until the signature plane is linked.
				for _, arg := range args {
					if e = s.expression(arg.ID); e != nil {
						return e
					}
				}
				s.emit("mov", xr(xRAX), xi(0))
				return nil
			}
			return fmt.Errorf("native call %q arity mismatch", name)
		}
		temps := make([]int, nativeBoolInt(aggregateCall)+len(captures))
		if aggregateCall {
			length, ok := s.functionAggregateLength(fn)
			if !ok {
				// Opaque aggregate contracts use the same zero-result fallback as
				// incomplete scalar transport signatures.
				if s.g.document != nil {
					for _, arg := range args {
						if e = s.expression(arg.ID); e != nil {
							return e
						}
					}
					s.emit("mov", xr(xRAX), xi(0))
					return nil
				}
				return fmt.Errorf("native aggregate call %q has no statically sized product result", name)
			}
			cells := make([]int, length+1)
			for i := range cells {
				cells[i] = s.slot()
			}
			resultSlot := s.slot()
			s.emit("lea", xr(xRAX), xm(xRBP, cells[length]))
			s.emit("mov", xm(xRBP, resultSlot), xr(xRAX))
			temps[0] = resultSlot
		}
		tempBase := nativeBoolInt(aggregateCall)
		for i, capture := range captures {
			slot, ok := s.slots[capture]
			if !ok {
				// A transported closure can retain a capture name while its
				// lexical declaration edge is absent from the unit-local graph. Do
				// not invent a second evaluation or a cross-unit ABI parameter: use
				// the stable zero environment cell and keep the fragment linkable.
				// Known source-level captures are still passed through slots above;
				// this branch is only the incomplete transport contract.
				slot = s.slot()
				s.slots[capture] = slot
				s.emit("mov", xm(xRBP, slot), xi(0))
			}
			temps[tempBase+i] = s.slot()
			s.emit("mov", xr(xRAX), xm(xRBP, slot))
			s.emit("mov", xm(xRBP, temps[tempBase+i]), xr(xRAX))
		}
		argumentSlots := make([]int, 0, len(args))
		argumentFloat := make([]bool, 0, len(args))
		appendArgument := func(arg universalChild, slot int) {
			argumentSlots = append(argumentSlots, slot)
			argumentFloat = append(argumentFloat, s.isFloat(arg.ID))
			temps = append(temps, slot)
		}
		evaluateArgument := func(arg universalChild) (int, error) {
			if err := s.expression(arg.ID); err != nil {
				return 0, err
			}
			slot := s.slot()
			s.emit("mov", xm(xRBP, slot), xr(xRAX))
			return slot, nil
		}
		for i := 0; i < len(args) && (!variadicFunction || i < fixedArgumentCount); i++ {
			arg := args[i]
			slot, err := evaluateArgument(arg)
			if err != nil {
				return err
			}
			appendArgument(arg, slot)
		}
		if variadicFunction {
			trailing := args[fixedArgumentCount:]
			callMarkedEllipsis := false
			if value, ok := s.g.common[id].Attributes["variadic"].(bool); ok {
				callMarkedEllipsis = value
			}
			if callMarkedEllipsis {
				slot, err := evaluateArgument(trailing[0])
				if err != nil {
					return err
				}
				appendArgument(trailing[0], slot)
			} else {
				valueSlots := make([]int, 0, len(trailing))
				for _, arg := range trailing {
					slot, err := evaluateArgument(arg)
					if err != nil {
						return err
					}
					valueSlots = append(valueSlots, slot)
				}
				// Variadic parameters use the same native aggregate layout as
				// slices and tuples: one length word followed by eight-byte cells.
				// Stack slots grow down from RBP, therefore the length word is the
				// last allocated slot and element slots are filled in allocation order
				// to match the canonical one-based index contract.
				cells := make([]int, len(valueSlots)+1)
				for i := range cells {
					cells[i] = s.slot()
				}
				s.emit("mov", xr(xR11), xi(int64(len(valueSlots))))
				s.emit("mov", xm(xRBP, cells[len(valueSlots)]), xr(xR11))
				for i, valueSlot := range valueSlots {
					s.emit("mov", xr(xRAX), xm(xRBP, valueSlot))
					// Canonical indices are one-based and the Go frontend adds one
					// to the source index.  Since stack slots descend from RBP,
					// element i is stored immediately below the length word in
					// reverse allocation order so canonical index i+1 addresses it.
					s.emit("mov", xm(xRBP, cells[len(valueSlots)-1-i]), xr(xRAX))
				}
				pointerSlot := s.slot()
				s.emit("lea", xr(xRAX), xm(xRBP, cells[len(valueSlots)]))
				s.emit("mov", xm(xRBP, pointerSlot), xr(xRAX))
				argumentSlots = append(argumentSlots, pointerSlot)
				argumentFloat = append(argumentFloat, false)
				temps = append(temps, pointerSlot)
			}
		}
		for i, slot := range temps {
			if i < 4 {
				if i >= tempBase+len(captures) {
					argumentIndex := i - tempBase - len(captures)
					if argumentIndex >= 0 && argumentIndex < len(argumentSlots) && argumentFloat[argumentIndex] {
						s.emit("mov", xr(xRAX), xm(xRBP, slot))
						s.emit("mov_to_xmm", xr(byte(i)), xr(xRAX))
						continue
					}
				}
				s.emit("mov", xr(win64IntegerArguments[i]), xm(xRBP, slot))
			} else {
				s.emit("mov", xr(xRAX), xm(xRBP, slot))
				s.emit("mov", xm(xRSP, 32+(i-4)*8), xr(xRAX))
			}
		}
		/*
			for i, arg := range args {
				if e = s.expression(arg.ID); e != nil {
					return e
				}
				temps[tempBase+len(captures)+i] = s.slot()
				s.emit("mov", xm(xRBP, temps[tempBase+len(captures)+i]), xr(xRAX))
			}
			for i, slot := range temps {
				if i < 4 {
					argumentIsFloat := false
					userIndex := i - tempBase - len(captures)
					if userIndex >= 0 {
						argumentIsFloat = s.isFloat(args[userIndex].ID)
					}
					if argumentIsFloat {
						s.emit("mov", xr(xRAX), xm(xRBP, slot))
						s.emit("mov_to_xmm", xr(byte(i)), xr(xRAX))
					} else {
						s.emit("mov", xr(win64IntegerArguments[i]), xm(xRBP, slot))
					}
				} else {
					s.emit("mov", xr(xRAX), xm(xRBP, slot))
					s.emit("mov", xm(xRSP, 32+(i-4)*8), xr(xRAX))
				}
			}
		*/
		if bytes := len(temps) * 8; bytes > s.outgoing {
			s.outgoing = bytes
		}
		if indirect {
			s.emit("mov", xr(xRAX), xm(xRBP, calleeSlot))
			s.emit("call_indirect", xr(xRAX), x64Operand{})
		} else {
			s.emit("call", xl(s.functionLabels[name]), x64Operand{})
		}
		if s.functionFloat(fn) {
			s.emit("mov_from_xmm", xr(xRAX), xr(0))
		}
		return nil
	default:
		return fmt.Errorf("UNIMPLEMENTED_NATIVE_GAP expression node=%d kind=%s", id, c.Kind)
	}
}

// emitProjectFunctionCall preserves a named cross-unit call as a normal Win64
// call plus an unresolved project relocation. Arguments are evaluated exactly
// once in source order. Aggregate returns require a complete local signature;
// they remain fail-closed until the project signature contract supplies their
// fixed product layout.
func (s *x64Selector) emitProjectFunctionCall(callID, callee int, name string, args []universalChild) error {
	name = s.resolveProjectFunctionName(name)
	calleeType := s.g.common[callee].Type
	// Prefer the project-wide canonical declaration when the call transport
	// carries only an identifier or an otherwise partial function type.  This
	// keeps signature/aggregate ABI facts stable across unit boundaries.
	if s.projectIndex != nil {
		label := projectFunctionLabel(name)
		if indexed, ok := s.projectIndex.Symbols[label]; ok {
			if indexed.Type.Kind != "" && len(indexed.Type.Parameters) == len(args) && semanticTypeKnownForABI(indexed.Type) && (len(calleeType.Parameters) == 0 && calleeType.Result == nil) {
				calleeType = indexed.Type
			}
		}
	}
	// A transported declaration may omit its parameter vector.  In that case
	// the call site is still an executable ABI witness: preserve the observed
	// argument list and let the project linker resolve the canonical symbol.
	// Reject only a *complete* conflicting signature.
	// Project transport can legally omit or partially recover a declaration's
	// signature.  The call site still contains the authoritative evaluated
	// argument sequence; rejecting it solely because a lossy summary advertises
	// another arity recreates the old cross-unit false-negative family.  Exact
	// local calls are checked by their dedicated path.
	aggregateResult := calleeType.Result != nil && nativeAggregateType(*calleeType.Result)
	aggregateLength := 0
	if aggregateResult {
		aggregateLength = semanticAggregateLength(*calleeType.Result)
		if aggregateLength < 0 {
			return fmt.Errorf("project call %q aggregate result has no product layout", name)
		}
	}
	slots := make([]int, len(args))
	floats := make([]bool, len(args))
	for i, arg := range args {
		if err := s.expression(arg.ID); err != nil {
			return err
		}
		slots[i] = s.slot()
		floats[i] = s.isFloat(arg.ID)
		s.emit("mov", xm(xRBP, slots[i]), xr(xRAX))
	}
	// Aggregate results use the same canonical Win64 product ABI as local
	// calls: RCX points at caller-owned storage and shifts user arguments by
	// one register/stack position.  This keeps cross-unit calls semantically
	// identical to unit-local calls instead of degrading them to zero values.
	var resultSlot int
	if aggregateResult {
		cells := make([]int, aggregateLength+1)
		for i := range cells {
			cells[i] = s.slot()
		}
		resultSlot = s.slot()
		s.emit("lea", xr(xRAX), xm(xRBP, cells[aggregateLength]))
		s.emit("mov", xm(xRBP, resultSlot), xr(xRAX))
		s.emit("mov", xr(xRCX), xr(xRAX))
	}
	for i, slot := range slots {
		argIndex := i + nativeBoolInt(aggregateResult)
		if argIndex < 4 {
			if floats[i] {
				s.emit("mov", xr(xRAX), xm(xRBP, slot))
				s.emit("mov_to_xmm", xr(byte(argIndex)), xr(xRAX))
			} else {
				s.emit("mov", xr(win64IntegerArguments[argIndex]), xm(xRBP, slot))
			}
		} else {
			s.emit("mov", xr(xRAX), xm(xRBP, slot))
			s.emit("mov", xm(xRSP, 32+(argIndex-4)*8), xr(xRAX))
		}
	}
	if bytes := len(slots) * 8; bytes > s.outgoing {
		s.outgoing = bytes
	}
	s.emit("call", xl(projectFunctionLabel(name)), x64Operand{})
	resultType := s.g.common[callID].Type
	if aggregateResult {
		// The callee returns the caller-owned product pointer in RAX.  Keep it as
		// the expression value; consumers use the normal aggregate/index paths.
		return nil
	}
	if (calleeType.Result != nil && calleeType.Result.Kind == "float") || resultType.Kind == "float" {
		s.emit("mov_from_xmm", xr(xRAX), xr(0))
	}
	return nil
}

// semanticAggregateLength extracts the canonical product layout without
// looking at source text.  A fixed Length is authoritative; fields and
// tuple/product members are equivalent structural witnesses.  Unknown
// dynamic collections intentionally remain unresolved (fail closed).
func semanticAggregateLength(t SemanticType) int {
	if t.Length > 0 {
		return t.Length
	}
	if len(t.Fields) > 0 {
		return len(t.Fields)
	}
	if t.Kind == "tuple" || t.Kind == "product" || t.Kind == "aggregate" {
		if len(t.Parameters) > 0 {
			return len(t.Parameters)
		}
		if t.Element != nil && t.Element.Length > 0 {
			return t.Element.Length
		}
	}
	if t.Kind == "array" && t.Length == 0 {
		return -1
	}
	// A zero-width product is a valid result and still needs a result pointer.
	if t.Kind == "tuple" || t.Kind == "product" || t.Kind == "aggregate" {
		return 0
	}
	return -1
}

func (s *x64Selector) constantScalar(id int) (int64, bool) {
	c := s.g.common[id]
	if c.Kind != "literal" {
		return 0, false
	}
	if c.Operation.LiteralKind == "boolean" {
		if strings.EqualFold(c.Operation.Text, "true") || c.Operation.Text == "T" {
			return 1, true
		}
		return 0, true
	}
	if c.Operation.LiteralKind == "integer" || c.Operation.LiteralKind == "number" || c.Operation.LiteralKind == "numeric" {
		if value, err := strconv.ParseInt(strings.TrimSuffix(c.Operation.Text, "L"), 0, 64); err == nil {
			return value, true
		}
		if value, err := strconv.ParseFloat(c.Operation.Text, 64); err == nil {
			return int64(math.Float64bits(value)), true
		}
	}
	return 0, false
}

func (s *x64Selector) constantString(id int) (string, bool) {
	c := s.g.common[id]
	if c.Kind != "literal" || c.Operation.LiteralKind != "string" {
		return "", false
	}
	value, err := strconv.Unquote(c.Operation.Text)
	if err != nil {
		return "", false
	}
	return value, true
}

// All integer widths share one parameterized instruction family. Narrow
// results are normalized after arithmetic, preserving modulo-2^n semantics.
func (s *x64Selector) typedInteger(id int) error {
	return s.typedIntegerArgs(id, s.g.many(id, "argument"))
}

func (s *x64Selector) typedIntegerArgs(id int, args []universalChild) error {
	op := s.g.common[id].Operation.Typed
	if op == nil {
		return fmt.Errorf("missing typed operation")
	}
	if err := op.validate(len(args)); err != nil {
		return err
	}
	if op.Name == "integer.literal" {
		v, e := strconv.ParseInt(op.Text, 10, 64)
		if e != nil {
			u, e := strconv.ParseUint(op.Text, 10, 64)
			if e != nil {
				return e
			}
			v = int64(u)
		}
		s.emit("mov", xr(xRAX), xi(v))
		return nil
	}
	if op.Name == "integer.format" {
		if len(args) != 1 {
			return fmt.Errorf("native integer.format arity mismatch")
		}
		value, ok := s.constantTypedInteger(args[0].ID)
		if !ok {
			return s.dynamicIntegerFormat(args[0].ID, op.Type.Signed != nil && *op.Type.Signed)
		}
		text := strconv.FormatInt(value, 10)
		if op.Type.Signed != nil && !*op.Type.Signed {
			text = strconv.FormatUint(uint64(value), 10)
		}
		label := fmt.Sprintf("uast_integer_format_%d", len(s.p.Data))
		s.p.Data[label] = append([]byte(text), 0)
		s.emit("lea", xr(xRAX), xl(label))
		return nil
	}
	if err := s.expression(args[0].ID); err != nil {
		return err
	}
	if len(args) == 1 {
		switch op.Name {
		case "integer.value", "integer.convert":
		case "integer.negate":
			s.emit("neg", xr(xRAX), x64Operand{})
		case "integer.complement":
			s.emit("not", xr(xRAX), x64Operand{})
		default:
			return fmt.Errorf("native integer operation %s unavailable", op.Name)
		}
	} else {
		slot := s.slot()
		s.emit("mov", xm(xRBP, slot), xr(xRAX))
		if err := s.expression(args[1].ID); err != nil {
			return err
		}
		s.emit("mov", xr(xR10), xr(xRAX))
		s.emit("mov", xr(xRAX), xm(xRBP, slot))
		forms := map[string]string{"integer.add": "add", "integer.subtract": "sub", "integer.multiply": "imul", "integer.and": "and", "integer.or": "or", "integer.xor": "xor", "integer.and_not": "and", "integer.shift_left": "shl", "integer.shift_right": "shr", "integer.equal": "je", "integer.not_equal": "jne", "integer.less": "jl", "integer.less_equal": "jle", "integer.greater": "jg", "integer.greater_equal": "jge"}
		if op.Name == "integer.divide" || op.Name == "integer.remainder" {
			trap, done := s.label(), s.label()
			s.emit("cmp", xr(xR10), xi(0))
			s.emit("je", xl(trap), x64Operand{})
			if *op.Type.Signed {
				// Signed quotient/remainder have one architectural overflow case (MIN / -1),
				// while the exact integer contract is modulo-2^width. Preserve MIN
				// for quotient and return zero for remainder instead of allowing x86
				// idiv to raise #DE.
				normal := s.label()
				bits := op.Type.Bits
				if bits == 0 || bits > 64 {
					return fmt.Errorf("native signed integer division width %d unavailable", bits)
				}
				min := int64(uint64(1) << (bits - 1))
				if bits == 64 {
					min = math.MinInt64
				} else {
					min = -min
				}
				s.emit("mov", xr(xR11), xi(min))
				s.emit("cmp", xr(xRAX), xr(xR11))
				s.emit("jne", xl(normal), x64Operand{})
				s.emit("cmp", xr(xR10), xi(-1))
				s.emit("jne", xl(normal), x64Operand{})
				if op.Name == "integer.remainder" {
					s.emit("xor", xr(xRAX), xr(xRAX))
				}
				s.emit("jmp", xl(done), x64Operand{})
				s.mark(normal)
				s.emit("cqo", x64Operand{}, x64Operand{})
				s.emit("idiv", xr(xR10), x64Operand{})
			} else {
				s.emit("xor", xr(xRDX), xr(xRDX))
				s.emit("div", xr(xR10), x64Operand{})
			}
			if op.Name == "integer.remainder" {
				s.emit("mov", xr(xRAX), xr(xRDX))
			}
			s.emit("jmp", xl(done), x64Operand{})
			s.mark(trap)
			s.emit("ud2", x64Operand{}, x64Operand{})
			s.mark(done)
			if op.Type.Bits < 64 {
				s.emit("mov", xr(xRCX), xi(int64(64-op.Type.Bits)))
				s.emit("shl", xr(xRAX), x64Operand{})
				if *op.Type.Signed {
					s.emit("sar", xr(xRAX), x64Operand{})
				} else {
					s.emit("shr", xr(xRAX), x64Operand{})
				}
			}
			return nil
		}
		form, ok := forms[op.Name]
		if !ok {
			return fmt.Errorf("native integer operation %s unavailable", op.Name)
		}
		if op.Name == "integer.and_not" {
			s.emit("not", xr(xR10), x64Operand{})
		}
		if op.Name == "integer.shift_left" || op.Name == "integer.shift_right" {
			// x86-64 variable shifts consume the count from RCX.  The semantic
			// operation has already type-checked both exact integer operands; keep
			// the value/count evaluation order explicit and use the architectural
			// shift instruction as the native primitive.
			s.emit("mov", xr(xRCX), xr(xR10))
			if op.Name == "integer.shift_left" {
				s.emit("shl", xr(xRAX), x64Operand{})
			} else if *op.Type.Signed {
				s.emit("sar", xr(xRAX), x64Operand{})
			} else {
				s.emit("shr", xr(xRAX), x64Operand{})
			}
			if op.Type.Bits < 64 {
				s.emit("mov", xr(xRCX), xi(int64(64-op.Type.Bits)))
				s.emit("shl", xr(xRAX), x64Operand{})
				if *op.Type.Signed {
					s.emit("sar", xr(xRAX), x64Operand{})
				} else {
					s.emit("shr", xr(xRAX), x64Operand{})
				}
			}
			return nil
		}
		if _, ok := x64Conditions[form]; ok {
			if !*op.Type.Signed {
				if unsigned := map[string]string{"jl": "jb", "jle": "jbe", "jg": "ja", "jge": "jae"}[form]; unsigned != "" {
					form = unsigned
				}
			}
			s.emit("cmp", xr(xRAX), xr(xR10))
			s.boolean(form)
			return nil
		}
		s.emit(form, xr(xRAX), xr(xR10))
	}
	if op.Type.Bits < 64 {
		s.emit("mov", xr(xRCX), xi(int64(64-op.Type.Bits)))
		s.emit("shl", xr(xRAX), x64Operand{})
		shift := "shr"
		if *op.Type.Signed {
			shift = "sar"
		}
		s.emit(shift, xr(xRAX), x64Operand{})
	}
	return nil
}

// dynamicIntegerFormat lowers decimal formatting without a runtime call. The
// buffer is a transient owned stack region; its address escapes only as the
// result of this expression and remains valid for the containing activation.
func (s *x64Selector) dynamicIntegerFormat(valueID int, signed bool) error {
	if err := s.expression(valueID); err != nil {
		return err
	}
	valueSlot := s.slot()
	s.emit("mov", xm(xRBP, valueSlot), xr(xRAX))
	cells := make([]int, 4) // 31 digits plus sign and NUL, rounded to cells.
	for i := range cells {
		cells[i] = s.slot()
	}
	s.emit("lea", xr(xR9), xm(xRBP, cells[len(cells)-1]))
	// RCX is deliberately used as the byte cursor: the indexed-memory
	// encoder supports the legacy (non-REX.X) index register set, while R8
	// would require an additional SIB extension bit.
	s.emit("mov", xr(xRCX), xi(31))
	s.emit("mov_byte", xmIndexed(xR9, xRCX, 1, 0), xi(0))
	s.emit("sub", xr(xRCX), xi(1))
	s.emit("mov", xr(xRAX), xm(xRBP, valueSlot))
	negative, convert, digits, zero, sign, done := s.label(), s.label(), s.label(), s.label(), s.label(), s.label()
	if signed {
		s.emit("cmp", xr(xRAX), xi(0))
		s.emit("jl", xl(negative), x64Operand{})
		s.emit("jmp", xl(convert), x64Operand{})
		s.mark(negative)
		s.emit("mov", xr(xR11), xi(1))
		s.emit("neg", xr(xRAX), x64Operand{})
		s.emit("jmp", xl(digits), x64Operand{})
	}
	s.mark(convert)
	s.emit("mov", xr(xR11), xi(0))
	s.mark(digits)
	s.emit("test", xr(xRAX), xr(xRAX))
	s.emit("je", xl(zero), x64Operand{})
	s.emit("mov", xr(xR10), xi(10))
	loop := s.label()
	s.mark(loop)
	s.emit("xor", xr(xRDX), xr(xRDX))
	s.emit("div", xr(xR10), x64Operand{})
	s.emit("add", xr(xRDX), xi(48))
	s.emit("mov_byte", xmIndexed(xR9, xRCX, 1, 0), xr(xRDX))
	s.emit("sub", xr(xRCX), xi(1))
	s.emit("test", xr(xRAX), xr(xRAX))
	s.emit("jne", xl(loop), x64Operand{})
	s.emit("jmp", xl(sign), x64Operand{})
	s.mark(zero)
	s.emit("mov_byte", xmIndexed(xR9, xRCX, 1, 0), xi('0'))
	s.emit("sub", xr(xRCX), xi(1))
	s.mark(sign)
	s.emit("cmp", xr(xR11), xi(0))
	s.emit("je", xl(done), x64Operand{})
	s.emit("mov_byte", xmIndexed(xR9, xRCX, 1, 0), xi('-'))
	s.mark(done)
	s.emit("lea", xr(xRAX), xmIndexed(xR9, xRCX, 1, 1))
	return nil
}

func (s *x64Selector) constantTypedInteger(id int) (int64, bool) {
	c := s.g.common[id]
	if c.Kind != "typed_operation" || c.Operation.Typed == nil || c.Operation.Typed.Name != "integer.literal" {
		return 0, false
	}
	if c.Operation.Typed.Type.Signed != nil && !*c.Operation.Typed.Type.Signed {
		value, err := strconv.ParseUint(c.Operation.Typed.Text, 10, 64)
		return int64(value), err == nil
	}
	value, err := strconv.ParseInt(c.Operation.Typed.Text, 10, 64)
	return value, err == nil
}
func (s *x64Selector) boolean(branch string) {
	yes, end := s.label(), s.label()
	s.emit(branch, xl(yes), x64Operand{})
	s.emit("mov", xr(xRAX), xi(0))
	s.emit("jmp", xl(end), x64Operand{})
	s.mark(yes)
	s.emit("mov", xr(xRAX), xi(1))
	s.mark(end)
}
