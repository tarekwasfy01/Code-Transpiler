// Copyright (c) 2026 Tarek Wasfy
package backend

import "testing"

const binaryFrontendFixture = "bits 64\ndefault rel\nsection .text\nglobal native_entry\nnative_entry:\n    mov rax, 42\n    ret\n"

func TestBinaryFrontendAssemblyMachineCOFFPE(t *testing.T) {
	asm := CompileOptions{InputKind: CompileInputAssembly, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"}
	machine, err := CompileBinaryInput([]byte(binaryFrontendFixture), CompileOptions{InputKind: CompileInputAssembly, OutputKind: CompileMachineCode, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	if err != nil || len(machine.Bytes) == 0 {
		t.Fatalf("assembly to machine: %v", err)
	}
	decoded, err := CompileBinaryInput(machine.Bytes, CompileOptions{InputKind: CompileInputMachine, OutputKind: CompileAssembly, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	if err != nil || decoded.Text == "" {
		t.Fatalf("machine to assembly: %v", err)
	}
	if _, err = LiftBinaryInput(machine.Bytes, CompileOptions{InputKind: CompileInputMachine}); err != nil {
		t.Fatalf("machine lifting: %v", err)
	}
	reconstructed, err := CompileBinaryInput(machine.Bytes, CompileOptions{InputKind: CompileInputMachine, OutputKind: CompileSource, TargetLanguage: "c", TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	if err != nil || reconstructed.Text == "" {
		t.Fatalf("machine to reconstructed C: %v", err)
	}
	obj, err := CompileBinaryInput([]byte(binaryFrontendFixture), CompileOptions{InputKind: CompileInputAssembly, OutputKind: CompileObject, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	if err != nil || len(obj.Bytes) == 0 {
		t.Fatalf("assembly to coff: %v", err)
	}
	coffAsm, err := CompileBinaryInput(obj.Bytes, CompileOptions{InputKind: CompileInputObject, OutputKind: CompileAssembly, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	if err != nil || coffAsm.Text == "" {
		t.Fatalf("coff to assembly: %v", err)
	}
	exe, err := CompileBinaryInput([]byte(binaryFrontendFixture), CompileOptions{InputKind: CompileInputAssembly, OutputKind: CompileExecutable, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	if err != nil || len(exe.Bytes) == 0 {
		t.Fatalf("assembly to pe: %v", err)
	}
	peAsm, err := CompileBinaryInput(exe.Bytes, CompileOptions{InputKind: CompileInputExecutable, OutputKind: CompileAssembly, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	if err != nil || peAsm.Text == "" {
		t.Fatalf("pe to assembly: %v", err)
	}
	_ = asm
}

func TestBinaryFrontendRecoversStraightLineIntegerDataflow(t *testing.T) {
	const source = "mov rax, 7\nmov rcx, 5\nimul rax, rcx\nadd rax, 2\nret\n"
	p, err := LiftBinaryInput([]byte(source), CompileOptions{InputKind: CompileInputAssembly, TargetArch: "x86_64", TargetOS: "windows", ABI: "win64"})
	if err != nil {
		t.Fatalf("lift straight-line integer dataflow: %v", err)
	}
	if len(p.Body.List) != 1 {
		t.Fatalf("recovered statements = %d, want one return", len(p.Body.List))
	}
	ret, ok := p.Body.List[0].(*ReturnStmt)
	if !ok {
		t.Fatalf("recovered statement type = %T, want ReturnStmt", p.Body.List[0])
	}
	if _, ok := ret.X.(*BinaryExpr); !ok {
		t.Fatalf("recovered return expression = %T, want BinaryExpr", ret.X)
	}
	if _, err := EmitSemanticDirect("c", p); err != nil {
		t.Fatalf("emit recovered UAST to C: %v", err)
	}
}

func TestX64EncoderDecoderClosure(t *testing.T) {
	p := x64Program{Instructions: []x64Instruction{
		{"label", xl("native_entry"), x64Operand{}},
		{"push", xr(xRBP), x64Operand{}}, {"mov", xr(xRAX), xi(7)}, {"mov", xr(xRCX), xr(xRAX)},
		{"mov", xm(xRBP, -8), xr(xRAX)}, {"mov", xr(xRDX), xm(xRBP, -8)},
		{"add", xr(xRAX), xr(xRCX)}, {"sub", xr(xRAX), xr(xRCX)}, {"and", xr(xRAX), xr(xRCX)}, {"or", xr(xRAX), xr(xRCX)}, {"xor", xr(xRAX), xr(xRCX)}, {"cmp", xr(xRAX), xr(xRCX)}, {"test", xr(xRAX), xr(xRCX)},
		{"imul", xr(xRAX), xr(xRCX)}, {"idiv", xr(xRCX), x64Operand{}}, {"div", xr(xRCX), x64Operand{}}, {"not", xr(xRAX), x64Operand{}}, {"neg", xr(xRAX), x64Operand{}}, {"shl", xr(xRAX), x64Operand{}}, {"shr", xr(xRAX), x64Operand{}}, {"sar", xr(xRAX), x64Operand{}},
		{"sub_sp", xi(32), x64Operand{}}, {"add_sp", xi(32), x64Operand{}}, {"cqo", x64Operand{}, x64Operand{}},
		{"je", xl("done"), x64Operand{}}, {"jmp", xl("done"), x64Operand{}}, {"call", xl("done"), x64Operand{}}, {"ud2", x64Operand{}, x64Operand{}},
		{"label", xl("done"), x64Operand{}}, {"pop", xr(xRBP), x64Operand{}}, {"ret", x64Operand{}, x64Operand{}},
	}}
	b, _, err := encodeX64(p)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := decodeX64(b, 0x140000000)
	if err != nil {
		t.Fatalf("decode emitted bytes: %v", err)
	}
	if len(got.Instructions) < 20 {
		t.Fatalf("decoded only %d instructions", len(got.Instructions))
	}
	floatForms := x64Program{Instructions: []x64Instruction{
		{"mov_to_xmm", xr(0), xr(xRAX)}, {"mov_from_xmm", xr(xRAX), xr(0)}, {"cvtsi2sd", xr(0), xr(xRAX)},
		{"addsd", xr(0), xr(1)}, {"subsd", xr(0), xr(1)}, {"mulsd", xr(0), xr(1)}, {"divsd", xr(0), xr(1)}, {"ucomisd", xr(0), xr(1)}, {"ret", x64Operand{}, x64Operand{}},
	}}
	fb, _, err := encodeX64(floatForms)
	if err != nil {
		t.Fatalf("encode float forms: %v", err)
	}
	if _, err = decodeX64(fb, 0); err != nil {
		t.Fatalf("decode float forms: %v", err)
	}
}

func TestAssemblyFrontendAcceptsOwnRenderer(t *testing.T) {
	// This is the productive assembler-input closure: any syntax emitted by
	// renderX64 for the currently supported instruction forms must parse back
	// into an encodable structured instruction sequence.
	p := x64Program{Instructions: []x64Instruction{
		{"label", xl("native_entry"), x64Operand{}},
		{"push", xr(xRBP), x64Operand{}},
		{"mov", xr(xRBP), xr(xRSP)},
		{"sub_sp", xi(32), x64Operand{}},
		{"mov", xr(xRAX), xi(42)},
		{"mov", xm(xRBP, -8), xr(xRAX)},
		{"mov", xr(xR10), xm(xRBP, -8)},
		{"add", xr(xRAX), xr(xR10)},
		{"sub", xr(xRAX), xr(xRCX)},
		{"lea", xr(xRDX), xl("done")},
		{"mov_to_xmm", xr(0), xr(xRAX)},
		{"cvtsi2sd", xr(1), xr(xR10)},
		{"addsd", xr(0), xr(1)},
		{"je", xl("done"), x64Operand{}},
		{"label", xl("done"), x64Operand{}},
		{"add_sp", xi(32), x64Operand{}},
		{"pop", xr(xRBP), x64Operand{}},
		{"ret", x64Operand{}, x64Operand{}},
	}}
	parsed, err := parseX64Assembly(renderX64(p))
	if err != nil {
		t.Fatalf("parse renderer output: %v\n%s", err, renderX64(p))
	}
	if _, _, err = encodeX64(parsed); err != nil {
		t.Fatalf("encode parsed renderer output: %v\n%s", err, renderX64(p))
	}
}

func TestAssemblyFrontendPreservesSignedImmediatesAndFrames(t *testing.T) {
	asm := "bits 64\ndefault rel\nsection .text\nglobal native_entry\nnative_entry:\n" +
		"    sub rsp, strict dword 80\n" +
		"    mov rax, -1\n" +
		"    add rax, -5\n" +
		"    sub rax, -8\n" +
		"    cmp rax, -1\n" +
		"    add rsp, strict dword 80\n" +
		"    ret\n"
	p, err := parseX64Assembly(asm)
	if err != nil {
		t.Fatalf("parse signed immediates: %v", err)
	}
	if len(p.Instructions) != 8 {
		t.Fatalf("instruction count = %d, want 8", len(p.Instructions))
	}
	if p.Instructions[1].Op != "sub_sp" || p.Instructions[1].A.Value != 80 {
		t.Fatalf("stack frame parsed as %#v", p.Instructions[1])
	}
	if p.Instructions[2].B.Value != -1 || p.Instructions[3].B.Value != -5 || p.Instructions[4].B.Value != -8 || p.Instructions[5].B.Value != -1 {
		t.Fatalf("signed immediates were not preserved: %#v", p.Instructions)
	}
	if p.Instructions[6].Op != "add_sp" || p.Instructions[6].A.Value != 80 {
		t.Fatalf("stack frame teardown parsed as %#v", p.Instructions[6])
	}
	if _, _, err = encodeX64(p); err != nil {
		t.Fatalf("encode parsed signed immediates: %v", err)
	}
	if got := inferredX64Frame(p.Instructions); got != 80 {
		t.Fatalf("inferred frame = %d, want 80", got)
	}
}

func TestX64DecoderAcceptsSignExtendedGroup1Immediate(t *testing.T) {
	// clang/nasm commonly encode `sub rsp, 32` as 48 83 EC 20.
	p, err := decodeX64([]byte{0x48, 0x83, 0xec, 0x20, 0xc3}, 0)
	if err != nil {
		t.Fatalf("decode 0x83 stack adjustment: %v", err)
	}
	if len(p.Instructions) != 2 || p.Instructions[0].Op != "sub_sp" || p.Instructions[0].A.Value != 32 {
		t.Fatalf("decoded instructions = %#v", p.Instructions)
	}
}

func TestX64DecoderMachineAddressingFormsAndShortBranches(t *testing.T) {
	tests := []struct {
		name  string
		code  []byte
		check func(*testing.T, x64Program)
	}{
		{"disp8", []byte{0x48, 0x8b, 0x45, 0x08, 0xc3}, func(t *testing.T, p x64Program) {
			if len(p.Instructions) != 2 || p.Instructions[0].A.Kind != 'r' || p.Instructions[0].B.Kind != 'm' || p.Instructions[0].B.Reg != xRBP || p.Instructions[0].B.Value != 8 || !p.Instructions[0].B.HasBase {
				t.Fatalf("disp8 decode = %#v", p.Instructions)
			}
		}},
		{"sib-index", []byte{0x48, 0x8b, 0x44, 0x88, 0x08, 0xc3}, func(t *testing.T, p x64Program) {
			m := p.Instructions[0].B
			if m.Kind != 'm' || !m.HasBase || m.Reg != xRAX || !m.HasIndex || m.Index != xRCX || m.Scale != 4 || m.Value != 8 {
				t.Fatalf("SIB decode = %#v", m)
			}
		}},
		{"rip-relative", []byte{0x48, 0x8b, 0x05, 0x78, 0x56, 0x34, 0x12, 0xc3}, func(t *testing.T, p x64Program) {
			m := p.Instructions[0].B
			if m.Kind != 'm' || !m.RIPRelative || m.Value != 0x12345678 || m.HasBase {
				t.Fatalf("RIP-relative decode = %#v", m)
			}
		}},
		{"group1-disp8", []byte{0x48, 0x83, 0x6d, 0x08, 0xfb, 0xc3}, func(t *testing.T, p x64Program) {
			if p.Instructions[0].Op != "sub" || p.Instructions[0].A.Kind != 'm' || p.Instructions[0].A.Value != 8 || p.Instructions[0].B.Value != -5 {
				t.Fatalf("group1 disp8 decode = %#v", p.Instructions)
			}
		}},
		{"short-branch-reachable-trap", []byte{0x74, 0x02, 0x90, 0xcc, 0xc3}, func(t *testing.T, p x64Program) {
			if p.Instructions[0].Op != "je" || p.Instructions[1].Op != "nop" || p.Instructions[2].Op != "int3" {
				t.Fatalf("short branch/padding = %#v", p.Instructions)
			}
			if p.Classifications[2] != "trap_int3" {
				t.Fatalf("reachable INT3 classification = %q", p.Classifications[2])
			}
		}},
		{"short-jump-unreachable-padding", []byte{0xeb, 0x01, 0xcc, 0xc3}, func(t *testing.T, p x64Program) {
			if p.Instructions[0].Op != "jmp" || p.Classifications[1] != "padding_int3" {
				t.Fatalf("short jump/padding = %#v classifications=%#v", p.Instructions, p.Classifications)
			}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p, err := decodeX64(tc.code, 0x1000)
			if err != nil {
				t.Fatal(err)
			}
			tc.check(t, p)
		})
	}
}

func TestDecodeMachineIRPreservesAddressingAndCFG(t *testing.T) {
	ir, err := DecodeMachineIR([]byte{0x74, 0x02, 0x90, 0xcc, 0xc3}, CompileOptions{InputKind: CompileInputMachine, BaseAddress: 0x1000})
	if err != nil {
		t.Fatal(err)
	}
	if len(ir.Instructions) != 5 || len(ir.CFG) == 0 {
		t.Fatalf("machine IR = %#v", ir)
	}
	if ir.Instructions[1].Classification != "padding_nop" || ir.Instructions[2].Classification != "trap_int3" {
		t.Fatalf("provenance lost: %#v", ir.Instructions)
	}
}

func TestX64INT3ReachabilitySeparatesPaddingFromTrapLift(t *testing.T) {
	// The short jump makes the intervening INT3 unreachable. It is residual
	// padding. Structured branch lifting is intentionally still fail-closed;
	// this test proves the decoder classification boundary only.
	padding := []byte{0xeb, 0x01, 0xcc, 0x48, 0xb8, 42, 0, 0, 0, 0, 0, 0, 0, 0xc3}
	p, err := decodeX64(padding, 0)
	if err != nil || p.Classifications[1] != "padding_int3" {
		t.Fatalf("unreachable INT3 padding classification: err=%v program=%#v", err, p)
	}
	// On the fallthrough path the same byte is executable and therefore an
	// observable trap. The lifter must reject rather than erase it.
	trap := []byte{0x48, 0xb8, 42, 0, 0, 0, 0, 0, 0, 0, 0xcc, 0xc3}
	if _, err := LiftBinaryInput(trap, CompileOptions{InputKind: CompileInputMachine}); err == nil {
		t.Fatal("reachable INT3 trap was silently discarded")
	}
}
