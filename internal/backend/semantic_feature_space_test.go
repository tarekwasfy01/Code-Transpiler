package backend

import (
	"strings"
	"testing"
)

func TestEightLanguageSemanticFeatureSpaceMatrixRoundTrip(t *testing.T) {
	cases := map[string]string{"go": "go", "python": "python", "r": "r", "rust": "rust", "cpp": "clang_cpp", "kotlin": "kotlin", "java": "java", "csharp": "csharp"}
	for source, profile := range cases {
		t.Run(source, func(t *testing.T) {
			p := NewSemanticProgram(&BlockStmt{}, "eager_left_to_right")
			p.Origin.SourceLanguage = source
			if err := p.AttachSemanticFeatureProfile(source); err != nil {
				t.Fatal(err)
			}
			m := p.SemanticFeatures
			if m.ProfileLanguage != profile || len(m.Basis.Languages) != 8 || len(m.Basis.Features) != 98 || len(m.Basis.DialectFeatures) != 434 || len(m.Basis.NodeKinds) != 82 || len(m.Basis.RelationKinds) != 23 {
				t.Fatalf("unexpected semantic matrix dimensions/profile: %+v", m)
			}
			data, err := p.MarshalSemanticJSON()
			if err != nil {
				t.Fatal(err)
			}
			q, err := ParseSemanticJSON(data)
			if err != nil {
				t.Fatal(err)
			}
			if q.SemanticFeatures == nil || q.SemanticFeatures.ProfileLanguage != profile {
				t.Fatal("semantic feature profile lost in JSON roundtrip")
			}
		})
	}
}

func TestSemanticFeatureSpaceRejectsTamperedProductsAndBasis(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{}, "eager_left_to_right")
	p.Origin.SourceLanguage = "java"
	if err := p.AttachSemanticFeatureProfile("java"); err != nil {
		t.Fatal(err)
	}
	p.SemanticFeatures.NodeDemand[0]++
	if _, err := p.Document(); err == nil || !strings.Contains(err.Error(), "node demand") {
		t.Fatalf("tampered product accepted: %v", err)
	}
	if err := p.AttachSemanticFeatureProfile("java"); err != nil {
		t.Fatal(err)
	}
	p.SemanticFeatures.Basis.Features[0] = "forged"
	if _, err := p.Document(); err == nil || !strings.Contains(err.Error(), "basis differs") {
		t.Fatalf("tampered basis accepted: %v", err)
	}
}

func TestSemanticFeatureSpaceRejectsWrongLanguageSelection(t *testing.T) {
	p := NewSemanticProgram(&BlockStmt{}, "eager_left_to_right")
	p.Origin.SourceLanguage = "go"
	if err := p.AttachSemanticFeatureProfile("go"); err != nil {
		t.Fatal(err)
	}
	p.SemanticFeatures.LanguageSelection[0] = 0
	p.SemanticFeatures.LanguageSelection[1] = 1
	if _, err := p.Document(); err == nil || !strings.Contains(err.Error(), "does not select profile") {
		t.Fatalf("wrong selection accepted: %v", err)
	}
}
