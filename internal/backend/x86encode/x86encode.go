// Copyright (c) 2026 Tarek Wasfy
package x86encode

import (
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

type Operand struct {
	Kind  string
	Reg   string
	Bits  int
	Value int64
	Base  string
	Index string
	Scale int
	Disp  int64
}

type Instruction struct {
	Mnemonic    string
	Operands    []Operand
	BranchWidth int // 8 for short, 32 for near/default
}

var registers = map[string]struct {
	code byte
	bits int
}{
	"al": {0, 8}, "cl": {1, 8}, "dl": {2, 8}, "bl": {3, 8}, "ah": {4, 8}, "ch": {5, 8}, "dh": {6, 8}, "bh": {7, 8},
	"spl": {4, 8}, "bpl": {5, 8}, "sil": {6, 8}, "dil": {7, 8},
	"r8b": {8, 8}, "r9b": {9, 8}, "r10b": {10, 8}, "r11b": {11, 8}, "r12b": {12, 8}, "r13b": {13, 8}, "r14b": {14, 8}, "r15b": {15, 8},
	"ax": {0, 16}, "cx": {1, 16}, "dx": {2, 16}, "bx": {3, 16}, "sp": {4, 16}, "bp": {5, 16}, "si": {6, 16}, "di": {7, 16},
	"r8w": {8, 16}, "r9w": {9, 16}, "r10w": {10, 16}, "r11w": {11, 16}, "r12w": {12, 16}, "r13w": {13, 16}, "r14w": {14, 16}, "r15w": {15, 16},
	"eax": {0, 32}, "ecx": {1, 32}, "edx": {2, 32}, "ebx": {3, 32},
	"esp": {4, 32}, "ebp": {5, 32}, "esi": {6, 32}, "edi": {7, 32},
	"rax": {0, 64}, "rcx": {1, 64}, "rdx": {2, 64}, "rbx": {3, 64},
	"rsp": {4, 64}, "rbp": {5, 64}, "rsi": {6, 64}, "rdi": {7, 64},
	"r8": {8, 64}, "r9": {9, 64}, "r10": {10, 64}, "r11": {11, 64},
	"r12": {12, 64}, "r13": {13, 64}, "r14": {14, 64}, "r15": {15, 64},
	"r8d": {8, 32}, "r9d": {9, 32}, "r10d": {10, 32}, "r11d": {11, 32},
}

var highByteRegisters = map[string]bool{"ah": true, "bh": true, "ch": true, "dh": true}
var forcedREXRegisters = map[string]bool{"spl": true, "bpl": true, "sil": true, "dil": true}

func ParseAssembly(source string) ([]Instruction, error) {
	var out []Instruction
	for _, line := range strings.Split(source, "\n") {
		line = strings.TrimSpace(strings.SplitN(line, ";", 2)[0])
		if line == "" || strings.HasPrefix(line, "bits ") || strings.HasPrefix(line, "section ") {
			continue
		}
		if strings.HasSuffix(line, ":") {
			out = append(out, Instruction{Mnemonic: "label", Operands: []Operand{{Kind: "label", Reg: strings.TrimSuffix(line, ":")}}})
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		ins := Instruction{Mnemonic: strings.ToLower(parts[0])}
		if len(parts) > 1 {
			for _, raw := range strings.Split(strings.Join(parts[1:], " "), ",") {
				operandText := strings.TrimSpace(raw)
				if isBranch(ins.Mnemonic) {
					if strings.HasPrefix(operandText, "short ") {
						ins.BranchWidth = 8
					} else if strings.HasPrefix(operandText, "near ") {
						ins.BranchWidth = 32
					}
					operandText = strings.TrimPrefix(strings.TrimPrefix(operandText, "near"), "short")
					operandText = strings.TrimSpace(operandText)
				}
				var op Operand
				var err error
				if isBranch(ins.Mnemonic) && !isMemoryText(operandText) && !isRegisterText(operandText) {
					op = Operand{Kind: "label", Reg: operandText}
				} else {
					op, err = parseOperand(operandText)
				}
				if err != nil {
					return nil, err
				}
				ins.Operands = append(ins.Operands, op)
			}
		}
		out = append(out, ins)
	}
	return out, nil
}

func isMemoryText(s string) bool {
	s = strings.ToLower(strings.TrimSpace(s))
	for _, q := range []string{"byte ", "word ", "dword ", "qword "} {
		s = strings.TrimPrefix(s, q)
	}
	return strings.HasPrefix(strings.TrimSpace(s), "[")
}
func isRegisterText(s string) bool {
	_, ok := registers[strings.ToLower(strings.TrimSpace(s))]
	return ok
}

func parseOperand(s string) (Operand, error) {
	s = strings.TrimSpace(s)
	bits := 0
	for _, qualifier := range []string{"byte ", "word ", "dword ", "qword ", "byte[", "word[", "dword[", "qword["} {
		if strings.HasPrefix(strings.ToLower(s), qualifier) {
			bits = map[string]int{"byte ": 8, "word ": 16, "dword ": 32, "qword ": 64, "byte[": 8, "word[": 16, "dword[": 32, "qword[": 64}[qualifier]
			if strings.HasSuffix(qualifier, "[") {
				s = s[len(qualifier)-1:]
			} else {
				s = strings.TrimSpace(s[len(qualifier):])
			}
			break
		}
	}
	if _, ok := registers[strings.ToLower(s)]; ok {
		return Operand{Kind: "reg", Reg: strings.ToLower(s)}, nil
	}
	var v int64
	var err error
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		v, err = strconv.ParseInt(strings.TrimPrefix(strings.ToLower(s), "0x"), 16, 64)
		if err == nil {
			return Operand{Kind: "imm", Value: v}, nil
		}
	} else if v, err = strconv.ParseInt(s, 10, 64); err == nil {
		return Operand{Kind: "imm", Value: v}, nil
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		body := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s[1:len(s)-1])), "-", "+-")
		var mem Operand
		mem.Kind, mem.Scale, mem.Bits = "mem", 1, bits
		for _, term := range strings.Split(body, "+") {
			term = strings.TrimSpace(term)
			if term == "" {
				continue
			}
			if r, ok := registers[term]; ok && r.bits == 64 {
				if mem.Base == "" {
					mem.Base = term
				} else if mem.Index == "" {
					mem.Index = term
				} else {
					return Operand{}, fmt.Errorf("x86encode: too many memory registers")
				}
				continue
			}
			if strings.Contains(term, "*") {
				parts := strings.SplitN(term, "*", 2)
				if len(parts) != 2 {
					return Operand{}, fmt.Errorf("x86encode: invalid SIB term %q", term)
				}
				if _, ok := registers[parts[0]]; !ok || mem.Index != "" {
					return Operand{}, fmt.Errorf("x86encode: invalid SIB index %q", term)
				}
				scale, e := strconv.Atoi(parts[1])
				if e != nil || (scale != 1 && scale != 2 && scale != 4 && scale != 8) {
					return Operand{}, fmt.Errorf("x86encode: invalid SIB scale %q", term)
				}
				mem.Index, mem.Scale = parts[0], scale
				continue
			}
			var n int64
			var e error
			if strings.HasPrefix(term, "0x") {
				n, e = strconv.ParseInt(strings.TrimPrefix(term, "0x"), 16, 64)
			} else {
				n, e = strconv.ParseInt(term, 10, 64)
			}
			if e != nil {
				return Operand{}, fmt.Errorf("x86encode: invalid displacement %q", term)
			}
			mem.Disp += n
		}
		if mem.Base == "" && mem.Index == "" {
			return Operand{}, fmt.Errorf("x86encode: memory operand has no address")
		}
		return mem, nil
	}
	return Operand{}, fmt.Errorf("x86encode: unsupported operand %q", s)
}

func EncodeProgram(instructions []Instruction) ([]byte, error) {
	labels := make(map[string]int)
	pc := 0
	for _, ins := range instructions {
		if ins.Mnemonic == "label" {
			labels[strings.ToLower(ins.Operands[0].Reg)] = pc
			continue
		}
		n, err := instructionSize(ins)
		if err != nil {
			return nil, err
		}
		pc += n
	}
	var out []byte
	for _, ins := range instructions {
		if ins.Mnemonic == "label" {
			continue
		}
		if isBranch(ins.Mnemonic) && len(ins.Operands) == 1 && ins.Operands[0].Kind == "label" {
			target, ok := labels[strings.ToLower(ins.Operands[0].Reg)]
			if !ok {
				return nil, fmt.Errorf("x86encode: unknown label %q", ins.Operands[0].Reg)
			}
			size, _ := instructionSize(ins)
			rel := int64(target - (len(out) + size))
			if ins.BranchWidth == 8 && (rel < -128 || rel > 127) {
				return nil, fmt.Errorf("x86encode: short branch displacement out of range")
			}
			out = append(out, branchBytes(ins.Mnemonic, int32(rel), ins.BranchWidth)...)
			continue
		}
		b, err := EncodeInstruction(ins)
		if err != nil {
			return nil, err
		}
		out = append(out, b...)
	}
	return out, nil
}

func isBranch(op string) bool {
	op = strings.ToLower(op)
	if op == "jmp" || op == "call" {
		return true
	}
	_, ok := conditionCode(op)
	return ok
}
func instructionSize(ins Instruction) (int, error) {
	if isBranch(ins.Mnemonic) && len(ins.Operands) == 1 {
		if ins.BranchWidth == 8 {
			if ins.Mnemonic == "call" {
				return 0, fmt.Errorf("x86encode: CALL has no rel8 form")
			}
			return 2, nil
		}
		if ins.Mnemonic == "jmp" || ins.Mnemonic == "call" {
			return 5, nil
		}
		return 6, nil
	}
	b, e := EncodeInstruction(ins)
	return len(b), e
}
func branchBytes(op string, rel int32, width int) []byte {
	if width == 8 {
		if strings.ToLower(op) == "jmp" {
			return []byte{0xeb, byte(int8(rel))}
		}
		cc, _ := conditionCode(op)
		return []byte{0x70 + cc, byte(int8(rel))}
	}
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(rel))
	switch strings.ToLower(op) {
	case "jmp":
		return append([]byte{0xe9}, b...)
	case "call":
		return append([]byte{0xe8}, b...)
	default:
		cc, ok := conditionCode(op)
		if !ok {
			return nil
		}
		return append([]byte{0x0f, 0x80 + cc}, b...)
	}
}

func conditionCode(op string) (byte, bool) {
	cc, ok := map[string]byte{"jo": 0, "jno": 1, "jb": 2, "jc": 2, "jnae": 2, "jae": 3, "jnb": 3, "jnc": 3, "je": 4, "jz": 4, "jne": 5, "jnz": 5, "jbe": 6, "jna": 6, "ja": 7, "jnbe": 7, "js": 8, "jns": 9, "jp": 10, "jpe": 10, "jnp": 11, "jpo": 11, "jl": 12, "jnge": 12, "jge": 13, "jnl": 13, "jle": 14, "jng": 14, "jg": 15, "jnle": 15}[strings.ToLower(op)]
	return cc, ok
}

func EncodeInstruction(ins Instruction) ([]byte, error) {
	op := strings.ToLower(ins.Mnemonic)
	if op == "ret" && len(ins.Operands) == 0 {
		return []byte{0xc3}, nil
	}
	if len(ins.Operands) == 1 && ins.Operands[0].Kind == "reg" {
		if b, ok, err := encodeIndirectBranch(op, ins.Operands[0]); ok || err != nil {
			return b, err
		}
		if b, ok, err := encodeStackRegister(op, ins.Operands[0]); ok || err != nil {
			return b, err
		}
		if b, ok, err := encodeUnaryGroup(op, ins.Operands[0]); ok || err != nil {
			return b, err
		}
	}
	if len(ins.Operands) == 1 && ins.Operands[0].Kind == "mem" {
		if b, ok, err := encodeIndirectBranch(op, ins.Operands[0]); ok || err != nil {
			return b, err
		}
		if b, ok, err := encodeStackMemory(op, ins.Operands[0]); ok || err != nil {
			return b, err
		}
		if b, ok, err := encodeUnaryMemory(op, ins.Operands[0]); ok || err != nil {
			return b, err
		}
	}
	if len(ins.Operands) == 1 && ins.Operands[0].Kind == "imm" {
		if op == "push" {
			return encodePushImmediate(ins.Operands[0].Value)
		}
		if op == "ret" {
			return encodeRetImmediate(ins.Operands[0].Value)
		}
	}
	if len(ins.Operands) == 2 && ins.Operands[0].Kind == "reg" && ins.Operands[1].Kind == "imm" {
		if b, ok, err := encodeTestImmediate(op, ins.Operands[0], ins.Operands[1].Value); ok || err != nil {
			return b, err
		}
		if b, ok, err := encodeShiftImmediate(op, ins.Operands[0], ins.Operands[1].Value); ok || err != nil {
			return b, err
		}
	}
	if len(ins.Operands) == 2 && ins.Operands[0].Kind == "mem" && ins.Operands[1].Kind == "imm" {
		if b, ok, err := encodeShiftMemoryImmediate(op, ins.Operands[0], ins.Operands[1].Value); ok || err != nil {
			return b, err
		}
	}
	if len(ins.Operands) == 2 && ins.Operands[0].Kind == "reg" && ins.Operands[1].Kind == "mem" && op == "lea" {
		return encodeLEA(ins.Operands[0], ins.Operands[1])
	}
	if len(ins.Operands) == 2 && ((ins.Operands[0].Kind == "reg" && ins.Operands[1].Kind == "mem") || (ins.Operands[0].Kind == "mem" && ins.Operands[1].Kind == "reg")) {
		return encodeRegMem(op, ins.Operands[0], ins.Operands[1])
	}
	if len(ins.Operands) == 2 && ins.Operands[0].Kind == "reg" && ins.Operands[1].Kind == "reg" {
		dst, dok := registers[ins.Operands[0].Reg]
		src, sok := registers[ins.Operands[1].Reg]
		if !dok || !sok || dst.bits != src.bits {
			return nil, fmt.Errorf("x86encode: register width mismatch")
		}
		code, ok := registerOpcodeWidth(op, false, dst.bits)
		if !ok {
			return nil, fmt.Errorf("x86encode: unsupported register operation %q", op)
		}
		var rex byte = 0x40
		if dst.bits == 64 {
			rex |= 0x08
		}
		if src.code >= 8 {
			rex |= 0x04
		}
		if dst.code >= 8 {
			rex |= 0x01
		}
		forcedREX := forcedREXRegisters[ins.Operands[0].Reg] || forcedREXRegisters[ins.Operands[1].Reg]
		if forcedREX {
			rex |= 0x40
		}
		if (highByteRegisters[ins.Operands[0].Reg] || highByteRegisters[ins.Operands[1].Reg]) && rex != 0x40 {
			return nil, fmt.Errorf("x86encode: high-byte register cannot be encoded with REX")
		}
		out := []byte{}
		if dst.bits == 16 {
			out = append(out, 0x66)
		}
		if rex != 0x40 || forcedREX {
			out = append(out, rex)
		}
		out = append(out, code, 0xc0|(src.code&7)<<3|(dst.code&7))
		return out, nil
	}
	if len(ins.Operands) == 2 && ins.Operands[0].Kind == "reg" && ins.Operands[1].Kind == "imm" {
		if b, ok, err := encodeGroup1Immediate(op, ins.Operands[0], ins.Operands[1].Value); ok || err != nil {
			return b, err
		}
	}
	if len(ins.Operands) == 2 && ins.Operands[0].Kind == "mem" && ins.Operands[1].Kind == "imm" {
		if b, ok, err := encodeMemoryImmediate(op, ins.Operands[0], ins.Operands[1].Value); ok || err != nil {
			return b, err
		}
	}
	return nil, fmt.Errorf("x86encode: unsupported instruction %q", ins.Mnemonic)
}

func encodeIndirectBranch(op string, operand Operand) ([]byte, bool, error) {
	group := byte(0)
	switch op {
	case "call":
		group = 2
	case "jmp":
		group = 4
	default:
		return nil, false, nil
	}
	bits := operand.Bits
	if operand.Kind == "reg" {
		if r, ok := registers[operand.Reg]; ok {
			bits = r.bits
		}
	}
	if bits == 0 {
		bits = 64
	}
	if bits != 64 && bits != 16 {
		return nil, true, fmt.Errorf("x86encode: indirect %s width %d unsupported", op, bits)
	}
	mod, rm, sib, disp, rexB, rexX := byte(3), byte(0), []byte(nil), []byte(nil), false, false
	if operand.Kind == "reg" {
		r := registers[operand.Reg]
		rm = r.code & 7
		rexB = r.code >= 8
	} else {
		mod, rm, sib, disp, rexB, rexX = memoryEncoding(operand)
	}
	out := []byte{}
	if bits == 16 {
		out = append(out, 0x66)
	}
	rex := byte(0x40)
	if rexB {
		rex |= 1
	}
	if rexX {
		rex |= 2
	}
	if rex != 0x40 {
		out = append(out, rex)
	}
	out = append(out, 0xff, modrm(mod, group, rm))
	if sib != nil {
		out = append(out, sib...)
	}
	out = append(out, disp...)
	return out, true, nil
}

func encodePushImmediate(v int64) ([]byte, error) {
	if v >= -128 && v <= 127 {
		return []byte{0x6a, byte(v)}, nil
	}
	if v < -2147483648 || v > 2147483647 {
		return nil, fmt.Errorf("x86encode: PUSH immediate must fit signed imm32")
	}
	return append([]byte{0x68}, EncodeImmediate32(v)...), nil
}
func encodeRetImmediate(v int64) ([]byte, error) {
	if v < 0 || v > 65535 {
		return nil, fmt.Errorf("x86encode: RET adjustment must fit imm16")
	}
	return []byte{0xc2, byte(v), byte(v >> 8)}, nil
}

func encodeTestImmediate(op string, dst Operand, imm int64) ([]byte, bool, error) {
	if op != "test" {
		return nil, false, nil
	}
	r, ok := registers[dst.Reg]
	if !ok {
		return nil, true, fmt.Errorf("x86encode: unknown TEST register")
	}
	bits := r.bits
	if bits != 8 && bits != 16 && bits != 32 && bits != 64 {
		return nil, true, fmt.Errorf("x86encode: TEST width unsupported")
	}
	if bits == 64 && (imm < -2147483648 || imm > 2147483647) {
		return nil, true, fmt.Errorf("x86encode: TEST qword immediate must fit signed imm32")
	}
	out := []byte{}
	if bits == 16 {
		out = append(out, 0x66)
	}
	rex := byte(0x40)
	if bits == 64 {
		rex |= 8
	}
	if r.code >= 8 {
		rex |= 1
	}
	forced := forcedREXRegisters[dst.Reg]
	if forced {
		rex |= 0x40
	}
	if highByteRegisters[dst.Reg] && rex != 0x40 {
		return nil, true, fmt.Errorf("x86encode: high-byte register cannot be encoded with REX")
	}
	if rex != 0x40 || forced {
		out = append(out, rex)
	}
	if r.code == 0 {
		code := byte(0xa9)
		if bits == 8 {
			code = 0xa8
		}
		out = append(out, code)
		if bits == 8 {
			out = append(out, byte(imm))
		} else if bits == 16 {
			var b [2]byte
			binary.LittleEndian.PutUint16(b[:], uint16(imm))
			out = append(out, b[:]...)
		} else {
			out = append(out, EncodeImmediate32(imm)...)
		}
		return out, true, nil
	}
	opcode := byte(0xf7)
	if bits == 8 {
		opcode = 0xf6
	}
	out = append(out, opcode, 0xc0|(r.code&7))
	if bits == 8 {
		out = append(out, byte(imm))
	} else if bits == 16 {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], uint16(imm))
		out = append(out, b[:]...)
	} else {
		out = append(out, EncodeImmediate32(imm)...)
	}
	return out, true, nil
}

func encodeUnaryMemory(op string, mem Operand) ([]byte, bool, error) {
	group, ok := map[string]byte{"not": 2, "mul": 4, "imul": 5, "div": 6, "idiv": 7}[op]
	if !ok {
		return nil, false, nil
	}
	if mem.Bits != 8 && mem.Bits != 16 && mem.Bits != 32 && mem.Bits != 64 {
		return nil, true, fmt.Errorf("x86encode: unary memory operand requires byte/word/dword/qword width")
	}
	mod, rm, sib, disp, rexB, rexX := memoryEncoding(mem)
	out := []byte{}
	if mem.Bits == 16 {
		out = append(out, 0x66)
	}
	rex := byte(0x40)
	if mem.Bits == 64 {
		rex |= 8
	}
	if rexB {
		rex |= 1
	}
	if rexX {
		rex |= 2
	}
	if rex != 0x40 {
		out = append(out, rex)
	}
	opcode := byte(0xf7)
	if mem.Bits == 8 {
		opcode = 0xf6
	}
	out = append(out, opcode, modrm(mod, group, rm))
	if sib != nil {
		out = append(out, sib...)
	}
	out = append(out, disp...)
	return out, true, nil
}

func encodeShiftMemoryImmediate(op string, mem Operand, count int64) ([]byte, bool, error) {
	group, ok := map[string]byte{"shl": 4, "sal": 4, "shr": 5, "sar": 7}[op]
	if !ok {
		return nil, false, nil
	}
	if mem.Bits != 8 && mem.Bits != 16 && mem.Bits != 32 && mem.Bits != 64 {
		return nil, true, fmt.Errorf("x86encode: shift memory operand requires explicit width")
	}
	if count < 0 || count > 255 {
		return nil, true, fmt.Errorf("x86encode: shift count out of imm8 range")
	}
	mod, rm, sib, disp, rexB, rexX := memoryEncoding(mem)
	out := []byte{}
	if mem.Bits == 16 {
		out = append(out, 0x66)
	}
	rex := byte(0x40)
	if mem.Bits == 64 {
		rex |= 8
	}
	if rexB {
		rex |= 1
	}
	if rexX {
		rex |= 2
	}
	if rex != 0x40 {
		out = append(out, rex)
	}
	opcode := byte(0xc1)
	if mem.Bits == 8 {
		opcode = 0xc0
	}
	if count == 1 {
		opcode = 0xd1
		if mem.Bits == 8 {
			opcode = 0xd0
		}
	}
	out = append(out, opcode, modrm(mod, group, rm))
	if sib != nil {
		out = append(out, sib...)
	}
	out = append(out, disp...)
	if count != 1 {
		out = append(out, byte(count))
	}
	return out, true, nil
}

func encodeStackMemory(op string, mem Operand) ([]byte, bool, error) {
	group, opcode := byte(0), byte(0)
	switch op {
	case "push":
		group, opcode = 6, 0xff
	case "pop":
		group, opcode = 0, 0x8f
	default:
		return nil, false, nil
	}
	if mem.Bits != 16 && mem.Bits != 64 {
		return nil, true, fmt.Errorf("x86encode: memory %s requires word or qword width", op)
	}
	mod, rm, sib, disp, rexB, rexX := memoryEncoding(mem)
	out := []byte{}
	if mem.Bits == 16 {
		out = append(out, 0x66)
	}
	rex := byte(0x40)
	if rexB {
		rex |= 1
	}
	if rexX {
		rex |= 2
	}
	if rex != 0x40 {
		out = append(out, rex)
	}
	out = append(out, opcode, modrm(mod, group, rm))
	if sib != nil {
		out = append(out, sib...)
	}
	out = append(out, disp...)
	return out, true, nil
}

func encodeStackRegister(op string, operand Operand) ([]byte, bool, error) {
	r, ok := registers[operand.Reg]
	if !ok {
		return nil, false, nil
	}
	var opcode byte
	switch op {
	case "push":
		opcode = 0x50
	case "pop":
		opcode = 0x58
	default:
		return nil, false, nil
	}
	if r.bits != 64 && r.bits != 16 {
		return nil, true, fmt.Errorf("x86encode: %s requires 16- or 64-bit register", op)
	}
	out := []byte{}
	if r.bits == 16 {
		out = append(out, 0x66)
	}
	if r.code >= 8 {
		out = append(out, 0x41)
	}
	out = append(out, opcode+(r.code&7))
	return out, true, nil
}

func encodeUnaryGroup(op string, operand Operand) ([]byte, bool, error) {
	r, ok := registers[operand.Reg]
	if !ok {
		return nil, false, nil
	}
	group, supported := map[string]byte{"not": 2, "mul": 4, "imul": 5, "div": 6, "idiv": 7}[op]
	if !supported {
		return nil, false, nil
	}
	if r.bits != 8 && r.bits != 16 && r.bits != 32 && r.bits != 64 {
		return nil, true, fmt.Errorf("x86encode: unary %s width %d unsupported", op, r.bits)
	}
	out := []byte{}
	if r.bits == 16 {
		out = append(out, 0x66)
	}
	rex := byte(0x40)
	if r.bits == 64 {
		rex |= 8
	}
	if r.code >= 8 {
		rex |= 1
	}
	forcedREX := forcedREXRegisters[operand.Reg]
	if forcedREX {
		rex |= 0x40
	}
	if rex != 0x40 || forcedREX {
		out = append(out, rex)
	}
	opcode := byte(0xf7)
	if r.bits == 8 {
		opcode = 0xf6
	}
	out = append(out, opcode, 0xc0|(group<<3)|(r.code&7))
	return out, true, nil
}

func encodeShiftImmediate(op string, operand Operand, count int64) ([]byte, bool, error) {
	r, ok := registers[operand.Reg]
	if !ok {
		return nil, false, nil
	}
	group, supported := map[string]byte{"shl": 4, "sal": 4, "shr": 5, "sar": 7}[op]
	if !supported {
		return nil, false, nil
	}
	if r.bits != 8 && r.bits != 16 && r.bits != 32 && r.bits != 64 {
		return nil, true, fmt.Errorf("x86encode: shift width %d unsupported", r.bits)
	}
	if count < 0 || count > 255 {
		return nil, true, fmt.Errorf("x86encode: shift count out of imm8 range")
	}
	out := []byte{}
	if r.bits == 16 {
		out = append(out, 0x66)
	}
	rex := byte(0x40)
	if r.bits == 64 {
		rex |= 8
	}
	if r.code >= 8 {
		rex |= 1
	}
	forcedREX := forcedREXRegisters[operand.Reg]
	if forcedREX {
		rex |= 0x40
	}
	if rex != 0x40 || forcedREX {
		out = append(out, rex)
	}
	if count == 1 {
		opcode := byte(0xd1)
		if r.bits == 8 {
			opcode = 0xd0
		}
		out = append(out, opcode, 0xc0|(group<<3)|(r.code&7))
	} else {
		opcode := byte(0xc1)
		if r.bits == 8 {
			opcode = 0xc0
		}
		out = append(out, opcode, 0xc0|(group<<3)|(r.code&7), byte(count))
	}
	return out, true, nil
}

func encodeLEA(dst, src Operand) ([]byte, error) {
	r, ok := registers[dst.Reg]
	if !ok {
		return nil, fmt.Errorf("x86encode: invalid LEA destination")
	}
	if r.bits != 16 && r.bits != 32 && r.bits != 64 {
		return nil, fmt.Errorf("x86encode: LEA destination width unsupported")
	}
	mod, rm, sib, disp, rexB, rexX := memoryEncoding(src)
	rex := byte(0x40)
	out := []byte{}
	if r.bits == 16 {
		out = append(out, 0x66)
	}
	if r.bits == 64 {
		rex |= 8
	}
	if r.code >= 8 {
		rex |= 4
	}
	if rexB {
		rex |= 1
	}
	if rexX {
		rex |= 2
	}
	if rex != 0x40 {
		out = append(out, rex)
	}
	out = append(out, 0x8d, modrm(mod, r.code&7, rm))
	if sib != nil {
		out = append(out, sib...)
	}
	out = append(out, disp...)
	return out, nil
}

func encodeGroup1Immediate(op string, dst Operand, imm int64) ([]byte, bool, error) {
	r, ok := registers[dst.Reg]
	if !ok {
		return nil, false, fmt.Errorf("x86encode: unknown destination register %q", dst.Reg)
	}
	group, supported := map[string]byte{"add": 0, "or": 1, "and": 4, "sub": 5, "xor": 6, "cmp": 7}[op]
	if !supported {
		return nil, false, nil
	}
	if r.bits != 8 && r.bits != 16 && r.bits != 32 && r.bits != 64 {
		return nil, true, fmt.Errorf("x86encode: immediate ALU width %d unsupported", r.bits)
	}
	short := r.bits != 8 && imm >= -128 && imm <= 127
	if r.bits == 64 && (imm < -2147483648 || imm > 2147483647) {
		return nil, true, fmt.Errorf("x86encode: 64-bit ALU immediate must fit sign-extended imm32")
	}
	modrmByte := byte(0xc0 | (group&7)<<3 | (r.code & 7))
	out := make([]byte, 0, 7)
	rex := byte(0x40)
	if r.bits == 64 {
		rex |= 0x08
	}
	if r.code >= 8 {
		rex |= 0x01
	}
	forcedREX := forcedREXRegisters[dst.Reg]
	if forcedREX {
		rex |= 0x40
	}
	if highByteRegisters[dst.Reg] && rex != 0x40 {
		return nil, true, fmt.Errorf("x86encode: high-byte register cannot be encoded with REX")
	}
	if r.bits == 16 {
		out = append(out, 0x66)
	}
	if rex != 0x40 || forcedREX {
		out = append(out, rex)
	}
	if r.bits == 8 {
		if r.code == 0 {
			acc := map[byte]byte{0: 0x04, 1: 0x0c, 4: 0x24, 5: 0x2c, 6: 0x34, 7: 0x3c}[group]
			if acc != 0 {
				out = append(out, acc, byte(imm))
				return out, true, nil
			}
		}
		out = append(out, 0x80, modrmByte, byte(imm))
		return out, true, nil
	}
	if short {
		out = append(out, 0x83, modrmByte, byte(imm))
	} else {
		// The accumulator form has a dedicated opcode and is preferred by
		// NASM when the destination is AL/AX/EAX/RAX.
		if r.code == 0 {
			acc := map[byte]byte{0: 0x05, 1: 0x0d, 4: 0x25, 5: 0x2d, 6: 0x35, 7: 0x3d}[group]
			if r.bits == 8 {
				acc = map[byte]byte{0: 0x04, 1: 0x0c, 4: 0x24, 5: 0x2c, 6: 0x34, 7: 0x3c}[group]
			}
			if acc != 0 {
				out = append(out, acc)
				if r.bits == 16 {
					var b [2]byte
					binary.LittleEndian.PutUint16(b[:], uint16(imm))
					out = append(out, b[:]...)
				} else {
					out = append(out, EncodeImmediate32(imm)...)
				}
				return out, true, nil
			}
		}
		out = append(out, 0x81, modrmByte)
		if r.bits == 16 {
			var b [2]byte
			binary.LittleEndian.PutUint16(b[:], uint16(imm))
			out = append(out, b[:]...)
		} else {
			out = append(out, EncodeImmediate32(imm)...)
		}
	}
	return out, true, nil
}

func encodeMemoryImmediate(op string, mem Operand, imm int64) ([]byte, bool, error) {
	group, supported := map[string]byte{"add": 0, "or": 1, "and": 4, "sub": 5, "xor": 6, "cmp": 7}[op]
	if op == "test" {
		bits := mem.Bits
		if bits != 8 && bits != 16 && bits != 32 && bits != 64 {
			return nil, true, fmt.Errorf("x86encode: TEST memory immediate requires explicit width")
		}
		mod, rm, sib, disp, rexB, rexX := memoryEncoding(mem)
		out := []byte{}
		if bits == 16 {
			out = append(out, 0x66)
		}
		rex := byte(0x40)
		if bits == 64 {
			rex |= 8
		}
		if rexB {
			rex |= 1
		}
		if rexX {
			rex |= 2
		}
		if rex != 0x40 {
			out = append(out, rex)
		}
		opcode := byte(0xf7)
		if bits == 8 {
			opcode = 0xf6
		}
		out = append(out, opcode, modrm(mod, 0, rm))
		if sib != nil {
			out = append(out, sib...)
		}
		out = append(out, disp...)
		if bits == 8 {
			out = append(out, byte(imm))
		} else if bits == 16 {
			var b [2]byte
			binary.LittleEndian.PutUint16(b[:], uint16(imm))
			out = append(out, b[:]...)
		} else {
			out = append(out, EncodeImmediate32(imm)...)
		}
		return out, true, nil
	}
	if op == "mov" {
		bits := mem.Bits
		if bits == 0 {
			return nil, true, fmt.Errorf("x86encode: MOV memory immediate requires size qualifier")
		}
		if bits != 8 && bits != 16 && bits != 32 && bits != 64 {
			return nil, true, fmt.Errorf("x86encode: invalid MOV immediate width")
		}
		mod, rm, sib, disp, rexB, rexX := memoryEncoding(mem)
		out := []byte{}
		if bits == 16 {
			out = append(out, 0x66)
		}
		rex := byte(0x40)
		if bits == 64 {
			rex |= 8
		}
		if rexB {
			rex |= 1
		}
		if rexX {
			rex |= 2
		}
		if rex != 0x40 {
			out = append(out, rex)
		}
		opcode := byte(0xc7)
		if bits == 8 {
			opcode = 0xc6
		}
		out = append(out, opcode, modrm(mod, 0, rm))
		if sib != nil {
			out = append(out, sib...)
		}
		out = append(out, disp...)
		if bits == 8 {
			out = append(out, byte(imm))
		} else if bits == 16 {
			var b [2]byte
			binary.LittleEndian.PutUint16(b[:], uint16(imm))
			out = append(out, b[:]...)
		} else {
			if bits == 64 && (imm < -2147483648 || imm > 2147483647) {
				return nil, true, fmt.Errorf("x86encode: qword memory immediate must fit sign-extended imm32")
			}
			out = append(out, EncodeImmediate32(imm)...)
		}
		return out, true, nil
	}
	if !supported {
		return nil, false, nil
	}
	bits := mem.Bits
	if bits == 0 {
		return nil, true, fmt.Errorf("x86encode: ALU memory immediate requires size qualifier")
	}
	if bits != 8 && bits != 16 && bits != 32 && bits != 64 {
		return nil, true, fmt.Errorf("x86encode: invalid ALU memory width")
	}
	short := imm >= -128 && imm <= 127
	if bits == 64 && (imm < -2147483648 || imm > 2147483647) {
		return nil, true, fmt.Errorf("x86encode: qword ALU immediate must fit sign-extended imm32")
	}
	mod, rm, sib, disp, rexB, rexX := memoryEncoding(mem)
	out := []byte{}
	if bits == 16 {
		out = append(out, 0x66)
	}
	rex := byte(0x40)
	if bits == 64 {
		rex |= 8
	}
	if rexB {
		rex |= 1
	}
	if rexX {
		rex |= 2
	}
	if rex != 0x40 {
		out = append(out, rex)
	}
	opcode := byte(0x81)
	if bits == 8 {
		opcode = 0x80
	} else if short {
		opcode = 0x83
	}
	out = append(out, opcode, modrm(mod, group, rm))
	if sib != nil {
		out = append(out, sib...)
	}
	out = append(out, disp...)
	if bits == 8 || short {
		out = append(out, byte(imm))
	} else if bits == 16 {
		var b [2]byte
		binary.LittleEndian.PutUint16(b[:], uint16(imm))
		out = append(out, b[:]...)
	} else {
		out = append(out, EncodeImmediate32(imm)...)
	}
	return out, true, nil
}

func encodeRegMem(op string, a, b Operand) ([]byte, error) {
	var regOp, mem Operand
	regIsDst := a.Kind == "reg"
	if regIsDst {
		regOp, mem = a, b
	} else {
		mem, regOp = a, b
	}
	r, ok := registers[regOp.Reg]
	if !ok {
		return nil, fmt.Errorf("x86encode: unknown register")
	}
	if r.bits != 8 && r.bits != 16 && r.bits != 32 && r.bits != 64 {
		return nil, fmt.Errorf("x86encode: unsupported width")
	}
	opcode, ok := registerOpcodeWidth(op, regIsDst, r.bits)
	if !ok {
		return nil, fmt.Errorf("x86encode: unsupported memory operation %q", op)
	}
	mod, rm, sib, disp, rexB, rexX := memoryEncoding(mem)
	rex := byte(0x40)
	if r.bits == 64 {
		rex |= 0x08
	}
	if r.code >= 8 {
		rex |= 0x04
	}
	if rexB {
		rex |= 0x01
	}
	if rexX {
		rex |= 0x02
	}
	forcedREX := forcedREXRegisters[regOp.Reg]
	if forcedREX {
		rex |= 0x40
	}
	if highByteRegisters[regOp.Reg] && rex != 0x40 {
		return nil, fmt.Errorf("x86encode: high-byte register cannot be encoded with REX")
	}
	out := []byte{}
	if r.bits == 16 {
		out = append(out, 0x66)
	}
	if rex != 0x40 || forcedREX {
		out = append(out, rex)
	}
	out = append(out, opcode, modrm(mod, r.code&7, rm))
	if sib != nil {
		out = append(out, sib...)
	}
	out = append(out, disp...)
	return out, nil
}

// registerOpcode returns the legacy non-byte opcode for a register/memory
// operation. For regIsDst it selects the r <- r/m direction; otherwise it
// selects r/m <- r. TEST has one direction and ignores regIsDst.
func registerOpcode(op string, regIsDst bool) (byte, bool) {
	return registerOpcodeWidth(op, regIsDst, 32)
}

func registerOpcodeWidth(op string, regIsDst bool, bits int) (byte, bool) {
	pair := map[string][2]byte{
		"mov": {0x89, 0x8b}, "add": {0x01, 0x03}, "or": {0x09, 0x0b},
		"and": {0x21, 0x23}, "sub": {0x29, 0x2b}, "xor": {0x31, 0x33},
		"cmp": {0x39, 0x3b},
	}
	if bits == 8 {
		pair = map[string][2]byte{"mov": {0x88, 0x8a}, "add": {0x00, 0x02}, "or": {0x08, 0x0a}, "and": {0x20, 0x22}, "sub": {0x28, 0x2a}, "xor": {0x30, 0x32}, "cmp": {0x38, 0x3a}}
		if op == "test" {
			return 0x84, true
		}
	} else if op == "test" {
		return 0x85, true
	}
	p, ok := pair[op]
	if !ok {
		return 0, false
	}
	if regIsDst {
		return p[1], true
	}
	return p[0], true
}
func modrm(mod, reg, rm byte) byte { return mod<<6 | (reg&7)<<3 | (rm & 7) }
func memoryEncoding(m Operand) (mod, rm byte, sib, disp []byte, rexB, rexX bool) {
	base := -1
	index := -1
	if r, ok := registers[m.Base]; ok {
		base = int(r.code)
		rexB = base >= 8
	}
	if r, ok := registers[m.Index]; ok {
		index = int(r.code)
		rexX = index >= 8
	}
	needSIB := index >= 0 || base < 0 || base&7 == 4
	if base < 0 {
		mod, rm = 0, 4
		sib = append(sib, byte(0<<6|4<<3|5))
		disp = EncodeImmediate32(m.Disp)
		if index >= 0 {
			sib[0] = (scaleBits(m.Scale) << 6) | byte((index&7)<<3) | 5
		}
		return
	}
	if m.Disp == 0 && base&7 != 5 {
		mod = 0
	} else if m.Disp >= -128 && m.Disp <= 127 {
		mod = 1
		disp = []byte{byte(m.Disp)}
	} else {
		mod = 2
		disp = EncodeImmediate32(m.Disp)
	}
	if needSIB {
		rm = 4
		s := (scaleBits(m.Scale) << 6) | byte(4<<3) | byte(base&7)
		if index >= 0 {
			s = (scaleBits(m.Scale) << 6) | byte((index&7)<<3) | byte(base&7)
		}
		sib = []byte{s}
	} else {
		rm = byte(base & 7)
	}
	return
}
func scaleBits(s int) byte {
	switch s {
	case 2:
		return 1
	case 4:
		return 2
	case 8:
		return 3
	default:
		return 0
	}
}

func EncodeImmediate32(v int64) []byte {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], uint32(v))
	return b[:]
}
