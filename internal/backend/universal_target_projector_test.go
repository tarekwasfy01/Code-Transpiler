package backend

import (
	"strings"
	"testing"
)

// universalAggregateGraph exercises the generic AggregateExpr projection
// contract without passing through a source-language AST. Its semantics are
// exactly the direct UAST executor's ordered aggregate value contract.
func universalAggregateGraph(t *testing.T) *uastExecutionGraph {
	t.Helper()
	nodes := map[int]*UniversalASTNode{
		0: {ID: 0, StructuralKind: "AggregateExpr"},
		1: {ID: 1, StructuralKind: "LiteralExpr"},
		2: {ID: 2, StructuralKind: "LiteralExpr"},
	}
	return &uastExecutionGraph{
		nodes: nodes,
		common: map[int]universalDecodedCommon{
			0: {Kind: "aggregate"},
			1: {Kind: "literal", Operation: universalOperationRecord{LiteralKind: "number", Text: "1"}},
			2: {Kind: "literal", Operation: universalOperationRecord{LiteralKind: "number", Text: "2"}},
		},
		children: map[int]map[string][]universalChild{0: {"member": {{ID: 1, Meta: universalChildRecord{Role: "member", Ordinal: 0}}, {ID: 2, Meta: universalChildRecord{Role: "member", Ordinal: 1}}}}},
	}
}

func TestUniversalTargetProjectorUsesRegisteredSpecs(t *testing.T) {
	specs := RegisteredTargetSpecs()
	if len(specs) != len(Backends()) {
		t.Fatalf("specs=%d targets=%d", len(specs), len(Backends()))
	}
	for _, spec := range specs {
		if spec.ID == "" || len(spec.Hooks) == 0 || len(spec.ProjectionForms) == 0 {
			t.Fatalf("incomplete spec %#v", spec)
		}
		for _, form := range []string{projectionFormCore, projectionFormAggregate, projectionFormVariable, projectionFormDeclGroup, projectionFormMetadata} {
			if _, ok := spec.ProjectionForms[form]; !ok {
				t.Fatalf("target %s has no projection form %q", spec.ID, form)
			}
		}
	}
}

func TestMutableVariableProjectionClassUsesSharedDeclarationContract(t *testing.T) {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range registry.Contracts {
		if contract.StructureKind == "VariableDecl" && (!contract.Implemented || contract.ProjectionForm != projectionFormVariable) {
			t.Fatalf("variable declaration contract is incomplete: %+v", contract)
		}
	}
	graph := &uastExecutionGraph{
		nodes: map[int]*UniversalASTNode{0: {ID: 0, StructuralKind: "VariableDecl"}, 1: {ID: 1, StructuralKind: "LiteralExpr"}},
		common: map[int]universalDecodedCommon{
			0: {Kind: "variable", Name: "x"},
			1: {Kind: "literal", Operation: universalOperationRecord{LiteralKind: "number", Text: "1"}},
		},
		children: map[int]map[string][]universalChild{0: {"initializer": {{ID: 1, Meta: universalChildRecord{Role: "initializer"}}}}},
	}
	for _, target := range Backends() {
		if target.ID == "r" {
			writer := &semanticWriter{}
			out, emitErr := writer.uastStatement(graph, 0)
			if emitErr != nil || out != "x <- 1\n" {
				t.Fatalf("target r declaration=%q err=%v", out, emitErr)
			}
			continue
		}
		generator := &targetGen{target: target.ID, declared: []map[string]bool{{}}, cValues: map[string]bool{}}
		if emitErr := generator.uastStatement(graph, 0); emitErr != nil || !strings.Contains(generator.b.String(), generator.name("x")) {
			t.Fatalf("target %s declaration=%q err=%v", target.ID, generator.b.String(), emitErr)
		}
	}
}

func TestVariableDeclarationGroupUsesItsChildrenAsTheProjectionContract(t *testing.T) {
	graph := &uastExecutionGraph{
		nodes: map[int]*UniversalASTNode{
			0: {ID: 0, StructuralKind: "VariableDeclGroup"},
			1: {ID: 1, StructuralKind: "VariableDecl"},
			2: {ID: 2, StructuralKind: "LiteralExpr"},
		},
		common: map[int]universalDecodedCommon{
			0: {Kind: "variable_group"}, 1: {Kind: "variable", Name: "x"},
			2: {Kind: "literal", Operation: universalOperationRecord{LiteralKind: "number", Text: "1"}},
		},
		children: map[int]map[string][]universalChild{
			0: {"declaration": {{ID: 1, Meta: universalChildRecord{Role: "declaration", Ordinal: 0}}}},
			1: {"initializer": {{ID: 2, Meta: universalChildRecord{Role: "initializer", Ordinal: 0}}}},
		},
	}
	for _, target := range Backends() {
		if target.ID == "r" {
			writer := &semanticWriter{}
			out, err := writer.uastStatement(graph, 0)
			if err != nil || out != "x <- 1\n" {
				t.Fatalf("target r group=%q err=%v", out, err)
			}
			continue
		}
		generator := &targetGen{target: target.ID, declared: []map[string]bool{{}}, cValues: map[string]bool{}}
		if err := generator.uastStatement(graph, 0); err != nil || !strings.Contains(generator.b.String(), generator.name("x")) {
			t.Fatalf("target %s group=%q err=%v", target.ID, generator.b.String(), err)
		}
	}
}

func TestUniversalTargetProjectorDeterministic(t *testing.T) {
	u := universalBackendFixture(t)
	p := UniversalTargetProjector{}
	first, err := p.Emit(u, "go")
	if err != nil {
		t.Fatal(err)
	}
	second, err := p.Emit(u, "go")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("same UAST and TargetSpec emitted different source")
	}
}

func TestUniversalPrecedenceEngine(t *testing.T) {
	ops := universalOperatorSpecs()
	if NeedsParentheses(ops["+"], ops["*"], "right") || !NeedsParentheses(ops["*"], ops["+"], "left") || !NeedsParentheses(ops["-"], ops["-"], "right") || !NeedsParentheses(ops["&&"], ops["||"], "right") {
		t.Fatal("precedence contract changed")
	}
}

func TestUniversalHelperResolver(t *testing.T) {
	registry := map[string]HelperSpec{"a": {ID: "a", Dependencies: []string{"b"}}, "b": {ID: "b"}}
	resolved, err := (UniversalHelperResolver{}).Resolve([]HelperRequirement{{ID: "a"}, {ID: "a"}}, registry)
	if err != nil || len(resolved) != 2 || resolved[0].ID != "b" {
		t.Fatalf("helper closure=%v err=%v", resolved, err)
	}
	registry["b"] = HelperSpec{ID: "b", Dependencies: []string{"a"}}
	if _, err := (UniversalHelperResolver{}).Resolve([]HelperRequirement{{ID: "a"}}, registry); err == nil {
		t.Fatal("cyclic helper dependency accepted")
	}
}

func TestRequirementRegistryDeterministicClosure(t *testing.T) {
	r := RequirementRegistry{Rules: map[string]Requirement{"a": {ID: "a", Kind: RequirementHelper, Dependencies: []string{"b"}}, "b": {ID: "b", Kind: RequirementImport}}}
	got, err := r.Resolve([]string{"a", "a"})
	if err != nil || len(got) != 2 || got[0].ID != "b" {
		t.Fatalf("requirements=%v err=%v", got, err)
	}
	r.Rules["b"] = Requirement{ID: "b", Dependencies: []string{"a"}}
	if _, err := r.Resolve([]string{"a"}); err == nil {
		t.Fatal("requirement cycle accepted")
	}
}

func TestPreservationFallbackOrderAndRuntimeIsolation(t *testing.T) {
	r := PreservationRegistry{Rules: []PreservationRule{{Capability: "x", Target: "go", Mode: PreservationRuntime}, {Capability: "x", Target: "go", Mode: PreservationHelper}, {Capability: "x", Target: "go", Mode: PreservationDirect}}}
	got, ok := r.Solve("go", "x")
	if !ok || got.Mode != PreservationDirect {
		t.Fatalf("got %#v", got)
	}
	modules := RuntimeModuleRegistry{Modules: map[string]RuntimeModule{"checked": {ID: "checked", Target: "go", ProvidedCapabilities: []string{"checked.add"}}}}
	if !modules.Provides("go", "checked.add") || modules.Provides("python", "checked.add") {
		t.Fatal("runtime target isolation lost")
	}
	if len(modules.RequirementRegistry().Rules) != 1 {
		t.Fatal("runtime requirement missing")
	}
}

func TestUniversalTargetProjectorClassifiesCoreDirectPath(t *testing.T) {
	u := universalBackendFixture(t)
	_, decision, err := (UniversalTargetProjector{}).Analyze(u, TargetSpec{ID: "go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(decision.Direct) == 0 {
		t.Fatal("Go core target path did not report its direct consumer")
	}
	if len(decision.Unsupported) != 0 {
		t.Fatalf("core fixture has unexpected unsupported target semantics: %v", decision.Unsupported)
	}
}

func TestAggregateProjectionClassUsesSharedTargetRuntimeContract(t *testing.T) {
	graph := universalAggregateGraph(t)
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		t.Fatal(err)
	}
	for _, contract := range registry.Contracts {
		if genericProjectionStructures[contract.StructureKind] && !contract.Implemented {
			t.Fatalf("%s contract is not registered as implemented", contract.StructureKind)
		}
	}
	for _, target := range Backends() {
		if target.ID == "r" {
			writer := &semanticWriter{}
			out, emitErr := writer.uastExpression(graph, 0)
			if emitErr != nil || out != "list(1, 2)" {
				t.Fatalf("target r aggregate=%q err=%v", out, emitErr)
			}
			continue
		}
		generator := &targetGen{target: target.ID, cValues: map[string]bool{}}
		out, emitErr := generator.uastExpression(graph, 0)
		if emitErr != nil || !strings.Contains(out, "list") {
			t.Fatalf("target %s aggregate=%q err=%v", target.ID, out, emitErr)
		}
	}
}

func TestMetadataProjectionContractsAreMatrixDerivedAndDoNotDropRuntimeSemantics(t *testing.T) {
	registry, err := UniversalStructureProjectionRegistry()
	if err != nil {
		t.Fatal(err)
	}
	byStructure := map[string]StructureProjectionContract{}
	for _, contract := range registry.Contracts {
		byStructure[contract.StructureKind] = contract
	}
	// Source-level operations require a productive runtime or target execution
	// primitive. The shared fallback renderer now provides one explicit,
	// syntax-complete runtime contract for these structures; it must never be
	// confused with metadata-only omission.
	for _, structure := range []string{"AtomicOp", "ForeignDeclCall", "SynchronizationOp"} {
		contract, ok := byStructure[structure]
		if !ok {
			t.Fatalf("missing contract for %s", structure)
		}
		if contract.EmissionPolicy == "METADATA_ONLY" || !contract.Implemented || contract.ProjectionForm != projectionFormFallback {
			t.Fatalf("runtime-bearing contract %q lacks explicit fallback projection: %+v", structure, contract)
		}
	}
	// Explicit semantic contracts remain no-syntax structures; their facts are
	// still validated and preserved in the UAST graph.
	for _, structure := range []string{"ABIContract", "Annotation", "BindingResolution", "Effect", "Visibility"} {
		contract := byStructure[structure]
		if !contract.Implemented || contract.EmissionPolicy != "METADATA_ONLY" {
			t.Fatalf("metadata contract %q lost its no-syntax proof: %+v", structure, contract)
		}
	}
	graph := &uastExecutionGraph{nodes: map[int]*UniversalASTNode{0: {ID: 0, StructuralKind: "ABIContract"}}, common: map[int]universalDecodedCommon{0: {Kind: "abi_contract"}}}
	generator := &targetGen{target: "go"}
	if err := generator.uastStatement(graph, 0); err != nil || generator.b.Len() != 0 {
		t.Fatalf("metadata node emitted target syntax: err=%v source=%q", err, generator.b.String())
	}
}
