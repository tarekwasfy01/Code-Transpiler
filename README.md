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

See [Universal AST migration status](docs/UAST_MIGRATION_STATUS.md) for measured
direct coverage, target capability matrices and the remaining compatibility
adapters.

> Route availability means that the supported common subset can be parsed and
> emitted. It does not claim complete equivalence for every feature of every
> source and target language.

## Semantic formats

Semantic is the language-independent program representation. `.se` is the
readable Semantic language, `.sp` is compact Semantic transport text, and
`.spz` is its compressed transport form. All three are lossless views of the
same `SemanticProgram`; JSON remains an explicit interchange/debug format.

SFPC (Semantic Fixed Point Compression) stores only an irreducible explicit
basis and derives repeated facts by closure. For a program graph `G`:

```text
F(G) = G_explicit ∪ derive(F(G))
closure(B) = G
```

where `B` is the smallest explicit basis. Grammar compression encodes a
repeated production `A → X₁…Xₙ` once and uses references, reducing repeated
cost from `k·Σ|Xᵢ|` to `Σ|Xᵢ| + k·|ref(A)|`. `.spz` compresses that canonical
stream; decoding satisfies `Decode(Encode(P)) ≡ P`.

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
go get github.com/tarekwasfy01/Code-Transpiler@v1.2.9
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

Defaults are `-source auto` (file extension) and `-target go`. If `-o` is omitted, the output is
written beside the input using the target extension. Value flags may appear
before or after the input filename.

### Batch translation

For one input and all registered targets, use:

```powershell
go run ./cmd/r2many transpile input.py -target all -o translated
go run ./cmd/r2many transpile -from c -to rust input.c -o output.rs
```

`-from`/`-to` alias `-source`/`-target`. The all-target mode parses once and
emits 13 outputs including a copy for the identity route. It writes
`translation-report.json` with per-target paths or errors and returns nonzero
if any target fails. Source overwrites are refused; implicit identity output
uses `.transpiled` in the filename. Unknown extensions require `-source`.
`-native` opts into the strict native frontend; unsupported languages/features
are rejected without fallback. Default mode retains the existing common-subset
frontends for all 13 languages, not complete native-language equivalence.

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
sp semantic-export `
  -source python `
  input.py `
  -o program.semantic.json
```

Use the same command for the three Semantic formats:

```powershell
sp semantic-export -source go input.go -format se  -o program.se
sp semantic-export -source go input.go -format sp  -o program.sp
sp semantic-export -source go input.go -format spz -o program.spz
```

### Translate SemanticProgram

```powershell
sp semantic-transpile `
  -target rust `
  program.se `
  -o output.rs
```

If `-o` is omitted, generated source is written to standard output.

Convert and format Semantic documents without changing their meaning:

```powershell
sp semantic-convert program.semantic.json -o program.se
sp semantic-format program.se --readable -o readable.se
sp semantic-format program.se --compact -o compact.se
sp semantic-format program.se -o program.sp
sp semantic-format program.se -o program.spz
sp semantic-validate program.spz
sp semantic-info program.se
```

Every command shown here also accepts the executable form
`CodeTranspiler.exe <command> ...`; `sp <command> ...` and
`CodeTranspiler.exe <command> ...` are equivalent.

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

See [docs/SEMANTIC_PROGRAM.md](docs/SEMANTIC_PROGRAM.md) for the complete current format
and its semantic boundaries.

## Build from source

Requirements:

- Windows x64
- Go 1.26 or newer
- PowerShell

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-onefile.ps1
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
Third-party information is recorded in [docs/THIRD_PARTY_NOTICES.md](docs/THIRD_PARTY_NOTICES.md)
and [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt).

CrossTL source is not bundled. Its possible future role as an external GPU
adapter is described in [docs/CROSSTL_DESIGN.md](docs/CROSSTL_DESIGN.md).

## Repository

Local development: run `./run-matrix-workbench.ps1` in PowerShell to calculate
current implementation gaps, analyze type probes and run project tests. Each run
writes a fresh report under `outputs/matrix-workbench/`. See
[Matrix workbench](tools/matrix-audit/WORKBENCH.md) for calculations, options and
the distinction between declared support and execution evidence.

For the combined Go/Python/R/Rust/C++/Kotlin/Java/C# matrix handoffs, use
`./run-all-handoffs.ps1`; see [Joint handoff workflow](tools/matrix-audit/ALL_HANDOFFS.md).

https://github.com/tarekwasfy01/Code-Transpiler

