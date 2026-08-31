package main

import (
	"reflect"
	"testing"
)

func TestReorderValueFlagsAfterInput(t *testing.T) {
	got := reorderValueFlags([]string{"input.py", "-o", "out.json", "-source", "python"}, map[string]bool{"-o": true, "-source": true})
	want := []string{"-o", "out.json", "-source", "python", "input.py"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
