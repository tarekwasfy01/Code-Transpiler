// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCompileLLVMProjectsCanonicalUAST(t *testing.T) {
	p, err := LowerNativeGo("llvm_witness.go", `package main
func add(a int, b int) int { return a + b }
func main() { _ = add(2, 3) }
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompileLLVM(p, LLVMCompileOptions{OutputKind: LLVMIR, EmitIR: true})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"target triple =", "define i32 @main", "define i64 @native_function_0", "define void @native_function_1", "call i64 @native_function_0", "call void @native_function_1", "add i64"} {
		if !strings.Contains(result.Text, want) {
			t.Fatalf("LLVM IR missing %q:\n%s", want, result.Text)
		}
	}
}

func TestCompileLLVMProjectsLoopAndAggregateFamilies(t *testing.T) {
	p, err := LowerNativeGo("llvm_loop_aggregate.go", `package main
func loopStatus() int64 { total := int64(0); for i := int64(1); i <= 3; i++ { total += i }; return total }
func aggregateStatus() int64 { values := []int64{4, 5}; return values[2] }
func main() {}
`)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"loopStatus", "aggregateStatus"} {
		result, compileErr := CompileLLVM(p, LLVMCompileOptions{OutputKind: LLVMIR, EmitIR: true, EntryPoint: entry})
		if compileErr != nil {
			t.Fatalf("entry %s: %v", entry, compileErr)
		}
		if !strings.Contains(result.Text, "uast_while_cond") && entry == "loopStatus" {
			t.Fatalf("entry %s did not produce loop control flow:\n%s", entry, result.Text)
		}
		if entry == "aggregateStatus" && (!strings.Contains(result.Text, "alloca [2 x i64]") || !strings.Contains(result.Text, "getelementptr inbounds i64")) {
			t.Fatalf("entry %s did not produce aggregate layout/indexing:\n%s", entry, result.Text)
		}
	}
}

func TestCompileLLVMProjectsNonCapturingFunctionValue(t *testing.T) {
	p, err := LowerNativeGo("llvm_function_value.go", `package main
func inc(value int64) int64 { return value + 1 }
func status() int64 { f := inc; return f(41) }
func main() {}
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompileLLVM(p, LLVMCompileOptions{OutputKind: LLVMIR, EmitIR: true, EntryPoint: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "store ptr @") || !strings.Contains(result.Text, "call i64 %") {
		t.Fatalf("function value was not projected as an indirect call:\n%s", result.Text)
	}
}

func TestCompileLLVMProjectsMatrixPlanAndMemberLayout(t *testing.T) {
	p, err := LowerNativeGo("llvm_member.go", `package main
type Pair struct { Left int64; Right int64 }
func status() int64 { pair := Pair{Left: 41, Right: 1}; return pair.Left }
func main() {}
`)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildLLVMProjectionPlan(p)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Schema != "code-transpiler.uast-llvm-projection.v1" || plan.BasisSHA256 == "" {
		t.Fatalf("matrix plan lacks schema/basis: %+v", plan)
	}
	if len(plan.cellByNode) != len(plan.Cells) {
		t.Fatalf("projection plan node index has %d entries for %d cells", len(plan.cellByNode), len(plan.Cells))
	}
	for _, cell := range plan.Cells {
		indexed, found := plan.cell(cell.NodeID)
		if !found || indexed.NodeID != cell.NodeID || indexed.Mode != cell.Mode || indexed.Family != cell.Family {
			t.Fatalf("projection cell lookup mismatch for node %d: got=%+v found=%t want=%+v", cell.NodeID, indexed, found, cell)
		}
	}
	if plan.Families["member-layout"] == 0 && plan.Families["aggregate-layout"] == 0 {
		t.Fatalf("matrix plan did not classify the aggregate/member family: %+v", plan.Families)
	}
	result, err := CompileLLVM(p, LLVMCompileOptions{OutputKind: LLVMIR, EmitIR: true, EntryPoint: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectionSchema != plan.Schema || result.ProjectionBasis != plan.BasisSHA256 {
		t.Fatalf("compile result was not driven by the same projection matrix: result=%+v plan=%+v", result, plan)
	}
	if result.ProjectionFamilies["aggregate-layout"] == 0 {
		t.Fatalf("aggregate family was not applied by the LLVM projection matrix: %+v", result.ProjectionFamilies)
	}
}

func TestLLVMLayoutUsesAttachedAggregateAndShapeContracts(t *testing.T) {
	element := SemanticType{Kind: "integer", Bits: 32, TypeOrigin: "explicit"}
	length := 0
	aggregate := SemanticAggregateContract{Kind: "array", ElementType: &element, FixedLength: "true", DynamicLength: "false"}
	shape := SemanticShapeContract{LengthKind: "fixed", Length: &length}
	aggregatePayload, err := contractPayload(SemanticAggregateContractKind, aggregate)
	if err != nil {
		t.Fatal(err)
	}
	shapePayload, err := contractPayload(SemanticShapeContractKind, shape)
	if err != nil {
		t.Fatal(err)
	}
	doc := &UniversalASTDocument{
		ContractTable: []SemanticContract{
			{ID: 0, Kind: SemanticAggregateContractKind, Hash: semanticContractHash(SemanticAggregateContractKind, aggregatePayload), Payload: aggregatePayload},
			{ID: 1, Kind: SemanticShapeContractKind, Hash: semanticContractHash(SemanticShapeContractKind, shapePayload), Payload: shapePayload},
		},
		ContractRefs: []SemanticContractReference{
			{NodeID: 7, ContractID: 0, Role: "aggregate"},
			{NodeID: 7, ContractID: 1, Role: "shape"},
		},
	}
	g := &uastExecutionGraph{document: doc, common: map[int]universalDecodedCommon{7: {Type: SemanticType{Kind: "unknown", TypeOrigin: "unknown"}}}, nodes: map[int]*UniversalASTNode{7: {ID: 7, StructuralKind: "ArraySliceType"}}}
	e := newLLVMEmitter(g, LLVMCompileOptions{})
	value, err := e.applyNodeAggregateContract(7, llvmValue{typ: "ptr", ref: "%array"})
	if err != nil {
		t.Fatal(err)
	}
	if value.pointee != "i32" || !value.knownLen || value.length != 0 {
		t.Fatalf("attached array contract was not projected exactly: %+v", value)
	}
}

func TestCompileLLVMProjectsDynamicSliceBounds(t *testing.T) {
	p, err := LowerNativeGo("llvm_slice.go", `package main
func status() int64 { values := []int64{4, 5, 6}; start := int64(2); end := int64(3); view := values[start:end]; return view[1] }
func main() {}
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompileLLVM(p, LLVMCompileOptions{OutputKind: LLVMIR, EmitIR: true, EntryPoint: "status"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "uast_slice_good") || !strings.Contains(result.Text, "uast_slice_bad") || !strings.Contains(result.Text, "uast_index_good") {
		t.Fatalf("dynamic slice/index bounds were not projected:\n%s", result.Text)
	}
}

func TestCompileLLVMProjectsExactUnsignedIntegerPrimitiveBatch(t *testing.T) {
	unsigned := false
	integer := SemanticType{Kind: "integer", Bits: 64, Signed: &unsigned, TypeOrigin: "explicit"}
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ReturnStmt{X: &OperationExpr{Operation: SemanticOperation{Name: "integer.remainder", Type: integer}, Operands: []Expr{
			&OperationExpr{Operation: SemanticOperation{Name: "integer.literal", Type: integer, Text: "17"}},
			&OperationExpr{Operation: SemanticOperation{Name: "integer.literal", Type: integer, Text: "5"}},
		}}},
	}}, "eager_left_to_right")
	result, err := CompileLLVM(p, LLVMCompileOptions{OutputKind: LLVMIR, EmitIR: true})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(result.Text, "urem i64") {
		t.Fatalf("unsigned primitive batch missing urem:\n%s", result.Text)
	}
}

func TestLLVMCallResolutionUsesSelectedDeclarationIdentity(t *testing.T) {
	selected := 0
	g := &uastExecutionGraph{common: map[int]universalDecodedCommon{
		1: {Kind: "function", Name: "pkg.run"},
		2: {Kind: "function", Name: "other.run"},
		3: {Kind: "call", Operation: universalOperationRecord{CallResolution: &SemanticCallResolution{
			Candidates: []SemanticCallCandidate{{Name: "run", Declaration: "1"}}, Selected: &selected,
		}}},
	}}
	got, ok := llvmCallResolutionTarget(g, 3)
	if !ok || got != 1 {
		t.Fatalf("selected declaration identity did not resolve uniquely: got=%d ok=%v", got, ok)
	}
}

func TestLLVMCallGapsReduceToABIQuotientFamilies(t *testing.T) {
	qualified := &uastExecutionGraph{
		nodes: map[int]*UniversalASTNode{
			10: {ID: 10, StructuralKind: "CallExpr"},
			11: {ID: 11, StructuralKind: "SymbolRef"},
		},
		common: map[int]universalDecodedCommon{
			10: {Kind: "call"},
			11: {Kind: "identifier", Name: "pkg.missing"},
		},
		children: map[int]map[string][]universalChild{10: {"callee": {{ID: 11}}}},
	}
	got := llvmCallGapQuotient(qualified, 10)
	if got.Family != "call-abi" || got.Primitive != "call.target" || got.Contract != "module-symbol-contract" {
		t.Fatalf("qualified call was not reduced to module ABI family: %+v", got)
	}
	ambiguous := &uastExecutionGraph{
		nodes: map[int]*UniversalASTNode{
			20: {ID: 20, StructuralKind: "CallExpr"},
			21: {ID: 21, StructuralKind: "SymbolRef"},
			22: {ID: 22, StructuralKind: "ClosureExpr"},
			23: {ID: 23, StructuralKind: "ClosureExpr"},
		},
		common: map[int]universalDecodedCommon{
			20: {Kind: "call"},
			21: {Kind: "identifier", Name: "b"},
			22: {Kind: "function", Name: "b"},
			23: {Kind: "function", Name: "b"},
		},
		children: map[int]map[string][]universalChild{20: {"callee": {{ID: 21}}}},
	}
	got = llvmCallGapQuotient(ambiguous, 20)
	if got.Family != "call-abi" || got.Primitive != "call.dispatch" || got.Contract != "unique-callee-contract" {
		t.Fatalf("ambiguous call was not reduced to dispatch ABI family: %+v", got)
	}
}

func TestLLVMCallingConventionFamilyMatrix(t *testing.T) {
	cases := map[string]string{
		"": "", "ccc": "", "cdecl": "", "c-default": "",
		"win64": "win64cc", "stdcall": "x86_stdcallcc",
		"fastcall": "x86_fastcallcc", "thiscall": "x86_thiscallcc",
		"vectorcall": "x86_vectorcallcc",
	}
	for input, want := range cases {
		got, err := llvmCallingConvention(input)
		if err != nil || got != want {
			t.Fatalf("calling convention %q mapped to %q err=%v want=%q", input, got, err, want)
		}
	}
	if _, err := llvmCallingConvention("pascal"); err == nil {
		t.Fatal("unsupported calling convention was accepted without a target contract")
	}
}

func TestLLVMExternalABIContractProjectsLinkedCall(t *testing.T) {
	signed := true
	integer := SemanticType{Kind: "integer", Bits: 64, Signed: &signed, TypeOrigin: "explicit"}
	abi, err := json.Marshal(map[string]any{
		"symbol": "semantic_external_probe", "calling_convention": "ccc",
		"parameters": []SemanticType{integer}, "result": integer,
	})
	if err != nil {
		t.Fatal(err)
	}
	g := &uastExecutionGraph{
		document: &UniversalASTDocument{Relations: []UniversalASTRelation{{Kind: "abi.calls", From: 10, To: UniversalASTReference{Domain: "node", ID: "20"}}}},
		nodes: map[int]*UniversalASTNode{
			10: {ID: 10, StructuralKind: "CallExpr"},
			11: {ID: 11, StructuralKind: "LiteralExpr"},
			12: {ID: 12, StructuralKind: "SymbolRef"},
			20: {ID: 20, StructuralKind: "ABIContract", Fields: map[string]json.RawMessage{"abi_contract": abi, "linkage": json.RawMessage(`{"symbol":"semantic_external_probe"}`), "calling_convention": json.RawMessage(`"ccc"`)}},
		},
		common: map[int]universalDecodedCommon{
			10: {Kind: "call", Type: integer},
			11: {Kind: "literal", Type: integer, Operation: universalOperationRecord{LiteralKind: "integer", Text: "7"}},
			12: {Kind: "identifier", Name: "semantic_external_probe"},
			20: {Kind: "abi_contract"},
		},
		children: map[int]map[string][]universalChild{
			10: {"callee": {{ID: 12}}, "argument": {{ID: 11}}},
		},
	}
	contract, ok := llvmExternalCallContract(g, 10)
	if !ok || contract.Symbol != "semantic_external_probe" {
		t.Fatalf("strict external ABI contract was not recognized: %+v ok=%v", contract, ok)
	}
	e := newLLVMEmitter(g, LLVMCompileOptions{OutputKind: LLVMIR})
	e.projection = LLVMProjectionPlan{Cells: []LLVMProjectionCell{{NodeID: 11, Mode: llvmProjectionDirect}}}
	e.current = &llvmFunction{vars: map[string]llvmValue{}}
	if _, err := e.emitCall(10); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(e.b.String(), "call i64 @semantic_external_probe(i64 7)") {
		t.Fatalf("external ABI call was not emitted from the linked UAST contract: %s", e.b.String())
	}
}

func TestLLVMExternalABIAdapterProjectsConflictingCompleteCalls(t *testing.T) {
	signed := true
	i64Type := SemanticType{Kind: "integer", Bits: 64, Signed: &signed, TypeOrigin: "explicit"}
	i32Type := SemanticType{Kind: "integer", Bits: 32, Signed: &signed, TypeOrigin: "explicit"}
	contract := func(parameter, result SemanticType) json.RawMessage {
		payload, err := json.Marshal(map[string]any{
			"symbol": "semantic_external_adapter", "calling_convention": "ccc",
			"parameters": []SemanticType{parameter}, "result": result,
		})
		if err != nil {
			t.Fatal(err)
		}
		return payload
	}
	g := &uastExecutionGraph{
		document: &UniversalASTDocument{Relations: []UniversalASTRelation{
			{Kind: "abi.calls", From: 10, To: UniversalASTReference{Domain: "node", ID: "20"}},
			{Kind: "abi.calls", From: 30, To: UniversalASTReference{Domain: "node", ID: "40"}},
		}},
		nodes: map[int]*UniversalASTNode{
			10: {ID: 10, StructuralKind: "CallExpr"}, 11: {ID: 11, StructuralKind: "SymbolRef"}, 12: {ID: 12, StructuralKind: "LiteralExpr"},
			20: {ID: 20, StructuralKind: "ABIContract", Fields: map[string]json.RawMessage{"abi_contract": contract(i64Type, i64Type)}},
			30: {ID: 30, StructuralKind: "CallExpr"}, 31: {ID: 31, StructuralKind: "SymbolRef"}, 32: {ID: 32, StructuralKind: "LiteralExpr"},
			40: {ID: 40, StructuralKind: "ABIContract", Fields: map[string]json.RawMessage{"abi_contract": contract(i32Type, i32Type)}},
		},
		common: map[int]universalDecodedCommon{
			10: {Kind: "call", Type: i64Type}, 11: {Kind: "identifier", Name: "semantic_external_adapter"}, 12: {Kind: "literal", Type: i64Type, Operation: universalOperationRecord{LiteralKind: "integer", Text: "7"}},
			20: {Kind: "abi_contract"}, 30: {Kind: "call", Type: i32Type}, 31: {Kind: "identifier", Name: "semantic_external_adapter"}, 32: {Kind: "literal", Type: i32Type, Operation: universalOperationRecord{LiteralKind: "integer", Text: "3"}},
			40: {Kind: "abi_contract"},
		},
		children: map[int]map[string][]universalChild{
			10: {"callee": {{ID: 11}}, "argument": {{ID: 12}}},
			30: {"callee": {{ID: 31}}, "argument": {{ID: 32}}},
		},
	}
	e := newLLVMEmitter(g, LLVMCompileOptions{OutputKind: LLVMIR})
	e.projection = LLVMProjectionPlan{Cells: []LLVMProjectionCell{{NodeID: 12, Mode: llvmProjectionDirect}, {NodeID: 32, Mode: llvmProjectionDirect}}}
	e.current = &llvmFunction{vars: map[string]llvmValue{}}
	if err := e.discoverExternalCalls(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.emitCall(10); err != nil {
		t.Fatal(err)
	}
	if _, err := e.emitCall(30); err != nil {
		t.Fatal(err)
	}
	text := e.b.String()
	if !strings.Contains(text, "call i64 @semantic_external_adapter(i64 7)") {
		t.Fatalf("first complete ABI call missing: %s", text)
	}
	if !strings.Contains(text, "call i32 bitcast (i64 (i64)* @semantic_external_adapter to i32 (i32)*)(i32 3)") {
		t.Fatalf("conflicting complete ABI call was not adapted at call site: %s", text)
	}
}

func TestLLVMSwitchMatchProjectsSelectorOnce(t *testing.T) {
	signed := true
	integer := SemanticType{Kind: "integer", Bits: 64, Signed: &signed, TypeOrigin: "explicit"}
	g := &uastExecutionGraph{
		document: &UniversalASTDocument{},
		nodes: map[int]*UniversalASTNode{
			1: {ID: 1, StructuralKind: "SwitchMatchStmt"},
			2: {ID: 2, StructuralKind: "LiteralExpr"},
			3: {ID: 3, StructuralKind: "Scope"},
			4: {ID: 4, StructuralKind: "LiteralExpr"},
			5: {ID: 5, StructuralKind: "Scope"},
		},
		common: map[int]universalDecodedCommon{
			1: {Kind: "switch"},
			2: {Kind: "literal", Type: integer, Operation: universalOperationRecord{LiteralKind: "integer", Text: "7"}},
			3: {Kind: "block"},
			4: {Kind: "literal", Type: integer, Operation: universalOperationRecord{LiteralKind: "integer", Text: "7"}},
			5: {Kind: "block"},
		},
		children: map[int]map[string][]universalChild{
			1: {"condition": {{ID: 2, Meta: universalChildRecord{Role: "condition", Ordinal: 0}}}, "case": {{ID: 3, Meta: universalChildRecord{Role: "case", Ordinal: 1}}}},
			3: {"pattern": {{ID: 4, Meta: universalChildRecord{Role: "pattern", Ordinal: 0}}}, "body": {{ID: 5, Meta: universalChildRecord{Role: "body", Ordinal: 1}}}},
		},
	}
	e := newLLVMEmitter(g, LLVMCompileOptions{OutputKind: LLVMIR})
	e.projection = LLVMProjectionPlan{Cells: []LLVMProjectionCell{
		{NodeID: 2, Mode: llvmProjectionDirect}, {NodeID: 4, Mode: llvmProjectionDirect}, {NodeID: 5, Mode: llvmProjectionDirect},
	}}
	e.current = &llvmFunction{vars: map[string]llvmValue{}}
	if err := e.emitSwitchMatch(1); err != nil {
		t.Fatal(err)
	}
	text := e.b.String()
	if strings.Count(text, "icmp eq i64") != 1 || !strings.Contains(text, "uast_switch_body") {
		t.Fatalf("switch selector/pattern contract was not projected once: %s", text)
	}
}
