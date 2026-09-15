param(
  [int]$Workers = 6,
  [int]$BatchSize = 50,
  [int]$TimeoutSeconds = 240,
  [string]$OutputRoot = "matrices/post_decompiler_mining_freeze_v3/source_shards",
  [string]$ValidatorPath = "",
  [ValidateSet('source','native')]
  [string]$Lane = 'source'
)
$ErrorActionPreference = 'Stop'
$root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$exe = if ($ValidatorPath) {
  if ([IO.Path]::IsPathRooted($ValidatorPath)) { $ValidatorPath } else { Join-Path $root $ValidatorPath }
} else { Join-Path $root 'outputs\tools\real-source-validation-freeze.exe' }
$input = Join-Path $root 'matrices\tree_sitter_full\15_corpus_cases.csv'
if (!(Test-Path -LiteralPath $exe)) { throw "missing validator: $exe" }
$rows = (Import-Csv $input).Count
$outputRootAbs = if ([IO.Path]::IsPathRooted($OutputRoot)) { $OutputRoot } else { Join-Path $root $OutputRoot }
New-Item -ItemType Directory -Force -Path $outputRootAbs | Out-Null
Get-FileHash -LiteralPath $exe -Algorithm SHA256 | ForEach-Object {
  [pscustomobject]@{ validator_path=$exe; validator_sha256=$_.Hash; lane=$Lane; workers=$Workers; batch_size=$BatchSize; timeout_seconds=$TimeoutSeconds; started_utc=(Get-Date).ToUniversalTime().ToString('o') } | ConvertTo-Json | Set-Content (Join-Path $outputRootAbs 'validator_build.json')
}
$logRoot = Join-Path $root ('outputs\second-freeze-logs\' + $Lane + '-shards')
New-Item -ItemType Directory -Force -Path $logRoot | Out-Null
$queue = [System.Collections.Generic.Queue[int]]::new()
for ($o=0; $o -lt $rows; $o += $BatchSize) { $queue.Enqueue($o) }
$running = @{}
$results = [System.Collections.Generic.List[object]]::new()
while ($queue.Count -gt 0 -or $running.Count -gt 0) {
  while ($queue.Count -gt 0 -and $running.Count -lt $Workers) {
    $offset = $queue.Dequeue(); $name = ('shard-{0:D4}' -f $offset)
    $out = Join-Path $outputRootAbs $name
    $stdout = Join-Path $logRoot "$name.stdout.log"; $stderr = Join-Path $logRoot "$name.stderr.log"
    $laneArgs = if ($Lane -eq 'native') { '-skip-source-target -direct-only' } else { '-skip-native -direct-only' }
    $arguments = "-input `"$input`" -out `"$out`" -offset $offset -limit $BatchSize $laneArgs"
    $p = Start-Process -FilePath $exe -ArgumentList $arguments -WorkingDirectory $root -RedirectStandardOutput $stdout -RedirectStandardError $stderr -PassThru -WindowStyle Hidden
    $running[$p.Id] = [pscustomobject]@{ Process=$p; Offset=$offset; Name=$name; Out=$out; Started=(Get-Date) }
  }
  Start-Sleep -Milliseconds 800
  foreach ($entry in @($running.Values)) {
    $entry.Process.Refresh()
    $elapsed = ((Get-Date)-$entry.Started).TotalSeconds
    if (!$entry.Process.HasExited -and $elapsed -gt $TimeoutSeconds) { Stop-Process -Id $entry.Process.Id -Force; $entry.Process.Refresh() }
    if ($entry.Process.HasExited) {
      $summary = Join-Path $entry.Out 'final_summary.json'
      $state = if (Test-Path -LiteralPath $summary) {'PASS'} elseif ($elapsed -gt $TimeoutSeconds) {'TIMEOUT'} else {'FAIL'}
      $results.Add([pscustomobject]@{lane=$Lane; offset=$entry.Offset; shard=$entry.Name; status=$state; exit_code=$entry.Process.ExitCode; elapsed_seconds=[math]::Round($elapsed,1); summary=$summary})
      $running.Remove($entry.Process.Id)
      Write-Host "SHARD=$($entry.Name) STATUS=$state ELAPSED=$([math]::Round($elapsed,1))s"
    }
  }
}
$results | Sort-Object offset | Export-Csv (Join-Path $outputRootAbs 'shard_status.csv') -NoTypeInformation
$results | ConvertTo-Json | Set-Content (Join-Path $outputRootAbs 'shard_status.json')
$pass=($results | Where-Object status -eq PASS).Count; $bad=$results.Count-$pass
Write-Host "SHARDS_TOTAL=$($results.Count) PASS=$pass NONPASS=$bad ROOT=$outputRootAbs"
if ($bad -ne 0) { exit 2 }
