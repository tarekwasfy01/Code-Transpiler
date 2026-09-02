package backend

import "fmt"

const ExactSignatureCapability = "function.signature.exact.v1"

type signatureContractVisitor struct{ exact bool }

func (*signatureContractVisitor) EnterStatement(*SemanticStatement) error   { return nil }
func (*signatureContractVisitor) LeaveStatement(*SemanticStatement) error   { return nil }
func (*signatureContractVisitor) LeaveExpression(*SemanticExpression) error { return nil }
func (v *signatureContractVisitor) EnterExpression(e *SemanticExpression) error {
	if e.Function == nil {
		return nil
	}
	f := e.Function
	if f.Binding == "" {
		if f.DefaultEvaluation != "" {
			return fmt.Errorf("default evaluation requires an explicit function binding contract")
		}
		for _, p := range f.Parameters {
			if p.Mode != "" {
				return fmt.Errorf("parameter modes require exact binding")
			}
		}
		return nil
	}
	if f.Binding != "exact_v1" || (f.DefaultEvaluation != "definition" && f.DefaultEvaluation != "call") {
		return fmt.Errorf("unsupported function binding/default contract")
	}
	v.exact = true
	var params []SignatureParameter
	var args []SignatureArgument
	for _, p := range f.Parameters {
		params = append(params, SignatureParameter{Name: p.Name, Passing: p.Mode, HasDefault: p.Default != nil})
		switch p.Mode {
		case "positional_only", "positional_or_keyword":
			args = append(args, SignatureArgument{})
		case "keyword_only":
			args = append(args, SignatureArgument{Name: p.Name})
		case "variadic_positional", "variadic_keyword":
			if p.Passing == "value" {
				return fmt.Errorf("typed variadic parameters require aggregate element semantics")
			}
		}
	}
	_, err := BindSignature(params, args)
	return err
}
func validateSignatureContracts(doc *SemanticDocument) (bool, error) {
	v := &signatureContractVisitor{}
	if err := WalkSemanticDocument(doc, v); err != nil {
		return false, err
	}
	if v.exact && doc.Evaluation != "eager_left_to_right" {
		return false, fmt.Errorf("exact signatures currently require explicit eager evaluation")
	}
	return v.exact, nil
}
