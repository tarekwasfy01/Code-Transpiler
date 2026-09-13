// Copyright (c) 2026 Tarek Wasfy
package x86encode

import (
	"encoding/hex"
	"testing"
)

func TestEncodeRegisterAndImmediateFamilies(t *testing.T) {
	cases := []struct {
		assembly string
		want     string
	}{
		{"add eax, ebx", "01d8"},
		{"add rax, rbx", "4801d8"},
		{"add r8, r9", "4d01c8"},
		{"sub eax, ebx", "29d8"},
		{"mov eax, ebx", "89d8"},
		{"add eax, 1", "83c001"},
		{"add rax, 1", "4883c001"},
		{"ret", "c3"},
	}
	for _, tc := range cases {
		instructions, err := ParseAssembly("bits 64\nsection .text\n" + tc.assembly + "\n")
		if err != nil {
			t.Fatalf("%s: %v", tc.assembly, err)
		}
		got, err := EncodeProgram(instructions)
		if err != nil {
			t.Fatalf("%s: %v", tc.assembly, err)
		}
		if hex.EncodeToString(got) != tc.want {
			t.Errorf("%s: got %s want %s", tc.assembly, hex.EncodeToString(got), tc.want)
		}
	}
}

func TestEncodeMemoryAddressing(t *testing.T) {
	tests := []struct {
		assembly string
		want     string
	}{
		{"mov eax, [rax]", "8b00"},
		{"mov eax, [rsp+8]", "8b442408"},
		{"mov eax, [r12+8]", "418b442408"},
		{"mov eax, [rbp+0]", "8b4500"},
		{"mov eax, [r13+0]", "418b4500"},
		{"mov eax, [rax+rcx*2+16]", "8b444810"},
		{"mov [rax+rcx*4+16], eax", "89448810"},
	}
	for _, tt := range tests {
		program, err := ParseAssembly(tt.assembly)
		if err != nil {
			t.Fatalf("%s: %v", tt.assembly, err)
		}
		got, err := EncodeProgram(program)
		if err != nil {
			t.Fatalf("%s: %v", tt.assembly, err)
		}
		if gotHex := hex.EncodeToString(got); gotHex != tt.want {
			t.Errorf("%s: got %s want %s", tt.assembly, gotHex, tt.want)
		}
	}
}

func TestEncodeRelativeBranches(t *testing.T) {
	program, err := ParseAssembly("start:\n  jmp done\n  add eax, 1\ndone:\n  ret\n")
	if err != nil {
		t.Fatal(err)
	}
	got, err := EncodeProgram(program)
	if err != nil {
		t.Fatal(err)
	}
	if gotHex := hex.EncodeToString(got); gotHex != "e90300000083c001c3" {
		t.Fatalf("got %s want e90300000083c001c3", gotHex)
	}
}
