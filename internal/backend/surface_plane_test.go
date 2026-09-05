package backend

import (
	"bytes"
	"testing"
)

func TestSourceSurfacePreservesSameLanguageBytes(t *testing.T) {
	source := "x = [1, 2, 3] # keep spacing\n"
	p, err := LowerMatrixLanguage("python", source)
	if err != nil {
		t.Fatal(err)
	}
	if p.UniversalAST == nil || p.UniversalAST.Surface == nil {
		t.Fatal("frontend did not attach a source surface")
	}
	got, err := p.UniversalAST.Surface.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, []byte(source)) {
		t.Fatalf("surface bytes changed: %q", got)
	}
	wire, err := p.MarshalUniversalASTJSON()
	if err != nil {
		t.Fatal(err)
	}
	q, err := ParseUniversalASTJSON(wire)
	if err != nil {
		t.Fatal(err)
	}
	out, err := EmitSemanticPreserveOriginal("python", q)
	if err != nil {
		t.Fatal(err)
	}
	if out != source {
		t.Fatalf("same-language preservation changed source: %q", out)
	}
}
