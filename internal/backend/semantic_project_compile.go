// Copyright (c) 2026 Tarek Wasfy
// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SemanticCompilationUnit is one immutable canonical semantic input. Its
// UAST is never appended to another unit.
type SemanticCompilationUnit struct {
	ID           string
	Path         string
	SemanticRoot string
	Program      *SemanticProgram
}

// SemanticProjectIndex contains only cross-unit names and roots.
type SemanticProjectIndex struct {
	Units        map[string]string
	Symbols      map[string]ProjectSymbol
	Dependencies map[string][]string
	Summaries    map[string]SemanticUnitSummary
}

type SemanticUnitSummary struct {
	Schema       string                      `json:"schema"`
	UnitID       string                      `json:"unit_id"`
	Root         string                      `json:"root"`
	Package      string                      `json:"package,omitempty"`
	ModulePath   string                      `json:"module_path,omitempty"`
	Symbols      []ProjectSymbolSummary      `json:"symbols,omitempty"`
	Functions    []ProjectFunctionSummary    `json:"functions,omitempty"`
	Globals      []ProjectGlobalSummary      `json:"globals,omitempty"`
	Calls        []ProjectCallSummary        `json:"calls,omitempty"`
	DataRefs     []ProjectDataReference      `json:"data_references,omitempty"`
	Imports      []ProjectSymbolReference    `json:"imports,omitempty"`
	Exports      []ProjectSymbolReference    `json:"exports,omitempty"`
	Initializers []ProjectInitializerSummary `json:"initializers,omitempty"`
}

const semanticUnitSummarySchema = "semantic-project-unit-summary-v6"

type ProjectSymbolSummary struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Kind     string                 `json:"kind"`
	Type     SemanticType           `json:"type,omitempty"`
	ABI      string                 `json:"abi,omitempty"`
	External *ProjectExternalImport `json:"external,omitempty"`
}
type ProjectFunctionSummary struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name,omitempty"`
	Binding  string                 `json:"binding,omitempty"`
	Type     SemanticType           `json:"type,omitempty"`
	ABI      string                 `json:"abi,omitempty"`
	Variadic bool                   `json:"variadic,omitempty"`
	NodeID   int                    `json:"node_id,omitempty"`
	HasBody  bool                   `json:"has_body"`
	Local    bool                   `json:"local,omitempty"`
	External *ProjectExternalImport `json:"external,omitempty"`
}
type ProjectGlobalSummary struct {
	ID      string       `json:"id"`
	Name    string       `json:"name"`
	Type    SemanticType `json:"type,omitempty"`
	Mutable bool         `json:"mutable"`
	NodeID  int          `json:"node_id,omitempty"`
}
type ProjectSymbolReference struct {
	ID     string `json:"id,omitempty"`
	Name   string `json:"name,omitempty"`
	Kind   string `json:"kind,omitempty"`
	Caller string `json:"caller,omitempty"`
	NodeID int    `json:"node_id,omitempty"`
}

type ProjectCallSummary struct {
	CallerID       string                 `json:"caller_id"`
	NodeID         int                    `json:"node_id"`
	TargetID       string                 `json:"target_id,omitempty"`
	TargetName     string                 `json:"target_name,omitempty"`
	TargetBinding  string                 `json:"target_binding,omitempty"`
	Dispatch       string                 `json:"dispatch,omitempty"`
	Signature      SemanticType           `json:"signature,omitempty"`
	ExternalImport *ProjectExternalImport `json:"external_import,omitempty"`
}

type ProjectDataReference struct {
	CallerID string `json:"caller_id"`
	NodeID   int    `json:"node_id"`
	TargetID string `json:"target_id,omitempty"`
	Name     string `json:"name,omitempty"`
}

type ProjectExternalImport struct {
	Library           string         `json:"library"`
	Symbol            string         `json:"symbol"`
	CallingConvention string         `json:"calling_convention"`
	Parameters        []SemanticType `json:"parameters"`
	Result            SemanticType   `json:"result"`
	Variadic          bool           `json:"variadic,omitempty"`
}

type ProjectInitializerSummary struct {
	ID           string   `json:"id"`
	UnitID       string   `json:"unit_id,omitempty"`
	FunctionID   string   `json:"function_id,omitempty"`
	NodeID       int      `json:"node_id,omitempty"`
	Order        int      `json:"order,omitempty"`
	Dependencies []string `json:"dependencies,omitempty"`
}

type ProjectSymbol struct {
	ID            string                 `json:"id"`
	UnitID        string                 `json:"unit_id"`
	NodeID        int                    `json:"node_id"`
	Name          string                 `json:"name"`
	QualifiedName string                 `json:"qualified_name"`
	Visibility    string                 `json:"visibility,omitempty"`
	Linkage       string                 `json:"linkage,omitempty"`
	Type          SemanticType           `json:"type,omitempty"`
	ABI           string                 `json:"abi,omitempty"`
	External      *ProjectExternalImport `json:"external,omitempty"`
}

func containsProjectReference(refs []ProjectSymbolReference, id string) bool {
	for _, ref := range refs {
		if ref.ID == id {
			return true
		}
	}
	return false
}

// canonicalFunctionBindingName resolves a function node only through an
// explicit declaration name, FunctionBinding fact, or a direct assignment
// edge to a binding recorded by the frontend. Node order is not symbol
// identity: assigning names to anonymous closures by ordinal can fabricate
// project exports and poison every caller's ABI summary.
func canonicalFunctionBindingName(g *uastExecutionGraph, nodeID int, bindingNames map[string]string) string {
	if g == nil {
		return ""
	}
	resolve := func(candidate string) string {
		if source := bindingNames[candidate]; source != "" {
			return source
		}
		if strings.HasPrefix(candidate, "native_function_") || strings.HasPrefix(candidate, "native_var_") {
			return ""
		}
		return candidate
	}
	common, ok := g.common[nodeID]
	if !ok || common.Kind != "function" {
		return ""
	}
	if common.Name != "" {
		if name := resolve(common.Name); name != "" {
			return name
		}
	}
	if common.Operation.FunctionBinding != "" {
		if name := resolve(common.Operation.FunctionBinding); name != "" {
			return name
		}
	}
	matched := map[string]bool{}
	for parentID, roles := range g.children {
		parent, exists := g.common[parentID]
		if !exists || parent.Kind != "assign" || parent.Name == "" {
			continue
		}
		for _, role := range []string{"expression", "value"} {
			for _, child := range roles[role] {
				if child.ID == nodeID {
					if source := bindingNames[parent.Name]; source != "" {
						matched[source] = true
					}
				}
			}
		}
	}
	if len(matched) != 1 {
		return ""
	}
	for name := range matched {
		return name
	}
	return ""
}

type SemanticProject struct {
	Units      []*SemanticCompilationUnit
	Index      SemanticProjectIndex
	EntryPoint string
}

func selectSemanticTargetUnits(units []*SemanticCompilationUnit, targetOS string) []*SemanticCompilationUnit {
	if len(units) == 0 {
		return nil
	}
	target := strings.ToLower(strings.TrimSpace(targetOS))
	if target == "" {
		target = strings.ToLower(runtime.GOOS)
	}
	// Go build-file suffixes are part of the package-selection contract and
	// remain meaningful after a source file has been transported as .se.
	// Keep the rule data-driven by suffix; no package or language is special.
	prefer := "_" + target + "."
	exclude := map[string]bool{}
	if target == "windows" {
		exclude["_other."] = true
	} else if target != "" {
		exclude["_windows."] = true
	}
	selected := make([]*SemanticCompilationUnit, 0, len(units))
	for _, unit := range units {
		if unit == nil {
			continue
		}
		base := strings.ToLower(filepath.Base(unit.Path))
		matchedTarget := strings.Contains(base, prefer)
		matchedExcluded := false
		for suffix := range exclude {
			if strings.Contains(base, suffix) {
				matchedExcluded = true
				break
			}
		}
		if matchedExcluded && !matchedTarget {
			continue
		}
		selected = append(selected, unit)
	}
	if len(selected) == 0 {
		return units
	}
	return selected
}

const projectUnitCompileCacheSchema = "semantic-project-unit-fragment-v3"

type projectUnitCompileCacheRecord struct {
	Schema           string                     `json:"schema"`
	Key              string                     `json:"key"`
	Fragments        map[string]MachineFragment `json:"fragments,omitempty"`
	FunctionObjects  map[string][]byte          `json:"function_objects,omitempty"`
	NativeFunctions  []x64Function              `json:"native_functions,omitempty"`
	ProjectSymbols   map[string]ProjectSymbol   `json:"project_symbols,omitempty"`
	Metrics          StreamingMetrics           `json:"metrics"`
	OutputKind       CompileOutputKind          `json:"output_kind,omitempty"`
	InstructionCount int                        `json:"instruction_count,omitempty"`
	Imports          []pe64ImportSpec           `json:"imports,omitempty"`
	HasEntry         bool                       `json:"has_entry"`
	Completed        bool                       `json:"completed"`
}

// recoveredCallableContract is the compact result of transport repair.  It
// deliberately contains declaration facts only; executable UAST bodies and
// recovery graphs never enter this cache.
type recoveredCallableContract struct {
	Name string
	Type SemanticType
}

var projectCallableRecoveryCache = struct {
	sync.RWMutex
	entries map[string][]recoveredCallableContract
}{entries: make(map[string][]recoveredCallableContract)}

func applyRecoveredCallableContracts(summary *SemanticUnitSummary, contracts []recoveredCallableContract) {
	if summary == nil {
		return
	}
	for _, recovered := range contracts {
		for i := range summary.Functions {
			if summary.Functions[i].Name == recovered.Name {
				summary.Functions[i].Type = recovered.Type
			}
		}
		for i := range summary.Symbols {
			if summary.Symbols[i].Name == recovered.Name && summary.Symbols[i].Kind == "function" {
				summary.Symbols[i].Type = recovered.Type
			}
		}
	}
}

// applyProjectCallableContractsToUAST projects compact declaration facts back
// onto the current unit only. Bodies, node identity, and relations are never
// merged across units.
func applyProjectCallableContractsToUAST(u *UniversalASTDocument, summary SemanticUnitSummary) error {
	if u == nil {
		return fmt.Errorf("missing unit UAST")
	}
	graph, err := projectSummaryGraph(u)
	if err != nil {
		return err
	}
	return applyProjectCallableContractsToGraph(u, summary, graph)
}

func applyProjectCallableContractsToGraph(u *UniversalASTDocument, summary SemanticUnitSummary, graph *uastExecutionGraph) error {
	if u == nil || graph == nil || graph.document != u {
		return fmt.Errorf("missing or mismatched unit summary graph")
	}
	contracts := make(map[string]SemanticType, len(summary.Functions))
	// Prefer an interned UAST FUNCTION contract when the transport already
	// carries one. This is the canonical primitive: it rehydrates the local
	// declaration type from existing semantic facts without source recovery.
	for id, common := range graph.common {
		if common.Kind != "function" || common.Name == "" {
			continue
		}
		var payload SemanticFunctionContract
		ok, err := contractForNode(u, id, SemanticFunctionContractKind, &payload)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		result := SemanticType{Kind: "void", TypeOrigin: "derived"}
		if len(payload.Results) == 1 {
			result = payload.Results[0]
		} else if len(payload.Results) > 1 {
			result = SemanticType{Kind: "tuple", Parameters: append([]SemanticType(nil), payload.Results...), TypeOrigin: "derived"}
		}
		params := make([]SemanticType, 0, len(payload.Parameters))
		complete := true
		for _, parameter := range payload.Parameters {
			if isUnknownSemanticType(parameter.Type) {
				complete = false
				break
			}
			params = append(params, parameter.Type)
		}
		if complete && !isUnknownSemanticType(result) {
			contracts[common.Name] = SemanticType{Kind: "function", Name: common.Name, Parameters: params, Result: &result, TypeOrigin: "derived"}
		}
	}
	for _, fn := range summary.Functions {
		if fn.Name == "" || fn.Type.Kind != "function" || fn.Type.Result == nil {
			continue
		}
		complete := true
		for _, parameter := range fn.Type.Parameters {
			if isUnknownSemanticType(parameter) {
				complete = false
				break
			}
		}
		if complete {
			contracts[fn.Name] = fn.Type
		}
	}
	putType := func(node *UniversalASTNode, typ SemanticType) error {
		encoded, err := json.Marshal(typ)
		if err != nil {
			return err
		}
		if node.Fields == nil {
			node.Fields = map[string]json.RawMessage{}
		}
		node.Fields["type_ref"] = append(json.RawMessage(nil), encoded...)
		origin, err := json.Marshal(typ.TypeOrigin)
		if err != nil {
			return err
		}
		node.Fields["type_origin"] = append(json.RawMessage(nil), origin...)
		return nil
	}
	for id, common := range graph.common {
		if common.Kind != "function" {
			continue
		}
		// Node-attached FUNCTION contracts are authoritative even when the
		// transport omits a declaration name or keeps it only in a binding.
		// Resolve by canonical node identity first; name matching is only the
		// cross-unit summary fallback below.
		var payload SemanticFunctionContract
		if attached, err := contractForNode(u, id, SemanticFunctionContractKind, &payload); err != nil {
			return err
		} else if attached {
			result := SemanticType{Kind: "void", TypeOrigin: "derived"}
			if len(payload.Results) == 1 {
				result = payload.Results[0]
			} else if len(payload.Results) > 1 {
				result = SemanticType{Kind: "tuple", Parameters: append([]SemanticType(nil), payload.Results...), TypeOrigin: "derived"}
			}
			params := make([]SemanticType, 0, len(payload.Parameters))
			complete := true
			for _, parameter := range payload.Parameters {
				if isUnknownSemanticType(parameter.Type) {
					complete = false
					break
				}
				params = append(params, parameter.Type)
			}
			if complete && !isUnknownSemanticType(result) {
				contract := SemanticType{Kind: "function", Name: common.Name, Parameters: params, Result: &result, TypeOrigin: "derived"}
				if err := putType(graph.nodes[id], contract); err != nil {
					return err
				}
				parameters := graph.many(id, "parameter")
				if len(parameters) == len(params) {
					for i, parameter := range parameters {
						if err := putType(graph.nodes[parameter.ID], params[i]); err != nil {
							return err
						}
					}
				}
			}
		}
		contract, ok := contracts[common.Name]
		if !ok {
			continue
		}
		if err := putType(graph.nodes[id], contract); err != nil {
			return err
		}
		parameters := graph.many(id, "parameter")
		if len(parameters) != len(contract.Parameters) {
			continue
		}
		for i, parameter := range parameters {
			if err := putType(graph.nodes[parameter.ID], contract.Parameters[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// CompleteCanonicalUASTContracts closes the transport boundary before a
// SemanticProgram is serialized. It only projects contracts already interned
// and referenced by the canonical UAST; it never infers types or reads source.
func CompleteCanonicalUASTContracts(p *SemanticProgram) error {
	if p == nil {
		return fmt.Errorf("missing semantic program")
	}
	u, err := canonicalUniversalAST(p)
	if err != nil {
		return err
	}
	if err := applyProjectCallableContractsToUAST(u, SemanticUnitSummary{}); err != nil {
		return err
	}
	p.UniversalAST = u
	return nil
}

func projectCallableRecoveryCachePath(key string) string {
	return filepath.Join(".semantic-cache", "callable-contracts-v1", key+".json")
}

func loadPersistedCallableContracts(key string) ([]recoveredCallableContract, bool) {
	data, err := os.ReadFile(projectCallableRecoveryCachePath(key))
	if err != nil {
		return nil, false
	}
	var contracts []recoveredCallableContract
	if json.Unmarshal(data, &contracts) != nil {
		return nil, false
	}
	return contracts, true
}

func persistCallableContracts(key string, contracts []recoveredCallableContract) {
	path := projectCallableRecoveryCachePath(key)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	data, err := json.Marshal(contracts)
	if err != nil {
		return
	}
	// A deterministic replacement is sufficient: the key is content-addressed
	// and callers only observe a complete JSON record.
	_ = os.WriteFile(path, data, 0644)
}

// LoadSemanticProject records only unit metadata. Bodies are parsed lazily by
// CompileSemanticProject and are never merged or retained project-wide.
func LoadSemanticProject(dir string, entry string) (*SemanticProject, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".semantic-cache" {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if ext == ".se" || ext == ".sp" || ext == ".spz" || (ext == ".json" && isSemanticJSONFile(path)) {
				paths = append(paths, path)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return nil, fmt.Errorf("directory contains no semantic units")
	}
	p := &SemanticProject{EntryPoint: entry, Index: SemanticProjectIndex{Units: map[string]string{}, Symbols: map[string]ProjectSymbol{}, Dependencies: map[string][]string{}, Summaries: map[string]SemanticUnitSummary{}}}
	for _, path := range paths {
		id := filepath.ToSlash(strings.TrimPrefix(path, filepath.Clean(dir)+string(os.PathSeparator)))
		// Parsing is deferred to exactly one worker invocation. Roots and symbol
		// summaries are populated from that unit-local compile result instead of
		// forcing a second parse or retaining all UAST bodies globally.
		p.Units = append(p.Units, &SemanticCompilationUnit{ID: id, Path: path})
		p.Index.Units[id] = ""
	}
	return p, nil
}

// buildSemanticProjectSummaries is a bounded prepass. It loads one transport
// unit, extracts only compact symbol facts, and releases its body before the
// next unit. The resulting index is available before any machine lowering.
func buildSemanticProjectSummaries(p *SemanticProject, recoverSourceContracts bool) error {
	for _, unit := range p.Units {
		if summary, ok := loadSemanticUnitSummarySidecar(unit.Path, unit.ID); ok {
			p.Index.Summaries[unit.ID] = summary
			p.Index.Units[unit.ID] = summary.Root
			unit.SemanticRoot = summary.Root
			publishProjectSummary(&p.Index, unit.ID, summary)
			continue
		}
		// A cache hit remains useful even when another unit changed. The cache
		// loader deliberately keeps every valid per-unit summary in Index and
		// returns false only to request rebuilding missing/invalid entries.
		if summary, ok := p.Index.Summaries[unit.ID]; ok && summary.Root != "" {
			unit.SemanticRoot = summary.Root
			p.Index.Units[unit.ID] = summary.Root
			continue
		}
		data, err := os.ReadFile(unit.Path)
		if err != nil {
			return err
		}
		program, err := loadSemanticUnitBytes(unit.Path, data)
		if err != nil {
			return fmt.Errorf("summary %s: %w", unit.ID, err)
		}
		u, err := canonicalUniversalAST(program)
		if err != nil {
			return fmt.Errorf("summary %s: %w", unit.ID, err)
		}
		graph, err := projectSummaryGraph(u)
		if err != nil {
			return fmt.Errorf("summary %s graph: %w", unit.ID, err)
		}
		summary := SemanticUnitSummary{Schema: semanticUnitSummarySchema, UnitID: unit.ID, Root: stableBytesHash(mustJSONBytes(u))}
		pkg := u.Metadata["package"]
		if pkg == "" {
			pkg = u.Metadata["package_name"]
		}
		summary.Package = pkg
		summary.ModulePath = semanticProjectModulePath(program.Extensions, u.Extensions)
		var bindings map[string]string
		bindingValue := u.Extensions["function_entry_bindings"]
		if bindingValue == nil {
			bindingValue = program.Extensions["function_entry_bindings"]
		}
		encoded, _ := json.Marshal(bindingValue)
		_ = json.Unmarshal(encoded, &bindings)
		bindingNames := make(map[string]string, len(bindings))
		for source, binding := range bindings {
			bindingNames[binding] = source
		}
		for name, binding := range bindings {
			qualified := name
			if pkg != "" {
				qualified = pkg + "." + name
			}
			id := projectFunctionLabel(qualified)
			fn := ProjectFunctionSummary{ID: id, Name: name, ABI: "win64"}
			summary.Functions = append(summary.Functions, fn)
			summary.Symbols = append(summary.Symbols, ProjectSymbolSummary{ID: id, Name: name, Kind: "function", ABI: "win64"})
			if binding != "" {
				summary.Exports = append(summary.Exports, ProjectSymbolReference{ID: id, Name: name, Kind: "function"})
			}
		}
		// Some transports omit function_entry_bindings while retaining the
		// canonical declaration nodes.  The prepass must still expose those
		// symbols before any unit body is lowered.
		for _, node := range u.Nodes {
			kind := strings.ToLower(node.StructuralKind)
			if kind != "functiondecl" && kind != "function" && kind != "methoddecl" && kind != "globaldecl" && kind != "variabledecl" && kind != "vardecl" {
				continue
			}
			var name string
			for _, raw := range []json.RawMessage{node.Fields["name"], node.Attributes["name"]} {
				if len(raw) > 0 && json.Unmarshal(raw, &name) == nil && name != "" {
					break
				}
			}
			if name == "" {
				continue
			}
			qualified := name
			if pkg != "" {
				qualified = pkg + "." + name
			}
			id := projectFunctionLabel(qualified)
			if kind == "globaldecl" || kind == "variabledecl" || kind == "vardecl" {
				id = projectGlobalLabel(qualified)
				summary.Globals = append(summary.Globals, ProjectGlobalSummary{ID: id, Name: name, Mutable: true, NodeID: node.ID})
				summary.Symbols = append(summary.Symbols, ProjectSymbolSummary{ID: id, Name: name, Kind: "global"})
			} else {
				summary.Functions = append(summary.Functions, ProjectFunctionSummary{ID: id, Name: name, ABI: "win64"})
				summary.Symbols = append(summary.Symbols, ProjectSymbolSummary{ID: id, Name: name, Kind: "function", ABI: "win64"})
			}
		}
		// Replace name-only function rows with the graph's complete callable
		// contract. The graph is unit-local and released with the body below.
		functionNodeIDs := make([]int, 0)
		for id, common := range graph.common {
			if common.Kind == "function" {
				functionNodeIDs = append(functionNodeIDs, id)
			}
		}
		sort.Ints(functionNodeIDs)
		for _, id := range functionNodeIDs {
			common := graph.common[id]
			if common.Kind != "function" {
				continue
			}
			name := canonicalFunctionBindingName(graph, id, bindingNames)
			if name == "" {
				continue
			}
			qualified := name
			if pkg != "" {
				qualified = pkg + "." + name
			}
			fnID := projectFunctionLabel(qualified)
			params := make([]SemanticType, 0)
			for _, parameter := range graph.many(id, "parameter") {
				params = append(params, graph.common[parameter.ID].Type)
			}
			resultType := common.Type.Result
			if resultType == nil {
				if hasFunctionReturn(graph, id) {
					// The return is structurally present, but this transport
					// omitted its scalar type. Preserve the proven scalar ABI
					// shape; do not synthesize a return value.
					fallback := SemanticType{Kind: "integer", Bits: 64, TypeOrigin: "derived"}
					resultType = &fallback
				}
			}
			fnType := SemanticType{Kind: "function", Name: name, Parameters: params, Result: resultType}
			for i := range summary.Functions {
				if summary.Functions[i].ID == fnID {
					summary.Functions[i].Type = fnType
				}
			}
			for i := range summary.Symbols {
				if summary.Symbols[i].ID == fnID {
					summary.Symbols[i].Type = fnType
				}
			}
		}
		// Older semantic-only transports may retain exact source provenance but
		// contain an Invalid/unknown callable contract. Recover only the compact
		// declaration facts from that source, never the executable body. This is
		// a repair of transport loss, not a source-language lowering fallback.
		if recoverSourceContracts {
			recoverProjectCallableContracts(u, &summary)
		}
		// Rehydrate contracts that are already interned in this UAST transport
		// before publishing the summary. This keeps the global index and the
		// unit-local declaration plane identical without source recovery.
		if err := applyProjectCallableContractsToGraph(u, summary, graph); err != nil {
			return fmt.Errorf("summary %s contract projection: %w", unit.ID, err)
		}
		// Contract projection mutates only type_ref/type_origin on declaration
		// nodes. Re-decode those touched nodes through the existing node/edge
		// index instead of rebuilding a second complete projectSummaryGraph.
		for id, previous := range graph.common {
			if previous.Kind != "function" || graph.nodes[id] == nil {
				continue
			}
			common, decodeErr := decodeUniversalCommon(graph.nodes[id])
			if decodeErr != nil {
				return fmt.Errorf("summary %s refreshed function %d: %w", unit.ID, id, decodeErr)
			}
			if common.Kind != "function" || common.Name == "" || common.Type.Result == nil {
				continue
			}
			params := make([]SemanticType, 0)
			for _, parameter := range graph.many(id, "parameter") {
				parameterCommon, decodeErr := decodeUniversalCommon(graph.nodes[parameter.ID])
				if decodeErr != nil {
					return fmt.Errorf("summary %s refreshed parameter %d: %w", unit.ID, parameter.ID, decodeErr)
				}
				params = append(params, parameterCommon.Type)
			}
			fnType := SemanticType{Kind: "function", Name: common.Name, Parameters: params, Result: common.Type.Result, TypeOrigin: "derived"}
			for i := range summary.Functions {
				if summary.Functions[i].Name == common.Name || summary.Functions[i].ID == projectFunctionLabel(common.Name) {
					summary.Functions[i].Type = fnType
				}
			}
			for i := range summary.Symbols {
				if summary.Symbols[i].Name == common.Name && summary.Symbols[i].Kind == "function" {
					summary.Symbols[i].Type = fnType
				}
			}
		}
		if err := collectExecutableUnitFacts(graph, u, program.Extensions, unit.ID, pkg, bindingNames, &summary); err != nil {
			return fmt.Errorf("summary %s executable closure facts: %w", unit.ID, err)
		}
		// Quotient declarations from extension and node projections by canonical
		// symbol identity so the index remains deterministic and duplicate-free.
		uniqueSymbols := make(map[string]ProjectSymbolSummary, len(summary.Symbols))
		for _, symbol := range summary.Symbols {
			uniqueSymbols[symbol.ID] = symbol
		}
		summary.Symbols = summary.Symbols[:0]
		for _, symbol := range uniqueSymbols {
			summary.Symbols = append(summary.Symbols, symbol)
		}
		for _, mod := range u.Origin.Modules {
			summary.Imports = append(summary.Imports, ProjectSymbolReference{Name: mod, Kind: "module"})
		}
		sort.Slice(summary.Functions, func(i, j int) bool { return summary.Functions[i].ID < summary.Functions[j].ID })
		sort.Slice(summary.Symbols, func(i, j int) bool { return summary.Symbols[i].ID < summary.Symbols[j].ID })
		p.Index.Summaries[unit.ID] = summary
		unit.SemanticRoot = summary.Root
		p.Index.Units[unit.ID] = summary.Root
		// The summary is the compact, authoritative declaration plane. Populate
		// the project index before unit lowering so cross-unit callers can resolve
		// canonical identity and ABI without loading another unit body.
		for _, symbol := range summary.Symbols {
			qualified := symbol.Name
			if summary.Package != "" && !strings.Contains(qualified, ".") {
				qualified = summary.Package + "." + qualified
			}
			linkage := "internal"
			if containsProjectReference(summary.Exports, symbol.ID) {
				linkage = "project"
			}
			p.Index.Symbols[symbol.ID] = ProjectSymbol{ID: symbol.ID, UnitID: unit.ID, Name: symbol.Name, QualifiedName: qualified, Visibility: "project", Linkage: linkage, Type: symbol.Type, ABI: symbol.ABI, External: symbol.External}
		}
		program, u = nil, nil
	}
	return nil
}

func loadSemanticUnitSummarySidecar(unitPath, unitID string) (SemanticUnitSummary, bool) {
	for _, path := range []string{unitPath + ".summary.json", filepath.Join(filepath.Dir(unitPath), "summary", filepath.Base(unitPath)+".json")} {
		b, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var summary SemanticUnitSummary
		if json.Unmarshal(b, &summary) == nil && summary.Schema == semanticUnitSummarySchema && summary.UnitID == unitID && summary.Root != "" {
			return summary, true
		}
	}
	return SemanticUnitSummary{}, false
}

// BuildSemanticUnitSummary creates the compact declaration record for one
// emitted semantic unit. The sidecar contains no executable body and can be
// loaded before LLVM lowering.
func BuildSemanticUnitSummary(path, unitID string) (SemanticUnitSummary, error) {
	p := &SemanticProject{
		Units: []*SemanticCompilationUnit{{ID: unitID, Path: path}},
		Index: SemanticProjectIndex{
			Units: map[string]string{}, Symbols: map[string]ProjectSymbol{},
			Dependencies: map[string][]string{}, Summaries: map[string]SemanticUnitSummary{},
		},
	}
	if err := buildSemanticProjectSummaries(p, false); err != nil {
		return SemanticUnitSummary{}, err
	}
	summary, ok := p.Index.Summaries[unitID]
	if !ok {
		return SemanticUnitSummary{}, fmt.Errorf("summary %s was not produced", unitID)
	}
	return summary, nil
}

func publishProjectSummary(index *SemanticProjectIndex, unitID string, summary SemanticUnitSummary) {
	if index == nil {
		return
	}
	if index.Units == nil {
		index.Units = map[string]string{}
	}
	if index.Symbols == nil {
		index.Symbols = map[string]ProjectSymbol{}
	}
	if index.Summaries == nil {
		index.Summaries = map[string]SemanticUnitSummary{}
	}
	index.Summaries[unitID] = summary
	index.Units[unitID] = summary.Root
	for _, symbol := range summary.Symbols {
		qualified := symbol.Name
		if summary.Package != "" && !strings.Contains(qualified, ".") {
			qualified = summary.Package + "." + qualified
		}
		linkage := "internal"
		if containsProjectReference(summary.Exports, symbol.ID) {
			linkage = "project"
		}
		index.Symbols[symbol.ID] = ProjectSymbol{ID: symbol.ID, UnitID: unitID, Name: symbol.Name, QualifiedName: qualified, Visibility: "project", Linkage: linkage, Type: symbol.Type, ABI: symbol.ABI, External: symbol.External}
	}
}

// projectSummaryGraph is deliberately smaller than uastExecutionGraph. The
// project index needs declaration facts only; running executable validation,
// crosswalk checks, and all body-level consumers here rebuilt the same graph
// before every unit was lowered. LLVM still performs the complete unit-local
// execution-graph validation later.
func projectSummaryGraph(u *UniversalASTDocument) (*uastExecutionGraph, error) {
	if u == nil || len(u.Nodes) == 0 {
		return nil, fmt.Errorf("universal AST has no declaration nodes")
	}
	children, err := universalChildrenByRole(u)
	if err != nil {
		return nil, err
	}
	graph := &uastExecutionGraph{
		document: u,
		nodes:    make(map[int]*UniversalASTNode, len(u.Nodes)),
		common:   make(map[int]universalDecodedCommon, len(u.Nodes)),
		children: children,
	}
	for i := range u.Nodes {
		n := &u.Nodes[i]
		common, err := decodeUniversalCommon(n)
		if err != nil {
			return nil, err
		}
		graph.nodes[n.ID] = n
		graph.common[n.ID] = common
	}
	return graph, nil
}

func recoverProjectCallableContracts(u *UniversalASTDocument, summary *SemanticUnitSummary) {
	if u == nil || summary == nil || !strings.EqualFold(u.Origin.SourceLanguage, "go") {
		return
	}
	needsRecovery := len(summary.Functions) == 0
	for _, fn := range summary.Functions {
		if fn.Type.Result == nil {
			needsRecovery = true
			break
		}
		for _, parameter := range fn.Type.Parameters {
			if isUnknownSemanticType(parameter) {
				needsRecovery = true
				break
			}
		}
		if needsRecovery {
			break
		}
	}
	if !needsRecovery {
		// The summary already carries complete callable contracts. Rebuilding a
		// second source-derived graph here would only duplicate work and retain
		// no additional semantic fact.
		return
	}
	files := map[string]bool{}
	for _, node := range u.Nodes {
		if node.Source != nil && strings.EqualFold(filepath.Ext(node.Source.File), ".go") {
			files[node.Source.File] = true
		}
	}
	sources := make(map[string][]byte, len(files))
	for source := range files {
		data, err := readSemanticProvenanceSource(source)
		if err == nil {
			sources[source] = data
		}
	}
	// Readable .se exports may preserve the exact source surface while omitting
	// source-span file paths.  The surface is already an explicit provenance
	// fact, so use it as the recovery input when no external path is available.
	// This restores declaration contracts only; it does not import or lower a
	// second executable body and never supplies a guessed type.
	if len(sources) == 0 && u.Surface != nil {
		if data, err := u.Surface.Bytes(); err == nil {
			sources["<semantic-surface>.go"] = data
		}
	}
	for source, data := range sources {
		cacheKey := stableBytesHash(data)
		projectCallableRecoveryCache.RLock()
		cached, found := projectCallableRecoveryCache.entries[cacheKey]
		projectCallableRecoveryCache.RUnlock()
		if found {
			applyRecoveredCallableContracts(summary, cached)
			continue
		}
		if persisted, ok := loadPersistedCallableContracts(cacheKey); ok {
			projectCallableRecoveryCache.Lock()
			projectCallableRecoveryCache.entries[cacheKey] = append([]recoveredCallableContract(nil), persisted...)
			projectCallableRecoveryCache.Unlock()
			applyRecoveredCallableContracts(summary, persisted)
			continue
		}
		recovered, err := LowerNativeGo(source, string(data))
		if err != nil || recovered == nil {
			continue
		}
		recoveredAST, err := canonicalUniversalAST(recovered)
		if err != nil {
			continue
		}
		recoveredGraph, err := newUASTExecutionGraph(recoveredAST)
		if err != nil {
			continue
		}
		bindings := semanticFunctionBindings(recovered)
		ordered := make([]string, 0, len(bindings))
		for binding := range bindings {
			ordered = append(ordered, binding)
		}
		sort.Strings(ordered)
		functionIDs := make([]int, 0)
		for id, common := range recoveredGraph.common {
			if common.Kind == "function" {
				functionIDs = append(functionIDs, id)
			}
		}
		sort.Ints(functionIDs)
		recoveredContracts := make([]recoveredCallableContract, 0, len(functionIDs))
		for ordinal, id := range functionIDs {
			common := recoveredGraph.common[id]
			name := common.Name
			if name == "" {
				name = bindings[common.Operation.FunctionBinding]
			}
			if name == "" && ordinal < len(ordered) {
				name = bindings[ordered[ordinal]]
			}
			// The recovered declaration is authoritative for transport-level
			// callable facts even when the semantic-only copy is currently
			// unknown or has no parameters.  An empty parameter/result list is a
			// valid zero-argument/void contract and must not be discarded.
			if name == "" {
				continue
			}
			// Some source-derived transports leave the function node's own
			// SemanticType empty while retaining typed parameter and return
			// nodes. Reconstruct only that existing declaration contract; no
			// executable body or guessed value is introduced.
			params := make([]SemanticType, 0)
			for _, parameter := range recoveredGraph.many(id, "parameter") {
				params = append(params, recoveredGraph.common[parameter.ID].Type)
			}
			result := common.Type.Result
			if result == nil {
				if inferred, found := inferFunctionResultType(recoveredGraph, id); found {
					result = &inferred
				} else {
					void := SemanticType{Kind: "void", TypeOrigin: "derived"}
					result = &void
				}
			}
			complete := true
			for _, parameter := range params {
				if isUnknownSemanticType(parameter) {
					complete = false
					break
				}
			}
			if !complete || isUnknownSemanticType(*result) {
				continue
			}
			common.Type = SemanticType{Kind: "function", Name: name, Parameters: params, Result: result, TypeOrigin: "derived"}
			recoveredContracts = append(recoveredContracts, recoveredCallableContract{Name: name, Type: common.Type})
			for i := range summary.Functions {
				if summary.Functions[i].Name == name {
					summary.Functions[i].Type = common.Type
				}
			}
		}
		projectCallableRecoveryCache.Lock()
		projectCallableRecoveryCache.entries[cacheKey] = append([]recoveredCallableContract(nil), recoveredContracts...)
		projectCallableRecoveryCache.Unlock()
		persistCallableContracts(cacheKey, recoveredContracts)
		recovered = nil
		recoveredAST = nil
		recoveredGraph = nil
	}
}

// readSemanticProvenanceSource resolves the source path recorded in a
// semantic transport without changing semantic meaning.  Transports may
// retain a path relative to their export bundle rather than the current
// checkout; the ordered candidates keep this lookup deterministic.
func readSemanticProvenanceSource(source string) ([]byte, error) {
	candidates := []string{source}
	if !filepath.IsAbs(source) {
		// Exported transports commonly preserve paths such as
		// ..\\..\\outputs\\gui-go-se-closure\\go-source\\... .  Resolve the
		// stable workspace-relative `outputs` suffix instead of interpreting
		// the exporter traversal from the current process directory.
		lower := strings.ToLower(filepath.ToSlash(source))
		if at := strings.Index(lower, "outputs/"); at >= 0 {
			candidates = append(candidates, filepath.FromSlash(source[at:]))
		}
		candidates = append(candidates,
			filepath.Join("outputs", "gui-go-se-closure", "go-source", source),
			filepath.Join("outputs", "gui-maximalbuild-2026-09-08", "go-source", source),
		)
	}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == "" || seen[candidate] {
			continue
		}
		seen[candidate] = true
		if data, err := os.ReadFile(candidate); err == nil {
			return data, nil
		}
	}
	return nil, os.ErrNotExist
}

func mustJSONBytes(v any) []byte { b, _ := json.Marshal(v); return b }

// isSemanticJSONFile prevents build journals, diagnostics and arbitrary JSON
// files living beside a project from being mistaken for semantic units. It
// only inspects a bounded prefix and recognizes the stable semantic transport
// markers; full decoding remains deferred to the prepare stage.
func isSemanticJSONFile(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	if len(b) > 16384 {
		b = b[:16384]
	}
	s := string(b)
	return strings.Contains(s, `"schema":"r2many.semantic-program"`) ||
		strings.Contains(s, `"basis_sha256"`) ||
		strings.Contains(s, `"universal_ast"`)
}

// CompileSemanticProject lowers units independently and links their machine
// fragments. No merged SemanticProgram is created.
func CompileSemanticProject(p *SemanticProject, opts CompileOptions) (CompileResult, error) {
	if p == nil || len(p.Units) == 0 {
		return CompileResult{}, fmt.Errorf("empty semantic project")
	}
	// Apply the same target-file selection contract used by source package
	// loading to semantic units.  A project may contain mutually exclusive
	// platform files (for example *_windows.se and *_other.se), both exporting
	// the package entry.  They are alternative units, not duplicate symbols.
	// Filter only when the filename carries an explicit platform suffix; units
	// without one remain part of the project.  Restore the caller's unit slice
	// after compilation so this planning detail never mutates project ownership.
	originalUnits := p.Units
	p.Units = selectSemanticTargetUnits(originalUnits, opts.TargetOS)
	defer func() { p.Units = originalUnits }()
	if len(p.Units) == 0 {
		return CompileResult{}, fmt.Errorf("no semantic units match target %q", opts.TargetOS)
	}
	moduleBase := opts.ModuleBaseDir
	if moduleBase == "" {
		moduleBase = filepath.Dir(p.Units[0].Path)
	}
	if err := expandSemanticProjectModules(p, moduleBase, opts.ModuleStoreRoot); err != nil {
		return CompileResult{}, fmt.Errorf("SEMANTIC_PROJECT_MODULE_UNITS: %w", err)
	}
	if !loadProjectSummaryCache(p, opts.CacheDir) {
		if err := buildSemanticProjectSummaries(p, false); err != nil {
			return CompileResult{}, err
		}
		saveProjectSummaryCache(p, opts.CacheDir)
	}
	var projectClosure ExecutableClosure
	if opts.OutputKind == CompileExecutable {
		root := strings.TrimSpace(opts.EntryPoint)
		if root == "" {
			root = strings.TrimSpace(p.EntryPoint)
		}
		opts.EntryPoint = root
		closure, err := projectExecutableClosure(p, []string{root}, opts.ExecutableClosureReportPath)
		if err != nil {
			return CompileResult{}, err
		}
		projectClosure = closure
		p.Units, err = filterProjectUnitsToClosure(p.Units, closure)
		if err != nil {
			return CompileResult{}, fmt.Errorf("EXECUTABLE_CLOSURE_UNITS: %w", err)
		}
	}
	projectIndexFingerprint := semanticProjectIndexFingerprint(p.Index)
	// Entry ownership is a project-index fact. Resolve it from the compact
	// summaries before workers begin releasing unit bodies. Relying solely on a
	// freshly parsed body made entry discovery depend on which transport plane
	// (SemanticProgram or UniversalAST extensions) happened to retain aliases.
	entryByUnit := make(map[string]bool)
	if opts.EntryPoint != "" {
		for _, unit := range p.Units {
			summary := p.Index.Summaries[unit.ID]
			for _, function := range summary.Functions {
				if function.Name == opts.EntryPoint {
					entryByUnit[unit.ID] = true
					break
				}
			}
		}
	}
	// Bounded pipeline: bytes are prefetched, programs are parsed lazily, and
	// only W units may hold a complete canonical body at once. The semaphore is
	// acquired before parsing (rather than before compile) so a fast parser
	// cannot fill an unbounded queue of UASTs while workers are busy.
	workers := runtime.NumCPU() * 80 / 100
	// The default project pipeline keeps enough runnable workers to saturate
	// large hosts even when the Go runtime reports a conservative CPU quota.
	// An explicit MaxWorkers or RAM budget may still lower this value.
	if workers < 32 {
		workers = 32
	}
	if opts.MaxWorkers > 0 && workers > opts.MaxWorkers {
		workers = opts.MaxWorkers
	}
	if opts.MemoryBudgetBytes > 0 && opts.UnitPeakBytes > 0 {
		available := opts.MemoryBudgetBytes - opts.GlobalMemoryBytes - opts.QueueMemoryBytes - opts.LinkMemoryBytes
		if available <= 0 {
			workers = 1
		} else if ramWorkers := int(available / opts.UnitPeakBytes); ramWorkers > 0 && workers > ramWorkers {
			workers = ramWorkers
		}
	}
	if workers > len(p.Units) {
		workers = len(p.Units)
	}
	if workers < 1 {
		workers = 1
	}
	// Keep every queue bounded to the number of compile slots.  A 2W load
	// queue allowed large transport payloads to accumulate while compilation
	// was busy, defeating lazy body loading and increasing GC pressure.  One
	// item per slot preserves overlap without retaining an extra full batch.
	queueSize := workers
	pipelineStart := time.Now()
	var loadPeak, preparePeak, fragmentPeak atomic.Int64
	trackPeak := func(dst *atomic.Int64, value int) {
		for {
			old := dst.Load()
			if int64(value) <= old || dst.CompareAndSwap(old, int64(value)) {
				return
			}
		}
	}
	type loadedUnit struct {
		index int
		unit  *SemanticCompilationUnit
		data  []byte
	}
	type preparedUnit struct {
		index   int
		unit    *SemanticCompilationUnit
		program *SemanticProgram
	}
	type unitResult struct {
		index    int
		r        CompileResult
		err      error
		hasEntry bool
	}
	loadCh := make(chan loadedUnit, queueSize)
	prepareCh := make(chan preparedUnit, workers)
	fragmentCh := make(chan unitResult, queueSize)
	resultCh := make(chan unitResult, queueSize)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var loadWG, prepWG, compileWG sync.WaitGroup
	loadWG.Add(1)
	go func() {
		defer loadWG.Done()
		defer close(loadCh)
		for i, unit := range p.Units {
			select {
			case <-ctx.Done():
				return
			default:
			}
			// A complete target fragment cache hit is resolved from the compact
			// semantic root and project index. Do this before opening the unit body
			// so unchanged units need neither an input read nor a Semantic/UAST parse.
			entry := ""
			if opts.EntryPoint != "" && entryByUnit[unit.ID] {
				entry = opts.EntryPoint
			}
			cacheKey := projectUnitCompileCacheKey(unit, opts, projectIndexFingerprint, entry)
			if cached, ok := loadProjectUnitCompileCache(opts.CacheDir, cacheKey); ok {
				select {
				case fragmentCh <- unitResult{index: i, r: cached.result, hasEntry: cached.hasEntry}:
					trackPeak(&fragmentPeak, len(fragmentCh))
				case <-ctx.Done():
					return
				}
				continue
			}
			var data []byte
			var err error
			if unit.Program == nil {
				data, err = os.ReadFile(unit.Path)
			}
			if err != nil {
				resultCh <- unitResult{index: i, err: err}
				continue
			}
			select {
			case loadCh <- loadedUnit{index: i, unit: unit, data: data}:
				trackPeak(&loadPeak, len(loadCh))
			case <-ctx.Done():
				return
			}
		}
	}()
	// One preparation worker per compile worker, but the token bound below
	// guarantees at most W parsed programs in memory.
	bodyTokens := make(chan struct{}, workers)
	prepWorkers := workers
	if prepWorkers > 8 {
		prepWorkers = 8
	} // parsing is cheap; avoid parser oversubscription
	for n := 0; n < prepWorkers; n++ {
		prepWG.Add(1)
		go func() {
			defer prepWG.Done()
			for in := range loadCh {
				select {
				case bodyTokens <- struct{}{}:
				case <-ctx.Done():
					return
				}
				var program *SemanticProgram
				var err error
				if in.unit.Program != nil {
					program = in.unit.Program
				} else {
					program, err = loadSemanticUnitBytes(in.unit.Path, in.data)
				}
				if err != nil {
					<-bodyTokens
					resultCh <- unitResult{index: in.index, err: err}
					continue
				}
				select {
				case prepareCh <- preparedUnit{index: in.index, unit: in.unit, program: program}:
					trackPeak(&preparePeak, len(prepareCh))
				case <-ctx.Done():
					<-bodyTokens
					return
				}
			}
		}()
	}
	go func() { prepWG.Wait(); close(prepareCh) }()
	for n := 0; n < workers; n++ {
		compileWG.Add(1)
		go func() {
			defer compileWG.Done()
			for in := range prepareCh {
				localOpts := opts
				localOpts.ProjectMode = true
				localOpts.ProjectIndex = &p.Index
				localOpts.ProjectUnitID = in.unit.ID
				if localOpts.ModuleBaseDir == "" {
					localOpts.ModuleBaseDir = filepath.Dir(in.unit.Path)
				}
				localOpts.projectIndexFingerprint = projectIndexFingerprint
				localOpts.projectUnitSemanticRoot = in.unit.SemanticRoot
				// Project fragments are cacheable once their semantic root and
				// dependency ABI contracts are part of the cache key.  Keep the
				// caller's cache policy; CompileMachine stores relocations and
				// symbols alongside the fragment rather than flattening them.
				// The requested entry belongs only to the unit that defines it.
				// Applying it to every unit makes ordinary library units fail with
				// "entry not found" and aborts an otherwise valid project.
				hasEntry := localOpts.EntryPoint != "" && (entryByUnit[in.unit.ID] || semanticProgramHasFunction(in.program, localOpts.EntryPoint))
				if localOpts.EntryPoint != "" && !hasEntry {
					localOpts.EntryPoint = ""
				}
				localOpts.ProjectInitializers = nil
				if hasEntry {
					localOpts.ProjectInitializers = append([]ExecutableFunctionRef(nil), projectClosure.InitializerFunctions...)
				}
				r, err := CompileMachine(in.program, localOpts)
				if err == nil {
					entry := ""
					if hasEntry {
						entry = opts.EntryPoint
					}
					cacheKey := projectUnitCompileCacheKey(in.unit, opts, projectIndexFingerprint, entry)
					saveProjectUnitCompileCache(opts.CacheDir, cacheKey, r, hasEntry)
				}
				// Release the full UAST before making the compact result visible.
				in.program = nil
				in.unit.Program = nil
				<-bodyTokens
				select {
				case fragmentCh <- unitResult{index: in.index, r: r, err: err, hasEntry: hasEntry}:
					trackPeak(&fragmentPeak, len(fragmentCh))
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	// Fragment writer stage drops bulky whole-program buffers before the linker
	// collector sees a result. Only fragments, symbols and compact metadata
	// cross this boundary.
	var writeWG sync.WaitGroup
	writeWG.Add(1)
	go func() {
		defer writeWG.Done()
		defer close(resultCh)
		for x := range fragmentCh {
			x.r.Bytes = nil
			x.r.ObjectBytes = nil
			x.r.Text = ""
			select {
			case resultCh <- x:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() {
		loadWG.Wait()
		compileWG.Wait()
		close(fragmentCh)
		writeWG.Wait()
	}()
	results := make([]unitResult, len(p.Units))
	for x := range resultCh {
		if x.err != nil {
			cancel()
		}
		results[x.index] = x
	}
	entryUnit := 0
	if opts.EntryPoint != "" {
		entryUnit = -1
		availableEntries := make([]string, 0)
		for i := range results {
			for _, function := range p.Index.Summaries[p.Units[i].ID].Functions {
				if function.Name != "" {
					availableEntries = append(availableEntries, function.Name)
				}
			}
			if entryByUnit[p.Units[i].ID] || results[i].hasEntry {
				if entryUnit >= 0 {
					return CompileResult{}, fmt.Errorf("project entry %q is defined by multiple units", opts.EntryPoint)
				}
				entryUnit = i
			}
		}
		if entryUnit < 0 {
			sort.Strings(availableEntries)
			return CompileResult{}, fmt.Errorf("project entry %q not found (indexed entries: %s)", opts.EntryPoint, strings.Join(uniqueStrings(availableEntries), ","))
		}
	}
	var combined = map[string]MachineFragment{}
	var funcs []x64Function
	base := 0
	var aggregate StreamingMetrics
	var projectImports []pe64ImportSpec
	functionObjects := map[string][]byte{}
	for i, unit := range p.Units {
		r, err := results[i].r, results[i].err
		if err != nil {
			return CompileResult{}, fmt.Errorf("unit %s: %w", unit.ID, err)
		}
		aggregate.SummaryMicros += r.Metrics.SummaryMicros
		aggregate.PreparationMicros += r.Metrics.PreparationMicros
		aggregate.LegalityMicros += r.Metrics.LegalityMicros
		aggregate.SelectionMicros += r.Metrics.SelectionMicros
		aggregate.EncodingMicros += r.Metrics.EncodingMicros
		aggregate.CacheHits += r.Metrics.CacheHits
		aggregate.CacheMisses += r.Metrics.CacheMisses
		projectImports = append(projectImports, r.Imports...)
		aggregate.StencilHits += r.Metrics.StencilHits
		aggregate.StencilMisses += r.Metrics.StencilMisses
		aggregate.Functions += r.Metrics.Functions
		unitHashes := make([]string, 0, len(r.Fragments))
		for _, f := range r.Fragments {
			unitHashes = append(unitHashes, f.SemanticHash)
		}
		sort.Strings(unitHashes)
		unit.SemanticRoot = stableBytesHash([]byte(strings.Join(unitHashes, "\n")))
		p.Index.Units[unit.ID] = unit.SemanticRoot
		for label, symbol := range r.projectSymbols {
			if old, exists := p.Index.Symbols[label]; exists && old.UnitID != unit.ID {
				return CompileResult{}, fmt.Errorf("project symbol %q is defined by both %s and %s", symbol.Name, old.UnitID, unit.ID)
			}
			symbol.UnitID = unit.ID
			symbol.ABI = opts.ABI
			p.Index.Symbols[label] = symbol
		}
		for name, obj := range r.FunctionObjects {
			functionObjects[fmt.Sprintf("unit_%d_%s", i, name)] = obj
		}
		prefix := fmt.Sprintf("unit_%d_", i)
		if i == entryUnit {
			prefix = ""
		}
		for name, f := range r.Fragments {
			mapped := MachineFragment{UnitID: base + f.UnitID, SemanticHash: f.SemanticHash, Text: append([]byte(nil), f.Text...), Symbols: map[string]uint32{}}
			for sym, off := range f.Symbols {
				if isProjectSymbolLabel(sym) {
					mapped.Symbols[sym] = off
				} else {
					mapped.Symbols[prefix+sym] = off
				}
			}
			for _, rel := range f.Relocations {
				if !isProjectSymbolLabel(rel.Target) {
					rel.Target = prefix + rel.Target
				} else {
					p.Index.Dependencies[unit.ID] = append(p.Index.Dependencies[unit.ID], rel.Target)
				}
				mapped.Relocations = append(mapped.Relocations, rel)
			}
			combined[fmt.Sprintf("%s:%s", unit.ID, name)] = mapped
		}
		sort.Strings(p.Index.Dependencies[unit.ID])
		p.Index.Dependencies[unit.ID] = uniqueStrings(p.Index.Dependencies[unit.ID])
		for _, fn := range r.nativeFunctions {
			if !isProjectSymbolLabel(fn.Label) {
				fn.Label = prefix + fn.Label
			}
			fn.End = prefix + fn.End
			funcs = append(funcs, fn)
		}
		max := 0
		for _, f := range r.Fragments {
			if f.UnitID+len(f.Text) > max {
				max = f.UnitID + len(f.Text)
			}
		}
		base += max
	}
	// A project label can occur in a unit-local relocation (most notably the
	// native entry trampoline). Dependencies describe only references whose
	// defining unit differs from the referencing unit.
	for unitID, dependencies := range p.Index.Dependencies {
		crossUnit := dependencies[:0]
		for _, target := range dependencies {
			definition, ok := p.Index.Symbols[target]
			if !ok || definition.UnitID != unitID {
				crossUnit = append(crossUnit, target)
			}
		}
		p.Index.Dependencies[unitID] = uniqueStrings(crossUnit)
	}
	unresolvedBeforeLink := countUnresolvedFragmentTargets(combined)
	// Every project relocation must resolve to a real fragment.  Deliberately
	// do not synthesize executable stubs here: a stub would make an incomplete
	// SemanticProgram appear linkable while silently discarding its semantics.
	linkStart := time.Now()
	code, err := linkMachineFragments(combined)
	if err != nil {
		return CompileResult{}, err
	}
	labels := map[string]int{}
	for _, f := range combined {
		for n, off := range f.Symbols {
			labels[n] = int(f.UnitID + int(off))
		}
	}
	if _, ok := labels["native_entry"]; !ok {
		return CompileResult{}, fmt.Errorf("project has no native_entry")
	}
	bytes, err := pe64Image(code, labels, funcs, projectImports)
	if err != nil {
		return CompileResult{}, err
	}
	aggregate.Units = len(p.Units)
	aggregate.Fragments = len(combined)
	aggregate.PipelineWorkers = workers
	aggregate.PipelineQueueSize = queueSize
	aggregate.PipelineMicros = time.Since(pipelineStart).Microseconds()
	aggregate.LoadQueuePeak = int(loadPeak.Load())
	aggregate.PrepareQueuePeak = int(preparePeak.Load())
	aggregate.FragmentQueuePeak = int(fragmentPeak.Load())
	aggregate.LinkMicros = time.Since(linkStart).Microseconds()
	plan := StreamingPlan{Workers: workers, QueueSize: workers * 2, Units: make([]CompilationUnit, len(p.Units))}
	for i := range plan.Units {
		plan.Units[i] = CompilationUnit{ID: i}
	}
	return CompileResult{Bytes: bytes, Fragments: combined, FunctionObjects: functionObjects, Plan: plan, OutputKind: CompileExecutable, Metrics: aggregate, NativeUnitCount: len(p.Units), NativeFragmentCount: len(combined), NativeRelocationCount: countFragmentRelocations(combined), UnresolvedProjectSymbolsBeforeLink: unresolvedBeforeLink, UnresolvedProjectSymbolsAfterLink: 0}, nil
}

// Summary cache avoids reparsing unchanged semantic units on repeated project
// builds. Entries are per-unit: editing one transport invalidates only that
// unit instead of forcing the whole project prepass to decode every large
// sibling again. The metadata key follows the same size/mtime invalidation
// contract as the fragment cache.
func projectUnitSummaryCacheKey(unit *SemanticCompilationUnit) string {
	if unit == nil {
		return ""
	}
	info, err := os.Stat(unit.Path)
	if err != nil {
		return ""
	}
	return stableBytesHash([]byte(fmt.Sprintf("semantic-unit-summary-v6|%s|%d|%d", unit.ID, info.Size(), info.ModTime().UnixNano())))
}

func projectUnitCompileCacheKey(unit *SemanticCompilationUnit, opts CompileOptions, indexFingerprint string, entry string) string {
	if unit == nil || unit.SemanticRoot == "" || indexFingerprint == "" {
		return ""
	}
	identity := struct {
		Schema, UnitID, SemanticRoot, IndexFingerprint        string
		SourceLanguage, SourceArch, SourceOS, SourceABI       string
		TargetArch, TargetOS, ABI, OutputKind, TargetLanguage string
		InputKind                                             string
		BaseAddress                                           uint64
		EntryPoint, ModuleBaseDir, ModuleStoreRoot            string
		ProjectMode, ViaAssembly, EmbedAllModules             bool
	}{
		Schema: projectUnitCompileCacheSchema, UnitID: unit.ID, SemanticRoot: unit.SemanticRoot,
		SourceLanguage: opts.SourceLanguage, SourceArch: opts.SourceArch, SourceOS: opts.SourceOS, SourceABI: opts.SourceABI,
		IndexFingerprint: indexFingerprint, TargetArch: opts.TargetArch, TargetOS: opts.TargetOS,
		ABI: opts.ABI, OutputKind: string(opts.OutputKind), TargetLanguage: opts.TargetLanguage,
		InputKind: string(opts.InputKind), BaseAddress: opts.BaseAddress, EntryPoint: entry,
		ModuleBaseDir: opts.ModuleBaseDir, ModuleStoreRoot: opts.ModuleStoreRoot,
		ProjectMode: true, ViaAssembly: opts.ViaAssembly, EmbedAllModules: opts.EmbedAllModules,
	}
	encoded, err := json.Marshal(identity)
	if err != nil {
		return ""
	}
	return stableBytesHash(encoded)
}

func loadProjectUnitCompileCache(dir, key string) (unitResultCacheValue, bool) {
	if dir == "" || key == "" {
		return unitResultCacheValue{}, false
	}
	b, err := os.ReadFile(filepath.Join(dir, "project-unit-"+key+".json"))
	if err != nil {
		return unitResultCacheValue{}, false
	}
	var record projectUnitCompileCacheRecord
	if json.Unmarshal(b, &record) != nil || record.Schema != projectUnitCompileCacheSchema || record.Key != key || !record.Completed {
		return unitResultCacheValue{}, false
	}
	r := CompileResult{Fragments: record.Fragments, FunctionObjects: record.FunctionObjects, Metrics: record.Metrics, OutputKind: record.OutputKind, InstructionCount: record.InstructionCount, nativeFunctions: record.NativeFunctions, projectSymbols: record.ProjectSymbols, Imports: record.Imports, CacheHit: true}
	r.Metrics.SummaryMicros = 0
	r.Metrics.PreparationMicros = 0
	r.Metrics.LegalityMicros = 0
	r.Metrics.SelectionMicros = 0
	r.Metrics.EncodingMicros = 0
	r.Metrics.CacheHits = 1
	r.Metrics.CacheMisses = 0
	return unitResultCacheValue{result: r, hasEntry: record.HasEntry}, true
}

type unitResultCacheValue struct {
	result   CompileResult
	hasEntry bool
}

func saveProjectUnitCompileCache(dir, key string, result CompileResult, hasEntry bool) {
	if dir == "" || key == "" {
		return
	}
	if os.MkdirAll(dir, 0755) != nil {
		return
	}
	record := projectUnitCompileCacheRecord{Schema: projectUnitCompileCacheSchema, Key: key, Fragments: result.Fragments, FunctionObjects: result.FunctionObjects, NativeFunctions: result.nativeFunctions, ProjectSymbols: result.projectSymbols, Imports: result.Imports, Metrics: result.Metrics, OutputKind: result.OutputKind, InstructionCount: result.InstructionCount, HasEntry: hasEntry, Completed: true}
	b, err := json.Marshal(record)
	if err != nil {
		return
	}
	path := filepath.Join(dir, "project-unit-"+key+".json")
	tmp := path + ".tmp"
	if os.WriteFile(tmp, b, 0644) != nil {
		return
	}
	_ = os.Remove(path)
	_ = os.Rename(tmp, path)
}

func semanticProjectIndexFingerprint(index SemanticProjectIndex) string {
	unitIDs := make([]string, 0, len(index.Summaries))
	for id := range index.Summaries {
		unitIDs = append(unitIDs, id)
	}
	sort.Strings(unitIDs)
	type indexIdentity struct {
		Units     []string
		Symbols   []ProjectSymbol
		Summaries []SemanticUnitSummary
	}
	identity := indexIdentity{Units: unitIDs, Symbols: make([]ProjectSymbol, 0, len(index.Symbols)), Summaries: make([]SemanticUnitSummary, 0, len(unitIDs))}
	symbolIDs := make([]string, 0, len(index.Symbols))
	for id := range index.Symbols {
		symbolIDs = append(symbolIDs, id)
	}
	sort.Strings(symbolIDs)
	for _, id := range symbolIDs {
		identity.Symbols = append(identity.Symbols, index.Symbols[id])
	}
	for _, id := range unitIDs {
		identity.Summaries = append(identity.Summaries, index.Summaries[id])
	}
	b, _ := json.Marshal(identity)
	return stableBytesHash(b)
}

func loadProjectSummaryCache(p *SemanticProject, dir string) bool {
	if p == nil || dir == "" {
		return false
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return false
	}
	idx := SemanticProjectIndex{Units: map[string]string{}, Symbols: map[string]ProjectSymbol{}, Dependencies: map[string][]string{}, Summaries: map[string]SemanticUnitSummary{}}
	allCached := true
	for _, unit := range p.Units {
		key := projectUnitSummaryCacheKey(unit)
		if key == "" {
			allCached = false
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, "project-summary-unit-"+key+".json"))
		if err != nil {
			allCached = false
			continue
		}
		var summary SemanticUnitSummary
		if json.Unmarshal(b, &summary) != nil || summary.Schema != semanticUnitSummarySchema || summary.UnitID != unit.ID || summary.Root == "" {
			allCached = false
			continue
		}
		idx.Summaries[unit.ID] = summary
		idx.Units[unit.ID] = summary.Root
		unit.SemanticRoot = summary.Root
		idx.Dependencies[unit.ID] = nil
		for _, symbol := range summary.Symbols {
			qualified := symbol.Name
			if summary.Package != "" && !strings.Contains(qualified, ".") {
				qualified = summary.Package + "." + qualified
			}
			linkage := "internal"
			if containsProjectReference(summary.Exports, symbol.ID) {
				linkage = "project"
			}
			idx.Symbols[symbol.ID] = ProjectSymbol{ID: symbol.ID, UnitID: unit.ID, Name: symbol.Name, QualifiedName: qualified, Visibility: "project", Linkage: linkage, Type: symbol.Type, ABI: symbol.ABI, External: symbol.External}
		}
	}
	p.Index = idx
	return allCached
}
func saveProjectSummaryCache(p *SemanticProject, dir string) {
	if p == nil || dir == "" {
		return
	}
	if os.MkdirAll(dir, 0755) != nil {
		return
	}
	for _, unit := range p.Units {
		key := projectUnitSummaryCacheKey(unit)
		if key == "" {
			continue
		}
		summary, ok := p.Index.Summaries[unit.ID]
		if !ok {
			continue
		}
		b, err := json.Marshal(summary)
		if err != nil {
			continue
		}
		path := filepath.Join(dir, "project-summary-unit-"+key+".json")
		tmp := path + ".tmp"
		if os.WriteFile(tmp, b, 0644) == nil {
			_ = os.Rename(tmp, path)
		}
	}
}

func countUnresolvedFragmentTargets(fragments map[string]MachineFragment) int {
	known := map[string]struct{}{}
	for _, fragment := range fragments {
		for symbol := range fragment.Symbols {
			known[symbol] = struct{}{}
		}
	}
	missing := map[string]struct{}{}
	for _, fragment := range fragments {
		for _, relocation := range fragment.Relocations {
			if _, ok := known[relocation.Target]; !ok {
				missing[relocation.Target] = struct{}{}
			}
		}
	}
	return len(missing)
}

func isProjectSymbolLabel(label string) bool {
	return strings.HasPrefix(label, "__project_fn_") || strings.HasPrefix(label, "__project_data_")
}

func semanticProgramHasFunction(p *SemanticProgram, name string) bool {
	if p == nil || name == "" {
		return false
	}
	// Frontends may assign a canonical binding that intentionally differs from
	// the source spelling. This map is the explicit source-name-to-binding
	// contract consumed by selectX64 as well; entry discovery must use the same
	// authority or a valid library entry is discarded before selection.
	extensions := p.Extensions
	if len(extensions) == 0 && p.UniversalAST != nil {
		extensions = p.UniversalAST.Extensions
	}
	var aliases map[string]string
	if encoded, err := json.Marshal(extensions["function_entry_bindings"]); err == nil {
		_ = json.Unmarshal(encoded, &aliases)
		if aliases[name] != "" {
			return true
		}
	}
	u := p.UniversalAST
	if u == nil {
		var err error
		u, err = canonicalUniversalAST(p)
		if err != nil {
			return false
		}
	}
	for _, n := range u.Nodes {
		if n.StructuralKind != "FunctionDecl" && n.StructuralKind != "Function" && n.StructuralKind != "MethodDecl" && n.StructuralKind != "FunctionExpr" {
			continue
		}
		if raw, ok := n.Fields["name"]; ok {
			var got string
			if json.Unmarshal(raw, &got) == nil && got == name {
				return true
			}
		}
		if raw, ok := n.Attributes["name"]; ok {
			var got string
			if json.Unmarshal(raw, &got) == nil && got == name {
				return true
			}
		}
	}
	return false
}

func loadSemanticUnitFile(path string) (*SemanticProgram, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return loadSemanticUnitBytes(path, data)
}

func loadSemanticUnitBytes(path string, data []byte) (*SemanticProgram, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".se":
		return ParseSemanticSE(data)
	case ".sp":
		return ParseSemanticSP(data)
	case ".spz":
		return ParseSemanticSPZ(data)
	case ".json":
		return ParseSemanticJSON(data)
	default:
		return nil, fmt.Errorf("unsupported semantic unit %q", path)
	}
}

func countFragmentRelocations(f map[string]MachineFragment) int {
	n := 0
	for _, x := range f {
		n += len(x.Relocations)
	}
	return n
}
