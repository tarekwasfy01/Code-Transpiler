// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func assertExternalExecutableOutput(t *testing.T, artifact []byte, source, want string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "external-c-smoke.exe")
	if err := os.WriteFile(path, artifact, 0o700); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(path).CombinedOutput()
	if err != nil {
		t.Fatalf("external C artifact failed: %v: %s", err, out)
	}
	if got := strings.TrimSpace(string(out)); got != want {
		t.Fatalf("external C artifact output=%q, want %q\nC source:\n%s", got, want, source)
	}
}

func TestExternalGCCConsumesCanonicalUAST(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("GCC/MinGW is not installed")
	}
	program, err := LowerNativeGo("external_gcc.go", `package main
func main() { println(7) }
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompileExternalC(program, ExternalCCompileOptions{
		Family: ExternalCompilerGCC, OutputKind: CompileExecutable, Optimization: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bytes) == 0 || result.SourceSHA256 == "" || result.ArtifactSHA256 == "" {
		t.Fatalf("incomplete GCC result: %+v", result)
	}
	if result.Family != ExternalCompilerGCC || result.CompilerPath == "" {
		t.Fatalf("wrong toolchain evidence: %+v", result)
	}
	assertExternalExecutableOutput(t, result.Bytes, result.Source, "7")
}

func TestExternalMSVCConsumesCanonicalUAST(t *testing.T) {
	available := false
	discovered := DiscoverExternalCompilers()
	t.Logf("external compiler discovery: %+v", discovered)
	for _, compiler := range discovered {
		if compiler.Family == ExternalCompilerMSVC && compiler.Available {
			available = true
			break
		}
	}
	if !available {
		t.Skip("MSVC is not installed")
	}
	program, err := LowerNativeGo("external_msvc.go", `package main
func main() { println(7) }
`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompileExternalC(program, ExternalCCompileOptions{
		Family: ExternalCompilerMSVC, OutputKind: CompileExecutable, Optimization: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Bytes) == 0 || result.ArtifactSHA256 == "" || result.Family != ExternalCompilerMSVC {
		t.Fatalf("incomplete MSVC result: %+v", result)
	}
	assertExternalExecutableOutput(t, result.Bytes, result.Source, "7")
}
