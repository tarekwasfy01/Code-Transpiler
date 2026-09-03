package backend

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

func legacyEffectClass(effect string) string {
	switch effect {
	case "local.read", "local.write", "call.unknown", "control":
		return "B"
	default:
		return "D" // name/runtime-specific evidence not proved by direct facts
	}
}

// LegacyDecorationRecord is deliberately test-only.  It records output facts
// added by the historical decorator so migration code can classify facts by
// their actual output rather than guessing from syntax names.
type LegacyDecorationRecord struct {
	Category       string
	SourceEntity   int
	TargetEntity   int
	FieldName      string
	FieldValue     string
	RelationKind   string
	Role           string
	Ordinal        int
	TypeRef        string
	SymbolRef      string
	BindingRef     int
	ScopeRef       int
	EffectKind     string
	EvidenceSource string
	Producer       string
	Class          string // A canonical, B derivable, C legacy-only, D unproved/schema gap
}

func legacyDecorationOracle(t *testing.T, source, code string) []LegacyDecorationRecord {
	t.Helper()
	p, err := ParseSemantic(source, code)
	if err != nil {
		t.Fatal(err)
	}
	root, err := documentStatement(p.Body)
	if err != nil {
		t.Fatal(err)
	}
	assignDocumentIDs(&root)
	// Snapshot the only fields decorateDocument is allowed to add.
	before, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	decorateDocument(&root, p.Evidence, p.IndexBase)
	after, err := json.Marshal(root)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) == string(after) {
		return nil
	}
	records := []LegacyDecorationRecord{}
	var visitExpr func(*SemanticExpression)
	var visitStmt func(*SemanticStatement)
	visitExpr = func(e *SemanticExpression) {
		if e == nil {
			return
		}
		for _, effect := range e.Effects {
			records = append(records, LegacyDecorationRecord{Category: "effect", SourceEntity: e.ID, ScopeRef: semanticScope(p.Evidence, e.ID), EffectKind: effect, EvidenceSource: "SemanticEvidence.Effects", Producer: "SemanticProgram.analyze→decorateDocument", Class: legacyEffectClass(effect)})
		}
		if e.Type.Kind != "" {
			raw, _ := json.Marshal(e.Type)
			records = append(records, LegacyDecorationRecord{Category: "type", SourceEntity: e.ID, TypeRef: string(raw), Producer: "decorateDocument", Class: "A"})
		}
		if e.Binding != nil {
			record := LegacyDecorationRecord{Category: "binding", SourceEntity: e.ID, BindingRef: *e.Binding, ScopeRef: semanticScope(p.Evidence, e.ID), EvidenceSource: "SemanticEvidence.Binding", Producer: "decorateDocument", Class: "B"}
			if *e.Binding >= 0 && *e.Binding < len(p.Evidence.Bindings) {
				record.SymbolRef = p.Evidence.Bindings[*e.Binding].Name
				record.ScopeRef = p.Evidence.Bindings[*e.Binding].Scope
				record.TargetEntity = p.Evidence.Bindings[*e.Binding].Definition
			}
			records = append(records, record)
		}
		visitExpr(e.Left)
		visitExpr(e.Right)
		visitExpr(e.Value)
		for _, a := range e.Arguments {
			visitExpr(a.Value)
		}
		if e.Function != nil {
			for _, p := range e.Function.Parameters {
				if p.Type.Kind != "" {
					raw, _ := json.Marshal(p.Type)
					records = append(records, LegacyDecorationRecord{Category: "parameter_type", SourceEntity: p.ID, TypeRef: string(raw), Producer: "decorateDocument", Class: "A"})
				}
				visitExpr(p.Default)
			}
			visitStmt(&e.Function.Body)
		}
	}
	visitStmt = func(s *SemanticStatement) {
		if s == nil {
			return
		}
		for _, effect := range s.Effects {
			records = append(records, LegacyDecorationRecord{Category: "effect", SourceEntity: s.ID, ScopeRef: semanticScope(p.Evidence, s.ID), EffectKind: effect, EvidenceSource: "SemanticEvidence.Effects", Producer: "SemanticProgram.analyze→decorateDocument", Class: legacyEffectClass(effect)})
		}
		if s.Type.Kind != "" {
			raw, _ := json.Marshal(s.Type)
			records = append(records, LegacyDecorationRecord{Category: "type", SourceEntity: s.ID, TypeRef: string(raw), Producer: "decorateDocument", Class: "A"})
		}
		visitExpr(s.Expression)
		visitExpr(s.Condition)
		visitExpr(s.Sequence)
		visitStmt(s.Then)
		visitStmt(s.Else)
		visitStmt(s.Body)
		for i := range s.Statements {
			visitStmt(&s.Statements[i])
		}
	}
	visitStmt(&root)
	table, _, err := deriveTypeTable(&root)
	if err != nil {
		t.Fatal(err)
	}
	relations, err := deriveTypeRelations(&root, table)
	if err != nil {
		t.Fatal(err)
	}
	for i, path := range relations.Occurrences {
		typeRef := ""
		for col, entry := range table {
			if relations.Uses.At(i, col) != 0 {
				raw, _ := json.Marshal(entry.Type)
				typeRef = string(raw)
				break
			}
		}
		records = append(records, LegacyDecorationRecord{Category: "type_occurrence", SourceEntity: i, FieldName: path, TypeRef: typeRef, EvidenceSource: "SemanticTypeRelations.Uses", Producer: "deriveTypeRelations", Class: "C"})
	}
	for row, edge := range relations.Edges {
		parent, target := -1, -1
		for col := range table {
			if relations.Parents.At(row, col) != 0 {
				parent = col
			}
			if relations.Children.At(row, col) != 0 {
				target = col
			}
		}
		records = append(records, LegacyDecorationRecord{Category: "type_edge", SourceEntity: parent, TargetEntity: target, RelationKind: "type.structural_child", Role: edge.Role, Ordinal: edge.Index, FieldName: edge.Name, EvidenceSource: "SemanticTypeRelations.Parents/Children", Producer: "deriveTypeRelations", Class: "B"})
	}
	sort.Slice(records, func(i, j int) bool {
		return records[i].Category+records[i].FieldName+records[i].TypeRef < records[j].Category+records[j].FieldName+records[j].TypeRef
	})
	return records
}

func semanticScope(e SemanticEvidence, id int) int {
	if id >= 0 && id < len(e.Nodes) {
		return e.Nodes[id].Scope
	}
	return -1
}

func TestLegacyDecorationOracleComplexCorpus(t *testing.T) {
	records := legacyDecorationOracle(t, "python", "x <- 1; f <- function(a) { if (a > x) return(a + x); x <- a }; f(x)")
	if len(records) == 0 {
		t.Fatal("legacy decoration oracle captured no records")
	}
}

func TestLegacyDecorationOracleClassificationIsExhaustive(t *testing.T) {
	records := legacyDecorationOracle(t, "python", "x <- 1; f <- function(a) { if (a > x) return(a + x); x <- a }; print(f(x))")
	counts := map[string]int{}
	for _, record := range records {
		if record.Class != "A" && record.Class != "B" && record.Class != "C" && record.Class != "D" {
			t.Fatalf("unclassified oracle record: %+v", record)
		}
		counts[record.Class]++
	}
	if counts["A"] == 0 || counts["B"] == 0 || counts["C"] == 0 {
		t.Fatalf("oracle did not exercise A/B/C classes: %v", counts)
	}
}

func TestLegacyDecorationOracleSchemaAdmissibility(t *testing.T) {
	program, err := LowerMatrixLanguage("python", "x = 1\ndef f(a):\n    if a > x:\n        return a + x\n    return a\ny = f(x)\n")
	if err != nil {
		t.Fatal(err)
	}
	if err = validateUniversalASTDocument(program.UniversalAST); err != nil {
		t.Fatal(err)
	}
	nodes := map[int]*UniversalASTNode{}
	for i := range program.UniversalAST.Nodes {
		common, decodeErr := decodeUniversalCommon(&program.UniversalAST.Nodes[i])
		if decodeErr != nil {
			t.Fatal(decodeErr)
		}
		nodes[common.ID] = &program.UniversalAST.Nodes[i]
	}
	for _, record := range legacyDecorationOracle(t, "python", "x <- 1; f <- function(a) { if (a > x) return(a + x); return(a) }; f(x)") {
		if record.Class != "A" {
			continue
		}
		node := nodes[record.SourceEntity]
		if node == nil {
			t.Fatalf("canonical record has no UAST source node: %+v", record)
		}
		if (record.Category == "type" || record.Category == "parameter_type") && !containsString(node.FieldMask, "type_ref") {
			t.Fatalf("canonical type record violates field mask: %+v", record)
		}
		if record.RelationKind != "" && !universalRelationAllowed(node, record.RelationKind) {
			t.Fatalf("canonical relation record violates schema admissibility: %+v", record)
		}
	}
}

func oracleCanonicalKey(r LegacyDecorationRecord) string {
	switch r.Category {
	case "type", "parameter_type":
		return fmt.Sprintf("type:%d:%s", r.SourceEntity, r.TypeRef)
	case "effect":
		return fmt.Sprintf("effect:%d:%s", r.SourceEntity, r.EffectKind)
	case "binding":
		return fmt.Sprintf("binding:%d:%d", r.SourceEntity, r.BindingRef)
	case "type_edge":
		return fmt.Sprintf("type_edge:%d:%d:%s:%d:%s", r.SourceEntity, r.TargetEntity, r.Role, r.Ordinal, r.FieldName)
	default:
		return ""
	}
}

func directDecorationKeys(u *UniversalASTDocument) map[string]bool {
	out := map[string]bool{}
	if u == nil {
		return out
	}
	for i := range u.Nodes {
		c, err := decodeUniversalCommon(&u.Nodes[i])
		if err != nil {
			continue
		}
		if c.Type.Kind != "" {
			raw, _ := json.Marshal(c.Type)
			out[fmt.Sprintf("type:%d:%s", c.ID, raw)] = true
		}
		for _, effect := range c.Effects {
			if legacyEffectClass(effect) != "D" {
				out[fmt.Sprintf("effect:%d:%s", c.ID, effect)] = true
			}
		}
		if c.Binding != nil {
			out[fmt.Sprintf("binding:%d:%d", c.ID, *c.Binding)] = true
		}
	}
	if u.TypeRelations != nil {
		for row, edge := range u.TypeRelations.Edges {
			parent, target := -1, -1
			for col := range u.TypeTable {
				if u.TypeRelations.Parents.At(row, col) != 0 {
					parent = col
				}
				if u.TypeRelations.Children.At(row, col) != 0 {
					target = col
				}
			}
			out[fmt.Sprintf("type_edge:%d:%d:%s:%d:%s", parent, target, edge.Role, edge.Index, edge.Name)] = true
		}
	}
	return out
}

func assertOracleCanonicalPresent(t *testing.T, language, source string) {
	t.Helper()
	canonical, err := matrixir.Canonicalize(language, source)
	if err != nil {
		t.Fatal(err)
	}
	parts := []string{}
	for _, event := range canonical.Events {
		if strings.TrimSpace(event.Text) != "" {
			parts = append(parts, event.Text)
		}
	}
	oracle := legacyDecorationOracle(t, language, strings.Join(parts, "\n"))
	legacy, err := LowerMatrixEvents(language, canonical.Events)
	if err != nil {
		t.Fatal(err)
	}
	legacyDoc, err := legacy.Document()
	if err != nil {
		t.Fatal(err)
	}
	legacyFacts, err := frontendSemanticFactsFromUniversalAST(legacyDoc.UniversalAST, nil)
	if err != nil {
		t.Fatal(err)
	}
	reference, err := BuildCanonicalUniversalASTFromFrontendFacts(legacyFacts)
	if err != nil {
		t.Fatal(err)
	}
	direct, err := LowerMatrixLanguage(language, source)
	if err != nil {
		t.Fatal(err)
	}
	got := directDecorationKeys(direct.UniversalAST)
	for _, record := range oracle {
		if record.Class != "A" && record.Class != "B" {
			continue
		}
		key := oracleCanonicalKey(record)
		if key != "" && !got[key] {
			t.Errorf("%s missing canonical legacy decoration %s producer=%s", language, key, record.Producer)
		}
	}
	for category, ok := range compareCanonicalUAST(reference, direct.UniversalAST) {
		if !ok {
			switch category {
			case "fields":
				for i := range reference.Nodes {
					a, _ := json.Marshal(reference.Nodes[i].Fields)
					b, _ := json.Marshal(direct.UniversalAST.Nodes[i].Fields)
					if string(a) != string(b) {
						t.Logf("%s fields node=%d legacy=%s direct=%s", language, i, a, b)
						break
					}
				}
			case "evidence":
				if err := validateSemanticEvidence(direct.UniversalAST.Evidence, reference.Evidence); err != nil {
					t.Logf("%s evidence diff: %v", language, err)
				}
			case "relations":
				t.Logf("%s relation counts legacy=%d direct=%d", language, len(reference.Relations), len(direct.UniversalAST.Relations))
				legacyRelations := map[string]bool{}
				directRelations := map[string]bool{}
				for _, relation := range reference.Relations {
					data, _ := json.Marshal(relation)
					legacyRelations[string(data)] = true
				}
				for _, relation := range direct.UniversalAST.Relations {
					data, _ := json.Marshal(relation)
					directRelations[string(data)] = true
				}
				for relation := range legacyRelations {
					if !directRelations[relation] {
						t.Logf("%s relation missing direct=%s", language, relation)
					}
				}
				for relation := range directRelations {
					if !legacyRelations[relation] {
						t.Logf("%s relation extra direct=%s", language, relation)
					}
				}
			}
			t.Errorf("%s canonical category mismatch: %s", language, category)
		}
	}
}

func TestLegacyOracleCanonicalParityComplex(t *testing.T) {
	assertOracleCanonicalPresent(t, "python", "x = 1\ndef f(a):\n    if a > x:\n        return a + x\n    return a\ny = f(x)\n")
}

func TestLegacyOracleCanonicalParityExtendedCorpus(t *testing.T) {
	assertOracleCanonicalPresent(t, "r", "x <- 1\nxs <- c(1, 2, 3)\nf <- function(a, b = 2) {\n  while (a < b) { a <- a + 1 }\n  for (i in 1:3) { a <- a + i }\n  if (!(a > 0)) { return(-a) } else { return(a + xs[1]) }\n}\ny <- f(x, 2)\n")
}

func TestLegacyDecorationOracleSummary(t *testing.T) {
	source := "x <- 1\nxs <- c(1, 2, 3)\nf <- function(a, b = 2) {\n  while (a < b) { a <- a + 1 }\n  for (i in 1:3) { a <- a + i }\n  if (!(a > 0)) { return(-a) } else { return(a + xs[1]) }\n}\ny <- f(x, 2)\nprint(y)\n"
	canonical, err := matrixir.Canonicalize("r", source)
	if err != nil {
		t.Fatal(err)
	}
	parts := []string{}
	for _, event := range canonical.Events {
		if strings.TrimSpace(event.Text) != "" {
			parts = append(parts, event.Text)
		}
	}
	records := legacyDecorationOracle(t, "r", strings.Join(parts, "\n"))
	direct, err := LowerMatrixLanguage("r", source)
	if err != nil {
		t.Fatal(err)
	}
	keys := directDecorationKeys(direct.UniversalAST)
	type summary struct {
		Total, A, B, C, D, Matched, Missing int
	}
	summaries := map[string]*summary{"types": {}, "effects": {}, "bindings": {}}
	for _, record := range records {
		group := ""
		switch record.Category {
		case "type", "parameter_type", "type_occurrence", "type_edge":
			group = "types"
		case "effect":
			group = "effects"
		case "binding":
			group = "bindings"
		}
		if group == "" {
			continue
		}
		s := summaries[group]
		s.Total++
		switch record.Class {
		case "A":
			s.A++
		case "B":
			s.B++
		case "C":
			s.C++
		case "D":
			s.D++
		}
		if record.Class == "A" || record.Class == "B" {
			key := oracleCanonicalKey(record)
			if key == "" || keys[key] {
				s.Matched++
			} else {
				s.Missing++
			}
		}
	}
	for _, group := range []string{"types", "effects", "bindings"} {
		s := summaries[group]
		t.Logf("%s legacy=%d A=%d B=%d C=%d D=%d matched=%d missing=%d", group, s.Total, s.A, s.B, s.C, s.D, s.Matched, s.Missing)
		if s.Missing != 0 {
			t.Fatalf("%s has %d missing canonical records", group, s.Missing)
		}
	}
}

func TestLegacyOracleCanonicalParityAllMatrixLanguages(t *testing.T) {
	for _, language := range MatrixFrontendLanguages() {
		language := language
		t.Run(language, func(t *testing.T) { assertOracleCanonicalPresent(t, language, "x = 1\ny = x + 2\nprint(y)\n") })
	}
}
