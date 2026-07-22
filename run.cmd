@echo off
REM Starts LANcast and opens it in your browser.
REM Closing this window stops the server.
cd /d "%~dp0"

if not exist lancastd.exe (
    echo lancastd.exe not found. Building it...
    go build -o lancastd.exe ./cmd/lancastd || goto :nogo
)

start "" http://localhost:8080
lancastd.exe -addr :8080
goto :eof

:nogo
echo.
echo Build failed. Is Go installed and on your PATH?
echo Open a new terminal and run: go version
pause
