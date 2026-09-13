// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"reflect"
	"testing"
)

func semanticBehaviorBool(v bool) *bool { return &v }

func TestSemanticBehaviorOperationCarrierRoundTrip(t *testing.T) {
	want := universalOperationRecord{Semantics: SemanticSemantics{
		Operation: "collection.concat", Mutation: "in_place", Identity: "preserved",
		Aliasing: "observers_see_mutation", IndexDomain: "sequence_position",
		Slicing: "range", SliceStartInclusive: semanticBehaviorBool(true),
		SliceEndInclusive: semanticBehaviorBool(false), SliceStartDefault: "sequence_begin",
		SliceEndDefault: "sequence_end", SliceStep: "unit_positive",
		FailureCondition: "invalid_operand", FailureResult: "raise_exception",
		ExceptionCategory: "argument", FailureContinuation: "propagate",
		Suspension: "yield", Resumption: "next", StatePersistence: "locals_across_suspend",
		Completion: "stop_iteration", Confidence: "exact",
	}}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got universalOperationRecord
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("semantic behavior changed across operation JSON roundtrip")
	}
}

func TestSemanticBehaviorEvidenceCarrierRoundTrip(t *testing.T) {
	want := SemanticEvidence{
		Scopes: []SemanticScope{{
			ID: 1, Kind: "for", Parent: 0,
			Semantics: &SemanticScopeSemantics{
				NameResolution:    "lexical",
				LookupOrder:       []string{"local", "enclosing", "global", "builtin"},
				AssignmentBinding: "nearest_existing", Shadowing: "allowed",
				CreatesChildScope:   semanticBehaviorBool(false),
				PostScopeVisibility: "visible_in_parent", InteractiveSensitivity: "none",
			},
		}},
		Bindings: []SemanticBinding{{
			ID: 7, Name: "i", Scope: 1, Mutable: true, Definition: 3, TypeOrigin: "inferred",
			Semantics: &SemanticBindingSemantics{Lifetime: "function", Rebinding: "allowed", ReferenceIdentity: "value"},
		}},
	}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got SemanticEvidence
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.Scopes, want.Scopes) || !reflect.DeepEqual(got.Bindings, want.Bindings) {
		t.Fatal("scope/binding semantics changed across evidence JSON roundtrip")
	}
}

func TestSemanticBehaviorValidatorAcceptsCanonicalValues(t *testing.T) {
	record := universalOperationRecord{Semantics: SemanticSemantics{
		Mutation: "in_place", Identity: "preserved", Aliasing: "observers_see_mutation",
		FailureCondition: "division_by_zero", FailureResult: "raise_exception",
		ExceptionCategory: "division_by_zero", FailureContinuation: "propagate",
	}}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	u := &UniversalASTDocument{Nodes: []UniversalASTNode{{ID: 4, Fields: map[string]json.RawMessage{"operation": raw}}}}
	if err := validateSemanticBehaviorExtensions(u); err != nil {
		t.Fatal(err)
	}
}

func TestSemanticBehaviorValidatorRejectsContradictoryIdentity(t *testing.T) {
	record := universalOperationRecord{Semantics: SemanticSemantics{Mutation: "in_place", Identity: "replaced"}}
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	u := &UniversalASTDocument{Nodes: []UniversalASTNode{{ID: 9, Fields: map[string]json.RawMessage{"operation": raw}}}}
	if err := validateSemanticBehaviorExtensions(u); err == nil {
		t.Fatal("accepted in_place mutation with replaced identity")
	}
}

func TestSemanticBehaviorValidatorRejectsSourceSpecificEnumValue(t *testing.T) {
	if err := validateSemanticBehaviorValues(SemanticSemantics{Mutation: "python_list_magic"}); err == nil {
		t.Fatal("accepted source-language-specific mutation value")
	}
}
