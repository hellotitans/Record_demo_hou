package orchestrator

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yourorg/decision-debate/internal/debate"
	"github.com/yourorg/decision-debate/internal/llm"
)

// newMock 构造一个能产出合法结构化输出（角色分配/主持人/假设）的 Mock，
// 让四段式流程能完整跑通。各方发言带可区分的标记，便于断言"后手看见先手"。
func newMock() *llm.MockClient {
	return &llm.MockClient{Handler: func(req llm.Request) string {
		um := lastUserMsg(req.Messages)
		switch {
		case strings.Contains(um, "你需要为一场决策辩论分配角色"):
			return `{"data":{"option_id":"a","reason":"买房涉及大量可量化财务变量"},"life":{"option_id":"b","reason":"租房关乎生活方式与心安"}}`
		case strings.Contains(um, "你是这场辩论的主持人"):
			return `{"unresolved":["双方在首付压力的量化口径上不一致"],"repeated":[]}`
		case strings.Contains(um, "提取双方各自暴露的关键假设"):
			return `[{"side":"data","statement":"房价年涨幅不低于3%","variable":"房价年涨幅","operator":">=","value":3,"unit":"%"}]`
		default:
			// 普通轮次发言：按所代表的选项给可区分文本。
			if strings.Contains(um, "你代表：买") {
				return "买房派的发言：租金是纯支出，买房是把支出变成资产。"
			}
			return "租房派的发言：买房要背二三十年贷款，自由现金流更重要。"
		}
	}}
}

// chineseSideMock 模拟真实模型的"不守规矩"：提示词示例写的是 "data"，
// 但整场辩论发言标签都是中文，模型照抄成 "数据派"。
func chineseSideMock() *llm.MockClient {
	return &llm.MockClient{Handler: func(req llm.Request) string {
		um := lastUserMsg(req.Messages)
		switch {
		case strings.Contains(um, "你需要为一场决策辩论分配角色"):
			return `{"data":{"option_id":"a","reason":"r"},"life":{"option_id":"b","reason":"r"}}`
		case strings.Contains(um, "你是这场辩论的主持人"):
			return `{"unresolved":["x"],"repeated":[]}`
		case strings.Contains(um, "提取双方各自暴露的关键假设"):
			return `[{"side":"数据派","statement":"房价年涨幅不低于3%","variable":"房价年涨幅","operator":">=","value":3,"unit":"%"},` +
				`{"side":"生活派","statement":"通勤时间不超过1小时","variable":"通勤时间","operator":"<=","value":1,"unit":"小时"},` +
				`{"side":"路人甲","statement":"这条应被丢弃","variable":"x","operator":">","value":1,"unit":"次"}]`
		default:
			return "发言内容"
		}
	}}
}

// TestAssumptionsAcceptChineseSide 锁定真实联调暴露的缺陷：
// 模型输出中文角色名时不能被静默丢弃，否则前端临界点计算器会是空的。
func TestAssumptionsAcceptChineseSide(t *testing.T) {
	o, err := New(chineseSideMock(), Config{Router: llm.Router{Cheap: "c", Strong: "s"}})
	if err != nil {
		t.Fatal(err)
	}
	result, err := o.Run(context.Background(), sampleDilemma(), func(debate.Frame) error { return nil })
	if err != nil {
		t.Fatal(err)
	}

	// 中文标签应被归一化并保留；无法识别的角色仍然丢弃。
	if len(result.Assumptions) != 2 {
		t.Fatalf("假设数=%d, 期望 2（'路人甲' 应被丢弃）", len(result.Assumptions))
	}
	if result.Assumptions[0].Side != debate.SideData {
		t.Errorf("第1条 Side=%q, 期望 data", result.Assumptions[0].Side)
	}
	if result.Assumptions[1].Side != debate.SideLife {
		t.Errorf("第2条 Side=%q, 期望 life", result.Assumptions[1].Side)
	}
	// 编号必须连续：丢弃中间项后不能留下空洞。
	if result.Assumptions[0].ID != "asm-1" || result.Assumptions[1].ID != "asm-2" {
		t.Errorf("编号=%q,%q, 期望 asm-1,asm-2", result.Assumptions[0].ID, result.Assumptions[1].ID)
	}
}

func lastUserMsg(msgs []llm.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == llm.RoleUser {
			return msgs[i].Content
		}
	}
	return ""
}

func sampleDilemma() debate.Dilemma {
	return debate.Dilemma{
		ID:       "d1",
		Question: "买房还是租房",
		Options:  [2]debate.Option{{ID: "a", Label: "买"}, {ID: "b", Label: "租"}},
	}
}

// TestRunFullDebate 是 P3 的主验收：Mock 模型下跑完整场，
// 产出 8 个 Turn、帧序列正确、主持人三轮、假设一轮、角色分配一轮。
func TestRunFullDebate(t *testing.T) {
	m := newMock()
	o, err := New(m, Config{Router: llm.Router{Cheap: "cheap", Strong: "strong"}})
	if err != nil {
		t.Fatal(err)
	}

	var frames []debate.Frame
	emit := func(f debate.Frame) error { frames = append(frames, f); return nil }

	result, err := o.Run(context.Background(), sampleDilemma(), emit)
	if err != nil {
		t.Fatalf("整场应当成功，实际 err=%v", err)
	}
	if result == nil {
		t.Fatal("result 不应为 nil")
	}
	// 8 个 Turn：4 轮 × 2 方。
	if len(result.Turns) != 8 {
		t.Fatalf("Turn 数=%d, 期望 8", len(result.Turns))
	}
	// 假设提取成功（结构化 JSON 合法）。
	if len(result.Assumptions) != 1 || result.Assumptions[0].ID != "asm-1" {
		t.Errorf("假设数=%d, 期望 1 且 id=asm-1", len(result.Assumptions))
	}

	// 模型调用次数：角色分配(1) + 8 轮发言 + 主持人(R1/R2/R3 共3) + 假设(1) = 13。
	if got := len(m.Calls()); got != 13 {
		t.Errorf("模型调用数=%d, 期望 13", got)
	}
	// 主持人恰好三轮：R4 之后不再跑主持人，改为提取假设。
	var moderatorCalls int
	for _, c := range m.Calls() {
		if strings.Contains(lastUserMsg(c.Messages), "你是这场辩论的主持人") {
			moderatorCalls++
		}
	}
	if moderatorCalls != 3 {
		t.Errorf("主持人调用数=%d, 期望 3（R4 后不应再跑主持人）", moderatorCalls)
	}

	// 帧序列：首帧 role_assign，尾帧 done。
	if frames[0].Kind != debate.FrameRoleAssign {
		t.Errorf("首帧=%v, 期望 role_assign", frames[0].Kind)
	}
	if frames[len(frames)-1].Kind != debate.FrameDone {
		t.Errorf("尾帧=%v, 期望 done", frames[len(frames)-1].Kind)
	}
	var (
		roleAssign, turnStart, turnEnd, moderator, assumption, done int
	)
	for _, f := range frames {
		switch f.Kind {
		case debate.FrameRoleAssign:
			roleAssign++
		case debate.FrameTurnStart:
			turnStart++
		case debate.FrameTurnEnd:
			turnEnd++
		case debate.FrameModerator:
			moderator++
		case debate.FrameAssumption:
			assumption++
		case debate.FrameDone:
			done++
		}
	}
	if roleAssign != 1 || turnStart != 8 || turnEnd != 8 || moderator != 3 || assumption != 1 || done != 1 {
		t.Errorf("帧计数异常：role=%d turnStart=%d turnEnd=%d mod=%d asm=%d done=%d（期望 1/8/8/3/1/1）",
			roleAssign, turnStart, turnEnd, moderator, assumption, done)
	}
}

// TestUnresolvedFedToNextRound 验证"深度递增的传动轴"：
// 主持人在 R2 挑出的未解分歧点，必须出现在 R3 的提示词里。
func TestUnresolvedFedToNextRound(t *testing.T) {
	m := newMock()
	o, _ := New(m, Config{Router: llm.Router{Cheap: "cheap", Strong: "strong"}})
	if _, err := o.Run(context.Background(), sampleDilemma(), nil); err != nil {
		t.Fatal(err)
	}
	const marker = "双方在首付压力的量化口径上不一致"
	for _, c := range m.Calls() {
		um := lastUserMsg(c.Messages)
		// 只在"轮次发言"提示词里找（含【本轮任务），且排除主持人自己的提示词。
		if strings.Contains(um, "【本轮任务") && strings.Contains(um, marker) {
			return // 找到：分歧点已喂进下一轮
		}
	}
	t.Fatalf("主持人的未解分歧点未出现在后续轮次提示词中（深度递增失效）")
}

// TestSecondSpeakerSeesFirst 验证串行轮次的数据依赖：
// 后手（数据派）的提示词里必须包含先手（生活派）的发言内容。
func TestSecondSpeakerSeesFirst(t *testing.T) {
	m := newMock()
	o, _ := New(m, Config{Router: llm.Router{Cheap: "cheap", Strong: "strong"}})
	if _, err := o.Run(context.Background(), sampleDilemma(), nil); err != nil {
		t.Fatal(err)
	}
	for _, c := range m.Calls() {
		um := lastUserMsg(c.Messages)
		// 数据派在某个"非立论"轮次的提示词：既代表买、又带本轮任务、又含对手发言。
		if strings.Contains(um, "你代表：买") && strings.Contains(um, "【本轮任务") &&
			strings.Contains(um, "租房派的发言") {
			return
		}
	}
	t.Fatal("后手（数据派）的提示词未包含先手（生活派）的发言，串行数据依赖缺失")
}

// timingClient 记录每次调用的起止时间，用于证明 R1 两方并行。
type timingClient struct {
	mu    sync.Mutex
	spans []callSpan
}

type callSpan struct {
	opening bool
	start   time.Time
	end     time.Time
}

func (c *timingClient) Stream(ctx context.Context, req llm.Request, onDelta llm.StreamFunc) (llm.Response, error) {
	opening := strings.Contains(lastUserMsg(req.Messages), "【本轮任务：立论】")
	s := callSpan{opening: opening, start: time.Now()}
	defer func() {
		s.end = time.Now()
		c.mu.Lock()
		c.spans = append(c.spans, s)
		c.mu.Unlock()
	}()
	select {
	case <-time.After(60 * time.Millisecond):
	case <-ctx.Done():
		return llm.Response{}, ctx.Err()
	}
	if onDelta != nil {
		_ = onDelta("x")
	}
	return llm.Response{Content: "x", Model: req.Model, Usage: llm.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
}

// TestOpeningRunsInParallel 证明立论轮确实并行：
// 两次 R1 调用的执行区间应当重叠（而非先后串行）。
func TestOpeningRunsInParallel(t *testing.T) {
	c := &timingClient{}
	o, _ := New(c, Config{Router: llm.Router{Cheap: "cheap", Strong: "strong"}})
	if _, err := o.Run(context.Background(), sampleDilemma(), nil); err != nil {
		t.Fatal(err)
	}

	var opening []callSpan
	for _, s := range c.spans {
		if s.opening {
			opening = append(opening, s)
		}
	}
	if len(opening) != 2 {
		t.Fatalf("立论轮应有 2 次调用，实际 %d", len(opening))
	}
	a, b := opening[0], opening[1]
	overlap := a.start.Before(b.end) && b.start.Before(a.end)
	if !overlap {
		t.Errorf("两次立论调用未重叠：a=[%v,%v] b=[%v,%v]，说明它们是串行的",
			a.start, a.end, b.start, b.end)
	}
}

// failAfterClient 在前 n 次成功调用后开始报错，用于验证"失败仍保留已完成轮次"。
type failAfterClient struct {
	mu    sync.Mutex
	n     int
	count int
}

func (c *failAfterClient) Stream(ctx context.Context, req llm.Request, onDelta llm.StreamFunc) (llm.Response, error) {
	c.mu.Lock()
	c.count++
	k := c.count
	c.mu.Unlock()
	if k > c.n {
		return llm.Response{}, errors.New("simulated model failure")
	}
	if onDelta != nil {
		_ = onDelta("ok")
	}
	return llm.Response{Content: "ok", Model: req.Model, Usage: llm.Usage{PromptTokens: 1, CompletionTokens: 1}}, nil
}

// TestRunReturnsPartialOnFailure 验证核心契约：模型调用中途失败，
// 仍返回非 nil 的 result 且包含已完成轮次，不让用户看到空白页。
func TestRunReturnsPartialOnFailure(t *testing.T) {
	c := &failAfterClient{n: 4} // 角色(1)+R1两方(2,3)+R1主持人(4) 成功，R2 首手(5) 失败
	o, _ := New(c, Config{Router: llm.Router{Cheap: "cheap", Strong: "strong"}})

	result, err := o.Run(context.Background(), sampleDilemma(), nil)
	if err == nil {
		t.Fatal("期望中途失败返回 error")
	}
	if result == nil {
		t.Fatal("即使失败，result 也绝不应为 nil")
	}
	if len(result.Turns) < 1 {
		t.Errorf("失败时应保留已完成轮次，实际 Turn 数=%d", len(result.Turns))
	}
}

// TestRunStopsOnDownstreamDisconnect 验证"下游断开立即停止后续调用"：
// emit 返回 error 时不再为没人看的辩论烧 token。
func TestRunStopsOnDownstreamDisconnect(t *testing.T) {
	m := newMock()
	o, _ := New(m, Config{Router: llm.Router{Cheap: "cheap", Strong: "strong"}})

	var frames int
	emit := func(debate.Frame) error {
		frames++
		if frames >= 4 { // 早期就断开
			return errors.New("downstream gone")
		}
		return nil
	}

	result, err := o.Run(context.Background(), sampleDilemma(), emit)
	if err == nil {
		t.Fatal("期望下游断开返回 error")
	}
	if result == nil {
		t.Fatal("result 不应为 nil")
	}
	// 13 是全量调用数；提前断开应显著少于全量。
	if got := len(m.Calls()); got >= 13 {
		t.Errorf("下游断开后模型调用数=%d, 期望明显少于 13（仍在为没人看的辩论烧 token）", got)
	}
}
