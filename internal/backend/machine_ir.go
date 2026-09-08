// Copyright (c) 2026 Tarek Wasfy
package backend

import "fmt"

// MachineIR is the lossless structural boundary between x86 decoding and the
// existing semantic lifter.  It contains addressing/provenance facts without
// promoting instruction encodings into semantic primitives.
type MachineIR struct {
	Architecture string                 `json:"architecture"`
	BaseAddress  uint64                 `json:"base_address"`
	Instructions []MachineIRInstruction `json:"instructions"`
	CFG          []MachineIREdge        `json:"cfg"`
}

type MachineIRInstruction struct {
	Index          int                `json:"index"`
	Offset         int                `json:"offset"`
	Operation      string             `json:"operation"`
	Classification string             `json:"classification"`
	Primitive      string             `json:"primitive,omitempty"`
	Operands       []MachineIROperand `json:"operands,omitempty"`
}

type MachineIROperand struct {
	Kind         string `json:"kind"`
	Register     string `json:"register,omitempty"`
	Index        string `json:"index,omitempty"`
	Scale        byte   `json:"scale,omitempty"`
	Immediate    int64  `json:"immediate,omitempty"`
	Displacement int64  `json:"displacement,omitempty"`
	Label        string `json:"label,omitempty"`
	HasBase      bool   `json:"has_base,omitempty"`
	RIPRelative  bool   `json:"rip_relative,omitempty"`
	Absolute     bool   `json:"absolute,omitempty"`
}

type MachineIREdge struct {
	From int    `json:"from"`
	To   int    `json:"to"`
	Kind string `json:"kind"`
}

var x64RegisterNames = []string{"rax", "rcx", "rdx", "rbx", "rsp", "rbp", "rsi", "rdi", "r8", "r9", "r10", "r11", "r12", "r13", "r14", "r15"}

func x64MachineClassification(op string) string {
	switch op {
	case "nop":
		return "padding_nop"
	case "int3":
		return "padding_or_trap"
	case "label":
		return "label"
	case "je", "jne", "jl", "jle", "jg", "jge", "jb", "jbe", "ja", "jae", "jo", "jno", "js", "jns", "jp", "jnp":
		return "conditional_branch"
	case "jmp":
		return "unconditional_branch"
	case "call":
		return "call"
	case "ret":
		return "return"
	default:
		return "instruction"
	}
}

func x64PrimitiveFor(op string, a, b x64Operand) string {
	switch op {
	case "add", "sub", "imul", "and", "or", "xor", "shl", "shr", "sar", "addsd", "subsd", "mulsd", "divsd":
		return "BINARY"
	case "sqrtsd":
		return "UNARY"
	case "cmp", "test", "ucomisd":
		return "COMPARE"
	case "neg", "not":
		return "UNARY"
	case "mov":
		if a.Kind == 'm' {
			return "STORE"
		}
		if b.Kind == 'm' {
			return "LOAD"
		}
		return "MOVE"
	case "lea":
		return "ADDRESS"
	case "sub_sp", "add_sp":
		return "STACK_FRAME"
	case "ret":
		return "CONTROL_RETURN"
	case "call":
		return "CALL"
	case "jmp", "je", "jne", "jl", "jle", "jg", "jge", "jb", "jbe", "ja", "jae", "jo", "jno", "js", "jns", "jp", "jnp":
		return "CONTROL_BRANCH"
	case "nop", "int3", "label":
		return ""
	default:
		return ""
	}
}

func machineIROperand(o x64Operand) MachineIROperand {
	switch o.Kind {
	case 'r':
		name := fmt.Sprintf("r%d", o.Reg)
		if int(o.Reg) < len(x64RegisterNames) {
			name = x64RegisterNames[o.Reg]
		}
		return MachineIROperand{Kind: "register", Register: name}
	case 'i':
		return MachineIROperand{Kind: "immediate", Immediate: o.Value}
	case 'l':
		return MachineIROperand{Kind: "label", Label: o.Label}
	case 'm':
		m := MachineIROperand{Kind: "memory", Displacement: o.Value, HasBase: o.HasBase, RIPRelative: o.RIPRelative, Absolute: o.Absolute}
		if o.HasBase && int(o.Reg) < len(x64RegisterNames) {
			m.Register = x64RegisterNames[o.Reg]
		}
		if o.HasIndex && int(o.Index) < len(x64RegisterNames) {
			m.Index = x64RegisterNames[o.Index]
			m.Scale = o.Scale
		}
		return m
	default:
		return MachineIROperand{Kind: "none"}
	}
}

// DecodeMachineIR decodes assembly, raw machine bytes, COFF objects, or PE32+
// executables using the same x64 decoder as the binary frontend.  It is an
// evidence/inspection API; semantic promotion still occurs only in
// LiftBinaryInput and remains fail-closed for unknown dataflow.
func DecodeMachineIR(data []byte, opts CompileOptions) (*MachineIR, error) {
	var p x64Program
	base := opts.BaseAddress
	var err error
	switch opts.InputKind {
	case CompileInputAssembly:
		p, err = parseX64Assembly(string(data))
		if err == nil {
			var encoded []byte
			encoded, _, err = encodeX64(p)
			if err == nil {
				p, err = decodeX64(encoded, base)
			}
		}
	case CompileInputMachine:
		p, err = decodeX64(data, base)
	case CompileInputObject:
		var b []byte
		b, err = coffText(data)
		if err == nil {
			p, err = decodeX64(b, base)
		}
	case CompileInputExecutable:
		var b []byte
		b, base, err = peText(data)
		if err == nil {
			p, err = decodeX64(b, base)
		}
	default:
		return nil, fmt.Errorf("machine IR: unsupported input kind %q", opts.InputKind)
	}
	if err != nil {
		return nil, err
	}
	ir := &MachineIR{Architecture: "x86_64", BaseAddress: base}
	labelIndex := map[string]int{}
	for i, in := range p.Instructions {
		off := -1
		if i < len(p.Offsets) {
			off = p.Offsets[i]
		}
		class := x64MachineClassification(in.Op)
		if i < len(p.Classifications) && p.Classifications[i] != "" {
			class = p.Classifications[i]
		}
		if in.Op == "label" {
			labelIndex[in.A.Label] = i
		}
		row := MachineIRInstruction{Index: i, Offset: off, Operation: in.Op, Classification: class, Primitive: x64PrimitiveFor(in.Op, in.A, in.B)}
		if in.A.Kind != 0 {
			row.Operands = append(row.Operands, machineIROperand(in.A))
		}
		if in.B.Kind != 0 {
			row.Operands = append(row.Operands, machineIROperand(in.B))
		}
		ir.Instructions = append(ir.Instructions, row)
	}
	for i, in := range p.Instructions {
		if in.Op == "label" {
			continue
		}
		if i+1 < len(p.Instructions) {
			ir.CFG = append(ir.CFG, MachineIREdge{From: i, To: i + 1, Kind: "fallthrough"})
		}
		if in.Op == "jmp" || x64MachineClassification(in.Op) == "conditional_branch" {
			if in.A.Kind == 'l' {
				if to, ok := labelIndex[in.A.Label]; ok {
					ir.CFG = append(ir.CFG, MachineIREdge{From: i, To: to, Kind: x64MachineClassification(in.Op)})
				}
			}
		}
	}
	return ir, nil
}
