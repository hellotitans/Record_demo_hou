import type { DecisionRecord } from '../types'
import { isDueForFollowup } from '../lib/archive'
import { FollowUp } from './FollowUp'

interface DecisionArchiveProps {
  records: DecisionRecord[]
  onFollowup: (id: string, regret: 'yes' | 'no') => void
}

// 决策档案列表：每场辩论的结论存档。超期且未回访的会内联弹出回访卡。
export function DecisionArchive({ records, onFollowup }: DecisionArchiveProps) {
  if (records.length === 0) {
    return (
      <div className="mt-6 rounded-2xl border border-dashed border-slate-200 bg-white/50 p-4 text-center text-xs text-slate-400">
        还没有决策档案。每场辩论结束后会自动存到这里，三个月后回来问问自己后不后悔。
      </div>
    )
  }

  return (
    <div className="mt-6 space-y-3">
      <div className="text-xs font-semibold text-slate-500">决策档案（{records.length}）</div>
      {records.map((r) => (
        <div
          key={r.id}
          className="rounded-2xl border border-slate-200 bg-white/70 p-4"
          data-testid="archive-item"
        >
          <div className="text-sm font-medium text-slate-800">{r.question}</div>
          <div className="mt-0.5 text-xs text-slate-400">
            {r.createdAt.slice(0, 10)} · {r.optionA} vs {r.optionB} · 假设 {r.assumptionCount} 条
          </div>

          {r.followup ? (
            <div className="mt-2 text-xs text-emerald-700" data-testid="followup-done">
              已回访：{r.followup.regret === 'yes' ? '后悔' : '不后悔'} ·{' '}
              {r.followup.answeredAt.slice(0, 10)}
            </div>
          ) : isDueForFollowup(r) ? (
            <FollowUp record={r} onAnswer={(regret) => onFollowup(r.id, regret)} />
          ) : null}
        </div>
      ))}
    </div>
  )
}
