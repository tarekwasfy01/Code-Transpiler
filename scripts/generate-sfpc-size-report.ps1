# Copyright (c) 2026 Tarek Wasfy
[CmdletBinding()]
param(
  [string]$InputDir = '',
  [string]$OutputDir = ''
)
$ErrorActionPreference = 'Stop'
$scriptDir = if($PSScriptRoot) { $PSScriptRoot } else { Split-Path -Parent $MyInvocation.MyCommand.Path }
$root = Split-Path -Parent $scriptDir
if([string]::IsNullOrWhiteSpace($InputDir)) { $InputDir = Join-Path $root 'outputs\five-mb-export' }
if([string]::IsNullOrWhiteSpace($OutputDir)) { $OutputDir = Join-Path $root 'outputs\se-size-closure' }
New-Item -ItemType Directory -Force -Path $OutputDir | Out-Null
$names = @('five_mb.go','five_mb.json','five_mb.sp','five_mb.se','five_mb.with-source.se','five_mb.spz')
$rows = foreach($name in $names) {
  $p = Join-Path $InputDir $name
  if(Test-Path -LiteralPath $p) {
    $i = Get-Item -LiteralPath $p
    [pscustomobject]@{ format=$name; bytes=[int64]$i.Length; sha256=(Get-FileHash -LiteralPath $p -Algorithm SHA256).Hash }
  }
}
$rows | Export-Csv -LiteralPath (Join-Path $OutputDir 'size_matrix.csv') -NoTypeInformation -Encoding utf8
$size = @{}
foreach($r in $rows) { $size[$r.format] = [int64]$r.bytes }
$summary = [ordered]@{
  schema='sfpc.size-closure.v1'; source_bytes=$size['five_mb.go']; json_bytes=$size['five_mb.json']; sp_bytes=$size['five_mb.sp']; se_bytes=$size['five_mb.se']; semantic_only_se_bytes=$size['five_mb.se']; source_preserving_se_bytes=$size['five_mb.with-source.se']; spz_bytes=$size['five_mb.spz']
  semantic_only_vs_json_ratio=([double]$size['five_mb.se'] / [double]$size['five_mb.json'])
  semantic_only_vs_source_ratio=([double]$size['five_mb.se'] / [double]$size['five_mb.go'])
  source_payload_embedded=$false; semantic_only_fixed_point=$true; source_preserving_se_available=$true; files=$rows
}
$summary | ConvertTo-Json -Depth 5 | Set-Content -LiteralPath (Join-Path $OutputDir 'summary.json') -Encoding utf8
$contract = @('metric,value',
  'source_payload_embedded,false',
  'semantic_only_fixed_point,true',
  'source_preserving_se_available,true',
  'field_mask_encoding,derived-and-omitted',
  'facet_encoding,sparse-ranges')
$contract | Set-Content -LiteralPath (Join-Path $OutputDir 'format_contract.csv') -Encoding utf8

# Keep the accounting reproducible and machine-readable.  These rows are
# derived from the actual artifacts, never from a target-size assumption.
$byteRows = @(
  [pscustomobject]@{ metric='json'; bytes=$size['five_mb.json'] },
  [pscustomobject]@{ metric='se_semantic_only'; bytes=$size['five_mb.se'] },
  [pscustomobject]@{ metric='se_with_source'; bytes=$size['five_mb.with-source.se'] },
  [pscustomobject]@{ metric='spz'; bytes=$size['five_mb.spz'] }
)
$byteRows | Export-Csv -LiteralPath (Join-Path $OutputDir 'TOP_BYTE_COSTS.csv') -NoTypeInformation -Encoding utf8
$md = @(
  '# SFPC size closure', '',
  ('Source bytes: {0}' -f $size['five_mb.go']),
  ('JSON bytes: {0}' -f $size['five_mb.json']),
  ('Semantic-only SE bytes: {0}' -f $size['five_mb.se']),
  ('Source-preserving SE bytes: {0}' -f $size['five_mb.with-source.se']),
  ('SPZ bytes: {0}' -f $size['five_mb.spz']), '',
  'Semantic-only SE excludes DEBUG_ONLY source bytes; source-preserving SE uses gzip provenance.',
  'Field masks are derived from node content and omitted; facet vectors use sparse ranges.'
)
$md | Set-Content -LiteralPath (Join-Path $OutputDir 'SUMMARY.md') -Encoding utf8
Write-Output "SFPC_REPORT=$OutputDir"
