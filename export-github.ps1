$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot

$exportRoot = Join-Path $PSScriptRoot 'github-release\Code-Transpiler'
$resolvedWorkspace = (Resolve-Path -LiteralPath $PSScriptRoot).Path
$exportParent = Split-Path -Parent $exportRoot
New-Item -ItemType Directory -Path $exportParent -Force | Out-Null
if (Test-Path -LiteralPath $exportRoot) {
    $resolvedExport = (Resolve-Path -LiteralPath $exportRoot).Path
    if (-not $resolvedExport.StartsWith($resolvedWorkspace + [IO.Path]::DirectorySeparatorChar)) {
        throw 'Refusing to replace export outside workspace'
    }
    Remove-Item -LiteralPath $resolvedExport -Recurse -Force
}
New-Item -ItemType Directory -Path $exportRoot | Out-Null

$files = @(
    '.gitignore', 'LICENSE', 'README.md', 'SEMANTIC_PROGRAM.md',
    'CROSSTL_DESIGN.md', 'SEMANTIC_FRONTEND_V2.md', 'SEMANTIC_DEVELOPMENT.md',
    'FRONTEND_UAST_CONTRACT.md', 'IMPLEMENTATION_MATRIX.md', 'UAST_MIGRATION_STATUS.md',
    'THIRD_PARTY_NOTICES.md', 'THIRD_PARTY_NOTICES.txt',
    'go.mod', 'go.sum', 'code_transpiler.go', 'code_transpiler_test.go',
    'build-onefile.ps1', 'build-code-transpiler.bat', 'build-universal-code-transpiler.bat',
    'export-github.ps1'
)
foreach ($file in $files) { Copy-Item -LiteralPath $file -Destination (Join-Path $exportRoot $file) }
$architecture = Get-ChildItem -LiteralPath $PSScriptRoot -File | Where-Object { $_.Name -like '*Canonical Architecture.md' } | Select-Object -First 1
if ($architecture) { Copy-Item -LiteralPath $architecture.FullName -Destination (Join-Path $exportRoot $architecture.Name) }
# These directories are part of the productive, self-contained frontend/UAST
# distribution.  In particular `matrices` is required at runtime by the CLI;
# omitting it made the published package fall back to an incomplete frontend.
foreach ($dir in @('.github', 'cmd', 'internal', 'assets', 'matrices', 'cpp_runtime', 'licenses', 'scripts', 'tools', 'docs', 'tests')) {
    Copy-Item -LiteralPath $dir -Destination (Join-Path $exportRoot $dir) -Recurse
}
# Never ship local build caches, downloaded JS dependencies, or helper binaries
# from the workspace.  They are not part of the Go package and made earlier
# exports appear to contain a different/older frontend implementation.
Get-ChildItem -LiteralPath $exportRoot -Recurse -Directory |
    Where-Object { $_.Name -in @('node_modules', '.gocache', '.gocache2', '.gocache3') } |
    Sort-Object FullName -Descending |
    ForEach-Object { Remove-Item -LiteralPath $_.FullName -Recurse -Force }
Get-ChildItem -LiteralPath (Join-Path $exportRoot 'tools') -Filter '*.exe' -File -ErrorAction SilentlyContinue |
    Remove-Item -Force

$dist = Join-Path $exportRoot 'dist'
New-Item -ItemType Directory -Path $dist | Out-Null
Copy-Item -LiteralPath 'dist\CodeTranspiler.exe' -Destination (Join-Path $dist 'CodeTranspiler.exe')

Get-ChildItem -LiteralPath $exportRoot -Recurse -File |
    ForEach-Object { $_.FullName.Substring($exportRoot.Length + 1).Replace('\', '/') } |
    Sort-Object | Set-Content -LiteralPath (Join-Path $exportRoot 'MANIFEST.txt') -Encoding utf8

Write-Host ('GitHub folder: ' + $exportRoot)
