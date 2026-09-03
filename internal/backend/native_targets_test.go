package backend

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// Candidate backend validation goes directly through lowering. Passing this
// test is required before the public capability matrix enables the route.
func TestNativeScalarCandidateTargets(t *testing.T) {
	if os.Getenv("CODETRANSPILER_NATIVE_E2E") != "1" {
		t.Skip("set CODETRANSPILER_NATIVE_E2E=1 for candidate compiler execution")
	}
	source, _ := nativeScalarCorpus()
	p, err := LowerNativeGo("native.go", source)
	if err != nil {
		t.Fatal(err)
	}
	data, err := p.MarshalSemanticJSON()
	if err != nil {
		t.Fatal(err)
	}
	p, err = ParseSemanticJSON(data)
	if err != nil {
		t.Fatal(err)
	}
	expected := ObserveSemantic(p)
	if expected.Error != "" {
		t.Fatal(expected.Error)
	}
	for _, target := range []string{"python", "rust", "c"} {
		t.Run(target, func(t *testing.T) {
			compiler := map[string]string{"python": "python", "rust": "rustc", "c": "gcc"}[target]
			if _, err := exec.LookPath(compiler); err != nil {
				t.Skipf("%s unavailable", compiler)
			}
			code, err := EmitSemantic(target, p)
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			file := filepath.Join(dir, "program"+map[string]string{"python": ".py", "rust": ".rs", "c": ".c"}[target])
			binary := filepath.Join(dir, "program.exe")
			if err = os.WriteFile(file, []byte(code), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			var command *exec.Cmd
			switch target {
			case "python":
				command = exec.CommandContext(ctx, compiler, file)
			case "rust":
				command = exec.CommandContext(ctx, compiler, "--edition=2021", file, "-o", binary)
			case "c":
				command = exec.CommandContext(ctx, compiler, "-std=c11", file, "-o", binary, "-lm")
			}
			if target != "python" {
				if out, err := command.CombinedOutput(); err != nil {
					t.Fatalf("compile: %v\n%s", err, out)
				}
				command = exec.CommandContext(ctx, binary)
			}
			var stderr bytes.Buffer
			command.Stderr = &stderr
			output, err := command.Output()
			if err != nil {
				t.Fatalf("execution: %v %s", err, stderr.String())
			}
			if string(output) != expected.Stdout || stderr.Len() != 0 {
				t.Fatalf("stdout %q != %q, stderr %q", output, expected.Stdout, stderr.String())
			}
		})
	}
}
