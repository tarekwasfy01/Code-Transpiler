// Copyright (c) 2026 Tarek Wasfy
package nasm

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// AssembleNASMWitness invokes the configured NASM executable and returns the
// real COFF object bytes. NASM remains an oracle; Native bytes come from the
// shared x86 encoder package.
func AssembleNASMWitness(assembly string) ([]byte, error) {
	dir, err := os.MkdirTemp("", "semantic-nasm-witness-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	asmPath := filepath.Join(dir, "witness.asm")
	objPath := filepath.Join(dir, "witness.obj")
	if err := os.WriteFile(asmPath, []byte(assembly), 0600); err != nil {
		return nil, err
	}
	cmd := exec.Command("nasm", "-f", "win64", "-o", objPath, asmPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("nasm witness: %w: %s", err, output)
	}
	return os.ReadFile(objPath)
}

// ExtractCOFFText returns the bytes from the actual COFF .text section.
// Header offsets are read from the object; no opcode-pattern search is used.
func ExtractCOFFText(object []byte) ([]byte, error) {
	if len(object) < 20 {
		return nil, fmt.Errorf("COFF header truncated")
	}
	sections := int(binary.LittleEndian.Uint16(object[2:4]))
	optional := int(binary.LittleEndian.Uint16(object[16:18]))
	start := 20 + optional
	if sections < 1 || start < 20 || start+40*sections > len(object) {
		return nil, fmt.Errorf("COFF section table truncated")
	}
	for i := 0; i < sections; i++ {
		h := object[start+i*40 : start+(i+1)*40]
		name := string(h[:8])
		for j, c := range name {
			if c == 0 {
				name = name[:j]
				break
			}
		}
		if name != ".text" {
			continue
		}
		size := int(binary.LittleEndian.Uint32(h[16:20]))
		offset := int(binary.LittleEndian.Uint32(h[20:24]))
		if offset < 0 || size < 0 || offset+size > len(object) {
			return nil, fmt.Errorf("COFF .text bounds invalid")
		}
		return append([]byte(nil), object[offset:offset+size]...), nil
	}
	return nil, fmt.Errorf("COFF .text section not found")
}

type WitnessComparison struct {
	WitnessID      string   `json:"witness_id"`
	Instruction    string   `json:"instruction"`
	NASMBytes      []byte   `json:"nasm_bytes"`
	NativeBytes    []byte   `json:"native_bytes"`
	Result         string   `json:"result"`
	MismatchLayer  string   `json:"mismatch_layer,omitempty"`
	MismatchFields []string `json:"mismatch_fields,omitempty"`
	Evidence       []string `json:"evidence,omitempty"`
}

func CompareNASMNativeWitness(assembly string) (WitnessComparison, error) {
	native, err := EncodeNativeWitness(assembly)
	if err != nil {
		return WitnessComparison{}, err
	}
	object, err := AssembleNASMWitness(assembly)
	if err != nil {
		return WitnessComparison{}, err
	}
	nasmBytes, err := ExtractCOFFText(object)
	if err != nil {
		return WitnessComparison{}, err
	}
	result := "PASS"
	mismatchLayer := ""
	mismatchFields := []string(nil)
	if string(nasmBytes) != string(native) {
		result = "FAIL"
		mismatchLayer = "byte_encoding"
		mismatchFields = []string{"nasm_bytes", "native_bytes"}
	}
	hash := sha256.Sum256([]byte(strings.TrimSpace(assembly)))
	return WitnessComparison{WitnessID: "nasm-native:" + hex.EncodeToString(hash[:8]), Instruction: assembly, NASMBytes: nasmBytes, NativeBytes: native, Result: result, MismatchLayer: mismatchLayer, MismatchFields: mismatchFields, Evidence: []string{"nasm.exe", "COFF:.text", "x86encode.EncodeProgram"}}, nil
}

// WriteWitnessComparison persists a real comparison without retaining a
// growing evidence matrix in memory. The file is append-only JSONL.
func WriteWitnessComparison(path string, comparison WitnessComparison) error {
	if comparison.WitnessID == "" {
		h := sha256.Sum256([]byte(strings.TrimSpace(comparison.Instruction)))
		comparison.WitnessID = "nasm-native:" + hex.EncodeToString(h[:8])
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	return json.NewEncoder(f).Encode(comparison)
}
