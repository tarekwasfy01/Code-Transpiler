package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/tarekwasfy01/Code-Transpiler/v2/v2/internal/evidence/nasm"
)

func main() {
	root := flag.String("source", "", "NASM source checkout")
	expanded := flag.String("expanded", "", "preinsns.pl output for x86/insns.dat")
	out := flag.String("out", "compiler-evidence/nasm", "evidence output directory")
	backend := flag.String("backend", "internal/backend/machine_x64.go", "read-only native encoder source for candidate comparison")
	queryMnemonic := flag.String("query-mnemonic", "", "optionally query exported variants by mnemonic")
	queryMode := flag.String("query-mode", "", "optional mode: 16, 32 or 64")
	queryOperands := flag.String("query-operands", "", "optional comma-separated operand classes, e.g. r32,imm32")
	queryOpcode := flag.String("query-opcode", "", "optional reverse opcode-byte query, e.g. 05 or 0f 1f")
	flag.Parse()
	if *root == "" {
		fatal("-source is required")
	}
	commit := git(*root, "rev-parse", "HEAD")
	branch := git(*root, "branch", "--show-current")
	if *expanded == "" {
		*expanded = filepath.Join(os.TempDir(), "nasm-insns-expanded.dat")
		perl := findPerl()
		if perl == "" {
			fatal("Perl is required to run NASM's checked-in preinsns.pl; pass -expanded to use an already expanded table")
		}
		cmd := exec.Command(perl, "-I.", "x86/preinsns.pl", "x86/insns.dat", *expanded)
		cmd.Dir = *root
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fatal("preinsns.pl: %v", err)
		}
	}
	st, err := nasm.Extract(nasm.Config{SourceRoot: *root, ExpandedFile: *expanded, OutputDir: *out, Commit: commit, Branch: branch, NativeBackendFile: *backend})
	if err != nil {
		fatal("extract: %v", err)
	}
	fmt.Printf("NASM evidence generated: records=%d variants=%d encodings=%d mnemonics=%d registers=%d features=%d direct=%d derived=%d unresolved=%d\n", st.Records, st.Variants, st.Encodings, st.Mnemonics, st.Registers, st.Features, st.DirectFacts, st.DerivedFacts, st.Unresolved)
	if *queryMnemonic != "" {
		ops := []string{}
		if *queryOperands != "" {
			for _, x := range split(*queryOperands) {
				ops = append(ops, x)
			}
		}
		rows, e := nasm.LookupVariants(*out, *queryMnemonic, *queryMode, ops)
		if e != nil {
			fatal("query: %v", e)
		}
		printJSON(rows)
	}
	if *queryOpcode != "" {
		rows, e := nasm.LookupEncoding(*out, split(*queryOpcode))
		if e != nil {
			fatal("reverse query: %v", e)
		}
		printJSON(rows)
	}
}
func git(root string, args ...string) string {
	a := append([]string{"-C", root}, args...)
	b, e := exec.Command("git", a...).Output()
	if e != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(b))
}
func findPerl() string {
	for _, p := range []string{`C:\Program Files\Git\usr\bin\perl.exe`, `C:\msys64\usr\bin\perl.exe`} {
		if _, e := os.Stat(p); e == nil {
			return p
		}
	}
	p, e := exec.LookPath("perl")
	if e == nil {
		return p
	}
	return ""
}
func split(s string) []string {
	out := []string{}
	for _, x := range strings.Split(s, ",") {
		if x = strings.TrimSpace(x); x != "" {
			out = append(out, x)
		}
	}
	return out
}
func printJSON(v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		fatal("json: %v", e)
	}
	fmt.Println(string(b))
}
func fatal(f string, a ...any) { fmt.Fprintf(os.Stderr, f+"\n", a...); os.Exit(2) }
