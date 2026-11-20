@echo off
setlocal
cd /d "%~dp0"
go test ./...
if errorlevel 1 (
    echo Portprog Go tests failed.
    exit /b 1
) else (
    go vet ./...
)
if errorlevel 1 (
    echo Portprog vet checks failed.
    exit /b 1
) else (
    go build -o portprog.exe .
)
if errorlevel 1 (
    echo Portprog build failed.
    exit /b 1
) else (
    portprog.exe -test
    exit /b %ERRORLEVEL%
)
