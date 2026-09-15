import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { DecisionArchive } from './DecisionArchive'
import type { DecisionRecord } from '../types'

function old(over: Partial<DecisionRecord> = {}): DecisionRecord {
  return {
    id: 'd1',
    question: '买 iPhone 还是安卓？',
    optionA: '买iPhone',
    optionB: '买安卓',
    createdAt: new Date(Date.now() - 200 * 24 * 60 * 60 * 1000).toISOString(), // 200 天前，超 3 个月
    assumptionCount: 2,
    ...over,
  }
}

function recent(over: Partial<DecisionRecord> = {}): DecisionRecord {
  return {
    id: 'd2',
    question: '最近的决定',
    optionA: 'A',
    optionB: 'B',
    createdAt: new Date().toISOString(),
    assumptionCount: 1,
    ...over,
  }
}

describe('DecisionArchive', () => {
  it('空档案显示引导文案', () => {
    render(<DecisionArchive records={[]} onFollowup={() => {}} />)
    expect(screen.getByText(/还没有决策档案/)).toBeInTheDocument()
  })

  it('超期且未回访 → 显示回访卡，点击触发 onFollowup', () => {
    const onFollowup = vi.fn()
    render(<DecisionArchive records={[old()]} onFollowup={onFollowup} />)
    expect(screen.getByText(/三个月过去了/)).toBeInTheDocument()
    fireEvent.click(screen.getByLabelText('回访-后悔'))
    expect(onFollowup).toHaveBeenCalledWith('d1', 'yes')
  })

  it('近期决策 → 不弹回访，只展示记录', () => {
    render(<DecisionArchive records={[recent()]} onFollowup={() => {}} />)
    expect(screen.queryByText(/三个月过去了/)).not.toBeInTheDocument()
    expect(screen.getByText('最近的决定')).toBeInTheDocument()
  })

  it('已回访 → 显示已回访态，不再弹回访', () => {
    render(
      <DecisionArchive
        records={[old({ followup: { regret: 'no', answeredAt: new Date().toISOString() } })]}
        onFollowup={() => {}}
      />,
    )
    expect(screen.queryByText(/三个月过去了/)).not.toBeInTheDocument()
    expect(screen.getByText(/已回访/)).toBeInTheDocument()
  })
})
