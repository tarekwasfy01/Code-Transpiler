$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $PSScriptRoot
$env:GOCACHE = Join-Path $PSScriptRoot '.audit-cache\go-build'
$env:CGO_ENABLED = '1'
$releaseDir = Join-Path $PSScriptRoot 'dist'
New-Item -ItemType Directory -Path $releaseDir -Force | Out-Null

Write-Host 'Testing source packages...'
& go test . ./cmd/... ./internal/... ./tools/matrix-audit
if ($LASTEXITCODE -ne 0) { throw 'Package tests failed' }

Write-Host 'Generating the Windows icon resource with pinned rsrc v0.10.2...'
& go run github.com/akavel/rsrc@v0.10.2 -ico assets/code-transpiler.ico -o cmd/r2many/r2many_windows.syso
if ($LASTEXITCODE -ne 0) { throw 'Icon resource generation failed' }

$candidate = Join-Path $releaseDir 'CodeTranspiler.new.exe'
Write-Host 'Building the standalone Windows executable...'
& go build -mod=readonly -trimpath -ldflags='-s -w -H windowsgui' -o $candidate ./cmd/r2many
if ($LASTEXITCODE -ne 0) { throw 'Onefile build failed' }

$data = [System.IO.File]::ReadAllBytes($candidate)
if ($data.Length -lt 1024 -or $data[0] -ne 77 -or $data[1] -ne 90) { throw 'Invalid executable header' }
$pe = [BitConverter]::ToInt32($data, 60)
if ([BitConverter]::ToUInt32($data, $pe) -ne 17744) { throw 'Invalid PE signature' }
if ([BitConverter]::ToUInt16($data, $pe + 4) -ne 34404) { throw 'Expected x64 PE machine' }
if ([BitConverter]::ToUInt16($data, $pe + 24 + 68) -ne 2) { throw 'Expected Windows GUI subsystem' }

$final = Join-Path $releaseDir 'CodeTranspiler.exe'
if (Test-Path -LiteralPath $final) {
    $backup = Join-Path $releaseDir ('CodeTranspiler.previous-' + (Get-Date -Format 'yyyyMMdd-HHmmss') + '.exe')
    Copy-Item -LiteralPath $final -Destination $backup
}
Move-Item -LiteralPath $candidate -Destination $final -Force
Get-FileHash -LiteralPath $final -Algorithm SHA256
Write-Host ('Built: ' + $final)
