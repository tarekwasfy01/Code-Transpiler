package backend

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSignatureBindingMatrix(t *testing.T) {
	p := []SignatureParameter{{"x", "positional_only", false}, {"y", "positional_or_keyword", true}, {"args", "variadic_positional", false}, {"required", "keyword_only", false}, {"optional", "keyword_only", true}, {"kwargs", "variadic_keyword", false}}
	a := []SignatureArgument{{Name: ""}, {Name: "required"}, {Name: "x"}, {Name: "extra"}}
	b, err := BindSignature(p, a)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b.ArgumentCounts, []int{1, 0, 0, 1, 0, 2}) || !reflect.DeepEqual(b.UseDefaults, []int{0, 1, 0, 0, 1, 0}) {
		t.Fatalf("wrong binding vectors: %+v", b)
	}
	if b.ParameterArguments.At(0, 0) != 1 || b.ParameterArguments.At(3, 1) != 1 || b.ParameterArguments.At(5, 2) != 1 {
		t.Fatal("argument columns reordered")
	}
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var restored SignatureBinding
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(b, &restored) {
		t.Fatal("binding roundtrip changed")
	}
}

func TestSignatureBindingRejectsWholeCallConflicts(t *testing.T) {
	p := []SignatureParameter{{"x", "positional_or_keyword", false}}
	for _, args := range [][]SignatureArgument{nil, {{Name: ""}, {Name: "x"}}, {{Name: "other"}}, {{Spread: true}}, {{Name: "x"}, {Name: "x"}}} {
		if _, err := BindSignature(p, args); err == nil {
			t.Fatalf("invalid call accepted: %+v", args)
		}
	}
	for _, params := range [][]SignatureParameter{
		{{"x", "bad", false}}, {{"x", "positional_only", true}, {"y", "positional_or_keyword", false}},
		{{"x", "keyword_only", true}, {"x", "variadic_keyword", false}},
		{{"x", "variadic_keyword", false}, {"y", "positional_only", true}},
	} {
		if _, err := BindSignature(params, nil); err == nil {
			t.Fatalf("invalid signature accepted: %+v", params)
		}
	}
}

func TestSignatureBindingVariadicCardinality(t *testing.T) {
	p := []SignatureParameter{{"args", "variadic_positional", false}, {"kw", "variadic_keyword", false}}
	for n := 0; n < 32; n++ {
		args := make([]SignatureArgument, n)
		b, err := BindSignature(p, args)
		if err != nil {
			t.Fatal(err)
		}
		if b.ArgumentCounts[0] != n || b.ArgumentCounts[1] != 0 || b.ParameterArguments.NonZeros() != n {
			t.Fatal("variadic arguments lost")
		}
	}
}
