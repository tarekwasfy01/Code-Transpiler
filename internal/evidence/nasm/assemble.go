// Copyright (c) 2026 Tarek Wasfy
package nasm

import (
	"bufio"
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
	nasm := os.Getenv("NASM_PATH")
	if nasm == "" {
		nasm = "nasm"
	}
	if _, err := exec.LookPath(nasm); err != nil {
		for _, candidate := range []string{`C:\Users\tarek\AppData\Local\bin\NASM\nasm.exe`, `C:\Program Files\NASM\nasm.exe`} {
			if _, e := os.Stat(candidate); e == nil {
				nasm = candidate
				break
			}
		}
	}
	cmd := exec.Command(nasm, "-f", "win64", "-o", objPath, asmPath)
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("nasm witness: %w: %s", err, output)
	}
	return os.ReadFile(objPath)
}

type WitnessRunSummary struct {
	Candidates  int `json:"candidates"`
	Generated   int `json:"generated"`
	Pass        int `json:"pass_exact"`
	Failed      int `json:"fail"`
	Unsupported int `json:"unsupported"`
	Skipped     int `json:"skipped"`
}

// RunNativeWitnessCorpus executes deterministically generatable native-rule
// candidates through both NASM and the repository encoder. Candidates that
// cannot be represented by the current encoder are recorded as unsupported,
// never counted as confirmed.
func RunNativeWitnessCorpus(contractPath, resultPath string, limit int) (WitnessRunSummary, error) {
	var summary WitnessRunSummary
	in, err := os.Open(contractPath)
	if err != nil {
		return summary, err
	}
	defer in.Close()
	out, err := os.Create(resultPath)
	if err != nil {
		return summary, err
	}
	defer out.Close()
	confirmed, err := os.Create(filepath.Join(filepath.Dir(resultPath), "nasm-native-confirmed.jsonl"))
	if err != nil {
		return summary, err
	}
	defer confirmed.Close()
	conflicts, err := os.Create(filepath.Join(filepath.Dir(resultPath), "nasm-native-conflicts.jsonl"))
	if err != nil {
		return summary, err
	}
	defer conflicts.Close()
	enc := json.NewEncoder(out)
	scan := bufio.NewScanner(in)
	scan.Buffer(make([]byte, 64*1024), 32*1024*1024)
	for scan.Scan() {
		var c NativeEncodingContract
		if err := json.Unmarshal(scan.Bytes(), &c); err != nil {
			return summary, err
		}
		if c.Status != "native_rule_available" {
			summary.Skipped++
			continue
		}
		summary.Candidates++
		if limit > 0 && summary.Generated >= limit {
			summary.Skipped++
			continue
		}
		w, ok := GenerateNASMWitness(Variant{ID: c.VariantID, Mnemonic: c.Mnemonic, Operands: c.Operands})
		if !ok {
			summary.Skipped++
			continue
		}
		summary.Generated++
		source := "bits 64\nsection .text\n" + w.Assembly + "\n"
		if strings.Contains(source, "target") {
			source += "target:\nret\n"
		}
		comparison, err := CompareNASMNativeWitness(source)
		row := map[string]any{"witness_id": w.WitnessID, "nasm_variant_id": c.VariantID, "native_contract_id": c.ID, "native_rule_candidate": true, "assembly": source, "operand_form": w.Operands, "mode": "64", "nasm_bytes": comparison.NASMBytes, "native_bytes": comparison.NativeBytes, "comparison_kind": "pass_exact", "mismatch_fields": comparison.MismatchFields, "result": "pass_exact", "evidence": c.Evidence}
		if err != nil {
			row["result"] = "unsupported"
			row["error"] = err.Error()
			summary.Unsupported++
		} else if comparison.Result != "PASS" {
			row["result"] = "fail"
			row["comparison_kind"] = "fail"
			row["mismatch_layer"] = comparison.MismatchLayer
			summary.Failed++
		} else {
			summary.Pass++
		}
		if err := enc.Encode(row); err != nil {
			return summary, err
		}
		if row["result"] == "pass_exact" {
			if err := json.NewEncoder(confirmed).Encode(row); err != nil {
				return summary, err
			}
		}
		if row["result"] == "fail" {
			if err := json.NewEncoder(conflicts).Encode(row); err != nil {
				return summary, err
			}
		}
	}
	if err := scan.Err(); err != nil {
		return summary, err
	}
	coveragePath := filepath.Join(filepath.Dir(resultPath), "nasm-native-coverage.json")
	if data, e := os.ReadFile(coveragePath); e == nil {
		var coverage map[string]any
		if json.Unmarshal(data, &coverage) == nil {
			coverage["witness_candidates"] = summary.Candidates
			coverage["witnesses_generated"] = summary.Generated
			coverage["witnesses_confirmed_exact"] = summary.Pass
			coverage["witness_conflicts"] = summary.Failed
			coverage["witness_unsupported"] = summary.Unsupported
			b, _ := json.MarshalIndent(coverage, "", "  ")
			if e := os.WriteFile(coveragePath, append(b, '\n'), 0644); e != nil {
				return summary, e
			}
		}
	}
	return summary, nil
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
