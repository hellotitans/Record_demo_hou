package httpsrv

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/yourorg/decision-debate/internal/debate"
	"github.com/yourorg/decision-debate/internal/orchestrator"
	"github.com/yourorg/decision-debate/internal/telemetry"
)

// MaxDilemmaBytes 限制辩题请求体大小。
const MaxDilemmaBytes = 1 << 16 // 64 KiB

// DebateHandler 处理开辩请求，把编排过程以 SSE 流式推送给前端。
type DebateHandler struct {
	orch      *orchestrator.Orchestrator
	telemetry *telemetry.Collector

	// HeartbeatInterval 心跳间隔。辩论中途可能有几十秒无数据，
	// 没有心跳反向代理会掐断连接。
	HeartbeatInterval time.Duration
}

// NewDebateHandler 创建处理器。telemetry 可以为 nil（例如本地调试）。
func NewDebateHandler(orch *orchestrator.Orchestrator, tc *telemetry.Collector) *DebateHandler {
	return &DebateHandler{orch: orch, telemetry: tc}
}

func (h *DebateHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, MaxDilemmaBytes+1))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > MaxDilemmaBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	var d debate.Dilemma
	if err := json.Unmarshal(body, &d); err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := d.Validate(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// SessionID 缺失时回退到 Dilemma ID，保证埋点事件永远可归属。
	if d.SessionID == "" {
		d.SessionID = d.ID
	}

	stream, err := NewStream(w)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	go stream.Heartbeat(ctx, h.HeartbeatInterval)

	h.trackStart(d)

	result, runErr := h.orch.Run(ctx, d, h.emitter(ctx, d, stream))

	h.trackCost(d, result)

	if runErr != nil {
		// 失败时把已完成的轮次一起推给前端：用户已经等了很久，
		// 拿回半场辩论也比看到一个空白页好。
		_ = stream.Frame(debate.Frame{
			Kind: debate.FrameError,
			Text: runErr.Error(),
			Data: result,
		})
	}
}

// emitter 把编排帧同时推给前端和埋点。
func (h *DebateHandler) emitter(ctx context.Context, d debate.Dilemma, s *Stream) orchestrator.EmitFunc {
	return func(f debate.Frame) error {
		if h.telemetry != nil {
			switch f.Kind {
			case debate.FrameTurnEnd:
				h.trackTurn(d, f)
			case debate.FrameModerator:
				h.trackModerator(d, f)
			}
		}
		if err := s.Frame(f); err != nil {
			return err
		}
		return ctx.Err()
	}
}

func (h *DebateHandler) trackStart(d debate.Dilemma) {
	if h.telemetry == nil {
		return
	}
	_ = h.telemetry.Emit(telemetry.NewEvent(
		telemetry.EventDebateStarted, telemetry.ZoneDecision, d.SessionID, d.ID,
		telemetry.MustProperties(map[string]any{"category": d.Category}),
	))
}

func (h *DebateHandler) trackTurn(d debate.Dilemma, f debate.Frame) {
	t, ok := f.Data.(debate.Turn)
	if !ok {
		return
	}
	// Layer 1：逐轮到达情况。配合前端的 round_read（用户是否真的读完），
	// 就能算出"哪一轮用户划走了" —— 这是辩论质量最直接的体温计。
	_ = h.telemetry.Emit(telemetry.NewEvent(
		telemetry.EventRoundDelivered, telemetry.ZoneDecision, d.SessionID, d.ID,
		telemetry.MustProperties(telemetry.RoundDeliveredProps{
			Round:      int(t.Round),
			Side:       string(t.Side),
			Model:      t.Model,
			LatencyMs:  t.LatencyMs,
			CharCount:  len([]rune(t.Content)),
			PromptTok:  t.PromptTokens,
			Completion: t.CompletionTokens,
		}),
	))
	// Layer 4：成本。把 Model 与前端的辩论有用度评分关联，
	// 就能验证"混合模型策略"是否真的省钱不伤质量。
	_ = h.telemetry.Emit(telemetry.NewEvent(
		telemetry.EventLLMCallCompleted, telemetry.ZoneDecision, d.SessionID, d.ID,
		telemetry.MustProperties(telemetry.LLMCostProps{
			Round:            int(t.Round),
			Role:             string(t.Side),
			Model:            t.Model,
			LatencyMs:        t.LatencyMs,
			PromptTokens:     t.PromptTokens,
			CompletionTokens: t.CompletionTokens,
		}),
	))
}

func (h *DebateHandler) trackModerator(d debate.Dilemma, f debate.Frame) {
	note, ok := f.Data.(*debate.ModeratorNote)
	if !ok || note == nil {
		return
	}
	// 模型侧预判的重复论点，与前端 argument_feedback.verdict=repetitive 互为印证。
	if len(note.Repeated) == 0 {
		return
	}
	_ = h.telemetry.Emit(telemetry.NewEvent(
		telemetry.EventModeratorRepeated, telemetry.ZoneDecision, d.SessionID, d.ID,
		telemetry.MustProperties(map[string]any{
			"round":    int(note.Round),
			"repeated": note.Repeated,
		}),
	))
}

func (h *DebateHandler) trackCost(d debate.Dilemma, r *debate.Result) {
	if h.telemetry == nil || r == nil {
		return
	}
	_ = h.telemetry.Emit(telemetry.NewEvent(
		telemetry.EventDebateCostTotal, telemetry.ZoneDecision, d.SessionID, d.ID,
		telemetry.MustProperties(telemetry.DebateCostTotalProps{
			TotalCost:      r.Usage.EstimatedCostCNY,
			TotalLatencyMs: r.Usage.TotalLatencyMs,
			ModelMix:       modelMix(r.Turns),
		}),
	))
}

func modelMix(turns []debate.Turn) map[string]int {
	mix := map[string]int{}
	for _, t := range turns {
		if t.Model != "" {
			mix[t.Model]++
		}
	}
	return mix
}
