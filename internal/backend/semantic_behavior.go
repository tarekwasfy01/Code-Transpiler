// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"encoding/json"
	"fmt"
	"strings"
)

type SemanticScopeSemantics struct {
	NameResolution         string   `json:"name_resolution,omitempty"`
	LookupOrder            []string `json:"lookup_order,omitempty"`
	AssignmentBinding      string   `json:"assignment_binding,omitempty"`
	Shadowing              string   `json:"shadowing,omitempty"`
	CreatesChildScope      *bool    `json:"creates_child_scope,omitempty"`
	PostScopeVisibility    string   `json:"post_scope_visibility,omitempty"`
	InteractiveSensitivity string   `json:"interactive_sensitivity,omitempty"`
}

type SemanticBindingSemantics struct {
	Lifetime          string `json:"lifetime,omitempty"`
	Rebinding         string `json:"rebinding,omitempty"`
	ReferenceIdentity string `json:"reference_identity,omitempty"`
}

var semanticBehaviorEnums = map[string]map[string]struct{}{
	"mutation":                enumSet("none", "in_place", "rebind", "copy_on_write", "conditional", "source_defined", "unknown"),
	"identity":                enumSet("preserved", "replaced", "conditional", "not_applicable", "source_defined", "unknown"),
	"aliasing":                enumSet("observers_see_mutation", "detached_on_write", "copy_on_write", "not_applicable", "source_defined", "unknown"),
	"index_domain":            enumSet("sequence_position", "mapping_key", "multidimensional", "custom_dispatch", "source_defined", "unknown"),
	"slice_start_default":     enumSet("explicit", "sequence_begin", "language_default", "source_defined", "unknown"),
	"slice_end_default":       enumSet("explicit", "sequence_end", "language_default", "source_defined", "unknown"),
	"slice_step":              enumSet("explicit", "unit_positive", "supports_negative", "language_default", "source_defined", "unknown"),
	"failure_result":          enumSet("raise_exception", "produce_infinity", "produce_nan", "return_sentinel", "trap", "undefined", "implementation_defined", "source_defined", "unknown"),
	"failure_continuation":    enumSet("propagate", "abort_expression", "continue_with_value", "terminate", "undefined", "source_defined", "unknown"),
	"suspension":              enumSet("none", "yield", "await", "coroutine", "source_defined", "unknown"),
	"resumption":              enumSet("next", "send", "throw", "await_completion", "resume", "source_defined", "unknown"),
	"state_persistence":       enumSet("none", "locals_across_suspend", "source_defined", "unknown"),
	"completion":              enumSet("normal_return", "return_value", "stop_iteration", "future_resolution", "source_defined", "unknown"),
	"name_resolution":         enumSet("lexical", "dynamic", "mixed", "source_defined", "unknown"),
	"assignment_binding":      enumSet("nearest_existing", "local_by_default", "global_by_default", "context_sensitive", "source_defined", "unknown"),
	"shadowing":               enumSet("allowed", "forbidden", "context_sensitive", "source_defined", "unknown"),
	"post_scope_visibility":   enumSet("visible_in_parent", "not_visible_in_parent", "binding_specific", "source_defined", "unknown"),
	"interactive_sensitivity": enumSet("none", "repl_sensitive", "context_sensitive", "source_defined", "unknown"),
	"binding_lifetime":        enumSet("scope", "function", "module", "object", "process", "source_defined", "unknown"),
	"rebinding":               enumSet("allowed", "forbidden", "declaration_only", "source_defined", "unknown"),
	"reference_identity":      enumSet("value", "reference", "mixed", "source_defined", "unknown"),
}

func enumSet(values ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(values))
	for _, value := range values {
		out[value] = struct{}{}
	}
	return out
}

func validateSemanticBehaviorEnum(axis, value string) error {
	if value == "" {
		return nil
	}
	allowed, ok := semanticBehaviorEnums[axis]
	if !ok {
		return fmt.Errorf("unknown semantic behavior axis %q", axis)
	}
	if _, ok := allowed[value]; !ok {
		return fmt.Errorf("invalid %s semantic value %q", axis, value)
	}
	return nil
}

func validateSemanticIdentifier(name, value string) error {
	if value == "" || value == "unknown" {
		return nil
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-' {
			continue
		}
		return fmt.Errorf("%s must be a canonical lowercase semantic identifier, got %q", name, value)
	}
	return nil
}

func validateSemanticBehaviorValues(s SemanticSemantics) error {
	for axis, value := range map[string]string{
		"mutation":             s.Mutation,
		"identity":             s.Identity,
		"aliasing":             s.Aliasing,
		"index_domain":         s.IndexDomain,
		"slice_start_default":  s.SliceStartDefault,
		"slice_end_default":    s.SliceEndDefault,
		"slice_step":           s.SliceStep,
		"failure_result":       s.FailureResult,
		"failure_continuation": s.FailureContinuation,
		"suspension":           s.Suspension,
		"resumption":           s.Resumption,
		"state_persistence":    s.StatePersistence,
		"completion":           s.Completion,
	} {
		if err := validateSemanticBehaviorEnum(axis, value); err != nil {
			return err
		}
	}
	if err := validateSemanticIdentifier("failure_condition", s.FailureCondition); err != nil {
		return err
	}
	if err := validateSemanticIdentifier("exception_category", s.ExceptionCategory); err != nil {
		return err
	}
	if s.Mutation == "in_place" && s.Identity == "replaced" {
		return fmt.Errorf("in_place mutation cannot declare replaced identity")
	}
	if s.Suspension == "none" && (s.Resumption != "" || s.StatePersistence == "locals_across_suspend") {
		return fmt.Errorf("non-suspending operation cannot define resumption/persistent suspended locals")
	}
	return nil
}

func validateSemanticScopeBehavior(s *SemanticScopeSemantics) error {
	if s == nil {
		return nil
	}
	for axis, value := range map[string]string{
		"name_resolution":         s.NameResolution,
		"assignment_binding":      s.AssignmentBinding,
		"shadowing":               s.Shadowing,
		"post_scope_visibility":   s.PostScopeVisibility,
		"interactive_sensitivity": s.InteractiveSensitivity,
	} {
		if err := validateSemanticBehaviorEnum(axis, value); err != nil {
			return err
		}
	}
	for _, part := range s.LookupOrder {
		if strings.TrimSpace(part) == "" {
			return fmt.Errorf("scope lookup_order contains an empty component")
		}
		if err := validateSemanticIdentifier("scope lookup_order", part); err != nil {
			return err
		}
	}
	return nil
}

func validateSemanticBindingBehavior(s *SemanticBindingSemantics) error {
	if s == nil {
		return nil
	}
	for axis, value := range map[string]string{
		"binding_lifetime":   s.Lifetime,
		"rebinding":          s.Rebinding,
		"reference_identity": s.ReferenceIdentity,
	} {
		if err := validateSemanticBehaviorEnum(axis, value); err != nil {
			return err
		}
	}
	return nil
}

func validateSemanticBehaviorExtensions(u *UniversalASTDocument) error {
	if u == nil {
		return nil
	}
	for i := range u.Nodes {
		raw, ok := u.Nodes[i].Fields["operation"]
		if !ok || len(raw) == 0 {
			continue
		}
		var record universalOperationRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			return fmt.Errorf("node %d operation behavior: %w", u.Nodes[i].ID, err)
		}
		if err := validateSemanticBehaviorValues(record.Semantics); err != nil {
			return fmt.Errorf("node %d semantic behavior: %w", u.Nodes[i].ID, err)
		}
	}
	for i := range u.Evidence.Scopes {
		if err := validateSemanticScopeBehavior(u.Evidence.Scopes[i].Semantics); err != nil {
			return fmt.Errorf("scope %d semantic behavior: %w", u.Evidence.Scopes[i].ID, err)
		}
	}
	for i := range u.Evidence.Bindings {
		if err := validateSemanticBindingBehavior(u.Evidence.Bindings[i].Semantics); err != nil {
			return fmt.Errorf("binding %d semantic behavior: %w", u.Evidence.Bindings[i].ID, err)
		}
	}
	return nil
}
