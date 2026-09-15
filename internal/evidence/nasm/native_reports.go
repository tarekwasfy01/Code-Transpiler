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
		// Operand classes and widths are part of the contract.  A contract
		// with incompatible concrete classes must never be promoted merely
		// because its mnemonic matches.
		compatible := true
		for i := range v.Operands {
			if !operandClassCompatible(v.Operands[i], c.Operands[i]) {
				compatible = false
				break
			}
		}
		if !compatible {
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

func operandClassCompatible(variant, contract string) bool {
	v := strings.ToLower(strings.TrimSpace(variant))
	c := strings.ToLower(strings.TrimSpace(contract))
	if v == c || c == "" || v == "" {
		return true
	}
	// NASM evidence uses r/m aliases and register-width spellings.
	if strings.ReplaceAll(v, " ", "") == strings.ReplaceAll(c, " ", "") {
		return true
	}
	if strings.HasPrefix(v, "r/m") && strings.HasPrefix(c, "r/m") {
		return strings.TrimPrefix(v, "r/m") == strings.TrimPrefix(c, "r/m")
	}
	if strings.HasPrefix(v, "r") && strings.HasPrefix(c, "r") {
		return strings.TrimPrefix(v, "r") == strings.TrimPrefix(c, "r")
	}
	return false
}

type nativeCoverage struct {
	NASMVariantsTotal          int                       `json:"nasm_variants_total"`
	RelevantX64Variants        int                       `json:"relevant_x64_variants"`
	MatchedNativeVariants      int                       `json:"native_rule_candidates"`
	Confirmed                  int                       `json:"confirmed"`
	MissingNativeLowering      int                       `json:"variants_without_native_operation_rule"`
	MissingMachineIR           int                       `json:"missing_machine_ir"`
	MissingEncoder             int                       `json:"missing_encoder"`
	EncodingMismatches         int                       `json:"encoding_mismatches"`
	ConstraintMismatches       int                       `json:"constraint_mismatches"`
	UnsupportedFeatureVariants int                       `json:"unsupported_feature_variants"`
	Unresolved                 int                       `json:"unresolved"`
	Families                   map[string]map[string]int `json:"families,omitempty"`
	FamilyContractsTotal       int                       `json:"family_contracts_total"`
}

type nativeRuleRef struct {
	ID               string `json:"id"`
	SemanticIdentity string `json:"semantic_identity"`
	Relation         string `json:"relation"`
	Confidence       string `json:"confidence"`
	Locations        []struct {
		File      string `json:"file"`
		Symbol    string `json:"symbol"`
		StartLine int    `json:"start_line"`
		SHA256    string `json:"sha256"`
	} `json:"locations"`
}

func writeNativeReports(out string, contracts []NativeEncodingContract) error {
	// Reconcile NASM evidence with the repository's actual encoder evidence.
	// This is deliberately provenance based: only operations documented by the
	// native translation-rules export become executable candidates.
	nativeOps := map[string][]nativeRuleRef{}
	if data, err := os.ReadFile("compiler-evidence/native/translation-rules.jsonl"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			var row nativeRuleRef
			if json.Unmarshal([]byte(line), &row) == nil && strings.HasPrefix(row.SemanticIdentity, "machine.operation:") {
				key := normalizeMachineOp(row.SemanticIdentity)
				nativeOps[key] = append(nativeOps[key], row)
			}
		}
	}
	for i := range contracts {
		if len(nativeOps[normalizeMachineOp(contracts[i].MachineOperation)]) > 0 {
			contracts[i].Status = "native_rule_available"
		}
	}
	// Emit the deduplicated family basis used by the matcher and downstream
	// lowering.  Width/register/addressing variants remain parameters of one
	// family rule instead of becoming separate handlers.
	familiesFile, err := os.Create(out + "/native-encoding-families.jsonl")
	if err != nil {
		return err
	}
	familyMembers := map[string][]string{}
	for _, c := range contracts {
		key := normalizeMachineOp(c.MachineOperation)
		if key == "fixed_width" || key == "unknown" || key == "" {
			key = strings.ToLower(c.Mnemonic)
		}
		familyMembers[key] = append(familyMembers[key], c.ID)
	}
	for family, members := range familyMembers {
		if family == "" {
			continue
		}
		row := map[string]any{"family_id": "x64." + family, "machine_operation": family, "contract_count": len(members), "contract_ids": members, "native_rule_ids": ruleIDs(nativeOps[family]), "parameterized": true, "basis": []string{"operand_width", "operand_class", "register_encoding", "memory_addressing", "immediate", "displacement"}}
		b, _ := json.Marshal(row)
		_, _ = familiesFile.Write(append(b, '\n'))
	}
	_ = familiesFile.Close()
	// Materialize one contract per executable family, retaining the concrete
	// variant contracts as evidence and parameters.
	fc, err := os.Create(out + "/native-family-contracts.jsonl")
	if err != nil {
		return err
	}
	for family, members := range familyMembers {
		if family == "" {
			continue
		}
		row := map[string]any{"id": "native.family." + family, "architecture": "x86_64", "machine_operation": family, "parameterized": true, "variant_contract_ids": members, "native_rules": nativeOps[family], "operand_parameters": []string{"class", "width", "direction"}, "encoding_parameters": []string{"rex", "modrm", "sib", "immediate", "displacement"}, "provenance": "compiler-evidence/native/translation-rules.jsonl", "status": "conditional_until_witnessed"}
		b, _ := json.Marshal(row)
		_, _ = fc.Write(append(b, '\n'))
	}
	_ = fc.Close()
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
		rules := nativeOps[normalizeMachineOp(c.MachineOperation)]
		matchKind := "no_match"
		if len(rules) > 0 {
			matchKind = "conditional"
		}
		r := map[string]any{"nasm_variant_id": c.VariantID, "native_rule_ids": ruleIDs(rules), "status": c.Status, "match_kind": matchKind, "mnemonic": c.Mnemonic, "operand_pattern": c.Operands, "architecture": c.Architecture, "mode": c.Mode, "nasm_encoding": c.Encoding, "native_encoding": nil, "conditions": []string{"operand_form_requires_native_witness"}, "reason": map[bool]string{true: "native encoder rule candidate; byte witness required", false: "no native machine-operation rule"}[len(rules) > 0], "nasm_evidence": c.Evidence, "native_rules": rules}
		b, _ = json.Marshal(r)
		_, _ = comparison.Write(append(b, '\n'))
		if len(rules) > 0 {
			if w, ok := GenerateNASMWitness(Variant{ID: c.VariantID, Mnemonic: c.Mnemonic, Operands: c.Operands}); ok {
				row := map[string]any{"witness_id": w.WitnessID, "nasm_variant_id": c.VariantID, "native_rule_ids": ruleIDs(rules), "assembly": w.Assembly, "mode": w.Mode, "status": "generated_not_run", "reason": "requires operand contract and native encoder support"}
				b, _ = json.Marshal(row)
				_, _ = witness.Write(append(b, '\n'))
			}
		}
	}
	for _, name := range []string{"nasm-native-conflicts.jsonl", "nasm-native-confirmed.jsonl"} {
		_ = os.WriteFile(out+"/"+name, nil, 0644)
	}
	matched := 0
	families := map[string]map[string]int{}
	for _, c := range contracts {
		m := strings.ToUpper(c.Mnemonic)
		if families[m] == nil {
			families[m] = map[string]int{}
		}
		families[m]["total"]++
		if c.Status == "native_rule_available" {
			families[m]["matched"]++
		} else {
			families[m]["missing"]++
		}
	}
	for _, c := range contracts {
		if c.Status == "native_rule_available" {
			matched++
		}
	}
	return writeJSON(out+"/nasm-native-coverage.json", nativeCoverage{NASMVariantsTotal: len(contracts), RelevantX64Variants: len(contracts), MatchedNativeVariants: matched, Confirmed: 0, MissingNativeLowering: len(contracts) - matched, Unresolved: len(contracts) - matched, Families: families, FamilyContractsTotal: len(familyMembers)})
}

func ruleIDs(rows []nativeRuleRef) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.ID)
	}
	return out
}

func normalizeMachineOp(op string) string {
	op = strings.ToLower(strings.TrimSpace(op))
	if k := strings.LastIndexByte(op, ':'); k >= 0 {
		op = op[k+1:]
	}
	aliases := map[string]string{
		"integer.add.fixed_width": "add", "integer.subtract.fixed_width": "sub", "integer.compare.fixed_width": "cmp",
		"integer.multiply.fixed_width": "multiply", "integer.divide.fixed_width": "divide",
		"bitwise.and": "and", "bitwise.or": "or", "bitwise.xor": "xor", "bitwise.not": "not", "bitwise.test": "test",
		"bitwise.shift.shl": "shl", "bitwise.shift.sal": "shl", "bitwise.shift.shr": "shr", "bitwise.shift.sar": "sar",
		"data.move": "mov", "effective_address": "lea", "control.call": "call", "control.ret": "ret", "control.jmp": "jmp", "control.conditional_branch": "conditional_branch",
		"stack.push": "push", "stack.pop": "pop", "add_sp": "add", "sub_sp": "sub",
	}
	if v, ok := aliases[op]; ok {
		return v
	}
	if strings.HasPrefix(op, "j") && op != "jmp" {
		return "conditional_branch"
	}
	if op == "mul" || op == "imul" {
		return "multiply"
	}
	if op == "div" || op == "idiv" {
		return "divide"
	}
	if op == "sal" {
		return "shl"
	}
	return op
}
