#!/usr/bin/env bash
# 一键启动「决策辩论产品」—— Mock 模式（零成本，无需 API Key）
#
# 用途：本地试玩界面与交互，不联网、不花钱。辩论内容是 Mock 生成的
#       占位文本，用来验证流程和 UI，不是真实论证。
#
# 用法：在 Git Bash 里执行
#     bash start-mock.sh
# 然后浏览器打开 http://localhost:5173/

set -e
# 注意：git-bash 下 pwd 返回 /c/Users/... 形式，Go 不认（会报
# "GOPATH entry is relative"）。必须转成 C:/Users/... 的盘符形式。
ROOT_RAW="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$ROOT_RAW" && pwd -W 2>/dev/null || echo "$ROOT_RAW" | sed 's|^/\([a-zA-Z]\)/|\1:/|')"

export GOROOT="C:/Users/Arina/.workbuddy/binaries/go"
export GOPATH="$ROOT/server/gopath"
export GOCACHE="$ROOT/server/gocache"
GO="$GOROOT/bin/go.exe"

# 清掉可能残留的 Key，确保走 Mock 分支
unset OPENAI_API_KEY

echo "==> 启动后端（Mock 模式，:8080）"
cd "$ROOT/server"
"$GO" run ./cmd/server &
BACKEND_PID=$!

sleep 3
if ! curl -s --noproxy '*' http://127.0.0.1:8080/healthz | grep -q ok; then
  echo "!! 后端启动失败，请检查上方日志"
  kill $BACKEND_PID 2>/dev/null || true
  exit 1
fi
echo "    后端就绪 ✓"

echo "==> 启动前端（Vite，:5173）"
cd "$ROOT/web"
npm run dev &
FRONTEND_PID=$!

cat <<'TIP'

────────────────────────────────────────────────
  浏览器打开：http://localhost:5173/

  注意：请用 localhost，不要用 127.0.0.1 ——
  本机 Vite 只监听 IPv6 的 [::1]，用 127.0.0.1 会连不上。

  按 Ctrl+C 停止全部服务。
────────────────────────────────────────────────
TIP

trap 'echo; echo "==> 正在停止..."; kill $BACKEND_PID $FRONTEND_PID 2>/dev/null || true; exit 0' INT TERM
wait
