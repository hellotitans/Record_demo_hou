package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIClient 调用 OpenAI 兼容的 Chat Completions 接口（含各家兼容实现）。
//
// 只依赖标准库：V0 阶段不值得为了一个 HTTP 客户端引入 SDK，
// 而且各家兼容协议的字段差异自己控制起来更清楚。
type OpenAIClient struct {
	BaseURL    string // 例如 "https://api.openai.com/v1"
	APIKey     string
	HTTPClient *http.Client

	// Thinking 控制思考型模型的推理输出，取值 "disabled" / "enabled" / ""。
	//
	// 空串表示不发送该字段（兼容不支持此参数的 OpenAI 官方接口）。
	// 为什么必须有这个开关：思考型模型（DeepSeek 等）默认把正文写进
	// delta.reasoning_content 而 delta.content 全程为 null，readSSE 只读 content，
	// 结果就是整场辩论拿到空响应。显式禁用后正文回到 content，
	// 且实测 token 消耗下降约一个数量级（300 → 11）。
	Thinking string
}

// NewOpenAIClient 创建一个带默认超时的客户端。
//
// 超时刻意设长（单次调用可能持续数十秒），请求级的取消交给调用方通过
// context 控制 —— 用户中途喊停要能立刻生效，这个必须由 ctx 负责。
func NewOpenAIClient(baseURL, apiKey string) *OpenAIClient {
	return &OpenAIClient{
		BaseURL: strings.TrimRight(baseURL, "/"),
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// thinkingMode 是各家思考型模型的推理开关，字段名为各家通用约定。
type thinkingMode struct {
	Type string `json:"type"`
}

type chatRequest struct {
	Model         string        `json:"model"`
	Messages      []Message     `json:"messages"`
	Thinking      *thinkingMode `json:"thinking,omitempty"`
	Temperature   *float64      `json:"temperature,omitempty"`
	MaxTokens     int           `json:"max_tokens,omitempty"`
	Stream        bool          `json:"stream"`
	StreamOptions *struct {
		IncludeUsage bool `json:"include_usage"`
	} `json:"stream_options,omitempty"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format,omitempty"`
}

type chatChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

// Stream 实现 Client。
//
// 契约：无论成功失败，HTTP 响应体都一定会被读完并关闭，否则长连接会被耗尽。
func (c *OpenAIClient) Stream(ctx context.Context, req Request, onDelta StreamFunc) (Response, error) {
	if req.Model == "" {
		return Response{}, errors.New("llm: model is required")
	}

	body := chatRequest{
		Model:    req.Model,
		Messages: req.Messages,
		Stream:   true,
	}
	if c.Thinking != "" {
		body.Thinking = &thinkingMode{Type: c.Thinking}
	}
	if req.Temperature != nil {
		body.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}
	if req.JSONMode {
		body.ResponseFormat = &struct {
			Type string `json:"type"`
		}{Type: "json_object"}
	}
	// usage 一律显式要求。流式下不请求 usage 就拿不到 token 消耗，
	// 成本埋点会静默缺数据 —— 依赖服务端默认行为等于把可观测性交给运气。
	body.StreamOptions = &struct {
		IncludeUsage bool `json:"include_usage"`
	}{IncludeUsage: true}

	payload, err := json.Marshal(body)
	if err != nil {
		return Response{}, fmt.Errorf("llm: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return Response{}, fmt.Errorf("llm: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.http().Do(httpReq)
	if err != nil {
		return Response{}, fmt.Errorf("llm: do request: %w", err)
	}
	defer func() {
		// 读完再关：不读完 body 会导致连接无法复用，高并发下很快耗尽连接池。
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return Response{}, fmt.Errorf("llm: unexpected status %d: %s", resp.StatusCode, strings.TrimSpace(string(errBody)))
	}

	return readSSE(ctx, resp.Body, req.Model, onDelta)
}

func (c *OpenAIClient) http() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// readSSE 解析 SSE 流，逐块回调 onDelta，最后返回完整内容。
func readSSE(ctx context.Context, r io.Reader, model string, onDelta StreamFunc) (Response, error) {
	var (
		buf     strings.Builder
		usage   Usage
		scanner = bufio.NewScanner(r)
	)
	// 单块 JSON 可能很长（长文本 + 转义），放宽上限。
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			// 用户中途喊停：立刻返回，已收到的内容不丢。
			return Response{Content: buf.String(), Usage: usage, Model: model}, err
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			break
		}

		var chunk chatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// 单行解析失败不该终止整场辩论：跳过这块继续。
			// 但内容已经不完整，调用方需要自行判断是否可用。
			continue
		}
		if chunk.Usage != nil {
			usage = *chunk.Usage
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		buf.WriteString(delta)
		if onDelta != nil {
			if err := onDelta(delta); err != nil {
				// onDelta 报错通常是下游断开，直接终止，别再往下读。
				return Response{Content: buf.String(), Usage: usage, Model: model}, err
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Response{Content: buf.String(), Usage: usage, Model: model}, fmt.Errorf("llm: read stream: %w", err)
	}

	return Response{Content: buf.String(), Usage: usage, Model: model}, nil
}
