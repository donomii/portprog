@echo off
setlocal
cd /d "%~dp0"
go build -o portprog.exe .
if errorlevel 1 (
    echo Portprog build failed. Review the Go error above.
    exit /b 1
) else (
    echo Built Portprog: %CD%\portprog.exe
    exit /b 0
)
