// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestLLVMProjectTwoUnitAcceptance(t *testing.T) {
	dir := t.TempDir()
	write := func(name, source string) {
		p, err := LowerNativeGo(name, source)
		if err != nil {
			t.Fatal(err)
		}
		data, err := p.MarshalSemanticSESemanticOnly()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, name+".se"), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("a", "package main\nfunc foo() int { return 37 }\n")
	write("b", "package main\nfunc entry() int { return foo()+5 }\n")
	p, err := LoadSemanticProject(dir, "entry")
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompileLLVMProject(p, LLVMCompileOptions{LLVMPath: `C:\Program Files\clang+llvm-23.1.1-x86_64-pc-windows-msvc\bin`, TargetTriple: "x86_64-pc-windows-msvc", EntryPoint: "entry", OutputKind: LLVMExecutable})
	if err != nil {
		t.Fatal(err)
	}
	if result.ObjectCount != 2 {
		t.Fatalf("object count=%d, want 2", result.ObjectCount)
	}
	if len(result.Executable) == 0 {
		t.Fatal("LLVM project executable is empty")
	}
	exe := filepath.Join(dir, "project.exe")
	if err := os.WriteFile(exe, result.Executable, 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(exe).Run(); err != nil {
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 42 {
			t.Fatalf("LLVM project exit=%v, want 42", err)
		}
	}
}
