import { describe, it, expect, vi, beforeEach } from 'vitest'
import { render, screen, waitFor, fireEvent } from '@testing-library/react'
import userEvent from '@testing-library/user-event'
import App from './App'
import { streamDebate } from './lib/sse'
import { createDecision, setFollowup, fetchStats } from './lib/decisionsApi'
import type { Frame } from './types'

// 用录制的帧驱动 streamDebate，不依赖真实网络/后端。
const h = vi.hoisted(() => ({ frames: [] as Frame[] }))

vi.mock('./lib/sse', () => ({
  streamDebate: vi.fn(async (_url: string, _body: unknown, handlers: { onFrame: (f: Frame) => void }) => {
    for (const f of h.frames) handlers.onFrame(f)
  }),
}))

// 档案 API 也换成 mock：默认"后端不可用"（返回 null），
// 这样既有测试不受影响，新测试再单独让它成功。
vi.mock('./lib/decisionsApi', () => ({
  listDecisions: vi.fn(async () => null),
  createDecision: vi.fn(async () => null),
  setFollowup: vi.fn(async () => null),
  fetchStats: vi.fn(async () => null),
}))

function fillAndStart() {
  const q = screen.getByPlaceholderText(/你纠结的问题/)
  const a = screen.getByPlaceholderText('选项 A')
  const b = screen.getByPlaceholderText('选项 B')
  const btn = screen.getByRole('button', { name: '开始辩论' })
  return userEvent.type(q, '买 iPhone 还是安卓旗舰？').then(() =>
    userEvent.type(a, '买 iPhone').then(() =>
      userEvent.type(b, '买安卓').then(() => userEvent.click(btn)),
    ),
  )
}

beforeEach(() => {
  h.frames = []
  localStorage.clear()
  // 只清调用记录，不重置实现（档案 API 的默认实现是"后端不可用返回 null"）。
  vi.clearAllMocks()
})

describe('App 端到端（mock SSE）', () => {
  it('完整流程：输入 → 看到发言 → 决策卡片出现并含关键假设', async () => {
    h.frames = [
      {
        kind: 'role_assign',
        data: [
          { side: 'data', option_id: 'a', reason: 'r' },
          { side: 'life', option_id: 'b', reason: 'r' },
        ],
      },
      { kind: 'turn_start', round: 1, side: 'data' },
      { kind: 'delta', round: 1, side: 'data', text: '数据说买安卓' },
      {
        kind: 'turn_end',
        round: 1,
        side: 'data',
        data: { round: 1, side: 'data', content: '数据说买安卓', model: 'm', prompt_tokens: 1, completion_tokens: 1, latency_ms: 1 },
      },
      { kind: 'turn_start', round: 1, side: 'life' },
      { kind: 'delta', round: 1, side: 'life', text: '生活说买iPhone' },
      {
        kind: 'turn_end',
        round: 1,
        side: 'life',
        data: { round: 1, side: 'life', content: '生活说买iPhone', model: 'm', prompt_tokens: 1, completion_tokens: 1, latency_ms: 1 },
      },
      { kind: 'moderator', data: { round: 1, unresolved: ['价格敏感度'], repeated: [] } },
      {
        kind: 'assumption',
        data: { id: 'as1', side: 'data', statement: '安卓三年省2000', variable: '三年节省', operator: '>=', value: 2000, unit: '元' },
      },
      {
        kind: 'done',
        data: {
          dilemma_id: 'd1',
          assignments: [],
          turns: [
            { round: 1, side: 'data', content: '数据说买安卓', model: 'm', prompt_tokens: 1, completion_tokens: 1, latency_ms: 1 },
            { round: 1, side: 'life', content: '生活说买iPhone', model: 'm', prompt_tokens: 1, completion_tokens: 1, latency_ms: 1 },
          ],
          notes: [{ round: 1, unresolved: ['价格敏感度'], repeated: [] }],
          assumptions: [{ id: 'as1', side: 'data', statement: '安卓三年省2000', variable: '三年节省', operator: '>=', value: 2000, unit: '元' }],
          usage: { prompt_tokens: 2, completion_tokens: 2, total_latency_ms: 2000, estimated_cost_cny: 0.002 },
        },
      },
    ]

    render(<App />)
    await fillAndStart()

    // 发言内容出现在流里（辩论流 + 决策卡片都会呈现最终立场，故用 findAllByText 取首个）
    expect((await screen.findAllByText('数据说买安卓')).length).toBeGreaterThan(0)
    expect((await screen.findAllByText('生活说买iPhone')).length).toBeGreaterThan(0)

    // 辩论结束，决策卡片出现，关键假设被正确渲染
    // （决策卡片与临界点计算器都会呈现假设文案，故用 getAllByText 取首个）
    expect(await screen.findByText('决策卡片')).toBeInTheDocument()
    await waitFor(() => expect(screen.getAllByText(/安卓三年省2000/).length).toBeGreaterThan(0))
  })

  it('空输入不触发辩论', async () => {
    render(<App />)
    const btn = screen.getByRole('button', { name: '开始辩论' }) as HTMLButtonElement
    expect(btn.disabled).toBe(true)
  })

  it('打字机：delta 增量累积成完整内容', async () => {
    h.frames = [
      { kind: 'turn_start', round: 1, side: 'data' },
      { kind: 'delta', round: 1, side: 'data', text: '你' },
      { kind: 'delta', round: 1, side: 'data', text: '好' },
      {
        kind: 'turn_end',
        round: 1,
        side: 'data',
        data: { round: 1, side: 'data', content: '你好', model: 'm', prompt_tokens: 1, completion_tokens: 1, latency_ms: 1 },
      },
      {
        kind: 'done',
        data: {
          dilemma_id: 'd',
          assignments: [],
          turns: [{ round: 1, side: 'data', content: '你好', model: 'm', prompt_tokens: 1, completion_tokens: 1, latency_ms: 1 }],
          notes: [],
          assumptions: [],
          usage: { prompt_tokens: 1, completion_tokens: 1, total_latency_ms: 100, estimated_cost_cny: 0.001 },
        },
      },
    ]
    render(<App />)
    await fillAndStart()
    // 完整内容最终出现（增量累积的结果；辩论流与决策卡片都会呈现，取首个）
    expect((await screen.findAllByText('你好')).length).toBeGreaterThan(0)
  })

  it('选品类后提交：请求体带 category 与 dimensions', async () => {
    render(<App />)
    // 选中「数码产品购买」品类 → 自动预填问题/选项 + 维度
    fireEvent.click(screen.getByLabelText('品类 数码产品购买'))
    await fillAndStart()

    const calls = vi.mocked(streamDebate).mock.calls
    expect(calls.length).toBeGreaterThan(0)
    // mock.calls 在同文件测试间累积，取最后一次调用（即本次开始辩论的请求）。
    const body = calls[calls.length - 1][1] as {
      category?: string
      dimensions?: Array<{ source: string }>
    }
    expect(body.category).toBe('phone')
    expect(Array.isArray(body.dimensions)).toBe(true)
    expect((body.dimensions ?? []).length).toBeGreaterThanOrEqual(2)
    expect((body.dimensions ?? [])[0].source).toBe('template')
  })

  it('完整流程结束后，决策自动存进档案（localStorage 可读回）', async () => {
    h.frames = [
      {
        kind: 'done',
        data: {
          dilemma_id: 'd1',
          assignments: [],
          turns: [],
          notes: [],
          assumptions: [
            { id: 'as1', side: 'data', statement: '安卓三年省2000', variable: '三年节省', operator: '>=', value: 2000, unit: '元' },
          ],
          usage: { prompt_tokens: 1, completion_tokens: 1, total_latency_ms: 100, estimated_cost_cny: 0.001 },
        },
      },
    ]
    render(<App />)
    await fillAndStart()
    // 等辩论进入 done（决策卡片出现即代表 done）
    await screen.findByText('决策卡片')
    const raw = localStorage.getItem('decision-debate:archive')
    expect(raw).toBeTruthy()
    const list = JSON.parse(raw!) as Array<{ id: string; question: string; assumptionCount: number }>
    expect(list).toHaveLength(1)
    expect(list[0].id).toBe('d1')
    expect(list[0].question).toBe('买 iPhone 还是安卓旗舰？')
    expect(list[0].assumptionCount).toBe(1)
  })

  it('辩论结束后把决策推送到后端（跨设备同步）', async () => {
    h.frames = [
      {
        kind: 'done',
        data: {
          dilemma_id: 'd1',
          assignments: [],
          turns: [],
          notes: [],
          assumptions: [
            { id: 'as1', side: 'data', statement: '安卓三年省2000', variable: '三年节省', operator: '>=', value: 2000, unit: '元' },
          ],
          usage: { prompt_tokens: 1, completion_tokens: 1, total_latency_ms: 100, estimated_cost_cny: 0.001 },
        },
      },
    ]
    vi.mocked(createDecision).mockResolvedValue({
      id: 'd1',
      question: '买 iPhone 还是安卓旗舰？',
      optionA: '买 iPhone',
      optionB: '买安卓',
      createdAt: '2026-09-01T00:00:00Z',
      assumptionCount: 1,
    })

    render(<App />)
    await fillAndStart()
    await screen.findByText('决策卡片')

    await waitFor(() => expect(createDecision).toHaveBeenCalledTimes(1))
    const sent = vi.mocked(createDecision).mock.calls[0][0]
    expect(sent.id).toBe('d1')
    expect(sent.question).toBe('买 iPhone 还是安卓旗舰？')
    expect(sent.assumptionCount).toBe(1)
  })

  // P8 的核心：品类要跟着决策一起落库，看板才能算"哪类决策你最容易后悔"。
  // 这条同时守着一个容易踩的坑 —— start() 一开头就 reset() 清掉了 category state，
  // 而落库发生在几十秒后的 done 帧；不先用 ref 抓住，这里拿到的就是 null。
  it('落库时带上本场品类（选了品类 → category=phone）', async () => {
    h.frames = [
      {
        kind: 'done',
        data: {
          dilemma_id: 'd2',
          assignments: [],
          turns: [],
          notes: [],
          assumptions: [],
          usage: { prompt_tokens: 1, completion_tokens: 1, total_latency_ms: 100, estimated_cost_cny: 0.001 },
        },
      },
    ]
    vi.mocked(createDecision).mockResolvedValue({
      id: 'd2',
      question: '买 iPhone 还是安卓旗舰？',
      optionA: '买 iPhone',
      optionB: '买安卓',
      createdAt: '2026-09-10T00:00:00Z',
      assumptionCount: 0,
    })

    render(<App />)
    fireEvent.click(screen.getByLabelText('品类 数码产品购买'))
    await fillAndStart()
    await screen.findByText('决策卡片')

    await waitFor(() => expect(createDecision).toHaveBeenCalled())
    const calls = vi.mocked(createDecision).mock.calls
    expect(calls[calls.length - 1][0].category).toBe('phone')
  })

  it('没选品类（自定义）时落库不带 category，而不是带空串', async () => {
    h.frames = [
      {
        kind: 'done',
        data: {
          dilemma_id: 'd3',
          assignments: [],
          turns: [],
          notes: [],
          assumptions: [],
          usage: { prompt_tokens: 1, completion_tokens: 1, total_latency_ms: 100, estimated_cost_cny: 0.001 },
        },
      },
    ]
    vi.mocked(createDecision).mockResolvedValue({
      id: 'd3',
      question: '买 iPhone 还是安卓旗舰？',
      optionA: '买 iPhone',
      optionB: '买安卓',
      createdAt: '2026-09-10T00:00:00Z',
      assumptionCount: 0,
    })

    render(<App />)
    await fillAndStart()
    await screen.findByText('决策卡片')

    await waitFor(() => expect(createDecision).toHaveBeenCalled())
    const calls = vi.mocked(createDecision).mock.calls
    // 空串会让老记录和新记录混进同一个"未分类"桶，语义就糊了：宁可没这个字段。
    expect(calls[calls.length - 1][0].category).toBeUndefined()
  })

  // 看板的接线测试。其余测试里 fetchStats 默认返回 null（当作后端不可用），
  // 看板整块不渲染；这条是唯一覆盖"成功路径"的，守的是 App → StatsBoard 这段接线。
  // 用 Once 而不是 mockResolvedValue：后者会改掉默认实现，污染后面的测试。
  it('后端返回统计时，首页渲染出后悔率看板', async () => {
    vi.mocked(fetchStats).mockResolvedValueOnce({
      total: 5,
      answered: 3,
      pending: 1,
      regretYes: 2,
      regretNo: 1,
      regretRate: 2 / 3,
      byCategory: [{ category: 'phone', total: 2, answered: 2, regretYes: 1, regretRate: 0.5 }],
      byMonth: [{ month: '2026-08', total: 2, answered: 2, regretYes: 1, regretRate: 0.5 }],
      regretted: [],
    })

    render(<App />)
    expect(await screen.findByText('决策复盘')).toBeTruthy()
    expect(screen.getByText('总决策')).toBeTruthy()
    expect(screen.getByText('已回访')).toBeTruthy()
    expect(screen.getByText('后悔率')).toBeTruthy()
    expect(screen.getByText('待回访')).toBeTruthy()
  })

  it('点击回访 → 结果推送到后端，并先落在本地', async () => {
    // 造一条 4 个月前的决策，档案列表会为它弹出回访卡。
    const old = {
      id: 'old1',
      question: '很久以前做的决定',
      optionA: 'A',
      optionB: 'B',
      createdAt: new Date(Date.now() - 4 * 30 * 24 * 60 * 60 * 1000).toISOString(),
      assumptionCount: 1,
    }
    localStorage.setItem('decision-debate:archive', JSON.stringify([old]))
    vi.mocked(setFollowup).mockResolvedValue({
      ...old,
      followup: { regret: 'yes', answeredAt: '2026-09-01T00:00:00Z' },
    })

    render(<App />)
    fireEvent.click(await screen.findByLabelText('回访-后悔'))

    await waitFor(() => expect(setFollowup).toHaveBeenCalledTimes(1))
    expect(vi.mocked(setFollowup).mock.calls[0][0]).toBe('old1')
    expect(vi.mocked(setFollowup).mock.calls[0][1]).toBe('yes')
    // 本地也要立刻更新，后端没起时用户的选择不能白点。
    const saved = JSON.parse(localStorage.getItem('decision-debate:archive')!) as Array<{
      followup?: { regret: string }
    }>
    expect(saved[0].followup?.regret).toBe('yes')
  })
})
