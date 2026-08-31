package backend

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Empty collection calls previously emitted empty compound-array initializers,
// which GCC accepts as an extension but a strict C11 compiler must reject.
func TestCEmptyCollectionDispatchStrictC11(t *testing.T) {
	compiler, err := exec.LookPath("gcc")
	if err != nil {
		t.Skip("native C11 regression requires gcc")
	}
	ast, err := parse("print(length(c())); print(length(list())); print(length(c(2,3)))")
	if err != nil {
		t.Fatal(err)
	}
	code, err := generateTarget("c", ast)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	source, executable := filepath.Join(dir, "program.c"), filepath.Join(dir, "program.exe")
	if err := os.WriteFile(source, []byte(code), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if output, err := exec.CommandContext(ctx, compiler, "-std=c11", "-pedantic-errors", source, "-o", executable, "-lm").CombinedOutput(); err != nil {
		t.Fatalf("strict C11 compilation failed: %v\n%s", err, output)
	}
	output, err := exec.CommandContext(ctx, executable).CombinedOutput()
	if err != nil {
		t.Fatalf("native C execution failed: %v\n%s", err, output)
	}
	if got := strings.TrimSpace(strings.ReplaceAll(string(output), "\r", "")); got != "0\n0\n2" {
		t.Fatalf("empty/nonempty collection lengths = %q, want 0, 0, 2", got)
	}
}
