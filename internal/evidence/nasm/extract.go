package nasm

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const Repository = "https://github.com/netwide-assembler/nasm"

var Families = map[string]bool{
	"ADD": true, "SUB": true, "MUL": true, "IMUL": true, "DIV": true, "IDIV": true, "CMP": true, "TEST": true,
	"MOV": true, "LEA": true, "AND": true, "OR": true, "XOR": true, "NOT": true, "SHL": true, "SHR": true,
	"SAR": true, "JMP": true, "CALL": true, "RET": true, "PUSH": true, "POP": true,
}
var mnemonicAliases = map[string]bool{"SAL": true}
var condJcc = map[string]bool{"JO": true, "JNO": true, "JB": true, "JC": true, "JNAE": true, "JAE": true, "JNB": true, "JNC": true, "JE": true, "JZ": true, "JNE": true, "JNZ": true, "JBE": true, "JNA": true, "JA": true, "JNBE": true, "JS": true, "JNS": true, "JP": true, "JPE": true, "JNP": true, "JPO": true, "JL": true, "JNGE": true, "JGE": true, "JNL": true, "JLE": true, "JNG": true, "JG": true, "JNLE": true}

func selectedMnemonic(m string) bool {
	m = strings.ToUpper(m)
	return Families[m] || mnemonicAliases[m] || condJcc[m] || m == "JCC"
}

type Evidence struct {
	Repository       string `json:"repository"`
	Commit           string `json:"commit"`
	Branch           string `json:"branch"`
	SourceFile       string `json:"source_file"`
	StartLine        int    `json:"start_line"`
	EndLine          int    `json:"end_line"`
	Definition       string `json:"definition,omitempty"`
	ExtractionMethod string `json:"extraction_method"`
	Confidence       string `json:"confidence"`
}

// EvidenceLocation is the versioned source provenance attached to every extracted fact.
type EvidenceLocation = Evidence
type SourceSpan struct {
	StartLine int `json:"start_line"`
	EndLine   int `json:"end_line"`
}

func (e Evidence) Span() SourceSpan { return SourceSpan{StartLine: e.StartLine, EndLine: e.EndLine} }

// The exported evidence model deliberately separates NASM table facts, machine
// effects, and encoding rules. Unknown fields remain explicit instead of being
// guessed from a mnemonic spelling.
type CompilerEvidence struct {
	Repository string            `json:"repository"`
	Commit     string            `json:"commit"`
	Branch     string            `json:"branch"`
	Timestamp  string            `json:"extraction_timestamp_utc"`
	SourceHash map[string]string `json:"source_hashes_sha256"`
}

type SourceUnitEvidence struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int    `json:"bytes"`
}

type OperandConstraint struct {
	Index                 int        `json:"index"`
	RawClass              string     `json:"raw_class"`
	Kind                  string     `json:"kind"`
	WidthBits             int        `json:"width_bits"`
	SourceDestinationRole string     `json:"source_destination_role"`
	RegisterConstraint    string     `json:"register_constraint"`
	MemoryConstraint      bool       `json:"memory_constraint"`
	ImmediateConstraint   bool       `json:"immediate_constraint"`
	AddressingConstraint  string     `json:"addressing_constraint"`
	Confidence            string     `json:"confidence"`
	Evidence              []Evidence `json:"evidence,omitempty"`
}

type ModeConstraint struct {
	Mode     string     `json:"mode"`
	Validity string     `json:"validity"`
	Evidence []Evidence `json:"evidence,omitempty"`
}

type FeatureConstraint struct {
	FeatureID   string     `json:"feature_id"`
	Kind        string     `json:"kind"`
	Requirement string     `json:"requirement"`
	RawFlag     string     `json:"raw_flag"`
	Description string     `json:"description,omitempty"`
	Confidence  string     `json:"confidence"`
	Evidence    []Evidence `json:"evidence,omitempty"`
}

type FeatureIdentity struct {
	ID          string     `json:"feature_id"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	Description string     `json:"description"`
	Evidence    []Evidence `json:"evidence"`
}

type FlagDefinition struct {
	ID          string
	Group       string
	Description string
	Evidence    Evidence
}

type PrefixConstraint struct {
	Token       string     `json:"token"`
	Class       string     `json:"class"`
	Requirement string     `json:"requirement"`
	Effect      string     `json:"effect"`
	Confidence  string     `json:"confidence"`
	Evidence    []Evidence `json:"evidence,omitempty"`
}

type InstructionSemantic struct {
	CanonicalSemanticRelation string     `json:"canonical_semantic_relation"`
	MachineOperation          string     `json:"machine_operation"`
	Reads                     string     `json:"reads"`
	Writes                    string     `json:"writes"`
	FlagsRead                 string     `json:"flags_read"`
	FlagsWritten              string     `json:"flags_written"`
	FlagsUndefined            string     `json:"flags_undefined"`
	Confidence                string     `json:"confidence"`
	EvidenceBasis             string     `json:"evidence_basis"`
	Evidence                  []Evidence `json:"evidence,omitempty"`
}

type CompilerEvidenceIndex struct {
	CompilerEvidence
	SourceUnits []SourceUnitEvidence `json:"source_units"`
	Expansion   ExpansionEvidence    `json:"expansion"`
}

type ExpansionEvidence struct {
	Tool               string `json:"tool"`
	Input              string `json:"input"`
	ExpandedFileSHA256 string `json:"expanded_file_sha256"`
}

type TranslationRule struct {
	From       string     `json:"from"`
	Relation   string     `json:"relation"`
	To         string     `json:"to"`
	Confidence string     `json:"confidence"`
	Evidence   []Evidence `json:"evidence,omitempty"`
}

type SemanticEquivalence struct {
	LeftVariantID         string     `json:"left_variant_id"`
	RightVariantID        string     `json:"right_variant_id"`
	LeftMnemonic          string     `json:"left_mnemonic"`
	RightMnemonic         string     `json:"right_mnemonic"`
	Relation              string     `json:"relation"`
	Confidence            string     `json:"confidence"`
	SemanticAliasAsserted bool       `json:"semantic_alias_asserted"`
	Evidence              []Evidence `json:"evidence,omitempty"`
}

type SemanticDifference struct {
	VariantID        string   `json:"variant_id"`
	Mnemonic         string   `json:"mnemonic"`
	Operands         []string `json:"operands"`
	Dimensions       []string `json:"dimensions"`
	Status           string   `json:"comparison"`
	Confidence       string   `json:"confidence"`
	Reason           string   `json:"reason"`
	NativeSourceFile string   `json:"native_source_file"`
}

type InstructionEvidence struct {
	Architecture string     `json:"architecture"`
	Mnemonic     string     `json:"mnemonic"`
	VariantID    string     `json:"variant_id"`
	OperandCount int        `json:"operand_count"`
	Operands     []string   `json:"operands"`
	SourceFlags  []string   `json:"source_flags"`
	Pseudo       bool       `json:"pseudo"`
	Evidence     []Evidence `json:"evidence"`
}

type Record struct {
	Mnemonic      string     `json:"mnemonic"`
	Operands      []string   `json:"operands"`
	Encoding      string     `json:"encoding"`
	Flags         []string   `json:"flags"`
	Pseudo        bool       `json:"pseudo"`
	Evidence      []Evidence `json:"evidence"`
	GeneratedLine int        `json:"generated_line"`
}
type Encoding struct {
	Raw               string           `json:"raw_bytecode"`
	Slot              string           `json:"operand_slots"`
	Tokens            []string         `json:"tokens"`
	OpcodeMap         string           `json:"opcode_map"`
	OpcodeBytes       []string         `json:"opcode_bytes,omitempty"`
	OpcodeExpressions []string         `json:"opcode_expressions,omitempty"`
	PrefixTokens      []string         `json:"prefix_tokens,omitempty"`
	ModRM             string           `json:"modrm"`
	SIB               string           `json:"sib"`
	Immediate         string           `json:"immediate"`
	ImmediateWidth    string           `json:"immediate_width"`
	Displacement      string           `json:"displacement"`
	DisplacementWidth string           `json:"displacement_width"`
	UnknownTokens     []string         `json:"unknown_tokens,omitempty"`
	SemanticMarkers   []EncodingMarker `json:"semantic_markers,omitempty"`
	Evidence          []Evidence       `json:"evidence,omitempty"`
}
type InstructionEncoding = Encoding
type EncodingMarker struct {
	Token      string `json:"token"`
	Meaning    string `json:"meaning"`
	SourceFile string `json:"source_file"`
	SourceLine int    `json:"source_line"`
	Confidence string `json:"confidence"`
}
type InstructionVariant struct {
	ID                       string               `json:"variant_id"`
	Mnemonic                 string               `json:"mnemonic"`
	OperandCount             int                  `json:"operand_count"`
	Operands                 []string             `json:"operands"`
	OperandConstraints       []OperandConstraint  `json:"operand_constraints"`
	SourceFlags              []string             `json:"source_flags"`
	Modes                    map[string]string    `json:"mode_validity"`
	ModeConstraints          []ModeConstraint     `json:"mode_constraints"`
	FeatureConstraints       []string             `json:"feature_constraints"`
	FeatureDetails           []FeatureConstraint  `json:"feature_constraint_details"`
	CPUGenerationConstraints []string             `json:"cpu_generation_constraints"`
	PrefixConstraints        []string             `json:"prefix_constraints"`
	PrefixDetails            []PrefixConstraint   `json:"prefix_constraint_details"`
	EncodingID               string               `json:"encoding_id"`
	Encoding                 *InstructionEncoding `json:"encoding,omitempty"`
	MachineSemantics         InstructionSemantic  `json:"machine_instruction_semantic"`
	Evidence                 []Evidence           `json:"evidence"`
	Pseudo                   bool                 `json:"pseudo"`
}

// Variant remains the concise public name used by existing lookup callers.
type Variant = InstructionVariant
type Stats struct {
	SourceRecordsScanned      int `json:"source_records_scanned"`
	Records                   int `json:"records_analyzed"`
	Variants                  int `json:"instruction_variants"`
	Encodings                 int `json:"encoding_variants"`
	Mnemonics                 int `json:"instruction_mnemonics"`
	Registers                 int `json:"register_identities"`
	Features                  int `json:"feature_identities"`
	CPUGenerations            int `json:"cpu_generation_identities"`
	Aliases                   int `json:"aliases"`
	Pseudo                    int `json:"pseudo_instructions"`
	DirectFacts               int `json:"direct_evidence_facts"`
	DerivedFacts              int `json:"derived_evidence_facts"`
	Unresolved                int `json:"unresolved_records"`
	UnresolvedEncodingRecords int `json:"unresolved_encoding_records"`
	NativeMatches             int `json:"native_backend_matches"`
	NativeConflicts           int `json:"native_backend_conflicts"`
}

type Config struct{ SourceRoot, ExpandedFile, OutputDir, Commit, Branch, NativeBackendFile string }

// LookupVariants answers a conservative mnemonic/operand-width query. A query
// register class rN matches an explicit regN or a register-or-memory rmN form;
// callers receive all candidates rather than an invented unique selection.
func LookupVariants(dir, mnemonic, mode string, operands []string) ([]Variant, error) {
	encodingByID, err := loadVariantEncodings(dir)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, "nasm-instruction-variants.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Variant
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 2<<20)
	mnemonic = strings.ToUpper(mnemonic)
	for s.Scan() {
		var v Variant
		if err := json.Unmarshal(s.Bytes(), &v); err != nil {
			return nil, err
		}
		if v.Mnemonic != mnemonic || len(v.Operands) != len(operands) {
			continue
		}
		match := true
		constraints := v.OperandConstraints
		for i, q := range operands {
			if !operandQueryMatches(q, v.Operands[i], constraints[i]) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		if mode != "" {
			valid := v.Modes[mode]
			if valid == "invalid" || valid == "not_hardware_instruction" {
				continue
			}
		}
		if enc, ok := encodingByID[v.EncodingID]; ok {
			v.Encoding = &enc
		}
		out = append(out, v)
	}
	return out, s.Err()
}

func loadVariantEncodings(dir string) (map[string]InstructionEncoding, error) {
	f, err := os.Open(filepath.Join(dir, "nasm-encodings.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	type row struct {
		EncodingID string              `json:"encoding_id"`
		Encoding   InstructionEncoding `json:"encoding"`
	}
	result := map[string]InstructionEncoding{}
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 2<<20)
	for s.Scan() {
		var r row
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			return nil, err
		}
		result[r.EncodingID] = r.Encoding
	}
	return result, s.Err()
}

// LookupEncoding performs a reverse, non-unique opcode-byte lookup.
func LookupEncoding(dir string, opcodeBytes []string) ([]Variant, error) {
	need := strings.ToLower(strings.Join(opcodeBytes, " "))
	ef, err := os.Open(filepath.Join(dir, "nasm-encodings.jsonl"))
	if err != nil {
		return nil, err
	}
	defer ef.Close()
	type row struct {
		VariantID string   `json:"variant_id"`
		Encoding  Encoding `json:"encoding"`
	}
	ids := map[string]bool{}
	s := bufio.NewScanner(ef)
	s.Buffer(make([]byte, 4096), 2<<20)
	for s.Scan() {
		var r row
		if err := json.Unmarshal(s.Bytes(), &r); err != nil {
			return nil, err
		}
		have := strings.ToLower(strings.Join(r.Encoding.OpcodeBytes, " "))
		if need != "" && strings.Contains(have, need) {
			ids[r.VariantID] = true
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	f, err := os.Open(filepath.Join(dir, "nasm-instruction-variants.jsonl"))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := []Variant{}
	s = bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 2<<20)
	for s.Scan() {
		var v Variant
		if err := json.Unmarshal(s.Bytes(), &v); err != nil {
			return nil, err
		}
		if ids[v.ID] {
			out = append(out, v)
		}
	}
	return out, s.Err()
}
func operandQueryMatches(q, form string, c OperandConstraint) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	f := strings.ToLower(strings.TrimSpace(form))
	if q == f {
		return true
	}
	width := 0
	if digits := regexp.MustCompile(`\d+$`).FindString(q); digits != "" {
		_, _ = fmt.Sscanf(digits, "%d", &width)
	}
	kind, gotWidth := c.Kind, c.WidthBits
	switch {
	case strings.HasPrefix(q, "imm"):
		return kind == "immediate" && (width == 0 || gotWidth == width)
	case strings.HasPrefix(q, "rm"):
		return (kind == "register_or_memory" || kind == "register") && (width == 0 || gotWidth == width)
	case strings.HasPrefix(q, "r"):
		return (kind == "register" || kind == "register_or_memory") && (width == 0 || gotWidth == width)
	case strings.HasPrefix(q, "m"):
		return (kind == "register_or_memory" || strings.HasPrefix(f, "mem")) && (width == 0 || gotWidth == width)
	case strings.HasPrefix(q, "rel"):
		return strings.Contains(f, "rel") && (width == 0 || gotWidth == width)
	}
	return false
}

func Extract(c Config) (Stats, error) {
	var st Stats
	if c.SourceRoot == "" || c.ExpandedFile == "" || c.OutputDir == "" {
		return st, fmt.Errorf("source root, expanded file and output dir are required")
	}
	if c.Commit == "" {
		c.Commit = "unknown"
	}
	if c.Branch == "" {
		c.Branch = "unknown"
	}
	if err := os.MkdirAll(c.OutputDir, 0755); err != nil {
		return st, err
	}
	index, hashes, err := sourceIndex(c)
	if err != nil {
		return st, err
	}
	if err = writeJSON(filepath.Join(c.OutputDir, "nasm-source-index.json"), index); err != nil {
		return st, err
	}
	insns := filepath.Join(c.SourceRoot, "x86", "insns.dat")
	origins, err := loadOrigins(insns)
	if err != nil {
		return st, err
	}
	flagDefinitions, err := loadFlagDefinitions(filepath.Join(c.SourceRoot, "x86", "iflags.ph"), c)
	if err != nil {
		return st, err
	}
	files := map[string]*os.File{}
	for _, name := range []string{"nasm-instructions.jsonl", "nasm-instruction-variants.jsonl", "nasm-encodings.jsonl", "nasm-registers.jsonl", "nasm-features.jsonl", "nasm-aliases.jsonl", "nasm-evidence-graph.jsonl", "nasm-native-backend-comparison.jsonl"} {
		f, e := os.Create(filepath.Join(c.OutputDir, name))
		if e != nil {
			return st, e
		}
		files[name] = f
	}
	defer func() {
		for _, f := range files {
			_ = f.Close()
		}
	}()
	mnemonics := map[string]bool{}
	encodings := map[string]bool{}
	features := map[string]FeatureIdentity{}
	cpuGenerations := map[string]FeatureIdentity{}
	aliasRows := map[string]bool{}
	encodingEquivs := map[string][]map[string]any{}
	contracts := []NativeEncodingContract{}
	if err = scanRecords(c.ExpandedFile, func(r Record) error {
		st.SourceRecordsScanned++
		m := strings.ToUpper(r.Mnemonic)
		if !selectedMnemonic(m) {
			return nil
		}
		locs := origins[m]
		evidence := []Evidence{}
		if len(locs) == 0 {
			evidence = []Evidence{{Repository: Repository, Commit: c.Commit, Branch: c.Branch, SourceFile: "x86/insns.dat", StartLine: 0, EndLine: 0, ExtractionMethod: "expanded_record_mnemonic_join_unresolved_source_span", Confidence: "candidate"}}
		} else {
			for _, loc := range locs {
				confidence := "derived"
				if loc.Definition == m {
					confidence = "direct"
				}
				evidence = append(evidence, Evidence{Repository: Repository, Commit: c.Commit, Branch: c.Branch, SourceFile: "x86/insns.dat", StartLine: loc.Start, EndLine: loc.End, Definition: loc.Definition, ExtractionMethod: "preinsns_macro_expansion_with_source_line_join", Confidence: confidence})
			}
		}
		r.Evidence = evidence
		id := fmt.Sprintf("nasm:%s:%s:%d", c.Commit, m, r.GeneratedLine)
		enc := parseEncoding(r.Encoding)
		enc.Evidence = evidence
		if len(enc.UnknownTokens) > 0 {
			st.UnresolvedEncodingRecords++
		}
		eid := id + ":encoding"
		variant := Variant{
			ID: id, Mnemonic: m, OperandCount: len(r.Operands), Operands: r.Operands,
			OperandConstraints: operandConstraintsWithEvidence(r.Operands, evidence), SourceFlags: r.Flags,
			Modes: modeValidity(r.Flags), ModeConstraints: modeConstraintDetails(r.Flags, evidence),
			FeatureConstraints: featureFlags(r.Flags), FeatureDetails: featureConstraintDetails(r.Flags, evidence, flagDefinitions),
			CPUGenerationConstraints: cpuGenerationFlags(r.Flags), PrefixConstraints: enc.PrefixTokens,
			PrefixDetails: prefixConstraintDetails(enc.PrefixTokens, evidence), EncodingID: eid,
			Evidence: evidence, Pseudo: r.Pseudo, MachineSemantics: machineSemantics(m, r), Encoding: &enc,
		}
		contracts = append(contracts, NativeEncodingContract{ID: eid, Architecture: "x86_64", MachineOperation: variant.MachineSemantics.MachineOperation, VariantID: id, Mnemonic: m, Operands: r.Operands, Mode: variant.Modes, Features: variant.FeatureDetails, Prefixes: variant.PrefixDetails, ModRM: enc.ModRM, SIB: enc.SIB, Immediate: enc.Immediate, Displacement: enc.Displacement, Encoding: enc, Status: "insufficient_evidence", Evidence: evidence})
		for _, x := range variant.FeatureDetails {
			if x.Kind == "isa_feature" {
				if _, ok := features[x.FeatureID]; !ok && len(evidence) > 0 {
					features[x.FeatureID] = FeatureIdentity{ID: x.FeatureID, Name: x.FeatureID, Kind: x.Kind, Description: x.Description, Evidence: x.Evidence}
				}
			} else if x.Kind == "cpu_generation" {
				if _, ok := cpuGenerations[x.FeatureID]; !ok {
					cpuGenerations[x.FeatureID] = FeatureIdentity{ID: x.FeatureID, Name: x.FeatureID, Kind: x.Kind, Description: x.Description, Evidence: x.Evidence}
				}
			}
		}
		if err := writeLine(files["nasm-instructions.jsonl"], InstructionEvidence{Architecture: "x86", Mnemonic: m, VariantID: id, OperandCount: len(r.Operands), Operands: r.Operands, SourceFlags: r.Flags, Pseudo: r.Pseudo, Evidence: evidence}); err != nil {
			return err
		}
		if err := writeLine(files["nasm-instruction-variants.jsonl"], variant); err != nil {
			return err
		}
		if err := writeLine(files["nasm-encodings.jsonl"], map[string]any{"encoding_id": eid, "variant_id": id, "mnemonic": m, "encoding": enc, "mode_constraints": variant.Modes, "feature_constraints": variant.FeatureConstraints, "cpu_generation_constraints": variant.CPUGenerationConstraints, "evidence": evidence}); err != nil {
			return err
		}
		_ = writeLine(files["nasm-evidence-graph.jsonl"], TranslationRule{From: id, Relation: "encodes_as", To: eid, Confidence: "direct", Evidence: evidence})
		for _, p := range enc.PrefixTokens {
			_ = writeLine(files["nasm-evidence-graph.jsonl"], map[string]any{"from": eid, "relation": "uses_prefix", "to": p, "confidence": "direct", "evidence": evidence})
		}
		if enc.ModRM != "unknown" {
			_ = writeLine(files["nasm-evidence-graph.jsonl"], map[string]any{"from": eid, "relation": "uses_modrm", "to": enc.ModRM, "confidence": "direct", "evidence": evidence})
		}
		if enc.SIB != "unknown" {
			_ = writeLine(files["nasm-evidence-graph.jsonl"], map[string]any{"from": eid, "relation": "uses_sib", "to": enc.SIB, "confidence": "direct", "evidence": evidence})
		}
		mnemonics[m] = true
		encodings[eid] = true
		st.Records++
		st.Variants++
		st.Encodings++
		if r.Pseudo {
			st.Pseudo++
		}
		for _, ev := range evidence {
			if ev.Confidence == "direct" {
				st.DirectFacts++
			} else if ev.Confidence == "derived" {
				st.DerivedFacts++
			} else {
				st.Unresolved++
			}
		}
		for _, ev := range evidence {
			_ = writeLine(files["nasm-evidence-graph.jsonl"], map[string]any{"from": id, "relation": "defined_by", "to": fmt.Sprintf("%s:%d-%d", ev.SourceFile, ev.StartLine, ev.EndLine), "confidence": ev.Confidence, "evidence": ev})
		}
		key := strings.Join(r.Operands, ",") + "|" + r.Encoding + "|" + strings.Join(r.Flags, ",")
		encodingEquivs[key] = append(encodingEquivs[key], map[string]any{"variant_id": id, "mnemonic": m, "evidence": evidence})
		if c.NativeBackendFile != "" {
			_ = writeLine(files["nasm-native-backend-comparison.jsonl"], SemanticDifference{VariantID: id, Mnemonic: m, Operands: r.Operands, Dimensions: []string{"architecture", "mnemonic", "operand_pattern", "width", "mode", "addressing_form", "normalized_encoding"}, Status: "not_comparable", Confidence: "candidate", Reason: "no normalized native encoding form was found; source presence/absence cannot establish support or conflict", NativeSourceFile: c.NativeBackendFile})
		}
		return nil
	}); err != nil {
		return st, err
	}
	if err = extractMacroAliasCandidates(insns, files["nasm-aliases.jsonl"], c); err != nil {
		return st, err
	}
	for _, group := range encodingEquivs {
		if len(group) < 2 {
			continue
		}
		for i := 0; i < len(group); i++ {
			for j := i + 1; j < len(group); j++ {
				a, b := group[i], group[j]
				if a["mnemonic"] == b["mnemonic"] {
					continue
				}
				aliasRows[fmt.Sprint(a["mnemonic"], "|", b["mnemonic"])] = true
				_ = writeLine(files["nasm-aliases.jsonl"], SemanticEquivalence{LeftVariantID: fmt.Sprint(a["variant_id"]), RightVariantID: fmt.Sprint(b["variant_id"]), LeftMnemonic: fmt.Sprint(a["mnemonic"]), RightMnemonic: fmt.Sprint(b["mnemonic"]), Relation: "encoding_equivalent", Confidence: "derived", SemanticAliasAsserted: false, Evidence: append(asEvidence(a["evidence"]), asEvidence(b["evidence"])...)})
			}
		}
	}
	if err = extractRegisters(filepath.Join(c.SourceRoot, "x86", "regs.dat"), files["nasm-registers.jsonl"], c, &st); err != nil {
		return st, err
	}
	for _, identity := range features {
		_ = writeLine(files["nasm-features.jsonl"], identity)
	}
	for _, identity := range cpuGenerations {
		_ = writeLine(files["nasm-features.jsonl"], identity)
	}
	if err = writePipelineGraph(c, files["nasm-evidence-graph.jsonl"]); err != nil {
		return st, err
	}
	if err = writeNativeReports(c.OutputDir, contracts); err != nil {
		return st, err
	}
	// Report encoding-equivalent aliases only as candidates; no semantic alias is asserted from spelling alone.
	_ = aliasRows
	st.Mnemonics = len(mnemonics)
	st.Features = len(features)
	st.CPUGenerations = len(cpuGenerations)
	st.Aliases = len(aliasRows)
	for _, f := range files {
		if err = f.Sync(); err != nil {
			return st, err
		}
	}
	st.Registers = countJSONL(filepath.Join(c.OutputDir, "nasm-registers.jsonl"))
	timestamp := time.Now().UTC().Format(time.RFC3339)
	summary := map[string]any{"schema_version": "1.1", "compiler_evidence": CompilerEvidence{Repository: Repository, Commit: c.Commit, Branch: c.Branch, Timestamp: timestamp, SourceHash: hashes}, "vertical_prototype_families": sortedFamilyNames(), "stats": st, "scope_note": "NASM source is evidence only; no NASM code is vendored and no backend rule was modified."}
	if err = writeJSON(filepath.Join(c.OutputDir, "nasm-extraction-summary.json"), summary); err != nil {
		return st, err
	}
	if err = writeReport(filepath.Join(c.OutputDir, "nasm-extraction-report.md"), st, c); err != nil {
		return st, err
	}
	return st, nil
}

type origin struct {
	Start, End int
	Definition string
}

func loadOrigins(path string) (map[string][]origin, error) {
	out := map[string][]origin{}
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 2<<20)
	line := 0
	for s.Scan() {
		line++
		t := strings.TrimSpace(s.Text())
		if t == "" || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#") {
			continue
		}
		fields := strings.Fields(t)
		if len(fields) == 0 {
			continue
		}
		if strings.HasPrefix(fields[0], "$") {
			for _, field := range fields[1:] {
				for _, part := range strings.Split(field, ",") {
					m := strings.Trim(part, "!,%?#")
					if selectedMnemonic(m) {
						out[strings.ToUpper(m)] = append(out[strings.ToUpper(m)], origin{line, line, fields[0]})
					}
				}
			}
		} else if selectedMnemonic(fields[0]) {
			out[strings.ToUpper(fields[0])] = append(out[strings.ToUpper(fields[0])], origin{line, line, fields[0]})
		}
	}
	return out, s.Err()
}

func extractMacroAliasCandidates(path string, w io.Writer, c Config) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	line := 0
	for s.Scan() {
		line++
		t := strings.TrimSpace(s.Text())
		fields := strings.Fields(t)
		if len(fields) < 2 || !strings.HasPrefix(fields[0], "$") {
			continue
		}
		for _, field := range fields[1:] {
			parts := []string{}
			for _, part := range strings.Split(field, ",") {
				part = strings.Trim(part, "!,")
				if selectedMnemonic(part) {
					parts = append(parts, part)
				}
			}
			if len(parts) < 2 {
				continue
			}
			for i := 0; i < len(parts); i++ {
				for j := i + 1; j < len(parts); j++ {
					ev := Evidence{Repository: Repository, Commit: c.Commit, Branch: c.Branch, SourceFile: "x86/insns.dat", StartLine: line, EndLine: line, Definition: fields[0] + " " + field, ExtractionMethod: "macro_argument_group_extraction", Confidence: "direct"}
					if err := writeLine(w, map[string]any{"left_mnemonic": parts[i], "right_mnemonic": parts[j], "relation": "macro_grouped_mnemonics", "confidence": "candidate", "semantic_alias_asserted": false, "evidence": ev}); err != nil {
						return err
					}
				}
			}
		}
	}
	return s.Err()
}

var recRE = regexp.MustCompile(`^\s*(\S+)\s+(\S+)\s+(\[[^\]]*\]|\S+)\s+(\S+)\s*$`)

func scanRecords(path string, fn func(Record) error) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 8192), 2<<20)
	line := 0
	for s.Scan() {
		line++
		t := strings.TrimSpace(s.Text())
		if t == "" || strings.HasPrefix(t, ";") || strings.HasPrefix(t, "#") {
			continue
		}
		m := recRE.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		r := Record{Mnemonic: m[1], Encoding: m[3], GeneratedLine: line}
		if m[2] != "void" && m[2] != "\\0" && m[2] != "ignore" {
			for _, o := range strings.Split(m[2], ",") {
				r.Operands = append(r.Operands, strings.TrimSpace(o))
			}
		}
		r.Flags = splitFlags(m[4])
		r.Pseudo = contains(r.Flags, "PSEUDO") || m[2] == "ignore"
		if e := fn(r); e != nil {
			return fmt.Errorf("line %d: %w", line, e)
		}
	}
	return s.Err()
}
func splitFlags(s string) []string {
	out := []string{}
	for _, x := range strings.Split(s, ",") {
		x = strings.TrimSpace(x)
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

var hexByte = regexp.MustCompile(`^(?:[0-9a-fA-F]{2}|[0-9a-fA-F]{2}#)$`)

func parseEncoding(s string) Encoding {
	z := Encoding{Raw: s, OpcodeMap: "one_byte", ModRM: "unknown", SIB: "unknown", Immediate: "unknown", Displacement: "unknown"}
	q := strings.TrimSpace(strings.Trim(s, "[]"))
	p := strings.SplitN(q, ":", 2)
	if len(p) == 2 {
		z.Slot = strings.TrimSpace(p[0])
		if strings.Contains(z.Slot, "x") {
			z.SIB = "required by x operand slot; NASM bytecode documents this as ModRM+SIB MIB encoding"
		}
		q = p[1]
	}
	for _, t := range strings.Fields(q) {
		z.Tokens = append(z.Tokens, t)
		lt := strings.ToLower(t)
		switch {
		case strings.HasPrefix(lt, "/r"):
			z.ModRM = "required: reg field from opcode operand; r/m field from encoded operand"
		case regexp.MustCompile(`^/[0-7]$`).MatchString(lt):
			z.ModRM = "required: fixed reg/opcode extension " + lt
		case strings.Contains(lt, "sib") || lt == "x":
			z.SIB = "explicitly indicated by bytecode token " + t
		case strings.HasPrefix(lt, "i"):
			z.Immediate = "explicit bytecode immediate token " + t
			z.ImmediateWidth = immediateWidth(lt)
		case strings.Contains(lt, "disp") || strings.Contains(lt, "rel"):
			z.Displacement = "explicit bytecode displacement token " + t
			z.DisplacementWidth = displacementWidth(lt)
		case lt == "0f38" || lt == "0f3a":
			z.OpcodeMap = lt
			z.OpcodeBytes = append(z.OpcodeBytes, lt)
		case lt == "0f":
			z.OpcodeMap = "0f"
			z.OpcodeBytes = append(z.OpcodeBytes, lt)
		case regexp.MustCompile(`^[0-9a-fA-F]{2}\+[a-zA-Z][a-zA-Z0-9]*#?$`).MatchString(t):
			z.OpcodeExpressions = append(z.OpcodeExpressions, t)
		case hexByte.MatchString(t):
			z.OpcodeBytes = append(z.OpcodeBytes, strings.TrimSuffix(strings.ToLower(t), "#"))
		case isPrefixToken(lt):
			z.PrefixTokens = append(z.PrefixTokens, t)
		default:
			if marker, ok := describeEncodingMarker(lt); ok {
				marker.Token = t
				z.SemanticMarkers = append(z.SemanticMarkers, marker)
			} else {
				z.UnknownTokens = append(z.UnknownTokens, t)
			}
		}
	}
	return z
}

func immediateWidth(t string) string {
	switch {
	case strings.HasPrefix(t, "iwdq"):
		return "mode/operand-size dependent (NASM iwdq)"
	case strings.HasPrefix(t, "ib"):
		return "8 bits"
	case strings.HasPrefix(t, "iw"):
		return "16 bits"
	case strings.HasPrefix(t, "id"):
		return "32 bits"
	case strings.HasPrefix(t, "iq"):
		return "64 bits"
	default:
		return "operand-size or bytecode dependent; raw token retained"
	}
}

func displacementWidth(t string) string {
	switch {
	case strings.HasPrefix(t, "rel8"):
		return "8 bits relative"
	case strings.HasPrefix(t, "rel16"):
		return "16 bits relative"
	case strings.HasPrefix(t, "rel32"):
		return "32 bits relative"
	case t == "rel":
		return "mode/operand-size dependent relative field"
	default:
		return "bytecode dependent; raw token retained"
	}
}

func describeEncodingMarker(token string) (EncodingMarker, bool) {
	bytecode := map[string]string{
		"nw":    "operand-size rule: defaults to 64-bit in long mode; 32-bit size unavailable in that mode unless overridden as specified by adjacent o16/o32/o64 tokens",
		"o64nw": "fixed 64-bit operand size; equivalent to nw plus o64",
		"odf":   "valid only when operand size is default",
		"osz":   "default or user-specified operand size",
		"optd":  "32- and 64-bit operation identical; generator optimization hint",
		"optw":  "16-, 32- and 64-bit operation identical; generator optimization hint",
		"rex.l": "LOCK prefix used as REX.R in 16/32-bit mode",
		"rex2":  "obligatory REX2 prefix",
		"np":    "no SSE prefix",
		"seg":   "16-bit segment immediate from segment part of operand",
		"jcc8":  "conditional-branch short-form eligibility test",
		"jmp8":  "unconditional-branch short-form eligibility test",
		"jlen":  "conditional jump-over length marker (03 in 16-bit, 05 in 32-bit per bytecode spec)",
		"m0":    "zero-byte marker; following bytes are literal and not prefix bytes",
	}
	if m, ok := bytecode[token]; ok {
		line := 0
		switch token {
		case "nw":
			line = 193
		case "o64nw":
			line = 189
		case "odf":
			line = 186
		case "osz":
			line = 201
		case "optd":
			line = 208
		case "optw":
			line = 207
		case "rex.l":
			line = 205
		case "np":
			line = 223
		case "seg":
			line = 94
		case "jcc8", "jmp8":
			line = 229
		case "jlen":
			line = 231
		case "m0":
			line = 221
		}
		return EncodingMarker{Meaning: m, SourceFile: "x86/bytecode.txt", SourceLine: line, Confidence: "direct"}, true
	}
	if strings.HasPrefix(token, "vex+") {
		return EncodingMarker{Meaning: "instruction has both VEX and EVEX encoding alternatives; insns.pl expands the alternatives according to operands", SourceFile: "x86/bytecode.txt", SourceLine: 161, Confidence: "direct"}, true
	}
	if token == "nd" || token == "nds" || token == "ndd" {
		return EncodingMarker{Meaning: "NDD/ND encoding decorator control handled by insns.pl; exact resulting prefix bits depend on template context", SourceFile: "x86/insns.pl", SourceLine: 1508, Confidence: "derived"}, true
	}
	return EncodingMarker{}, false
}

func isPrefixToken(s string) bool {
	return s == "lock" || s == "rep" || s == "repe" || s == "repne" || s == "o8" || s == "o16" || s == "o32" || s == "o64" || s == "a16" || s == "a32" || s == "a64" || s == "rex" || s == "norex" || s == "vex" || s == "evex" || strings.HasPrefix(s, "evex.") || strings.HasPrefix(s, "vex.") || strings.HasPrefix(s, "hle") || s == "66" || s == "f2" || s == "f3" || s == "0f" || s == "0f38" || s == "0f3a"
}
func operandConstraints(ops []string) []OperandConstraint {
	r := make([]OperandConstraint, 0, len(ops))
	for i, o := range ops {
		w := widthOf(o)
		kind := "class"
		if strings.Contains(o, "imm") {
			kind = "immediate"
		} else if strings.Contains(o, "rm") || strings.HasPrefix(o, "mem") || strings.Contains(o, "moffs") {
			kind = "register_or_memory"
		} else if strings.HasPrefix(o, "reg") || strings.Contains(o, "_ax") || strings.Contains(o, "_al") {
			kind = "register"
		}
		r = append(r, OperandConstraint{Index: i, RawClass: o, Kind: kind, WidthBits: w, SourceDestinationRole: "unknown", RegisterConstraint: o, MemoryConstraint: strings.Contains(o, "rm") || strings.Contains(o, "mem"), ImmediateConstraint: strings.Contains(o, "imm"), AddressingConstraint: "unknown_from_insns_dat_record", Confidence: "direct_operand_class; role_unresolved"})
	}
	return r
}
func operandConstraintsWithEvidence(ops []string, evidence []Evidence) []OperandConstraint {
	out := operandConstraints(ops)
	for i := range out {
		out[i].Evidence = evidence
	}
	return out
}
func widthOf(s string) int {
	lower := strings.ToLower(s)
	for _, x := range []struct {
		suffix string
		width  int
	}{{"_rax", 64}, {"_eax", 32}, {"_ax", 16}, {"_al", 8}, {"_cl", 8}} {
		if strings.HasSuffix(lower, x.suffix) {
			return x.width
		}
	}
	re := regexp.MustCompile(`(?:8|16|32|64|80|128|256|512)(?:$|[^0-9])`)
	m := re.FindStringSubmatch(s)
	if len(m) > 0 {
		fmt.Sscanf(m[0], "%d", new(int))
		n := 0
		_, _ = fmt.Sscanf(m[0], "%d", &n)
		return n
	}
	return 0
}
func modeValidity(flags []string) map[string]string {
	out := map[string]string{"16": "unknown", "32": "unknown", "64": "unknown"}
	set := map[string]bool{}
	for _, f := range flags {
		set[strings.ToUpper(f)] = true
	}
	if set["LONG"] || set["X86_64"] {
		out["64"] = "valid"
	}
	if set["NOLONG"] {
		out["64"] = "invalid"
	}
	if set["PSEUDO"] {
		for k := range out {
			out[k] = "not_hardware_instruction"
		}
	}
	return out
}
func modeConstraintDetails(flags []string, evidence []Evidence) []ModeConstraint {
	v := modeValidity(flags)
	return []ModeConstraint{{Mode: "16", Validity: v["16"], Evidence: evidence}, {Mode: "32", Validity: v["32"], Evidence: evidence}, {Mode: "64", Validity: v["64"], Evidence: evidence}}
}
func featureFlags(flags []string) []string {
	out := []string{}
	for _, f := range flags {
		u := strings.ToUpper(f)
		if regexp.MustCompile(`^(?:SSE|AVX|BMI|AES|FMA|3DNOW|XOP|F16C|SHA|ADX|GFNI|VAES|VPCLMUL|AMX|APX|RTM|SMX|VMX)`).MatchString(u) {
			out = append(out, u)
		}
	}
	return out
}
func featureConstraintDetails(flags []string, evidence []Evidence, catalogs ...map[string]FlagDefinition) []FeatureConstraint {
	catalog := map[string]FlagDefinition{}
	if len(catalogs) > 0 {
		catalog = catalogs[0]
	}
	out := []FeatureConstraint{}
	for _, raw := range flags {
		f := strings.ToUpper(raw)
		requirement := "required"
		id := f
		// NASM uses NO<feature> flags to express a forbidden ISA feature.
		// Resolve the positive feature first so NOAPX is not mistaken for an
		// independent instruction-eligibility flag when it is present in iflags.ph.
		lookup := f
		if strings.HasPrefix(f, "NO") && isISAFeature(strings.TrimPrefix(f, "NO")) {
			id = strings.TrimPrefix(f, "NO")
			requirement = "forbidden"
			lookup = id
		}
		if def, ok := catalog[lookup]; ok {
			kind := "instruction_eligibility"
			if def.Group == "FEATURE" {
				kind = "isa_feature"
			}
			if def.Group == "CPU" {
				kind = "cpu_generation"
			}
			out = append(out, FeatureConstraint{FeatureID: id, Kind: kind, Requirement: requirement, RawFlag: raw, Description: def.Description, Confidence: "direct_flag_definition", Evidence: append(append([]Evidence{}, evidence...), def.Evidence)})
			continue
		}
		if isISAFeature(id) {
			out = append(out, FeatureConstraint{FeatureID: id, Kind: "isa_feature", Requirement: requirement, RawFlag: raw, Confidence: "derived_feature_prefix", Evidence: evidence})
			continue
		}
		if contains(cpuGenerationFlags([]string{f}), f) {
			out = append(out, FeatureConstraint{FeatureID: f, Kind: "cpu_generation", Requirement: "minimum_generation_or_eligibility_token; exact predicate retained in raw flag", Evidence: evidence})
		}
	}
	return out
}

func loadFlagDefinitions(path string, c Config) (map[string]FlagDefinition, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	defs := map[string]FlagDefinition{}
	group := "IGEN"
	line := 0
	re := regexp.MustCompile(`if_\(\s*"([A-Za-z0-9_]+)"\s*,\s*"([^"]*)"`)
	gr := regexp.MustCompile(`if_align\(\s*'([A-Za-z0-9_]+)'`)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line++
		text := s.Text()
		if m := gr.FindStringSubmatch(text); m != nil {
			group = m[1]
		}
		if m := re.FindStringSubmatch(text); m != nil {
			id := strings.ToUpper(m[1])
			defs[id] = FlagDefinition{ID: id, Group: group, Description: m[2], Evidence: Evidence{Repository: Repository, Commit: c.Commit, Branch: c.Branch, SourceFile: "x86/iflags.ph", StartLine: line, EndLine: line, Definition: "if_(" + id + ")", ExtractionMethod: "iflags_definition_parse", Confidence: "direct"}}
		}
	}
	return defs, s.Err()
}

func isISAFeature(flag string) bool {
	return regexp.MustCompile(`^(?:SSE|AVX|BMI|AES|FMA|3DNOW|XOP|F16C|SHA|ADX|GFNI|VAES|VPCLMUL|AMX|APX|RTM|SMX|VMX)`).MatchString(flag)
}
func prefixConstraintDetails(prefixes []string, evidence []Evidence) []PrefixConstraint {
	out := make([]PrefixConstraint, 0, len(prefixes))
	for _, p := range prefixes {
		class := "encoding_token"
		switch strings.ToLower(p) {
		case "lock":
			class = "lock"
		case "rep", "repe", "repne":
			class = "repeat"
		case "rex", "norex", "vex", "evex":
			class = "vector_or_rex"
		case "o8", "o16", "o32", "o64", "a16", "a32", "a64":
			class = "size_control"
		case "66", "f2", "f3":
			class = "mandatory_prefix_candidate"
		}
		out = append(out, PrefixConstraint{Token: p, Class: class, Requirement: "present_in_encoding_template", Effect: "exact architectural legality/effect is context-dependent or not established here", Confidence: "direct_token; effect_unresolved", Evidence: evidence})
	}
	return out
}
func cpuGenerationFlags(flags []string) []string {
	out := []string{}
	for _, f := range flags {
		u := strings.ToUpper(f)
		if regexp.MustCompile(`^(?:8086|186|286|386|486|P[0-9]+)$`).MatchString(u) {
			out = append(out, u)
		}
	}
	return out
}
func machineSemantics(m string, r Record) InstructionSemantic {
	op := "unknown"
	switch m {
	case "ADD":
		op = "integer.add.fixed_width"
	case "SUB":
		op = "integer.subtract.fixed_width"
	case "MUL", "IMUL":
		op = "integer.multiply.fixed_width"
	case "DIV", "IDIV":
		op = "integer.divide.fixed_width"
	case "CMP":
		op = "integer.compare.fixed_width"
	case "TEST":
		op = "bitwise.test"
	case "MOV":
		op = "data.move"
	case "LEA":
		op = "effective_address"
	case "AND", "OR", "XOR", "NOT":
		op = "bitwise." + strings.ToLower(m)
	case "SHL", "SHR", "SAR":
		op = "bitwise.shift." + strings.ToLower(m)
	case "JMP", "CALL", "RET":
		op = "control." + strings.ToLower(m)
	case "PUSH", "POP":
		op = "stack." + strings.ToLower(m)
	}
	if (strings.HasPrefix(m, "J") && m != "JMP") || m == "Jcc" {
		op = "control.conditional_branch"
	}
	return InstructionSemantic{MachineOperation: op, CanonicalSemanticRelation: "machine_realization_candidate", Reads: "unknown_from_encoding_table_alone", Writes: "unknown_from_encoding_table_alone", FlagsRead: "unknown", FlagsWritten: "unknown", FlagsUndefined: "unknown", Confidence: "inferred", EvidenceBasis: "mnemonic family identity; insns.dat encoding record does not fully specify architectural effects", Evidence: r.Evidence}
}

func asEvidence(v any) []Evidence {
	if e, ok := v.([]Evidence); ok {
		return e
	}
	return nil
}
func extractRegisters(path string, w io.Writer, c Config, st *Stats) error {
	f, e := os.Open(path)
	if e != nil {
		return e
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 4096), 1<<20)
	type reg struct {
		name, cls, dc, num, tokenFlags string
		line                           int
	}
	rows := []reg{}
	ln := 0
	for s.Scan() {
		ln++
		t := strings.TrimSpace(s.Text())
		if t == "" || strings.HasPrefix(t, "#") {
			continue
		}
		p := strings.Fields(t)
		if len(p) < 4 {
			continue
		}
		name := p[0]
		start, end, prefix, suffix, ranged := registerRange(name)
		flags := ""
		if len(p) > 4 {
			flags = strings.Join(p[4:], " ")
		}
		if ranged {
			for i := start; i <= end; i++ {
				rows = append(rows, reg{prefix + fmt.Sprint(i) + suffix, p[1], p[2], fmt.Sprint(i), flags, ln})
			}
		} else {
			rows = append(rows, reg{name, p[1], p[2], p[3], flags, ln})
		}
	}
	if e := s.Err(); e != nil {
		return e
	}
	root := func(n string) string {
		seen := map[string]bool{}
		for registerParent(n) != "" && !seen[n] {
			seen[n] = true
			n = registerParent(n)
		}
		return n
	}
	aliases := map[string][]string{}
	for _, r := range rows {
		aliases[r.name] = []string{}
		rt := root(r.name)
		if rt != r.name {
			aliases[rt] = append(aliases[rt], r.name)
		}
	}
	for _, r := range rows {
		ev := Evidence{Repository: Repository, Commit: c.Commit, Branch: c.Branch, SourceFile: "x86/regs.dat", StartLine: r.line, EndLine: r.line, Definition: r.name, ExtractionMethod: "structured_register_record", Confidence: "direct"}
		if e := writeLine(w, map[string]any{"register_id": r.name, "name": r.name, "register_class": r.cls, "disassembler_classes": r.dc, "register_number": r.num, "token_flags": r.tokenFlags, "width_bits": registerWidth(r.name), "parent_register": registerParent(r.name), "aliases": aliases[r.name], "mode_restrictions": "unknown", "evidence": ev}); e != nil {
			return e
		}
		st.DirectFacts++
	}
	return nil
}
func registerWidth(s string) int {
	u := strings.ToLower(s)
	if strings.HasPrefix(u, "zmm") {
		return 512
	}
	if strings.HasPrefix(u, "ymm") {
		return 256
	}
	if strings.HasPrefix(u, "xmm") {
		return 128
	}
	if strings.HasPrefix(u, "r") && len(u) > 1 {
		tail := u[1:]
		if len(tail) > 0 {
			switch tail[len(tail)-1] {
			case 'b':
				return 8
			case 'w':
				return 16
			case 'd':
				return 32
			}
		}
		if _, err := fmt.Sscanf(tail, "%d", new(int)); err == nil {
			return 64
		}
	}
	switch u {
	case "rax", "rbx", "rcx", "rdx", "rsi", "rdi", "rbp", "rsp":
		return 64
	case "eax", "ebx", "ecx", "edx", "esi", "edi", "ebp", "esp":
		return 32
	case "ax", "bx", "cx", "dx", "si", "di", "bp", "sp":
		return 16
	case "al", "ah", "bl", "bh", "cl", "ch", "dl", "dh":
		return 8
	case "mm0", "mm1", "mm2", "mm3", "mm4", "mm5", "mm6", "mm7":
		return 64
	case "st0", "st1", "st2", "st3", "st4", "st5", "st6", "st7":
		return 80
	}
	return 0
}
func registerParent(s string) string {
	m := map[string]string{"EAX": "RAX", "AX": "EAX", "AL": "AX", "AH": "AX", "EBX": "RBX", "BX": "EBX", "BL": "BX", "BH": "BX", "ECX": "RCX", "CX": "ECX", "CL": "CX", "CH": "CX", "EDX": "RDX", "DX": "EDX", "DL": "DX", "DH": "DX", "ESI": "RSI", "SI": "ESI", "EDI": "RDI", "DI": "EDI", "EBP": "RBP", "BP": "EBP", "ESP": "RSP", "SP": "ESP"}
	u := strings.ToUpper(s)
	if p := m[u]; p != "" {
		return p
	}
	if strings.HasSuffix(u, "B") {
		n := strings.TrimSuffix(strings.TrimPrefix(u, "R"), "B")
		if n != "" {
			return "R" + n + "W"
		}
	}
	if strings.HasSuffix(u, "W") {
		n := strings.TrimSuffix(strings.TrimPrefix(u, "R"), "W")
		if n != "" {
			return "R" + n + "D"
		}
	}
	if strings.HasSuffix(u, "D") {
		n := strings.TrimSuffix(strings.TrimPrefix(u, "R"), "D")
		if n != "" {
			return "R" + n
		}
	}
	if strings.HasPrefix(u, "XMM") {
		return "YMM" + strings.TrimPrefix(u, "XMM")
	}
	if strings.HasPrefix(u, "YMM") {
		return "ZMM" + strings.TrimPrefix(u, "YMM")
	}
	return ""
}

var registerRangeRE = regexp.MustCompile(`^([A-Za-z]+)([0-9]+)-([0-9]+)([A-Za-z]*)$`)

func registerRange(name string) (int, int, string, string, bool) {
	m := registerRangeRE.FindStringSubmatch(name)
	if m == nil {
		return 0, 0, "", "", false
	}
	var first, last int
	if _, err := fmt.Sscanf(m[2], "%d", &first); err != nil {
		return 0, 0, "", "", false
	}
	if _, err := fmt.Sscanf(m[3], "%d", &last); err != nil || last < first {
		return 0, 0, "", "", false
	}
	return first, last, m[1], m[4], true
}
func sourceIndex(c Config) (CompilerEvidenceIndex, map[string]string, error) {
	files := []string{"LICENSE", "x86/insns.dat", "x86/insns.pl", "x86/preinsns.pl", "x86/bytecode.txt", "x86/regs.dat", "asm/parser.c", "asm/assemble.c", "x86/insns-iflags.ph", "x86/iflags.ph"}
	hash := map[string]string{}
	units := []SourceUnitEvidence{}
	for _, rel := range files {
		p := filepath.Join(c.SourceRoot, filepath.FromSlash(rel))
		b, e := os.ReadFile(p)
		if e != nil {
			continue
		}
		h := sha256.Sum256(b)
		hs := hex.EncodeToString(h[:])
		hash[rel] = hs
		units = append(units, SourceUnitEvidence{Path: rel, SHA256: hs, Bytes: len(b)})
	}
	return CompilerEvidenceIndex{CompilerEvidence: CompilerEvidence{Repository: Repository, Commit: c.Commit, Branch: c.Branch, Timestamp: time.Now().UTC().Format(time.RFC3339), SourceHash: hash}, SourceUnits: units, Expansion: ExpansionEvidence{Tool: "x86/preinsns.pl", Input: "x86/insns.dat", ExpandedFileSHA256: fileHash(c.ExpandedFile)}}, hash, nil
}

func writePipelineGraph(c Config, w io.Writer) error {
	units := []struct {
		file    string
		symbols []string
	}{{"asm/parser.c", []string{"parse_line", "parse_operands", "parse_mref", "parse_decorators", "process_size_override"}}, {"asm/assemble.c", []string{"nasm_ofmt_output", "find_match", "matches", "calcsize", "emit_prefixes", "gencode", "assemble"}}, {"x86/insns.pl", []string{"read_insns", "insns"}}, {"x86/preinsns.pl", []string{"preprocess", "expand"}}}
	for _, u := range units {
		path := filepath.Join(c.SourceRoot, filepath.FromSlash(u.file))
		b, e := os.ReadFile(path)
		if e != nil {
			continue
		}
		lines := strings.Split(string(b), "\n")
		for _, sym := range u.symbols {
			found := findFunctionDefinitionLine(lines, sym)
			if found == 0 {
				continue
			}
			ev := Evidence{Repository: Repository, Commit: c.Commit, Branch: c.Branch, SourceFile: u.file, StartLine: found, EndLine: found, Definition: sym, ExtractionMethod: "source_symbol_line_lookup", Confidence: "direct"}
			if e := writeLine(w, map[string]any{"from": "nasm_pipeline:" + u.file + ":" + sym, "relation": "source_symbol", "to": u.file + ":" + sym, "stage": pipelineStage(u.file, sym), "confidence": "direct", "evidence": ev}); e != nil {
				return e
			}
		}
	}
	// The arrows describe the documented data-generation/assembly pipeline, not
	// a claim that each C helper calls the next one directly.
	stages := []string{"insns.dat record", "preinsns macro expansion", "insns.pl template-table generation", "asm/parser.c source and operand parsing", "asm/assemble.c template matching", "mode and prefix resolution", "calcsize sizing and fixup planning", "gencode byte emission", "nasm_ofmt_output relocation/output handoff"}
	for i := 0; i < len(stages)-1; i++ {
		if e := writeLine(w, map[string]any{"from": "nasm_pipeline:" + stages[i], "relation": "pipeline_stage_candidate", "to": "nasm_pipeline:" + stages[i+1], "confidence": "inferred", "evidence": Evidence{Repository: Repository, Commit: c.Commit, Branch: c.Branch, SourceFile: "x86/insns.dat", StartLine: 1, EndLine: 1, ExtractionMethod: "cross-source-pipeline-reconstruction; inspect source_symbol rows for concrete locations", Confidence: "inferred"}}); e != nil {
			return e
		}
	}
	return nil
}

func findFunctionDefinitionLine(lines []string, symbol string) int {
	for i, line := range lines {
		if strings.Contains(line, "sub "+symbol) {
			return i + 1
		}
		if !strings.Contains(line, symbol+"(") {
			continue
		}
		snippet := ""
		for j := i; j < len(lines) && j < i+6; j++ {
			snippet += lines[j] + "\n"
			if strings.Contains(lines[j], "{") {
				return i + 1
			}
			if strings.Contains(lines[j], ";") {
				break
			}
		}
	}
	return 0
}
func pipelineStage(file, symbol string) string {
	if file == "x86/preinsns.pl" {
		return "macro_expansion"
	}
	if file == "x86/insns.pl" {
		return "instruction_table_generation"
	}
	if file == "asm/parser.c" {
		return "syntax_and_operand_parsing"
	}
	switch symbol {
	case "find_match", "matches":
		return "template_matching"
	case "emit_prefixes":
		return "prefix_resolution"
	case "calcsize":
		return "size_and_fixup_planning"
	case "gencode":
		return "machine_byte_emission"
	case "assemble":
		return "assembly_driver"
	}
	return "source_analysis"
}
func fileHash(p string) string {
	b, e := os.ReadFile(p)
	if e != nil {
		return ""
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func writeJSON(path string, v any) error {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(b, '\n'), 0644)
}
func writeLine(w io.Writer, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	_, e = w.Write(append(b, '\n'))
	return e
}
func countJSONL(p string) int {
	f, e := os.Open(p)
	if e != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	n := 0
	for s.Scan() {
		n++
	}
	return n
}
func contains(a []string, s string) bool {
	for _, x := range a {
		if x == s {
			return true
		}
	}
	return false
}
func sortedFamilyNames() []string {
	r := []string{}
	for x := range Families {
		r = append(r, x)
	}
	r = append(r, "Jcc")
	sort.Strings(r)
	return r
}
func writeReport(path string, s Stats, c Config) error {
	body := fmt.Sprintf("# NASM source evidence extraction\n\nRepository: %s\n\nCommit: `%s`\n\nExpanded records are parsed from NASM's generated preprocessed instruction table. Machine effects not specified by source tables are marked `unknown`; backend support is not inferred. No NASM code or encoder changes are included.\n\n- Source records scanned: %d\n- Prototype records analyzed: %d\n- Mnemonics discovered (includes JCC/SAL forms): %d\n- Variants: %d\n- Encoding variants: %d\n- Register identities (compressed ranges expanded): %d\n- ISA feature identities: %d\n- CPU generation identities: %d\n- Encoding-equivalent pairs: %d\n- Pseudo records: %d\n- Direct evidence facts: %d\n- Derived evidence facts: %d\n- Unresolved provenance records: %d\n- Records with unknown encoding tokens preserved: %d\n- Native matches/conflicts: %d/%d (native comparison rows remain NOT_COMPARABLE)\n\nMode validity in 16/32-bit is kept `unknown` unless the source proves it. Flags such as `8086` and `386` are CPU-generation requirements, not mode assertions. Flag effects remain unknown. The data export uses `preinsns.pl`-expanded rows; later `insns.pl` template transformations are linked as source-pipeline evidence but not claimed as fully replayed.\n", Repository, c.Commit, s.SourceRecordsScanned, s.Records, s.Mnemonics, s.Variants, s.Encodings, s.Registers, s.Features, s.CPUGenerations, s.Aliases, s.Pseudo, s.DirectFacts, s.DerivedFacts, s.Unresolved, s.UnresolvedEncodingRecords, s.NativeMatches, s.NativeConflicts)
	return os.WriteFile(path, []byte(body), 0644)
}
