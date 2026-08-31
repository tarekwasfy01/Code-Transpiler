package backend

// SemanticVisitor provides deterministic pre-order traversal of a complete
// SemanticDocument. Visitors can analyze, validate, optimize or annotate a
// document without embedding target-specific logic in AST node types.
type SemanticVisitor interface {
	EnterStatement(*SemanticStatement) error
	LeaveStatement(*SemanticStatement) error
	EnterExpression(*SemanticExpression) error
	LeaveExpression(*SemanticExpression) error
}
type SemanticParameterVisitor interface {
	EnterParameter(*SemanticParameter) error
}

func WalkSemanticDocument(doc *SemanticDocument, visitor SemanticVisitor) error {
	if doc == nil || visitor == nil {
		return nil
	}
	var expression func(*SemanticExpression) error
	var statement func(*SemanticStatement) error
	statement = func(s *SemanticStatement) error {
		if s == nil {
			return nil
		}
		if err := visitor.EnterStatement(s); err != nil {
			return err
		}
		if err := expression(s.Expression); err != nil {
			return err
		}
		if err := expression(s.Condition); err != nil {
			return err
		}
		if err := expression(s.Sequence); err != nil {
			return err
		}
		if err := statement(s.Then); err != nil {
			return err
		}
		if err := statement(s.Else); err != nil {
			return err
		}
		if err := statement(s.Body); err != nil {
			return err
		}
		for i := range s.Statements {
			if err := statement(&s.Statements[i]); err != nil {
				return err
			}
		}
		return visitor.LeaveStatement(s)
	}
	expression = func(e *SemanticExpression) error {
		if e == nil {
			return nil
		}
		if err := visitor.EnterExpression(e); err != nil {
			return err
		}
		if err := expression(e.Left); err != nil {
			return err
		}
		if err := expression(e.Right); err != nil {
			return err
		}
		if err := expression(e.Value); err != nil {
			return err
		}
		for i := range e.Arguments {
			if err := expression(e.Arguments[i].Value); err != nil {
				return err
			}
		}
		if e.Function != nil {
			for i := range e.Function.Parameters {
				if parameters, ok := visitor.(SemanticParameterVisitor); ok {
					if err := parameters.EnterParameter(&e.Function.Parameters[i]); err != nil {
						return err
					}
				}
				if err := expression(e.Function.Parameters[i].Default); err != nil {
					return err
				}
			}
			if err := statement(&e.Function.Body); err != nil {
				return err
			}
		}
		return visitor.LeaveExpression(e)
	}
	return statement(&doc.Root)
}
