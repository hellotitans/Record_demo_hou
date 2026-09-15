package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// recordingSink 记录写入的事件，供测试断言。
type recordingSink struct {
	mu       sync.Mutex
	batches  [][]Event
	closed   bool
	failWith error
}

func (s *recordingSink) Write(_ context.Context, events []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failWith != nil {
		return s.failWith
	}
	cp := make([]Event, len(events))
	copy(cp, events)
	s.batches = append(s.batches, cp)
	return nil
}

func (s *recordingSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

func (s *recordingSink) all() []Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Event
	for _, b := range s.batches {
		out = append(out, b...)
	}
	return out
}

func (s *recordingSink) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// gatedSink 在进入 Write 时发信号，然后阻塞在 gate 上，
// 让测试能确定性地把后台 goroutine 卡住，从而精确复现缓冲区满的场景。
type gatedSink struct {
	entered chan struct{}
	gate    chan struct{}
}

func (s *gatedSink) Write(_ context.Context, _ []Event) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.gate
	return nil
}

func (s *gatedSink) Close() error { return nil }

func newTestEvent(name EventName) Event {
	return NewEvent(name, ZoneDecision, "sess-1", "debate-1", nil)
}

func TestNewCollectorRejectsNilSink(t *testing.T) {
	if _, err := NewCollector(nil, Config{}); err == nil {
		t.Fatal("NewCollector(nil) = nil error, want error")
	}
}

func TestCollectorFlushesOnClose(t *testing.T) {
	sink := &recordingSink{}
	c, err := NewCollector(sink, Config{BatchSize: 100, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	const n = 5
	for i := 0; i < n; i++ {
		if err := c.Emit(newTestEvent(EventRoundRead)); err != nil {
			t.Fatalf("Emit %d: %v", i, err)
		}
	}

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := len(sink.all()); got != n {
		t.Errorf("sink received %d events, want %d", got, n)
	}
	if !sink.isClosed() {
		t.Error("sink was not closed")
	}
	if st := c.Stats(); st.Written != n {
		t.Errorf("Stats.Written = %d, want %d (%s)", st.Written, n, st)
	}
}

// 批量刷盘：不关闭收集器也应把数据推出去，否则低流量下数据会一直留在内存里。
func TestCollectorFlushesOnBatchSize(t *testing.T) {
	sink := &recordingSink{}
	c, err := NewCollector(sink, Config{BatchSize: 2, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	defer c.Close()

	for i := 0; i < 2; i++ {
		if err := c.Emit(newTestEvent(EventRoundRead)); err != nil {
			t.Fatalf("Emit %d: %v", i, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(sink.all()) == 2 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("sink received %d events, want 2 (batch flush did not trigger)", len(sink.all()))
}

// 定时刷盘：事件数不足批大小时，也要在 FlushInterval 后落地。
func TestCollectorFlushesOnInterval(t *testing.T) {
	sink := &recordingSink{}
	c, err := NewCollector(sink, Config{BatchSize: 1000, FlushInterval: 20 * time.Millisecond})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	defer c.Close()

	if err := c.Emit(newTestEvent(EventRoundRead)); err != nil {
		t.Fatalf("Emit: %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(sink.all()) == 1 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("interval flush did not happen within 2s")
}

// 缓冲区满时丢弃而不是阻塞 —— 这是本包最重要的行为契约。
func TestCollectorDropsInsteadOfBlocking(t *testing.T) {
	entered := make(chan struct{}, 1)
	gate := make(chan struct{})
	sink := &gatedSink{entered: entered, gate: gate}

	const bufSize = 4
	c, err := NewCollector(sink, Config{BufferSize: bufSize, BatchSize: 1, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	// 第一条会被后台 goroutine 取走并卡在 Write 里。
	if err := c.Emit(newTestEvent(EventRoundRead)); err != nil {
		t.Fatalf("Emit first: %v", err)
	}
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("sink Write was never entered")
	}

	// 此时后台 goroutine 被卡住，缓冲区可以稳定填满。
	for i := 0; i < bufSize; i++ {
		if err := c.Emit(newTestEvent(EventRoundRead)); err != nil {
			t.Fatalf("Emit fill %d: %v", i, err)
		}
	}

	start := time.Now()
	if err := c.Emit(newTestEvent(EventRoundRead)); !errors.Is(err, ErrBufferFull) {
		t.Fatalf("Emit overflow = %v, want ErrBufferFull", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("Emit blocked for %v, want non-blocking", elapsed)
	}

	close(gate)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if st := c.Stats(); st.Dropped != 1 {
		t.Errorf("Stats.Dropped = %d, want 1 (%s)", st.Dropped, st)
	}
}

func TestCollectorEmitRejectsInvalidEvent(t *testing.T) {
	c, err := NewCollector(&recordingSink{}, Config{})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	defer c.Close()

	if err := c.Emit(Event{}); err == nil {
		t.Fatal("Emit(invalid) = nil, want error")
	}
	if st := c.Stats(); st.Accepted != 0 {
		t.Errorf("Stats.Accepted = %d, want 0 (rejected events must not count)", st.Accepted)
	}
}

// Close 之后 Emit 必须安全返回，不能 panic —— 辩论协程可能还在飞行中。
func TestCollectorEmitAfterCloseIsSafe(t *testing.T) {
	c, err := NewCollector(&recordingSink{}, Config{})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := c.Emit(newTestEvent(EventRoundRead)); err == nil {
		t.Fatal("Emit after Close = nil, want error")
	}
	// 重复 Close 也必须安全（shutdown 路径常被多处调用）。
	if err := c.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestCollectorCloseDrainsBufferedEvents(t *testing.T) {
	sink := &recordingSink{}
	c, err := NewCollector(sink, Config{BatchSize: 1000, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	const n = 7
	for i := 0; i < n; i++ {
		if err := c.Emit(newTestEvent(EventArgumentFeedback)); err != nil {
			t.Fatalf("Emit %d: %v", i, err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if got := len(sink.all()); got != n {
		t.Errorf("sink received %d events after Close, want %d", got, n)
	}
	if st := c.Stats(); st.Buffered != 0 {
		t.Errorf("Stats.Buffered = %d, want 0 (%s)", st.Buffered, st)
	}
}

// Sink 持续失败时，收集器必须继续运行而不是卡死或崩溃。
func TestCollectorSurvivesSinkFailure(t *testing.T) {
	sink := &recordingSink{failWith: errors.New("clickhouse down")}
	c, err := NewCollector(sink, Config{BatchSize: 1, FlushInterval: time.Hour})
	if err != nil {
		t.Fatalf("NewCollector: %v", err)
	}

	for i := 0; i < 3; i++ {
		if err := c.Emit(newTestEvent(EventRoundRead)); err != nil {
			t.Fatalf("Emit %d: %v", i, err)
		}
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	st := c.Stats()
	if st.Failed == 0 {
		t.Errorf("Stats.Failed = 0, want > 0 (%s)", st)
	}
}

func TestStatsString(t *testing.T) {
	got := Stats{Buffered: 1, Accepted: 2, Written: 3, Dropped: 4, Failed: 5}.String()
	want := "buffered=1 accepted=2 written=3 dropped=4 failed=5"
	if got != want {
		t.Errorf("Stats.String() = %q, want %q", got, want)
	}
}
