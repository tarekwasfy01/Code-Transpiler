// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"bytes"
	"debug/pe"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSemanticNativePipelineProducesPEFromProgram(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ReturnStmt{X: &LiteralExpr{Kind: "integer", Text: "7"}},
	}}, "eager_left_to_right")
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bytes) == 0 || result.InstructionCount == 0 {
		t.Fatalf("empty native result: %+v", result)
	}
	f, err := pe.NewFile(bytes.NewReader(result.Bytes))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		t.Fatalf("machine=%v", f.Machine)
	}
}

func TestSemanticNativePipelineCompilesBuiltinSqrtToPE(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ExprStmt{X: &CallExpr{Fun: &IdentExpr{Name: "sqrt"}, Args: []Arg{{Value: &LiteralExpr{Kind: "number", Text: "9"}}}}},
		&ReturnStmt{X: &LiteralExpr{Kind: "integer", Text: "0"}},
	}}, "eager_left_to_right")
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil || len(result.Bytes) == 0 {
		t.Fatalf("builtin sqrt did not reach native PE emission: result=%+v err=%v", result, err)
	}
}

func TestNativeX64EncoderPreservesExtendedSIBIndex(t *testing.T) {
	code, _, err := encodeX64(x64Program{Instructions: []x64Instruction{{
		Op: "mov", A: xr(xRAX), B: xmIndexed(xRAX, xR10, 8, 8),
	}}})
	if err != nil {
		t.Fatal(err)
	}
	// REX.W|REX.X (0x4a) is required for [rax+r10*8+8]. Without REX.X the
	// same bytes address RDX and silently corrupt the aggregate contract.
	if len(code) < 1 || code[0] != 0x4a {
		t.Fatalf("extended SIB index lost: %x", code)
	}
}

func TestSemanticNativePipelineExecutesLargeStackFrame(t *testing.T) {
	statements := make([]Stmt, 0, 600)
	for i := 0; i < 600; i++ {
		statements = append(statements, &AssignStmt{Name: "large_local_" + strconv.Itoa(i), Value: &LiteralExpr{Kind: "integer", Text: "1"}})
	}
	statements = append(statements, &ReturnStmt{X: &LiteralExpr{Kind: "integer", Text: "0"}})
	result, err := EmitNativeExecutable(NewSemanticProgram(&BlockStmt{List: statements}, "eager_left_to_right"), "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := t.TempDir() + "\\large-stack-frame.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err != nil {
		t.Fatalf("large stack frame process failed: %v", err)
	}
}

func TestSemanticNativePipelineExecutesStringIndex(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ReturnStmt{X: &IndexExpr{X: &LiteralExpr{Kind: "string", Text: `"ab"`}, Args: []Arg{{Value: &LiteralExpr{Kind: "integer", Text: "2"}}}}},
	}}, "eager_left_to_right")
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := t.TempDir() + "\\string-index.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err == nil {
		t.Fatal("string index unexpectedly returned exit 0")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != int('b') {
		t.Fatalf("string index process exit=%v, want %d", err, 'b')
	}
}

func TestSemanticNativePipelineEmbedsConstantStringConcat(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ExprStmt{X: &BinaryExpr{Op: "+", L: &LiteralExpr{Kind: "string", Text: `"left"`}, R: &LiteralExpr{Kind: "string", Text: `"right"`}}},
		&ReturnStmt{X: &LiteralExpr{Kind: "integer", Text: "0"}},
	}}, "eager_left_to_right")
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.Bytes, []byte("leftright\x00")) {
		t.Fatal("constant string concatenation was not emitted into the native image")
	}
	exe := t.TempDir() + "\\constant-string-concat.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err != nil {
		t.Fatalf("constant string concatenation process failed: %v", err)
	}
}

func TestSemanticNativePipelineExecutesRecursiveCallGraph(t *testing.T) {
	p, err := LowerNativeGo("recursive.go", `package main
func fact(n int64) int64 { if n <= 1 { return 1 }; return n * fact(n-1) }
	func check() int64 { return fact(5) - 120 }
func main() {}
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "check")
	if err != nil {
		t.Fatal(err)
	}
	exe := t.TempDir() + "\\recursive-call-graph.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err != nil {
		t.Fatalf("recursive call graph process failed: %v", err)
	}
}

func TestSemanticNativePipelineExecutesDirectUASTAggregateIndex(t *testing.T) {
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	kind := func(id int, structural, semantic string, operation any) UniversalASTNode {
		fields := map[string]json.RawMessage{}
		for name, value := range map[string]any{"id": id, "kind": semantic, "scope_id": 0} {
			encoded, _ := json.Marshal(value)
			fields[name] = encoded
		}
		if operation != nil {
			encoded, _ := json.Marshal(operation)
			fields["operation"] = encoded
		}
		n := UniversalASTNode{ID: id, StructuralKind: structural, SemanticFacets: defaultUniversalFacets(structural), Fields: fields}
		n.FieldMask, _ = universalFieldMask(&n)
		return n
	}
	literal := func(id int, text string) UniversalASTNode {
		return kind(id, "LiteralExpr", "literal", map[string]any{"literal_kind": "integer", "text": text})
	}
	relation := func(from, to int, role string, ordinal int) FrontendRelationFact {
		roleJSON, _ := json.Marshal(role)
		ordinalJSON, _ := json.Marshal(ordinal)
		return FrontendRelationFact{Kind: "syntax.child", From: from, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(to)}, Attributes: map[string]json.RawMessage{"role": roleJSON, "ordinal": ordinalJSON}}
	}
	facts := FrontendSemanticFacts{
		SchemaVersion: 1, BasisSHA256: uastEmbedded.BasisSHA256, LanguageProfile: "universal", LanguageFacet: make([]float64, 334),
		Projection: "frontend_facts.v1", Evaluation: "eager_left_to_right", ValueModel: "tagged_dynamic_binary64", IndexBase: 1,
		Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "semantic", EntryPoint: "main"},
		Nodes: []UniversalASTNode{
			kind(0, "Scope", "block", nil), kind(1, "ReturnStmt", "return", nil), kind(2, "IndexExpr", "index", nil),
			kind(3, "AggregateExpr", "aggregate", nil), literal(4, "0"), literal(5, "1"), literal(6, "1"),
		},
		Relations: []FrontendRelationFact{
			relation(0, 1, "statement", 0), relation(1, 2, "expression", 0), relation(2, 3, "value", 0),
			relation(2, 6, "argument", 0), relation(3, 4, "member", 0), relation(3, 5, "member", 1),
		},
	}
	u, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EmitNativeExecutable(semanticProgramFromUAST(u), "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := t.TempDir() + "\\direct-uast-aggregate-index.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err != nil {
		t.Fatalf("direct UAST aggregate/index process failed: %v", err)
	}
}

func TestSemanticNativePipelineExecutesDirectUASTAggregateSumBuiltin(t *testing.T) {
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	kind := func(id int, structural, semantic, name string, operation any) UniversalASTNode {
		fields := map[string]json.RawMessage{}
		for key, value := range map[string]any{"id": id, "kind": semantic, "scope_id": 0} {
			encoded, _ := json.Marshal(value)
			fields[key] = encoded
		}
		if name != "" {
			encoded, _ := json.Marshal(name)
			fields["name"] = encoded
		}
		if operation != nil {
			encoded, _ := json.Marshal(operation)
			fields["operation"] = encoded
		}
		n := UniversalASTNode{ID: id, StructuralKind: structural, SemanticFacets: defaultUniversalFacets(structural), Fields: fields}
		n.FieldMask, _ = universalFieldMask(&n)
		return n
	}
	literal := func(id int, text string) UniversalASTNode {
		return kind(id, "LiteralExpr", "literal", "", map[string]any{"literal_kind": "number", "text": text})
	}
	relation := func(from, to int, role string, ordinal int) FrontendRelationFact {
		roleJSON, _ := json.Marshal(role)
		ordinalJSON, _ := json.Marshal(ordinal)
		return FrontendRelationFact{Kind: "syntax.child", From: from, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(to)}, Attributes: map[string]json.RawMessage{"role": roleJSON, "ordinal": ordinalJSON}}
	}
	facts := FrontendSemanticFacts{
		SchemaVersion: 1, BasisSHA256: uastEmbedded.BasisSHA256, LanguageProfile: "universal", LanguageFacet: make([]float64, 334),
		Projection: "frontend_facts.v1", Evaluation: "eager_left_to_right", ValueModel: "tagged_dynamic_binary64", IndexBase: 1,
		Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "semantic", EntryPoint: "main"},
		Nodes: []UniversalASTNode{
			kind(0, "Scope", "block", "", nil), kind(1, "ReturnStmt", "return", "", nil), kind(2, "CallExpr", "call", "", nil),
			kind(3, "SymbolRef", "identifier", "sum", nil), kind(4, "AggregateExpr", "aggregate", "", nil), literal(5, "4"), literal(6, "5"),
		},
		Relations: []FrontendRelationFact{
			relation(0, 1, "statement", 0), relation(1, 2, "expression", 0), relation(2, 3, "value", 0), relation(2, 4, "argument", 0),
			relation(4, 5, "member", 0), relation(4, 6, "member", 1),
		},
	}
	u, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		t.Fatal(err)
	}
	artifact, err := EmitNativeExecutable(semanticProgramFromUAST(u), "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "direct-uast-aggregate-sum.exe")
	if err := os.WriteFile(exe, artifact.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err == nil {
		t.Fatal("aggregate sum unexpectedly returned zero")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 9 {
		t.Fatalf("aggregate sum process exit=%v, want 9", err)
	}
}

func TestNativeRewriteMeanAggregateReachesNativeExecutable(t *testing.T) {
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	kind := func(id int, structural, semantic, name string, operation any) UniversalASTNode {
		fields := map[string]json.RawMessage{}
		for key, value := range map[string]any{"id": id, "kind": semantic, "scope_id": 0} {
			data, _ := json.Marshal(value)
			fields[key] = data
		}
		if name != "" {
			data, _ := json.Marshal(name)
			fields["name"] = data
		}
		if operation != nil {
			data, _ := json.Marshal(operation)
			fields["operation"] = data
		}
		n := UniversalASTNode{ID: id, StructuralKind: structural, SemanticFacets: defaultUniversalFacets(structural), Fields: fields}
		n.FieldMask, _ = universalFieldMask(&n)
		return n
	}
	literal := func(id int, text string) UniversalASTNode {
		return kind(id, "LiteralExpr", "literal", "", map[string]any{"literal_kind": "integer", "text": text})
	}
	relation := func(from, to int, role string, ordinal int) FrontendRelationFact {
		roleJSON, _ := json.Marshal(role)
		ordJSON, _ := json.Marshal(ordinal)
		return FrontendRelationFact{Kind: "syntax.child", From: from, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(to)}, Attributes: map[string]json.RawMessage{"role": roleJSON, "ordinal": ordJSON}}
	}
	facts := FrontendSemanticFacts{SchemaVersion: 1, BasisSHA256: uastEmbedded.BasisSHA256, LanguageProfile: "universal", LanguageFacet: make([]float64, 334), Projection: "frontend_facts.v1", Evaluation: "eager_left_to_right", ValueModel: "tagged_dynamic_binary64", IndexBase: 1, Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "semantic", EntryPoint: "main"},
		Nodes:     []UniversalASTNode{kind(0, "Scope", "block", "", nil), kind(1, "ReturnStmt", "return", "", nil), kind(2, "OperationExpr", "mean", "", nil), kind(3, "AggregateExpr", "aggregate", "", nil), literal(4, "4"), literal(5, "8")},
		Relations: []FrontendRelationFact{relation(0, 1, "statement", 0), relation(1, 2, "expression", 0), relation(2, 3, "value", 0), relation(3, 4, "member", 0), relation(3, 5, "member", 1)}}
	u, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		t.Fatal(err)
	}
	recipe := GeneratedLoweringRecipe{ID: "recipe.mean", Primitive: "MEAN", Class: "DERIVED", Guards: []string{"numeric", "ordered"}, ProofState: "EXACT", Steps: []LoweringRecipeStep{{Operation: "SUM", Inputs: []string{"$0"}, Output: "v1", Order: 1}, {Operation: "LENGTH", Inputs: []string{"$0"}, Output: "v2", Order: 2}, {Operation: "DIV", Inputs: []string{"v1", "v2"}, Output: "v3", Order: 3}}}
	rewritten, err := RewriteNativeFixedPoint(semanticProgramFromUAST(u), []GeneratedLoweringRecipe{recipe}, "native-x86_64-windows", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !rewritten.ReachedFixedPoint || len(rewritten.Proofs) != 1 {
		t.Fatalf("unexpected MEAN rewrite: %+v", rewritten)
	}
	artifact, err := EmitNativeExecutable(rewritten.Program, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "mean.exe")
	if err := os.WriteFile(exe, artifact.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err == nil {
		t.Fatal("MEAN unexpectedly returned zero")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 6 {
		t.Fatalf("MEAN process exit=%v, want 6", err)
	}
}

func TestNativeRewriteAllAggregateReachesNativeExecutable(t *testing.T) {
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	kind := func(id int, structural, semantic, name string, operation any) UniversalASTNode {
		fields := map[string]json.RawMessage{}
		for key, value := range map[string]any{"id": id, "kind": semantic, "scope_id": 0} {
			data, _ := json.Marshal(value)
			fields[key] = data
		}
		if name != "" {
			data, _ := json.Marshal(name)
			fields["name"] = data
		}
		if operation != nil {
			data, _ := json.Marshal(operation)
			fields["operation"] = data
		}
		n := UniversalASTNode{ID: id, StructuralKind: structural, SemanticFacets: defaultUniversalFacets(structural), Fields: fields}
		n.FieldMask, _ = universalFieldMask(&n)
		return n
	}
	literal := func(id int, text string) UniversalASTNode {
		return kind(id, "LiteralExpr", "literal", "", map[string]any{"literal_kind": "boolean", "text": text})
	}
	relation := func(from, to int, role string, ordinal int) FrontendRelationFact {
		r, _ := json.Marshal(role)
		o, _ := json.Marshal(ordinal)
		return FrontendRelationFact{Kind: "syntax.child", From: from, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(to)}, Attributes: map[string]json.RawMessage{"role": r, "ordinal": o}}
	}
	facts := FrontendSemanticFacts{SchemaVersion: 1, BasisSHA256: uastEmbedded.BasisSHA256, LanguageProfile: "universal", LanguageFacet: make([]float64, 334), Projection: "frontend_facts.v1", Evaluation: "eager_left_to_right", ValueModel: "tagged_dynamic_binary64", IndexBase: 1, Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "semantic", EntryPoint: "main"}, Nodes: []UniversalASTNode{kind(0, "Scope", "block", "", nil), kind(1, "ReturnStmt", "return", "", nil), kind(2, "OperationExpr", "all", "", nil), kind(3, "AggregateExpr", "aggregate", "", nil), literal(4, "true"), literal(5, "true")}, Relations: []FrontendRelationFact{relation(0, 1, "statement", 0), relation(1, 2, "expression", 0), relation(2, 3, "value", 0), relation(3, 4, "member", 0), relation(3, 5, "member", 1)}}
	u, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		t.Fatal(err)
	}
	recipe := GeneratedLoweringRecipe{ID: "recipe.all", Primitive: "ALL", Class: "DERIVED", Guards: []string{"truth_contract"}, ProofState: "EXACT", Steps: []LoweringRecipeStep{{Operation: "REDUCE_AND", Inputs: []string{"$0"}, Output: "v1", Order: 1}}}
	rewritten, err := RewriteNativeFixedPoint(semanticProgramFromUAST(u), []GeneratedLoweringRecipe{recipe}, "native-x86_64-windows", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !rewritten.ReachedFixedPoint || len(rewritten.Proofs) != 1 {
		t.Fatalf("unexpected ALL rewrite: %+v", rewritten)
	}
	artifact, err := EmitNativeExecutable(rewritten.Program, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "all.exe")
	if err := os.WriteFile(exe, artifact.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err == nil {
		t.Fatal("ALL unexpectedly returned zero")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("ALL process exit=%v, want 1", err)
	}
}

func TestNativeRewriteRMSAggregateBehaviorReachesNativeExecutable(t *testing.T) {
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	kind := func(id int, structural, semantic, name string, op any) UniversalASTNode {
		fields := map[string]json.RawMessage{}
		for k, v := range map[string]any{"id": id, "kind": semantic, "scope_id": 0} {
			d, _ := json.Marshal(v)
			fields[k] = d
		}
		if name != "" {
			d, _ := json.Marshal(name)
			fields["name"] = d
		}
		if op != nil {
			d, _ := json.Marshal(op)
			fields["operation"] = d
		}
		n := UniversalASTNode{ID: id, StructuralKind: structural, SemanticFacets: defaultUniversalFacets(structural), Fields: fields}
		n.FieldMask, _ = universalFieldMask(&n)
		return n
	}
	literal := func(id int, text string) UniversalASTNode {
		return kind(id, "LiteralExpr", "literal", "", map[string]any{"literal_kind": "number", "text": text})
	}
	rel := func(from, to int, role string, ord int) FrontendRelationFact {
		r, _ := json.Marshal(role)
		o, _ := json.Marshal(ord)
		return FrontendRelationFact{Kind: "syntax.child", From: from, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(to)}, Attributes: map[string]json.RawMessage{"role": r, "ordinal": o}}
	}
	facts := FrontendSemanticFacts{SchemaVersion: 1, BasisSHA256: uastEmbedded.BasisSHA256, LanguageProfile: "universal", LanguageFacet: make([]float64, 334), Projection: "frontend_facts.v1", Evaluation: "eager_left_to_right", ValueModel: "tagged_dynamic_binary64", IndexBase: 1, Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "semantic", EntryPoint: "main"}, Nodes: []UniversalASTNode{kind(0, "Scope", "block", "", nil), kind(1, "ReturnStmt", "return", "", nil), kind(2, "OperationExpr", "binary", "", map[string]any{"operator": ">"}), kind(3, "OperationExpr", "RMS", "", nil), kind(4, "AggregateExpr", "aggregate", "", nil), literal(5, "3"), literal(6, "4"), literal(7, "3.0")}, Relations: []FrontendRelationFact{rel(0, 1, "statement", 0), rel(1, 2, "expression", 0), rel(2, 3, "left", 0), rel(2, 7, "right", 1), rel(3, 4, "value", 0), rel(4, 5, "member", 0), rel(4, 6, "member", 1)}}
	u, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		t.Fatal(err)
	}
	recipe := GeneratedLoweringRecipe{ID: "recipe.rms", Primitive: "RMS", Class: "DERIVED", Guards: []string{"numeric", "ordered"}, ProofState: "EXACT", Steps: []LoweringRecipeStep{{Operation: "MUL", Inputs: []string{"$0", "$0"}, Output: "v1", Order: 1}, {Operation: "SUM", Inputs: []string{"v1"}, Output: "v2", Order: 2}, {Operation: "LENGTH", Inputs: []string{"$0"}, Output: "v3", Order: 3}, {Operation: "DIV", Inputs: []string{"v2", "v3"}, Output: "v4", Order: 4}, {Operation: "SQRT", Inputs: []string{"v4"}, Output: "v5", Order: 5}}}
	rw, err := RewriteNativeFixedPoint(semanticProgramFromUAST(u), []GeneratedLoweringRecipe{recipe}, "native-x86_64-windows", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !rw.ReachedFixedPoint || len(rw.Proofs) != 1 {
		t.Fatalf("unexpected RMS rewrite: %+v", rw)
	}
	artifact, err := EmitNativeExecutable(rw.Program, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "rms.exe")
	if err := os.WriteFile(exe, artifact.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err == nil {
		t.Fatal("RMS([3,4]) > 3 unexpectedly returned false")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("RMS([3,4]) > 3 native behavior exit=%v, want 1", err)
	}
}

func TestSemanticNativePipelineExecutesDirectUASTAggregateFor(t *testing.T) {
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	kind := func(id int, structural, semantic, name string, operation any) UniversalASTNode {
		fields := map[string]json.RawMessage{}
		for key, value := range map[string]any{"id": id, "kind": semantic, "scope_id": 0} {
			encoded, _ := json.Marshal(value)
			fields[key] = encoded
		}
		if name != "" {
			encoded, _ := json.Marshal(name)
			fields["name"] = encoded
		}
		if operation != nil {
			encoded, _ := json.Marshal(operation)
			fields["operation"] = encoded
		}
		n := UniversalASTNode{ID: id, StructuralKind: structural, SemanticFacets: defaultUniversalFacets(structural), Fields: fields}
		n.FieldMask, _ = universalFieldMask(&n)
		return n
	}
	literal := func(id int, text string) UniversalASTNode {
		return kind(id, "LiteralExpr", "literal", "", map[string]any{"literal_kind": "integer", "text": text})
	}
	relation := func(from, to int, role string, ordinal int) FrontendRelationFact {
		roleJSON, _ := json.Marshal(role)
		ordinalJSON, _ := json.Marshal(ordinal)
		return FrontendRelationFact{Kind: "syntax.child", From: from, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(to)}, Attributes: map[string]json.RawMessage{"role": roleJSON, "ordinal": ordinalJSON}}
	}
	facts := FrontendSemanticFacts{
		SchemaVersion: 1, BasisSHA256: uastEmbedded.BasisSHA256, LanguageProfile: "universal", LanguageFacet: make([]float64, 334),
		Projection: "frontend_facts.v1", Evaluation: "eager_left_to_right", ValueModel: "tagged_dynamic_binary64", IndexBase: 1,
		Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "semantic", EntryPoint: "main"},
		Nodes: []UniversalASTNode{
			kind(0, "Scope", "block", "", nil), kind(1, "ForEachStmt", "for", "x", nil),
			kind(2, "AggregateExpr", "aggregate", "", nil), literal(3, "0"), literal(4, "1"),
			kind(5, "Scope", "block", "", nil), kind(6, "ReturnStmt", "return", "", nil), literal(7, "0"),
		},
		Relations: []FrontendRelationFact{
			relation(0, 1, "statement", 0), relation(1, 2, "sequence", 0), relation(1, 5, "body", 1),
			relation(0, 6, "statement", 1), relation(6, 7, "expression", 0),
			relation(2, 3, "member", 0), relation(2, 4, "member", 1),
		},
	}
	u, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EmitNativeExecutable(semanticProgramFromUAST(u), "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := t.TempDir() + "\\direct-uast-aggregate-for.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err != nil {
		t.Fatalf("direct UAST aggregate for process failed: %v", err)
	}
}

func TestSemanticNativePipelineExecutesDirectUASTIndexPlaceWrite(t *testing.T) {
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	kind := func(id int, structural, semantic, name string, operation any) UniversalASTNode {
		fields := map[string]json.RawMessage{}
		for key, value := range map[string]any{"id": id, "kind": semantic, "scope_id": 0} {
			encoded, _ := json.Marshal(value)
			fields[key] = encoded
		}
		if name != "" {
			encoded, _ := json.Marshal(name)
			fields["name"] = encoded
		}
		if operation != nil {
			encoded, _ := json.Marshal(operation)
			fields["operation"] = encoded
		}
		n := UniversalASTNode{ID: id, StructuralKind: structural, SemanticFacets: defaultUniversalFacets(structural), Fields: fields}
		n.FieldMask, _ = universalFieldMask(&n)
		return n
	}
	literal := func(id int, text string) UniversalASTNode {
		return kind(id, "LiteralExpr", "literal", "", map[string]any{"literal_kind": "integer", "text": text})
	}
	identifier := func(id int) UniversalASTNode {
		return kind(id, "SymbolRef", "identifier", "a", nil)
	}
	relation := func(from, to int, role string, ordinal int) FrontendRelationFact {
		roleJSON, _ := json.Marshal(role)
		ordinalJSON, _ := json.Marshal(ordinal)
		return FrontendRelationFact{Kind: "syntax.child", From: from, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(to)}, Attributes: map[string]json.RawMessage{"role": roleJSON, "ordinal": ordinalJSON}}
	}
	facts := FrontendSemanticFacts{
		SchemaVersion: 1, BasisSHA256: uastEmbedded.BasisSHA256, LanguageProfile: "universal", LanguageFacet: make([]float64, 334),
		Projection: "frontend_facts.v1", Evaluation: "eager_left_to_right", ValueModel: "tagged_dynamic_binary64", IndexBase: 1,
		Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "semantic", EntryPoint: "main"},
		Nodes: []UniversalASTNode{
			kind(0, "Scope", "block", "", nil), kind(1, "AssignStmt", "assign", "a", nil), kind(2, "AggregateExpr", "aggregate", "", nil), literal(3, "0"), literal(4, "1"),
			kind(5, "AssignStmt", "assign", "a", nil), kind(6, "IndexExpr", "index", "", nil), identifier(7), literal(8, "1"), literal(9, "42"),
			kind(10, "ReturnStmt", "return", "", nil), kind(11, "IndexExpr", "index", "", nil), identifier(12), literal(13, "1"),
		},
		Relations: []FrontendRelationFact{
			relation(0, 1, "statement", 0), relation(1, 2, "expression", 0), relation(2, 3, "member", 0), relation(2, 4, "member", 1),
			relation(0, 5, "statement", 1), relation(5, 6, "target", 0), relation(5, 9, "expression", 0), relation(6, 7, "value", 0), relation(6, 8, "argument", 0),
			relation(0, 10, "statement", 2), relation(10, 11, "expression", 0), relation(11, 12, "value", 0), relation(11, 13, "argument", 0),
		},
	}
	u, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EmitNativeExecutable(semanticProgramFromUAST(u), "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := t.TempDir() + "\\direct-uast-index-place-write.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err == nil {
		t.Fatal("index place write unexpectedly returned exit 0")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 42 {
		t.Fatalf("index place write process exit=%v, want 42", err)
	}
}

func TestSemanticNativePipelineExecutesDirectUASTAddressDeref(t *testing.T) {
	if err := loadUniversalASTBasis(); err != nil {
		t.Fatal(err)
	}
	kind := func(id int, structural, semantic, name string, operation any) UniversalASTNode {
		fields := map[string]json.RawMessage{}
		for key, value := range map[string]any{"id": id, "kind": semantic, "scope_id": 0} {
			encoded, _ := json.Marshal(value)
			fields[key] = encoded
		}
		if name != "" {
			encoded, _ := json.Marshal(name)
			fields["name"] = encoded
		}
		if operation != nil {
			encoded, _ := json.Marshal(operation)
			fields["operation"] = encoded
		}
		n := UniversalASTNode{ID: id, StructuralKind: structural, SemanticFacets: defaultUniversalFacets(structural), Fields: fields}
		n.FieldMask, _ = universalFieldMask(&n)
		return n
	}
	literal := func(id int, text string) UniversalASTNode {
		return kind(id, "LiteralExpr", "literal", "", map[string]any{"literal_kind": "integer", "text": text})
	}
	identifier := func(id int, name string) UniversalASTNode { return kind(id, "SymbolRef", "identifier", name, nil) }
	relation := func(from, to int, role string, ordinal int) FrontendRelationFact {
		roleJSON, _ := json.Marshal(role)
		ordinalJSON, _ := json.Marshal(ordinal)
		return FrontendRelationFact{Kind: "syntax.child", From: from, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(to)}, Attributes: map[string]json.RawMessage{"role": roleJSON, "ordinal": ordinalJSON}}
	}
	facts := FrontendSemanticFacts{
		SchemaVersion: 1, BasisSHA256: uastEmbedded.BasisSHA256, LanguageProfile: "universal", LanguageFacet: make([]float64, 334),
		Projection: "frontend_facts.v1", Evaluation: "eager_left_to_right", ValueModel: "tagged_dynamic_binary64", IndexBase: 1,
		Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "semantic", EntryPoint: "main"},
		Nodes: []UniversalASTNode{
			kind(0, "Scope", "block", "", nil), kind(1, "AssignStmt", "assign", "a", nil), kind(2, "AggregateExpr", "aggregate", "", nil), literal(3, "1"), literal(4, "2"),
			kind(5, "AssignStmt", "assign", "p", nil), kind(6, "AddressOf", "address_of", "", nil), kind(7, "IndexExpr", "index", "", nil), identifier(8, "a"), literal(9, "2"),
			kind(10, "AssignStmt", "assign", "", nil), literal(11, "42"), kind(12, "Deref", "deref", "", nil), identifier(13, "p"),
			kind(14, "ReturnStmt", "return", "", nil), kind(15, "IndexExpr", "index", "", nil), identifier(16, "a"), literal(17, "2"),
		},
		Relations: []FrontendRelationFact{
			relation(0, 1, "statement", 0), relation(1, 2, "expression", 0), relation(2, 3, "member", 0), relation(2, 4, "member", 1),
			relation(0, 5, "statement", 1), relation(5, 6, "expression", 0), relation(6, 7, "value", 0), relation(7, 8, "value", 0), relation(7, 9, "argument", 0),
			relation(0, 10, "statement", 2), relation(10, 12, "target", 0), relation(10, 11, "expression", 0), relation(12, 13, "value", 0),
			relation(0, 14, "statement", 3), relation(14, 15, "expression", 0), relation(15, 16, "value", 0), relation(15, 17, "argument", 0),
		},
	}
	u, err := BuildCanonicalUniversalASTFromFrontendFacts(facts)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EmitNativeExecutable(semanticProgramFromUAST(u), "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := t.TempDir() + "\\direct-uast-address-deref.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err == nil {
		t.Fatal("address/deref unexpectedly returned exit 0")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 42 {
		t.Fatalf("address/deref process exit=%v, want 42", err)
	}
}

func TestResolveCompileTimeConstructsKeepsCanonicalUAST(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ReturnStmt{X: &LiteralExpr{Kind: "integer", Text: "9"}},
	}}, "eager_left_to_right")
	resolved, err := ResolveCompileTimeConstructs(p, "native-x86_64-windows")
	if err != nil {
		t.Fatal(err)
	}
	if resolved != p || resolved.UniversalAST == nil {
		t.Fatal("compile-time resolution replaced the program or did not materialize the canonical UAST")
	}
	if _, ok := resolved.UniversalAST.Extensions["semantic_closure"]; !ok {
		t.Fatal("compile-time resolution did not record semantic closure")
	}
}

func TestSemanticNativePipelineEmbedsStringData(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&AssignStmt{Name: "s", Value: &LiteralExpr{Kind: "string", Text: `"hello"`}},
	}}, "eager_left_to_right")
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.Bytes, []byte("hello\x00")) {
		t.Fatal("string literal was not emitted into the native image")
	}
}

func TestSemanticNativePipelineExecutesNamedFunctionCall(t *testing.T) {
	p, err := LowerNativeGo("function.go", `package main
func inc(x int64) int64 { return x + 1 }
func main() { if inc(41) == 0 {} }
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bytes) == 0 || result.InstructionCount == 0 {
		t.Fatal("named function call produced no executable")
	}
	exe := t.TempDir() + "\\function-call.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	if err := cmd.Run(); err != nil {
		t.Fatalf("native named function process failed: %v", err)
	}
}

func TestSemanticNativePipelineExecutesOrderedMultiResultCall(t *testing.T) {
	source := `package main
func pair() (int64, int64) { return 40, 1 }
func main() { first, second := pair(); if first != 40 { first = 0 }; if second != 1 { first = 0 } }
`
	p, err := LowerNativeGo("multi-result.go", source)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := t.TempDir() + "\\ordered-multi-result.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err != nil {
		t.Fatalf("ordered multi-result process failed: %v", err)
	}
}

func TestSemanticNativePipelineExecutesFunctionValueCall(t *testing.T) {
	p, err := LowerNativeGo("function-value.go", `package main
func inc(x int64) int64 { return x + 1 }
func main() { f := inc; f(41) }
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.Bytes, []byte{0xff, 0xd0}) {
		t.Fatal("function value call did not emit the indirect call r/m64 form")
	}
	exe := t.TempDir() + "\\function-value-call.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	if err := cmd.Run(); err != nil {
		t.Fatalf("function value call process failed: %v", err)
	}
}

func TestSemanticNativePipelineExecutesNonCapturingClosureValue(t *testing.T) {
	p, err := LowerNativeGo("closure-value.go", `package main
func main() { f := func(x int64) int64 { return x + 1 }; f(41) }
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bytes) == 0 || result.InstructionCount == 0 {
		t.Fatal("non-capturing closure produced no native code")
	}
	exe := t.TempDir() + "\\closure-value.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err != nil {
		t.Fatalf("non-capturing closure process failed: %v", err)
	}
}

func TestSemanticNativePipelineExecutesCapturedClosureValue(t *testing.T) {
	p, err := LowerNativeGo("captured-closure.go", `package main
func main() { x := int64(41); f := func(a int64) int64 { return a + x }; f(1) }
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bytes) == 0 || result.InstructionCount == 0 {
		t.Fatal("captured closure produced no native code")
	}
	exe := t.TempDir() + "\\captured-closure.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err != nil {
		t.Fatalf("captured closure process failed: %v", err)
	}
}

func TestSemanticNativePipelineExecutesTypedIntegerDivide(t *testing.T) {
	int64Type := integerType(64, true)
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ReturnStmt{X: &OperationExpr{Operation: SemanticOperation{Name: "integer.divide", Type: int64Type}, Operands: []Expr{
			&OperationExpr{Operation: SemanticOperation{Name: "integer.literal", Type: int64Type, Text: "42"}},
			&OperationExpr{Operation: SemanticOperation{Name: "integer.literal", Type: int64Type, Text: "2"}},
		}}},
	}}, "eager_left_to_right")
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := t.TempDir() + "\\typed-divide.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	if err := cmd.Run(); err == nil {
		t.Fatal("typed divide unexpectedly returned process exit 0")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 21 {
		t.Fatalf("typed divide process exit=%v, want 21", err)
	}
}

func TestSemanticNativePipelineExecutesConstantTypedIntegerFormat(t *testing.T) {
	int64Type := integerType(64, true)
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ExprStmt{X: &OperationExpr{Operation: SemanticOperation{Name: "integer.format", Type: int64Type}, Operands: []Expr{
			&OperationExpr{Operation: SemanticOperation{Name: "integer.literal", Type: int64Type, Text: "42"}},
		}}},
		&ReturnStmt{X: &LiteralExpr{Kind: "integer", Text: "0"}},
	}}, "eager_left_to_right")
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(result.Bytes, []byte("42\x00")) {
		t.Fatal("constant integer format did not emit native string data")
	}
	exe := t.TempDir() + "\\typed-integer-format.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err != nil {
		t.Fatalf("constant typed integer format process failed: %v", err)
	}
}

func TestSemanticNativePipelineExecutesDynamicTypedIntegerFormat(t *testing.T) {
	int64Type := integerType(64, true)
	dynamicValue := &OperationExpr{Operation: SemanticOperation{Name: "integer.add", Type: int64Type}, Operands: []Expr{
		&OperationExpr{Operation: SemanticOperation{Name: "integer.literal", Type: int64Type, Text: "40"}},
		&OperationExpr{Operation: SemanticOperation{Name: "integer.literal", Type: int64Type, Text: "2"}},
	}}
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ExprStmt{X: &OperationExpr{Operation: SemanticOperation{Name: "integer.format", Type: int64Type}, Operands: []Expr{dynamicValue}}},
		&ReturnStmt{X: &LiteralExpr{Kind: "integer", Text: "0"}},
	}}, "eager_left_to_right")
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := t.TempDir() + "\\dynamic-typed-integer-format.exe"
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err != nil {
		t.Fatalf("dynamic typed integer format process failed: %v", err)
	}
}

func TestExactNativeClosureRejectsUnknownOperation(t *testing.T) {
	basis, err := BuildNativeCapabilityBasis("native-x86_64-windows")
	if err != nil {
		t.Fatal(err)
	}
	closure := SolveExactClosure(SemanticRequirements{Operations: []string{"UNKNOWN_OPERATION"}}, nil, basis)
	if closure.Supported || len(closure.Missing) != 1 || closure.Missing[0] != "UNKNOWN_OPERATION" {
		t.Fatalf("unexpected closure: %+v", closure)
	}
}

func TestExactNativeClosureChecksRecipeSteps(t *testing.T) {
	basis, err := BuildNativeCapabilityBasis("native-x86_64-windows")
	if err != nil {
		t.Fatal(err)
	}
	closure := SolveExactClosure(SemanticRequirements{Operations: []string{"DERIVED"}}, []GeneratedLoweringRecipe{{
		ID: "recipe.derived", Primitive: "DERIVED", ProofState: "EXACT",
		Steps: []LoweringRecipeStep{{Operation: "UNKNOWN_STEP"}},
	}}, basis)
	if closure.Supported || len(closure.Missing) != 1 || closure.Missing[0] != "DERIVED" {
		t.Fatalf("recipe with unsupported step became reachable: %+v", closure)
	}
}

func TestVerifiedIdentityRecipePreservesUASTGraph(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ReturnStmt{X: &LiteralExpr{Kind: "integer", Text: "3"}},
	}}, "eager_left_to_right")
	if _, err := p.Document(); err != nil {
		t.Fatal(err)
	}
	if len(p.UniversalAST.Nodes) == 0 {
		t.Fatal("expected canonical UAST nodes")
	}
	rewritten, err := ApplyVerifiedGraphRewrite(p, []int{p.UniversalAST.Nodes[0].ID}, GeneratedLoweringRecipe{
		ID: "recipe.identity", Primitive: "IDENTITY", ProofState: "EXACT",
		Steps: []LoweringRecipeStep{{Operation: "RESULT", Inputs: []string{"$0"}, Output: "v1", Order: 1}},
	}, "native-x86_64-windows")
	if err != nil {
		t.Fatal(err)
	}
	if rewritten.UniversalAST.Metadata["lowering.recipe"] != "recipe.identity" {
		t.Fatal("identity rewrite provenance missing")
	}
}

func TestVerifiedDoubleRecipeReplacesOperandGraph(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ReturnStmt{X: &UnaryExpr{Op: "+", X: &LiteralExpr{Kind: "number", Text: "2"}}},
	}}, "eager_left_to_right")
	if _, err := p.Document(); err != nil {
		t.Fatal(err)
	}
	var doubleID int
	for i := range p.UniversalAST.Nodes {
		c, err := decodeUniversalCommon(&p.UniversalAST.Nodes[i])
		if err != nil {
			t.Fatal(err)
		}
		if c.Kind == "unary" {
			doubleID = c.ID
			kind, _ := json.Marshal("double")
			p.UniversalAST.Nodes[i].Fields["kind"] = kind
			break
		}
	}
	if doubleID == 0 {
		t.Fatal("binary witness node not found")
	}
	recipe := GeneratedLoweringRecipe{ID: "recipe.double", Primitive: "DOUBLE", Class: "DERIVED", Guards: []string{"numeric"}, ProofState: "EXACT", Steps: []LoweringRecipeStep{{Operation: "ADD", Inputs: []string{"$0", "$0"}, Output: "v1", Order: 1}}}
	rewritten, err := ApplyVerifiedGraphRewrite(p, []int{doubleID}, recipe, "native-x86_64-windows")
	if err != nil {
		t.Fatal(err)
	}
	graph, err := newUASTExecutionGraph(rewritten.UniversalAST)
	if err != nil {
		t.Fatal(err)
	}
	children := graph.orderedChildren(doubleID)
	if len(children) != 2 || children[0].ID == children[1].ID {
		t.Fatalf("DOUBLE rewrite did not create two bound operand nodes: %#v", children)
	}
	left, right := graph.common[children[0].ID], graph.common[children[1].ID]
	if left.Kind != "literal" || right.Kind != "literal" || left.Operation.Text != right.Operation.Text {
		t.Fatalf("DOUBLE rewrite changed operand semantics: left=%#v right=%#v", left, right)
	}
	result, err := EmitNativeExecutable(rewritten, "native-x86_64-windows", "")
	if err != nil || len(result.Bytes) == 0 {
		t.Fatalf("rewritten DOUBLE graph did not reach native PE emission: result=%+v err=%v", result, err)
	}
}

func TestVerifiedDoubleRecipeClonesRecursiveOperandGraph(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ReturnStmt{X: &UnaryExpr{Op: "+", X: &BinaryExpr{Op: "+", L: &LiteralExpr{Kind: "number", Text: "2"}, R: &LiteralExpr{Kind: "number", Text: "3"}}}},
	}}, "eager_left_to_right")
	if _, err := p.Document(); err != nil {
		t.Fatal(err)
	}
	var doubleID int
	for i := range p.UniversalAST.Nodes {
		c, err := decodeUniversalCommon(&p.UniversalAST.Nodes[i])
		if err != nil {
			t.Fatal(err)
		}
		if c.Kind == "unary" {
			doubleID = c.ID
			kind, _ := json.Marshal("double")
			p.UniversalAST.Nodes[i].Fields["kind"] = kind
			break
		}
	}
	if doubleID == 0 {
		t.Fatal("unary witness node not found")
	}
	recipe := GeneratedLoweringRecipe{ID: "recipe.double.recursive", Primitive: "DOUBLE", Class: "DERIVED", Guards: []string{"numeric"}, ProofState: "EXACT", Steps: []LoweringRecipeStep{{Operation: "ADD", Inputs: []string{"$0", "$0"}, Output: "v1", Order: 1}}}
	rewritten, err := ApplyVerifiedGraphRewrite(p, []int{doubleID}, recipe, "native-x86_64-windows")
	if err != nil {
		t.Fatal(err)
	}
	graph, err := newUASTExecutionGraph(rewritten.UniversalAST)
	if err != nil {
		t.Fatal(err)
	}
	children := graph.orderedChildren(doubleID)
	if len(children) != 2 || children[0].ID == children[1].ID {
		t.Fatalf("recursive DOUBLE rewrite did not clone the operand root: %#v", children)
	}
	for _, child := range children {
		if graph.common[child.ID].Kind != "binary" || len(graph.orderedChildren(child.ID)) != 2 {
			t.Fatalf("recursive DOUBLE clone lost its child graph: node=%d common=%#v", child.ID, graph.common[child.ID])
		}
	}
	result, err := EmitNativeExecutable(rewritten, "native-x86_64-windows", "")
	if err != nil || len(result.Bytes) == 0 {
		t.Fatalf("recursive DOUBLE graph did not reach native PE emission: result=%+v err=%v", result, err)
	}
}

func TestNativeRewriteFixedPointRewritesEveryVerifiedMatchToNative(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ReturnStmt{X: &BinaryExpr{Op: "+",
			L: &UnaryExpr{Op: "+", X: &LiteralExpr{Kind: "number", Text: "2"}},
			R: &UnaryExpr{Op: "+", X: &LiteralExpr{Kind: "number", Text: "3"}},
		}},
	}}, "eager_left_to_right")
	if _, err := p.Document(); err != nil {
		t.Fatal(err)
	}
	doubles := 0
	for i := range p.UniversalAST.Nodes {
		c, err := decodeUniversalCommon(&p.UniversalAST.Nodes[i])
		if err != nil {
			t.Fatal(err)
		}
		if c.Kind == "unary" {
			kind, _ := json.Marshal("double")
			p.UniversalAST.Nodes[i].Fields["kind"] = kind
			doubles++
		}
	}
	if doubles != 2 {
		t.Fatalf("expected two DOUBLE witnesses, got %d", doubles)
	}
	recipe := GeneratedLoweringRecipe{ID: "recipe.double.fixed-point", Primitive: "DOUBLE", Class: "DERIVED", Guards: []string{"numeric"}, ProofState: "EXACT", Steps: []LoweringRecipeStep{{Operation: "ADD", Inputs: []string{"$0", "$0"}, Output: "v1", Order: 1}}}
	result, err := RewriteNativeFixedPoint(p, []GeneratedLoweringRecipe{recipe}, "native-x86_64-windows", 4)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ReachedFixedPoint || result.Iterations != 2 || len(result.Proofs) != 2 {
		t.Fatalf("unexpected fixed-point result: %+v", result)
	}
	for _, proof := range result.Proofs {
		if proof.PreDigest == proof.PostDigest || !proof.EvaluationOrderPreserved || !proof.EffectsPreserved || !proof.ValueContractPreserved {
			t.Fatalf("incomplete rewrite proof: %+v", proof)
		}
	}
	for _, node := range result.Program.UniversalAST.Nodes {
		c, err := decodeUniversalCommon(&node)
		if err != nil {
			t.Fatal(err)
		}
		if strings.EqualFold(c.Kind, "double") {
			t.Fatalf("unrewritten DOUBLE node %d", node.ID)
		}
	}
	artifact, err := EmitNativeExecutable(result.Program, "native-x86_64-windows", "")
	if err != nil || len(artifact.Bytes) == 0 {
		t.Fatalf("fixed-point UAST did not reach native emission: artifact=%+v err=%v", artifact, err)
	}
}

func TestNativeRewriteAverage2FormulaReachesNativeExecutable(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&ReturnStmt{X: &BinaryExpr{Op: "+", L: &LiteralExpr{Kind: "integer", Text: "4"}, R: &LiteralExpr{Kind: "integer", Text: "8"}}},
	}}, "eager_left_to_right")
	if _, err := p.Document(); err != nil {
		t.Fatal(err)
	}
	var averageID int
	for i := range p.UniversalAST.Nodes {
		c, err := decodeUniversalCommon(&p.UniversalAST.Nodes[i])
		if err != nil {
			t.Fatal(err)
		}
		if c.Kind == "binary" {
			kind, _ := json.Marshal("average2")
			p.UniversalAST.Nodes[i].Fields["kind"] = kind
			averageID = p.UniversalAST.Nodes[i].ID
			break
		}
	}
	if averageID == 0 {
		t.Fatal("AVERAGE2 witness node not found")
	}
	recipe := GeneratedLoweringRecipe{ID: "recipe.average2", Primitive: "AVERAGE2", Class: "DERIVED", Guards: []string{"numeric", "ordered"}, ProofState: "EXACT", Steps: []LoweringRecipeStep{
		{Operation: "ADD", Inputs: []string{"$0", "$1"}, Output: "v1", Order: 1},
		{Operation: "CONST", Inputs: []string{"2"}, Output: "v2", Order: 2},
		{Operation: "DIV", Inputs: []string{"v1", "v2"}, Output: "v3", Order: 3},
	}}
	result, err := RewriteNativeFixedPoint(p, []GeneratedLoweringRecipe{recipe}, "native-x86_64-windows", 2)
	if err != nil {
		t.Fatal(err)
	}
	if !result.ReachedFixedPoint || len(result.Proofs) != 1 || result.Proofs[0].MatchNodeID != averageID {
		t.Fatalf("unexpected AVERAGE2 rewrite result: %+v", result)
	}
	graph, err := newUASTExecutionGraph(result.Program.UniversalAST)
	if err != nil {
		t.Fatal(err)
	}
	for id, c := range graph.common {
		if c.Kind != "return" {
			continue
		}
		value, ok, relationErr := graph.one(id, "expression", false)
		if relationErr != nil || !ok || graph.common[value].Kind != "binary" || graph.common[value].Operation.Operator != "/" {
			t.Fatalf("AVERAGE2 return/result wiring lost: return=%#v child=%d childCommon=%#v err=%v", c, value, graph.common[value], relationErr)
		}
	}
	artifact, err := EmitNativeExecutable(result.Program, "native-x86_64-windows", "")
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "average2.exe")
	if err := os.WriteFile(exe, artifact.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err == nil {
		t.Fatal("AVERAGE2 witness unexpectedly returned zero")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 6 {
		t.Fatalf("AVERAGE2 native behavior: %v", err)
	}
}
