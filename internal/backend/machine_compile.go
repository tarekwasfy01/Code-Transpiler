package backend

import (
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
	g              *uastExecutionGraph
	p              x64Program
	functions      map[string]int
	functionLabels map[string]string
	slots          map[string]int
	allocated      int
	outgoing       int
	serial         int
	returnLabel    string
	loops          [][2]string
	depth          int
	bindingTypes   map[string]SemanticType
	floatReturn    bool
}

func (s *x64Selector) emit(op string, a, b x64Operand) {
	s.p.Instructions = append(s.p.Instructions, x64Instruction{op, a, b})
}
func (s *x64Selector) label() string { s.serial++; return fmt.Sprintf("L%d", s.serial) }
func (s *x64Selector) mark(l string) { s.emit("label", xl(l), x64Operand{}) }
func (s *x64Selector) slot() int     { s.allocated++; return -8 * s.allocated }
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
	s := &x64Selector{g: g, functions: map[string]int{}, functionLabels: map[string]string{}}
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
			if name == "" {
				name = c.Name
			}
			if name == "" {
				return s.p, fmt.Errorf("UNIMPLEMENTED_NATIVE_GAP function binding node=%d", id)
			}
			s.functions[name] = id
			s.functionLabels[name] = s.label()
		}
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
			if s.functionFloat(s.functions[entry]) {
				return fmt.Errorf("native process entry must return an integer exit status")
			}
			if len(g.many(s.functions[entry], "parameter")) != 0 {
				return fmt.Errorf("entry requires arguments")
			}
			s.emit("call", xl(s.functionLabels[entry]), x64Operand{})
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
	for _, name := range names {
		id := s.functions[name]
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

func (s *x64Selector) function(label string, id int, body func() error) error {
	s.slots = map[string]int{}
	s.allocated = 0
	s.outgoing = 32
	s.returnLabel = s.label()
	s.loops = nil
	s.floatReturn = id >= 0 && s.functionFloat(id)
	s.mark(label)
	s.emit("push", xr(xRBP), x64Operand{})
	s.emit("mov", xr(xRBP), xr(xRSP))
	frameAt := len(s.p.Instructions)
	s.emit("sub_sp", xi(0), x64Operand{})
	if id >= 0 {
		for i, p := range s.g.many(id, "parameter") {
			slot := s.slot()
			s.slots[s.binding(p.ID)] = slot
			s.slots[s.g.common[p.ID].Name] = slot
			if i < 4 {
				if s.isFloat(p.ID) {
					s.emit("mov_from_xmm", xr(xRAX), xr(byte(i)))
					s.emit("mov", xm(xRBP, slot), xr(xRAX))
				} else {
					s.emit("mov", xm(xRBP, slot), xr(win64IntegerArguments[i]))
				}
			} else {
				s.emit("mov", xr(xRAX), xm(xRBP, 48+(i-4)*8))
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
	if frame >= 4096 {
		return fmt.Errorf("native stack probe required for frame %d", frame)
	}
	s.p.Instructions[frameAt].A = xi(int64(frame))
	s.p.Functions = append(s.p.Functions, x64Function{label, end, frame})
	return nil
}

func (s *x64Selector) statement(id int) error {
	c := s.g.common[id]
	switch c.Kind {
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
		key := s.binding(id)
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
		} else {
			s.emit("mov", xr(xRAX), xi(0))
		}
		s.emit("jmp", xl(s.returnLabel), x64Operand{})
		return nil
	case "if":
		cond, e := s.child(id, "condition")
		if e != nil {
			return e
		}
		if e = s.expression(cond); e != nil {
			return e
		}
		other, end := s.label(), s.label()
		s.emit("test", xr(xRAX), xr(xRAX))
		s.emit("je", xl(other), x64Operand{})
		yes, e := s.child(id, "then")
		if e != nil {
			return e
		}
		if e = s.statement(yes); e != nil {
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
	case "literal", "identifier", "binary", "unary", "call":
		if expressionOwnedByStructuredParent(s.g, id) {
			return nil
		}
		return s.expression(id)
	default:
		return fmt.Errorf("UNIMPLEMENTED_NATIVE_GAP node=%d kind=%s operation=%s", id, c.Kind, c.Operation.Operator)
	}
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
	case "literal":
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
	case "identifier":
		slot, ok := s.slots[s.binding(id)]
		if !ok {
			slot, ok = s.slots[c.Name]
		}
		if !ok {
			return fmt.Errorf("native unresolved binding %q node=%d", c.Name, id)
		}
		s.emit("mov", xr(xRAX), xm(xRBP, slot))
		return nil
	case "unary":
		v, e := s.child(id, "value", "operand")
		if e != nil {
			return e
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
		// Signed quotient/remainder share one x86-64 representation kernel. The
		// source operation remains parameterized in the canonical node; only the
		// proven integer machine form is selected here.
		if op == "/" || op == "%" || op == "%/%" || op == "%%" {
			s.emit("cqo", x64Operand{}, x64Operand{})
			s.emit("idiv", xr(xR10), x64Operand{})
			if op == "%" || op == "%%" { s.emit("mov", xr(xRAX), xr(xRDX)) }
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
	case "call":
		callee, e := s.child(id, "value", "callee")
		if e != nil {
			return e
		}
		name := s.g.common[callee].Name
		fn, ok := s.functions[name]
		if !ok {
			return fmt.Errorf("native call %q requires linked implementation", name)
		}
		args := s.g.many(id, "argument")
		if len(args) != len(s.g.many(fn, "parameter")) {
			return fmt.Errorf("native call %q arity mismatch", name)
		}
		temps := make([]int, len(args))
		for i, arg := range args {
			if e = s.expression(arg.ID); e != nil {
				return e
			}
			temps[i] = s.slot()
			s.emit("mov", xm(xRBP, temps[i]), xr(xRAX))
		}
		for i, slot := range temps {
			if i < 4 {
				if s.isFloat(args[i].ID) {
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
		if bytes := len(args) * 8; bytes > s.outgoing {
			s.outgoing = bytes
		}
		s.emit("call", xl(s.functionLabels[name]), x64Operand{})
		if s.functionFloat(fn) {
			s.emit("mov_from_xmm", xr(xRAX), xr(0))
		}
		return nil
	default:
		return fmt.Errorf("UNIMPLEMENTED_NATIVE_GAP expression node=%d kind=%s", id, c.Kind)
	}
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
		return fmt.Errorf("UNIMPLEMENTED_NATIVE_GAP integer string formatting")
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
		forms := map[string]string{"integer.add": "add", "integer.subtract": "sub", "integer.multiply": "imul", "integer.and": "and", "integer.or": "or", "integer.xor": "xor", "integer.and_not": "and", "integer.equal": "je", "integer.not_equal": "jne", "integer.less": "jl", "integer.less_equal": "jle", "integer.greater": "jg", "integer.greater_equal": "jge"}
		form, ok := forms[op.Name]
		if !ok {
			return fmt.Errorf("native integer operation %s unavailable", op.Name)
		}
		if op.Name == "integer.and_not" {
			s.emit("not", xr(xR10), x64Operand{})
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
func (s *x64Selector) boolean(branch string) {
	yes, end := s.label(), s.label()
	s.emit(branch, xl(yes), x64Operand{})
	s.emit("mov", xr(xRAX), xi(0))
	s.emit("jmp", xl(end), x64Operand{})
	s.mark(yes)
	s.emit("mov", xr(xRAX), xi(1))
	s.mark(end)
}
