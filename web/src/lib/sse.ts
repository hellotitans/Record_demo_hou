import type { Frame, FrameKind } from '../types'

// 增量 SSE 解析器。
//
// fetch + ReadableStream 把响应切成任意大小的块喂进来，一个 SSE 事件可能被
// 切成两半。解析器跨块保留未完成的事件缓冲，只有遇到标准 `\n\n` 分隔符
// 才把一个完整事件交出去。单行损坏（JSON 解析失败）只丢那一帧，绝不整场崩。
export class SSEParser {
  private buf = ''

  push(chunk: string): Frame[] {
    this.buf += chunk
    const frames: Frame[] = []
    let idx: number
    while ((idx = this.buf.indexOf('\n\n')) !== -1) {
      const rawEvent = this.buf.slice(0, idx)
      this.buf = this.buf.slice(idx + 2)
      const f = parseOneEvent(rawEvent)
      if (f) frames.push(f)
    }
    return frames
  }

  // 流结束时把残留缓冲再尝试解析一次（后端正常以 done 收尾，这里只是兜底）。
  flush(): Frame[] {
    if (this.buf.trim().length === 0) return []
    const f = parseOneEvent(this.buf)
    this.buf = ''
    return f ? [f] : []
  }
}

function parseOneEvent(raw: string): Frame | null {
  let data = ''
  let event = ''
  for (const line of raw.split('\n')) {
    if (line === '' || line.startsWith(':')) continue // 空行 / 心跳注释
    const colon = line.indexOf(':')
    if (colon === -1) continue
    const field = line.slice(0, colon)
    const value = line.slice(colon + 1).replace(/^ /, '')
    if (field === 'data') data += (data ? '\n' : '') + value
    else if (field === 'event') event = value
  }
  if (!data) return null
  try {
    const parsed = JSON.parse(data) as Frame
    // data 里的 kind 是权威来源；少数实现若只发 event: 行，则用 event 兜底。
    if (!parsed.kind && event) parsed.kind = event as FrameKind
    return parsed
  } catch {
    return null
  }
}

export interface DebateHandlers {
  onFrame: (frame: Frame) => void
  onError?: (err: Error) => void
  signal?: AbortSignal
}

// 消费 POST /api/debate 的 SSE 流。
// 浏览器原生 EventSource 只支持 GET，而我们的接口是 POST，所以必须 fetch + ReadableStream。
// fetchImpl 可注入，便于单测用假响应驱动，而不依赖真实网络。
export async function streamDebate(
  url: string,
  body: unknown,
  handlers: DebateHandlers,
  fetchImpl: typeof fetch = fetch,
): Promise<void> {
  const parser = new SSEParser()

  let resp: Response
  try {
    resp = await fetchImpl(url, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
      signal: handlers.signal,
    })
  } catch (e) {
    handlers.onError?.(e as Error)
    return
  }

  if (!resp.ok || !resp.body) {
    handlers.onError?.(new Error(`辩论请求失败：HTTP ${resp.status}`))
    return
  }

  const reader = resp.body.getReader()
  const decoder = new TextDecoder()
  try {
    for (;;) {
      const { value, done } = await reader.read()
      if (done) break
      const text = decoder.decode(value, { stream: true })
      for (const f of parser.push(text)) handlers.onFrame(f)
    }
    for (const f of parser.flush()) handlers.onFrame(f)
  } catch (e) {
    if ((e as Error).name === 'AbortError') return
    handlers.onError?.(e as Error)
  }
}
