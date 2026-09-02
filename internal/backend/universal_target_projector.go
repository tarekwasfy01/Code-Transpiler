package backend

import (
	"fmt"
	"sort"
	"strings"
)

// TargetSpec is declarative target syntax and capability metadata.  It never
// carries source-program semantics: those remain exclusively in UAST.
type TargetSpec struct {
	ID                  string                          `json:"id"`
	Aliases             []string                        `json:"aliases"`
	Capabilities        []string                        `json:"capabilities"`
	StatementTerminator string                          `json:"statement_terminator"`
	ChildSeparator      string                          `json:"child_separator"`
	Indent              string                          `json:"indent"`
	BlockOpen           string                          `json:"block_open"`
	BlockClose          string                          `json:"block_close"`
	Hooks               []string                        `json:"hooks"`
	Operators           map[string]TargetOperatorSpec   `json:"operators"`
	Types               map[string]string               `json:"types"`
	Literals            TargetLiteralSpec               `json:"literals"`
	Naming              TargetNamingSpec                `json:"naming"`
	Imports             TargetImportSpec                `json:"imports"`
	TypedOperations     TargetTypedOperationSpec        `json:"typed_operations"`
	ProjectionForms     map[string]TargetProjectionSpec `json:"projection_forms"`
	SyntaxTokens        map[string]string               `json:"syntax_tokens"`
}

// TargetProjectionSpec is a syntax/runtime declaration for a whole family of
// schema-derived projection contracts. It contains no source-program data.
type TargetProjectionSpec struct {
	Mode         PreservationMode `json:"mode"`
	Requirements []string         `json:"requirements,omitempty"`
}

type TargetOperatorSpec struct {
	Spelling      string `json:"spelling"`
	Precedence    int    `json:"precedence"`
	Associativity string `json:"associativity"`
	Fixity        string `json:"fixity"`
}
type TargetLiteralSpec struct {
	True        string `json:"true"`
	False       string `json:"false"`
	Null        string `json:"null"`
	StringQuote string `json:"string_quote"`
	StringWrap  string `json:"string_wrap"`
	NumberWrap  string `json:"number_wrap"`
	NumberRule  string `json:"number_rule"`
}
type TargetNamingSpec struct {
	CaseSensitive    bool   `json:"case_sensitive"`
	StyleInsensitive bool   `json:"style_insensitive"`
	GeneratedPrefix  string `json:"generated_prefix"`
}
type TargetImportSpec struct {
	RuntimeRequirement string `json:"runtime_requirement"`
	Ordering           string `json:"ordering"`
	Prelude            string `json:"-"`
}
type TargetTypedOperationSpec struct{ Form, Runtime, ArgumentsOpen, ArgumentsClose, SignedTrue, SignedFalse string }

type HelperSpec struct {
	ID, Target, RequiredCapability, Source string
	Dependencies                           []string
}

type PreservationMode string

const (
	PreservationDirect  PreservationMode = "DIRECT"
	PreservationRewrite PreservationMode = "REWRITE"
	PreservationHelper  PreservationMode = "HELPER"
	PreservationEmulate PreservationMode = "EMULATE"
	PreservationRuntime PreservationMode = "RUNTIME"
	// PreservationError is an explicit semantic refusal.  It is deliberately
	// distinct from an absent matrix entry: every target × UASF cell must have
	// a deterministic result.
	PreservationError PreservationMode = "ERROR"
)

type PreservationRule struct {
	Capability, Target          string
	Mode                        PreservationMode
	Preconditions, Requirements []string
	Handler, Test               string
}
type PreservationRegistry struct{ Rules []PreservationRule }

func (r PreservationRegistry) Solve(target, capability string) (PreservationRule, bool) {
	for _, mode := range []PreservationMode{PreservationDirect, PreservationRewrite, PreservationHelper, PreservationEmulate, PreservationRuntime} {
		for _, rule := range r.Rules {
			if rule.Target == target && rule.Capability == capability && rule.Mode == mode {
				return rule, true
			}
		}
	}
	return PreservationRule{}, false
}

type RequirementKind string

const (
	RequirementImport    RequirementKind = "IMPORT"
	RequirementHelper    RequirementKind = "HELPER"
	RequirementEmulation RequirementKind = "EMULATION"
	RequirementRuntime   RequirementKind = "RUNTIME"
)

type Requirement struct {
	ID                   string
	Kind                 RequirementKind
	Target               string
	Dependencies         []string
	Preconditions, Tests []string
	Emit                 string
}
type RequirementRegistry struct{ Rules map[string]Requirement }

func (r RequirementRegistry) Resolve(ids []string) ([]Requirement, error) {
	state, out := map[string]int{}, []Requirement{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("CYCLIC_REQUIREMENT_DEPENDENCY: %s", id)
		}
		if state[id] == 2 {
			return nil
		}
		rule, ok := r.Rules[id]
		if !ok {
			return fmt.Errorf("unknown requirement %q", id)
		}
		state[id] = 1
		for _, dep := range rule.Dependencies {
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[id] = 2
		out = append(out, rule)
		return nil
	}
	for _, id := range uniqueSorted(ids) {
		if err := visit(id); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func DefaultPreservationRegistry() PreservationRegistry {
	// These are the proven paths used by the direct UAST emitter.  R emits the
	// common semantic core in its own syntax; every other registered target
	// emits that exact core through its checked-in target runtime prelude.  Do
	// not add a rule here merely because a target language has comparable
	// syntax: a rule is a product path and therefore must name its handler and
	// regression test.
	rules := []PreservationRule{}
	for _, target := range Backends() {
		mode := PreservationRuntime
		handler := "UniversalTargetEngine+targetPrelude"
		if target.ID == "r" {
			mode = PreservationDirect
			handler = "UniversalTargetEngine"
		}
		rules = append(rules, PreservationRule{Capability: "uast.core", Target: target.ID, Mode: mode, Handler: handler, Test: "TestUniversalBackendUASTOnlyTargetMatrix"})
	}
	return PreservationRegistry{Rules: rules}
}
func DefaultRequirementRegistry() RequirementRegistry {
	return RequirementRegistry{Rules: map[string]Requirement{}}
}

// RuntimeModule is a modular, source-independent target support declaration.
// It is registered as a normal RUNTIME requirement and is emitted only when a
// preservation rule explicitly requests one.
type RuntimeModule struct {
	ID, Target                                                        string
	ProvidedCapabilities, RequiredCapabilities, Dependencies, Imports []string
	Emit                                                              Doc
	Tests                                                             []string
}
type RuntimeModuleRegistry struct{ Modules map[string]RuntimeModule }

func (r RuntimeModuleRegistry) RequirementRegistry() RequirementRegistry {
	rules := map[string]Requirement{}
	for id, module := range r.Modules {
		rules["runtime."+id] = Requirement{ID: "runtime." + id, Kind: RequirementRuntime, Target: module.Target, Dependencies: append([]string(nil), module.Dependencies...), Tests: append([]string(nil), module.Tests...)}
	}
	return RequirementRegistry{Rules: rules}
}
func (r RuntimeModuleRegistry) Provides(target, capability string) bool {
	for _, module := range r.Modules {
		if module.Target == target {
			for _, provided := range module.ProvidedCapabilities {
				if provided == capability {
					return true
				}
			}
		}
	}
	return false
}

type HelperRequirement struct{ ID string }
type UniversalHelperResolver struct{}

func (UniversalHelperResolver) Resolve(requirements []HelperRequirement, registry map[string]HelperSpec) ([]HelperSpec, error) {
	state, out := map[string]int{}, []HelperSpec{}
	var visit func(string) error
	visit = func(id string) error {
		if state[id] == 1 {
			return fmt.Errorf("CYCLIC_HELPER_DEPENDENCY: %s", id)
		}
		if state[id] == 2 {
			return nil
		}
		spec, ok := registry[id]
		if !ok {
			return fmt.Errorf("unknown helper %q", id)
		}
		state[id] = 1
		for _, dependency := range spec.Dependencies {
			if err := visit(dependency); err != nil {
				return err
			}
		}
		state[id] = 2
		out = append(out, spec)
		return nil
	}
	for _, requirement := range requirements {
		if err := visit(requirement.ID); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func renderTargetHelpers(raw []string) string {
	seen, out := map[string]bool{}, make([]string, 0, len(raw))
	for _, helper := range raw {
		if helper != "" && !seen[helper] {
			seen[helper] = true
			out = append(out, helper)
		}
	}
	return strings.Join(out, "\n")
}

func (g *targetGen) requiredHelperSources() []string {
	out := make([]string, 0, len(g.helperRequirements))
	for _, id := range g.helperRequirements {
		if source := g.helperSources[id]; source != "" {
			out = append(out, source)
		}
	}
	return out
}

func universalOperatorSpecs() map[string]TargetOperatorSpec {
	// These contracts drive the shared parenthesis engine.  Existing emitters
	// currently route the operation through each target's tested runtime hook.
	return map[string]TargetOperatorSpec{
		"||": {Spelling: "||", Precedence: 10, Associativity: "left", Fixity: "infix"}, "&&": {Spelling: "&&", Precedence: 20, Associativity: "left", Fixity: "infix"},
		"==": {Spelling: "==", Precedence: 30, Associativity: "none", Fixity: "infix"}, "!=": {Spelling: "!=", Precedence: 30, Associativity: "none", Fixity: "infix"},
		"<": {Spelling: "<", Precedence: 40, Associativity: "none", Fixity: "infix"}, "<=": {Spelling: "<=", Precedence: 40, Associativity: "none", Fixity: "infix"}, ">": {Spelling: ">", Precedence: 40, Associativity: "none", Fixity: "infix"}, ">=": {Spelling: ">=", Precedence: 40, Associativity: "none", Fixity: "infix"},
		"+": {Spelling: "+", Precedence: 50, Associativity: "left", Fixity: "infix"}, "-": {Spelling: "-", Precedence: 50, Associativity: "left", Fixity: "infix"}, "*": {Spelling: "*", Precedence: 60, Associativity: "left", Fixity: "infix"}, "/": {Spelling: "/", Precedence: 60, Associativity: "left", Fixity: "infix"},
		"unary+": {Spelling: "+", Precedence: 70, Associativity: "right", Fixity: "prefix"}, "unary-": {Spelling: "-", Precedence: 70, Associativity: "right", Fixity: "prefix"},
	}
}

// NeedsParentheses is target-neutral: TargetSpec supplies precedence and
// associativity while the algorithm uses only operand role.
func NeedsParentheses(parent, child TargetOperatorSpec, role string) bool {
	if parent.Fixity == "" || child.Fixity == "" {
		return false
	}
	if child.Precedence < parent.Precedence {
		return true
	}
	if child.Precedence > parent.Precedence {
		return false
	}
	if parent.Associativity == "none" {
		return true
	}
	return (role == "right" && parent.Associativity == "left") || (role == "left" && parent.Associativity == "right")
}

// RegisteredTargetSpecs is derived from the target registry, keeping the set
// of targets single-sourced.  The hook names document existing syntax helpers
// that are deliberately reused during this migration.
func RegisteredTargetSpecs() []TargetSpec {
	specs := make([]TargetSpec, 0, len(Backends()))
	for _, backend := range Backends() {
		noTerminator := map[string]bool{"python": true, "julia": true, "nim": true, "kotlin": true, "swift": true}
		terminator := ";"
		if noTerminator[backend.ID] {
			terminator = ""
		}
		styleInsensitive := backend.ID == "nim"
		prefix := "r2_"
		if styleInsensitive {
			prefix = "r2ms"
		}
		specs = append(specs, TargetSpec{
			ID: backend.ID, Aliases: append([]string(nil), backend.Aliases...), Capabilities: append([]string(nil), backend.Capabilities...),
			StatementTerminator: terminator, ChildSeparator: ", ", Indent: indentUnit(backend.ID), BlockOpen: targetBlockOpen(backend.ID), BlockClose: targetBlockClose(backend.ID),
			Hooks: []string{"targetPrelude", "emitDispatch", "mainOpen", "mainClose"}, Operators: universalOperatorSpecs(),
			Types:    map[string]string{"number": "RValue", "boolean": "RValue", "string": "RValue", "null": "RValue"},
			Literals: targetLiteralSpecExisting(backend.ID),
			Naming:   TargetNamingSpec{CaseSensitive: true, StyleInsensitive: styleInsensitive, GeneratedPrefix: prefix}, Imports: TargetImportSpec{RuntimeRequirement: "r2-runtime", Ordering: "lexical", Prelude: targetPreludeExisting(backend.ID)},
			TypedOperations: targetTypedOperationSpecExisting(backend.ID), ProjectionForms: targetProjectionFormsExisting(backend.ID), SyntaxTokens: targetSyntaxTokensExisting(backend.ID),
		})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	return specs
}

// targetSyntaxTokensExisting is the declarative harvest of the tokens that
// the proven direct-UAST emitter already emits. It deliberately contains only
// syntax parameters, never source-program semantics or UAST data.
func targetSyntaxTokensExisting(target string) map[string]string {
	tokens := map[string]string{
		"keyword.if": "if", "keyword.else": "else", "keyword.for": "for", "keyword.return": "return",
		"keyword.break": "break", "keyword.continue": "continue", "keyword.in": "in",
	}
	// Declarative native tokens used by the shared backend emitter. Syntax and
	// template capabilities are layout contracts; exception uses the target's
	// established panic/throw spelling where available.
	switch target {
	case "go", "rust", "zig", "nim", "python", "julia":
		tokens["keyword.panic"] = "panic"
	case "c", "cpp", "csharp", "java", "kotlin", "swift":
		tokens["keyword.throw"] = "throw"
	case "r":
		tokens["keyword.throw"] = "stop"
	}
	switch target {
	case "go":
		tokens["keyword.while"] = "for"
		tokens["loop.iterator"] = "range"
	case "csharp":
		tokens["keyword.while"] = "while"
		tokens["keyword.foreach"] = "foreach"
	case "python", "julia", "nim":
		tokens["keyword.while"] = "while"
		tokens["block.style"] = "indent"
	case "c", "cpp", "java", "kotlin", "rust", "swift", "zig":
		tokens["keyword.while"] = "while"
		tokens["block.style"] = "delimited"
	case "r":
		tokens["keyword.while"] = "while"
		tokens["block.style"] = "delimited"
	default:
		tokens["keyword.while"] = "while"
	}
	return tokens
}

// targetBlockOpen and targetBlockClose are syntax parameters, not semantic
// decisions. They let the generic Doc recipe executor use the same target
// layout facts as the established direct renderer.
func targetBlockOpen(target string) string {
	switch target {
	case "python", "nim":
		return ":"
	case "julia":
		return ""
	default:
		return "{"
	}
}

func targetBlockClose(target string) string {
	switch target {
	case "python", "nim":
		return ""
	case "julia":
		return "end"
	default:
		return "}"
	}
}

func targetProjectionFormsExisting(target string) map[string]TargetProjectionSpec {
	coreMode := PreservationRuntime
	if target == "r" {
		coreMode = PreservationDirect
	}
	return map[string]TargetProjectionSpec{
		projectionFormCore:      {Mode: coreMode, Requirements: []string{"targetPrelude"}},
		projectionFormAggregate: {Mode: coreMode, Requirements: []string{"targetPrelude", "runtime.list"}},
		projectionFormVariable:  {Mode: PreservationDirect},
		projectionFormDeclGroup: {Mode: coreMode, Requirements: []string{"targetPrelude"}},
		projectionFormAtomic:    {Mode: PreservationDirect},
		projectionFormStatement: {Mode: PreservationDirect},
		// Metadata contracts are consumed before source emission and therefore
		// need no target prelude or target syntax.
		projectionFormMetadata: {Mode: PreservationDirect},
		// The fallback is a syntax-complete, explicit runtime contract.  Unknown
		// semantics are delegated to the existing dispatcher, which reports an
		// unsupported operation rather than silently dropping the UAST node.
		projectionFormFallback: {Mode: PreservationRuntime, Requirements: []string{"targetPrelude"}},
	}
}

// Typed-operation forms are syntax-only target contracts. The operation and
// its integer/type semantics remain the proved UAST contract.
func targetTypedOperationSpecExisting(target string) TargetTypedOperationSpec {
	s := TargetTypedOperationSpec{Form: "unsupported"}
	switch target {
	case "go":
		s = TargetTypedOperationSpec{"standard", "rExact", "[]any{", "}", "true", "false"}
	case "rust":
		s = TargetTypedOperationSpec{"standard", "r_exact", "vec![", "]", "true", "false"}
	case "cpp":
		s = TargetTypedOperationSpec{"standard", "r_exact", "{", "}", "true", "false"}
	case "java":
		s = TargetTypedOperationSpec{"standard", "RExact.apply", "new Object[]{", "}", "true", "false"}
	case "csharp":
		s = TargetTypedOperationSpec{"standard", "RExact.Apply", "new object[]{", "}", "true", "false"}
	case "c":
		s = TargetTypedOperationSpec{Form: "c", Runtime: "r_exact", SignedTrue: "1", SignedFalse: "0"}
	case "python":
		s = TargetTypedOperationSpec{"standard", "r_exact", "[", "]", "True", "False"}
	case "r":
		s = TargetTypedOperationSpec{"standard", "r_exact", "list(", ")", "TRUE", "FALSE"}
	case "julia":
		s = TargetTypedOperationSpec{"standard", "r_exact", "Any[", "]", "true", "false"}
	case "nim":
		s = TargetTypedOperationSpec{"standard", "rExact", "@[", "]", "true", "false"}
	case "zig":
		s = TargetTypedOperationSpec{"standard", "rExact", "&[_]RValue{", "}", "true", "false"}
	case "kotlin":
		s = TargetTypedOperationSpec{"standard", "rExact", "arrayOf(", ")", "true", "false"}
	case "swift":
		s = TargetTypedOperationSpec{"standard", "rExact", "[", "]", "true", "false"}
	}
	return s
}

func targetLiteralSpecExisting(target string) TargetLiteralSpec {
	s := TargetLiteralSpec{True: "true", False: "false", Null: "nil", StringQuote: "\"", StringWrap: "%s", NumberWrap: "%s", NumberRule: "plain"}
	switch target {
	case "python":
		s.True, s.False, s.Null = "True", "False", "None"
	case "julia":
		s.Null = "nothing"
	case "nim":
		s.True, s.False, s.Null, s.StringWrap, s.NumberWrap, s.NumberRule = "rBool(true)", "rBool(false)", "rNull()", "rStr(%s)", "rNum(float64(%s))", "wrapped"
	case "go":
		s.NumberRule = "go_float64_integer"
	case "rust":
		s.True, s.False, s.Null, s.StringWrap, s.NumberWrap, s.NumberRule = "RValue::Bool(true)", "RValue::Bool(false)", "RValue::Null", "RValue::Str(%s.to_string())", "RValue::Num(%s)", "rust_number"
	case "cpp":
		s.True, s.False, s.Null, s.StringWrap, s.NumberWrap, s.NumberRule = "RValue(true)", "RValue(false)", "RValue::null()", "RValue(%s)", "RValue((double)%s)", "wrapped"
	case "c":
		s.True, s.False, s.Null, s.StringWrap, s.NumberWrap, s.NumberRule = "r_bool(1)", "r_bool(0)", "r_null()", "r_str(%s)", "r_num((double)%s)", "wrapped"
	case "zig":
		s.True, s.False, s.Null, s.StringWrap, s.NumberWrap, s.NumberRule = "RValue{ .boolean = true }", "RValue{ .boolean = false }", "RValue.null", "RValue{ .str = %s }", "RValue{ .num = %s }", "wrapped"
	case "csharp":
		s.Null, s.NumberWrap, s.NumberRule = "R2.Null", "new R2.Value((double)%s)", "wrapped"
	case "java":
		s.Null, s.NumberWrap, s.NumberRule = "R2.RValue.NULL", "new R2.RValue((double)%s)", "wrapped"
	case "kotlin":
		s.Null, s.NumberWrap, s.NumberRule = "RValue.Null", "RValue.Num(%s)", "kotlin_number"
	case "swift":
		s.Null, s.NumberWrap = "RValue.null", "RValue.num(%s)"
	}
	return s
}

type UniversalTargetNameResolver struct{}

func (UniversalTargetNameResolver) Resolve(preferred string, spec TargetSpec) string {
	if strings.HasPrefix(preferred, "\x00") {
		return preferred
	}
	if spec.Naming.StyleInsensitive {
		return fmt.Sprintf("%s%x", spec.Naming.GeneratedPrefix, preferred)
	}
	return safeName(preferred)
}

func targetSpec(id string) (TargetSpec, bool) {
	id = NormalizeLanguage(id)
	for _, spec := range RegisteredTargetSpecs() {
		if spec.ID == id {
			return spec, true
		}
	}
	return TargetSpec{}, false
}

func targetPrelude(target string) string {
	if spec, ok := targetSpec(target); ok {
		return spec.Imports.Prelude
	}
	return targetPreludeExisting(target)
}

// Doc is a syntax-only, ephemeral rendering tree.  It contains no types,
// symbols, UAST nodes, or semantic meaning.
type Doc interface{ doc() }
type DocText struct{ Text string }
type DocConcat struct{ Parts []Doc }
type DocIndent struct {
	By   string
	Body Doc
}
type DocHardLine struct{}

func (DocText) doc()     {}
func (DocConcat) doc()   {}
func (DocIndent) doc()   {}
func (DocHardLine) doc() {}

// UniversalFormatter knows only document layout.  Semantic analysis is
// complete before a Doc is constructed.
type UniversalFormatter struct {
	Indent  string
	Newline string
}

func (f UniversalFormatter) Format(doc Doc) string {
	if f.Indent == "" {
		f.Indent = "    "
	}
	if f.Newline == "" {
		f.Newline = "\n"
	}
	var b strings.Builder
	var write func(Doc, string)
	write = func(n Doc, prefix string) {
		switch x := n.(type) {
		case DocText:
			b.WriteString(x.Text)
		case DocConcat:
			for _, part := range x.Parts {
				write(part, prefix)
			}
		case DocIndent:
			write(x.Body, prefix+x.By)
		case DocHardLine:
			b.WriteString(f.Newline)
			b.WriteString(prefix)
		}
	}
	write(doc, "")
	return b.String()
}

type RequiredSemantics struct{ Structures, Relations, Facets, Fields []string }
type TargetCapabilityDecision struct {
	Direct, Rewrite, Helper, Emulate, Runtime, Unsupported []string
}

func uniqueSorted(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		seen[value] = true
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

// RequiredSemanticsFromUAST reads the canonical graph without source-language
// provenance or a legacy compatibility view.
func RequiredSemanticsFromUAST(u *UniversalASTDocument) RequiredSemantics {
	r := RequiredSemantics{}
	for _, node := range u.Nodes {
		r.Structures = append(r.Structures, node.StructuralKind)
		r.Facets = append(r.Facets, node.SemanticFacets...)
		for field := range node.Fields {
			r.Fields = append(r.Fields, field)
		}
	}
	for _, relation := range u.Relations {
		r.Relations = append(r.Relations, relation.Kind)
	}
	r.Structures, r.Relations, r.Facets, r.Fields = uniqueSorted(r.Structures), uniqueSorted(r.Relations), uniqueSorted(r.Facets), uniqueSorted(r.Fields)
	return r
}

// UniversalTargetProjector performs the two-pass target pipeline.  It has no
// source-language input and delegates only syntax emission to the tested
// existing UAST emitter while the declarative TargetSpec controls selection.
type UniversalTargetProjector struct{}

func (UniversalTargetProjector) Analyze(u *UniversalASTDocument, spec TargetSpec) (RequiredSemantics, TargetCapabilityDecision, error) {
	if u == nil {
		return RequiredSemantics{}, TargetCapabilityDecision{}, fmt.Errorf("missing universal AST")
	}
	if _, ok := DefaultPreservationRegistry().Solve(spec.ID, "uast.core"); !ok {
		return RequiredSemantics{}, TargetCapabilityDecision{Unsupported: []string{"uast.core"}}, fmt.Errorf("UNSUPPORTED_TARGET_SEMANTICS: %s", spec.ID)
	}
	if err := validateUASTTargetCapabilities(u, spec.ID); err != nil {
		return RequiredSemantics{}, TargetCapabilityDecision{Unsupported: []string{err.Error()}}, err
	}
	if err := validateUASTTargetPreservation(u, spec.ID); err != nil {
		return RequiredSemantics{}, TargetCapabilityDecision{Unsupported: []string{err.Error()}}, err
	}
	required := RequiredSemanticsFromUAST(u)
	capabilities, err := UniversalTargetCapabilityMatrix()
	if err != nil {
		return RequiredSemantics{}, TargetCapabilityDecision{}, err
	}
	decision := TargetCapabilityDecision{}
	appendCapabilityPlane := func(values []string, plane UASTCapabilityPlane) {
		col := indexOf(plane.Targets, spec.ID)
		for _, value := range values {
			row := indexOf(plane.Rows, value)
			if row < 0 || col < 0 {
				decision.Unsupported = append(decision.Unsupported, value)
				continue
			}
			switch plane.Status(row, col) {
			case UASTDirect:
				decision.Direct = append(decision.Direct, value)
			case UASTLowering:
				decision.Rewrite = append(decision.Rewrite, value)
			case UASTRuntimeRequired:
				decision.Runtime = append(decision.Runtime, value)
			default:
				decision.Unsupported = append(decision.Unsupported, value)
			}
		}
	}
	appendCapabilityPlane(required.Structures, capabilities.Structures)
	appendCapabilityPlane(required.Relations, capabilities.Relations)
	appendCapabilityPlane(required.Fields, capabilities.Fields)
	preservation, err := UniversalTargetPreservationMatrix()
	if err != nil {
		return RequiredSemantics{}, TargetCapabilityDecision{}, err
	}
	col := indexOf(preservation.Targets, spec.ID)
	for _, facet := range required.Facets {
		row := indexOf(preservation.Capabilities, facet)
		if row < 0 || col < 0 {
			decision.Unsupported = append(decision.Unsupported, facet)
			continue
		}
		switch preservation.Status(row, col) {
		case PreservationDirect:
			decision.Direct = append(decision.Direct, facet)
		case PreservationRewrite:
			decision.Rewrite = append(decision.Rewrite, facet)
		case PreservationHelper:
			decision.Helper = append(decision.Helper, facet)
		case PreservationEmulate:
			decision.Emulate = append(decision.Emulate, facet)
		case PreservationRuntime:
			decision.Runtime = append(decision.Runtime, facet)
		default:
			decision.Unsupported = append(decision.Unsupported, facet)
		}
	}
	decision.Direct = uniqueSorted(decision.Direct)
	decision.Rewrite = uniqueSorted(decision.Rewrite)
	decision.Helper = uniqueSorted(decision.Helper)
	decision.Emulate = uniqueSorted(decision.Emulate)
	decision.Runtime = uniqueSorted(decision.Runtime)
	decision.Unsupported = uniqueSorted(decision.Unsupported)
	return required, decision, nil
}

func validateUASTTargetPreservation(u *UniversalASTDocument, target string) error {
	m, err := UniversalTargetPreservationMatrix()
	if err != nil {
		return err
	}
	col := indexOf(m.Targets, NormalizeLanguage(target))
	if col < 0 {
		return fmt.Errorf("unknown target %q", target)
	}
	bad := []string{}
	for _, node := range u.Nodes {
		for _, facet := range node.SemanticFacets {
			row := indexOf(m.Capabilities, facet)
			if row < 0 || m.Status(row, col) == PreservationError {
				bad = append(bad, facet)
			}
		}
	}
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf("target %q has no preservation path for UAST facets %v", target, uniqueSorted(bad))
}

func (p UniversalTargetProjector) Project(u *UniversalASTDocument, spec TargetSpec) (Doc, error) {
	if _, _, err := p.Analyze(u, spec); err != nil {
		return nil, err
	}
	if err := validateUASTStructureProjectionContracts(u); err != nil {
		return nil, err
	}
	if err := validateUASTTargetSyntaxTemplates(u, spec); err != nil {
		return nil, err
	}
	graph, err := newUASTExecutionGraph(u)
	if err != nil {
		return nil, err
	}
	source, err := generateTargetFromUniversalExisting(u.Evaluation, spec.ID, graph)
	if err != nil {
		return nil, err
	}
	// Every successful DIRECT projection crosses the shared native emitter
	// contract.  The renderer above has already produced target syntax from the
	// UAST graph; this final document recipe is syntax-only and cannot add a
	// runtime prelude, dispatch call, or helper.
	return EmitNativeDocument(spec, source)
}

func (p UniversalTargetProjector) Emit(u *UniversalASTDocument, target string) (string, error) {
	spec, ok := targetSpec(target)
	if !ok {
		return "", fmt.Errorf("unknown target %q", target)
	}
	doc, err := p.Project(u, spec)
	if err != nil {
		return "", err
	}
	return (UniversalFormatter{Indent: spec.Indent, Newline: "\n"}).Format(doc), nil
}

// generateTargetFromUniversal is the stable direct-UAST entry point used by
// existing callers and tests.  It deliberately has no source-language input.
func generateTargetFromUniversal(evaluation, target string, graph *uastExecutionGraph) (string, error) {
	if graph == nil || graph.document == nil {
		return "", fmt.Errorf("missing UAST execution graph")
	}
	if graph.document.Evaluation != evaluation {
		return "", fmt.Errorf("UAST evaluation contract mismatch")
	}
	return (UniversalTargetProjector{}).Emit(graph.document, target)
}
