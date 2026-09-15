// Copyright (c) 2026 Tarek Wasfy
package manytomany

import (
	"strings"
	"testing"
)

// Small semantic witnesses may deliberately contain free identifiers. A direct
// emitter must preserve their valid target representation; scope resolution is
// a later compile-stage concern, not a routing criterion.
func TestDirectPowerWitnessDoesNotRequireTargetNameResolution(t *testing.T) {
	p, parseErr := Parse("r", "a ^ b")
	if parseErr != nil {
		t.Fatalf("parse witness: %v", parseErr)
	}
	if _, emitErr := EmitDirect("java", p); emitErr != nil {
		t.Fatalf("direct emitter rejects Java witness: %v", emitErr)
	}
	result, err := TranspileCore(TranspileRequest{Source: "a ^ b", SourceLanguage: "r", TargetLanguage: "java", DisableRuntimeFallback: true})
	if err != nil {
		t.Fatalf("direct Java power witness rejected: %v", err)
	}
	if !strings.Contains(result.Code, "Math.pow") && !strings.Contains(result.Code, "Math.Pow") {
		t.Fatalf("missing native Java power form: %s", result.Code)
	}
	if strings.Contains(result.Code, "Math.Pow") {
		t.Fatalf("Java must use lowercase Math.pow: %s", result.Code)
	}
	if result.Trace.RuntimeFallback {
		t.Fatalf("direct witness unexpectedly used runtime: %+v", result.Trace)
	}
}

func TestDirectPowerWitnessCoversEveryTarget(t *testing.T) {
	p, err := Parse("r", "a ^ b")
	if err != nil {
		t.Fatalf("parse witness: %v", err)
	}
	for _, target := range Languages {
		t.Run(target, func(t *testing.T) {
			code, emitErr := EmitDirect(target, p)
			if emitErr != nil {
				t.Fatalf("direct POWER unavailable: %v", emitErr)
			}
			if strings.TrimSpace(code) == "" {
				t.Fatal("direct POWER emitted empty source")
			}
		})
	}
}

func TestJavaDirectLegalizesValueExpressionStatement(t *testing.T) {
	p, err := Parse("r", "a + b")
	if err != nil {
		t.Fatalf("parse witness: %v", err)
	}
	code, err := EmitDirect("java", p)
	if err != nil {
		t.Fatalf("direct Java expression rejected: %v", err)
	}
	if !strings.Contains(code, "Object __r2m_discard_") {
		t.Fatalf("Java value expression was not legalized through discard binding: %s", code)
	}
}
