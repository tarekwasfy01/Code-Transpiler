@echo off
setlocal
where py >nul 2>nul
if errorlevel 1 (
  echo Python launcher 'py' was not found.
  exit /b 1
)
py -3 calculate_uast_schema.py
pause
