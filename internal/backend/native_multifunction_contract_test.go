// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// This witness exercises the generic SemanticProgram -> native path without a
// language frontend. It guards function enumeration, Win64 argument binding,
// internal CALL lowering and return propagation as one contract.
func TestNativeSemanticInternalCallReturnsValue(t *testing.T) {
	i32 := &SemanticType{Kind: "integer", Bits: 32}
	add := &FunctionExpr{Binding: "Add", Params: []Param{{Name: "a", Type: i32}, {Name: "b", Type: i32}}, Body: &BlockStmt{List: []Stmt{
		&ReturnStmt{X: &BinaryExpr{Op: "+", L: &IdentExpr{Name: "a"}, R: &IdentExpr{Name: "b"}}},
	}}}
	main := &FunctionExpr{Binding: "main", Body: &BlockStmt{List: []Stmt{
		&ReturnStmt{X: &CallExpr{Fun: &IdentExpr{Name: "Add"}, Args: []Arg{{Value: &LiteralExpr{Kind: "integer", Text: "20"}}, {Value: &LiteralExpr{Kind: "integer", Text: "22"}}}, Eager: true}},
	}}}
	p := NewSemanticProgram(&BlockStmt{List: []Stmt{
		&AssignStmt{Name: "Add", Op: "<-", Value: add},
		&AssignStmt{Name: "main", Op: "<-", Value: main},
	}}, "eager_left_to_right")
	result, err := EmitNativeExecutable(p, "native-x86_64-windows", "main")
	if err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(t.TempDir(), "internal-call.exe")
	if err := os.WriteFile(exe, result.Bytes, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(exe)
	if err := cmd.Run(); err == nil {
		t.Fatal("native witness returned success; expected process exit 42")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 42 {
		t.Fatalf("native witness exit=%v, want 42", err)
	}
}
