package backend

// This is a target instruction representation, not a semantic IR. Both text
// and binary output consume these same selected instructions.
import (
	"encoding/binary"
	"fmt"
	"strings"
)

type x64Operand struct {
	Kind  byte
	Reg   byte
	Value int64
	Label string
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
	Functions    []x64Function
}

func xr(r byte) x64Operand          { return x64Operand{Kind: 'r', Reg: r} }
func xi(v int64) x64Operand         { return x64Operand{Kind: 'i', Value: v} }
func xm(r byte, off int) x64Operand { return x64Operand{Kind: 'm', Reg: r, Value: int64(off)} }
func xl(s string) x64Operand        { return x64Operand{Kind: 'l', Label: s} }

const (
	xRAX byte = 0
	xRCX byte = 1
	xRDX byte = 2
	xRSP byte = 4
	xRBP byte = 5
	xR8  byte = 8
	xR9  byte = 9
	xR10 byte = 10
)

// M_ENC: form -> opcode and ModRM group. Operand placement is shared by all
// source languages. No parser or source text participates in encoding.
var x64BinaryOpcodes = map[string]byte{"add": 0x03, "sub": 0x2b, "and": 0x23, "or": 0x0b, "xor": 0x33, "cmp": 0x3b, "test": 0x85}
var x64Conditions = map[string]byte{"je": 4, "jne": 5, "jl": 12, "jle": 14, "jg": 15, "jge": 13, "jb": 2, "jbe": 6, "ja": 7, "jae": 3, "jp": 10}

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
	rex := func(w bool, r, b byte) {
		x := byte(0x40)
		if w {
			x |= 8
		}
		x |= (r>>3)<<2 | b>>3
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
		out = append(out, 0x80|(r&7)<<3|b.Reg&7)
		if b.Reg&7 == 4 {
			out = append(out, 0x24)
		}
		put32(b.Value)
		return nil
	}
	for _, in := range p.Instructions {
		a, b := in.A, in.B
		if in.Op == "label" {
			if _, ok := labels[a.Label]; ok {
				return nil, nil, fmt.Errorf("duplicate label %s", a.Label)
			}
			labels[a.Label] = len(out)
			continue
		}
		if opcode, ok := x64BinaryOpcodes[in.Op]; ok {
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
		case "addsd", "subsd", "mulsd", "divsd", "ucomisd":
			prefix := byte(0xf2)
			if in.Op == "ucomisd" {
				prefix = 0x66
			}
			out = append(out, prefix)
			rex(false, a.Reg, b.Reg)
			out = append(out, 0x0f, map[string]byte{"addsd": 0x58, "subsd": 0x5c, "mulsd": 0x59, "divsd": 0x5e, "ucomisd": 0x2e}[in.Op])
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
				return nil, nil, fmt.Errorf("invalid mov operands")
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
			return fmt.Sprintf("qword [dword %s%+d]", names[a.Reg], a.Value)
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
		if in.Op == "cvtsi2sd" {
			fmt.Fprintf(&out, "    cvtsi2sd xmm%d, %s\n", in.A.Reg, operand(in.B))
			continue
		}
		if in.Op == "addsd" || in.Op == "subsd" || in.Op == "mulsd" || in.Op == "divsd" || in.Op == "ucomisd" {
			fmt.Fprintf(&out, "    %s xmm%d, xmm%d\n", in.Op, in.A.Reg, in.B.Reg)
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
	return out.String()
}
