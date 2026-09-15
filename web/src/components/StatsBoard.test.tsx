import { describe, expect, it } from 'vitest'
import { render, screen } from '@testing-library/react'
import { StatsBoard } from './StatsBoard'
import type { DecisionStats } from '../types'

function stats(over: Partial<DecisionStats> = {}): DecisionStats {
  return {
    total: 4,
    answered: 2,
    pending: 1,
    regretYes: 1,
    regretNo: 1,
    regretRate: 0.5,
    byCategory: [
      { category: 'phone', total: 2, answered: 2, regretYes: 1, regretRate: 0.5 },
      { category: '', total: 2, answered: 0, regretYes: 0, regretRate: 0 },
    ],
    byMonth: [{ month: '2026-08', total: 4, answered: 2, regretYes: 1, regretRate: 0.5 }],
    regretted: [
      {
        id: 'r1',
        question: '买 iPhone 还是安卓',
        optionA: 'iPhone',
        optionB: '安卓',
        createdAt: '2026-08-31T00:00:00Z',
        assumptionCount: 3,
        category: 'phone',
        followup: { regret: 'yes', answeredAt: '2026-09-09T00:00:00Z' },
      },
    ],
    ...over,
  }
}

describe('StatsBoard', () => {
  it('后端不可用（null）时整块不渲染', () => {
    const { container } = render(<StatsBoard stats={null} />)
    expect(container.firstChild).toBeNull()
  })

  it('还没有任何决策时不渲染', () => {
    const { container } = render(<StatsBoard stats={stats({ total: 0 })} />)
    expect(container.firstChild).toBeNull()
  })

  it('渲染四个总览数字', () => {
    render(<StatsBoard stats={stats()} />)
    for (const label of ['总决策', '已回访', '后悔率', '待回访']) {
      expect(screen.getByText(label)).toBeTruthy()
    }
    // 值：total 4 / answered 2 / rate 50% / pending 1
    expect(screen.getByText('4')).toBeTruthy()
    expect(screen.getByText('50%')).toBeTruthy()
  })

  it('品类用模板的中文名渲染，未分类显示为「未分类」', () => {
    // regretted 置空：否则同一个品类名会在"最后悔的决策"里再出现一次，断言会命中多个。
    render(<StatsBoard stats={stats({ regretted: [] })} />)
    expect(screen.getByText(/数码产品购买/)).toBeTruthy()
    expect(screen.getByText('未分类')).toBeTruthy()
  })

  it('品类排行带着样本量，不裸给百分比', () => {
    // 只留一个品类，避免"（2 条）"同时命中多行。
    render(
      <StatsBoard
        stats={stats({
          regretted: [],
          byCategory: [{ category: 'phone', total: 2, answered: 2, regretYes: 1, regretRate: 0.5 }],
        })}
      />,
    )
    // "后悔 1/2" + "（2 条）"：1/1=100% 和 40/40=100% 不是一回事，样本量必须可见。
    expect(screen.getByText(/1\/2/)).toBeTruthy()
    expect(screen.getByText(/（2 条）/)).toBeTruthy()
  })

  it('品类按后端给的顺序（后悔率降序）渲染', () => {
    render(
      <StatsBoard
        stats={stats({
          regretted: [],
          byCategory: [
            { category: 'city', total: 1, answered: 1, regretYes: 1, regretRate: 1 },
            { category: 'phone', total: 2, answered: 2, regretYes: 1, regretRate: 0.5 },
          ],
        })}
      />,
    )
    const rows = screen.getAllByText(/城市选择|数码产品购买/)
    expect(rows[0].textContent).toContain('城市选择')
    expect(rows[1].textContent).toContain('数码产品购买')
  })

  it('月度趋势渲染月份与条数', () => {
    render(<StatsBoard stats={stats()} />)
    expect(screen.getByText('2026-08')).toBeTruthy()
    expect(screen.getByText(/4 条/)).toBeTruthy()
  })

  it('时间未知的月份显示为「未知」而不是空白', () => {
    render(
      <StatsBoard
        stats={stats({ byMonth: [{ month: '', total: 1, answered: 0, regretYes: 0, regretRate: 0 }] })}
      />,
    )
    expect(screen.getByText('未知')).toBeTruthy()
  })

  it('最后悔的决策列出问题与日期', () => {
    render(<StatsBoard stats={stats()} />)
    expect(screen.getByText('买 iPhone 还是安卓')).toBeTruthy()
    expect(screen.getByText(/2026-08-31/)).toBeTruthy()
  })

  it('后悔列表为空时不渲染该区块', () => {
    render(<StatsBoard stats={stats({ regretted: [] })} />)
    expect(screen.queryByText('最后悔的决策')).toBeNull()
  })
})
