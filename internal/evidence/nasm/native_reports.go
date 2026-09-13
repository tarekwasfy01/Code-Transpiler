package nasm

import (
	"encoding/json"
	"os"
	"strings"
)

// NativeEncodingContract is the machine-readable contract emitted from NASM
// evidence. Native matching is deliberately conservative: without a native
// rule witness the status remains insufficient_evidence.
type NativeEncodingContract struct {
	ID                string              `json:"id"`
	Architecture      string              `json:"architecture"`
	MachineOperation  string              `json:"machine_operation"`
	VariantID         string              `json:"nasm_variant_id"`
	Mnemonic          string              `json:"mnemonic"`
	Operands          []string            `json:"operands"`
	Mode              map[string]string   `json:"mode_constraints"`
	Features          []FeatureConstraint `json:"feature_constraints"`
	ForbiddenFeatures []string            `json:"forbidden_features,omitempty"`
	Prefixes          []PrefixConstraint  `json:"prefix_constraints"`
	OperandRoles      []string            `json:"operand_roles,omitempty"`
	OperandWidths     []string            `json:"operand_widths,omitempty"`
	RegisterClasses   []string            `json:"register_classes,omitempty"`
	MemoryConstraints []string            `json:"memory_constraints,omitempty"`
	REX               string              `json:"rex,omitempty"`
	ModRM             string              `json:"modrm,omitempty"`
	SIB               string              `json:"sib,omitempty"`
	Immediate         string              `json:"immediate,omitempty"`
	Displacement      string              `json:"displacement,omitempty"`
	Relocation        string              `json:"relocation,omitempty"`
	Encoding          InstructionEncoding `json:"encoding"`
	Status            string              `json:"status"`
	Evidence          []Evidence          `json:"evidence"`
}

type ContractMatch struct {
	ContractID string   `json:"contract_id"`
	Status     string   `json:"status"`
	Reasons    []string `json:"reasons,omitempty"`
}

type NASMWitness struct {
	WitnessID string   `json:"witness_id"`
	VariantID string   `json:"nasm_variant_id"`
	Mnemonic  string   `json:"mnemonic"`
	Operands  []string `json:"operand_form"`
	Assembly  string   `json:"assembly"`
	Mode      string   `json:"mode"`
}

// GenerateNASMWitness chooses deterministic architectural operands from the
// normalized operand classes; it never parses source text or uses case IDs.
func GenerateNASMWitness(v Variant) (NASMWitness, bool) {
	regs := map[string]string{"r8": "al", "r16": "ax", "r32": "eax", "r64": "rax", "reg_eax": "eax", "reg_rax": "rax", "rm8": "byte [rbx]", "rm16": "word [rbx]", "rm32": "dword [rbx]", "rm64": "qword [rbx]", "m": "[rbx+8]", "imm8": "8", "imm16": "16", "imm32": "32", "imm64": "64", "rel8": "short target", "rel32": "near target"}
	operands := make([]string, len(v.Operands))
	for i, o := range v.Operands {
		x, ok := regs[strings.ToLower(o)]
		if !ok {
			return NASMWitness{}, false
		}
		operands[i] = x
	}
	asm := strings.ToLower(v.Mnemonic)
	if len(operands) > 0 {
		asm += " " + strings.Join(operands, ", ")
	}
	return NASMWitness{WitnessID: v.ID + ":witness", VariantID: v.ID, Mnemonic: v.Mnemonic, Operands: operands, Assembly: asm, Mode: "64"}, true
}

// MatchNASMVariantToNativeContract performs structural matching. A contract
// with an empty native rule is never promoted to exact; this keeps evidence
// and executable encoder support separate.
func MatchNASMVariantToNativeContract(v Variant, contracts []NativeEncodingContract) []ContractMatch {
	out := make([]ContractMatch, 0)
	for _, c := range contracts {
		reasons := []string{}
		if !strings.EqualFold(c.Mnemonic, v.Mnemonic) {
			continue
		}
		if len(c.Operands) != len(v.Operands) {
			continue
		}
		if c.Architecture != "" && c.Architecture != "x86_64" {
			reasons = append(reasons, "architecture")
		}
		if c.Status == "unsupported" {
			out = append(out, ContractMatch{ContractID: c.ID, Status: "unsupported", Reasons: reasons})
			continue
		}
		if c.Status == "insufficient_evidence" {
			reasons = append(reasons, "native_rule_unavailable")
		}
		status := "conditional"
		if len(reasons) == 0 {
			status = "exact"
		}
		out = append(out, ContractMatch{ContractID: c.ID, Status: status, Reasons: reasons})
	}
	if len(out) == 0 {
		return []ContractMatch{{Status: "no_match", Reasons: []string{"no_structural_candidate"}}}
	}
	return out
}

type nativeCoverage struct {
	NASMVariantsTotal          int `json:"nasm_variants_total"`
	RelevantX64Variants        int `json:"relevant_x64_variants"`
	MatchedNativeVariants      int `json:"matched_native_variants"`
	Confirmed                  int `json:"confirmed"`
	MissingNativeLowering      int `json:"missing_native_lowering"`
	MissingMachineIR           int `json:"missing_machine_ir"`
	MissingEncoder             int `json:"missing_encoder"`
	EncodingMismatches         int `json:"encoding_mismatches"`
	ConstraintMismatches       int `json:"constraint_mismatches"`
	UnsupportedFeatureVariants int `json:"unsupported_feature_variants"`
	Unresolved                 int `json:"unresolved"`
}

func writeNativeReports(out string, contracts []NativeEncodingContract) error {
	f, err := os.Create(out + "/native-encoding-contracts.jsonl")
	if err != nil {
		return err
	}
	defer f.Close()
	comparison, _ := os.Create(out + "/native-encoding-comparison.jsonl")
	defer comparison.Close()
	conflicts, _ := os.Create(out + "/nasm-native-conflicts.jsonl")
	defer conflicts.Close()
	confirmed, _ := os.Create(out + "/nasm-native-confirmed.jsonl")
	defer confirmed.Close()
	witness, _ := os.Create(out + "/nasm-native-witness-results.jsonl")
	defer witness.Close()
	for _, c := range contracts {
		b, _ := json.Marshal(c)
		_, _ = f.Write(append(b, '\n'))
		matches := MatchNASMVariantToNativeContract(Variant{Mnemonic: c.Mnemonic, Operands: c.Operands}, contracts)
		matchKind, contractID := "no_match", ""
		if len(matches) > 0 {
			matchKind, contractID = matches[0].Status, matches[0].ContractID
		}
		r := map[string]any{"nasm_variant_id": c.VariantID, "native_contract_id": contractID, "status": c.Status, "match_kind": matchKind, "mnemonic": c.Mnemonic, "operand_pattern": c.Operands, "architecture": c.Architecture, "mode": c.Mode, "nasm_encoding": c.Encoding, "native_encoding": nil, "conditions": matches[0].Reasons, "reason": "native rule witness unavailable", "evidence_ids": c.Evidence, "nasm_evidence": c.Evidence, "native_evidence": nil}
		b, _ = json.Marshal(r)
		_, _ = comparison.Write(append(b, '\n'))
		b, _ = json.Marshal(map[string]any{"variant_id": c.VariantID, "status": "not_run", "reason": "native encoder witness not supplied"})
		_, _ = witness.Write(append(b, '\n'))
	}
	for _, name := range []string{"nasm-native-conflicts.jsonl", "nasm-native-confirmed.jsonl"} {
		_ = os.WriteFile(out+"/"+name, nil, 0644)
	}
	return writeJSON(out+"/nasm-native-coverage.json", nativeCoverage{NASMVariantsTotal: len(contracts), RelevantX64Variants: len(contracts), Unresolved: len(contracts)})
}
