import type { ReactElement } from 'react'
import type { LiveTurn, ModeratorNote as Note, Turn } from '../types'
import { SIDE_LABEL } from '../types'
import { Avatar } from './Avatar'
import { ModeratorNote } from './ModeratorNote'

type AnyTurn = { round: number; side: Turn['side']; content: string }

function TurnBubble({ turn, speaking }: { turn: AnyTurn; speaking?: boolean }) {
  const isData = turn.side === 'data'
  return (
    <div className={`flex ${isData ? 'justify-start' : 'justify-end'}`}>
      <div
        className={`flex max-w-[82%] items-start gap-2 ${
          isData ? '' : 'flex-row-reverse'
        }`}
      >
        <Avatar side={turn.side} speaking={speaking} size={36} />
        <div
          className={`rounded-2xl px-4 py-2 text-sm leading-relaxed shadow-sm ${
            isData
              ? 'rounded-tl-sm bg-data-soft text-data-deep'
              : 'rounded-tr-sm bg-life-soft text-life-deep'
          }`}
        >
          <div className="mb-1 text-[11px] opacity-70">
            {SIDE_LABEL[turn.side]} · 第 {turn.round} 轮
          </div>
          <span>
            {turn.content}
            {speaking && (
              <span className="ml-0.5 inline-block h-4 w-1.5 translate-y-0.5 animate-pulse bg-current" />
            )}
          </span>
        </div>
      </div>
    </div>
  )
}

// 把"已完成轮次 + 正在流式的一帧 + 主持人笔记"按轮次重排成正确顺序的聊天流。
// 这样即便后端把 R1 两方并行推过来，前端展示依然是一轮一轮往下走。
export function DebateStream({
  turns,
  notes,
  live,
}: {
  turns: Turn[]
  notes: Note[]
  live: LiveTurn | null
}) {
  const rounds = Math.max(0, ...turns.map((t) => t.round), live ? live.round : 0)
  const items: ReactElement[] = []
  for (let r = 1; r <= rounds; r++) {
    for (const t of turns.filter((t) => t.round === r)) {
      items.push(<TurnBubble key={`t-${r}-${t.side}`} turn={t} />)
    }
    if (live && live.round === r) {
      items.push(<TurnBubble key="live" turn={live} speaking />)
    }
    const n = notes.find((n) => n.round === r)
    if (n) items.push(<ModeratorNote key={`n-${r}`} note={n} />)
  }
  return <div className="space-y-3">{items}</div>
}
