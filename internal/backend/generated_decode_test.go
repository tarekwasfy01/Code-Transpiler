package backend

import (
	"strings"
	"testing"
)

func TestGeneratedOperationRoundtrips(t *testing.T) {
	cases := []string{`print(1 + 2 * 3)`, "x <- 3\nx <- x + 1\nprint(x)", `print("if # RValue [ ] hello")`, `print(3 < 8)`, `print(NULL)`, `print(c())`, `print(!FALSE)`, `print(TRUE && FALSE)`, `print(c(4, 7)[2])`, "x <- 2\nif(x > 1) { print(3) } else { print(4) }", "x <- 0\nwhile(x < 3) { x <- x + 1 }\nprint(x)"}
	for _, lang := range Languages {
		for _, src := range cases {
			t.Run(lang.ID+"/"+src, func(t *testing.T) {
				code, err := TranspileFrom("python", lang.ID, src)
				if err != nil {
					t.Fatal(err)
				}
				decoded, recognized, err := DecodeGenerated(lang.ID, code)
				if !recognized || err != nil {
					t.Fatalf("recognized=%t err=%v", recognized, err)
				}
				actual, err := EmitSemantic(lang.ID, decoded)
				if err != nil || !sameTokens(lang.ID, code, actual) {
					t.Fatalf("roundtrip: %v", err)
				}
			})
		}
	}
}
func TestGeneratedIterableHeadersUseEmitterTemplates(t *testing.T) {
	for _, l := range Languages {
		if l.ID == "c" {
			continue
		}
		t.Run(l.ID, func(t *testing.T) {
			code, e := Transpile(l.ID, "x <- 0\nfor (i in c(2,4,6)) { x <- x + i }\nprint(x)")
			if e != nil {
				t.Fatal(e)
			}
			_, ok, e := DecodeGenerated(l.ID, code)
			if !ok || e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestGeneratedDecoderDoesNotRecoverHiddenOriginal(t *testing.T) {
	code, _ := Transpile("python", "print(1 + 2)")
	changed := strings.Replace(code, "[1, 2]", "[1, 9]", 1)
	p, ok, err := DecodeGenerated("python", changed)
	if !ok || err != nil {
		t.Fatal(err)
	}
	r, err := p.RSource(false)
	if err != nil || !strings.Contains(r, "9") {
		t.Fatalf("actual edit was ignored: %s %v", r, err)
	}
	changed = strings.Replace(code, "def r_num", "def altered_r_num", 1)
	if _, ok, _ := DecodeGenerated("python", changed); ok {
		t.Fatal("modified runtime trusted")
	}
	changed = code + "print('extra')\n"
	if _, ok, err := DecodeGenerated("python", changed); !ok || err == nil {
		t.Fatal("unrecognized trailing call ignored")
	}
	changed = strings.Replace(code, `"arithmetic", "__binary_+"`, `"wrong", "__binary_+"`, 1)
	if _, ok, err := DecodeGenerated("python", changed); !ok || err == nil {
		t.Fatal("wrong runtime dispatch kernel accepted")
	}
}

func TestGeneratedDecoderRejectsChangedIndentation(t *testing.T) {
	code, _ := Transpile("python", "if (TRUE) { print(1); print(2) }")
	changed := strings.Replace(code, "    r_call(", "     r_call(", 1)
	if _, ok, e := DecodeGenerated("python", changed); !ok || e == nil {
		t.Fatal("inconsistent indentation lost in token comparison")
	}
	code, _ = Transpile("python", "print(1)")
	prelude := targetPrelude("python")
	changed = strings.Replace(code, prelude+"\n\n", prelude+"\n\n ", 1)
	if changed == code {
		t.Fatal("indentation mutation did not modify the generated program")
	}
	if _, ok, e := DecodeGenerated("python", changed); !ok || e == nil {
		t.Fatal("unexpected top-level indentation ignored")
	}
}
