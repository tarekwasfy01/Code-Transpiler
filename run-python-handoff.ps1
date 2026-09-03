param(
    [string]$Handoff = 'F:/download/python_semanticprogram_codex_matrix_handoff.zip',
    [string]$Source = 'C:/Users/tarek/Desktop/cpython-main',
    [switch]$SkipProjectTests
)
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    & python -m unittest discover -s tools/matrix-audit -p test_python_handoff.py -v
    if ($LASTEXITCODE -ne 0) { throw 'Python handoff differential tests failed.' }
    & python tools/matrix-audit/python_handoff.py --handoff $Handoff --source $Source
    if ($LASTEXITCODE -ne 0) { throw 'Handoff calculation or source scan failed.' }
    & "$PSScriptRoot/run-matrix-workbench.ps1" -SkipTests:$SkipProjectTests
} finally {
    Pop-Location
}
