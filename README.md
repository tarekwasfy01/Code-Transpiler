# Code Transpiler

<p align="center">
  <img width="256" height="256" alt="code-transpiler-logo" src="https://github.com/user-attachments/assets/88fbf224-6e52-426e-a124-5482df814b75" />
</p>

<p align="center">
  <a href="https://pkg.go.dev/github.com/tarekwasfy01/Code-Transpiler">
    <img src="https://pkg.go.dev/badge/github.com/tarekwasfy01/Code-Transpiler.svg" alt="Go Reference" />
  </a>
  &nbsp;
  <a href="https://get.microsoft.com/installer/download/9n1kb1kxxtmn?referrer=appbadge">
    <img src="https://get.microsoft.com/images/en-us%20dark.svg" width="200" alt="Get it from Microsoft" />
  </a>
</p>

## Semantic Programming Language

The **Semantic Programming Language (`.sp`) originated from Code Transpiler** and its language-independent `SemanticProgram / Universal AST`.

Code Transpiler remains the associated **Go package, reference implementation and bootstrap compiler**.

Semantic Programming Language:

https://github.com/tarekwasfy01/Semantic-Programming-Language

---

## About

Code Transpiler is a matrix-driven many-to-many compiler and transpiler for 13 programming languages.

It provides:

* Go package
* Windows application
* command-line interface
* SemanticProgram / UAST
* source-to-source transpilation
* native x86-64 compilation
* assembly, machine-code, object and executable processing
* binary-to-Semantic lifting

```text
Go module:  github.com/tarekwasfy01/Code-Transpiler
Go package: codetranspiler
Executable: CodeTranspiler.exe
```

---

### Run the API example directly

The list-languages example is its own ready-to-run Go module. No manual
`go mod init` or `go get` step is required:

```text
cd examples/list-languages
go run .
```

On Windows, `examples\list-languages\run.cmd` performs dependency tidying and
runs the example automatically. The importable library supports pure-Go builds
with `CGO_ENABLED=0`; the optional external grammar scanner reports a capability
error only when a grammar that actually requires it is used.

The compiler lowers supported constructs into `SemanticProgram`, whose
canonical state is the matrix-derived Universal AST. The old recursive
statement/expression tree is retained only as a generated compatibility view.
The semantic runtime, R writer, signature checks, call resolution, typed
operation checks, generic emitters and function-flow analysis now consume
universal nodes directly. No legacy function or call tree is reconstructed.
Canonical JSON contains universal nodes, semantic facets, typed fields,
language projection, source positions and graph relations. A backend rejects
UAST semantics that its direct or compatibility lowering cannot preserve.

| ID       | Language | Extensions                    |
| -------- | -------- | ----------------------------- |
| `r`      | R        | `.R`, `.r`                    |
| `go`     | Go       | `.go`                         |
| `rust`   | Rust     | `.rs`                         |
| `cpp`    | C++      | `.cpp`, `.cc`, `.cxx`, `.hpp` |
| `c`      | C        | `.c`, `.h`                    |
| `python` | Python   | `.py`                         |
| `zig`    | Zig      | `.zig`                        |
| `julia`  | Julia    | `.jl`                         |
| `nim`    | Nim      | `.nim`                        |
| `csharp` | C#       | `.cs`                         |
| `java`   | Java     | `.java`                       |
| `kotlin` | Kotlin   | `.kt`                         |
| `swift`  | Swift    | `.swift`                      |

The registry exposes **156 directed source-to-target routes**.

---

# Install the Go Package

```bash
go get github.com/tarekwasfy01/Code-Transpiler
```

Import:

```go
import codetranspiler "github.com/tarekwasfy01/Code-Transpiler"
```

Example:

```go
package main

import (
    "fmt"
    "log"

    codetranspiler "github.com/tarekwasfy01/Code-Transpiler"
)

func main() {
    code, err := codetranspiler.Transpile(
        "python",
        "go",
        "print(2 + 3)",
    )
    if err != nil {
        log.Fatal(err)
    }

    fmt.Print(code)
}
```

---

# Main Go API

### Transpile source

```go
func Transpile(source, target, code string) (string, error)
```

### Export SemanticProgram JSON

```go
func SemanticJSON(source, code string) ([]byte, error)
```

### Export Semantic `.sp`

```go
func SemanticSP(source, code string) ([]byte, error)
```

### Load `.sp`

```go
func ParseSemanticSP(data []byte) (*SemanticProgram, error)
```

### Write `.sp`

```go
func MarshalSemanticSP(program *SemanticProgram) ([]byte, error)
```

### Transpile Semantic JSON

```go
func TranspileSemanticJSON(target string, data []byte) (string, error)
```

### Transpile Semantic `.sp`

```go
func TranspileSemanticSP(target string, data []byte) (string, error)
```

### Compile

```go
func Compile(source string, options CompileOptions) (CompileResult, error)
```

### Languages

```go
func Languages() []Language
func LanguagesJSON() ([]byte, error)
```

### Capabilities

```go
func BackendCapability(feature, target string) Capability
func CapabilityMatrixJSON(features []string) ([]byte, error)
func ImplementationMatrixJSON() ([]byte, error)
```

### Native Go analysis

```go
func NativeAnalysisJSON(source, filename, code string) ([]byte, error)
func NativeSemanticJSON(source, filename, code string) ([]byte, error)
```

### Binary / executable lifting

```go
func BinarySemanticJSON(data []byte, options CompileOptions) ([]byte, error)
func DecompileSemanticJSON(data []byte, options CompileOptions) ([]byte, error)
```

---

# Command-Line Interface

All commands use:

```powershell
CodeTranspiler.exe <command>
```

The compact alias is also available:

```powershell
CodeTranspiler.exe sp <command>
```

---

## GUI

Start the graphical application:

```powershell
CodeTranspiler.exe
```

or:

```powershell
CodeTranspiler.exe gui
```

---

## Help

```powershell
CodeTranspiler.exe help
CodeTranspiler.exe --help
CodeTranspiler.exe -h
```

---

## Version

```powershell
CodeTranspiler.exe version
CodeTranspiler.exe --version
```

Displays version, commit, build date and compiler engine information.

---

## Licenses

```powershell
CodeTranspiler.exe licenses
```

Also accepted:

```powershell
CodeTranspiler.exe licences
CodeTranspiler.exe --licenses
CodeTranspiler.exe --licences
```

Displays bundled third-party license information.

---

## Languages

```powershell
CodeTranspiler.exe languages
```

or:

```powershell
CodeTranspiler.exe targets
```

Lists registered language IDs, names and extensions.

---

## Routes

```powershell
CodeTranspiler.exe routes
```

Lists all directed source-to-target language routes.

---

## Runtime Bundles

```powershell
CodeTranspiler.exe runtimes
```

Lists embedded target-runtime source bundles.

---

# Transpile Source Code

```powershell
CodeTranspiler.exe transpile `
  -source python `
  -target go `
  input.py `
  -o output.go
```

Aliases:

```text
-source = -from
-target = -to
```

Examples:

```powershell
CodeTranspiler.exe transpile -source c -target go input.c -o output.go
CodeTranspiler.exe transpile -source go -target rust input.go -o output.rs
CodeTranspiler.exe transpile -source python -target cpp input.py -o output.cpp
CodeTranspiler.exe transpile -source r -target julia input.R -o output.jl
```

Automatic source detection:

```powershell
CodeTranspiler.exe transpile input.py -target rust -o output.rs
```

Generate all registered targets:

```powershell
CodeTranspiler.exe transpile input.py -target all -o translated
```

Strict native frontend where supported:

```powershell
CodeTranspiler.exe transpile -native -source go -target rust input.go -o output.rs
```

---

# Batch Transpilation

```powershell
CodeTranspiler.exe transpile-batch
```

Reads a JSON array from standard input:

```json
[
  {
    "id": "python-go",
    "source": "python",
    "target": "go",
    "code": "print(2 + 3)"
  },
  {
    "id": "c-rust",
    "source": "c",
    "target": "rust",
    "code": "int main(void) { return 0; }"
  }
]
```

Example PowerShell pipeline:

```powershell
Get-Content requests.json |
  .\CodeTranspiler.exe transpile-batch |
  Set-Content responses.json
```

---

# Run Source Code

Embedded execution:

```powershell
CodeTranspiler.exe run -source python -target embedded input.py
```

Run through a target:

```powershell
CodeTranspiler.exe run -source c -target go input.c
```

Aliases:

```powershell
CodeTranspiler.exe run -from python -to embedded input.py
```

---

# SemanticProgram Export

Export JSON:

```powershell
CodeTranspiler.exe semantic-export `
  -source go `
  input.go `
  -o program.semantic.json
```

Export Semantic `.sp`:

```powershell
CodeTranspiler.exe semantic-export `
  -source go `
  -format sp `
  input.go `
  -o program.sp
```

Export compressed `.spz`:

```powershell
CodeTranspiler.exe semantic-export `
  -source go `
  -format spz `
  input.go `
  -o program.spz
```

Strict native Go frontend:

```powershell
CodeTranspiler.exe semantic-export `
  -native `
  -source go `
  input.go `
  -o program.semantic.json
```

---

# Semantic Transpilation

From JSON:

```powershell
CodeTranspiler.exe semantic-transpile `
  -target rust `
  program.semantic.json `
  -o output.rs
```

From `.sp`:

```powershell
CodeTranspiler.exe semantic-transpile `
  -target rust `
  program.sp `
  -o output.rs
```

From `.spz`:

```powershell
CodeTranspiler.exe semantic-transpile `
  -target rust `
  program.spz `
  -o output.rs
```

---

# Semantic Format Conversion

JSON to `.sp`:

```powershell
CodeTranspiler.exe semantic-convert `
  program.semantic.json `
  -o program.sp
```

`.sp` to JSON:

```powershell
CodeTranspiler.exe semantic-convert `
  program.sp `
  -o program.semantic.json
```

`.sp` to `.spz`:

```powershell
CodeTranspiler.exe semantic-convert `
  program.sp `
  -o program.spz
```

`.spz` to `.sp`:

```powershell
CodeTranspiler.exe semantic-convert `
  program.spz `
  -o program.sp
```

---

# Format Semantic `.sp`

```powershell
CodeTranspiler.exe semantic-format program.sp
```

Write formatted output:

```powershell
CodeTranspiler.exe semantic-format `
  program.sp `
  -o formatted.sp
```

---

# Validate SemanticProgram

Validate `.sp`:

```powershell
CodeTranspiler.exe semantic-validate program.sp
```

Validate `.spz`:

```powershell
CodeTranspiler.exe semantic-validate program.spz
```

Validate JSON:

```powershell
CodeTranspiler.exe semantic-validate program.semantic.json
```

Successful validation prints:

```text
VALID
```

---

# Semantic Information

```powershell
CodeTranspiler.exe semantic-info program.sp
```

Also accepts JSON and `.spz`.

Returns information such as:

* schema version
* source language
* evaluation model
* UAST presence
* semantic node count

---

# Native Compilation

Compile Go directly to a Windows x86-64 executable:

```powershell
CodeTranspiler.exe compile `
  -source go `
  -target native-x86_64-windows `
  input.go `
  -o program.exe
```

Compile Semantic:

```powershell
CodeTranspiler.exe compile `
  -source sp `
  input.sp `
  -o program.exe
```

Available native output targets:

```text
native-x86_64-windows
object-x86_64-windows
machine-x86_64
asm-x86_64
```

---

## Machine Code

```powershell
CodeTranspiler.exe compile `
  -source go `
  -target machine-x86_64 `
  input.go `
  -o program.bin
```

Hexadecimal output:

```powershell
CodeTranspiler.exe compile `
  -source go `
  -target machine-x86_64 `
  --hex `
  input.go
```

---

## Assembly

```powershell
CodeTranspiler.exe compile `
  -source go `
  -target asm-x86_64 `
  input.go `
  -o program.asm
```

---

## Object File

```powershell
CodeTranspiler.exe compile `
  -source go `
  -target object-x86_64-windows `
  input.go `
  -o program.obj
```

---

## Compile Options

Common options:

```text
-source <language>
-target <target>
-input <kind>
-output <kind>
-arch <architecture>
-os <operating-system>
-abi <ABI>
-entry <function>
-o <file>
--hex
--via-assembly
```

Input kinds:

```text
source
assembly
machine
object
executable
```

Output kinds:

```text
source
assembly
machine
object
executable
```

Default native platform:

```text
Architecture: x86_64
OS:           windows
ABI:          win64
```

`--via-assembly` explicitly uses an assembly route. Direct native compilation normally uses the internal encoder.

---

# Machine IR

Decode supported low-level input into Machine IR:

```powershell
CodeTranspiler.exe machine-ir `
  -input machine `
  program.bin `
  -o machine-ir.json
```

Supported input kinds:

```text
assembly
machine
object
executable
```

Examples:

```powershell
CodeTranspiler.exe machine-ir -input assembly program.asm -o machine-ir.json
CodeTranspiler.exe machine-ir -input object program.obj -o machine-ir.json
CodeTranspiler.exe machine-ir -input executable program.exe -o machine-ir.json
```

---

# Decompile / Semantic Lift

Assembly to SemanticProgram:

```powershell
CodeTranspiler.exe decompile `
  -input assembly `
  program.asm `
  -o program.semantic.json
```

Machine code:

```powershell
CodeTranspiler.exe decompile `
  -input machine `
  program.bin `
  -o program.semantic.json
```

Object file:

```powershell
CodeTranspiler.exe decompile `
  -input object `
  program.obj `
  -o program.semantic.json
```

Executable:

```powershell
CodeTranspiler.exe decompile `
  -input executable `
  program.exe `
  -o program.semantic.json
```

---

# Native Go Analysis

```powershell
CodeTranspiler.exe native-analysis `
  -source go `
  input.go `
  -o analysis.json
```

Extracts native type, symbol and source information without producing the regular source-language translation output.

---

# Capability Query

```powershell
CodeTranspiler.exe capability go core
```

General form:

```powershell
CodeTranspiler.exe capability <target> <feature>
```

Returns JSON describing the backend capability.

---

# Capability Matrix

```powershell
CodeTranspiler.exe capability-matrix
```

Additional features may be supplied:

```powershell
CodeTranspiler.exe capability-matrix feature1 feature2
```

Returns the feature-by-target capability matrix.

---

# Implementation Matrix

```powershell
CodeTranspiler.exe implementation-matrix
```

Displays typed operation implementation information across the compiler.

---

# Add Code Transpiler to PATH

Windows:

```powershell
CodeTranspiler.exe setpath
```

Adds the installation directory to the machine PATH using an elevated PowerShell process.

---

# Build from Source

Requirements:

* Go
* Windows x64
* PowerShell

Build:

```powershell
powershell -ExecutionPolicy Bypass -File .\build-onefile.ps1
```

Output:

```text
dist\CodeTranspiler.exe
```

Run tests:

```powershell
go test ./...
```

Build directly with Go:

```powershell
go build ./...
```

---

# License

Code Transpiler is licensed under the **MIT License**.

See:

```text
LICENSE
THIRD_PARTY_NOTICES.md
THIRD_PARTY_NOTICES.txt
```

---

# Repositories

**Code Transpiler**

https://github.com/tarekwasfy01/Code-Transpiler

**Semantic Programming Language**

https://github.com/tarekwasfy01/Semantic-Programming-Language
