// 决策档案 API：与后端 internal/httpsrv/decisions.go 的 JSON 形状一一对应。
//
// 降级约定（重要）：本文件所有函数在网络失败、后端 5xx、返回结构不合法时
// 一律返回 null，绝不抛异常。
//
// 理由：档案是"锦上添花"的数据，辩论才是主流程。后端没起、断网、
// 后端报错，都不应该让用户在辩论页上看到一个红色报错弹窗。
// 调用方拿到 null 就回退到本地缓存 —— 见 archive.ts 的 hydrateArchive。

import type { DecisionRecord, DecisionStats } from '../types'

export interface ApiOptions {
  /** API 前缀，默认空串（走 vite dev proxy 转发到后端 /api）。 */
  baseURL?: string
  /** 注入用，便于单测不打真实网络。 */
  fetchImpl?: typeof fetch
  /** 超时毫秒，默认 5000。档案请求不该让用户等。 */
  timeoutMs?: number
}

const DEFAULT_TIMEOUT_MS = 5000

// 结构校验：后端挂了、返回了 {"error":...} 或半截 JSON 时，
// 这里要挡住，不能让脏数据混进档案列表。
function isRecord(v: unknown): v is DecisionRecord {
  if (typeof v !== 'object' || v === null) return false
  const r = v as Partial<DecisionRecord>
  return typeof r.id === 'string' && typeof r.question === 'string'
}

async function call<T>(
  path: string,
  init: RequestInit,
  parse: (json: unknown) => T | null,
  opts: ApiOptions = {},
): Promise<T | null> {
  const doFetch = opts.fetchImpl ?? globalThis.fetch
  if (typeof doFetch !== 'function') return null

  const ctrl = new AbortController()
  const timer = setTimeout(() => ctrl.abort(), opts.timeoutMs ?? DEFAULT_TIMEOUT_MS)
  try {
    const res = await doFetch((opts.baseURL ?? '') + path, {
      ...init,
      signal: ctrl.signal,
      headers: { 'Content-Type': 'application/json', ...(init.headers ?? {}) },
    })
    if (!res.ok) return null
    return parse(await res.json())
  } catch {
    // 网络错误 / 超时 / 坏 JSON：统一降级为 null，由调用方回退本地。
    return null
  } finally {
    clearTimeout(timer)
  }
}

/** 拉取全部决策（新→旧）。失败返回 null。 */
export async function listDecisions(opts: ApiOptions = {}): Promise<DecisionRecord[] | null> {
  return call<DecisionRecord[]>(
    '/api/decisions',
    { method: 'GET' },
    (json) => {
      const list = (json as { decisions?: unknown } | null)?.decisions
      if (!Array.isArray(list)) return null
      // 过滤掉形状不对的条目，一条脏数据不该毁掉整个列表。
      return list.filter(isRecord)
    },
    opts,
  )
}

/**
 * 创建/更新一条决策（按 id 幂等）。
 * 返回后端落库后的记录；失败返回 null（此时调用方应已先落本地缓存）。
 */
export async function createDecision(
  rec: DecisionRecord,
  opts: ApiOptions = {},
): Promise<DecisionRecord | null> {
  return call<DecisionRecord>(
    '/api/decisions',
    { method: 'POST', body: JSON.stringify(rec) },
    (json) => {
      const d = (json as { decision?: unknown } | null)?.decision
      return isRecord(d) ? d : null
    },
    opts,
  )
}

// 统计的结构校验：三个数组必须真的是数组 —— 看板会直接 .map()，
// 后端若返回了 null 或半截结构，这里挡住，不让看板整块崩掉。
function isStats(v: unknown): v is DecisionStats {
  if (typeof v !== 'object' || v === null) return false
  const s = v as Partial<DecisionStats>
  return (
    typeof s.total === 'number' &&
    Array.isArray(s.byCategory) &&
    Array.isArray(s.byMonth) &&
    Array.isArray(s.regretted)
  )
}

/**
 * 拉取决策档案的聚合统计（P8 后悔率看板）。失败返回 null。
 *
 * 只认后端这一份数据，不在前端用本地档案重算一遍：
 * 聚合口径（后悔率的分母、90 天到期）在 Go 侧有单测守着，
 * 前端再实现一遍就等于把同一套语义复制成两份，迟早漂移。
 * 后端不可用时看板整块不渲染 —— 它是锦上添花，不该反过来成为主流程的负担。
 */
export async function fetchStats(opts: ApiOptions = {}): Promise<DecisionStats | null> {
  return call<DecisionStats>(
    '/api/decisions/stats',
    { method: 'GET' },
    (json) => {
      const s = (json as { stats?: unknown } | null)?.stats
      return isStats(s) ? s : null
    },
    opts,
  )
}

/** 记录回访结果（后悔/不后悔）。失败返回 null。 */
export async function setFollowup(
  id: string,
  regret: 'yes' | 'no',
  opts: ApiOptions = {},
): Promise<DecisionRecord | null> {
  return call<DecisionRecord>(
    `/api/decisions/${encodeURIComponent(id)}/followup`,
    { method: 'POST', body: JSON.stringify({ regret }) },
    (json) => {
      const d = (json as { decision?: unknown } | null)?.decision
      return isRecord(d) ? d : null
    },
    opts,
  )
}
