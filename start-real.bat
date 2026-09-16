@echo off
chcp 65001 >nul
setlocal EnableDelayedExpansion

REM ══════════════════════════════════════════════════════════
REM  一键启动「决策辩论产品」—— 真实 LLM 模式（会调用 API，产生费用）
REM
REM  前提：key.env 里已填好 OPENAI_API_KEY
REM  用法：直接双击本文件（不要用 bash 跑，会走 WSL 认不出 C:/ 路径）
REM
REM  费用参考：一场完整辩论（四轮 8 段发言）约 ¥0.03~0.05
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
set "KEYFILE=%ROOT%\key.env"

echo.
echo ========================================
echo   决策辩论产品 - 启动（真实 LLM 模式）
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
if not exist "%KEYFILE%" (
    echo [错误] 找不到 key.env
    echo.
    echo        请在本文件同目录新建 key.env，内容一行：
    echo            OPENAI_API_KEY="sk-你的真实Key"
    echo.
    echo        获取 Key：https://platform.deepseek.com/api_keys
    echo.
    pause
    exit /b 1
)

REM --- 读取 Key：取第一个以 OPENAI_API_KEY 开头的非注释行 ---
set "OPENAI_API_KEY="
for /f "usebackq tokens=1,* delims==" %%a in ("%KEYFILE%") do (
    if not defined OPENAI_API_KEY (
        echo %%a | findstr /b /c:"#" >nul || (
            for /f "tokens=* delims= " %%k in ("%%a") do (
                if /i "%%k"=="OPENAI_API_KEY" set "OPENAI_API_KEY=%%b"
            )
        )
    )
)
REM 去掉值两侧的引号与空格
if defined OPENAI_API_KEY (
    set "OPENAI_API_KEY=!OPENAI_API_KEY:"=!"
    for /f "tokens=* delims= " %%v in ("!OPENAI_API_KEY!") do set "OPENAI_API_KEY=%%v"
)

if not defined OPENAI_API_KEY (
    echo [错误] key.env 里没有解析到 OPENAI_API_KEY。
    echo        正确格式（引号是要的，等号两边不要空格）：
    echo            OPENAI_API_KEY="sk-xxxxxxxx"
    echo.
    pause
    exit /b 1
)
if /i "!OPENAI_API_KEY!"=="sk-xxxx" goto :BadKey
if /i "!OPENAI_API_KEY!"=="sk-你的真实Key" goto :BadKey
echo !OPENAI_API_KEY! | findstr /b /c:"sk-" >nul
if errorlevel 1 goto :BadKey

echo [检查] Key 已读取：!OPENAI_API_KEY:~0,7!****!OPENAI_API_KEY:~-4!
echo.

REM --- 端口检查：占用时明确报错，绝不静默换端口 ---
call :CheckPort %BACKEND_PORT%  "后端"
if errorlevel 1 goto :PortBusy
call :CheckPort %FRONTEND_PORT% "前端"
if errorlevel 1 goto :PortBusy

REM --- 编译后端 ---
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

REM --- DeepSeek 配置 ---
REM  LLM_THINKING=disabled 必须保留：DeepSeek 的思考型模型默认把正文写进
REM  reasoning_content，本服务只读 content，关掉推理才有正文。
REM  模型名会换代，若报 404 请先查 GET /models 再改这两行。
set "OPENAI_BASE_URL=https://api.deepseek.com"
set "MODEL_CHEAP=deepseek-flash"
set "MODEL_STRONG=deepseek-v4-pro"
set "LLM_THINKING=disabled"
set "PORT=%BACKEND_PORT%"
set "TELEMETRY_FILE=%ROOT%\server\telemetry.jsonl"
set "DECISIONS_FILE=%ROOT%\server\decisions.json"

echo [2/3] 启动后端（真实模型 :%BACKEND_PORT%，会调用 API）...
start "debate-backend-real" cmd /k ""%EXE%""

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
    echo        "debate-backend-real" 的窗口里的报错信息。
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
echo   现在是真实模型，一场辩论约 ¥0.03~0.05，耗时 1~3 分钟。
echo   两个黑窗口不要关，关掉就等于停止服务。
echo ========================================
echo.
pause
exit /b 0

:BadKey
echo [错误] key.env 里的 OPENAI_API_KEY 还是占位符，请替换成真实 Key。
echo.
echo        获取 Key：https://platform.deepseek.com/api_keys
echo.
pause
exit /b 1

:PortBusy
echo.
echo [错误] 端口已被占用，无法启动。
echo.
echo   解决办法：
echo     1. 先关闭所有标题含 "debate" 的黑窗口
echo     2. 若仍占用，在命令行执行：
echo          netstat -ano ^| findstr ":%BACKEND_PORT% :%FRONTEND_PORT%"
echo        再用 taskkill /PID ^<进程号^> /F 结束它
echo.
pause
exit /b 1

:CheckPort
set "BUSY="
for /f "tokens=*" %%L in ('netstat -ano ^| findstr "LISTENING" ^| findstr ":%~1 "') do set "BUSY=1"
if defined BUSY (
    echo [错误] 端口 %~1（%~2）已被占用。
    exit /b 1
)
exit /b 0
