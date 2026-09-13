// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestCanonicalFunctionBindingNameDoesNotAssignAnonymousClosuresByOrder(t *testing.T) {
	g := &uastExecutionGraph{
		common: map[int]universalDecodedCommon{
			1: {Kind: "function"},
			2: {Kind: "function"},
			3: {Kind: "assign", Name: "native_function_0"},
			4: {Kind: "assign", Name: "native_var_local"},
		},
		children: map[int]map[string][]universalChild{
			3: {"expression": {{ID: 1}}},
			4: {"expression": {{ID: 2}}},
		},
	}
	bindings := map[string]string{"native_function_0": "declared"}
	if got := canonicalFunctionBindingName(g, 1, bindings); got != "declared" {
		t.Fatalf("declared function binding=%q, want declared", got)
	}
	if got := canonicalFunctionBindingName(g, 2, bindings); got != "" {
		t.Fatalf("local anonymous closure was exported as %q", got)
	}
}

func TestProjectFunctionReferenceResolvesCanonicalPackageQualifiedName(t *testing.T) {
	fn := ProjectFunctionSummary{ID: projectFunctionLabel("backend.MergeSemanticFiles"), Name: "MergeSemanticFiles"}
	if !projectFunctionReferenceMatches("backend_MergeSemanticFiles", "backend", fn) {
		t.Fatal("package-qualified UAST symbol did not resolve to its owning project declaration")
	}
	if projectFunctionReferenceMatches("other_MergeSemanticFiles", "backend", fn) {
		t.Fatal("unrelated package symbol resolved by name tail")
	}
}

// This is the smallest executable proof for the project linker contract. The
// two bodies remain independent SemanticPrograms; only a symbol relocation
// connects main's call site to foo's defining MachineFragment.
func TestSemanticProjectLinksCrossUnitFunction(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("the current native image writer targets Windows PE")
	}
	dir := t.TempDir()
	fooGo := filepath.Join(dir, "foo.go")
	mainGo := filepath.Join(dir, "main.go")
	fooSource := "package main\nfunc foo() int { return 37 }\n"
	mainSource := "package main\nfunc entry() int { return foo() + 5 }\n"
	if err := os.WriteFile(fooGo, []byte(fooSource), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainGo, []byte(mainSource), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		name   string
		path   string
		source string
	}{
		{name: "a_foo.se", path: fooGo, source: fooSource},
		{name: "b_main.se", path: mainGo, source: mainSource},
	} {
		program, err := LowerNativeGo(input.path, input.source)
		if err != nil {
			t.Fatalf("lower %s: %v", input.name, err)
		}
		data, err := program.MarshalSemanticSESemanticOnly()
		if err != nil {
			t.Fatalf("marshal %s: %v", input.name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, input.name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	project, err := LoadSemanticProject(dir, "entry")
	if err != nil {
		t.Fatal(err)
	}
	result, err := CompileSemanticProject(project, CompileOptions{
		TargetArch: "x86_64", TargetOS: "windows", ABI: "win64",
		OutputKind: CompileExecutable, EntryPoint: "entry", MaxWorkers: 2,
	})
	if err != nil {
		t.Fatalf("compile: %v symbols=%#v dependencies=%#v", err, project.Index.Symbols, project.Index.Dependencies)
	}
	if result.NativeUnitCount != 2 {
		t.Fatalf("native units=%d, want 2", result.NativeUnitCount)
	}
	if result.NativeRelocationCount == 0 {
		t.Fatal("cross-unit call produced no relocation")
	}
	fooLabel := projectFunctionLabel("main.foo")
	foo, ok := project.Index.Symbols[fooLabel]
	if !ok || foo.UnitID == "" || foo.Name != "foo" || foo.QualifiedName != "main.foo" {
		t.Fatalf("global symbol index lacks foo: %#v", foo)
	}
	if len(project.Index.Dependencies["b_main.se"]) == 0 {
		t.Fatalf("main unit has no indexed cross-unit dependency: deps=%#v symbols=%#v relocations=%d", project.Index.Dependencies, project.Index.Symbols, result.NativeRelocationCount)
	}
	exe := filepath.Join(dir, "cross-unit.exe")
	if err := os.WriteFile(exe, result.Bytes, 0o755); err != nil {
		t.Fatal(err)
	}
	err = exec.Command(exe).Run()
	if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 42 {
		t.Fatalf("cross-unit executable exit=%v, want 42", err)
	}
}
