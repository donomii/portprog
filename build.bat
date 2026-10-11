@echo off
setlocal DisableDelayedExpansion
cd /d "%~dp0"
go build -o portprog.exe .
exit /b %errorlevel%
