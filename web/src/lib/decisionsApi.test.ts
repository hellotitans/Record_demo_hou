// decisionsApi 的测试重点是"降级"：后端 500、网络中断、返回结构不合法，
// 都必须变成 null 而不是抛异常 —— 档案挂了不能连累辩论。

import { describe, it, expect, vi, afterEach } from 'vitest'
import type { DecisionRecord, DecisionStats } from '../types'
import { listDecisions, createDecision, setFollowup, fetchStats } from './decisionsApi'

// 不依赖真实 Response：只实现被用到的 ok / status / json，行为完全确定。
function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as unknown as Response
}

const rec: DecisionRecord = {
  id: 'd1',
  question: '买房还是租房？',
  optionA: '买房',
  optionB: '租房',
  createdAt: '2026-06-01T10:00:00Z',
  assumptionCount: 2,
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('listDecisions', () => {
  it('成功时返回决策数组，并请求 GET /api/decisions', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse({ decisions: [rec] }))
    const got = await listDecisions({ fetchImpl, timeoutMs: 10 })
    expect(got).toEqual([rec])
    expect(fetchImpl).toHaveBeenCalledTimes(1)
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/decisions')
    expect(fetchImpl.mock.calls[0][1]?.method).toBe('GET')
  })

  it('后端 500 → 返回 null 而不是抛异常', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse({ error: 'internal error' }, 500))
    await expect(listDecisions({ fetchImpl, timeoutMs: 10 })).resolves.toBeNull()
  })

  it('网络中断（fetch reject）→ 返回 null', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => {
      throw new Error('network down')
    })
    await expect(listDecisions({ fetchImpl, timeoutMs: 10 })).resolves.toBeNull()
  })

  it('返回结构不合法 → 返回 null', async () => {
    const cases = [
      { error: 'bad request' }, // 后端错误体，没有 decisions 字段
      { decisions: 'not an array' },
      null,
    ]
    for (const body of cases) {
      const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse(body))
      await expect(listDecisions({ fetchImpl, timeoutMs: 10 })).resolves.toBeNull()
    }
  })

  it('过滤掉形状不合法的条目，一条脏数据不毁掉整个列表', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () =>
      jsonResponse({ decisions: [rec, { id: 'x' }, null, { ...rec, id: 'd2' }] }),
    )
    const got = await listDecisions({ fetchImpl, timeoutMs: 10 })
    expect(got?.map((r) => r.id)).toEqual(['d1', 'd2'])
  })

  it('baseURL 前缀生效（便于部署到子路径）', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse({ decisions: [] }))
    await listDecisions({ fetchImpl, baseURL: 'https://api.example.com', timeoutMs: 10 })
    expect(fetchImpl.mock.calls[0][0]).toBe('https://api.example.com/api/decisions')
  })

  it('环境没有 fetch → 返回 null', async () => {
    vi.stubGlobal('fetch', undefined)
    await expect(listDecisions({ timeoutMs: 10 })).resolves.toBeNull()
  })
})

describe('createDecision', () => {
  it('成功时返回落库后的记录，并 POST 完整档案字段', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse({ decision: rec }, 201))
    const got = await createDecision(rec, { fetchImpl, timeoutMs: 10 })
    expect(got).toEqual(rec)
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/decisions')
    expect(fetchImpl.mock.calls[0][1]?.method).toBe('POST')
    expect(JSON.parse(fetchImpl.mock.calls[0][1]?.body as string)).toEqual(rec)
  })

  it('后端 400 → 返回 null', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () =>
      jsonResponse({ error: 'store: invalid decision record: id is required' }, 400),
    )
    await expect(createDecision(rec, { fetchImpl, timeoutMs: 10 })).resolves.toBeNull()
  })

  it('响应缺 decision 字段 → 返回 null', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse({}, 200))
    await expect(createDecision(rec, { fetchImpl, timeoutMs: 10 })).resolves.toBeNull()
  })
})

describe('setFollowup', () => {
  const withFollowup: DecisionRecord = {
    ...rec,
    followup: { regret: 'yes', answeredAt: '2026-09-01T16:44:59Z' },
  }

  it('成功时返回带回访结果的记录，并 POST 到正确的 URL', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse({ decision: withFollowup }))
    const got = await setFollowup('d1', 'yes', { fetchImpl, timeoutMs: 10 })
    expect(got?.followup?.regret).toBe('yes')
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/decisions/d1/followup')
    expect(fetchImpl.mock.calls[0][1]?.method).toBe('POST')
    expect(JSON.parse(fetchImpl.mock.calls[0][1]?.body as string)).toEqual({ regret: 'yes' })
  })

  it('对 id 做 URL 编码（防止特殊字符破坏路径）', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse({ decision: rec }))
    await setFollowup('a/b?c', 'no', { fetchImpl, timeoutMs: 10 })
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/decisions/a%2Fb%3Fc/followup')
  })

  it('未知 id（404）→ 返回 null', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse({ error: 'store: decision not found' }, 404))
    await expect(setFollowup('nope', 'yes', { fetchImpl, timeoutMs: 10 })).resolves.toBeNull()
  })
})

// P8 后悔率看板的数据源。同样遵守"失败一律降级为 null"的约定：
// 看板是锦上添花，后端挂了它整块不渲染就是了，不该弹红色报错。
describe('fetchStats', () => {
  const full: DecisionStats = {
    total: 3,
    answered: 2,
    pending: 1,
    regretYes: 1,
    regretNo: 1,
    regretRate: 0.5,
    byCategory: [{ category: 'phone', total: 2, answered: 2, regretYes: 1, regretRate: 0.5 }],
    byMonth: [{ month: '2026-08', total: 3, answered: 2, regretYes: 1, regretRate: 0.5 }],
    regretted: [{ ...rec, category: 'phone', followup: { regret: 'yes', answeredAt: '2026-09-09T00:00:00Z' } }],
  }

  it('成功时返回统计，并请求 GET /api/decisions/stats', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse({ stats: full }))
    const got = await fetchStats({ fetchImpl, timeoutMs: 10 })
    expect(got).toEqual(full)
    expect(fetchImpl.mock.calls[0][0]).toBe('/api/decisions/stats')
    expect(fetchImpl.mock.calls[0][1]?.method).toBe('GET')
  })

  it('后端 500 → 返回 null', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse({ error: 'internal error' }, 500))
    await expect(fetchStats({ fetchImpl, timeoutMs: 10 })).resolves.toBeNull()
  })

  it('网络中断 → 返回 null', async () => {
    const fetchImpl = vi.fn<typeof fetch>(async () => {
      throw new Error('network down')
    })
    await expect(fetchStats({ fetchImpl, timeoutMs: 10 })).resolves.toBeNull()
  })

  it('结构不合法 → 返回 null（三个数组必须是数组，看板会直接 .map）', async () => {
    const bad: unknown[] = [
      { error: 'boom' }, // 后端错误体，没有 stats 字段
      null,
      { stats: { total: 1 } }, // 缺三个数组
      { stats: { ...full, byCategory: null, byMonth: null, regretted: null } },
      { stats: { ...full, byMonth: 'not an array' } },
    ]
    for (const body of bad) {
      const fetchImpl = vi.fn<typeof fetch>(async () => jsonResponse(body))
      await expect(fetchStats({ fetchImpl, timeoutMs: 10 })).resolves.toBeNull()
    }
  })

  it('没有注入 fetch 的环境（SSR/老浏览器）→ 返回 null 而不是抛异常', async () => {
    vi.stubGlobal('fetch', undefined)
    await expect(fetchStats({ timeoutMs: 10 })).resolves.toBeNull()
  })
})
