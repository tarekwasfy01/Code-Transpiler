@echo off
setlocal
cd /d "%~dp0"
semantic-proof-compressor.exe "."
echo.
if errorlevel 1 (
  echo VERIFY: FAIL
  exit /b 1
)
echo VERIFY: PASS
pause
