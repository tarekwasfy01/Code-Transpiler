param(
    [string]$GoHandoff = 'C:/Users/tarek/Desktop/go_universal_ast_codex_handoff.zip',
    [string]$PythonHandoff = 'F:/download/python_semanticprogram_codex_matrix_handoff.zip',
    [string]$RHandoff = 'C:/Users/tarek/Desktop/r_semanticprogram_codex_matrix_handoff.zip',
    [string]$RustHandoff = 'C:/Users/tarek/Desktop/rust_semanticprogram_codex_matrix_handoff.zip',
    [string]$ClangCppHandoff = 'C:/Users/tarek/Desktop/clang_cpp_semanticprogram_codex_matrix_handoff.zip',
    [string]$KotlinHandoff = 'C:/Users/tarek/Desktop/kotlin_semanticprogram_codex_matrix_handoff.zip',
    [string]$JavaHandoff = 'C:/Users/tarek/Desktop/java_semanticprogram_codex_matrix_handoff.zip',
    [string]$CSharpHandoff = 'C:/Users/tarek/Desktop/csharp_semanticprogram_codex_matrix_handoff.zip',
    [string]$PythonSource = 'C:/Users/tarek/Desktop/cpython-main',
    [switch]$SkipPythonScan
)
$ErrorActionPreference = 'Stop'
Push-Location $PSScriptRoot
try {
    & python -m unittest discover -s tools/matrix-audit -p 'test_*handoff*.py' -v
    if ($LASTEXITCODE -ne 0) { throw 'Handoff calculation/differential tests failed.' }
    & python tools/matrix-audit/extract_handoffs.py
    if ($LASTEXITCODE -ne 0) { throw 'Handoff matrix extraction failed.' }
	& python tools/matrix-audit/build_uast_schema.py
	if ($LASTEXITCODE -ne 0) { throw 'Universal AST schema algebra failed.' }
	& python tools/matrix-audit/report_uast_coverage.py
	if ($LASTEXITCODE -ne 0) { throw 'Universal AST coverage report failed.' }
    & python tools/matrix-audit/all_handoffs.py --go $GoHandoff --python $PythonHandoff --r $RHandoff --rust $RustHandoff --clang-cpp $ClangCppHandoff --kotlin $KotlinHandoff --java $JavaHandoff --csharp $CSharpHandoff
    if ($LASTEXITCODE -ne 0) { throw 'Combined handoff reproduction failed.' }
	& python tools/matrix-audit/build_semantic_space.py
	if ($LASTEXITCODE -ne 0) { throw 'SemanticProgram feature-space generation failed.' }
    if (-not $SkipPythonScan) {
        & python tools/matrix-audit/python_handoff.py --handoff $PythonHandoff --source $PythonSource
        if ($LASTEXITCODE -ne 0) { throw 'CPython scan failed.' }
    }
    & "$PSScriptRoot/run-matrix-workbench.ps1"
} finally { Pop-Location }
