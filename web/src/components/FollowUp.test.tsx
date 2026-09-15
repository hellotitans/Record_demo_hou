import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { FollowUp } from './FollowUp'
import type { DecisionRecord } from '../types'

const record: DecisionRecord = {
  id: 'd1',
  question: '买 iPhone 还是安卓？',
  optionA: '买iPhone',
  optionB: '买安卓',
  createdAt: new Date().toISOString(),
  assumptionCount: 2,
}

describe('FollowUp', () => {
  it('渲染回访问题并带上决策问题', () => {
    render(<FollowUp record={record} onAnswer={() => {}} />)
    expect(screen.getByText(/三个月过去了/)).toBeInTheDocument()
    expect(screen.getByText(/买 iPhone 还是安卓？/)).toBeInTheDocument()
  })

  it('点「后悔」→ onAnswer("yes")', () => {
    const onAnswer = vi.fn()
    render(<FollowUp record={record} onAnswer={onAnswer} />)
    fireEvent.click(screen.getByLabelText('回访-后悔'))
    expect(onAnswer).toHaveBeenCalledWith('yes')
  })

  it('点「不后悔」→ onAnswer("no")', () => {
    const onAnswer = vi.fn()
    render(<FollowUp record={record} onAnswer={onAnswer} />)
    fireEvent.click(screen.getByLabelText('回访-不后悔'))
    expect(onAnswer).toHaveBeenCalledWith('no')
  })
})
