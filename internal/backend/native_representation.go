// Copyright (c) 2026 Tarek Wasfy
package backend

import "fmt"

// NativeTargetProfile is analysis data derived from a requested target. It is
// deliberately not a program IR and contains no source/UAST nodes.
type NativeTargetProfile struct {
	OS, ISA, ObjectFormat, ABI, Endianness, SystemABI                    string
	PointerSize, StackAlignment, FunctionPointerAlignment, IndexBitWidth int
	LegalIntegerWidths                                                   []int
}

func NativeWindowsX64TargetProfile() NativeTargetProfile {
	return NativeTargetProfile{OS: "windows", ISA: "x86_64", ObjectFormat: "PE32+", ABI: "microsoft-x64", Endianness: "little", SystemABI: "kernel32", PointerSize: 8, StackAlignment: 16, FunctionPointerAlignment: 8, IndexBitWidth: 64, LegalIntegerWidths: []int{8, 16, 32, 64}}
}

type NativeRepresentation string

const (
	NativeScalar            NativeRepresentation = "SCALAR"
	NativeAddress           NativeRepresentation = "ADDRESS"
	NativePair              NativeRepresentation = "PAIR"
	NativeDescriptor        NativeRepresentation = "DESCRIPTOR"
	NativeOwnedRegion       NativeRepresentation = "OWNED_REGION"
	NativeFunctionReference NativeRepresentation = "FUNCTION_REFERENCE"
	NativeClosureValue      NativeRepresentation = "CLOSURE_VALUE"
)

type NativeABIContext string

const (
	NativeABIValue    NativeABIContext = "value"
	NativeABIArgument NativeABIContext = "argument"
	NativeABIResult   NativeABIContext = "result"
	NativeABICapture  NativeABIContext = "capture"
)

type NativeLayout struct {
	Representation                                        NativeRepresentation
	SizeBits, SizeBytes, ABIAlignment, PreferredAlignment int
	FieldOffsets                                          []int
	Stride, IndexBitWidth, PointerSize, StackAlignment    int
	Endianness, MemorySpace                               string
}

// SolveNativeLayout is target-scoped: no semantic type stores a platform
// layout. The caller supplies ABI context for aggregate result buffers and
// closure environments.
func SolveNativeLayout(profile NativeTargetProfile, typ SemanticType, context NativeABIContext) (NativeLayout, error) {
	if profile.PointerSize <= 0 || profile.StackAlignment <= 0 {
		return NativeLayout{}, fmt.Errorf("invalid target profile")
	}
	pointer := profile.PointerSize
	l := NativeLayout{Representation: NativeScalar, ABIAlignment: 1, PreferredAlignment: 1, IndexBitWidth: profile.IndexBitWidth, PointerSize: pointer, StackAlignment: profile.StackAlignment, Endianness: profile.Endianness, MemorySpace: "default"}
	switch typ.Kind {
	case "", "unknown", "any", "dynamic":
		return NativeLayout{}, fmt.Errorf("NATIVE_LAYOUT_GAP: dynamic type has no native representation")
	case "boolean", "bool":
		l.SizeBits, l.SizeBytes = 8, 1
	case "integer":
		if typ.Bits != 8 && typ.Bits != 16 && typ.Bits != 32 && typ.Bits != 64 {
			return NativeLayout{}, fmt.Errorf("NATIVE_LAYOUT_GAP: integer width %d", typ.Bits)
		}
		l.SizeBits, l.SizeBytes = typ.Bits, typ.Bits/8
		l.ABIAlignment, l.PreferredAlignment = minNative(l.SizeBytes, pointer), minNative(l.SizeBytes, pointer)
	case "float", "number", "numeric", "binary64":
		bits := typ.Bits
		if bits == 0 {
			bits = 64
		}
		if bits != 32 && bits != 64 {
			return NativeLayout{}, fmt.Errorf("NATIVE_LAYOUT_GAP: float width %d", bits)
		}
		l.SizeBits, l.SizeBytes, l.ABIAlignment, l.PreferredAlignment = bits, bits/8, bits/8, bits/8
	case "string":
		l.Representation, l.SizeBits, l.SizeBytes, l.ABIAlignment, l.PreferredAlignment, l.FieldOffsets = NativePair, pointer*16, pointer*2, pointer, pointer, []int{0, pointer}
	case "pointer", "reference", "optional":
		l.Representation, l.SizeBits, l.SizeBytes, l.ABIAlignment, l.PreferredAlignment = NativeAddress, pointer*8, pointer, pointer, pointer
	case "function":
		l.Representation, l.SizeBits, l.SizeBytes, l.ABIAlignment, l.PreferredAlignment = NativeFunctionReference, pointer*8, pointer, profile.FunctionPointerAlignment, profile.FunctionPointerAlignment
	case "closure":
		l.Representation, l.SizeBits, l.SizeBytes, l.ABIAlignment, l.PreferredAlignment, l.FieldOffsets = NativeClosureValue, pointer*16, pointer*2, pointer, pointer, []int{0, pointer}
	case "slice", "array", "vector", "list":
		l.Representation, l.SizeBits, l.SizeBytes, l.ABIAlignment, l.PreferredAlignment, l.FieldOffsets = NativeDescriptor, pointer*16, pointer*2, pointer, pointer, []int{0, pointer}
	case "tuple", "aggregate", "record", "struct":
		l.Representation, l.SizeBits, l.SizeBytes, l.ABIAlignment, l.PreferredAlignment = NativeAddress, pointer*8, pointer, pointer, pointer
		if context == NativeABIValue && typ.Length > 0 && typ.Length*pointer <= 16 {
			l.Representation, l.SizeBits, l.SizeBytes = NativeScalar, typ.Length*pointer*8, typ.Length*pointer
		}
	default:
		return NativeLayout{}, fmt.Errorf("NATIVE_LAYOUT_GAP: type kind %q", typ.Kind)
	}
	l.Stride = l.SizeBytes
	if l.PreferredAlignment < l.ABIAlignment {
		return NativeLayout{}, fmt.Errorf("invalid layout alignment")
	}
	return l, nil
}

func minNative(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type NativeStorageDecision string

const (
	NativeStaticStorage     NativeStorageDecision = "STATIC"
	NativeStackStorage      NativeStorageDecision = "STACK"
	NativeOwnedStorage      NativeStorageDecision = "OWNED_REGION"
	NativeStorageUnresolved NativeStorageDecision = "UNRESOLVED"
)

type NativeStorageRequest struct {
	Escapes, ProgramStatic, OwnershipKnown, AliasMutationSafe bool
	DynamicSize                                               bool
	Lifetime                                                  string
}

// DecideNativeStorage centralizes the lifetime decision shared by strings,
// aggregates, closure environments and returned dynamic values.
func DecideNativeStorage(request NativeStorageRequest) NativeStorageDecision {
	if request.ProgramStatic && !request.Escapes && !request.DynamicSize {
		return NativeStaticStorage
	}
	if !request.Escapes && (request.Lifetime == "frame" || request.Lifetime == "block") && request.AliasMutationSafe {
		return NativeStackStorage
	}
	if request.Escapes && request.OwnershipKnown && request.AliasMutationSafe {
		return NativeOwnedStorage
	}
	return NativeStorageUnresolved
}
