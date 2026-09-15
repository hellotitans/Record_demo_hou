package telemetry

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SchemaVersion 事件结构版本。事件结构一定会演进，
// 一开始就要有版本号，否则后期无法区分新旧数据。
const SchemaVersion = 1

// Zone 标识事件发生的区域，是"商业内容时序隔离"在数据层的落地。
//
//   - ZoneDecision：决策区（进入 → 决策卡片生成），绝对无菌，零商业内容。
//     这里采集到的权重、假设、回访数据全部干净，可用于决策质量分析。
//   - ZoneExecution：执行区（决策卡片之后），允许商业内容。
//     这里的数据单独存储，永不进入训练集。
type Zone string

const (
	ZoneDecision  Zone = "decision"
	ZoneExecution Zone = "execution"
)

// EventName 事件名。每个常量都对应设计方案里的一个具体问题。
type EventName string

// Layer 1 —— 漏斗健康度：这个产品有没有跑通。
const (
	EventDebateStarted       EventName = "debate_started"
	EventFormStepCompleted   EventName = "form_step_completed"
	EventFormAbandoned       EventName = "form_abandoned"
	EventGapListShown        EventName = "gap_list_shown"
	EventGapItemResolved     EventName = "gap_item_resolved"
	EventRoundDelivered      EventName = "round_delivered"
	EventRoundRead           EventName = "round_read"
	EventAssumptionResolved  EventName = "assumption_resolved"
	EventWeightAdjusted      EventName = "weight_adjusted"
	EventDimensionCustomized EventName = "dimension_customized"
	EventTippingPointViewed  EventName = "tipping_point_viewed"
	EventCardGenerated       EventName = "card_generated"
	EventCardShared          EventName = "card_shared"
	EventDeepCalcPurchased   EventName = "deep_calc_purchased"
)

// Layer 2 —— 辩论质量：本产品特有，也是价值最高的一层。
const (
	EventArgumentFeedback    EventName = "argument_feedback"
	EventArgumentExpanded    EventName = "argument_expanded"
	EventFactCardClicked     EventName = "fact_card_clicked"
	EventFactCardRejected    EventName = "fact_card_rejected"
	EventDebateHelpfulRating EventName = "debate_helpful_rating"
	EventDebateMissingInfo   EventName = "debate_missing_info"
	EventRoleSwapped         EventName = "role_swapped"
	EventDebateStoppedEarly  EventName = "debate_stopped_early"
	EventModeratorRepeated   EventName = "moderator_repeated"
)

// Layer 3 —— 结果闭环：护城河所在。
const (
	EventDecisionRecorded EventName = "decision_recorded"
	EventRevisitResponded EventName = "revisit_responded"
)

// Layer 4 —— 成本与性能：别等账单来了才算。
const (
	EventLLMCallCompleted EventName = "llm_call_completed"
	EventDebateCostTotal  EventName = "debate_cost_total"
)

// Verdict 用户对单条论点的评价。
//
// VerdictRepetitive 是金矿：它直接说明主持人的"去重"职责失效了，
// 比任何技术指标都准，而且是用户亲口说的。
type Verdict string

const (
	VerdictUseful         Verdict = "useful"
	VerdictWeak           Verdict = "weak"
	VerdictFactuallyWrong Verdict = "factually_wrong"
	VerdictRepetitive     Verdict = "repetitive"
)

// Event 是统一的事件结构。
type Event struct {
	SchemaVersion int            `json:"schema_version"`
	EventID       string         `json:"event_id"`
	EventName     EventName      `json:"event_name"`
	Timestamp     time.Time      `json:"ts"`
	Zone          Zone           `json:"zone"`
	SessionID     string         `json:"session_id"`
	DebateID      string         `json:"debate_id"`
	UserID        string         `json:"user_id,omitempty"`
	Properties    map[string]any `json:"properties,omitempty"`
}

// NewEvent 构造一个事件，自动填充 schema 版本、事件 ID 与时间戳。
//
// debateID 由客户端在进入时生成（UUID v4），贯穿整场辩论，
// 这样即使 debate_started 之前的 form_* 事件也能归属到同一场辩论。
func NewEvent(name EventName, zone Zone, sessionID, debateID string, props map[string]any) Event {
	return Event{
		SchemaVersion: SchemaVersion,
		EventID:       newEventID(),
		EventName:     name,
		Timestamp:     time.Now().UTC(),
		Zone:          zone,
		SessionID:     sessionID,
		DebateID:      debateID,
		Properties:    props,
	}
}

// newEventID 生成 16 字节随机 ID。
//
// 刻意不引入第三方 uuid 包：这是 V0 阶段，为了一个 ID 拉进一个依赖不划算。
// 出错时降级为空字符串而不是 panic —— 事件 ID 只用于去重，主流程不该因为它挂掉。
func newEventID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}

// Validate 校验必填字段。
//
// user_id 允许为空（未登录用户用匿名哈希），其余字段缺失的数据无法分析，
// 宁可在入口拒绝，也不要让脏数据污染后续所有结论。
func (e Event) Validate() error {
	if e.EventName == "" {
		return errors.New("telemetry: event_name is required")
	}
	switch e.Zone {
	case ZoneDecision, ZoneExecution:
	case "":
		return errors.New("telemetry: zone is required")
	default:
		return fmt.Errorf("telemetry: unknown zone %q", e.Zone)
	}
	if e.SessionID == "" {
		return errors.New("telemetry: session_id is required")
	}
	if e.DebateID == "" {
		return errors.New("telemetry: debate_id is required")
	}
	if e.Timestamp.IsZero() {
		return errors.New("telemetry: ts is required")
	}
	return nil
}

// MustProperties 把任意结构体转成属性表，避免手写 map 时字段名拼错。
//
// 只接受可 JSON 序列化的值；不可序列化时降级为空表而不是 panic —— 埋点
// 永远不应该让主流程崩溃。
func MustProperties(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return nil
	}
	return m
}

// ArgumentFeedbackProps 对应 EventArgumentFeedback。
//
// 这是整套埋点里最有价值的一条：逐条论点收集用户评价，
// 聚合后直接产出提示词调优清单。
type ArgumentFeedbackProps struct {
	Round      int     `json:"round"`
	Side       string  `json:"side"` // "data" | "life"
	ArgumentID string  `json:"argument_id"`
	Verdict    Verdict `json:"verdict"`
}

// RoundReadProps 对应 EventRoundRead。
//
// 逐轮阅读完成率是辩论质量的体温计：R3 如果掉一半，
// 说明"承认 + 前提下反击"这一轮的提示词没做好。
type RoundReadProps struct {
	Round            int     `json:"round"`
	DwellMs          int64   `json:"dwell_ms"`
	ScrollCompletion float64 `json:"scroll_completion"`
}

// FactCardRejectedProps 对应 EventFactCardRejected。
//
// 按 domain 聚合就是一张自动生成的检索源黑名单，
// 比人工维护的白名单更贴合真实用户判断。
type FactCardRejectedProps struct {
	URL    string `json:"url"`
	Domain string `json:"domain"`
	Reason string `json:"reason"`
}

// DebateCostTotalProps 对应 EventDebateCostTotal。
//
// 免费模式下单次成本 ¥0.3–1.5 只是估算，必须被真实数据校正。
// 把 ModelMix 与 EventDebateHelpfulRating 关联，可以验证
// "R1 用便宜模型、R2–R4 用好模型"是否真的省钱不伤质量。
type DebateCostTotalProps struct {
	TotalCost      float64        `json:"total_cost"`
	TotalLatencyMs int64          `json:"total_latency_ms"`
	ModelMix       map[string]int `json:"model_mix"`
}

// RoundDeliveredProps 对应 EventRoundDelivered。
//
// Layer 1 漏斗：每一轮发言有没有真的到达用户。配合前端的 round_read
// （用户是否读完），就能算"哪一轮用户划走了" —— 辩论质量最直接的体温计。
type RoundDeliveredProps struct {
	Round      int    `json:"round"`
	Side       string `json:"side"` // "data" | "life"
	Model      string `json:"model"`
	LatencyMs  int64  `json:"latency_ms"`
	CharCount  int    `json:"char_count"`
	PromptTok  int    `json:"prompt_tokens"`
	Completion int    `json:"completion_tokens"`
}

// LLMCostProps 对应 EventLLMCallCompleted。
//
// Layer 4 成本：单次调用的模型、延迟与 token 消耗。聚合后把 Model
// 与前端的辩论有用度评分关联，验证混合模型策略是否真的省钱不伤质量。
type LLMCostProps struct {
	Round            int    `json:"round"`
	Role             string `json:"role"` // "data" | "life"
	Model            string `json:"model"`
	LatencyMs        int64  `json:"latency_ms"`
	PromptTokens     int    `json:"prompt_tokens"`
	CompletionTokens int    `json:"completion_tokens"`
}
