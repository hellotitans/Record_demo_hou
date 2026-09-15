package telemetry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestCollector(t *testing.T) *Collector {
	t.Helper()
	c, err := NewCollector(&recordingSink{}, Config{BatchSize: 1000, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func post(t *testing.T, c *Collector, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/events", strings.NewReader(body))
	rec := httptest.NewRecorder()
	c.ServeHTTP(rec, req)
	return rec
}

func TestServeHTTP(t *testing.T) {
	validJSON := `{"schema_version":1,"event_name":"argument_feedback","ts":"2026-09-01T13:20:41Z",` +
		`"zone":"decision","session_id":"s1","debate_id":"d1",` +
		`"properties":{"round":3,"side":"data","argument_id":"r3-data-2","verdict":"repetitive"}}`

	tests := []struct {
		name       string
		method     string
		body       string
		wantStatus int
		wantAccept int
	}{
		{
			name:       "single event",
			body:       validJSON,
			wantStatus: http.StatusAccepted,
			wantAccept: 1,
		},
		{
			name:       "wrapped batch",
			body:       `{"events":[` + validJSON + `,` + validJSON + `]}`,
			wantStatus: http.StatusAccepted,
			wantAccept: 2,
		},
		{
			name:       "bare array",
			body:       `[` + validJSON + `,` + validJSON + `,` + validJSON + `]`,
			wantStatus: http.StatusAccepted,
			wantAccept: 3,
		},
		{
			name:       "wrong method",
			method:     http.MethodGet,
			body:       validJSON,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "empty body",
			body:       "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "malformed json",
			body:       `{not json`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "not an object or array",
			body:       `"just a string"`,
			wantStatus: http.StatusBadRequest,
		},
		{
			// 整批拒绝而不是静默跳过：客户端有 bug 就该暴露出来。
			name:       "batch with one invalid event is rejected entirely",
			body:       `{"events":[` + validJSON + `,{"event_name":"x"}]}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "unknown zone rejected",
			body:       strings.Replace(validJSON, `"zone":"decision"`, `"zone":"ad"`, 1),
			wantStatus: http.StatusBadRequest,
		},
		{
			// execution 区（商业区）的事件同样合法，只是下游会分开存储。
			name:       "execution zone accepted",
			body:       strings.Replace(validJSON, `"zone":"decision"`, `"zone":"execution"`, 1),
			wantStatus: http.StatusAccepted,
			wantAccept: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newTestCollector(t)
			method := tt.method
			if method == "" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, "/api/v1/events", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			c.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus != http.StatusAccepted {
				return
			}
			var resp ingestResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if resp.Accepted != tt.wantAccept {
				t.Errorf("accepted = %d, want %d", resp.Accepted, tt.wantAccept)
			}
			if resp.Dropped != 0 {
				t.Errorf("dropped = %d, want 0", resp.Dropped)
			}
		})
	}
}

func TestServeHTTPRejectsOversizedBody(t *testing.T) {
	c := newTestCollector(t)

	// 构造一个超过 MaxBodyBytes 的合法数组，验证体积限制先于解析生效。
	event := `{"event_name":"argument_feedback","ts":"2026-09-01T13:20:41Z","zone":"decision","session_id":"s1","debate_id":"d1"}`
	var sb strings.Builder
	sb.WriteString("[")
	for sb.Len() < MaxBodyBytes {
		sb.WriteString(event)
		sb.WriteString(",")
	}
	sb.WriteString(event)
	sb.WriteString("]")

	rec := post(t, c, sb.String())
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestServeHTTPRejectsTooManyEvents(t *testing.T) {
	c := newTestCollector(t)

	event := `{"event_name":"argument_feedback","ts":"2026-09-01T13:20:41Z","zone":"decision","session_id":"s1","debate_id":"d1"}`
	var sb strings.Builder
	sb.WriteString("[")
	for i := 0; i < MaxBatchEvents+1; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(event)
	}
	sb.WriteString("]")

	rec := post(t, c, sb.String())
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
}

// 上报失败（缓冲区满）时接口仍返回 202 并如实报告丢弃数，
// 前端据此可以降级上报频率。
func TestServeHTTPReportsDroppedCount(t *testing.T) {
	entered := make(chan struct{}, 1)
	gate := make(chan struct{})
	c, err := NewCollector(&gatedSink{entered: entered, gate: gate}, Config{
		BufferSize:    1,
		BatchSize:     1,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	defer func() {
		close(gate)
		_ = c.Close()
	}()

	event := `{"event_name":"argument_feedback","ts":"2026-09-01T13:20:41Z","zone":"decision","session_id":"s1","debate_id":"d1"}`
	body := `{"events":[` + event + `,` + event + `,` + event + `]}`

	rec := post(t, c, body)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}

	var resp ingestResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Dropped == 0 {
		t.Errorf("dropped = 0, want > 0 when buffer overflows")
	}
	if resp.Accepted+resp.Dropped != 3 {
		t.Errorf("accepted+dropped = %d, want 3", resp.Accepted+resp.Dropped)
	}
}
