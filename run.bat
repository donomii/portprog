@echo off
setlocal DisableDelayedExpansion
cd /d "%~dp0"
if not exist updater.go goto run
go build -o portprog.exe .
if errorlevel 1 exit /b %errorlevel%
:run
if not exist portprog.exe (
  echo Portprog launcher expected "%~dp0portprog.exe" or buildable source "%~dp0updater.go"; neither is available. 1>&2
  exit /b 1
)
portprog.exe %*
exit /b %errorlevel%
