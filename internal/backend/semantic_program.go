package backend

import (
	"fmt"
	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
	"strconv"
	"strings"
)

// SemanticProgram owns the executable tree. Canonical R is now a diagnostic
// serialization, not the carrier of evaluation semantics. Legacy values retain
// the binary64 contract; typed operations use an explicit exact-scalar contract
// and integer-width domains. Unknown types/effects remain explicit in matrices.
type SemanticProgram struct {
	nodeSources map[int]SemanticSourceSpan
	sourceTree  []byte
	// CompatibilityR is a generated diagnostic view retained for callers that
	// still execute the matrix language's canonical compatibility form. It is
	// produced by the modern MatrixIR frontend; it is never used as semantic
	// input by emitters or by TranspileCore.
	CompatibilityR   string                `json:"-"`
	Body             *BlockStmt            `json:"-"`
	Evaluation       string                `json:"evaluation"`
	ValueModel       string                `json:"value_model"`
	IndexBase        int                   `json:"index_base"`
	Types            SemanticTypeContract  `json:"type_contract"`
	Origin           SemanticOrigin        `json:"origin"`
	Metadata         map[string]string     `json:"metadata,omitempty"`
	Extensions       map[string]any        `json:"extensions,omitempty"`
	Contracts        SemanticContracts     `json:"contracts,omitempty"`
	Dialects         []SemanticDialect     `json:"dialects,omitempty"`
	SemanticFeatures *SemanticFeatureModel `json:"semantic_features,omitempty"`
	UniversalAST     *UniversalASTDocument `json:"universal_ast,omitempty"`
	Evidence         SemanticEvidence      `json:"evidence"`
}

// SemanticContracts state assertions made by a producer. They are data, not
// executable checks: backends must reject contracts they cannot preserve.
type SemanticContracts struct {
	Requires   []string `json:"requires,omitempty"`
	Ensures    []string `json:"ensures,omitempty"`
	Invariants []string `json:"invariants,omitempty"`
}

// SemanticDialect keeps specialist domains out of the universal core. A
// backend may emit it only after declaring every required capability.
type SemanticDialect struct {
	Name         string                     `json:"name"`
	Capabilities []string                   `json:"capabilities"`
	Operations   []SemanticDialectOperation `json:"operations"`
}
type SemanticDialectOperation struct {
	ID         string         `json:"id"`
	Kind       string         `json:"kind"`
	Attributes map[string]any `json:"attributes,omitempty"`
}

type SemanticOrigin struct {
	SourceLanguage string   `json:"source_language"`
	SourceVersion  string   `json:"source_version,omitempty"`
	EntryPoint     string   `json:"entry_point"`
	Modules        []string `json:"modules"`
}

// SemanticTypeContract is deliberately explicit about what is not yet known.
// It is part of the language-neutral program contract, not a target profile.
type SemanticTypeContract struct {
	SchemaVersion int    `json:"schema_version"`
	Numeric       string `json:"numeric"`
	IntegerWidth  string `json:"integer_width"`
	Text          string `json:"text"`
	Truth         string `json:"truth"`
	Null          string `json:"null"`
	Collection    string `json:"collection"`
	Pointer       string `json:"pointer"`
	Ownership     string `json:"ownership"`
	ABI           string `json:"abi"`
}

func defaultSemanticTypeContract() SemanticTypeContract {
	return SemanticTypeContract{1, "binary64", "unknown", "utf8", "r_compatible", "explicit", "dynamic_vector", "unknown", "unknown", "unknown"}
}

type SemanticNode struct {
	ID     int    `json:"id"`
	Kind   string `json:"kind"`
	Symbol string `json:"symbol,omitempty"`
	Scope  int    `json:"scope"`
}
type SemanticEvidence struct {
	Nodes        []SemanticNode        `json:"nodes"`
	TypeAxes     []string              `json:"type_axes"`
	EffectAxes   []string              `json:"effect_axes"`
	Types        matrixir.SparseMatrix `json:"types"`
	Effects      matrixir.SparseMatrix `json:"effects"`
	Syntax       matrixir.SparseMatrix `json:"syntax"`
	Control      matrixir.SparseMatrix `json:"control"`
	Data         matrixir.SparseMatrix `json:"data"`
	Binding      matrixir.SparseMatrix `json:"binding"`
	Order        matrixir.SparseMatrix `json:"evaluation_order"`
	CallModes    matrixir.SparseMatrix `json:"call_modes"`
	CallModeAxes []string              `json:"call_mode_axes"`
	Scope        matrixir.SparseMatrix `json:"scope_membership"`
	Scopes       []SemanticScope       `json:"scopes"`
	Bindings     []SemanticBinding     `json:"bindings"`
	Contract     matrixir.Vector       `json:"contract"`
	ContractAxes []string              `json:"contract_axes"`
}

type SemanticScope struct {
	ID     int    `json:"id"`
	Kind   string `json:"kind"`
	Parent int    `json:"parent"`
}
type SemanticBinding struct {
	ID         int    `json:"id"`
	Name       string `json:"name"`
	Scope      int    `json:"scope"`
	Mutable    bool   `json:"mutable"`
	Definition int    `json:"definition"`
	TypeOrigin string `json:"type_origin"`
}

// ParseSemanticCompatibility is the explicit legacy text ingress retained for
// compatibility fixtures and oracle tooling.  The productive source path is
// LowerSource, which never calls this function.
func ParseSemanticCompatibility(source, code string) (*SemanticProgram, error) {
	body, err := parse(code)
	if err != nil {
		return nil, err
	}
	if source == "r" {
		if err = decodeEagerR(body); err != nil {
			return nil, err
		}
	}
	evaluation := "eager_left_to_right"
	if source == "r" {
		evaluation = "lazy_demand"
	}
	p := NewSemanticProgram(body, evaluation)
	p.Origin.SourceLanguage = source
	if semanticProfileLanguage(source) != "" {
		if err := p.AttachSemanticFeatureProfile(source); err != nil {
			return nil, err
		}
	}
	// ParseSemantic is a legacy ingress API. Project once at ingress so every
	// runtime and target backend receives the canonical UAST only.
	doc, err := p.Document()
	if err != nil {
		return nil, err
	}
	// Keep the original source in the dedicated lossless surface plane. This
	// is metadata for same-language preservation, never an executable semantic
	// fallback and never a second intermediate representation.
	if doc.UniversalAST != nil {
		doc.UniversalAST.Surface = NewUniversalASTSurface(source, code)
		p.UniversalAST = doc.UniversalAST
	}
	return p, nil
}

// ParseSemantic is kept as a source-compatible alias for older callers. New
// product code must use LowerSource so text parsing cannot re-enter the
// canonical frontend after structured facts have been produced.
// Deprecated: use LowerSource for production and ParseSemanticCompatibility
// only for explicit compatibility/oracle work.
func ParseSemantic(source, code string) (*SemanticProgram, error) {
	return ParseSemanticCompatibility(source, code)
}
func NewSemanticProgram(body *BlockStmt, evaluation string) *SemanticProgram {
	p := &SemanticProgram{Body: body, Evaluation: evaluation, ValueModel: "tagged_dynamic_binary64", IndexBase: 1, Types: defaultSemanticTypeContract(), Origin: SemanticOrigin{SourceLanguage: "semantic", EntryPoint: "main"}}
	p.Evidence = p.analyze()
	for _, node := range p.Evidence.Nodes {
		if node.Kind == "typed_operation" {
			p.ValueModel = "tagged_exact_scalars_v1"
			p.Types.Numeric = "binary64_and_fixed_width_integer"
			p.Types.IntegerWidth = "per_operation"
			break
		}
	}
	return p
}
func EmitSemantic(target string, p *SemanticProgram) (string, error) {
	if err := ValidateSemanticProgram(p); err != nil {
		return "", err
	}
	if err := validateExecutableDialects(p); err != nil {
		return "", err
	}
	u, err := canonicalUniversalAST(p)
	if err != nil {
		return "", err
	}
	graph, err := newUASTExecutionGraph(u)
	if err != nil {
		return "", err
	}
	if err := validateUASTTargetCapabilities(u, target); err != nil {
		return "", err
	}
	if err := validateUASTTargetPreservation(u, target); err != nil {
		return "", err
	}
	if exact, err := validateDirectSignatureContracts(graph); err != nil {
		return "", err
	} else if exact {
		return "", fmt.Errorf("target %q does not implement %s", target, ExactSignatureCapability)
	}
	if resolved, err := validateDirectCallResolutions(graph); err != nil {
		return "", err
	} else if resolved {
		return "", fmt.Errorf("target %q does not implement %s", target, ExactCallResolutionCapability)
	}
	operations, err := directTypedRequirements(graph)
	if err != nil {
		return "", err
	}
	if len(operations) > 0 {
		if err := TypedImplementationMatrix().Check(operations, "target."+NormalizeLanguage(target)); err != nil {
			return "", err
		}
	}
	if u.Evaluation != "lazy_demand" && u.Evaluation != "eager_left_to_right" {
		return "", fmt.Errorf("unknown evaluation contract %q", u.Evaluation)
	}
	if !validValueContract(u.ValueModel, u.Types) || u.IndexBase != 1 {
		return "", fmt.Errorf("unmodeled value or indexing contract")
	}
	for _, dialect := range u.Dialects {
		for _, capability := range dialect.Capabilities {
			if !SupportsCapability(BackendCapabilities(target), capability) {
				return "", fmt.Errorf("target %q does not support dialect %q capability %q", target, dialect.Name, capability)
			}
		}
	}
	if len(u.Contracts.Requires) > 0 {
		capabilities := SemanticCapabilityMatrix(u.Contracts.Requires)
		rejected, err := capabilities.RejectedTargets(u.Contracts.Requires)
		if err != nil {
			return "", err
		}
		found := false
		for col, name := range capabilities.Targets {
			if name == NormalizeLanguage(target) {
				found = true
				if rejected.At(0, col) > 0 {
					return "", fmt.Errorf("target %q has %.0f unsupported semantic requirements: %v", target, rejected.At(0, col), u.Contracts.Requires)
				}
			}
		}
		if !found {
			return "", fmt.Errorf("unknown target %q", target)
		}
	}
	if target == "r" {
		return universalRSource(u, true)
	}
	if _, ok := ByID(target); !ok {
		return "", fmt.Errorf("unknown target %q", target)
	}
	return generateTargetFromUniversal(u.Evaluation, target, graph)
}

// EmitSemanticPreserveOriginal is the explicit same-language surface-plane
// mode. The ordinary EmitSemantic path still regenerates canonical target
// source from UAST, while callers that request PRESERVE_ORIGINAL can return
// the verified original bytes without making source text part of semantics.
func EmitSemanticPreserveOriginal(target string, p *SemanticProgram) (string, error) {
	if err := ValidateSemanticProgram(p); err != nil {
		return "", err
	}
	if p == nil || p.UniversalAST == nil || p.UniversalAST.Surface == nil {
		return "", fmt.Errorf("source surface is unavailable")
	}
	u := p.UniversalAST
	if NormalizeLanguage(target) != NormalizeLanguage(u.LanguageProfile) {
		return "", fmt.Errorf("PRESERVE_ORIGINAL requires same-language target %q", target)
	}
	b, err := u.Surface.Bytes()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// EmitSemanticDirect is used by the intermediate-route planner. It shares the
// canonical semantic/UAST construction with EmitSemantic but asks the
// projector for a strict native result, so a route can be attempted before
// compatibility runtime output is considered.
func EmitSemanticDirect(target string, p *SemanticProgram) (string, error) {
	if err := ValidateSemanticProgram(p); err != nil {
		return "", err
	}
	if err := validateExecutableDialects(p); err != nil {
		return "", err
	}
	u, err := canonicalUniversalAST(p)
	if err != nil {
		return "", err
	}
	if err := validateUASTTargetCapabilities(u, target); err != nil {
		return "", err
	}
	if err := validateUASTTargetPreservation(u, target); err != nil {
		return "", err
	}
	if u.Evaluation != "lazy_demand" && u.Evaluation != "eager_left_to_right" {
		return "", fmt.Errorf("unknown evaluation contract %q", u.Evaluation)
	}
	if !validValueContract(u.ValueModel, u.Types) || u.IndexBase != 1 {
		return "", fmt.Errorf("unmodeled value or indexing contract")
	}
	return (UniversalTargetProjector{}).EmitDirect(u, target)
}

// EmitSemanticLoweredDirect is the explicit second stage in the native-first
// pipeline. It records lowering separately so a failed clone cannot leak into
// intermediate or runtime fallback.
func EmitSemanticLoweredDirect(target string, p *SemanticProgram) (string, LoweringTrace, error) {
	if err := ValidateSemanticProgram(p); err != nil {
		return "", LoweringTrace{Target: target, Attempted: true}, err
	}
	if err := validateExecutableDialects(p); err != nil {
		return "", LoweringTrace{Target: target, Attempted: true}, err
	}
	u, err := canonicalUniversalAST(p)
	if err != nil {
		return "", LoweringTrace{Target: target, Attempted: true}, err
	}
	// Primitive closure is a productive stage of semantic lowering. It uses
	// generated recipes when their canonical operation is present and validates
	// the remaining recipes on isolated UAST clones.
	var applied []string
	u, applied, err = ApplyPrimitiveClosure(u, target)
	if err != nil {
		return "", LoweringTrace{Target: target, Attempted: true, ErrorClass: "PRIMITIVE_CLOSURE_FAILED"}, err
	}
	if err := validateUASTTargetCapabilities(u, target); err != nil {
		return "", LoweringTrace{Target: target, Attempted: true}, err
	}
	if err := validateUASTTargetPreservation(u, target); err != nil {
		return "", LoweringTrace{Target: target, Attempted: true}, err
	}
	spec, ok := targetSpec(target)
	if !ok {
		return "", LoweringTrace{Target: target, Attempted: true}, fmt.Errorf("unknown target %q", target)
	}
	doc, trace, err := (UniversalTargetProjector{}).ProjectLoweredDirect(u, spec)
	if err != nil {
		return "", trace, err
	}
	if len(applied) > 0 {
		trace.Rules = append(trace.Rules, applied...)
	}
	return (UniversalFormatter{Indent: spec.Indent, Newline: "\n"}).Format(doc), trace, nil
}

// The node projections preserve lexical scope and ordered operand occurrences.
// They are descriptions of this IR, not claims that all source-language types
// or environment semantics were understood by the conservative frontends.
func (p *SemanticProgram) analyze() SemanticEvidence {
	e := SemanticEvidence{
		TypeAxes: []string{"binary64", "string", "boolean", "null", "na", "nan", "function", "unknown"},
		EffectAxes: []string{
			"local.read", "local.write", "io.read", "io.write", "memory.allocate",
			"global.read", "global.write", "filesystem.read", "filesystem.write",
			"network", "exception.throw", "thread.spawn", "synchronization", "ffi",
			"time", "random", "call.unknown", "control",
		},
		ContractAxes: []string{"lazy_demand", "eager_left_to_right", "binary64", "one_based_index", "full_source_type_equivalence"},
	}
	e.Contract = matrixir.Vector{0, 0, 1, 1, 0}
	if p.Evaluation == "lazy_demand" {
		e.Contract[0] = 1
	} else {
		e.Contract[1] = 1
	}
	type cell struct{ r, c int }
	var types, effects, syntax, order, callModes []cell
	scopes := 1
	scopeParents := []int{-1}
	add := func(kind, symbol string, scope, typ, eff int) int {
		id := len(e.Nodes)
		e.Nodes = append(e.Nodes, SemanticNode{ID: id, Kind: kind, Symbol: symbol, Scope: scope})
		types = append(types, cell{id, typ})
		if eff >= 0 {
			effects = append(effects, cell{id, eff})
		}
		return id
	}
	var expr func(Expr, int) int
	var stmt func(Stmt, int) int
	children := func(parent int, ids []int, ordered bool) {
		for i, id := range ids {
			if id < 0 {
				continue
			}
			syntax = append(syntax, cell{parent, id})
			if ordered && i > 0 && ids[i-1] >= 0 {
				order = append(order, cell{ids[i-1], id})
			}
		}
	}
	expr = func(v Expr, scope int) int {
		if v == nil {
			return -1
		}
		kind, sym, typ, eff := "unknown", "", 7, -1
		switch x := v.(type) {
		case *OperationExpr:
			kind, sym = "typed_operation", x.Operation.Name
			if x.Operation.Name != "integer.literal" {
				eff = 10
			}
			result := x.Operation.resultType()
			if result.Kind == "boolean" {
				typ = 2
			} else if result.Kind == "string" {
				typ = 1
			} else {
				axis := integerFeature(result)
				typ = -1
				for i, name := range e.TypeAxes {
					if name == axis {
						typ = i
						break
					}
				}
				if typ < 0 {
					typ = len(e.TypeAxes)
					e.TypeAxes = append(e.TypeAxes, axis)
				}
			}
		case *LiteralExpr:
			kind = "literal"
			sym = x.Text
			typ = 0
			if x.Kind == "string" {
				typ = 1
			}
		case *IdentExpr:
			kind = "identifier"
			sym = x.Name
			eff = 0
			switch x.Name {
			case "TRUE", "FALSE", "T", "F":
				typ = 2
				eff = -1
			case "NULL":
				typ = 3
				eff = -1
			case "NA", "NA_integer_", "NA_real_", "NA_character_", "NA_complex_":
				typ = 4
				eff = -1
			case "NaN":
				typ = 5
				eff = -1
			}
		case *UnaryExpr:
			kind = "unary"
			sym = x.Op
		case *BinaryExpr:
			kind = "binary"
			sym = x.Op
		case *CallExpr:
			kind = "call"
			eff = 16
			if id, ok := x.Fun.(*IdentExpr); ok {
				sym = id.Name
				switch sym {
				case "print", "show", "cat", "message", "warning":
					eff = 3
				case "readLines", "scan", "read.csv", "read.table":
					eff = 7
				case "writeLines", "write.csv", "write.table":
					eff = 8
				case "stop", "throw", "panic":
					eff = 10
				case ".C", ".Call", ".External", "ffi":
					eff = 13
				case "Sys.time", "time":
					eff = 14
				case "runif", "rnorm", "sample", "rand", "random":
					eff = 15
				}
			}
		case *IndexExpr:
			kind = "index"
		case *FunctionExpr:
			kind = "function"
			typ = 6
		case *IterationExpr:
			kind = "iteration"
			sym = x.Kind
		}
		id := add(kind, sym, scope, typ, eff)
		if call, ok := v.(*CallExpr); ok {
			mode := 0
			if p.Evaluation == "eager_left_to_right" || call.Eager {
				mode = 1
			}
			callModes = append(callModes, cell{id, mode})
		}
		var ids []int
		ordered := true
		switch x := v.(type) {
		case *OperationExpr:
			for _, operand := range x.Operands {
				ids = append(ids, expr(operand, scope))
			}
		case *UnaryExpr:
			ids = []int{expr(x.X, scope)}
		case *BinaryExpr:
			ids = []int{expr(x.L, scope), expr(x.R, scope)}
			ordered = x.Op != "&&" && x.Op != "||" // conditional edge cannot be an unconditional order claim
		case *CallExpr:
			ids = append(ids, expr(x.Fun, scope))
			for _, a := range x.Args {
				ids = append(ids, expr(a.Value, scope))
			}
			ordered = p.Evaluation == "eager_left_to_right" || x.Eager
		case *IndexExpr:
			ids = append(ids, expr(x.X, scope))
			for _, a := range x.Args {
				ids = append(ids, expr(a.Value, scope))
			}
		case *FunctionExpr:
			childScope := scopes
			scopes++
			scopeParents = append(scopeParents, scope)
			for _, param := range x.Params {
				typeColumn := 7
				if param.Type != nil {
					axis := integerFeature(*param.Type)
					typeColumn = -1
					for i, name := range e.TypeAxes {
						if name == axis {
							typeColumn = i
							break
						}
					}
					if typeColumn < 0 {
						typeColumn = len(e.TypeAxes)
						e.TypeAxes = append(e.TypeAxes, axis)
					}
				}
				pid := add("parameter", param.Name, childScope, typeColumn, 1)
				defaultScope := childScope
				if x.Binding == "exact_v1" && x.DefaultEvaluation == "definition" {
					defaultScope = scope
				}
				children(pid, []int{expr(param.Default, defaultScope)}, false)
				ids = append(ids, pid)
			}
			ids = append(ids, stmt(x.Body, childScope))
			ordered = false
		case *IterationExpr:
			ids = []int{expr(x.Value, scope)}
		}
		children(id, ids, ordered)
		return id
	}
	stmt = func(v Stmt, scope int) int {
		if v == nil {
			return -1
		}
		kind, sym, eff := "statement", "", 17
		switch x := v.(type) {
		case *BlockStmt:
			kind = "block"
		case *AssignStmt:
			kind = "assign"
			sym = x.Name
			eff = 1
		case *ExprStmt:
			kind = "expression"
		case *IfStmt:
			kind = "if"
		case *WhileStmt:
			kind = "while"
		case *ForStmt:
			kind = "for"
			sym = x.Name
		case *RepeatStmt:
			kind = "repeat"
		case *ReturnStmt:
			kind = "return"
		case *BreakStmt:
			kind = "break"
		case *NextStmt:
			kind = "continue"
		}
		id := add(kind, sym, scope, 7, eff)
		var ids []int
		ordered := false
		switch x := v.(type) {
		case *BlockStmt:
			for _, s := range x.List {
				ids = append(ids, stmt(s, scope))
			}
			ordered = true
		case *AssignStmt:
			ids = []int{expr(x.Value, scope)}
		case *ExprStmt:
			ids = []int{expr(x.X, scope)}
		case *IfStmt:
			ids = []int{expr(x.Cond, scope), stmt(x.Then, scope), stmt(x.Else, scope)}
		case *WhileStmt:
			ids = []int{expr(x.Cond, scope), stmt(x.Body, scope)}
		case *ForStmt:
			ids = []int{expr(x.Seq, scope), stmt(x.Body, scope)}
		case *RepeatStmt:
			ids = []int{stmt(x.Body, scope)}
		case *ReturnStmt:
			ids = []int{expr(x.X, scope)}
		}
		children(id, ids, ordered)
		return id
	}
	stmt(p.Body, 0)
	n := len(e.Nodes)
	e.Types = matrixir.NewSparseMatrix(n, len(e.TypeAxes))
	e.Effects = matrixir.NewSparseMatrix(n, len(e.EffectAxes))
	e.Syntax = matrixir.NewSparseMatrix(n, n)
	e.Control = matrixir.NewSparseMatrix(n, n)
	e.Data = matrixir.NewSparseMatrix(n, n)
	e.Order = matrixir.NewSparseMatrix(n, n)
	e.CallModeAxes = []string{"lazy_demand", "eager_left_to_right"}
	e.CallModes = matrixir.NewSparseMatrix(n, len(e.CallModeAxes))
	for _, x := range callModes {
		e.CallModes.Set(x.r, x.c, 1)
	}
	e.Scope = matrixir.NewSparseMatrix(n, scopes)
	e.Scopes = make([]SemanticScope, scopes)
	for i := range e.Scopes {
		kind := "block"
		if i == 0 {
			kind = "program"
		} else {
			kind = "function"
		}
		e.Scopes[i] = SemanticScope{ID: i, Kind: kind, Parent: scopeParents[i]}
	}
	for _, x := range types {
		e.Types.Set(x.r, x.c, 1)
	}
	for _, x := range effects {
		e.Effects.Set(x.r, x.c, 1)
	}
	for _, x := range syntax {
		e.Syntax.Set(x.r, x.c, 1)
		switch e.Nodes[x.r].Kind {
		case "if", "while", "for", "repeat", "block":
			e.Control.Set(x.r, x.c, 1)
		}
	}
	for _, x := range order {
		e.Order.Set(x.r, x.c, 1)
	}
	for i, node := range e.Nodes {
		e.Scope.Set(i, node.Scope, 1)
	}
	// Bindings represent lexical candidate resolution. Multiple candidates remain
	// visible rather than guessing a single declaration in languages where the
	// frontend has not yet proved declaration timing or dynamic environments.
	for i, node := range e.Nodes {
		if node.Kind == "assign" || node.Kind == "parameter" {
			e.Bindings = append(e.Bindings, SemanticBinding{ID: len(e.Bindings), Name: node.Symbol, Scope: node.Scope, Mutable: node.Kind == "assign", Definition: i, TypeOrigin: "unknown"})
		}
	}
	e.Binding = matrixir.NewSparseMatrix(n, len(e.Bindings))
	ancestor := func(scope, target int) bool {
		for scope >= 0 {
			if scope == target {
				return true
			}
			scope = scopeParents[scope]
		}
		return false
	}
	for i, node := range e.Nodes {
		if node.Kind != "identifier" {
			continue
		}
		for j, b := range e.Bindings {
			if b.Name == node.Symbol && ancestor(node.Scope, b.Scope) {
				e.Binding.Set(i, j, 1)
				e.Data.Set(i, b.Definition, 1)
			}
		}
	}
	return e
}

// RSource writes executable R syntax from the typed tree. In eager mode calls
// with user functions are handled by the explicit call-boundary lowering below.
func (p *SemanticProgram) RSource(enforce bool) (string, error) {
	u, err := canonicalUniversalAST(p)
	if err != nil {
		return "", err
	}
	return universalRSource(u, enforce)
}

type semanticWriter struct {
	eager   bool
	enforce bool
	used    map[string]bool
	serial  int
	helpers []string
}

func (w *semanticWriter) expression(v Expr) (string, error) {
	if v == nil {
		return "NULL", nil
	}
	switch x := v.(type) {
	case *LiteralExpr:
		if x.Kind == "string" {
			return strconv.Quote(unquote(x.Text)), nil
		}
		return x.Text, nil
	case *IdentExpr:
		return x.Name, nil
	case *UnaryExpr:
		a, e := w.expression(x.X)
		return "(" + x.Op + a + ")", e
	case *BinaryExpr:
		a, e := w.expression(x.L)
		if e != nil {
			return "", e
		}
		b, e := w.expression(x.R)
		return "(" + a + " " + x.Op + " " + b + ")", e
	case *IndexExpr:
		a, e := w.expression(x.X)
		if e != nil {
			return "", e
		}
		args, e := w.arguments(x.Args)
		open, close := "[", "]"
		if x.Double {
			open, close = "[[", "]]"
		}
		return a + open + strings.Join(args, ", ") + close, e
	case *CallExpr:
		fn, e := w.expression(x.Fun)
		if e != nil {
			return "", e
		}
		args, e := w.arguments(x.Args)
		if e != nil {
			return "", e
		}
		if (w.eager || (w.enforce && x.Eager)) && len(args) > 0 {
			return w.eagerCall(fn, x.Args, args)
		}
		return fn + "(" + strings.Join(args, ", ") + ")", nil
	case *FunctionExpr:
		if x.Binding != "" {
			return "", fmt.Errorf("R text cannot preserve %s", ExactSignatureCapability)
		}
		params := make([]string, len(x.Params))
		for i, p := range x.Params {
			params[i] = p.Name
			if p.Default != nil {
				d, e := w.expression(p.Default)
				if e != nil {
					return "", e
				}
				params[i] += " = " + d
			}
		}
		body, e := w.statement(x.Body)
		return "function(" + strings.Join(params, ", ") + ") {\n" + body + "}", e
	default:
		return "", fmt.Errorf("no R serialization for %T", v)
	}
}
func (w *semanticWriter) arguments(args []Arg) ([]string, error) {
	out := make([]string, len(args))
	for i, a := range args {
		if a.Missing {
			continue
		}
		v, e := w.expression(a.Value)
		if e != nil {
			return nil, e
		}
		if a.Name != "" {
			v = a.Name + " = " + v
		}
		out[i] = v
	}
	return out, nil
}
func (w *semanticWriter) statement(v Stmt) (string, error) {
	switch x := v.(type) {
	case nil:
		return "", nil
	case *BlockStmt:
		var b strings.Builder
		for _, s := range x.List {
			text, e := w.statement(s)
			if e != nil {
				return "", e
			}
			b.WriteString(text)
		}
		return b.String(), nil
	case *AssignStmt:
		a, e := w.expression(x.Value)
		op := x.Op
		if op == "" || op == "=" {
			op = "<-"
		}
		return x.Name + " " + op + " " + a + "\n", e
	case *ExprStmt:
		a, e := w.expression(x.X)
		return a + "\n", e
	case *ReturnStmt:
		a, e := w.expression(x.X)
		return "return(" + a + ")\n", e
	case *BreakStmt:
		return "break\n", nil
	case *NextStmt:
		return "next\n", nil
	case *IfStmt:
		c, e := w.expression(x.Cond)
		if e != nil {
			return "", e
		}
		a, e := w.statement(x.Then)
		if e != nil {
			return "", e
		}
		out := "if (" + c + ") {\n" + a + "}"
		if x.Else != nil {
			b, e := w.statement(x.Else)
			if e != nil {
				return "", e
			}
			out += " else {\n" + b + "}"
		}
		return out + "\n", nil
	case *WhileStmt:
		c, e := w.expression(x.Cond)
		if e != nil {
			return "", e
		}
		b, e := w.statement(x.Body)
		return "while (" + c + ") {\n" + b + "}\n", e
	case *ForStmt:
		c, e := w.expression(x.Seq)
		if e != nil {
			return "", e
		}
		b, e := w.statement(x.Body)
		return "for (" + x.Name + " in " + c + ") {\n" + b + "}\n", e
	case *RepeatStmt:
		b, e := w.statement(x.Body)
		return "repeat {\n" + b + "}\n", e
	default:
		return "", fmt.Errorf("no R serialization for %T", v)
	}
}

// A wrapper evaluates supplied arguments in lexical order, even when unused.
// It uses ordinary R force operations, never an encoded copy of original code.
// Its decoder checks the actual helper tree (see eager_r.go).
func (w *semanticWriter) eagerCall(fn string, args []Arg, values []string) (string, error) {
	if fn == "force" {
		return fn + "(" + strings.Join(values, ", ") + ")", nil
	}
	if w.used["force"] {
		return "", fmt.Errorf("eager R boundary requires unshadowed force")
	}
	var name string
	for {
		w.serial++
		name = fmt.Sprintf("r2m_eager_%d", w.serial)
		if !w.used[name] {
			w.used[name] = true
			break
		}
	}
	params := []string{"r2m_fun"}
	actual := []string{fn}
	inner := make([]string, len(args))
	forces := []string{"force(r2m_fun)"}
	for i, a := range args {
		if a.Missing {
			return "", fmt.Errorf("eager R lowering requires explicit supplied arguments")
		}
		param := fmt.Sprintf("r2m_value_%d", i)
		params = append(params, param)
		raw := values[i]
		if a.Name != "" {
			raw = strings.TrimPrefix(raw, a.Name+" = ")
			inner[i] = a.Name + " = " + param
		} else {
			inner[i] = param
		}
		actual = append(actual, raw)
		forces = append(forces, "force("+param+")")
	}
	helper := name + " <- function(" + strings.Join(params, ", ") + ") {\n" + strings.Join(forces, "\n") + "\nreturn(r2m_fun(" + strings.Join(inner, ", ") + "))\n}\n"
	w.helpers = append(w.helpers, helper)
	return name + "(" + strings.Join(actual, ", ") + ")", nil
}
