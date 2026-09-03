$ErrorActionPreference = "Stop"

$module = "github.com/tarekwasfy01/Code-Transpiler"
$version = "v1.0.3"
$localGoPath = Join-Path $PSScriptRoot ".go-package-refresh-cache"

$env:GOPROXY = "https://proxy.golang.org"
$env:GOSUMDB = "sum.golang.org"
$env:GOPATH = $localGoPath
$env:GOMODCACHE = Join-Path $localGoPath "pkg\mod"

New-Item -ItemType Directory -Force -Path $env:GOMODCACHE | Out-Null

Write-Host "Waiting until the public Go proxy indexes $module@$version..."
$available = $false
for ($attempt = 1; $attempt -le 20; $attempt++) {
    $oldErrorActionPreference = $ErrorActionPreference
    $ErrorActionPreference = "Continue"
    $result = & go list -m "$module@$version" 2>&1
    $goExitCode = $LASTEXITCODE
    $ErrorActionPreference = $oldErrorActionPreference
    if ($goExitCode -eq 0) {
        Write-Host $result
        $available = $true
        break
    }

    Write-Host "Attempt $attempt/20: not indexed yet. Retrying in 15 seconds."
    if ($attempt -lt 20) {
        Start-Sleep -Seconds 15
    }
}

if (-not $available) {
    throw "The GitHub tag exists, but proxy.golang.org has not indexed it yet. Run this file again later."
}

Write-Host "Downloading the indexed release..."
go mod download "$module@$version"

Write-Host "Checking the latest published version..."
go list -m "$module@latest"

Write-Host "pkg.go.dev page:"
Write-Host "https://pkg.go.dev/$module"
