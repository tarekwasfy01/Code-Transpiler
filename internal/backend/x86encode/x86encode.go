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
	Value int64
	Base  string
	Index string
	Scale int
	Disp  int64
}

type Instruction struct {
	Mnemonic string
	Operands []Operand
}

var registers = map[string]struct {
	code byte
	bits int
}{
	"eax": {0, 32}, "ecx": {1, 32}, "edx": {2, 32}, "ebx": {3, 32},
	"esp": {4, 32}, "ebp": {5, 32}, "esi": {6, 32}, "edi": {7, 32},
	"rax": {0, 64}, "rcx": {1, 64}, "rdx": {2, 64}, "rbx": {3, 64},
	"rsp": {4, 64}, "rbp": {5, 64}, "rsi": {6, 64}, "rdi": {7, 64},
	"r8": {8, 64}, "r9": {9, 64}, "r10": {10, 64}, "r11": {11, 64},
	"r12": {12, 64}, "r13": {13, 64}, "r14": {14, 64}, "r15": {15, 64},
	"r8d": {8, 32}, "r9d": {9, 32}, "r10d": {10, 32}, "r11d": {11, 32},
}

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
			for _, raw := range strings.Split(strings.Join(parts[1:], ""), ",") {
				operandText := strings.TrimSpace(raw)
				if isBranch(ins.Mnemonic) {
					operandText = strings.TrimPrefix(strings.TrimPrefix(operandText, "near"), "short")
					operandText = strings.TrimSpace(operandText)
				}
				var op Operand
				var err error
				if isBranch(ins.Mnemonic) && !strings.HasPrefix(operandText, "[") {
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

func parseOperand(s string) (Operand, error) {
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(s, "qword "), "dword "))
	if _, ok := registers[strings.ToLower(s)]; ok {
		return Operand{Kind: "reg", Reg: strings.ToLower(s)}, nil
	}
	v, err := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(s), "0x"), 16, 64)
	if err == nil {
		return Operand{Kind: "imm", Value: v}, nil
	}
	if v, err = strconv.ParseInt(s, 10, 64); err == nil {
		return Operand{Kind: "imm", Value: v}, nil
	}
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		body := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s[1:len(s)-1])), "-", "+-")
		var mem Operand
		mem.Kind, mem.Scale = "mem", 1
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
			out = append(out, branchBytes(ins.Mnemonic, int32(rel))...)
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
	switch strings.ToLower(op) {
	case "jmp", "je", "jne", "call":
		return true
	}
	return false
}
func instructionSize(ins Instruction) (int, error) {
	if isBranch(ins.Mnemonic) && len(ins.Operands) == 1 {
		return map[string]int{"jmp": 5, "call": 5, "je": 6, "jne": 6}[strings.ToLower(ins.Mnemonic)], nil
	}
	b, e := EncodeInstruction(ins)
	return len(b), e
}
func branchBytes(op string, rel int32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(rel))
	switch strings.ToLower(op) {
	case "jmp":
		return append([]byte{0xe9}, b...)
	case "call":
		return append([]byte{0xe8}, b...)
	case "je":
		return append([]byte{0x0f, 0x84}, b...)
	default:
		return append([]byte{0x0f, 0x85}, b...)
	}
}

func EncodeInstruction(ins Instruction) ([]byte, error) {
	op := strings.ToLower(ins.Mnemonic)
	if op == "ret" && len(ins.Operands) == 0 {
		return []byte{0xc3}, nil
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
		codes := map[string]byte{"add": 0x01, "sub": 0x29, "and": 0x21, "or": 0x09, "xor": 0x31, "cmp": 0x39, "test": 0x85, "mov": 0x89}
		code, ok := codes[op]
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
		if rex != 0x40 {
			out := []byte{rex, code, 0xc0 | (src.code&7)<<3 | (dst.code & 7)}
			return out, nil
		}
		return []byte{code, 0xc0 | (src.code&7)<<3 | (dst.code & 7)}, nil
	}
	if len(ins.Operands) == 2 && ins.Operands[0].Kind == "reg" && ins.Operands[1].Kind == "imm" && (op == "add" || op == "sub") {
		dst := registers[ins.Operands[0].Reg]
		if ins.Operands[1].Value < -128 || ins.Operands[1].Value > 127 {
			return nil, fmt.Errorf("x86encode: immediate out of short range")
		}
		group := byte(0)
		if op == "sub" {
			group = 5
		}
		prefix := byte(0)
		if dst.bits == 64 {
			prefix = 0x48
		}
		b := []byte{}
		if prefix != 0 {
			b = append(b, prefix)
		}
		b = append(b, 0x83, 0xc0|(group<<3)|(dst.code&7), byte(ins.Operands[1].Value))
		return b, nil
	}
	return nil, fmt.Errorf("x86encode: unsupported instruction %q", ins.Mnemonic)
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
	if r.bits != 32 && r.bits != 64 {
		return nil, fmt.Errorf("x86encode: unsupported width")
	}
	opcode := byte(0)
	switch op {
	case "mov":
		if regIsDst {
			opcode = 0x8b
		} else {
			opcode = 0x89
		}
	case "add":
		if regIsDst {
			opcode = 0x03
		} else {
			opcode = 0x01
		}
	case "sub":
		if regIsDst {
			opcode = 0x2b
		} else {
			opcode = 0x29
		}
	default:
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
	out := []byte{}
	if rex != 0x40 {
		out = append(out, rex)
	}
	out = append(out, opcode, modrm(mod, r.code&7, rm))
	if sib != nil {
		out = append(out, sib...)
	}
	out = append(out, disp...)
	return out, nil
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
