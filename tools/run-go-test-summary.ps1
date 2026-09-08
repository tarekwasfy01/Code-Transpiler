param(
    [string]$OutputDirectory = "outputs/uast-migration",
    [string[]]$Packages = @(".", "./cmd/...", "./internal/...")
)

$ErrorActionPreference = "Stop"
$projectRoot = Split-Path -Parent $PSScriptRoot
$outputRoot = Join-Path $projectRoot $OutputDirectory
$cacheRoot = Join-Path $projectRoot ".cache\go-build"
New-Item -ItemType Directory -Force -Path $outputRoot, $cacheRoot | Out-Null

$env:GOCACHE = $cacheRoot
$env:GOMEMLIMIT = "4GiB"
$env:GOMAXPROCS = "2"

$eventsPath = Join-Path $outputRoot "go-test-events.jsonl"
$savedPreference = $ErrorActionPreference
$ErrorActionPreference = "Continue"
$raw = @(& go test -json -p 2 @Packages -count=1 2>&1 | ForEach-Object { $_.ToString() })
$exitCode = $LASTEXITCODE
$ErrorActionPreference = $savedPreference
$raw | Set-Content -LiteralPath $eventsPath -Encoding utf8

$events = foreach ($line in $raw) {
    try { $line | ConvertFrom-Json -ErrorAction Stop } catch { }
}
$testPass = @($events | Where-Object { $_.Test -and $_.Action -eq "pass" }).Count
$testFail = @($events | Where-Object { $_.Test -and $_.Action -eq "fail" }).Count
$testSkip = @($events | Where-Object { $_.Test -and $_.Action -eq "skip" }).Count
$packageStarted = @($events | Where-Object { -not $_.Test -and $_.Package -and $_.Action -eq "start" } | Select-Object -ExpandProperty Package -Unique).Count
$packagePass = @($events | Where-Object { -not $_.Test -and $_.Package -and $_.Action -eq "pass" } | Select-Object -ExpandProperty Package -Unique).Count
$packageFail = @($events | Where-Object { -not $_.Test -and $_.Package -and $_.Action -eq "fail" } | Select-Object -ExpandProperty Package -Unique).Count
$packageSkip = @($events | Where-Object { -not $_.Test -and $_.Package -and $_.Action -eq "skip" } | Select-Object -ExpandProperty Package -Unique).Count

$summary = [ordered]@{
    schema = "code-transpiler.go-test-summary.v1"
    command = "go test -json -p 2 $($Packages -join ' ') -count=1"
    exit_code = $exitCode
    tests_and_subtests_passed = $testPass
    tests_and_subtests_failed = $testFail
    tests_and_subtests_skipped = $testSkip
    go_packages_started = $packageStarted
    go_packages_passed = $packagePass
    go_packages_failed = $packageFail
    go_packages_without_tests = $packageSkip
    event_log = "go-test-events.jsonl"
}
$summaryPath = Join-Path $outputRoot "go-test-summary.json"
$summary | ConvertTo-Json | Set-Content -LiteralPath $summaryPath -Encoding utf8
$summary | ConvertTo-Json
exit $exitCode
