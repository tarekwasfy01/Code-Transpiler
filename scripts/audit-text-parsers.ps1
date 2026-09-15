param(
    [string]$Root = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path,
    [string]$Output = (Join-Path $Root 'outputs\legacy-removal\text_parser_audit.csv')
)

$patterns = [ordered]@{
    'regexp.MustCompile|regexp.Compile|FindString(Submatch)?|MatchString' = 'regexp'
    'strings\.HasPrefix|strings\.HasSuffix|strings\.Split|strings\.Index' = 'string-operation'
    'parseExpr|parseExpression|parseStmt|parseStatement|parseAssign|parsePrint' = 'text-parser-helper'
    'translateExpr|toRExpr' = 'text-emitter-helper'
}
$roots = @('internal', 'cmd') | ForEach-Object { Join-Path $Root $_ }
$rows = [System.Collections.Generic.List[object]]::new()
foreach ($file in Get-ChildItem $roots -Recurse -File -Filter '*.go' -ErrorAction SilentlyContinue) {
    $lines = Get-Content -LiteralPath $file.FullName
    $function = ''
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match '^\s*func\s+(?:\([^)]*\)\s*)?([A-Za-z0-9_]+)\s*\(') { $function = $Matches[1] }
        foreach ($entry in $patterns.GetEnumerator()) {
            if ($lines[$i] -match $entry.Key) {
                $testOnly = $file.Name -like '*_test.go'
                $semantic = $entry.Value -in @('text-parser-helper') -or $lines[$i] -match 'Canonicalize|parseFrontendFacts|AnalyzeSemantic'
                if ($testOnly) { $classification = 'TEST_ONLY'; $productive = $false }
                elseif ($file.FullName -match '\\scripts\\|\\tools\\') { $classification = 'TOOLING_ONLY'; $productive = $false }
                elseif ($semantic -and (($file.Name -eq 'frontend_fact_parser.go') -or ($file.Name -eq 'manytomany.go' -and $function -in @('parseAssign','parsePrint')))) { $classification = 'DEAD_LEGACY'; $productive = $false }
                elseif ($semantic) { $classification = 'PRODUCTIVE_SEMANTIC_PARSE'; $productive = $true }
                elseif ($entry.Value -eq 'regexp') { $classification = 'DIAGNOSTIC_ONLY'; $productive = $false }
                else { $classification = 'TARGET_FORMATTING'; $productive = $false }
                $replacement = if ($productive) { 'CanonicalSemanticEvent -> FrontendSemanticFacts -> UAST' } else { 'retain outside canonical source-to-UAST path' }
                $rows.Add([pscustomobject]@{
                    file = $file.FullName.Substring($Root.Length + 1)
                    line = $i + 1
                    function = $function
                    mechanism = $entry.Value
                    regexp_or_string_operation = $lines[$i].Trim()
                    reads_source_text = [bool]($lines[$i] -match 'source|code|text|line')
                    infers_syntax = $semantic
                    infers_semantics = $semantic
                    productive_reachable = $productive
                    classification = $classification
                    replacement = $replacement
                    status = if ($productive) { 'ACTION_REQUIRED' } else { 'AUDITED' }
                })
                break
            }
        }
    }
}
New-Item -ItemType Directory -Force (Split-Path -Parent $Output) | Out-Null
$rows | Export-Csv -LiteralPath $Output -NoTypeInformation -Encoding UTF8
Write-Output "TEXT_PARSER_AUDIT=$Output"
Write-Output "ROWS=$($rows.Count)"
Write-Output "PRODUCTIVE_SEMANTIC_PARSE=$(@($rows | Where-Object productive_reachable).Count)"
