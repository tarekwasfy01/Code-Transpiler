package backend

import (
	"fmt"
	"strconv"
)

// runtime:start
// exactInteger stores bits, never a float. Operations reduce modulo 2^width;
// signed ordering and formatting interpret those bits as two's complement.
type exactInteger struct {
	raw    uint64
	bits   int
	signed bool
}

func (x exactInteger) normalized() exactInteger {
	if x.bits < 64 {
		x.raw &= (uint64(1) << x.bits) - 1
	}
	return x
}
func (x exactInteger) signedValue() int64 {
	if x.bits == 64 {
		return int64(x.raw)
	}
	shift := 64 - x.bits
	return int64(x.raw<<shift) >> shift
}
func (x exactInteger) String() string {
	if x.signed {
		return strconv.FormatInt(x.signedValue(), 10)
	}
	return strconv.FormatUint(x.raw, 10)
}
func parseExactIntegerValue(bits int, signed bool, text string) (exactInteger, error) {
	x := exactInteger{bits: bits, signed: signed}
	var err error
	if x.signed {
		var n int64
		n, err = strconv.ParseInt(text, 10, x.bits)
		x.raw = uint64(n)
	} else {
		x.raw, err = strconv.ParseUint(text, 10, x.bits)
	}
	if err != nil {
		return x, fmt.Errorf("invalid %s literal %q", fmt.Sprintf("integer(%d,%t)", bits, signed), text)
	}
	x = x.normalized()
	if x.String() != text {
		return x, fmt.Errorf("integer literal must use canonical decimal text")
	}
	return x, nil
}
func exactIntegerOperation(name string, bits int, signed bool, text string, values []any) (any, error) {
	if name == "integer.literal" {
		return parseExactIntegerValue(bits, signed, text)
	}
	operands := make([]exactInteger, len(values))
	for i, value := range values {
		x, ok := value.(exactInteger)
		if !ok {
			return nil, fmt.Errorf("%s operand %d is not an exact integer", name, i)
		}
		if name != "integer.convert" && (x.bits != bits || x.signed != signed) {
			return nil, fmt.Errorf("%s operand type mismatch", name)
		}
		operands[i] = x
	}
	a := operands[0]
	r := exactInteger{raw: a.raw, bits: bits, signed: signed}
	if name == "integer.convert" {
		if a.signed {
			r.raw = uint64(a.signedValue())
		}
		return r.normalized(), nil
	}
	if name == "integer.value" {
		return a, nil
	}
	if name == "integer.format" {
		return a.String(), nil
	}
	if name == "integer.negate" {
		r.raw = 0 - a.raw
		return r.normalized(), nil
	}
	if name == "integer.complement" {
		r.raw = ^a.raw
		return r.normalized(), nil
	}
	b := operands[1]
	less := a.raw < b.raw
	if a.signed {
		less = a.signedValue() < b.signedValue()
	}
	switch name {
	case "integer.add":
		r.raw = a.raw + b.raw
	case "integer.subtract":
		r.raw = a.raw - b.raw
	case "integer.multiply":
		r.raw = a.raw * b.raw
	case "integer.and":
		r.raw = a.raw & b.raw
	case "integer.or":
		r.raw = a.raw | b.raw
	case "integer.xor":
		r.raw = a.raw ^ b.raw
	case "integer.and_not":
		r.raw = a.raw &^ b.raw
	case "integer.equal":
		return a.raw == b.raw, nil
	case "integer.not_equal":
		return a.raw != b.raw, nil
	case "integer.less":
		return less, nil
	case "integer.less_equal":
		return less || a.raw == b.raw, nil
	case "integer.greater":
		return !less && a.raw != b.raw, nil
	case "integer.greater_equal":
		return !less, nil
	default:
		return nil, fmt.Errorf("unimplemented operation %s", name)
	}
	return r.normalized(), nil
}
