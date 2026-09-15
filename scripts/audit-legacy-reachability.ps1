param(
    [string]$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path,
    [string]$Output = (Join-Path $Root 'outputs\legacy-removal')
)
New-Item -ItemType Directory -Force $Output | Out-Null
$files = Get-ChildItem (Join-Path $Root 'internal'), (Join-Path $Root 'cmd') -Recurse -File -Filter '*.go' -ErrorAction SilentlyContinue
$rows = [System.Collections.Generic.List[object]]::new()
$compatFunctions = @('ParseSemantic', 'ParseSemanticCompatibility', 'TraceCanonicalize', 'Canonicalize', 'parseFrontendFacts', 'LowerMatrixEvents', 'LowerMatrixEventsWithFactSink', 'LowerMatrixActions')
$compatFiles = @('frontend_fact_parser.go', 'generated_decode.go', 'bootstrap_trace.go')
foreach ($file in $files) {
    $lines = Get-Content -LiteralPath $file.FullName
    $function = ''
    for ($i=0; $i -lt $lines.Count; $i++) {
        $line = $lines[$i]
        if ($line -match '^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z0-9_]+)\s*\(') { $function = $Matches[1] }
        if ($line -match '^\s*(//|/\*|\*)') { continue }
        if ($line -match '\b(ParseSemantic|TraceCanonicalize|Legacy|legacy|parseFrontendFacts|LowerMatrixEvents)\b') {
            $component = $Matches[1]
            $test = $file.Name -like '*_test.go'
            $declaration = $line -match '^\s*(?:func|type|var|const)\b'
            $compat = $file.Name -in $compatFiles -or $file.FullName -match 'legacy|compat|bootstrap' -or $function -in $compatFunctions -or ($file.Name -eq 'generic_engine.go' -and $function -eq 'Trace') -or $line -match 'compatibility|legacy'
            if ($declaration) { $compat = $true }
            $productive = (-not $test) -and (-not $compat)
            $rows.Add([pscustomobject]@{
                component = $component
                file = $file.FullName.Substring($Root.Length+1)
                line = $i+1
                entry_point = if ($file.FullName -match 'manytomany') {'public-transpile'} elseif ($file.FullName -match 'cmd') {'cli'} else {'internal'}
                productive = $productive
                classification = if ($test) {'TEST_ONLY'} elseif ($compat) {'COMPATIBILITY_CALLER'} else {'PRODUCTIVE_CALLER_REVIEW'}
            })
        }
    }
}
$rows | Export-Csv (Join-Path $Output 'legacy_symbols.csv') -NoTypeInformation -Encoding UTF8
$rows | Where-Object productive | Export-Csv (Join-Path $Output 'productive_callers.csv') -NoTypeInformation -Encoding UTF8
$rows | Where-Object { -not $_.productive -and $_.classification -eq 'TEST_ONLY' } | Export-Csv (Join-Path $Output 'test_only_callers.csv') -NoTypeInformation -Encoding UTF8
$rows | Where-Object { $_.classification -eq 'COMPATIBILITY_CALLER' } | Export-Csv (Join-Path $Output 'compatibility_callers.csv') -NoTypeInformation -Encoding UTF8
@('dead_code.csv','uast_only_backend.csv','removal_plan.csv') | ForEach-Object { if (-not (Test-Path (Join-Path $Output $_))) { 'component,status,action' | Set-Content (Join-Path $Output $_) } }
Write-Output "LEGACY_AUDIT=$Output"
Write-Output "LEGACY_SYMBOL_ROWS=$($rows.Count)"
Write-Output "PRODUCTIVE_CALLER_ROWS=$(@($rows | Where-Object productive).Count)"
