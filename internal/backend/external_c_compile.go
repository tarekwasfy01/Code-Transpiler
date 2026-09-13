// Copyright (c) 2026 Tarek Wasfy
package backend

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// ExternalCompilerFamily selects the final native C toolchain. The semantic
// compiler boundary remains the canonical UAST; GCC/MinGW and MSVC only
// consume the target-legal C translation unit produced by the existing
// projector.
type ExternalCompilerFamily string

const (
	ExternalCompilerAuto ExternalCompilerFamily = "auto"
	ExternalCompilerGCC  ExternalCompilerFamily = "gcc"
	ExternalCompilerMSVC ExternalCompilerFamily = "msvc"
)

type ExternalCCompileOptions struct {
	Family         ExternalCompilerFamily
	TargetLanguage string
	OutputKind     CompileOutputKind
	CompilerPath   string
	MSVCSetupPath  string
	Optimization   int
	Timeout        time.Duration
}

type ExternalCCompileResult struct {
	Bytes          []byte                 `json:"-"`
	Source         string                 `json:"source"`
	SourceSHA256   string                 `json:"source_sha256"`
	ArtifactSHA256 string                 `json:"artifact_sha256,omitempty"`
	OutputKind     CompileOutputKind      `json:"output_kind"`
	Family         ExternalCompilerFamily `json:"family"`
	CompilerPath   string                 `json:"compiler_path"`
	Command        []string               `json:"command"`
	ProjectionMode string                 `json:"projection_mode"`
	Stdout         string                 `json:"stdout,omitempty"`
	Stderr         string                 `json:"stderr,omitempty"`
}

type ExternalCompilerAvailability struct {
	Family        ExternalCompilerFamily `json:"family"`
	Available     bool                   `json:"available"`
	CompilerPath  string                 `json:"compiler_path,omitempty"`
	MSVCSetupPath string                 `json:"msvc_setup_path,omitempty"`
	Diagnostic    string                 `json:"diagnostic,omitempty"`
}

type resolvedExternalCompiler struct {
	family    ExternalCompilerFamily
	compiler  string
	env       []string
	setupPath string
}

// DiscoverExternalCompilers reports every usable driver independently. Auto
// selection uses this same discovery and therefore cannot disagree with GUI or
// CLI preflight.
func DiscoverExternalCompilers() []ExternalCompilerAvailability {
	out := make([]ExternalCompilerAvailability, 0, 2)
	for _, family := range []ExternalCompilerFamily{ExternalCompilerGCC, ExternalCompilerMSVC} {
		resolved, err := resolveExternalCompiler(ExternalCCompileOptions{Family: family})
		cell := ExternalCompilerAvailability{Family: family, Available: err == nil}
		if err != nil {
			cell.Diagnostic = err.Error()
		} else {
			cell.CompilerPath = resolved.compiler
			cell.MSVCSetupPath = resolved.setupPath
		}
		out = append(out, cell)
	}
	return out
}

// CompileExternalC executes the existing structured lowering path and then
// delegates only native object/executable production to GCC/MinGW or MSVC.
// It never reparses Event.Text and never invokes a source-language compiler.
func CompileExternalC(p *SemanticProgram, opts ExternalCCompileOptions) (ExternalCCompileResult, error) {
	var result ExternalCCompileResult
	if p == nil {
		return result, fmt.Errorf("EXTERNAL_C_COMPILE_CONTRACT: missing semantic program")
	}
	if opts.OutputKind == "" {
		opts.OutputKind = CompileExecutable
	}
	if opts.OutputKind != CompileSource && opts.OutputKind != CompileObject && opts.OutputKind != CompileExecutable {
		return result, fmt.Errorf("EXTERNAL_C_COMPILE_CONTRACT: unsupported output kind %q", opts.OutputKind)
	}
	if opts.Optimization < 0 || opts.Optimization > 3 {
		return result, fmt.Errorf("EXTERNAL_C_COMPILE_CONTRACT: optimization must be 0..3")
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 3 * time.Minute
	}

	target := NormalizeLanguage(opts.TargetLanguage)
	if target == "" {
		target = "c"
	}
	if target != "c" && target != "cpp" {
		return result, fmt.Errorf("EXTERNAL_TARGET_CONTRACT: unsupported native target %q (expected c or cpp)", target)
	}
	source, projection, err := externalNativeTargetSource(target, p)
	if err != nil {
		return result, fmt.Errorf("EXTERNAL_%s_PROJECTION_FAILED: %w", strings.ToUpper(target), err)
	}
	result.Source = source
	result.SourceSHA256 = sha256String(source)
	result.OutputKind = opts.OutputKind
	result.ProjectionMode = projection
	if opts.OutputKind == CompileSource {
		result.Bytes = []byte(source)
		result.ArtifactSHA256 = result.SourceSHA256
		return result, nil
	}

	tool, err := resolveExternalCompiler(opts)
	if err != nil {
		return result, err
	}
	result.Family, result.CompilerPath = tool.family, tool.compiler
	work, err := os.MkdirTemp("", "code-transpiler-external-c-")
	if err != nil {
		return result, err
	}
	// Keep generated sources for compiler diagnostics when explicitly requested.
	// Normal builds retain the existing cleanup behavior.
	if os.Getenv("CODE_TRANSPILER_KEEP_EXTERNAL_C") == "" {
		defer os.RemoveAll(work)
	}
	sourceExt := ".c"
	if target == "cpp" {
		sourceExt = ".cpp"
	}
	sourcePath := filepath.Join(work, "program"+sourceExt)
	if err := os.WriteFile(sourcePath, []byte(source), 0o600); err != nil {
		return result, err
	}
	ext := ".exe"
	if opts.OutputKind == CompileObject {
		ext = ".obj"
	}
	artifactPath := filepath.Join(work, "program"+ext)

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()
	args := externalCompilerArgs(tool.family, sourcePath, artifactPath, opts)
	result.Command = append([]string{tool.compiler}, args...)
	cmd := exec.CommandContext(ctx, tool.compiler, args...)
	cmd.Dir = work
	if len(tool.env) > 0 {
		cmd.Env = tool.env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	runErr := cmd.Run()
	result.Stdout, result.Stderr = stdout.String(), stderr.String()
	if ctx.Err() == context.DeadlineExceeded {
		return result, fmt.Errorf("EXTERNAL_C_TOOLCHAIN_TIMEOUT: %s", tool.family)
	}
	if runErr != nil {
		return result, fmt.Errorf("EXTERNAL_C_TOOLCHAIN_FAILED family=%s compiler=%s: %w: %s", tool.family, tool.compiler, runErr, strings.TrimSpace(result.Stderr+"\n"+result.Stdout))
	}
	result.Bytes, err = os.ReadFile(artifactPath)
	if err != nil {
		return result, fmt.Errorf("EXTERNAL_C_ARTIFACT_MISSING: %w", err)
	}
	result.ArtifactSHA256 = sha256Bytes(result.Bytes)
	return result, nil
}

func externalCTargetSource(p *SemanticProgram) (string, string, error) {
	return externalNativeTargetSource("c", p)
}

func externalNativeTargetSource(target string, p *SemanticProgram) (string, string, error) {
	if code, err := EmitSemanticDirect(target, p); err == nil && strings.TrimSpace(code) != "" && !AnalyzeRuntimeTaint(code, nil).Tainted() {
		return code, "direct", nil
	}
	if code, trace, err := EmitSemanticLoweredDirect(target, p); err == nil && strings.TrimSpace(code) != "" && !AnalyzeRuntimeTaint(code, nil).Tainted() {
		mode := "primitive-lowering"
		if len(trace.Rules) == 0 {
			mode = "lowered-direct"
		}
		return code, mode, nil
	}
	code, err := EmitSemanticCompatibility(target, p)
	if err != nil {
		return "", "", fmt.Errorf("EXTERNAL_%s_PROJECTION_FAILED: %w", strings.ToUpper(target), err)
	}
	if strings.TrimSpace(code) == "" {
		return "", "", fmt.Errorf("EXTERNAL_%s_PROJECTION_FAILED: empty native translation unit", strings.ToUpper(target))
	}
	return code, "compatibility-runtime", nil
}

func externalCompilerArgs(family ExternalCompilerFamily, source, output string, opts ExternalCCompileOptions) []string {
	if family == ExternalCompilerMSVC {
		msvcOptimization := []string{"/Od", "/O1", "/O2", "/Ox"}[opts.Optimization]
		args := []string{"/nologo", "/std:c11", msvcOptimization}
		if NormalizeLanguage(opts.TargetLanguage) == "cpp" {
			args = []string{"/nologo", "/std:c++20", "/TP", msvcOptimization}
		}
		if opts.OutputKind == CompileObject {
			return append(args, "/c", source, "/Fo:"+output)
		}
		return append(args, source, "/Fe:"+output)
	}
	standard := "-std=c11"
	if NormalizeLanguage(opts.TargetLanguage) == "cpp" {
		standard = "-std=c++20"
	}
	args := []string{standard, fmt.Sprintf("-O%d", opts.Optimization), source}
	if opts.OutputKind == CompileObject {
		args = append(args, "-c")
	}
	args = append(args, "-o", output)
	if opts.OutputKind == CompileExecutable {
		args = append(args, "-lm")
	}
	return args
}

func resolveExternalCompiler(opts ExternalCCompileOptions) (resolvedExternalCompiler, error) {
	family := opts.Family
	if family == "" {
		family = ExternalCompilerAuto
	}
	if family == ExternalCompilerAuto {
		if tool, err := resolveExternalCompiler(ExternalCCompileOptions{Family: ExternalCompilerGCC, CompilerPath: opts.CompilerPath}); err == nil {
			return tool, nil
		}
		return resolveExternalCompiler(ExternalCCompileOptions{Family: ExternalCompilerMSVC, CompilerPath: opts.CompilerPath, MSVCSetupPath: opts.MSVCSetupPath})
	}
	switch family {
	case ExternalCompilerGCC:
		path := opts.CompilerPath
		if path == "" {
			for _, name := range []string{"gcc", "x86_64-w64-mingw32-gcc"} {
				if found, err := exec.LookPath(name); err == nil {
					path = found
					break
				}
			}
		}
		if path == "" {
			return resolvedExternalCompiler{}, fmt.Errorf("GCC/MinGW compiler is not available")
		}
		return resolvedExternalCompiler{family: family, compiler: path}, nil
	case ExternalCompilerMSVC:
		if opts.CompilerPath != "" {
			env := os.Environ()
			if opts.MSVCSetupPath != "" {
				var err error
				env, err = environmentFromBatch(opts.MSVCSetupPath)
				if err != nil {
					return resolvedExternalCompiler{}, err
				}
			}
			return resolvedExternalCompiler{family: family, compiler: opts.CompilerPath, env: env, setupPath: opts.MSVCSetupPath}, nil
		}
		if found, err := exec.LookPath("cl.exe"); err == nil {
			return resolvedExternalCompiler{family: family, compiler: found, env: os.Environ()}, nil
		}
		setup := opts.MSVCSetupPath
		if setup == "" {
			setup = discoverMSVCSetup()
		}
		if setup == "" {
			return resolvedExternalCompiler{}, fmt.Errorf("MSVC compiler environment is not available")
		}
		env, err := environmentFromBatch(setup)
		if err != nil {
			return resolvedExternalCompiler{}, err
		}
		env = augmentMSVCWindowsSDK(env)
		compiler := lookPathInEnvironment("cl.exe", env)
		if compiler == "" {
			return resolvedExternalCompiler{}, fmt.Errorf("MSVC setup did not expose cl.exe")
		}
		return resolvedExternalCompiler{family: family, compiler: compiler, env: env, setupPath: setup}, nil
	default:
		return resolvedExternalCompiler{}, fmt.Errorf("unknown external compiler family %q", family)
	}
}

func discoverMSVCSetup() string {
	if runtime.GOOS != "windows" {
		return ""
	}
	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	if programFilesX86 == "" {
		programFilesX86 = `C:\Program Files (x86)`
	}
	vswhere := filepath.Join(programFilesX86, "Microsoft Visual Studio", "Installer", "vswhere.exe")
	if found, err := exec.LookPath("vswhere.exe"); err == nil {
		vswhere = found
	}
	if _, err := os.Stat(vswhere); err == nil {
		out, err := exec.Command(vswhere, "-latest", "-products", "*", "-requires", "Microsoft.VisualStudio.Component.VC.Tools.x86.x64", "-property", "installationPath").Output()
		if err == nil {
			root := strings.TrimSpace(string(out))
			for _, rel := range []string{filepath.Join("VC", "Auxiliary", "Build", "vcvars64.bat"), filepath.Join("Common7", "Tools", "VsDevCmd.bat")} {
				candidate := filepath.Join(root, rel)
				if _, err := os.Stat(candidate); err == nil {
					return candidate
				}
			}
		}
	}
	return ""
}

// Some portable/development Visual Studio installations expose the VC paths
// through vcvars64 but omit the separately installed Windows SDK paths. Add
// the newest complete SDK generically; no SDK version is hard-coded.
func augmentMSVCWindowsSDK(env []string) []string {
	programFilesX86 := os.Getenv("ProgramFiles(x86)")
	if programFilesX86 == "" {
		programFilesX86 = `C:\Program Files (x86)`
	}
	root := filepath.Join(programFilesX86, "Windows Kits", "10")
	entries, err := os.ReadDir(filepath.Join(root, "Lib"))
	if err != nil {
		return env
	}
	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			versions = append(versions, entry.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(versions)))
	for _, version := range versions {
		umLib := filepath.Join(root, "Lib", version, "um", "x64")
		ucrtLib := filepath.Join(root, "Lib", version, "ucrt", "x64")
		if _, err := os.Stat(filepath.Join(umLib, "kernel32.lib")); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(ucrtLib, "ucrt.lib")); err != nil {
			continue
		}
		includeRoot := filepath.Join(root, "Include", version)
		env = appendEnvironmentPath(env, "LIB", ucrtLib, umLib)
		env = appendEnvironmentPath(env, "INCLUDE",
			filepath.Join(includeRoot, "ucrt"), filepath.Join(includeRoot, "shared"),
			filepath.Join(includeRoot, "um"), filepath.Join(includeRoot, "winrt"),
			filepath.Join(includeRoot, "cppwinrt"))
		return env
	}
	return env
}

func appendEnvironmentPath(env []string, key string, values ...string) []string {
	for i, entry := range env {
		if split := strings.IndexByte(entry, '='); split > 0 && strings.EqualFold(entry[:split], key) {
			current := entry[split+1:]
			parts := append([]string{}, values...)
			if current != "" {
				parts = append(parts, current)
			}
			env[i] = key + "=" + strings.Join(parts, string(os.PathListSeparator))
			return env
		}
	}
	return append(env, key+"="+strings.Join(values, string(os.PathListSeparator)))
}

func environmentFromBatch(setup string) ([]string, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("MSVC is available only on Windows")
	}
	script, err := os.CreateTemp("", "code-transpiler-msvc-env-*.cmd")
	if err != nil {
		return nil, fmt.Errorf("initialize MSVC environment script: %w", err)
	}
	scriptPath := script.Name()
	defer os.Remove(scriptPath)
	_, writeErr := fmt.Fprintf(script, "@call \"%s\" >nul\r\n@if errorlevel 1 exit /b %%errorlevel%%\r\n@set\r\n", setup)
	closeErr := script.Close()
	if writeErr != nil {
		return nil, fmt.Errorf("initialize MSVC environment script: %w", writeErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("initialize MSVC environment script: %w", closeErr)
	}
	out, err := exec.Command("cmd.exe", "/d", "/c", scriptPath).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("initialize MSVC environment: %w: %s", err, strings.TrimSpace(string(out)))
	}
	values := map[string]string{}
	for _, entry := range os.Environ() {
		if i := strings.IndexByte(entry, '='); i > 0 {
			values[strings.ToUpper(entry[:i])] = entry[i+1:]
		}
	}
	for _, line := range strings.Split(strings.ReplaceAll(string(out), "\r\n", "\n"), "\n") {
		if i := strings.IndexByte(line, '='); i > 0 {
			values[strings.ToUpper(line[:i])] = line[i+1:]
		}
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, key := range keys {
		env = append(env, key+"="+values[key])
	}
	return env, nil
}

func lookPathInEnvironment(name string, env []string) string {
	pathValue := ""
	for _, entry := range env {
		if i := strings.IndexByte(entry, '='); i > 0 && strings.EqualFold(entry[:i], "PATH") {
			pathValue = entry[i+1:]
			break
		}
	}
	for _, dir := range filepath.SplitList(pathValue) {
		candidate := filepath.Join(dir, name)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func sha256String(value string) string { return sha256Bytes([]byte(value)) }
func sha256Bytes(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
