@echo off
setlocal EnableExtensions DisableDelayedExpansion
pushd "%~dp0"
if errorlevel 1 exit /b %errorlevel%
go build -o portprog.exe .
set "_portprog_status=%errorlevel%"
popd
exit /b %_portprog_status%
