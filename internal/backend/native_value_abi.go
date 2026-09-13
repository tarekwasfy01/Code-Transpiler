// Copyright (c) 2026 Tarek Wasfy
package backend

// NativeValueTag identifies the runtime representation carried through a
// native variadic call. The layout is deliberately fixed-width so it can be
// passed through the existing Win64 integer argument registers.
type NativeValueTag uint64

const (
	NativeValueInvalid NativeValueTag = iota
	NativeValueInteger
	NativeValueUnsigned
	NativeValueFloat
	NativeValueBool
	NativeValueString
	NativeValuePointer
)

// NativeValue is the canonical boxed value used by generated native helpers.
// Payload is either an integer bit pattern or a pointer to a managed string
// cell. The compiler must preserve Tag when lowering interface{} values.
type NativeValue struct {
	Tag     NativeValueTag
	Payload uint64
}

// NativeVariadicFrame is the ABI-neutral form of a variadic argument list.
// Count is explicit; Args points at contiguous NativeValue cells owned by the
// caller for the duration of the call.
type NativeVariadicFrame struct {
	Count uint64
	Args  *NativeValue
}

func nativeValueInteger(v int64) NativeValue {
	return NativeValue{Tag: NativeValueInteger, Payload: uint64(v)}
}

func nativeValueUnsigned(v uint64) NativeValue {
	return NativeValue{Tag: NativeValueUnsigned, Payload: v}
}

func nativeValuePointer(v uintptr) NativeValue {
	return NativeValue{Tag: NativeValuePointer, Payload: uint64(v)}
}

func nativeValueString(v uintptr) NativeValue {
	return NativeValue{Tag: NativeValueString, Payload: uint64(v)}
}

func nativeValueFloatBits(v uint64) NativeValue {
	return NativeValue{Tag: NativeValueFloat, Payload: v}
}

func nativeValueBool(v bool) NativeValue {
	if v {
		return NativeValue{Tag: NativeValueBool, Payload: 1}
	}
	return NativeValue{Tag: NativeValueBool}
}
