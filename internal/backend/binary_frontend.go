package backend

// This file is the productive binary boundary for the bounded x86-64 subset
// emitted by machine_x64.go.  It deliberately works on bytes and structured
// operands; binary input is never routed through a source-language parser.

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

func CompileBinaryInput(data []byte, opts CompileOptions) (CompileResult, error) {
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
		opts.OutputKind = CompileAssembly
	}
	if opts.TargetArch != "x86_64" {
		return CompileResult{}, fmt.Errorf("binary frontend: unsupported architecture %q", opts.TargetArch)
	}
	var p x64Program
	var err error
	switch opts.InputKind {
	case CompileInputAssembly:
		p, err = parseX64Assembly(string(data))
	case CompileInputMachine:
		p, err = decodeX64(data, opts.BaseAddress)
	case CompileInputObject:
		var text []byte
		text, err = coffText(data)
		if err == nil {
			p, err = decodeX64(text, opts.BaseAddress)
		}
	case CompileInputExecutable:
		var text []byte
		text, opts.BaseAddress, err = peText(data)
		if err == nil {
			p, err = decodeX64(text, opts.BaseAddress)
		}
	default:
		return CompileResult{}, fmt.Errorf("binary frontend: unsupported input kind %q", opts.InputKind)
	}
	if err != nil {
		return CompileResult{}, err
	}
	result := CompileResult{OutputKind: opts.OutputKind, InstructionCount: len(p.Instructions)}
	if opts.OutputKind == CompileSource {
		sp, e := LiftBinaryInput(data, opts)
		if e != nil {
			return result, e
		}
		target := opts.TargetLanguage
		if target == "" {
			target = "c"
		}
		text, e := EmitSemantic(target, sp)
		if e != nil {
			return result, e
		}
		result.Text = text
		return result, nil
	}
	if opts.OutputKind == CompileAssembly {
		result.Text = renderX64(p)
		return result, nil
	}
	// Re-encoding is only accepted for instructions represented exactly by the
	// structured decoder. The original bytes remain the lossless payload for a
	// binary-to-binary conversion.
	var code []byte
	if opts.InputKind == CompileInputMachine || opts.InputKind == CompileInputObject || opts.InputKind == CompileInputExecutable {
		code = append([]byte(nil), data...)
		if opts.InputKind == CompileInputObject {
			code, _ = coffText(data)
		}
		if opts.InputKind == CompileInputExecutable {
			code, _, _ = peText(data)
		}
	} else {
		code, _, err = encodeX64(p)
		if err != nil {
			return result, err
		}
	}
	switch opts.OutputKind {
	case CompileMachineCode:
		result.Bytes = code
	case CompileObject:
		result.Bytes = coff64Object(code)
	case CompileExecutable:
		// Add a real entry/end label around the parsed sequence. This is valid
		// for the emitted subset and keeps the existing PE writer authoritative.
		if opts.InputKind != CompileInputAssembly {
			p, err = decodeX64(code, opts.BaseAddress)
		}
		if err != nil {
			return result, err
		}
		wrapped := p
		hasEntry := false
		for _, in := range wrapped.Instructions {
			if in.Op == "label" && in.A.Label == "native_entry" {
				hasEntry = true
				break
			}
		}
		if !hasEntry {
			wrapped.Instructions = append([]x64Instruction{{"label", xl("native_entry"), x64Operand{}}}, wrapped.Instructions...)
		}
		wrapped.Instructions = append(wrapped.Instructions, x64Instruction{"label", xl("native_end"), x64Operand{}})
		code, labels, err := encodeX64(wrapped)
		if err != nil {
			return result, err
		}
		result.Bytes, err = pe64Image(code, labels, []x64Function{{Label: "native_entry", End: "native_end", Frame: 16}})
	default:
		return result, fmt.Errorf("binary frontend: unsupported output kind %q", opts.OutputKind)
	}
	return result, err
}

// LiftBinaryInput exposes the same canonical SemanticProgram used by source
// inputs for a straight-line integer subset. The recovery is dataflow based:
// register definitions are represented as canonical expressions and a return
// is recovered only when every instruction on the path has a proven semantic
// transfer. No opcode spelling is promoted to a source-language construct.
func LiftBinaryInput(data []byte, opts CompileOptions) (*SemanticProgram, error) {
	var p x64Program
	var err error
	if opts.InputKind == CompileInputAssembly {
		p, err = parseX64Assembly(string(data))
	} else {
		var b []byte = data
		if opts.InputKind == CompileInputObject {
			b, err = coffText(data)
		}
		if opts.InputKind == CompileInputExecutable {
			b, opts.BaseAddress, err = peText(data)
		}
		if err == nil {
			p, err = decodeX64(b, opts.BaseAddress)
		}
	}
	if err != nil {
		return nil, err
	}
	return liftStraightLineX64(p)
}

// liftStraightLineX64 is one generic recovery rule for the bounded decoder
// vocabulary. It intentionally accepts only a single fully-known path: an
// unmodelled side effect, memory access or control edge rejects the lift
// rather than guessing source semantics.
func liftStraightLineX64(p x64Program) (*SemanticProgram, error) {
	var registers [16]Expr
	// The native selector represents local bindings as rbp-relative stack
	// slots. Keeping those values in this recovery environment is the same
	// dataflow rule as register recovery; it does not turn an address into a
	// source variable or guess aliasing beyond the proven frame-local form.
	stackSlots := map[int64]Expr{}
	// PUSH/POP are modeled as a balanced RSP stack transfer. The temporary ABI
	// save has no standalone source meaning, but its value dependency and stack
	// write/read must survive until the matching POP proves it is safe to omit.
	machineStack := []Expr{}
	operand := func(x x64Operand) Expr {
		switch x.Kind {
		case 'i':
			return &LiteralExpr{Kind: "integer", Text: strconv.FormatInt(x.Value, 10)}
		case 'r':
			return registers[x.Reg]
		case 'm':
			if x.Reg == xRBP {
				return stackSlots[x.Value]
			}
		default:
			return nil
		}
		return nil
	}
	set := func(x x64Operand, value Expr) bool {
		switch x.Kind {
		case 'r':
			if int(x.Reg) >= len(registers) {
				return false
			}
			registers[x.Reg] = value
			return true
		case 'm':
			if x.Reg != xRBP {
				return false
			}
			stackSlots[x.Value] = value
			return true
		}
		return false
	}
	bin := func(op string, left, right Expr) Expr {
		if left == nil || right == nil {
			return nil
		}
		return &BinaryExpr{Op: op, L: left, R: right}
	}
	for _, in := range p.Instructions {
		switch in.Op {
		case "label", "sub_sp", "add_sp", "cqo":
			// Labels and a balanced frame setup carry representation facts but do
			// not alter the recovered expression value on this proven path.
			continue
		case "push":
			machineStack = append(machineStack, operand(in.A))
		case "pop":
			if in.A.Kind != 'r' || len(machineStack) == 0 {
				return nil, fmt.Errorf("binary frontend: unbalanced stack pop")
			}
			value := machineStack[len(machineStack)-1]
			machineStack = machineStack[:len(machineStack)-1]
			if !set(in.A, value) {
				return nil, fmt.Errorf("binary frontend: unproven pop recovery")
			}
		case "mov":
			// Function frame establish/tear-down is representation-only. Handle
			// it before generic move transfer so rsp/rbp never becomes a source
			// value in the recovered UAST.
			if in.A.Kind == 'r' && in.B.Kind == 'r' && ((in.A.Reg == xRBP && in.B.Reg == xRSP) || (in.A.Reg == xRSP && in.B.Reg == xRBP)) {
				continue
			}
			if !set(in.A, operand(in.B)) {
				return nil, fmt.Errorf("binary frontend: unproven mov recovery")
			}
		case "xor":
			if in.A.Kind == 'r' && in.B.Kind == 'r' && in.A.Reg == in.B.Reg {
				set(in.A, &LiteralExpr{Kind: "integer", Text: "0"})
				continue
			}
			if !set(in.A, bin("^", operand(in.A), operand(in.B))) {
				return nil, fmt.Errorf("binary frontend: unproven xor recovery")
			}
		case "add", "sub", "imul", "and", "or":
			op := map[string]string{"add": "+", "sub": "-", "imul": "*", "and": "&", "or": "|"}[in.Op]
			if !set(in.A, bin(op, operand(in.A), operand(in.B))) {
				return nil, fmt.Errorf("binary frontend: unproven %s recovery", in.Op)
			}
		case "neg", "not":
			op := map[string]string{"neg": "-", "not": "!"}[in.Op]
			value := operand(in.A)
			if value == nil || !set(in.A, &UnaryExpr{Op: op, X: value}) {
				return nil, fmt.Errorf("binary frontend: unproven %s recovery", in.Op)
			}
		case "idiv", "div":
			if registers[xRAX] == nil || operand(in.A) == nil {
				return nil, fmt.Errorf("binary frontend: unproven %s recovery", in.Op)
			}
			registers[xRAX] = bin("/", registers[xRAX], operand(in.A))
		case "shl", "shr", "sar":
			if in.A.Kind != 'r' || registers[in.A.Reg] == nil || registers[xRCX] == nil {
				return nil, fmt.Errorf("binary frontend: unproven %s recovery", in.Op)
			}
			op := map[string]string{"shl": "<<", "shr": ">>", "sar": ">>"}[in.Op]
			registers[in.A.Reg] = bin(op, registers[in.A.Reg], registers[xRCX])
		case "ret":
			if len(machineStack) != 0 {
				return nil, fmt.Errorf("binary frontend: unbalanced stack at return")
			}
			if registers[xRAX] == nil {
				return nil, fmt.Errorf("binary frontend: return value recovery unavailable")
			}
			// The first returned path is the selected machine entry. Subsequent
			// labels/functions are not part of this linear recovery slice.
			return NewSemanticProgram(&BlockStmt{List: []Stmt{&ReturnStmt{X: registers[xRAX]}}}, "eager_left_to_right"), nil
		default:
			return nil, fmt.Errorf("binary frontend: semantic lifting unavailable for instruction %q", in.Op)
		}
	}
	return nil, fmt.Errorf("binary frontend: return recovery unavailable")
}

type asmTok struct{ text string }

func asmLex(s string) []asmTok {
	var out []asmTok
	for i := 0; i < len(s); {
		if s[i] == ' ' || s[i] == '\t' || s[i] == ',' {
			i++
			continue
		}
		if s[i] == '[' || s[i] == ']' || s[i] == '+' || s[i] == '-' || s[i] == ':' {
			out = append(out, asmTok{string(s[i])})
			i++
			continue
		}
		j := i
		for j < len(s) && !strings.ContainsRune(" \t,[]+-:", rune(s[j])) {
			j++
		}
		out = append(out, asmTok{s[i:j]})
		i = j
	}
	return out
}
func parseAsmOperand(ts []asmTok) (x64Operand, int, error) {
	if len(ts) == 0 {
		return x64Operand{}, 0, fmt.Errorf("assembly: missing operand")
	}
	t := strings.ToLower(ts[0].text)
	regs := map[string]byte{"rax": 0, "rcx": 1, "rdx": 2, "rbx": 3, "rsp": 4, "rbp": 5, "rsi": 6, "rdi": 7, "r8": 8, "r9": 9, "r10": 10, "r11": 11, "r12": 12, "r13": 13, "r14": 14, "r15": 15}
	if r, ok := regs[t]; ok {
		return xr(r), 1, nil
	}
	if strings.HasPrefix(t, "xmm") {
		if r, e := strconv.Atoi(strings.TrimPrefix(t, "xmm")); e == nil {
			return xr(byte(r)), 1, nil
		}
	}
	if t == "[" {
		// The renderer emits qword [dword base+/-disp].  Keep this parser
		// structural: qualifiers are ignored but the base and displacement are
		// represented as the same memory operand consumed by the encoder.
		i := 1
		for i < len(ts) && (strings.EqualFold(ts[i].text, "qword") || strings.EqualFold(ts[i].text, "dword") || strings.EqualFold(ts[i].text, "word") || strings.EqualFold(ts[i].text, "byte")) {
			i++
		}
		if i >= len(ts) {
			return x64Operand{}, 0, fmt.Errorf("assembly: unterminated memory operand")
		}
		baseName := strings.ToLower(ts[i].text)
		if baseName == "rel" {
			if i+1 >= len(ts) || ts[i+2].text != "]" {
				return x64Operand{}, 0, fmt.Errorf("assembly: malformed relative label")
			}
			return xl(ts[i+1].text), i + 3, nil
		}
		base, ok := regs[baseName]
		if !ok {
			return x64Operand{}, 0, fmt.Errorf("assembly: memory base %q is not a register", ts[i].text)
		}
		i++
		disp := int64(0)
		if i < len(ts) && (ts[i].text == "+" || ts[i].text == "-") {
			sign := int64(1)
			if ts[i].text == "-" {
				sign = -1
			}
			i++
			if i >= len(ts) {
				return x64Operand{}, 0, fmt.Errorf("assembly: missing memory displacement")
			}
			v, e := strconv.ParseInt(strings.TrimPrefix(ts[i].text, "#"), 0, 64)
			if e != nil {
				return x64Operand{}, 0, fmt.Errorf("assembly: invalid memory displacement %q", ts[i].text)
			}
			disp = sign * v
			i++
		}
		if i >= len(ts) || ts[i].text != "]" {
			return x64Operand{}, 0, fmt.Errorf("assembly: unterminated memory operand")
		}
		return xm(base, int(disp)), i + 1, nil
	}
	if t == "rel" {
		if len(ts) < 2 {
			return x64Operand{}, 0, fmt.Errorf("assembly: missing relative label")
		}
		return xl(ts[1].text), 2, nil
	}
	if t == "strict" || t == "near" || t == "short" || t == "qword" || t == "dword" || t == "word" || t == "byte" {
		x, n, e := parseAsmOperand(ts[1:])
		return x, n + 1, e
	}
	if n, e := strconv.ParseInt(strings.TrimPrefix(t, "#"), 0, 64); e == nil {
		return xi(n), 1, nil
	}
	return xl(ts[0].text), 1, nil
}
func parseX64Assembly(src string) (x64Program, error) {
	var p x64Program
	sc := bufio.NewScanner(bytes.NewReader([]byte(src)))
	for sc.Scan() {
		line := strings.TrimSpace(strings.SplitN(sc.Text(), ";", 2)[0])
		if line == "" {
			continue
		}
		ts := asmLex(line)
		if len(ts) == 0 {
			continue
		}
		if len(ts) > 1 && ts[1].text == ":" {
			p.Instructions = append(p.Instructions, x64Instruction{"label", xl(ts[0].text), x64Operand{}})
			ts = ts[2:]
			if len(ts) == 0 {
				continue
			}
		}
		op := strings.ToLower(ts[0].text)
		if op == "bits" || op == "default" || op == "section" || op == "global" {
			continue
		}
		if len(ts) == 1 {
			if op == "ret" || op == "cqo" || op == "ud2" {
				p.Instructions = append(p.Instructions, x64Instruction{op, x64Operand{}, x64Operand{}})
				continue
			}
			return p, fmt.Errorf("assembly: instruction %s needs operands", op)
		}
		a, n, e := parseAsmOperand(ts[1:])
		if e != nil {
			return p, e
		}
		b := x64Operand{}
		if n+1 < len(ts) {
			b, _, e = parseAsmOperand(ts[1+n:])
			if e != nil {
				return p, e
			}
		}
		if op == "movq" {
			op = "mov"
		}
		if op == "sub" && a.Kind == 'r' && a.Reg == xRSP {
			op = "sub_sp"
		}
		if op == "add" && a.Kind == 'r' && a.Reg == xRSP {
			op = "add_sp"
		}
		p.Instructions = append(p.Instructions, x64Instruction{op, a, b})
	}
	return p, sc.Err()
}

func coffText(b []byte) ([]byte, error) {
	if len(b) < 20 || binary.LittleEndian.Uint16(b) != 0x8664 {
		return nil, fmt.Errorf("COFF: unsupported header")
	}
	n := int(binary.LittleEndian.Uint16(b[2:]))
	off := int(binary.LittleEndian.Uint32(b[20-12:]))
	if off <= 0 || off > len(b) {
		off = 60
	}
	for i := 0; i < n; i++ {
		h := 20 + i*40
		if h+40 > len(b) {
			break
		}
		size := int(binary.LittleEndian.Uint32(b[h+16:]))
		ptr := int(binary.LittleEndian.Uint32(b[h+20:]))
		name := strings.TrimRight(string(b[h:h+8]), "\x00")
		if name == ".text" && ptr+size <= len(b) {
			return append([]byte(nil), b[ptr:ptr+size]...), nil
		}
	}
	return nil, fmt.Errorf("COFF: executable .text section missing")
}
func peText(b []byte) ([]byte, uint64, error) {
	if len(b) < 0x100 || string(b[:2]) != "MZ" {
		return nil, 0, fmt.Errorf("PE: DOS header missing")
	}
	pe := int(binary.LittleEndian.Uint32(b[0x3c:]))
	if pe+24 > len(b) || string(b[pe:pe+4]) != "PE\x00\x00" {
		return nil, 0, fmt.Errorf("PE: signature missing")
	}
	n := int(binary.LittleEndian.Uint16(b[pe+6:]))
	opt := pe + 24
	magic := binary.LittleEndian.Uint16(b[opt:])
	if magic != 0x20b {
		return nil, 0, fmt.Errorf("PE: only PE32+ supported")
	}
	image := binary.LittleEndian.Uint64(b[opt+24:])
	sec := opt + int(binary.LittleEndian.Uint16(b[pe+20:]))
	for i := 0; i < n; i++ {
		h := sec + i*40
		if h+40 > len(b) {
			break
		}
		name := strings.TrimRight(string(b[h:h+8]), "\x00")
		virtual := int(binary.LittleEndian.Uint32(b[h+8:]))
		raw := int(binary.LittleEndian.Uint32(b[h+16:]))
		size := virtual
		if size == 0 {
			size = raw
		}
		ptr := int(binary.LittleEndian.Uint32(b[h+20:]))
		if name == ".text" && ptr+size <= len(b) {
			return append([]byte(nil), b[ptr:ptr+size]...), image, nil
		}
	}
	return nil, 0, fmt.Errorf("PE: executable .text section missing")
}

func decodeX64(b []byte, base uint64) (x64Program, error) {
	// The encoder emits a deliberately bounded x86-64 subset. Decode the same
	// forms here, including its ModRM/SIB memory representation. Relative
	// targets are reconstructed as labels so downstream assembly and CFG code
	// consumes one structured instruction sequence rather than byte patterns.
	type decoded struct {
		at int
		in x64Instruction
	}
	var rows []decoded
	labels := map[int]string{}
	label := func(at int) string {
		if s := labels[at]; s != "" {
			return s
		}
		s := fmt.Sprintf("L_%x", uint64(at)+base)
		labels[at] = s
		return s
	}
	need := func(i, n int) error {
		if i+n > len(b) {
			return fmt.Errorf("x64: truncated instruction at %d", i)
		}
		return nil
	}
	for i := 0; i < len(b); {
		at := i
		prefix := byte(0)
		if b[i] == 0x66 || b[i] == 0xf2 {
			prefix = b[i]
			i++
			if err := need(i, 1); err != nil {
				return x64Program{}, err
			}
		}
		rex := byte(0)
		if b[i] >= 0x40 && b[i] <= 0x4f {
			rex = b[i]
			i++
			if err := need(i, 1); err != nil {
				return x64Program{}, err
			}
		}
		op := b[i]
		i++
		reg := func(v byte) byte { return (v & 7) | ((rex & 4) << 1) }
		rmReg := func(v byte) byte { return (v & 7) | ((rex & 1) << 3) }
		readRM := func() (byte, x64Operand, error) {
			if err := need(i, 1); err != nil {
				return 0, x64Operand{}, err
			}
			m := b[i]
			i++
			r := reg(m >> 3)
			mod, low := m>>6, m&7
			if mod == 3 {
				return r, xr(rmReg(low)), nil
			}
			if mod != 2 {
				return 0, x64Operand{}, fmt.Errorf("x64: unsupported ModRM mode %d at %d", mod, at)
			}
			baseReg := rmReg(low)
			if low == 4 {
				if err := need(i, 1); err != nil {
					return 0, x64Operand{}, err
				}
				sib := b[i]
				i++
				if sib != 0x24 {
					return 0, x64Operand{}, fmt.Errorf("x64: unsupported SIB 0x%02x at %d", sib, at)
				}
				baseReg = rmReg(sib)
			}
			if err := need(i, 4); err != nil {
				return 0, x64Operand{}, err
			}
			disp := int64(int32(binary.LittleEndian.Uint32(b[i:])))
			i += 4
			return r, xm(baseReg, int(disp)), nil
		}
		add := func(op string, a, b x64Operand) { rows = append(rows, decoded{at, x64Instruction{op, a, b}}) }
		switch {
		case op >= 0xb8 && op <= 0xbf && rex&8 != 0:
			if err := need(i, 8); err != nil {
				return x64Program{}, err
			}
			v := int64(binary.LittleEndian.Uint64(b[i:]))
			i += 8
			add("mov", xr(rmReg(op-0xb8)), xi(v))
		case op == 0xc3:
			add("ret", x64Operand{}, x64Operand{})
		case op == 0x99 && rex&8 != 0:
			add("cqo", x64Operand{}, x64Operand{})
		case op == 0x0f:
			if err := need(i, 1); err != nil {
				return x64Program{}, err
			}
			ext := b[i]
			i++
			if ext >= 0x80 && ext <= 0x8f {
				if err := need(i, 4); err != nil {
					return x64Program{}, err
				}
				d := int(int32(binary.LittleEndian.Uint32(b[i:])))
				i += 4
				names := map[byte]string{0x84: "je", 0x85: "jne", 0x8c: "jl", 0x8e: "jle", 0x8f: "jg", 0x8d: "jge", 0x82: "jb", 0x86: "jbe", 0x87: "ja", 0x83: "jae", 0x8a: "jp"}
				n := names[ext]
				if n == "" {
					return x64Program{}, fmt.Errorf("x64: unsupported condition 0x%02x", ext)
				}
				add(n, xl(label(i+d)), x64Operand{})
				break
			}
			if ext == 0x0b {
				add("ud2", x64Operand{}, x64Operand{})
				break
			}
			r, x, e := readRM()
			if e != nil {
				return x64Program{}, e
			}
			switch ext {
			case 0xaf:
				add("imul", xr(r), x)
			case 0x6e:
				if prefix != 0x66 {
					return x64Program{}, fmt.Errorf("x64: mov_to_xmm missing 66 prefix")
				}
				add("mov_to_xmm", xr(r), x)
			case 0x7e:
				if prefix != 0x66 {
					return x64Program{}, fmt.Errorf("x64: mov_from_xmm missing 66 prefix")
				}
				add("mov_from_xmm", x, xr(r))
			case 0x2a:
				if prefix != 0xf2 {
					return x64Program{}, fmt.Errorf("x64: cvtsi2sd missing f2 prefix")
				}
				add("cvtsi2sd", xr(r), x)
			case 0x58, 0x5c, 0x59, 0x5e, 0x2e:
				names := map[byte]string{0x58: "addsd", 0x5c: "subsd", 0x59: "mulsd", 0x5e: "divsd", 0x2e: "ucomisd"}
				add(names[ext], xr(r), x)
			default:
				return x64Program{}, fmt.Errorf("x64: unsupported 0f opcode 0x%02x at %d", ext, at)
			}
		case op == 0x89 || op == 0x8b || op == 0x03 || op == 0x2b || op == 0x33 || op == 0x23 || op == 0x0b || op == 0x3b || op == 0x85 || op == 0x8d || op == 0x01 || op == 0x29 || op == 0x31 || op == 0x21 || op == 0x09 || op == 0x39:
			r, x, e := readRM()
			if e != nil {
				return x64Program{}, e
			}
			names := map[byte]string{0x89: "mov", 0x8b: "mov", 0x03: "add", 0x2b: "sub", 0x33: "xor", 0x23: "and", 0x0b: "or", 0x3b: "cmp", 0x85: "test", 0x8d: "lea", 0x01: "add", 0x29: "sub", 0x31: "xor", 0x21: "and", 0x09: "or", 0x39: "cmp"}
			name := names[op]
			a, bv := xr(r), x
			if op == 0x89 || op == 0x01 || op == 0x29 || op == 0x31 || op == 0x21 || op == 0x09 || op == 0x39 {
				a, bv = x, xr(r)
			}
			add(name, a, bv)
		case op == 0xf7:
			r, x, e := readRM()
			if e != nil {
				return x64Program{}, e
			}
			names := map[byte]string{7: "idiv", 6: "div", 2: "not", 3: "neg"}
			n := names[r&7]
			if n == "" {
				return x64Program{}, fmt.Errorf("x64: unsupported f7 group %d", r&7)
			}
			add(n, x, x64Operand{})
		case op == 0xd3:
			r, x, e := readRM()
			if e != nil {
				return x64Program{}, e
			}
			names := map[byte]string{4: "shl", 5: "shr", 7: "sar"}
			n := names[r&7]
			if n == "" {
				return x64Program{}, fmt.Errorf("x64: unsupported d3 group %d", r&7)
			}
			add(n, x, x64Operand{})
		case op == 0x81:
			if err := need(i, 5); err != nil {
				return x64Program{}, err
			}
			m := b[i]
			i++
			if m != 0xec && m != 0xc4 {
				return x64Program{}, fmt.Errorf("x64: unsupported 81 ModRM 0x%02x", m)
			}
			v := int64(int32(binary.LittleEndian.Uint32(b[i:])))
			i += 4
			if m == 0xec {
				add("sub_sp", xi(v), x64Operand{})
			} else {
				add("add_sp", xi(v), x64Operand{})
			}
		case op >= 0x50 && op <= 0x57:
			add("push", xr(rmReg(op-0x50)), x64Operand{})
		case op >= 0x58 && op <= 0x5f:
			add("pop", xr(rmReg(op-0x58)), x64Operand{})
		case op == 0xe8 || op == 0xe9:
			if err := need(i, 4); err != nil {
				return x64Program{}, err
			}
			d := int(int32(binary.LittleEndian.Uint32(b[i:])))
			i += 4
			n := "jmp"
			if op == 0xe8 {
				n = "call"
			}
			add(n, xl(label(i+d)), x64Operand{})
		default:
			return x64Program{}, fmt.Errorf("x64: unsupported opcode 0x%02x at %d", op, at)
		}
	}
	var p x64Program
	for _, row := range rows {
		if l := labels[row.at]; l != "" {
			p.Instructions = append(p.Instructions, x64Instruction{"label", xl(l), x64Operand{}})
		}
		p.Instructions = append(p.Instructions, row.in)
	}
	return p, nil
}
