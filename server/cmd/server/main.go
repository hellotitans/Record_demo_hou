// Command server 启动决策辩论服务。
//
// 设计原则（与分阶段计划一致）：
//   - 配置全部走环境变量，API Key 绝不进代码。
//   - 未配置 API Key 时自动用 Mock 模型启动，本地零成本即可跑通整场。
//   - 埋点异步落盘为 JSON Lines，永不阻塞主流程；SIGINT 时刷完再退出。
//   - 决策档案落盘为单文件 JSON（P7），跨设备可读同一份"你后悔吗"。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/yourorg/decision-debate/internal/httpsrv"
	"github.com/yourorg/decision-debate/internal/llm"
	"github.com/yourorg/decision-debate/internal/orchestrator"
	"github.com/yourorg/decision-debate/internal/store"
	"github.com/yourorg/decision-debate/internal/telemetry"
)

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// resolveDataPath 把数据文件路径锚定到「可执行文件所在目录」，而不是进程的当前工作目录。
//
// 为什么必须这样：埋点与决策档案默认用相对路径（telemetry.jsonl / decisions.json），
// 而相对路径是相对 CWD 解析的。CWD 在不同启动方式下并不一致 ——
// `go run` 会先把源码编译到临时目录再生效，子进程的 CWD 可能是用户的 shell 目录，
// 也可能落在一个不可写的构建临时目录里。2026-09-16 实测：在项目根目录用
// `cmd /k "cd /d %ROOT%\server && go run ./cmd/server"` 启动时，
// os.OpenFile("telemetry.jsonl") 报 "Access is denied"，log.Fatalf 直接让进程秒退，
// 前端因此表现为「点开始辩论后跳空白页」—— 排查成本极高，因为日志一闪而过。
//
// 绝对路径原样返回（用户显式指定时不改写意图）；相对路径则拼到 exe 目录下。
// 用 go run 时 exe 在临时目录，此时回退到 CWD，至少行为可预期。
func resolveDataPath(p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	exe, err := os.Executable()
	if err != nil {
		return p
	}
	dir := filepath.Dir(exe)
	// go run 的临时目录不算「项目目录」，这种情况保持原行为，不要去拼一个随机路径。
	if strings.Contains(dir, "go-build") || dir == os.TempDir() {
		return p
	}
	return filepath.Join(dir, p)
}

// envFloat 解析浮点环境变量。留空或非法值都返回 def，
// 配置错误不该让服务起不来 —— 退回默认值并继续。
func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		log.Printf("[server] %s=%q 不是合法数字，忽略并使用 %v", key, v, def)
		return def
	}
	return f
}

func main() {
	port := env("PORT", "8080")
	apiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := env("OPENAI_BASE_URL", "https://api.openai.com/v1")
	cheap := env("MODEL_CHEAP", "mock-cheap")
	strong := env("MODEL_STRONG", "mock-strong")
	// 思考型模型必须显式禁用推理输出，否则正文会写进 reasoning_content，
	// 本服务只读 content，结果是整场辩论拿到空响应。默认禁用正是为此。
	thinking := env("LLM_THINKING", "disabled")
	telemetryPath := resolveDataPath(env("TELEMETRY_FILE", "telemetry.jsonl"))
	decisionsPath := resolveDataPath(env("DECISIONS_FILE", "decisions.json"))
	heartbeat := 15 * time.Second

	// --- 模型客户端：无 Key 用 Mock，零成本跑通 ---
	var client llm.Client
	if apiKey == "" {
		log.Println("[server] 未配置 OPENAI_API_KEY，使用 Mock 模型（不联网、不花钱）")
		client = llm.NewMockClient()
	} else {
		log.Printf("[server] 使用 OpenAI 兼容模型（%s，thinking=%q）", baseURL, thinking)
		oc := llm.NewOpenAIClient(baseURL, apiKey)
		oc.Thinking = thinking
		client = oc
	}

	orch, err := orchestrator.New(client, orchestrator.Config{
		Router:      llm.Router{Cheap: cheap, Strong: strong},
		TurnTimeout: 90 * time.Second,
		// 定价随模型变，走环境变量而不是硬编码。留空则用包内的 DeepSeek 口径默认值。
		DefaultPrice: orchestrator.Price{
			PromptPerK:     envFloat("PRICE_PROMPT_PER_K", 0),
			CompletionPerK: envFloat("PRICE_COMPLETION_PER_K", 0),
		},
	})
	if err != nil {
		log.Fatalf("[server] 创建编排器失败: %v", err)
	}

	// --- 埋点：JSON Lines 落盘，随进程退出刷完 ---
	f, err := os.OpenFile(telemetryPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatalf("[server] 打开埋点文件失败: %v", err)
	}
	sink := telemetry.NewWriterSink(f)
	col, err := telemetry.NewCollector(sink, telemetry.Config{})
	if err != nil {
		log.Fatalf("[server] 创建埋点收集器失败: %v", err)
	}

	// --- 决策档案：单文件 JSON，跨设备共享同一份"你后悔吗" ---
	// 文件存在但 JSON 损坏时直接启动失败：宁可让人来修，
	// 也不能静默清空历史决策。
	decisions, err := store.NewFileStore(decisionsPath)
	if err != nil {
		log.Fatalf("[server] 打开决策档案失败: %v", err)
	}

	handler := httpsrv.NewDebateHandler(orch, col)
	handler.HeartbeatInterval = heartbeat
	mux := http.NewServeMux()
	mux.Handle("POST /api/debate", handler)
	mux.Handle("GET /api/decisions", httpsrv.NewDecisionsHandler(decisions))
	mux.Handle("POST /api/decisions", httpsrv.NewDecisionsHandler(decisions))
	mux.Handle("POST /api/decisions/{id}/followup", httpsrv.NewDecisionFollowupHandler(decisions))
	mux.Handle("GET /api/decisions/stats", httpsrv.NewStatsHandler(decisions))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{
		Addr:              ":" + port,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// --- 优雅关闭：SIGINT/SIGTERM 时停止接收、刷完埋点再退出 ---
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		log.Println("[server] 收到退出信号，正在关闭……")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	log.Printf("[server] 监听 :%s，埋点落盘 %s，决策档案落盘 %s",
		port, filepath.Clean(telemetryPath), filepath.Clean(decisionsPath))
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[server] 服务异常退出: %v", err)
	}

	// 服务关闭后才到这里：刷完剩余埋点（Close 会等待后台 goroutine 退出）。
	if err := col.Close(); err != nil {
		log.Printf("[server] 关闭埋点收集器出错: %v", err)
	}
	log.Println("[server] 已退出")
}
