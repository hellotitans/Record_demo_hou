// 决策档案持久化层：远端权威 + 本地兜底。
//
// P6·c 时这里是纯前端 localStorage；P7 起后端 /api/decisions 成为真数据源，
// localStorage 退居为"离线缓存 + 降级兜底"。读写策略：
//
//   - 读（hydrateArchive）：先拉远端，成功则以远端为准并回写本地；
//     失败（后端没起 / 断网 / 5xx）就用本地缓存，界面照常显示。
//   - 写（pushDecision / pushFollowup）：先落本地（乐观，永不丢），
//     再推远端；推送失败静默保留本地，下次水合时再带上。
//
// 一句话：后端在的时候跨设备一致，后端不在的时候功能不残废。

import type { DecisionRecord } from '../types'
import { createDecision, listDecisions, setFollowup, type ApiOptions } from './decisionsApi'

const STORAGE_KEY = 'decision-debate:archive'

// 读档案：任何异常（无 localStorage / JSON 损坏）都降级为 []，不让存档拖垮主流程。
export function loadArchive(): DecisionRecord[] {
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    if (!raw) return []
    const parsed = JSON.parse(raw)
    return Array.isArray(parsed) ? (parsed as DecisionRecord[]) : []
  } catch {
    return []
  }
}

export function saveArchive(list: DecisionRecord[]): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(list))
  } catch {
    // 配额满或环境不可用：静默丢弃，存档失败绝不影响辩论本身。
  }
}

// 新增一条决策；按 id 去重（同一场辩论重复 done 不重复入库）。最新的排在最前。
export function addDecision(rec: DecisionRecord): DecisionRecord[] {
  const next = [rec, ...loadArchive().filter((r) => r.id !== rec.id)]
  saveArchive(next)
  return next
}

// 回访：记录后悔/不后悔 + 回答时间。返回更新后的完整列表。
export function markFollowup(
  id: string,
  regret: 'yes' | 'no',
  now: number = Date.now(),
): DecisionRecord[] {
  const next = loadArchive().map((r) =>
    r.id === id ? { ...r, followup: { regret, answeredAt: new Date(now).toISOString() } } : r,
  )
  saveArchive(next)
  return next
}

// 是否该弹回访：已回访 → false；未回访且距今 ≥ months 个月 → true。
// now 可注入，方便单测不依赖真实时钟。
export function isDueForFollowup(
  rec: DecisionRecord,
  months = 3,
  now: number = Date.now(),
): boolean {
  if (rec.followup) return false
  const ageMs = now - new Date(rec.createdAt).getTime()
  return ageMs >= months * 30 * 24 * 60 * 60 * 1000
}

// ---------------------------------------------------------------------------
// 同步层：远端权威 + 本地兜底
// ---------------------------------------------------------------------------

// upsertLocal 就地替换同 id 记录（不存在则置顶插入）。
// 与 addDecision 的区别：远端回执用它，不改变记录已有位置 ——
// 否则"后端确认一下"就会把老决策顶到列表最前，用户看着像凭空冒出来一条。
function upsertLocal(rec: DecisionRecord): DecisionRecord[] {
  const list = loadArchive()
  const idx = list.findIndex((r) => r.id === rec.id)
  let next: DecisionRecord[]
  if (idx >= 0) {
    next = list.slice()
    next[idx] = { ...next[idx], ...rec }
  } else {
    next = [rec, ...list]
  }
  saveArchive(next)
  return next
}

/**
 * 页面加载时水合档案：以远端为准，远端不可用则退回本地缓存。
 * 返回的列表可直接 setState。
 */
export async function hydrateArchive(opts: ApiOptions = {}): Promise<DecisionRecord[]> {
  const remote = await listDecisions(opts)
  if (remote) {
    // 远端才是权威：本地可能有"推送失败只落了本地"的脏数据，用远端覆盖。
    saveArchive(remote)
    return remote
  }
  return loadArchive()
}

/**
 * 落库一条决策：先写本地（后端没起也不丢），再推远端。
 * 远端回执会就地更新本地，保证两边一致。
 */
export async function pushDecision(
  rec: DecisionRecord,
  opts: ApiOptions = {},
): Promise<DecisionRecord[]> {
  const optimistic = addDecision(rec)
  const saved = await createDecision(rec, opts)
  return saved ? upsertLocal(saved) : optimistic
}

/**
 * 记录回访：先写本地（用客户端时钟，界面立刻有反馈），再推远端。
 * 远端成功则用服务端时钟覆盖 answeredAt —— 跨设备场景下，
 * 只有服务端时钟是大家公认的那一个。
 */
export async function pushFollowup(
  id: string,
  regret: 'yes' | 'no',
  opts: ApiOptions = {},
): Promise<DecisionRecord[]> {
  const optimistic = markFollowup(id, regret)
  const saved = await setFollowup(id, regret, opts)
  return saved ? upsertLocal(saved) : optimistic
}
