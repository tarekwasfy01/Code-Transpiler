// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestSemanticContractTableInternsPhaseOneContracts(t *testing.T) {
	p, err := ParseSemantic("python", "f <- function(x) { return(x + 1) }\nprint(f(2))")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := p.MarshalUniversalASTJSON()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseUniversalASTJSON(wire)
	if err != nil {
		t.Fatal(err)
	}
	again, err := q.MarshalUniversalASTJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(wire, again) {
		t.Fatal("contract table round-trip is not canonical")
	}
	d := q.UniversalAST
	if d.ContractSchema != semanticContractSchema || len(d.ContractTable) == 0 || len(d.ContractRefs) == 0 {
		t.Fatalf("phase-one contract plane missing: schema=%q contracts=%d refs=%d", d.ContractSchema, len(d.ContractTable), len(d.ContractRefs))
	}
	seen := map[SemanticContractKind]bool{}
	for _, c := range d.ContractTable {
		if err := validateSemanticContractPayload(c); err != nil {
			t.Fatal(err)
		}
		seen[c.Kind] = true
	}
	for _, kind := range []SemanticContractKind{SemanticFunctionContractKind, SemanticCallContractKind, SemanticNumericContractKind, SemanticSymbolContractKind} {
		if !seen[kind] {
			t.Fatalf("missing interned contract kind %s", kind)
		}
	}
}

func TestCanonicalExportContractCacheSkipsReDerivationButFailsClosed(t *testing.T) {
	const source = "package main\nfunc main() { var x int32 = 1; _ = x }\n"
	p, err := LowerNativeGo("canonical-cache.go", source)
	if err != nil {
		t.Fatal(err)
	}
	d := p.UniversalAST
	if d.Metadata == nil {
		d.Metadata = map[string]string{}
	}
	d.Metadata["frontend_route"] = "CANONICALIZE_ONLY"
	d.Surface = NewUniversalASTSurface("go", source)
	oldContracts := len(d.ContractTable)
	maxID := -1
	for i := range d.Nodes {
		if d.Nodes[i].ID > maxID {
			maxID = d.Nodes[i].ID
		}
	}
	d.Nodes = append(d.Nodes, UniversalASTNode{ID: maxID + 1, StructuralKind: "LiteralExpr", Fields: map[string]json.RawMessage{
		"kind": []byte(`"literal"`), "type_ref": []byte(`{"kind":"integer","bits":32,"signed":true,"type_origin":"explicit"}`),
	}})
	if err := normalizeUniversalContracts(d); err != nil {
		t.Fatal(err)
	}
	if len(d.ContractTable) != oldContracts {
		t.Fatalf("canonical import re-derived contracts: before=%d after=%d", oldContracts, len(d.ContractTable))
	}
	if err := validateUniversalContractTable(d); err == nil {
		t.Fatal("contract cache fast path accepted the added numeric node without its required contract")
	}
}

func TestPhaseOneContractSchemasAndABIConsumer(t *testing.T) {
	signed := true
	i32 := SemanticType{Kind: "integer", Bits: 32, Signed: &signed, TypeOrigin: "explicit"}
	function := SemanticFunctionContract{Parameters: []SemanticContractParameter{{ID: 1, Name: "x", Type: i32, Passing: "value"}}, Results: []SemanticType{i32}, VariadicMode: "nonvariadic", CallingSemantics: "direct", Throws: "nothrow"}
	call := SemanticCallContract{CallKind: "foreign", Arguments: []SemanticCallArgumentContract{{NodeID: 2}}, EvaluationOrder: "left_to_right"}
	numeric := SemanticNumericContract{Type: i32, Overflow: "wrap", Rounding: "toward_zero", Division: "truncating", NaN: "forbidden", SignedZero: "ignore"}
	conversion := SemanticConversionContract{SourceType: i32, TargetType: i32, Kind: "identity", Lossiness: "lossless", Overflow: "wrap", Rounding: "toward_zero", RuntimeCheck: "none"}
	abi := SemanticABIContract{Symbol: "probe", CallingConvention: "cdecl", Parameters: []SemanticType{i32}, Result: &i32}
	symbol := SemanticSymbolContract{Identity: "probe", Linkage: "external", Visibility: "public", ImportExport: "import"}
	values := []struct {
		kind  SemanticContractKind
		value any
	}{
		{SemanticFunctionContractKind, function}, {SemanticCallContractKind, call}, {SemanticNumericContractKind, numeric},
		{SemanticConversionContractKind, conversion}, {SemanticABIContractKind, abi}, {SemanticSymbolContractKind, symbol},
	}
	for _, item := range values {
		raw, err := contractPayload(item.kind, item.value)
		if err != nil {
			t.Fatal(err)
		}
		contract := SemanticContract{ID: 0, Kind: item.kind, Payload: raw, Hash: semanticContractHash(item.kind, raw)}
		if err := validateSemanticContractPayload(contract); err != nil {
			t.Fatalf("%s: %v", item.kind, err)
		}
	}
	raw, err := contractPayload(SemanticABIContractKind, abi)
	if err != nil {
		t.Fatal(err)
	}
	d := &UniversalASTDocument{
		ContractSchema: semanticContractSchema,
		Nodes:          []UniversalASTNode{{ID: 7, StructuralKind: "ABIContract"}},
		ContractTable:  []SemanticContract{{ID: 0, Kind: SemanticABIContractKind, Payload: raw, Hash: semanticContractHash(SemanticABIContractKind, raw)}},
		ContractRefs:   []SemanticContractReference{{NodeID: 7, ContractID: 0, Role: "abi"}},
	}
	got, ok := decodeLLVMExternalABIContractFromContractTable(d, 7)
	if !ok || got.Symbol != "probe" || got.CallingConvention != "cdecl" || len(got.Parameters) != 1 || got.Result.Bits != 32 {
		t.Fatalf("ABI table consumer failed: %+v ok=%v", got, ok)
	}
}

func TestExtendedSemanticContractFamiliesAreTypedAndDerived(t *testing.T) {
	signed := true
	i32 := SemanticType{Kind: "integer", Bits: 32, Signed: &signed, TypeOrigin: "explicit"}
	values := []struct {
		kind  SemanticContractKind
		value any
	}{
		{SemanticMemoryContractKind, SemanticMemoryContract{Access: "read", Aliasing: "unknown", Addressability: "unknown", Provenance: "unknown", Safety: "unknown"}},
		{SemanticOwnershipContractKind, SemanticOwnershipContract{Mode: "owned", Passing: "value", Moved: "unknown", Borrowed: "unknown", Copyable: "unknown", Drop: "unknown"}},
		{SemanticReferenceContractKind, SemanticReferenceContract{Kind: "reference", TargetType: i32, Mutable: "unknown", Rebindable: "unknown"}},
		{SemanticPointerContractKind, SemanticPointerContract{Nullable: "nonnull", Provenance: "unknown", AddressSpace: "unknown", Bounds: "unknown", Arithmetic: "unknown", Dereferenceability: "unknown"}},
		{SemanticLifetimeContractKind, SemanticLifetimeContract{Kind: "scope", State: "unknown", Region: "unknown", Cleanup: "unknown"}},
		{SemanticStorageContractKind, SemanticStorageContract{Kind: "frame", Addressable: "unknown", Escapes: "unknown", Allocation: "unknown"}},
		{SemanticMutabilityContractKind, SemanticMutabilityContract{Kind: "immutable", Rebinding: "unknown"}},
		{SemanticAggregateContractKind, SemanticAggregateContract{Kind: "array", FixedLength: "true", DynamicLength: "false", Contiguous: "unknown", Packed: "unknown", Tagged: "unknown"}},
		{SemanticLayoutContractKind, SemanticLayoutContract{SourceDefined: "unknown", Packed: "unknown", Alignment: "unknown", Size: "unknown"}},
		{SemanticShapeContractKind, SemanticShapeContract{LengthKind: "dynamic", Order: "unknown"}},
		{SemanticBoundsContractKind, SemanticBoundsContract{Kind: "index", Policy: "checked", IndexDomain: "integer", Checking: "runtime", NegativeIndex: "unknown", SliceStartInclusive: "unknown", SliceEndInclusive: "unknown"}},
		{SemanticNullableContractKind, SemanticNullableContract{Mode: "nonnull", Null: "unknown", Unwrap: "unknown"}},
		{SemanticValueCategoryContractKind, SemanticValueCategoryContract{Kind: "rvalue", Addressable: "unknown", Temporary: "false", Materialization: "unknown"}},
		{SemanticTextContractKind, SemanticTextContract{Encoding: "utf8", StorageUnit: "unknown", IndexUnit: "unknown", LengthUnit: "unknown", Normalization: "unknown"}},
		{SemanticControlContractKind, SemanticControlContract{EvaluationOrder: "left_to_right", ShortCircuit: "false", Termination: "unknown", Branching: "unknown"}},
		{SemanticEffectContractKind, SemanticEffectContract{Effects: []string{"read"}, Purity: "unknown"}},
		{SemanticExceptionContractKind, SemanticExceptionContract{Model: "nothrow", MayThrow: "false", Nothrow: "true", CleanupRequired: "unknown", UnwindRequired: "unknown"}},
		{SemanticClosureContractKind, SemanticClosureContract{EnvironmentLifetime: "scope", Escaping: "unknown", Receiver: "unknown"}},
		{SemanticDispatchContractKind, SemanticDispatchContract{Kind: "static", ResolutionStage: "frontend"}},
		{SemanticGenericContractKind, SemanticGenericContract{Specialization: "unknown"}},
		{SemanticModuleContractKind, SemanticModuleContract{Kind: "module", Identity: "main"}},
		{SemanticCompileTimeContractKind, SemanticCompileTimeContract{Required: "false", Allowed: "true", RuntimeForbidden: "false", Result: "unknown"}},
		{SemanticConcurrencyContractKind, SemanticConcurrencyContract{Operation: "unknown", Synchronization: "unknown", Communication: "unknown", Spawn: "unknown"}},
		{SemanticAsyncContractKind, SemanticAsyncContract{Operation: "unknown", SuspendPoint: "unknown", ResumeTarget: "unknown", ExceptionContinuation: "unknown"}},
		{SemanticMemoryOrderContractKind, SemanticMemoryOrderContract{Order: "seq_cst"}},
		{SemanticValidationContractKind, SemanticValidationContract{Kind: "check", Policy: "reject", Failure: "error"}},
	}
	for _, item := range values {
		raw, err := contractPayload(item.kind, item.value)
		if err != nil {
			t.Fatalf("%s marshal: %v", item.kind, err)
		}
		contract := SemanticContract{ID: 0, Kind: item.kind, Payload: raw, Hash: semanticContractHash(item.kind, raw)}
		if err := validateSemanticContractPayload(contract); err != nil {
			t.Fatalf("%s schema: %v", item.kind, err)
		}
	}
	p, err := ParseSemantic("python", "f <- function(x) { return(x + 1) }\nprint(f(2))")
	if err != nil {
		t.Fatal(err)
	}
	if p.UniversalAST == nil {
		t.Fatal("semantic parser did not produce UAST")
	}
	seen := map[SemanticContractKind]bool{}
	for _, contract := range p.UniversalAST.ContractTable {
		seen[contract.Kind] = true
	}
	for _, kind := range []SemanticContractKind{SemanticMemoryContractKind, SemanticControlContractKind, SemanticEffectContractKind, SemanticDispatchContractKind, SemanticValueCategoryContractKind} {
		if !seen[kind] {
			t.Fatalf("derived contract family %s missing", kind)
		}
	}
}
