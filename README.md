# 决策辩论产品

> record html demo. it is a very first vibe coding item i created.

一个**两难决策辩论产品**：用户提出两难问题，两个 AI 角色（**数据派** vs **生活派**）进行四段式辩论，
最后收敛为可量化计算的**临界点假设** —— 用户拖动滑块就能看到"结论在哪一点翻转"。

**产品定位**：不替用户做决定，而是把"该不该做"的价值观问题，转化成"你能承受多少"的事实判断问题。

---

## 快速开始

```bash
# 后端（Mock 模式，零成本，无需 API Key）
cd server
export GOROOT="C:/Users/Arina/.workbuddy/binaries/go"
export GOPATH="$(pwd)/gopath"
export GOCACHE="$(pwd)/gocache"
"$GOROOT/bin/go.exe" run ./cmd/server

# 前端
cd web
npm install
npm run dev
```

接真实模型见 `P9-真实LLM联调-完成报告.md` 第一节。

---

## 文档从哪里开始读

**先读 [`PROJECT-INDEX.md`](PROJECT-INDEX.md)** —— 它是唯一入口，包含「我要做 X → 该读哪几份文档」的对照表。

| 想了解 | 读 |
|---|---|
| 现在能做什么、状态如何 | `PROJECT-INDEX.md` |
| 创意是怎么形成的、为什么这么设计 | `overview.md` |
| 各阶段做了什么、怎么验收 | `开发计划-分阶段实施.md` |
| 提示词为什么这么写 | `P10-提示词打磨-完成报告.md` |
| 某个功能的设计取舍 | 对应阶段的「完成报告」 |

---

## 技术栈

**后端**：Go（零第三方依赖）· `debate` / `llm` / `orchestrator` / `httpsrv` / `telemetry` / `store`

**前端**：Vite + React 19 + TypeScript + Tailwind + Framer Motion

**模型**：DeepSeek（`deepseek-flash` 廉价档 / `deepseek-v4-pro` 强力档），单场约 ¥0.045 / 40 秒

---

## 目录结构

```
server/
  cmd/server/          # 可执行入口
  internal/
    debate/            # 领域模型 + 提示词（零 I/O）
    llm/               # 模型调用抽象 + Mock
    orchestrator/      # 四段式流程编排
    httpsrv/           # HTTP / SSE 协议层
    telemetry/         # 埋点采集（Emit 永不阻塞）
    store/             # 决策档案持久化
web/
  src/
    lib/               # 纯逻辑：sse / critical / archive / templates / decisionsApi
    components/        # UI 组件
    App.tsx
```

---

## 工作纪律

1. **改代码前先读对应文档**（见 `PROJECT-INDEX.md` 第三节）
2. **门禁全绿才算完成**：Go `gofmt -s -l` / `vet` / `build` / `test -cover`；前端 `typecheck` / `test` / `build`
3. **密钥永不入库** —— 一律走环境变量
4. **约定由代码兜住，提示词只做提示** —— 涉及 LLM 结构化输出时，入口必须容错

---

## License

MIT
