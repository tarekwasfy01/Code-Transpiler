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
	for _, branch := range []string{"jmp near done", "je near done", "jne near done"} {
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
