import { describe, it, expect } from 'vitest'
import { SSEParser } from './sse'
import type { Frame } from '../types'

function frame(kind: string, extra: Record<string, unknown> = {}): string {
  return `event: ${kind}\ndata: ${JSON.stringify({ kind, ...extra })}\n\n`
}

describe('SSEParser', () => {
  it('解析多帧 SSE 文本，类型与字段正确', () => {
    const raw =
      frame('role_assign', {
        data: [
          { side: 'data', option_id: 'a', reason: 'r1' },
          { side: 'life', option_id: 'b', reason: 'r2' },
        ],
      }) +
      frame('turn_start', { round: 1, side: 'life' }) +
      frame('delta', { round: 1, side: 'life', text: '你好' }) +
      frame('turn_end', {
        round: 1,
        side: 'life',
        data: { round: 1, side: 'life', content: '你好', model: 'm', prompt_tokens: 1, completion_tokens: 2, latency_ms: 3 },
      }) +
      frame('moderator', { data: { round: 1, unresolved: ['x'], repeated: [] } }) +
      frame('done', { data: { dilemma_id: 'd1', assumptions: [] } })

    const frames = new SSEParser().push(raw)
    expect(frames.map((f) => f.kind)).toEqual([
      'role_assign',
      'turn_start',
      'delta',
      'turn_end',
      'moderator',
      'done',
    ])
    expect(frames[0].data).toEqual([
      { side: 'data', option_id: 'a', reason: 'r1' },
      { side: 'life', option_id: 'b', reason: 'r2' },
    ])
    expect((frames[3].data as { content: string }).content).toBe('你好')
  })

  it('跨块拆分同一事件也能正确拼回', () => {
    const parser = new SSEParser()
    const full = frame('delta', { round: 2, side: 'data', text: '分段' })
    const mid = full.indexOf('\n\n') // 在分隔符处切开
    const first = full.slice(0, mid)
    const second = full.slice(mid)
    expect(parser.push(first)).toEqual([]) // 事件未完整，先不吐
    const got = parser.push(second)
    expect(got).toHaveLength(1)
    expect(got[0].kind).toBe('delta')
    expect((got[0] as Frame).text).toBe('分段')
  })

  it('忽略心跳注释帧 : ping', () => {
    const raw = ': ping\n\n' + frame('turn_start', { round: 1, side: 'data' }) + ': ping\n\n'
    const frames = new SSEParser().push(raw)
    expect(frames).toHaveLength(1)
    expect(frames[0].kind).toBe('turn_start')
  })

  it('单行 data 损坏只丢该帧，不影响其他帧', () => {
    const raw =
      frame('turn_start', { round: 1, side: 'data' }) +
      'event: delta\ndata: {这不是合法json\n\n' +
      frame('turn_end', { round: 1, side: 'data', data: { content: 'ok' } })
    const frames = new SSEParser().push(raw)
    expect(frames.map((f) => f.kind)).toEqual(['turn_start', 'turn_end'])
  })

  it('flush 处理流尾残留缓冲', () => {
    const parser = new SSEParser()
    parser.push(frame('done', { data: { dilemma_id: 'x' } }).slice(0, -1)) // 去掉末尾 \n\n
    const flushed = parser.flush()
    expect(flushed).toHaveLength(1)
    expect(flushed[0].kind).toBe('done')
  })
})
