package backend

import "fmt"

// Source coordinates never participate in semantic equivalence. Offsets and
// columns are byte-based, end-exclusive (matching Go token positions).
type sourceSpanVisitor struct {
	spans   map[int]SemanticSourceSpan
	restore bool
}

func (v *sourceSpanVisitor) visit(id int, source **SemanticSourceSpan) error {
	if v.restore {
		if span, ok := v.spans[id]; ok {
			copy := span
			*source = &copy
		}
		return nil
	}
	if *source == nil {
		return nil
	}
	span := **source
	if span.StartOffset < 0 || span.EndOffset < span.StartOffset || span.StartLine < 0 || span.EndLine < span.StartLine || span.StartColumn < 0 || span.EndColumn < 0 || (span.StartLine == span.EndLine && span.EndColumn < span.StartColumn) {
		return fmt.Errorf("node %d: invalid source span", id)
	}
	if _, exists := v.spans[id]; exists {
		return fmt.Errorf("duplicate source node ID %d", id)
	}
	v.spans[id] = span
	*source = nil
	return nil
}
func (v *sourceSpanVisitor) EnterStatement(s *SemanticStatement) error {
	return v.visit(s.ID, &s.Source)
}
func (v *sourceSpanVisitor) EnterExpression(e *SemanticExpression) error {
	return v.visit(e.ID, &e.Source)
}
func (*sourceSpanVisitor) LeaveStatement(*SemanticStatement) error   { return nil }
func (*sourceSpanVisitor) LeaveExpression(*SemanticExpression) error { return nil }
