package targetrun

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tarekwasfy01/Code-Transpiler/internal/backend"
	"github.com/tarekwasfy01/Code-Transpiler/internal/manytomany"
	"github.com/tarekwasfy01/Code-Transpiler/internal/runtimeassets"
)

type Result struct {
	Target     string
	Stdout     string
	Stderr     string
	SourcePath string
	Command    string
}

func RunRSource(target, rsrc string) (Result, error) {
	return RunSource(target, "r", rsrc)
}

// RunSource executes a source program through the canonical all-to-all
// frontend/UAST/backend route. Corpus packages are deliberately not passed
// here: the miner treats them as observations only.
func RunSource(target, sourceLanguage, sourceText string) (Result, error) {
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" || target == "r" || target == "embedded" {
		program, err := manytomany.Parse(sourceLanguage, sourceText)
		if err != nil {
			return Result{}, fmt.Errorf("parse %s source: %w", sourceLanguage, err)
		}
		out, err := backend.RunSemantic(program.Semantic)
		return Result{
			Target:  "embedded",
			Stdout:  out,
			Command: "embedded UniversalAST runtime",
		}, err
	}

	lang, ok := backend.ByID(target)
	if !ok {
		return Result{}, fmt.Errorf("unknown target %q", target)
	}

	program, err := manytomany.Parse(sourceLanguage, sourceText)
	if err != nil {
		return Result{}, fmt.Errorf("parse %s source: %w", sourceLanguage, err)
	}
	code, err := manytomany.Emit(target, program)
	if err != nil {
		return Result{}, fmt.Errorf("transpile %s to %s: %w", sourceLanguage, lang.Name, err)
	}

	tmp, err := os.MkdirTemp("", "r2many-run-"+target+"-")
	if err != nil {
		return Result{}, err
	}
	defer os.RemoveAll(tmp)

	base := "program"
	if target == "java" {
		base = "Main"
	}
	source := filepath.Join(tmp, base+lang.Extension)
	if err := os.WriteFile(source, []byte(code), 0o644); err != nil {
		return Result{}, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	runtimeDir, err := runtimeassets.Materialize(target, tmp)
	if err != nil {
		return Result{}, fmt.Errorf("materialize embedded %s runtime: %w", lang.Name, err)
	}

	res := Result{Target: target, SourcePath: source}
	_ = runtimeDir
	var stdout, stderr bytes.Buffer

	run := func(name string, args ...string) error {
		path, err := exec.LookPath(name)
		if err != nil {
			return fmt.Errorf("%s toolchain is not available in PATH", name)
		}
		res.Command = strings.Join(append([]string{path}, args...), " ")
		cmd := exec.CommandContext(ctx, path, args...)
		cmd.Dir = tmp
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
		res.Stdout = stdout.String()
		res.Stderr = stderr.String()
		if ctx.Err() == context.DeadlineExceeded {
			return fmt.Errorf("%s execution timed out", lang.Name)
		}
		if err != nil {
			return fmt.Errorf("%s command failed: %w", lang.Name, err)
		}
		return nil
	}

	exe := filepath.Join(tmp, "program")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}

	switch target {
	case "go":
		err = run("go", "run", source)

	case "rust":
		if err = run("rustc", "-O", source, "-o", exe); err == nil {
			stdout.Reset()
			stderr.Reset()
			err = run(exe)
		}

	case "cpp":
		compiler := firstTool("g++", "clang++")
		if compiler == "" {
			err = fmt.Errorf("C++ toolchain is not available in PATH (need g++ or clang++)")
			break
		}
		if err = run(compiler, "-std=c++17", "-O2", source, "-o", exe); err == nil {
			stdout.Reset()
			stderr.Reset()
			err = run(exe)
		}

	case "c":
		compiler := firstTool("gcc", "clang")
		if compiler == "" {
			err = fmt.Errorf("C toolchain is not available in PATH (need gcc or clang)")
			break
		}
		args := []string{"-std=c11", "-O2", source, "-o", exe}
		if runtime.GOOS != "windows" {
			args = append(args, "-lm")
		}
		if err = run(compiler, args...); err == nil {
			stdout.Reset()
			stderr.Reset()
			err = run(exe)
		}

	case "python":
		python := firstTool("python", "python3")
		if python == "" && runtime.GOOS == "windows" {
			if firstTool("py") != "" {
				err = run("py", "-3", source)
				break
			}
		}
		if python == "" {
			err = fmt.Errorf("Python runtime is not available in PATH")
			break
		}
		err = run(python, source)

	case "zig":
		err = run("zig", "run", source)

	case "julia":
		err = run("julia", source)

	case "nim":
		nimExe := filepath.Join(tmp, "nim-program")
		if runtime.GOOS == "windows" {
			nimExe += ".exe"
		}
		if err = run("nim", "c", "--hints:off", "--verbosity:0", "-o:"+nimExe, source); err == nil {
			stdout.Reset()
			stderr.Reset()
			err = run(nimExe)
		}

	case "csharp":
		err = runCSharp(ctx, tmp, source, &res, &stdout, &stderr)

	case "java":
		if err = run("javac", source); err == nil {
			stdout.Reset()
			stderr.Reset()
			err = run("java", "-cp", tmp, "Main")
		}

	case "kotlin":
		jar := filepath.Join(tmp, "program.jar")
		if err = run("kotlinc", source, "-include-runtime", "-d", jar); err == nil {
			stdout.Reset()
			stderr.Reset()
			err = run("java", "-jar", jar)
		}

	case "swift":
		swift := firstTool("swift")
		if swift != "" {
			err = run(swift, source)
			break
		}
		if err = run("swiftc", source, "-o", exe); err == nil {
			stdout.Reset()
			stderr.Reset()
			err = run(exe)
		}

	default:
		err = fmt.Errorf("target runner is not configured for %q", target)
	}

	res.Stdout = stdout.String()
	res.Stderr = stderr.String()
	return res, err
}

func firstTool(names ...string) string {
	for _, name := range names {
		if _, err := exec.LookPath(name); err == nil {
			return name
		}
	}
	return ""
}

func runCSharp(ctx context.Context, tmp, source string, res *Result, stdout, stderr *bytes.Buffer) error {
	if csc := firstTool("csc"); csc != "" {
		exe := filepath.Join(tmp, "program.exe")
		res.Command = csc + " /nologo /out:" + exe + " " + source
		cmd := exec.CommandContext(ctx, csc, "/nologo", "/out:"+exe, source)
		cmd.Dir = tmp
		cmd.Stdout, cmd.Stderr = stdout, stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("C# compile failed: %w", err)
		}
		stdout.Reset()
		stderr.Reset()
		res.Command = exe
		cmd = exec.CommandContext(ctx, exe)
		cmd.Dir = tmp
		cmd.Stdout, cmd.Stderr = stdout, stderr
		return cmd.Run()
	}

	if _, err := exec.LookPath("dotnet"); err != nil {
		return fmt.Errorf("C# runtime/toolchain is not available in PATH (need csc or dotnet)")
	}
	project := filepath.Join(tmp, "R2ManyRun.csproj")
	csproj := `<Project Sdk="Microsoft.NET.Sdk">
  <PropertyGroup>
    <OutputType>Exe</OutputType>
    <TargetFramework>net8.0</TargetFramework>
    <ImplicitUsings>enable</ImplicitUsings>
    <Nullable>disable</Nullable>
  </PropertyGroup>
</Project>`
	if err := os.WriteFile(project, []byte(csproj), 0o644); err != nil {
		return err
	}
	program := filepath.Join(tmp, "Program.cs")
	if source != program {
		data, err := os.ReadFile(source)
		if err != nil {
			return err
		}
		if err := os.WriteFile(program, data, 0o644); err != nil {
			return err
		}
	}
	res.Command = "dotnet run --project " + project
	cmd := exec.CommandContext(ctx, "dotnet", "run", "--project", project, "--nologo")
	cmd.Dir = tmp
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("C# dotnet run failed: %w", err)
	}
	return nil
}
