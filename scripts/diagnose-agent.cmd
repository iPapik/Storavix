@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0diagnose-agent-windows.ps1" %*
exit /b %ERRORLEVEL%
