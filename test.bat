@echo off
setlocal EnableExtensions DisableDelayedExpansion
pushd "%~dp0"
if errorlevel 1 exit /b %errorlevel%
go test ./...
if errorlevel 1 goto finish
go build -o portprog.exe .
if errorlevel 1 goto finish
portprog.exe -test
if errorlevel 1 goto finish
portprog.exe --validate
:finish
set "_portprog_status=%errorlevel%"
popd
exit /b %_portprog_status%
