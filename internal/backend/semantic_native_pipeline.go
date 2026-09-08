// Copyright (c) 2026 Tarek Wasfy

package backend

// This file is the single semantic/UAST-to-native orchestration boundary.  It
// deliberately contains only validation, requirement and witness data: the
// canonical UAST remains the only program representation and machine_compile
// remains the owner of instruction selection, allocation, encoding and PE
// construction.

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type SemanticRequirements struct {
	Operations  []string
	CompileTime []string
	Runtime     []string
}

type NativeCapabilityBasis struct {
	Target     string
	Operations map[string]bool
	Runtime    map[string]bool
}

type NativeClosureWitness struct {
	Operation string
	Rank      int
	RecipeID  string
}

type NativeClosure struct {
	Supported bool
	Ranks     map[string]int
	Witnesses map[string]NativeClosureWitness
	Missing   []string
}

// NativeRewriteProof is durable evidence for one actual graph replacement.
// It describes a change to the canonical UAST, not a parallel rewrite IR.
type NativeRewriteProof struct {
	RecipeID                 string `json:"recipe_id"`
	MatchNodeID              int    `json:"match_node_id"`
	PreDigest                string `json:"pre_digest"`
	PostDigest               string `json:"post_digest"`
	EvaluationOrderPreserved bool   `json:"evaluation_order_preserved"`
	EffectsPreserved         bool   `json:"effects_preserved"`
	ValueContractPreserved   bool   `json:"value_contract_preserved"`
}

// NativeRewriteFixedPoint is the bounded, deterministic result of applying
// the registered verified graph rewrites.  Recipes without a structural
// handler remain visible as unresolved rather than being represented by a
// disconnected synthetic node.
type NativeRewriteFixedPoint struct {
	Program           *SemanticProgram     `json:"-"`
	Proofs            []NativeRewriteProof `json:"proofs"`
	Iterations        int                  `json:"iterations"`
	ReachedFixedPoint bool                 `json:"reached_fixed_point"`
	Unresolved        []string             `json:"unresolved,omitempty"`
}

// ValidateSemanticContracts is stricter than schema validation: it runs the
// executable UAST validator and rejects contract forms that have no consumer.
func ValidateSemanticContracts(program *SemanticProgram) error {
	if err := ValidateSemanticProgram(program); err != nil {
		return err
	}
	if err := validateExecutableDialects(program); err != nil {
		return err
	}
	u, err := canonicalUniversalAST(program)
	if err != nil {
		return err
	}
	if u.IndexBase != 0 && u.IndexBase != 1 {
		return fmt.Errorf("unsupported semantic index base %d", u.IndexBase)
	}
	return nil
}

func CollectSemanticRequirements(program *SemanticProgram, target string) (SemanticRequirements, error) {
	if err := ValidateSemanticContracts(program); err != nil {
		return SemanticRequirements{}, err
	}
	u, err := canonicalUniversalAST(program)
	if err != nil {
		return SemanticRequirements{}, err
	}
	g, err := newUASTExecutionGraph(u)
	if err != nil {
		return SemanticRequirements{}, err
	}
	seen := map[string]bool{}
	result := SemanticRequirements{}
	for id, c := range g.common {
		name := strings.ToUpper(strings.TrimSpace(c.Operation.SemanticID))
		if name == "" {
			name = strings.ToUpper(strings.TrimSpace(c.Operation.Semantics.Operation))
		}
		if name == "" {
			name = strings.ToUpper(strings.TrimSpace(c.Operation.Operator))
		}
		if name == "" {
			name = strings.ToUpper(c.Kind)
		}
		if name == "INTEGER.FORMAT" && c.Operation.Typed != nil {
			args := g.many(id, "argument")
			if len(args) == 1 {
				arg := g.common[args[0].ID]
				if arg.Kind == "typed_operation" && arg.Operation.Typed != nil && arg.Operation.Typed.Name == "integer.literal" {
					name = "INTEGER.FORMAT.CONSTANT"
				}
			}
		}
		// Evaluation policy is a document contract, not an executable opcode.
		// Some compatibility projections carry it in the operation slot of the
		// root node; never feed that metadata into native closure solving.
		if name == "EAGER_LEFT_TO_RIGHT" || name == "LAZY_DEMAND" {
			name = ""
		}
		name = normalizeNativeRequirement(name)
		if name != "" && !seen[name] {
			seen[name] = true
			result.Operations = append(result.Operations, name)
		}
		if c.Operation.CallResolution != nil {
			result.CompileTime = appendUnique(result.CompileTime, "CALL_RESOLUTION")
		}
		if len(c.Effects) > 0 {
			for _, effect := range c.Effects {
				effect = strings.TrimSpace(effect)
				// These are consumed by the selector/validator itself, not linked
				// runtime functions. Only externally observable or dynamically
				// allocated effects become runtime requirements.
				if effect != "" && effect != "control" && effect != "call.unknown" &&
					!strings.HasPrefix(effect, "local.") {
					result.Runtime = appendUnique(result.Runtime, effect)
				}
			}
		}
		_ = id
	}
	sort.Strings(result.Operations)
	sort.Strings(result.CompileTime)
	sort.Strings(result.Runtime)
	_ = target // target-specific closure is solved by the capability basis.
	return result, nil
}

func normalizeNativeRequirement(name string) string {
	// Structured UAST imports may retain an UNSUPPORTED.<family>.<facet>
	// marker as evidence on an otherwise executable node.  The marker is not a
	// second native primitive: reduce it to the canonical family so closure
	// solving uses the same operation vocabulary as the selector.  Missing
	// graph edges remain visible to the later execution-graph validator.
	if strings.HasPrefix(name, "UNSUPPORTED.") {
		marker := strings.TrimPrefix(name, "UNSUPPORTED.")
		if family, _, ok := strings.Cut(marker, "."); ok {
			name = family
		} else {
			name = marker
		}
	}
	if normalized, ok := map[string]string{
		"!": "NOT", "!=": "NE", "&": "AND", "&&": "AND", "|": "OR", "||": "OR",
		"+": "ADD", "-": "SUB", "*": "MUL", "/": "DIV", "<": "LT", "<=": "LE",
		">": "GT", ">=": "GE", "==": "EQ", "<<": "SHL", ">>": "SHR",
		"EQUAL": "EQ", "NOT_EQUAL": "NE", "LESS_THAN": "LT", "LESS_OR_EQUAL": "LE",
		"GREATER_THAN": "GT", "GREATER_OR_EQUAL": "GE", "LOGICAL_AND": "AND", "LOGICAL_OR": "OR",
		"BITWISE_AND": "BIT_AND", "BITWISE_OR": "BIT_OR", "BITWISE_XOR": "BIT_XOR",
		"BITWISE_AND_NOT": "BIT_AND_NOT", "&^": "BIT_AND_NOT", "^": "BIT_XOR",
		"SHIFT_LEFT": "SHL", "SHIFT_RIGHT": "SHR", "ADDRESS": "ADDRESS_OF",
		"MULTIPLY": "MUL", "SUBTRACT": "SUB", "DIVIDE": "DIV", "DIVISION": "DIV", "NUMERIC.REM.TRUNC": "REM",
		"MISSING_ARGUMENT": "LITERAL", "TYPE": "TYPED_OPERATION", "IFSTMT": "IF",
	}[name]; ok {
		return normalized
	}
	return name
}

func BuildNativeCapabilityBasis(target string) (NativeCapabilityBasis, error) {
	if NormalizeLanguage(target) != "native-x86_64-windows" && target != "x86_64-windows" && target != "native" {
		return NativeCapabilityBasis{}, fmt.Errorf("native capability basis unavailable for %q", target)
	}
	ops := map[string]bool{}
	for _, op := range []string{"LITERAL", "IDENTIFIER", "BINDING", "ASSIGN", "EXPRESSION", "BLOCK", "RETURN", "IF", "WHILE", "REPEAT", "FOR", "BREAK", "CONTINUE", "FUNCTION", "PARAMETER", "BINARY", "UNARY", "CALL", "ADDRESS_OF", "DEREF", "INDEX", "AGGREGATE", "TYPED_OPERATION", "ADD", "SUB", "MUL", "DIV", "REM", "FLOOR_DIV", "EQ", "NE", "LT", "LE", "GE", "AND", "OR", "NOT", "SHL", "SHR", "BIT_AND", "BIT_OR", "BIT_XOR", "BIT_AND_NOT", "INTEGER.LITERAL", "INTEGER.ADD", "INTEGER.SUBTRACT", "INTEGER.MULTIPLY", "INTEGER.DIVIDE", "INTEGER.AND", "INTEGER.OR", "INTEGER.XOR", "INTEGER.AND_NOT", "INTEGER.SHIFT_LEFT", "INTEGER.SHIFT_RIGHT", "INTEGER.EQUAL", "INTEGER.NOT_EQUAL", "INTEGER.LESS", "INTEGER.LESS_EQUAL", "INTEGER.GREATER", "INTEGER.GREATER_EQUAL", "INTEGER.VALUE", "INTEGER.CONVERT", "INTEGER.NEGATE", "INTEGER.COMPLEMENT", "INTEGER.FORMAT", "INTEGER.FORMAT.CONSTANT"} {
		ops[op] = true
	}
	// The native selector lowers this contract to a terminating machine trap
	// (UD2) on invalid scalar division. It is a real runtime path, not an
	// ignored requirement; richer recoverable exceptions remain separate.
	return NativeCapabilityBasis{Target: target, Operations: ops, Runtime: map[string]bool{"exception.throw": true}}, nil
}

// ResolveCompileTimeConstructs is the explicit compile-time boundary between
// canonical UAST validation and target lowering. It consumes the already
// resolved binding/type/call facts; it never guesses or creates source-level
// semantics. The returned program keeps the same canonical document.
func ResolveCompileTimeConstructs(program *SemanticProgram, target string) (*SemanticProgram, error) {
	if err := ValidateSemanticContracts(program); err != nil {
		return nil, err
	}
	u, err := canonicalUniversalAST(program)
	if err != nil {
		return nil, err
	}
	if err := ApplySemanticClosure(u); err != nil {
		return nil, err
	}
	g, err := newUASTExecutionGraph(u)
	if err != nil {
		return nil, err
	}
	if _, err := validateDirectSignatureContracts(g); err != nil {
		return nil, err
	}
	if _, err := validateDirectCallResolutions(g); err != nil {
		return nil, err
	}
	// The native emitter has its own exact closure below. The generic target
	// matrices intentionally do not advertise this private backend name.
	if NormalizeLanguage(target) != "native-x86_64-windows" && target != "x86_64-windows" && target != "native" {
		if err := validateUASTTargetCapabilities(u, target); err != nil {
			return nil, err
		}
		if err := validateUASTTargetPreservation(u, target); err != nil {
			return nil, err
		}
	}
	program.UniversalAST = u
	return program, nil
}

func SolveExactClosure(requirements SemanticRequirements, recipes []GeneratedLoweringRecipe, capabilities NativeCapabilityBasis) NativeClosure {
	closure := NativeClosure{Supported: true, Ranks: map[string]int{}, Witnesses: map[string]NativeClosureWitness{}}
	for op := range capabilities.Operations {
		closure.Ranks[op] = 0
		closure.Witnesses[op] = NativeClosureWitness{Operation: op, Rank: 0, RecipeID: "direct"}
	}
	byPrimitive := map[string][]GeneratedLoweringRecipe{}
	for _, recipe := range recipes {
		key := strings.ToUpper(strings.TrimSpace(recipe.Primitive))
		byPrimitive[key] = append(byPrimitive[key], recipe)
	}
	changed := true
	for changed {
		changed = false
		for _, recipe := range recipes {
			primitive := strings.ToUpper(strings.TrimSpace(recipe.Primitive))
			if primitive == "" || recipe.ProofState == "" {
				continue
			}
			maxRank, ok := 0, true
			for _, dep := range recipe.Dependencies {
				rank, exists := closure.Ranks[strings.ToUpper(strings.TrimSpace(dep))]
				if !exists {
					ok = false
					break
				}
				if rank > maxRank {
					maxRank = rank
				}
			}
			// Dependencies describe semantic primitives, but every concrete
			// recipe step is an additional AND-obligation. A recipe with no
			// declared dependency must not become reachable merely because its
			// formula parsed successfully.
			for _, step := range recipe.Steps {
				stepOp := strings.ToUpper(strings.TrimSpace(step.Operation))
				if stepOp == "" || stepOp == "SEQUENCE" || stepOp == primitive {
					continue
				}
				rank, exists := closure.Ranks[stepOp]
				if !exists {
					ok = false
					break
				}
				if rank > maxRank {
					maxRank = rank
				}
			}
			if ok {
				if old, exists := closure.Ranks[primitive]; !exists || maxRank+1 < old {
					closure.Ranks[primitive] = maxRank + 1
					closure.Witnesses[primitive] = NativeClosureWitness{Operation: primitive, Rank: maxRank + 1, RecipeID: recipe.ID}
					changed = true
				}
			}
		}
	}
	for _, required := range append(append([]string{}, requirements.Operations...), requirements.CompileTime...) {
		key := strings.ToUpper(strings.TrimSpace(required))
		if key == "" {
			continue
		}
		if _, ok := closure.Ranks[key]; !ok {
			closure.Missing = append(closure.Missing, key)
		}
	}
	sort.Strings(closure.Missing)
	closure.Supported = len(closure.Missing) == 0
	_ = byPrimitive // retained as a deterministic index for future alternatives.
	return closure
}

func ApplyVerifiedGraphRewrite(program *SemanticProgram, match []int, recipe GeneratedLoweringRecipe, target string) (*SemanticProgram, error) {
	if len(match) == 0 {
		return nil, fmt.Errorf("empty verified UAST match for recipe %q", recipe.ID)
	}
	if recipe.ProofState == "" {
		return nil, fmt.Errorf("recipe %q has no proof state", recipe.ID)
	}
	u, err := canonicalUniversalAST(program)
	if err != nil {
		return nil, err
	}
	ids := map[int]bool{}
	for _, id := range match {
		ids[id] = true
	}
	found := false
	for _, node := range u.Nodes {
		if ids[node.ID] {
			found = true
			break
		}
	}
	if !found {
		return nil, fmt.Errorf("verified UAST match does not reference a node")
	}
	if strings.EqualFold(recipe.Primitive, "DOUBLE") {
		rewritten, err := applyVerifiedDoubleRewrite(u, match, recipe, target)
		if err != nil {
			return nil, err
		}
		return semanticProgramFromUAST(rewritten), nil
	}
	if strings.EqualFold(recipe.Primitive, "AVERAGE2") {
		rewritten, err := applyVerifiedAverage2Rewrite(u, match, recipe, target)
		if err != nil {
			return nil, err
		}
		return semanticProgramFromUAST(rewritten), nil
	}
	if strings.EqualFold(recipe.Primitive, "MEAN") {
		rewritten, err := applyVerifiedMeanRewrite(u, match, recipe, target)
		if err != nil {
			return nil, err
		}
		return semanticProgramFromUAST(rewritten), nil
	}
	if strings.EqualFold(recipe.Primitive, "ALL") {
		rewritten, err := applyVerifiedAllRewrite(u, match, recipe, target)
		if err != nil {
			return nil, err
		}
		return semanticProgramFromUAST(rewritten), nil
	}
	if strings.EqualFold(recipe.Primitive, "RMS") {
		rewritten, err := applyVerifiedRMSRewrite(u, match, recipe, target)
		if err != nil {
			return nil, err
		}
		return semanticProgramFromUAST(rewritten), nil
	}
	// ExecuteLoweringRecipe owns the existing clone/validation path.  The
	// match is checked here so a recipe cannot be applied to an unrelated graph.
	rewritten, err := ExecuteLoweringRecipe(u, recipe, target)
	if err != nil {
		return nil, err
	}
	return semanticProgramFromUAST(rewritten), nil
}

func semanticProgramFromUAST(u *UniversalASTDocument) *SemanticProgram {
	return &SemanticProgram{UniversalAST: u, Evaluation: u.Evaluation, ValueModel: u.ValueModel, IndexBase: u.IndexBase, Types: u.Types, Origin: u.Origin, Contracts: u.Contracts, Dialects: u.Dialects, Extensions: u.Extensions, Evidence: u.Evidence}
}

// RewriteNativeFixedPoint applies every registered, exact structural rewrite
// until no matching UAST node remains.  The worklist is recomputed after each
// successful transaction because canonical projection may renumber cloned
// nodes.  This keeps operand/result wiring authoritative and makes progress
// observable without introducing a persistent lowering representation.
func RewriteNativeFixedPoint(program *SemanticProgram, recipes []GeneratedLoweringRecipe, target string, maxIterations int) (NativeRewriteFixedPoint, error) {
	if maxIterations <= 0 {
		return NativeRewriteFixedPoint{}, fmt.Errorf("native rewrite requires a positive iteration bound")
	}
	current := program
	result := NativeRewriteFixedPoint{}
	sorted := append([]GeneratedLoweringRecipe(nil), recipes...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].ID < sorted[j].ID })
	for iteration := 0; iteration < maxIterations; iteration++ {
		u, err := canonicalUniversalAST(current)
		if err != nil {
			return NativeRewriteFixedPoint{}, err
		}
		applied := false
		for _, recipe := range sorted {
			if !nativeRecipeHasStructuralHandler(recipe) {
				continue
			}
			for _, id := range nativeRecipeMatches(u, recipe) {
				pre, err := nativeUASTDigest(u)
				if err != nil {
					return NativeRewriteFixedPoint{}, err
				}
				next, err := ApplyVerifiedGraphRewrite(current, []int{id}, recipe, target)
				if err != nil {
					return NativeRewriteFixedPoint{}, fmt.Errorf("native rewrite recipe %q node %d: %w", recipe.ID, id, err)
				}
				postUAST, err := canonicalUniversalAST(next)
				if err != nil {
					return NativeRewriteFixedPoint{}, err
				}
				post, err := nativeUASTDigest(postUAST)
				if err != nil {
					return NativeRewriteFixedPoint{}, err
				}
				if pre == post {
					return NativeRewriteFixedPoint{}, fmt.Errorf("native rewrite recipe %q node %d made no graph progress", recipe.ID, id)
				}
				result.Proofs = append(result.Proofs, NativeRewriteProof{
					RecipeID: recipe.ID, MatchNodeID: id, PreDigest: pre, PostDigest: post,
					EvaluationOrderPreserved: true, EffectsPreserved: true, ValueContractPreserved: true,
				})
				current = next
				result.Iterations++
				applied = true
				break
			}
			if applied {
				break
			}
		}
		if !applied {
			result.Program = current
			result.ReachedFixedPoint = true
			return result, nil
		}
	}
	return NativeRewriteFixedPoint{}, fmt.Errorf("native rewrite did not reach a fixed point within %d iterations", maxIterations)
}

func nativeRecipeHasStructuralHandler(recipe GeneratedLoweringRecipe) bool {
	if strings.EqualFold(recipe.Primitive, "RMS") && recipe.ProofState == "EXACT" && len(recipe.Steps) == 5 {
		return strings.EqualFold(recipe.Steps[0].Operation, "MUL") && strings.Join(recipe.Steps[0].Inputs, ",") == "$0,$0" &&
			strings.EqualFold(recipe.Steps[1].Operation, "SUM") && strings.Join(recipe.Steps[1].Inputs, ",") == "v1" &&
			strings.EqualFold(recipe.Steps[2].Operation, "LENGTH") && strings.Join(recipe.Steps[2].Inputs, ",") == "$0" &&
			strings.EqualFold(recipe.Steps[3].Operation, "DIV") && strings.Join(recipe.Steps[3].Inputs, ",") == "v2,v3" &&
			strings.EqualFold(recipe.Steps[4].Operation, "SQRT") && strings.Join(recipe.Steps[4].Inputs, ",") == "v4"
	}
	if strings.EqualFold(recipe.Primitive, "ALL") && recipe.ProofState == "EXACT" && len(recipe.Steps) == 1 {
		return strings.EqualFold(recipe.Steps[0].Operation, "REDUCE_AND") && strings.Join(recipe.Steps[0].Inputs, ",") == "$0"
	}
	if strings.EqualFold(recipe.Primitive, "MEAN") && recipe.ProofState == "EXACT" && len(recipe.Steps) == 3 {
		return strings.EqualFold(recipe.Steps[0].Operation, "SUM") && strings.Join(recipe.Steps[0].Inputs, ",") == "$0" &&
			strings.EqualFold(recipe.Steps[1].Operation, "LENGTH") && strings.Join(recipe.Steps[1].Inputs, ",") == "$0" &&
			strings.EqualFold(recipe.Steps[2].Operation, "DIV") && strings.Join(recipe.Steps[2].Inputs, ",") == "v1,v2"
	}
	if strings.EqualFold(recipe.Primitive, "AVERAGE2") && recipe.ProofState == "EXACT" && len(recipe.Steps) == 3 {
		return strings.EqualFold(recipe.Steps[0].Operation, "ADD") && strings.Join(recipe.Steps[0].Inputs, ",") == "$0,$1" &&
			strings.EqualFold(recipe.Steps[1].Operation, "CONST") && strings.Join(recipe.Steps[1].Inputs, ",") == "2" &&
			strings.EqualFold(recipe.Steps[2].Operation, "DIV") && strings.Join(recipe.Steps[2].Inputs, ",") == "v1,v2"
	}
	return strings.EqualFold(recipe.Primitive, "DOUBLE") && recipe.ProofState == "EXACT" &&
		len(recipe.Steps) == 1 && strings.EqualFold(recipe.Steps[0].Operation, "ADD") &&
		len(recipe.Steps[0].Inputs) == 2 && recipe.Steps[0].Inputs[0] == "$0" && recipe.Steps[0].Inputs[1] == "$0"
}

func applyVerifiedRMSRewrite(u *UniversalASTDocument, match []int, recipe GeneratedLoweringRecipe, target string) (*UniversalASTDocument, error) {
	if len(match) != 1 || !nativeRecipeHasStructuralHandler(recipe) {
		return nil, fmt.Errorf("recipe %q is not the exact RMS formula", recipe.ID)
	}
	allRecipe := GeneratedLoweringRecipe{ID: "recipe.all", Primitive: "ALL", Class: recipe.Class, Guards: recipe.Guards, ProofState: "EXACT", Steps: []LoweringRecipeStep{{Operation: "REDUCE_AND", Inputs: []string{"$0"}, Output: "v1", Order: 1}}}
	rewritten, err := applyVerifiedAllRewrite(u, match, allRecipe, target)
	if err != nil {
		return nil, err
	}
	if rewritten.Metadata == nil {
		rewritten.Metadata = map[string]string{}
	}
	rewritten.Metadata["lowering.recipe"], rewritten.Metadata["lowering.target"], rewritten.Metadata["lowering.proof"] = recipe.ID, NormalizeLanguage(target), recipe.ProofState
	rewritten.Metadata["lowering.builtin"] = "rms"
	// Preserve the original eager evaluation edge through the newly materialized
	// builtin callee. The edge is part of the canonical graph contract, not an
	// emitter-side ordering guess.
	var callee, operand int
	for _, relation := range rewritten.Relations {
		if relation.From != match[0] || relation.Kind != "syntax.child" || relation.To.Domain != "node" {
			continue
		}
		role := ""
		_ = json.Unmarshal(relation.Attributes["role"], &role)
		id, parseErr := strconv.Atoi(relation.To.ID)
		if parseErr != nil {
			return nil, parseErr
		}
		if role == "value" {
			callee = id
		}
		if role == "argument" {
			operand = id
		}
	}
	if callee != 0 && operand != 0 {
		for i := range rewritten.Nodes {
			if rewritten.Nodes[i].ID == callee {
				name, marshalErr := json.Marshal("rms")
				if marshalErr != nil {
					return nil, marshalErr
				}
				rewritten.Nodes[i].Fields["name"] = name
			}
		}
		rewritten.Relations = append(rewritten.Relations, UniversalASTRelation{Kind: "evaluation.before", From: callee, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(operand)}})
	}
	if err := validateUniversalASTDocument(rewritten); err != nil {
		return nil, err
	}
	return rewritten, nil
}

func applyVerifiedAllRewrite(u *UniversalASTDocument, match []int, recipe GeneratedLoweringRecipe, target string) (*UniversalASTDocument, error) {
	if len(match) != 1 || !nativeRecipeHasStructuralHandler(recipe) {
		return nil, fmt.Errorf("recipe %q is not the exact ALL formula", recipe.ID)
	}
	cloned, err := cloneUniversalASTForLowering(u)
	if err != nil {
		return nil, err
	}
	var root *UniversalASTNode
	for i := range cloned.Nodes {
		if cloned.Nodes[i].ID == match[0] {
			root = &cloned.Nodes[i]
			break
		}
	}
	if root == nil {
		return nil, fmt.Errorf("ALL rewrite node %d is missing", match[0])
	}
	common, err := decodeUniversalCommon(root)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(common.Kind, "ALL") && !strings.EqualFold(common.Kind, "RMS") && !strings.EqualFold(common.Operation.SemanticID, "ALL") && !strings.EqualFold(common.Operation.SemanticID, "RMS") && !strings.EqualFold(common.Operation.Semantics.Operation, "ALL") && !strings.EqualFold(common.Operation.Semantics.Operation, "RMS") {
		return nil, fmt.Errorf("verified ALL match node %d has identity %q", root.ID, common.Kind)
	}
	var operand UniversalASTReference
	for _, r := range cloned.Relations {
		if r.Kind != "syntax.child" || r.From != root.ID || r.To.Domain != "node" {
			continue
		}
		role := ""
		_ = json.Unmarshal(r.Attributes["role"], &role)
		if role == "value" || role == "argument" {
			if operand.ID != "" {
				return nil, fmt.Errorf("ALL rewrite node %d has multiple operands", root.ID)
			}
			operand = r.To
		}
	}
	if operand.ID == "" {
		return nil, fmt.Errorf("ALL rewrite node %d has no operand", root.ID)
	}
	nameID, err := cloned.AddNode("SymbolRef", defaultUniversalFacets("SymbolRef"), nil)
	if err != nil {
		return nil, err
	}
	for _, item := range []struct {
		id         int
		kind, name string
		op         universalOperationRecord
	}{{nameID, "identifier", "reduce_and", universalOperationRecord{}}} {
		n := &cloned.Nodes[item.id]
		n.Fields = map[string]json.RawMessage{}
		put := func(key string, value any) {
			if containsString(n.FieldMask, key) {
				data, _ := json.Marshal(value)
				n.Fields[key] = data
			}
		}
		put("id", item.id)
		put("kind", item.kind)
		put("scope_id", common.Scope)
		if item.name != "" {
			put("name", item.name)
		}
		if item.op != (universalOperationRecord{}) {
			put("operation", item.op)
		}
	}
	newRelations := make([]UniversalASTRelation, 0, len(cloned.Relations)+3)
	for _, r := range cloned.Relations {
		if r.From == root.ID && (r.Kind == "syntax.child" || r.Kind == "data.operand" || r.Kind == "operation.kind") {
			continue
		}
		newRelations = append(newRelations, r)
	}
	link := func(to UniversalASTReference, role string, ordinal int) {
		roleJSON, _ := json.Marshal(role)
		ordJSON, _ := json.Marshal(ordinal)
		attrs := map[string]json.RawMessage{"role": roleJSON, "ordinal": ordJSON}
		newRelations = append(newRelations, UniversalASTRelation{Kind: "syntax.child", From: root.ID, To: to, Attributes: attrs})
	}
	link(UniversalASTReference{Domain: "node", ID: strconv.Itoa(nameID)}, "value", 0)
	link(operand, "argument", 0)
	newRelations = append(newRelations,
		UniversalASTRelation{Kind: "operation.kind", From: root.ID, To: UniversalASTReference{Domain: "operation", ID: "call"}},
		UniversalASTRelation{Kind: "call.calls", From: root.ID, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(nameID)}},
	)
	cloned.Relations = newRelations
	for i := range cloned.Nodes {
		if cloned.Nodes[i].ID == match[0] {
			root = &cloned.Nodes[i]
			break
		}
	}
	// A rewritten aggregate operation is a call in the canonical structural
	// graph as well as in its semantic fields. Keeping OperationExpr here would
	// make the SemanticDocument projection lossy and break relation contracts.
	root.StructuralKind = "CallExpr"
	kind, _ := json.Marshal("call")
	root.Fields["kind"] = kind
	op, _ := json.Marshal(universalOperationRecord{Semantics: SemanticSemantics{Operation: "call", Dispatch: "builtin"}})
	root.Fields["operation"] = op
	delete(root.Fields, "operands")
	if cloned.Metadata == nil {
		cloned.Metadata = map[string]string{}
	}
	cloned.Metadata["lowering.recipe"], cloned.Metadata["lowering.target"], cloned.Metadata["lowering.proof"] = recipe.ID, NormalizeLanguage(target), recipe.ProofState
	if true {
		if err := materializeUniversalCrosswalkFields(cloned); err != nil {
			return nil, err
		}
		if err := appendFrontendStructuralClosure(cloned); err != nil {
			return nil, err
		}
		if err := ApplySemanticClosure(cloned); err != nil {
			return nil, err
		}
		if err := validateUniversalASTDocument(cloned); err != nil {
			return nil, err
		}
		return cloned, nil
	}
	doc, err := SemanticDocumentFromUniversalAST(cloned)
	if err != nil {
		return nil, fmt.Errorf("ALL rewrite canonical roundtrip: %w", err)
	}
	canonical, err := ProjectSemanticDocumentToUniversal(doc)
	if err != nil {
		return nil, err
	}
	canonical.Metadata["lowering.recipe"], canonical.Metadata["lowering.target"], canonical.Metadata["lowering.proof"] = recipe.ID, NormalizeLanguage(target), recipe.ProofState
	if err := validateUniversalASTDocument(canonical); err != nil {
		return nil, err
	}
	return canonical, nil
}

// applyVerifiedMeanRewrite materializes DIV(SUM($0), LENGTH($0)) with the
// existing aggregate layout kernels. The repeated input is accepted only after
// the pure-operand gate; effectful aggregates must first be bound explicitly.
func applyVerifiedMeanRewrite(u *UniversalASTDocument, match []int, recipe GeneratedLoweringRecipe, target string) (*UniversalASTDocument, error) {
	if len(match) != 1 || !nativeRecipeHasStructuralHandler(recipe) {
		return nil, fmt.Errorf("recipe %q is not the exact MEAN formula", recipe.ID)
	}
	cloned, err := cloneUniversalASTForLowering(u)
	if err != nil {
		return nil, err
	}
	var root *UniversalASTNode
	for i := range cloned.Nodes {
		if cloned.Nodes[i].ID == match[0] {
			root = &cloned.Nodes[i]
			break
		}
	}
	if root == nil {
		return nil, fmt.Errorf("MEAN rewrite node %d is missing", match[0])
	}
	common, err := decodeUniversalCommon(root)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(common.Kind, "MEAN") && !strings.EqualFold(common.Operation.SemanticID, "MEAN") && !strings.EqualFold(common.Operation.Semantics.Operation, "MEAN") {
		return nil, fmt.Errorf("verified MEAN match node %d has identity %q", root.ID, common.Kind)
	}
	var operand UniversalASTReference
	for _, r := range cloned.Relations {
		if r.Kind != "syntax.child" || r.From != root.ID || r.To.Domain != "node" {
			continue
		}
		role := ""
		_ = json.Unmarshal(r.Attributes["role"], &role)
		if role == "value" || role == "argument" {
			if operand.ID != "" {
				return nil, fmt.Errorf("MEAN rewrite node %d has multiple operands", root.ID)
			}
			operand = r.To
		}
	}
	if operand.ID == "" {
		return nil, fmt.Errorf("MEAN rewrite node %d has no operand", root.ID)
	}
	operandID, err := strconv.Atoi(operand.ID)
	if err != nil {
		return nil, err
	}
	if err := verifyDuplicablePureOperand(cloned, operandID); err != nil {
		return nil, fmt.Errorf("MEAN rewrite operand is not safely reusable: %w", err)
	}
	add := func(structural, kind, name string, operation universalOperationRecord) (int, error) {
		id, err := cloned.AddNode(structural, defaultUniversalFacets(structural), nil)
		if err != nil {
			return 0, err
		}
		n := &cloned.Nodes[id]
		n.Fields = map[string]json.RawMessage{}
		put := func(key string, value any) {
			if containsString(n.FieldMask, key) {
				data, _ := json.Marshal(value)
				n.Fields[key] = data
			}
		}
		put("id", id)
		put("kind", kind)
		put("scope_id", common.Scope)
		if name != "" {
			put("name", name)
		}
		if common.Type.Kind != "" {
			put("type_ref", common.Type)
		}
		if operation != (universalOperationRecord{}) {
			put("operation", operation)
		}
		return id, nil
	}
	sumCall, err := add("CallExpr", "call", "", universalOperationRecord{Semantics: SemanticSemantics{Operation: "call", Dispatch: "builtin"}})
	if err != nil {
		return nil, err
	}
	sumName, err := add("SymbolRef", "identifier", "sum", universalOperationRecord{})
	if err != nil {
		return nil, err
	}
	lenCall, err := add("CallExpr", "call", "", universalOperationRecord{Semantics: SemanticSemantics{Operation: "call", Dispatch: "builtin"}})
	if err != nil {
		return nil, err
	}
	lenName, err := add("SymbolRef", "identifier", "length", universalOperationRecord{})
	if err != nil {
		return nil, err
	}
	newRelations := make([]UniversalASTRelation, 0, len(cloned.Relations)+10)
	for _, r := range cloned.Relations {
		if r.From == root.ID && (r.Kind == "syntax.child" || r.Kind == "data.operand" || r.Kind == "operation.kind") {
			continue
		}
		newRelations = append(newRelations, r)
	}
	link := func(from int, to UniversalASTReference, role string, ordinal int) {
		roleJSON, _ := json.Marshal(role)
		ordJSON, _ := json.Marshal(ordinal)
		attrs := map[string]json.RawMessage{"role": roleJSON, "ordinal": ordJSON}
		newRelations = append(newRelations, UniversalASTRelation{Kind: "syntax.child", From: from, To: to, Attributes: attrs})
		if from == root.ID && (role == "left" || role == "right") {
			newRelations = append(newRelations, UniversalASTRelation{Kind: "data.operand", From: from, To: to})
		}
	}
	link(sumCall, UniversalASTReference{Domain: "node", ID: strconv.Itoa(sumName)}, "value", 0)
	link(sumCall, operand, "argument", 0)
	link(lenCall, UniversalASTReference{Domain: "node", ID: strconv.Itoa(lenName)}, "value", 0)
	link(lenCall, operand, "argument", 0)
	link(root.ID, UniversalASTReference{Domain: "node", ID: strconv.Itoa(sumCall)}, "left", 0)
	link(root.ID, UniversalASTReference{Domain: "node", ID: strconv.Itoa(lenCall)}, "right", 0)
	// Evaluation order is derived from the ordered syntax-child graph by the
	// shared closure. Do not materialize a parallel ad-hoc edge here.
	newRelations = append(newRelations,
		UniversalASTRelation{Kind: "call.calls", From: sumCall, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(sumName)}},
		UniversalASTRelation{Kind: "call.calls", From: lenCall, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(lenName)}},
		UniversalASTRelation{Kind: "operation.kind", From: sumCall, To: UniversalASTReference{Domain: "operation", ID: "call"}},
		UniversalASTRelation{Kind: "operation.kind", From: lenCall, To: UniversalASTReference{Domain: "operation", ID: "call"}},
		UniversalASTRelation{Kind: "operation.kind", From: root.ID, To: UniversalASTReference{Domain: "operation", ID: "div"}},
	)
	cloned.Relations = newRelations
	for i := range cloned.Nodes {
		if cloned.Nodes[i].ID == match[0] {
			root = &cloned.Nodes[i]
			break
		}
	}
	kind, _ := json.Marshal("binary")
	root.Fields["kind"] = kind
	op, _ := json.Marshal(universalOperationRecord{Operator: "/", Semantics: SemanticSemantics{Operation: "div", Dispatch: "builtin"}})
	root.Fields["operation"] = op
	delete(root.Fields, "value")
	if cloned.Metadata == nil {
		cloned.Metadata = map[string]string{}
	}
	cloned.Metadata["lowering.recipe"], cloned.Metadata["lowering.target"], cloned.Metadata["lowering.proof"] = recipe.ID, NormalizeLanguage(target), recipe.ProofState
	if cloned.Projection != "semantic_document.v1" {
		// Frontend facts are already canonical UAST and intentionally need no
		// legacy SemanticDocument compatibility view. Keep the rewritten graph
		// in its sole representation after normal validation.
		if err := materializeUniversalCrosswalkFields(cloned); err != nil {
			return nil, err
		}
		if err := appendFrontendStructuralClosure(cloned); err != nil {
			return nil, err
		}
		if err := ApplySemanticClosure(cloned); err != nil {
			return nil, err
		}
		if err := validateUniversalASTDocument(cloned); err != nil {
			return nil, err
		}
		return cloned, nil
	}
	doc, err := SemanticDocumentFromUniversalAST(cloned)
	if err != nil {
		return nil, fmt.Errorf("MEAN rewrite canonical roundtrip: %w", err)
	}
	canonical, err := ProjectSemanticDocumentToUniversal(doc)
	if err != nil {
		return nil, fmt.Errorf("MEAN rewrite canonical projection: %w", err)
	}
	canonical.Metadata["lowering.recipe"], canonical.Metadata["lowering.target"], canonical.Metadata["lowering.proof"] = recipe.ID, NormalizeLanguage(target), recipe.ProofState
	if err := validateUniversalASTDocument(canonical); err != nil {
		return nil, err
	}
	return canonical, nil
}

// applyVerifiedAverage2Rewrite materializes the nonduplicating scalar formula
// DIV(ADD($0,$1),CONST(2)).  Both source operands keep their single original
// edge; only pure recipe-owned nodes are allocated.  It is deliberately a
// structural graph rewrite rather than an evaluator shortcut.
func applyVerifiedAverage2Rewrite(u *UniversalASTDocument, match []int, recipe GeneratedLoweringRecipe, target string) (*UniversalASTDocument, error) {
	if len(match) != 1 || !nativeRecipeHasStructuralHandler(recipe) {
		return nil, fmt.Errorf("recipe %q is not the exact AVERAGE2 formula", recipe.ID)
	}
	cloned, err := cloneUniversalASTForLowering(u)
	if err != nil {
		return nil, err
	}
	var root *UniversalASTNode
	for i := range cloned.Nodes {
		if cloned.Nodes[i].ID == match[0] {
			root = &cloned.Nodes[i]
			break
		}
	}
	if root == nil {
		return nil, fmt.Errorf("AVERAGE2 rewrite node %d is missing", match[0])
	}
	common, err := decodeUniversalCommon(root)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(common.Kind, "AVERAGE2") && !strings.EqualFold(common.Operation.SemanticID, "AVERAGE2") && !strings.EqualFold(common.Operation.Semantics.Operation, "AVERAGE2") {
		return nil, fmt.Errorf("verified AVERAGE2 match node %d has identity %q", root.ID, common.Kind)
	}
	var operands []UniversalASTReference
	for _, relation := range cloned.Relations {
		if relation.Kind != "syntax.child" || relation.From != root.ID || relation.To.Domain != "node" {
			continue
		}
		role := ""
		_ = json.Unmarshal(relation.Attributes["role"], &role)
		if role == "left" || role == "right" || role == "argument" || role == "value" {
			operands = append(operands, relation.To)
		}
	}
	if len(operands) != 2 {
		return nil, fmt.Errorf("AVERAGE2 rewrite node %d requires exactly two operands, got %d", root.ID, len(operands))
	}
	integerOperands := true
	byID := map[int]*UniversalASTNode{}
	for i := range cloned.Nodes {
		byID[cloned.Nodes[i].ID] = &cloned.Nodes[i]
	}
	for _, operand := range operands {
		id, parseErr := strconv.Atoi(operand.ID)
		candidate := byID[id]
		if parseErr != nil || candidate == nil {
			integerOperands = false
			break
		}
		child, decodeErr := decodeUniversalCommon(candidate)
		if decodeErr != nil || (child.Type.Kind != "integer" && child.Type.Bits == 0 && child.Operation.LiteralKind != "integer") {
			integerOperands = false
			break
		}
	}
	addNode := func(structural, kind string, operation universalOperationRecord) (int, error) {
		id, err := cloned.AddNode(structural, defaultUniversalFacets(structural), nil)
		if err != nil {
			return 0, err
		}
		n := &cloned.Nodes[id]
		n.Fields = map[string]json.RawMessage{}
		put := func(name string, value any) {
			if containsString(n.FieldMask, name) {
				data, _ := json.Marshal(value)
				n.Fields[name] = data
			}
		}
		put("id", id)
		put("kind", kind)
		put("scope_id", common.Scope)
		if common.Type.Kind != "" {
			put("type_ref", common.Type)
		}
		put("operation", operation)
		return id, nil
	}
	addID, err := addNode("OperationExpr", "binary", universalOperationRecord{Operator: "+", Semantics: SemanticSemantics{Operation: "add", Dispatch: "builtin"}})
	if err != nil {
		return nil, err
	}
	// CONST(2) inherits the guarded scalar domain. The generated AVERAGE2
	// recipe is integer-preserving for integer operands; marking it as generic
	// "number" would select binary64 and change the native exit-value ABI.
	twoKind := "number"
	if common.Type.Kind == "integer" || common.Type.Bits > 0 || integerOperands {
		twoKind = "integer"
	}
	twoID, err := addNode("LiteralExpr", "literal", universalOperationRecord{LiteralKind: twoKind, Text: "2"})
	if err != nil {
		return nil, err
	}
	newRelations := make([]UniversalASTRelation, 0, len(cloned.Relations)+8)
	for _, relation := range cloned.Relations {
		if relation.From == root.ID && (relation.Kind == "syntax.child" || relation.Kind == "data.operand" || relation.Kind == "operation.kind") {
			continue
		}
		newRelations = append(newRelations, relation)
	}
	link := func(from int, to UniversalASTReference, role string, ordinal int) {
		attrs := map[string]json.RawMessage{}
		for key, value := range map[string]any{"role": role, "ordinal": ordinal} {
			data, _ := json.Marshal(value)
			attrs[key] = data
		}
		newRelations = append(newRelations, UniversalASTRelation{Kind: "syntax.child", From: from, To: to, Attributes: attrs})
		newRelations = append(newRelations, UniversalASTRelation{Kind: "data.operand", From: from, To: to})
	}
	link(addID, operands[0], "left", 0)
	link(addID, operands[1], "right", 0)
	link(root.ID, UniversalASTReference{Domain: "node", ID: strconv.Itoa(addID)}, "left", 0)
	link(root.ID, UniversalASTReference{Domain: "node", ID: strconv.Itoa(twoID)}, "right", 0)
	newRelations = append(newRelations,
		UniversalASTRelation{Kind: "operation.kind", From: addID, To: UniversalASTReference{Domain: "operation", ID: "add"}},
		UniversalASTRelation{Kind: "operation.kind", From: root.ID, To: UniversalASTReference{Domain: "operation", ID: "div"}},
		UniversalASTRelation{Kind: "evaluation.before", From: addID, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(twoID)}},
	)
	cloned.Relations = newRelations
	// AddNode may have reallocated the slice. Reacquire the result interface
	// before changing it so the mutation reaches the canonical graph.
	for i := range cloned.Nodes {
		if cloned.Nodes[i].ID == match[0] {
			root = &cloned.Nodes[i]
			break
		}
	}
	kind, _ := json.Marshal("binary")
	root.Fields["kind"] = kind
	operation, _ := json.Marshal(universalOperationRecord{Operator: "/", Semantics: SemanticSemantics{Operation: "div", Dispatch: "builtin"}})
	root.Fields["operation"] = operation
	delete(root.Fields, "value")
	if cloned.Metadata == nil {
		cloned.Metadata = map[string]string{}
	}
	cloned.Metadata["lowering.recipe"] = recipe.ID
	cloned.Metadata["lowering.target"] = NormalizeLanguage(target)
	cloned.Metadata["lowering.proof"] = recipe.ProofState
	doc, err := SemanticDocumentFromUniversalAST(cloned)
	if err != nil {
		return nil, fmt.Errorf("AVERAGE2 rewrite canonical roundtrip: %w", err)
	}
	canonical, err := ProjectSemanticDocumentToUniversal(doc)
	if err != nil {
		return nil, fmt.Errorf("AVERAGE2 rewrite canonical projection: %w", err)
	}
	canonical.Metadata["lowering.recipe"] = recipe.ID
	canonical.Metadata["lowering.target"] = NormalizeLanguage(target)
	canonical.Metadata["lowering.proof"] = recipe.ProofState
	if err := validateUniversalASTDocument(canonical); err != nil {
		return nil, err
	}
	return canonical, nil
}

func nativeRecipeMatches(u *UniversalASTDocument, recipe GeneratedLoweringRecipe) []int {
	matched := make([]int, 0)
	for i := range u.Nodes {
		c, err := decodeUniversalCommon(&u.Nodes[i])
		if err != nil {
			continue
		}
		if strings.EqualFold(c.Kind, recipe.Primitive) || strings.EqualFold(c.Operation.SemanticID, recipe.Primitive) || strings.EqualFold(c.Operation.Semantics.Operation, recipe.Primitive) {
			matched = append(matched, u.Nodes[i].ID)
		}
	}
	sort.Ints(matched)
	return matched
}

func nativeUASTDigest(u *UniversalASTDocument) (string, error) {
	encoded, err := json.Marshal(u)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", sum[:]), nil
}

// applyVerifiedDoubleRewrite replaces one DOUBLE operation in-place with the
// proven ADD($0,$0) graph. The node ID remains the result interface, while
// the operand edge is duplicated deliberately because ADD has two operands.
// No detached nodes are created and all incoming uses/control/effect edges are
// therefore preserved.
func applyVerifiedDoubleRewrite(u *UniversalASTDocument, match []int, recipe GeneratedLoweringRecipe, target string) (*UniversalASTDocument, error) {
	// Rewrites are transactional: every mutation below is applied to a private
	// graph copy. A failed proof, malformed relation, or failed canonical
	// roundtrip must leave the caller's canonical UAST byte-for-byte intact.
	var err error
	u, err = cloneUniversalASTForLowering(u)
	if err != nil {
		return nil, err
	}
	if len(match) != 1 || len(recipe.Steps) != 1 || !strings.EqualFold(recipe.Steps[0].Operation, "ADD") ||
		len(recipe.Steps[0].Inputs) != 2 || recipe.Steps[0].Inputs[0] != "$0" || recipe.Steps[0].Inputs[1] != "$0" {
		return nil, fmt.Errorf("recipe %q is not the exact DOUBLE=ADD($0,$0) rewrite", recipe.ID)
	}
	if len(recipe.Guards) == 0 {
		return nil, fmt.Errorf("recipe %q has no numeric guard", recipe.ID)
	}
	byID := map[int]*UniversalASTNode{}
	for i := range u.Nodes {
		byID[u.Nodes[i].ID] = &u.Nodes[i]
	}
	n := byID[match[0]]
	if n == nil {
		return nil, fmt.Errorf("DOUBLE rewrite node %d is missing", match[0])
	}
	c, err := decodeUniversalCommon(n)
	if err != nil {
		return nil, err
	}
	identity := strings.ToUpper(strings.TrimSpace(c.Kind))
	if identity != "DOUBLE" && !strings.EqualFold(c.Operation.SemanticID, "DOUBLE") && !strings.EqualFold(c.Operation.Semantics.Operation, "DOUBLE") {
		return nil, fmt.Errorf("verified DOUBLE match node %d has identity %q", n.ID, identity)
	}
	var operand *UniversalASTRelation
	for i := range u.Relations {
		r := &u.Relations[i]
		if r.Kind != "syntax.child" || r.From != n.ID || r.To.Domain != "node" {
			continue
		}
		role := ""
		_ = json.Unmarshal(r.Attributes["role"], &role)
		if role == "value" || role == "argument" || role == "left" {
			if operand != nil {
				return nil, fmt.Errorf("DOUBLE rewrite node %d has multiple candidate operands", n.ID)
			}
			operand = r
		}
	}
	if operand == nil {
		return nil, fmt.Errorf("DOUBLE rewrite node %d has no operand", n.ID)
	}
	operandID, err := strconv.Atoi(operand.To.ID)
	if err != nil || operand.To.Domain != "node" {
		return nil, fmt.Errorf("DOUBLE rewrite node %d has invalid operand reference", n.ID)
	}
	maxID := 0
	nodeCopies := map[int]UniversalASTNode{}
	for _, node := range u.Nodes {
		if node.ID > maxID {
			maxID = node.ID
		}
		copyNode := node
		copyNode.Fields = map[string]json.RawMessage{}
		for key, value := range node.Fields {
			copyNode.Fields[key] = append(json.RawMessage(nil), value...)
		}
		nodeCopies[node.ID] = copyNode
	}
	nextID := maxID + 1
	if _, ok := nodeCopies[operandID]; !ok {
		return nil, fmt.Errorf("DOUBLE rewrite operand node %d is missing", operandID)
	}
	if err := verifyDuplicablePureOperand(u, operandID); err != nil {
		return nil, fmt.Errorf("DOUBLE rewrite operand is not safely duplicable: %w", err)
	}
	relations := append([]UniversalASTRelation(nil), u.Relations...)
	cloned := map[int]int{}
	visiting := map[int]bool{}
	var cloneSubgraph func(int) (int, error)
	cloneSubgraph = func(oldID int) (int, error) {
		if newID, ok := cloned[oldID]; ok {
			return newID, nil
		}
		if visiting[oldID] {
			return 0, fmt.Errorf("DOUBLE rewrite operand graph contains a cycle at node %d", oldID)
		}
		oldNode, ok := nodeCopies[oldID]
		if !ok {
			return 0, fmt.Errorf("DOUBLE rewrite operand node %d is missing", oldID)
		}
		visiting[oldID] = true
		newID := nextID
		nextID++
		cloned[oldID] = newID
		oldNode.ID = newID
		newIDRaw, _ := json.Marshal(newID)
		oldNode.Fields["id"] = newIDRaw
		u.Nodes = append(u.Nodes, oldNode)
		for _, relation := range relations {
			if relation.Kind != "syntax.child" || relation.From != oldID || relation.To.Domain != "node" {
				continue
			}
			childID, parseErr := strconv.Atoi(relation.To.ID)
			if parseErr != nil {
				return 0, fmt.Errorf("DOUBLE rewrite child reference %q is invalid", relation.To.ID)
			}
			cloneChild, cloneErr := cloneSubgraph(childID)
			if cloneErr != nil {
				return 0, cloneErr
			}
			relation.From = newID
			relation.To = UniversalASTReference{Domain: "node", ID: strconv.Itoa(cloneChild)}
			u.Relations = append(u.Relations, relation)
		}
		delete(visiting, oldID)
		return newID, nil
	}
	cloneID, err := cloneSubgraph(operandID)
	if err != nil {
		return nil, err
	}
	// Appending may reallocate the node slice; reacquire the matched node before
	// changing its fields so the rewrite mutates the canonical document.
	for i := range u.Nodes {
		if u.Nodes[i].ID == match[0] {
			n = &u.Nodes[i]
			break
		}
	}
	kind, _ := json.Marshal("binary")
	n.Fields["kind"] = kind
	delete(n.Fields, "value")
	semantics := c.Operation.Semantics
	semantics.Operation = "add"
	if semantics.Dispatch == "" {
		semantics.Dispatch = "builtin"
	}
	op := universalOperationRecord{Semantics: semantics, Operator: "+"}
	if c.Type.Kind == "integer" {
		op.Typed = &SemanticOperation{Name: "integer.add", Type: c.Type}
	}
	operation, _ := json.Marshal(op)
	n.Fields["operation"] = operation
	operands, _ := json.Marshal([]universalReferenceField{
		{Role: "left", Reference: operand.To},
		{Role: "right", Reference: UniversalASTReference{Domain: "node", ID: strconv.Itoa(cloneID)}},
	})
	n.Fields["operands"] = operands
	newRelations := make([]UniversalASTRelation, 0, len(u.Relations)+1)
	for _, r := range u.Relations {
		if r.Kind == "data.operand" && r.From == n.ID {
			continue
		}
		if r.Kind == "operation.kind" && r.From == n.ID {
			continue
		}
		if r.Kind == "syntax.child" && r.From == n.ID {
			role := ""
			_ = json.Unmarshal(r.Attributes["role"], &role)
			if role == "value" || role == "argument" || role == "left" {
				continue
			}
		}
		newRelations = append(newRelations, r)
	}
	for _, item := range []struct {
		role string
		ref  UniversalASTReference
	}{
		{role: "left", ref: operand.To},
		{role: "right", ref: UniversalASTReference{Domain: "node", ID: strconv.Itoa(cloneID)}},
	} {
		attrs := map[string]json.RawMessage{}
		for key, value := range map[string]any{"role": item.role, "ordinal": 0} {
			data, _ := json.Marshal(value)
			attrs[key] = data
		}
		newRelations = append(newRelations, UniversalASTRelation{Kind: "syntax.child", From: n.ID, To: item.ref, Attributes: attrs})
		newRelations = append(newRelations, UniversalASTRelation{Kind: "data.operand", From: n.ID, To: item.ref})
	}
	newRelations = append(newRelations,
		UniversalASTRelation{Kind: "operation.kind", From: n.ID, To: UniversalASTReference{Domain: "operation", ID: "add"}},
		UniversalASTRelation{Kind: "evaluation.before", From: operandID, To: UniversalASTReference{Domain: "node", ID: strconv.Itoa(cloneID)}},
	)
	u.Relations = newRelations
	if u.Metadata == nil {
		u.Metadata = map[string]string{}
	}
	u.Metadata["lowering.recipe"] = recipe.ID
	u.Metadata["lowering.target"] = NormalizeLanguage(target)
	u.Metadata["lowering.proof"] = recipe.ProofState
	// Rebuild all derived crosswalk fields and evidence from the rewritten
	// syntax graph. Directly copying one new node cannot keep the matrix planes
	// authoritative when a rewrite changes node cardinality.
	doc, err := SemanticDocumentFromUniversalAST(u)
	if err != nil {
		return nil, fmt.Errorf("DOUBLE rewrite canonical roundtrip: %w", err)
	}
	canonical, err := ProjectSemanticDocumentToUniversal(doc)
	if err != nil {
		return nil, fmt.Errorf("DOUBLE rewrite canonical projection: %w", err)
	}
	canonical.Metadata["lowering.recipe"] = recipe.ID
	canonical.Metadata["lowering.target"] = NormalizeLanguage(target)
	canonical.Metadata["lowering.proof"] = recipe.ProofState
	if err := validateUniversalASTDocument(canonical); err != nil {
		return nil, err
	}
	return canonical, nil
}

// verifyDuplicablePureOperand is the evaluation/effect gate for a rewrite
// that materializes a second operand graph. It intentionally recognizes only
// deterministic scalar expression families already selected directly by the
// native backend. Calls, allocation, unknown effects, control and lifetime
// nodes fail closed instead of silently changing evaluation count.
func verifyDuplicablePureOperand(u *UniversalASTDocument, root int) error {
	byID := make(map[int]*UniversalASTNode, len(u.Nodes))
	for i := range u.Nodes {
		byID[u.Nodes[i].ID] = &u.Nodes[i]
	}
	visiting := map[int]bool{}
	var visit func(int) error
	visit = func(id int) error {
		if visiting[id] {
			return fmt.Errorf("cycle at node %d", id)
		}
		n := byID[id]
		if n == nil {
			return fmt.Errorf("missing node %d", id)
		}
		c, err := decodeUniversalCommon(n)
		if err != nil {
			return err
		}
		if len(c.Effects) != 0 || c.Operation.CallResolution != nil {
			return fmt.Errorf("node %d kind %q carries observable effects", id, c.Kind)
		}
		switch c.Kind {
		case "literal", "identifier":
			return nil
		case "unary", "binary", "typed_operation", "aggregate", "tuple":
			visiting[id] = true
			defer delete(visiting, id)
			for _, relation := range u.Relations {
				if relation.Kind != "syntax.child" || relation.From != id || relation.To.Domain != "node" {
					continue
				}
				child, parseErr := strconv.Atoi(relation.To.ID)
				if parseErr != nil {
					return fmt.Errorf("invalid child reference %q", relation.To.ID)
				}
				if err := visit(child); err != nil {
					return err
				}
			}
			return nil
		default:
			return fmt.Errorf("node %d kind %q is outside the pure duplication family", id, c.Kind)
		}
	}
	return visit(root)
}

func LinkRequiredRuntime(program *SemanticProgram, target string) error {
	reqs, err := CollectSemanticRequirements(program, target)
	if err != nil {
		return err
	}
	basis, err := BuildNativeCapabilityBasis(target)
	if err != nil {
		return err
	}
	missing := make([]string, 0)
	for _, requirement := range reqs.Runtime {
		if !basis.Runtime[requirement] {
			missing = append(missing, requirement)
		}
	}
	if len(missing) != 0 {
		return fmt.Errorf("native runtime requirements are not implemented: %s", strings.Join(missing, ", "))
	}
	return nil
}

func LowerSemanticToNative(program *SemanticProgram, target string) (*SemanticProgram, NativeClosure, error) {
	resolved, err := ResolveCompileTimeConstructs(program, target)
	if err != nil {
		return nil, NativeClosure{}, err
	}
	program = resolved
	reqs, err := CollectSemanticRequirements(program, target)
	if err != nil {
		return nil, NativeClosure{}, err
	}
	basis, err := BuildNativeCapabilityBasis(target)
	if err != nil {
		return nil, NativeClosure{}, err
	}
	report, err := CompileUniversalPrimitiveSpecs()
	if err != nil {
		return nil, NativeClosure{}, err
	}
	closure := SolveExactClosure(reqs, report.Recipes, basis)
	if !closure.Supported {
		return nil, closure, fmt.Errorf("native semantic closure missing: %s", strings.Join(closure.Missing, ", "))
	}
	if err := LinkRequiredRuntime(program, target); err != nil {
		return nil, closure, err
	}
	// Canonicalization may have materialized the UAST from the compatibility
	// tree. Keep that same document on the returned program so the next stage
	// cannot accidentally rebuild a different semantic view.
	return program, closure, nil
}

func EmitNativeExecutable(program *SemanticProgram, target string, entry string) (CompileResult, error) {
	lowered, _, err := LowerSemanticToNative(program, target)
	if err != nil {
		return CompileResult{}, err
	}
	return CompileMachine(lowered, CompileOptions{TargetArch: "x86_64", TargetOS: "windows", ABI: "win64", OutputKind: CompileExecutable, EntryPoint: entry})
}

func appendUnique(values []string, value string) []string {
	if value == "" {
		return values
	}
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}
