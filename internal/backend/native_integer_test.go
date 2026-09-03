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

func TestNativeExactIntegerPipeline(t *testing.T) {
	source, err := os.ReadFile("testdata/native_integers.go")
	if err != nil {
		t.Fatal(err)
	}
	want := "9007199254740993\n9007199254740994\n0\n-9223372036854775808\n65535\n255\n254\n1\n0\nsigned order\nunsigned order\n0\n1\n2\n"
	p, err := LowerNativeGo("integers.go", string(source))
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
	again, err := p.MarshalSemanticJSON()
	if err != nil || !bytes.Equal(data, again) {
		t.Fatal("integer JSON is not deterministic", err)
	}
	observation := ObserveSemantic(p)
	if observation.Error != "" || observation.Stdout != want {
		t.Fatalf("runtime: %+v", observation)
	}
	for _, target := range Backends() {
		_, err := EmitSemantic(target.ID, p)
		supported := target.ID == "go" || target.ID == "python" || target.ID == "rust" || target.ID == "c" || target.ID == "cpp" || target.ID == "java" || target.ID == "csharp"
		if supported != (err == nil) {
			t.Errorf("target %s: %v", target.ID, err)
		}
	}
	checkIntegerTargets(t, source, want, p)
}

func checkIntegerTargets(t *testing.T, source []byte, want string, p *SemanticProgram) {
	t.Helper()
	var err error
	if os.Getenv("CODETRANSPILER_NATIVE_E2E") != "1" {
		return
	}
	for _, target := range []string{"original", "go", "python", "rust", "c", "cpp", "java", "csharp"} {
		t.Run(target, func(t *testing.T) {
			code := string(source)
			if target != "original" {
				code, err = EmitSemantic(target, p)
				if err != nil {
					t.Fatal(err)
				}
			}
			extension := ".go"
			if target == "python" {
				extension = ".py"
			}
			if target == "rust" {
				extension = ".rs"
			}
			if target == "c" {
				extension = ".c"
			}
			if target == "cpp" {
				extension = ".cpp"
			}
			if target == "java" {
				extension = ".java"
			}
			if target == "csharp" {
				extension = ".cs"
			}
			base := "main"
			if target == "java" {
				base = "Main"
			}
			file := filepath.Join(t.TempDir(), base+extension)
			if err := os.WriteFile(file, []byte(code), 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			command := exec.CommandContext(ctx, "go", "run", file)
			if target == "python" {
				command = exec.CommandContext(ctx, "python", file)
			}
			if target == "rust" || target == "c" || target == "cpp" {
				binary := filepath.Join(filepath.Dir(file), "program.exe")
				compiler := exec.CommandContext(ctx, "rustc", "--edition=2021", file, "-o", binary)
				if target == "c" {
					compiler = exec.CommandContext(ctx, "gcc", "-std=c11", "-O2", file, "-o", binary, "-lm")
				}
				if target == "cpp" {
					compiler = exec.CommandContext(ctx, "g++", "-std=c++17", "-O2", file, "-o", binary)
				}
				if out, err := compiler.CombinedOutput(); err != nil {
					t.Fatalf("compile: %v\n%s", err, out)
				}
				command = exec.CommandContext(ctx, binary)
			}
			if target == "java" {
				javac, lookErr := exec.LookPath("javac")
				if lookErr != nil {
					t.Fatal(lookErr)
				}
				if out, err := exec.CommandContext(ctx, javac, "-d", filepath.Dir(file), file).CombinedOutput(); err != nil {
					t.Fatalf("compile: %v\n%s", err, out)
				}
				command = exec.CommandContext(ctx, filepath.Join(filepath.Dir(javac), "java.exe"), "-cp", filepath.Dir(file), "Main")
			}
			if target == "csharp" {
				project := filepath.Join(filepath.Dir(file), "Program.csproj")
				projectText := "<Project Sdk=\"Microsoft.NET.Sdk\"><PropertyGroup><OutputType>Exe</OutputType><TargetFramework>net10.0</TargetFramework><ImplicitUsings>disable</ImplicitUsings><Nullable>disable</Nullable></PropertyGroup></Project>"
				if err := os.WriteFile(project, []byte(projectText), 0600); err != nil {
					t.Fatal(err)
				}
				command = exec.CommandContext(ctx, "dotnet", "run", "--project", project, "-c", "Release")
			}
			var stderr bytes.Buffer
			command.Stderr = &stderr
			out, err := command.Output()
			if err != nil || stderr.Len() != 0 || string(out) != want {
				t.Fatalf("%v stdout=%q stderr=%s", err, out, stderr.String())
			}
		})
	}
}
