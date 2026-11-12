@echo off
powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%~dp0configure-agent-windows.ps1" %*
exit /b %ERRORLEVEL%
