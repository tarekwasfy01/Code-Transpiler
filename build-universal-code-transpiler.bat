@echo off
setlocal
cd /d "%~dp0"

where go >nul 2>nul
if errorlevel 1 (
  echo Go was not found in PATH.
  pause
  exit /b 1
)

if not exist dist mkdir dist

echo Downloading Go modules...
go mod tidy
if errorlevel 1 goto :fail
del /q cmd\r2many\r2many_windows.syso >nul 2>nul

echo Embedding Code Transpiler icon...
go run github.com/akavel/rsrc@latest -ico assets\code-transpiler.ico -o cmd\r2many\r2many_windows.syso
if errorlevel 1 goto :fail

echo Building CodeTranspiler.exe...
set CGO_ENABLED=1
go build -trimpath -ldflags="-s -w -H windowsgui" -o dist\CodeTranspiler.exe .\cmd\r2many
if errorlevel 1 goto :fail

echo.
echo Build successful:
echo   dist\CodeTranspiler.exe
del /q cmd\r2many\r2many_windows.syso >nul 2>nul
pause
exit /b 0

:fail
echo.
echo Build failed.
pause
exit /b 1
