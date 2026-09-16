// Package debate 定义决策辩论的领域模型。
//
// 这里只有数据结构，没有任何 I/O。编排逻辑在 orchestrator 包，
// 提示词在 prompt.go —— 提示词是本产品的核心资产，别和流程控制混在一起。
package debate

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Side 是辩论中的两个角色。
//
// 刻意不叫"天使/恶魔"：那对意象自带道德暗示，与"不给建议"的产品定位直接冲突。
// 数据派与生活派的差异落在**思维方式**上（要证据 vs 要感受），没有高下之分，
// 因此中立但依然有火花。
type Side string

const (
	SideData Side = "data"
	SideLife Side = "life"
)

// DisplayName 是面向用户的角色名。
func (s Side) DisplayName() string {
	switch s {
	case SideData:
		return "数据派"
	case SideLife:
		return "生活派"
	default:
		return string(s)
	}
}

// ParseSide 把模型输出的角色标识归一化为 Side，无法识别时返回 false。
//
// 为什么需要它（真实联调的教训，不是理论假设）：
// 提示词里给的示例是 "data"，但整场辩论的发言标签都是中文的"数据派/生活派"，
// 模型在真实环境下会照抄它看到的中文标签。如果直接拿 Side 去和 SideData/SideLife
// 比较，这些假设会被**静默丢弃**——不报错、不埋点，用户只看到空的临界点计算器。
//
// 与其指望模型永远守规矩，不如在入口处容错：约定由代码兜住，提示词只做提示。
func ParseSide(s string) (Side, bool) {
	v := strings.ToLower(strings.TrimSpace(s))
	v = strings.TrimSuffix(v, "派")
	switch v {
	case "data", "数据", "d":
		return SideData, true
	case "life", "生活", "l":
		return SideLife, true
	}
	return "", false
}

// Opponent 返回对手。
func (s Side) Opponent() Side {
	if s == SideData {
		return SideLife
	}
	return SideData
}

// RoundNo 是轮次编号。四轮的战术指令完全不同，这是"每轮比前一轮更深"的
// 唯一保障 —— 靠一句"请继续反驳"是做不到的，大模型会原地打转。
type RoundNo int

const (
	RoundOpening  RoundNo = 1 // 立论
	RoundRebuttal RoundNo = 2 // 质询
	RoundConcede  RoundNo = 3 // 承认 + 前提下反击
	RoundClosing  RoundNo = 4 // 总结 + 暴露关键假设
)

// AllRounds 按顺序列出全部轮次。
var AllRounds = []RoundNo{RoundOpening, RoundRebuttal, RoundConcede, RoundClosing}

func (r RoundNo) Name() string {
	switch r {
	case RoundOpening:
		return "立论"
	case RoundRebuttal:
		return "质询"
	case RoundConcede:
		return "承认与反击"
	case RoundClosing:
		return "总结"
	default:
		return fmt.Sprintf("第%d轮", int(r))
	}
}

// Valid 判断轮次编号是否合法。
func (r RoundNo) Valid() bool {
	switch r {
	case RoundOpening, RoundRebuttal, RoundConcede, RoundClosing:
		return true
	}
	return false
}

// firstSpeaker 决定某一轮谁先发言。
//
// R1 双方并行，先手无意义，返回零值。
// R2 起交替先手：固定一个顺序会让先手方持续占据叙事主动权，
// 交替是消除先手优势成本最低的方式。
func (r RoundNo) FirstSpeaker() (Side, bool) {
	switch r {
	case RoundRebuttal:
		return SideLife, true
	case RoundConcede:
		return SideData, true
	case RoundClosing:
		return SideLife, true
	default:
		return "", false
	}
}

// Option 是被抉择的一个选项。严格二选一。
type Option struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// Dimension 是一个决策维度。
//
// Source 记录维度来源，用于分析品类模板的覆盖率：
// 用户频繁新增的维度就是模板该补的维度（对应埋点 dimension_customized）。
type Dimension struct {
	Key    string  `json:"key"`
	Label  string  `json:"label"`
	Weight float64 `json:"weight"`
	Source string  `json:"source"` // "template" | "model" | "user"
}

// Fact 是一条事实。
//
// Verified 是硬约束的关键：只有 Verified 为 true 的事实才能被模型引用为
// 数字依据。模型自己推演的内容一律不许进计算器，否则幻觉会把临界点带偏。
type Fact struct {
	Statement string `json:"statement"`
	Source    string `json:"source"` // 来源 URL，或 "user" 表示用户亲口提供
	Verified  bool   `json:"verified"`
}

// Dilemma 是用户提交的两难问题。
type Dilemma struct {
	ID         string      `json:"id"`
	SessionID  string      `json:"session_id"`
	Category   string      `json:"category"`
	Question   string      `json:"question"`
	Options    [2]Option   `json:"options"`
	Dimensions []Dimension `json:"dimensions,omitempty"`
	Facts      []Fact      `json:"facts,omitempty"`
}

// Option 按 ID 查找选项，找不到返回零值。
func (d Dilemma) Option(id string) Option {
	for _, o := range d.Options {
		if o.ID == id {
			return o
		}
	}
	return Option{}
}

// Validate 校验一个辩题是否可以开辩。
//
// 两个选项必须都有内容且 ID 不同 —— 严格二选一是产品决策，
// 在数据入口就挡住，不要等编排到一半才发现没得辩。
func (d Dilemma) Validate() error {
	if strings.TrimSpace(d.ID) == "" {
		return errors.New("debate: dilemma id is required")
	}
	if strings.TrimSpace(d.Question) == "" {
		return errors.New("debate: question is required")
	}
	for i, o := range d.Options {
		if strings.TrimSpace(o.ID) == "" {
			return fmt.Errorf("debate: option[%d] id is required", i)
		}
		if strings.TrimSpace(o.Label) == "" {
			return fmt.Errorf("debate: option[%d] label is required", i)
		}
	}
	if d.Options[0].ID == d.Options[1].ID {
		return errors.New("debate: the two options must have distinct ids")
	}
	return nil
}

// Assignment 是角色与选项的对应关系。
//
// Reason 必须公开给用户看：透明本身就是最好的去暗示手段，
// 用户也可以一键对调（对应埋点 role_swapped）。
type Assignment struct {
	Side     Side   `json:"side"`
	OptionID string `json:"option_id"`
	Reason   string `json:"reason"`
}

// Turn 是一方在某一轮的发言。
type Turn struct {
	Round            RoundNo `json:"round"`
	Side             Side    `json:"side"`
	Content          string  `json:"content"`
	Model            string  `json:"model"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	LatencyMs        int64   `json:"latency_ms"`
}

// ModeratorNote 是主持人每轮之后的输出。
//
// Unresolved 会被直接喂给下一轮，这是"深度递增"的传动轴。
// Repeated 非空说明去重机制检测到重复论点，对应埋点里最有价值的
// argument_feedback.verdict = repetitive 的模型侧预判。
type ModeratorNote struct {
	Round      RoundNo  `json:"round"`
	Unresolved []string `json:"unresolved"`
	Repeated   []string `json:"repeated"`
}

// MarshalJSON 保证 unresolved / repeated 永远序列化成数组，绝不输出 null。
//
// 为什么必须显式做这件事：Go 的 nil slice 会被 encoding/json 序列化成 `null`，
// 而前端 TypeScript 把这两个字段声明成 string[]，于是 `note.unresolved.length`
// 在 null 上取值 → TypeError → React 卸载整棵组件树 → 用户看到整页白屏。
// （2026-09-16 实测确认：Mock 模式下主持人输出不是合法 JSON，降级分支把两个字段
// 设为 nil，三个 moderator 帧全是 `"unresolved":null`，点「开始辩论」必白屏。）
//
// 放在类型自己身上，而不是在每个调用点手动补 []string{}：
// 调用点会漏，类型不会。任何构造 ModeratorNote 的路径都自动获得这个保证。
func (n ModeratorNote) MarshalJSON() ([]byte, error) {
	type plain ModeratorNote // 避免递归调用本方法
	unresolved := n.Unresolved
	if unresolved == nil {
		unresolved = []string{}
	}
	repeated := n.Repeated
	if repeated == nil {
		repeated = []string{}
	}
	// 外层同名字段的 json tag 覆盖内嵌结构的，实现"只改这两个字段"。
	return json.Marshal(struct {
		plain
		Unresolved []string `json:"unresolved"`
		Repeated   []string `json:"repeated"`
	}{
		plain:      plain(n),
		Unresolved: unresolved,
		Repeated:   repeated,
	})
}

// Assumption 是某一方论证所依赖的关键假设。
//
// 这是辩论与量化计算器之间的接口：R4 暴露的假设会被提取成结构化字段，
// 用户填数值后直接进入临界点分析。吵架中露出的软肋，自动变成要算的数。
type Assumption struct {
	ID        string  `json:"id"`
	Side      Side    `json:"side"`
	Statement string  `json:"statement"`
	Variable  string  `json:"variable"`
	Operator  string  `json:"operator"` // ">=" | "<=" | ">" | "<"
	Value     float64 `json:"value"`
	Unit      string  `json:"unit"`
}

// Usage 累计消耗，用于成本埋点。
type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	TotalLatencyMs   int64   `json:"total_latency_ms"`
	EstimatedCostCNY float64 `json:"estimated_cost_cny"`
}

// Result 是一场完整辩论的产物。
type Result struct {
	DilemmaID   string          `json:"dilemma_id"`
	Assignments []Assignment    `json:"assignments"`
	Turns       []Turn          `json:"turns"`
	Notes       []ModeratorNote `json:"notes"`
	Assumptions []Assumption    `json:"assumptions"`
	Usage       Usage           `json:"usage"`
}
