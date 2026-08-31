package backend

import "testing"

func TestRegistryNormalizesAliasesAndDeclaresCoreBoundary(t *testing.T) {
	if NormalizeLanguage(" C++ ") != "cpp" || NormalizeLanguage("py") != "python" || !HasFrontend("c#") || !HasBackend("rust") {
		t.Fatal("registry alias resolution failed")
	}
	for _, spec := range Frontends() {
		if !SupportsCapability(spec.Capabilities, "core") {
			t.Fatalf("frontend %s lacks core capability", spec.ID)
		}
	}
}

func TestCapabilityContractsAreExplicit(t *testing.T) {
	if got := BackendCapability("core", "go"); got.Status != CapabilityLowering {
		t.Fatalf("core capability=%#v", got)
	}
	if got := BackendCapability("gpu.compute", "go"); got.Status != CapabilityUnsupported {
		t.Fatalf("GPU capability=%#v", got)
	}
}

type walkRecorder struct{ order []int }

func (w *walkRecorder) EnterStatement(s *SemanticStatement) error {
	w.order = append(w.order, s.ID)
	return nil
}
func (w *walkRecorder) LeaveStatement(*SemanticStatement) error { return nil }
func (w *walkRecorder) EnterExpression(e *SemanticExpression) error {
	w.order = append(w.order, e.ID)
	return nil
}
func (w *walkRecorder) LeaveExpression(*SemanticExpression) error { return nil }
func (w *walkRecorder) EnterParameter(p *SemanticParameter) error {
	w.order = append(w.order, p.ID)
	return nil
}

func TestSemanticWalkVisitsStableIDsOnce(t *testing.T) {
	p, err := ParseSemantic("r", "f <- function(x) { return(x + 1) }; print(f(2))")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := p.Document()
	if err != nil {
		t.Fatal(err)
	}
	w := &walkRecorder{}
	if err := WalkSemanticDocument(&doc, w); err != nil {
		t.Fatal(err)
	}
	seen := map[int]bool{}
	for _, id := range w.order {
		if seen[id] {
			t.Fatalf("node %d visited twice", id)
		}
		seen[id] = true
	}
	if len(seen) != len(doc.Evidence.Nodes) {
		t.Fatalf("walked %d nodes, evidence has %d", len(seen), len(doc.Evidence.Nodes))
	}
}
