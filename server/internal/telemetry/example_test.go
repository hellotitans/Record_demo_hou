package telemetry

import (
	"fmt"
	"io"
	"time"
)

// ExampleCollector 演示典型用法：一场辩论跑完后批量上报，进程退出前 Close 刷盘。
func ExampleCollector() {
	// 用 io.Discard 保证示例输出可断言；换成 os.Stdout 就能看到真实的事件流。
	// 生产环境换成 ClickHouse / Kafka 实现。
	sink := NewWriterSink(io.Discard)

	c, err := NewCollector(sink, Config{
		BufferSize:    4096,
		BatchSize:     256,
		FlushInterval: 2 * time.Second,
	})
	if err != nil {
		fmt.Println("new collector:", err)
		return
	}

	const debateID = "debate-7f3a"
	const sessionID = "sess-9c21"

	// 辩论进行中：Emit 永不阻塞，就算下游 ClickHouse 挂了也只是丢埋点。
	_ = c.Emit(NewEvent(EventDebateStarted, ZoneDecision, sessionID, debateID,
		MustProperties(map[string]any{"category": "rent_vs_buy"})))

	// Layer 2 最有价值的一条：用户对单条论点的评价。
	// verdict=repetitive 说明主持人的"去重"职责失效了。
	_ = c.Emit(NewEvent(EventArgumentFeedback, ZoneDecision, sessionID, debateID,
		MustProperties(ArgumentFeedbackProps{
			Round:      3,
			Side:       "data",
			ArgumentID: "r3-data-2",
			Verdict:    VerdictRepetitive,
		})))

	// Layer 1 辩论质量体温计：逐轮阅读完成率。
	_ = c.Emit(NewEvent(EventRoundRead, ZoneDecision, sessionID, debateID,
		MustProperties(RoundReadProps{Round: 3, DwellMs: 8400, ScrollCompletion: 0.62})))

	// 决策完成。
	_ = c.Emit(NewEvent(EventCardGenerated, ZoneDecision, sessionID, debateID, nil))

	// 注意 zone 变成 execution：从这里开始才允许出现商业内容，
	// 且这些数据下游会分开存储，永不进入决策质量分析。
	_ = c.Emit(NewEvent(EventDeepCalcPurchased, ZoneExecution, sessionID, debateID,
		MustProperties(map[string]any{"price_cny": 29.9})))

	// 进程退出前必须 Close：它会刷空缓冲区并等待后台 goroutine 退出。
	if err := c.Close(); err != nil {
		fmt.Println("close:", err)
		return
	}

	st := c.Stats()
	fmt.Printf("written=%d dropped=%d failed=%d\n", st.Written, st.Dropped, st.Failed)
	// Output:
	// written=5 dropped=0 failed=0
}
