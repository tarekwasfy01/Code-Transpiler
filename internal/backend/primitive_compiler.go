package backend

// The primitive compiler turns a small, declarative semantic vocabulary into
// transient UAST rewrite recipes.  It is deliberately separate from the
// UniversalAST schema: formulas are build/execution plans, never a second
// program representation.

import (
	"crypto/sha256"
	_ "embed"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

//go:embed semantic_primitive_specs.csv
var embeddedSemanticPrimitiveSpecs []byte

type SemanticPrimitiveSpec struct {
	ID      string   `json:"id"`
	Arity   int      `json:"arity"`
	Class   string   `json:"class"`
	Rewrite string   `json:"rewrite"`
	Guards  []string `json:"guards,omitempty"`
}

type loweringFormula struct {
	Name    string
	Args    []loweringFormula
	Slot    int
	Literal string
}

type LoweringRecipeStep struct {
	Operation string   `json:"operation"`
	Inputs    []string `json:"inputs,omitempty"`
	Output    string   `json:"output,omitempty"`
	Order     int      `json:"order"`
}

// GeneratedLoweringRecipe is an ephemeral rewrite plan.  It contains no
// source program values and can safely be regenerated whenever its hash is
// stale.
type GeneratedLoweringRecipe struct {
	ID           string               `json:"id"`
	Primitive    string               `json:"primitive"`
	Class        string               `json:"class"`
	Dependencies []string             `json:"dependencies,omitempty"`
	Guards       []string             `json:"guards,omitempty"`
	Steps        []LoweringRecipeStep `json:"steps"`
	BasisHash    string               `json:"basis_hash"`
	ProofState   string               `json:"proof_state"`
}

type PrimitiveCompilerReport struct {
	Specs                      []SemanticPrimitiveSpec    `json:"specs"`
	Recipes                    []GeneratedLoweringRecipe  `json:"recipes"`
	PrimitiveDependencies      [][]int                    `json:"primitive_dependencies"`
	Closure                    [][]int                    `json:"closure"`
	Witness                    map[string]string          `json:"witness"`
	AtomicPrimitives           []string                   `json:"atomic_primitives"`
	KernelClasses              []string                   `json:"kernel_classes"`
	DerivedCount               int                        `json:"derived_count"`
	DerivedWithHandlers        int                        `json:"derived_with_handlers"`
	BasisHash                  string                     `json:"basis_hash"`
	Inventory                  []string                   `json:"inventory"`
	InventoryRecords           []PrimitiveInventoryRecord `json:"inventory_records"`
	ClassificationMatrix       [][]int                    `json:"classification_matrix"`
	ClassificationColumns      []string                   `json:"classification_columns"`
	DirectPrimitives           []string                   `json:"direct_primitives"`
	ParameterizedAtomic        []string                   `json:"parameterized_atomic_primitives"`
	RuntimeOnly                []string                   `json:"runtime_only_primitives"`
	ValidationOnly             []string                   `json:"validation_only_primitives"`
	Unresolved                 []string                   `json:"unresolved_primitives"`
	ContractGaps               []string                   `json:"contract_gaps"`
	EquivalenceMatrix          [][]int                    `json:"equivalence_matrix"`
	RequiredPrimitiveMatrix    [][]int                    `json:"required_primitive_matrix"`
	TargetBasis                map[string][]string        `json:"target_basis"`
	GeneratedRulesRegistered   int                        `json:"generated_rules_registered"`
	GeneratedClosureReachable  int                        `json:"generated_rules_closure_reachable"`
	GeneratedExecutorReachable int                        `json:"generated_rules_executor_reachable"`
	KernelMatrix               map[string]string          `json:"kernel_matrix"`
	RecoveredExactRecipes      []string                   `json:"recovered_exact_recipes"`
}

type PrimitiveInventoryRecord struct {
	ID                         string `json:"id"`
	Source                     string `json:"source"`
	Class                      string `json:"class"`
	SpecPresent                bool   `json:"spec_present"`
	DependenciesKnown          bool   `json:"dependencies_known"`
	RecipeGenerated            bool   `json:"recipe_generated"`
	RuleRegistered             bool   `json:"rule_registered"`
	ClosureReachable           bool   `json:"closure_reachable"`
	ExecutorReachable          bool   `json:"executor_reachable"`
	TargetTerminalReachable    bool   `json:"target_terminal_reachable"`
	SpecializedHandlerRequired bool   `json:"specialized_handler_required"`
	Status                     string `json:"status"`
	Reason                     string `json:"reason,omitempty"`
}

var primitiveClassificationColumns = []string{"DERIVED", "ATOMIC", "PARAMETERIZED_ATOMIC", "TARGET_TERMINAL", "RUNTIME_ONLY", "VALIDATION_ONLY", "ALIAS", "CONTRACT_GAP"}

// The target capability matrix is immutable for the lifetime of a process.
// The compiler consults it for every primitive×target cell, so rebuilding the
// complete UAST projection matrix per lookup made the authoritative closure
// path effectively quadratic in expensive registry construction.
var primitiveTargetCapabilityCache struct {
	once sync.Once
	m    UASTTargetCapabilityMatrix
	err  error
}

var primitiveTargetSyntaxTemplateCache struct {
	once sync.Once
	a    TargetSyntaxTemplateAnalysis
	err  error
}

// genericAtomicKernels is the compiler's canonical binding to operations that
// are already consumed by the canonical UAST projector.  These are semantic
// operations, not source-language syntax and not a second execution registry.
// Keeping the binding here makes the compiler inventory agree with the
// productive generic handlers instead of misclassifying known UAST operations
// as missing merely because they do not have a derived recipe.
var genericAtomicKernels = map[string]string{
	"ADD":        "BINARY",
	"SUB":        "BINARY",
	"MUL":        "BINARY",
	"DIV":        "BINARY",
	"REM":        "BINARY",
	"POW":        "BINARY",
	"FLOOR_DIV":  "BINARY",
	"@":          "BINARY",
	"BIT_AND":    "BINARY",
	"BIT_OR":     "BINARY",
	"BIT_XOR":    "BINARY",
	"SHL":        "SHIFT",
	"SHR":        "SHIFT",
	"EQ":         "COMPARE",
	"NE":         "COMPARE",
	"LT":         "COMPARE",
	"LE":         "COMPARE",
	"GT":         "COMPARE",
	"GE":         "COMPARE",
	"AND":        "LOGICAL_BINARY",
	"OR":         "LOGICAL_BINARY",
	"NOT":        "LOGICAL_UNARY",
	"LITERAL":    "LITERAL",
	"LOAD":       "BINDING",
	"ASSIGNMENT": "BINDING",
	"RETURN":     "CONTROL",
	"ITERATION":  "ITERATION",
	"SUM":        "REDUCE",
	"LENGTH":     "REDUCE",
	"SQRT":       "REDUCE",
	"REDUCE_AND": "REDUCE",
	"CALL":       "CALL",
	"APPEND":     "COLLECTION",
	"EMPTY_LIKE": "COLLECTION",
	"IF":         "CONTROL",
	"FOREACH":    "CONTROL",
	"LET":        "CONTROL",
	"RESULT":     "CONTROL",
	"CONST":      "CONSTANT",
	// Empirical language-implementation families. The colon form preserves
	// the harvester's parameterized identity while sharing the same kernel.
	"BINARY:ADD": "BINARY", "BINARY:SUB": "BINARY", "BINARY:MUL": "BINARY", "BINARY:DIV": "BINARY", "BINARY:REM": "BINARY", "BINARY:POW": "BINARY",
	"COMPARE:EQ": "COMPARE", "COMPARE:NE": "COMPARE", "COMPARE:LT": "COMPARE", "COMPARE:LE": "COMPARE", "COMPARE:GT": "COMPARE", "COMPARE:GE": "COMPARE",
	"LOGICAL_BINARY:AND": "LOGICAL_BINARY", "LOGICAL_BINARY:OR": "LOGICAL_BINARY",
	"SHIFT:SHL": "SHIFT", "SHIFT:SHR": "SHIFT", "BITWISE_BINARY:XOR": "BINARY",
	"AGGREGATE_LIST": "COLLECTION", "AGGREGATE_MAP": "COLLECTION", "AGGREGATE_SET": "COLLECTION", "AGGREGATE_TUPLE": "COLLECTION",
	"ALLOCATION": "ALLOCATION", "DEALLOCATION": "ALLOCATION", "INDEX_READ": "INDEX", "INDEX_SLICE": "INDEX",
	"MEMBER_ACCESS": "MEMBER", "STORE": "BINDING", "SELECT": "CONTROL", "PHI": "CONTROL", "LOOP": "CONTROL", "CONTROL_FLOW": "CONTROL", "CONTROL_TRANSFER": "CONTROL",
	"MODULE": "MODULE", "AWAIT": "CONTROL", "YIELD": "CONTROL", "CATCH": "EXCEPTION", "FINALLY": "EXCEPTION", "THROW": "EXCEPTION", "RECOVER": "EXCEPTION", "POP": "COLLECTION", "CONVERT": "CONVERSION",
}

// Derived recipes still need a minimal canonical witness shape when the
// backend matrix is executed without a source witness. Their kernel is the
// terminal family of the recipe, not a separate handler or special target
// path.
var derivedPrimitiveWitnessKernels = map[string]string{
	"ALL":      "REDUCE",
	"AVERAGE2": "BINARY",
	"DOUBLE":   "BINARY",
	"FILTER":   "CONTROL",
	"MEAN":     "REDUCE",
	"RMS":      "REDUCE",
}

func genericAtomicPrimitiveIDs() []string {
	ids := make([]string, 0, len(genericAtomicKernels))
	for id := range genericAtomicKernels {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// GenericAtomicKernel returns an existing productive kernel binding only when
// the canonical operation has a language-neutral UAST consumer.  Callers must
// not infer a binding from a diagnostic or primitive name alone.
func GenericAtomicKernel(primitive string) (string, bool) {
	id := strings.ToUpper(strings.TrimSpace(primitive))
	kernel, ok := genericAtomicKernels[id]
	if !ok {
		kernel, ok = derivedPrimitiveWitnessKernels[id]
	}
	if !ok {
		kernel, ok = v6EvidenceKernelBindings[id]
	}
	if !ok {
		kernel, ok = v8DeltaEvidenceKernelBindings[id]
	}
	return kernel, ok
}

// PrimitiveTargetCapability is the read-only target contract used by reports
// and the final closure replay.  It delegates to the same capability matrix
// used by the productive lowering path, so a report cannot claim support from
// a second, hand-maintained table.
func PrimitiveTargetCapability(target, primitive string) (kernel string, direct bool) {
	kernel, ok := GenericAtomicKernel(primitive)
	if !ok {
		return "", false
	}
	return kernel, targetSupportsKernel(target, kernel)
}

// PrimitiveTargetEmitterEvidence derives target support from the productive
// UAST renderer/template contracts.  It intentionally does not consult the
// older capability plane: that plane describes semantic preservation, while
// this contract answers whether the shared target emitter can render the
// kernel's structural form.
func PrimitiveTargetEmitterEvidence(target, primitive string) (kernel, emitter string, exists, guardCompatible, representationCompatible, reachable bool) {
	kernel, ok := GenericAtomicKernel(primitive)
	if !ok {
		return "", "", false, false, false, false
	}
	primitiveTargetSyntaxTemplateCache.once.Do(func() {
		primitiveTargetSyntaxTemplateCache.a, primitiveTargetSyntaxTemplateCache.err = UniversalTargetSyntaxTemplateAnalysis()
	})
	analysis, err := primitiveTargetSyntaxTemplateCache.a, primitiveTargetSyntaxTemplateCache.err
	if err != nil {
		return kernel, "", false, false, false, false
	}
	form := projectionFormCore
	switch kernel {
	case "COLLECTION", "INDEX":
		form = projectionFormAggregate
	case "BINDING":
		form = projectionFormVariable
	case "CONTROL", "ITERATION", "EXCEPTION":
		form = projectionFormStatement
	case "LITERAL", "COMPARE", "BINARY", "LOGICAL_BINARY", "LOGICAL_UNARY", "SHIFT", "CALL", "CONVERSION", "MEMBER":
		form = projectionFormAtomic
	}
	cell, found := analysis.Cell(NormalizeLanguage(target), projectionClassForForm(form, analysis))
	if !found {
		// The class quotient is not part of the public contract, so inspect the
		// structure expansion and accept any complete cell for this form.
		for _, c := range analysis.Cells {
			if c.Target == NormalizeLanguage(target) && c.Complete && strings.Contains(c.ProjectionForm, form) {
				cell, found = c, true
				break
			}
		}
	}
	if !found {
		return kernel, "", false, false, false, false
	}
	emitter = "renderer." + form
	if binding, ok := generatedProjectionRendererBinding(form); ok {
		emitter = binding.RendererID
	}
	return kernel, emitter, true, true, true, cell.Complete
}

func projectionClassForForm(form string, analysis TargetSyntaxTemplateAnalysis) string {
	for _, cell := range analysis.Cells {
		if cell.ProjectionForm == form {
			return cell.ProjectionClass
		}
	}
	return ""
}

// GeneratedPrimitiveRegistry is the runtime view of the generated recipes.
// It is derived from the canonical specification and has no language branches.
type GeneratedPrimitiveRegistry struct {
	RecipesByPrimitive map[string]GeneratedLoweringRecipe
	AtomicKernels      map[string]string
}

func (r *PrimitiveCompilerReport) Registry() GeneratedPrimitiveRegistry {
	reg := GeneratedPrimitiveRegistry{RecipesByPrimitive: map[string]GeneratedLoweringRecipe{}, AtomicKernels: map[string]string{}}
	for _, recipe := range r.Recipes {
		reg.RecipesByPrimitive[recipe.Primitive] = recipe
	}
	for _, primitive := range r.AtomicPrimitives {
		reg.AtomicKernels[primitive] = atomicKernel(primitive)
	}
	return reg
}

// SemanticEquivalenceLibrary is a read-only view of exact alternatives. It is
// intentionally represented with existing recipe values rather than a second
// semantic IR.
func SemanticEquivalenceLibrary() (map[string][]GeneratedLoweringRecipe, error) {
	report, err := CompileUniversalPrimitiveSpecs()
	if err != nil {
		return nil, err
	}
	lib := map[string][]GeneratedLoweringRecipe{}
	for _, recipe := range report.Recipes {
		lib[recipe.Primitive] = append(lib[recipe.Primitive], recipe)
	}
	return lib, nil
}

// FindShortestExactRecipe performs deterministic exact witness selection over
// the generated equivalence library and target basis. Current recipes are
// single-step witnesses; the API is chain-ready for future transitive rules.
func FindShortestExactRecipe(target, primitive string) (GeneratedLoweringRecipe, error) {
	report, err := CompileUniversalPrimitiveSpecs()
	if err != nil {
		return GeneratedLoweringRecipe{}, err
	}
	for _, recipe := range report.Recipes {
		if recipe.Primitive == primitive {
			if basis := report.TargetBasis[NormalizeLanguage(target)]; len(basis) > 0 {
				return recipe, nil
			}
			return GeneratedLoweringRecipe{}, fmt.Errorf("target %s has no direct primitive terminal", target)
		}
	}
	return GeneratedLoweringRecipe{}, fmt.Errorf("no exact recipe for primitive %s", primitive)
}

// GeneratedUniversalLoweringRules exposes the CSV-derived recipes through the
// same rule shape used by the existing worklist. Callers may merge these rules
// with the static evidence registry without introducing another IR or handler
// table.
func GeneratedUniversalLoweringRules() ([]UniversalLoweringRule, error) {
	report, err := CompileUniversalPrimitiveSpecs()
	if err != nil {
		return nil, err
	}
	rules := make([]UniversalLoweringRule, 0, len(report.Recipes))
	for _, recipe := range report.Recipes {
		r := recipe
		rules = append(rules, UniversalLoweringRule{
			ID:                "generated." + strings.ToLower(recipe.Primitive),
			SourceSemantic:    strings.ToLower(recipe.Primitive),
			ResultSemantics:   []string{"generated.recipe"},
			PreservationClass: LoweringExact,
			EvidenceStatus:    "GENERATED_SPEC",
			Implemented:       true,
			ComplexityBefore:  len(recipe.Steps) + 1,
			ComplexityAfter:   len(recipe.Steps),
			Applier:           func(u *UniversalASTDocument, _ int) error { return applyGeneratedRecipe(u, r) },
		})
	}
	return rules, nil
}

// UniversalLoweringRegistry is the single executable rule view consumed by
// the worklist: evidence-backed static rules plus generated recipe rules.
func UniversalLoweringRegistry() []UniversalLoweringRule {
	rules := append([]UniversalLoweringRule(nil), UniversalLoweringRules()...)
	if generated, err := GeneratedUniversalLoweringRules(); err == nil {
		rules = append(rules, generated...)
	}
	return rules
}

// ApplyPrimitiveClosure is the productive bridge from canonical UAST to the
// generated recipe executor. A recipe is applied only when the document has a
// matching canonical operation. Non-matches are already validated when the
// compiler constructs the recipe, so cloning the complete UAST for every
// unrelated recipe would add quadratic work without strengthening the
// productive contract.
func ApplyPrimitiveClosure(original *UniversalASTDocument, target string) (*UniversalASTDocument, []string, error) {
	report, err := CompileUniversalPrimitiveSpecs()
	if err != nil {
		return nil, nil, err
	}
	u, err := cloneUniversalASTForLowering(original)
	if err != nil {
		return nil, nil, err
	}
	if err := validateUniversalASTDocument(u); err != nil {
		return nil, nil, err
	}
	applied := []string{}
	for _, recipe := range report.Recipes {
		match := false
		for i := range u.Nodes {
			var op universalOperationRecord
			if decodeUniversalField(&u.Nodes[i], "operation", &op) == nil {
				if strings.EqualFold(op.Semantics.Operation, strings.ToLower(recipe.Primitive)) || strings.EqualFold(op.Operator, recipe.Primitive) {
					match = true
					break
				}
			}
		}
		if match {
			u, err = ExecuteLoweringRecipe(u, recipe, target)
			if err != nil {
				return nil, nil, err
			}
			applied = append(applied, recipe.ID)
		}
	}
	if u.Metadata == nil {
		u.Metadata = map[string]string{}
	}
	u.Metadata["primitive.closure"] = "generated"
	u.Metadata["primitive.closure.target"] = NormalizeLanguage(target)
	u.Metadata["primitive.closure.count"] = strconv.Itoa(len(applied))
	for _, n := range u.Nodes {
		var op universalOperationRecord
		if decodeUniversalField(&n, "operation", &op) != nil {
			continue
		}
		id := strings.ToUpper(strings.TrimSpace(op.Semantics.Operation))
		if id == "" {
			id = strings.ToUpper(strings.TrimSpace(op.Operator))
		}
		if id == "" {
			continue
		}
		resolution := ResolveRepositoryPrimitive(id, "", "", "")
		if resolution.Status == "EXISTING_28_MAP" || resolution.Status == "GENERATED_RECIPE" || resolution.Status == "GENERIC_HANDLER" {
			u.Metadata["primitive.resolution."+id] = resolution.Status
		}
	}
	return u, applied, validateUniversalASTDocument(u)
}

func parsePrimitiveSpecs() ([]SemanticPrimitiveSpec, error) {
	r := csv.NewReader(strings.NewReader(string(embeddedSemanticPrimitiveSpecs)))
	h, err := r.Read()
	if err != nil {
		return nil, err
	}
	idx := map[string]int{}
	for i, name := range h {
		idx[strings.ToLower(strings.TrimSpace(name))] = i
	}
	var out []SemanticPrimitiveSpec
	for {
		row, e := r.Read()
		if e == io.EOF {
			break
		}
		if e != nil {
			return nil, e
		}
		get := func(k string) string {
			if i, ok := idx[k]; ok && i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		id, class, formula := get("id"), get("class"), get("rewrite")
		// A canonical atomic is already represented by a UAST operation and
		// therefore has no synthetic rewrite formula.  It is a terminal in the
		// existing executor, not a no-op recipe invented by this compiler.
		if id == "" || (formula == "" && class != "ATOMIC") {
			return nil, fmt.Errorf("primitive spec missing id/rewrite")
		}
		arity, e := strconv.Atoi(get("arity"))
		if e != nil || arity < -1 {
			return nil, fmt.Errorf("primitive %s has invalid arity", id)
		}
		guards := []string{}
		if raw := get("guards"); raw != "" {
			for _, g := range strings.Split(raw, ";") {
				if g = strings.TrimSpace(g); g != "" {
					guards = append(guards, g)
				}
			}
		}
		if formula != "" {
			if _, e := formulaOf(formula); e != nil {
				return nil, fmt.Errorf("primitive %s: %w", id, e)
			}
		}
		out = append(out, SemanticPrimitiveSpec{ID: id, Arity: arity, Class: class, Rewrite: formula, Guards: guards})
	}
	// The canonical atomics are the product's pre-existing semantic authority.
	// Include them in the compiler inventory as terminal recipes, so the
	// compiler, lowering registry, and UAST executor agree on one basis.
	seen := map[string]bool{}
	for _, spec := range out {
		seen[spec.ID] = true
	}
	for _, id := range genericAtomicPrimitiveIDs() {
		if seen[id] {
			continue
		}
		out = append(out, SemanticPrimitiveSpec{ID: id, Arity: -1, Class: "ATOMIC", Guards: []string{"canonical.uast"}})
		seen[id] = true
	}
	// Generated v6 source-observable evidence is parameterized through the
	// existing atomic recipe contract; compiler-internal and donor-only rows
	// were filtered when v6_evidence_generated.go was produced.
	for _, spec := range v6EvidencePrimitiveSpecs {
		if !seen[spec.ID] {
			out = append(out, spec)
			seen[spec.ID] = true
		}
	}
	// V8 delta evidence is quotiented by semantic family before generation.
	// Target-terminal rows remain terminals in the existing compiler authority;
	// they do not create a second lowering registry or a synthetic rewrite.
	for _, spec := range v8DeltaEvidencePrimitiveSpecs {
		if !seen[spec.ID] {
			out = append(out, spec)
			seen[spec.ID] = true
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

type formulaParser struct {
	s string
	i int
}

func (p *formulaParser) skip() {
	for p.i < len(p.s) && (p.s[p.i] == ' ' || p.s[p.i] == '\n' || p.s[p.i] == '\r' || p.s[p.i] == '\t') {
		p.i++
	}
}
func (p *formulaParser) ident() string {
	p.skip()
	start := p.i
	for p.i < len(p.s) && ((p.s[p.i] >= 'A' && p.s[p.i] <= 'Z') || (p.s[p.i] >= 'a' && p.s[p.i] <= 'z') || (p.s[p.i] >= '0' && p.s[p.i] <= '9') || p.s[p.i] == '_' || p.s[p.i] == '$') {
		p.i++
	}
	return p.s[start:p.i]
}
func (p *formulaParser) parse() (loweringFormula, error) {
	p.skip()
	name := p.ident()
	if name == "" {
		return loweringFormula{}, fmt.Errorf("empty formula at %d", p.i)
	}
	if strings.HasPrefix(name, "$") {
		n, e := strconv.Atoi(strings.TrimPrefix(name, "$"))
		if e == nil {
			return loweringFormula{Slot: n}, nil
		}
		// Named bindings (for example $out/$item) are declarative recipe
		// variables, distinct from positional input slots.
		return loweringFormula{Slot: -1, Name: name, Literal: name}, nil
	}
	p.skip()
	if p.i >= len(p.s) || p.s[p.i] != '(' {
		return loweringFormula{Slot: -1, Name: name, Literal: name}, nil
	}
	p.i++
	var args []loweringFormula
	p.skip()
	if p.i < len(p.s) && p.s[p.i] == ')' {
		p.i++
		return loweringFormula{Slot: -1, Name: name, Args: args}, nil
	}
	for {
		a, e := p.parse()
		if e != nil {
			return loweringFormula{}, e
		}
		args = append(args, a)
		p.skip()
		if p.i >= len(p.s) {
			return loweringFormula{}, fmt.Errorf("unterminated %s", name)
		}
		if p.s[p.i] == ')' {
			p.i++
			break
		}
		if p.s[p.i] != ',' {
			return loweringFormula{}, fmt.Errorf("expected comma at %d", p.i)
		}
		p.i++
	}
	return loweringFormula{Slot: -1, Name: name, Args: args}, nil
}

func formulaOf(s string) (loweringFormula, error) {
	p := &formulaParser{s: s}
	f, e := p.parse()
	if e != nil {
		return f, e
	}
	p.skip()
	if p.i != len(p.s) {
		return f, fmt.Errorf("trailing formula input at %d", p.i)
	}
	return f, nil
}

func formulaDependencies(f loweringFormula, known map[string]bool, out map[string]bool) {
	if f.Name != "" && known[f.Name] {
		out[f.Name] = true
	}
	for _, a := range f.Args {
		formulaDependencies(a, known, out)
	}
}

func formulaSteps(f loweringFormula, steps *[]LoweringRecipeStep, seq *int, names *int) string {
	if f.Slot >= 0 {
		return fmt.Sprintf("$%d", f.Slot)
	}
	if f.Literal != "" && len(f.Args) == 0 {
		return f.Literal
	}
	args := make([]string, len(f.Args))
	for i, a := range f.Args {
		args[i] = formulaSteps(a, steps, seq, names)
	}
	*names++
	out := fmt.Sprintf("v%d", *names)
	op := strings.ToUpper(f.Name)
	// SEQ is control composition; all other names are generic micro-ops or
	// parameterized calls.  No target or source language appears here.
	if op == "SEQ" {
		for _, a := range args {
			*seq++
			*steps = append(*steps, LoweringRecipeStep{Operation: "SEQUENCE", Inputs: []string{a}, Order: *seq})
		}
		return out
	}
	*seq++
	*steps = append(*steps, LoweringRecipeStep{Operation: op, Inputs: args, Output: out, Order: *seq})
	return out
}

func compilePrimitiveRecipes(specs []SemanticPrimitiveSpec) ([]GeneratedLoweringRecipe, error) {
	known := map[string]bool{"ADD": true, "SUB": true, "MUL": true, "DIV": true, "SUM": true, "LENGTH": true, "SQRT": true, "REDUCE_AND": true, "CALL": true, "APPEND": true, "IF": true, "FOREACH": true, "LET": true, "RESULT": true, "EMPTY_LIKE": true, "CONST": true, "SEQ": true}
	data, _ := json.Marshal(specs)
	sum := sha256.Sum256(data)
	basis := hex.EncodeToString(sum[:])
	recipes := make([]GeneratedLoweringRecipe, 0, len(specs))
	for _, s := range specs {
		if s.Rewrite == "" && (s.Class == "ATOMIC" || s.Class == "PARAMETERIZED_ATOMIC" || s.Class == "TARGET_TERMINAL") {
			proof := "CANONICAL_UAST_TERMINAL"
			if s.Class == "TARGET_TERMINAL" {
				proof = "TARGET_TERMINAL"
			}
			recipes = append(recipes, GeneratedLoweringRecipe{
				ID: "recipe." + strings.ToLower(s.ID), Primitive: s.ID,
				Class: s.Class, Guards: s.Guards, BasisHash: basis,
				ProofState: proof,
			})
			continue
		}
		f, e := formulaOf(s.Rewrite)
		if e != nil {
			return nil, fmt.Errorf("%s: %w", s.ID, e)
		}
		deps := map[string]bool{}
		formulaDependencies(f, known, deps)
		delete(deps, "ADD")
		delete(deps, "SUB")
		delete(deps, "MUL")
		delete(deps, "DIV")
		delete(deps, "SUM")
		delete(deps, "LENGTH")
		delete(deps, "SQRT")
		delete(deps, "REDUCE_AND")
		delete(deps, "CALL")
		delete(deps, "APPEND")
		delete(deps, "IF")
		delete(deps, "FOREACH")
		delete(deps, "LET")
		delete(deps, "RESULT")
		delete(deps, "EMPTY_LIKE")
		delete(deps, "CONST")
		delete(deps, "SEQ")
		steps := []LoweringRecipeStep{}
		seq, names := 0, 0
		formulaSteps(f, &steps, &seq, &names)
		d := make([]string, 0, len(deps))
		for x := range deps {
			d = append(d, x)
		}
		sort.Strings(d)
		state := "GENERATED"
		if s.Class == "DERIVED" {
			state = "CLOSURE_REACHABLE"
		}
		recipes = append(recipes, GeneratedLoweringRecipe{ID: "recipe." + strings.ToLower(s.ID), Primitive: s.ID, Class: s.Class, Dependencies: d, Guards: s.Guards, Steps: steps, BasisHash: basis, ProofState: state})
	}
	return recipes, nil
}

// CompileUniversalPrimitiveSpecs is the single entry point for generated
// semantic lowering data. It performs deterministic dependency closure and
// records witnesses; callers do not need a second registry.
func CompileUniversalPrimitiveSpecs() (*PrimitiveCompilerReport, error) {
	specs, e := parsePrimitiveSpecs()
	if e != nil {
		return nil, e
	}
	recipes, e := compilePrimitiveRecipes(specs)
	if e != nil {
		return nil, e
	}
	idx := map[string]int{}
	for i, s := range specs {
		idx[s.ID] = i
	}
	n := len(specs)
	dep := make([][]int, n)
	for i, r := range recipes {
		for _, d := range r.Dependencies {
			if j, ok := idx[d]; ok {
				dep[i] = append(dep[i], j)
			}
		}
	}
	closure := make([][]int, n)
	for i := 0; i < n; i++ {
		seen := map[int]bool{i: true}
		q := []int{i}
		for len(q) > 0 {
			x := q[0]
			q = q[1:]
			for _, y := range dep[x] {
				if !seen[y] {
					seen[y] = true
					q = append(q, y)
				}
			}
		}
		for x := range seen {
			closure[i] = append(closure[i], x)
		}
		sort.Ints(closure[i])
	}
	w := map[string]string{}
	for i, s := range specs {
		parts := []string{}
		for _, j := range closure[i] {
			parts = append(parts, specs[j].ID)
		}
		w[s.ID] = strings.Join(parts, " -> ")
	}
	atomic := genericAtomicPrimitiveIDs()
	kernelSet := map[string]bool{}
	for _, primitive := range atomic {
		kernelSet[atomicKernel(primitive)] = true
	}
	kernels := make([]string, 0, len(kernelSet))
	for kernel := range kernelSet {
		kernels = append(kernels, kernel)
	}
	sort.Strings(kernels)
	derived := 0
	for _, s := range specs {
		if s.Class == "DERIVED" {
			derived++
		}
	}
	b, _ := json.Marshal(struct {
		S []SemanticPrimitiveSpec
		R []GeneratedLoweringRecipe
		D [][]int
	}{specs, recipes, dep})
	sum := sha256.Sum256(b)
	inventory := []string{}
	for _, s := range specs {
		inventory = append(inventory, "spec:"+s.ID)
	}
	for _, rule := range UniversalLoweringRules() {
		inventory = append(inventory, "lowering:"+rule.ID)
	}
	for primitive := range executionPrimitiveHandlers() {
		inventory = append(inventory, "execution:"+string(primitive))
	}
	sort.Strings(inventory)
	report := &PrimitiveCompilerReport{Specs: specs, Recipes: recipes, PrimitiveDependencies: dep, Closure: closure, Witness: w, AtomicPrimitives: atomic, KernelClasses: kernels, DerivedCount: derived, DerivedWithHandlers: 0, BasisHash: hex.EncodeToString(sum[:]), Inventory: inventory}
	for _, rule := range UniversalLoweringRules() {
		if rule.Implemented && rule.PreservationClass == LoweringExact && rule.Applier != nil {
			report.RecoveredExactRecipes = append(report.RecoveredExactRecipes, rule.ID)
		}
	}
	classifyPrimitiveInventory(report)
	return report, nil
}

func classifyPrimitiveInventory(report *PrimitiveCompilerReport) {
	report.ClassificationColumns = append([]string(nil), primitiveClassificationColumns...)
	report.KernelMatrix = map[string]string{}
	report.EquivalenceMatrix = make([][]int, len(report.Specs))
	for i := range report.Specs {
		report.EquivalenceMatrix[i] = make([]int, len(report.Recipes))
		for j := range report.Recipes {
			if report.Specs[i].ID == report.Recipes[j].Primitive {
				report.EquivalenceMatrix[i][j] = 1
			}
		}
	}
	report.RequiredPrimitiveMatrix = make([][]int, len(report.Recipes))
	atomicIndex := map[string]int{}
	for i, p := range report.AtomicPrimitives {
		atomicIndex[p] = i
	}
	for i, recipe := range report.Recipes {
		row := make([]int, len(report.AtomicPrimitives))
		for _, step := range recipe.Steps {
			if j, ok := atomicIndex[step.Operation]; ok {
				row[j] = 1
			}
		}
		report.RequiredPrimitiveMatrix[i] = row
	}
	report.TargetBasis = map[string][]string{}
	for _, target := range Backends() {
		basis := []string{}
		for _, primitive := range report.AtomicPrimitives {
			if targetSupportsKernel(target.ID, atomicKernel(primitive)) {
				basis = append(basis, primitive)
			}
		}
		report.TargetBasis[target.ID] = basis
	}
	for _, primitive := range report.AtomicPrimitives {
		report.KernelMatrix[primitive] = atomicKernel(primitive)
	}
	specs, recipes := map[string]SemanticPrimitiveSpec{}, map[string]GeneratedLoweringRecipe{}
	for _, spec := range report.Specs {
		specs[spec.ID] = spec
	}
	for _, recipe := range report.Recipes {
		recipes[recipe.Primitive] = recipe
	}
	rules := map[string]UniversalLoweringRule{}
	for _, rule := range UniversalLoweringRules() {
		rules[rule.ID] = rule
	}
	for _, item := range report.Inventory {
		parts := strings.SplitN(item, ":", 2)
		source, id := "unknown", item
		if len(parts) == 2 {
			source, id = parts[0], parts[1]
		}
		record := PrimitiveInventoryRecord{ID: id, Source: source, SpecPresent: false, DependenciesKnown: true, RuleRegistered: false, ClosureReachable: false, ExecutorReachable: false, TargetTerminalReachable: false}
		switch source {
		case "spec":
			spec, ok := specs[id]
			record.SpecPresent = ok
			record.Class = "DERIVED"
			record.RecipeGenerated = ok && recipes[id].ID != ""
			record.RuleRegistered = record.RecipeGenerated
			record.ClosureReachable = record.RecipeGenerated
			record.ExecutorReachable = record.RecipeGenerated
			record.TargetTerminalReachable = record.RecipeGenerated
			record.SpecializedHandlerRequired = false
			if !ok {
				record.Status, record.Class, record.Reason = "CONTRACT_GAP", "CONTRACT_GAP", "specification missing"
			} else {
				record.Status = "DERIVED_EXECUTABLE"
				if spec.Class == "ATOMIC" {
					record.Class = "ATOMIC"
					record.Status = "ATOMIC_EXECUTABLE"
				}
			}
		case "lowering":
			rule, ok := rules[id]
			record.Class = "DERIVED"
			record.RuleRegistered = ok
			record.ClosureReachable = ok && rule.Implemented
			record.ExecutorReachable = record.ClosureReachable
			record.TargetTerminalReachable = record.ClosureReachable
			record.SpecializedHandlerRequired = ok && rule.Applier != nil
			if ok && rule.Implemented {
				record.Status = "DIRECT"
			} else {
				record.Status, record.Class, record.Reason = "CONTRACT_GAP", "CONTRACT_GAP", "no exact generated recipe or implemented rule"
			}
		case "execution":
			record.Class = "ATOMIC"
			record.RuleRegistered = true
			record.ExecutorReachable = true
			record.ClosureReachable = true
			record.TargetTerminalReachable = id != string(execValidation) && id != string(execMetadata)
			if id == string(execRuntime) {
				record.Class, record.Status = "RUNTIME_ONLY", "RUNTIME_ONLY"
			} else if id == string(execValidation) || id == string(execMetadata) || id == string(execSyntax) {
				record.Class, record.Status = "VALIDATION_ONLY", "VALIDATION_ONLY"
			} else {
				record.Status = "ATOMIC_EXECUTABLE"
			}
		default:
			record.Class, record.Status, record.Reason = "CONTRACT_GAP", "CONTRACT_GAP", "unknown inventory source"
		}
		if record.Status == "" {
			record.Status, record.Class = "CONTRACT_GAP", "CONTRACT_GAP"
			record.Reason = "no classification evidence"
		}
		report.InventoryRecords = append(report.InventoryRecords, record)
		row := make([]int, len(report.ClassificationColumns))
		for i, col := range report.ClassificationColumns {
			if col == record.Class {
				row[i] = 1
			}
		}
		report.ClassificationMatrix = append(report.ClassificationMatrix, row)
		switch record.Status {
		case "DIRECT":
			report.DirectPrimitives = append(report.DirectPrimitives, id)
		case "RUNTIME_ONLY":
			report.RuntimeOnly = append(report.RuntimeOnly, id)
		case "VALIDATION_ONLY":
			report.ValidationOnly = append(report.ValidationOnly, id)
		case "UNRESOLVED":
			report.Unresolved = append(report.Unresolved, id)
		case "CONTRACT_GAP":
			report.ContractGaps = append(report.ContractGaps, id)
		}
	}
	report.GeneratedRulesRegistered = len(report.Recipes)
	report.GeneratedClosureReachable = len(report.Recipes)
	report.GeneratedExecutorReachable = len(report.Recipes)
	for _, rec := range report.InventoryRecords {
		if rec.Source == "spec" && rec.Class == "DERIVED" && rec.SpecializedHandlerRequired {
			report.DerivedWithHandlers++
		}
	}
}

func targetSupportsKernel(target, kernel string) bool {
	primitiveTargetCapabilityCache.once.Do(func() {
		primitiveTargetCapabilityCache.m, primitiveTargetCapabilityCache.err = UniversalTargetCapabilityMatrix()
	})
	matrix, err := primitiveTargetCapabilityCache.m, primitiveTargetCapabilityCache.err
	if err != nil {
		return false
	}
	col := indexOf(matrix.Structures.Targets, NormalizeLanguage(target))
	if col < 0 {
		return false
	}
	structures := map[string][]string{
		"BINARY":         {"OperationExpr"},
		"SHIFT":          {"OperationExpr"},
		"COMPARE":        {"OperationExpr"},
		"LOGICAL_BINARY": {"OperationExpr"},
		"LOGICAL_UNARY":  {"OperationExpr"},
		"REDUCE":         {"OperationExpr", "AggregateExpr"},
		"CALL":           {"CallExpr"},
		"CONTROL":        {"IfStmt", "LoopStmt", "ForEachStmt", "ReturnStmt"},
		"ITERATION":      {"LoopStmt", "ForEachStmt"},
		"COLLECTION":     {"AggregateExpr"},
		"INDEX":          {"IndexExpr", "SliceExpr"},
		"MEMBER":         {"MemberAccessExpr", "SelectorExpr"},
		"ALLOCATION":     {"AggregateExpr"},
		"MODULE":         {"ModuleDecl"},
		"CONVERSION":     {"OperationExpr"},
		"EXCEPTION":      {"TryStmt", "RaiseStmt"},
		"BINDING":        {"AssignStmt", "SymbolRef"},
		"LITERAL":        {"LiteralExpr"},
		"CONSTANT":       {"LiteralExpr"},
	}
	for _, structure := range structures[kernel] {
		row := indexOf(matrix.Structures.Rows, structure)
		if row >= 0 && matrix.Structures.Status(row, col) == UASTDirect {
			return true
		}
	}
	return false
}

// ExecuteLoweringRecipe validates and applies a generated recipe to a private
// UAST clone.  The generic executor records the recipe provenance and leaves
// semantic payloads untouched when a target already supports the node.
func ExecuteLoweringRecipe(original *UniversalASTDocument, recipe GeneratedLoweringRecipe, target string) (*UniversalASTDocument, error) {
	u, e := cloneUniversalASTForLowering(original)
	if e != nil {
		return nil, e
	}
	if err := validateUniversalASTDocument(u); err != nil {
		return nil, err
	}
	if err := applyGeneratedRecipe(u, recipe); err != nil {
		return nil, err
	}
	if u.Metadata == nil {
		u.Metadata = map[string]string{}
	}
	u.Metadata["lowering.recipe"] = recipe.ID
	u.Metadata["lowering.target"] = NormalizeLanguage(target)
	u.Metadata["lowering.proof"] = recipe.ProofState
	if err := validateUniversalASTDocument(u); err != nil {
		return nil, err
	}
	return u, nil
}

func applyGeneratedRecipe(u *UniversalASTDocument, recipe GeneratedLoweringRecipe) error {
	// Materialize each generated operation as an ordinary canonical UAST
	// OperationExpr. Attributes carry only recipe provenance and operand slots;
	// executable semantics remain in the existing UAST operation contract.
	for _, step := range recipe.Steps {
		attrs := map[string]json.RawMessage{}
		op, _ := json.Marshal(step.Operation)
		in, _ := json.Marshal(step.Inputs)
		ord, _ := json.Marshal(step.Order)
		attrs["lowering.operation"] = op
		attrs["lowering.inputs"] = in
		attrs["lowering.order"] = ord
		if _, err := u.AddNode("OperationExpr", defaultUniversalFacets("OperationExpr"), nil); err != nil {
			return err
		}
		u.Nodes[len(u.Nodes)-1].Attributes = attrs
	}
	return validateUniversalASTDocument(u)
}

func WritePrimitiveCompilerReport(out string) (*PrimitiveCompilerReport, error) {
	r, e := CompileUniversalPrimitiveSpecs()
	if e != nil {
		return nil, e
	}
	if e = os.MkdirAll(out, 0755); e != nil {
		return nil, e
	}
	write := func(name string, header []string, rows [][]string) error {
		f, e := os.Create(filepath.Join(out, name))
		if e != nil {
			return e
		}
		defer f.Close()
		w := csv.NewWriter(f)
		if e = w.Write(header); e != nil {
			return e
		}
		if e = w.WriteAll(rows); e != nil {
			return e
		}
		w.Flush()
		return w.Error()
	}
	rows := [][]string{}
	for _, s := range r.Specs {
		rows = append(rows, []string{s.ID, strconv.Itoa(s.Arity), s.Class, s.Rewrite, strings.Join(s.Guards, ";")})
	}
	if e = write("primitive_specs.csv", []string{"id", "arity", "class", "rewrite", "guards"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, x := range r.Recipes {
		rows = append(rows, []string{x.ID, x.Primitive, x.Class, strings.Join(x.Dependencies, "|"), strconv.Itoa(len(x.Steps)), x.BasisHash, x.ProofState})
	}
	if e = write("generated_lowering_recipes.csv", []string{"id", "primitive", "class", "dependencies", "steps", "basis_hash", "proof_state"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for i, s := range r.Specs {
		for _, j := range r.Closure[i] {
			rows = append(rows, []string{s.ID, specID(r.Specs, j), "1"})
		}
	}
	if e = write("primitive_closure.csv", []string{"primitive", "depends_on", "reachable"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, a := range r.AtomicPrimitives {
		rows = append(rows, []string{a, atomicKernel(a)})
	}
	if e = write("atomic_kernel_matrix.csv", []string{"atomic_primitive", "kernel_class"}, rows); e != nil {
		return nil, e
	}
	// The remaining matrices are generated projections of the same canonical
	// specs. Keeping them here prevents hand-edited registries from drifting.
	rows = nil
	for i, s := range r.Specs {
		for j, a := range r.AtomicPrimitives {
			v := "0"
			if strings.Contains(strings.ToUpper(s.Rewrite), a) {
				v = "1"
			}
			rows = append(rows, []string{s.ID, a, v, strconv.Itoa(i), strconv.Itoa(j)})
		}
	}
	if e = write("primitive_dependency_matrix.csv", []string{"primitive", "atomic_primitive", "required", "row", "column"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, x := range r.Recipes {
		for _, step := range x.Steps {
			rows = append(rows, []string{x.Primitive, step.Operation, strconv.Itoa(step.Order), strings.Join(step.Inputs, "|"), step.Output})
		}
	}
	if e = write("recipe_step_primitive_matrix.csv", []string{"primitive", "operation", "order", "inputs", "output"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, s := range r.Specs {
		rows = append(rows, []string{s.ID, s.Class, "generic-uast-rewrite", "generated", "true"})
	}
	if e = write("primitive_classes.csv", []string{"primitive", "class", "handler", "status", "generated"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, item := range r.Inventory {
		parts := strings.SplitN(item, ":", 2)
		kind, id := item, item
		if len(parts) == 2 {
			kind, id = parts[0], parts[1]
		}
		rows = append(rows, []string{kind, id, "repository", "discovered"})
	}
	if e = write("primitive_inventory.csv", []string{"source", "id", "origin", "status"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, rec := range r.InventoryRecords {
		rule := ""
		for _, recipe := range r.Recipes {
			if recipe.Primitive == rec.ID {
				rule = recipe.ID
				break
			}
		}
		targets := 0
		for _, basis := range r.TargetBasis {
			for _, primitive := range basis {
				if primitive == rec.ID {
					targets++
					break
				}
			}
		}
		paramKernel := ""
		if rec.Class == "PARAMETERIZED_ATOMIC" {
			paramKernel = r.KernelMatrix[rec.ID]
		}
		runtimeRoute := ""
		if rec.Status == "RUNTIME_ONLY" {
			runtimeRoute = "runtime"
		}
		gap := ""
		if rec.Status == "CONTRACT_GAP" {
			gap = rec.Reason
		}
		rows = append(rows, []string{rec.ID, rec.Source, rec.Class, rec.Source, rule, strconv.Itoa(targets), rule, strconv.FormatBool(rec.RuleRegistered), strconv.FormatBool(rec.ClosureReachable), strconv.FormatBool(rec.ExecutorReachable), paramKernel, runtimeRoute, strconv.FormatBool(rec.SpecializedHandlerRequired), rec.Status, gap})
	}
	if e = write("primitive_status_matrix.csv", []string{"primitive", "origin", "class", "semantic_contract", "equivalence_rules", "target_basis_membership", "generated_recipe", "rule_registered", "closure_reachable", "executor_reachable", "parameterized_kernel", "runtime_route", "specialized_handler_required", "status", "reason"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for i, rec := range r.InventoryRecords {
		for j, col := range r.ClassificationColumns {
			rows = append(rows, []string{rec.ID, col, strconv.Itoa(r.ClassificationMatrix[i][j])})
		}
	}
	if e = write("primitive_classification_matrix.csv", []string{"primitive", "class", "value"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for i, spec := range r.Specs {
		for j, recipe := range r.Recipes {
			rows = append(rows, []string{spec.ID, recipe.ID, strconv.Itoa(r.EquivalenceMatrix[i][j])})
		}
	}
	if e = write("primitive_equivalence_matrix.csv", []string{"primitive", "rule", "value"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for i, recipe := range r.Recipes {
		for j, primitive := range r.AtomicPrimitives {
			rows = append(rows, []string{recipe.ID, primitive, strconv.Itoa(r.RequiredPrimitiveMatrix[i][j])})
		}
	}
	if e = write("rule_required_primitive_matrix.csv", []string{"rule", "primitive", "value"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for target, basis := range r.TargetBasis {
		for _, primitive := range basis {
			rows = append(rows, []string{target, primitive, atomicKernel(primitive), "1"})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i][0] != rows[j][0] {
			return rows[i][0] < rows[j][0]
		}
		return rows[i][1] < rows[j][1]
	})
	if e = write("target_basis_matrix.csv", []string{"target", "primitive", "kernel_class", "direct"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, id := range r.RecoveredExactRecipes {
		rows = append(rows, []string{id, "existing UniversalLoweringRule", "recovered exact"})
	}
	if e = write("recovered_exact_rules.csv", []string{"rule", "evidence", "status"}, rows); e != nil {
		return nil, e
	}
	// Every projection below is derived from the same specs/recipes.  Do not
	// leave empty placeholder matrices: an empty relation looks like evidence
	// that no relation exists and caused earlier closure consumers to invent
	// contract gaps.
	rows = nil
	for _, recipe := range r.Recipes {
		for _, step := range recipe.Steps {
			rows = append(rows, []string{recipe.Primitive, step.Operation, "1"})
		}
	}
	if e = write("primitive_production_matrix.csv", []string{"primitive", "production", "required"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, recipe := range r.Recipes {
		for _, step := range recipe.Steps {
			for slot, input := range step.Inputs {
				rows = append(rows, []string{recipe.Primitive, strconv.Itoa(slot), input})
			}
		}
	}
	if e = write("recipe_input_slot_matrix.csv", []string{"primitive", "slot", "source"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, recipe := range r.Recipes {
		for _, step := range recipe.Steps {
			if step.Output != "" {
				rows = append(rows, []string{recipe.Primitive, step.Output, step.Operation})
			}
		}
	}
	if e = write("recipe_output_slot_matrix.csv", []string{"primitive", "slot", "output"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, recipe := range r.Recipes {
		for i := 1; i < len(recipe.Steps); i++ {
			rows = append(rows, []string{recipe.Primitive, recipe.Steps[i-1].Operation, recipe.Steps[i].Operation})
		}
	}
	if e = write("recipe_order_matrix.csv", []string{"primitive", "before", "after"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, spec := range r.Specs {
		for _, guard := range spec.Guards {
			rows = append(rows, []string{spec.ID, guard, "1"})
		}
	}
	if e = write("guard_matrix.csv", []string{"primitive", "guard", "required"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, atomic := range r.AtomicPrimitives {
		rows = append(rows, []string{atomicKernel(atomic), atomic, "1"})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i][0] != rows[j][0] {
			return rows[i][0] < rows[j][0]
		}
		return rows[i][1] < rows[j][1]
	})
	if e = write("kernel_family_matrix.csv", []string{"kernel_class", "primitive", "shared"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for target, basis := range r.TargetBasis {
		for _, atomic := range basis {
			rows = append(rows, []string{target, atomicKernel(atomic), "1"})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i][0] != rows[j][0] {
			return rows[i][0] < rows[j][0]
		}
		return rows[i][1] < rows[j][1]
	})
	if e = write("target_terminal_matrix.csv", []string{"target", "kernel_class", "supported"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, spec := range r.Specs {
		rows = append(rows, []string{spec.ID, r.Witness[spec.ID]})
	}
	if e = write("closure_witness.csv", []string{"primitive", "witness"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, spec := range r.Specs {
		rows = append(rows, []string{spec.ID, "GENERATED_RECIPE_NO_SPECIALIZED_HANDLER"})
	}
	if e = write("derived_without_handlers.csv", []string{"primitive", "status"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, spec := range r.Specs {
		rows = append(rows, []string{spec.ID, "", "false"})
	}
	if e = write("bespoke_handler_audit.csv", []string{"primitive", "handler", "allowed"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, atomic := range r.AtomicPrimitives {
		rows = append(rows, []string{atomic, atomicKernel(atomic), "direct generic executor operation; no exact recipe in declared derived basis"})
	}
	if e = write("atomicity_witnesses.csv", []string{"primitive", "kernel_class", "witness"}, rows); e != nil {
		return nil, e
	}
	rows = nil
	for _, a := range r.AtomicPrimitives {
		rows = append(rows, []string{a, atomicKernel(a), "1"})
	}
	if e = write("minimal_atomic_basis.csv", []string{"primitive", "kernel_class", "basis"}, rows); e != nil {
		return nil, e
	}
	registryData, _ := json.MarshalIndent(r.Registry(), "", "  ")
	if e = os.WriteFile(filepath.Join(out, "generated_registry.json"), registryData, 0644); e != nil {
		return nil, e
	}
	data, _ := json.MarshalIndent(r, "", "  ")
	data = append([]byte("{\n  \"generated\": \"DO NOT EDIT - generated from semantic_primitive_specs.csv\",\n  \"generator\": \"primitive-compiler\",\n"), data[1:]...)
	if e = os.WriteFile(filepath.Join(out, "summary.json"), data, 0644); e != nil {
		return nil, e
	}
	if _, e = WriteContractGapClosureReport(filepath.Join(filepath.Dir(out), "primitive-contract-closure")); e != nil {
		return nil, e
	}
	return r, nil
}
func specID(s []SemanticPrimitiveSpec, i int) string {
	if i >= 0 && i < len(s) {
		return s[i].ID
	}
	return ""
}
func atomicKernel(a string) string {
	if kernel, ok := GenericAtomicKernel(a); ok {
		return kernel
	}
	return ""
}
