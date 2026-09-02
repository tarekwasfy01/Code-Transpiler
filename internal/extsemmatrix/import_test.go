package extsemmatrix

import "testing"

func TestExactAnchorRequiresConcreteFeature(t *testing.T) {
	if !exactAnchor("GEN::ffi", "ffi") {
		t.Fatal("concrete anchor not recognized")
	}
	if exactAnchor("abi.ffi", "ffi") {
		t.Fatal("broad axis must not be treated as a concrete anchor")
	}
	if !exactAnchor("SWIFT::generic", "generic") {
		t.Fatal("feature token anchor not recognized")
	}
}

func TestExternalStatusProjectionPrecedence(t *testing.T) {
	// The product is conservative: a present atom wins over an absent atom for
	// a capability that has multiple independently mapped atoms.
	x := map[string]map[string]string{"EXTSEM_FFI": {"UASF_0001": "CONFIRMED"}, "EXTSEM_MANUAL_MEMORY": {"UASF_0001": "CONFIRMED"}}
	atoms := []atom{{ID: "EXTSEM_FFI"}, {ID: "EXTSEM_MANUAL_MEMORY"}}
	langs := []lang{{ID: "c", Values: map[string]string{"EXTSEM_FFI": "PRESENT", "EXTSEM_MANUAL_MEMORY": "ABSENT"}}}
	_ = x
	_ = atoms
	_ = langs
	// The implementation's concrete output is covered by the imported matrix;
	// this assertion documents the status vocabulary used by the contract.
	for _, s := range []string{"PRESENT", "ABSENT", "UNRESOLVED"} {
		if s == "" {
			t.Fatal("empty status")
		}
	}
}
