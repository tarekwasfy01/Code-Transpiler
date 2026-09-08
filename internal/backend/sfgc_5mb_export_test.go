// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"os"
	"strings"
	"testing"
)

func TestExportFiveMBGo(t *testing.T) {
	const target = 5 * 1024 * 1024
	var b strings.Builder
	b.WriteString("package main\nfunc main(){ println(1) }\n")
	for b.Len() < target {
		b.WriteString("// semantic export stress padding: this source remains valid Go and is ignored by the frontend\n")
	}
	src := b.String()
	if len(src) > target {
		src = src[:target]
	}
	p, err := LowerSource("go", "five_mb.go", src)
	if err != nil {
		t.Fatal(err)
	}
	d := "../../outputs/five-mb-export"
	if err = os.MkdirAll(d, 0755); err != nil {
		t.Fatal(err)
	}
	jsonData, _ := p.MarshalSemanticJSON()
	sp, _ := p.MarshalSemanticSP()
	se, _ := p.MarshalSemanticSESemanticOnly()
	seWithSource, _ := p.MarshalSemanticSEWithSource()
	seSemanticOnly := se
	if vr := VerifySemanticSE(seSemanticOnly, 32*1024*1024); !vr.Valid {
		t.Fatalf("semantic-only verify: %v", vr.Errors)
	}
	spz, _ := p.MarshalSemanticSPZ()
	os.WriteFile(d+"/five_mb.go", []byte(src), 0644)
	os.WriteFile(d+"/five_mb.json", jsonData, 0644)
	os.WriteFile(d+"/five_mb.sp", sp, 0644)
	os.WriteFile(d+"/five_mb.se", se, 0644)
	os.WriteFile(d+"/five_mb.with-source.se", seWithSource, 0644)
	os.WriteFile(d+"/five_mb.semantic-only.se", seSemanticOnly, 0644)
	os.WriteFile(d+"/five_mb.spz", spz, 0644)
	t.Logf("SOURCE_BYTES=%d JSON_BYTES=%d SP_BYTES=%d SE_BYTES=%d SE_SEMANTIC_ONLY_BYTES=%d SPZ_BYTES=%d", len(src), len(jsonData), len(sp), len(se), len(seSemanticOnly), len(spz))
}
