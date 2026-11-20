@echo off
setlocal
cd /d "%~dp0"
portprog.exe
exit /b %ERRORLEVEL%
