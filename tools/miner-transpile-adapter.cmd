@echo off
setlocal EnableExtensions DisableDelayedExpansion

set "SOURCE="
set "TARGET="
set "INPUT="
set "OUTPUT="

:next
if "%~1"=="" goto execute
if /I "%~1"=="--source" set "SOURCE=%~2"& shift & shift & goto next
if /I "%~1"=="--target" set "TARGET=%~2"& shift & shift & goto next
if /I "%~1"=="--input" set "INPUT=%~2"& shift & shift & goto next
if /I "%~1"=="--output" set "OUTPUT=%~2"& shift & shift & goto next
echo Unknown adapter argument: %~1 1>&2
exit /b 64

:execute
if "%SOURCE%"=="" exit /b 64
if "%TARGET%"=="" exit /b 64
if "%INPUT%"=="" exit /b 64
if "%OUTPUT%"=="" exit /b 64

"C:\Users\tarek\Desktop\Transpiler\Universal-Code-Transpiler - Kopie\dist\CodeTranspiler.exe" transpile -source "%SOURCE%" -target "%TARGET%" "%INPUT%" -o "%OUTPUT%"
exit /b %ERRORLEVEL%
