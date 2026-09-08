$ErrorActionPreference = 'Stop'
& (Join-Path $PSScriptRoot 'build\build-onefile.ps1')
exit $LASTEXITCODE
