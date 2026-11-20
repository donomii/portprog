@echo off
setlocal
cd /d "%~dp0"
call build.bat
if errorlevel 1 (
    echo Portprog installation stopped because its build failed.
    exit /b 1
) else (
    if not exist "dist\portprog" mkdir "dist\portprog"
)
copy /Y "portprog.exe" "dist\portprog\portprog.exe" >NUL
copy /Y "SPEC.md" "dist\portprog\SPEC.md" >NUL
copy /Y "MakeYourOwnPackage.md" "dist\portprog\MakeYourOwnPackage.md" >NUL
copy /Y "LICENSE" "dist\portprog\LICENSE" >NUL
xcopy /E /I /Y "catalog" "dist\portprog\catalog" >NUL
xcopy /E /I /Y "profiles" "dist\portprog\profiles" >NUL
copy /Y "packaging\run.bat" "dist\portprog\run.bat" >NUL
if exist "dist\portprog\portprog.conf" (
    echo Kept existing editable configuration: dist\portprog\portprog.conf
) else (
    copy /Y "portprog.conf" "dist\portprog\portprog.conf" >NUL
    echo Installed editable default configuration: dist\portprog\portprog.conf
)
echo Portable distribution ready: %CD%\dist\portprog
echo Start it with: %CD%\dist\portprog\run.bat
exit /b 0
