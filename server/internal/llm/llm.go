// Package llm 抽象模型调用。
//
// 抽象的意义在于两件事：
//  1. 编排逻辑可以在不花钱、不联网的情况下被完整测试（见 mock.go）。
//  2. 混合模型策略可以在一处切换，而不是散落在编排代码里。
package llm

import (
	"context"
	"errors"
)

// Role 是消息角色。
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message 是一条对话消息。
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// Request 是一次模型调用的参数。
type Request struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	// JSONMode 要求模型输出合法 JSON（OpenAI 的 response_format=json_object）。
	// 主持人、角色分配、假设提取都依赖它 —— 靠正则从自然语言里抠结构化
	// 数据是不可靠的，会在最关键的环节掉链子。
	JSONMode bool `json:"json_mode,omitempty"`
}

// Usage 是单次调用的 token 消耗，用于成本埋点。
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

// Response 是一次调用的完整结果。
type Response struct {
	Content string `json:"content"`
	Usage   Usage  `json:"usage"`
	Model   string `json:"model"`
}

// StreamFunc 接收流式文本片段。
//
// 由 Client 串行调用，实现方不需要加锁。返回 error 会中断流式读取，
// 用于把"下游断开"传导到上游，避免客户端走了还在继续烧 token。
type StreamFunc func(delta string) error

// Client 抽象一次模型调用。
type Client interface {
	// Stream 发起流式调用。ctx 取消会立即终止。
	// 即使返回 error，onDelta 已经送出的内容依然有效（辩论可以保留已完成的部分）。
	Stream(ctx context.Context, req Request, onDelta StreamFunc) (Response, error)
}

// Tier 是模型档位，混合模型策略的基础。
//
// 立论和主持人的事务性工作用便宜模型完全够；质询、承认后反击、假设提取
// 是推理密度最高、最决定产品质量的三轮，用好模型。这一刀能省 40–60% 成本。
type Tier int

const (
	TierCheap  Tier = iota // 立论、主持人挑分歧与去重
	TierStrong             // 质询、承认与反击、总结与暴露假设、假设提取、角色分配
)

// Router 按档位选择模型。
type Router struct {
	Cheap  string
	Strong string
}

// Model 返回档位对应的模型名。未配置时回退到另一个档位，保证永远不为空。
func (r Router) Model(t Tier) string {
	if t == TierCheap && r.Cheap != "" {
		return r.Cheap
	}
	if t == TierStrong && r.Strong != "" {
		return r.Strong
	}
	if r.Cheap != "" {
		return r.Cheap
	}
	return r.Strong
}

// ErrEmptyResponse 表示模型返回了空内容。
// 编排层据此重试或降级，而不是把空发言渲染给用户。
var ErrEmptyResponse = errors.New("llm: empty response content")
