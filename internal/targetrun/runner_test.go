package targetrun

import "testing"

func TestRunSourceUsesCanonicalUASTForPython(t *testing.T) {
	result, err := RunSource("embedded", "python", "x = 2\nprint(x)\n")
	if err != nil {
		t.Fatal(err)
	}
	if result.Stdout != "2\n" {
		t.Fatalf("stdout = %q, want %q", result.Stdout, "2\n")
	}
	if result.Command != "embedded UniversalAST runtime" {
		t.Fatalf("command = %q", result.Command)
	}
}
