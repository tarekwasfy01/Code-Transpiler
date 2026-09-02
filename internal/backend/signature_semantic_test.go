package backend

import (
	"bytes"
	"strings"
	"testing"
)

func signatureProgram(t *testing.T, source, defaults string, modes []string) *SemanticProgram {
	t.Helper()
	body, err := parse(source)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range body.List {
		if a, ok := statement.(*AssignStmt); ok && a.Name == "f" {
			fn, ok := a.Value.(*FunctionExpr)
			if !ok {
				t.Fatal("f is not a function")
			}
			fn.Binding = "exact_v1"
			fn.DefaultEvaluation = defaults
			for i := range fn.Params {
				fn.Params[i].Mode = modes[i]
			}
		}
	}
	p := NewSemanticProgram(body, "eager_left_to_right")
	p.Origin = SemanticOrigin{SourceLanguage: "semantic", EntryPoint: "main"}
	return p
}

func TestSemanticExactSignatureDefaultsAndRoundtrip(t *testing.T) {
	for _, mode := range []string{"definition", "call"} {
		t.Run(mode, func(t *testing.T) {
			p := signatureProgram(t, "x <- 10; f <- function(a = x) { print(a) }; x <- 20; f(); f()", mode, []string{"positional_or_keyword"})
			wire, err := p.MarshalSemanticJSON()
			if err != nil {
				t.Fatal(err)
			}
			q, err := ParseSemanticJSON(wire)
			if err != nil {
				t.Fatal(err)
			}
			again, err := q.MarshalSemanticJSON()
			if err != nil {
				t.Fatal(err)
			}
			// The first import projects old SemanticDocument JSON to the
			// canonical UAST.  Verify stability of the canonical wire rather
			// than requiring it to retain the obsolete legacy envelope.
			r, err := ParseSemanticJSON(again)
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := r.MarshalSemanticJSON()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(again, repeated) {
				t.Fatal("signature contract lost in roundtrip")
			}
			out, err := RunSemantic(q)
			if err != nil {
				t.Fatal(err)
			}
			want := "10\n10\n"
			if mode == "call" {
				want = "20\n20\n"
			}
			if out != want {
				t.Fatalf("got %q want %q", out, want)
			}
			for _, target := range Backends() {
				if _, err := EmitSemantic(target.ID, q); err == nil || !strings.Contains(err.Error(), ExactSignatureCapability) {
					t.Fatalf("%s failed to capability-reject: %v", target.ID, err)
				}
			}
			if _, err := q.RSource(false); err == nil {
				t.Fatal("lossy R serialization accepted")
			}
		})
	}
}

func TestSemanticExactSignatureCalleeAndDefaultsTiming(t *testing.T) {
	p := signatureProgram(t, `d <- function() { print("default"); return(7) }; f <- function(x = d()) { print(x) }; make <- function() { print("callee"); return(f) }; arg <- function() { print("argument"); return(3) }; make()(arg()); f()`, "definition", []string{"positional_or_keyword"})
	out, err := RunSemantic(p)
	if err != nil {
		t.Fatal(err)
	}
	if want := "default\ncallee\nargument\n3\n7\n"; out != want {
		t.Fatalf("got %q want %q", out, want)
	}
}

func TestSemanticExactSignatureRejectsCallConflicts(t *testing.T) {
	for _, call := range []string{"f()", "f(1, x=2)", "f(z=2)"} {
		p := signatureProgram(t, "f <- function(x) { return(x) }; "+call, "call", []string{"positional_or_keyword"})
		if _, err := RunSemantic(p); err == nil {
			t.Fatalf("invalid call accepted: %s", call)
		}
	}
	p := signatureProgram(t, "f <- function(x) { return(x) }; f(x=1)", "call", []string{"positional_only"})
	if _, err := RunSemantic(p); err == nil {
		t.Fatal("positional-only keyword accepted")
	}
}

func TestSemanticExactSignatureVariadicBinding(t *testing.T) {
	p := signatureProgram(t, "f <- function(x, rest, k, options) { print(rest); print(k) }; f(1,2,3,k=4,z=5)", "call", []string{"positional_only", "variadic_positional", "keyword_only", "variadic_keyword"})
	out, err := RunSemantic(p)
	if err != nil {
		t.Fatal(err)
	}
	if out != "[2, 3]\n4\n" {
		t.Fatalf("variadic binding: %q", out)
	}
}
