import { describe, it, expect, beforeEach, vi } from 'vitest'
import {
  loadArchive,
  saveArchive,
  addDecision,
  markFollowup,
  isDueForFollowup,
  hydrateArchive,
  pushDecision,
  pushFollowup,
} from './archive'
import { listDecisions, createDecision, setFollowup } from './decisionsApi'
import type { DecisionRecord } from '../types'

// 同步层测试不碰真实网络：把 API 层整个换成 mock。
vi.mock('./decisionsApi', () => ({
  listDecisions: vi.fn(),
  createDecision: vi.fn(),
  setFollowup: vi.fn(),
}))

const MONTH = 30 * 24 * 60 * 60 * 1000

function rec(over: Partial<DecisionRecord> = {}): DecisionRecord {
  return {
    id: 'd1',
    question: '买 iPhone 还是安卓？',
    optionA: '买iPhone',
    optionB: '买安卓',
    createdAt: new Date().toISOString(),
    assumptionCount: 2,
    ...over,
  }
}

describe('决策档案 localStorage 持久化', () => {
  beforeEach(() => localStorage.clear())

  it('空档案返回 []，损坏数据也降级为 []', () => {
    expect(loadArchive()).toEqual([])
    localStorage.setItem('decision-debate:archive', '{not json')
    expect(loadArchive()).toEqual([])
  })

  it('addDecision 写入后 loadArchive 能读回（round-trip）', () => {
    addDecision(rec())
    const list = loadArchive()
    expect(list).toHaveLength(1)
    expect(list[0].question).toBe('买 iPhone 还是安卓？')
  })

  it('addDecision 按 id 去重：同 id 再写只保留一条（最新的在前）', () => {
    addDecision(rec({ question: '旧' }))
    addDecision(rec({ question: '新' }))
    const list = loadArchive()
    expect(list).toHaveLength(1)
    expect(list[0].question).toBe('新')
  })

  it('saveArchive 直接存、loadArchive 读回多条', () => {
    saveArchive([rec({ id: 'a' }), rec({ id: 'b' })])
    expect(loadArchive().map((r) => r.id)).toEqual(['a', 'b'])
  })

  it('markFollowup 置后悔 + 回答时间', () => {
    addDecision(rec())
    const list = markFollowup('d1', 'yes', 1700000000000)
    expect(list[0].followup).toEqual({
      regret: 'yes',
      answeredAt: new Date(1700000000000).toISOString(),
    })
    // 持久化也生效
    expect(loadArchive()[0].followup?.regret).toBe('yes')
  })
})

describe('isDueForFollowup', () => {
  const now = 1_700_000_000_000

  it('近期决策不过期 → false', () => {
    const r = rec({ createdAt: new Date(now - 10 * 24 * 60 * 60 * 1000).toISOString() })
    expect(isDueForFollowup(r, 3, now)).toBe(false)
  })

  it('超过 3 个月且未回访 → true', () => {
    const r = rec({ createdAt: new Date(now - 4 * MONTH).toISOString() })
    expect(isDueForFollowup(r, 3, now)).toBe(true)
  })

  it('刚好满 3 个月边界 → true', () => {
    const r = rec({ createdAt: new Date(now - 3 * MONTH).toISOString() })
    expect(isDueForFollowup(r, 3, now)).toBe(true)
  })

  it('已回访 → 无论多久都 false', () => {
    const r = rec({
      createdAt: new Date(now - 10 * MONTH).toISOString(),
      followup: { regret: 'no', answeredAt: new Date().toISOString() },
    })
    expect(isDueForFollowup(r, 3, now)).toBe(false)
  })
})

// ---------------------------------------------------------------------------
describe('同步层：远端权威 + 本地兜底', () => {
  beforeEach(() => {
    localStorage.clear()
    vi.clearAllMocks()
  })

  it('hydrateArchive 远端成功 → 以远端为准并回写本地缓存', async () => {
    const remote = [rec({ id: 'r1', question: '远端的决策' })]
    vi.mocked(listDecisions).mockResolvedValue(remote)

    const got = await hydrateArchive()
    expect(got.map((r) => r.id)).toEqual(['r1'])
    // 关键：不仅返回远端，本地缓存也要被覆盖成远端，
    // 否则下次离线打开看到的还是旧数据。
    expect(loadArchive().map((r) => r.id)).toEqual(['r1'])
  })

  it('hydrateArchive 远端失败 → 降级用本地缓存', async () => {
    saveArchive([rec({ id: 'local1', question: '本地缓存的决策' })])
    vi.mocked(listDecisions).mockResolvedValue(null) // 后端没起 / 5xx / 断网

    const got = await hydrateArchive()
    expect(got.map((r) => r.id)).toEqual(['local1'])
  })

  it('pushDecision 远端成功 → 本地落库且内容以远端回执为准', async () => {
    const saved = rec({ id: 'd1', question: '远端确认过的问题' })
    vi.mocked(createDecision).mockResolvedValue(saved)

    const got = await pushDecision(rec({ id: 'd1', question: '本地先写的' }))
    expect(got[0].question).toBe('远端确认过的问题')
    expect(loadArchive()[0].question).toBe('远端确认过的问题')
    expect(createDecision).toHaveBeenCalledTimes(1)
  })

  it('pushDecision 远端失败 → 记录仍留在本地，不丢', async () => {
    vi.mocked(createDecision).mockResolvedValue(null)

    const got = await pushDecision(rec({ id: 'd1' }))
    expect(got).toHaveLength(1)
    // 后端挂了也要保住这条决策 —— 这是"本地兜底"的意义。
    expect(loadArchive().map((r) => r.id)).toEqual(['d1'])
  })

  it('pushFollowup 远端成功 → answeredAt 采用服务端时钟', async () => {
    addDecision(rec({ id: 'd1' }))
    vi.mocked(setFollowup).mockResolvedValue({
      ...rec({ id: 'd1' }),
      followup: { regret: 'yes', answeredAt: '2026-09-01T16:44:59Z' },
    })

    const got = await pushFollowup('d1', 'yes')
    expect(got[0].followup?.regret).toBe('yes')
    expect(got[0].followup?.answeredAt).toBe('2026-09-01T16:44:59Z')
    expect(loadArchive()[0].followup?.answeredAt).toBe('2026-09-01T16:44:59Z')
  })

  it('pushFollowup 远端失败 → 本地仍记录后悔（乐观更新不回滚）', async () => {
    addDecision(rec({ id: 'd1' }))
    vi.mocked(setFollowup).mockResolvedValue(null)

    const got = await pushFollowup('d1', 'no')
    expect(got[0].followup?.regret).toBe('no')
    expect(loadArchive()[0].followup?.regret).toBe('no')
  })

  it('远端确认不会把老决策顶到列表最前（就地替换不乱序）', async () => {
    addDecision(rec({ id: 'new1' }))
    addDecision(rec({ id: 'old1' }))
    expect(loadArchive().map((r) => r.id)).toEqual(['old1', 'new1'])

    // 对 old1 做回访，远端回执不该改变它在列表里的位置
    vi.mocked(setFollowup).mockResolvedValue({
      ...rec({ id: 'old1' }),
      followup: { regret: 'yes', answeredAt: '2026-09-01T16:44:59Z' },
    })
    const got = await pushFollowup('old1', 'yes')
    expect(got.map((r) => r.id)).toEqual(['old1', 'new1'])
  })
})
