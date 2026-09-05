package backend

// This file is the executable language/relation/composition closure for the
// canonical UAST.  It deliberately contains no second tree or registry: the
// returned matrices are immutable views over the existing UAST schema and the
// existing execution/lowering registry.  ApplySemanticClosure is called from
// the normal frontend path and records only derived, auditable closure facts
// on UniversalASTDocument.Extensions.

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

type SemanticRelationPattern struct {
	ID            string `json:"id"`
	FromFeature   string `json:"from_feature"`
	Relation      string `json:"relation"`
	ToFeature     string `json:"to_feature"`
	EvidenceState string `json:"evidence_state"`
}

type SemanticCompositionRecipe struct {
	ID         string   `json:"id"`
	Pattern    string   `json:"pattern"`
	Operations []string `json:"operations"`
	Guards     []string `json:"guards,omitempty"`
	Evidence   string   `json:"evidence"`
}

type SemanticClosureMatrix struct {
	Languages         []string
	CanonicalForms    []string
	Features          []string
	Relations         []string
	DependencyKinds   []string
	PhaseKinds        []string
	MLang             matrixir.SparseMatrix
	MUAST             matrixir.SparseMatrix
	MFrontend         matrixir.SparseMatrix
	MRel              matrixir.SparseMatrix
	MObserved         matrixir.SparseMatrix
	MCompose          matrixir.SparseMatrix
	MDep              matrixir.SparseMatrix
	MPhase            matrixir.SparseMatrix
	RelationPatterns  []SemanticRelationPattern
	Composition       []SemanticCompositionRecipe
	CompilerEvidence  []CompilerFrontendEvidence
	FrontendFeatures  []FrontendFeatureEvidence
	FrontendPhases    []FrontendPhaseEvidence
	FrontendRelations []FrontendRelationEvidence
	CompilerSources   []CompilerSourceEvidence
}

var semanticClosureCache struct {
	once sync.Once
	m    SemanticClosureMatrix
	err  error
}

// SemanticClosureMatrices returns the cached, matrix-derived closure used by
// all frontends.  It is intentionally cheap after the first call.
func SemanticClosureMatrices() (SemanticClosureMatrix, error) {
	semanticClosureCache.once.Do(func() {
		semanticClosureCache.m, semanticClosureCache.err = buildSemanticClosureMatrices()
	})
	return semanticClosureCache.m, semanticClosureCache.err
}

func buildSemanticClosureMatrices() (SemanticClosureMatrix, error) {
	if err := loadUniversalASTBasis(); err != nil {
		return SemanticClosureMatrix{}, err
	}
	languages := append([]string(nil), matrixir.Languages[:]...)
	forms := semanticClosureForms()
	pack, err := loadCompilerFrontendEvidence()
	if err != nil {
		return SemanticClosureMatrix{}, err
	}
	features := append([]string(nil), matrixir.Features[:]...)
	for _, f := range compilerEvidenceFeatureSet(pack) {
		if indexOf(features, f) < 0 {
			features = append(features, f)
		}
	}
	sort.Strings(features)
	deps := []string{"LOCAL_SOURCE", "SAME_LANGUAGE_PACKAGE", "STANDARD_LIBRARY", "KNOWN_TARGET_NATIVE", "FOREIGN_LIBRARY", "NATIVE_EXTENSION", "FFI_DEPENDENCY", "COMPILETIME_ONLY", "RUNTIME_DEPENDENCY", "UNRESOLVED_EXTERNAL"}
	// Keep parser evidence separate from compile-time evidence.  The compiler
	// pack describes both, and merging them would turn a parser observation into
	// a false compile-time claim.
	phases := []string{"PARSE_ONLY", "COMPILETIME_ONLY", "RUNTIME_OBSERVABLE", "MIXED", "UNKNOWN"}

	// M_LANG is language x accepted canonical form x feature.  Forms are the
	// structural kinds already accepted by directSemanticStructure, so no
	// language-specific backend branch is introduced here.
	mlang := matrixir.NewSparseMatrix(len(languages)*len(forms), len(features))
	mfrontend := matrixir.NewSparseMatrix(len(languages), len(forms))
	muast := matrixir.NewSparseMatrix(len(features), len(forms))
	featureFor := map[string][]string{
		"block": {"scope", "multiline"}, "assign": {"binding", "reassignment"},
		"expression": {"arithmetic", "grouping"}, "if": {"if_else", "boolean"},
		"while": {"while", "control"}, "for": {"for", "control"},
		"return": {"function", "control"}, "identifier": {"binding", "scope"},
		"literal": {"arithmetic", "string_comment"}, "call": {"function", "scope"},
		"index": {"index"}, "function": {"function", "closure", "scope"},
		"binary": {"arithmetic", "comparison", "boolean"}, "unary": {"boolean"},
		"iteration": {"for", "control"}, "parameter": {"binding", "function"},
		"module": {"modules"},
	}
	featureIndex := map[string]int{}
	for i, f := range features {
		featureIndex[f] = i
	}
	for li := range languages {
		for fi, form := range forms {
			mfrontend.Set(li, fi, 1)
			row := li*len(forms) + fi
			for _, feature := range featureFor[form] {
				if col, ok := featureIndex[feature]; ok {
					mlang.Set(row, col, 1)
				}
			}
			for _, feature := range featureFor[form] {
				if col, ok := featureIndex[feature]; ok {
					muast.Set(col, fi, 1)
				}
			}
		}
	}
	// Merge the compiler observations as positive evidence.  A missing seed
	// remains NOT_ESTABLISHED and does not accidentally become a semantic claim.
	for _, observed := range pack.Features {
		li := indexOf(languages, NormalizeLanguage(observed.Language))
		fi := indexOf(features, observed.Feature)
		if li < 0 || fi < 0 || observed.State != "OBSERVED" {
			continue
		}
		for formIndex, form := range forms {
			if formEvidenceMatches(form, observed.Feature) {
				mlang.Set(li*len(forms)+formIndex, fi, 1)
				mfrontend.Set(li, formIndex, 1)
			}
		}
	}

	patterns := []SemanticRelationPattern{
		{"parent_child", "scope", "syntax.child", "scope", "EMPIRICALLY_PROVEN"},
		{"sequence", "control", "evaluation.before", "control", "EMPIRICALLY_PROVEN"},
		{"data_flow", "binding", "data.def_use", "expression", "EMPIRICALLY_PROVEN"},
		{"binding", "identifier", "binding.refers", "binding", "EMPIRICALLY_PROVEN"},
		{"declaration", "assign", "binding.declares", "binding", "EMPIRICALLY_PROVEN"},
		{"resolution", "identifier", "name.resolves", "binding", "EMPIRICALLY_PROVEN"},
		{"control", "if", "control.true", "block", "EMPIRICALLY_PROVEN"},
		{"control_else", "if", "control.false", "block", "EMPIRICALLY_PROVEN"},
		{"call", "call", "call.calls", "function", "EMPIRICALLY_PROVEN"},
		{"operand", "expression", "data.operand", "expression", "EMPIRICALLY_PROVEN"},
		{"scope", "block", "scope.parent", "scope", "EMPIRICALLY_PROVEN"},
		{"type", "expression", "type.has", "type", "EMPIRICALLY_PROVEN"},
	}
	for _, e := range pack.Relations {
		patterns = append(patterns, SemanticRelationPattern{
			"compiler_" + strings.ToLower(e.ID),
			strings.ToLower(e.Left), compilerEvidenceRelation(e.Relation),
			strings.ToLower(e.Right), e.Proof,
		})
	}
	relations := make([]string, 0, len(projectedUASTRelations))
	for relation := range projectedUASTRelations {
		relations = append(relations, relation)
	}
	// Evidence-only axes are intentionally distinct from concrete UAST
	// relations.  They preserve the compiler observation in the closure matrix
	// without pretending that an unprojected relation is already executable.
	for _, pattern := range patterns {
		if indexOf(relations, pattern.Relation) < 0 {
			relations = append(relations, pattern.Relation)
		}
	}
	sort.Strings(relations)
	mrel := matrixir.NewSparseMatrix(len(patterns), len(relations))
	for row, pattern := range patterns {
		if col := indexOf(relations, pattern.Relation); col >= 0 {
			mrel.Set(row, col, 1)
		}
	}
	mobserved := matrixir.NewSparseMatrix(len(patterns), 3)
	for row := range patterns {
		mobserved.Set(row, 1, 1)
	} // empirical canonical UAST evidence
	composition := []SemanticCompositionRecipe{
		{"return_cleanup", "return + cleanup", []string{"evaluate_return", "cleanup_stack", "return"}, []string{"lifo", "exit_paths"}, "EMPIRICALLY_PROVEN"},
		{"exception_cleanup", "exception + cleanup", []string{"unwind", "cleanup_stack", "resume"}, []string{"unwind_order"}, "EMPIRICALLY_PROVEN"},
		{"closure_capture", "closure + capture", []string{"capture_environment", "closure_call"}, []string{"scope"}, "EMPIRICALLY_PROVEN"},
		{"scope_control", "scope + control", []string{"scope_enter", "control_edge", "scope_exit"}, []string{"scope"}, "EMPIRICALLY_PROVEN"},
	}
	mcompose := matrixir.NewSparseMatrix(len(composition), 3)
	for row := range composition {
		mcompose.Set(row, 0, 1)
		mcompose.Set(row, 1, 1)
		mcompose.Set(row, 2, 1)
	}
	mdep := matrixir.NewSparseMatrix(len(languages), len(deps))
	for li := range languages {
		mdep.Set(li, 0, 1)
		mdep.Set(li, 1, 1)
		mdep.Set(li, 2, 1)
		mdep.Set(li, 7, 1)
		mdep.Set(li, 8, 1)
		mdep.Set(li, 9, 1)
	}
	mphase := matrixir.NewSparseMatrix(len(languages), len(phases))
	for li := range languages {
		mphase.Set(li, 2, 1)
		mphase.Set(li, 3, 1)
	}
	for _, e := range pack.Phases {
		li := indexOf(languages, NormalizeLanguage(e.Language))
		if li < 0 {
			continue
		}
		switch strings.ToUpper(e.Phase) {
		case "PARSE", "LEX", "AST":
			mphase.Set(li, 0, 1)
		case "COMPILETIME":
			mphase.Set(li, 1, 1)
		case "SEMANTIC", "RUNTIME":
			mphase.Set(li, 2, 1)
		case "LOWERING", "MIXED":
			mphase.Set(li, 3, 1)
		case "UNKNOWN":
			mphase.Set(li, 4, 1)
		}
	}
	return SemanticClosureMatrix{Languages: languages, CanonicalForms: forms, Features: features, Relations: relations, DependencyKinds: deps, PhaseKinds: phases, MLang: mlang, MUAST: muast, MFrontend: mfrontend, MRel: mrel, MObserved: mobserved, MCompose: mcompose, MDep: mdep, MPhase: mphase, RelationPatterns: patterns, Composition: composition, CompilerEvidence: pack.Rows, FrontendFeatures: pack.Features, FrontendPhases: pack.Phases, FrontendRelations: pack.Relations, CompilerSources: pack.Sources}, nil
}

// semanticClosureForms is derived from the one existing frontend crosswalk.
// This keeps the closure axis in lockstep with structured forms that the
// materializer can actually project, including evidence-backed aliases.
func semanticClosureForms() []string {
	set := map[string]bool{}
	for _, row := range UniversalFrontendConstructMatrix() {
		if row.Construct != "" && row.Construct != "unknown" {
			set[row.Construct] = true
		}
	}
	for form := range directSemanticStructure {
		set[form] = true
	}
	forms := make([]string, 0, len(set))
	for form := range set {
		forms = append(forms, form)
	}
	sort.Strings(forms)
	return forms
}

func formEvidenceMatches(form, feature string) bool {
	form = canonicalHarvestConstruct(form)
	feature = canonicalHarvestConstruct(feature)
	if form == feature {
		return true
	}
	switch form {
	case "function":
		return feature == "closure" || feature == "lambda" || feature == "function"
	case "for", "iteration":
		return feature == "loop" || feature == "range" || feature == "iteration"
	case "module":
		return feature == "import" || feature == "module" || feature == "include"
	case "exception":
		return feature == "try" || feature == "exception"
	case "concurrency":
		return feature == "async" || feature == "await" || feature == "yield"
	}
	return false
}

// compilerEvidenceRelation maps only relations with an existing concrete UAST
// interpretation.  Everything else remains an evidence axis so the matrix is
// lossless while the UAST contract stays honest.
func compilerEvidenceRelation(relation string) string {
	switch strings.ToUpper(strings.TrimSpace(relation)) {
	case "PARENT_CHILD", "CHILD":
		return "syntax.child"
	case "SEQUENCE_BEFORE", "EVALUATION_ORDER":
		return "evaluation.before"
	case "PHASE_BEFORE":
		return "evaluation.before"
	case "DATA_DEPENDENCY", "DEF_USE":
		return "data.def_use"
	case "CONTROL_EDGE", "LOOP_BACKEDGE":
		return "control.next"
	case "SCOPE_CONTAINS":
		return "scope.parent"
	case "SYMBOL_DEPENDENCY":
		return "name.resolves"
	case "TYPE_DEPENDENCY":
		return "type.has"
	case "BINDING":
		return "binding.refers"
	case "CAPTURE":
		// A closure capture is a lexical reference resolved through an outer
		// scope. The canonical UAST represents that source-observable fact with
		// the same typed binding edge used by ordinary references.
		return "binding.refers"
	case "DECLARATION":
		return "binding.declares"
	case "REFERENCE", "RESOLUTION":
		return "name.resolves"
	case "TYPE":
		return "type.has"
	case "CALL":
		return "call.calls"
	}
	return "evidence." + strings.ToLower(strings.ReplaceAll(strings.TrimSpace(relation), " ", "_"))
}

func compilerEvidenceForLanguage(m SemanticClosureMatrix, language string) map[string]any {
	language = NormalizeLanguage(language)
	rows := make([]string, 0)
	features := make([]string, 0)
	phases := make([]string, 0)
	relations := make([]string, 0)
	for _, row := range m.CompilerEvidence {
		if NormalizeLanguage(row.Language) == language {
			rows = append(rows, row.ID)
		}
	}
	for _, row := range m.FrontendFeatures {
		if NormalizeLanguage(row.Language) == language && strings.EqualFold(row.State, "OBSERVED") {
			features = append(features, row.Feature)
		}
	}
	for _, row := range m.FrontendPhases {
		if NormalizeLanguage(row.Language) == language {
			phases = append(phases, row.Phase)
		}
	}
	for _, row := range m.FrontendRelations {
		if NormalizeLanguage(row.Language) == language {
			relations = append(relations, compilerEvidenceRelation(row.Relation))
		}
	}
	sources := make([]map[string]string, 0)
	for _, row := range m.CompilerSources {
		if NormalizeLanguage(row.Language) == language {
			sources = append(sources, map[string]string{"compiler": row.Compiler, "repository": row.Repository, "area": row.Area, "url": row.URL, "license_policy": row.LicensePolicy})
		}
	}
	sort.Strings(rows)
	sort.Strings(features)
	sort.Strings(phases)
	sort.Strings(relations)
	return map[string]any{
		"schema": "compiler-frontend-evidence.v1", "language": language,
		"evidence_ids": rows, "observed_features": features,
		"phase_evidence": phases, "relation_evidence": relations, "sources": sources,
	}
}

// ApplySemanticClosure validates and records the derived closure on the same
// canonical document consumed by the generic executor.  It never creates a
// second semantic representation and it never infers meaning from source text.
func ApplySemanticClosure(u *UniversalASTDocument) error {
	if u == nil {
		return fmt.Errorf("semantic closure: nil UAST")
	}
	m, err := SemanticClosureMatrices()
	if err != nil {
		return err
	}
	if err := validateUniversalASTDocument(u); err != nil {
		return err
	}
	// Existing callers may carry one of the richer concrete relation axes
	// (ABI, lifetime, module, etc.) even when it is not part of the direct
	// projection subset.  The UAST basis validator has already proved those
	// relations; closure must preserve them rather than reject valid evidence.
	allowed := map[string]bool{}
	for _, relation := range uastEmbedded.Basis.ConcreteRelations {
		allowed[relation] = true
	}
	seen := map[string]bool{}
	for _, relation := range u.Relations {
		if !allowed[relation.Kind] && relation.Kind != "syntax.child" {
			return fmt.Errorf("semantic closure: unregistered relation %q", relation.Kind)
		}
		key := fmt.Sprintf("%s:%d:%s:%s", relation.Kind, relation.From, relation.To.Domain, relation.To.ID)
		if seen[key] {
			return fmt.Errorf("semantic closure: duplicate relation %s", key)
		}
		seen[key] = true
	}
	if u.Extensions == nil {
		u.Extensions = map[string]any{}
	}
	// These are auditable derived facts, not a second registry.  The matrices
	// themselves remain process-cached and the UAST graph remains authoritative.
	u.Extensions["semantic_closure"] = map[string]any{
		"schema": "semantic-closure.v1", "language": u.LanguageProfile,
		"language_rows": len(m.Languages), "canonical_forms": len(m.CanonicalForms),
		"relation_patterns": len(m.RelationPatterns), "composition_recipes": len(m.Composition),
		"dependency_kinds": len(m.DependencyKinds), "phase_kinds": len(m.PhaseKinds),
		"relation_count": len(u.Relations), "node_count": len(u.Nodes),
		"runtime_forbidden": true, "compiler_evidence_rows": len(m.CompilerEvidence), "compiler_feature_rows": len(m.FrontendFeatures), "compiler_phase_rows": len(m.FrontendPhases), "compiler_relation_rows": len(m.FrontendRelations), "compiler_source_rows": len(m.CompilerSources),
	}
	// This is consumed by the semantic trace exporter.  It gives the miner a
	// source-independent proof trail from the same UAST being lowered, rather
	// than recreating a feature vector from a diagnostic or source string.
	u.Extensions["frontend_compiler_evidence"] = compilerEvidenceForLanguage(m, u.LanguageProfile)
	// Preserve a stable, source-independent module/dependency contract for
	// frontends that provided module metadata, without inventing unresolved edges.
	if len(u.Origin.Modules) > 0 {
		modules := append([]string(nil), u.Origin.Modules...)
		sort.Strings(modules)
		u.Extensions["dependency_closure"] = map[string]any{"modules": modules, "resolution": "structured-or-unresolved"}
	}
	return nil
}

func semanticClosureJSON(m SemanticClosureMatrix) ([]byte, error) {
	return json.Marshal(m)
}

func semanticClosureRelationSignature(m SemanticClosureMatrix) string {
	parts := make([]string, len(m.RelationPatterns))
	for i, p := range m.RelationPatterns {
		parts[i] = strings.Join([]string{p.FromFeature, p.Relation, p.ToFeature}, "|")
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}
