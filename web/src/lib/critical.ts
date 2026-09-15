// 临界点计算器：把辩论暴露的结构化假设变成可计算的"结论翻转沙盘"。
//
// 数据契约（来自后端 internal/debate.Assumption）：
//   假设即"某方结论成立的前提线" —— realValue OP threshold 为真时该方论证成立，
//   现实一旦越过 threshold，该方论证崩溃，结论可能翻转。
//
// 本模块是纯函数，不碰 DOM、不发请求，方便单测，也给 UI 当唯一事实源。
import type { Assumption, Side } from '../types'

export type Winner = Side | 'tie' | 'both-broken'

// 同一变量下、各方在该变量上的假设归并（通常 1 条，也可能双方各 1 条）。
export interface VariableGroup {
  variable: string
  assumptions: Assumption[]
}

export interface SliderRange {
  min: number
  max: number
  step: number
}

export interface WinnerResult {
  winner: Winner
  dataHolds: boolean
  lifeHolds: boolean
}

// 评估单条假设在给定变量取值下是否成立。
// 未知算子一律视为成立，避免幻觉或脏数据误触发翻转。
export function evalAssumption(a: Assumption, value: number): boolean {
  switch (a.operator) {
    case '>=':
      return value >= a.value
    case '<=':
      return value <= a.value
    case '>':
      return value > a.value
    case '<':
      return value < a.value
    default:
      return true
  }
}

// 某一方"所有"假设是否都成立。该方没有任何假设时返回 false（无前提即无法防守）。
export function sideHolds(
  assumptions: Assumption[],
  values: Record<string, number>,
  side: Side,
): boolean {
  const own = assumptions.filter((a) => a.side === side)
  if (own.length === 0) return false
  return own.every((a) => evalAssumption(a, values[a.variable] ?? a.value))
}

// 综合双方假设，算出当前取值下谁胜出。
export function computeWinner(assumptions: Assumption[], values: Record<string, number>): WinnerResult {
  const dataHolds = sideHolds(assumptions, values, 'data')
  const lifeHolds = sideHolds(assumptions, values, 'life')
  let winner: Winner
  if (dataHolds && lifeHolds) winner = 'tie'
  else if (!dataHolds && !lifeHolds) winner = 'both-broken'
  else winner = dataHolds ? 'data' : 'life'
  return { winner, dataHolds, lifeHolds }
}

// 按变量归并假设，供 UI 每组渲染一根滑块。
export function groupByVariable(assumptions: Assumption[]): VariableGroup[] {
  const map = new Map<string, Assumption[]>()
  for (const a of assumptions) {
    const arr = map.get(a.variable)
    if (arr) arr.push(a)
    else map.set(a.variable, [a])
  }
  return [...map.entries()].map(([variable, assumptions]) => ({ variable, assumptions }))
}

// 滑块范围：围绕该变量所有阈值，留出足够可拖拽空间以越过临界点。
export function groupRange(g: VariableGroup): SliderRange {
  const thresholds = g.assumptions.map((a) => a.value)
  const lo = Math.min(...thresholds)
  const hi = Math.max(...thresholds)
  const mid = (lo + hi) / 2
  const span = Math.max((hi - lo) / 2, Math.abs(mid) * 0.5, 1)
  return { min: lo - span, max: hi + span, step: niceStep(span) }
}

function niceStep(span: number): number {
  if (span >= 20) return 1
  if (span >= 2) return 0.1
  return 0.01
}

// 人类可读的"临界"提示：越过哪条线、朝哪个方向，结论可能翻转。
export function flipHint(a: Assumption): string {
  const v = `${a.value}${a.unit}`
  switch (a.operator) {
    case '>=':
      return `低于 ${v} 时结论可能翻转`
    case '>':
      return `≤ ${v} 时结论可能翻转`
    case '<=':
      return `高于 ${v} 时结论可能翻转`
    case '<':
      return `≥ ${v} 时结论可能翻转`
    default:
      return `越过 ${v} 时结论可能翻转`
  }
}

// 每个变量的默认取值：放在该组阈值中点（同变量双方各一条时为中界，默认即平局）。
export function defaultValues(assumptions: Assumption[]): Record<string, number> {
  const out: Record<string, number> = {}
  for (const g of groupByVariable(assumptions)) {
    const thresholds = g.assumptions.map((a) => a.value)
    out[g.variable] = (Math.min(...thresholds) + Math.max(...thresholds)) / 2
  }
  return out
}
