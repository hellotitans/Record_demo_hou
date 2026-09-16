#!/usr/bin/env bash
# 一键启动「决策辩论产品」—— 真实 LLM 模式（DeepSeek）
#
# 用法：
#   1. 复制 key.env.example 为 key.env，填入你的 DeepSeek Key
#   2. 在 Git Bash 里执行：bash start-real.sh
#   3. 浏览器打开 http://localhost:5173/
#
# 成本参考：单场辩论约 ¥0.046（实测 2026-09-16），约 45~60 秒。
#           DeepSeek 充值 ¥10 大约能跑 200 场。
#
# 设计说明：Key 存在独立的 key.env 里，该文件已被 .gitignore 排除。
#           本脚本本身不含任何密钥，可安全提交。

set -e
# 注意：git-bash 下 pwd 返回 /c/Users/... 形式，Go 不认（会报
# "GOPATH entry is relative"）。必须转成 C:/Users/... 的盘符形式。
ROOT_RAW="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$ROOT_RAW" && pwd -W 2>/dev/null || echo "$ROOT_RAW" | sed 's|^/\([a-zA-Z]\)/|\1:/|')"
KEYFILE="$ROOT/key.env"

if [ -z "$OPENAI_API_KEY" ]; then
  if [ ! -f "$KEYFILE" ]; then
    cat <<MSG
!! 没找到密钥文件：$KEYFILE

请先执行：

    cp key.env.example key.env
    # 然后编辑 key.env，把 sk-xxxx 换成你的真实 Key

（key.env 已在 .gitignore 中，不会被提交）
MSG
    exit 1
  fi
  # shellcheck disable=SC1090
  source "$KEYFILE"
fi

if [ -z "$OPENAI_API_KEY" ]; then
  echo "!! key.env 里的 OPENAI_API_KEY 为空，请填写后重试"
  exit 1
fi

export OPENAI_BASE_URL="https://api.deepseek.com"
export MODEL_CHEAP="deepseek-flash"      # 立论 + 主持人
export MODEL_STRONG="deepseek-v4-pro"    # 质询 / 承认反击 / 总结 / 假设提取
export LLM_THINKING="disabled"           # 必须：思考型模型要显式关推理输出

export GOROOT="C:/Users/Arina/.workbuddy/binaries/go"
export GOPATH="$ROOT/server/gopath"
export GOCACHE="$ROOT/server/gocache"
GO="$GOROOT/bin/go.exe"

echo "==> 启动后端（真实模型 DeepSeek，:8080）"
cd "$ROOT/server"
"$GO" run ./cmd/server &
BACKEND_PID=$!

sleep 3
if ! curl -s --noproxy '*' http://127.0.0.1:8080/healthz | grep -q ok; then
  echo "!! 后端启动失败，请检查上方日志"
  kill $BACKEND_PID 2>/dev/null || true
  exit 1
fi
echo "    后端就绪 ✓（将调用真实模型并产生费用）"

echo "==> 启动前端（Vite，:5173）"
cd "$ROOT/web"
npm run dev &
FRONTEND_PID=$!

cat <<'TIP'

────────────────────────────────────────────────
  浏览器打开：http://localhost:5173/

  请用 localhost，不要用 127.0.0.1
  （本机 Vite 只监听 IPv6 的 [::1]）

  首次跑一场约 45~60 秒，请耐心等。
  按 Ctrl+C 停止全部服务。
────────────────────────────────────────────────
TIP

trap 'echo; echo "==> 正在停止..."; kill $BACKEND_PID $FRONTEND_PID 2>/dev/null || true; exit 0' INT TERM
wait
