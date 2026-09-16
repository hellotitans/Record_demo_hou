@echo off
chcp 65001 >nul 2>&1
setlocal EnableExtensions EnableDelayedExpansion

REM ==========================================================
REM  Decision Debate - one-click launcher (REAL LLM mode)
REM  Calls the DeepSeek API. Costs about CNY 0.05 per debate.
REM
REM  PREREQUISITE: key.env must contain your OPENAI_API_KEY.
REM
REM  USAGE: Double-click this file.
REM  DO NOT run it via bash (bash goes to WSL and cannot read C:/ paths).
REM
REM  IMPORTANT (do not "fix" this file casually):
REM  All comments below are ASCII English on purpose. This file is UTF-8;
REM  cmd.exe parses .bat with the system code page (GBK here), so multi-byte
REM  UTF-8 in comments gets mangled and the leftovers run as commands.
REM  Keep comments ASCII-only and keep echo output ASCII-only too.
REM
REM  ALSO: never put "(" or ")" inside echo text that sits inside an
REM  "if (...)" / "for (...)" block. cmd.exe closes the block at the first
REM  ")" it finds even inside echo text, and whatever is left over becomes a
REM  stray token. One such line once produced the message
REM      ": was unexpected at this time."
REM  and the script died right after printing the banner. Keep echo text
REM  inside blocks free of parentheses (or escape them as ^( and ^) ).
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
set "KEYFILE=%ROOT%\key.env"

echo.
echo ========================================
echo   Decision Debate - starting (REAL LLM)
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
if not exist "%KEYFILE%" (
    echo [ERROR] key.env not found at:
    echo         %KEYFILE%
    echo.
    echo         Create it with one line:
    echo             OPENAI_API_KEY="sk-your-real-key"
    echo         Get a key: https://platform.deepseek.com/api_keys
    echo.
    pause
    exit /b 1
)

REM --- read the key from key.env ---
REM  Skips comment lines (they may contain = and " characters, which would
REM  otherwise derail the parser). Takes the first OPENAI_API_KEY line only.
set "OPENAI_API_KEY="
for /f "usebackq tokens=1,* delims==" %%a in ("%KEYFILE%") do (
    if not defined OPENAI_API_KEY (
        set "K=%%a"
        set "V=%%b"
        REM Tolerate "OPENAI_API_KEY = ..." with spaces around the equals sign.
        REM Variable names and API keys never contain spaces, so dropping all
        REM of them is safe and removes a whole class of user typos.
        set "K=!K: =!"
        set "V=!V: =!"
        if /i "!K!"=="OPENAI_API_KEY" set "OPENAI_API_KEY=!V!"
    )
)
REM strip quotes and surrounding spaces
if defined OPENAI_API_KEY (
    set "OPENAI_API_KEY=!OPENAI_API_KEY:"=!"
    for /f "tokens=* delims= " %%v in ("!OPENAI_API_KEY!") do set "OPENAI_API_KEY=%%v"
)

if not defined OPENAI_API_KEY (
    echo [ERROR] Could not read OPENAI_API_KEY from key.env
    echo.
    echo         Correct format - keep the quotes, no spaces around = :
    echo             OPENAI_API_KEY="sk-xxxxxxxx"
    echo.
    pause
    exit /b 1
)

REM reject obvious placeholders
echo !OPENAI_API_KEY! | findstr /b /c:"sk-" >nul
if errorlevel 1 goto :BadKey
if /i "!OPENAI_API_KEY!"=="sk-xxxx" goto :BadKey

echo [CHECK] Key loaded: !OPENAI_API_KEY:~0,7!****!OPENAI_API_KEY:~-4!
echo.

REM --- port check: fail loudly instead of silently switching ports ---
call :CheckPort %BACKEND_PORT%
if errorlevel 1 goto :PortBusy
call :CheckPort %FRONTEND_PORT%
if errorlevel 1 goto :PortBusy

REM --- build first, then run the exe ---
REM  "go run" leaves the child process CWD inside the go build temp dir,
REM  which made the telemetry file fail with "Access is denied" and the
REM  process exited instantly (symptom: blank page after clicking start).
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

REM --- DeepSeek settings ---
REM  LLM_THINKING=disabled is REQUIRED. Thinking models write the answer into
REM  reasoning_content while content stays null; this service reads content
REM  only, so disabling thinking is what makes the text show up.
REM  Model names change over time. If you get 404, check GET /models first.
set "OPENAI_BASE_URL=https://api.deepseek.com"
set "MODEL_CHEAP=deepseek-flash"
set "MODEL_STRONG=deepseek-v4-pro"
set "LLM_THINKING=disabled"
set "PORT=%BACKEND_PORT%"
set "TELEMETRY_FILE=%ROOT%\server\telemetry.jsonl"
set "DECISIONS_FILE=%ROOT%\server\decisions.json"

echo [2/3] Starting backend (REAL LLM :%BACKEND_PORT%)...
start "debate-backend-real" cmd /k ""%EXE%""

echo       Waiting for backend to become ready...
set "READY="
for /l %%i in (1,1,25) do (
    if not defined READY (
        call :Nap
        REM Readiness is checked with netstat, NOT with curl:
        REM on this machine curl.exe exits 0 even when the port is closed
        REM (verified: curl to port 9, nothing listening, exit code 0), so a
        REM curl-based probe reports "backend is up" when it never started.
        REM netstat is the only readiness signal here that actually lies not.
        call :CheckListening %BACKEND_PORT%
        if not errorlevel 1 set "READY=1"
    )
)
if defined READY (
    echo       Backend is up.
) else (
    echo.
    echo [WARN] Backend did not respond within 25s.
    echo        Check the window titled "debate-backend-real" for errors.
    echo.
)
echo.

echo [3/3] Starting frontend (:%FRONTEND_PORT%)...
start "debate-frontend" cmd /k "cd /d "%ROOT%\web" && npm run dev"

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
echo   Real model: one debate takes 1-3 minutes, costs ~CNY 0.05.
echo   Keep both black windows open.
echo ========================================
echo.
pause
exit /b 0

:BadKey
echo [ERROR] OPENAI_API_KEY in key.env is still a placeholder.
echo.
echo         Get a real key: https://platform.deepseek.com/api_keys
echo.
pause
exit /b 1

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
REM  "Input redirection is not supported" when stdin is redirected.
:Nap
ping -n 2 127.0.0.1 >nul 2>&1
exit /b 0
