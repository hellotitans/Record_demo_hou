package telemetry

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

const (
	// MaxBodyBytes 限制单次上报体积，防止被打爆内存。256 KiB。
	MaxBodyBytes = 1 << 18
	// MaxBatchEvents 限制单次上报的事件条数。
	MaxBatchEvents = 100
)

// ingestResponse 告诉客户端有多少条被丢弃。
// 前端可以据此降级：丢弃率持续偏高时降低上报频率。
type ingestResponse struct {
	Accepted int `json:"accepted"`
	Dropped  int `json:"dropped"`
}

// ServeHTTP 接收埋点上报。
//
// 同时支持单条事件和批量上报：
//
//	{"event_name":"argument_feedback", ...}
//	{"events":[{...}, {...}]}
//
// 批量是为了配合 navigator.sendBeacon —— 辩论一场会产生几十条事件，
// 逐条发请求既不经济，也会在页面关闭时丢失。
func (c *Collector) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 多读 1 字节用于判断是否超限，而不是依赖 Content-Length（客户端可以不给）。
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxBodyBytes+1))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(body) > MaxBodyBytes {
		http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
		return
	}

	events, err := decodeEvents(body)
	if err != nil {
		http.Error(w, "bad request: "+err.Error(), http.StatusBadRequest)
		return
	}
	if len(events) > MaxBatchEvents {
		http.Error(w, "too many events", http.StatusRequestEntityTooLarge)
		return
	}

	var resp ingestResponse
	for _, ev := range events {
		if err := ev.Validate(); err != nil {
			// 整批拒绝而不是跳过坏的那条：客户端有 bug 就该暴露出来，
			// 静默丢弃只会让数据缺口在几周后才被发现。
			http.Error(w, "invalid event: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := c.Emit(ev); err != nil {
			resp.Dropped++
			continue
		}
		resp.Accepted++
	}

	w.Header().Set("Content-Type", "application/json")
	// 202 而不是 200：事件已接收但不保证已落盘，语义更准确。
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(resp)
}

// decodeEvents 解析单条事件、事件数组，或 {"events":[...]} 包装。
func decodeEvents(body []byte) ([]Event, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, errors.New("empty body")
	}

	switch trimmed[0] {
	case '[':
		var events []Event
		if err := json.Unmarshal(trimmed, &events); err != nil {
			return nil, err
		}
		return events, nil

	case '{':
		var wrapper struct {
			Events []Event `json:"events"`
		}
		if err := json.Unmarshal(trimmed, &wrapper); err != nil {
			return nil, err
		}
		if len(wrapper.Events) > 0 {
			return wrapper.Events, nil
		}
		// 没有 events 字段，按单条事件解析。
		var ev Event
		if err := json.Unmarshal(trimmed, &ev); err != nil {
			return nil, err
		}
		return []Event{ev}, nil

	default:
		return nil, errors.New("body must be a JSON object or array")
	}
}
