// Copyright (c) 2026 Tarek Wasfy
package nasm

import (
	"bytes"
	"encoding/hex"
	"os"
	"os/exec"
	"testing"
)

func TestNASMNativeByteComparisonAddEAXEBX(t *testing.T) {
	if _, err := exec.LookPath("nasm"); err != nil {
		t.Skip("nasm is not installed")
	}
	comparison, err := CompareNASMNativeWitness("bits 64\nsection .text\nadd eax, ebx\n")
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Result != "PASS" {
		t.Fatalf("NASM/native mismatch: nasm=%s native=%s", hex.EncodeToString(comparison.NASMBytes), hex.EncodeToString(comparison.NativeBytes))
	}
	if got := hex.EncodeToString(comparison.NativeBytes); got != "01d8" {
		t.Fatalf("unexpected native encoding: %s", got)
	}
}

func TestNASMNativeBaseInstructionFamily(t *testing.T) {
	if _, err := exec.LookPath("nasm"); err != nil {
		t.Skip("nasm is not installed")
	}
	for _, instruction := range []string{
		"add rax, rbx", "add r8, r9", "sub eax, ebx", "mov eax, ebx", "and eax, ebx", "or eax, ebx", "xor eax, ebx", "cmp eax, ebx", "test eax, ebx", "add eax, 1", "add rax, 1", "ret",
	} {
		comparison, err := CompareNASMNativeWitness("bits 64\nsection .text\n" + instruction + "\n")
		if err != nil {
			t.Fatalf("%s: %v", instruction, err)
		}
		if comparison.Result != "PASS" {
			t.Fatalf("%s: nasm=%s native=%s", instruction, hex.EncodeToString(comparison.NASMBytes), hex.EncodeToString(comparison.NativeBytes))
		}
	}
}

func TestNASMNativeLogicalAndCompareDirections(t *testing.T) {
	for _, instruction := range []string{
		"and eax, ebx", "and [rax], ebx", "and ebx, [rax]",
		"or eax, ebx", "or [rax], ebx", "or ebx, [rax]",
		"xor eax, ebx", "xor [rax], ebx", "xor ebx, [rax]",
		"cmp eax, ebx", "cmp [rax], ebx", "cmp ebx, [rax]",
		"test eax, ebx", "test [rax], ebx",
		"mov byte [rbx], 8", "mov dword [rbx+8], 300", "mov qword [r12+8], 300",
		"or byte [rbx], 8", "and dword [rbx+8], 300", "cmp qword [r12+8], 7",
	} {
		comparison, err := CompareNASMNativeWitness("bits 64\nsection .text\n" + instruction + "\n")
		if err != nil {
			t.Fatalf("%s: %v", instruction, err)
		}
		if comparison.Result != "PASS" {
			t.Fatalf("%s: NASM=%x native=%x", instruction, comparison.NASMBytes, comparison.NativeBytes)
		}
	}
}

func TestNASMNativeGroup1ImmediateWidths(t *testing.T) {
	for _, instruction := range []string{
		"add eax, 7", "add eax, 300", "add r9, -7", "add r9, 300",
		"or eax, 7", "and r9, 300", "sub eax, -128", "xor r9, -129", "cmp eax, 300",
	} {
		comparison, err := CompareNASMNativeWitness("bits 64\nsection .text\n" + instruction + "\n")
		if err != nil {
			t.Fatalf("%s: %v", instruction, err)
		}
		if comparison.Result != "PASS" {
			t.Fatalf("%s: NASM=%x native=%x", instruction, comparison.NASMBytes, comparison.NativeBytes)
		}
	}
}

func TestNASMNativeUnaryShiftStackAndLEAFamilies(t *testing.T) {
	for _, instruction := range []string{
		"not eax", "not r9", "mul eax", "imul rax", "div eax", "idiv r9",
		"shl eax, 3", "shr r9, 1", "sar rax, 7", "push rax", "push r9", "pop rbx", "pop r12",
		"lea rax, [rbx+rcx*4+16]", "lea r9, [r12+8]",
	} {
		comparison, err := CompareNASMNativeWitness("bits 64\nsection .text\n" + instruction + "\n")
		if err != nil {
			t.Fatalf("%s: %v", instruction, err)
		}
		if comparison.Result != "PASS" {
			t.Fatalf("%s: NASM=%x native=%x", instruction, comparison.NASMBytes, comparison.NativeBytes)
		}
	}
}

func TestNASMNativeByteWordRegisterAndREXRules(t *testing.T) {
	for _, instruction := range []string{
		"mov al, bl", "add ah, bh", "xor spl, dil", "mov r8b, r15b",
		"add ax, bx", "sub r8w, r15w", "mov ax, [rbx+8]", "mov [r12+8], ax",
		"add al, 7", "add ax, 300", "shl ah, 1", "sar r8b, 3",
		"mov bl, [r12+8]", "xor [rbx+8], al", "cmp ax, [r12+8]", "test byte [rbx], al",
		"push ax", "pop r15w", "not ax",
		"not dword [rbx+8]", "idiv qword [r12+8]", "mul byte [rbx]", "shl qword [rbx+8], 3",
		"sar byte [r12+8], 1", "push qword [rbx+8]", "pop word [r12+8]", "test qword [rbx], 15",
		"call qword [rbx+8]", "jmp qword [r12+8]", "push 8", "push 300", "test eax, 32", "ret 16",
	} {
		comparison, err := CompareNASMNativeWitness("bits 64\nsection .text\n" + instruction + "\n")
		if err != nil {
			t.Fatalf("%s: %v", instruction, err)
		}
		if comparison.Result != "PASS" {
			t.Fatalf("%s: NASM=%x native=%x", instruction, comparison.NASMBytes, comparison.NativeBytes)
		}
	}
	if _, err := EncodeNativeWitness("bits 64\nmov ah, r8b\n"); err == nil {
		t.Fatal("expected high-byte plus REX register combination to fail")
	}
}

func TestNASMNativeMemoryAndBranchFamilies(t *testing.T) {
	if _, err := exec.LookPath("nasm"); err != nil {
		t.Skip("nasm is not installed")
	}
	cases := []string{
		"mov eax, [rax]",
		"mov eax, [rsp+8]",
		"mov eax, [r12+8]",
		"mov eax, [rbp+0]",
		"mov eax, [r13+0]",
		"mov eax, [rax+rcx*2+16]",
		"mov [rax+rcx*4+16], eax",
	}
	for _, instruction := range cases {
		comparison, err := CompareNASMNativeWitness("bits 64\nsection .text\n" + instruction + "\n")
		if err != nil {
			t.Fatalf("%s: %v", instruction, err)
		}
		if comparison.Result != "PASS" {
			t.Fatalf("%s: nasm=%s native=%s", instruction, hex.EncodeToString(comparison.NASMBytes), hex.EncodeToString(comparison.NativeBytes))
		}
	}
	comparison, err := CompareNASMNativeWitness("bits 64\nsection .text\ncall done\ndone:\nret\n")
	if err != nil {
		t.Fatal(err)
	}
	if comparison.Result != "PASS" {
		t.Fatalf("call done: nasm=%s native=%s", hex.EncodeToString(comparison.NASMBytes), hex.EncodeToString(comparison.NativeBytes))
	}
	for _, branch := range []string{"jmp near done", "je near done", "jne near done", "jmp short done", "je short done", "ja short done"} {
		comparison, err := CompareNASMNativeWitness("bits 64\nsection .text\n" + branch + "\ndone:\nret\n")
		if err != nil {
			t.Fatalf("%s: %v", branch, err)
		}
		if comparison.Result != "PASS" {
			t.Fatalf("%s: nasm=%s native=%s", branch, hex.EncodeToString(comparison.NASMBytes), hex.EncodeToString(comparison.NativeBytes))
		}
	}
}

func TestWriteWitnessComparisonPersistsJSONL(t *testing.T) {
	if _, err := exec.LookPath("nasm"); err != nil {
		t.Skip("nasm is not installed")
	}
	comparison, err := CompareNASMNativeWitness("bits 64\nsection .text\nadd eax, ebx\n")
	if err != nil {
		t.Fatal(err)
	}
	path := t.TempDir() + "\\witness.jsonl"
	if err := WriteWitnessComparison(path, comparison); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte(`"witness_id"`)) || !bytes.Contains(data, []byte(`"result":"PASS"`)) {
		t.Fatalf("persisted comparison missing required fields: %s", data)
	}
}
