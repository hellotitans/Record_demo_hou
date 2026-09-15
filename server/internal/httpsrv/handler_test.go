package httpsrv

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/yourorg/decision-debate/internal/debate"
	"github.com/yourorg/decision-debate/internal/llm"
	"github.com/yourorg/decision-debate/internal/orchestrator"
	"github.com/yourorg/decision-debate/internal/telemetry"
)

// captureSink 收集事件供断言，是测试埋点自动上报的关键。
type captureSink struct {
	mu     sync.Mutex
	events []telemetry.Event
}

func (s *captureSink) Write(_ context.Context, evs []telemetry.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, evs...)
	return nil
}

func (s *captureSink) Close() error { return nil }

func (s *captureSink) count(name telemetry.EventName) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.events {
		if e.EventName == name {
			n++
		}
	}
	return n
}

// newMock 与编排器测试同款：产出合法结构化输出，四段式流程可完整跑通。
func newMock() *llm.MockClient {
	return &llm.MockClient{Handler: func(req llm.Request) string {
		um := lastUserMsg(req.Messages)
		switch {
		case strings.Contains(um, "你需要为一场决策辩论分配角色"):
			return `{"data":{"option_id":"a","reason":"买房涉及大量可量化财务变量"},"life":{"option_id":"b","reason":"租房关乎生活方式"}}`
		case strings.Contains(um, "你是这场辩论的主持人"):
			return `{"unresolved":["口径不一致"],"repeated":[]}`
		case strings.Contains(um, "提取双方各自暴露的关键假设"):
			return `[{"side":"data","statement":"房价年涨幅不低于3%","variable":"房价年涨幅","operator":">=","value":3,"unit":"%"}]`
		default:
			return "一方发言内容。"
		}
	}}
}

func lastUserMsg(msgs []llm.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

func newTestOrchestrator(t *testing.T) *orchestrator.Orchestrator {
	t.Helper()
	o, err := orchestrator.New(newMock(), orchestrator.Config{Router: llm.Router{Cheap: "cheap", Strong: "strong"}})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func sampleBody(t *testing.T) []byte {
	t.Helper()
	d := debate.Dilemma{ID: "d1", Question: "买房还是租房", Options: [2]debate.Option{{ID: "a", Label: "买"}, {ID: "b", Label: "租"}}}
	b, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestDebateHandlerFullFlow 验证端到端：一次 POST 收完整帧序列，且埋点自动上报。
func TestDebateHandlerFullFlow(t *testing.T) {
	sink := &captureSink{}
	col, err := telemetry.NewCollector(sink, telemetry.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	h := NewDebateHandler(newTestOrchestrator(t), col)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(sampleBody(t)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	// 显式关闭收集器，把缓冲区里待刷盘的事件落进 sink，再断言埋点。
	col.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, 期望 200；body=%s", resp.StatusCode, raw)
	}

	frames := parseSSE(string(raw))
	if len(frames) == 0 {
		t.Fatal("未解析到任何 SSE 帧")
	}
	if frames[0].event != string(debate.FrameRoleAssign) {
		t.Errorf("首帧=%q, 期望 role_assign", frames[0].event)
	}
	if frames[len(frames)-1].event != string(debate.FrameDone) {
		t.Errorf("尾帧=%q, 期望 done", frames[len(frames)-1].event)
	}

	var (
		turnStart, turnEnd, moderator, assumption, delta int
	)
	for _, f := range frames {
		switch debate.FrameKind(f.event) {
		case debate.FrameTurnStart:
			turnStart++
		case debate.FrameTurnEnd:
			turnEnd++
		case debate.FrameDelta:
			delta++
		case debate.FrameModerator:
			moderator++
		case debate.FrameAssumption:
			assumption++
		}
	}
	if turnStart != 8 || turnEnd != 8 {
		t.Errorf("turn_start=%d turn_end=%d, 期望 8/8", turnStart, turnEnd)
	}
	if delta < 8 {
		t.Errorf("delta 帧=%d, 期望至少 8（每轮至少一个分块）", delta)
	}
	if moderator != 3 {
		t.Errorf("moderator 帧=%d, 期望 3", moderator)
	}
	if assumption != 1 {
		t.Errorf("assumption 帧=%d, 期望 1", assumption)
	}

	// 埋点自动上报（随 Close 落盘）。
	if sink.count(telemetry.EventDebateStarted) < 1 {
		t.Error("缺少 debate_started 埋点")
	}
	if sink.count(telemetry.EventRoundDelivered) != 8 {
		t.Errorf("round_delivered=%d, 期望 8（逐轮到达埋点）", sink.count(telemetry.EventRoundDelivered))
	}
	if sink.count(telemetry.EventLLMCallCompleted) != 8 {
		t.Errorf("llm_call_completed=%d, 期望 8（成本埋点）", sink.count(telemetry.EventLLMCallCompleted))
	}
	if sink.count(telemetry.EventDebateCostTotal) != 1 {
		t.Errorf("debate_cost_total=%d, 期望 1", sink.count(telemetry.EventDebateCostTotal))
	}
}

// TestDebateHandlerBodyTooLarge 验证请求体超限返回 413。
func TestDebateHandlerBodyTooLarge(t *testing.T) {
	h := NewDebateHandler(newTestOrchestrator(t), nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	big := make([]byte, MaxDilemmaBytes+1024)
	for i := range big {
		big[i] = 'a'
	}
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(big))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status=%d, 期望 413", resp.StatusCode)
	}
}

// TestDebateHandlerInvalidDilemma 验证非法辩题返回 400。
func TestDebateHandlerInvalidDilemma(t *testing.T) {
	h := NewDebateHandler(newTestOrchestrator(t), nil)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// 缺 question 与 options，Validate 必失败。
	body, _ := json.Marshal(debate.Dilemma{ID: "d1"})
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status=%d, 期望 400", resp.StatusCode)
	}
}

// failingSink 的 Write 永远报错，用于验证"埋点失败绝不影响主流程"。
type failingSink struct{}

func (failingSink) Write(_ context.Context, _ []telemetry.Event) error { return io.EOF }
func (failingSink) Close() error                                       { return nil }

// TestDebateHandlerTelemetryFailureIgnored 验证即使埋点 Sink 持续报错，
// 辩论依然完整流式推送给前端，不卡主流程。
func TestDebateHandlerTelemetryFailureIgnored(t *testing.T) {
	col, err := telemetry.NewCollector(failingSink{}, telemetry.Config{})
	if err != nil {
		t.Fatal(err)
	}
	defer col.Close()

	h := NewDebateHandler(newTestOrchestrator(t), col)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(sampleBody(t)))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("埋点失败不应影响主流程，status=%d", resp.StatusCode)
	}
	frames := parseSSE(string(raw))
	var turnEnd int
	for _, f := range frames {
		if debate.FrameKind(f.event) == debate.FrameTurnEnd {
			turnEnd++
		}
	}
	if turnEnd != 8 {
		t.Errorf("埋点失败下完整轮次应为 8，实际 %d", turnEnd)
	}
}

// parseSSE 把 SSE 文本解析为帧序列。心跳帧（以 ":" 开头的注释）被忽略。
func parseSSE(raw string) []sseFrame {
	var out []sseFrame
	for _, block := range strings.Split(raw, "\n\n") {
		var f sseFrame
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "event:"):
				f.event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				f.data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			}
		}
		if f.event != "" {
			out = append(out, f)
		}
	}
	return out
}

type sseFrame struct {
	event string
	data  string
}
