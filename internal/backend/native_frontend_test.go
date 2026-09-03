package backend

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestNativeGoTypeBindingMatrix(t *testing.T) {
	source := `package demo
func f(x uint64) uint64 {
 var p *uint64 = &x
 _ = p
 { x := uint32(3); _ = x }
 return x + 9007199254740993
}`
	a, err := (GoNativeFrontend{}).Analyze("input.go", source)
	if err != nil {
		t.Fatal(err)
	}
	bindings := map[int]bool{}
	foundLiteral, foundPointer, foundAdd := false, false, false
	for _, event := range a.Events {
		if event.Source.StartLine < 1 || event.Source.EndOffset < event.Source.StartOffset {
			t.Fatalf("invalid source span: %+v", event)
		}
		if event.Name == "x" && event.Binding != nil {
			bindings[*event.Binding] = true
		}
		if event.Literal == "9007199254740993" {
			foundLiteral = true
		}
		if event.Type.Kind == "pointer" && event.Type.Element.Kind == "integer" && event.Type.Element.Bits == 64 {
			foundPointer = true
		}
		if event.Operation == "integer.add" {
			foundAdd = true
		}
	}
	if len(bindings) != 2 || !foundLiteral || !foundPointer || !foundAdd {
		t.Fatalf("lost facts: bindings=%v literal=%v pointer=%v add=%v", bindings, foundLiteral, foundPointer, foundAdd)
	}
	for _, x := range a.Events {
		for _, y := range a.Events {
			if x.Name == "x" && y.Name == "x" && x.Binding != nil && y.Binding != nil {
				want := 0.0
				if *x.Binding == *y.Binding {
					want = 1
				}
				if got := a.SameBinding.At(x.ID, y.ID); got != want {
					t.Fatalf("binding matrix confuses shadowing: %d %d", x.ID, y.ID)
				}
			}
		}
	}
	first, _ := json.Marshal(a)
	b, err := (GoNativeFrontend{}).Analyze("input.go", source)
	if err != nil {
		t.Fatal(err)
	}
	second, _ := json.Marshal(b)
	if !bytes.Equal(first, second) {
		t.Fatal("native extraction not deterministic")
	}
	if a.Executable {
		t.Fatal("analysis claims execution support")
	}
	if _, err := ParseSemanticJSON(first); err == nil {
		t.Fatal("analysis accepted as executable document")
	}
}

func TestNativeGoRejectsInvalidAndUnresolvedImports(t *testing.T) {
	for _, source := range []string{`package a; var x uint8 = 256`, `package a; import "fmt"; func f(){fmt.Println(1)}`, `package a; func f(){missing()}`} {
		if _, err := (GoNativeFrontend{}).Analyze("bad.go", source); err == nil {
			t.Fatalf("invalid or unresolved source accepted: %s", source)
		}
	}
}

func TestNativeGoCollectionsAndRecursiveTypes(t *testing.T) {
	a, err := (GoNativeFrontend{}).Analyze("types.go", `package a
type Node struct { Next *Node }
var m map[string][]uint32
var a [4]float32
var ch chan int
var n Node
`)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(a)
	for _, kind := range []string{"map", "slice", "array", "floating", "channel", "named", "pointer"} {
		if !strings.Contains(string(data), `"kind":"`+kind+`"`) {
			t.Fatalf("missing type %s", kind)
		}
	}
}
