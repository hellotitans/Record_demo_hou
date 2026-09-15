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

func main() {
	port := env("PORT", "8080")
	apiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := env("OPENAI_BASE_URL", "https://api.openai.com/v1")
	cheap := env("MODEL_CHEAP", "mock-cheap")
	strong := env("MODEL_STRONG", "mock-strong")
	// 思考型模型必须显式禁用推理输出，否则正文会写进 reasoning_content，
	// 本服务只读 content，结果是整场辩论拿到空响应。默认禁用正是为此。
	thinking := env("LLM_THINKING", "disabled")
	telemetryPath := env("TELEMETRY_FILE", "telemetry.jsonl")
	decisionsPath := env("DECISIONS_FILE", "decisions.json")
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
