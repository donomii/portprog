@echo off
setlocal
cd /d "%~dp0"
call build.bat
if errorlevel 1 (
    echo Portprog demo could not start because its build failed.
    exit /b 1
) else (
    portprog.exe plan
    exit /b %ERRORLEVEL%
)
