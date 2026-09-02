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

func TestNativeFunctions(t *testing.T) {
	sourceBytes, err := os.ReadFile("testdata/native_functions.go")
	if err != nil {
		t.Fatal(err)
	}
	source := string(sourceBytes)
	want := "changed\noriginal\nleft\nright\nleft\nskipped\nprobe\nevaluated\nchosen\nfallback\nbinary-left\nbinary-right\nunequal\nearly\nafter\nfallthrough\nunused effect\n"
	p, err := LowerNativeGo("functions.go", source)
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
		t.Fatal("unstable function document", err)
	}
	observation := ObserveSemantic(p)
	if observation.Error != "" || observation.Stdout != want {
		t.Fatalf("runtime: %+v", observation)
	}
	if os.Getenv("CODETRANSPILER_NATIVE_E2E") != "1" {
		return
	}
	for _, target := range []string{"original", "go", "python", "rust", "c", "cpp", "java"} {
		t.Run(target, func(t *testing.T) {
			code := source
			if target != "original" {
				code, err = EmitSemantic(target, p)
				if err != nil {
					t.Fatal(err)
				}
			}
			compiler, extension := "go", ".go"
			if target == "python" {
				compiler, extension = "python", ".py"
			}
			if target == "rust" {
				compiler, extension = "rustc", ".rs"
			}
			if target == "c" {
				compiler, extension = "gcc", ".c"
			}
			if target == "cpp" {
				compiler, extension = "g++", ".cpp"
			}
			if target == "java" {
				compiler, extension = "javac", ".java"
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
			args := []string{file}
			if compiler == "go" {
				args = []string{"run", file}
			}
			command := exec.CommandContext(ctx, compiler, args...)
			if target == "rust" || target == "c" || target == "cpp" {
				binary := filepath.Join(filepath.Dir(file), "program.exe")
				args = []string{"--edition=2021", file, "-o", binary}
				if target == "c" {
					args = []string{"-std=c11", "-O2", file, "-o", binary, "-lm"}
				}
				if target == "cpp" {
					args = []string{"-std=c++17", "-O2", file, "-o", binary}
				}
				if out, err := exec.CommandContext(ctx, compiler, args...).CombinedOutput(); err != nil {
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
			var stderr bytes.Buffer
			command.Stderr = &stderr
			output, err := command.Output()
			if err != nil || stderr.Len() != 0 || string(output) != want {
				t.Fatalf("execution: %v stdout=%q stderr=%s", err, output, stderr.String())
			}
		})
	}
}

func TestNativeFunctionBoundaries(t *testing.T) {
	for _, source := range []string{
		`package main; func a(){b()};func b(){a()};func main(){a()}`,
		`package main; func a(x ...string){};func main(){a("x")}`,
		`package main; func a()(s string){return};func main(){a()}`,
		`package main; func a()(bool,string){return true,"x"};func main(){a()}`,
		`package main; func a(x int){};func main(){a(1)}`,
		`package main; func a()bool{return true};func main(){f:=a;f()}`,
	} {
		if _, err := LowerNativeGo("unsupported.go", source); err == nil {
			t.Errorf("accepted unsupported function: %s", source)
		}
	}
	p, err := LowerNativeGo("graph.go", `package main;func a(){b()};func b(){};func main(){a()}`)
	if err != nil {
		t.Fatal(err)
	}
	graph := p.Extensions["native_call_graph"].(map[string]any)
	if graph["rows"] != 2 || graph["cols"] != 2 {
		t.Fatalf("unexpected graph %v", graph)
	}
	entries := graph["entries"].([][3]int)
	if len(entries) != 1 || entries[0] != [3]int{0, 1, 1} {
		t.Fatalf("unexpected call edge %v", entries)
	}
	for _, target := range Backends() {
		_, err := EmitSemantic(target.ID, p)
		supported := target.ID == "go" || target.ID == "python" || target.ID == "c" || target.ID == "rust" || target.ID == "cpp" || target.ID == "java" || target.ID == "csharp"
		if supported != (err == nil) {
			t.Errorf("target %s: %v", target.ID, err)
		}
	}
}
