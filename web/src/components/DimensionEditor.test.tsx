import { describe, it, expect, vi } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import { useState } from 'react'
import type { Dimension } from '../types'
import { DimensionEditor } from './DimensionEditor'

const initial: Dimension[] = [
  { key: 'a', label: '预算', weight: 0.3, source: 'template' },
  { key: 'b', label: '影像', weight: 0.2, source: 'template' },
]

// DimensionEditor 是受控组件：用带状态的 wrapper 把它接到真实 state，
// 才能验证"拖权重 → 显示更新"这类交互（否则 onChange 是空操作，显示不会变）。
function Harness({ initial }: { initial: Dimension[] }) {
  const [dims, setDims] = useState<Dimension[]>(initial)
  return <DimensionEditor dimensions={dims} onChange={setDims} />
}

describe('DimensionEditor', () => {
  it('渲染模板带来的两条维度', () => {
    render(<DimensionEditor dimensions={initial} onChange={() => {}} />)
    expect(screen.getAllByTestId('dim-row')).toHaveLength(2)
    expect(screen.getByText('30%')).toBeInTheDocument()
    expect(screen.getByText('20%')).toBeInTheDocument()
  })

  it('删除一条维度 → 剩一条', () => {
    const onChange = vi.fn()
    render(<DimensionEditor dimensions={initial} onChange={onChange} />)
    fireEvent.click(screen.getByLabelText('删除维度 预算'))
    // onChange 收到的是去掉"预算"后的新数组
    expect(onChange).toHaveBeenCalledWith([initial[1]])
  })

  it('添加一条维度 → 数组 +1 且新项 source=user', () => {
    const onChange = vi.fn()
    render(<DimensionEditor dimensions={initial} onChange={onChange} />)
    fireEvent.change(screen.getByLabelText('新维度名'), { target: { value: '通勤时间' } })
    fireEvent.click(screen.getByText('添加维度'))
    const next = onChange.mock.calls[0][0] as Dimension[]
    expect(next).toHaveLength(3)
    expect(next[2].source).toBe('user')
    expect(next[2].label).toBe('通勤时间')
    expect(next[2].weight).toBe(0.2)
  })

  it('拖动权重滑块 → 百分比显示更新', () => {
    render(<Harness initial={initial} />)
    const slider = screen.getByLabelText('权重 预算') as HTMLInputElement
    fireEvent.input(slider, { target: { value: '50' } })
    // 第一条（预算）应变成 50%，且全屏唯一
    expect(screen.getByText('50%')).toBeInTheDocument()
  })

  it('空维度显示引导文案', () => {
    render(<DimensionEditor dimensions={[]} onChange={() => {}} />)
    expect(screen.getByText(/还没有决策维度/)).toBeInTheDocument()
  })
})
