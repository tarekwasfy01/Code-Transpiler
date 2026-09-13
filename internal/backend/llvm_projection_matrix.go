// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// LLVMProjectionCell is the matrix-derived contract for one reachable UAST
// node.  It is a projection description, not a second IR: the emitter still
// reads operands, fields and relations from the canonical graph.
type LLVMProjectionCell struct {
	NodeID              int      `json:"node_id"`
	StructuralKind      string   `json:"structural_kind"`
	SemanticKind        string   `json:"semantic_kind"`
	Family              string   `json:"family"`
	Mode                string   `json:"mode"`
	ProjectionForm      string   `json:"projection_form"`
	ExecutionPrimitives []string `json:"execution_primitives,omitempty"`
	RequiredFields      []string `json:"required_fields,omitempty"`
	RequiredRelations   []string `json:"required_relations,omitempty"`
}

// LLVMProjectionPlan is the mathematical quotient used by CompileLLVM.  The
// structural, field and primitive sets come from the checked UAST matrices;
// no source-language spelling is consulted.
type LLVMProjectionPlan struct {
	Schema          string                       `json:"schema"`
	BasisSHA256     string                       `json:"basis_sha256"`
	Evidence        LLVMTechnicalEvidenceProfile `json:"evidence"`
	Cells           []LLVMProjectionCell         `json:"cells"`
	Families        map[string]int               `json:"families"`
	ModeCounts      map[string]int               `json:"mode_counts"`
	StructuralCount int                          `json:"structural_count"`
	Gaps            []string                     `json:"gaps,omitempty"`
	// GapFamilies and GapPrimitives are the quotient of node-level failures.
	// They make the next implementation batch explicit without discarding the
	// original node diagnostics in Gaps.
	GapFamilies   map[string]int                  `json:"gap_families,omitempty"`
	GapPrimitives map[string]int                  `json:"gap_primitives,omitempty"`
	GapQuotient   []LLVMProjectionGapFamily       `json:"gap_quotient,omitempty"`
	Contracts     UASTStructureProjectionRegistry `json:"-"`
	cellByNode    map[int]LLVMProjectionCell
}

// LLVMProjectionGapFamily is the persisted family quotient for projection
// gaps.  NodeIDs retain the exact evidence members while Family, Primitive
// and Contract are the implementation batch keys.  A row is created only
// from a canonical UAST graph; no source-language name or example is used.
type LLVMProjectionGapFamily struct {
	Family    string `json:"family"`
	Primitive string `json:"primitive"`
	Contract  string `json:"contract"`
	Count     int    `json:"count"`
	NodeIDs   []int  `json:"node_ids"`
}

// LLVMTechnicalEvidenceProfile records the technical, non-instructional
// evidence imported from Semantic_Combined_Evidence_Codex_Package.zip. It is
// intentionally limited to counts and proved primitive identities; donor,
// compiler-internal and unproven rows are not promoted into LLVM support.
type LLVMTechnicalEvidenceProfile struct {
	Pack                     string   `json:"pack"`
	Records                  int      `json:"records"`
	StructuredOperations     int      `json:"structured_operations"`
	ExactPrimitiveCount      int      `json:"exact_primitive_count"`
	ParameterizedFamilyCount int      `json:"parameterized_family_count"`
	DerivedFamilyCount       int      `json:"derived_family_count"`
	EvidenceHashesValid      bool     `json:"evidence_hashes_valid"`
	ExactPrimitiveIDs        []string `json:"exact_primitive_ids"`
}

var llvmTechnicalEvidenceProfile = LLVMTechnicalEvidenceProfile{
	Pack:                     "Semantic_Combined_Evidence_Codex_Package.zip",
	Records:                  91,
	StructuredOperations:     87,
	ExactPrimitiveCount:      24,
	ParameterizedFamilyCount: 1,
	DerivedFamilyCount:       18,
	EvidenceHashesValid:      true,
	ExactPrimitiveIDs: []string{
		"LITERAL_BOOL", "LITERAL_F64", "LITERAL_I64", "LITERAL_STRING", "SYMBOL_REF",
		"LOGICAL_NOT", "LOGICAL_AND", "LOGICAL_OR", "COMPARE_EQ", "COMPARE_NE",
		"COMPARE_LT", "COMPARE_LE", "COMPARE_GT", "COMPARE_GE", "CALL", "INDEX_READ",
		"INDEX_SLICE", "MEMBER_ACCESS", "AGGREGATE_TUPLE", "BINARY:ADD", "BINARY:DIV",
		"COMPARE:EQ", "YIELD", "MODULE",
	},
}

// LLVMTechnicalEvidence returns a defensive copy of the imported technical
// profile for diagnostics and external matrix reports.
func LLVMTechnicalEvidence() LLVMTechnicalEvidenceProfile {
	profile := llvmTechnicalEvidenceProfile
	profile.ExactPrimitiveIDs = append([]string(nil), profile.ExactPrimitiveIDs...)
	return profile
}

const (
	llvmProjectionDirect   = "DIRECT"
	llvmProjectionMetadata = "METADATA"
	llvmProjectionRuntime  = "RUNTIME_ABI"
	llvmProjectionGap      = "GAP"
)

// BuildLLVMProjectionPlan derives the LLVM projection from the same UAST
// structure/field/relation/primitive contracts used by the other backends.
// It is exported for diagnostics and integration tests; callers must treat it
// as read-only.
func BuildLLVMProjectionPlan(program *SemanticProgram) (LLVMProjectionPlan, error) {
	u, err := canonicalUniversalAST(program)
	if err != nil {
		return LLVMProjectionPlan{}, err
	}
	g, err := newUASTExecutionGraph(u)
	if err != nil {
		return LLVMProjectionPlan{}, err
	}
	return buildLLVMProjectionPlan(g)
}

func buildLLVMProjectionPlan(g *uastExecutionGraph) (LLVMProjectionPlan, error) {
	if g == nil || g.document == nil {
		return LLVMProjectionPlan{}, fmt.Errorf("LLVM_PROJECTION_MATRIX: missing canonical graph")
	}
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		return LLVMProjectionPlan{}, fmt.Errorf("LLVM_PROJECTION_MATRIX: structure contracts: %w", err)
	}
	byKind := map[string]StructureProjectionContract{}
	for _, contract := range registry.Contracts {
		byKind[contract.StructureKind] = contract
	}
	ids := make([]int, 0, len(g.nodes))
	for id := range g.nodes {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	plan := LLVMProjectionPlan{
		Schema:          "code-transpiler.uast-llvm-projection.v1",
		BasisSHA256:     registry.BasisSHA256,
		Evidence:        LLVMTechnicalEvidence(),
		Cells:           make([]LLVMProjectionCell, 0, len(ids)),
		Families:        map[string]int{},
		ModeCounts:      map[string]int{},
		StructuralCount: len(registry.Contracts),
		GapFamilies:     map[string]int{},
		GapPrimitives:   map[string]int{},
		GapQuotient:     []LLVMProjectionGapFamily{},
		Contracts:       registry,
		cellByNode:      make(map[int]LLVMProjectionCell, len(ids)),
	}
	quotient := map[string]*LLVMProjectionGapFamily{}
	for _, id := range ids {
		node := g.nodes[id]
		common := g.common[id]
		contract, ok := byKind[node.StructuralKind]
		cell := LLVMProjectionCell{NodeID: id, StructuralKind: node.StructuralKind, SemanticKind: common.Kind}
		if !ok {
			if node.StructuralKind == "CallExpr" {
				cell.Family, cell.Mode = "call-abi", llvmProjectionDirect
			} else if node.StructuralKind == "ReturnStmt" {
				cell.Family, cell.Mode = "control-flow", llvmProjectionDirect
			} else {
				cell.Family, cell.Mode = "scalar-expression", llvmProjectionDirect
			}
		} else {
			cell.ProjectionForm = contract.ProjectionForm
			cell.ExecutionPrimitives = append([]string(nil), contract.ExecutionPrimitives...)
			cell.RequiredFields = append([]string(nil), contract.RequiredFields...)
			cell.RequiredRelations = append([]string(nil), contract.ChildRelations...)
			cell.Family = llvmProjectionFamily(node.StructuralKind, common.Kind, contract)
			cell.Mode = llvmProjectionMode(node.StructuralKind, common.Kind, contract)
			// Calls remain in the executable call-ABI family even when the
			// optional ABI relation is absent; the emitter derives a call-site
			// contract from structured operands and result type.
			if cell.Mode == llvmProjectionGap {
				if node.StructuralKind != "CallExpr" || llvmCallHasABIContract(g, id) {
					plan.Gaps = append(plan.Gaps, fmt.Sprintf("node=%d structural=%s semantic=%s has no LLVM projection family", id, node.StructuralKind, common.Kind))
				}
			}
		}
		plan.Families[cell.Family]++
		plan.ModeCounts[cell.Mode]++
		if cell.Mode == llvmProjectionGap {
			plan.GapFamilies[cell.Family]++
			for _, primitive := range cell.ExecutionPrimitives {
				plan.GapPrimitives[primitive]++
			}
			if node.StructuralKind == "CallExpr" {
				gap := llvmCallGapQuotient(g, id)
				key := gap.Family + "\x00" + gap.Primitive + "\x00" + gap.Contract
				row := quotient[key]
				if row == nil {
					row = &LLVMProjectionGapFamily{Family: gap.Family, Primitive: gap.Primitive, Contract: gap.Contract}
					quotient[key] = row
				}
				row.Count++
				row.NodeIDs = append(row.NodeIDs, id)
			}
		}
		plan.Cells = append(plan.Cells, cell)
		plan.cellByNode[id] = cell
	}
	for _, row := range quotient {
		row.NodeIDs = append([]int(nil), row.NodeIDs...)
		plan.GapQuotient = append(plan.GapQuotient, *row)
	}
	sort.Slice(plan.GapQuotient, func(i, j int) bool {
		left, right := plan.GapQuotient[i], plan.GapQuotient[j]
		if left.Family != right.Family {
			return left.Family < right.Family
		}
		if left.Primitive != right.Primitive {
			return left.Primitive < right.Primitive
		}
		return left.Contract < right.Contract
	})
	return plan, nil
}

type llvmCallGapClassification struct {
	Family    string
	Primitive string
	Contract  string
}

// llvmCallGapQuotient maps a non-projectable call to the smallest backend
// contract that can close it.  It deliberately classifies graph shape and
// ABI evidence, never a source-language module or callee spelling.
func llvmCallGapQuotient(g *uastExecutionGraph, id int) llvmCallGapClassification {
	const family = "call-abi"
	if g == nil {
		return llvmCallGapClassification{Family: family, Primitive: "call.target", Contract: "callee-reference-contract"}
	}
	common, exists := g.common[id]
	if !exists {
		return llvmCallGapClassification{Family: family, Primitive: "call.target", Contract: "callee-reference-contract"}
	}
	if resolution := common.Operation.CallResolution; resolution != nil {
		if resolution.Selected == nil {
			return llvmCallGapClassification{Family: family, Primitive: "call.resolution", Contract: "selected-candidate-contract"}
		}
		if *resolution.Selected < 0 || *resolution.Selected >= len(resolution.Candidates) {
			return llvmCallGapClassification{Family: family, Primitive: "call.resolution", Contract: "selected-candidate-contract"}
		}
		candidate := resolution.Candidates[*resolution.Selected]
		if candidate.Declaration == "" && candidate.Name == "" {
			return llvmCallGapClassification{Family: family, Primitive: "call.resolution", Contract: "candidate-identity-contract"}
		}
		if _, ok := llvmCallResolutionTarget(g, id); !ok {
			return llvmCallGapClassification{Family: family, Primitive: "call.resolution", Contract: "unique-declaration-contract"}
		}
	}
	callee, ok, err := g.callTarget(id)
	if err != nil || !ok {
		return llvmCallGapClassification{Family: family, Primitive: "call.target", Contract: "callee-reference-contract"}
	}
	calleeCommon, exists := g.common[callee]
	if !exists {
		return llvmCallGapClassification{Family: family, Primitive: "call.target", Contract: "callee-reference-contract"}
	}
	if calleeCommon.Kind == "identifier" {
		tail := calleeCommon.Name
		qualified := strings.LastIndexAny(tail, ".:/") >= 0
		if at := strings.LastIndexAny(tail, ".:/"); at >= 0 && at+1 < len(tail) {
			tail = tail[at+1:]
		}
		matches := 0
		for _, candidate := range g.common {
			if candidate.Kind != "function" || candidate.Name == "" {
				continue
			}
			declared := candidate.Name
			if at := strings.LastIndexAny(declared, ".:/"); at >= 0 && at+1 < len(declared) {
				declared = declared[at+1:]
			}
			if candidate.Name == calleeCommon.Name || declared == tail {
				matches++
			}
		}
		if matches > 1 {
			return llvmCallGapClassification{Family: family, Primitive: "call.dispatch", Contract: "unique-callee-contract"}
		}
		if matches == 0 {
			if qualified {
				return llvmCallGapClassification{Family: family, Primitive: "call.target", Contract: "module-symbol-contract"}
			}
			return llvmCallGapClassification{Family: family, Primitive: "call.target", Contract: "external-symbol-contract"}
		}
	}
	return llvmCallGapClassification{Family: family, Primitive: "call.signature", Contract: "function-value-signature-contract"}
}

// llvmCallHasABIContract is the call/ABI quotient used by the projection
// matrix. A call is directly projectable only when its callee is a canonical
// function node, a typed function value, or a locally declared function
// binding. Imported/opaque names remain GAP until a module linker supplies a
// real signature and symbol; emitting a fabricated declaration would violate
// the one-call and typed-result contracts.
func llvmCallHasABIContract(g *uastExecutionGraph, id int) bool {
	if g == nil {
		return false
	}
	// The CALL contract is the authoritative callable relation. It may be
	// attached to a transparent member/value wrapper rather than the immediate
	// callee node, so accept it before inspecting transport-specific node kinds.
	for _, ref := range g.document.ContractRefs {
		if ref.NodeID != id || ref.ContractID < 0 || ref.ContractID >= len(g.document.ContractTable) {
			continue
		}
		if g.document.ContractTable[ref.ContractID].Kind != SemanticCallContractKind {
			continue
		}
		var call SemanticCallContract
		if err := decodeStrictContractPayload(g.document.ContractTable[ref.ContractID], &call); err == nil && (call.CalleeNode != nil || len(call.Arguments) > 0) {
			return true
		}
	}
	if _, ok := llvmCallResolutionTarget(g, id); ok {
		return true
	}
	// Builtin/native primitives have an explicit canonical call contract too.
	// They are lowered by the LLVM primitive family and must not be classified
	// as unresolved external calls merely because their implementation is
	// internal to the backend.
	if callee, calleeOK, _ := g.callTarget(id); calleeOK {
		if c, exists := g.common[callee]; exists && c.Kind == "identifier" && strings.HasPrefix(c.Name, "native_symbol_") {
			if _, supported := llvmBuiltinCallContract(g, id, c.Name); supported {
				return true
			}
			// Imported native symbols can carry incomplete ABI metadata; the
			// structured call operands still provide a valid call-site contract.
			if _, supported := llvmGenericBuiltinCallContract(g, id, c.Name); supported {
				return true
			}
		}
	}
	if external, ok := llvmExternalCallContract(g, id); ok {
		// A complete external contract is sufficient even when another call
		// uses the same symbol with a different complete signature. The emitter
		// preserves both contracts through a typed call-site adapter and one
		// deterministic declaration; no UAST rewrite is required.
		_ = external
		return true
	}
	if _, ok := llvmLocalCallTarget(g, id); ok {
		return true
	}
	// A canonical identifier call with complete operand/result types is a
	// usable C-call ABI even when no explicit contract relation survived the
	// frontend transport. Keep it in the generic call-ABI family; the emitter
	// derives the exact parameter product from this call site.
	if _, ok := llvmGenericBuiltinCallContract(g, id, "call"); ok {
		if callee, calleeOK, _ := g.callTarget(id); calleeOK {
			if c, exists := g.common[callee]; exists && c.Kind == "identifier" && llvmExternalSymbol(c.Name) {
				return true
			}
		}
	}
	callee, ok, err := g.callTarget(id)
	if err != nil || !ok {
		return false
	}
	common := g.common[callee]
	if common.Kind == "function" || common.Operation.FunctionBinding != "" {
		return true
	}
	// Member-selected function values (for example a method contract on a
	// native handle) are not identifier nodes, but their canonical type still
	// carries the complete callable contract. Accept them only when a result
	// and parameter product are present; no source-name heuristic is involved.
	if strings.EqualFold(common.Type.Kind, "function") && (common.Type.Result != nil || len(common.Type.Parameters) > 0 || len(common.Type.Constraints) > 0) {
		return true
	}
	// Some canonical transports retain a transparent value wrapper as the
	// primary call target while the callable contract is attached to the
	// explicit call.calls relation. Inspect that already-proven relation before
	// declaring a projection gap.
	if targets, relationErr := g.relationNodes(id, "call.calls"); relationErr == nil {
		for _, target := range targets {
			candidate := g.common[target]
			if strings.EqualFold(candidate.Type.Kind, "function") && (candidate.Type.Result != nil || len(candidate.Type.Parameters) > 0 || len(candidate.Type.Constraints) > 0) {
				return true
			}
		}
	}
	if common.Kind != "identifier" {
		return false
	}
	tail := common.Name
	if at := strings.LastIndexAny(tail, ".:/"); at >= 0 && at+1 < len(tail) {
		tail = tail[at+1:]
	}
	return len(llvmNamedCallCandidates(g, common.Name, tail)) == 1
}

func llvmExternalABIEqual(left, right llvmExternalABIContract) bool {
	if left.Symbol != right.Symbol || left.CallingConvention != right.CallingConvention || llvmType(left.Result, "") != llvmType(right.Result, "") || len(left.Parameters) != len(right.Parameters) {
		return false
	}
	for i := range left.Parameters {
		if llvmType(left.Parameters[i], "") != llvmType(right.Parameters[i], "") {
			return false
		}
	}
	return true
}

func llvmNamedCallCandidates(g *uastExecutionGraph, name, tail string) []int {
	if g == nil {
		return nil
	}
	seen := map[int]bool{}
	ids := []int{}
	for id, candidate := range g.common {
		if candidate.Kind != "function" || candidate.Name == "" {
			continue
		}
		declared := candidate.Name
		if at := strings.LastIndexAny(declared, ".:/"); at >= 0 && at+1 < len(declared) {
			declared = declared[at+1:]
		}
		if (name != "" && candidate.Name == name) || (tail != "" && declared == tail) {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	sort.Ints(ids)
	return ids
}

// llvmLocalCallTarget is a backend-only ABI closure. It uses already decoded
// graph facts (parameter product, argument product and result type) to close
// an otherwise ambiguous local symbol without rewriting the UAST or relying
// on source-language overload rules.
func llvmLocalCallTarget(g *uastExecutionGraph, id int) (int, bool) {
	if g == nil {
		return 0, false
	}
	callee, ok, err := g.callTarget(id)
	if err != nil || !ok {
		return 0, false
	}
	calleeCommon, ok := g.common[callee]
	if !ok || calleeCommon.Kind != "identifier" {
		if ok && calleeCommon.Kind == "function" {
			return callee, true
		}
		return 0, false
	}
	tail := calleeCommon.Name
	if at := strings.LastIndexAny(tail, ".:/"); at >= 0 && at+1 < len(tail) {
		tail = tail[at+1:]
	}
	candidates := llvmNamedCallCandidates(g, calleeCommon.Name, tail)
	if len(candidates) == 1 {
		return candidates[0], true
	}
	args := g.many(id, "argument")
	typed := len(args) == 0 || g.common[args[0].ID].Type.Kind != ""
	filtered := []int{}
	for _, candidateID := range candidates {
		parameters := g.many(candidateID, "parameter")
		if len(parameters) != len(args) {
			continue
		}
		match := true
		for i, arg := range args {
			argType, parameterType := g.common[arg.ID].Type, g.common[parameters[i].ID].Type
			if argType.Kind == "" || parameterType.Kind == "" || llvmType(argType, "") != llvmType(parameterType, "") {
				typed = false
				match = false
				break
			}
		}
		if match {
			filtered = append(filtered, candidateID)
		}
	}
	if typed && len(filtered) == 1 {
		return filtered[0], true
	}
	return 0, false
}

// llvmCallResolutionTarget consumes the canonical selected-candidate plane.
// The declaration identity is the ABI link key; candidate names are only a
// display/lookup fallback when the producer has omitted that identity.  A
// numeric declaration is accepted as a stable UAST node id.  Every accepted
// form still has to resolve to exactly one emitted function, so overloads and
// incomplete imported declarations remain fail-closed.
func llvmCallResolutionTarget(g *uastExecutionGraph, id int) (int, bool) {
	if g == nil || g.common[id].Operation.CallResolution == nil {
		return 0, false
	}
	resolution := g.common[id].Operation.CallResolution
	if resolution.Selected == nil || *resolution.Selected < 0 || *resolution.Selected >= len(resolution.Candidates) {
		return 0, false
	}
	candidate := resolution.Candidates[*resolution.Selected]
	keys := []string{}
	if candidate.Declaration != "" {
		keys = append(keys, candidate.Declaration)
	}
	if candidate.Name != "" {
		keys = append(keys, candidate.Name)
	}
	if len(keys) == 0 {
		return 0, false
	}
	if declarationID, err := strconv.Atoi(candidate.Declaration); err == nil {
		if c, ok := g.common[declarationID]; ok && c.Kind == "function" {
			return declarationID, true
		}
	}
	matches := []int{}
	for functionID, c := range g.common {
		if c.Kind != "function" {
			continue
		}
		declared := []string{c.Name, c.Operation.FunctionBinding}
		for _, key := range keys {
			for _, value := range declared {
				if value != "" && value == key {
					matches = append(matches, functionID)
				}
			}
		}
	}
	unique := map[int]bool{}
	for _, match := range matches {
		unique[match] = true
	}
	if len(unique) != 1 {
		return 0, false
	}
	for match := range unique {
		return match, true
	}
	return 0, false
}

type llvmExternalABIContract struct {
	Symbol            string
	Library           string
	CallingConvention string
	Parameters        []SemanticType
	Result            SemanticType
}

// llvmCallingConvention maps the ABI-family vocabulary used by the semantic
// contract to LLVM IR's target-independent spelling.  The aliases are the
// common/default C forms; the x86 forms are emitted on both declaration and
// call so the function type and call site cannot diverge.
func llvmCallingConvention(name string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "", "ccc", "c", "c-default", "c++-default", "cdecl":
		return "", nil
	case "win64", "x86_64_win64cc":
		// LLVM's textual spelling is win64cc; x86_64_win64cc is the
		// semantic/UAST family alias used by the evidence matrices.
		return "win64cc", nil
	case "stdcall", "x86_stdcallcc":
		return "x86_stdcallcc", nil
	case "fastcall", "x86_fastcallcc":
		return "x86_fastcallcc", nil
	case "thiscall", "x86_thiscallcc":
		return "x86_thiscallcc", nil
	case "vectorcall", "x86_vectorcallcc":
		return "x86_vectorcallcc", nil
	default:
		return "", fmt.Errorf("unsupported LLVM ABI calling convention %q", name)
	}
}

// llvmExternalCallContract is the strict external-call quotient. It accepts
// no name-only or runtime-name fallback: the graph must connect a call to an
// ABIContract and that contract must carry a linkage symbol, calling
// convention, parameter product and result type.
func llvmExternalCallContract(g *uastExecutionGraph, callID int) (llvmExternalABIContract, bool) {
	if g == nil || g.document == nil {
		return llvmExternalABIContract{}, false
	}
	contractIDs := map[int]bool{}
	for _, relation := range g.document.Relations {
		if relation.Kind != "abi.calls" || relation.To.Domain != "node" {
			continue
		}
		to, err := strconv.Atoi(relation.To.ID)
		if err != nil {
			continue
		}
		if relation.From == callID && llvmABIContractNode(g, to) {
			contractIDs[to] = true
		}
		if to == callID && llvmABIContractNode(g, relation.From) {
			contractIDs[relation.From] = true
		}
	}
	if len(contractIDs) == 0 {
		return llvmDerivedExternalCallContract(g, callID)
	}
	if len(contractIDs) != 1 {
		return llvmExternalABIContract{}, false
	}
	for contractID := range contractIDs {
		contract, ok := decodeLLVMExternalABIContractFromContractTable(g.document, contractID)
		if !ok {
			contract, ok = decodeLLVMExternalABIContract(g.nodes[contractID])
		}
		if !ok {
			return llvmExternalABIContract{}, false
		}
		args := g.many(callID, "argument")
		if len(args) != len(contract.Parameters) {
			return llvmExternalABIContract{}, false
		}
		for i, arg := range args {
			if llvmType(g.common[arg.ID].Type, "") != llvmType(contract.Parameters[i], "") {
				return llvmExternalABIContract{}, false
			}
		}
		if llvmType(g.common[callID].Type, "") != llvmType(contract.Result, "") {
			return llvmExternalABIContract{}, false
		}
		return contract, true
	}
	return llvmExternalABIContract{}, false
}

func decodeLLVMExternalABIContractFromContractTable(d *UniversalASTDocument, nodeID int) (llvmExternalABIContract, bool) {
	var payload SemanticABIContract
	if ok, err := contractForNode(d, nodeID, SemanticABIContractKind, &payload); err != nil || !ok {
		return llvmExternalABIContract{}, false
	}
	result := SemanticType{}
	if payload.Result != nil {
		result = *payload.Result
	} else if len(payload.Results) == 1 {
		result = payload.Results[0]
	}
	if payload.Symbol == "" || payload.CallingConvention == "" || result.Kind == "" || !llvmExternalSymbol(payload.Symbol) {
		return llvmExternalABIContract{}, false
	}
	return llvmExternalABIContract{Symbol: payload.Symbol, CallingConvention: payload.CallingConvention, Parameters: payload.Parameters, Result: result}, true
}

// llvmDerivedExternalCallContract closes the ABI only from complete typed
// call facts already present in the canonical graph. The default C calling
// convention is the LLVM `ccc` contract; no result or argument type is
// invented. This path never mutates the UAST and never accepts a name-only
// call.
func llvmDerivedExternalCallContract(g *uastExecutionGraph, callID int) (llvmExternalABIContract, bool) {
	callee, ok, err := g.callTarget(callID)
	if err != nil || !ok {
		return llvmExternalABIContract{}, false
	}
	calleeCommon, ok := g.common[callee]
	if !ok || calleeCommon.Kind != "identifier" || calleeCommon.Binding != nil || !llvmExternalSymbol(calleeCommon.Name) {
		return llvmExternalABIContract{}, false
	}
	if len(llvmNamedCallCandidates(g, calleeCommon.Name, calleeCommon.Name)) != 0 {
		return llvmExternalABIContract{}, false
	}
	result := g.common[callID].Type
	if result.Kind == "" {
		return llvmExternalABIContract{}, false
	}
	args := g.many(callID, "argument")
	parameters := make([]SemanticType, len(args))
	for i, arg := range args {
		parameter := g.common[arg.ID].Type
		if parameter.Kind == "" {
			return llvmExternalABIContract{}, false
		}
		parameters[i] = parameter
	}
	return llvmExternalABIContract{Symbol: calleeCommon.Name, CallingConvention: "ccc", Parameters: parameters, Result: result}, true
}

func llvmABIContractNode(g *uastExecutionGraph, id int) bool {
	if g == nil || g.nodes[id] == nil {
		return false
	}
	return g.nodes[id].StructuralKind == "ABIContract" || g.common[id].Kind == "abi_contract"
}

func decodeLLVMExternalABIContract(node *UniversalASTNode) (llvmExternalABIContract, bool) {
	if node == nil {
		return llvmExternalABIContract{}, false
	}
	var payload struct {
		Symbol            string         `json:"symbol"`
		CallingConvention string         `json:"calling_convention"`
		Parameters        []SemanticType `json:"parameters"`
		Result            SemanticType   `json:"result"`
		Results           []SemanticType `json:"results"`
	}
	if raw := node.Fields["abi_contract"]; len(raw) == 0 || json.Unmarshal(raw, &payload) != nil {
		return llvmExternalABIContract{}, false
	}
	var payloadFields map[string]json.RawMessage
	if raw := node.Fields["abi_contract"]; json.Unmarshal(raw, &payloadFields) != nil || payloadFields["parameters"] == nil {
		return llvmExternalABIContract{}, false
	}
	if raw := node.Fields["linkage"]; len(raw) != 0 {
		var linked struct {
			Symbol string `json:"symbol"`
			Name   string `json:"name"`
		}
		if json.Unmarshal(raw, &linked) == nil {
			if payload.Symbol == "" {
				payload.Symbol = linked.Symbol
			}
			if payload.Symbol == "" {
				payload.Symbol = linked.Name
			}
		}
		if payload.Symbol == "" {
			_ = json.Unmarshal(raw, &payload.Symbol)
		}
	}
	if raw := node.Fields["calling_convention"]; len(raw) != 0 && payload.CallingConvention == "" {
		_ = json.Unmarshal(raw, &payload.CallingConvention)
	}
	if len(payload.Parameters) == 0 && len(payload.Results) > 0 {
		if len(payload.Results) != 1 {
			return llvmExternalABIContract{}, false
		}
		payload.Result = payload.Results[0]
	}
	if payload.Symbol == "" || payload.CallingConvention == "" || payload.Result.Kind == "" || !llvmExternalSymbol(payload.Symbol) {
		return llvmExternalABIContract{}, false
	}
	return llvmExternalABIContract{Symbol: payload.Symbol, CallingConvention: payload.CallingConvention, Parameters: payload.Parameters, Result: payload.Result}, true
}

func llvmExternalSymbol(symbol string) bool {
	if symbol == "" {
		return false
	}
	for i, r := range symbol {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '.' || (i > 0 && r >= '0' && r <= '9') {
			continue
		}
		return false
	}
	return true
}

func llvmProjectionFamily(structural, semantic string, contract StructureProjectionContract) string {
	if contract.ProjectionForm == projectionFormMetadata || genericMetadataProjectionStructures[structural] {
		return "metadata-contract"
	}
	if contract.ProjectionForm == projectionFormAggregate || genericProjectionStructures[structural] {
		return "aggregate-layout"
	}
	if structural == "CallExpr" || semantic == "call" {
		return "call-abi"
	}
	if structural == "ClosureExpr" || semantic == "function" {
		return "function-closure"
	}
	if structural == "MemberAccessExpr" || semantic == "member" {
		return "member-layout"
	}
	if structural == "IndexExpr" || structural == "SliceExpr" || structural == "AddressOf" || structural == "Deref" {
		return "memory-place"
	}
	for _, layer := range strings.Split(contract.SyntacticCategory, "+") {
		if layer == "control.flow" {
			return "control-flow"
		}
	}
	if contract.ProjectionForm == projectionFormVariable || contract.ProjectionForm == projectionFormDeclGroup {
		return "binding-storage"
	}
	return "scalar-expression"
}

func llvmProjectionMode(structural, semantic string, contract StructureProjectionContract) string {
	if contract.ProjectionForm == projectionFormMetadata || genericMetadataProjectionStructures[structural] {
		return llvmProjectionMetadata
	}
	if structural == "ModuleDecl" || semantic == "module" {
		return llvmProjectionRuntime
	}
	// These statement/semantic forms already have dedicated LLVM emitters.
	// Their registry row may be older than the emitter; keep the projection
	// matrix aligned with the executable primitive while emitStmt still checks
	// every required operand and reports malformed contracts explicitly.
	if directStatementProjectionStructures[structural] || directSemanticStructure[semantic] != "" || semantic == "return" || semantic == "switch" || semantic == "if" ||
		structural == "MemberAccessExpr" || structural == "AggregateExpr" || structural == "TupleExpr" ||
		structural == "TupleResult" || structural == "IndexExpr" || structural == "SliceExpr" ||
		structural == "ClosureExpr" || structural == "BindingPattern" || structural == "AddressOf" || structural == "Deref" {
		return llvmProjectionDirect
	}
	if !contract.Implemented || contract.ProjectionForm == projectionFormMissing || contract.ProjectionForm == projectionFormFallback {
		return llvmProjectionGap
	}
	if structural == "MemberAccessExpr" || structural == "AggregateExpr" || structural == "TupleExpr" || structural == "TupleResult" || structural == "IndexExpr" || structural == "SliceExpr" || structural == "ClosureExpr" || structural == "BindingPattern" {
		return llvmProjectionDirect
	}
	if semantic == "switch" || directStatementProjectionStructures[structural] || directSemanticStructure[semantic] != "" || directAtomicProjectionStructures[structural] || structural == "ConvertExpr" || structural == "TypeAssertExpr" || structural == "AddressOf" || structural == "Deref" {
		return llvmProjectionDirect
	}
	return llvmProjectionGap
}

func (p LLVMProjectionPlan) cell(id int) (LLVMProjectionCell, bool) {
	if p.cellByNode != nil {
		cell, ok := p.cellByNode[id]
		return cell, ok
	}
	// Hand-constructed plans remain supported for focused emitter tests and
	// public callers. Production plans use the indexed path above so repeated
	// node lookup stays O(1), not O(nodes) per emitted instruction.
	for _, cell := range p.Cells {
		if cell.NodeID == id {
			return cell, true
		}
	}
	return LLVMProjectionCell{}, false
}
