package backend

import (
	"encoding/json"
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

type directFieldTypeVisitor struct{ done bool }

func (*directFieldTypeVisitor) EnterStatement(*SemanticStatement) error   { return nil }
func (*directFieldTypeVisitor) LeaveStatement(*SemanticStatement) error   { return nil }
func (*directFieldTypeVisitor) LeaveExpression(*SemanticExpression) error { return nil }
func (v *directFieldTypeVisitor) EnterExpression(e *SemanticExpression) error {
	if !v.done && e.Kind == "literal" {
		e.Type = SemanticType{Kind: "opaque", Ownership: "owned", Lifetime: "scope", TypeOrigin: "explicit"}
		e.TypeOrigin = "explicit"
		v.done = true
	}
	return nil
}

func cloneUniversalForDirectTest(t *testing.T, u *UniversalASTDocument) *UniversalASTDocument {
	t.Helper()
	data, err := json.Marshal(u)
	if err != nil {
		t.Fatal(err)
	}
	var out UniversalASTDocument
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return &out
}

func TestDirectUASTCrosswalkFieldMatrix(t *testing.T) {
	doc, err := universalExecutableRoundtripProgram(t).Document()
	if err != nil {
		t.Fatal(err)
	}
	visitor := &directFieldTypeVisitor{}
	if err := WalkSemanticDocument(&doc, visitor); err != nil {
		t.Fatal(err)
	}
	if !visitor.done {
		t.Fatal("fixture has no literal for lifetime/ownership projection")
	}
	u, err := ProjectSemanticDocumentToUniversal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := newUASTExecutionGraph(u); err != nil {
		t.Fatal(err)
	}
	newChannels := []string{"operands", "condition", "branches", "body", "members", "arguments", "parameters", "value", "callee", "ownership", "lifetime", "evaluation_order", "dispatch", "exception_model", "candidates"}
	locations := map[string]int{}
	for i := range u.Nodes {
		for field := range u.Nodes[i].Fields {
			if _, ok := locations[field]; !ok {
				locations[field] = i
			}
		}
	}
	for _, field := range newChannels {
		field := field
		t.Run(field, func(t *testing.T) {
			index, ok := locations[field]
			if !ok {
				t.Fatalf("direct field channel %q was not projected", field)
			}
			forged := cloneUniversalForDirectTest(t, u)
			forged.Nodes[index].Fields[field] = json.RawMessage("null")
			if _, err := newUASTExecutionGraph(forged); err == nil || !strings.Contains(err.Error(), "crosswalk matrix projection") {
				t.Fatalf("inconsistent direct field %q was accepted: %v", field, err)
			}
		})
	}
}

func TestDirectUASTRelationProjectionAndConsumptionMatrix(t *testing.T) {
	u := func() *UniversalASTDocument {
		doc, err := universalExecutableRoundtripProgram(t).Document()
		if err != nil {
			t.Fatal(err)
		}
		return doc.UniversalAST
	}()
	present := map[string]int{}
	for _, relation := range u.Relations {
		present[relation.Kind]++
	}
	for relation := range projectedUASTRelations {
		if present[relation] == 0 {
			t.Errorf("projected relation %q has no proved fixture instance", relation)
		}
	}
	for relation := range directlyConsumedUASTRelations {
		if !projectedUASTRelations[relation] {
			t.Errorf("direct relation %q is not projected", relation)
		}
	}
	forged := cloneUniversalForDirectTest(t, u)
	for _, relation := range forged.Relations {
		if relation.Kind != "syntax.child" {
			forged.Relations = append(forged.Relations, relation)
			break
		}
	}
	if _, err := newUASTExecutionGraph(forged); err == nil || !strings.Contains(err.Error(), "matrix/evidence projection") {
		t.Fatalf("unproved duplicate relation was accepted: %v", err)
	}
}

func TestUASTTargetCapabilityMatrixOneHotAndApplied(t *testing.T) {
	matrix, err := UniversalTargetCapabilityMatrix()
	if err != nil {
		t.Fatal(err)
	}
	if err := validateUASTCapabilityMatrix(matrix); err != nil {
		t.Fatal(err)
	}
	if len(matrix.Structures.Rows) != 109 || len(matrix.Facets.Rows) != 334 || len(matrix.Relations.Rows) != 55 || len(matrix.Fields.Rows) != 57 || len(matrix.Structures.Targets) != len(Backends()) {
		t.Fatalf("capability matrix shape changed: structures=%d facets=%d relations=%d fields=%d targets=%d", len(matrix.Structures.Rows), len(matrix.Facets.Rows), len(matrix.Relations.Rows), len(matrix.Fields.Rows), len(matrix.Structures.Targets))
	}
	for category, total := range map[string]int{"structure": len(matrix.Structures.Rows), "facet": len(matrix.Facets.Rows), "relation": len(matrix.Relations.Rows), "field": len(matrix.Fields.Rows)} {
		for target, counts := range matrix.StatusCounts[category] {
			sum := 0
			for _, count := range counts {
				sum += count
			}
			if sum != total {
				t.Errorf("%s capability counts for %s sum to %d, want %d", category, target, sum, total)
			}
		}
	}
	p, err := ParseSemantic("r", "x <- 1\nif (x < 2) print(x)\n")
	if err != nil {
		t.Fatal(err)
	}
	u, err := canonicalUniversalAST(p)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range Backends() {
		if err := validateUASTTargetCapabilities(u, target.ID); err != nil {
			t.Errorf("target %s rejected executable facet vector: %v", target.ID, err)
		}
	}
}

func TestUASTEndToEndCapabilityMatrix(t *testing.T) {
	report, err := UniversalEndToEndCapabilityReport()
	if err != nil {
		t.Fatal(err)
	}
	for category, plane := range map[string]UASTEndToEndCapabilityPlane{
		"structure": report.Structures, "relation": report.Relations, "facet": report.Facets, "field": report.Fields,
	} {
		if got := report.Summary[category]["total"]; got != len(plane.Rows) {
			t.Fatalf("%s total=%d want=%d", category, got, len(plane.Rows))
		}
		for row := range plane.Rows {
			full := plane.Full.At(row, 0) != 0
			allStages := plane.Frontend.At(row, 0) != 0 && plane.Enrichment.At(row, 0) != 0 && plane.Evidence.At(row, 0) != 0 && plane.Normalize.At(row, 0) != 0 && plane.Backend.At(row, 0) != 0 && plane.Target.At(row, 0) != 0 && plane.Test.At(row, 0) != 0
			if full != allStages {
				t.Fatalf("%s row %q has invalid boolean full vector", category, plane.Rows[row])
			}
			if plane.BackendGap.At(row, 0) != 0 && plane.Backend.At(row, 0) != 0 {
				t.Fatalf("%s row %q is both backend gap and backend-capable", category, plane.Rows[row])
			}
		}
	}
	if got, want := report.Summary["relation"]["full"], len(projectedUASTRelations); got != want {
		t.Fatalf("relation full=%d want all %d projected relations", got, want)
	}
}

func newDeterministicRNG() *rand.Rand { return rand.New(rand.NewSource(1)) }

func TestUASTDirectExecutionReportCounts(t *testing.T) {
	report, err := UniversalDirectExecutionReport()
	if err != nil {
		t.Fatal(err)
	}
	wantTotals := map[string]int{"structure": 109, "facet": 334, "field": 57, "relation": 55}
	for category, total := range wantTotals {
		if report.Summary[category]["total"] != total {
			t.Errorf("%s total=%d want=%d", category, report.Summary[category]["total"], total)
		}
	}
	if report.Summary["facet"]["direct"] != 14 {
		t.Errorf("direct facets=%d want=14", report.Summary["facet"]["direct"])
	}
	if report.Summary["relation"]["projected"] != len(projectedUASTRelations) || report.Summary["relation"]["direct"] != len(directlyConsumedUASTRelations) {
		t.Fatal("relation report does not match executable registries")
	}
	if report.Summary["field"]["direct"] != len(directUASTFields)+2 {
		t.Fatal("field report does not include direct node records")
	}
	if reflect.DeepEqual(report.TargetCapabilities, UASTTargetCapabilityMatrix{}) {
		t.Fatal("target capability matrix missing")
	}
}
