# Code-Transpiler

[![Go Reference](https://pkg.go.dev/badge/github.com/tarekwasfy01/Code-Transpiler.svg)](https://pkg.go.dev/github.com/tarekwasfy01/Code-Transpiler)

Code-Transpiler is a matrix-driven many-to-many compiler for 13 programming
languages. It is available as a Windows application, a command-line program
and an importable Go package.

```text
Go module:   github.com/tarekwasfy01/Code-Transpiler
Go package:  codetranspiler
Executable:  CodeTranspiler.exe
```

Go package names cannot contain `-`, so the module and repository are named
`Code-Transpiler`, while the identifier used in Go source is
`codetranspiler`.

The compiler lowers supported constructs into `SemanticProgram`, a versioned
JSON representation containing the executable tree, structured types,
effects, bindings, scopes and sparse semantic relation matrices.

> Route availability means that the supported common subset can be parsed and
> emitted. It does not claim complete equivalence for every feature of every
> source and target language.

## Supported languages

| ID | Language | Extensions |
|---|---|---|
| `r` | R | `.R`, `.r` |
| `go` | Go | `.go` |
| `rust` | Rust | `.rs` |
| `cpp` | C++ | `.cpp`, `.cc`, `.cxx`, `.hpp` |
| `c` | C | `.c`, `.h` |
| `python` | Python | `.py` |
| `zig` | Zig | `.zig` |
| `julia` | Julia | `.jl` |
| `nim` | Nim | `.nim` |
| `csharp` | C# | `.cs` |
| `java` | Java | `.java` |
| `kotlin` | Kotlin | `.kt` |
| `swift` | Swift | `.swift` |

Aliases include `py`, `rs`, `c++` and `c#`. The registry therefore exposes
156 directed source-to-target routes (`13 × 12`).

## Install the Go package

```bash
go get github.com/tarekwasfy01/Code-Transpiler
```

Import it:

```go
import "github.com/tarekwasfy01/Code-Transpiler"
```

Go automatically binds that import to the declared package name
`codetranspiler`. An explicit alias is also possible:

```go
import transpiler "github.com/tarekwasfy01/Code-Transpiler"
```

## Complete Go API

### Translate source code

```go
func Transpile(source, target, code string) (string, error)
```

```go
package main

import (
	"fmt"
	"log"

	codetranspiler "github.com/tarekwasfy01/Code-Transpiler"
)

func main() {
	source := "x = 2\nprint(x + 3)\n"
	generated, err := codetranspiler.Transpile("python", "go", source)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(generated)
}
```

If source and target are identical, the input is returned unchanged.
Unsupported input produces an error instead of a silent semantic fallback.

### Export SemanticProgram JSON

```go
func SemanticJSON(source, code string) ([]byte, error)
```

```go
document, err := codetranspiler.SemanticJSON("c", cSource)
if err != nil {
	log.Fatal(err)
}
err = os.WriteFile("program.semantic.json", document, 0o644)
```

The JSON document stores the executable program and its verified semantic
relations. Original source code is not required for later emission.

### Translate SemanticProgram JSON

```go
func TranspileSemanticJSON(target string, data []byte) (string, error)
```

```go
document, err := os.ReadFile("program.semantic.json")
if err != nil {
	log.Fatal(err)
}

rustCode, err := codetranspiler.TranspileSemanticJSON("rust", document)
if err != nil {
	log.Fatal(err)
}
```

The importer validates the schema version, AST structure, semantic contracts
and relation matrices.

### List languages

```go
func Languages() []Language
```

```go
type Language struct {
	ID         string
	Aliases    []string
	Extensions []string
}
```

```go
for _, language := range codetranspiler.Languages() {
	fmt.Printf("%s %v\n", language.ID, language.Extensions)
}
```

### Language registry as JSON

```go
func LanguagesJSON() ([]byte, error)
```

This is intended for editors, GUIs and integrations that should consume the
compiler registry instead of maintaining their own language list.

### Query backend capabilities

```go
func BackendCapability(feature, target string) Capability
```

```go
type Capability struct {
	Feature string
	Backend string
	Status  string
	Reason  string
}
```

```go
capability := codetranspiler.BackendCapability("core", "go")
fmt.Println(capability.Status, capability.Reason)
```

Possible status values:

| Status | Meaning |
|---|---|
| `native` | Direct backend representation |
| `lowering` | Converted to an equivalent lower-level construction |
| `emulated` | Implemented through generated runtime support |
| `unsupported` | Must be rejected by the backend |

## CLI command reference

### GUI

```powershell
CodeTranspiler.exe
CodeTranspiler.exe gui
```

### Help and version

```powershell
CodeTranspiler.exe help
CodeTranspiler.exe --help
CodeTranspiler.exe -h
CodeTranspiler.exe version
CodeTranspiler.exe --version
```

### Languages and targets

```powershell
CodeTranspiler.exe languages
CodeTranspiler.exe targets
```

Both commands list all 13 registered language IDs.

### All routes

```powershell
CodeTranspiler.exe routes
```

Prints all 156 directed `source target` pairs as tab-separated rows.

### Embedded runtime sources

```powershell
CodeTranspiler.exe runtimes
```

Lists generated target-runtime source bundles. Native compilers and
interpreters are not bundled.

### Translate one file

```powershell
CodeTranspiler.exe transpile `
  -source <source-id> `
  -target <target-id> `
  input-file `
  -o output-file
```

Examples:

```powershell
CodeTranspiler.exe transpile -source c -target go input.c -o output.go
CodeTranspiler.exe transpile -source go -target rust input.go -o output.rs
CodeTranspiler.exe transpile -source python -target cpp input.py -o output.cpp
CodeTranspiler.exe transpile -source r -target julia input.R -o output.jl
```

Defaults are `-source r` and `-target go`. If `-o` is omitted, the output is
written beside the input using the target extension. Value flags may appear
before or after the input filename.

### Batch translation

```powershell
CodeTranspiler.exe transpile-batch
```

The command reads a JSON request array from standard input and writes a JSON
response array to standard output. No code is executed.

```json
[
  {
    "id": "c-to-go",
    "source": "c",
    "target": "go",
    "code": "int main(void) { return 0; }"
  },
  {
    "id": "python-to-rust",
    "source": "python",
    "target": "rust",
    "code": "print(2 + 3)"
  }
]
```

Each response contains the same `id` and either `code` or `error`.

```powershell
Get-Content requests.json |
  .\CodeTranspiler.exe transpile-batch |
  Set-Content responses.json
```

### Export SemanticProgram

```powershell
CodeTranspiler.exe semantic-export `
  -source python `
  input.py `
  -o program.semantic.json
```

### Translate SemanticProgram

```powershell
CodeTranspiler.exe semantic-transpile `
  -target rust `
  program.semantic.json `
  -o output.rs
```

If `-o` is omitted, generated source is written to standard output.

### Query a capability

```powershell
CodeTranspiler.exe capability <target> <feature>
CodeTranspiler.exe capability go core
```

Example response:

```json
{
  "feature": "core",
  "backend": "go",
  "status": "lowering",
  "reason": "shared semantic core lowering"
}
```

### Execute R or a generated target

```powershell
CodeTranspiler.exe run input.R
CodeTranspiler.exe run -target go input.R
CodeTranspiler.exe run -target rust input.R
CodeTranspiler.exe run -target python input.R
```

`run` currently accepts R source. Without `-target`, it uses the embedded
compatibility runtime. With `-target`, it transpiles R and invokes an installed
target toolchain.

| Target | Required external command |
|---|---|
| Go | `go` |
| Rust | `rustc` |
| C++ | `g++` or `clang++` |
| C | `gcc` or `clang` |
| Python | `python`, `python3` or `py` |
| Zig | `zig` |
| Julia | `julia` |
| Nim | `nim` |
| C# | `csc` or `dotnet` |
| Java | `javac` and `java` |
| Kotlin | `kotlinc` and `java` |
| Swift | `swift` or `swiftc` |

## SemanticProgram contents

The versioned document includes:

- executable statements and expressions
- stable node, scope and binding IDs
- structured recursive types and exact textual literals
- operation, dispatch, evaluation and indexing semantics
- named, default and missing arguments
- effects and conservative purity information
- syntax, control, data, binding, scope and evaluation-order matrices
- contracts, metadata, extensions and capability-gated dialects

Sparse relations use COO encoding:

```json
{
  "rows": 20,
  "cols": 6,
  "storage": "coo",
  "entries": [[0,1,1], [4,3,1]]
}
```

See [SEMANTIC_PROGRAM.md](SEMANTIC_PROGRAM.md) for the complete current format
and its semantic boundaries.

## Build from source

Requirements:

- Windows x64
- Go 1.26 or newer
- PowerShell

```powershell
powershell -ExecutionPolicy Bypass -File .\build-onefile.ps1
```

Output:

```text
dist\CodeTranspiler.exe
```

The build script runs all source-package tests, generates the Windows icon
resource, builds a trimmed x64 executable and validates its PE header,
architecture and GUI subsystem.

Run tests without building:

```powershell
go test ./...
```

## Validation status

The current v1 release has been checked for:

- 156/156 directed routes producing target output for small common-subset
  smoke programs
- SemanticProgram JSON export/import and target generation
- direct SemanticProgram execution and observation comparison
- import from a separate Go consumer module
- tests from the minimal GitHub folder
- GitHub Actions `go test ./...`

Routing coverage and full semantic equivalence are reported separately.
Complex language-specific constructs may require dialect lowering, runtime
emulation or an explicit unsupported result.

## License

Code-Transpiler is licensed under the MIT License. See [LICENSE](LICENSE).
Third-party information is recorded in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)
and [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt).

CrossTL source is not bundled. Its possible future role as an external GPU
adapter is described in [CROSSTL_DESIGN.md](CROSSTL_DESIGN.md).

## Repository

https://github.com/tarekwasfy01/Code-Transpiler
