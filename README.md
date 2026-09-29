# CodeTranspiler.Managed

A dependency-free managed .NET port of the Code Transpiler core.

The NuGet package does **not** ship Go, Rust, Julia, Python, SWC, LLVM, GCC, Roslyn packages, Node, or any foreign runtime/compiler. Those implementations were used only as bootstrap/reference engines while producing this managed source tree.

## Included

- 13 registered languages: R, Go, Python, Rust, C, C++, Zig, Julia, Nim, C#, Java, Kotlin, Swift.
- All 156 directed source/target route IDs.
- Managed lexer and expression parser.
- Managed semantic IR.
- Frontends for functions, variables, imports, common expressions/control flow and common class/struct/interface/enum shapes.
- Emitters for all 13 target languages.
- Fragment fallback and route racing for partially understood source.
- Semantic JSON export/import and semantic merging.
- Batch API.
- Directory/project translation.
- CLI in the same assembly.
- Local browser GUI implemented with `HttpListener` and embedded HTML/CSS/JS; no GUI framework package is required.
- Zero `<PackageReference>` dependencies.

## Library

```csharp
using CodeTranspiler.Managed;

var result = new CodeTranspiler().Transpile(
    "def add(a: int, b: int) -> int:\n    return a + b",
    "python",
    "csharp");

Console.WriteLine(result.Code);
```

## CLI

Because the package assembly is also executable, it can be run directly with `dotnet`:

```text
dotnet CodeTranspiler.Managed.dll languages
dotnet CodeTranspiler.Managed.dll routes
dotnet CodeTranspiler.Managed.dll transpile input.py --to rust -o output.rs
dotnet CodeTranspiler.Managed.dll project ./src --to csharp -o ./generated
dotnet CodeTranspiler.Managed.dll analyze input.go
dotnet CodeTranspiler.Managed.dll gui
```

`gui` starts a loopback-only local HTTP server and opens the browser UI. No source leaves the machine.

## NuGet packaging

CI does not transpile or generate package source. The checked-in C# tree is the package source. GitHub only invokes `dotnet pack`, then optionally exchanges its GitHub OIDC identity for a short-lived nuget.org API key and pushes the `.nupkg`.

For nuget.org Trusted Publishing create a policy for this repository and workflow file `publish-nuget.yml`. If the workflow uses the included `release` environment, set the same environment on the policy. Define the GitHub Actions variable `NUGET_USER` as the nuget.org profile username (not the email address).

## Bootstrap note

The repository may contain `bootstrap-evidence/` for provenance/debugging. It is not compiled into or packed with the library.

## Port status

See `PORT_STATUS.md` for the exact managed surface and bootstrap validation notes.
