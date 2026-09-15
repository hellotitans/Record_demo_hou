package llm

import (
	"context"
	"strings"
	"sync"
	"time"
)

// MockClient 是供测试与本地跑通使用的模型客户端。
//
// 它让整套编排逻辑可以在不花钱、不联网的前提下被完整验证 ——
// 这对 V0 阶段尤其重要：核心假设是"四段式辩论是否真的更有深度"，
// 验证它不应该依赖 API 额度。
type MockClient struct {
	// Handler 根据请求动态生成回复。为 nil 时返回固定文本。
	// 注意：R1 两个角色是并行调用的，Handler 会被并发调用，需要自行保证线程安全。
	Handler func(req Request) string

	// ChunkSize 控制流式分块大小，用于验证前端打字机效果与中断逻辑。
	// 0 或负数表示一次性返回。
	ChunkSize int

	// ChunkDelay 模拟真实网络延迟，用于验证超时与取消路径。
	ChunkDelay time.Duration

	// Err 非空时，Stream 立即返回该错误（在送出任何 delta 之前）。
	Err error

	mu    sync.Mutex
	calls []Request
}

// NewMockClient 返回一个默认可用的 Mock 客户端。
func NewMockClient() *MockClient {
	return &MockClient{}
}

// Stream 实现 Client。
func (m *MockClient) Stream(ctx context.Context, req Request, onDelta StreamFunc) (Response, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	m.mu.Unlock()

	if m.Err != nil {
		return Response{Model: req.Model}, m.Err
	}

	content := "（模拟回复）"
	if m.Handler != nil {
		content = m.Handler(req)
	}

	if m.ChunkSize <= 0 {
		if onDelta != nil {
			if err := onDelta(content); err != nil {
				return Response{Model: req.Model}, err
			}
		}
		return m.response(req, content), nil
	}

	runes := []rune(content)
	for start := 0; start < len(runes); start += m.ChunkSize {
		// 每块之前检查取消 —— 这样"用户中途喊停"的测试才是真的。
		if err := ctx.Err(); err != nil {
			return Response{Model: req.Model}, err
		}
		if m.ChunkDelay > 0 {
			select {
			case <-ctx.Done():
				return Response{Model: req.Model}, ctx.Err()
			case <-time.After(m.ChunkDelay):
			}
		}
		end := start + m.ChunkSize
		if end > len(runes) {
			end = len(runes)
		}
		if onDelta != nil {
			if err := onDelta(string(runes[start:end])); err != nil {
				return Response{Model: req.Model}, err
			}
		}
	}

	return m.response(req, content), nil
}

func (m *MockClient) response(req Request, content string) Response {
	return Response{
		Content: content,
		Model:   req.Model,
		Usage: Usage{
			// 粗略估算：中文大约 1 token/字。真实值由服务端 usage 字段提供，
			// 这里只需要一个量级正确的数，用于验证成本埋点链路。
			PromptTokens:     countTokens(messagesText(req.Messages)),
			CompletionTokens: countTokens(content),
		},
	}
}

// Calls 返回已收到的请求快照，供测试断言模型选择、提示词内容等。
func (m *MockClient) Calls() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Request, len(m.calls))
	copy(out, m.calls)
	return out
}

// LastUserMessage 返回最后一次调用的用户消息，便于断言提示词内容。
func (m *MockClient) LastUserMessage() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.calls) - 1; i >= 0; i-- {
		for j := len(m.calls[i].Messages) - 1; j >= 0; j-- {
			if m.calls[i].Messages[j].Role == RoleUser {
				return m.calls[i].Messages[j].Content
			}
		}
	}
	return ""
}

func messagesText(msgs []Message) string {
	var b strings.Builder
	for _, m := range msgs {
		b.WriteString(m.Content)
	}
	return b.String()
}

// countTokens 粗略估算 token 数：CJK 字符约 1 字 1 token，其余约 4 字符 1 token。
// 只用于 Mock，真实成本以服务端返回的 usage 为准。
func countTokens(s string) int {
	if s == "" {
		return 0
	}
	n := 0
	ascii := 0
	for _, r := range s {
		if r > 0x2E80 {
			n++
		} else {
			ascii++
		}
	}
	return n + ascii/4
}
