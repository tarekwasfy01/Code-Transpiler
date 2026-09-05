package backend

import "fmt"

func (s *x64Selector) isFloat(id int) bool { return s.floatKind(id, map[int]bool{}) }
func (s *x64Selector) floatKind(id int, seen map[int]bool) bool {
	if seen[id] {
		return false
	}
	seen[id] = true
	c := s.g.common[id]
	t := c.Type
	if binding, ok := s.bindingTypes[c.Name]; ok {
		t = binding
	}
	if t.IEEE754 || t.Kind == "float" || t.Kind == "number" || t.Name == "float64" || t.Name == "float32" {
		return true
	}
	if c.Kind == "typed_operation" || t.Kind == "integer" || t.Kind == "boolean" {
		return false
	}
	if c.Kind == "literal" {
		return c.Operation.LiteralKind == "number" || c.Operation.LiteralKind == "numeric"
	}
	if c.Kind == "binary" {
		switch c.Operation.Operator {
		case "==", "!=", "<", "<=", ">", ">=", "&&", "||":
			return false
		}
		a, e := s.child(id, "left")
		if e == nil && s.floatKind(a, seen) {
			return true
		}
		b, e := s.child(id, "right")
		return e == nil && s.floatKind(b, seen)
	}
	if c.Kind == "call" {
		callee, e := s.child(id, "value", "callee")
		if e == nil {
			if fn, ok := s.functions[s.g.common[callee].Name]; ok {
				return s.functionFloatSeen(fn, seen)
			}
		}
	}
	if c.Kind == "unary" {
		v, e := s.child(id, "value", "operand")
		return e == nil && s.floatKind(v, seen)
	}
	return false
}
func (s *x64Selector) functionFloat(id int) bool { return s.functionFloatSeen(id, map[int]bool{}) }
func (s *x64Selector) functionFloatSeen(id int, seen map[int]bool) bool {
	if seen[id] {
		return false
	}
	seen[id] = true
	if s.g.common[id].Kind == "return" {
		v, e := s.child(id, "expression")
		return e == nil && s.floatKind(v, seen)
	}
	for _, role := range []string{"body", "statement", "then", "else"} {
		for _, n := range s.g.many(id, role) {
			if s.functionFloatSeen(n.ID, seen) {
				return true
			}
		}
	}
	return false
}
func (s *x64Selector) floatBinary(op string, a, b int) error {
	if err := s.expression(a); err != nil {
		return err
	}
	if !s.isFloat(a) {
		s.emit("cvtsi2sd", xr(4), xr(xRAX))
		s.emit("mov_from_xmm", xr(xRAX), xr(4))
	}
	slot := s.slot()
	s.emit("mov", xm(xRBP, slot), xr(xRAX))
	if err := s.expression(b); err != nil {
		return err
	}
	if s.isFloat(b) {
		s.emit("mov_to_xmm", xr(5), xr(xRAX))
	} else {
		s.emit("cvtsi2sd", xr(5), xr(xRAX))
	}
	s.emit("mov", xr(xRAX), xm(xRBP, slot))
	s.emit("mov_to_xmm", xr(4), xr(xRAX))
	if form := map[string]string{"+": "addsd", "-": "subsd", "*": "mulsd", "/": "divsd"}[op]; form != "" {
		s.emit(form, xr(4), xr(5))
		s.emit("mov_from_xmm", xr(xRAX), xr(4))
		return nil
	}
	branch := map[string]string{"==": "je", "!=": "jne", "<": "jb", "<=": "jbe", ">": "ja", ">=": "jae"}[op]
	if branch == "" {
		return fmt.Errorf("floating operation %q unavailable", op)
	}
	s.emit("ucomisd", xr(4), xr(5))
	unordered, end := s.label(), s.label()
	s.emit("jp", xl(unordered), x64Operand{})
	s.boolean(branch)
	s.emit("jmp", xl(end), x64Operand{})
	s.mark(unordered)
	value := int64(0)
	if op == "!=" {
		value = 1
	}
	s.emit("mov", xr(xRAX), xi(value))
	s.mark(end)
	return nil
}
