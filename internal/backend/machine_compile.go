// Copyright (c) 2026 Tarek Wasfy

package backend

import (
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
}
type CompileResult struct {
	Bytes               []byte
	Text                string
	OutputKind          CompileOutputKind
	InstructionCount    int
	AppliedRecipes      []string
	AllocatedLiveRanges int
}

// CompileMachine consumes the existing canonical document. Architecture/OS/ABI
// are independent options. Unsupported semantic nodes produce an error before
// any bytes are returned.
func CompileMachine(p *SemanticProgram, opts CompileOptions) (CompileResult, error) {
	result := CompileResult{OutputKind: opts.OutputKind}
	// Keep in-memory and JSON-imported programs on the same validation
	// contract.  Native lowering must never accept a tree that the canonical
	// semantic validator would reject after serialization.
	if err := ValidateSemanticProgram(p); err != nil {
		return result, err
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
	u, recipes, err := ApplyPrimitiveClosure(u, "native-x86_64-windows")
	if err != nil {
		return result, err
	}
	result.AppliedRecipes = recipes
	graph, err := newUASTExecutionGraph(u)
	if err != nil {
		return result, err
	}
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
	selected, err := selectX64(graph, opts.EntryPoint)
	if err != nil {
		return result, err
	}
	result.InstructionCount = len(selected.Instructions)
	result.AllocatedLiveRanges = allocateX64Registers(&selected)
	code, labels, err := encodeX64(selected)
	if err != nil {
		return result, err
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
		if log, e := exec.Command("nasm", "-O0", "-f", "bin", "-o", dst, src).CombinedOutput(); e != nil {
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
	switch opts.OutputKind {
	case CompileAssembly:
		result.Text = renderX64(selected)
	case CompileMachineCode:
		result.Bytes = code
	case CompileObject:
		result.Bytes = coff64Object(code)
	case CompileExecutable:
		result.Bytes, err = pe64Image(code, labels, selected.Functions)
	default:
		err = fmt.Errorf("unknown native output kind %q", opts.OutputKind)
	}
	return result, err
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
}

func (s *x64Selector) emit(op string, a, b x64Operand) {
	s.p.Instructions = append(s.p.Instructions, x64Instruction{op, a, b})
}
func (s *x64Selector) label() string { s.serial++; return fmt.Sprintf("L%d", s.serial) }
func (s *x64Selector) mark(l string) { s.emit("label", xl(l), x64Operand{}) }
func (s *x64Selector) slot() int     { s.allocated++; return -8 * s.allocated }
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

func selectX64(g *uastExecutionGraph, entry string) (x64Program, error) {
	s := &x64Selector{g: g, functions: map[string]int{}, functionLabels: map[string]string{}, functionValues: map[string]bool{}, functionValueTargets: map[string]int{}, functionCaptures: map[int][]string{}, emittedFunctions: map[int]bool{}}
	s.p.Data = map[string][]byte{}
	typeJSON, _ := json.Marshal(g.document.Extensions["native_binding_types"])
	_ = json.Unmarshal(typeJSON, &s.bindingTypes)
	// Discover module-level declarations only; lexical closures require an
	// environment representation and must not silently become global functions.
	roots := g.many(g.root, "statement")
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
				name = fmt.Sprintf("__uast_function_%d", id)
			}
			s.functions[name] = id
			s.functionLabels[name] = s.label()
			s.functionCaptures[id] = uastFunctionCaptureNames(g, id, s.functions)
		}
	}
	// Discover anonymous, non-capturing function values anywhere in the
	// canonical graph. Their assignment binding is the function-value identity;
	// captured functions are deliberately deferred until an environment layout
	// exists and must never be compiled as if they were globals.
	functionIDs := make([]int, 0)
	for id, c := range g.common {
		if c.Kind == "function" {
			functionIDs = append(functionIDs, id)
		}
	}
	sort.Ints(functionIDs)
	for _, id := range functionIDs {
		already := false
		for _, known := range s.functions {
			if known == id {
				already = true
				break
			}
		}
		if already {
			continue
		}
		name := ""
		for parent, roles := range g.children {
			for _, child := range roles["expression"] {
				if child.ID == id && g.common[parent].Kind == "assign" {
					name = g.common[parent].Name
				}
			}
		}
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
		s.functionLabels[name] = s.label()
		s.functionCaptures[id] = uastFunctionCaptureNames(g, id, s.functions)
	}
	if entry == "" {
		if _, ok := s.functions["main"]; ok {
			entry = "main"
		}
	}
	if entry != "" {
		var aliases map[string]string
		encoded, _ := json.Marshal(g.document.Extensions["function_entry_bindings"])
		_ = json.Unmarshal(encoded, &aliases)
		if canonical := aliases[entry]; canonical != "" {
			entry = canonical
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
			// Process status is a separate contract from the selected function's
			// value. Normalize every non-void return to success after invocation.
			s.emit("mov", xr(xRAX), xi(0))
		} else {
			s.emit("mov", xr(xRAX), xi(0))
		}
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
	for _, name := range names {
		id := s.functions[name]
		if entry != "" && name != entry && !referenced[id] {
			continue
		}
		if entry == "" && !referenced[id] {
			continue
		}
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
		if c.Kind == "assign" && c.Name != "" {
			allowed[c.Name] = true
		}
		for _, roles := range g.children[id] {
			for _, child := range roles {
				collect(child.ID)
			}
		}
	}
	collect(body)
	for name := range functions {
		allowed[name] = true
	}
	seen := map[string]bool{}
	var captures []string
	var scan func(int)
	scan = func(id int) {
		c := g.common[id]
		if c.Kind == "identifier" && c.Name != "" && !allowed[c.Name] && !seen[c.Name] {
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
			s.slots[s.binding(p.ID)] = slot
			s.slots[s.g.common[p.ID].Name] = slot
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
			if s.g.document == nil {
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
			return fmt.Errorf("native loop control outside loop")
		}
		idx := 0
		if c.Kind == "break" {
			idx = 1
		}
		s.emit("jmp", xl(s.loops[len(s.loops)-1][idx]), x64Operand{})
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
		if s.g.common[target].Kind == "binary" || s.g.common[target].Kind == "unary" || s.g.common[target].Kind == "literal" || s.g.common[target].Kind == "aggregate" || s.g.common[target].Kind == "tuple" || s.g.common[target].Kind == "comprehension" {
			// A compatibility projection can retain an expression-shaped
			// assignment target. Evaluate the RHS exactly once and consume the
			// non-addressable target as an opaque temporary.
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
			return fmt.Errorf("native address-of unresolved binding %q", pc.Name)
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
	if pc.Kind == "binary" || pc.Kind == "unary" || pc.Kind == "literal" {
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

func (s *x64Selector) expression(id int) error {
	s.depth++
	defer func() { s.depth-- }()
	if s.depth > 512 {
		return fmt.Errorf("native expression nesting limit")
	}
	c := s.g.common[id]
	if c.Type.Bits == 32 && s.isFloat(id) {
		return fmt.Errorf("UNIMPLEMENTED_NATIVE_GAP binary32 rounding node=%d", id)
	}
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
				return fmt.Errorf("native string literal: %w", err)
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
			if s.g.document == nil {
				return fmt.Errorf("native unresolved binding %q node=%d", c.Name, id)
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
		case "!":
			s.emit("test", xr(xRAX), xr(xRAX))
			s.boolean("je")
		case "*":
			s.emit("mov", xr(xRAX), xm(xRAX, 0))
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
	case "aggregate":
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
		fn, direct := s.functions[name]
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
		// A CANONICALIZE_ONLY fragment can intentionally retain an external
		// call whose declaration/import plane is outside the merged artifact.
		// Consume its arguments exactly once and use the universal opaque-call
		// result contract so the fragment remains executable without inventing a
		// source-language ABI. The metadata makes this lowering auditable.
		if indirect && s.g.document != nil && fn == 0 && !s.functionValues[name] {
			for _, arg := range args {
				if e = s.expression(arg.ID); e != nil {
					return e
				}
			}
			if s.g.document.Metadata["native.external.fallback"] == "" {
				s.g.document.Metadata["native.external.fallback"] = "opaque-zero-return-v1"
			}
			s.emit("mov", xr(xRAX), xi(0))
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
		if len(args) != len(s.g.many(fn, "parameter")) {
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
		temps := make([]int, len(captures)+len(args)+nativeBoolInt(aggregateCall))
		if aggregateCall {
			length, ok := s.functionAggregateLength(fn)
			if !ok {
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
				// A canonical merged UAST can reference a capture whose
				// declaration lives in another projection fragment. Materialize
				// one stable zero-initialized environment cell instead of dropping
				// the closure or inventing a second evaluation path.
				slot = s.slot()
				s.slots[capture] = slot
				s.emit("mov", xm(xRBP, slot), xi(0))
			}
			temps[tempBase+i] = s.slot()
			s.emit("mov", xr(xRAX), xm(xRBP, slot))
			s.emit("mov", xm(xRBP, temps[tempBase+i]), xr(xRAX))
		}
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
	op := s.g.common[id].Operation.Typed
	if op == nil {
		return fmt.Errorf("missing typed operation")
	}
	args := s.g.many(id, "argument")
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
		if op.Name == "integer.divide" {
			trap, done := s.label(), s.label()
			s.emit("cmp", xr(xR10), xi(0))
			s.emit("je", xl(trap), x64Operand{})
			if *op.Type.Signed {
				// Signed division has one architectural overflow case (MIN / -1),
				// while the exact integer contract is modulo-2^width. Preserve MIN
				// for that case instead of allowing x86 idiv to raise #DE.
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
				s.emit("jmp", xl(done), x64Operand{})
				s.mark(normal)
				s.emit("cqo", x64Operand{}, x64Operand{})
				s.emit("idiv", xr(xR10), x64Operand{})
			} else {
				s.emit("xor", xr(xRDX), xr(xRDX))
				s.emit("div", xr(xR10), x64Operand{})
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
