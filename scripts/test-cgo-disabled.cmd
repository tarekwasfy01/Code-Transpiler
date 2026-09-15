@echo off
setlocal
cd /d "%~dp0\.."
set CGO_ENABLED=0

go test ./...
if errorlevel 1 exit /b %errorlevel%

for %%G in (windows linux darwin) do (
    echo Validating importable pure-Go library for %%G...
    set GOOS=%%G
    go build .
    if errorlevel 1 exit /b %errorlevel%
)

set GOOS=windows
pushd examples\list-languages
go mod tidy
if errorlevel 1 (
    popd
    exit /b %errorlevel%
)
go run .
set RESULT=%errorlevel%
popd
exit /b %RESULT%
