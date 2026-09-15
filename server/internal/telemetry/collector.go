package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ErrBufferFull 表示缓冲区已满，事件被丢弃。
//
// 这不是异常，而是设计的一部分：埋点绝不阻塞主流程。
// 辩论是 60–120 秒的长任务，宁可丢一批埋点，也不能让用户在那里等。
// 丢弃会被计数，通过 Stats().Dropped 暴露出来 —— 如果它持续增长，
// 说明下游 Sink 太慢，需要扩容或调大缓冲区，而不是让它继续卡住主流程。
var ErrBufferFull = errors.New("telemetry: buffer full, event dropped")

// Config 收集器配置。
type Config struct {
	// BufferSize 是内存缓冲区容量。满则丢弃。默认 4096。
	BufferSize int
	// BatchSize 是单次刷盘的批大小。默认 256。
	BatchSize int
	// FlushInterval 是定时刷盘间隔，保证低流量下事件也能及时落地。默认 2s。
	FlushInterval time.Duration
	// FlushTimeout 是单次刷盘超时。超时即放弃这一批并计入 Failed。默认 3s。
	FlushTimeout time.Duration
}

func (c Config) withDefaults() Config {
	if c.BufferSize <= 0 {
		c.BufferSize = 4096
	}
	if c.BatchSize <= 0 {
		c.BatchSize = 256
	}
	if c.FlushInterval <= 0 {
		c.FlushInterval = 2 * time.Second
	}
	if c.FlushTimeout <= 0 {
		c.FlushTimeout = 3 * time.Second
	}
	return c
}

// Stats 是收集器运行指标，用于监控埋点管道本身的健康度。
type Stats struct {
	Buffered uint64 // 当前缓冲区中待刷盘的事件数
	Accepted uint64 // 累计接收
	Dropped  uint64 // 累计因缓冲区满而丢弃
	Written  uint64 // 累计成功写入 Sink
	Failed   uint64 // 累计写入失败
}

// Collector 是异步事件收集器。
//
// 生命周期：NewCollector 启动后台 goroutine；Close 刷空剩余事件并等待退出。
// Close 之后 Emit 仍然安全（会返回错误而不是 panic），
// 这样正在飞行中的辩论协程不需要做额外的同步判断。
type Collector struct {
	sink Sink
	cfg  Config

	events chan Event

	closeOnce sync.Once
	closeCh   chan struct{}
	doneCh    chan struct{}
	closed    atomic.Bool

	accepted atomic.Uint64
	dropped  atomic.Uint64
	written  atomic.Uint64
	failed   atomic.Uint64
}

// NewCollector 创建收集器并立即启动后台刷盘 goroutine。
func NewCollector(sink Sink, cfg Config) (*Collector, error) {
	if sink == nil {
		return nil, errors.New("telemetry: sink must not be nil")
	}
	cfg = cfg.withDefaults()
	c := &Collector{
		sink:    sink,
		cfg:     cfg,
		events:  make(chan Event, cfg.BufferSize),
		closeCh: make(chan struct{}),
		doneCh:  make(chan struct{}),
	}
	go c.run()
	return c, nil
}

// Emit 提交一个事件，永不阻塞。
//
// 校验失败返回 error（这是调用方的 bug，应该被修）；
// 缓冲区满返回 ErrBufferFull（这是系统压力，应该被监控，不是 bug）。
func (c *Collector) Emit(ev Event) error {
	if err := ev.Validate(); err != nil {
		return err
	}
	if c.closed.Load() {
		return errors.New("telemetry: collector is closed")
	}

	c.accepted.Add(1)

	select {
	case c.events <- ev:
		return nil
	default:
		c.dropped.Add(1)
		return ErrBufferFull
	}
}

// Stats 返回当前累计指标。
func (c *Collector) Stats() Stats {
	return Stats{
		Buffered: uint64(len(c.events)),
		Accepted: c.accepted.Load(),
		Dropped:  c.dropped.Load(),
		Written:  c.written.Load(),
		Failed:   c.failed.Load(),
	}
}

// Close 停止接收新事件，刷空缓冲区，关闭 Sink，并等待后台 goroutine 退出。
// 可重复调用，第二次及以后直接返回。
func (c *Collector) Close() error {
	c.closed.Store(true)
	c.closeOnce.Do(func() { close(c.closeCh) })
	<-c.doneCh
	return c.sink.Close()
}

// run 是后台刷盘循环，保证退出时 doneCh 一定被关闭。
func (c *Collector) run() {
	defer close(c.doneCh)

	ticker := time.NewTicker(c.cfg.FlushInterval)
	defer ticker.Stop()

	batch := make([]Event, 0, c.cfg.BatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		// 用独立超时而不是外部 context：刷盘是 Collector 自己的事，
		// 不该被请求级 context 的取消牵连。
		ctx, cancel := context.WithTimeout(context.Background(), c.cfg.FlushTimeout)
		err := c.sink.Write(ctx, batch)
		cancel()

		n := uint64(len(batch))
		batch = batch[:0]
		if err != nil {
			c.failed.Add(n)
			return
		}
		c.written.Add(n)
	}

	for {
		select {
		case ev := <-c.events:
			batch = append(batch, ev)
			if len(batch) >= c.cfg.BatchSize {
				flush()
			}

		case <-ticker.C:
			flush()

		case <-c.closeCh:
			// 先排空缓冲区中已接收的事件，再刷最后一次。
			// 注意用非阻塞读：此时不会再有新的 Emit 成功写入。
			for {
				select {
				case ev := <-c.events:
					batch = append(batch, ev)
					if len(batch) >= c.cfg.BatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		}
	}
}

// String 便于日志排查。
func (s Stats) String() string {
	return fmt.Sprintf("buffered=%d accepted=%d written=%d dropped=%d failed=%d",
		s.Buffered, s.Accepted, s.Written, s.Dropped, s.Failed)
}
