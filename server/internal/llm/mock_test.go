package llm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestMockStreamChunks 验证分块流式：onDelta 被多次调用，且拼回完整内容。
// 这是前端打字机效果的来源，必须一段段送而不是一次性吐。
func TestMockStreamChunks(t *testing.T) {
	m := &MockClient{ChunkSize: 3}
	var deltas []string
	resp, err := m.Stream(context.Background(), Request{Model: "m"}, func(d string) error {
		deltas = append(deltas, d)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "（模拟回复）" {
		t.Errorf("content=%q", resp.Content)
	}
	if len(deltas) <= 1 {
		t.Errorf("期望分块多次回调，实际 %d 次：%v", len(deltas), deltas)
	}
	joined := ""
	for _, d := range deltas {
		joined += d
	}
	if joined != resp.Content {
		t.Errorf("分块拼接 %q 与完整内容 %q 不一致", joined, resp.Content)
	}
}

// TestMockHandler 验证 Handler 能根据请求动态生成内容，且 Calls() 记录模型选择。
// 编排层据此断言"R1 便宜模型、R2–R4 强模型"。
func TestMockHandlerAndCalls(t *testing.T) {
	m := &MockClient{Handler: func(req Request) string {
		return "model=" + req.Model
	}}
	models := []string{"cheap", "strong", "strong", "strong"}
	for _, mod := range models {
		if _, err := m.Stream(context.Background(), Request{Model: mod}, nil); err != nil {
			t.Fatal(err)
		}
	}
	calls := m.Calls()
	if len(calls) != len(models) {
		t.Fatalf("Calls() 长度=%d, 期望 %d", len(calls), len(models))
	}
	for i, mod := range models {
		if calls[i].Model != mod {
			t.Errorf("Calls()[%d].Model=%q, 期望 %q", i, calls[i].Model, mod)
		}
	}
}

// TestMockError 验证预置错误时立即返回，且不送任何 delta。
func TestMockError(t *testing.T) {
	wantErr := errors.New("boom")
	m := &MockClient{Err: wantErr}
	called := false
	_, err := m.Stream(context.Background(), Request{Model: "m"}, func(string) error {
		called = true
		return nil
	})
	if !errors.Is(err, wantErr) {
		t.Errorf("err=%v, 期望 %v", err, wantErr)
	}
	if called {
		t.Error("出错前不应回调 onDelta")
	}
}

// TestMockCancelMidStream 验证"用户中途喊停"：
// ctx 取消时 Stream 立即返回，且已送出的内容不丢（辩论可保留已完成部分）。
func TestMockCancelMidStream(t *testing.T) {
	m := &MockClient{ChunkSize: 1, ChunkDelay: 100 * time.Millisecond}
	ctx, cancel := context.WithCancel(context.Background())

	var got []string
	done := make(chan struct{})
	go func() {
		_, _ = m.Stream(ctx, Request{Model: "m"}, func(d string) error {
			got = append(got, d)
			return nil
		})
		close(done)
	}()

	time.Sleep(150 * time.Millisecond) // 让第一段内容已送出
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 Stream 未在 2s 内返回（取消路径失效）")
	}

	if len(got) == 0 {
		t.Fatal("已送出的内容丢失")
	}
	// 段大小 1、每段延迟 100ms，150ms 时应已送出第 1 段。
	if joined := join(got); joined != "（" {
		t.Errorf("已送达内容=%q, 期望已送达首段 %q", joined, "（")
	}
}

// TestMockConcurrency 验证 R1 两个角色并行调用下 MockClient 是安全的：
// Calls() 不丢、不重、不乱。并发安全由 MockClient 内部的 mu 保证。
func TestMockConcurrency(t *testing.T) {
	m := &MockClient{ChunkSize: 2}
	const n = 32
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			_, _ = m.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "hi"}}}, func(string) error {
				return nil
			})
		}()
	}
	wg.Wait()

	if len(m.Calls()) != n {
		t.Errorf("并发后 Calls() 长度=%d, 期望 %d（存在丢失或重复）", len(m.Calls()), n)
	}
}

// TestMockLastUserMessage 验证能从 Calls 快照里取到最近一次用户消息，
// 用于断言编排层塞给模型的提示词内容。
func TestMockLastUserMessage(t *testing.T) {
	m := &MockClient{}
	_, _ = m.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleSystem, Content: "sys"}, {Role: RoleUser, Content: "第一轮提示词"}}}, nil)
	_, _ = m.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: RoleUser, Content: "第二轮提示词"}}}, nil)

	if got := m.LastUserMessage(); got != "第二轮提示词" {
		t.Errorf("LastUserMessage()=%q, 期望 %q", got, "第二轮提示词")
	}
}

func join(ss []string) string {
	s := ""
	for _, v := range ss {
		s += v
	}
	return s
}
