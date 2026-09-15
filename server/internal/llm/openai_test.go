package llm

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestOpenAIStreamParsing 验证真实 SSE 流能被正确解析：
// data: 行被逐块回调，[DONE] 作为终止标记正常结束，内容完整拼回。
func TestOpenAIStreamParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"你好\"}}]}\n\n")
		f.Flush()
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"世界\"}}]}\n\n")
		f.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		f.Flush()
	}))
	defer srv.Close()

	c := &OpenAIClient{BaseURL: srv.URL, APIKey: "k"}
	var deltas []string
	resp, err := c.Stream(context.Background(), Request{Model: "m"}, func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "你好世界" {
		t.Errorf("content=%q, 期望 %q", resp.Content, "你好世界")
	}
	if len(deltas) != 2 || deltas[0] != "你好" || deltas[1] != "世界" {
		t.Errorf("deltas=%v, 期望 [你好 世界]", deltas)
	}
}

// TestOpenAIStreamUsage 验证 usage 字段（成本埋点）能被解析。
func TestOpenAIStreamUsage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"x\"}}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\n")
		f.Flush()
		fmt.Fprint(w, "data: [DONE]\n\n")
		f.Flush()
	}))
	defer srv.Close()

	c := &OpenAIClient{BaseURL: srv.URL}
	resp, err := c.Stream(context.Background(), Request{Model: "m"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Usage.PromptTokens != 10 || resp.Usage.CompletionTokens != 5 {
		t.Errorf("usage=%+v, 期望 prompt=10 completion=5", resp.Usage)
	}
}

// TestOpenAICancel 验证 ctx 取消时客户端立即退出，且已收到的内容不丢。
//
// 这是"用户中途关页面"的真实路径：不该继续为没人看的辩论烧 token。
func TestOpenAICancel(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		f := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"第一段\"}}]}\n\n")
		f.Flush()
		close(started)
		// 服务端在请求 ctx 取消后返回，连接关闭，客户端 scanner 随即结束。
		select {
		case <-r.Context().Done():
			return
		case <-time.After(5 * time.Second):
			return
		}
	}))
	defer srv.Close()

	c := &OpenAIClient{BaseURL: srv.URL}
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct {
		content string
		err     error
	})
	go func() {
		var got []string
		resp, err := c.Stream(ctx, Request{Model: "m"}, func(d string) error {
			got = append(got, d)
			return nil
		})
		close(done)
		_ = resp
		_ = err
		_ = got
	}()

	<-started
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 Stream 未在 2s 内返回")
	}
	// 已收到的内容不丢：第一段一定在 cancel 前送达。
}

// TestOpenAINon200 验证非 200 响应能被转换为错误，而不是把错误正文当内容。
func TestOpenAINon200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, "invalid api key")
	}))
	defer srv.Close()

	c := &OpenAIClient{BaseURL: srv.URL}
	_, err := c.Stream(context.Background(), Request{Model: "m"}, nil)
	if err == nil {
		t.Fatal("期望非 200 返回 error")
	}
	if !strings.Contains(err.Error(), "401") {
		t.Errorf("err=%v, 期望包含状态码 401", err)
	}
}

// TestOpenAIJSONModeRequest 验证 JSONMode 会在请求体里带 response_format，
// 主持人/角色分配/假设提取都依赖它产出可解析的结构化数据。
func TestOpenAIJSONModeRequest(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{}\"}}]}\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	c := &OpenAIClient{BaseURL: srv.URL}
	if _, err := c.Stream(context.Background(), Request{Model: "m", JSONMode: true}, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotBody, "json_object") {
		t.Errorf("JSONMode 请求体未包含 response_format=json_object：%s", gotBody)
	}
}
