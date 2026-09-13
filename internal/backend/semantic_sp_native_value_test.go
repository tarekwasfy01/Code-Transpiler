// Copyright (c) 2026 Tarek Wasfy
package backend

import "testing"

func TestNativeSEObjectQuotedKeyRoundTrip(t *testing.T) {
	v, err := parseNativeSEValue(`object { "iteration.index_binding" = "i" }`)
	if err != nil {
		t.Fatal(err)
	}
	m, ok := v.(map[string]spValue)
	if !ok || m["iteration.index_binding"] != "i" {
		t.Fatalf("quoted object key was not decoded: %#v", v)
	}
	if _, escaped := m[`"iteration.index_binding"`]; escaped {
		t.Fatalf("quoted object key retained transport quotes: %#v", m)
	}
}
