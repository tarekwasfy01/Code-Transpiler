package backend

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

//go:embed semantic_feature_space.json
var embeddedSemanticFeatureSpace []byte

type SemanticFeatureBasis struct {
	Schema                 string            `json:"schema"`
	Languages              []string          `json:"languages"`
	Features               []string          `json:"features"`
	LanguageFeature        matrixir.Matrix   `json:"language_feature"`
	DialectFeatures        []string          `json:"dialect_features"`
	LanguageDialectFeature matrixir.Matrix   `json:"language_dialect_feature"`
	DialectObserved        matrixir.Vector   `json:"dialect_observed"`
	NodeKinds              []string          `json:"node_kinds"`
	FeatureNode            matrixir.Matrix   `json:"feature_node"`
	RelationKinds          []string          `json:"relation_kinds"`
	FeatureRelation        matrixir.Matrix   `json:"feature_relation"`
	Provenance             map[string]string `json:"provenance"`
}

// SemanticFeatureModel makes the complete supplied eight-language feature
// space part of SemanticProgram. All derived vectors are matrix products and
// are validated on every interchange boundary.
type SemanticFeatureModel struct {
	BasisSHA256       string               `json:"basis_sha256"`
	Basis             SemanticFeatureBasis `json:"basis"`
	ProfileLanguage   string               `json:"profile_language"`
	LanguageSelection matrixir.Vector      `json:"language_selection"`
	FeatureDemand     matrixir.Vector      `json:"feature_demand"`
	DialectDemand     matrixir.Vector      `json:"dialect_demand"`
	NodeDemand        matrixir.Vector      `json:"node_demand"`
	RelationDemand    matrixir.Vector      `json:"relation_demand"`
}

var semanticFeatureOnce sync.Once
var semanticFeatureBase struct {
	BasisSHA256 string               `json:"basis_sha256"`
	Basis       SemanticFeatureBasis `json:"basis"`
}
var semanticFeatureErr error

func loadSemanticFeatureBasis() error {
	semanticFeatureOnce.Do(func() {
		semanticFeatureErr = json.Unmarshal(embeddedSemanticFeatureSpace, &semanticFeatureBase)
		if semanticFeatureErr == nil {
			semanticFeatureErr = validateFeatureBasis(&semanticFeatureBase.Basis)
		}
	})
	return semanticFeatureErr
}

func semanticProfileLanguage(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "go", "python", "r", "rust", "kotlin", "java":
		return strings.ToLower(strings.TrimSpace(source))
	case "c", "cpp", "c++", "clang", "clang_cpp":
		return "clang_cpp"
	case "c#", "cs", "csharp":
		return "csharp"
	default:
		return ""
	}
}

// AttachSemanticFeatureProfile selects one language row and calculates every
// program vector. It never fills individual feature cells.
func (p *SemanticProgram) AttachSemanticFeatureProfile(source string) error {
	if p == nil {
		return fmt.Errorf("missing semantic program")
	}
	if p.UniversalAST != nil {
		return fmt.Errorf("cannot mutate the legacy semantic-feature view after canonical UAST projection")
	}
	profile := semanticProfileLanguage(source)
	if profile == "" {
		return fmt.Errorf("no calculated semantic feature profile for %q", source)
	}
	if err := loadSemanticFeatureBasis(); err != nil {
		return err
	}
	encoded, _ := json.Marshal(semanticFeatureBase.Basis)
	var basis SemanticFeatureBasis
	if err := json.Unmarshal(encoded, &basis); err != nil {
		return err
	}
	selection := make(matrixir.Vector, len(basis.Languages))
	found := -1
	for i, language := range basis.Languages {
		if language == profile {
			found, selection[i] = i, 1
		}
	}
	if found < 0 {
		return fmt.Errorf("semantic feature profile %q missing from basis", profile)
	}
	feature, _ := rowVector(selection).Multiply(basis.LanguageFeature)
	dialect, _ := rowVector(selection).Multiply(basis.LanguageDialectFeature)
	nodes, _ := feature.Multiply(basis.FeatureNode)
	relations, _ := feature.Multiply(basis.FeatureRelation)
	p.SemanticFeatures = &SemanticFeatureModel{BasisSHA256: semanticFeatureBase.BasisSHA256, Basis: basis,
		ProfileLanguage: profile, LanguageSelection: selection, FeatureDemand: feature.Row(0),
		DialectDemand: dialect.Row(0), NodeDemand: nodes.Row(0), RelationDemand: relations.Row(0)}
	return nil
}

func rowVector(v matrixir.Vector) matrixir.Matrix {
	m := matrixir.NewMatrix(1, len(v))
	copy(m.Data, v)
	return m
}

func validateFeatureBasis(b *SemanticFeatureBasis) error {
	if b.Schema != "code-transpiler.semantic-feature-basis.v1" || !uniqueNonempty(b.Languages) || !uniqueNonempty(b.Features) || !uniqueNonempty(b.DialectFeatures) || !uniqueNonempty(b.NodeKinds) || !uniqueNonempty(b.RelationKinds) {
		return fmt.Errorf("invalid semantic feature basis axes")
	}
	shapes := []struct {
		m          matrixir.Matrix
		rows, cols int
	}{
		{b.LanguageFeature, len(b.Languages), len(b.Features)}, {b.LanguageDialectFeature, len(b.Languages), len(b.DialectFeatures)},
		{b.FeatureNode, len(b.Features), len(b.NodeKinds)}, {b.FeatureRelation, len(b.Features), len(b.RelationKinds)},
	}
	for _, shape := range shapes {
		if !validDenseMatrix(shape.m) || shape.m.Rows != shape.rows || shape.m.Cols != shape.cols {
			return fmt.Errorf("semantic feature basis matrix dimensions differ from axes")
		}
		for _, value := range shape.m.Data {
			if !finiteNonnegative(value) {
				return fmt.Errorf("semantic feature basis contains invalid weight")
			}
		}
	}
	if len(b.DialectObserved) != len(b.Languages) {
		return fmt.Errorf("semantic dialect observation vector dimension mismatch")
	}
	for row, observed := range b.DialectObserved {
		if observed != 0 && observed != 1 {
			return fmt.Errorf("semantic dialect observation vector must be binary")
		}
		if math.Abs(sumRow(b.LanguageFeature, row)-1) > 1e-9 {
			return fmt.Errorf("generic language feature row is not normalized")
		}
		if observed == 1 && math.Abs(sumRow(b.LanguageDialectFeature, row)-1) > 1e-9 {
			return fmt.Errorf("dialect language feature row is not normalized")
		}
	}
	return nil
}

func validateSemanticFeatureModel(model *SemanticFeatureModel, source string) error {
	if model == nil {
		return nil
	}
	if err := loadSemanticFeatureBasis(); err != nil {
		return err
	}
	if model.BasisSHA256 != semanticFeatureBase.BasisSHA256 || !reflect.DeepEqual(model.Basis, semanticFeatureBase.Basis) {
		return fmt.Errorf("semantic feature basis differs from embedded calculated matrix")
	}
	if semanticProfileLanguage(source) != model.ProfileLanguage {
		return fmt.Errorf("semantic feature profile does not match source language")
	}
	if len(model.LanguageSelection) != len(model.Basis.Languages) {
		return fmt.Errorf("semantic language selection dimension mismatch")
	}
	ones, index := 0, -1
	for i, value := range model.LanguageSelection {
		if value != 0 && value != 1 {
			return fmt.Errorf("semantic language selection must be one-hot")
		}
		if value == 1 {
			ones++
			index = i
		}
	}
	if ones != 1 || model.Basis.Languages[index] != model.ProfileLanguage {
		return fmt.Errorf("semantic language selection does not select profile")
	}
	feature, _ := rowVector(model.LanguageSelection).Multiply(model.Basis.LanguageFeature)
	dialect, _ := rowVector(model.LanguageSelection).Multiply(model.Basis.LanguageDialectFeature)
	nodes, _ := feature.Multiply(model.Basis.FeatureNode)
	relations, _ := feature.Multiply(model.Basis.FeatureRelation)
	for name, gotwant := range map[string][2]matrixir.Vector{"feature": {model.FeatureDemand, feature.Row(0)}, "dialect": {model.DialectDemand, dialect.Row(0)}, "node": {model.NodeDemand, nodes.Row(0)}, "relation": {model.RelationDemand, relations.Row(0)}} {
		if !sameFloatVector(gotwant[0], gotwant[1]) {
			return fmt.Errorf("semantic %s demand does not match matrix product", name)
		}
	}
	return nil
}

func uniqueNonempty(values []string) bool {
	seen := map[string]bool{}
	for _, v := range values {
		if v == "" || seen[v] {
			return false
		}
		seen[v] = true
	}
	return len(values) > 0
}
func sumRow(m matrixir.Matrix, row int) float64 {
	sum := 0.0
	for col := 0; col < m.Cols; col++ {
		sum += m.At(row, col)
	}
	return sum
}
func sameFloatVector(a, b matrixir.Vector) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if math.Abs(a[i]-b[i]) > 1e-12 {
			return false
		}
	}
	return true
}
