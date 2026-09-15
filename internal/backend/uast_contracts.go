// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// SemanticContractKind is the small, language-neutral contract vocabulary
// shared by frontends and native consumers.  The payload remains one typed
// envelope instead of becoming a second intermediate representation.
type SemanticContractKind string

const (
	SemanticFunctionContractKind   SemanticContractKind = "FUNCTION"
	SemanticCallContractKind       SemanticContractKind = "CALL"
	SemanticNumericContractKind    SemanticContractKind = "NUMERIC"
	SemanticConversionContractKind SemanticContractKind = "CONVERSION"
	SemanticABIContractKind        SemanticContractKind = "ABI"
	SemanticSymbolContractKind     SemanticContractKind = "SYMBOL"
)

const semanticContractSchema = "code-transpiler.semantic-contracts.v2"

// SemanticContract is an interned, strictly typed semantic payload.  Hash is
// over Kind and canonical Payload, never over source spelling or target
// machine details.
type SemanticContract struct {
	ID      int                  `json:"id"`
	Kind    SemanticContractKind `json:"kind"`
	Hash    string               `json:"hash"`
	Payload json.RawMessage      `json:"payload"`
}

// SemanticContractReference attaches a contract to a UAST node.  Role makes
// multiple contracts of one kind on one node unambiguous without duplicating
// the payload in the node.
type SemanticContractReference struct {
	NodeID     int    `json:"node_id"`
	ContractID int    `json:"contract_id"`
	Role       string `json:"role"`
}

type SemanticContractParameter struct {
	ID      int          `json:"id"`
	Name    string       `json:"name"`
	Type    SemanticType `json:"type,omitempty"`
	Mode    string       `json:"mode,omitempty"`
	Passing string       `json:"passing,omitempty"`
}

type SemanticReceiverContract struct {
	Type       SemanticType `json:"type,omitempty"`
	Passing    string       `json:"passing,omitempty"`
	Ownership  string       `json:"ownership,omitempty"`
	Mutability string       `json:"mutability,omitempty"`
	Dispatch   string       `json:"dispatch,omitempty"`
}

type SemanticFunctionContract struct {
	Parameters       []SemanticContractParameter `json:"parameters"`
	Results          []SemanticType              `json:"results"`
	Receiver         *SemanticReceiverContract   `json:"receiver,omitempty"`
	VariadicMode     string                      `json:"variadic_mode,omitempty"`
	CallingSemantics string                      `json:"calling_semantics,omitempty"`
	Effects          []string                    `json:"effects,omitempty"`
	Throws           string                      `json:"throws,omitempty"`
}

type SemanticCallArgumentContract struct {
	NodeID  int    `json:"node_id"`
	Name    string `json:"name,omitempty"`
	Missing bool   `json:"missing,omitempty"`
}

type SemanticCallContract struct {
	CallKind          string                         `json:"call_kind"`
	CalleeNode        *int                           `json:"callee_node,omitempty"`
	ReceiverNode      *int                           `json:"receiver_node,omitempty"`
	Arguments         []SemanticCallArgumentContract `json:"arguments"`
	SelectedCandidate *int                           `json:"selected_candidate,omitempty"`
	EvaluationOrder   string                         `json:"evaluation_order"`
	TailPosition      bool                           `json:"tail_position,omitempty"`
}

type SemanticNumericContract struct {
	Type       SemanticType `json:"type"`
	Overflow   string       `json:"overflow"`
	Rounding   string       `json:"rounding"`
	Division   string       `json:"division"`
	NaN        string       `json:"nan"`
	SignedZero string       `json:"signed_zero"`
}

type SemanticConversionContract struct {
	SourceType   SemanticType `json:"source_type"`
	TargetType   SemanticType `json:"target_type"`
	Kind         string       `json:"kind"`
	Lossiness    string       `json:"lossiness"`
	Overflow     string       `json:"overflow"`
	Rounding     string       `json:"rounding"`
	RuntimeCheck string       `json:"runtime_check"`
}

type SemanticABIContract struct {
	CallingConvention           string         `json:"calling_convention"`
	External                    bool           `json:"external,omitempty"`
	VariadicABI                 string         `json:"variadic_abi,omitempty"`
	ReturnConventionRequirement string         `json:"return_convention_requirement,omitempty"`
	ForeignABIIdentity          string         `json:"foreign_abi_identity,omitempty"`
	Symbol                      string         `json:"symbol,omitempty"`
	Parameters                  []SemanticType `json:"parameters,omitempty"`
	Results                     []SemanticType `json:"results,omitempty"`
	Result                      *SemanticType  `json:"result,omitempty"`
}

type SemanticSymbolContract struct {
	Identity     string `json:"identity"`
	Linkage      string `json:"linkage"`
	Visibility   string `json:"visibility"`
	ImportExport string `json:"import_export"`
	Module       string `json:"module,omitempty"`
	External     bool   `json:"external,omitempty"`
	Strength     string `json:"strength,omitempty"`
}

type contractCandidate struct {
	kind    SemanticContractKind
	payload json.RawMessage
	nodeID  int
	role    string
}

type contractReferenceCandidate struct {
	nodeID int
	hash   string
	role   string
}

func contractPayload(kind SemanticContractKind, value any) (json.RawMessage, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return canonicalJSONBytes(raw)
}

func semanticContractHash(kind SemanticContractKind, payload []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(kind))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

func decodeStrictContractPayload(c SemanticContract, out any) (err error) {
	// Some malformed-but-json-valid payloads can still trigger a panic in the
	// strict json decoder when a typed field has the wrong shape. Contract
	// validation is an input boundary: never let one unit abort a project
	// measurement or compilation process. Convert the decoder panic into the
	// same explicit schema error returned for ordinary decode failures.
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("contract payload decoder panic: %v", recovered)
		}
	}()
	trimmed := bytes.TrimSpace(c.Payload)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return fmt.Errorf("contract payload must be a JSON object")
	}
	dec := json.NewDecoder(bytes.NewReader(c.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("contract payload contains trailing JSON")
	}
	return nil
}

func validateSemanticContractPayload(c SemanticContract) error {
	if c.ID < 0 || c.Kind == "" || len(c.Payload) == 0 || !json.Valid(c.Payload) {
		return fmt.Errorf("invalid semantic contract envelope")
	}
	canonical, err := canonicalJSONBytes(c.Payload)
	if err != nil {
		return fmt.Errorf("contract %s payload: %w", c.Kind, err)
	}
	if c.Hash == "" || c.Hash != semanticContractHash(c.Kind, canonical) {
		return fmt.Errorf("contract %s hash does not match canonical payload", c.Kind)
	}
	var target any
	switch c.Kind {
	case SemanticFunctionContractKind:
		target = &SemanticFunctionContract{}
	case SemanticCallContractKind:
		target = &SemanticCallContract{}
	case SemanticNumericContractKind:
		target = &SemanticNumericContract{}
	case SemanticConversionContractKind:
		target = &SemanticConversionContract{}
	case SemanticABIContractKind:
		target = &SemanticABIContract{}
	case SemanticSymbolContractKind:
		target = &SemanticSymbolContract{}
	default:
		target = extendedContractPayloadTarget(c.Kind)
		if target == nil {
			return fmt.Errorf("unknown semantic contract kind %q", c.Kind)
		}
	}
	if err := decodeStrictContractPayload(c, target); err != nil {
		return fmt.Errorf("contract %s payload schema: %w", c.Kind, err)
	}
	return nil
}

// normalizeUniversalContracts interns existing contracts and derives the six
// phase-1 contracts from already explicit UAST fields.  It never guesses a
// target register, stack location, or source-language construct; unavailable
// semantic facts remain "unknown" in a typed payload.
func normalizeUniversalContracts(d *UniversalASTDocument) error {
	if d == nil {
		return nil
	}
	// Canonical structured-facts exports have already interned and validated
	// their contract table at the producer boundary. On import, verify the
	// compact table's identity and hashes, then reuse it instead of rebuilding
	// node/common/child indexes and re-deriving every extended contract for each
	// backend pass. The ordinary document validator still checks payload schemas,
	// reference targets, required consumer attachments, and extended consumers.
	if d.Metadata != nil && d.Metadata["frontend_route"] == "CANONICALIZE_ONLY" && d.Surface != nil &&
		d.ContractSchema == semanticContractSchema && len(d.ContractTable) > 0 && len(d.ContractRefs) > 0 {
		for i := range d.ContractTable {
			contract := &d.ContractTable[i]
			if contract.ID != i {
				return fmt.Errorf("canonical contract cache IDs must be contiguous")
			}
			canonical, err := canonicalJSONBytes(contract.Payload)
			if err != nil {
				return err
			}
			hash := semanticContractHash(contract.Kind, canonical)
			if contract.Hash == "" || contract.Hash != hash {
				return fmt.Errorf("canonical contract cache hash mismatch at contract %d", i)
			}
			contract.Payload = canonical
		}
		return nil
	}
	byHash := map[string]SemanticContract{}
	oldHash := map[int]string{}
	for _, c := range d.ContractTable {
		canonical, err := canonicalJSONBytes(c.Payload)
		if err != nil {
			return err
		}
		hash := semanticContractHash(c.Kind, canonical)
		if c.Hash != "" && c.Hash != hash {
			return fmt.Errorf("contract %d hash mismatch", c.ID)
		}
		c.Payload, c.Hash = canonical, hash
		if err := validateSemanticContractPayload(c); err != nil {
			return err
		}
		if _, exists := byHash[hash]; !exists {
			byHash[hash] = c
		}
		oldHash[c.ID] = hash
	}
	refs := []contractReferenceCandidate{}
	refsByRole := map[string]contractReferenceCandidate{}
	for _, ref := range d.ContractRefs {
		hash, ok := oldHash[ref.ContractID]
		if !ok {
			return fmt.Errorf("contract reference %d points to missing contract %d", ref.NodeID, ref.ContractID)
		}
		if ref.NodeID < 0 || ref.Role == "" {
			return fmt.Errorf("invalid contract reference")
		}
		refsByRole[strconv.Itoa(ref.NodeID)+"\x00"+ref.Role] = contractReferenceCandidate{nodeID: ref.NodeID, hash: hash, role: ref.Role}
	}
	// Rewrites may retain a result node ID while changing its operation/type.
	// Remove only stale references for the six derived roles; references to
	// missing nodes remain errors so malformed external UAST is still rejected.
	for key, ref := range refsByRole {
		if isDerivedContractRole(ref.role) && !nodeSupportsContractRole(d, ref.nodeID, ref.role) {
			delete(refsByRole, key)
		}
	}
	derived, err := deriveUniversalContractCandidates(d)
	if err != nil {
		return err
	}
	for _, candidate := range derived {
		canonical, err := canonicalJSONBytes(candidate.payload)
		if err != nil {
			return err
		}
		hash := semanticContractHash(candidate.kind, canonical)
		if _, exists := byHash[hash]; !exists {
			byHash[hash] = SemanticContract{Kind: candidate.kind, Payload: canonical, Hash: hash}
		}
		// A node/role has one semantic authority. Replacing an old derived
		// reference here is essential after a verified graph rewrite changes a
		// type or operation while retaining the stable result node ID.
		refsByRole[strconv.Itoa(candidate.nodeID)+"\x00"+candidate.role] = contractReferenceCandidate{nodeID: candidate.nodeID, hash: hash, role: candidate.role}
	}
	for _, ref := range refsByRole {
		refs = append(refs, ref)
	}
	if len(byHash) == 0 {
		d.ContractSchema = ""
		d.ContractTable = nil
		d.ContractRefs = nil
		return nil
	}
	hashes := make([]string, 0, len(byHash))
	for hash := range byHash {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	ids := map[string]int{}
	table := make([]SemanticContract, len(hashes))
	for i, hash := range hashes {
		entry := byHash[hash]
		entry.ID = i
		entry.Hash = hash
		table[i] = entry
		ids[hash] = i
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].nodeID != refs[j].nodeID {
			return refs[i].nodeID < refs[j].nodeID
		}
		if refs[i].role != refs[j].role {
			return refs[i].role < refs[j].role
		}
		return refs[i].hash < refs[j].hash
	})
	outRefs := make([]SemanticContractReference, 0, len(refs))
	for _, ref := range refs {
		outRefs = append(outRefs, SemanticContractReference{NodeID: ref.nodeID, ContractID: ids[ref.hash], Role: ref.role})
	}
	d.ContractSchema = semanticContractSchema
	d.ContractTable = table
	d.ContractRefs = outRefs
	return nil
}

func isDerivedContractRole(role string) bool {
	switch role {
	case "function", "call", "numeric", "conversion", "abi", "symbol":
		return true
	default:
		return knownExtendedContractRole(role)
	}
}

func nodeSupportsContractRole(d *UniversalASTDocument, nodeID int, role string) bool {
	found := false
	for i := range d.Nodes {
		n := &d.Nodes[i]
		if n.ID != nodeID {
			continue
		}
		found = true
		c, err := decodeUniversalCommon(n)
		if err != nil {
			return false
		}
		switch role {
		case "function":
			return c.Kind == "function" || n.StructuralKind == "FunctionDecl" || n.StructuralKind == "ClosureExpr"
		case "call":
			return c.Kind == "call" || n.StructuralKind == "CallExpr"
		case "numeric":
			return c.Type.Kind == "integer" || c.Type.Kind == "float" || c.Type.Kind == "decimal" || c.Operation.Typed != nil
		case "conversion":
			return c.Kind == "conversion" || n.StructuralKind == "ConvertExpr"
		case "abi":
			return len(n.Fields["abi_contract"]) != 0 || n.StructuralKind == "ABIContract" || len(n.Fields["calling_convention"]) != 0
		case "symbol":
			return c.Kind == "identifier" || n.StructuralKind == "SymbolRef"
		default:
			return nodeSupportsExtendedContractRole(n, c, role)
		}
	}
	if !found {
		return true
	}
	return false
}

func deriveUniversalContractCandidates(d *UniversalASTDocument) ([]contractCandidate, error) {
	children, err := universalChildrenByRole(d)
	if err != nil {
		return nil, err
	}
	common := map[int]universalDecodedCommon{}
	for i := range d.Nodes {
		c, err := decodeUniversalCommon(&d.Nodes[i])
		if err != nil {
			continue
		}
		common[d.Nodes[i].ID] = c
	}
	var out []contractCandidate
	add := func(nodeID int, role string, kind SemanticContractKind, value any) error {
		payload, err := contractPayload(kind, value)
		if err != nil {
			return err
		}
		out = append(out, contractCandidate{kind: kind, payload: payload, nodeID: nodeID, role: role})
		return nil
	}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		c, ok := common[n.ID]
		if !ok {
			continue
		}
		roles := children[n.ID]
		isFunction := c.Kind == "function" || n.StructuralKind == "FunctionDecl" || n.StructuralKind == "ClosureExpr"
		if isFunction {
			fn := SemanticFunctionContract{Parameters: []SemanticContractParameter{}, Results: []SemanticType{}}
			for _, child := range roles["parameter"] {
				p, exists := common[child.ID]
				if !exists {
					continue
				}
				fn.Parameters = append(fn.Parameters, SemanticContractParameter{ID: p.ID, Name: p.Name, Type: p.Type, Mode: p.Operation.ParameterMode, Passing: p.Operation.ParameterPassing})
				if p.Operation.ParameterMode == "variadic_positional" || p.Operation.ParameterMode == "variadic_keyword" {
					fn.VariadicMode = p.Operation.ParameterMode
				}
				if p.Operation.ParameterMode == "receiver" {
					fn.Receiver = &SemanticReceiverContract{Type: p.Type, Passing: p.Operation.ParameterPassing, Ownership: p.Type.Ownership, Dispatch: c.Semantics.Dispatch}
				}
			}
			if fn.VariadicMode == "" {
				fn.VariadicMode = "nonvariadic"
			}
			if c.Type.Kind == "function" {
				fn.Results = append(fn.Results, typeResults(c.Type)...)
			}
			fn.CallingSemantics = c.Operation.FunctionBinding
			if fn.CallingSemantics == "" {
				fn.CallingSemantics = "unknown"
			}
			fn.Throws = c.Semantics.ErrorModel
			if fn.Throws == "" {
				fn.Throws = "unknown"
			}
			if err := add(n.ID, "function", SemanticFunctionContractKind, fn); err != nil {
				return nil, err
			}
		}
		if c.Kind == "call" || n.StructuralKind == "CallExpr" {
			call := SemanticCallContract{CallKind: c.Semantics.Dispatch, Arguments: []SemanticCallArgumentContract{}, EvaluationOrder: c.Semantics.EvaluationOrder}
			if call.CallKind == "" {
				call.CallKind = "unknown"
			}
			if call.EvaluationOrder == "" {
				call.EvaluationOrder = d.Evaluation
			}
			if call.EvaluationOrder == "" {
				call.EvaluationOrder = "unknown"
			}
			for _, child := range roles["value"] {
				id := child.ID
				call.CalleeNode = &id
				break
			}
			for _, child := range roles["callee"] {
				id := child.ID
				call.CalleeNode = &id
				break
			}
			for _, child := range roles["receiver"] {
				id := child.ID
				call.ReceiverNode = &id
				break
			}
			for _, child := range roles["argument"] {
				call.Arguments = append(call.Arguments, SemanticCallArgumentContract{NodeID: child.ID, Name: child.Meta.Name, Missing: child.Meta.Missing})
			}
			if c.Operation.CallResolution != nil && c.Operation.CallResolution.Selected != nil {
				selected := *c.Operation.CallResolution.Selected
				call.SelectedCandidate = &selected
			}
			if err := add(n.ID, "call", SemanticCallContractKind, call); err != nil {
				return nil, err
			}
		}
		if c.Type.Kind == "integer" || c.Type.Kind == "float" || c.Type.Kind == "decimal" || c.Operation.Typed != nil {
			numericType := c.Type
			if c.Operation.Typed != nil && c.Operation.Typed.Type.Kind != "" {
				numericType = c.Operation.Typed.Type
			}
			numeric := SemanticNumericContract{Type: numericType, Overflow: c.Semantics.Overflow, Rounding: "unknown", Division: "unknown", NaN: "unknown", SignedZero: "unknown"}
			if numeric.Overflow == "" {
				numeric.Overflow = "unknown"
			}
			if err := add(n.ID, "numeric", SemanticNumericContractKind, numeric); err != nil {
				return nil, err
			}
		}
		if c.Kind == "conversion" || n.StructuralKind == "ConvertExpr" {
			conversion := SemanticConversionContract{TargetType: c.Type, Kind: "unknown", Lossiness: "unknown", Overflow: "unknown", Rounding: "unknown", RuntimeCheck: "unknown"}
			for _, role := range []string{"value", "operand", "base"} {
				for _, child := range roles[role] {
					if source, exists := common[child.ID]; exists {
						conversion.SourceType = source.Type
					}
					break
				}
				if conversion.SourceType.Kind != "" {
					break
				}
			}
			if sameSemanticType(conversion.SourceType, conversion.TargetType) && conversion.SourceType.Kind != "" {
				conversion.Kind = "identity"
			}
			if err := add(n.ID, "conversion", SemanticConversionContractKind, conversion); err != nil {
				return nil, err
			}
		}
		if len(n.Fields["abi_contract"]) != 0 || n.StructuralKind == "ABIContract" || len(n.Fields["calling_convention"]) != 0 {
			abi := SemanticABIContract{CallingConvention: "unknown"}
			if raw := n.Fields["abi_contract"]; len(raw) != 0 {
				var legacy struct {
					Symbol            string         `json:"symbol"`
					CallingConvention string         `json:"calling_convention"`
					Parameters        []SemanticType `json:"parameters"`
					Result            *SemanticType  `json:"result"`
					Results           []SemanticType `json:"results"`
				}
				if json.Unmarshal(raw, &legacy) == nil {
					abi.Symbol, abi.CallingConvention, abi.Parameters, abi.Result, abi.Results = legacy.Symbol, legacy.CallingConvention, legacy.Parameters, legacy.Result, legacy.Results
				}
			}
			if raw := n.Fields["calling_convention"]; len(raw) != 0 {
				_ = json.Unmarshal(raw, &abi.CallingConvention)
			}
			if abi.CallingConvention == "" {
				abi.CallingConvention = "unknown"
			}
			if raw := n.Fields["linkage"]; len(raw) != 0 {
				var linked map[string]json.RawMessage
				if json.Unmarshal(raw, &linked) == nil {
					_ = json.Unmarshal(linked["symbol"], &abi.Symbol)
					if abi.Symbol == "" {
						_ = json.Unmarshal(linked["name"], &abi.Symbol)
					}
				}
			}
			if abi.Symbol != "" {
				abi.External = true
			}
			if err := add(n.ID, "abi", SemanticABIContractKind, abi); err != nil {
				return nil, err
			}
		}
		if c.Kind == "identifier" || n.StructuralKind == "SymbolRef" {
			identity := c.Name
			linkage, visibility, importExport, module, strength := "unknown", "unknown", "unknown", "", "unknown"
			external := false
			if raw := n.Fields["linkage"]; len(raw) != 0 {
				var linked map[string]json.RawMessage
				if json.Unmarshal(raw, &linked) == nil {
					var symbol string
					_ = json.Unmarshal(linked["identity"], &identity)
					_ = json.Unmarshal(linked["symbol"], &symbol)
					if identity == "" {
						identity = symbol
					}
					_ = json.Unmarshal(linked["linkage"], &linkage)
					_ = json.Unmarshal(linked["visibility"], &visibility)
					_ = json.Unmarshal(linked["import_export"], &importExport)
					_ = json.Unmarshal(linked["module"], &module)
					_ = json.Unmarshal(linked["strength"], &strength)
					_ = json.Unmarshal(linked["external"], &external)
				}
			}
			if identity != "" {
				if err := add(n.ID, "symbol", SemanticSymbolContractKind, SemanticSymbolContract{Identity: identity, Linkage: linkage, Visibility: visibility, ImportExport: importExport, Module: module, External: external, Strength: strength}); err != nil {
					return nil, err
				}
			}
		}
	}
	extended, err := deriveExtendedUniversalContractCandidates(d, common, children)
	if err != nil {
		return nil, err
	}
	out = append(out, extended...)
	return out, nil
}

func typeResults(t SemanticType) []SemanticType {
	if len(t.Parameters) == 0 && t.Result == nil {
		return nil
	}
	if t.Result == nil {
		return append([]SemanticType(nil), t.Parameters...)
	}
	return []SemanticType{*t.Result}
}

func sameSemanticType(left, right SemanticType) bool {
	a, _ := json.Marshal(left)
	b, _ := json.Marshal(right)
	return bytes.Equal(a, b)
}

func typeFingerprint(t SemanticType) string {
	raw, _ := canonicalJSONBytes(mustJSON(t))
	return string(raw)
}

func mustJSON(value any) []byte {
	raw, _ := json.Marshal(value)
	return raw
}

func contractForNode[T any](d *UniversalASTDocument, nodeID int, kind SemanticContractKind, out *T) (bool, error) {
	if d == nil {
		return false, nil
	}
	for _, ref := range d.ContractRefs {
		if ref.NodeID != nodeID || ref.ContractID < 0 || ref.ContractID >= len(d.ContractTable) {
			continue
		}
		contract := d.ContractTable[ref.ContractID]
		if contract.Kind != kind {
			continue
		}
		if err := decodeStrictContractPayload(contract, out); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

func validateUniversalContractTable(d *UniversalASTDocument) error {
	if d == nil {
		return nil
	}
	if len(d.ContractTable) == 0 && len(d.ContractRefs) == 0 {
		return nil
	}
	if d.ContractSchema != semanticContractSchema {
		return fmt.Errorf("unsupported semantic contract schema %q", d.ContractSchema)
	}
	seen := map[int]bool{}
	for i, c := range d.ContractTable {
		if c.ID != i || seen[c.ID] {
			return fmt.Errorf("semantic contract IDs must be contiguous and unique")
		}
		seen[c.ID] = true
		if err := validateSemanticContractPayload(c); err != nil {
			return err
		}
	}
	nodes := map[int]bool{}
	for _, n := range d.Nodes {
		nodes[n.ID] = true
	}
	for _, ref := range d.ContractRefs {
		if !nodes[ref.NodeID] || ref.ContractID < 0 || ref.ContractID >= len(d.ContractTable) || strings.TrimSpace(ref.Role) == "" {
			return fmt.Errorf("invalid semantic contract reference node=%d contract=%d", ref.NodeID, ref.ContractID)
		}
	}
	attached := func(nodeID int, kind SemanticContractKind) bool {
		for _, ref := range d.ContractRefs {
			if ref.NodeID == nodeID && ref.ContractID >= 0 && ref.ContractID < len(d.ContractTable) && d.ContractTable[ref.ContractID].Kind == kind {
				return true
			}
		}
		return false
	}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		c, decodeErr := decodeUniversalCommon(n)
		if decodeErr != nil {
			continue
		}
		if c.Kind == "function" || n.StructuralKind == "FunctionDecl" || n.StructuralKind == "ClosureExpr" {
			if !attached(n.ID, SemanticFunctionContractKind) {
				return fmt.Errorf("function node %d lacks interned FUNCTION contract", n.ID)
			}
		}
		if c.Kind == "call" || n.StructuralKind == "CallExpr" {
			if !attached(n.ID, SemanticCallContractKind) {
				return fmt.Errorf("call node %d lacks interned CALL contract", n.ID)
			}
		}
		if c.Type.Kind == "integer" || c.Type.Kind == "float" || c.Type.Kind == "decimal" || c.Operation.Typed != nil {
			if !attached(n.ID, SemanticNumericContractKind) {
				return fmt.Errorf("numeric node %d lacks interned NUMERIC contract", n.ID)
			}
		}
		if c.Kind == "conversion" || n.StructuralKind == "ConvertExpr" {
			if !attached(n.ID, SemanticConversionContractKind) {
				return fmt.Errorf("conversion node %d lacks interned CONVERSION contract", n.ID)
			}
		}
		if len(n.Fields["abi_contract"]) != 0 || n.StructuralKind == "ABIContract" || len(n.Fields["calling_convention"]) != 0 {
			if !attached(n.ID, SemanticABIContractKind) {
				return fmt.Errorf("ABI node %d lacks interned ABI contract", n.ID)
			}
		}
		if c.Kind == "identifier" || n.StructuralKind == "SymbolRef" {
			if c.Name != "" && !attached(n.ID, SemanticSymbolContractKind) {
				return fmt.Errorf("symbol node %d lacks interned SYMBOL contract", n.ID)
			}
		}
	}
	return validateExtendedContractConsumers(d)
}

// validatePhaseOneContractConsumers is the backend-facing consumption gate.
// It proves that interned contracts are attached to the graph objects whose
// execution paths consume them; it does not lower or invent missing facts.
func validatePhaseOneContractConsumers(d *UniversalASTDocument) error {
	if d == nil || len(d.ContractTable) == 0 {
		return nil
	}
	children, err := universalChildrenByRole(d)
	if err != nil {
		return err
	}
	for _, ref := range d.ContractRefs {
		var c universalDecodedCommon
		for i := range d.Nodes {
			if d.Nodes[i].ID == ref.NodeID {
				c, err = decodeUniversalCommon(&d.Nodes[i])
				break
			}
		}
		if err != nil {
			return err
		}
		switch refContract := d.ContractTable[ref.ContractID].Kind; refContract {
		case SemanticFunctionContractKind:
			var payload SemanticFunctionContract
			if _, err := contractForNode(d, ref.NodeID, refContract, &payload); err != nil {
				return err
			}
			if c.Kind == "function" && len(payload.Parameters) != len(children[ref.NodeID]["parameter"]) {
				return fmt.Errorf("FUNCTION contract on node %d disagrees with parameter graph", ref.NodeID)
			}
		case SemanticCallContractKind:
			var payload SemanticCallContract
			if _, err := contractForNode(d, ref.NodeID, refContract, &payload); err != nil {
				return err
			}
			if c.Kind == "call" && len(payload.Arguments) != len(children[ref.NodeID]["argument"]) {
				return fmt.Errorf("CALL contract on node %d disagrees with argument graph", ref.NodeID)
			}
		case SemanticNumericContractKind:
			var payload SemanticNumericContract
			if _, err := contractForNode(d, ref.NodeID, refContract, &payload); err != nil {
				return err
			}
			numericType := c.Type
			if c.Operation.Typed != nil && c.Operation.Typed.Type.Kind != "" {
				numericType = c.Operation.Typed.Type
			}
			if numericType.Kind != "" && payload.Type.Kind != "" && !sameSemanticType(numericType, payload.Type) {
				return fmt.Errorf("NUMERIC contract on node %d disagrees with type fact: node=%s contract=%s", ref.NodeID, typeFingerprint(numericType), typeFingerprint(payload.Type))
			}
		case SemanticConversionContractKind:
			var payload SemanticConversionContract
			if _, err := contractForNode(d, ref.NodeID, refContract, &payload); err != nil {
				return err
			}
			if c.Type.Kind != "" && payload.TargetType.Kind != "" && !sameSemanticType(c.Type, payload.TargetType) {
				return fmt.Errorf("CONVERSION contract on node %d disagrees with target type fact", ref.NodeID)
			}
		case SemanticABIContractKind:
			var payload SemanticABIContract
			if _, err := contractForNode(d, ref.NodeID, refContract, &payload); err != nil {
				return err
			}
			if payload.CallingConvention == "" {
				return fmt.Errorf("ABI contract on node %d has no calling convention state", ref.NodeID)
			}
		case SemanticSymbolContractKind:
			var payload SemanticSymbolContract
			if _, err := contractForNode(d, ref.NodeID, refContract, &payload); err != nil {
				return err
			}
			if payload.Identity == "" {
				return fmt.Errorf("SYMBOL contract on node %d has no semantic identity", ref.NodeID)
			}
		}
	}
	return nil
}
