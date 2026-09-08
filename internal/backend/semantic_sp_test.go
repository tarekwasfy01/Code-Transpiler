// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"bytes"
	"testing"
)

func TestSemanticSPLosslessRoundTrip(t *testing.T) {
	p, err := LowerSource("go", "sp_test.go", "package main\nfunc main(){ println(1) }\n")
	if err != nil {
		t.Fatal(err)
	}
	jsonBefore, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	sp, err := p.MarshalSemanticSP()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseSemanticSP(sp)
	if err != nil {
		t.Fatal(err)
	}
	jsonAfter, err := q.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(jsonBefore, jsonAfter) {
		t.Fatal("SP roundtrip changed canonical Semantic JSON")
	}
	spAgain, err := q.MarshalSemanticSP()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sp, spAgain) {
		t.Fatal("SP formatting is not deterministic")
	}
}

func TestSemanticSPRejectsMalformedEnvelope(t *testing.T) {
	cases := [][]byte{
		[]byte("sp 2\nprogram {\n schema = 1\n semantic_json_base64 = \"x\"\n}\n"),
		[]byte("sp 1\nprogram {\n schema = 1\n schema = 1\n semantic_json_base64 = \"x\"\n}\n"),
		[]byte("sp 1\nprogram {\n schema = 1\n semantic_json_base64 = \"x\"\n}\ntrailing\n"),
	}
	for i, data := range cases {
		if _, err := ParseSemanticSP(data); err == nil {
			t.Fatalf("case %d unexpectedly accepted", i)
		}
	}
}

func TestSemanticSPZRoundTrip(t *testing.T) {
	p, err := LowerSource("go", "spz_test.go", "package main\nfunc main(){ println(2) }\n")
	if err != nil {
		t.Fatal(err)
	}
	wire, err := p.MarshalSemanticSPZ()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseSemanticSPZ(wire)
	if err != nil {
		t.Fatal(err)
	}
	a, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	b, err := q.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("SPZ roundtrip changed canonical Semantic JSON")
	}
}

func TestSemanticSPZAcceptsSEPayload(t *testing.T) {
	p, err := LowerSource("go", "se_payload.go", "package main\nfunc main(){ println(3) }\n")
	if err != nil {
		t.Fatal(err)
	}
	readable, err := p.MarshalSemanticSE()
	if err != nil {
		t.Fatal(err)
	}
	compressed := encodeSPZ(readable)
	q, err := ParseSemanticSPZ(compressed)
	if err != nil {
		t.Fatal(err)
	}
	a, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	b, err := q.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("SPZ with SE payload changed canonical Semantic JSON")
	}
}

func TestSemanticSEDirectKeysRoundTrip(t *testing.T) {
	p, err := LowerSource("go", "se_direct.go", "package main\nfunc main(){}\n")
	if err != nil {
		t.Fatal(err)
	}
	se, err := p.MarshalSemanticSE()
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(se, []byte("field.")) {
		t.Fatal("SE still contains field namespace")
	}
	q, err := ParseSemanticSE(se)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := p.MarshalSemanticJSON()
	b, _ := q.MarshalSemanticJSON()
	if !bytes.Equal(a, b) {
		t.Fatal("SE direct-key roundtrip changed UAST")
	}
}

func TestSemanticSPJSONEquivalenceAndDeterminism(t *testing.T) {
	p, err := LowerSource("go", "equivalence.go", "package main\nfunc main(){ x := 1; println(x) }\n")
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	sp, err := p.MarshalSemanticSP()
	if err != nil {
		t.Fatal(err)
	}
	fromSP, err := ParseSemanticSP(sp)
	if err != nil {
		t.Fatal(err)
	}
	jsonFromSP, err := fromSP.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, jsonFromSP) {
		t.Fatal("Go -> SP -> Semantic is not lossless")
	}
	fromJSON, err := ParseSemanticJSON(canonical)
	if err != nil {
		t.Fatal(err)
	}
	spFromJSON, err := fromJSON.MarshalSemanticSP()
	if err != nil {
		t.Fatal(err)
	}
	fromSPJSON, err := ParseSemanticSP(spFromJSON)
	if err != nil {
		t.Fatal(err)
	}
	jsonFromSPJSON, err := fromSPJSON.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(canonical, jsonFromSPJSON) {
		t.Fatal("JSON -> SP -> Semantic is not equivalent")
	}
	spAgain, err := fromSP.MarshalSemanticSP()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(sp, spAgain) {
		t.Fatal("SP pretty printer is not deterministic")
	}
}
