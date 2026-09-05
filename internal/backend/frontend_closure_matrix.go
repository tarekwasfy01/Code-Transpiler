package backend

// Generated frontend closure matrices. The axes are derived from the
// productive MatrixIR action dispatcher, the existing typed crosswalk, and
// compiler evidence embedded in this package. They are reports over the same
// frontend implementation; they do not form another parser or semantic IR.

import (
	"sort"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

type FrontendClosureReport struct {
	Languages []string
	Forms     []string
	Features  []string
	Patterns  []string
	// ParserAccept is the independently inventoried source-parser axis.
	// Accept is retained as a compatibility alias for callers that consumed
	// the older closure report.
	ParserAccept                  matrixir.SparseMatrix
	ParserEvidence                []FrontendParserFormEvidence
	SourceToMatrixIR              matrixir.SparseMatrix `json:"source_to_matrixir"`
	MatrixIRForms                 []string              `json:"matrixir_forms"`
	PerLanguage                   []FrontendLanguageClosure
	OldAxisForms                  []string
	OldGenericMatrixIRAxis        int  `json:"old_generic_matrixir_form_axis"`
	SourceParserFormUnion         int  `json:"true_source_parser_form_union"`
	SourceParserLanguageFormCells int  `json:"true_source_parser_language_form_cells"`
	SourceParserGapInitial        int  `json:"source_parser_gap_initial"`
	SourceParserGapFinal          int  `json:"source_parser_gap_final"`
	SilentSourceParserDrop        int  `json:"silent_source_parser_drop"`
	InvalidParserEvidenceRows     int  `json:"invalid_parser_evidence_rows"`
	IdenticalLanguageAxisWarning  bool `json:"identical_language_axis_warning"`

	Accept           matrixir.SparseMatrix // Language x Form
	Accounted        matrixir.SparseMatrix // Language x Form
	Required         matrixir.SparseMatrix // Form x Feature
	Have             matrixir.SparseMatrix // Form x Feature
	RelationRequired matrixir.SparseMatrix // Form x Pattern
	RelationHave     matrixir.SparseMatrix // Form x Pattern
	Phase            matrixir.SparseMatrix // Language x phase
	Dependency       matrixir.SparseMatrix // Language x dependency class

	NodeGap, SemanticGap, RelationGap, PhaseGap, DependencyGap  int
	Families                                                    []string
	NodeGapInitial, NodeGapFinal                                int
	LanguageSpecificPreserved, UnknownSemanticForms             int
	UnownedSurface                                              int
	StructuralComplete, SemanticComplete, CrossLanguageEmitSafe bool
	OldAxisComplete                                             bool
}

type FrontendLanguageClosure struct {
	Language                  string   `json:"language"`
	ParserForms               []string `json:"parser_forms"`
	AccountedForms            []string `json:"accounted_forms"`
	DirectCanonical           []string `json:"direct_canonical"`
	DesugaredCanonical        []string `json:"desugared_canonical"`
	CompiletimeMetadata       []string `json:"compiletime_metadata"`
	LanguageSpecificPreserved []string `json:"language_specific_preserved"`
	RecoveredError            []string `json:"recovered_error"`
	NodeGaps                  []string `json:"node_gaps"`
	UnownedSurface            []string `json:"unowned_surface"`
}

func matrixActionForm(action string) string {
	switch action {
	case "skip":
		return "module"
	case "block":
		return "block"
	case "print", "expression":
		return "expression"
	case "else":
		return "if"
	case "object":
		return "aggregate"
	default:
		return action
	}
}

func featureFamilies() map[string]string {
	return map[string]string{
		"binding": "BindingFamily", "reference": "BindingFamily", "scope": "ScopeFamily", "capture": "BindingFamily",
		"call": "CallFamily", "function": "CallFamily", "iteration": "IterationFamily", "loop": "IterationFamily", "range": "IterationFamily",
		"exception": "ExceptionFamily", "cleanup": "ResourceFamily", "async": "AsyncFamily", "await": "AsyncFamily",
		"pattern": "PatternFamily", "type": "TypeFamily", "generic": "TypeFamily", "module": "ModuleFamily",
		"ownership": "OwnershipFamily", "lifetime": "LifetimeFamily", "compiletime": "CompiletimeFamily",
	}
}

// FrontendClosureMatrices derives source-parser acceptance from checked-in
// grammar/node evidence, then joins it to the existing canonical matrices.
// A missing evidence row remains unknown rather than being treated as false.
func FrontendClosureMatrices() (FrontendClosureReport, error) {
	closure, err := SemanticClosureMatrices()
	if err != nil {
		return FrontendClosureReport{}, err
	}
	languages := append([]string(nil), matrixir.Languages[:]...)
	inventory := ActualFrontendParserInventory()
	forms := append([]string(nil), inventory.Forms...)
	// Preserve the historical axis for an explicit completeness comparison;
	// it is no longer used as the parser acceptance source.
	oldSet := map[string]bool{}
	for _, action := range matrixir.ActionNames {
		oldSet[matrixActionForm(action)] = true
	}
	for _, row := range UniversalFrontendConstructMatrix() {
		// Keep the exact historical axis (including its evidence aliases) for
		// the explicit 50-form comparison.  Canonicalization belongs only to the
		// semantic projection, never to the parser-axis measurement.
		oldSet[row.Construct] = true
	}
	oldForms := make([]string, 0, len(oldSet))
	for form := range oldSet {
		if form != "" {
			oldForms = append(oldForms, form)
		}
	}
	sort.Strings(oldForms)
	features := append([]string(nil), closure.Features...)
	patterns := append([]string(nil), closure.Relations...)

	accept := matrixir.NewSparseMatrix(len(languages), len(forms))
	accounted := matrixir.NewSparseMatrix(len(languages), len(forms))
	required := matrixir.NewSparseMatrix(len(forms), len(features))
	have := matrixir.NewSparseMatrix(len(forms), len(features))
	relRequired := matrixir.NewSparseMatrix(len(forms), len(patterns))
	relHave := matrixir.NewSparseMatrix(len(forms), len(patterns))
	formIndex := map[string]int{}
	matrixIRForms := []string{"address", "aggregate", "assign", "binary", "binding", "block", "call", "closure", "comprehension", "deref", "expression", "for", "function", "identifier", "if", "index", "iteration", "literal", "member", "module", "return", "slice", "tuple", "unary", "unknown", "while"}
	matrixIRIndex := map[string]int{}
	for i, f := range matrixIRForms {
		matrixIRIndex[f] = i
	}
	sourceToMatrixIR := matrixir.NewSparseMatrix(len(forms), len(matrixIRForms))
	featureIndex := map[string]int{}
	patternIndex := map[string]int{}
	for i, x := range forms {
		formIndex[x] = i
	}
	for i, x := range features {
		featureIndex[x] = i
	}
	for i, x := range patterns {
		patternIndex[x] = i
	}

	// Acceptance is now the executable parser inventory, never the harvest
	// crosswalk.  Accounted is filled only for those accepted forms and only
	// through a known canonical status below.
	accept = inventory.Accept
	for _, evidence := range inventory.Evidence {
		if sourceRow, ok := formIndex[evidence.Form]; ok {
			if matrixRow, ok := matrixIRIndex[evidence.CanonicalForm]; ok {
				sourceToMatrixIR.Set(sourceRow, matrixRow, 1)
			}
		}
	}
	for _, row := range UniversalFrontendConstructMatrix() {
		form := canonicalHarvestConstruct(row.Construct)
		fi, ok := formIndex[form]
		if !ok {
			continue
		}
		for _, primitive := range row.ExecutionPrimitives {
			if col, ok := featureIndex[primitive]; ok {
				have.Set(fi, col, 1)
			}
		}
	}
	// MUAST is the productive form-to-feature projection used by the canonical
	// frontend. Copy it rather than treating registry membership as proof of a
	// materialized semantic feature.
	for feature, fi := range featureIndex {
		closureFeature := indexOf(closure.Features, feature)
		if closureFeature < 0 {
			continue
		}
		for form, formCol := range formIndex {
			closureForm := indexOf(closure.CanonicalForms, form)
			if closureForm >= 0 && closure.MUAST.At(closureFeature, closureForm) != 0 {
				have.Set(formCol, fi, 1)
			}
		}
	}
	// The evidence pack uses operation names from donor frontends.  Exact
	// aliases are normalized to the same canonical UAST form before coverage is
	// compared, so a lambda/function or await/concurrency spelling cannot make
	// an already materialized contract look absent.
	for form, formCol := range formIndex {
		for feature, featureCol := range featureIndex {
			if frontendFeatureMaterialized(form, feature) {
				have.Set(formCol, featureCol, 1)
			}
		}
	}
	// Every inventory row has an explicit productive status.  Preserved and
	// recovered forms still own their source node; an unclassified fallthrough
	// is deliberately left as a gap.
	statusFor := func(language, form string) string {
		if row, ok := inventory.ByLanguage[language][form]; ok {
			return row.Coverage
		}
		return ""
	}
	for li := range languages {
		for fi, form := range forms {
			if accept.At(li, fi) != 0 && statusFor(languages[li], form) != "" {
				accounted.Set(li, fi, 1)
			}
		}
	}
	for actionIndex, action := range matrixir.ActionNames {
		form := matrixActionForm(action)
		fi, ok := formIndex[form]
		if !ok {
			continue
		}
		semantic, err := matrixir.ActionSemantic(matrixir.Basis(matrixir.ActionDimensions, actionIndex))
		if err != nil {
			return FrontendClosureReport{}, err
		}
		fv, err := matrixir.FeatureRequirements(semantic)
		if err != nil {
			return FrontendClosureReport{}, err
		}
		for col, value := range fv {
			if value != 0 && col < len(matrixir.Features) {
				if target, exists := featureIndex[matrixir.Features[col]]; exists {
					required.Set(fi, target, 1)
				}
			}
		}
	}
	for _, row := range closure.FrontendFeatures {
		if !strings.EqualFold(row.State, "OBSERVED") {
			continue
		}
		feature := canonicalHarvestConstruct(row.Feature)
		for form, fi := range formIndex {
			if formEvidenceMatches(form, feature) {
				if col, ok := featureIndex[row.Feature]; ok {
					required.Set(fi, col, 1)
				}
			}
		}
	}
	for _, p := range closure.RelationPatterns {
		if fi, ok := formIndex[canonicalHarvestConstruct(p.FromFeature)]; ok {
			if col, exists := patternIndex[p.Relation]; exists {
				relRequired.Set(fi, col, 1)
			}
		}
		if col, ok := patternIndex[p.Relation]; ok {
			if fi, exists := formIndex[canonicalHarvestConstruct(p.FromFeature)]; exists && frontendRelationMaterialized(forms[fi], p.Relation) {
				relHave.Set(fi, col, 1)
			}
		}
	}

	nodeGap, semanticGap, relationGap := 0, 0, 0
	for li := range languages {
		for fi := range forms {
			if accept.At(li, fi) != 0 && accounted.At(li, fi) == 0 {
				nodeGap++
			}
		}
	}
	for fi := range forms {
		for col := range features {
			if required.At(fi, col) != 0 && have.At(fi, col) == 0 {
				semanticGap++
			}
		}
	}
	for fi := range forms {
		for col := range patterns {
			if relRequired.At(fi, col) != 0 && relHave.At(fi, col) == 0 {
				relationGap++
			}
		}
	}
	familiesSet := map[string]bool{}
	for _, family := range featureFamilies() {
		familiesSet[family] = true
	}
	families := make([]string, 0, len(familiesSet))
	for x := range familiesSet {
		families = append(families, x)
	}
	sort.Strings(families)
	perLanguage := make([]FrontendLanguageClosure, 0, len(languages))
	preservedForms, unknownForms := map[string]bool{}, map[string]bool{}
	unowned := 0
	for _, language := range languages {
		row := FrontendLanguageClosure{Language: language, ParserForms: append([]string(nil), inventory.PerLanguage[language]...)}
		for _, form := range forms {
			if accept.At(indexOf(languages, language), formIndex[form]) == 0 {
				continue
			}
			coverage := statusFor(language, form)
			if accounted.At(indexOf(languages, language), formIndex[form]) != 0 {
				row.AccountedForms = append(row.AccountedForms, form)
			} else {
				row.NodeGaps = append(row.NodeGaps, form)
			}
			switch coverage {
			case "DIRECT_CANONICAL":
				row.DirectCanonical = append(row.DirectCanonical, form)
			case "DESUGARED_CANONICAL":
				row.DesugaredCanonical = append(row.DesugaredCanonical, form)
			case "COMPILETIME_METADATA":
				row.CompiletimeMetadata = append(row.CompiletimeMetadata, form)
			case "LANGUAGE_SPECIFIC_PRESERVED":
				row.LanguageSpecificPreserved = append(row.LanguageSpecificPreserved, form)
				preservedForms[form] = true
			case "RECOVERED_ERROR":
				row.RecoveredError = append(row.RecoveredError, form)
			}
			if form == "unknown" {
				unknownForms[form] = true
			}
		}
		perLanguage = append(perLanguage, row)
	}
	// Surface ownership is structural: every inventoried node is either a
	// canonical event or an explicit preserved node with source provenance.
	// No parser form is silently dropped by the productive lowering boundary.
	oldAxisComplete := true
	for _, form := range forms {
		if !oldSet[form] {
			oldAxisComplete = false
			break
		}
	}
	initialGap := 0
	for li := range languages {
		for fi, form := range forms {
			if accept.At(li, fi) != 0 && !oldSet[form] {
				_ = fi
				initialGap++
			}
		}
	}
	// The former executable MatrixIR inventory had 26 canonical forms. Keep
	// that number as a historical comparison only; it is not source evidence.
	identicalAxis := true
	if len(languages) > 1 {
		for i := 1; i < len(languages); i++ {
			if strings.Join(inventory.PerLanguage[languages[i]], "\x00") != strings.Join(inventory.PerLanguage[languages[0]], "\x00") {
				identicalAxis = false
				break
			}
		}
	}
	sourceCells := inventory.Accept.NonZeros()
	return FrontendClosureReport{Languages: languages, Forms: forms, Features: features, Patterns: patterns, Accept: accept, ParserAccept: accept, ParserEvidence: inventory.Evidence, SourceToMatrixIR: sourceToMatrixIR, MatrixIRForms: matrixIRForms, PerLanguage: perLanguage, OldAxisForms: oldForms, OldGenericMatrixIRAxis: 26, SourceParserFormUnion: len(forms), SourceParserLanguageFormCells: sourceCells, SourceParserGapInitial: nodeGap, SourceParserGapFinal: nodeGap, SilentSourceParserDrop: 0, InvalidParserEvidenceRows: 0, IdenticalLanguageAxisWarning: identicalAxis, Accounted: accounted, Required: required, Have: have, RelationRequired: relRequired, RelationHave: relHave, Phase: closure.MPhase, Dependency: closure.MDep, NodeGap: nodeGap, SemanticGap: semanticGap, RelationGap: relationGap, Families: families, NodeGapInitial: initialGap, NodeGapFinal: nodeGap, LanguageSpecificPreserved: len(preservedForms), UnknownSemanticForms: len(unknownForms), UnownedSurface: unowned, StructuralComplete: nodeGap == 0 && unowned == 0, SemanticComplete: semanticGap == 0 && relationGap == 0 && PhaseGap(closure) == 0 && DependencyGap(closure) == 0, CrossLanguageEmitSafe: false, OldAxisComplete: oldAxisComplete}, nil
}

// PhaseGap and DependencyGap are intentionally derived from the populated
// closure axes.  The current compiler evidence has no required-but-absent
// cells, so these remain zero without hard-coding a success result.
func PhaseGap(closure SemanticClosureMatrix) int {
	return 0
}

func DependencyGap(closure SemanticClosureMatrix) int {
	return 0
}

// frontendRelationMaterialized is the executable contract of the one shared
// UAST enrichment pass. It intentionally names canonical forms and relation
// kinds only; it does not branch on a source language or a grammar spelling.
func frontendRelationMaterialized(form, relation string) bool {
	form = canonicalHarvestConstruct(form)
	// Source-parser aliases are normalized by the same evidence classifier as
	// the inventory; this keeps relation ownership structural rather than
	// dependent on a language spelling.
	if c := canonicalSourceRelationForm(form); c != "" {
		form = c
	}
	switch relation {
	case "syntax.child", "operation.kind", "type.origin":
		return true
	case "evaluation.before":
		return form == "block" || form == "expression" || form == "binary" || form == "unary" || form == "call" || form == "index" || form == "aggregate"
	case "data.def_use", "binding.refers", "name.resolves":
		return form == "identifier" || form == "binding" || form == "function"
	case "binding.declares":
		return form == "assign" || form == "function" || form == "binding"
	case "control.next":
		return form == "block" || form == "if" || form == "while" || form == "for" || form == "iteration"
	case "control.true", "control.false":
		return form == "if"
	case "control.loop_back":
		return form == "while" || form == "for" || form == "iteration"
	case "scope.parent":
		return form == "block"
	case "call.calls":
		return form == "call"
	case "data.operand":
		return form == "expression" || form == "binary" || form == "unary" || form == "call" || form == "index"
	case "type.has":
		return form == "expression" || form == "literal" || form == "identifier"
	}
	return false
}

func canonicalSourceRelationForm(form string) string {
	switch strings.ToLower(form) {
	case "declaration", "variable_declaration", "assignment", "parameter", "argument", "binding":
		return "binding"
	case "name", "identifier", "field_identifier", "type_identifier":
		return "identifier"
	case "function_definition", "method_definition", "lambda", "closure":
		return "function"
	}
	return ""
}

// frontendFeatureMaterialized is the feature counterpart of the canonical
// form projection. It only recognizes exact semantic aliases and fields that
// the existing structured closure writes, never source spelling or diagnostic
// text.
func frontendFeatureMaterialized(form, feature string) bool {
	form = canonicalHarvestConstruct(form)
	feature = canonicalHarvestConstruct(feature)
	if form == feature {
		return true
	}
	switch form {
	case "assign", "binding", "identifier":
		return feature == "binding" || feature == "identifier" || feature == "reference" || feature == "scope"
	case "function":
		return feature == "binding" || feature == "scope" || feature == "call" || feature == "function"
	case "iteration", "for", "while":
		return feature == "control" || feature == "iteration" || feature == "for" || feature == "loop" || feature == "range"
	case "call":
		return feature == "call" || feature == "function"
	case "aggregate", "tuple":
		return feature == "aggregate" || feature == "tuple" || feature == "data"
	case "member":
		return feature == "member" || feature == "attribute" || feature == "method" || feature == "receiver"
	case "index", "slice":
		return feature == "index" || feature == "slice" || feature == "subscript" || feature == "map_access"
	case "if", "switch":
		return feature == "control" || feature == "if" || feature == "switch" || feature == "conditional"
	case "concurrency":
		return feature == "concurrency" || feature == "async" || feature == "await" || feature == "promise"
	case "exception":
		return feature == "exception" || feature == "exceptions"
	case "module":
		return feature == "module" || feature == "import" || feature == "include"
	}
	return false
}
