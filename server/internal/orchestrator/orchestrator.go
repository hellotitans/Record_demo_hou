// Package orchestrator 编排一场四段式决策辩论。
//
// 为什么主体是串行的：质询轮要求 B 点名反驳 A 的发言，存在数据依赖，
// 并行反而会让 B 看不到 A 说了什么。全流程唯一的并行点是第一轮立论 ——
// 立论轮明确禁止提及对方，两边互不依赖，可以并跑省下一次调用的时间。
// 不为了用并发而用并发，这是这里最重要的判断。
package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/yourorg/decision-debate/internal/debate"
	"github.com/yourorg/decision-debate/internal/llm"
)

// Price 是每千 token 的价格（人民币）。
type Price struct {
	PromptPerK     float64
	CompletionPerK float64
}

// Config 是编排器配置。
type Config struct {
	Router llm.Router

	// Prices 按模型名定价；未命中的模型用 DefaultPrice。
	Prices       map[string]Price
	DefaultPrice Price

	// TurnTimeout 限制单次发言。超时即中止该轮，已生成的内容保留。
	TurnTimeout time.Duration

	// Temperature 辩论文本用较高温度（要有立场感、有火花），
	// 结构化输出（角色分配、主持人、假设提取）内部强制用低温。
	Temperature float64

	MaxTurnTokens int
}

func (c Config) withDefaults() Config {
	if c.DefaultPrice == (Price{}) {
		// 占位估值，务必用 Layer 4 的真实成本埋点校准 —— 定价和模型选型都依赖它。
		c.DefaultPrice = Price{PromptPerK: 0.004, CompletionPerK: 0.012}
	}
	if c.TurnTimeout <= 0 {
		c.TurnTimeout = 90 * time.Second
	}
	if c.Temperature == 0 {
		c.Temperature = 0.8
	}
	if c.MaxTurnTokens <= 0 {
		c.MaxTurnTokens = 1200
	}
	return c
}

// EmitFunc 推送一帧给前端。返回 error 表示下游断开，编排会立即停止后续调用。
type EmitFunc func(debate.Frame) error

// Orchestrator 编排一场辩论。可并发使用。
type Orchestrator struct {
	client llm.Client
	cfg    Config
}

// New 创建编排器。
func New(client llm.Client, cfg Config) (*Orchestrator, error) {
	if client == nil {
		return nil, errors.New("orchestrator: client must not be nil")
	}
	return &Orchestrator{client: client, cfg: cfg.withDefaults()}, nil
}

// Run 执行一整场辩论。
//
// 契约：
//   - 出错时仍返回已完成的轮次，不返回 nil —— 用户等了 60 秒，
//     中途失败也该让他看到已完成的部分，而不是一个空白页。
//   - ctx 取消或下游断开会立即停止后续模型调用，不再烧 token。
func (o *Orchestrator) Run(ctx context.Context, d debate.Dilemma, emit EmitFunc) (*debate.Result, error) {
	if emit == nil {
		emit = func(debate.Frame) error { return nil }
	}

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// emit 会被 R1 的两个 goroutine 并发调用，必须串行化。
	// 更重要的：下游一旦断开，立刻取消 runCtx，让还在跑的模型调用停下来 ——
	// 否则用户关了页面，我们还在为一场没人看的辩论付钱。
	var (
		emitMu  sync.Mutex
		emitErr error
	)
	safeEmit := func(f debate.Frame) error {
		emitMu.Lock()
		defer emitMu.Unlock()
		if emitErr != nil {
			return emitErr
		}
		if err := emit(f); err != nil {
			emitErr = err
			cancel()
			return err
		}
		return nil
	}

	result := &debate.Result{DilemmaID: d.ID}
	usage := &debate.Usage{}

	// --- 1. 角色分配 ---
	assignments, err := o.assignRoles(runCtx, d, safeEmit, usage)
	if err != nil {
		return result, err
	}
	result.Assignments = assignments
	bySide := map[debate.Side]debate.Assignment{}
	for _, a := range assignments {
		bySide[a.Side] = a
	}

	var history []debate.Turn
	var note *debate.ModeratorNote

	// --- 2. 四段式辩论 ---
	for _, r := range debate.AllRounds {
		var roundTurns []debate.Turn

		if r == debate.RoundOpening {
			roundTurns, err = o.runOpeningRound(runCtx, d, bySide, safeEmit, usage)
		} else {
			roundTurns, err = o.runSerialRound(runCtx, d, r, bySide, history, note, safeEmit, usage)
		}
		if err != nil {
			result.Turns = history
			result.Usage = *usage
			return result, err
		}
		history = append(history, roundTurns...)
		result.Turns = history

		if r == debate.RoundClosing {
			// 辩论已结束，主持人不再挑分歧，改为提取关键假设。
			break
		}

		note, err = o.runModerator(runCtx, d, r, history, safeEmit, usage)
		if err != nil {
			result.Usage = *usage
			return result, err
		}
		if note != nil {
			result.Notes = append(result.Notes, *note)
		}
	}

	// --- 3. 提取关键假设，交给临界点计算器 ---
	result.Assumptions = o.extractAssumptions(runCtx, d, history, safeEmit, usage)
	result.Usage = *usage

	if err := safeEmit(debate.Frame{Kind: debate.FrameDone, Data: result}); err != nil {
		return result, err
	}
	return result, nil
}

// runOpeningRound 并行跑第一轮立论。
//
// 这是全流程唯一的并行点：立论轮禁止提及对方，两边没有数据依赖。
func (o *Orchestrator) runOpeningRound(
	ctx context.Context,
	d debate.Dilemma,
	bySide map[debate.Side]debate.Assignment,
	emit EmitFunc,
	usage *debate.Usage,
) ([]debate.Turn, error) {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		turns   []debate.Turn
		errs    []error
		usageMu sync.Mutex
	)

	for _, side := range []debate.Side{debate.SideData, debate.SideLife} {
		wg.Add(1)
		go func(s debate.Side) {
			defer wg.Done()
			t, u, err := o.runTurn(ctx, d, s, debate.RoundOpening, bySide, nil, nil, emit)

			usageMu.Lock()
			*usage = addUsage(*usage, u)
			usageMu.Unlock()

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				errs = append(errs, err)
				return
			}
			turns = append(turns, t)
		}(side)
	}
	wg.Wait()

	if len(errs) > 0 {
		// 即使有一方失败，另一方的内容也保留 —— 半场辩论也比没有强。
		return orderedTurns(turns), errors.Join(errs...)
	}
	return orderedTurns(turns), nil
}

// orderedTurns 固定按 数据派、生活派 排序，保证输出可复现（测试与回放都需要）。
func orderedTurns(turns []debate.Turn) []debate.Turn {
	out := make([]debate.Turn, 0, len(turns))
	for _, s := range []debate.Side{debate.SideData, debate.SideLife} {
		for _, t := range turns {
			if t.Side == s {
				out = append(out, t)
			}
		}
	}
	return out
}

// runSerialRound 串行跑一轮：先手说完，后手才能看到并反驳。
func (o *Orchestrator) runSerialRound(
	ctx context.Context,
	d debate.Dilemma,
	r debate.RoundNo,
	bySide map[debate.Side]debate.Assignment,
	history []debate.Turn,
	note *debate.ModeratorNote,
	emit EmitFunc,
	usage *debate.Usage,
) ([]debate.Turn, error) {
	first, ok := r.FirstSpeaker()
	if !ok {
		return nil, fmt.Errorf("orchestrator: round %d has no first speaker", int(r))
	}

	firstTurn, u1, err := o.runTurn(ctx, d, first, r, bySide, history, note, emit)
	*usage = addUsage(*usage, u1)
	if err != nil {
		return nil, err
	}

	// 先手发言立刻进入 history，这样后手能看到它 —— 这是串行的数据依赖所在。
	history = append(history, firstTurn)

	secondTurn, u2, err := o.runTurn(ctx, d, first.Opponent(), r, bySide, history, note, emit)
	*usage = addUsage(*usage, u2)
	if err != nil {
		// 后手失败时保留先手的内容。
		return []debate.Turn{firstTurn}, err
	}
	return []debate.Turn{firstTurn, secondTurn}, nil
}

// runTurn 执行一方的单次发言。
func (o *Orchestrator) runTurn(
	ctx context.Context,
	d debate.Dilemma,
	side debate.Side,
	r debate.RoundNo,
	bySide map[debate.Side]debate.Assignment,
	history []debate.Turn,
	note *debate.ModeratorNote,
	emit EmitFunc,
) (debate.Turn, debate.Usage, error) {
	asg := bySide[side]
	self := d.Option(asg.OptionID)
	opponent := d.Option(bySide[side.Opponent()].OptionID)

	turnCtx := debate.TurnContext{
		Dilemma:        d,
		SelfOption:     self,
		OpponentOption: opponent,
		History:        history,
		OpponentLast:   lastTurnOf(history, side.Opponent()),
		Note:           note,
	}

	// 混合模型策略：立论用便宜模型，其余三轮用强模型。
	tier := llm.TierStrong
	if r == debate.RoundOpening {
		tier = llm.TierCheap
	}
	model := o.cfg.Router.Model(tier)

	temp := o.cfg.Temperature
	req := llm.Request{
		Model:       model,
		Temperature: &temp,
		MaxTokens:   o.cfg.MaxTurnTokens,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: debate.SystemPrompt(side, self, asg.Reason)},
			{Role: llm.RoleUser, Content: debate.BuildTurnPrompt(r, side, turnCtx)},
		},
	}

	start := time.Now()
	if err := emit(debate.Frame{Kind: debate.FrameTurnStart, Round: r, Side: side}); err != nil {
		return debate.Turn{}, debate.Usage{}, err
	}

	callCtx, cancel := context.WithTimeout(ctx, o.cfg.TurnTimeout)
	defer cancel()

	resp, err := o.client.Stream(callCtx, req, func(delta string) error {
		return emit(debate.Frame{Kind: debate.FrameDelta, Round: r, Side: side, Text: delta})
	})
	elapsed := time.Since(start)

	if err != nil {
		return debate.Turn{}, debate.Usage{}, fmt.Errorf("orchestrator: round %d %s: %w", int(r), side, err)
	}
	if strings.TrimSpace(resp.Content) == "" {
		return debate.Turn{}, debate.Usage{}, fmt.Errorf("orchestrator: round %d %s: %w", int(r), side, llm.ErrEmptyResponse)
	}

	turn := debate.Turn{
		Round:            r,
		Side:             side,
		Content:          resp.Content,
		Model:            resp.Model,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		LatencyMs:        elapsed.Milliseconds(),
	}
	if err := emit(debate.Frame{Kind: debate.FrameTurnEnd, Round: r, Side: side, Data: turn}); err != nil {
		return turn, debate.Usage{}, err
	}

	return turn, debate.Usage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalLatencyMs:   elapsed.Milliseconds(),
		EstimatedCostCNY: o.estimateCost(model, resp.Usage),
	}, nil
}

// runModerator 执行主持人：挑出未解分歧 + 检测重复论点。
//
// 用便宜模型：这是事务性总结，不需要最强的推理能力。
// 失败时降级为空笔记而不是中止辩论 —— 主持人是质量控制手段，
// 它挂了辩论就该降级继续，而不是整场崩掉。
func (o *Orchestrator) runModerator(
	ctx context.Context,
	d debate.Dilemma,
	r debate.RoundNo,
	history []debate.Turn,
	emit EmitFunc,
	usage *debate.Usage,
) (*debate.ModeratorNote, error) {
	model := o.cfg.Router.Model(llm.TierCheap)
	temp := 0.2
	req := llm.Request{
		Model:       model,
		Temperature: &temp,
		JSONMode:    true,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: debate.BuildModeratorPrompt(r, debate.TurnContext{
				Dilemma: d,
				History: history,
			})},
		},
	}

	callCtx, cancel := context.WithTimeout(ctx, o.cfg.TurnTimeout)
	defer cancel()

	start := time.Now()
	resp, err := o.client.Stream(callCtx, req, func(string) error { return nil })
	elapsed := time.Since(start)
	*usage = addUsage(*usage, debate.Usage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalLatencyMs:   elapsed.Milliseconds(),
		EstimatedCostCNY: o.estimateCost(model, resp.Usage),
	})
	if err != nil {
		return nil, err
	}

	note := &debate.ModeratorNote{Round: r}
	if err := decodeJSONStrict(resp.Content, note); err != nil {
		// 主持人输出不合规范：降级为空笔记，辩论继续。
		// 这里不返回 error —— 让一场已经跑了几十秒的辩论因为 JSON 少个括号
		// 而全盘失败，是典型的把工程质量问题转嫁给用户。
		note.Unresolved = nil
		note.Repeated = nil
	}
	return note, emit(debate.Frame{Kind: debate.FrameModerator, Round: r, Data: note})
}

// extractAssumptions 从最后一轮提取结构化假设。失败时返回空列表，不中止。
func (o *Orchestrator) extractAssumptions(
	ctx context.Context,
	d debate.Dilemma,
	history []debate.Turn,
	emit EmitFunc,
	usage *debate.Usage,
) []debate.Assumption {
	var closing []debate.Turn
	for _, t := range history {
		if t.Round == debate.RoundClosing {
			closing = append(closing, t)
		}
	}
	if len(closing) == 0 {
		return nil
	}

	model := o.cfg.Router.Model(llm.TierStrong)
	temp := 0.2
	req := llm.Request{
		Model:       model,
		Temperature: &temp,
		JSONMode:    true,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: debate.BuildAssumptionPrompt(d, closing)},
		},
	}

	callCtx, cancel := context.WithTimeout(ctx, o.cfg.TurnTimeout)
	defer cancel()

	start := time.Now()
	resp, err := o.client.Stream(callCtx, req, func(string) error { return nil })
	elapsed := time.Since(start)
	*usage = addUsage(*usage, debate.Usage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalLatencyMs:   elapsed.Milliseconds(),
		EstimatedCostCNY: o.estimateCost(model, resp.Usage),
	})
	if err != nil {
		return nil
	}

	var raw []debate.Assumption
	if err := decodeJSONStrict(resp.Content, &raw); err != nil {
		return nil
	}

	out := make([]debate.Assumption, 0, len(raw))
	for i, a := range raw {
		if strings.TrimSpace(a.Statement) == "" {
			continue
		}
		a.ID = fmt.Sprintf("asm-%d", i+1)
		if a.Side != debate.SideData && a.Side != debate.SideLife {
			continue
		}
		out = append(out, a)
		_ = emit(debate.Frame{Kind: debate.FrameAssumption, Data: a})
	}
	return out
}

// assignRoles 决定哪个角色代表哪个选项，并把理由公开给用户。
//
// 解析失败时降级为确定性分配 + 诚实的理由，绝不让一场辩论卡在这一步。
func (o *Orchestrator) assignRoles(
	ctx context.Context,
	d debate.Dilemma,
	emit EmitFunc,
	usage *debate.Usage,
) ([]debate.Assignment, error) {
	model := o.cfg.Router.Model(llm.TierStrong)
	temp := 0.2
	req := llm.Request{
		Model:       model,
		Temperature: &temp,
		JSONMode:    true,
		Messages: []llm.Message{
			{Role: llm.RoleUser, Content: debate.BuildRoleAssignPrompt(d)},
		},
	}

	callCtx, cancel := context.WithTimeout(ctx, o.cfg.TurnTimeout)
	defer cancel()

	start := time.Now()
	resp, err := o.client.Stream(callCtx, req, func(string) error { return nil })
	elapsed := time.Since(start)
	*usage = addUsage(*usage, debate.Usage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalLatencyMs:   elapsed.Milliseconds(),
		EstimatedCostCNY: o.estimateCost(model, resp.Usage),
	})

	assignments, parseErr := parseAssignments(resp.Content, d)
	if err != nil || parseErr != nil {
		// 降级：按固定顺序分配，理由照实说明，用户仍可一键对调。
		assignments = fallbackAssignments(d)
	}
	if err := emit(debate.Frame{Kind: debate.FrameRoleAssign, Data: assignments}); err != nil {
		return nil, err
	}
	if err != nil {
		// 模型调用本身失败才算错误；仅仅是解析失败则继续走降级分配。
		return assignments, err
	}
	return assignments, nil
}

func fallbackAssignments(d debate.Dilemma) []debate.Assignment {
	return []debate.Assignment{
		{Side: debate.SideData, OptionID: d.Options[0].ID, Reason: "角色分配未返回有效结果，已按默认顺序分配，你可以一键对调。"},
		{Side: debate.SideLife, OptionID: d.Options[1].ID, Reason: "角色分配未返回有效结果，已按默认顺序分配，你可以一键对调。"},
	}
}

type roleAssignPayload struct {
	Data struct {
		OptionID string `json:"option_id"`
		Reason   string `json:"reason"`
	} `json:"data"`
	Life struct {
		OptionID string `json:"option_id"`
		Reason   string `json:"reason"`
	} `json:"life"`
}

func parseAssignments(content string, d debate.Dilemma) ([]debate.Assignment, error) {
	var p roleAssignPayload
	if err := decodeJSONStrict(content, &p); err != nil {
		return nil, err
	}

	valid := map[string]bool{d.Options[0].ID: true, d.Options[1].ID: true}
	if !valid[p.Data.OptionID] || !valid[p.Life.OptionID] {
		return nil, errors.New("unknown option id")
	}
	// 两个角色必须代表不同选项，否则就不是辩论了。
	if p.Data.OptionID == p.Life.OptionID {
		return nil, errors.New("both sides assigned to the same option")
	}

	return []debate.Assignment{
		{Side: debate.SideData, OptionID: p.Data.OptionID, Reason: strings.TrimSpace(p.Data.Reason)},
		{Side: debate.SideLife, OptionID: p.Life.OptionID, Reason: strings.TrimSpace(p.Life.Reason)},
	}, nil
}

// decodeJSONStrict 从模型输出里抠出 JSON。
//
// 模型经常会在 JSON 外面包一层 ```json 代码块或一句客套话，
// 这里做宽松定位但严格解析：找到第一个 { 或 [ 之后交给 encoding/json，
// 宁可因为字段不合法失败，也不要用正则去猜内容。
func decodeJSONStrict(s string, dst any) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return errors.New("empty content")
	}
	if i := strings.IndexAny(s, "{["); i > 0 {
		s = s[i:]
	}
	if end := strings.LastIndexAny(s, "}]"); end >= 0 && end < len(s)-1 {
		s = s[:end+1]
	}
	return json.Unmarshal([]byte(s), dst)
}

// estimateCost 估算单次调用成本。这是估值，真实成本由 Layer 4 埋点校准。
func (o *Orchestrator) estimateCost(model string, u llm.Usage) float64 {
	p, ok := o.cfg.Prices[model]
	if !ok {
		p = o.cfg.DefaultPrice
	}
	return float64(u.PromptTokens)/1000*p.PromptPerK + float64(u.CompletionTokens)/1000*p.CompletionPerK
}

func addUsage(a, b debate.Usage) debate.Usage {
	return debate.Usage{
		PromptTokens:     a.PromptTokens + b.PromptTokens,
		CompletionTokens: a.CompletionTokens + b.CompletionTokens,
		TotalLatencyMs:   a.TotalLatencyMs + b.TotalLatencyMs,
		EstimatedCostCNY: a.EstimatedCostCNY + b.EstimatedCostCNY,
	}
}

func lastTurnOf(history []debate.Turn, side debate.Side) string {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Side == side {
			return history[i].Content
		}
	}
	return ""
}
