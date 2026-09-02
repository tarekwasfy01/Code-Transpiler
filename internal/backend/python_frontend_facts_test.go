package backend

import (
	"encoding/json"
	"reflect"
	"sort"
	"testing"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

func TestPythonSharedFactsFrontendEquivalence(t *testing.T) {
	source := "x = 2\nif x < 3:\n    x = x + 1\nelse:\n    x = 0\nprint(x)\n"
	canonical, err := matrixir.Canonicalize("python", source)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := LowerMatrixEvents("python", canonical.Events)
	if err != nil {
		t.Fatal(err)
	}
	legacyDoc, err := legacy.Document()
	if err != nil {
		t.Fatal(err)
	}
	referenceFacts, err := frontendSemanticFactsFromUniversalAST(legacyDoc.UniversalAST, nil)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := BuildCanonicalUniversalASTFromFrontendFacts(referenceFacts)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := LowerPython(source)
	if err != nil {
		t.Fatal(err)
	}
	if direct.Body != nil {
		t.Fatal("Python direct frontend retained a legacy executable body")
	}
	if err := ValidateSemanticProgram(direct); err != nil {
		t.Fatal(err)
	}
	want, err := EmitSemantic("go", &SemanticProgram{UniversalAST: reference, Evaluation: reference.Evaluation, ValueModel: reference.ValueModel, IndexBase: reference.IndexBase, Types: reference.Types, Origin: reference.Origin, Metadata: reference.Metadata, Evidence: reference.Evidence})
	if err != nil {
		t.Fatal(err)
	}
	got, err := EmitSemantic("go", direct)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatal("Python direct facts changed Go lowering")
	}
}

func TestTranspileFromPythonUsesSharedFrontend(t *testing.T) {
	code, err := TranspileFrom("python", "go", "x = 2\nprint(x)\n")
	if err != nil || code == "" {
		t.Fatalf("Python shared frontend output=%q err=%v", code, err)
	}
}

func TestPythonIterableForUsesCanonicalForEach(t *testing.T) {
	p, err := LowerPython("for item in values:\n    print(item)\n")
	if err != nil {
		t.Fatal(err)
	}
	if p.UniversalAST == nil {
		t.Fatal("missing canonical UAST")
	}
	found := false
	for _, n := range p.UniversalAST.Nodes {
		if n.StructuralKind == "ForEachStmt" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("iterable Python for did not project to ForEachStmt")
	}
}

func TestPythonSimpleLoopPatternUsesExistingBindingPattern(t *testing.T) {
	p, err := LowerMatrixLanguage("python", "for key, value in items:\n    print(value)\n")
	if err != nil {
		t.Fatal(err)
	}
	var loop, pattern bool
	for _, n := range p.UniversalAST.Nodes {
		loop = loop || n.StructuralKind == "ForEachStmt"
		pattern = pattern || n.StructuralKind == "BindingPattern"
	}
	if !loop || !pattern {
		t.Fatalf("expected ForEachStmt + BindingPattern, got %#v", p.UniversalAST.Nodes)
	}
}

func TestTranspileFromPythonRecognizesDictionaryLiteral(t *testing.T) {
	code, err := TranspileFrom("python", "go", "items = {\"a\": 1}\nprint(items)\n")
	if err != nil || code == "" {
		t.Fatalf("Python dictionary frontend output=%q err=%v", code, err)
	}
}

func TestPythonBundledMatrixLoweringsRemainCanonicalUAST(t *testing.T) {
	source := "fn = lambda x: x\nfor n in range(5, 0, -2):\n    if n < 4:\n        pass\n    elif n < 5:\n        print(fn(n))\n"
	p, err := LowerPython(source)
	if err != nil {
		t.Fatal(err)
	}
	if p.UniversalAST == nil || p.Body != nil {
		t.Fatal("bundled Python lowerings did not produce UAST-only program")
	}
	if err := ValidateSemanticProgram(p); err != nil {
		t.Fatal(err)
	}
}

func TestAllRegisteredMatrixLanguagesEmitCanonicalUASTFromFacts(t *testing.T) {
	for _, language := range MatrixFrontendLanguages() {
		language := language
		t.Run(language, func(t *testing.T) {
			source := "x = 1\nprint(x)\n"
			canonical, err := matrixir.Canonicalize(language, source)
			if err != nil {
				t.Fatal(err)
			}
			legacy, err := LowerMatrixEvents(language, canonical.Events)
			if err != nil {
				t.Fatal(err)
			}
			legacyDoc, err := legacy.Document()
			if err != nil {
				t.Fatal(err)
			}
			legacyFacts, err := frontendSemanticFactsFromUniversalAST(legacyDoc.UniversalAST, nil)
			if err != nil {
				t.Fatal(err)
			}
			reference, err := BuildCanonicalUniversalASTFromFrontendFacts(legacyFacts)
			if err != nil {
				t.Fatal(err)
			}
			program, err := LowerMatrixLanguage(language, source)
			if err != nil {
				t.Fatal(err)
			}
			if program.UniversalAST == nil || program.Body != nil {
				t.Fatal("matrix frontend did not return canonical UAST-only program")
			}
			if err := ValidateSemanticProgram(program); err != nil {
				t.Fatal(err)
			}
			want, err := EmitSemantic("go", &SemanticProgram{UniversalAST: reference, Evaluation: reference.Evaluation, ValueModel: reference.ValueModel, IndexBase: reference.IndexBase, Types: reference.Types, Origin: reference.Origin, Metadata: reference.Metadata, Evidence: reference.Evidence})
			if err != nil {
				t.Fatal(err)
			}
			got, err := EmitSemantic("go", program)
			if err != nil {
				t.Fatal(err)
			}
			if got != want {
				t.Fatal("direct facts changed Go lowering")
			}
		})
	}
}

// compareCanonicalUAST compares UAST semantics by category. Provenance unique
// to a legacy SemanticDocument is intentionally outside this migration oracle.
func compareCanonicalUAST(legacy, direct *UniversalASTDocument) map[string]bool {
	result := map[string]bool{}
	if legacy == nil || direct == nil {
		return map[string]bool{"structure": false}
	}
	normalNodes := func(nodes []UniversalASTNode, selectNode func(UniversalASTNode) any) []string {
		out := make([]string, 0, len(nodes))
		for _, n := range nodes {
			data, _ := json.Marshal(selectNode(n))
			out = append(out, string(data))
		}
		sort.Strings(out)
		return out
	}
	result["structure"] = reflect.DeepEqual(normalNodes(legacy.Nodes, func(n UniversalASTNode) any {
		return struct {
			ID   int
			Kind string
		}{n.ID, n.StructuralKind}
	}), normalNodes(direct.Nodes, func(n UniversalASTNode) any {
		return struct {
			ID   int
			Kind string
		}{n.ID, n.StructuralKind}
	}))
	result["fields"] = reflect.DeepEqual(normalNodes(legacy.Nodes, func(n UniversalASTNode) any {
		return struct {
			ID     int
			Fields map[string]json.RawMessage
		}{n.ID, n.Fields}
	}), normalNodes(direct.Nodes, func(n UniversalASTNode) any {
		return struct {
			ID     int
			Fields map[string]json.RawMessage
		}{n.ID, n.Fields}
	}))
	result["sources"] = reflect.DeepEqual(normalNodes(legacy.Nodes, func(n UniversalASTNode) any {
		return struct {
			ID     int
			Source *SemanticSourceSpan
		}{n.ID, n.Source}
	}), normalNodes(direct.Nodes, func(n UniversalASTNode) any {
		return struct {
			ID     int
			Source *SemanticSourceSpan
		}{n.ID, n.Source}
	}))
	result["types"] = reflect.DeepEqual(legacy.TypeTable, direct.TypeTable) && reflect.DeepEqual(legacy.TypeGraph, direct.TypeGraph) && reflect.DeepEqual(legacy.TypeRelations, direct.TypeRelations)
	result["symbols"] = reflect.DeepEqual(normalNodes(legacy.Nodes, func(n UniversalASTNode) any {
		return struct {
			ID   int
			Name json.RawMessage
		}{n.ID, n.Fields["name"]}
	}), normalNodes(direct.Nodes, func(n UniversalASTNode) any {
		return struct {
			ID   int
			Name json.RawMessage
		}{n.ID, n.Fields["name"]}
	}))
	result["bindings"] = reflect.DeepEqual(legacy.Evidence.Bindings, direct.Evidence.Bindings) && sameSparse(legacy.Evidence.Binding, direct.Evidence.Binding)
	result["facets"] = reflect.DeepEqual(normalNodes(legacy.Nodes, func(n UniversalASTNode) any {
		return struct {
			ID     int
			Facets []string
		}{n.ID, n.SemanticFacets}
	}), normalNodes(direct.Nodes, func(n UniversalASTNode) any {
		return struct {
			ID     int
			Facets []string
		}{n.ID, n.SemanticFacets}
	}))
	relations := func(rs []UniversalASTRelation) []string {
		out := make([]string, 0, len(rs))
		for _, r := range rs {
			b, _ := json.Marshal(r)
			out = append(out, string(b))
		}
		sort.Strings(out)
		return out
	}
	result["relations"] = reflect.DeepEqual(relations(legacy.Relations), relations(direct.Relations))
	result["evidence"] = reflect.DeepEqual(legacy.Evidence, direct.Evidence)
	result["coverage"] = reflect.DeepEqual(legacy.LanguageFacet, direct.LanguageFacet)
	result["language projection"] = legacy.LanguageProfile == direct.LanguageProfile && reflect.DeepEqual(legacy.LanguageFacet, direct.LanguageFacet)
	return result
}
