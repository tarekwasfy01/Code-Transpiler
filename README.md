# Code Transpiler

<a href="https://pkg.go.dev/github.com/tarekwasfy01/Code-Transpiler"><img src="https://pkg.go.dev/badge/github.com/tarekwasfy01/Code-Transpiler.svg" alt="Go Reference"></a>

A matrix-driven many-to-many source-code transpiler with a serializable universal semantic representation.

Code Transpiler currently supports 13 languages:

- R
- Go
- Rust
- C++
- C
- Python
- Zig
- Julia
- Nim
- C#
- Java
- Kotlin
- Swift

This provides 156 directed source-to-target routes.

## Architecture

```text
Source Language
      │
      ▼
Frontend / Matrix Analysis
      │
      ▼
SemanticProgram
 ├─ Executable AST
 ├─ Structured Types
 ├─ Effects
 ├─ Bindings and Scopes
 ├─ Control Flow
 ├─ Data Flow
 ├─ Evaluation Order
 └─ Capability Contracts
      │
      ▼
Target Backend
      │
      ▼
Go / Rust / C / C++ / Python / …
```

`SemanticProgram` is a versioned JSON interchange format. It contains the executable program tree and does not require the original source code after serialization.

## Go Package

```bash
go get github.com/tarekwasfy01/Code-Transpiler
```

Example:

```go
package main

import (
	"fmt"
	"log"

	transpiler "github.com/tarekwasfy01/Code-Transpiler"
)

func main() {
	source := `
x = 2
print(x + 3)
`

	code, err := transpiler.Transpile("python", "go", source)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(code)
}
```

### SemanticProgram JSON

```go
semanticJSON, err := transpiler.SemanticJSON("python", source)
if err != nil {
	log.Fatal(err)
}

rustCode, err := transpiler.TranspileSemanticJSON("rust", semanticJSON)
if err != nil {
	log.Fatal(err)
}
```

### Language Registry

```go
for _, language := range transpiler.Languages() {
	fmt.Println(language.ID, language.Extensions)
}
```

### Backend Capabilities

```go
capability := transpiler.BackendCapability("core", "go")
fmt.Println(capability.Status, capability.Reason)
```

Capability states are:

- `native`
- `lowering`
- `emulated`
- `unsupported`

Unsupported semantics are reported explicitly rather than silently replaced.

## Command-Line Interface

List languages:

```powershell
CodeTranspiler.exe languages
```

List all 156 routes:

```powershell
CodeTranspiler.exe routes
```

Translate C to Go:

```powershell
CodeTranspiler.exe transpile `
  -source c `
  -target go `
  input.c `
  -o output.go
```

Translate Python to Rust:

```powershell
CodeTranspiler.exe transpile `
  -source python `
  -target rust `
  input.py `
  -o output.rs
```

Flags may appear before or after the input filename.

## SemanticProgram CLI

Export a source file as SemanticProgram JSON:

```powershell
CodeTranspiler.exe semantic-export `
  -source python `
  input.py `
  -o program.semantic.json
```

Generate Rust from the stored SemanticProgram:

```powershell
CodeTranspiler.exe semantic-transpile `
  -target rust `
  program.semantic.json `
  -o output.rs
```

The second operation does not require the original Python source.

## Batch Translation

`transpile-batch` reads a JSON array from standard input:

```json
[
  {
    "id": "example",
    "source": "c",
    "target": "go",
    "code": "int main() { return 0; }"
  }
]
```

Run:

```powershell
Get-Content requests.json |
  CodeTranspiler.exe transpile-batch |
  Set-Content responses.json
```

Response:

```json
[
  {
    "id": "example",
    "code": "package main\n..."
  }
]
```

Errors are returned per request without terminating the entire batch.

## Running Programs

Execute R with the embedded compatibility runtime:

```powershell
CodeTranspiler.exe run input.R
```

Translate and execute through an installed target toolchain:

```powershell
CodeTranspiler.exe run -target go input.R
CodeTranspiler.exe run -target rust input.R
CodeTranspiler.exe run -target python input.R
```

External compilers and interpreters are not bundled.

## Build

Requirements:

- Windows x64
- Go 1.26 or newer
- PowerShell

Build and test:

```powershell
powershell -ExecutionPolicy Bypass -File .\build-onefile.ps1
```

Output:

```text
dist\CodeTranspiler.exe
```

The build performs package tests, embeds the Windows icon and validates the generated PE executable.

## Current Validation

The current release has been checked for:

- 156/156 directed CLI routes producing target output
- complete SemanticProgram JSON roundtrip
- direct SemanticProgram execution
- JSON-to-target generation without original source
- external Go-module consumption
- GitHub Actions package tests

Route generation does not imply complete semantic compatibility with every feature of every language. The supported common subset is continuously expanded, and unresolved semantics remain explicit.

## Semantic Matrices

SemanticProgram stores sparse relations for:

- syntax
- control flow
- data flow
- bindings
- effects
- scopes
- evaluation order
- call modes

The effect system distinguishes operations including I/O, file access, memory access, exceptions, FFI, time, randomness, synchronization and unknown calls.

## GPU Dialect

SemanticProgram includes a capability-gated dialect mechanism for specialized domains.

A future GPU adapter can represent:

- compute kernels
- workgroups
- buffers
- textures
- samplers
- barriers
- atomics
- subgroup operations
- cooperative matrices

CPU backends reject unsupported GPU capabilities explicitly. CrossTL/CrossGL is being evaluated as an external GPU backend rather than bundled into the universal core.

## License

The project is licensed under the MIT License.

Third-party components and development-tool notices are documented in:

- `THIRD_PARTY_NOTICES.md`
- `THIRD_PARTY_NOTICES.txt`

## Repository

https://github.com/tarekwasfy01/Code-Transpiler
