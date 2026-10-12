@echo off
setlocal EnableExtensions DisableDelayedExpansion
pushd "%~dp0"
if errorlevel 1 exit /b %errorlevel%
if not exist updater.go goto run
go build -o portprog.exe .
if errorlevel 1 goto finish
:run
if not exist portprog.exe (
  echo Portprog launcher expected "%~dp0portprog.exe" or buildable source "%~dp0updater.go"; neither is available. 1>&2
  set "_portprog_status=1"
  goto cleanup
)
portprog.exe %*
:finish
set "_portprog_status=%errorlevel%"
:cleanup
popd
exit /b %_portprog_status%
