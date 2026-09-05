package backend

// Canonical UAST semantic sidecar support for the ecosystem miner.  This file
// serializes only data that already exists in the trusted Canonical UAST; it
// never inspects diagnostics, source text, or renderer output to infer a
// primitive.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const SemanticTraceSchemaVersion = "uast-semantic-trace/v1"

type SemanticTraceRoute struct {
	RouteType                string `json:"route_type"`
	ProjectionMode           string `json:"projection_mode,omitempty"`
	RuntimeFallbackUsed      bool   `json:"runtime_fallback_used"`
	DirectSuccess            bool   `json:"direct_success"`
	PrimitiveLoweringSuccess bool   `json:"primitive_lowering_success"`
	IntermediateSuccess      bool   `json:"intermediate_success"`
	IntermediateLanguage     string `json:"intermediate_language,omitempty"`
	IntermediateRoute        string `json:"intermediate_route,omitempty"`
	Leg1CaseID               string `json:"leg1_case_id,omitempty"`
	Leg2CaseID               string `json:"leg2_case_id,omitempty"`
	Leg1InputHash            string `json:"leg1_input_hash,omitempty"`
	Leg1OutputHash           string `json:"leg1_output_hash,omitempty"`
	Leg2InputHash            string `json:"leg2_input_hash,omitempty"`
	Leg2OutputHash           string `json:"leg2_output_hash,omitempty"`
	RootUASTHash             string `json:"root_uast_hash,omitempty"`
	IntermediateUASTHash     string `json:"intermediate_uast_hash,omitempty"`
	FinalUASTHash            string `json:"final_uast_hash,omitempty"`
}

type SemanticTraceNode struct {
	NodeID            string `json:"node_id"`
	ParentNodeID      string `json:"parent_node_id"`
	NodeKind          string `json:"node_kind"`
	SemanticOperation string `json:"semantic_operation"`
	SemanticFamily    string `json:"semantic_family"`
	Arity             int    `json:"arity"`
	OperandRoles      string `json:"operand_roles"`
	ResultRole        string `json:"result_role"`
	TypeModel         string `json:"type_model"`
	NumericModel      string `json:"numeric_model"`
	Effects           string `json:"effects"`
	EvaluationOrder   string `json:"evaluation_order"`
	Binding           string `json:"binding"`
	Scope             string `json:"scope"`
	Ownership         string `json:"ownership"`
	Lifetime          string `json:"lifetime"`
	Representation    string `json:"representation"`
	ControlFlow       string `json:"control_flow"`
	MemoryBehavior    string `json:"memory_behavior"`
	ExceptionBehavior string `json:"exception_behavior"`
	PrimitiveID       string `json:"primitive_id,omitempty"`
	PrimitiveFamily   string `json:"primitive_family,omitempty"`
	Parameterization  string `json:"parameterization,omitempty"`
	LanguageOperation string `json:"language_operation"`
	SourceStart       string `json:"source_start"`
	SourceEnd         string `json:"source_end"`
}

type SemanticTracePrimitiveDemand struct {
	PrimitiveID      string `json:"primitive_id"`
	PrimitiveFamily  string `json:"primitive_family"`
	Parameterization string `json:"parameterization"`
	OccurrenceCount  int    `json:"occurrence_count"`
}

type SemanticTrace struct {
	SchemaVersion    string                         `json:"schema_version"`
	ParseSuccess     bool                           `json:"parse_success"`
	UASTSuccess      bool                           `json:"uast_success"`
	UASTHash         string                         `json:"uast_hash,omitempty"`
	Nodes            []SemanticTraceNode            `json:"nodes"`
	PrimitiveDemands []SemanticTracePrimitiveDemand `json:"primitive_demands,omitempty"`
	Route            SemanticTraceRoute             `json:"route"`
	// FrontendEvidence is provenance from the same canonical UAST closure. It
	// lets downstream matrix tooling distinguish observed compiler facts from
	// ordinary UAST demand without deriving semantics from source or diagnostics.
	FrontendEvidence map[string]any `json:"frontend_evidence,omitempty"`
}

func hashCanonicalUAST(u *UniversalASTDocument) string {
	if u == nil {
		return ""
	}
	b, err := json.Marshal(u)
	if err != nil {
		return ""
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// BuildSemanticTrace materializes the same structured sidecar document used by
// the miner without performing target projection. This is the discovery-only
// entry point: every primitive comes from the canonical UAST itself.
func BuildSemanticTrace(parseSuccess bool, u *UniversalASTDocument, route SemanticTraceRoute) SemanticTrace {
	trace := SemanticTrace{SchemaVersion: SemanticTraceSchemaVersion, ParseSuccess: parseSuccess, UASTSuccess: u != nil, Route: route}
	trace.UASTHash = hashCanonicalUAST(u)
	if trace.Route.RootUASTHash == "" {
		trace.Route.RootUASTHash = trace.UASTHash
	}
	if trace.Route.FinalUASTHash == "" && trace.Route.RouteType != "INTERMEDIATE" {
		trace.Route.FinalUASTHash = trace.UASTHash
	}
	if u != nil {
		trace.Nodes, trace.PrimitiveDemands = semanticTraceNodes(u)
		if raw, ok := u.Extensions["frontend_compiler_evidence"].(map[string]any); ok {
			trace.FrontendEvidence = raw
		}
	}
	return trace
}

// WriteSemanticTrace writes atomically when path is non-empty. A nil UAST is
// a valid parse/UAST failure sidecar and is intentionally not an error.
func WriteSemanticTrace(path string, parseSuccess bool, u *UniversalASTDocument, route SemanticTraceRoute) error {
	if path == "" {
		return nil
	}
	trace := BuildSemanticTrace(parseSuccess, u, route)
	b, err := json.MarshalIndent(trace, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func semanticTraceNodes(u *UniversalASTDocument) ([]SemanticTraceNode, []SemanticTracePrimitiveDemand) {
	parents := map[int]int{}
	roles := map[int][]string{}
	for _, r := range u.Relations {
		if r.Kind != "syntax.child" || r.To.Domain != "node" {
			continue
		}
		id, err := strconv.Atoi(r.To.ID)
		if err != nil {
			continue
		}
		parents[id] = r.From
		var role string
		_ = json.Unmarshal(r.Attributes["role"], &role)
		if role != "" {
			roles[r.From] = append(roles[r.From], role)
		}
	}
	for id := range roles {
		sort.Strings(roles[id])
	}
	demand := map[string]int{}
	var nodes []SemanticTraceNode
	for _, n := range u.Nodes {
		op, primitive, family, parameter := semanticTraceClassification(&n)
		var typ SemanticType
		_ = decodeUniversalField(&n, "type", &typ)
		var effects []string
		_ = decodeUniversalField(&n, "effects", &effects)
		sort.Strings(effects)
		parent := ""
		if p, ok := parents[n.ID]; ok {
			parent = strconv.Itoa(p)
		}
		spanStart, spanEnd := semanticTraceSpan(n.Source)
		result := "statement"
		if strings.HasSuffix(n.StructuralKind, "Expr") || n.StructuralKind == "SymbolRef" {
			result = "value"
		}
		control, memory, exception := "", "", ""
		switch n.StructuralKind {
		case "IfStmt", "LoopStmt", "ForEachStmt", "SwitchMatchStmt", "ReturnStmt":
			control = "structured"
		case "AllocationExpr", "DeallocationStmt":
			memory = "structured"
		case "TryStmt", "RaisePanicStmt":
			exception = "structured"
		}
		nodes = append(nodes, SemanticTraceNode{NodeID: strconv.Itoa(n.ID), ParentNodeID: parent, NodeKind: n.StructuralKind, SemanticOperation: op, SemanticFamily: family, Arity: len(roles[n.ID]), OperandRoles: strings.Join(roles[n.ID], "|"), ResultRole: result, TypeModel: typ.Kind, NumericModel: u.Types.Numeric, Effects: strings.Join(effects, "|"), EvaluationOrder: u.Evaluation, Binding: semanticTraceBinding(u, n.ID), Scope: semanticTraceScope(u, n.ID), Ownership: typ.Ownership, Lifetime: typ.Lifetime, Representation: u.ValueModel, ControlFlow: control, MemoryBehavior: memory, ExceptionBehavior: exception, PrimitiveID: primitive, PrimitiveFamily: family, Parameterization: parameter, LanguageOperation: op, SourceStart: spanStart, SourceEnd: spanEnd})
		if primitive != "" {
			demand[primitive+"\x00"+family+"\x00"+parameter]++
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		a, _ := strconv.Atoi(nodes[i].NodeID)
		b, _ := strconv.Atoi(nodes[j].NodeID)
		return a < b
	})
	keys := make([]string, 0, len(demand))
	for k := range demand {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	demands := make([]SemanticTracePrimitiveDemand, 0, len(keys))
	for _, k := range keys {
		p := strings.Split(k, "\x00")
		demands = append(demands, SemanticTracePrimitiveDemand{PrimitiveID: p[0], PrimitiveFamily: p[1], Parameterization: p[2], OccurrenceCount: demand[k]})
	}
	return nodes, demands
}

func semanticTraceClassification(n *UniversalASTNode) (operation, primitive, family, parameter string) {
	var record universalOperationRecord
	_ = decodeUniversalField(n, "operation", &record)
	operation = strings.ToUpper(strings.TrimSpace(record.Semantics.Operation))
	if operation == "" {
		operation = strings.ToUpper(strings.TrimSpace(record.Operator))
	}
	switch n.StructuralKind {
	case "LiteralExpr":
		return "LITERAL", "LITERAL", "LITERAL", record.LiteralKind
	case "CallExpr":
		return "CALL", "CALL", "CALL", ""
	case "SymbolRef":
		return "LOAD", "LOAD", "LOAD", ""
	case "SwitchMatchStmt", "SelectExpr":
		return "SELECT", "SELECT", "SELECT", ""
	case "LoopStmt", "ForEachStmt":
		return "ITERATION", "ITERATION", "ITERATION", ""
	case "TryStmt", "RaisePanicStmt":
		return "EXCEPTION", "EXCEPTION", "EXCEPTION", ""
	case "AssignStmt":
		return "ASSIGNMENT", "ASSIGNMENT", "BINDING", ""
	case "ReturnStmt":
		return "RETURN", "RETURN", "CONTROL", ""
	}
	if n.StructuralKind == "OperationExpr" && operation != "" {
		// Operators are already structured values in universalOperationRecord;
		// normalize their canonical primitive identity without looking at source.
		operation = map[string]string{"+": "ADD", "-": "SUB", "*": "MUL", "**": "POW", "/": "DIV", "//": "FLOOR_DIV", "%": "REM", "==": "EQ", "!=": "NE", "<": "LT", "<=": "LE", ">": "GT", ">=": "GE", "&&": "AND", "||": "OR", "!": "NOT", "&": "BIT_AND", "|": "BIT_OR", "^": "BIT_XOR", "<<": "SHL", ">>": "SHR"}[operation]
		if operation == "" {
			operation = strings.ToUpper(strings.TrimSpace(record.Semantics.Operation))
			if operation == "" {
				operation = strings.ToUpper(strings.TrimSpace(record.Operator))
			}
		}
		// An explicit unsupported marker is useful provenance, but it is not a
		// canonical primitive and must never be promoted into the miner demand
		// matrix merely because a frontend carried it as an operation label.
		if strings.HasPrefix(operation, "UNSUPPORTED.") {
			return operation, "", "", ""
		}
		family = "OPERATION"
		switch operation {
		case "ADD", "SUB", "MUL", "DIV", "REM", "POW", "FLOOR_DIV", "BIT_AND", "BIT_OR", "BIT_XOR", "@":
			family = "BINARY"
		case "SHL", "SHR":
			family = "SHIFT"
		case "EQ", "NE", "LT", "LE", "GT", "GE":
			family = "COMPARE"
		case "AND", "OR":
			family = "LOGICAL_BINARY"
		case "NOT":
			family = "LOGICAL_UNARY"
		}
		return operation, operation, family, ""
	}
	return operation, "", "", ""
}

func semanticTraceSpan(s *SemanticSourceSpan) (string, string) {
	if s == nil {
		return "", ""
	}
	return fmt.Sprintf("%d:%d", s.StartLine, s.StartColumn), fmt.Sprintf("%d:%d", s.EndLine, s.EndColumn)
}
func semanticTraceBinding(u *UniversalASTDocument, id int) string {
	for _, r := range u.Relations {
		if r.From == id && r.Kind == "scope.binding" {
			return "scope.binding"
		}
	}
	return ""
}
func semanticTraceScope(u *UniversalASTDocument, id int) string {
	for _, r := range u.Relations {
		if r.From == id && (r.Kind == "scope.parent" || r.Kind == "scope.member") {
			return r.Kind
		}
	}
	return ""
}
