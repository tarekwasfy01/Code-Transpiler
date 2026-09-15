// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// ExecutableClosure is a compact, backend-neutral reachability result. It
// contains identities and contracts only; unit UAST bodies remain owned by
// their SemanticCompilationUnit and are never merged here.
type ExecutableClosure struct {
	EntryRoots                []string                      `json:"entry_roots"`
	ReachableFunctions        []ExecutableFunctionRef       `json:"reachable_functions"`
	ReachableFunctionCount    int                           `json:"reachable_functions_count"`
	ReachableUnits            []string                      `json:"reachable_units"`
	ReachableUnitCount        int                           `json:"reachable_units_count"`
	RequiredInitializers      []string                      `json:"required_initializers"`
	InitializerFunctions      []ExecutableFunctionRef       `json:"initializer_functions"`
	InitializerOrderBasis     string                        `json:"initializer_order_basis,omitempty"`
	RequiredRuntimePrimitives []string                      `json:"runtime_primitives"`
	RequiredExternalImports   []ProjectExternalImport       `json:"external_imports"`
	Unresolved                []ExecutableClosureUnresolved `json:"unresolved"`
	Edges                     []ExecutableClosureEdge       `json:"edges,omitempty"`
}

type ExecutableFunctionRef struct {
	ID     string `json:"id"`
	Name   string `json:"name,omitempty"`
	UnitID string `json:"unit_id"`
	NodeID int    `json:"node_id,omitempty"`
}

type ExecutableClosureUnresolved struct {
	Caller             string   `json:"caller,omitempty"`
	UnitID             string   `json:"unit_id,omitempty"`
	NodeID             int      `json:"node_id,omitempty"`
	Callee             string   `json:"callee,omitempty"`
	SemanticIdentity   string   `json:"semantic_identity,omitempty"`
	ExpectedSignature  string   `json:"expected_signature,omitempty"`
	Reason             string   `json:"reason"`
	ResolutionAttempts []string `json:"resolution_attempts"`
}

type ExecutableClosureEdge struct {
	CallerID       string                 `json:"caller_id"`
	CallerUnit     string                 `json:"caller_unit"`
	Callee         string                 `json:"callee"`
	NodeID         int                    `json:"node_id"`
	Resolution     string                 `json:"resolution"`
	TargetID       string                 `json:"target_id,omitempty"`
	ExternalImport *ProjectExternalImport `json:"external_import,omitempty"`
}

func localProjectFunctionLabel(unitID string, nodeID int) string {
	return projectFunctionLabel(fmt.Sprintf("%s#local:%d", unitID, nodeID))
}

func projectFunctionHasBody(g *uastExecutionGraph, nodeID int) bool {
	if g == nil || g.nodes[nodeID] == nil {
		return false
	}
	for _, role := range []string{"body", "statement", "block"} {
		if len(g.many(nodeID, role)) > 0 {
			return true
		}
	}
	return false
}

func semanticFunctionTypeFromGraph(g *uastExecutionGraph, nodeID int, name string) SemanticType {
	common, ok := g.common[nodeID]
	if !ok {
		return SemanticType{}
	}
	params := make([]SemanticType, 0)
	for _, parameter := range g.many(nodeID, "parameter") {
		params = append(params, g.common[parameter.ID].Type)
	}
	return SemanticType{Kind: "function", Name: name, Parameters: params, Result: common.Type.Result, TypeOrigin: "derived"}
}

func collectExecutableUnitFacts(g *uastExecutionGraph, u *UniversalASTDocument, programExtensions map[string]any, unitID, packageName string, bindingNames map[string]string, summary *SemanticUnitSummary) error {
	if g == nil || u == nil || summary == nil {
		return fmt.Errorf("missing UAST graph or unit summary")
	}
	functionNodeIDs := make([]int, 0)
	globalNodeIDs := make([]int, 0)
	for id, common := range g.common {
		if common.Kind == "function" {
			functionNodeIDs = append(functionNodeIDs, id)
		}
		kind := strings.ToLower(strings.TrimSpace(g.nodes[id].StructuralKind))
		if kind == "globaldecl" || kind == "variabledecl" || kind == "vardecl" {
			globalNodeIDs = append(globalNodeIDs, id)
		}
	}
	sort.Ints(functionNodeIDs)
	sort.Ints(globalNodeIDs)
	functionIDByNode := make(map[int]string, len(functionNodeIDs))
	for _, nodeID := range functionNodeIDs {
		name := canonicalFunctionBindingName(g, nodeID, bindingNames)
		local := name == ""
		id := ""
		if local {
			id = localProjectFunctionLabel(unitID, nodeID)
		} else {
			qualified := name
			if packageName != "" && !strings.Contains(qualified, ".") {
				qualified = packageName + "." + qualified
			}
			id = projectFunctionLabel(qualified)
		}
		common := g.common[nodeID]
		fnType := semanticFunctionTypeFromGraph(g, nodeID, name)
		body := projectFunctionHasBody(g, nodeID)
		external := projectExternalImportForNode(g, nodeID)
		binding := strings.TrimSpace(common.Operation.FunctionBinding)
		if binding == "" {
			for candidate, sourceName := range bindingNames {
				if sourceName == name {
					binding = candidate
					break
				}
			}
		}
		fn := ProjectFunctionSummary{ID: id, Name: name, Binding: binding, Type: fnType, ABI: "win64", NodeID: nodeID, HasBody: body, Local: local, External: external}
		found := false
		for i := range summary.Functions {
			if summary.Functions[i].ID == id {
				// The graph node is the executable declaration proof. Preserve any
				// already-published signature when this node has no richer facts.
				if fnType.Kind != "" {
					summary.Functions[i].Type = fnType
				}
				if binding != "" {
					summary.Functions[i].Binding = binding
				}
				summary.Functions[i].NodeID = nodeID
				summary.Functions[i].HasBody = body
				summary.Functions[i].Local = local
				summary.Functions[i].External = external
				found = true
				break
			}
		}
		if !found {
			summary.Functions = append(summary.Functions, fn)
		}
		functionIDByNode[nodeID] = id
		if !local {
			for i := range summary.Symbols {
				if summary.Symbols[i].ID == id && summary.Symbols[i].Kind == "function" {
					summary.Symbols[i].Type = fnType
					summary.Symbols[i].External = external
				}
			}
		}
	}
	globalIDByNode := make(map[int]string, len(globalNodeIDs))
	for _, nodeID := range globalNodeIDs {
		name := g.common[nodeID].Name
		if name == "" {
			name = universalNodeName(g.nodes[nodeID])
		}
		if name == "" {
			continue
		}
		qualified := name
		if packageName != "" && !strings.Contains(qualified, ".") {
			qualified = packageName + "." + qualified
		}
		id := projectGlobalLabel(qualified)
		globalIDByNode[nodeID] = id
		for i := range summary.Globals {
			if summary.Globals[i].ID == id {
				summary.Globals[i].NodeID = nodeID
				summary.Globals[i].Type = g.common[nodeID].Type
			}
		}
		for i := range summary.Symbols {
			if summary.Symbols[i].ID == id && summary.Symbols[i].Kind == "global" {
				summary.Symbols[i].Type = g.common[nodeID].Type
			}
		}
	}

	// Walk each function body once in its own lexical scope. Nested function
	// bodies are visited under their own function identity rather than being
	// incorrectly attributed to the enclosing caller.
	for _, functionNode := range functionNodeIDs {
		callerID := functionIDByNode[functionNode]
		visited := map[int]bool{}
		var walk func(int) error
		walk = func(nodeID int) error {
			if visited[nodeID] {
				return nil
			}
			visited[nodeID] = true
			common, exists := g.common[nodeID]
			if !exists {
				return fmt.Errorf("function node %d references missing child node %d", functionNode, nodeID)
			}
			if nodeID != functionNode && common.Kind == "function" {
				return nil
			}
			if common.Kind == "call" {
				call, err := summarizeProjectCall(g, nodeID, callerID, functionIDByNode, bindingNames)
				if err != nil {
					return err
				}
				summary.Calls = append(summary.Calls, call)
				summary.Imports = append(summary.Imports, ProjectSymbolReference{ID: call.TargetID, Name: call.TargetName, Kind: "function", Caller: callerID, NodeID: call.NodeID})
			}
			if common.Kind == "identifier" || common.Kind == "symbol_ref" {
				if common.Binding != nil {
					if targetID := globalIDByNode[*common.Binding]; targetID != "" {
						summary.DataRefs = append(summary.DataRefs, ProjectDataReference{CallerID: callerID, NodeID: nodeID, TargetID: targetID, Name: common.Name})
						summary.Imports = append(summary.Imports, ProjectSymbolReference{ID: targetID, Name: common.Name, Kind: "global", Caller: callerID, NodeID: nodeID})
					}
				}
			}
			roles := make([]string, 0, len(g.children[nodeID]))
			for role := range g.children[nodeID] {
				roles = append(roles, role)
			}
			sort.Strings(roles)
			for _, role := range roles {
				for _, child := range g.children[nodeID][role] {
					if err := walk(child.ID); err != nil {
						return err
					}
				}
			}
			return nil
		}
		if err := walk(functionNode); err != nil {
			return err
		}
	}
	collectProjectInitializers(u, programExtensions, unitID, bindingNames, summary)
	dedupeProjectExecutableFacts(summary)
	return nil
}

func universalNodeName(node *UniversalASTNode) string {
	if node == nil {
		return ""
	}
	for _, raw := range []json.RawMessage{node.Fields["name"], node.Attributes["name"]} {
		var name string
		if len(raw) > 0 && json.Unmarshal(raw, &name) == nil && name != "" {
			return name
		}
	}
	return ""
}

func summarizeProjectCall(g *uastExecutionGraph, callNode int, callerID string, functionIDs map[int]string, bindingNames map[string]string) (ProjectCallSummary, error) {
	call := ProjectCallSummary{CallerID: callerID, NodeID: callNode, Signature: projectCallSignature(g, callNode)}
	common := g.common[callNode]
	call.Dispatch = strings.TrimSpace(common.Semantics.Dispatch)
	callee, ok, err := g.callTarget(callNode)
	if err != nil {
		return call, err
	}
	if !ok {
		return call, nil
	}
	target := g.common[callee]
	call.TargetID = functionIDs[callee]
	call.TargetName = target.Name
	if target.Operation.FunctionBinding != "" {
		call.TargetBinding = target.Operation.FunctionBinding
		if bindingNames[call.TargetBinding] != "" {
			call.TargetName = bindingNames[call.TargetBinding]
		}
	}
	if target.Binding != nil {
		call.TargetBinding = strconv.Itoa(*target.Binding)
		if bound, exists := g.common[*target.Binding]; exists {
			if bound.Name != "" {
				call.TargetName = bound.Name
			}
			if boundID := functionIDs[*target.Binding]; boundID != "" {
				call.TargetID = boundID
			}
		}
	}
	if bindingNames[call.TargetName] != "" {
		call.TargetBinding = call.TargetName
		call.TargetName = bindingNames[call.TargetName]
	}
	if call.Dispatch == "" {
		call.Dispatch = strings.TrimSpace(target.Semantics.Dispatch)
	}
	call.ExternalImport = projectExternalImportForCall(g, callNode, callee)
	return call, nil
}

func projectCallSignature(g *uastExecutionGraph, callNode int) SemanticType {
	if g == nil {
		return SemanticType{}
	}
	result := g.common[callNode].Type
	var resultPtr *SemanticType
	if !isUnknownSemanticType(result) {
		resultCopy := result
		resultPtr = &resultCopy
	}
	params := make([]SemanticType, 0)
	for _, arg := range g.many(callNode, "argument") {
		t := g.common[arg.ID].Type
		if isUnknownSemanticType(t) {
			params = append(params, SemanticType{})
			continue
		}
		params = append(params, t)
	}
	return SemanticType{Kind: "function", Parameters: params, Result: resultPtr, TypeOrigin: "derived"}
}

// registeredExecutableRuntimePrimitive is intentionally a closed registry.
// Each item names a canonical semantic primitive implemented by both
// project backends; arbitrary unresolved names never enter this path.
func registeredExecutableRuntimePrimitive(call ProjectCallSummary) (string, bool) {
	identity := firstNonEmpty(call.TargetName, call.TargetBinding)
	// Type assertions are canonicalized as a runtime intrinsic rather than a
	// project function. Keep them in the closed executable runtime registry.
	if identity == "__type_assert" {
		if strings.ToLower(call.Signature.Kind) != "function" {
			return "", false
		}
		return "__type_assert/any", true
	}
	if identity != "native_symbol_print" && identity != "native_symbol_println" {
		return "", false
	}
	signature := call.Signature
	if strings.ToLower(signature.Kind) != "function" || len(signature.Parameters) != 1 {
		return "", false
	}
	parameter := signature.Parameters[0]
	if strings.ToLower(parameter.Kind) != "integer" || (parameter.Bits != 0 && parameter.Bits != 64) {
		return "", false
	}
	if signature.Result != nil && strings.ToLower(signature.Result.Kind) != "void" {
		return "", false
	}
	return identity + "/i64", true
}

func projectExternalImportForNode(g *uastExecutionGraph, nodeID int) *ProjectExternalImport {
	return projectExternalImport(g, nodeID, -1)
}

func projectExternalImportForCall(g *uastExecutionGraph, callNode, calleeNode int) *ProjectExternalImport {
	if result := projectExternalImport(g, callNode, -1); result != nil {
		return result
	}
	if result := projectExternalImport(g, calleeNode, callNode); result != nil {
		return result
	}
	return nil
}

func projectExternalImport(g *uastExecutionGraph, nodeID, relatedCall int) *ProjectExternalImport {
	if g == nil || g.document == nil || nodeID < 0 {
		return nil
	}
	var abi SemanticABIContract
	abiRaw, abiOK := semanticContractPayloadForNode(g.document, nodeID, SemanticABIContractKind)
	if !abiOK && g.nodes[nodeID] != nil {
		abiRaw = g.nodes[nodeID].Fields["abi_contract"]
		abiOK = len(abiRaw) != 0
	}
	// Declarations keep their ABI in a dedicated ABIContract node so the
	// function remains a normal executable declaration when it has a body.
	// A hosted import intentionally has no body and is linked by abi.declares.
	if !abiOK {
		for _, relation := range g.document.Relations {
			if relation.Kind != "abi.graph" || relation.From != nodeID || relation.To.Domain != "node" {
				continue
			}
			contractNode, err := strconv.Atoi(relation.To.ID)
			if err != nil || g.nodes[contractNode] == nil {
				continue
			}
			abiRaw = g.nodes[contractNode].Fields["abi_contract"]
			abiOK = len(abiRaw) != 0
			if abiOK {
				break
			}
		}
	}
	if !abiOK && relatedCall >= 0 {
		for _, relation := range g.document.Relations {
			if relation.Kind != "abi.calls" || relation.To.Domain != "node" {
				continue
			}
			to, err := strconv.Atoi(relation.To.ID)
			if err != nil {
				continue
			}
			contractNode := -1
			if relation.From == relatedCall && g.nodes[to] != nil {
				contractNode = to
			} else if to == relatedCall && g.nodes[relation.From] != nil {
				contractNode = relation.From
			}
			if contractNode < 0 {
				continue
			}
			abiRaw = g.nodes[contractNode].Fields["abi_contract"]
			if len(abiRaw) == 0 {
				abiRaw, abiOK = semanticContractPayloadForNode(g.document, contractNode, SemanticABIContractKind)
			} else {
				abiOK = true
			}
			if abiOK {
				break
			}
		}
	}
	if !abiOK || json.Unmarshal(abiRaw, &abi) != nil || !abi.External {
		return nil
	}
	var symbol SemanticSymbolContract
	symbolOK := false
	if raw, ok := semanticContractPayloadForNode(g.document, nodeID, SemanticSymbolContractKind); ok {
		symbolOK = json.Unmarshal(raw, &symbol) == nil
	}
	if !symbolOK && g.nodes[nodeID] != nil {
		if raw := g.nodes[nodeID].Fields["linkage"]; len(raw) != 0 {
			symbolOK = json.Unmarshal(raw, &symbol) == nil
		}
	}
	library := strings.TrimSpace(symbol.Module)
	symbolName := strings.TrimSpace(abi.Symbol)
	if symbolName == "" {
		symbolName = strings.TrimSpace(symbol.Identity)
	}
	if library == "" {
		library = strings.TrimSpace(abi.ForeignABIIdentity)
	}
	if at := strings.IndexByte(library, '!'); at >= 0 {
		if symbolName == "" {
			symbolName = strings.TrimSpace(library[at+1:])
		}
		library = strings.TrimSpace(library[:at])
	}
	result := SemanticType{}
	if abi.Result != nil {
		result = *abi.Result
	} else if len(abi.Results) == 1 {
		result = abi.Results[0]
	} else if len(abi.Results) > 1 {
		result = SemanticType{Kind: "tuple", Parameters: append([]SemanticType(nil), abi.Results...)}
	}
	if library == "" || symbolName == "" || abi.CallingConvention == "" || result.Kind == "" || !hasCompleteSemanticABIPayload(abiRaw) {
		return nil
	}
	for _, parameter := range abi.Parameters {
		if isUnknownSemanticType(parameter) {
			return nil
		}
	}
	return &ProjectExternalImport{Library: library, Symbol: symbolName, CallingConvention: abi.CallingConvention, Parameters: append([]SemanticType(nil), abi.Parameters...), Result: result, Variadic: strings.Contains(strings.ToLower(abi.VariadicABI), "variadic")}
}

func semanticContractPayloadForNode(doc *UniversalASTDocument, nodeID int, kind SemanticContractKind) (json.RawMessage, bool) {
	if doc == nil {
		return nil, false
	}
	for _, ref := range doc.ContractRefs {
		if ref.NodeID != nodeID || ref.ContractID < 0 || ref.ContractID >= len(doc.ContractTable) {
			continue
		}
		contract := doc.ContractTable[ref.ContractID]
		if contract.Kind == kind {
			return contract.Payload, len(contract.Payload) > 0
		}
	}
	return nil, false
}

func hasCompleteSemanticABIPayload(raw []byte) bool {
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields["parameters"] == nil {
		return false
	}
	return fields["result"] != nil || fields["results"] != nil
}

func collectProjectInitializers(u *UniversalASTDocument, programExtensions map[string]any, unitID string, bindingNames map[string]string, summary *SemanticUnitSummary) {
	if u == nil || summary == nil {
		return
	}
	var contract map[string]any
	if raw := u.Extensions["initialization_contract"]; raw != nil {
		encoded, _ := json.Marshal(raw)
		_ = json.Unmarshal(encoded, &contract)
	}
	if contract == nil && programExtensions != nil {
		if raw := programExtensions["initialization_contract"]; raw != nil {
			encoded, _ := json.Marshal(raw)
			_ = json.Unmarshal(encoded, &contract)
		}
	}
	var order []string
	if contract != nil {
		encoded, _ := json.Marshal(contract["order"])
		_ = json.Unmarshal(encoded, &order)
	}
	if len(order) == 0 && programExtensions != nil {
		encoded, _ := json.Marshal(programExtensions["init_order"])
		_ = json.Unmarshal(encoded, &order)
	}
	for i, binding := range order {
		name := bindingNames[binding]
		if name == "" {
			name = binding
		}
		for _, function := range summary.Functions {
			if function.Name != name {
				continue
			}
			summary.Initializers = append(summary.Initializers, ProjectInitializerSummary{ID: function.ID, UnitID: unitID, FunctionID: function.ID, NodeID: function.NodeID, Order: i})
			break
		}
	}
	initializerByName := map[string]string{}
	for _, initializer := range summary.Initializers {
		for _, function := range summary.Functions {
			if function.ID == initializer.FunctionID && function.Name != "" {
				initializerByName[function.Name] = function.ID
			}
		}
	}
	for _, relation := range u.Relations {
		if relation.Kind != "initialization.before" || relation.To.Domain != "node" {
			continue
		}
		to, err := strconv.Atoi(relation.To.ID)
		if err != nil {
			continue
		}
		fromName, toName := universalNodeNameByID(u, relation.From), universalNodeNameByID(u, to)
		if fromName == "" || toName == "" {
			continue
		}
		fromID, fromOK := initializerByName[fromName]
		toID, toOK := initializerByName[toName]
		if !fromOK || !toOK {
			continue
		}
		for i := range summary.Initializers {
			if summary.Initializers[i].FunctionID == toID {
				summary.Initializers[i].Dependencies = append(summary.Initializers[i].Dependencies, fromID)
			}
		}
	}
}

func semanticProjectModulePath(programExtensions, uastExtensions map[string]any) string {
	for _, extensions := range []map[string]any{programExtensions, uastExtensions} {
		if extensions == nil || extensions["native_package_context"] == nil {
			continue
		}
		encoded, err := json.Marshal(extensions["native_package_context"])
		if err != nil {
			continue
		}
		var context struct {
			ModulePath string `json:"module_path"`
			ModuleRoot string `json:"module_root"`
		}
		if json.Unmarshal(encoded, &context) == nil {
			if path := strings.TrimSpace(context.ModulePath); path != "" {
				return path
			}
			if path := strings.TrimSpace(context.ModuleRoot); path != "" {
				return path
			}
		}
	}
	return ""
}

func universalNodeNameByID(u *UniversalASTDocument, nodeID int) string {
	if u == nil {
		return ""
	}
	for i := range u.Nodes {
		if u.Nodes[i].ID == nodeID {
			return universalNodeName(&u.Nodes[i])
		}
	}
	return ""
}

func dedupeProjectExecutableFacts(summary *SemanticUnitSummary) {
	if summary == nil {
		return
	}
	functions := make(map[string]ProjectFunctionSummary, len(summary.Functions))
	for _, function := range summary.Functions {
		if previous, exists := functions[function.ID]; exists {
			if !previous.HasBody && function.HasBody || previous.Type.Kind == "" && function.Type.Kind != "" {
				functions[function.ID] = function
			}
		} else {
			functions[function.ID] = function
		}
	}
	summary.Functions = summary.Functions[:0]
	for _, function := range functions {
		summary.Functions = append(summary.Functions, function)
	}
	sort.Slice(summary.Functions, func(i, j int) bool { return summary.Functions[i].ID < summary.Functions[j].ID })
	callSeen := map[string]bool{}
	calls := summary.Calls[:0]
	for _, call := range summary.Calls {
		key := call.CallerID + "\x00" + strconv.Itoa(call.NodeID)
		if callSeen[key] {
			continue
		}
		callSeen[key] = true
		calls = append(calls, call)
	}
	summary.Calls = calls
	dataSeen := map[string]bool{}
	dataRefs := summary.DataRefs[:0]
	for _, ref := range summary.DataRefs {
		key := ref.CallerID + "\x00" + strconv.Itoa(ref.NodeID) + "\x00" + ref.TargetID
		if dataSeen[key] {
			continue
		}
		dataSeen[key] = true
		dataRefs = append(dataRefs, ref)
	}
	summary.DataRefs = dataRefs
	importSeen := map[string]bool{}
	imports := summary.Imports[:0]
	for _, ref := range summary.Imports {
		key := ref.ID + "\x00" + ref.Name + "\x00" + ref.Kind + "\x00" + ref.Caller + "\x00" + strconv.Itoa(ref.NodeID)
		if importSeen[key] {
			continue
		}
		importSeen[key] = true
		imports = append(imports, ref)
	}
	summary.Imports = imports
	sort.Slice(summary.Imports, func(i, j int) bool {
		a, b := summary.Imports[i], summary.Imports[j]
		if a.Caller != b.Caller {
			return a.Caller < b.Caller
		}
		if a.NodeID != b.NodeID {
			return a.NodeID < b.NodeID
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Kind < b.Kind
	})
	sort.Slice(summary.Initializers, func(i, j int) bool {
		if summary.Initializers[i].Order != summary.Initializers[j].Order {
			return summary.Initializers[i].Order < summary.Initializers[j].Order
		}
		return summary.Initializers[i].ID < summary.Initializers[j].ID
	})
}

// BuildExecutableClosure resolves the transitive executable graph using only
// compact unit summaries. It never loads or combines SemanticProgram bodies.
// Missing or ambiguous identities are returned as structured unresolved facts;
// callers must persist the result and reject code generation when any exist.
func BuildExecutableClosure(index *SemanticProjectIndex, entryRoots []string) ExecutableClosure {
	closure := ExecutableClosure{EntryRoots: uniqueStrings(entryRoots)}
	if index == nil {
		closure.Unresolved = append(closure.Unresolved, ExecutableClosureUnresolved{Reason: "global semantic index is absent", ResolutionAttempts: []string{"project index"}})
		return closure
	}
	type functionOwner struct {
		unit string
		fn   ProjectFunctionSummary
		pkg  string
	}
	functions := map[string]functionOwner{}
	aliases := map[string][]string{}
	globals := map[string]string{}
	initializers := map[string]ProjectInitializerSummary{}
	unitIDs := make([]string, 0, len(index.Summaries))
	for unitID := range index.Summaries {
		unitIDs = append(unitIDs, unitID)
	}
	sort.Strings(unitIDs)
	addAlias := func(alias, id string) {
		alias = strings.TrimSpace(alias)
		if alias == "" || id == "" {
			return
		}
		for _, prior := range aliases[alias] {
			if prior == id {
				return
			}
		}
		aliases[alias] = append(aliases[alias], id)
	}
	for _, unitID := range unitIDs {
		summary := index.Summaries[unitID]
		for _, fn := range summary.Functions {
			if fn.ID == "" {
				continue
			}
			functions[fn.ID] = functionOwner{unit: unitID, fn: fn, pkg: summary.Package}
			addAlias(fn.ID, fn.ID)
			addAlias(fn.Name, fn.ID)
			addAlias(fn.Binding, fn.ID)
			if summary.Package != "" && fn.Name != "" && !strings.Contains(fn.Name, ".") {
				addAlias(summary.Package+"."+fn.Name, fn.ID)
			}
		}
		for _, global := range summary.Globals {
			if global.ID == "" {
				continue
			}
			globals[global.ID] = unitID
			addAlias(global.ID, global.ID)
			addAlias(global.Name, global.ID)
			if summary.Package != "" && global.Name != "" && !strings.Contains(global.Name, ".") {
				addAlias(summary.Package+"."+global.Name, global.ID)
			}
		}
		for _, init := range summary.Initializers {
			if init.FunctionID == "" {
				init.FunctionID = init.ID
			}
			if init.UnitID == "" {
				init.UnitID = unitID
			}
			initializers[init.ID] = init
		}
	}

	resolve := func(identity string) (string, string) {
		identity = strings.TrimSpace(identity)
		if identity == "" {
			return "", "empty semantic identity"
		}
		if _, ok := functions[identity]; ok {
			return identity, ""
		}
		matches := aliases[identity]
		if len(matches) == 1 {
			if _, ok := functions[matches[0]]; ok {
				return matches[0], ""
			}
		}
		if len(matches) > 1 {
			return "", "ambiguous semantic identity"
		}
		return "", "no function definition in global semantic index"
	}
	addUnresolved := func(caller, unit string, node int, callee, expected, reason string, attempts ...string) {
		closure.Unresolved = append(closure.Unresolved, ExecutableClosureUnresolved{
			Caller: caller, UnitID: unit, NodeID: node, Callee: callee,
			SemanticIdentity: callee, ExpectedSignature: expected, Reason: reason,
			ResolutionAttempts: uniqueStrings(attempts),
		})
	}

	roots := make([]string, 0, len(entryRoots))
	for _, root := range entryRoots {
		id, reason := resolve(root)
		if reason != "" {
			addUnresolved("", "", 0, root, "entry function with executable body", reason, "canonical symbol id", "qualified function name", "function binding")
			continue
		}
		roots = append(roots, id)
	}
	if len(roots) == 0 && len(entryRoots) == 0 {
		addUnresolved("", "", 0, "", "explicit executable entry root", "no entry root was supplied", "project entry point", "compile options entry point")
	}

	queued := map[string]bool{}
	visited := map[string]bool{}
	queue := append([]string(nil), roots...)
	for _, id := range roots {
		queued[id] = true
	}
	functionCalls := map[string][]ProjectCallSummary{}
	functionData := map[string][]ProjectDataReference{}
	for _, unitID := range unitIDs {
		summary := index.Summaries[unitID]
		for _, call := range summary.Calls {
			functionCalls[call.CallerID] = append(functionCalls[call.CallerID], call)
		}
		for _, ref := range summary.DataRefs {
			functionData[ref.CallerID] = append(functionData[ref.CallerID], ref)
		}
	}
	unitSet := map[string]bool{}
	initializerSet := map[string]bool{}
	primitiveSet := map[string]bool{}
	externalSet := map[string]ProjectExternalImport{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if visited[id] {
			continue
		}
		owner, exists := functions[id]
		if !exists {
			addUnresolved("", "", 0, id, "function definition", "resolved function identity has no unit summary", "function summaries")
			continue
		}
		visited[id] = true
		unitSet[owner.unit] = true
		if !owner.fn.HasBody {
			addUnresolved(id, owner.unit, owner.fn.NodeID, owner.fn.Name, "function body, runtime primitive, or explicit external ABI", "reachable function declaration has no body or external import contract", "unit-local function summary", "explicit external ABI import")
			continue
		}
		closure.ReachableFunctions = append(closure.ReachableFunctions, ExecutableFunctionRef{ID: id, Name: owner.fn.Name, UnitID: owner.unit, NodeID: owner.fn.NodeID})

		calls := append([]ProjectCallSummary(nil), functionCalls[id]...)
		sort.Slice(calls, func(i, j int) bool { return calls[i].NodeID < calls[j].NodeID })
		for _, call := range calls {
			targetIdentity := firstNonEmpty(call.TargetID, call.TargetName, call.TargetBinding)
			if call.ExternalImport != nil {
				if !validProjectExternalImport(*call.ExternalImport) {
					addUnresolved(id, owner.unit, call.NodeID, call.TargetName, "complete external library/symbol/signature/calling convention", "external ABI import contract is incomplete", "UAST ABI contract", "UAST symbol/linkage contract")
					continue
				}
				externalSet[externalImportKey(*call.ExternalImport)] = *call.ExternalImport
				closure.Edges = append(closure.Edges, ExecutableClosureEdge{CallerID: id, CallerUnit: owner.unit, Callee: targetIdentity, NodeID: call.NodeID, Resolution: "external_import", ExternalImport: call.ExternalImport})
				continue
			}
			resolved, reason := resolveProjectCallTarget(call, resolve)
			if reason != "" {
				if primitive, ok := registeredExecutableRuntimePrimitive(call); ok {
					primitiveSet[primitive] = true
					closure.Edges = append(closure.Edges, ExecutableClosureEdge{CallerID: id, CallerUnit: owner.unit, Callee: primitive, NodeID: call.NodeID, Resolution: "runtime_primitive"})
					continue
				}
				addUnresolved(id, owner.unit, call.NodeID, targetIdentity, semanticTypeFingerprint(call.Signature), reason, "project function id", "explicit function binding", "qualified function name", "explicit external ABI import", "registered runtime primitive")
				continue
			}
			target := functions[resolved]
			if target.fn.External != nil && !target.fn.HasBody {
				if !validProjectExternalImport(*target.fn.External) {
					addUnresolved(id, owner.unit, call.NodeID, target.fn.Name, "complete external library/symbol/signature/calling convention", "indexed external function contract is incomplete", "global function ABI contract", "global symbol/linkage contract")
					continue
				}
				externalSet[externalImportKey(*target.fn.External)] = *target.fn.External
				closure.Edges = append(closure.Edges, ExecutableClosureEdge{CallerID: id, CallerUnit: owner.unit, Callee: targetIdentity, NodeID: call.NodeID, Resolution: "external_import", TargetID: resolved, ExternalImport: target.fn.External})
				continue
			}
			if !callSignaturesCompatible(call.Signature, target.fn.Type) {
				addUnresolved(id, owner.unit, call.NodeID, targetIdentity, semanticTypeDescription(target.fn.Type)+"; call="+semanticTypeDescription(call.Signature), "call-site ABI signature conflicts with indexed function contract", "call-site semantic signature", "global function ABI contract")
				continue
			}
			closure.Edges = append(closure.Edges, ExecutableClosureEdge{CallerID: id, CallerUnit: owner.unit, Callee: targetIdentity, NodeID: call.NodeID, Resolution: "semantic_function", TargetID: resolved})
			if !queued[resolved] && !visited[resolved] {
				queued[resolved] = true
				queue = append(queue, resolved)
			}
		}
		refs := append([]ProjectDataReference(nil), functionData[id]...)
		sort.Slice(refs, func(i, j int) bool { return refs[i].NodeID < refs[j].NodeID })
		for _, ref := range refs {
			if _, ok := globals[ref.TargetID]; !ok {
				addUnresolved(id, owner.unit, ref.NodeID, ref.TargetID, "project global storage symbol", "global reference is absent from global semantic index", "canonical global symbol id")
				continue
			}
			unitSet[globals[ref.TargetID]] = true
			closure.Edges = append(closure.Edges, ExecutableClosureEdge{CallerID: id, CallerUnit: owner.unit, Callee: ref.TargetID, NodeID: ref.NodeID, Resolution: "project_data"})
		}
	}

	closeProjectPackageUnits(index, unitSet)
	// Initializers are selected for every unit reached by executable code or
	// referenced global data. Their declared dependencies are resolved before
	// their bodies are queued, preserving unit-local ownership.
	for changed := true; changed; {
		changed = false
		for _, item := range orderedProjectInitializerSummaries(index, orderedProjectInitializerUnits(index, unitSet)) {
			unitID, init := item.UnitID, item.Summary
			if !unitSet[unitID] {
				continue
			}
			if initializerSet[init.ID] {
				continue
			}
			initializerSet[init.ID] = true
			closure.RequiredInitializers = append(closure.RequiredInitializers, init.FunctionID)
			initID, reason := resolve(init.FunctionID)
			if reason != "" {
				addUnresolved("<initializer>", unitID, init.NodeID, init.FunctionID, "initializer function body", reason, "initializer contract", "global function index")
				continue
			}
			initOwner := functions[initID]
			if !initOwner.fn.HasBody {
				addUnresolved("<initializer>", unitID, init.NodeID, init.FunctionID, "initializer function body", "initializer declaration has no executable body", "initialization contract", "function summary")
				continue
			}
			closure.InitializerFunctions = append(closure.InitializerFunctions, ExecutableFunctionRef{ID: initID, Name: initOwner.fn.Name, UnitID: initOwner.unit, NodeID: initOwner.fn.NodeID})
			if !queued[initID] && !visited[initID] {
				queued[initID] = true
				queue = append(queue, initID)
			}
			for _, dependency := range init.Dependencies {
				depID, depReason := resolve(dependency)
				if depReason != "" {
					addUnresolved("<initializer>", unitID, init.NodeID, dependency, "initializer dependency", depReason, "initialization dependency contract", "global function index")
					continue
				}
				if !queued[depID] && !visited[depID] {
					queued[depID] = true
					queue = append(queue, depID)
				}
			}
			changed = true
		}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			if visited[id] {
				continue
			}
			owner, ok := functions[id]
			if !ok {
				addUnresolved("<initializer>", "", 0, id, "initializer dependency function", "no unit summary", "global function index")
				continue
			}
			visited[id] = true
			unitSet[owner.unit] = true
			if !owner.fn.HasBody {
				addUnresolved("<initializer>", owner.unit, owner.fn.NodeID, id, "initializer function body", "initializer dependency has no executable body", "function summary")
				continue
			}
			closure.ReachableFunctions = append(closure.ReachableFunctions, ExecutableFunctionRef{ID: id, Name: owner.fn.Name, UnitID: owner.unit, NodeID: owner.fn.NodeID})
			for _, call := range functionCalls[id] {
				targetIdentity := firstNonEmpty(call.TargetID, call.TargetName, call.TargetBinding)
				if call.ExternalImport != nil {
					if !validProjectExternalImport(*call.ExternalImport) {
						addUnresolved(id, owner.unit, call.NodeID, targetIdentity, "complete external ABI import contract", "initializer external call contract is incomplete", "UAST ABI contract")
						continue
					}
					externalSet[externalImportKey(*call.ExternalImport)] = *call.ExternalImport
					closure.Edges = append(closure.Edges, ExecutableClosureEdge{CallerID: id, CallerUnit: owner.unit, Callee: targetIdentity, NodeID: call.NodeID, Resolution: "external_import", ExternalImport: call.ExternalImport})
					continue
				}
				resolved, reason := resolveProjectCallTarget(call, resolve)
				if reason != "" {
					if primitive, ok := registeredExecutableRuntimePrimitive(call); ok {
						primitiveSet[primitive] = true
						closure.Edges = append(closure.Edges, ExecutableClosureEdge{CallerID: id, CallerUnit: owner.unit, Callee: primitive, NodeID: call.NodeID, Resolution: "runtime_primitive"})
						continue
					}
					addUnresolved(id, owner.unit, call.NodeID, targetIdentity, semanticTypeFingerprint(call.Signature), reason, "global function index", "external import contract")
					continue
				}
				target := functions[resolved]
				if target.fn.External != nil && !target.fn.HasBody {
					if !validProjectExternalImport(*target.fn.External) {
						addUnresolved(id, owner.unit, call.NodeID, target.fn.Name, "complete external ABI import contract", "initializer calls an external function with incomplete contract", "global ABI contract")
						continue
					}
					externalSet[externalImportKey(*target.fn.External)] = *target.fn.External
					closure.Edges = append(closure.Edges, ExecutableClosureEdge{CallerID: id, CallerUnit: owner.unit, Callee: targetIdentity, NodeID: call.NodeID, Resolution: "external_import", TargetID: resolved, ExternalImport: target.fn.External})
					continue
				}
				if !callSignaturesCompatible(call.Signature, target.fn.Type) {
					addUnresolved(id, owner.unit, call.NodeID, targetIdentity, semanticTypeDescription(target.fn.Type)+"; call="+semanticTypeDescription(call.Signature), "initializer call ABI signature conflicts with indexed function contract", "call-site semantic signature", "global function ABI contract")
					continue
				}
				closure.Edges = append(closure.Edges, ExecutableClosureEdge{CallerID: id, CallerUnit: owner.unit, Callee: targetIdentity, NodeID: call.NodeID, Resolution: "semantic_function", TargetID: resolved})
				queue = append(queue, resolved)
			}
			for _, ref := range functionData[id] {
				if globalUnit := globals[ref.TargetID]; globalUnit != "" {
					unitSet[globalUnit] = true
				} else {
					addUnresolved(id, owner.unit, ref.NodeID, ref.TargetID, "global storage", "missing global symbol", "global index")
				}
			}
		}
		beforeUnitClosure := len(unitSet)
		closeProjectPackageUnits(index, unitSet)
		if len(unitSet) != beforeUnitClosure {
			changed = true
		}
	}

	for unitID := range unitSet {
		closure.ReachableUnits = append(closure.ReachableUnits, unitID)
	}
	for primitive := range primitiveSet {
		closure.RequiredRuntimePrimitives = append(closure.RequiredRuntimePrimitives, primitive)
	}
	for _, external := range externalSet {
		closure.RequiredExternalImports = append(closure.RequiredExternalImports, external)
	}
	sort.Strings(closure.ReachableUnits)
	// Initializers were traversed in canonical unit order and per-unit semantic
	// initialization order. Keep that execution order while removing duplicate
	// declarations shared through repeated dependency paths.
	seenInitializers := map[string]bool{}
	orderedInitializers := closure.InitializerFunctions[:0]
	for _, init := range closure.InitializerFunctions {
		if seenInitializers[init.ID] {
			continue
		}
		seenInitializers[init.ID] = true
		orderedInitializers = append(orderedInitializers, init)
	}
	closure.InitializerFunctions = orderedInitializers
	sort.Strings(closure.RequiredRuntimePrimitives)
	sort.Slice(closure.ReachableFunctions, func(i, j int) bool { return closure.ReachableFunctions[i].ID < closure.ReachableFunctions[j].ID })
	sort.Slice(closure.RequiredExternalImports, func(i, j int) bool {
		return externalImportKey(closure.RequiredExternalImports[i]) < externalImportKey(closure.RequiredExternalImports[j])
	})
	sort.Slice(closure.Edges, func(i, j int) bool {
		if closure.Edges[i].CallerID != closure.Edges[j].CallerID {
			return closure.Edges[i].CallerID < closure.Edges[j].CallerID
		}
		if closure.Edges[i].NodeID != closure.Edges[j].NodeID {
			return closure.Edges[i].NodeID < closure.Edges[j].NodeID
		}
		return closure.Edges[i].Callee < closure.Edges[j].Callee
	})
	sort.Slice(closure.Unresolved, func(i, j int) bool {
		a, b := closure.Unresolved[i], closure.Unresolved[j]
		if a.UnitID != b.UnitID {
			return a.UnitID < b.UnitID
		}
		if a.NodeID != b.NodeID {
			return a.NodeID < b.NodeID
		}
		if a.Caller != b.Caller {
			return a.Caller < b.Caller
		}
		return a.Callee < b.Callee
	})
	closure.ReachableFunctionCount = len(closure.ReachableFunctions)
	closure.ReachableUnitCount = len(closure.ReachableUnits)
	if len(closure.InitializerFunctions) > 0 {
		closure.InitializerOrderBasis = "module-import dependencies, explicit initialization-before relations, stable unit identity within dependency SCCs, and per-unit semantic order"
	}
	return closure
}

func closeProjectPackageUnits(index *SemanticProjectIndex, unitSet map[string]bool) {
	if index == nil || unitSet == nil {
		return
	}
	for changed := true; changed; {
		changed = false
		for id := range unitSet {
			current, ok := index.Summaries[id]
			if !ok {
				continue
			}
			for candidateID, candidate := range index.Summaries {
				if unitSet[candidateID] {
					continue
				}
				if projectUnitsShareInitializationDomain(id, current, candidateID, candidate) || projectUnitImportedBy(current, candidate) {
					unitSet[candidateID], changed = true, true
				}
			}
		}
	}
}

func projectUnitsShareInitializationDomain(aID string, a SemanticUnitSummary, bID string, b SemanticUnitSummary) bool {
	if a.ModulePath != "" && b.ModulePath != "" {
		return a.ModulePath == b.ModulePath
	}
	if a.Package == "" || a.Package != b.Package || strings.HasPrefix(aID, "embedded/") || strings.HasPrefix(bID, "embedded/") || strings.HasPrefix(aID, "external-owner/") || strings.HasPrefix(bID, "external-owner/") {
		return false
	}
	return filepath.ToSlash(filepath.Dir(aID)) == filepath.ToSlash(filepath.Dir(bID))
}

func projectUnitImportedBy(importer, candidate SemanticUnitSummary) bool {
	for _, ref := range importer.Imports {
		if ref.Kind != "module" || strings.TrimSpace(ref.Name) == "" {
			continue
		}
		if ref.Name == candidate.ModulePath || ref.Name == candidate.Package || candidate.ModulePath != "" && strings.HasPrefix(ref.Name, strings.TrimSuffix(candidate.ModulePath, "/")+"/") {
			return true
		}
	}
	return false
}

type projectInitializerRef struct {
	UnitID  string
	Summary ProjectInitializerSummary
}

func orderedProjectInitializerSummaries(index *SemanticProjectIndex, unitOrder []string) []projectInitializerRef {
	if index == nil {
		return nil
	}
	ordered := make([]projectInitializerRef, 0)
	byFunction := map[string]projectInitializerRef{}
	for _, unitID := range unitOrder {
		initializers := append([]ProjectInitializerSummary(nil), index.Summaries[unitID].Initializers...)
		sort.Slice(initializers, func(i, j int) bool {
			if initializers[i].Order != initializers[j].Order {
				return initializers[i].Order < initializers[j].Order
			}
			return initializers[i].ID < initializers[j].ID
		})
		for _, initializer := range initializers {
			if initializer.FunctionID == "" {
				initializer.FunctionID = initializer.ID
			}
			if initializer.ID == "" {
				initializer.ID = initializer.FunctionID
			}
			initializer.UnitID = unitID
			ref := projectInitializerRef{UnitID: unitID, Summary: initializer}
			ordered = append(ordered, ref)
			byFunction[initializer.FunctionID] = ref
		}
	}
	state := map[string]uint8{}
	result := make([]projectInitializerRef, 0, len(ordered))
	var visit func(projectInitializerRef)
	visit = func(ref projectInitializerRef) {
		id := ref.Summary.FunctionID
		if id == "" {
			id = ref.Summary.ID
		}
		if state[id] == 2 {
			return
		}
		if state[id] == 1 {
			return
		} // SCC: stable source order breaks ties within the cycle.
		state[id] = 1
		deps := append([]string(nil), ref.Summary.Dependencies...)
		sort.Strings(deps)
		for _, dependency := range deps {
			if prerequisite, exists := byFunction[dependency]; exists {
				visit(prerequisite)
			}
		}
		state[id] = 2
		result = append(result, ref)
	}
	for _, ref := range ordered {
		visit(ref)
	}
	return result
}

func orderedProjectInitializerUnits(index *SemanticProjectIndex, reachable map[string]bool) []string {
	units := make([]string, 0, len(reachable))
	for id := range reachable {
		units = append(units, id)
	}
	sort.Strings(units)
	if index == nil || len(units) < 2 {
		return units
	}
	deps := map[string][]string{}
	for _, id := range units {
		for _, ref := range index.Summaries[id].Imports {
			if ref.Kind != "module" || ref.Name == "" {
				continue
			}
			for _, candidateID := range units {
				if id == candidateID {
					continue
				}
				candidate := index.Summaries[candidateID]
				if ref.Name == candidate.ModulePath || ref.Name == candidate.Package || candidate.ModulePath != "" && strings.HasPrefix(ref.Name, strings.TrimSuffix(candidate.ModulePath, "/")+"/") {
					deps[id] = append(deps[id], candidateID)
				}
			}
		}
		deps[id] = uniqueStrings(deps[id])
		sort.Strings(deps[id])
	}
	indexOf, low, onStack := map[string]int{}, map[string]int{}, map[string]bool{}
	stack, components, next := []string{}, [][]string{}, 1
	var strongConnect func(string)
	strongConnect = func(id string) {
		indexOf[id], low[id], next = next, next, next+1
		stack, onStack[id] = append(stack, id), true
		for _, dep := range deps[id] {
			if indexOf[dep] == 0 {
				strongConnect(dep)
				if low[dep] < low[id] {
					low[id] = low[dep]
				}
			} else if onStack[dep] && indexOf[dep] < low[id] {
				low[id] = indexOf[dep]
			}
		}
		if low[id] != indexOf[id] {
			return
		}
		component := []string{}
		for len(stack) > 0 {
			last := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			onStack[last] = false
			component = append(component, last)
			if last == id {
				break
			}
		}
		sort.Strings(component)
		components = append(components, component)
	}
	for _, id := range units {
		if indexOf[id] == 0 {
			strongConnect(id)
		}
	}
	componentOf, componentKey := map[string]int{}, make([]string, len(components))
	for i, component := range components {
		componentKey[i] = component[0]
		for _, id := range component {
			componentOf[id] = i
		}
	}
	componentDeps := map[int][]int{}
	for id, list := range deps {
		from := componentOf[id]
		for _, dep := range list {
			to := componentOf[dep]
			if from != to {
				componentDeps[from] = append(componentDeps[from], to)
			}
		}
	}
	for component, list := range componentDeps {
		seen, unique := map[int]bool{}, list[:0]
		for _, dep := range list {
			if !seen[dep] {
				seen[dep] = true
				unique = append(unique, dep)
			}
		}
		componentDeps[component] = unique
		sort.Slice(componentDeps[component], func(i, j int) bool {
			return componentKey[componentDeps[component][i]] < componentKey[componentDeps[component][j]]
		})
	}
	order, visited := []string{}, map[int]bool{}
	var visit func(int)
	visit = func(component int) {
		if visited[component] {
			return
		}
		visited[component] = true
		for _, dep := range componentDeps[component] {
			visit(dep)
		}
		order = append(order, components[component]...)
	}
	componentOrder := make([]int, len(components))
	for i := range componentOrder {
		componentOrder[i] = i
	}
	sort.Slice(componentOrder, func(i, j int) bool { return componentKey[componentOrder[i]] < componentKey[componentOrder[j]] })
	for _, component := range componentOrder {
		visit(component)
	}
	return order
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func resolveProjectCallTarget(call ProjectCallSummary, resolve func(string) (string, string)) (string, string) {
	identities := []string{call.TargetID, call.TargetName, call.TargetBinding}
	seen := map[string]bool{}
	var lastReason string
	for _, identity := range identities {
		identity = strings.TrimSpace(identity)
		if identity == "" || seen[identity] {
			continue
		}
		seen[identity] = true
		if id, reason := resolve(identity); reason == "" {
			return id, ""
		} else {
			lastReason = reason
		}
		// These are the compiler's canonical UAST binding namespaces, not
		// source-language symbol rules. The binding carries the declared symbol
		// tail when no GlobalSemanticIndex ID was attached to the call node.
		for _, prefix := range []string{"native_var_", "native_symbol_"} {
			if strings.HasPrefix(identity, prefix) {
				alias := strings.TrimPrefix(identity, prefix)
				if id, reason := resolve(alias); reason == "" {
					return id, ""
				} else {
					lastReason = reason
				}
			}
		}
	}
	if lastReason == "" {
		lastReason = "empty semantic identity"
	}
	return "", lastReason
}

func validProjectExternalImport(external ProjectExternalImport) bool {
	if strings.TrimSpace(external.Library) == "" || strings.TrimSpace(external.Symbol) == "" || strings.TrimSpace(external.CallingConvention) == "" || external.Result.Kind == "" {
		return false
	}
	for _, parameter := range external.Parameters {
		if isUnknownSemanticType(parameter) {
			return false
		}
	}
	return true
}

func externalImportKey(external ProjectExternalImport) string {
	return strings.ToLower(strings.TrimSpace(external.Library)) + "!" + strings.TrimSpace(external.Symbol) + "!" + strings.ToLower(strings.TrimSpace(external.CallingConvention))
}

func semanticTypeFingerprint(t SemanticType) string {
	if isUnknownSemanticType(t) {
		return ""
	}
	t.TypeOrigin = ""
	b, _ := json.Marshal(t)
	return stableBytesHash(b)
}

func semanticTypeDescription(t SemanticType) string {
	b, err := json.Marshal(t)
	if err != nil {
		return fmt.Sprintf("%#v", t)
	}
	return string(b)
}

func callSignaturesCompatible(call, target SemanticType) bool {
	if !completeProjectFunctionSignature(target) {
		return false
	}
	// Some canonical transports preserve the call edge but omit the optional
	// call-site type entirely. The canonical target identity and its complete
	// indexed contract are sufficient in that case.
	if strings.TrimSpace(call.Kind) == "" && len(call.Parameters) == 0 && call.Result == nil {
		return len(target.Parameters) == 0
	}
	if strings.ToLower(strings.TrimSpace(call.Kind)) != "function" {
		return false
	}
	if len(call.Parameters) != len(target.Parameters) {
		return false
	}
	for i := range call.Parameters {
		// An absent call-site type is resolved from the unique callable contract
		// found through the canonical symbol. It does not select an overload.
		if isUnknownSemanticType(call.Parameters[i]) {
			continue
		}
		if semanticTypeFingerprint(call.Parameters[i]) != semanticTypeFingerprint(target.Parameters[i]) {
			return false
		}
	}
	if call.Result == nil || isUnknownSemanticType(*call.Result) {
		return true
	}
	return semanticTypeFingerprint(*call.Result) == semanticTypeFingerprint(*target.Result)
}

func completeProjectFunctionSignature(signature SemanticType) bool {
	if strings.ToLower(strings.TrimSpace(signature.Kind)) != "function" || signature.Result == nil {
		return false
	}
	for _, parameter := range signature.Parameters {
		if isUnknownSemanticType(parameter) {
			return false
		}
	}
	return !isUnknownSemanticType(*signature.Result)
}

func WriteExecutableClosureReport(path string, closure ExecutableClosure) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("executable closure report path is empty")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(closure, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

func projectExecutableClosure(p *SemanticProject, roots []string, reportPath string) (ExecutableClosure, error) {
	closure := BuildExecutableClosure(&p.Index, roots)
	if strings.TrimSpace(reportPath) == "" {
		if len(p.Units) > 0 && p.Units[0] != nil && p.Units[0].Path != "" {
			reportPath = filepath.Join(filepath.Dir(p.Units[0].Path), "executable-closure.json")
		} else {
			reportPath = "executable-closure.json"
		}
	}
	if err := WriteExecutableClosureReport(reportPath, closure); err != nil {
		return closure, fmt.Errorf("EXECUTABLE_CLOSURE_REPORT: %w", err)
	}
	if len(closure.Unresolved) != 0 {
		first := closure.Unresolved[0]
		return closure, fmt.Errorf("EXECUTABLE_CLOSURE_UNRESOLVED: unresolved=%d unit=%q node=%d caller=%q callee=%q reason=%s expected=%s attempts=%v report=%s", len(closure.Unresolved), first.UnitID, first.NodeID, first.Caller, first.Callee, first.Reason, first.ExpectedSignature, first.ResolutionAttempts, reportPath)
	}
	return closure, nil
}

func filterProjectUnitsToClosure(units []*SemanticCompilationUnit, closure ExecutableClosure) ([]*SemanticCompilationUnit, error) {
	needed := make(map[string]bool, len(closure.ReachableUnits))
	for _, unitID := range closure.ReachableUnits {
		needed[unitID] = true
	}
	selected := make([]*SemanticCompilationUnit, 0, len(needed))
	for _, unit := range units {
		if unit != nil && needed[unit.ID] {
			selected = append(selected, unit)
			delete(needed, unit.ID)
		}
	}
	if len(needed) != 0 {
		missing := make([]string, 0, len(needed))
		for id := range needed {
			missing = append(missing, id)
		}
		sort.Strings(missing)
		return nil, fmt.Errorf("executable closure references units absent from compilation project: %s", strings.Join(missing, ", "))
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("executable closure contains no semantic units")
	}
	return selected, nil
}
