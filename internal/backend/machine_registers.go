package backend

import "sort"

// Promote block-local stack live ranges into ABI-volatile registers. Values
// crossing calls or control-flow joins remain spilled. This conservative
// allocation preserves all predecessor paths and never changes semantics.
func allocateX64Registers(p *x64Program) int {
	promoted := 0
	for begin := 0; begin < len(p.Instructions); {
		end := begin
		for end < len(p.Instructions) {
			op := p.Instructions[end].Op
			if op == "label" || op == "call" || op == "ret" || op == "jmp" {
				break
			}
			if _, branch := x64Conditions[op]; branch {
				break
			}
			end++
		}
		if end == begin {
			begin++
			continue
		}
		type interval struct {
			slot        int64
			first, last int
			writes      int
		}
		ranges := map[int64]*interval{}
		for i := begin; i < end; i++ {
			in := p.Instructions[i]
			for j, o := range []x64Operand{in.A, in.B} {
				if o.Kind != 'm' || o.Reg != xRBP || o.Value >= 0 {
					continue
				}
				r := ranges[o.Value]
				if r == nil {
					r = &interval{slot: o.Value, first: i, last: i}
					ranges[o.Value] = r
				}
				r.last = i
				if j == 0 && in.Op == "mov" {
					r.writes++
				}
			}
		}
		ordered := []*interval{}
		for _, r := range ranges {
			ordered = append(ordered, r)
		}
		sort.Slice(ordered, func(i, j int) bool { return ordered[i].first < ordered[j].first })
		type use struct {
			reg         byte
			first, last int
		}
		var uses []use
		for _, r := range ordered {
			first := p.Instructions[r.first]
			if r.writes != 1 || r.first == r.last || first.Op != "mov" || first.A.Kind != 'm' || first.A.Value != r.slot {
				continue
			}
			// Reject any use outside this interval/block, including another function
			// with the same frame offset. This is conservative, never an alias guess.
			outside := false
			for i, in := range p.Instructions {
				if i >= r.first && i <= r.last {
					continue
				}
				if (in.A.Kind == 'm' && in.A.Reg == xRBP && in.A.Value == r.slot) || (in.B.Kind == 'm' && in.B.Reg == xRBP && in.B.Value == r.slot) {
					outside = true
					break
				}
			}
			if outside {
				continue
			}
			for _, reg := range []byte{11, 8, 9, 2} {
				available := true
				for _, u := range uses {
					if u.reg == reg && r.first <= u.last && u.first <= r.last {
						available = false
					}
				}
				for i := r.first; i <= r.last && available; i++ {
					in := p.Instructions[i]
					if (in.A.Kind == 'r' || in.A.Kind == 'm') && in.A.Reg == reg {
						available = false
					}
					if (in.B.Kind == 'r' || in.B.Kind == 'm') && in.B.Reg == reg {
						available = false
					}
					if in.Op == "idiv" || in.Op == "div" || in.Op == "cqo" {
						available = false
					}
				}
				if !available {
					continue
				}
				for i := r.first; i <= r.last; i++ {
					in := &p.Instructions[i]
					if in.A.Kind == 'm' && in.A.Reg == xRBP && in.A.Value == r.slot {
						in.A = xr(reg)
					}
					if in.B.Kind == 'm' && in.B.Reg == xRBP && in.B.Value == r.slot {
						in.B = xr(reg)
					}
				}
				uses = append(uses, use{reg, r.first, r.last})
				promoted++
				break
			}
		}
		begin = end
	}
	return promoted
}
