// 前端领域类型，与后端 internal/debate 的 JSON 形状一一对应。
// 改后端结构时，这里要同步；单测会守住两边不漂移。

export type Side = 'data' | 'life'

export interface Assignment {
  side: Side
  option_id: string
  reason: string
}

export interface Turn {
  round: number
  side: Side
  content: string
  model: string
  prompt_tokens: number
  completion_tokens: number
  latency_ms: number
}

export interface ModeratorNote {
  round: number
  unresolved: string[]
  repeated: string[]
}

export interface Assumption {
  id: string
  side: Side
  statement: string
  variable: string
  operator: string
  value: number
  unit: string
}

export interface Usage {
  prompt_tokens: number
  completion_tokens: number
  total_latency_ms: number
  estimated_cost_cny: number
}

// 决策维度：与后端 debate.Dimension 的 JSON 形状对应。
// weight 是 0–1 的权重，前端展示成百分比；source 记录维度来源（模板/模型/用户）。
export interface Dimension {
  key: string
  label: string
  weight: number // 0–1
  source: 'template' | 'model' | 'user'
}

// 品类模板：高频两难的预设。点选后预填问题/选项，并挂上该品类的决策维度。
export interface CategoryTemplate {
  id: string
  name: string
  emoji: string
  sampleQuestion: string
  sampleOptions: { a: string; b: string }
  dimensions: Dimension[]
}

// 决策档案：每场辩论落库的一条记录（纯前端 localStorage 持久化）。
// followup 是三个月后的"你后悔吗"回访结果，未回访时为 undefined。
export interface DecisionRecord {
  id: string // 即 dilemma_id
  question: string
  optionA: string
  optionB: string
  createdAt: string // ISO 时间
  assumptionCount: number
  followup?: {
    regret: 'yes' | 'no'
    answeredAt: string // ISO 时间
  }
  /**
   * 决策品类（P8 引入），取值是 templates.ts 里 CategoryTemplate 的 id。
   * 可选：P8 之前落库的老记录没有这个字段，看板里归入"未分类"。
   */
  category?: string
}

// --- P8 后悔率看板：与后端 internal/store/stats.go 的 Stats 一一对应 ---
// 改任一侧都要同步另一侧；后悔率的口径（分母是已回访数）在后端注释里有详细说明。

export interface CategoryStat {
  category: string // 空串表示"未分类"
  total: number
  answered: number
  regretYes: number
  regretRate: number // 0–1
}

export interface MonthStat {
  month: string // YYYY-MM，空串表示时间未知
  total: number
  answered: number
  regretYes: number
  regretRate: number // 0–1
}

export interface DecisionStats {
  total: number
  answered: number
  pending: number
  regretYes: number
  regretNo: number
  regretRate: number // 0–1
  byCategory: CategoryStat[]
  byMonth: MonthStat[]
  regretted: DecisionRecord[]
}

export interface DebateResult {
  dilemma_id: string
  assignments: Assignment[]
  turns: Turn[]
  notes: ModeratorNote[]
  assumptions: Assumption[]
  usage: Usage
}

export type FrameKind =
  | 'role_assign'
  | 'turn_start'
  | 'delta'
  | 'turn_end'
  | 'moderator'
  | 'assumption'
  | 'done'
  | 'error'

export interface Frame {
  kind: FrameKind
  round?: number
  side?: Side
  text?: string
  data?: unknown
}

export const SIDE_LABEL: Record<Side, string> = {
  data: '数据派',
  life: '生活派',
}

// 正在流式渲染、尚未结束的那一帧发言（打字机效果的数据源）。
export interface LiveTurn {
  round: number
  side: Side
  content: string
}
