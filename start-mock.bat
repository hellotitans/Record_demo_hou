@echo off
chcp 65001 >nul
setlocal EnableDelayedExpansion

REM ══════════════════════════════════════════════════════════
REM  一键启动「决策辩论产品」—— Mock 模式（零成本，无需 Key）
REM
REM  用法：直接双击本文件即可（不要用 bash 跑，会走 WSL 认不出 C:/ 路径）
REM
REM  为什么用「先编译再运行」而不是 go run：
REM    go run 会让子进程的工作目录落到构建临时目录，
REM    导致埋点文件 telemetry.jsonl 因路径不可写而打开失败，
REM    进程秒退 —— 前端表现为「点开始辩论跳空白页」。
REM    编译成 exe 后工作目录完全可控，已实测通过。
REM ══════════════════════════════════════════════════════════

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
echo   决策辩论产品 - 启动（Mock 模式）
echo ========================================
echo.

REM --- 前置检查 ---
if not exist "%GO%" (
    echo [错误] 找不到 go.exe：
    echo        %GO%
    echo.
    pause
    exit /b 1
)
if not exist "%ROOT%\server\go.mod" (
    echo [错误] 找不到 server\go.mod，请确认本文件放在项目根目录。
    echo        当前推断的根目录：%ROOT%
    echo.
    pause
    exit /b 1
)
if not exist "%ROOT%\web\package.json" (
    echo [错误] 找不到 web\package.json，请确认本文件放在项目根目录。
    echo.
    pause
    exit /b 1
)

REM --- 端口检查：占用时明确报错，绝不静默换端口 ---
REM  曾出现过旧前端残留占用 5173，Vite 自动改到 5174，
REM  用户仍访问 5173 看到的是一个「半死」服务，误判为空白页 bug。
call :CheckPort %BACKEND_PORT%  "后端"
if errorlevel 1 goto :PortBusy
call :CheckPort %FRONTEND_PORT% "前端"
if errorlevel 1 goto :PortBusy

REM --- 编译后端（比 go run 更可靠，且能提前暴露编译错误）---
echo [1/3] 编译后端...
cd /d "%ROOT%\server"
"%GO%" build -o "%EXE%" ./cmd/server
if errorlevel 1 (
    echo.
    echo [错误] 后端编译失败，请把上面的报错发给开发者。
    echo.
    pause
    exit /b 1
)
echo       完成：%EXE%
echo.

REM --- 确保走 Mock 分支 ---
set "OPENAI_API_KEY="
set "PORT=%BACKEND_PORT%"
REM 数据文件用绝对路径，双保险（程序自身也会锚定到 exe 目录）
set "TELEMETRY_FILE=%ROOT%\server\telemetry.jsonl"
set "DECISIONS_FILE=%ROOT%\server\decisions.json"

echo [2/3] 启动后端（Mock :%BACKEND_PORT%，不联网、不花钱）...
start "debate-backend-mock" cmd /k ""%EXE%""

REM 等后端就绪，最多 20 秒
echo       等后端就绪...
set "READY="
for /l %%i in (1,1,20) do (
    if not defined READY (
        timeout /t 1 /nobreak >nul
        curl -s -o nul http://127.0.0.1:%BACKEND_PORT%/healthz && set "READY=1"
    )
)
if not defined READY (
    echo.
    echo [警告] 后端 20 秒内未就绪。请查看标题为
    echo        "debate-backend-mock" 的窗口里的报错信息。
    echo.
)
echo.

echo [3/3] 启动前端（:%FRONTEND_PORT%）...
start "debate-frontend" cmd /k "cd /d "%ROOT%\web" && npm run dev"

echo.
echo ========================================
echo   等约 10 秒，浏览器打开：
echo       http://localhost:%FRONTEND_PORT%/
echo.
echo   必须用 localhost，不要用 127.0.0.1
echo   （本机 Vite 只监听 IPv6，127.0.0.1 会连不上）
echo.
echo   Mock 模式：辩论内容是占位文本，
echo   只用来预览界面，不是真实论证。
echo   想看真实辩论请双击 start-real.bat
echo.
echo   两个黑窗口不要关，关掉就等于停止服务。
echo   用完直接关窗口即可。
echo ========================================
echo.
pause
exit /b 0

:PortBusy
echo.
echo [错误] 端口已被占用，无法启动。
echo.
echo   解决办法：
echo     1. 先关闭所有标题含 "debate" 的黑窗口
echo     2. 若仍占用，在命令行执行：
echo          netstat -ano ^| findstr ":%BACKEND_PORT% :%FRONTEND_PORT%"
echo        再用 taskkill /PID ^<进程号^> /F 结束它
echo     3. 或用别的端口：右键编辑本文件，改 BACKEND_PORT / FRONTEND_PORT
echo        （改完前端也要同步改 web\vite.config.ts 里的代理目标）
echo.
pause
exit /b 1

REM --- 子过程：检查端口是否已被监听 ---
REM  用 netstat 而非 curl：curl 对「有监听但不健康」的服务会误判为可用。
:CheckPort
set "BUSY="
for /f "tokens=*" %%L in ('netstat -ano ^| findstr "LISTENING" ^| findstr ":%~1 "') do set "BUSY=1"
if defined BUSY (
    echo [错误] 端口 %~1（%~2）已被占用。
    exit /b 1
)
exit /b 0
