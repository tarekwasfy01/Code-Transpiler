param(
  [string]$Root = (Get-Location).Path,
  [string]$Out = (Join-Path (Get-Location).Path 'outputs/go-semantic-project-current'),
  [int]$Workers = 6,
  [int]$Limit = 0
)
$ErrorActionPreference = 'Stop'
$null = New-Item -ItemType Directory -Force -Path $Out
$jsonRoot = Join-Path $Out 'semantic-json'
$null = New-Item -ItemType Directory -Force -Path $jsonRoot
$exe = Join-Path $Out 'r2many.exe'
$env:CGO_ENABLED = '0'
$env:GOCACHE = Join-Path $Root '.cache-go-semantic-project'
go build -o $exe ./cmd/r2many
$files = @(rg --files -g '*.go' -g '!*_test.go' -g '!\.cache*' -g '!outputs/**' | ForEach-Object { Join-Path $Root $_ })
if ($Limit -gt 0) { $files = @($files | Select-Object -First $Limit) }
$work = $files | ForEach-Object { [pscustomobject]@{File=$_;Root=$Root;Out=$jsonRoot;Exe=$exe} }
$rows = $work | ForEach-Object -Parallel {
  $item = $_; $file = $item.File; $root = $item.Root; $out = $item.Out; $bin = $item.Exe
  $bytes = [IO.File]::ReadAllBytes($file); $sha = [Security.Cryptography.SHA256]::Create().ComputeHash($bytes)
  $hash = ([BitConverter]::ToString($sha)).Replace('-','').ToLowerInvariant()
  $rel = [IO.Path]::GetRelativePath($root, $file); $safe = $rel.Replace('\','__').Replace('/','__')
  $json = Join-Path $out ($safe + '.semantic.json'); $sw = [Diagnostics.Stopwatch]::StartNew()
  $errFile = $json + '.err'; & $bin semantic-export -source go $file -o $json 2> $errFile | Out-Null; $exit = $LASTEXITCODE
  $sw.Stop(); $err = ''; if (Test-Path $errFile) { $err = Get-Content $errFile -Raw }
  $status = if ($exit -eq 0 -and (Test-Path $json)) { 'PASS' } else { 'FAIL' }
  $phase = if($status -eq 'PASS'){'SOURCE_TO_SEMANTIC'}else{'SOURCE_PARSE_OR_SEMANTIC'}; $outputPath = if($status -eq 'PASS'){$json}else{''}
  if ($null -eq $err) { $err = '' }
  [pscustomobject]@{ file=$rel; source_hash=$hash; status=$status; phase=$phase; exit_code=$exit; duration_ms=$sw.ElapsedMilliseconds; output=$outputPath; diagnostic=($err.Trim()) }
} -ThrottleLimit $Workers
$csv = Join-Path $Out 'results.csv'; $rows | Export-Csv -NoTypeInformation -Encoding UTF8 $csv
$failures = @($rows | Where-Object status -eq 'FAIL')
$matrix = foreach($r in $failures) {
  $d = ($r.diagnostic -replace '\s+',' ').Trim(); if($d.Length -gt 400){$d=$d.Substring(0,400)}
  $family = if($d -match 'import|package|module'){'PACKAGE_RESOLUTION'} elseif($d -match 'type|declared|signature'){'TYPE_OR_DECLARATION'} elseif($d -match 'uast|semantic|operation|lower'){'SEMANTIC_LOWERING'} else {'FRONTEND_CONTRACT'}
  $primitive = if($d -match 'function|method|call'){'CALL_BINDING'} elseif($d -match 'range|loop|for'){'ITERATION_CONTROL'} elseif($d -match 'slice|index|array|map'){'AGGREGATE_ACCESS'} elseif($d -match 'type'){'TYPE_CONTRACT'} else {'STRUCTURED_FRONTEND'}
  [pscustomobject]@{file=$r.file;source_hash=$r.source_hash;phase=$r.phase;failure_family=$family;primitive=$primitive;contract=($family+'|'+$primitive);diagnostic=$d}
}
$matrix | Export-Csv -NoTypeInformation -Encoding UTF8 (Join-Path $Out 'failure_matrix.csv')
$matrix | Group-Object failure_family,primitive | ForEach-Object { [pscustomobject]@{failure_family=$_.Group[0].failure_family;primitive=$_.Group[0].primitive;count=$_.Count;representative=$_.Group[0].diagnostic} } | Export-Csv -NoTypeInformation -Encoding UTF8 (Join-Path $Out 'failure_families.csv')
$summary = [pscustomobject]@{ total=$rows.Count; pass=@($rows|? status -eq PASS).Count; fail=$failures.Count; workers=$Workers; output=$Out }
$summary | ConvertTo-Json | Set-Content (Join-Path $Out 'summary.json')
$summary
