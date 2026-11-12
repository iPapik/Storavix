@echo off
setlocal
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0configure-agent-windows.ps1" %*
set EXITCODE=%ERRORLEVEL%
if not "%EXITCODE%"=="0" (
  echo.
  echo Storavix Agent configuration failed with exit code %EXITCODE%.
  echo See C:\ProgramData\StoravixAgent\setup.log
  echo Press a key to close.
  pause >nul
)
exit /b %EXITCODE%
