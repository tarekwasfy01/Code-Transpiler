package nasm

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseEncodingRetainsMachineFields(t *testing.T) {
	e := parseEncoding("[mr: hle o32 01 /r ib,s opaque_code]")
	if e.Slot != "mr" || e.ModRM == "unknown" || e.Immediate == "unknown" {
		t.Fatalf("lost encoding semantics: %#v", e)
	}
	if len(e.PrefixTokens) == 0 || e.PrefixTokens[0] != "hle" {
		t.Fatalf("prefix not retained: %#v", e.PrefixTokens)
	}
	if len(e.UnknownTokens) == 0 {
		t.Fatal("unknown encoding tokens must be preserved")
	}
	if len(e.OpcodeBytes) != 1 || e.OpcodeBytes[0] != "01" {
		t.Fatalf("literal opcode was not separated from expressions: %#v", e)
	}
	if e.ImmediateWidth != "8 bits" {
		t.Fatalf("immediate width lost: %#v", e)
	}
	branch := parseEncoding("[i: 0f 80+c rel]")
	if branch.OpcodeMap != "0f" || branch.DisplacementWidth != "mode/operand-size dependent relative field" || len(branch.OpcodeExpressions) != 1 {
		t.Fatalf("branch encoding pattern lost: %#v", branch)
	}
	markers := parseEncoding("[i: nw o64nw jcc8 jlen vex+.l0.0f.w0]")
	if len(markers.SemanticMarkers) != 5 || len(markers.UnknownTokens) != 0 {
		t.Fatalf("known bytecode semantics not normalized/preserved: %#v", markers)
	}
}

func TestMatchNASMVariantToNativeContractRequiresNativeRule(t *testing.T) {
	v := Variant{Mnemonic: "ADD", Operands: []string{"r32", "r32"}}
	c := []NativeEncodingContract{{ID: "nasm:add", Mnemonic: "ADD", Operands: []string{"r32", "r32"}, Architecture: "x86_64", Status: "insufficient_evidence"}}
	m := MatchNASMVariantToNativeContract(v, c)
	if len(m) != 1 || m[0].Status != "conditional" {
		t.Fatalf("expected conditional match, got %#v", m)
	}
}

func TestGenerateNASMWitnessDeterministic(t *testing.T) {
	w, ok := GenerateNASMWitness(Variant{ID: "v", Mnemonic: "ADD", Operands: []string{"r32", "r32"}})
	if !ok || w.Assembly != "add eax, eax" || w.Mode != "64" {
		t.Fatalf("unexpected witness: %#v", w)
	}
}

func TestEncodeNativeWitnessUsesOwnEncoder(t *testing.T) {
	b, err := EncodeNativeWitness("bits 64\nsection .text\nadd eax, eax\n")
	if err != nil || len(b) == 0 {
		t.Fatalf("native witness encode failed: %v bytes=%d", err, len(b))
	}
}

func TestModeFeatureAndAliasEvidenceStayQualified(t *testing.T) {
	if modeValidity([]string{"LONG"})["64"] != "valid" || modeValidity([]string{"NOLONG"})["64"] != "invalid" || modeValidity([]string{"8086"})["32"] != "unknown" {
		t.Fatal("mode evidence was overgeneralized")
	}
	if strings.Join(featureFlags([]string{"SSE2", "386", "LONG"}), ",") != "SSE2" || strings.Join(cpuGenerationFlags([]string{"SSE2", "386"}), ",") != "386" {
		t.Fatal("CPU generation and ISA feature were conflated")
	}
	details := featureConstraintDetails([]string{"APX", "NOAVX", "386"}, nil)
	if len(details) != 3 || details[0].FeatureID != "APX" || details[0].Requirement != "required" || details[1].FeatureID != "AVX" || details[1].Requirement != "forbidden" || details[2].Kind != "cpu_generation" {
		t.Fatalf("feature polarity/generation identity lost: %#v", details)
	}
	if machineSemantics("ADD", Record{}).FlagsWritten != "unknown" {
		t.Fatal("missing flag semantics were guessed")
	}
}

func TestStructuredConstraintsRetainProvenanceAndUnknowns(t *testing.T) {
	ev := []Evidence{{Repository: Repository, Commit: "abc", Branch: "main", SourceFile: "x86/insns.dat", StartLine: 12, EndLine: 12, ExtractionMethod: "test", Confidence: "direct"}}
	ops := operandConstraints([]string{"rm32", "imm32"})
	if len(ops) != 2 || ops[0].Kind != "register_or_memory" || ops[0].WidthBits != 32 || ops[1].Kind != "immediate" || ops[1].WidthBits != 32 {
		t.Fatalf("structured operand constraints lost distinctions: %#v", ops)
	}
	if ops[0].SourceDestinationRole != "unknown" || ops[0].AddressingConstraint == "" {
		t.Fatalf("operand role/addressing uncertainty not represented: %#v", ops[0])
	}
	modes := modeConstraintDetails([]string{"LONG"}, ev)
	if len(modes) != 3 || modes[0].Validity != "unknown" || modes[2].Validity != "valid" || modes[2].Evidence[0].Span().StartLine != 12 {
		t.Fatalf("mode evidence/span mismatch: %#v", modes)
	}
	prefixes := prefixConstraintDetails([]string{"o32", "lock"}, ev)
	if len(prefixes) != 2 || prefixes[0].Class != "size_control" || prefixes[0].Confidence == "" || prefixes[1].Effect == "" {
		t.Fatalf("prefix constraints not structured conservatively: %#v", prefixes)
	}
	sem := machineSemantics("ADD", Record{})
	if sem.MachineOperation != "integer.add.fixed_width" || sem.FlagsWritten != "unknown" || sem.Confidence != "inferred" {
		t.Fatalf("machine/canonical distinction or unknown effects lost: %#v", sem)
	}
}

func TestMacroMnemonicGroupingIsNotPromotedToSemanticAlias(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "insns.dat")
	out := filepath.Join(d, "aliases.jsonl")
	_ = os.WriteFile(src, []byte("$shift ROL ROR RCL RCR SHL,SAL SHR - SAR\n"), 0644)
	f, _ := os.Create(out)
	defer f.Close()
	if err := extractMacroAliasCandidates(src, f, Config{Commit: "c", Branch: "b"}); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	b, _ := os.ReadFile(out)
	if !strings.Contains(string(b), "macro_grouped_mnemonics") || !strings.Contains(string(b), `"semantic_alias_asserted":false`) {
		t.Fatalf("macro grouping became an asserted alias: %s", b)
	}
}

func TestOperandWidthsAndFormsStayDistinct(t *testing.T) {
	forms := [][]string{{"rm8", "reg8"}, {"rm16", "reg16"}, {"rm32", "reg32"}, {"rm64", "reg64"}, {"reg32", "rm32"}, {"reg32", "imm32"}}
	seen := map[string]bool{}
	for _, f := range forms {
		c := operandConstraints(f)
		b, _ := json.Marshal(c)
		seen[string(b)] = true
	}
	if len(seen) != len(forms) {
		t.Fatalf("operand forms collapsed: got %d want %d", len(seen), len(forms))
	}
	if widthOf("reg8") != 8 || widthOf("rm16") != 16 || widthOf("imm32") != 32 || widthOf("reg64") != 64 {
		t.Fatal("width parser failed")
	}
	if registerWidth("r8") != 64 || registerWidth("r8b") != 8 || registerWidth("r8w") != 16 || registerWidth("r8d") != 32 || registerWidth("xmm8") != 128 || registerWidth("ymm8") != 256 || registerWidth("zmm8") != 512 {
		t.Fatal("register width parser merged index digits with width")
	}
}

func TestRegisterRangesExpandWithoutLosingParentRelations(t *testing.T) {
	a, b, p, s, ok := registerRange("r8-31b")
	if !ok || a != 8 || b != 31 || p != "r" || s != "b" {
		t.Fatalf("range parse: %d %d %q %q %v", a, b, p, s, ok)
	}
	if registerParent("R8B") != "R8W" || registerParent("R8W") != "R8D" || registerParent("R8D") != "R8" {
		t.Fatal("subregister chain missing")
	}
}

func TestExtractionHasProvenanceAndBackwardEncodingIndex(t *testing.T) {
	d := t.TempDir()
	src := filepath.Join(d, "src")
	out := filepath.Join(d, "out")
	_ = os.MkdirAll(filepath.Join(src, "x86"), 0755)
	_ = os.MkdirAll(filepath.Join(src, "asm"), 0755)
	files := map[string]string{
		"x86/insns.dat": "ADD rm32,reg32 [mr: o32 01 /r] FL,386\nADD reg_eax,imm32 [ri: o32 05 id] 386\nADD rm32,imm32 [mi: o32 81 /0 id] 386\nADD ignore [u: 90] PSEUDO\n",
		"x86/insns.pl":  "# gen\n", "x86/preinsns.pl": "# expander\n", "x86/bytecode.txt": "# bytecode\n", "x86/regs.dat": "rax\tREG_RAX\treg64\t0\neax\tREG_EAX\treg32\t0\n", "asm/parser.c": "# parser\n", "asm/assemble.c": "# assembler\n", "x86/insns-iflags.ph": "# flags\n", "x86/iflags.ph": "if_(\"386\",\"386+\");\n",
	}
	for p, v := range files {
		_ = os.WriteFile(filepath.Join(src, filepath.FromSlash(p)), []byte(v), 0644)
	}
	exp := filepath.Join(d, "expanded.dat")
	_ = os.WriteFile(exp, []byte("ADD rm32,reg32 [mr: o32 01 /r] FL,386\nADD reg_eax,imm32 [ri: o32 05 id] 386\nADD rm32,imm32 [mi: o32 81 /0 id] 386\nADD ignore [u: 90] PSEUDO\n"), 0644)
	backend := filepath.Join(d, "backend.go")
	_ = os.WriteFile(backend, []byte("const assembler = true\n"), 0644)
	before, _ := os.ReadFile(backend)
	if _, err := Extract(Config{SourceRoot: src, ExpandedFile: exp, OutputDir: out, Commit: "abc", Branch: "test", NativeBackendFile: backend}); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(out, "nasm-instruction-variants.jsonl")
	f, _ := os.Open(p)
	defer f.Close()
	s := bufio.NewScanner(f)
	n := 0
	for s.Scan() {
		n++
		var v Variant
		if err := json.Unmarshal(s.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		if len(v.Evidence) == 0 || v.Evidence[0].Commit != "abc" {
			t.Fatalf("missing provenance: %+v", v)
		}
		if !v.Pseudo && v.OperandCount != 2 {
			t.Fatalf("operand distinction lost: %+v", v)
		}
	}
	if n != 4 {
		t.Fatalf("variants=%d want 4", n)
	}
	var pseudo Variant
	pf, _ := os.Open(p)
	ps := bufio.NewScanner(pf)
	for ps.Scan() {
		var candidate Variant
		_ = json.Unmarshal(ps.Bytes(), &candidate)
		if candidate.Pseudo {
			pseudo = candidate
		}
	}
	_ = pf.Close()
	if !pseudo.Pseudo || pseudo.OperandCount != 0 {
		t.Fatalf("pseudo form was not kept separate: %#v", pseudo)
	}
	enc, _ := os.ReadFile(filepath.Join(out, "nasm-encodings.jsonl"))
	if !strings.Contains(string(enc), "01") || !strings.Contains(string(enc), "05") {
		t.Fatalf("encoding lookup data missing: %s", enc)
	}
	forward, err := LookupVariants(out, "ADD", "64", []string{"r32", "imm32"})
	if err != nil || len(forward) != 2 {
		t.Fatalf("forward query mismatch: %#v, %v", forward, err)
	}
	for _, candidate := range forward {
		if candidate.Encoding == nil || len(candidate.Evidence) == 0 || candidate.Encoding.Evidence[0].StartLine == 0 {
			t.Fatalf("forward query omitted encoding/provenance: %#v", candidate)
		}
	}
	var accumulator, general *Variant
	for i := range forward {
		if forward[i].Operands[0] == "reg_eax" {
			accumulator = &forward[i]
		} else if forward[i].Operands[0] == "rm32" {
			general = &forward[i]
		}
	}
	if accumulator == nil || general == nil || accumulator.Encoding.OpcodeBytes[0] != "05" || general.Encoding.OpcodeBytes[0] != "81" || accumulator.Encoding.ModRM != "unknown" || general.Encoding.ModRM == "unknown" {
		t.Fatalf("operand-specific encoding proof was not returned: acc=%#v general=%#v", accumulator, general)
	}
	if !((forward[0].Operands[0] == "rm32" && forward[1].Operands[0] == "reg_eax") || (forward[1].Operands[0] == "rm32" && forward[0].Operands[0] == "reg_eax")) {
		t.Fatalf("operand candidates collapsed or changed: %#v", forward)
	}
	reverse, err := LookupEncoding(out, []string{"05"})
	if err != nil || len(reverse) != 1 || reverse[0].Mnemonic != "ADD" {
		t.Fatalf("reverse query mismatch: %#v, %v", reverse, err)
	}
	after, _ := os.ReadFile(backend)
	if string(before) != string(after) {
		t.Fatal("evidence comparison modified native encoder source")
	}
	comparison, _ := os.ReadFile(filepath.Join(out, "nasm-native-backend-comparison.jsonl"))
	if strings.Contains(string(comparison), "unsupported_in_native_backend") || !strings.Contains(string(comparison), "not_comparable") {
		t.Fatalf("unsupported status inferred without exact form proof: %s", comparison)
	}
}
