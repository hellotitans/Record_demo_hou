package httpsrv

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/yourorg/decision-debate/internal/debate"
)

// flusherRecorder 给 httptest.ResponseRecorder 加上 Flush，使其满足 http.Flusher。
// NewStream 要求 ResponseWriter 支持 Flush，否则打字机效果会变成一次性吐出。
type flusherRecorder struct {
	*httptest.ResponseRecorder
}

func (f *flusherRecorder) Flush() {}

// TestStreamFrameFormat 验证帧格式：必须是合法的 "event: X\ndata: <JSON>\n\n"。
func TestStreamFrameFormat(t *testing.T) {
	w := &flusherRecorder{httptest.NewRecorder()}
	s, err := NewStream(w)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Frame(debate.Frame{Kind: debate.FrameTurnStart, Round: 1, Side: debate.SideData}); err != nil {
		t.Fatal(err)
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: turn_start") {
		t.Errorf("帧缺少 event: turn_start；body=%q", body)
	}
	if !strings.Contains(body, "\ndata: ") {
		t.Errorf("帧缺少 data: 字段；body=%q", body)
	}
	// data 必须是合法 JSON，否则前端无法解析。
	if i := strings.Index(body, "data: "); i >= 0 {
		line := body[i+len("data: "):]
		line = strings.TrimSpace(strings.SplitN(line, "\n", 2)[0])
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Errorf("data 不是合法 JSON：%q (%v)", line, err)
		}
	}
}

// TestStreamHeartbeat 验证心跳帧持续发送，避免反向代理在思考间隙掐断连接。
func TestStreamHeartbeat(t *testing.T) {
	w := &flusherRecorder{httptest.NewRecorder()}
	s, err := NewStream(w)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go s.Heartbeat(ctx, 30*time.Millisecond)
	time.Sleep(90 * time.Millisecond)
	cancel()
	time.Sleep(30 * time.Millisecond) // 等心跳 goroutine 退出

	if !strings.Contains(w.Body.String(), ": ping") {
		t.Errorf("心跳帧缺失；body=%q", w.Body.String())
	}
}

// errWriter 的 Write 永远失败，用于验证 Stream 在底层写入失败时能报错而非静默丢失。
type errWriter struct {
	header http.Header
}

func (e *errWriter) Header() http.Header       { return e.header }
func (e *errWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func (e *errWriter) WriteHeader(int)           {}
func (e *errWriter) Flush()                    {}

// TestStreamWriteError 验证底层写入失败时 Send/Frame 返回 error，
// 这样编排器能据此停止为没人看的辩论继续烧 token。
func TestStreamWriteError(t *testing.T) {
	w := &errWriter{header: http.Header{}}
	s, err := NewStream(w)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Frame(debate.Frame{Kind: debate.FrameTurnStart}); err == nil {
		t.Error("底层写入失败时 Send 应返回 error")
	}
	// 流已标记关闭，再次发送同样应失败。
	if err := s.Frame(debate.Frame{Kind: debate.FrameDone}); err == nil {
		t.Error("已关闭的流再次发送应返回 error")
	}
}

// nonFlusherWriter 实现 http.ResponseWriter 但不实现 http.Flusher，
// 用来验证 NewStream 会拒绝无法流式推送的 ResponseWriter。
type nonFlusherWriter struct {
	header http.Header
}

func (n *nonFlusherWriter) Header() http.Header         { return n.header }
func (n *nonFlusherWriter) Write(p []byte) (int, error) { return len(p), nil }
func (n *nonFlusherWriter) WriteHeader(int)             {}

// TestNewStreamRequiresFlusher 验证不支持 Flush 的 ResponseWriter 会被直接拒绝，
// 而不是默默退化成"一次性吐出全部内容"（那会毁掉打字机体验）。
func TestNewStreamRequiresFlusher(t *testing.T) {
	if _, err := NewStream(&nonFlusherWriter{header: http.Header{}}); err == nil {
		t.Error("不支持 Flush 的 ResponseWriter 应当被拒绝")
	}
}
