// Copyright (c) 2026 Tarek Wasfy
package backend

// This is a target instruction representation, not a semantic IR. Both text
// and binary output consume these same selected instructions.
import (
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

type x64Operand struct {
	Kind  byte
	Reg   byte  // base register for memory operands
	Value int64 // displacement or immediate
	Label string
	// The decoder keeps the complete x86 addressing form.  HasIndex/HasBase
	// distinguish an absent SIB component from register zero (RAX), while
	// RIPRelative records the architectural next-instruction base explicitly.
	Index       byte
	Scale       byte
	HasIndex    bool
	HasBase     bool
	RIPRelative bool
	Absolute    bool
}
type x64Instruction struct {
	Op   string
	A, B x64Operand
}
type x64Function struct {
	Label, End string
	Frame      int
}
type x64Program struct {
	Instructions []x64Instruction
	// Data is immutable literal storage emitted after the selected text.  It is
	// part of the target image, not a semantic IR; labels are resolved by the
	// same encoder fixup pass as control-flow labels.
	Data      map[string][]byte
	Functions []x64Function
	// Offsets and Classifications are decoder provenance. They are parallel to
	// Instructions (labels use offset -1) and are intentionally ignored by the
	// encoder, so source/machine round-trips retain representation facts without
	// changing the semantic instruction model.
	Offsets         []int
	Classifications []string
}

func xr(r byte) x64Operand  { return x64Operand{Kind: 'r', Reg: r} }
func xi(v int64) x64Operand { return x64Operand{Kind: 'i', Value: v} }
func xm(r byte, off int) x64Operand {
	return x64Operand{Kind: 'm', Reg: r, Value: int64(off), HasBase: true}
}
func xmIndexed(base, index, scale byte, off int64) x64Operand {
	return x64Operand{Kind: 'm', Reg: base, Index: index, Scale: scale, Value: off, HasBase: true, HasIndex: true}
}
func xl(s string) x64Operand { return x64Operand{Kind: 'l', Label: s} }

const (
	xRAX byte = 0
	xRCX byte = 1
	xRDX byte = 2
	xRSP byte = 4
	xRBP byte = 5
	xR8  byte = 8
	xR9  byte = 9
	xR10 byte = 10
	xR11 byte = 11
)

// M_ENC: form -> opcode and ModRM group. Operand placement is shared by all
// source languages. No parser or source text participates in encoding.
var x64BinaryOpcodes = map[string]byte{"add": 0x03, "sub": 0x2b, "and": 0x23, "or": 0x0b, "xor": 0x33, "cmp": 0x3b, "test": 0x85}
var x64Conditions = map[string]byte{"jo": 0, "jno": 1, "jb": 2, "jae": 3, "je": 4, "jne": 5, "jbe": 6, "ja": 7, "js": 8, "jns": 9, "jp": 10, "jnp": 11, "jl": 12, "jge": 13, "jle": 14, "jg": 15}

func encodeX64(p x64Program) ([]byte, map[string]int, error) {
	var out []byte
	labels := map[string]int{}
	type fix struct {
		at    int
		label string
	}
	var fixes []fix
	put32 := func(v int64) {
		var b [4]byte
		binary.LittleEndian.PutUint32(b[:], uint32(v))
		out = append(out, b[:]...)
	}
	// REX.X belongs to the SIB index register, not to the ModRM rm/base
	// register. Keep it as per-instruction encoder state so every existing
	// instruction form gets the same correct extended-index handling.
	var rexIndex byte
	rex := func(w bool, r, b byte) {
		x := byte(0x40)
		if w {
			x |= 8
		}
		x |= (r>>3)<<2 | (rexIndex>>3)<<1 | b>>3
		if x != 0x40 {
			out = append(out, x)
		}
	}
	rm := func(r byte, b x64Operand) error {
		if b.Kind == 'r' {
			out = append(out, 0xc0|(r&7)<<3|b.Reg&7)
			return nil
		}
		if b.Kind != 'm' {
			return fmt.Errorf("x64: expected register/memory")
		}
		// Keep the structured decoder's addressing facts encodable whenever the
		// form is representable by the existing instruction subset.  RIP-relative
		// and absolute SIB forms use mod=00; base+disp8 uses mod=01.
		if b.RIPRelative || b.Absolute || !b.HasBase {
			if b.HasIndex {
				return fmt.Errorf("x64: indexed absolute addressing is not encodable")
			}
			out = append(out, (r&7)<<3|5)
			put32(b.Value)
			return nil
		}
		mod := byte(2)
		if b.Value >= -128 && b.Value <= 127 {
			mod = 1
		}
		rm := b.Reg & 7
		if b.HasIndex || rm == 4 {
			rm = 4
		}
		out = append(out, mod<<6|(r&7)<<3|rm)
		if rm == 4 {
			// A SIB scale field of zero encodes a factor of one.  Memory
			// operands using RSP/R12 as the base require a SIB even without an
			// index; the structured operand constructors leave Scale at zero in
			// that case, which is the architectural default rather than an
			// invalid scale.
			if !b.HasIndex && b.Scale == 0 {
				b.Scale = 1
			}
			scale := byte(0)
			switch b.Scale {
			case 1:
				scale = 0
			case 2:
				scale = 1
			case 4:
				scale = 2
			case 8:
				scale = 3
			default:
				return fmt.Errorf("x64: invalid SIB scale %d", b.Scale)
			}
			idx := byte(4)
			if b.HasIndex {
				idx = b.Index & 7
			}
			out = append(out, scale<<6|idx<<3|(b.Reg&7))
		}
		if mod == 1 {
			out = append(out, byte(int8(b.Value)))
		} else {
			put32(b.Value)
		}
		return nil
	}
	for _, in := range p.Instructions {
		a, b := in.A, in.B
		rexIndex = 0
		if a.Kind == 'm' && a.HasIndex {
			rexIndex = a.Index
		} else if b.Kind == 'm' && b.HasIndex {
			rexIndex = b.Index
		}
		if in.Op == "label" {
			if _, ok := labels[a.Label]; ok {
				return nil, nil, fmt.Errorf("duplicate label %s", a.Label)
			}
			labels[a.Label] = len(out)
			continue
		}
		if opcode, ok := x64BinaryOpcodes[in.Op]; ok {
			// Group-1 immediate forms are emitted by independent assemblers for
			// ordinary arithmetic (for example `add rax, -5` and `cmp rax, -1`).
			// Keep these as structured operations instead of rejecting the
			// immediate as a non register/memory operand.
			if b.Kind == 'i' {
				if b.Value < -2147483648 || b.Value > 2147483647 {
					return nil, nil, fmt.Errorf("x64: immediate out of signed dword range")
				}
				groups := map[string]byte{"add": 0, "and": 4, "sub": 5, "xor": 6, "cmp": 7, "or": 1}
				group, exists := groups[in.Op]
				if !exists || a.Kind != 'r' && a.Kind != 'm' {
					return nil, nil, fmt.Errorf("x64: invalid immediate %s operands", in.Op)
				}
				rex(true, 0, func() byte {
					if a.Kind == 'r' {
						return a.Reg
					}
					return a.Reg
				}())
				out = append(out, 0x81)
				if err := rm(group, a); err != nil {
					return nil, nil, err
				}
				put32(b.Value)
				continue
			}
			if a.Kind != 'r' {
				return nil, nil, fmt.Errorf("%s destination must be register", in.Op)
			}
			if b.Kind == 'r' {
				if in.Op != "test" {
					opcode -= 2
				}
				rex(true, b.Reg, a.Reg)
				out = append(out, opcode)
				if err := rm(b.Reg, a); err != nil {
					return nil, nil, err
				}
				continue
			}
			rex(true, a.Reg, b.Reg)
			out = append(out, opcode)
			if err := rm(a.Reg, b); err != nil {
				return nil, nil, err
			}
			continue
		}
		if cc, ok := x64Conditions[in.Op]; ok {
			out = append(out, 0x0f, 0x80+cc)
			fixes = append(fixes, fix{len(out), a.Label})
			put32(0)
			continue
		}
		switch in.Op {
		case "mov_to_xmm", "mov_from_xmm":
			out = append(out, 0x66)
			if in.Op == "mov_to_xmm" {
				rex(true, a.Reg, b.Reg)
				out = append(out, 0x0f, 0x6e)
				if err := rm(a.Reg, b); err != nil {
					return nil, nil, err
				}
			} else {
				rex(true, b.Reg, a.Reg)
				out = append(out, 0x0f, 0x7e)
				if err := rm(b.Reg, a); err != nil {
					return nil, nil, err
				}
			}
		case "addsd", "subsd", "mulsd", "divsd", "ucomisd", "sqrtsd":
			prefix := byte(0xf2)
			if in.Op == "ucomisd" {
				prefix = 0x66
			}
			out = append(out, prefix)
			rex(false, a.Reg, b.Reg)
			opcode := map[string]byte{"addsd": 0x58, "subsd": 0x5c, "mulsd": 0x59, "divsd": 0x5e, "ucomisd": 0x2e, "sqrtsd": 0x51}[in.Op]
			out = append(out, 0x0f, opcode)
			if err := rm(a.Reg, b); err != nil {
				return nil, nil, err
			}
		case "cvtsi2sd":
			out = append(out, 0xf2)
			rex(true, a.Reg, b.Reg)
			out = append(out, 0x0f, 0x2a)
			if err := rm(a.Reg, b); err != nil {
				return nil, nil, err
			}
		case "mov":
			if a.Kind == 'r' && b.Kind == 'i' {
				rex(true, 0, a.Reg)
				out = append(out, 0xb8+a.Reg&7)
				var v [8]byte
				binary.LittleEndian.PutUint64(v[:], uint64(b.Value))
				out = append(out, v[:]...)
			} else if a.Kind == 'r' && b.Kind == 'r' {
				rex(true, b.Reg, a.Reg)
				out = append(out, 0x89)
				if err := rm(b.Reg, a); err != nil {
					return nil, nil, err
				}
			} else if a.Kind == 'r' {
				rex(true, a.Reg, b.Reg)
				out = append(out, 0x8b)
				if err := rm(a.Reg, b); err != nil {
					return nil, nil, err
				}
			} else if a.Kind == 'm' && b.Kind == 'r' {
				rex(true, b.Reg, a.Reg)
				out = append(out, 0x89)
				if err := rm(b.Reg, a); err != nil {
					return nil, nil, err
				}
			} else {
				return nil, nil, fmt.Errorf("invalid mov operands a=%+v b=%+v", a, b)
			}
		case "mov_byte":
			if a.Kind != 'm' || (b.Kind != 'r' && b.Kind != 'i') {
				return nil, nil, fmt.Errorf("invalid mov_byte operands a=%+v b=%+v", a, b)
			}
			if b.Kind == 'i' {
				if b.Value < 0 || b.Value > 255 {
					return nil, nil, fmt.Errorf("mov_byte immediate out of range")
				}
				rex(false, 0, a.Reg)
				out = append(out, 0xc6)
				if err := rm(0, a); err != nil {
					return nil, nil, err
				}
				out = append(out, byte(b.Value))
			} else {
				rex(false, b.Reg, a.Reg)
				out = append(out, 0x88)
				if err := rm(b.Reg, a); err != nil {
					return nil, nil, err
				}
			}
		case "movzx_byte":
			if a.Kind != 'r' || b.Kind != 'm' {
				return nil, nil, fmt.Errorf("invalid movzx_byte operands a=%+v b=%+v", a, b)
			}
			rex(true, a.Reg, b.Reg)
			out = append(out, 0x0f, 0xb6)
			if err := rm(a.Reg, b); err != nil {
				return nil, nil, err
			}
		case "lea":
			rex(true, a.Reg, b.Reg)
			out = append(out, 0x8d)
			if b.Kind == 'l' {
				out = append(out, (a.Reg&7)<<3|5)
				fixes = append(fixes, fix{len(out), b.Label})
				put32(0)
			} else if err := rm(a.Reg, b); err != nil {
				return nil, nil, err
			}
		case "imul":
			rex(true, a.Reg, b.Reg)
			out = append(out, 0x0f, 0xaf)
			if err := rm(a.Reg, b); err != nil {
				return nil, nil, err
			}
		case "idiv", "div", "not", "neg":
			group := map[string]byte{"idiv": 7, "div": 6, "not": 2, "neg": 3}[in.Op]
			rex(true, 0, a.Reg)
			out = append(out, 0xf7)
			if err := rm(group, a); err != nil {
				return nil, nil, err
			}
		case "shl", "shr", "sar":
			group := map[string]byte{"shl": 4, "shr": 5, "sar": 7}[in.Op]
			rex(true, 0, a.Reg)
			out = append(out, 0xd3)
			if err := rm(group, a); err != nil {
				return nil, nil, err
			}
		case "sub_sp", "add_sp":
			out = append(out, 0x48, 0x81)
			if in.Op == "sub_sp" {
				out = append(out, 0xec)
			} else {
				out = append(out, 0xc4)
			}
			put32(a.Value)
		case "push", "pop":
			rex(false, 0, a.Reg)
			op := byte(0x50)
			if in.Op == "pop" {
				op = 0x58
			}
			out = append(out, op+a.Reg&7)
		case "jmp", "call":
			op := byte(0xe9)
			if in.Op == "call" {
				op = 0xe8
			}
			out = append(out, op)
			fixes = append(fixes, fix{len(out), a.Label})
			put32(0)
		case "call_indirect":
			if a.Kind != 'r' && a.Kind != 'm' {
				return nil, nil, fmt.Errorf("x64: indirect call requires register/memory")
			}
			rex(true, 2, a.Reg)
			out = append(out, 0xff)
			if err := rm(2, a); err != nil {
				return nil, nil, err
			}
		case "ret":
			out = append(out, 0xc3)
		case "cqo":
			out = append(out, 0x48, 0x99)
		case "ud2":
			out = append(out, 0x0f, 0x0b)
		default:
			return nil, nil, fmt.Errorf("x64 encoding unavailable: %s", in.Op)
		}
	}
	// Literal data is placed after text so RIP-relative LEA fixups remain
	// self-contained and the PE writer can keep its existing section layout.
	dataNames := make([]string, 0, len(p.Data))
	for name := range p.Data {
		dataNames = append(dataNames, name)
	}
	sort.Strings(dataNames)
	for _, name := range dataNames {
		if _, exists := labels[name]; exists {
			return nil, nil, fmt.Errorf("duplicate data label %s", name)
		}
		labels[name] = len(out)
		out = append(out, p.Data[name]...)
	}
	for _, f := range fixes {
		dest, ok := labels[f.label]
		if !ok {
			return nil, nil, fmt.Errorf("undefined machine label %q", f.label)
		}
		delta := int64(dest - f.at - 4)
		if delta < -2147483648 || delta > 2147483647 {
			return nil, nil, fmt.Errorf("branch out of range")
		}
		binary.LittleEndian.PutUint32(out[f.at:], uint32(delta))
	}
	return out, labels, nil
}

func renderX64(p x64Program) string {
	names := []string{"rax", "rcx", "rdx", "rbx", "rsp", "rbp", "rsi", "rdi", "r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15"}
	operand := func(a x64Operand) string {
		switch a.Kind {
		case 'r':
			return names[a.Reg]
		case 'i':
			return fmt.Sprint(a.Value)
		case 'l':
			return a.Label
		case 'm':
			var b strings.Builder
			b.WriteString("qword [dword ")
			if a.RIPRelative {
				b.WriteString("rel ")
			} else if a.HasBase {
				b.WriteString(names[a.Reg])
			}
			if a.HasIndex {
				if a.HasBase {
					b.WriteByte('+')
				}
				b.WriteString(names[a.Index])
				if a.Scale > 1 {
					fmt.Fprintf(&b, "*%d", a.Scale)
				}
			}
			if a.Value != 0 || (!a.HasBase && !a.RIPRelative) {
				fmt.Fprintf(&b, "%+d", a.Value)
			}
			b.WriteByte(']')
			return b.String()
		}
		return ""
	}
	var out strings.Builder
	out.WriteString("bits 64\ndefault rel\nsection .text\nglobal native_entry\n")
	for _, in := range p.Instructions {
		if in.Op == "label" {
			fmt.Fprintf(&out, "%s:\n", in.A.Label)
			continue
		}
		if in.Op == "sub_sp" || in.Op == "add_sp" {
			fmt.Fprintf(&out, "    %s rsp, strict dword %d\n", strings.TrimSuffix(in.Op, "_sp"), in.A.Value)
			continue
		}
		if in.Op == "mov_to_xmm" {
			fmt.Fprintf(&out, "    movq xmm%d, %s\n", in.A.Reg, operand(in.B))
			continue
		}
		if in.Op == "mov_from_xmm" {
			fmt.Fprintf(&out, "    movq %s, xmm%d\n", operand(in.A), in.B.Reg)
			continue
		}
		if in.Op == "mov_byte" {
			fmt.Fprintf(&out, "    mov %s, %s\n", strings.Replace(operand(in.A), "qword", "byte", 1), operand(in.B))
			continue
		}
		if in.Op == "movzx_byte" {
			fmt.Fprintf(&out, "    movzx %s, byte %s\n", operand(in.A), operand(in.B))
			continue
		}
		if in.Op == "cvtsi2sd" {
			fmt.Fprintf(&out, "    cvtsi2sd xmm%d, %s\n", in.A.Reg, operand(in.B))
			continue
		}
		if in.Op == "addsd" || in.Op == "subsd" || in.Op == "mulsd" || in.Op == "divsd" || in.Op == "ucomisd" {
			fmt.Fprintf(&out, "    %s xmm%d, xmm%d\n", in.Op, in.A.Reg, in.B.Reg)
			continue
		}
		if in.Op == "sqrtsd" {
			fmt.Fprintf(&out, "    sqrtsd xmm%d, xmm%d\n", in.A.Reg, in.B.Reg)
			continue
		}
		fmt.Fprintf(&out, "    %s", in.Op)
		if _, cc := x64Conditions[in.Op]; cc || in.Op == "jmp" {
			out.WriteString(" strict near")
		}
		if in.A.Kind != 0 {
			fmt.Fprintf(&out, " %s", operand(in.A))
		}
		if in.B.Kind != 0 {
			if in.Op == "lea" && in.B.Kind == 'l' {
				fmt.Fprintf(&out, ", [rel %s]", in.B.Label)
			} else {
				fmt.Fprintf(&out, ", %s", operand(in.B))
			}
		} else if in.Op == "shl" || in.Op == "shr" || in.Op == "sar" {
			out.WriteString(", cl")
		}
		out.WriteByte('\n')
	}
	dataNames := make([]string, 0, len(p.Data))
	for name := range p.Data {
		dataNames = append(dataNames, name)
	}
	sort.Strings(dataNames)
	if len(dataNames) > 0 {
		out.WriteString("section .data\n")
		for _, name := range dataNames {
			fmt.Fprintf(&out, "%s: db ", name)
			for i, b := range p.Data[name] {
				if i > 0 {
					out.WriteString(", ")
				}
				fmt.Fprintf(&out, "%d", b)
			}
			out.WriteByte('\n')
		}
	}
	return out.String()
}
