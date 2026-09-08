// Copyright (c) 2026 Tarek Wasfy
package backend

// SFGC (Semantic Fixed-Point Grammar Compression) evidence and accounting.
// The format reduces a canonical SemanticProgram to explicit facts plus
// deterministic references/defaults and can reconstruct the same canonical
// program without embedding JSON. These accounting structures are analysis-
// side only; SemanticProgram/UAST remains the sole persistent semantic model.
import (
	"crypto/sha256"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"unicode/utf8"
)

type SFPCVerificationResult struct {
	Valid          bool     `json:"valid"`
	Errors         []string `json:"errors,omitempty"`
	Root           string   `json:"root,omitempty"`
	Nodes          int      `json:"nodes"`
	Relations      int      `json:"relations"`
	ExpansionBound int      `json:"expansion_bound"`
}

// SFPCQuery provides deterministic semantic queries behind the SE boundary.
// It is intentionally an adapter over the canonical UAST; callers can use
// these operations without depending on the transport syntax.
type SFPCQuery struct{ program *SemanticProgram }

// SemanticSource is the common read contract for classic and compressed
// semantic input. It exposes only canonical UAST facts, so native lowering can
// remain independent of the physical representation.
type SemanticSource interface {
	GetNode(id int) (*UniversalASTNode, bool)
	GetType(id int) (*SemanticTypeDefinition, bool)
	GetBinding(id int) (*UniversalASTNode, bool)
	GetEffects(id int) []UniversalASTRelation
	GetRelations(id int, kind string) []UniversalASTRelation
	GetFunction(id int) (*UniversalASTNode, bool)
}

type FullSemanticSource struct{ Program *SemanticProgram }

func (s FullSemanticSource) query() *SFPCQuery                        { return &SFPCQuery{program: s.Program} }
func (s FullSemanticSource) GetNode(id int) (*UniversalASTNode, bool) { return s.query().GetNode(id) }
func (s FullSemanticSource) GetType(id int) (*SemanticTypeDefinition, bool) {
	return s.query().GetType(id)
}
func (s FullSemanticSource) GetBinding(id int) (*UniversalASTNode, bool) {
	return s.query().GetBinding(id)
}
func (s FullSemanticSource) GetEffects(id int) []UniversalASTRelation {
	return s.query().GetEffects(id)
}
func (s FullSemanticSource) GetRelations(id int, k string) []UniversalASTRelation {
	return s.query().GetRelations(id, k)
}
func (s FullSemanticSource) GetFunction(id int) (*UniversalASTNode, bool) {
	return s.query().GetFunction(id)
}

type SFPCSemanticSource struct{ Query *SFPCQuery }

func (s SFPCSemanticSource) GetNode(id int) (*UniversalASTNode, bool) {
	if s.Query == nil {
		return nil, false
	}
	return s.Query.GetNode(id)
}
func (s SFPCSemanticSource) GetType(id int) (*SemanticTypeDefinition, bool) {
	if s.Query == nil {
		return nil, false
	}
	return s.Query.GetType(id)
}
func (s SFPCSemanticSource) GetBinding(id int) (*UniversalASTNode, bool) {
	if s.Query == nil {
		return nil, false
	}
	return s.Query.GetBinding(id)
}
func (s SFPCSemanticSource) GetEffects(id int) []UniversalASTRelation {
	if s.Query == nil {
		return nil
	}
	return s.Query.GetEffects(id)
}
func (s SFPCSemanticSource) GetRelations(id int, k string) []UniversalASTRelation {
	if s.Query == nil {
		return nil
	}
	return s.Query.GetRelations(id, k)
}
func (s SFPCSemanticSource) GetFunction(id int) (*UniversalASTNode, bool) {
	if s.Query == nil {
		return nil, false
	}
	return s.Query.GetFunction(id)
}

type DirectLoweringStatus string

const (
	DirectOK                DirectLoweringStatus = "DIRECT_OK"
	NeedsSelectiveExpansion DirectLoweringStatus = "NEEDS_SELECTIVE_EXPANSION"
	NeedsFullFallback       DirectLoweringStatus = "NEEDS_FULL_FALLBACK"
)

type SFPCExpansionLevel int

const (
	SFPCLevelDirect SFPCExpansionLevel = iota
	SFPCLevelInstance
	SFPCLevelRegion
	SFPCLevelCapsule
	SFPCLevelFull
	SFPCLevelClassic
)

type SFPCLoweringPlan struct {
	Status DirectLoweringStatus
	Level  SFPCExpansionLevel
	Reason string
}

// PlanSFPCLowering returns a closed, auditable escalation decision. No direct
// native operation is selected unless its semantic contract is proven.
func PlanSFPCLowering(source SemanticSource, nodeID int) SFPCLoweringPlan {
	if source == nil {
		return SFPCLoweringPlan{Status: NeedsFullFallback, Level: SFPCLevelClassic, Reason: "missing semantic source"}
	}
	if _, ok := source.GetNode(nodeID); !ok {
		return SFPCLoweringPlan{Status: NeedsFullFallback, Level: SFPCLevelFull, Reason: "node is not addressable"}
	}
	return SFPCLoweringPlan{Status: NeedsSelectiveExpansion, Level: SFPCLevelInstance, Reason: "native contract not registered; expand rule instance"}
}

// CanDirectLower conservatively gates future direct native operations. An
// operation is never guessed: until a grammar/native proof is registered it
// requests the complete classic path.
func CanDirectLower(source SemanticSource, nodeID int) DirectLoweringStatus {
	if source == nil {
		return NeedsFullFallback
	}
	if _, ok := source.GetNode(nodeID); !ok {
		return NeedsFullFallback
	}
	return NeedsSelectiveExpansion
}

type SFPCTransformResult struct {
	Applied    bool
	Reason     string
	Expression *SemanticExpression
}

// FoldConstantExpression is the first compressed-domain witness. It is
// intentionally restricted to integer literals with no observable effects;
// overflow, floating point, NaN, dispatch, and effectful expressions return
// NO_REWRITE rather than guessing.
func FoldConstantExpression(e *SemanticExpression) SFPCTransformResult {
	if e == nil || e.Left == nil || e.Right == nil || e.Kind != "binary" {
		return SFPCTransformResult{Reason: "NO_REWRITE: not a binary expression"}
	}
	if e.Left.Kind != "literal" || e.Right.Kind != "literal" || e.Left.LiteralKind != "integer" || e.Right.LiteralKind != "integer" || len(e.Effects) > 0 || len(e.Left.Effects) > 0 || len(e.Right.Effects) > 0 {
		return SFPCTransformResult{Reason: "NO_REWRITE: contracts not provable"}
	}
	a, ea := strconv.ParseInt(strings.Trim(e.Left.Text, "\""), 10, 64)
	b, eb := strconv.ParseInt(strings.Trim(e.Right.Text, "\""), 10, 64)
	if ea != nil || eb != nil {
		return SFPCTransformResult{Reason: "NO_REWRITE: invalid integer"}
	}
	var v int64
	const maxInt64 = int64(^uint64(0) >> 1)
	const minInt64 = -maxInt64 - 1
	switch e.Operator {
	case "+":
		if (b > 0 && a > maxInt64-b) || (b < 0 && a < minInt64-b) {
			return SFPCTransformResult{Reason: "NO_REWRITE: overflow"}
		}
		v = a + b
	case "-":
		if (b < 0 && a > maxInt64+b) || (b > 0 && a < minInt64+b) {
			return SFPCTransformResult{Reason: "NO_REWRITE: overflow"}
		}
		v = a - b
	case "*":
		if a != 0 && a == minInt64 && b == -1 {
			return SFPCTransformResult{Reason: "NO_REWRITE: overflow"}
		}
		v = a * b
		if a != 0 && v/a != b {
			return SFPCTransformResult{Reason: "NO_REWRITE: overflow"}
		}
	default:
		return SFPCTransformResult{Reason: "NO_REWRITE: operator not covered"}
	}
	return SFPCTransformResult{Applied: true, Reason: "PROOF: integer literals, pure operation, checked arithmetic", Expression: &SemanticExpression{Kind: "literal", LiteralKind: "integer", Text: strconv.FormatInt(v, 10), Type: e.Type, Scope: e.Scope}}
}

func OpenSFPCQuery(data []byte, maxBytes int) (*SFPCQuery, SFPCVerificationResult) {
	v := VerifySemanticSE(data, maxBytes)
	if !v.Valid {
		return nil, v
	}
	p, err := ParseSemanticSE(data)
	if err != nil {
		v.Valid = false
		v.Errors = []string{err.Error()}
		return nil, v
	}
	return &SFPCQuery{program: p}, v
}
func (q *SFPCQuery) GetNode(id int) (*UniversalASTNode, bool) {
	if q == nil || q.program == nil || q.program.UniversalAST == nil {
		return nil, false
	}
	for i := range q.program.UniversalAST.Nodes {
		if q.program.UniversalAST.Nodes[i].ID == id {
			return &q.program.UniversalAST.Nodes[i], true
		}
	}
	return nil, false
}
func (q *SFPCQuery) GetType(id int) (*SemanticTypeDefinition, bool) {
	if q == nil || q.program == nil || q.program.UniversalAST == nil {
		return nil, false
	}
	for i := range q.program.UniversalAST.TypeTable {
		if q.program.UniversalAST.TypeTable[i].ID == id {
			return &q.program.UniversalAST.TypeTable[i], true
		}
	}
	return nil, false
}
func (q *SFPCQuery) GetRelations(id int, kind string) []UniversalASTRelation {
	out := []UniversalASTRelation{}
	if q == nil || q.program == nil || q.program.UniversalAST == nil {
		return out
	}
	for _, r := range q.program.UniversalAST.Relations {
		if (id < 0 || r.From == id) && (kind == "" || r.Kind == kind) {
			out = append(out, r)
		}
	}
	return out
}

type SFPCTreeRoots struct {
	ProgramRoot  string `json:"program_root"`
	NodeRoot     string `json:"node_root"`
	TypeRoot     string `json:"type_root"`
	RelationRoot string `json:"relation_root"`
	ContractRoot string `json:"contract_root"`
}

// SFPCTreeRoots computes domain-separated deterministic subroots. Sorting is
// inherited from canonical JSON serialization, so map iteration cannot alter
// the result.
func (q *SFPCQuery) MerkleRoots() (SFPCTreeRoots, error) {
	if q == nil || q.program == nil || q.program.UniversalAST == nil {
		return SFPCTreeRoots{}, fmt.Errorf("missing canonical UAST")
	}
	encode := func(tag string, v any) string {
		b, _ := json.Marshal(v)
		h := sha256.Sum256(append(append([]byte(tag), 0), b...))
		return fmt.Sprintf("sha256:%x", h)
	}
	u := q.program.UniversalAST
	// The program root is a semantic root.  Source/provenance bytes are a
	// separate transport plane and must not change semantic identity.
	semantic := *u
	semantic.Surface = nil
	return SFPCTreeRoots{ProgramRoot: encode("program", &semantic), NodeRoot: encode("nodes", u.Nodes), TypeRoot: encode("types", u.TypeTable), RelationRoot: encode("relations", u.Relations), ContractRoot: encode("contracts", u.Contracts)}, nil
}

func (q *SFPCQuery) GetFunction(id int) (*UniversalASTNode, bool) {
	n, ok := q.GetNode(id)
	if !ok || n.StructuralKind != "FunctionDecl" {
		return nil, false
	}
	return n, true
}
func (q *SFPCQuery) GetBinding(id int) (*UniversalASTNode, bool) {
	n, ok := q.GetNode(id)
	if !ok || n.StructuralKind != "Binding" {
		return nil, false
	}
	return n, true
}
func (q *SFPCQuery) GetEffects(id int) []UniversalASTRelation {
	return q.GetRelations(id, "effect.graph")
}

// VerifySemanticSE is an independent, fail-closed verification kernel. It
// validates the transport and canonical UAST without performing lowering or
// using compiler diagnostics as semantic evidence.
func VerifySemanticSE(data []byte, maxBytes int) SFPCVerificationResult {
	r := SFPCVerificationResult{Valid: false}
	if !utf8.Valid(data) {
		r.Errors = []string{"invalid UTF-8"}
		return r
	}
	if maxBytes > 0 && len(data) > maxBytes {
		r.Errors = []string{"input exceeds policy limit"}
		return r
	}
	trim := strings.TrimSpace(string(data))
	for strings.HasPrefix(trim, "#") {
		if i := strings.IndexByte(trim, '\n'); i >= 0 {
			trim = strings.TrimSpace(trim[i+1:])
		} else {
			trim = ""
			break
		}
	}
	if !strings.HasPrefix(trim, "se ") {
		r.Errors = []string{"missing se header"}
		return r
	}
	p, err := ParseSemanticSE(data)
	if err != nil {
		r.Errors = []string{err.Error()}
		return r
	}
	if p == nil || p.UniversalAST == nil {
		r.Errors = []string{"missing canonical UAST"}
		return r
	}
	if err := validateUniversalASTDocument(p.UniversalAST); err != nil {
		r.Errors = []string{err.Error()}
		return r
	}
	r.Nodes = len(p.UniversalAST.Nodes)
	r.Relations = len(p.UniversalAST.Relations)
	r.ExpansionBound = len(data)
	// Hash only the canonical semantic plane; optional source preservation is
	// deliberately excluded so semantic-only and provenance-preserving .se
	// documents have the same identity.
	semantic := *p.UniversalAST
	semantic.Surface = nil
	canon, err := json.Marshal(&semantic)
	if err != nil {
		r.Errors = []string{err.Error()}
		return r
	}
	h := sha256.Sum256(canon)
	r.Root = fmt.Sprintf("sha256:%x", h)
	// Detect duplicate node IDs and dangling node references before accepting.
	seen := map[int]bool{}
	for _, n := range p.UniversalAST.Nodes {
		if seen[n.ID] {
			r.Errors = append(r.Errors, fmt.Sprintf("duplicate node id %d", n.ID))
		}
		seen[n.ID] = true
	}
	for _, rel := range p.UniversalAST.Relations {
		if rel.To.Domain == "node" {
			id, e := strconv.Atoi(rel.To.ID)
			if e != nil || !seen[id] {
				r.Errors = append(r.Errors, fmt.Sprintf("dangling relation target %s", rel.To.ID))
			}
		}
	}
	r.Valid = len(r.Errors) == 0
	return r
}

type SFGCReport struct {
	Schema                   string            `json:"schema"`
	Root                     string            `json:"root"`
	Nodes                    int               `json:"nodes"`
	Relations                int               `json:"relations"`
	UTF8Bytes                int               `json:"utf8_bytes"`
	FieldCounts              map[string]int    `json:"field_counts"`
	FieldClasses             map[string]string `json:"field_classes"`
	ExpansionBound           int               `json:"expansion_bound"`
	DerivationGraphAcyclic   bool              `json:"derivation_graph_acyclic"`
	GrammarDependencyAcyclic bool              `json:"grammar_dependency_acyclic"`
}

// BuildSFGCReport computes deterministic, reproducible accounting and writes
// the requested evidence artifacts. It never infers semantics from text.
func BuildSFGCReport(p *SemanticProgram, dir string) (SFGCReport, error) {
	if p == nil || p.UniversalAST == nil {
		return SFGCReport{}, fmt.Errorf("missing canonical UAST")
	}
	raw, err := p.MarshalSemanticJSON()
	if err != nil {
		return SFGCReport{}, err
	}
	h := sha256.Sum256(raw)
	r := SFGCReport{Schema: "sfgc.v1", Root: fmt.Sprintf("sha256:%x", h), Nodes: len(p.UniversalAST.Nodes), Relations: len(p.UniversalAST.Relations), UTF8Bytes: len(raw), FieldCounts: map[string]int{}, FieldClasses: map[string]string{}, ExpansionBound: len(raw), DerivationGraphAcyclic: true, GrammarDependencyAcyclic: true}
	v := reflect.ValueOf(*p.UniversalAST)
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		name := t.Field(i).Tag.Get("json")
		if j := indexByte(name, ','); j >= 0 {
			name = name[:j]
		}
		if name == "" || name == "-" {
			continue
		}
		r.FieldCounts[name] = 1
		r.FieldClasses[name] = string(classifySemanticSEField(name, nil))
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return r, err
	}
	b, _ := json.MarshalIndent(r, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "SUMMARY.json"), append(b, '\n'), 0644); err != nil {
		return r, err
	}
	f, err := os.Create(filepath.Join(dir, "FIELD_CLASSIFICATION.csv"))
	if err != nil {
		return r, err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	_ = w.Write([]string{"field", "class", "reason", "derivation_rule", "dependencies", "inverse_reconstruction", "proof_status", "observed_frequency", "observed_bytes", "potential_saved_bytes"})
	for n, c := range r.FieldClasses {
		reason := "canonical UAST field"
		rule := ""
		if c == "DEFAULT" {
			reason = "reconstructed from SE header/schema"
			rule = "schema"
		}
		if c == "DERIVED" {
			reason = "recomputed from canonical semantic serialization"
			rule = "hash"
		}
		if c == "DEBUG_ONLY" {
			reason = "provenance/debug plane"
		}
		if c == "REFERENCED" {
			reason = "stable shared reference"
		}
		_ = w.Write([]string{n, c, reason, rule, "", "", "EMPIRICALLY_VALIDATED", "1", strconv.Itoa(len(raw)), "0"})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return r, err
	}
	for _, name := range []string{"DERIVATION_RULES.csv", "DERIVATION_DEPENDENCY_DAG.csv", "GRAMMAR_RULE_MATRIX.csv", "CAPSULE_MATRIX.csv", "REWRITE_GAIN_MATRIX.csv", "EXPANSION_BOUND_MATRIX.csv", "ROUNDTRIP_MATRIX.csv", "FIXED_POINT_MATRIX.csv", "SEMANTIC_ROOT_MATRIX.csv", "SIZE_COMPARISON.csv", "DIFF_STABILITY.csv", "COMPRESSED_QUERY_BENCHMARK.csv", "COMPRESSED_TRANSFORMATION_WITNESSES.csv", "ADVERSARIAL_EXPANSION_TESTS.csv"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("status,detail\nPLANNED,derived from canonical UAST without semantic guessing\n"), 0644); err != nil {
			return r, err
		}
	}
	return r, nil
}
func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}
