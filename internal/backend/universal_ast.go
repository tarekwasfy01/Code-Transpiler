package backend

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"sync"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

//go:embed universal_ast_schema.json
var embeddedUniversalASTSchema []byte

type UniversalASTBasis struct {
	Schema                     string                `json:"schema"`
	Features                   []string              `json:"features"`
	Facets                     []string              `json:"facets"`
	SemanticAxes               []string              `json:"semantic_axes"`
	RelationAxes               []string              `json:"relation_axes"`
	Languages                  []string              `json:"languages"`
	StructuralKinds            []string              `json:"structural_kinds"`
	ConcreteRelations          []string              `json:"concrete_relations"`
	Fields                     []string              `json:"fields"`
	Layers                     []string              `json:"layers"`
	GlobalRelations            []string              `json:"global_relations"`
	CrosswalkFields            []string              `json:"crosswalk_fields"`
	FeatureFacet               matrixir.SparseMatrix `json:"feature_facet"`
	FeatureSignature           matrixir.SparseMatrix `json:"feature_signature"`
	FacetAxis                  matrixir.SparseMatrix `json:"facet_axis"`
	FacetRelationAxis          matrixir.SparseMatrix `json:"facet_relation_axis"`
	LanguageFacet              matrixir.SparseMatrix `json:"language_facet"`
	CoverageLower              matrixir.SparseMatrix `json:"coverage_lower"`
	CoverageUpper              matrixir.SparseMatrix `json:"coverage_upper"`
	StructuralFacetSeed        matrixir.SparseMatrix `json:"structural_facet_seed"`
	FacetLayer                 matrixir.SparseMatrix `json:"facet_layer"`
	StructuralLayer            matrixir.SparseMatrix `json:"structural_layer"`
	FacetConcreteRelation      matrixir.SparseMatrix `json:"facet_concrete_relation"`
	StructuralConcreteRelation matrixir.SparseMatrix `json:"structural_concrete_relation"`
	FacetField                 matrixir.SparseMatrix `json:"facet_field"`
	StructuralField            matrixir.SparseMatrix `json:"structural_field"`
}

type UniversalASTDocument struct {
	SchemaVersion          int                      `json:"schema_version"`
	BasisSHA256            string                   `json:"basis_sha256"`
	LanguageProfile        string                   `json:"language_profile"`
	LanguageFacet          matrixir.Vector          `json:"language_facet"`
	Projection             string                   `json:"projection,omitempty"`
	SemanticDocumentSHA256 string                   `json:"semantic_document_sha256,omitempty"`
	Evaluation             string                   `json:"evaluation,omitempty"`
	ValueModel             string                   `json:"value_model,omitempty"`
	IndexBase              int                      `json:"index_base,omitempty"`
	Types                  SemanticTypeContract     `json:"type_contract,omitempty"`
	Origin                 SemanticOrigin           `json:"origin,omitempty"`
	Metadata               map[string]string        `json:"metadata,omitempty"`
	Extensions             map[string]any           `json:"extensions,omitempty"`
	Contracts              SemanticContracts        `json:"contracts,omitempty"`
	Dialects               []SemanticDialect        `json:"dialects,omitempty"`
	SemanticFeatures       *SemanticFeatureModel    `json:"semantic_features,omitempty"`
	TypeTable              []SemanticTypeDefinition `json:"type_table,omitempty"`
	TypeGraph              matrixir.SparseMatrix    `json:"type_graph,omitempty"`
	TypeRelations          *SemanticTypeRelations   `json:"type_relations,omitempty"`
	Evidence               SemanticEvidence         `json:"evidence,omitempty"`
	Nodes                  []UniversalASTNode       `json:"nodes"`
	Relations              []UniversalASTRelation   `json:"relations"`
}

type UniversalASTNode struct {
	ID             int                        `json:"id"`
	StructuralKind string                     `json:"structural_kind"`
	SemanticFacets []string                   `json:"semantic_facets,omitempty"`
	FieldMask      []string                   `json:"field_mask"`
	Fields         map[string]json.RawMessage `json:"fields,omitempty"`
	Source         *SemanticSourceSpan        `json:"source_span,omitempty"`
	Attributes     map[string]json.RawMessage `json:"attributes,omitempty"`
}

type UniversalASTReference struct {
	Domain string `json:"domain"`
	ID     string `json:"id"`
}
type UniversalASTRelation struct {
	Kind       string                     `json:"kind"`
	From       int                        `json:"from"`
	To         UniversalASTReference      `json:"to"`
	Attributes map[string]json.RawMessage `json:"attributes,omitempty"`
}

var uastOnce sync.Once
var uastEmbedded struct {
	BasisSHA256 string            `json:"basis_sha256"`
	Basis       UniversalASTBasis `json:"basis"`
}
var uastErr error

func loadUniversalASTBasis() error {
	uastOnce.Do(func() {
		uastErr = json.Unmarshal(embeddedUniversalASTSchema, &uastEmbedded)
		if uastErr == nil {
			uastErr = validateUniversalASTBasis(&uastEmbedded.Basis)
		}
	})
	return uastErr
}

func validateUniversalASTBasis(b *UniversalASTBasis) error {
	if b.Schema != "code-transpiler.universal-ast-basis.v1" || len(b.Features) != 553 || len(b.Facets) != 334 || len(b.StructuralKinds) != 109 || len(b.SemanticAxes) != 44 || len(b.RelationAxes) != 23 || len(b.ConcreteRelations) != 55 || len(b.Fields) != 57 || len(b.Layers) != 17 || len(b.Languages) != 8 || len(b.GlobalRelations) == 0 || len(b.CrosswalkFields) == 0 {
		return fmt.Errorf("universal AST basis dimensions differ from v1 contract")
	}
	if !uniqueNonempty(b.Features) || !uniqueNonempty(b.Facets) || !uniqueNonempty(b.SemanticAxes) || !uniqueNonempty(b.RelationAxes) || !uniqueNonempty(b.Languages) || !uniqueNonempty(b.StructuralKinds) || !uniqueNonempty(b.ConcreteRelations) || !uniqueNonempty(b.Fields) || !uniqueNonempty(b.Layers) || !uniqueNonempty(b.GlobalRelations) || !uniqueNonempty(b.CrosswalkFields) {
		return fmt.Errorf("universal AST basis labels are not unique")
	}
	// UASF is the canonical 334-dimensional facet axis. Keep the complete
	// structural catalog addressable even when evidence or target lowering for a
	// particular facet is still unresolved.
	for i, facet := range b.Facets {
		want := fmt.Sprintf("UASF_%04d", i+1)
		if facet != want {
			return fmt.Errorf("canonical UASF axis is incomplete at %d: got %q want %q", i+1, facet, want)
		}
	}
	for _, relation := range b.GlobalRelations {
		if indexOf(b.ConcreteRelations, relation) < 0 {
			return fmt.Errorf("global relation %q is absent from concrete relation catalog", relation)
		}
	}
	for _, field := range b.CrosswalkFields {
		if indexOf(b.Fields, field) < 0 {
			return fmt.Errorf("crosswalk field %q is absent from universal field catalog", field)
		}
	}
	shapes := []struct {
		m    matrixir.SparseMatrix
		r, c int
	}{
		{b.FeatureFacet, 553, 334}, {b.FeatureSignature, 553, 67}, {b.FacetAxis, 334, 44}, {b.FacetRelationAxis, 334, 23}, {b.LanguageFacet, 8, 334},
		{b.CoverageLower, 8, 334}, {b.CoverageUpper, 8, 334}, {b.FacetLayer, 334, 17}, {b.StructuralLayer, 109, 17},
		{b.StructuralFacetSeed, 109, 334},
		{b.FacetConcreteRelation, 334, 55}, {b.StructuralConcreteRelation, 109, 55}, {b.FacetField, 334, 57}, {b.StructuralField, 109, 57},
	}
	for _, s := range shapes {
		if s.m.Rows != s.r || s.m.Cols != s.c {
			return fmt.Errorf("universal AST basis matrix dimensions differ from axes")
		}
	}
	for row := 0; row < 553; row++ {
		count := 0
		for col := 0; col < 334; col++ {
			if b.FeatureFacet.At(row, col) != 0 {
				count++
			}
		}
		if count != 1 {
			return fmt.Errorf("feature quotient row %d is not one-hot", row)
		}
	}
	// Exact signature rows must correspond one-to-one with quotient facets.
	signatureFacet := map[string]int{}
	for row := 0; row < 553; row++ {
		signature := ""
		for col := 0; col < 67; col++ {
			if b.FeatureSignature.At(row, col) != 0 {
				signature += "," + strconv.Itoa(col)
			}
		}
		facet := -1
		for col := 0; col < 334; col++ {
			if b.FeatureFacet.At(row, col) != 0 {
				facet = col
				break
			}
		}
		if old, ok := signatureFacet[signature]; ok && old != facet {
			return fmt.Errorf("equal feature signatures split across facets")
		}
		signatureFacet[signature] = facet
	}
	if len(signatureFacet) != 334 {
		return fmt.Errorf("semantic facet quotient does not contain 334 exact classes")
	}
	for row := 0; row < 8; row++ {
		for col := 0; col < 334; col++ {
			lo, hi := b.CoverageLower.At(row, col), b.CoverageUpper.At(row, col)
			if math.IsNaN(lo) || math.IsNaN(hi) || math.IsInf(lo, 0) || math.IsInf(hi, 0) || lo < 0 || hi > 1 || lo > hi {
				return fmt.Errorf("invalid SemanticProgram coverage interval")
			}
		}
	}
	return nil
}

func NewUniversalASTDocument(source string) (*UniversalASTDocument, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return nil, err
	}
	profile := universalASTProfileLanguage(source)
	row := -1
	for i, name := range uastEmbedded.Basis.Languages {
		if name == profile {
			row = i
			break
		}
	}
	if row < 0 && (profile == "" || profile == "semantic") {
		return &UniversalASTDocument{SchemaVersion: 1, BasisSHA256: uastEmbedded.BasisSHA256, LanguageProfile: "universal", LanguageFacet: make(matrixir.Vector, len(uastEmbedded.Basis.Facets))}, nil
	}
	if row < 0 {
		return nil, fmt.Errorf("no universal AST language projection for %q", source)
	}
	vector := make(matrixir.Vector, len(uastEmbedded.Basis.Facets))
	for col := range vector {
		vector[col] = uastEmbedded.Basis.LanguageFacet.At(row, col)
	}
	return &UniversalASTDocument{SchemaVersion: 1, BasisSHA256: uastEmbedded.BasisSHA256, LanguageProfile: profile, LanguageFacet: vector}, nil
}

func universalASTProfileLanguage(source string) string {
	profile := semanticProfileLanguage(source)
	if profile == "clang_cpp" {
		return "cpp"
	}
	return profile
}

func (d *UniversalASTDocument) AddNode(kind string, facets []string, fields map[string]json.RawMessage) (int, error) {
	n := UniversalASTNode{ID: len(d.Nodes), StructuralKind: kind, SemanticFacets: append([]string(nil), facets...), Fields: fields}
	mask, err := universalFieldMask(&n)
	if err != nil {
		return 0, err
	}
	n.FieldMask = mask
	d.Nodes = append(d.Nodes, n)
	return n.ID, nil
}
func (d *UniversalASTDocument) AddRelation(kind string, from int, to UniversalASTReference, attributes map[string]json.RawMessage) error {
	d.Relations = append(d.Relations, UniversalASTRelation{Kind: kind, From: from, To: to, Attributes: attributes})
	return validateUniversalASTDocument(d)
}

func universalFieldMask(n *UniversalASTNode) ([]string, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return nil, err
	}
	kind := indexOf(uastEmbedded.Basis.StructuralKinds, n.StructuralKind)
	if kind < 0 {
		return nil, fmt.Errorf("unknown structural kind %q", n.StructuralKind)
	}
	mask := make([]bool, len(uastEmbedded.Basis.Fields))
	for _, field := range uastEmbedded.Basis.CrosswalkFields {
		if col := indexOf(uastEmbedded.Basis.Fields, field); col >= 0 {
			mask[col] = true
		}
	}
	for col := range mask {
		mask[col] = mask[col] || uastEmbedded.Basis.StructuralField.At(kind, col) != 0
	}
	seen := map[string]bool{}
	for _, name := range n.SemanticFacets {
		if seen[name] {
			return nil, fmt.Errorf("duplicate semantic facet %q", name)
		}
		seen[name] = true
		row := indexOf(uastEmbedded.Basis.Facets, name)
		if row < 0 {
			return nil, fmt.Errorf("unknown semantic facet %q", name)
		}
		for col := range mask {
			mask[col] = mask[col] || uastEmbedded.Basis.FacetField.At(row, col) != 0
		}
	}
	out := []string{}
	for col, yes := range mask {
		if yes {
			out = append(out, uastEmbedded.Basis.Fields[col])
		}
	}
	return out, nil
}

func validateUniversalASTDocument(d *UniversalASTDocument) error {
	if d == nil {
		return nil
	}
	if err := loadUniversalASTBasis(); err != nil {
		return err
	}
	if d.SchemaVersion != 1 || d.BasisSHA256 != uastEmbedded.BasisSHA256 {
		return fmt.Errorf("unsupported or modified universal AST basis")
	}
	if d.Projection != "" && d.Projection != "semantic_document.v1" && d.Projection != "frontend_facts.v1" {
		return fmt.Errorf("unknown universal AST compatibility projection %q", d.Projection)
	}
	if d.Projection == "semantic_document.v1" && len(d.SemanticDocumentSHA256) != 64 {
		return fmt.Errorf("universal AST compatibility projection missing semantic document digest")
	}
	profileRow := indexOf(uastEmbedded.Basis.Languages, d.LanguageProfile)
	if (profileRow < 0 && d.LanguageProfile != "universal") || len(d.LanguageFacet) != 334 {
		return fmt.Errorf("invalid universal AST language projection")
	}
	for col, v := range d.LanguageFacet {
		want := 0.0
		if profileRow >= 0 {
			want = uastEmbedded.Basis.LanguageFacet.At(profileRow, col)
		}
		if v != want {
			return fmt.Errorf("universal AST language facet vector differs from matrix product")
		}
	}
	nodes := map[int]*UniversalASTNode{}
	for i := range d.Nodes {
		n := &d.Nodes[i]
		if n.ID < 0 || nodes[n.ID] != nil {
			return fmt.Errorf("universal AST node IDs must be unique and nonnegative")
		}
		nodes[n.ID] = n
		mask, err := universalFieldMask(n)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(mask, n.FieldMask) {
			return fmt.Errorf("node %d field mask differs from facet/structural matrix product", n.ID)
		}
		allowed := map[string]bool{}
		for _, f := range mask {
			allowed[f] = true
		}
		for field, value := range n.Fields {
			if !allowed[field] {
				return fmt.Errorf("field %q is not applicable to node %d", field, n.ID)
			}
			if !json.Valid(value) {
				return fmt.Errorf("field %q on node %d is not valid JSON", field, n.ID)
			}
		}
	}
	for _, r := range d.Relations {
		from := nodes[r.From]
		if from == nil {
			return fmt.Errorf("relation source node %d missing", r.From)
		}
		relation := indexOf(uastEmbedded.Basis.ConcreteRelations, r.Kind)
		if relation < 0 {
			return fmt.Errorf("unknown concrete relation %q", r.Kind)
		}
		allowed := indexOf(uastEmbedded.Basis.GlobalRelations, r.Kind) >= 0
		kind := indexOf(uastEmbedded.Basis.StructuralKinds, from.StructuralKind)
		allowed = allowed || uastEmbedded.Basis.StructuralConcreteRelation.At(kind, relation) != 0
		for _, facet := range from.SemanticFacets {
			allowed = allowed || uastEmbedded.Basis.FacetConcreteRelation.At(indexOf(uastEmbedded.Basis.Facets, facet), relation) != 0
		}
		if !allowed {
			return fmt.Errorf("relation %q is not applicable to source node %d", r.Kind, r.From)
		}
		if r.To.Domain == "node" {
			id, err := strconv.Atoi(r.To.ID)
			if err != nil || nodes[id] == nil {
				return fmt.Errorf("relation target node %q missing", r.To.ID)
			}
		} else if r.To.Domain == "" || r.To.ID == "" {
			return fmt.Errorf("relation target reference incomplete")
		}
		for key, value := range r.Attributes {
			if key == "" || !json.Valid(value) {
				return fmt.Errorf("invalid relation attribute")
			}
		}
	}
	return nil
}

func validateExecutableUniversalAST(p *SemanticProgram) error {
	if p != nil && p.UniversalAST != nil && (len(p.UniversalAST.Nodes) > 0 || len(p.UniversalAST.Relations) > 0) {
		return validateUniversalExecutionContracts(p.UniversalAST)
	}
	return nil
}

// NormalizeUniversalAST is the shared canonical validation/normalization pass
// for all ingress paths. It operates only on UAST data and never constructs a
// legacy statement or expression tree.
func NormalizeUniversalAST(d *UniversalASTDocument) (*UniversalASTDocument, error) {
	if err := validateUniversalASTDocument(d); err != nil {
		return nil, err
	}
	return d, nil
}
func indexOf(values []string, want string) int {
	for i, v := range values {
		if v == want {
			return i
		}
	}
	return -1
}
func sortedUnique(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	return out
}
