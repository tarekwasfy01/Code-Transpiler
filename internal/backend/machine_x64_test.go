package backend

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestX64AssemblerCrosscheck(t *testing.T) {
	if _, err := exec.LookPath("nasm"); err != nil {
		t.Skip("NASM not installed")
	}
	p := x64Program{Instructions: []x64Instruction{{"label", xl("native_entry"), x64Operand{}}, {"push", xr(xRBP), x64Operand{}}, {"mov", xr(xRBP), xr(xRSP)}, {"sub_sp", xi(64), x64Operand{}}, {"mov", xr(xRAX), xi(123456789)}, {"mov", xm(xRBP, -8), xr(xRAX)}, {"mov", xr(xR10), xm(xRBP, -8)}, {"add", xr(xRAX), xr(xR10)}, {"imul", xr(xRAX), xr(xR10)}, {"cmp", xr(xRAX), xr(xR10)}, {"je", xl("end"), x64Operand{}}, {"mov_to_xmm", xr(4), xr(xRAX)}, {"mov_to_xmm", xr(5), xr(xR10)}, {"addsd", xr(4), xr(5)}, {"mov_from_xmm", xr(xRAX), xr(4)}, {"label", xl("end"), x64Operand{}}, {"mov", xr(xRSP), xr(xRBP)}, {"pop", xr(xRBP), x64Operand{}}, {"ret", x64Operand{}, x64Operand{}}}}
	expected, _, err := encodeX64(p)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "test.asm")
	dst := filepath.Join(dir, "test.bin")
	if err = os.WriteFile(src, []byte(renderX64(p)), 0600); err != nil {
		t.Fatal(err)
	}
	if log, err := exec.Command("nasm", "-O0", "-f", "bin", "-o", dst, src).CombinedOutput(); err != nil {
		t.Fatalf("assembler: %v %s\n%s", err, log, renderX64(p))
	}
	actual, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, actual) {
		t.Fatalf("encoding mismatch\ninternal %x\nassembler %x\n%s", expected, actual, renderX64(p))
	}
}

func TestX64RejectUndefinedLabel(t *testing.T) {
	if _, _, err := encodeX64(x64Program{Instructions: []x64Instruction{{"call", xl("missing"), x64Operand{}}}}); err == nil {
		t.Fatal("undefined symbol accepted")
	}
}
