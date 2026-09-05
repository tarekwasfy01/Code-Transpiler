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
    'Package.appxmanifest',
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
foreach ($dir in @('.github', 'cmd', 'internal', 'assets', 'cpp_runtime', 'licenses', 'scripts', 'tools', 'docs', 'tests', 'oracle')) {
    Copy-Item -LiteralPath $dir -Destination (Join-Path $exportRoot $dir) -Recurse
}
# Copy only the runtime-facing matrix contracts.  The complete raw parser and
# mining corpus remain in the development checkout; they are not required by
# the Go module build and made the previous export exceed proxy limits.
$matrixRoot = Join-Path $exportRoot 'matrices'
New-Item -ItemType Directory -Path $matrixRoot -Force | Out-Null
foreach ($relative in @(
    'REAL_TS_MATRIX/execution_ready',
    'REAL_TS_MATRIX/15_construct_true_parser_kernel.csv',
    'frontend_closure/producer_spec',
    'frontend_closure/tree_sitter_input',
    'uast_engine',
    'uast_handoff/semantic_language_matrix_13'
)) {
    $sourcePath = Join-Path 'matrices' $relative
    $destinationPath = Join-Path $matrixRoot $relative
    $parent = Split-Path -Parent $destinationPath
    New-Item -ItemType Directory -Path $parent -Force | Out-Null
    Copy-Item -LiteralPath $sourcePath -Destination $destinationPath -Recurse -Force
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

# Keep the published Go module self-contained without copying the historical
# raw parser sources and duplicate analysis dumps.  The product consumes the
# partitioned execution-ready tables plus the producer/kernel contracts below;
# raw parser.c files are development/Oracle inputs and are not needed to build
# or run the packaged Go code.  Removing the global table copies is safe because
# LoadRealTables selects the language partition whenever it exists.
$prune = @(
    'matrices/REAL_TS_MATRIX/raw_parser_c',
    'matrices/REAL_TS_MATRIX/normalized',
    'matrices/tree_sitter_full',
    'matrices/language_source_scans',
    'matrices/uast_corpus_partial',
    'matrices/REAL_TS_MATRIX/execution_ready/parse_dispatch.csv',
    'matrices/REAL_TS_MATRIX/execution_ready/parse_dispatch_supplement.csv',
    'matrices/REAL_TS_MATRIX/execution_ready/parse_dispatch.pre_source_order.csv'
)
foreach ($relative in $prune) {
    $path = Join-Path $exportRoot $relative
    if (Test-Path -LiteralPath $path) {
        Remove-Item -LiteralPath $path -Recurse -Force
    }
}

$dist = Join-Path $exportRoot 'dist'
New-Item -ItemType Directory -Path $dist | Out-Null
Copy-Item -LiteralPath 'dist\CodeTranspiler.exe' -Destination (Join-Path $dist 'CodeTranspiler.exe')

Get-ChildItem -LiteralPath $exportRoot -Recurse -File |
    ForEach-Object { $_.FullName.Substring($exportRoot.Length + 1).Replace('\', '/') } |
    Sort-Object | Set-Content -LiteralPath (Join-Path $exportRoot 'MANIFEST.txt') -Encoding utf8

Write-Host ('GitHub folder: ' + $exportRoot)
