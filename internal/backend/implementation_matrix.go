package backend

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/tarekwasfy01/Code-Transpiler/internal/matrixir"
)

// ImplementationMatrix describes installed lowering paths, not test results.
// The operation basis and availability planes also gate actual code emission.
type ImplementationMatrix struct {
	Operations  []string              `json:"operations"`
	Stages      []string              `json:"stages"`
	Implemented matrixir.SparseMatrix `json:"implemented"`
	Unsupported matrixir.SparseMatrix `json:"unsupported"`
	Arity       matrixir.Vector       `json:"operand_count"`
	Results     []string              `json:"result_kinds"`
	Evidence    string                `json:"evidence_policy"`
	Languages   []string              `json:"languages"`
	Routes      matrixir.SparseMatrix `json:"declared_complete_operation_routes"`
}

func TypedImplementationMatrix() ImplementationMatrix {
	m := ImplementationMatrix{Stages: []string{"semantic_json", "runtime"}, Evidence: "Implementation declarations; external execution is verified separately by native integer differential tests."}
	for _, source := range Frontends() {
		m.Stages = append(m.Stages, "frontend."+source.ID)
		m.Languages = append(m.Languages, source.ID)
	}
	for name := range integerRules {
		m.Operations = append(m.Operations, name)
	}
	sort.Strings(m.Operations)
	for _, target := range Backends() {
		m.Stages = append(m.Stages, "target."+target.ID)
	}
	m.Implemented = matrixir.NewSparseMatrix(len(m.Operations), len(m.Stages))
	m.Unsupported = matrixir.NewSparseMatrix(len(m.Operations), len(m.Stages))
	for row, name := range m.Operations {
		m.Arity = append(m.Arity, float64(integerRules[name].arity))
		m.Results = append(m.Results, integerRules[name].result)
		for col, stage := range m.Stages {
			switch stage {
			case "semantic_json", "runtime", "frontend.go", "target.go", "target.python", "target.c", "target.rust", "target.cpp", "target.java", "target.csharp":
				m.Implemented.Set(row, col, 1)
			default:
				m.Unsupported.Set(row, col, 1)
			}
		}
	}
	// A source-target route exists only when both adapters implement every
	// operation in this matrix. This is availability, not semantic test proof.
	missing, _ := m.Reject(m.Operations)
	front := matrixir.NewSparseMatrix(1, len(m.Languages))
	back := matrixir.NewSparseMatrix(1, len(m.Languages))
	for i := range m.Languages {
		if missing.At(0, 2+i) == 0 {
			front.Set(0, i, 1)
		}
		if missing.At(0, 2+len(m.Languages)+i) == 0 {
			back.Set(0, i, 1)
		}
	}
	m.Routes, _ = front.Transpose().Multiply(back)
	return m
}
func (m ImplementationMatrix) Reject(required []string) (matrixir.SparseMatrix, error) {
	vector := matrixir.NewSparseMatrix(1, len(m.Operations))
	for _, name := range required {
		row := sort.SearchStrings(m.Operations, name)
		if row == len(m.Operations) || m.Operations[row] != name {
			return matrixir.SparseMatrix{}, fmt.Errorf("operation %q missing from implementation matrix", name)
		}
		vector.Set(0, row, 1)
	}
	return vector.Multiply(m.Unsupported)
}
func (m ImplementationMatrix) Check(required []string, stage string) error {
	rejected, err := m.Reject(required)
	if err != nil {
		return err
	}
	for col, name := range m.Stages {
		if name == stage {
			if rejected.At(0, col) > 0 {
				return fmt.Errorf("%s: %.0f operations lack an implementation", stage, rejected.At(0, col))
			}
			return nil
		}
	}
	return fmt.Errorf("unregistered implementation stage %q", stage)
}

type typedOperationVisitor struct {
	required        []string
	functions       map[string]*SemanticFunction
	integerBindings map[string]bool
	integerResults  map[string]bool
}

func (v *typedOperationVisitor) integerExpression(e *SemanticExpression) bool {
	if e == nil {
		return false
	}
	if e.Operation != nil {
		return e.Operation.resultType().Kind == "integer"
	}
	if e.Kind == "identifier" {
		return v.integerBindings[e.Name]
	}
	return e.Kind == "call" && e.Value != nil && v.integerResults[e.Value.Name]
}
func (v *typedOperationVisitor) EnterStatement(s *SemanticStatement) error {
	if v.integerExpression(s.Condition) || v.integerExpression(s.Sequence) {
		return fmt.Errorf("integer control/index input requires explicit modeled semantics")
	}
	return nil
}
func (*typedOperationVisitor) LeaveStatement(*SemanticStatement) error   { return nil }
func (*typedOperationVisitor) LeaveExpression(*SemanticExpression) error { return nil }
func (v *typedOperationVisitor) EnterParameter(p *SemanticParameter) error {
	if p.Passing == "value" {
		if err := (SemanticOperation{Name: "integer.value", Type: p.Type}).validate(1); err != nil {
			return err
		}
		v.required = append(v.required, "integer.value")
	}
	return nil
}
func (v *typedOperationVisitor) EnterExpression(e *SemanticExpression) error {
	if e.Operation == nil {
		// A typed value must never leak into a legacy binary64 operation or
		// primitive. Require an explicit conversion/format or typed parameter.
		isInteger := v.integerExpression
		if e.Kind == "binary" && (isInteger(e.Left) || isInteger(e.Right)) {
			return fmt.Errorf("integer operands require a typed operation")
		}
		if (e.Kind == "unary" || e.Kind == "index") && isInteger(e.Value) {
			return fmt.Errorf("integer value requires an explicit typed operation")
		}
		if e.Kind == "index" {
			for _, arg := range e.Arguments {
				if isInteger(arg.Value) {
					return fmt.Errorf("exact integer indexing requires explicit index semantics")
				}
			}
		}
		if e.Kind == "call" {
			var function *SemanticFunction
			if e.Value != nil && e.Value.Kind == "identifier" {
				function = v.functions[e.Value.Name]
			}
			for i, arg := range e.Arguments {
				if !isInteger(arg.Value) {
					continue
				}
				if function == nil || i >= len(function.Parameters) || arg.Name != "" || function.Parameters[i].Passing != "value" {
					return fmt.Errorf("integer argument requires a typed function parameter or explicit format")
				}
				if arg.Value.Operation == nil {
					return fmt.Errorf("integer argument needs an explicit typed load")
				}
				actual, expected := arg.Value.Operation.resultType(), function.Parameters[i].Type
				actual.TypeOrigin, expected.TypeOrigin = "", ""
				if !reflect.DeepEqual(actual, expected) {
					return fmt.Errorf("integer function argument type mismatch")
				}
			}
		}
		return nil
	}
	o := e.Operation
	if err := o.validate(len(e.Arguments)); err != nil {
		return err
	}
	v.required = append(v.required, o.Name)
	for _, arg := range e.Arguments {
		if arg.Value == nil {
			return fmt.Errorf("missing operation operand")
		}
		if arg.Value.Operation == nil {
			// Only a typed load can bridge from lexical storage or a function result.
			if o.Name != "integer.value" || (arg.Value.Kind != "identifier" && arg.Value.Kind != "call") {
				return fmt.Errorf("%s requires typed integer operands", o.Name)
			}
			continue
		}
		actual := arg.Value.Operation.resultType()
		expected := o.Type
		actual.TypeOrigin, expected.TypeOrigin = "", ""
		if actual.Kind != "integer" || (o.Name != "integer.convert" && !reflect.DeepEqual(actual, expected)) {
			return fmt.Errorf("%s has inconsistent operand type", o.Name)
		}
	}
	return nil
}
func typedRequirements(doc *SemanticDocument) ([]string, error) {
	visitor := &typedOperationVisitor{functions: map[string]*SemanticFunction{}, integerBindings: map[string]bool{}, integerResults: map[string]bool{}}
	for _, s := range doc.Root.Statements {
		if s.Kind == "assign" && s.Expression != nil && s.Expression.Function != nil {
			visitor.functions[s.Name] = s.Expression.Function
		}
	}
	// Conservative prepass: native bindings have unique identities. Mixed
	// dynamic uses of an exact integer still require explicit conversion nodes.
	var scanStatement func(*SemanticStatement, string)
	var scanExpression func(*SemanticExpression, string)
	scanExpression = func(e *SemanticExpression, function string) {
		if e == nil {
			return
		}
		scanExpression(e.Left, function)
		scanExpression(e.Right, function)
		scanExpression(e.Value, function)
		for _, arg := range e.Arguments {
			scanExpression(arg.Value, function)
		}
		if e.Function != nil {
			for _, p := range e.Function.Parameters {
				if p.Passing == "value" {
					visitor.integerBindings[p.Name] = true
				}
			}
			scanStatement(&e.Function.Body, function)
		}
	}
	scanStatement = func(s *SemanticStatement, function string) {
		if s == nil {
			return
		}
		if s.Kind == "assign" && s.Expression != nil {
			if s.Expression.Operation != nil && s.Expression.Operation.resultType().Kind == "integer" {
				visitor.integerBindings[s.Name] = true
			}
			if s.Expression.Function != nil {
				function = s.Name
			}
		}
		if s.Kind == "return" && s.Expression != nil && s.Expression.Operation != nil && s.Expression.Operation.resultType().Kind == "integer" {
			visitor.integerResults[function] = true
		}
		scanExpression(s.Expression, function)
		scanExpression(s.Condition, function)
		scanExpression(s.Sequence, function)
		scanStatement(s.Then, function)
		scanStatement(s.Else, function)
		scanStatement(s.Body, function)
		for i := range s.Statements {
			scanStatement(&s.Statements[i], function)
		}
	}
	scanStatement(&doc.Root, "")
	err := WalkSemanticDocument(doc, visitor)
	return visitor.required, err
}
