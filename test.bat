@echo off
setlocal DisableDelayedExpansion
cd /d "%~dp0"
go test ./...
if errorlevel 1 exit /b %errorlevel%
go build -o portprog.exe .
if errorlevel 1 exit /b %errorlevel%
portprog.exe -test
if errorlevel 1 exit /b %errorlevel%
portprog.exe --validate
exit /b %errorlevel%
