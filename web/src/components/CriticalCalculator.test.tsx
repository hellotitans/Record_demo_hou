import { describe, it, expect } from 'vitest'
import { render, screen, fireEvent } from '@testing-library/react'
import type { Assumption } from '../types'
import { CriticalCalculator } from './CriticalCalculator'

// React 的 onChange 对 range 监听的是 input 事件，fireEvent.change 派发的是
// change 事件在 range 下不生效；用 fireEvent.input 才能可靠触发状态更新。
function setRange(input: HTMLInputElement, value: string) {
  fireEvent.input(input, { target: { value } })
}

// 两条典型假设：数据派押"房价年涨幅≥3%"，生活派押"月供占比≤40%"。
const assumptions: Assumption[] = [
  {
    id: 'asm-1',
    side: 'data',
    statement: '房价年涨幅不低于 3%',
    variable: '房价年涨幅',
    operator: '>=',
    value: 3,
    unit: '%',
  },
  {
    id: 'asm-2',
    side: 'life',
    statement: '月供占收入不高于 40%',
    variable: '月供占比',
    operator: '<=',
    value: 40,
    unit: '%',
  },
]

describe('CriticalCalculator', () => {
  it('默认双方都站在假设线上 → 平局', () => {
    render(<CriticalCalculator assumptions={assumptions} />)
    expect(screen.getByText('平局')).toBeInTheDocument()
    // 两条假设各自的临界提示都应出现
    expect(screen.getByText(/低于 3%/)).toBeInTheDocument()
    expect(screen.getByText(/高于 40%/)).toBeInTheDocument()
  })

  it('把"房价年涨幅"拖到 2 → 数据派破，生活派胜出', async () => {
    render(<CriticalCalculator assumptions={assumptions} />)
    const slider = screen.getByLabelText('滑块 房价年涨幅') as HTMLInputElement
    setRange(slider, '2')
    // 数据派假设破（2 < 3），生活派仍成立 → 生活派。
    // 断言用结论卡片里唯一的 subLine（胜方标签也会出现在假设静态文案中，不能用来定位）。
    expect(await screen.findByText(/生活派 的论证在当前取值下仍站得住/)).toBeInTheDocument()
  })

  it('把"月供占比"拖到 50 → 生活派破，数据派胜出', async () => {
    render(<CriticalCalculator assumptions={assumptions} />)
    const slider = screen.getByLabelText('滑块 月供占比') as HTMLInputElement
    setRange(slider, '50')
    expect(await screen.findByText(/数据派 的论证在当前取值下仍站得住/)).toBeInTheDocument()
  })

  it('复原按钮把取值拉回假设线（平局）', async () => {
    render(<CriticalCalculator assumptions={assumptions} />)
    const slider = screen.getByLabelText('滑块 房价年涨幅') as HTMLInputElement
    setRange(slider, '1')
    expect(await screen.findByText(/生活派 的论证在当前取值下仍站得住/)).toBeInTheDocument()
    fireEvent.click(screen.getByText('复原到假设线'))
    expect(await screen.findByText(/两边都还站在自己的假设线上/)).toBeInTheDocument()
    // 复原后滑块回到假设线 3
    expect((screen.getByLabelText('滑块 房价年涨幅') as HTMLInputElement).value).toBe('3')
  })
})
