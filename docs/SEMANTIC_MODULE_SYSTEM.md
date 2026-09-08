# Semantic Module System

The module resolver accepts source files, `.se`, `.spz`, assembly, PE/COFF and
native external artifacts through one `UniversalModuleResolver` boundary.
Source imports use the existing internal frontend and never start a compiler or
runtime. Semantic artifacts are parsed and verified directly.

Modules are stored atomically below `%LOCALAPPDATA%\Semantic\Modules` in a
content-addressed directory containing `module.spz`, `module.se`, and
`module.meta`. The cache key includes the source hash, frontend/schema/SFPC
versions, semantic root, dependencies, and contracts. A module is accepted only
when its stored semantic root and cache key match the canonical SPZ payload.

CLI examples:

```text
CodeTranspiler.exe semantic module import path\to\package
CodeTranspiler.exe semantic module import --language go path\to\main.go
CodeTranspiler.exe semantic module list
CodeTranspiler.exe semantic module verify <cache-key>
```

`sp semantic module ...` is an equivalent command alias.
