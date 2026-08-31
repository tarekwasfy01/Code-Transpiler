package matrixir

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

func TestGraphBulkPreservesIDsVectorsAndExistingRelations(t *testing.T) {
	bulk, _ := NewGraph("r")
	single, _ := NewGraph("r")
	for _, g := range []*Graph{bulk, single} {
		g.AddNode(Basis(SemanticDimensions, SemProgram), "program", "", 0)
		g.AddNode(Basis(SemanticDimensions, SemAssign), "x <- 1", "x", 4)
		for relation := Relation(0); relation < RelationCount; relation++ {
			g.Edges[relation].Set(0, 1, float64(relation)+0.5)
			g.Edges[relation].Set(1, 0, -float64(relation)-1)
		}
	}
	payloads := []Node{
		{ID: 900, Vector: Basis(SemanticDimensions, SemNumber), Text: "7", SourceAt: 13},
		{ID: -8, Vector: Basis(SemanticDimensions, SemIdentifier), Text: "x", Symbol: "x", SourceAt: 22},
	}
	ids := bulk.AddNodes(payloads)
	for _, node := range payloads {
		single.AddNode(node.Vector, node.Text, node.Symbol, node.SourceAt)
	}
	if !reflect.DeepEqual(ids, []int{2, 3}) || !reflect.DeepEqual(bulk, single) {
		t.Fatal("batch and incremental graphs differ or incoming IDs were retained")
	}
	for relation := Relation(0); relation < RelationCount; relation++ {
		if err := bulk.Connect(relation, ids[0], ids[1]); err != nil {
			t.Fatal(err)
		}
		if bulk.Edges[relation].At(0, 1) != float64(relation)+0.5 || bulk.Edges[relation].At(1, 0) != -float64(relation)-1 {
			t.Fatal("existing weighted relation lost")
		}
	}
	payloads[0].Vector[SemNumber] = 99
	payloads[1].Text = "changed"
	if bulk.Nodes[2].Vector[SemNumber] != 1 || bulk.Nodes[3].Text != "x" {
		t.Fatal("graph aliases input payload storage")
	}
	bulk.Nodes[3].Vector[SemIdentifier] = 42
	if payloads[1].Vector[SemIdentifier] != 1 {
		t.Fatal("input semantic vector aliases graph")
	}
	before := reflect.ValueOf(bulk.Edges[Syntax].entries).Pointer()
	if got := bulk.AddNodes(nil); len(got) != 0 || before != reflect.ValueOf(bulk.Edges[Syntax].entries).Pointer() {
		t.Fatal("empty batch changed storage")
	}
}

func TestGraphBulkRejectsInvalidBatchWithoutMutation(t *testing.T) {
	g, _ := NewGraph("r")
	g.AddNode(Basis(SemanticDimensions, SemProgram), "", "", 0)
	before := reflect.ValueOf(g.Edges[Syntax].entries).Pointer()
	defer func() {
		if recover() == nil {
			t.Fatal("invalid vector accepted")
		}
		if len(g.Nodes) != 1 || g.Edges[Syntax].Rows != 1 || before != reflect.ValueOf(g.Edges[Syntax].entries).Pointer() {
			t.Fatal("invalid batch partially mutated graph")
		}
	}()
	g.AddNodes([]Node{{Vector: Basis(SemanticDimensions, SemNumber)}, {Vector: Vector{1}}})
}

func TestLexicalBulkRelationsAndVectorIsolation(t *testing.T) {
	code := "x <- 1\nx <- x + 2\nprint(x)\n"
	g, tokens, err := BuildLexicalGraph("r", code)
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != len(tokens)+1 || g.Nodes[0].Vector[SemProgram] != 1 {
		t.Fatal("lexical node layout changed")
	}
	var bindings, effects, calls int
	for i, token := range tokens {
		node := i + 1
		if g.Nodes[node].ID != node || g.Nodes[node].Text != token.Text || g.Nodes[node].SourceAt != token.Start || g.Edges[Syntax].At(0, node) != 1 {
			t.Fatalf("token node %d lost identity or syntax edge", i)
		}
		if i > 0 && g.Edges[Control].At(node-1, node) != 1 {
			t.Fatal("control chain changed")
		}
		if token.Text == "<-" {
			bindings++
			if g.Edges[Binding].At(node, node-1) != 1 || g.Edges[Data].At(node+1, node-1) != 1 {
				t.Fatal("binding/data projection changed")
			}
			if bindings == 2 && g.Nodes[node].Vector[SemReassignment] != 1 {
				t.Fatal("reassignment evidence lost")
			}
		}
		if token.Text == "print" {
			effects++
			if g.Edges[Effect].At(node, node+1) != 1 {
				t.Fatal("print effect edge lost")
			}
		}
		if token.Text == "(" {
			calls++
			if g.Nodes[node].Vector[SemCall] != 1 || g.Edges[Syntax].At(node-1, node) != 1 {
				t.Fatal("call projection lost")
			}
		}
	}
	if bindings != 2 || effects != 1 || calls != 1 {
		t.Fatal("fixture lost its expected relation families")
	}
	tokens[0].Semantic[SemIdentifier] = 99
	if g.Nodes[1].Vector[SemIdentifier] != 1 {
		t.Fatal("lexical graph shares token semantic vectors")
	}
}

func BenchmarkGraphBulkVsIncremental(b *testing.B) {
	for _, count := range []int{64, 256, 512} {
		payloads := make([]Node, count)
		for i := range payloads {
			payloads[i] = Node{Vector: Basis(SemanticDimensions, SemIdentifier), Text: "x", SourceAt: i}
		}
		for _, bulk := range []bool{false, true} {
			b.Run(fmt.Sprintf("nodes_%d/bulk_%t", count, bulk), func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					g, _ := NewGraph("r")
					if bulk {
						g.AddNodes(payloads)
					} else {
						for _, node := range payloads {
							g.AddNode(node.Vector, node.Text, node.Symbol, node.SourceAt)
						}
					}
				}
			})
		}
	}
}

func BenchmarkLexicalGraphBulk(b *testing.B) {
	code := strings.Repeat("x <- x + 1\n", 64)
	b.ReportAllocs()
	for b.Loop() {
		if _, _, err := BuildLexicalGraph("r", code); err != nil {
			b.Fatal(err)
		}
	}
}
