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
    'CROSSTL_DESIGN.md', 'SEMANTIC_FRONTEND_V2.md', 'SEMANTIC_DEVELOPMENT.md', 'THIRD_PARTY_NOTICES.md', 'THIRD_PARTY_NOTICES.txt',
    'go.mod', 'go.sum', 'code_transpiler.go', 'code_transpiler_test.go',
    'build-onefile.ps1', 'build-code-transpiler.bat'
)
foreach ($file in $files) { Copy-Item -LiteralPath $file -Destination (Join-Path $exportRoot $file) }
foreach ($dir in @('.github', 'cmd', 'internal', 'assets')) { Copy-Item -LiteralPath $dir -Destination (Join-Path $exportRoot $dir) -Recurse }

$dist = Join-Path $exportRoot 'dist'
New-Item -ItemType Directory -Path $dist | Out-Null
Copy-Item -LiteralPath 'dist\CodeTranspiler.exe' -Destination (Join-Path $dist 'CodeTranspiler.exe')

Get-ChildItem -LiteralPath $exportRoot -Recurse -File |
    ForEach-Object { $_.FullName.Substring($exportRoot.Length + 1).Replace('\', '/') } |
    Sort-Object | Set-Content -LiteralPath (Join-Path $exportRoot 'MANIFEST.txt') -Encoding utf8

Write-Host ('GitHub folder: ' + $exportRoot)
