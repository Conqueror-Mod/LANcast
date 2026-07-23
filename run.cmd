@echo off
REM Starts LANcast and opens it in your browser.
REM Closing this window stops the server.
setlocal
cd /d "%~dp0"

set PORT=8080

REM A previous server still holding the port is the failure that looks like
REM "my changes did nothing": the new process cannot bind, exits immediately,
REM and the old build keeps serving the browser.
netstat -ano | findstr /r /c:"LISTENING" | findstr /c:":%PORT% " >nul
if not errorlevel 1 (
    echo.
    echo   Port %PORT% is already in use — LANcast is probably still running.
    echo.
    echo   That old server will keep answering your browser, so a restart
    echo   appears to do nothing. Close its window, or run:
    echo.
    echo       taskkill /IM lancastd.exe /F
    echo.
    pause
    exit /b 1
)

if not exist lancastd.exe (
    echo lancastd.exe not found. Building it...
    go build -o lancastd.exe ./cmd/lancastd || goto :nogo
)

start "" http://localhost:%PORT%
lancastd.exe -addr :%PORT%

REM Reached when the server exits. Pausing keeps the reason on screen instead
REM of the window vanishing with the error in it.
echo.
echo LANcast has stopped.
pause
exit /b

:nogo
echo.
echo Build failed. Is Go installed and on your PATH?
echo Open a new terminal and run: go version
pause
