// Copyright (c) 2026 Tarek Wasfy
package backend

import "testing"

func TestNativeWindowsX64RepresentationFamilies(t *testing.T) {
	p := NativeWindowsX64TargetProfile()
	for _, test := range []struct {
		name string
		typ  SemanticType
		want NativeRepresentation
	}{
		{"scalar", SemanticType{Kind: "integer", Bits: 64, Signed: boolPointer(true), TypeOrigin: "explicit"}, NativeScalar},
		{"string", SemanticType{Kind: "string"}, NativePair},
		{"slice", SemanticType{Kind: "slice"}, NativeDescriptor},
		{"closure", SemanticType{Kind: "closure"}, NativeClosureValue},
		{"reference", SemanticType{Kind: "reference"}, NativeAddress},
	} {
		layout, err := SolveNativeLayout(p, test.typ, NativeABIValue)
		if err != nil || layout.Representation != test.want || layout.PreferredAlignment < layout.ABIAlignment {
			t.Fatalf("%s: layout=%+v err=%v", test.name, layout, err)
		}
	}
}

func TestNativeStorageDecisionIsSharedAndFailClosed(t *testing.T) {
	if got := DecideNativeStorage(NativeStorageRequest{ProgramStatic: true}); got != NativeStaticStorage {
		t.Fatalf("static=%s", got)
	}
	if got := DecideNativeStorage(NativeStorageRequest{Lifetime: "frame", AliasMutationSafe: true}); got != NativeStackStorage {
		t.Fatalf("stack=%s", got)
	}
	if got := DecideNativeStorage(NativeStorageRequest{Escapes: true, OwnershipKnown: true, AliasMutationSafe: true}); got != NativeOwnedStorage {
		t.Fatalf("owned=%s", got)
	}
	if got := DecideNativeStorage(NativeStorageRequest{Escapes: true}); got != NativeStorageUnresolved {
		t.Fatalf("unresolved=%s", got)
	}
}
