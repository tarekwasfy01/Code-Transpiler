// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"bytes"
	"testing"
)

func TestSFPCSmokeEndToEnd(t *testing.T) {
	fold := FoldConstantExpression(&SemanticExpression{Kind: "binary", Operator: "+", Left: &SemanticExpression{Kind: "literal", LiteralKind: "integer", Text: "7"}, Right: &SemanticExpression{Kind: "literal", LiteralKind: "integer", Text: "5"}})
	if !fold.Applied || fold.Expression.Text != "12" {
		t.Fatalf("constant-fold witness failed: %+v", fold)
	}
	source := "package main\nfunc main(){ x:=1; println(x) }\n"
	p, err := LowerSource("go", "smoke.go", source)
	if err != nil {
		t.Fatal(err)
	}
	se, err := p.MarshalSemanticSE()
	if err != nil {
		t.Fatal(err)
	}
	vr := VerifySemanticSE(se, 16*1024*1024)
	if !vr.Valid {
		t.Fatalf("SE verification failed: %v", vr.Errors)
	}
	q, vr2 := OpenSFPCQuery(se, 16*1024*1024)
	if !vr2.Valid || q == nil {
		t.Fatalf("query open failed: %v", vr2.Errors)
	}
	if _, ok := q.GetNode(0); !ok {
		t.Fatal("root node query failed")
	}
	plan := PlanSFPCLowering(SFPCSemanticSource{Query: q}, 0)
	if plan.Status != NeedsSelectiveExpansion || plan.Level != SFPCLevelInstance {
		t.Fatalf("unexpected lowering plan: %+v", plan)
	}
	if _, err = q.MerkleRoots(); err != nil {
		t.Fatal(err)
	}
	spz, err := p.MarshalSemanticSPZ()
	if err != nil {
		t.Fatal(err)
	}
	round, err := ParseSemanticSPZ(spz)
	if err != nil {
		t.Fatal(err)
	}
	a, _ := p.MarshalSemanticJSON()
	b, _ := round.MarshalSemanticJSON()
	if !bytes.Equal(a, b) {
		t.Fatal("SPZ roundtrip changed canonical semantic JSON")
	}
	se2, err := round.MarshalSemanticSE()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(se, se2) {
		t.Fatal("SE canonical fixed point changed bytes")
	}
	// Provenance is a separate plane: adding compressed source must not alter
	// the semantic Merkle root used by closure and cache identity.
	withSource, err := p.MarshalSemanticSEWithSource()
	if err != nil {
		t.Fatal(err)
	}
	qSource, vrSource := OpenSFPCQuery(withSource, 16*1024*1024)
	if !vrSource.Valid || qSource == nil {
		t.Fatalf("source-preserving SE verification failed: %v", vrSource.Errors)
	}
	roots, err := q.MerkleRoots()
	if err != nil {
		t.Fatal(err)
	}
	rootsSource, err := qSource.MerkleRoots()
	if err != nil {
		t.Fatal(err)
	}
	if roots.ProgramRoot != rootsSource.ProgramRoot {
		t.Fatalf("semantic root changed with provenance: %s != %s", roots.ProgramRoot, rootsSource.ProgramRoot)
	}
	t.Logf("SE_BYTES=%d SPZ_BYTES=%d NODES=%d RELATIONS=%d ROOT=%s", len(se), len(spz), vr.Nodes, vr.Relations, vr.Root)
}

func TestSFPCVerifierRejectsMalformedInputs(t *testing.T) {
	for _, in := range [][]byte{[]byte("sp 1\nprogram {}"), []byte{0xff, 0xfe, 0xfd}, []byte("se 1\nprogram {\nschema=1\n}")} {
		if VerifySemanticSE(in, 1<<20).Valid {
			t.Fatalf("malformed input accepted: %q", in)
		}
	}
	if VerifySemanticSE([]byte("se 1\nprogram {}"), 2).Valid {
		t.Fatal("bound bypass accepted")
	}
}

func TestSEReadableCompactEquivalent(t *testing.T) {
	p, err := LowerSource("go", "readable.go", "package main\nfunc main(){ println(1) }\n")
	if err != nil {
		t.Fatal(err)
	}
	compact, err := p.MarshalSemanticSECompact()
	if err != nil {
		t.Fatal(err)
	}
	readable, err := p.MarshalSemanticSEReadable()
	if err != nil {
		t.Fatal(err)
	}
	a, err := ParseSemanticSE(compact)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ParseSemanticSE(readable)
	if err != nil {
		t.Fatal(err)
	}
	aj, _ := a.MarshalSemanticJSON()
	bj, _ := b.MarshalSemanticJSON()
	if !bytes.Equal(aj, bj) {
		t.Logf("compact=%s", aj)
		t.Logf("readable=%s", bj)
		t.Fatal("readable and compact SE differ semantically")
	}
}
