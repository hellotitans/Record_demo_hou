package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
)

// Sink 是事件的落地目标。实现必须能安全地被 Collector 的单 goroutine 调用，
// 但 Close 可能与 Write 并发（Collector.Close 期间），因此实现需自行加锁。
type Sink interface {
	// Write 批量写入。返回的 error 只用于计数，不会重试：
	// 埋点数据丢一批可以接受，拖慢主流程不行。
	Write(ctx context.Context, events []Event) error
	Close() error
}

// WriterSink 按 JSON Lines 写入底层 io.Writer。
// 适用于 stdout、本地文件；生产环境可换成 ClickHouse / Kafka 实现。
type WriterSink struct {
	mu     sync.Mutex
	enc    *json.Encoder
	closer io.Closer
}

// NewWriterSink 包装一个 io.Writer。若 w 实现了 io.Closer，Close 时会一并关闭。
func NewWriterSink(w io.Writer) *WriterSink {
	closer, _ := w.(io.Closer)
	return &WriterSink{enc: json.NewEncoder(w), closer: closer}
}

func (s *WriterSink) Write(_ context.Context, events []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range events {
		if err := s.enc.Encode(events[i]); err != nil {
			return fmt.Errorf("writer sink: encode event %d: %w", i, err)
		}
	}
	return nil
}

func (s *WriterSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closer == nil {
		return nil
	}
	return s.closer.Close()
}

// MultiSink 把同一批事件分发给多个 Sink，用于"本地留一份 + 上报一份"。
type MultiSink struct {
	sinks []Sink
}

func NewMultiSink(sinks ...Sink) *MultiSink {
	return &MultiSink{sinks: sinks}
}

// Write 逐个写入，任一失败都继续（不让一个后端拖垮另一个），
// 最后用 errors.Join 汇总 —— 调用方只需要知道"有失败"，不需要知道是谁。
func (s *MultiSink) Write(ctx context.Context, events []Event) error {
	var errs []error
	for _, sink := range s.sinks {
		if err := sink.Write(ctx, events); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *MultiSink) Close() error {
	var errs []error
	for _, sink := range s.sinks {
		if err := sink.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
