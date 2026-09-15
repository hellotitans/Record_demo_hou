// Package httpsrv 提供 HTTP 层：SSE 流式推送与埋点上报。
package httpsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/yourorg/decision-debate/internal/debate"
)

// Stream 把一个 HTTP 响应包装成 SSE 流。
//
// 一场辩论要跑 60–120 秒，中间可能有几十秒没有任何数据（模型在思考）。
// 没有心跳的话，中间的反向代理会直接掐断连接。
type Stream struct {
	w  http.ResponseWriter
	fl http.Flusher

	mu     sync.Mutex
	closed bool
}

// NewStream 创建 SSE 流并设置响应头。
//
// 要求 ResponseWriter 支持 http.Flusher —— 不支持的话打字机效果会变成
// 一次性吐出全部内容，那这个产品最核心的体验就没了，所以直接报错。
func NewStream(w http.ResponseWriter) (*Stream, error) {
	fl, ok := w.(http.Flusher)
	if !ok {
		return nil, errors.New("httpsrv: response writer does not support flushing")
	}
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	// 关掉 Nginx 的缓冲，否则同样会攒着不发。
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fl.Flush()
	return &Stream{w: w, fl: fl}, nil
}

// Send 推送一个命名事件。
func (s *Stream) Send(event string, data any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("httpsrv: stream is closed")
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("httpsrv: marshal frame: %w", err)
	}
	if _, err := fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", event, payload); err != nil {
		s.closed = true
		return err
	}
	s.fl.Flush()
	return nil
}

// Frame 推送一个辩论帧，事件名即帧类型。
func (s *Stream) Frame(f debate.Frame) error {
	return s.Send(string(f.Kind), f)
}

// Heartbeat 持续发送注释帧保持连接，直到 ctx 结束。应在独立 goroutine 中运行。
func (s *Stream) Heartbeat(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			if s.closed {
				s.mu.Unlock()
				return
			}
			// 注释帧：合法 SSE，客户端忽略，只为让中间设备看到流量。
			_, err := fmt.Fprint(s.w, ": ping\n\n")
			s.fl.Flush()
			s.mu.Unlock()
			if err != nil {
				return
			}
		}
	}
}
