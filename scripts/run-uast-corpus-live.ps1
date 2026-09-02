param(
    [string]$ProjectRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path,
    [string]$MinerRun = 'C:\Users\tarek\Desktop\uast-ecosystem-miner-v3.4-verified\run_20260901_101647',
    [string]$MLCPDRun = 'C:\Users\tarek\Desktop\mlcpd-stream-corpus-runner\mlcpd_results_20260901_105333',
    [string]$Output = 'outputs\uast-corpus-matrix-live',
    [int]$MinOccurrences = 2,
    [int]$MinDistinctHashes = 0,
    [int]$MinDistinctRepositories = 0,
    [int]$MinDistinctCorpusSources = 1,
    [int]$Workers = 4,
    [int]$Iteration = 1
)

$ErrorActionPreference = 'Stop'
Set-Location -LiteralPath $ProjectRoot
$outputPath = Join-Path $ProjectRoot $Output
$checkpoint = Join-Path $outputPath 'checkpoint.json'

$args = @(
    'run', './cmd/uast-corpus-matrix',
    '--project', $ProjectRoot,
    '--miner-zip', $MinerRun,
    '--out', $outputPath,
    '--min-occurrences', $MinOccurrences,
    '--min-distinct-hashes', $MinDistinctHashes,
    '--min-distinct-repositories', $MinDistinctRepositories,
    '--min-distinct-corpus-sources', $MinDistinctCorpusSources,
    '--workers', $Workers,
    '--iteration', $Iteration,
    '--checkpoint', $checkpoint
)

# MLCPD is included only when records have arrived. The adapter skips the
# runner's temporary parquet files and never treats its structural schema as
# canonical UAST input.
if (Test-Path -LiteralPath $MLCPDRun) {
    $args += @('--mlcpd', $MLCPDRun)
}

$cache = Join-Path $ProjectRoot '.gocache-uast-corpus-live'
$env:GOCACHE = $cache
& go @args
if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }

$summary = Join-Path $outputPath 'corpus_matrix_summary.json'
if (Test-Path -LiteralPath $summary) {
    Get-Content -LiteralPath $summary -Raw
}
