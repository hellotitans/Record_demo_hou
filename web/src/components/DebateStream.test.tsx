import { describe, it, expect } from 'vitest'
import { render, screen } from '@testing-library/react'
import { DebateStream } from './DebateStream'
import type { ModeratorNote as Note, Turn } from '../types'

const turns: Turn[] = [
  { round: 1, side: 'data', content: '数据说买安卓', model: 'm', prompt_tokens: 1, completion_tokens: 1, latency_ms: 1 },
  { round: 1, side: 'life', content: '生活说买iPhone', model: 'm', prompt_tokens: 1, completion_tokens: 1, latency_ms: 1 },
]

describe('DebateStream', () => {
  it('渲染已完成的双发言', () => {
    render(<DebateStream turns={turns} notes={[]} live={null} />)
    expect(screen.getByText('数据说买安卓')).toBeInTheDocument()
    expect(screen.getByText('生活说买iPhone')).toBeInTheDocument()
  })

  it('正在流式的 live 帧显示内容 + 光标（打字机效果）', () => {
    const { container } = render(
      <DebateStream turns={[]} notes={[]} live={{ round: 2, side: 'data', content: '正在输入' }} />,
    )
    expect(screen.getByText('正在输入')).toBeInTheDocument()
    // 光标：animate-pulse 的元素
    expect(container.querySelector('.animate-pulse')).toBeTruthy()
  })

  it('主持人笔记按轮渲染', () => {
    const notes: Note[] = [{ round: 1, unresolved: ['价格敏感度'], repeated: [] }]
    render(<DebateStream turns={turns} notes={notes} live={null} />)
    expect(screen.getByText('未解分歧：')).toBeInTheDocument()
    expect(screen.getByText('价格敏感度')).toBeInTheDocument()
  })

  it('轮次顺序：第一轮两发言在前，第二轮在后', () => {
    const twoRounds: Turn[] = [
      ...turns,
      { round: 2, side: 'life', content: '第二轮生活派', model: 'm', prompt_tokens: 1, completion_tokens: 1, latency_ms: 1 },
    ]
    render(<DebateStream turns={twoRounds} notes={[]} live={null} />)
    const order = [
      screen.getByText('数据说买安卓'),
      screen.getByText('生活说买iPhone'),
      screen.getByText('第二轮生活派'),
    ].map((el) => el.textContent)
    expect(order).toEqual(['数据说买安卓', '生活说买iPhone', '第二轮生活派'])
  })
})
