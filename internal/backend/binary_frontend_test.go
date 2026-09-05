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
