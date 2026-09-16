@echo off
chcp 65001 >nul 2>&1
setlocal EnableExtensions EnableDelayedExpansion

REM ==========================================================
REM  Decision Debate - one-click launcher (MOCK mode)
REM  Zero cost, no API key required.
REM
REM  USAGE: Double-click this file.
REM  DO NOT run it via bash (bash goes to WSL and cannot read C:/ paths).
REM
REM  IMPORTANT (do not "fix" this file casually):
REM  All comments below are ASCII English on purpose.
REM  This file is saved as UTF-8. cmd.exe parses .bat files using the
REM  system code page (GBK on this machine), so multi-byte UTF-8 text
REM  in comments gets mangled, breaks the line prefix (rem/echo), and
REM  the leftovers get executed as commands. Keep comments ASCII-only.
REM  Only echo output uses Chinese, printed after chcp 65001.
REM ==========================================================

set "ROOT=%~dp0"
if "%ROOT:~-1%"=="\" set "ROOT=%ROOT:~0,-1%"

set "GOROOT=C:\Users\Arina\.workbuddy\binaries\go"
set "GOPATH=%ROOT%\server\gopath"
set "GOCACHE=%ROOT%\server\gocache"
set "GO=%GOROOT%\bin\go.exe"

set "NODEDIR=C:\Users\Arina\.workbuddy\binaries\node\versions\22.22.2-3"
set "PATH=%NODEDIR%;%PATH%"

set "BACKEND_PORT=8080"
set "FRONTEND_PORT=5173"
set "EXE=%ROOT%\server\debate-server.exe"

echo.
echo ========================================
echo   Decision Debate - starting (MOCK mode)
echo ========================================
echo.

REM --- sanity checks ---
if not exist "%GO%" (
    echo [ERROR] go.exe not found at:
    echo         %GO%
    echo.
    pause
    exit /b 1
)
if not exist "%ROOT%\server\go.mod" (
    echo [ERROR] server\go.mod not found. Put this file in the project root.
    echo         Inferred root: %ROOT%
    echo.
    pause
    exit /b 1
)
if not exist "%ROOT%\web\package.json" (
    echo [ERROR] web\package.json not found. Put this file in the project root.
    echo.
    pause
    exit /b 1
)

REM --- port check: fail loudly instead of silently switching ports ---
REM  A leftover frontend once occupied 5173, so Vite silently moved to 5174
REM  while the user kept opening 5173 and saw a half-dead service.
call :CheckPort %BACKEND_PORT%
if errorlevel 1 goto :PortBusy
call :CheckPort %FRONTEND_PORT%
if errorlevel 1 goto :PortBusy

REM --- build first, then run the exe ---
REM  "go run" leaves the child process CWD inside the go build temp dir,
REM  which made the telemetry file fail with "Access is denied" and the
REM  process exited instantly (symptom: blank page after clicking start).
REM  Building an exe keeps the working directory under control.
echo [1/3] Building backend...
cd /d "%ROOT%\server"
"%GO%" build -o "%EXE%" ./cmd/server
if errorlevel 1 (
    echo.
    echo [ERROR] Backend build failed. Send the output above to the developer.
    echo.
    pause
    exit /b 1
)
echo       OK: %EXE%
echo.

REM --- force the mock branch ---
set "OPENAI_API_KEY="
set "PORT=%BACKEND_PORT%"
set "TELEMETRY_FILE=%ROOT%\server\telemetry.jsonl"
set "DECISIONS_FILE=%ROOT%\server\decisions.json"

echo [2/3] Starting backend (MOCK :%BACKEND_PORT%)...
start "debate-backend-mock" cmd /k ""%EXE%""

echo       Waiting for backend to become ready...
set "READY="
for /l %%i in (1,1,25) do (
    if not defined READY (
        call :Nap
        curl -s -o nul http://127.0.0.1:%BACKEND_PORT%/healthz && set "READY=1"
    )
)
if defined READY (
    echo       Backend is up.
) else (
    echo.
    echo [WARN] Backend did not respond within 25s.
    echo        Check the window titled "debate-backend-mock" for errors.
    echo.
)
echo.

echo [3/3] Starting frontend (:%FRONTEND_PORT%)...
start "debate-frontend" cmd /k "cd /d "%ROOT%\web" && npm run dev"

REM --- wait for the frontend port, then report only the truth ---
echo       Waiting for frontend...
set "FE_READY="
for /l %%i in (1,1,30) do (
    if not defined FE_READY (
        call :Nap
        call :CheckListening %FRONTEND_PORT%
        if not errorlevel 1 set "FE_READY=1"
    )
)
echo.

if defined READY (
    echo   [OK] Backend is LISTENING on port %BACKEND_PORT%
) else (
    echo   [!!] Backend is NOT listening on port %BACKEND_PORT%
)
if defined FE_READY (
    echo   [OK] Frontend is LISTENING on port %FRONTEND_PORT%
) else (
    echo   [!!] Frontend is NOT listening on port %FRONTEND_PORT%
)
echo.
echo ========================================
echo   Open this in your browser:
echo       http://localhost:%FRONTEND_PORT%/
echo.
echo   Use "localhost", NOT "127.0.0.1"
echo   (Vite here listens on IPv6 only)
echo.
echo   MOCK mode: debate text is placeholder only.
echo   For real debates double-click start-real.bat
echo.
echo   Keep both black windows open.
echo ========================================
echo.
pause
exit /b 0

:PortBusy
echo.
echo [ERROR] Port already in use. Cannot start.
echo.
echo   How to fix:
echo     1. Close all black windows whose title contains "debate"
echo     2. If still busy, run in a terminal:
echo          netstat -ano ^| findstr "LISTENING"
echo        then: taskkill /PID ^<pid^> /F
echo.
pause
exit /b 1

REM --- helper: is %1 already LISTENING? ---
:CheckPort
call :CheckListening %1
if not errorlevel 1 (
    echo [ERROR] Port %1 is already in use.
    exit /b 1
)
exit /b 0

REM --- helper: exit code 0 if %1 IS listening, 1 if not ---
:CheckListening
netstat -ano | findstr "LISTENING" | findstr ":%~1 " >nul 2>&1
exit /b %errorlevel%

REM --- helper: sleep ~1s ---
REM  Uses ping, NOT "timeout /t 1": timeout aborts with
REM  "Input redirection is not supported" when stdin is redirected,
REM  and it only works reliably in an interactive console.
:Nap
ping -n 2 127.0.0.1 >nul 2>&1
exit /b 0
