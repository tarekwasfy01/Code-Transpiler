@echo off
setlocal
cd /d "%~dp0"
set CGO_ENABLED=0

where go >nul 2>nul
if errorlevel 1 (
    echo ERROR: Go was not found on PATH.
    exit /b 1
)

go mod tidy
if errorlevel 1 exit /b %errorlevel%

go run .
exit /b %errorlevel%
