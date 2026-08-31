package backend

import (
	"bytes"
	"encoding/json"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"testing"
)

func TestSemanticDocumentRoundTripDrivesEveryTarget(t *testing.T) {
	source := "f <- function(a, b = 2) { x <- 0; for (i in c(1,2,3)) { if (i > 1) { x <- x + i } }; return(a + b + x) }\nprint(f(3))\n"
	p, err := ParseSemantic("python", source)
	if err != nil {
		t.Fatal(err)
	}
	first, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(first, []byte("canonical")) {
		t.Fatal("semantic document embeds canonical transport text")
	}
	q, err := ParseSemanticJSON(first)
	if err != nil {
		t.Fatal(err)
	}
	second, err := q.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("semantic document is not deterministic")
	}
	for _, lang := range Languages {
		a, err := EmitSemantic(lang.ID, p)
		if err != nil {
			t.Fatalf("%s direct: %v", lang.ID, err)
		}
		b, err := EmitSemantic(lang.ID, q)
		if err != nil || a != b {
			t.Fatalf("%s document target differs: %v", lang.ID, err)
		}
	}
}

func TestSemanticDocumentRejectsUnknownContractAndMalformedTree(t *testing.T) {
	p, err := ParseSemantic("r", "print(1)")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	doc.Types.Pointer = "native_pointer"
	if _, err := ParseSemanticDocument(doc); err == nil {
		t.Fatal("unmodeled pointer contract accepted")
	}
	doc, _ = p.Document()
	doc.Root = SemanticStatement{Kind: "assign"}
	if _, err := ParseSemanticDocument(doc); err == nil {
		t.Fatal("malformed semantic tree accepted")
	}
	doc, _ = p.Document()
	encoded, _ := json.Marshal(doc)
	encoded = append(encoded, 'x')
	if _, err := ParseSemanticJSON(encoded); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}

func TestSemanticFunctionFlowDoesNotNeedRText(t *testing.T) {
	p, err := ParseSemantic("python", "f <- function(x) { while (x < 3) { x <- x + 1 }; return(x) }\nprint(f(0))")
	if err != nil {
		t.Fatal(err)
	}
	flows, err := AnalyzeSemanticFunctionFlows(p)
	if err != nil || len(flows) != 1 || !flows[0].StateMachine {
		t.Fatalf("semantic flow=%v err=%v", flows, err)
	}
}

func TestSemanticDocumentCarriesVerifiedRelationsAndRejectsChangedGraph(t *testing.T) {
	p, err := ParseSemantic("python", "f <- function(x) { y <- x + 1; if (y > 2) { return(y) }; return(2) }\nprint(f(3))")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Evidence.Nodes) == 0 || doc.Evidence.Nodes[0].ID != 0 || doc.Evidence.Control.NonZeros() == 0 || doc.Evidence.Binding.NonZeros() == 0 || doc.Evidence.Data.NonZeros() == 0 {
		t.Fatal("semantic document is missing explicit graph relations")
	}
	if _, err := ParseSemanticDocument(doc); err != nil {
		t.Fatal(err)
	}
	doc.Evidence.Control = matrixir.NewSparseMatrix(doc.Evidence.Control.Rows, doc.Evidence.Control.Cols)
	if _, err := ParseSemanticDocument(doc); err == nil {
		t.Fatal("changed control graph accepted")
	}
	doc, _ = p.Document()
	doc.Evidence.TypeAxes[0] = "forged"
	if _, err := ParseSemanticDocument(doc); err == nil {
		t.Fatal("changed type axis accepted")
	}
}

func TestSemanticDocumentCarriesNodeLocalSemanticsAndSpecialValues(t *testing.T) {
	p, err := ParseSemantic("r", "f <- function(x = NA) { y <- x[1]; print(NULL); print(NaN); return(y + 1) }")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	var walkExpr func(*SemanticExpression)
	var walkStmt func(*SemanticStatement)
	walkStmt = func(s *SemanticStatement) {
		if s == nil {
			return
		}
		if s.ID < 0 || s.Type.Kind == "" || s.TypeOrigin == "" {
			t.Fatalf("statement lacks stable semantic metadata: %#v", s)
		}
		walkExpr(s.Expression)
		walkExpr(s.Condition)
		walkExpr(s.Sequence)
		walkStmt(s.Then)
		walkStmt(s.Else)
		walkStmt(s.Body)
		for i := range s.Statements {
			walkStmt(&s.Statements[i])
		}
	}
	walkExpr = func(e *SemanticExpression) {
		if e == nil {
			return
		}
		if e.Type.Kind == "" || e.TypeOrigin == "" {
			t.Fatalf("expression lacks type metadata: %#v", e)
		}
		if e.Kind == "literal" {
			seen[e.LiteralKind] = true
		}
		if e.Kind == "index" && (e.Semantics.IndexBase != 1 || e.Semantics.Operation != "index") {
			t.Fatalf("index semantics missing: %#v", e.Semantics)
		}
		if e.Kind == "binary" && e.Semantics.Operation != "add" {
			t.Fatalf("binary semantics missing: %#v", e.Semantics)
		}
		walkExpr(e.Left)
		walkExpr(e.Right)
		walkExpr(e.Value)
		for i := range e.Arguments {
			walkExpr(e.Arguments[i].Value)
		}
		if e.Function != nil {
			for i := range e.Function.Parameters {
				if e.Function.Parameters[i].ID < 0 {
					t.Fatal("parameter id missing")
				}
				walkExpr(e.Function.Parameters[i].Default)
			}
			walkStmt(&e.Function.Body)
		}
	}
	walkStmt(&doc.Root)
	if !seen["na"] || !seen["null"] || !seen["nan"] {
		t.Fatalf("special values not separated: %#v", seen)
	}
	if _, err := ParseSemanticDocument(doc); err != nil {
		t.Fatal(err)
	}
}

func TestSemanticDialectRoundTripAndCapabilityRejection(t *testing.T) {
	p, err := ParseSemantic("r", "print(1)")
	if err != nil {
		t.Fatal(err)
	}
	p.Dialects = []SemanticDialect{{
		Name: "gpu", Capabilities: []string{"gpu.compute"},
		Operations: []SemanticDialectOperation{{ID: "kernel-1", Kind: "compute", Attributes: map[string]any{"workgroup_size": []int{64, 1, 1}}}},
	}}
	data, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseSemanticJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Dialects) != 1 || q.Dialects[0].Operations[0].Kind != "compute" {
		t.Fatalf("dialect transport failed: %#v", q.Dialects)
	}
	if _, err := EmitSemantic("go", q); err == nil {
		t.Fatal("GPU dialect was silently emitted by CPU backend")
	}
	q.Dialects[0].Operations[0].ID = ""
	if _, err := q.Document(); err != nil {
		t.Fatal(err)
	}
	data, _ = q.MarshalSemanticJSON()
	if _, err := ParseSemanticJSON(data); err == nil {
		t.Fatal("malformed dialect operation accepted")
	}
}

func TestSemanticTypeCanRepresentNativeAndCompositeForms(t *testing.T) {
	signed := false
	typ := SemanticType{Kind: "matrix", Element: &SemanticType{Kind: "integer", Bits: 32, Signed: &signed}, Rows: 16, Columns: 16, Ownership: "owned", TypeOrigin: "explicit"}
	if typ.Element.Kind != "integer" || typ.Element.Bits != 32 || *typ.Element.Signed || typ.Rows != 16 || typ.Columns != 16 {
		t.Fatalf("structured type lost: %#v", typ)
	}
}

func TestDetailedEffectMatrixAndPuritySummary(t *testing.T) {
	p, err := ParseSemantic("r", "print(Sys.time()); runif(1); stop(\"x\")")
	if err != nil {
		t.Fatal(err)
	}
	s := SummarizeEffects(p)
	for _, effect := range []string{"io.write", "time", "random", "exception.throw"} {
		if s.Counts[effect] == 0 {
			t.Fatalf("missing effect %s: %#v", effect, s)
		}
	}
	if s.ConservativePure {
		t.Fatal("observable effects classified pure")
	}
	p, err = ParseSemantic("r", "1 + 2")
	if err != nil {
		t.Fatal(err)
	}
	if s = SummarizeEffects(p); !s.ConservativePure || s.Unknown {
		t.Fatalf("arithmetic not proven pure: %#v", s)
	}
}

func TestSemanticJSONRoundTripHasEquivalentObservation(t *testing.T) {
	p, err := ParseSemantic("r", "x <- 1; for (i in 1:4) { x <- x + i }; print(x)")
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseSemanticJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	a, b := ObserveSemantic(p), ObserveSemantic(q)
	if !EquivalentSemanticObservations(a, b) {
		t.Fatalf("semantic observations differ:\nA=%#v\nB=%#v", a, b)
	}
}
