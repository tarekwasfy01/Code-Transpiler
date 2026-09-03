param(
    [switch]$SkipTests
)
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
$previousCache = $env:GOCACHE
try {
    $env:GOCACHE = Join-Path $PSScriptRoot '.audit-cache/go-build'
    $workbenchArgs = @('run', './cmd/matrix-workbench')
    if (-not $SkipTests) { $workbenchArgs += '-test' }
    & go @workbenchArgs
    if ($LASTEXITCODE -ne 0) { throw 'Matrix verification failed. See the report and console output.' }
} finally {
    $env:GOCACHE = $previousCache
    Pop-Location
}
