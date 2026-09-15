package debate

// FrameKind 是推送给前端的事件类型，与 SSE 事件一一对应。
type FrameKind string

const (
	FrameRoleAssign FrameKind = "role_assign" // 角色与选项的对应关系及理由
	FrameTurnStart  FrameKind = "turn_start"  // 某一方开始发言
	FrameDelta      FrameKind = "delta"       // 流式文本片段（打字机效果）
	FrameTurnEnd    FrameKind = "turn_end"    // 某一方发言结束
	FrameModerator  FrameKind = "moderator"   // 主持人挑出的分歧点
	FrameAssumption FrameKind = "assumption"  // 提取出的关键假设，喂给临界点计算器
	FrameDone       FrameKind = "done"        // 全流程结束
	FrameError      FrameKind = "error"       // 出错，附带已完成的轮次
)

// Frame 是推送给前端的最小单元。
//
// Round + Side 让前端能把 delta 路由到正确的面板 —— 第一轮两个数派/生活派
// 是并行的，前端靠这两个字段同时渲染两块内容。
type Frame struct {
	Kind  FrameKind `json:"kind"`
	Round RoundNo   `json:"round,omitempty"`
	Side  Side      `json:"side,omitempty"`
	Text  string    `json:"text,omitempty"`
	Data  any       `json:"data,omitempty"`
}
