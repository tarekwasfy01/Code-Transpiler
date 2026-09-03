package backend

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// The values below are runtime carriers only. They are never stored back into
// the UAST and therefore do not form another semantic representation.
type runUASTCell struct{ value any }

type runUASTTask struct {
	value any
	err   error
}

type runUASTChannel struct {
	values chan any
	once   sync.Once
}

type runUASTException struct{ value any }

func (e runUASTException) Error() string { return "UAST exception: " + runText(e.value) }

type runUASTDeferred struct {
	env   *runEnv
	graph *uastExecutionGraph
	node  int
}

func (st *runState) deferUAST(env *runEnv, graph *uastExecutionGraph, node int) {
	st.deferred = append(st.deferred, runUASTDeferred{env: env, graph: graph, node: node})
}

func (st *runState) runDeferred(start int) (any, runSignal, error) {
	var last any
	for i := len(st.deferred) - 1; i >= start; i-- {
		entry := st.deferred[i]
		value, signal, err := st.uastStmt(entry.env, entry.graph, entry.node)
		if err != nil || signal != runNormal {
			st.deferred = st.deferred[:start]
			return value, signal, err
		}
		last = value
	}
	st.deferred = st.deferred[:start]
	return last, runNormal, nil
}

func (g *uastExecutionGraph) orderedChildren(id int) []universalChild {
	out := []universalChild{}
	for _, children := range g.children[id] {
		out = append(out, children...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Meta.Ordinal != out[j].Meta.Ordinal {
			return out[i].Meta.Ordinal < out[j].Meta.Ordinal
		}
		if out[i].Meta.Role != out[j].Meta.Role {
			return out[i].Meta.Role < out[j].Meta.Role
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (g *uastExecutionGraph) firstChild(id int, roles ...string) (int, bool, error) {
	for _, role := range roles {
		if child, ok, err := g.one(id, role, false); err != nil || ok {
			return child, ok, err
		}
	}
	children := g.orderedChildren(id)
	if len(children) == 0 {
		return 0, false, nil
	}
	return children[0].ID, true, nil
}

func (g *uastExecutionGraph) structureRequires(kind string, primitive UASTExecutionPrimitive) bool {
	analysis, err := UniversalExecutionAnalysis()
	if err != nil {
		return false
	}
	row, col := indexOf(analysis.Structures, kind), -1
	for i, spec := range analysis.Primitives {
		if spec.ID == primitive {
			col = i
			break
		}
	}
	return row >= 0 && col >= 0 && analysis.MSE.At(row, col) != 0
}

func (st *runState) uastEvaluateNode(env *runEnv, g *uastExecutionGraph, id int) (any, error) {
	c := g.common[id]
	if _, direct := directSemanticStructure[c.Kind]; direct {
		if c.Kind == "block" || c.Kind == "expression" || c.Kind == "assign" || c.Kind == "if" || c.Kind == "while" || c.Kind == "repeat" || c.Kind == "for" || c.Kind == "return" || c.Kind == "break" || c.Kind == "continue" {
			value, _, err := st.uastStmt(env, g, id)
			return value, err
		}
		return st.uastExpr(env, g, id)
	}
	kind := g.nodes[id].StructuralKind
	if g.structureRequires(kind, execExpression) && !g.structureRequires(kind, execDeclaration) && !g.structureRequires(kind, execControl) {
		return st.uastPrimitiveExpression(env, g, id)
	}
	value, _, err := st.uastPrimitiveStatement(env, g, id)
	return value, err
}

func (st *runState) uastPrimitiveStatement(env *runEnv, g *uastExecutionGraph, id int) (any, runSignal, error) {
	c, structural := g.common[id], g.nodes[id].StructuralKind
	evaluate := func(roles ...string) (any, bool, error) {
		child, ok, err := g.firstChild(id, roles...)
		if err != nil || !ok {
			return nil, ok, err
		}
		value, err := st.uastEvaluateNode(env, g, child)
		return value, true, err
	}
	switch structural {
	case "VariableDecl", "ConstantDecl", "Binding", "VariableDeclGroup":
		value, _, err := evaluate("initializer", "expression", "value")
		if err != nil {
			return nil, runNormal, err
		}
		if c.Name == "" {
			// Groups can consist solely of child declarations.
			var last any
			for _, child := range g.orderedChildren(id) {
				last, _, err = st.uastStmt(env, g, child.ID)
				if err != nil {
					return nil, runNormal, err
				}
			}
			return last, runNormal, nil
		}
		env.declare(c.Name, value, structural != "ConstantDecl")
		return value, runNormal, nil
	case "FunctionDecl", "MethodDecl":
		value, err := st.uastFunctionValue(env, g, id)
		if err != nil {
			return nil, runNormal, err
		}
		if c.Name == "" {
			return nil, runNormal, fmt.Errorf("%s node %d lacks a binding name", structural, id)
		}
		env.declare(c.Name, value, false)
		return value, runNormal, nil
	case "ImportDecl":
		module := c.Name
		if module == "" {
			_ = decodeUniversalField(g.nodes[id], "module", &module)
		}
		if module == "" {
			return nil, runNormal, fmt.Errorf("import node %d lacks module identity", id)
		}
		env.declare(module, map[string]any{"module": module}, false)
		return module, runNormal, nil
	case "ModuleDecl":
		moduleEnv := newRunEnv(env)
		var last any
		for _, child := range g.orderedChildren(id) {
			value, signal, err := st.uastStmt(moduleEnv, g, child.ID)
			if err != nil || signal != runNormal {
				return value, signal, err
			}
			last = value
		}
		if c.Name != "" {
			env.declare(c.Name, moduleEnv.vars, false)
		}
		return last, runNormal, nil
	case "NominalTypeDecl", "RecordTypeDecl", "InterfaceTypeDecl", "VariantTypeDecl", "GenericParamDecl":
		value := any(c.Type)
		if c.Type.Kind == "" {
			value = map[string]any{"kind": structural, "name": c.Name}
		}
		if c.Name != "" {
			env.declare(c.Name, value, false)
		}
		return value, runNormal, nil
	case "TryStmt":
		body, ok, err := g.firstChild(id, "body", "try")
		if err != nil || !ok {
			return nil, runNormal, fmt.Errorf("try node %d lacks body", id)
		}
		value, signal, runErr := st.uastStmt(env, g, body)
		if runErr != nil {
			var thrown runUASTException
			if !errors.As(runErr, &thrown) {
				return nil, runNormal, runErr
			}
			st.currentException = thrown.value
			if handler, found, findErr := g.firstChild(id, "catch", "handler"); findErr != nil {
				return nil, runNormal, findErr
			} else if found {
				value, signal, runErr = st.uastStmt(env, g, handler)
			} else {
				return nil, runNormal, runErr
			}
		}
		if cleanup, found, findErr := g.firstChild(id, "finally", "cleanup"); findErr != nil {
			return nil, runNormal, findErr
		} else if found {
			if cleanupValue, cleanupSignal, cleanupErr := st.uastStmt(env, g, cleanup); cleanupErr != nil || cleanupSignal != runNormal {
				return cleanupValue, cleanupSignal, cleanupErr
			}
		}
		return value, signal, runErr
	case "RaisePanicStmt":
		value, ok, err := evaluate("value", "expression")
		if err != nil {
			return nil, runNormal, err
		}
		if !ok {
			value = c.Operation.Text
		}
		return nil, runNormal, runUASTException{value: value}
	case "DeferStmt", "DeferCleanupStmt":
		body, bodyOK, err := g.firstChild(id, "body")
		if err != nil {
			return nil, runNormal, err
		}
		cleanup, cleanupOK, err := g.firstChild(id, "cleanup", "defer")
		if err != nil {
			return nil, runNormal, err
		}
		if cleanupOK {
			st.deferUAST(env, g, cleanup)
		}
		if bodyOK {
			return st.uastStmt(env, g, body)
		}
		return nil, runNormal, nil
	case "SpawnStmt":
		body, ok, err := g.firstChild(id, "body", "value")
		if err != nil || !ok {
			return nil, runNormal, fmt.Errorf("spawn node %d lacks body", id)
		}
		value, _, runErr := st.uastStmt(newRunEnv(env), g, body)
		task := &runUASTTask{value: value, err: runErr}
		if c.Name != "" {
			env.declare(c.Name, task, false)
		}
		return task, runNormal, nil
	case "SendStmt":
		channelValue, _, err := evaluate("channel", "receiver")
		if err != nil {
			return nil, runNormal, err
		}
		value, _, err := evaluate("value", "expression")
		if err != nil {
			return nil, runNormal, err
		}
		channel, ok := channelValue.(*runUASTChannel)
		if !ok {
			return nil, runNormal, fmt.Errorf("send node %d target is not a channel", id)
		}
		channel.values <- value
		return value, runNormal, nil
	case "CloseOp":
		value, _, err := evaluate("channel", "value")
		if err != nil {
			return nil, runNormal, err
		}
		channel, ok := value.(*runUASTChannel)
		if !ok {
			return nil, runNormal, fmt.Errorf("close node %d target is not a channel", id)
		}
		channel.once.Do(func() { close(channel.values) })
		return nil, runNormal, nil
	case "AtomicOp", "SynchronizationOp":
		value, _, err := evaluate("body", "value", "expression")
		return value, runNormal, err
	case "SelectStmt", "SwitchMatchStmt":
		condition, hasCondition, err := evaluate("condition", "value")
		if err != nil {
			return nil, runNormal, err
		}
		for _, child := range g.orderedChildren(id) {
			if hasCondition && child.Meta.Role == "condition" {
				continue
			}
			if child.Meta.Role == "case" || child.Meta.Role == "branch" || child.Meta.Role == "default" {
				if child.Meta.Role == "default" || child.Meta.Name == "" || child.Meta.Name == runText(condition) {
					return st.uastStmt(env, g, child.ID)
				}
			}
		}
		return nil, runNormal, nil
	case "GotoLabelStmt":
		if target, ok, err := g.oneRelationNode(id, "control.next", false); err != nil {
			return nil, runNormal, err
		} else if ok && c.Kind == "goto" {
			st.jumpTarget = target
		}
		return nil, runNormal, nil
	case "Annotation", "Visibility", "Effect", "ExecutionModel", "IRFact", "LoweringFact", "OptimizationFact", "LayoutContract", "ABIContract", "MemoryModelContract", "OwnershipSemantics", "SafetyRegion", "LifetimeRegion", "BindingResolution", "DispatchResolution", "DispatchSemantics", "MethodSet", "ShadowingRelation", "CaptureRelation", "TypeInferenceFact", "TypeRelation":
		return nil, runNormal, nil
	}
	// Matrix-classified expression nodes can appear directly as statements.
	if g.structureRequires(structural, execExpression) {
		value, err := st.uastPrimitiveExpression(env, g, id)
		return value, runNormal, err
	}
	var last any
	for _, child := range g.orderedChildren(id) {
		value, signal, err := st.uastStmt(env, g, child.ID)
		if err != nil || signal != runNormal {
			return value, signal, err
		}
		last = value
	}
	return last, runNormal, nil
}

func (st *runState) uastPrimitiveExpression(env *runEnv, g *uastExecutionGraph, id int) (any, error) {
	c, structural := g.common[id], g.nodes[id].StructuralKind
	evaluate := func(roles ...string) (any, bool, error) {
		child, ok, err := g.firstChild(id, roles...)
		if err != nil || !ok {
			return nil, ok, err
		}
		value, err := st.uastEvaluateNode(env, g, child)
		return value, true, err
	}
	switch structural {
	case "AggregateExpr", "TupleExpr", "TupleResult", "ComprehensionExpr":
		values := []any{}
		for _, child := range g.orderedChildren(id) {
			value, err := st.uastEvaluateNode(env, g, child.ID)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
		return values, nil
	case "ConvertExpr", "TypeAssertExpr":
		value, ok, err := evaluate("value", "expression", "operand")
		if err != nil || !ok {
			return nil, fmt.Errorf("conversion node %d lacks value", id)
		}
		return st.uastConversion(value, c.Type)
	case "AddressOf":
		value, ok, err := evaluate("value", "operand")
		if err != nil || !ok {
			return nil, fmt.Errorf("address node %d lacks value", id)
		}
		return &runUASTCell{value: value}, nil
	case "Deref":
		value, ok, err := evaluate("value", "operand")
		if err != nil || !ok {
			return nil, fmt.Errorf("deref node %d lacks pointer", id)
		}
		cell, ok := value.(*runUASTCell)
		if !ok {
			return nil, fmt.Errorf("deref node %d value is not a pointer", id)
		}
		return cell.value, nil
	case "MemberAccessExpr", "MethodValue", "MethodExpr":
		value, ok, err := evaluate("receiver", "value")
		if err != nil || !ok {
			return nil, fmt.Errorf("member node %d lacks receiver", id)
		}
		if object, ok := value.(map[string]any); ok {
			return object[c.Name], nil
		}
		return nil, fmt.Errorf("member %q requires an aggregate value", c.Name)
	case "MapIndexExpr":
		value, ok, err := evaluate("value", "receiver")
		if err != nil || !ok {
			return nil, fmt.Errorf("map index node %d lacks receiver", id)
		}
		key, keyOK, err := evaluate("index", "argument")
		if err != nil || !keyOK {
			return nil, fmt.Errorf("map index node %d lacks key", id)
		}
		if object, ok := value.(map[string]any); ok {
			return object[runText(key)], nil
		}
		return nil, fmt.Errorf("map index requires map value")
	case "SliceExpr":
		value, ok, err := evaluate("value", "receiver")
		if err != nil || !ok {
			return nil, fmt.Errorf("slice node %d lacks value", id)
		}
		items := runVec(value)
		bounds := g.many(id, "argument")
		start, end := 0, len(items)
		if len(bounds) > 0 {
			v, err := st.uastEvaluateNode(env, g, bounds[0].ID)
			if err != nil {
				return nil, err
			}
			start = int(runNumber(v)) - g.document.IndexBase
		}
		if len(bounds) > 1 {
			v, err := st.uastEvaluateNode(env, g, bounds[1].ID)
			if err != nil {
				return nil, err
			}
			end = int(runNumber(v)) - g.document.IndexBase
		}
		if start < 0 || end < start || end > len(items) {
			return nil, fmt.Errorf("slice bounds [%d:%d] outside length %d", start, end, len(items))
		}
		return append([]any(nil), items[start:end]...), nil
	case "AwaitExpr":
		value, ok, err := evaluate("value", "task")
		if err != nil || !ok {
			return nil, fmt.Errorf("await node %d lacks task", id)
		}
		task, ok := value.(*runUASTTask)
		if !ok {
			return nil, fmt.Errorf("await node %d value is not a task", id)
		}
		return task.value, task.err
	case "ReceiveExpr":
		value, ok, err := evaluate("channel", "value")
		if err != nil || !ok {
			return nil, fmt.Errorf("receive node %d lacks channel", id)
		}
		channel, ok := value.(*runUASTChannel)
		if !ok {
			return nil, fmt.Errorf("receive node %d value is not a channel", id)
		}
		item, open := <-channel.values
		if !open {
			return nil, nil
		}
		return item, nil
	case "ChannelTypeOp":
		return &runUASTChannel{values: make(chan any, 1024)}, nil
	case "CompileTimeEval", "MacroCompileTime", "Instantiation", "ReflectionOp", "UnsafeIntrinsic", "YieldExpr", "PanicRecover":
		if raw := g.nodes[id].Fields["compiletime_value"]; len(raw) > 0 {
			var value any
			if err := json.Unmarshal(raw, &value); err != nil {
				return nil, err
			}
			return value, nil
		}
		if structural == "PanicRecover" && st.currentException != nil {
			return st.currentException, nil
		}
		if value, ok, err := evaluate("value", "expression", "body"); err != nil || ok {
			return value, err
		}
		return nil, nil
	case "ForeignDeclCall":
		args := []any{}
		for _, child := range g.many(id, "argument") {
			value, err := st.uastEvaluateNode(env, g, child.ID)
			if err != nil {
				return nil, err
			}
			args = append(args, value)
		}
		name := c.Name
		if name == "" {
			name = c.Operation.Text
		}
		if name == "" {
			return nil, fmt.Errorf("foreign call node %d lacks symbol", id)
		}
		return st.primitive(name, args, nil)
	case "PointerType", "ReferenceType", "ArraySliceType", "BoolType", "ChannelType", "FloatType", "FunctionType", "IntegerType", "InterfaceType", "MapType", "OptionalType", "SliceType", "StringType", "TupleType", "UnionVariantType", "TypeConstraint", "TypeSet", "GenericParam":
		if c.Type.Kind != "" || c.Type.Name != "" {
			return c.Type, nil
		}
		return map[string]any{"kind": structural, "name": c.Name}, nil
	}
	if raw := g.nodes[id].Fields["value"]; len(raw) > 0 {
		var value any
		if err := json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		return value, nil
	}
	if value, ok, err := evaluate("value", "expression", "body", "operand"); err != nil || ok {
		return value, err
	}
	return nil, fmt.Errorf("EXECUTION_SEMANTIC_GAP: structure %q node %d has no value contract", structural, id)
}

func (st *runState) uastFunctionValue(env *runEnv, g *uastExecutionGraph, id int) (*runUASTFunction, error) {
	c := g.common[id]
	body, ok, err := g.firstChild(id, "body")
	if err != nil || !ok {
		return nil, fmt.Errorf("function node %d lacks body", id)
	}
	fn := &runUASTFunction{binding: c.Operation.FunctionBinding, defaultEvaluation: c.Operation.DefaultEvaluation, body: body, graph: g, env: env}
	for _, item := range g.many(id, "parameter") {
		p := g.common[item.ID]
		param := runUASTParameter{name: p.Name, mode: p.Operation.ParameterMode, passing: p.Operation.ParameterPassing, defaultNode: -1}
		if p.Type.Kind != "" {
			typ := p.Type
			param.typ = &typ
		}
		if q, found, findErr := g.one(item.ID, "default", false); findErr != nil {
			return nil, findErr
		} else if found {
			param.defaultNode = q
		}
		fn.params = append(fn.params, param)
	}
	fn.defaults = make([]any, len(fn.params))
	return fn, nil
}

func (st *runState) uastConversion(value any, target SemanticType) (any, error) {
	kind := strings.ToLower(target.Kind)
	if kind == "" {
		kind = strings.ToLower(target.Name)
	}
	switch kind {
	case "", "any", "dynamic", "unknown", "interface":
		return value, nil
	case "bool", "boolean", "truth":
		return runTruth(value)
	case "string", "text", "utf8":
		return runText(value), nil
	case "int", "integer", "uint", "uintptr":
		n := runNumber(value)
		if math.IsNaN(n) || math.IsInf(n, 0) {
			return nil, fmt.Errorf("cannot convert %v to integer", value)
		}
		return float64(int64(n)), nil
	case "float", "float32", "float64", "number", "numeric", "binary64":
		return runNumber(value), nil
	case "slice", "array", "tuple", "vector", "list":
		return runVec(value), nil
	case "optional", "nullable", "pointer", "reference":
		return value, nil
	default:
		return nil, fmt.Errorf("conversion to type %q is not defined", kind)
	}
}

func runUASTIndex(value any) int {
	if number, ok := value.(json.Number); ok {
		n, _ := strconv.Atoi(string(number))
		return n
	}
	return int(runNumber(value))
}
